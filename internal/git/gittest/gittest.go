// Package gittest holds the deterministic object hashing shared by the
// fake Repository implementations in higher packages (internal/workspace,
// internal/notebook). internal/git's own in-package tests cannot import
// this package (it would be an import cycle) and keep a local copy.
package gittest

import (
	"crypto/sha1"
	"fmt"

	"github.com/baalimago/slivingdoc/internal/git"
)

// ObjectID hashes a fake Git object to a deterministic OID using the
// Git object-header form: "<kind> <len>\x00<data>". It is a test
// identity only, not a Git-compatible object ID.
func ObjectID(kind string, data []byte) git.OID {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(data))
	h.Write(data)
	var id git.OID
	copy(id[:], h.Sum(nil))
	return id
}
