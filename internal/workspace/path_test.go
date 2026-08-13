package workspace

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCanonicalizeAccepts(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "workspace")
	cases := map[string]string{
		"below root":    root + string(filepath.Separator) + "notes",
		"nested":        root + string(filepath.Separator) + "notes" + string(filepath.Separator) + "agents",
		"root itself":   root,
		"dotted inside": root + string(filepath.Separator) + "a" + string(filepath.Separator) + ".." + string(filepath.Separator) + "notes",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			got, _, err := canonicalize(root, path)
			if err != nil {
				t.Fatalf("canonicalize() = %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("canonicalize() = %q, want absolute", got)
			}
			rel, err := filepath.Rel(root, got)
			if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
				t.Fatalf("canonicalize() = %q escapes %q", got, root)
			}
		})
	}
}

func TestCanonicalizeRejects(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "workspace")
	cases := map[string]string{
		"relative path":  "notes",
		"relative root":  filepath.Join("..", "workspace"),
		"parent escape":  filepath.Join(root, "..", "etc"),
		"deep escape":    filepath.Join(root, "notes", "..", "..", "tmp"),
		"absolute other": filepath.Join(string(filepath.Separator), "etc", "passwd"),
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := canonicalize(root, path); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("canonicalize(%q) error = %v, want ErrInvalidPath", path, err)
			}
		})
	}
}

func TestPrivateRootOverlap(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "workspace")
	cases := map[string]bool{
		root: true,
		root + string(filepath.Separator) + "private":             true,
		filepath.Join(string(filepath.Separator), "var", "cache"): false,
	}
	for priv, want := range cases {
		if got := RootsOverlap(priv, root); got != want {
			t.Fatalf("RootsOverlap(%q) = %v, want %v", priv, got, want)
		}
	}
}
