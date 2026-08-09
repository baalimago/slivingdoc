package workspace

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidPath reports a requested visible path that is not an absolute
// path at or below the workspace root (architecture sections 7.3 and 18.2).
var ErrInvalidPath = errors.New("workspace: invalid path")

// PathEscapeError reports a requested visible path that is not below the
// workspace root (architecture sections 7.3 and 18.2). Root is the
// caller-visible workspace root; Path is the rejected request. Caller text
// must name Root without echoing Path, because the rejected path may be a
// guess at private state.
type PathEscapeError struct {
	Path string
	Root string
}

func (e *PathEscapeError) Error() string {
	return fmt.Sprintf("%v: %q escapes the workspace root %q", ErrInvalidPath, e.Path, e.Root)
}

func (e *PathEscapeError) Unwrap() error { return ErrInvalidPath }

// CanonicalPath cleans the requested absolute path and requires it to stay
// at or below the workspace root (architecture section 7.3). The check is
// lexical: symlink policy is enforced separately at every filesystem touch.
// The returned path is the canonical host form used for visible-directory
// operations.
func CanonicalPath(root, path string) (string, error) {
	canonical, _, err := canonicalize(root, path)
	return canonical, err
}

// canonicalize validates the requested path against the workspace root and
// returns the canonical host path and its slash-separated form relative to
// the root ("." when the visible path is the root itself). The relative
// form never contains ".." and never starts with a slash, so it is safe
// for os.Root relative operations.
func canonicalize(root, path string) (canonical, rel string, err error) {
	if !filepath.IsAbs(root) {
		return "", "", fmt.Errorf("%w: workspace root %q is not absolute", ErrInvalidPath, root)
	}
	if !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("%w: path %q is not absolute", ErrInvalidPath, path)
	}
	canonical = filepath.Clean(path)
	rel, err = filepath.Rel(root, canonical)
	if err != nil {
		return "", "", fmt.Errorf("%w: %q is not below %q: %v", ErrInvalidPath, path, root, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", &PathEscapeError{Path: path, Root: root}
	}
	return canonical, filepath.ToSlash(rel), nil
}

// RootsOverlap reports whether the private root is at or below the
// workspace root. The architecture (section 17) forbids that overlap: P
// would otherwise live inside a visible directory and its contents would
// become notebook state.
func RootsOverlap(privateRoot, workspaceRoot string) bool {
	rel, err := filepath.Rel(workspaceRoot, privateRoot)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
