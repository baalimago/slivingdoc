package git

import (
	"errors"
	"fmt"
)

// Named engine failures. A caller-facing layer can only describe a cause it
// can identify, so every failure this package can classify itself carries one
// of these. Anything else is driver prose of unbounded shape and stays
// internal (architecture/product-contract.md).
var (
	// ErrNoNewObjects reports an increment export with nothing to publish.
	ErrNoNewObjects = errors.New("no new objects")
	// ErrObjectMissing reports notebook content the local object store
	// cannot supply: absent, or present but unreadable. Both are repaired by
	// re-importing the verified packs that carry it, so every failed object
	// read in this package carries it (architecture/pull.md).
	ErrObjectMissing = errors.New("object missing from the object store")
	// ErrEmptyPack reports an import of zero bytes.
	ErrEmptyPack = errors.New("empty pack")
	// ErrHeadRequired reports a boundary operation with no commit.
	ErrHeadRequired = errors.New("commit is required")
)

// UnsupportedModeError reports a tree entry whose file type the notebook
// cannot store. Name is the entry name within its directory, never an
// absolute path, so it is safe to show the caller.
type UnsupportedModeError struct {
	Name string
	Mode FileMode
}

func (e *UnsupportedModeError) Error() string {
	return fmt.Sprintf("unsupported file mode %o for %q", uint32(e.Mode), e.Name)
}

// unreadable marks a failed object read with ErrObjectMissing so a caller
// can tell a recoverable store failure from a content rule the stored state
// breaks. An engine that already classified the read (git2's ENOTFOUND) is
// not annotated twice.
func unreadable(err error, format string, args ...any) error {
	if errors.Is(err, ErrObjectMissing) {
		return fmt.Errorf(format+": %w", append(args, err)...)
	}
	return fmt.Errorf(format+": %w: %w", append(args, ErrObjectMissing, err)...)
}
