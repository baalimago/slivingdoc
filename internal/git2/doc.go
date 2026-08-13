// Package git2 is the only CGo and libgit2 boundary in slivingdoc.
//
// It implements the internal/git Engine contract against a pinned, statically
// linked libgit2 release. No C pointer or libgit2 type crosses this package
// boundary: the exported API uses only internal/git types.
//
// There is no pure-Go build: this package requires CGo, so
// CGO_ENABLED=0 fails to compile instead of producing a binary that
// starts and then fails every operation.
//
// The pinned libgit2 release and its source checksum are recorded in
// PinnedVersion and scripts/build-libgit2.sh. See docs/build.md for
// the build procedure and NOTICE for the license notice.
package git2
