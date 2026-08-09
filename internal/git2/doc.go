// Package git2 is the only CGo and libgit2 boundary in slivingdoc.
//
// It implements the internal/git Engine contract against a pinned, statically
// linked libgit2 release. No C pointer or libgit2 type crosses this package
// boundary: the exported API uses only internal/git types.
//
// Native implementation files carry the standard cgo build constraint. When
// the binary is built without CGo (CGO_ENABLED=0), New returns a stub that
// reports git.ErrUnavailable; the stub never emulates Git behavior.
//
// The pinned libgit2 release and its source checksum are recorded in
// PinnedVersion and scripts/build-libgit2.sh. See docs/build-libgit2.md for
// the build procedure and NOTICE for the license notice.
package git2
