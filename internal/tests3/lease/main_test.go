package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadyPublishesEndpoint(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "endpoint")
	const want = "http://127.0.0.1:8333"
	if err := writeReady(path, want); err != nil {
		t.Fatalf("writeReady() = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	if got := string(data); got != want+"\n" {
		t.Fatalf("ready file = %q, want %q", got, want+"\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("ready file mode = %o, want 600", got)
	}
}

func TestWriteReadyRejectsMissingDirectory(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing", "endpoint")
	if err := writeReady(path, "http://127.0.0.1:8333"); err == nil {
		t.Fatal("writeReady() = nil, want an error for a missing directory")
	}
}
