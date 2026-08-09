// Package storage owns the versioned manifest, the immutable pack key
// grammar, and the semantic object-store boundary. The manifest is the only
// authoritative state index; strict validation runs before any manifest
// drives a download or a local Git operation. AWS SDK types never cross the
// ObjectStore boundary.
package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// ErrIntegrity reports stored state that failed validation: a malformed
// manifest, pack bytes that contradict a descriptor, or a unique-key
// collision.
var ErrIntegrity = errors.New("storage: integrity failure")

// CurrentKey is the protocol key of the only authoritative state index
// (architecture section 9.2). An absent current object is the implicit
// empty-notebook state at generation 0.
const CurrentKey = "current"

// Manifest is a validated manifest version 1 value. Field order and names
// follow the normative shape in architecture section 9.2 exactly; the
// encoder writes compact JSON with HTML escaping disabled and no trailing
// newline.
type Manifest struct {
	Version    int         `json:"version"`
	Generation uint64      `json:"generation"`
	Head       git.OID     `json:"head"`
	Checkpoint Checkpoint  `json:"checkpoint"`
	Increments []Increment `json:"increments"`
	Retained   []Retained  `json:"retained"`
}

// Checkpoint is the active checkpoint descriptor: one complete state pack
// plus the publication ID of its head commit.
type Checkpoint struct {
	ID                UUID    `json:"id"`
	Publication       UUID    `json:"publication"`
	ThroughGeneration uint64  `json:"throughGeneration"`
	Head              git.OID `json:"head"`
	Key               Key     `json:"key"`
	SHA256            SHA256  `json:"sha256"`
	Size              uint64  `json:"size"`
}

// Increment is one accepted incremental pack descriptor. Parent is the
// head the increment builds on; Head is the published head.
type Increment struct {
	Generation  uint64  `json:"generation"`
	Publication UUID    `json:"publication"`
	Parent      git.OID `json:"parent"`
	Head        git.OID `json:"head"`
	Key         Key     `json:"key"`
	SHA256      SHA256  `json:"sha256"`
	Size        uint64  `json:"size"`
}

// Retained is one retained generation: the complete descriptor chain of an
// accepted state replaced by a newer checkpoint. It reconstructs the exact
// replaced state without any external pack.
type Retained struct {
	RetiredAtGeneration uint64      `json:"retiredAtGeneration"`
	Head                git.OID     `json:"head"`
	Checkpoint          Checkpoint  `json:"checkpoint"`
	Increments          []Increment `json:"increments"`
}

// DecodeManifest strictly decodes and validates a stored manifest. It
// rejects unknown fields, duplicate names, missing required fields, and
// explicit null at every object level, and applies every cross-field rule
// of architecture section 9.2 before returning. Any failure is an
// ErrIntegrity error; the caller must not touch referenced packs.
func DecodeManifest(data []byte) (Manifest, error) {
	root, err := strictjson.Parse(data)
	if err != nil {
		return Manifest{}, fmt.Errorf("storage: decode manifest: %v: %w", err, ErrIntegrity)
	}
	if root.Kind != strictjson.Object {
		return Manifest{}, fmt.Errorf("storage: decode manifest: root is not an object: %w", ErrIntegrity)
	}
	ver, err := requiredUint(root, "version", "")
	if err != nil {
		return Manifest{}, integrityErr(err)
	}
	if ver != 1 {
		return Manifest{}, fmt.Errorf("storage: decode manifest: unsupported version %d: %w", ver, ErrIntegrity)
	}
	m, err := decodeManifest(root)
	if err != nil {
		return Manifest{}, integrityErr(err)
	}
	if err := validateManifest(&m); err != nil {
		return Manifest{}, integrityErr(err)
	}
	return m, nil
}

