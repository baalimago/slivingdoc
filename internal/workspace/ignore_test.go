package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIgnoreMatching(t *testing.T) {
	ig, err := NewIgnore(append(append([]string(nil), DefaultIgnore...), "*.log", "private/scratch/", "docs/*.tmp"))
	if err != nil {
		t.Fatalf("NewIgnore() = %v", err)
	}
	tests := []struct {
		path string
		want bool
	}{
		{".DS_Store", true},
		{"a/b/.DS_Store", true},
		{"._notes.md", true},
		{"Thumbs.db", true},
		{"a/.git/config", true},
		{".git", true},
		{"x.swp", true},
		{".slivingdoc-tmp-0123abcd", true},
		{"a.md.slivingdoc-tmp-0123abcd", true},
		{"run.log", true},
		{"a/b/run.log", true},
		{"private/scratch", true},
		{"private/scratch/deep/file.md", true},
		{"other/private/scratch/file.md", false},
		{"private/kept.md", false},
		{"docs/x.tmp", true},
		{"docs/a/x.tmp", false},
		{"plan.md", false},
		{".DS_Stores.md", false},
		{"a._x", false},
	}
	for _, tt := range tests {
		if got := ig.Ignored(tt.path); got != tt.want {
			t.Errorf("Ignored(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
	if (Ignore{}).Ignored(".DS_Store") {
		t.Error("the zero Ignore ignores a path")
	}
}

func TestNewIgnoreRefusesBadPatterns(t *testing.T) {
	for _, pattern := range []string{"", "  ", "/", "[unclosed", "a/[b", "./private", "a/../b"} {
		if _, err := NewIgnore([]string{pattern}); !errors.Is(err, ErrInvalidIgnore) {
			t.Errorf("NewIgnore(%q) = %v, want ErrInvalidIgnore", pattern, err)
		}
	}
}

func ignoreWorkspace(t *testing.T) *Workspace {
	t.Helper()
	cfg := testConfig(t, newFakeEngine(), "notes")
	ig, err := NewIgnore(DefaultIgnore)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Ignore = ig
	return openWorkspace(t, cfg)
}

func TestSnapshotSkipsIgnoredEntriesWithoutValidatingThem(t *testing.T) {
	w := ignoreWorkspace(t)
	write := func(rel, data string) {
		t.Helper()
		p := filepath.Join(w.Path(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.md", "a")
	write(".DS_Store", "\x00\x01")
	write("sub/Thumbs.db", "\xff\xfe")
	if err := os.Symlink("/etc/passwd", filepath.Join(w.Path(), "._link")); err != nil {
		t.Fatal(err)
	}
	snap, err := w.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	if len(snap.Files) != 1 || snap.Files[0].Path != "a.md" {
		t.Fatalf("Snapshot() = %v, want only a.md", snap.Files)
	}
}

func TestSnapshotPinsIgnoredBaselineFiles(t *testing.T) {
	w := ignoreWorkspace(t)
	tree := buildTree(t, w, map[string]string{"a.md": "a", "dir/.DS_Store": "kept upstream"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(w.Path(), "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path(), "dir", ".DS_Store"), []byte("edited here"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := w.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	got := map[string]string{}
	for _, f := range snap.Files {
		got[f.Path] = string(f.Data)
	}
	want := map[string]string{"a.md": "a", "dir/.DS_Store": "kept upstream"}
	if len(got) != len(want) || got["a.md"] != "a" || got["dir/.DS_Store"] != "kept upstream" {
		t.Fatalf("Snapshot() = %v, want %v: an ignored file is the notebook's, not this machine's", got, want)
	}
}

func TestAcceptKeepsIgnoredEntries(t *testing.T) {
	w := ignoreWorkspace(t)
	first := buildTree(t, w, map[string]string{"gone/x.md": "x", "keep.md": "k"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: first}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path(), "gone", ".DS_Store"), []byte("finder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path(), ".DS_Store"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := buildTree(t, w, map[string]string{"keep.md": "k"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 2, Head: oidTest("d"), Tree: second}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"gone/.DS_Store", ".DS_Store", "keep.md"} {
		if _, err := os.Stat(filepath.Join(w.Path(), filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s: %v, want it kept", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(w.Path(), "gone", "x.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("gone/x.md still present: %v", err)
	}
}

func TestAcceptNeverWritesAnIgnoredTargetPath(t *testing.T) {
	w := ignoreWorkspace(t)
	if err := os.WriteFile(filepath.Join(w.Path(), ".DS_Store"), []byte("LOCAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree := buildTree(t, w, map[string]string{"a.md": "a", ".DS_Store": "remote", "d/x.swp": "remote swap"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), ".DS_Store")); string(got) != "LOCAL" {
		t.Fatalf(".DS_Store = %q, want the local file untouched", got)
	}
	if _, err := os.Stat(filepath.Join(w.Path(), "d")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("d exists (%v): an ignored path created a directory", err)
	}
}

func TestAcceptRefusesToReplaceADirectoryHoldingIgnoredFiles(t *testing.T) {
	w := ignoreWorkspace(t)
	first := buildTree(t, w, map[string]string{"d/x.md": "x"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: first}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path(), "d", ".DS_Store"), []byte("finder"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := buildTree(t, w, map[string]string{"d": "now a file"})
	err := w.Accept(context.Background(), Baseline{RemoteGeneration: 2, Head: oidTest("d"), Tree: second})
	if !errors.Is(err, ErrIgnoredConflict) {
		t.Fatalf("Accept() = %v, want ErrIgnoredConflict", err)
	}
	if w.RecoveryRequired() {
		t.Fatal("a refusal before any change left the workspace needing recovery")
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), "d", ".DS_Store")); string(got) != "finder" {
		t.Fatalf("ignored file = %q, want it kept", got)
	}
}

func TestIgnoreMatchesDecomposedNames(t *testing.T) {
	ig, err := NewIgnore([]string{"café.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !ig.Ignored("café.txt") {
		t.Fatal("an NFD pattern must match the NFC path")
	}
	w := openWorkspace(t, func() Config {
		c := testConfig(t, newFakeEngine(), "notes")
		c.Ignore = ig
		return c
	}())
	if err := os.WriteFile(filepath.Join(w.Path(), "café.txt"), []byte("nfd"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree := buildTree(t, w, map[string]string{"a.md": "a"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.Path(), "café.txt")); err != nil {
		t.Fatalf("ignored decomposed-name file was removed: %v", err)
	}
}
