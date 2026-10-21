package itest

import (
	"os"
	"path/filepath"
	"testing"
)

// packFiles lists the pack and index files of a private repository.
func PackFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "pack-*"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func CountPacks(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	for _, f := range PackFiles(t, dir) {
		if filepath.Ext(f) == ".pack" {
			n++
		}
	}
	return n
}

// removePackOfSize deletes the one imported pack whose bytes are the
// descriptor's (libgit2 stores an imported pack verbatim) and its index.
func RemovePackOfSize(t *testing.T, dir string, size uint64) {
	t.Helper()
	var victims []string
	for _, f := range PackFiles(t, dir) {
		if filepath.Ext(f) != ".pack" {
			continue
		}
		info, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if uint64(info.Size()) == size {
			victims = append(victims, f)
		}
	}
	if len(victims) != 1 {
		t.Fatalf("packs of %d bytes = %v, want exactly one", size, victims)
	}
	for _, f := range []string{victims[0], victims[0][:len(victims[0])-len(".pack")] + ".idx"} {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
}
