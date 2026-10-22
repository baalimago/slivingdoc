package keyrecord

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestKeyRecordPrivateFile proves a write happens only on an explicit request
// and through the shared private-file contract: 0600 in a 0700 directory, an
// atomic replacement, and a failed save that keeps the prior record.
func TestKeyRecordPrivateFile(t *testing.T) {
	f := testFile(t)
	set, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(set.Entries()) != 0 {
		t.Fatalf("Entries() = %d, want none", len(set.Entries()))
	}
	if _, err := os.Stat(f.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load() of a missing file touched %s: %v", f.Path(), err)
	}
	if err := f.CheckDir(); err != nil {
		t.Fatalf("CheckDir() without a directory = %v", err)
	}
	digest := workspace.IdentityDigest(testIdentity("private"))
	entry := mustPassphrase(t, digest, "s3cret")
	if err := f.Update(context.Background(), entry); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(f.Path()); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("file mode = %v, %v; want 0600", info, err)
		}
		if info, err := os.Stat(filepath.Dir(f.Path())); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("directory mode = %v, %v; want 0700", info, err)
		}
	}
	if err := f.Update(context.Background(), mustPassphrase(t, digest, "newer")); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	set, err = f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if stored, err := set.Lookup(digest); err != nil || stored.Value() != "newer" || len(set.Entries()) != 1 {
		t.Fatalf("Lookup() = %q with %d entries, %v; want the single replaced record", stored.Value(), len(set.Entries()), err)
	}
	if err := f.Forget(context.Background(), digest); err != nil {
		t.Fatalf("Forget() = %v", err)
	}
	set, err = f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if _, err := set.Lookup(digest); !errors.Is(err, ErrNoMaterial) {
		t.Fatalf("Lookup() after Forget() = %v, want ErrNoMaterial", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := f.Update(context.Background(), entry); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	dir := filepath.Dir(f.Path())
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(Set{}); err == nil {
		t.Fatal("Save() into a read-only directory = nil, want an error")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if stored, err := got.Lookup(digest); err != nil || stored.Value() != "s3cret" {
		t.Fatalf("the failed save changed the record to %q, %v", stored.Value(), err)
	}
}

// TestKeyRecordRejectsUnsafeFile proves an unsafe key record yields a typed
// refusal and never supplies a candidate.
func TestKeyRecordRejectsUnsafeFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX owners, and symbolic links need a privilege")
	}
	digest := workspace.IdentityDigest(testIdentity("unsafe"))
	entry := mustPassphrase(t, digest, "s3cret")
	for _, tt := range []struct {
		name, want string
		plant      func(t *testing.T, f File)
	}{
		{"a symbolic link", "symbolic link", func(t *testing.T, f File) {
			if err := f.Save(Set{}.Put(entry)); err != nil {
				t.Fatal(err)
			}
			real := filepath.Join(filepath.Dir(f.Path()), "real.json")
			if err := os.Rename(f.Path(), real); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, f.Path()); err != nil {
				t.Fatal(err)
			}
		}},
		{"a dangling symbolic link", "symbolic link", func(t *testing.T, f File) {
			if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(t.TempDir(), "planted.json"), f.Path()); err != nil {
				t.Fatal(err)
			}
		}},
		{"a directory", "not a regular file", func(t *testing.T, f File) {
			if err := os.MkdirAll(f.Path(), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"an exposed file", "chmod 600", func(t *testing.T, f File) {
			if err := f.Save(Set{}.Put(entry)); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(f.Path(), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"an exposed directory", "chmod go-w", func(t *testing.T, f File) {
			if err := f.Save(Set{}.Put(entry)); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Dir(f.Path()), 0o777); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := testFile(t)
			tt.plant(t, f)
			if _, err := f.Load(); !errors.Is(err, ErrExposed) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() = %v, want ErrExposed saying %q", err, tt.want)
			}
			env := lookup(map[string]string{credentials.DirEnv: filepath.Dir(f.Path())})
			if _, err := Resolve(testIdentity("unsafe"), env); !errors.Is(err, ErrExposed) {
				t.Fatalf("Resolve() = %v, want the record's refusal rather than no candidate", err)
			}
		})
	}
}