// EncodeManifest validates m and encodes it as compact JSON in the
// normative field order, with HTML escaping disabled and no trailing
// newline. An invalid manifest is rejected before any bytes are produced.
func EncodeManifest(m Manifest) ([]byte, error) {
	if m.Increments == nil {
		m.Increments = []Increment{}
	}
	if m.Retained == nil {
		m.Retained = []Retained{}
	}
	if err := validateManifest(&m); err != nil {
		return nil, integrityErr(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("storage: encode manifest: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// decodeManifest converts a validated value tree into a Manifest. Field
// grammar errors carry a field path for diagnostics.
func decodeManifest(root strictjson.Value) (Manifest, error) {
	if err := root.RejectUnknown("version", "generation", "head", "checkpoint", "increments", "retained"); err != nil {
		return Manifest{}, fmt.Errorf("storage: manifest: %w", err)
	}
	m := Manifest{}
	m.Version = 1
	gen, err := requiredUint(root, "generation", "manifest")
	if err != nil {
		return Manifest{}, err
	}
	m.Generation = gen
	headStr, err := requiredString(root, "head", "manifest")
	if err != nil {
		return Manifest{}, err
	}
	head, err := git.ParseOID(headStr)
	if err != nil {
		return Manifest{}, fmt.Errorf("storage: manifest.head: %w", err)
	}
	m.Head = head

	cpVal, err := requiredValue(root, "checkpoint", "manifest", strictjson.Object)
	if err != nil {
		return Manifest{}, err
	}
	cp, err := decodeCheckpoint(cpVal, "manifest.checkpoint")
	if err != nil {
		return Manifest{}, err
	}
	m.Checkpoint = cp

	incsVal, err := requiredValue(root, "increments", "manifest", strictjson.Array)
	if err != nil {
		return Manifest{}, err
	}
	incs, err := decodeIncrements(incsVal, "manifest.increments")
	if err != nil {
		return Manifest{}, err
	}
	m.Increments = incs

	retVal, err := requiredValue(root, "retained", "manifest", strictjson.Array)
	if err != nil {
		return Manifest{}, err
	}
	ret, err := decodeRetained(retVal, "manifest.retained")
	if err != nil {
		return Manifest{}, err
	}
	m.Retained = ret
	return m, nil
}

func decodeCheckpoint(v strictjson.Value, path string) (Checkpoint, error) {
	if err := v.RejectUnknown("id", "publication", "throughGeneration", "head", "key", "sha256", "size"); err != nil {
		return Checkpoint{}, fmt.Errorf("storage: %s: %w", path, err)
	}
	var cp Checkpoint
	id, err := requiredUUID(v, "id", path)
	if err != nil {
		return Checkpoint{}, err
	}
	cp.ID = id
	pub, err := requiredUUID(v, "publication", path)
	if err != nil {
		return Checkpoint{}, err
	}
	cp.Publication = pub
	through, err := requiredUint(v, "throughGeneration", path)
	if err != nil {
		return Checkpoint{}, err
	}
	cp.ThroughGeneration = through
	head, err := requiredOID(v, "head", path)
	if err != nil {
		return Checkpoint{}, err
	}
	cp.Head = head
	key, err := requiredKey(v, "key", path)
	if err != nil {
		return Checkpoint{}, err
	}
	cp.Key = key
	sha, err := requiredSHA256(v, "sha256", path)
	if err != nil {
		return Checkpoint{}, err
	}
	cp.SHA256 = sha
	size, err := requiredUint(v, "size", path)
	if err != nil {
		return Checkpoint{}, err
	}
	cp.Size = size
	return cp, nil
}

func decodeIncrements(v strictjson.Value, path string) ([]Increment, error) {
	incs := make([]Increment, 0, len(v.Arr))
	for i, item := range v.Arr {
		if item.Kind != strictjson.Object {
			return nil, fmt.Errorf("storage: %s[%d]: must be an object", path, i)
		}
		if err := item.RejectUnknown("generation", "publication", "parent", "head", "key", "sha256", "size"); err != nil {
			return nil, fmt.Errorf("storage: %s[%d]: %w", path, i, err)
		}
		var inc Increment
		gen, err := requiredUint(item, "generation", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		inc.Generation = gen
		pub, err := requiredUUID(item, "publication", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		inc.Publication = pub
		parent, err := requiredOID(item, "parent", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		inc.Parent = parent
		head, err := requiredOID(item, "head", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		inc.Head = head
		key, err := requiredKey(item, "key", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		inc.Key = key
		sha, err := requiredSHA256(item, "sha256", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		inc.SHA256 = sha
		size, err := requiredUint(item, "size", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		inc.Size = size
		incs = append(incs, inc)
	}
	return incs, nil
}

func decodeRetained(v strictjson.Value, path string) ([]Retained, error) {
	ret := make([]Retained, 0, len(v.Arr))
	for i, item := range v.Arr {
		if item.Kind != strictjson.Object {
			return nil, fmt.Errorf("storage: %s[%d]: must be an object", path, i)
		}
		if err := item.RejectUnknown("retiredAtGeneration", "head", "checkpoint", "increments"); err != nil {
			return nil, fmt.Errorf("storage: %s[%d]: %w", path, i, err)
		}
		var r Retained
		at, err := requiredUint(item, "retiredAtGeneration", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		r.RetiredAtGeneration = at
		head, err := requiredOID(item, "head", fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		r.Head = head
		cpVal, err := requiredValue(item, "checkpoint", fmt.Sprintf("%s[%d]", path, i), strictjson.Object)
		if err != nil {
			return nil, err
		}
		cp, err := decodeCheckpoint(cpVal, fmt.Sprintf("%s[%d].checkpoint", path, i))
		if err != nil {
			return nil, err
		}
		r.Checkpoint = cp
		incsVal, err := requiredValue(item, "increments", fmt.Sprintf("%s[%d]", path, i), strictjson.Array)
		if err != nil {
			return nil, err
		}
		incs, err := decodeIncrements(incsVal, fmt.Sprintf("%s[%d].increments", path, i))
		if err != nil {
			return nil, err
		}
		r.Increments = incs
		ret = append(ret, r)
	}
	return ret, nil
}

// requiredValue returns the named field and checks its JSON kind.
func requiredValue(v strictjson.Value, name, path string, kind strictjson.Kind) (strictjson.Value, error) {
	f, ok := v.Field(name)
	if !ok {
		return strictjson.Value{}, fmt.Errorf("storage: %s: missing required field %q", path, name)
	}
	if f.Kind != kind {
		return strictjson.Value{}, fmt.Errorf("storage: %s.%s: wrong value kind", path, name)
	}
	return f, nil
}

func requiredUint(v strictjson.Value, name, path string) (uint64, error) {
	f, err := requiredValue(v, name, path, strictjson.Number)
	if err != nil {
		return 0, err
	}
	return f.Num, nil
}

func requiredString(v strictjson.Value, name, path string) (string, error) {
	f, err := requiredValue(v, name, path, strictjson.String)
	if err != nil {
		return "", err
	}
	return f.Str, nil
}

func requiredUUID(v strictjson.Value, name, path string) (UUID, error) {
	s, err := requiredString(v, name, path)
	if err != nil {
		return UUID{}, err
	}
	id, err := ParseUUIDv7(s)
	if err != nil {
		return UUID{}, fmt.Errorf("storage: %s.%s: %w", path, name, err)
	}
	return id, nil
}

func requiredOID(v strictjson.Value, name, path string) (git.OID, error) {
	s, err := requiredString(v, name, path)
	if err != nil {
		return git.OID{}, err
	}
	id, err := git.ParseOID(s)
	if err != nil {
		return git.OID{}, fmt.Errorf("storage: %s.%s: %w", path, name, err)
	}
	return id, nil
}

func requiredKey(v strictjson.Value, name, path string) (Key, error) {
	s, err := requiredString(v, name, path)
	if err != nil {
		return Key{}, err
	}
	k, err := ParseKey(s)
	if err != nil {
		return Key{}, fmt.Errorf("storage: %s.%s: %w", path, name, err)
	}
	return k, nil
}

func requiredSHA256(v strictjson.Value, name, path string) (SHA256, error) {
	s, err := requiredString(v, name, path)
	if err != nil {
		return SHA256{}, err
	}
	h, err := ParseSHA256(s)
	if err != nil {
		return SHA256{}, fmt.Errorf("storage: %s.%s: %w", path, name, err)
	}
	return h, nil
}

func integrityErr(err error) error {
	return fmt.Errorf("storage: manifest: %w: %v", ErrIntegrity, err)
}

// validateManifest applies the cross-field rules of architecture section
// 9.2 to an already schema-valid manifest.
func validateManifest(m *Manifest) error {
	if m.Generation == 0 {
		return errors.New("generation must be at least 1")
	}
	seenKeys := make(map[string]bool)
	seenCheckpointIDs := make(map[UUID]bool)
	pubHeads := make(map[UUID]git.OID)

	// checkChain validates one reconstructable chain (active or retained):
	// positive sizes, unique keys and checkpoint IDs, the key grammar
	// binding, the parent/head chain, the generation ordering, and the
	// publication-ID/head consistency. It returns the final tail head.
	checkChain := func(checkpoint Checkpoint, incs []Increment, context string) (git.OID, error) {
		if checkpoint.Size == 0 {
			return git.OID{}, fmt.Errorf("%s checkpoint size must be positive", context)
		}
		if err := checkDescriptorKey(checkpoint.Key, KindCheckpoint, checkpoint.ThroughGeneration, checkpoint.ID, context+".checkpoint"); err != nil {
			return git.OID{}, err
		}
		key := checkpoint.Key.String()
		if seenKeys[key] {
			return git.OID{}, fmt.Errorf("%s checkpoint key %q is duplicated", context, key)
		}
		seenKeys[key] = true
		if seenCheckpointIDs[checkpoint.ID] {
			return git.OID{}, fmt.Errorf("checkpoint id %s is duplicated", checkpoint.ID)
		}
		seenCheckpointIDs[checkpoint.ID] = true
		if h, dup := pubHeads[checkpoint.Publication]; dup && h != checkpoint.Head {
			return git.OID{}, fmt.Errorf("publication %s binds different commit heads", checkpoint.Publication)
		}
		pubHeads[checkpoint.Publication] = checkpoint.Head

		prevHead := checkpoint.Head
		prevGen := checkpoint.ThroughGeneration
		for i := range incs {
			inc := &incs[i]
			if inc.Size == 0 {
				return git.OID{}, fmt.Errorf("%s increment %d size must be positive", context, inc.Generation)
			}
			if inc.Generation <= prevGen {
				return git.OID{}, fmt.Errorf("%s increment generations must be greater than the checkpoint cutoff %d", context, prevGen)
			}
			if inc.Generation != prevGen+1 {
				return git.OID{}, fmt.Errorf("%s increment generations must be consecutive, %d followed by %d", context, prevGen, inc.Generation)
			}
			if inc.Parent != prevHead {
				return git.OID{}, fmt.Errorf("%s increment %d parent does not equal the preceding head", context, inc.Generation)
			}
			if err := checkDescriptorKey(inc.Key, KindIncrement, inc.Generation, inc.Publication, context+".increment"); err != nil {
				return git.OID{}, err
			}
			key := inc.Key.String()
			if seenKeys[key] {
				return git.OID{}, fmt.Errorf("%s increment key %q is duplicated", context, key)
			}
			seenKeys[key] = true
			if h, dup := pubHeads[inc.Publication]; dup && h != inc.Head {
				return git.OID{}, fmt.Errorf("publication %s binds different commit heads", inc.Publication)
			}
			pubHeads[inc.Publication] = inc.Head
			prevHead = inc.Head
			prevGen = inc.Generation
		}
		return prevHead, nil
	}

	finalHead, err := checkChain(m.Checkpoint, m.Increments, "active")
	if err != nil {
		return err
	}
	if m.Head != finalHead {
		return errors.New("manifest head does not equal the final tail head")
	}
	if m.Checkpoint.ThroughGeneration > m.Generation {
		return fmt.Errorf("active checkpoint cutoff %d exceeds generation %d", m.Checkpoint.ThroughGeneration, m.Generation)
	}

	lastRetired := m.Generation + 1
	for i := range m.Retained {
		r := &m.Retained[i]
		if r.RetiredAtGeneration >= lastRetired {
			return fmt.Errorf("retained generations must decrease below %d, got %d", lastRetired-1, r.RetiredAtGeneration)
		}
		lastRetired = r.RetiredAtGeneration
		final := r.Checkpoint.ThroughGeneration
		if len(r.Increments) > 0 {
			final = r.Increments[len(r.Increments)-1].Generation
		}
		if r.RetiredAtGeneration <= final {
			return fmt.Errorf("retained generation %d is not greater than its final content generation %d", r.RetiredAtGeneration, final)
		}
		finalHead, err := checkChain(r.Checkpoint, r.Increments, "retained")
		if err != nil {
			return err
		}
		if r.Head != finalHead {
			return errors.New("retained head does not equal its final tail head")
		}
	}
	return nil
}

// checkDescriptorKey verifies that a descriptor key names the right
// namespace and binds the descriptor's own generation and ID.
func checkDescriptorKey(k Key, kind PackKind, generation uint64, id UUID, path string) error {
	if k.Kind != kind {
		return fmt.Errorf("%s key %q names the wrong pack kind", path, k.String())
	}
	if k.Generation != generation {
		return fmt.Errorf("%s key generation %d does not match %d", path, k.Generation, generation)
	}
	if k.ID != id {
		return fmt.Errorf("%s key id %s does not match %s", path, k.ID, id)
	}
	return nil
}
