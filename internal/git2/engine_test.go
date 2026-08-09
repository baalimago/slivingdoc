package git2

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
)

// TestBlobRoundTrip writes one blob through the pinned libgit2 and reads it
// back. The asserted OID is the Git blob hash of the payload, which is
// independent of any external Git installation.
func TestBlobRoundTrip(t *testing.T) {
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer e.Close()

	repo, err := e.CreateRepo(t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo() = %v", err)
	}

	payload := []byte("hello slivingdoc\n")
	id, err := repo.WriteBlob(payload)
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	want := blobID(t, payload)
	if id != want {
		t.Fatalf("WriteBlob() OID = %s, want %s", id, want)
	}

	got, err := repo.ReadBlob(id)
	if err != nil {
		t.Fatalf("ReadBlob() = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("ReadBlob() = %q, want %q", got, payload)
	}

	if err := repo.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}

// blobID computes the Git SHA-1 of a blob object from its content.
func blobID(t *testing.T, data []byte) git.OID {
	t.Helper()
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	var id git.OID
	copy(id[:], h.Sum(nil))
	return id
}

func TestEngineLifecycle(t *testing.T) {
	e := New()
	if _, err := e.Version(); err == nil {
		t.Fatal("Version() before Open() must fail")
	}
	if _, err := e.Features(); err == nil {
		t.Fatal("Features() before Open() must fail")
	}
	if _, err := e.CreateRepo(t.TempDir()); err == nil {
		t.Fatal("CreateRepo() before Open() must fail")
	}

	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if err := e.Open(); err == nil {
		t.Fatal("second Open() must fail")
	}

	version, err := e.Version()
	if err != nil {
		t.Fatalf("Version() = %v", err)
	}
	if version != PinnedVersion {
		t.Fatalf("Version() = %q, want pinned %q", version, PinnedVersion)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if err := e.Close(); err == nil {
		t.Fatal("second Close() must fail")
	}
	if _, err := e.Version(); err == nil {
		t.Fatal("Version() after Close() must fail")
	}

	// The engine must reopen after a full close cycle.
	if err := e.Open(); err != nil {
		t.Fatalf("reopen Open() = %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("reclose Close() = %v", err)
	}
}

func TestFeaturesReflectPinnedBuild(t *testing.T) {
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer e.Close()

	f, err := e.Features()
	if err != nil {
		t.Fatalf("Features() = %v", err)
	}
	// Exact lock of the pinned Linux configuration: threads, nsec,
	// http-parser, regex, compression, sha1; no transport feature.
	want := git.Features{
		Threads:     true,
		NSEC:        true,
		HTTPParser:  true,
		Regex:       true,
		Compression: true,
		SHA1:        true,
	}
	if f != want {
		t.Fatalf("Features() = %+v, want %+v", f, want)
	}
}

func TestRepeatedOpenCloseConcurrent(t *testing.T) {
	const workers = 16
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			e := New()
			if err := e.Open(); err != nil {
				t.Errorf("Open() = %v", err)
				return
			}
			repo, err := e.CreateRepo(t.TempDir())
			if err != nil {
				t.Errorf("CreateRepo() = %v", err)
				e.Close()
				return
			}
			id, err := repo.WriteBlob([]byte("concurrent payload"))
			if err != nil {
				t.Errorf("WriteBlob() = %v", err)
			} else if _, err := repo.ReadBlob(id); err != nil {
				t.Errorf("ReadBlob() = %v", err)
			}
			if err := repo.Close(); err != nil {
				t.Errorf("repo Close() = %v", err)
			}
			if err := e.Close(); err != nil {
				t.Errorf("engine Close() = %v", err)
			}
		})
	}
	wg.Wait()
}

