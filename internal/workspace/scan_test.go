package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"golang.org/x/text/unicode/norm"

	"github.com/baalimago/slivingdoc/internal/git"
)

// scanFixture opens a fresh workspace and writes the given files below L.
func scanFixture(t *testing.T, files map[string]string) (*Workspace, error) {
	t.Helper()
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	for path, data := range files {
		host := filepath.Join(w.Path(), filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", host, err)
		}
		if err := os.WriteFile(host, []byte(data), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", host, err)
		}
	}
	return w, nil
}

func snapshotMap(t *testing.T, snap git.Snapshot) map[string]string {
	t.Helper()
	out := make(map[string]string, len(snap.Files))
	for _, f := range snap.Files {
		out[f.Path] = string(f.Data)
	}
	return out
}

func TestScanPreservesBytesAndLineEndings(t *testing.T) {
	files := map[string]string{
		"empty.md":     "",
		"crlf.md":      "line1\r\nline2\r\n",
		"mixed.md":     "a\nb\r\nc\n",
		"nested/a.md":  "nested",
		"unicode/é.md": "café \u00e9\u0301",
	}
	w, err := scanFixture(t, files)
	if err != nil {
		t.Fatalf("scanFixture: %v", err)
	}
	snap, err := w.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	got := snapshotMap(t, snap)
	if len(got) != len(files) {
		t.Fatalf("Snapshot() has %d files, want %d: %v", len(got), len(files), got)
	}
	for path, want := range files {
		if got[path] != want {
			t.Fatalf("file %q = %q, want %q", path, got[path], want)
		}
	}
}

func TestScanRejectsInvalidContent(t *testing.T) {
	cases := map[string]string{
		"invalid utf8": string([]byte{0xff, 0xfe, 0x00}),
		"nul byte":     "a\x00b",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			w, _ := scanFixture(t, map[string]string{"bad.md": data})
			if _, err := w.Snapshot(context.Background()); !errors.Is(err, ErrInvalidContent) {
				t.Fatalf("Snapshot() error = %v, want ErrInvalidContent", err)
			}
		})
	}
}

func TestScanRejectsInvalidNames(t *testing.T) {
	cases := map[string]string{
		"git dir":        ".git",
		"reserved char":  "a*b.md",
		"trailing space": "name ",
		"device name":    "CON",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			w, _ := scanFixture(t, map[string]string{path: "x"})
			if _, err := w.Snapshot(context.Background()); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("Snapshot() error = %v, want ErrInvalidPath", err)
			}
		})
	}
}

func TestScanRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(w.Path(), "link.md")); err != nil {
		t.Fatalf("Symlink(): %v", err)
	}
	if _, err := w.Snapshot(context.Background()); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Snapshot() error = %v, want ErrSymlink", err)
	}
}

func TestScanRejectsSymlinkComponents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("Symlink(): %v", err)
	}
	cfg := Config{
		WorkspaceRoot: root,
		Path:          filepath.Join(root, "linked", "notes"),
		PrivateRoot:   t.TempDir(),
		Identity:      testIdentity(),
		Engine:        newFakeEngine(),
	}
	if _, err := Open(context.Background(), cfg); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Open() error = %v, want ErrSymlink", err)
	}
}

func TestScanRejectsSpecialFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO creation is POSIX-only")
	}
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	pipe := filepath.Join(w.Path(), "pipe")
	if err := unix.Mkfifo(pipe, 0o644); err != nil {
		t.Fatalf("Mkfifo(): %v", err)
	}
	_, err := w.Snapshot(context.Background())
	if !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("Snapshot() error = %v, want ErrUnsupportedFile", err)
	}
	if !strings.Contains(err.Error(), "pipe") {
		t.Fatalf("Snapshot() error %v does not name the relative path", err)
	}
}

func TestScanRejectsFileVersusDirectoryAmbiguity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NFD and NFC names collide on case-insensitive Windows")
	}
	// On-disk directory in NFD and on-disk file in NFC normalize to the
	// same internal path: one entry is a file, the other a directory.
	nfd := norm.NFD.String("café")
	nfc := norm.NFC.String("café")
	if nfd == nfc {
		t.Skip("normalization forms are identical on this platform")
	}
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	dir := filepath.Join(w.Path(), nfd)
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatalf("MkdirAll(NFD dir): %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.Path(), nfc), []byte("file"), 0o644); err != nil {
		t.Fatalf("write NFC file: %v", err)
	}
	if _, err := w.Snapshot(context.Background()); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Snapshot() error = %v, want ErrInvalidPath (ambiguity)", err)
	}
}

func TestScanNormalizesNamesToNFC(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NFD names cannot be created on Windows")
	}
	nfd := norm.NFD.String("café")
	if nfd == norm.NFC.String("café") {
		t.Skip("normalization forms are identical on this platform")
	}
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	if err := os.WriteFile(filepath.Join(w.Path(), nfd), []byte("data"), 0o644); err != nil {
		t.Fatalf("write NFD file: %v", err)
	}
	snap, err := w.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	if len(snap.Files) != 1 || snap.Files[0].Path != "café" {
		t.Fatalf("Snapshot() = %+v, want normalized café", snap.Files)
	}
}

func TestScanRejectsCaseFoldingCollision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("case-folding collisions cannot exist on case-insensitive hosts")
	}
	w, _ := scanFixture(t, map[string]string{"Notes.md": "a", "notes.md": "b"})
	if _, err := w.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot() succeeded for a case-folding collision")
	}
}

func TestScanTreatsHardLinksAsIndependentPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard-link creation needs privileges on Windows")
	}
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	a := filepath.Join(w.Path(), "a.md")
	if err := os.WriteFile(a, []byte("shared"), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	if err := os.Link(a, filepath.Join(w.Path(), "b.md")); err != nil {
		t.Fatalf("Link(): %v", err)
	}
	snap, err := w.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	got := snapshotMap(t, snap)
	if got["a.md"] != "shared" || got["b.md"] != "shared" {
		t.Fatalf("hard-linked files not read as independent paths: %v", got)
	}
}

func TestScanRecreatesMissingVisibleDir(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	if err := os.RemoveAll(w.Path()); err != nil {
		t.Fatalf("RemoveAll(): %v", err)
	}
	snap, err := w.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	if len(snap.Files) != 0 {
		t.Fatalf("Snapshot() = %+v, want empty", snap)
	}
	if _, err := os.Stat(w.Path()); err != nil {
		t.Fatalf("visible directory not recreated: %v", err)
	}
}
