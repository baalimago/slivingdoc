package scratch

import (
	"errors"
	"os"
	"testing"
)

func TestUseKeepsAnExplicitTmpdir(t *testing.T) {
	t.Setenv("TMPDIR", "/explicit")
	cleanup, err := Use()
	if err != nil {
		t.Fatalf("Use() = %v", err)
	}
	cleanup()
	if got := os.Getenv("TMPDIR"); got != "/explicit" {
		t.Fatalf("TMPDIR = %q, want it untouched", got)
	}
}

func TestUseMovesTmpdirOrSaysWhy(t *testing.T) {
	t.Setenv("TMPDIR", "")
	if err := os.Unsetenv("TMPDIR"); err != nil {
		t.Fatal(err)
	}
	cleanup, err := Use()
	var unavailable *Unavailable
	if errors.As(err, &unavailable) {
		if unavailable.Error() == "" || os.Getenv("TMPDIR") != "" {
			t.Fatalf("Use() = %v with TMPDIR %q, want the default kept", err, os.Getenv("TMPDIR"))
		}
		cleanup()
		return
	}
	if err != nil {
		t.Fatalf("Use() = %v", err)
	}
	dir := os.Getenv("TMPDIR")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("TMPDIR = %q: %v, want a directory", dir, err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("scratch %q still exists after cleanup: %v", dir, err)
	}
}
