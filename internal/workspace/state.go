package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// state is the durable private-state record (architecture section 7.2).
// Field order matches the normative JSON shape exactly. Identity is the
// lowercase SHA-256 derived key; BaselineHead is the empty string only at
// remote generation 0; BaselineTree is the authoritative accepted baseline
// Git tree. The file snapshot cache is never persisted: the tree is the
// baseline.
type state struct {
	Version          int    `json:"version"`
	Identity         string `json:"identity"`
	RemoteGeneration uint64 `json:"remoteGeneration"`
	BaselineHead     string `json:"baselineHead"`
	BaselineTree     string `json:"baselineTree"`
	RecoveryRequired bool   `json:"recoveryRequired"`
}

// stateFileName and friends are the private-directory layout. The staging
// directory and the replaced-directory backup are private and transient;
// a leftover backup or staging directory is garbage from a failed
// replacement and is removed by the next mutation.
const (
	stateFileName     = "state.json"
	stateTmpName      = "state.json.tmp"
	operationLockName = "operation.lock"
	repoDirName       = "repo"
	stagingDirName    = "staging"
	backupDirName     = "backup"
	pulledMarkerName  = "pulled"
	cacheDirName      = "pack-cache"
)

// ErrRecoveryRequired reports that the private state requires recovery
// before any normal work (architecture section 7.2): the durable flag is
// set, the state record is corrupt, or an interrupted state write was
// detected. The caller resynchronizes P and L from the authoritative remote
// state and calls Recover with the reconstructed baseline.
var ErrRecoveryRequired = errors.New("workspace: private state requires recovery")

// ErrPartial reports a mutation that started but could not be completed:
// the visible directory or the private state is partially mutated and the
// workspace must be recovered before normal work.
var ErrPartial = errors.New("workspace: partial mutation; recovery required")

// Baseline is the accepted remote state of one workspace (architecture
// section 7.2). Head is the zero OID only at remote generation 0; Tree is
// the authoritative baseline Git tree.
type Baseline struct {
	RemoteGeneration uint64
	Head             git.OID
	Tree             git.OID
}

// EmptyTreeID is the canonical empty Git tree used as the baseline of a
// new workspace and as the merge base of the first pull (architecture
// section 7.2).
var EmptyTreeID = mustParseEmptyTree()

func mustParseEmptyTree() git.OID {
	id, err := git.ParseOID("4b825dc642cb6eb9a060e54bf8d69288fbee4904")
	if err != nil {
		panic("workspace: invalid canonical empty tree id: " + err.Error())
	}
	return id
}

// newWorkspaceState returns the strict initial record of a new workspace:
// remote generation 0, an empty baseline head, the canonical empty tree,
// and recoveryRequired=false.
func newWorkspaceState(derivedKey string) state {
	return state{
		Version:      1,
		Identity:     derivedKey,
		BaselineTree: EmptyTreeID.String(),
	}
}

// baseline converts the state record into the accepted baseline value.
// Head is empty only at remote generation 0, which the decoder enforces.
func (s state) baseline() (Baseline, error) {
	tree, err := git.ParseOID(s.BaselineTree)
	if err != nil {
		return Baseline{}, fmt.Errorf("workspace: baseline tree: %w", err)
	}
	var head git.OID
	if s.BaselineHead != "" {
		head, err = git.ParseOID(s.BaselineHead)
		if err != nil {
			return Baseline{}, fmt.Errorf("workspace: baseline head: %w", err)
		}
	}
	return Baseline{RemoteGeneration: s.RemoteGeneration, Head: head, Tree: tree}, nil
}

