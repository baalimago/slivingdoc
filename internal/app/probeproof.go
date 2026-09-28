package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// The store compatibility proof (architecture/storage.md): one successful
// probe of a store identity is recorded below that identity's shared
// pack-cache directory, and a later process of the same slivingdoc version
// reuses the record instead of probing again until it expires. The record
// proves a property of the endpoint, so it is keyed by that endpoint's
// identity and repeats its configuration, never by a workspace.
const (
	probeProofFile    = "probe-ok.json"
	probeProofVersion = 1
	probeProofTTL     = 24 * time.Hour
)

// probeProof is the durable record. Field order is the normative JSON
// shape; probedAt is RFC 3339 in UTC. The store fields repeat what the
// directory name only digests, so a record is never reused for a store it
// did not prove, and pathStyle covers the addressing mode the identity
// omits.
type probeProof struct {
	Version    int    `json:"version"`
	Slivingdoc string `json:"slivingdoc"`
	ProbedAt   string `json:"probedAt"`
	Endpoint   string `json:"endpoint"`
	Region     string `json:"region"`
	Bucket     string `json:"bucket"`
	Prefix     string `json:"prefix"`
	PathStyle  bool   `json:"pathStyle"`
}

// probedStore is the configuration a proof binds to.
type probedStore struct {
	endpoint, region, bucket, prefix string
	pathStyle                        bool
}

// probeProofStore locates, validates, and writes the proof of one store
// identity. The zero value (no directory) disables proofs, which is a
// process without a shared pack cache.
type probeProofStore struct {
	dir     string
	version string
	store   probedStore
	now     func() time.Time
}

func (s probeProofStore) enabled() bool { return s.dir != "" }

func (s probeProofStore) path() string { return filepath.Join(s.dir, probeProofFile) }

// check returns the time of the recorded probe when a fresh record of this
// slivingdoc version exists. Every other state is an error naming why the
// probe must run; none of them carries the record's path.
func (s probeProofStore) check() (time.Time, error) {
	if !s.enabled() {
		return time.Time{}, errors.New("proof disabled")
	}
	data, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return time.Time{}, errors.New("no recorded proof")
		}
		return time.Time{}, fmt.Errorf("read recorded proof: %w", pathless(err))
	}
	p, err := decodeProbeProof(data)
	if err != nil {
		return time.Time{}, fmt.Errorf("recorded proof is corrupt: %w", err)
	}
	if p.Slivingdoc != s.version {
		return time.Time{}, fmt.Errorf("recorded proof is from slivingdoc %s", p.Slivingdoc)
	}
	recorded := probedStore{
		endpoint: p.Endpoint, region: p.Region, bucket: p.Bucket,
		prefix: p.Prefix, pathStyle: p.PathStyle,
	}
	if recorded != s.store {
		return time.Time{}, errors.New("recorded proof is for another store configuration")
	}
	probedAt, err := time.Parse(time.RFC3339, p.ProbedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("recorded proof is corrupt: probedAt: %w", err)
	}
	now := s.now()
	if probedAt.After(now) {
		return time.Time{}, errors.New("recorded proof is dated in the future")
	}
	if now.Sub(probedAt) > probeProofTTL {
		return time.Time{}, errors.New("recorded proof expired")
	}
	return probedAt, nil
}

// record writes a fresh proof through a temporary file and an atomic
// rename, creating the identity directory on demand.
func (s probeProofStore) record() error {
	if !s.enabled() {
		return errors.New("proof disabled")
	}
	data, err := encodeProbeProof(probeProof{
		Version:    probeProofVersion,
		Slivingdoc: s.version,
		ProbedAt:   s.now().UTC().Format(time.RFC3339),
		Endpoint:   s.store.endpoint,
		Region:     s.store.region,
		Bucket:     s.store.bucket,
		Prefix:     s.store.prefix,
		PathStyle:  s.store.pathStyle,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create proof directory: %w", pathless(err))
	}
	tmp, err := os.CreateTemp(s.dir, ".probe-*")
	if err != nil {
		return fmt.Errorf("create proof temporary: %w", pathless(err))
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write proof temporary: %w", pathless(err))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close proof temporary: %w", pathless(err))
	}
	if err := os.Rename(tmpName, s.path()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("place proof record: %w", pathless(err))
	}
	return nil
}

