package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// testStore is the store configuration the proof tests bind records to.
var testStore = probedStore{endpoint: "https://s3.example", region: "auto", bucket: "b", prefix: "p", pathStyle: true}

// TestProbeProofRoundTrip checks a recorded proof is accepted by the same
// version within its lifetime and reports the recorded time.
func TestProbeProofRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	s := probeProofStore{dir: filepath.Join(t.TempDir(), "identity"), version: "1.2.3", store: testStore, now: func() time.Time { return at }}
	if err := s.record(); err != nil {
		t.Fatalf("record() = %v", err)
	}
	s.now = func() time.Time { return at.Add(probeProofTTL) }
	got, err := s.check()
	if err != nil {
		t.Fatalf("check() = %v, want the recorded proof", err)
	}
	if !got.Equal(at) {
		t.Fatalf("check() = %v, want %v", got, at)
	}
}

// TestProbeProofRejects checks every state that must send startup back to
// the live probe, and that no refusal names the record's path.
func TestProbeProofRejects(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	record := func(fields string) string {
		return `{"version":1,"slivingdoc":"1.2.3","probedAt":"2026-09-28T10:00:00Z",` + fields + `}`
	}
	store := `"endpoint":"https://s3.example","region":"auto","bucket":"b","prefix":"p","pathStyle":true`
	valid := record(store)
	for _, tt := range []struct {
		name string
		data string // empty: no record
		now  time.Time
		want string
	}{
		{name: "absent", now: at, want: "no recorded proof"},
		{name: "unreadable", data: "dir", now: at, want: "read recorded proof"},
		{name: "corrupt json", data: "{", now: at, want: "corrupt"},
		{name: "not an object", data: `[]`, now: at, want: "corrupt"},
		{name: "unknown field", data: record(store + `,"x":1`), now: at, want: "corrupt"},
		{name: "wrong version", data: `{"version":2,"slivingdoc":"1.2.3","probedAt":"2026-09-28T10:00:00Z",` + store + `}`, now: at, want: "corrupt"},
		{name: "wrong kind", data: `{"version":1,"slivingdoc":1,"probedAt":"2026-09-28T10:00:00Z",` + store + `}`, now: at, want: "corrupt"},
		{name: "missing field", data: `{"version":1,"slivingdoc":"1.2.3",` + store + `}`, now: at, want: "corrupt"},
		{name: "missing store field", data: `{"version":1,"slivingdoc":"1.2.3","probedAt":"2026-09-28T10:00:00Z"}`, now: at, want: "corrupt"},
		{name: "bad time", data: `{"version":1,"slivingdoc":"1.2.3","probedAt":"yesterday",` + store + `}`, now: at, want: "corrupt"},
		{name: "other version", data: `{"version":1,"slivingdoc":"9.9.9","probedAt":"2026-09-28T10:00:00Z",` + store + `}`, now: at, want: "from slivingdoc 9.9.9"},
		{name: "other endpoint", data: record(`"endpoint":"https://other.example","region":"auto","bucket":"b","prefix":"p","pathStyle":true`), now: at, want: "another store"},
		{name: "other region", data: record(`"endpoint":"https://s3.example","region":"eu-north-1","bucket":"b","prefix":"p","pathStyle":true`), now: at, want: "another store"},
		{name: "other bucket", data: record(`"endpoint":"https://s3.example","region":"auto","bucket":"other","prefix":"p","pathStyle":true`), now: at, want: "another store"},
		{name: "other prefix", data: record(`"endpoint":"https://s3.example","region":"auto","bucket":"b","prefix":"other","pathStyle":true`), now: at, want: "another store"},
		{name: "other addressing", data: record(`"endpoint":"https://s3.example","region":"auto","bucket":"b","prefix":"p","pathStyle":false`), now: at, want: "another store"},
		{name: "expired", data: valid, now: at.Add(probeProofTTL + time.Second), want: "expired"},
		{name: "future", data: valid, now: at.Add(-time.Minute), want: "future"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			switch tt.data {
			case "":
			case "dir": // a directory in the record's place is a read error that is not "absent"
				if err := os.Mkdir(filepath.Join(dir, probeProofFile), 0o700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(filepath.Join(dir, probeProofFile), []byte(tt.data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s := probeProofStore{dir: dir, version: "1.2.3", store: testStore, now: func() time.Time { return tt.now }}
			_, err := s.check()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("check() = %v, want an error containing %q", err, tt.want)
			}
			if strings.Contains(err.Error(), dir) {
				t.Fatalf("check() = %v names the record's path", err)
			}
		})
	}
}

// TestProbeProofDisabled checks the zero store neither accepts nor writes.
func TestProbeProofDisabled(t *testing.T) {
	var s probeProofStore
	if s.enabled() {
		t.Fatal("zero probeProofStore is enabled")
	}
	if _, err := s.check(); err == nil {
		t.Fatal("check() on a disabled store = nil, want an error")
	}
	if err := s.record(); err == nil {
		t.Fatal("record() on a disabled store = nil, want an error")
	}
}

// TestProbeProofRecordUnwritable checks a directory that cannot be created
// is an error the caller reports, never a panic or a partial file.
func TestProbeProofRecordUnwritable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := probeProofStore{dir: filepath.Join(blocker, "identity"), version: "1.2.3", store: testStore, now: time.Now}
	err := s.record()
	if err == nil {
		t.Fatal("record() below a regular file = nil, want an error")
	}
	if strings.Contains(err.Error(), blocker) {
		t.Fatalf("record() = %v names the private path", err)
	}
}

// TestCheckStoreWithProof checks the three outcomes at the unit level: a
// plain store probes and records, a fresh proof skips the probe, and a
// probe failure records nothing.
func TestCheckStoreWithProof(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	proofs := probeProofStore{dir: filepath.Join(t.TempDir(), "identity"), version: Version, store: testStore, now: func() time.Time { return at }}

	store := fake.New("")
	report, err := checkStoreWithProof(context.Background(), store, config{}, proofs)
	if err != nil || report.outcome != storeProbed || report.recordErr != nil {
		t.Fatalf("first check = %+v, %v; want a recorded probe", report, err)
	}
	if got := store.Calls(fake.OpCreate); got != 2 {
		t.Fatalf("probe creates = %d, want 2", got)
	}

	store = fake.New("")
	report, err = checkStoreWithProof(context.Background(), store, config{}, proofs)
	if err != nil || report.outcome != storeProofReused || !report.probedAt.Equal(at) {
		t.Fatalf("second check = %+v, %v; want the reused proof", report, err)
	}
	if got := store.Calls(fake.OpCreate) + store.Calls(fake.OpReplace) + store.Calls(fake.OpGet); got != 0 {
		t.Fatalf("store calls with a reused proof = %d, want 0", got)
	}

	failing := probeProofStore{dir: filepath.Join(t.TempDir(), "other"), version: Version, store: testStore, now: func() time.Time { return at }}
	report, err = checkStoreWithProof(context.Background(), &refusingStore{err: storage.ErrTransport}, config{}, failing)
	if err == nil || report.outcome != storeProbed {
		t.Fatalf("failing probe = %+v, %v; want the probe refusal", report, err)
	}
	if _, statErr := os.Stat(failing.path()); statErr == nil {
		t.Fatal("a failed probe recorded a proof")
	}
}

// TestSetupReusesStoreProof drives the process body: the second setup of
// one store identity makes no store request, an expired record sends it
// back to the probe, and a host without a user cache directory probes every
// time.
func TestSetupReusesStoreProof(t *testing.T) {
	cacheDir := t.TempDir()
	var stores []*fake.Store
	start := func(t *testing.T, args ...string) *fake.Store {
		t.Helper()
		p := testProcess([]string{"SLIVINGDOC_BUCKET=bucket"}, args...)
		p.cacheDir = cacheDir
		p.storeFactory = func(context.Context, config) (storage.ObjectStore, error) {
			s := fake.New("")
			stores = append(stores, s)
			return s, nil
		}
		rt, err := setup(p)
		if err != nil {
			t.Fatalf("setup(%v) = %v", args, err)
		}
		rt.Close()
		return stores[len(stores)-1]
	}
	requests := func(s *fake.Store) int {
		return s.Calls(fake.OpCreate) + s.Calls(fake.OpReplace) + s.Calls(fake.OpGet) + s.Calls(fake.OpDelete)
	}

	if got := start(t); got.Calls(fake.OpCreate) != 2 {
		t.Fatalf("first setup probe creates = %d, want 2", got.Calls(fake.OpCreate))
	}
	proofs, err := filepath.Glob(filepath.Join(cacheDir, "slivingdoc", "pack-cache", "*", probeProofFile))
	if err != nil || len(proofs) != 1 {
		t.Fatalf("proof records = %v, %v; want exactly one", proofs, err)
	}
	if got := start(t); requests(got) != 0 {
		t.Fatalf("second setup store requests = %d, want 0", requests(got))
	}
	noCacheDir := testProcess([]string{"SLIVINGDOC_BUCKET=bucket"}, "--private-root", t.TempDir())
	noCacheDir.cacheDir = ""
	noCacheDir.storeFactory = func(context.Context, config) (storage.ObjectStore, error) {
		s := fake.New("")
		stores = append(stores, s)
		return s, nil
	}
	if rt, err := setup(noCacheDir); err != nil {
		t.Fatalf("setup(no cache dir) = %v", err)
	} else {
		rt.Close()
	}
	if got := stores[len(stores)-1]; got.Calls(fake.OpCreate) != 2 {
		t.Fatalf("setup without a user cache directory probe creates = %d, want 2", got.Calls(fake.OpCreate))
	}

	stale := `{"version":1,"slivingdoc":"` + Version + `","probedAt":"2000-01-01T00:00:00Z","endpoint":"","region":"us-east-1","bucket":"bucket","prefix":"slivingdoc","pathStyle":false}`
	if err := os.WriteFile(proofs[0], []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := start(t); got.Calls(fake.OpCreate) != 2 {
		t.Fatalf("setup with an expired proof probe creates = %d, want 2", got.Calls(fake.OpCreate))
	}
	data, err := os.ReadFile(proofs[0])
	if err != nil || strings.Contains(string(data), "2000-01-01") {
		t.Fatalf("proof after the re-probe = %q, %v; want a fresh record", data, err)
	}
}
