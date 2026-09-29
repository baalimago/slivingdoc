// Package scratch moves a test binary's temporary files onto a memory
// filesystem. The workspace, the credentials file and the private
// repositories fsync and create many small files, which on a disk made
// several test packages several times slower and pushed them past the
// 30-second gate (architecture/testing.md, Scratch on a memory filesystem).
package scratch

import (
	"fmt"
	"os"
)

// Unavailable says why the default temporary directory stays.
type Unavailable struct{ reason string }

func (e *Unavailable) Error() string { return "memory scratch unavailable: " + e.reason }

// Use points TMPDIR at a fresh directory on a memory filesystem when TMPDIR
// is unset, and returns the function that removes it; the function is never
// nil. An explicit TMPDIR wins. The error is an *Unavailable when the host
// has no fitting memory filesystem, or the failure to set TMPDIR; either way
// the run continues on the default directory, so callers report it on stderr
// and go on.
func Use() (cleanup func(), err error) {
	cleanup = func() {}
	if _, set := os.LookupEnv("TMPDIR"); set {
		return cleanup, nil
	}
	dir, err := memoryScratch()
	if err != nil {
		return cleanup, err
	}
	if err := os.Setenv("TMPDIR", dir); err != nil {
		_ = os.RemoveAll(dir)
		return cleanup, fmt.Errorf("scratch: set TMPDIR: %w", err)
	}
	return func() { _ = os.RemoveAll(dir) }, nil
}
