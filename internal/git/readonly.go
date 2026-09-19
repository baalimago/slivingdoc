package git

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/text/cases"
)

// EntrySet is a normalized, sorted set of notebook path entries, held by
// one side of a PathPolicy (architecture section 2, Read-only paths). An
// entry covers itself and every path below it, matched under the same case
// folding as ValidateSnapshot. The zero value is the empty set.
type EntrySet struct {
	entries []string
	folded  []string
}

var entryFold = cases.Fold()

// entryKind names the policy side an entry came from, so a refusal can
// point at the setting the operator wrote.
type entryKind string

const (
	kindReadOnly entryKind = "read-only"
	kindWritable entryKind = "writable"
)

// NormalizeEntries validates each entry with ValidatePath after trimming
// one trailing slash, drops entries covered by another entry, and sorts the
// rest. The error names the invalid entry.
func NormalizeEntries(entries []string) (EntrySet, error) {
	return normalizeEntries(entries, kindReadOnly)
}

// entryItem is one entry as the operator wrote it, trimmed and validated,
// beside the folded form every comparison uses.
type entryItem struct{ raw, folded string }

func normalizeEntries(entries []string, kind entryKind) (EntrySet, error) {
	items, err := validateEntries(entries, kind)
	if err != nil {
		return EntrySet{}, err
	}
	return collapseEntries(items, nil), nil
}

// validateEntries trims one trailing slash and validates each entry,
// keeping every written entry. Coverage within the set is collapsed only by
// collapseEntries, so a caller comparing two sets can still see an entry
// the operator wrote in both (architecture section 2, Writable paths).
func validateEntries(entries []string, kind entryKind) ([]entryItem, error) {
	items := make([]entryItem, 0, len(entries))
	for _, raw := range entries {
		trimmed := strings.TrimSuffix(raw, "/")
		if err := ValidatePath(trimmed); err != nil {
			return nil, fmt.Errorf("invalid %s path %q: %w", kind, raw, err)
		}
		items = append(items, entryItem{raw: trimmed, folded: entryFold.String(trimmed)})
	}
	return items, nil
}

// collapseEntries drops an entry made redundant by another entry of the
// same set, de-duplicates, and sorts. An entry is redundant only when its
// own-set ancestor decides every path it decides: if an entry of the other
// set lies strictly between the two, the ancestor loses the longest match
// there and the covered entry is kept, so the collapse normalizes what is
// advertised without deciding resolution (architecture section 2, Writable
// paths). other is nil for a set normalized on its own.
func collapseEntries(items, other []entryItem) EntrySet {
	var kept []entryItem
	for _, it := range items {
		covered := false
		for _, anc := range items {
			if anc.folded == it.folded {
				continue
			}
			if isEntryAncestor(anc.folded, it.folded) && !entryBetween(anc.folded, it.folded, other) {
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

	set := EntrySet{entries: make([]string, len(kept)), folded: make([]string, len(kept))}
	for i, k := range kept {
		set.entries[i] = k.raw
		set.folded[i] = k.folded
	}
	return set
}

// entryBetween reports whether an entry of other is a proper descendant of
// foldedAncestor and a proper ancestor of foldedEntry, which is the case in
// which foldedAncestor cannot stand for foldedEntry.
func entryBetween(foldedAncestor, foldedEntry string, other []entryItem) bool {
	for _, o := range other {
		if isEntryAncestor(foldedAncestor, o.folded) && isEntryAncestor(o.folded, foldedEntry) {
			return true
		}
	}
	return false
}

// isEntryAncestor reports whether foldedPath is a proper descendant of
// foldedAncestor on a segment boundary.
func isEntryAncestor(foldedAncestor, foldedPath string) bool {
	return len(foldedPath) > len(foldedAncestor) &&
		strings.HasPrefix(foldedPath, foldedAncestor) &&
		foldedPath[len(foldedAncestor)] == '/'
}

// Entries returns a copy of the sorted entries; never nil.
func (s EntrySet) Entries() []string {
	out := make([]string, len(s.entries))
	copy(out, s.entries)
	return out
}

// Covers reports whether the set protects path.
func (s EntrySet) Covers(path string) bool {
	_, ok := s.CoveringEntry(path)
	return ok
}

// CoveringEntry returns the longest entry covering path. A set can hold an
// entry below another when the other policy set splits them, so the most
// specific match is the one that decides.
func (s EntrySet) CoveringEntry(path string) (string, bool) {
	raw, _, ok := s.covering(path)
	return raw, ok
}

// covering returns the raw and folded longest entry covering path. The
// folded form carries the match length the policy resolves longest-match on.
func (s EntrySet) covering(path string) (raw, folded string, ok bool) {
	foldedPath := entryFold.String(path)
	for i, e := range s.folded {
		if foldedPath != e && !isEntryAncestor(e, foldedPath) {
			continue
		}
		if !ok || len(e) > len(folded) {
			raw, folded, ok = s.entries[i], e, true
		}
	}
	return raw, folded, ok
}

// ChangedUnder returns, sorted, every covered path that differs between
// local and base (added, removed, or different bytes).
func (s EntrySet) ChangedUnder(local, base Snapshot) []string {
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
func (s EntrySet) Pin(local, base Snapshot) Snapshot {
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
func (s EntrySet) ReadCovered(repo Repository, tree OID) (Snapshot, error) {
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
func (s EntrySet) walkCovered(repo Repository, tree OID, prefix string, inside bool, files *[]File) error {
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
func (s EntrySet) hasEntryBelow(path string) bool {
	folded := entryFold.String(path)
	for _, e := range s.folded {
		if isEntryAncestor(folded, e) {
			return true
		}
	}
	return false
}