// pathless strips the file name from an os error so a diagnostic or log
// record never carries a private path; the underlying errno stays.
func pathless(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}

func encodeProbeProof(p probeProof) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, fmt.Errorf("encode proof: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// decodeProbeProof strictly decodes a record: unknown, duplicate, missing,
// and null fields, a wrong field kind, and a version other than 1 are all
// corruption.
func decodeProbeProof(data []byte) (probeProof, error) {
	root, err := strictjson.Parse(data)
	if err != nil {
		return probeProof{}, err
	}
	if root.Kind != strictjson.Object {
		return probeProof{}, errors.New("root is not an object")
	}
	if err := root.RejectUnknown("version", "slivingdoc", "probedAt", "endpoint", "region", "bucket", "prefix", "pathStyle"); err != nil {
		return probeProof{}, err
	}
	ver, ok := root.Field("version")
	if !ok || ver.Kind != strictjson.Number {
		return probeProof{}, errors.New("field \"version\" is missing or has the wrong kind")
	}
	if ver.Num != probeProofVersion {
		return probeProof{}, fmt.Errorf("unsupported version %d", ver.Num)
	}
	p := probeProof{Version: probeProofVersion}
	// Field order is fixed so two missing fields always name the same one.
	for _, f := range []struct {
		name string
		dst  *string
	}{
		{"slivingdoc", &p.Slivingdoc},
		{"probedAt", &p.ProbedAt},
		{"endpoint", &p.Endpoint},
		{"region", &p.Region},
		{"bucket", &p.Bucket},
		{"prefix", &p.Prefix},
	} {
		name, dst := f.name, f.dst
		v, ok := root.Field(name)
		if !ok || v.Kind != strictjson.String {
			return probeProof{}, fmt.Errorf("field %q is missing or has the wrong kind", name)
		}
		*dst = v.Str
	}
	ps, ok := root.Field("pathStyle")
	if !ok || ps.Kind != strictjson.Bool {
		return probeProof{}, errors.New("field \"pathStyle\" is missing or has the wrong kind")
	}
	p.PathStyle = ps.B
	return p, nil
}

// storeCheckOutcome classifies how startup satisfied itself about the store.
type storeCheckOutcome uint8

const (
	// storeProbed ran the S3 compatibility probe against the live store.
	storeProbed storeCheckOutcome = iota
	// storeProofReused accepted a fresh recorded proof instead of probing.
	storeProofReused
	// storeAccessChecked ran a hosted store's access check (no probe).
	storeAccessChecked
)

// storeCheckReport is the outcome of one startup store check and, for a
// probe, why no proof stood in and whether recording a fresh one failed.
type storeCheckReport struct {
	outcome   storeCheckOutcome
	probedAt  time.Time
	proofErr  error
	recordErr error
}

// checkStoreWithProof is checkStore with the recorded proof in front of the
// probe: a hosted store still runs its access check, a fresh proof of this
// store identity skips the probe, and a successful probe records a new proof
// when proofs are enabled. A proof that cannot be written never fails
// startup; the report carries the cause.
func checkStoreWithProof(ctx context.Context, store storage.ObjectStore, proofs probeProofStore) (storeCheckReport, error) {
	if _, hosted := store.(accessChecker); hosted {
		return storeCheckReport{outcome: storeAccessChecked}, checkStore(ctx, store)
	}
	report := storeCheckReport{outcome: storeProbed}
	if proofs.enabled() {
		probedAt, err := proofs.check()
		if err == nil {
			return storeCheckReport{outcome: storeProofReused, probedAt: probedAt}, nil
		}
		report.proofErr = err
	}
	if err := checkStore(ctx, store); err != nil {
		return report, err
	}
	if proofs.enabled() {
		report.recordErr = proofs.record()
	}
	return report, nil
}
