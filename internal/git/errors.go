package git

import (
	"errors"
	"fmt"
)

// Named engine failures. A caller-facing layer can only describe a cause it
// can identify, so every failure this package can classify itself carries one
// of these. Anything else is driver prose of unbounded shape and stays
// internal (architecture section 2).
var (
	// ErrNoNewObjects reports an increment export with nothing to publish.
	ErrNoNewObjects = errors.New("no new objects")
	// ErrObjectMissing reports notebook content absent from the local store.
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