// encodeState validates the record and encodes it as compact JSON in the
// normative field order, with HTML escaping disabled and no trailing
// newline.
func encodeState(st state) ([]byte, error) {
	if err := validateState(st); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(st); err != nil {
		return nil, fmt.Errorf("workspace: encode state: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// decodeState strictly decodes and validates a stored state record: it
// rejects unknown, duplicate, missing, and null fields, a version other
// than 1, a non-canonical identity digest, and a baselineHead that is
// non-empty at generation 0 or empty past it.
func decodeState(data []byte) (state, error) {
	root, err := strictjson.Parse(data)
	if err != nil {
		return state{}, fmt.Errorf("workspace: decode state: %w", err)
	}
	if root.Kind != strictjson.Object {
		return state{}, errors.New("workspace: decode state: root is not an object")
	}
	if err := root.RejectUnknown("version", "identity", "remoteGeneration", "baselineHead", "baselineTree", "recoveryRequired"); err != nil {
		return state{}, fmt.Errorf("workspace: decode state: %w", err)
	}
	st := state{}

	ver, err := stateField(root, "version")
	if err != nil {
		return state{}, err
	}
	if ver.Kind != strictjson.Number {
		return state{}, errors.New("workspace: decode state: field \"version\" has the wrong kind")
	}
	if ver.Num != 1 {
		return state{}, fmt.Errorf("workspace: decode state: unsupported version %d", ver.Num)
	}
	st.Version = 1

	id, err := stateField(root, "identity")
	if err != nil {
		return state{}, err
	}
	if id.Kind != strictjson.String {
		return state{}, errors.New("workspace: decode state: field \"identity\" has the wrong kind")
	}
	if !validHex64(id.Str) {
		return state{}, fmt.Errorf("workspace: decode state: identity %q is not a canonical SHA-256 digest", id.Str)
	}
	st.Identity = id.Str

	gen, err := stateField(root, "remoteGeneration")
	if err != nil {
		return state{}, err
	}
	if gen.Kind != strictjson.Number {
		return state{}, errors.New("workspace: decode state: field \"remoteGeneration\" has the wrong kind")
	}
	st.RemoteGeneration = gen.Num

	head, err := stateField(root, "baselineHead")
	if err != nil {
		return state{}, err
	}
	if head.Kind != strictjson.String {
		return state{}, errors.New("workspace: decode state: field \"baselineHead\" has the wrong kind")
	}
	st.BaselineHead = head.Str

	tree, err := stateField(root, "baselineTree")
	if err != nil {
		return state{}, err
	}
	if tree.Kind != strictjson.String {
		return state{}, errors.New("workspace: decode state: field \"baselineTree\" has the wrong kind")
	}
	st.BaselineTree = tree.Str

	rec, err := stateField(root, "recoveryRequired")
	if err != nil {
		return state{}, err
	}
	if rec.Kind != strictjson.Bool {
		return state{}, errors.New("workspace: decode state: field \"recoveryRequired\" has the wrong kind")
	}
	st.RecoveryRequired = rec.B

	if err := validateState(st); err != nil {
		return state{}, err
	}
	return st, nil
}

// stateField returns the named field of the record or a missing-field
// error.
func stateField(root strictjson.Value, name string) (strictjson.Value, error) {
	f, ok := root.Field(name)
	if !ok {
		return strictjson.Value{}, fmt.Errorf("workspace: decode state: missing field %q", name)
	}
	return f, nil
}

// validateState applies the cross-field rules of architecture section 7.2.
func validateState(st state) error {
	if st.Version != 1 {
		return fmt.Errorf("workspace: state: unsupported version %d", st.Version)
	}
	if !validHex64(st.Identity) {
		return fmt.Errorf("workspace: state: identity %q is not a canonical SHA-256 digest", st.Identity)
	}
	if _, err := git.ParseOID(st.BaselineTree); err != nil {
		return fmt.Errorf("workspace: state: baseline tree: %w", err)
	}
	switch {
	case st.RemoteGeneration == 0 && st.BaselineHead != "":
		return errors.New("workspace: state: baseline head must be empty at remote generation 0")
	case st.RemoteGeneration > 0 && st.BaselineHead == "":
		return errors.New("workspace: state: baseline head must not be empty past remote generation 0")
	}
	if st.BaselineHead != "" {
		if _, err := git.ParseOID(st.BaselineHead); err != nil {
			return fmt.Errorf("workspace: state: baseline head: %w", err)
		}
	}
	return nil
}

// validHex64 reports whether s is exactly 64 lowercase hexadecimal
// characters, the canonical form of the derived-key digest.
func validHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case '0' <= c && c <= '9', 'a' <= c && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// persistState durably writes the record: temporary file, file sync, and
// atomic rename (architecture section 7.2). It always stamps the record
// with the workspace derived key, so a recovery write repairs a corrupt or
// mismatched identity. A failed rename leaves the previous record durable
// (rename is atomic), so the caller aborts the operation on error and the
// workspace keeps the state it already holds.
func persistState(privDir, derivedKey string, st state) (state, error) {
	st.Identity = derivedKey
	data, err := encodeState(st)
	if err != nil {
		return st, fmt.Errorf("workspace: persist state: %w", err)
	}
	statePath := filepath.Join(privDir, stateFileName)
	tmpPath := filepath.Join(privDir, stateTmpName)
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return st, fmt.Errorf("workspace: persist state: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return st, fmt.Errorf("workspace: persist state: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return st, fmt.Errorf("workspace: persist state: %w", err)
	}
	if err := f.Close(); err != nil {
		return st, fmt.Errorf("workspace: persist state: %w", err)
	}
	if err := os.Rename(tmpPath, statePath); err != nil {
		return st, fmt.Errorf("workspace: persist state: %w", err)
	}
	return st, nil
}

// readStateFile reads and strictly decodes the durable record. Any failure
// is reported; the caller decides whether the workspace must recover.
func readStateFile(privDir string) (state, error) {
	data, err := os.ReadFile(filepath.Join(privDir, stateFileName))
	if err != nil {
		return state{}, err
	}
	return decodeState(data)
}