func TestOpenReportsInitFailure(t *testing.T) {
	orig := initFn
	initFn = func() int32 { return -1 }
	t.Cleanup(func() { initFn = orig })

	e := New()
	err := e.Open()
	var ne *git.NativeError
	if !errors.As(err, &ne) {
		t.Fatalf("Open() error = %v, want *git.NativeError", err)
	}
	if ne.Op != "open libgit2" {
		t.Fatalf("NativeError.Op = %q, want %q", ne.Op, "open libgit2")
	}
	if err := e.Close(); err == nil {
		t.Fatal("Close() after failed Open() must fail")
	}
}

func TestOpenRefusesVersionMismatch(t *testing.T) {
	orig := versionFn
	versionFn = func() (int, int, int) { return 2, 0, 0 }
	t.Cleanup(func() { versionFn = orig })

	e := New()
	err := e.Open()
	var vme *git.VersionMismatchError
	if !errors.As(err, &vme) {
		t.Fatalf("Open() error = %v, want *git.VersionMismatchError", err)
	}
	if vme.Pinned != PinnedVersion || vme.Runtime != "2.0.0" {
		t.Fatalf("VersionMismatchError = %+v", vme)
	}

	// The failed open must leave the init reference count balanced: a fresh
	// engine must still be able to open and close cleanly.
	versionFn = orig
	fresh := New()
	if err := fresh.Open(); err != nil {
		t.Fatalf("fresh Open() after mismatch = %v", err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatalf("fresh Close() after mismatch = %v", err)
	}
}

func TestMissingObjectRead(t *testing.T) {
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer e.Close()

	repo, err := e.CreateRepo(t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo() = %v", err)
	}
	if _, err := repo.WriteBlob([]byte("present")); err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}

	var missing git.OID
	missing[0] = 0x01 // a valid-looking but absent object ID

	_, err = repo.ReadBlob(missing)
	var ne *git.NativeError
	if !errors.As(err, &ne) {
		t.Fatalf("ReadBlob() error = %v, want *git.NativeError", err)
	}
	if ne.Op != "read blob" {
		t.Fatalf("NativeError.Op = %q, want %q", ne.Op, "read blob")
	}
	if ne.Message == "" {
		t.Fatal("NativeError must carry the libgit2 error message")
	}

	if err := repo.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if ne.Message == "" {
		t.Fatal("error detail must survive native handle release")
	}
}

func TestInvalidRepositoryPath(t *testing.T) {
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer e.Close()

	// A path component that is a regular file must fail: libgit2 cannot
	// create the repository directory through it.
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	if _, err := e.CreateRepo(filepath.Join(file, "repo")); err == nil {
		t.Fatal("CreateRepo() through a regular file must fail")
	}

	// Opening a plain file or a directory without .git as a repository must
	// fail.
	if _, err := e.OpenRepo(file); err == nil {
		t.Fatal("OpenRepo() on a file must fail")
	}
	if _, err := e.OpenRepo(t.TempDir()); err == nil {
		t.Fatal("OpenRepo() on a directory without .git must fail")
	}
}

func TestInjectedAllocationFailuresDoNotDereference(t *testing.T) {
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer e.Close()

	origCreate := createRepoFn
	createRepoFn = func(string, bool) (*repoHandle, error) {
		return nil, &git.NativeError{Op: "create repository", Message: "out of memory"}
	}
	t.Cleanup(func() { createRepoFn = origCreate })

	repo, err := e.CreateRepo(t.TempDir())
	if err == nil {
		t.Fatal("CreateRepo() with injected allocation failure must fail")
	}
	if repo != nil {
		t.Fatal("CreateRepo() must not return a repository on failure")
	}

	origRead := odbReadFn
	odbReadFn = func(*odbHandle, git.OID) ([]byte, error) {
		return nil, &git.NativeError{Op: "read blob", Message: "odb backend failure"}
	}
	t.Cleanup(func() { odbReadFn = origRead })

	// The engine must stay usable after both injected failures.
	createRepoFn = libgit2CreateRepo
	good, err := e.CreateRepo(t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo() after injection = %v", err)
	}
	defer good.Close()
	if _, err := good.ReadBlob(git.OID{}); err == nil {
		t.Fatal("ReadBlob() with injected odb failure must fail")
	}
}
