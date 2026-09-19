package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// TestRuntimeReadOnlyPaths checks the flag reaches Runtime.ReadOnlyPaths
// normalized.
func TestRuntimeReadOnlyPaths(t *testing.T) {
	p := testProcess([]string{"SLIVINGDOC_BUCKET=bucket"}, "--read-only-paths=notes,docs/,docs/faq.md")
	p.engine = &fakeEngine{}
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	defer rt.Close()
	if got, want := rt.ReadOnlyPaths(), []string{"docs", "notes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadOnlyPaths() = %v, want %v", got, want)
	}
}

// TestNewServiceRejectsInvalidReadOnlyPaths checks an invalid entry refuses
// construction.
func TestNewServiceRejectsInvalidReadOnlyPaths(t *testing.T) {
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	cfg := testServiceConfig(t).serviceConfig()
	cfg.ReadOnlyPaths = []string{"../escape"}
	if _, err := NewService(eng, fake.New(cfg.Prefix), cfg, nil); err == nil {
		t.Fatal("NewService(invalid read-only path) = nil, want error")
	}
}

// TestNewServiceNormalizesReadOnlyPaths checks normalization and the empty
// non-nil default.
func TestNewServiceNormalizesReadOnlyPaths(t *testing.T) {
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	cfg := testServiceConfig(t).serviceConfig()
	cfg.ReadOnlyPaths = []string{"docs/", "docs/sub", "faq.md"}
	svc, err := NewService(eng, fake.New(cfg.Prefix), cfg, nil)
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if got, want := svc.ReadOnlyPaths(), []string{"docs", "faq.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadOnlyPaths() = %v, want %v", got, want)
	}

	emptyCfg := testServiceConfig(t).serviceConfig()
	empty, err := NewService(eng, fake.New(emptyCfg.Prefix), emptyCfg, nil)
	if err != nil {
		t.Fatalf("NewService(no read-only paths) = %v", err)
	}
	t.Cleanup(func() { _ = empty.Close() })
	if got := empty.ReadOnlyPaths(); got == nil || len(got) != 0 {
		t.Fatalf("ReadOnlyPaths() = %v, want an empty non-nil slice", got)
	}
}

// TestRuntimeWritablePaths checks the writable accessor sits beside the
// read-only one on Runtime and is an empty non-nil slice until a flag
// resolves into it.
func TestRuntimeWritablePaths(t *testing.T) {
	p := testProcess([]string{"SLIVINGDOC_BUCKET=bucket"}, "--read-only-paths=notes")
	p.engine = &fakeEngine{}
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	defer rt.Close()
	if got := rt.WritablePaths(); got == nil || len(got) != 0 {
		t.Fatalf("WritablePaths() = %v, want an empty non-nil slice", got)
	}
}

// TestNewServiceNormalizesWritablePaths checks the writable set reaches
// Service.WritablePaths normalized, and is empty and non-nil when unset.
func TestNewServiceNormalizesWritablePaths(t *testing.T) {
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	cfg := testServiceConfig(t).serviceConfig()
	cfg.ReadOnlyPaths = []string{"docs"}
	cfg.WritablePaths = []string{"notes/", "notes/sub", "team.md"}
	svc, err := NewService(eng, fake.New(cfg.Prefix), cfg, nil)
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if got, want := svc.WritablePaths(), []string{"notes", "team.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("WritablePaths() = %v, want %v", got, want)
	}
	if got, want := svc.ReadOnlyPaths(), []string{"docs"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadOnlyPaths() = %v, want %v", got, want)
	}

	emptyCfg := testServiceConfig(t).serviceConfig()
	empty, err := NewService(eng, fake.New(emptyCfg.Prefix), emptyCfg, nil)
	if err != nil {
		t.Fatalf("NewService(no writable paths) = %v", err)
	}
	t.Cleanup(func() { _ = empty.Close() })
	if got := empty.WritablePaths(); got == nil || len(got) != 0 {
		t.Fatalf("WritablePaths() = %v, want an empty non-nil slice", got)
	}
}

// TestServiceNestedEntriesSurviveRebuild checks the entries a nested
// configuration needs survive the service's own construction: a process
// carries its two sets as entries and rebuilds the policy from them, so an
// entry the other set splits from its own-set ancestor must still be
// advertised and still be enforceable after the rebuild.
func TestServiceNestedEntriesSurviveRebuild(t *testing.T) {
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	cfg := testServiceConfig(t).serviceConfig()
	cfg.ReadOnlyPaths = []string{"notes", "notes/agent-a/locked"}
	cfg.WritablePaths = []string{"notes/agent-a"}
	svc, err := NewService(eng, fake.New(cfg.Prefix), cfg, nil)
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	readOnly, writable := svc.ReadOnlyPaths(), svc.WritablePaths()
	if want := []string{"notes", "notes/agent-a/locked"}; !reflect.DeepEqual(readOnly, want) {
		t.Fatalf("ReadOnlyPaths() = %v, want %v", readOnly, want)
	}
	if want := []string{"notes/agent-a"}; !reflect.DeepEqual(writable, want) {
		t.Fatalf("WritablePaths() = %v, want %v", writable, want)
	}
	// The accessors are what the notebook is configured with, so the policy
	// built from them must protect what the written entries protect.
	rebuilt, err := git.NewPolicy(readOnly, writable)
	if err != nil {
		t.Fatalf("NewPolicy(accessors) = %v", err)
	}
	for path, want := range map[string]bool{
		"notes/agent-a/locked/secret.md": true,
		"notes/agent-a/free.md":          false,
		"notes/a.md":                     true,
	} {
		if got := rebuilt.Protects(path); got != want {
			t.Fatalf("rebuilt policy Protects(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestServicePolicyConstructionFailsSetup checks a policy the two sets
// cannot express refuses Service construction — the last step of startup,
// so the refusal is a startup refusal — and that the diagnostic names the
// offending entry.
func TestServicePolicyConstructionFailsSetup(t *testing.T) {
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	for _, tt := range []struct {
		name     string
		readOnly []string
		writable []string
		names    string
	}{
		{name: "exact overlap", readOnly: []string{"docs"}, writable: []string{"docs"}, names: "docs"},
		{name: "invalid writable entry", writable: []string{"../escape"}, names: "../escape"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testServiceConfig(t).serviceConfig()
			cfg.ReadOnlyPaths = tt.readOnly
			cfg.WritablePaths = tt.writable
			svc, err := NewService(eng, fake.New(cfg.Prefix), cfg, nil)
			if err == nil {
				_ = svc.Close()
				t.Fatal("NewService(unbuildable policy) = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.names) {
				t.Fatalf("NewService() error = %v, want it to name %q", err, tt.names)
			}
		})
	}
}
