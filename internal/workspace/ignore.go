package workspace

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrInvalidIgnore reports an ignore pattern that cannot be matched.
var ErrInvalidIgnore = errors.New("workspace: invalid ignore pattern")

// DefaultIgnore lists the names every notebook directory skips: the files
// operating systems and editors scatter that nobody means to publish
// (architecture/workspace.md, Ignored paths).
var DefaultIgnore = []string{
	".DS_Store",
	"._*",
	".AppleDouble",
	".Spotlight-V100",
	".Trashes",
	".fseventsd",
	".TemporaryItems",
	"Thumbs.db",
	"desktop.ini",
	"*.swp",
	"*.swo",
	".git",
	".slivingdoc-tmp-*",
}

// Ignore decides which notebook paths the workspace never reads, publishes,
// overwrites or removes. The zero value ignores nothing.
type Ignore struct {
	names   []string
	anchors []string
}

// NewIgnore validates patterns and returns the set. A pattern without a
// slash matches an entry of that name at any depth; a pattern with a slash
// is anchored at the notebook directory and matches that path and
// everything below it. Patterns use path.Match syntax.
func NewIgnore(patterns []string) (Ignore, error) {
	var ig Ignore
	for _, raw := range patterns {
		p := strings.Trim(strings.TrimSpace(raw), "/")
		if p == "" {
			return Ignore{}, fmt.Errorf("%w: %q is empty", ErrInvalidIgnore, raw)
		}
		if _, err := path.Match(p, ""); err != nil {
			return Ignore{}, fmt.Errorf("%w: %q: %w", ErrInvalidIgnore, raw, err)
		}
		if strings.Contains(p, "/") {
			ig.anchors = append(ig.anchors, p)
			continue
		}
		ig.names = append(ig.names, p)
	}
	return ig, nil
}

// Configured reports whether the set ignores anything.
func (ig Ignore) Configured() bool { return len(ig.names)+len(ig.anchors) > 0 }

// Ignored reports whether the notebook path, or any directory above it, is
// ignored.
func (ig Ignore) Ignored(p string) bool {
	if !ig.Configured() {
		return false
	}
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		for _, pattern := range ig.names {
			if ok, _ := path.Match(pattern, seg); ok {
				return true
			}
		}
		prefix := strings.Join(segs[:i+1], "/")
		for _, pattern := range ig.anchors {
			if ok, _ := path.Match(pattern, prefix); ok {
				return true
			}
		}
	}
	return false
}
