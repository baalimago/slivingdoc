package app

import (
	"reflect"
	"testing"

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
