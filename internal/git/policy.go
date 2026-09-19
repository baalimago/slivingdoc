package git

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// PathPolicy answers, for one process, which notebook paths may be written
// and which changes to protected paths an operation must refuse or restore
// (architecture section 2, Read-only paths). The two entry sets compose by
// longest match; the unmatched default is writable while the writable set
// is empty and protected once it is not. The zero value is unconfigured and
// protects nothing.
type PathPolicy struct {
	readOnly EntrySet
	writable EntrySet
}

// OverlapError reports one path named by both sets, the only composition
// with no unambiguous resolution.
type OverlapError struct {
	ReadOnly string
	Writable string
}

func (e *OverlapError) Error() string {
	if e.ReadOnly == e.Writable {
		return fmt.Sprintf("git: path %q is named by both the read-only and the writable set", e.ReadOnly)
	}
	return fmt.Sprintf("git: read-only path %q and writable path %q name the same path", e.ReadOnly, e.Writable)
}

// NewPolicy normalizes each set and refuses a path named by both. The
// overlap test runs over the entries as the operator wrote them, before
// each set drops its own covered entries, so an entry named by both
// settings is refused whatever else its set contains. Coverage across the
// sets is kept, not collapsed: a writable entry below a read-only entry is
// the composition the operator is expressing. Each set is collapsed against
// the other for the same reason — an entry the other set splits from its
// own-set ancestor still decides paths the ancestor does not, so it
// survives and the collapsed sets resolve exactly as the written ones do
// (architecture section 2, Writable paths).
func NewPolicy(readOnly, writable []string) (PathPolicy, error) {
	ro, err := validateEntries(readOnly, kindReadOnly)
	if err != nil {
		return PathPolicy{}, err
	}
	wr, err := validateEntries(writable, kindWritable)
	if err != nil {
		return PathPolicy{}, err
	}
	for _, r := range ro {
		for _, w := range wr {
			if r.folded == w.folded {
				return PathPolicy{}, &OverlapError{ReadOnly: r.raw, Writable: w.raw}
			}
		}
	}
	return PathPolicy{readOnly: collapseEntries(ro, wr), writable: collapseEntries(wr, ro)}, nil
}

// Configured reports whether either set is non-empty.
func (p PathPolicy) Configured() bool {
	return len(p.readOnly.entries) > 0 || len(p.writable.entries) > 0
}

// ReadOnly returns a copy of the normalized read-only entries; never nil.
// The collapse keeps every entry resolution needs, so the pair of accessors
// is a lossless input to NewPolicy: a policy rebuilt from them protects
// exactly the same paths, which is what lets a process carry its two sets
// as entries rather than as a policy value (architecture section 2,
// Writable paths).
func (p PathPolicy) ReadOnly() []string { return p.readOnly.Entries() }

// Writable returns a copy of the normalized writable entries; never nil,
// and a lossless input to NewPolicy as ReadOnly's entries are.
func (p PathPolicy) Writable() []string { return p.writable.Entries() }

// Protects reports whether path may not be written. The longest entry
// across both sets decides; with no match the answer is the unmatched
// default. The longest match is unique: within a set covering resolves the
// longest entry, and two matches of equal folded length across the sets
// would be the same folded string, which NewPolicy refuses. The answer is
// the one the entries the operator wrote give, since the collapse drops
// only an entry its own-set ancestor decides for (README invariant 5).
func (p PathPolicy) Protects(path string) bool {
	if !p.Configured() {
		return false
	}
	_, roFolded, roOK := p.readOnly.covering(path)
	_, wrFolded, wrOK := p.writable.covering(path)
	switch {
	case roOK && wrOK:
		return len(roFolded) > len(wrFolded)
	case roOK:
		return true
	case wrOK:
		return false
	default:
		return len(p.writable.entries) > 0
	}
}

// ChangedProtected returns, sorted, every protected path that differs
// between the local and base trees. It descends only where subtree IDs
// differ and reads no blob: a differing blob is proved by its differing
// object ID.
func (p PathPolicy) ChangedProtected(repo Repository, local, base OID) ([]string, error) {
	if !p.Configured() || local == base {
		return nil, nil
	}
	var changed []string
	if err := p.diffTrees(repo, local, base, "", &changed); err != nil {
		return nil, fmt.Errorf("git: changed protected paths: %w", err)
	}
	sort.Strings(changed)
	return changed, nil
}

