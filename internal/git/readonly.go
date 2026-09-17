package git

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/text/cases"
)

// ReadOnlySet is a normalized, sorted set of read-only notebook paths
// (architecture section 2, Read-only paths). An entry covers itself and
// every path below it, matched under the same case folding as
// ValidateSnapshot. The zero value is the empty set.
type ReadOnlySet struct {
	entries []string
	folded  []string
}

var readOnlyFold = cases.Fold()

// NormalizeReadOnly validates each entry with ValidatePath after trimming
// one trailing slash, drops entries covered by another entry, and sorts the
// rest. The error names the invalid entry.
func NormalizeReadOnly(entries []string) (ReadOnlySet, error) {
	type item struct{ raw, folded string }
	items := make([]item, 0, len(entries))
	for _, raw := range entries {
		trimmed := strings.TrimSuffix(raw, "/")
		if err := ValidatePath(trimmed); err != nil {
			return ReadOnlySet{}, fmt.Errorf("invalid read-only path %q: %w", raw, err)
		}
		items = append(items, item{raw: trimmed, folded: readOnlyFold.String(trimmed)})
	}

	var kept []item
	for _, it := range items {
		covered := false
		for _, other := range items {
			if other.folded == it.folded {
				continue
			}
			if isReadOnlyAncestor(other.folded, it.folded) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		exists := false
		for _, k := range kept {
			if k.folded == it.folded {
				exists = true
				break
			}
		}
		if !exists {
			kept = append(kept, it)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].raw < kept[j].raw })

	set := ReadOnlySet{entries: make([]string, len(kept)), folded: make([]string, len(kept))}
	for i, k := range kept {
		set.entries[i] = k.raw
		set.folded[i] = k.folded
	}
	return set, nil
}

// isReadOnlyAncestor reports whether foldedPath is a proper descendant of
// foldedAncestor on a segment boundary.
func isReadOnlyAncestor(foldedAncestor, foldedPath string) bool {
	return len(foldedPath) > len(foldedAncestor) &&
		strings.HasPrefix(foldedPath, foldedAncestor) &&
		foldedPath[len(foldedAncestor)] == '/'
}

// Entries returns a copy of the sorted entries; never nil.
func (s ReadOnlySet) Entries() []string {
	out := make([]string, len(s.entries))
	copy(out, s.entries)
	return out
}

// Covers reports whether the set protects path.
func (s ReadOnlySet) Covers(path string) bool {
	_, ok := s.CoveringEntry(path)
	return ok
}

// CoveringEntry returns the entry that covers path. Normalization keeps
// entries disjoint, so at most one can match.
func (s ReadOnlySet) CoveringEntry(path string) (string, bool) {
	folded := readOnlyFold.String(path)
	for i, e := range s.folded {
		if folded == e || isReadOnlyAncestor(e, folded) {
			return s.entries[i], true
		}
	}
	return "", false
}

// ChangedUnder returns, sorted, every covered path that differs between
// local and base (added, removed, or different bytes).
func (s ReadOnlySet) ChangedUnder(local, base Snapshot) []string {
	if len(s.entries) == 0 {
		return nil
	}
	localData := make(map[string][]byte, len(local.Files))
	for _, f := range local.Files {
		if s.Covers(f.Path) {
			localData[f.Path] = f.Data
		}
	}
	baseData := make(map[string][]byte, len(base.Files))
	for _, f := range base.Files {
		if s.Covers(f.Path) {
			baseData[f.Path] = f.Data
		}
	}
	var changed []string
	for path, data := range localData {
		if old, ok := baseData[path]; !ok || !bytes.Equal(old, data) {
			changed = append(changed, path)
		}
	}
	for path := range baseData {
		if _, ok := localData[path]; !ok {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

// Pin returns local with its covered files replaced by base's covered
// files, sorted by path.
func (s ReadOnlySet) Pin(local, base Snapshot) Snapshot {
	if len(s.entries) == 0 {
		return local
	}
	files := make([]File, 0, len(local.Files)+len(base.Files))
	for _, f := range local.Files {
		if !s.Covers(f.Path) {
			files = append(files, f)
		}
	}
	for _, f := range base.Files {
		if s.Covers(f.Path) {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Snapshot{Files: files}
}

// ReadCovered reads only the covered files of tree, descending into a
// subtree only when it is covered or contains an entry, and validates the
// result like ReadSnapshot.
func (s ReadOnlySet) ReadCovered(repo Repository, tree OID) (Snapshot, error) {
	if len(s.entries) == 0 {
		return Snapshot{}, nil
	}
	var files []File
	if err := s.walkCovered(repo, tree, "", false, &files); err != nil {
		return Snapshot{}, fmt.Errorf("git: read covered snapshot: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	snap := Snapshot{Files: files}
	if err := ValidateSnapshot(snap); err != nil {
		return Snapshot{}, fmt.Errorf("git: read covered snapshot: %w", err)
	}
	return snap, nil
}

// walkCovered is ReadCovered's tree walk; inside is true below a covered
// ancestor.
func (s ReadOnlySet) walkCovered(repo Repository, tree OID, prefix string, inside bool, files *[]File) error {
	entries, err := repo.ReadTree(tree)
	if err != nil {
		return fmt.Errorf("tree %s: %w", tree, err)
	}
	for _, e := range entries {
		path := prefix + e.Name
		covered := inside || s.Covers(path)
		switch e.Mode {
		case ModeTree:
			if !covered && !s.hasEntryBelow(path) {
				continue
			}
			if err := s.walkCovered(repo, e.ID, path+"/", covered, files); err != nil {
				return err
			}
		case ModeBlob:
			if !covered {
				continue
			}
			data, err := repo.ReadBlob(e.ID)
			if err != nil {
				return fmt.Errorf("blob %q (%s): %w", path, e.ID, err)
			}
			*files = append(*files, File{Path: path, Data: data})
		default:
			return &UnsupportedModeError{Name: path, Mode: e.Mode}
		}
	}
	return nil
}

// hasEntryBelow reports whether some entry lies strictly below path.
func (s ReadOnlySet) hasEntryBelow(path string) bool {
	folded := readOnlyFold.String(path)
	for _, e := range s.folded {
		if isReadOnlyAncestor(folded, e) {
			return true
		}
	}
	return false
}
