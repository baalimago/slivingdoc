package keyrecord

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// lookup answers from a map, so a test never reads the developer's own
// environment.
func lookup(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

// testFile is the key record of a fresh configuration directory.
func testFile(t *testing.T) File {
	t.Helper()
	f, err := Locate(lookup(map[string]string{credentials.DirEnv: filepath.Join(t.TempDir(), "cfg")}), runtime.GOOS)
	if err != nil {
		t.Fatalf("Locate() = %v", err)
	}
	return f
}

// testDir is a fresh 0700 directory below the per-test temporary directory.
// A configuration directory is checked for group and other write access, and
// t.TempDir itself is not private under every umask.
func testDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// testIdentity is one notebook storage identity, distinguished by its bucket.
func testIdentity(bucket string) workspace.Identity {
	return workspace.Identity{
		Endpoint:        "https://s3.example",
		Region:          "eu-west-1",
		Bucket:          bucket,
		Prefix:          "notes",
		ManifestVersion: workspace.ManifestVersion,
	}
}

// writeFile creates dir and writes data to path mode 0600.
func writeFile(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

// mustPassphrase is a stored passphrase entry that cannot fail to build.
func mustPassphrase(t *testing.T, digest, secret string) Entry {
	t.Helper()
	e, err := Passphrase(digest, secret)
	if err != nil {
		t.Fatalf("Passphrase() = %v", err)
	}
	return e
}