// diffTrees reports the protected differences between two trees rooted at
// prefix. Equal IDs never reach it, so every call has work to do.
func (p PathPolicy) diffTrees(repo Repository, local, base OID, prefix string, out *[]string) error {
	localEntries, err := readTreeAt(repo, local, prefix)
	if err != nil {
		return err
	}
	baseEntries, err := readTreeAt(repo, base, prefix)
	if err != nil {
		return err
	}
	baseByName := make(map[string]TreeEntry, len(baseEntries))
	for _, e := range baseEntries {
		baseByName[e.Name] = e
	}
	for _, le := range localEntries {
		be, ok := baseByName[le.Name]
		if !ok {
			if err := p.collectSide(repo, le, prefix, out); err != nil {
				return err
			}
			continue
		}
		delete(baseByName, le.Name)
		if le.Mode == be.Mode && le.ID == be.ID {
			continue
		}
		if le.Mode == ModeTree && be.Mode == ModeTree {
			if err := p.diffTrees(repo, le.ID, be.ID, prefix+le.Name+"/", out); err != nil {
				return err
			}
			continue
		}
		// Same name, different shape or different content: both sides are
		// reported, so a type change names the blob path and every path
		// beneath the tree.
		if err := p.collectSide(repo, le, prefix, out); err != nil {
			return err
		}
		if err := p.collectSide(repo, be, prefix, out); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(baseByName))
	for name := range baseByName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := p.collectSide(repo, baseByName[name], prefix, out); err != nil {
			return err
		}
	}
	return nil
}

// collectSide reports every protected blob path of one tree entry that
// exists on a single side.
func (p PathPolicy) collectSide(repo Repository, e TreeEntry, prefix string, out *[]string) error {
	path := prefix + e.Name
	switch e.Mode {
	case ModeBlob:
		if p.Protects(path) {
			*out = appendUnique(*out, path)
		}
		return nil
	case ModeTree:
		return p.collectTree(repo, e.ID, path+"/", out)
	default:
		return &UnsupportedModeError{Name: path, Mode: e.Mode}
	}
}

func (p PathPolicy) collectTree(repo Repository, tree OID, prefix string, out *[]string) error {
	entries, err := readTreeAt(repo, tree, prefix)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := p.collectSide(repo, e, prefix, out); err != nil {
			return err
		}
	}
	return nil
}

// RestoreProtected returns local with each named path taken from base: a
// path absent from local is added back, one absent from base is dropped,
// and a local directory standing where base holds a file is removed with
// it. It reads only the named paths.
func (p PathPolicy) RestoreProtected(repo Repository, base OID, local Snapshot, changed []string) (Snapshot, error) {
	if len(changed) == 0 {
		return local, nil
	}
	restored := make([]File, 0, len(changed))
	for _, path := range changed {
		data, ok, err := readBlobAt(repo, base, path)
		if err != nil {
			return Snapshot{}, fmt.Errorf("git: restore protected paths: %w", err)
		}
		if ok {
			restored = append(restored, File{Path: path, Data: data})
		}
	}
	files := make([]File, 0, len(local.Files)+len(restored))
	for _, f := range local.Files {
		if !namedOrUnder(f.Path, changed) {
			files = append(files, f)
		}
	}
	files = append(files, restored...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	snap := Snapshot{Files: files}
	if err := ValidateSnapshot(snap); err != nil {
		return Snapshot{}, fmt.Errorf("git: restore protected paths: %w", err)
	}
	return snap, nil
}

// readBlobAt resolves path in tree and reads it, reporting absence rather
// than failing: detection cannot name a path on neither side, and a caller
// that does is not corrupting state.
func readBlobAt(repo Repository, tree OID, path string) ([]byte, bool, error) {
	segments := strings.Split(path, "/")
	prefix := ""
	for i, seg := range segments {
		entries, err := readTreeAt(repo, tree, prefix)
		if err != nil {
			return nil, false, err
		}
		entry, ok := findEntry(entries, seg)
		if !ok {
			return nil, false, nil
		}
		last := i == len(segments)-1
		switch {
		case last && entry.Mode == ModeBlob:
			data, err := repo.ReadBlob(entry.ID)
			if err != nil {
				return nil, false, fmt.Errorf("blob %q (%s): %w", path, entry.ID, err)
			}
			return data, true, nil
		case !last && entry.Mode == ModeTree:
			tree = entry.ID
			prefix += seg + "/"
		default:
			return nil, false, nil
		}
	}
	return nil, false, nil
}

func findEntry(entries []TreeEntry, name string) (TreeEntry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return TreeEntry{}, false
}

// readTreeAt names the directory in the failure, so a caller can report
// where the walk stopped without exposing an object ID on its own.
func readTreeAt(repo Repository, tree OID, prefix string) ([]TreeEntry, error) {
	entries, err := repo.ReadTree(tree)
	if err != nil {
		return nil, fmt.Errorf("tree %q (%s): %w", strings.TrimSuffix(prefix, "/"), tree, err)
	}
	return entries, nil
}

// namedOrUnder reports whether path is one of names or lies below one.
func namedOrUnder(path string, names []string) bool {
	for _, name := range names {
		if path == name || strings.HasPrefix(path, name+"/") {
			return true
		}
	}
	return false
}

func appendUnique(paths []string, path string) []string {
	if slices.Contains(paths, path) {
		return paths
	}
	return append(paths, path)
}
