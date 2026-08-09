package workspace

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCanonicalPathAccepts(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "workspace")
	cases := map[string]string{
		"below root":    root + string(filepath.Separator) + "notes",
		"nested":        root + string(filepath.Separator) + "notes" + string(filepath.Separator) + "agents",
		"root itself":   root,
		"dotted inside": root + string(filepath.Separator) + "a" + string(filepath.Separator) + ".." + string(filepath.Separator) + "notes",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := CanonicalPath(root, path)
			if err != nil {
				t.Fatalf("CanonicalPath() = %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("CanonicalPath() = %q, want absolute", got)
			}
			rel, err := filepath.Rel(root, got)
			if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
				t.Fatalf("CanonicalPath() = %q escapes %q", got, root)
			}
		})
	}
}

func TestCanonicalPathRejects(t *testing.T) {
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
			if _, err := CanonicalPath(root, path); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("CanonicalPath(%q) error = %v, want ErrInvalidPath", path, err)
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
