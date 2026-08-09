package git

import (
	"fmt"
	"sort"
	"strings"
)

// BuildTree writes one deterministic tree for a snapshot: blobs use mode
// 100644, trees use mode 040000, and every level is sorted by Git tree order.
// The same normalized snapshot always produces the same tree OID.
func BuildTree(repo Repository, snap Snapshot) (OID, error) {
	if err := ValidateSnapshot(snap); err != nil {
		return OID{}, fmt.Errorf("git: build tree: %w", err)
	}
	files := append([]File(nil), snap.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	tree, err := buildTree(repo, files)
	if err != nil {
		return OID{}, fmt.Errorf("git: build tree: %w", err)
	}
	return tree, nil
}

// buildTree writes the tree for a path-sorted snapshot fragment. Every path
// in the fragment shares one directory context; the caller strips that
// context before recursing.
func buildTree(repo Repository, files []File) (OID, error) {
	entries := make([]TreeEntry, 0, len(files))
	for i := 0; i < len(files); {
		path := files[i].Path
		if found := strings.Contains(path, "/"); !found {
			// A regular file at this level.
			id, err := repo.WriteBlob(files[i].Data)
			if err != nil {
				return OID{}, fmt.Errorf("blob %q: %w", path, err)
			}
			entries = append(entries, TreeEntry{Name: path, Mode: ModeBlob, ID: id})
			i++
			continue
		}
		// A subtree: every remaining file below this segment. Sorting by
		// path keeps the members contiguous.
		seg := path[:strings.IndexByte(path, '/')]
		prefix := seg + "/"
		j := i
		for j < len(files) && strings.HasPrefix(files[j].Path, prefix) {
			j++
		}
		sub := make([]File, 0, j-i)
		for _, f := range files[i:j] {
			sub = append(sub, File{Path: f.Path[len(prefix):], Data: f.Data})
		}
		id, err := buildTree(repo, sub)
		if err != nil {
			return OID{}, fmt.Errorf("tree %q: %w", seg, err)
		}
		entries = append(entries, TreeEntry{Name: seg, Mode: ModeTree, ID: id})
		i = j
	}
	sort.SliceStable(entries, func(a, b int) bool { return treeEntryLess(entries[a], entries[b]) })
	id, err := repo.WriteTree(entries)
	if err != nil {
		return OID{}, fmt.Errorf("write tree: %w", err)
	}
	return id, nil
}

// ReadSnapshot reads a complete UTF-8 text-file snapshot from a tree. It
// rejects unsupported modes, unsafe path names, and invalid content while
// walking, so a corrupt or hostile pack fails before any caller sees its
// state. The returned snapshot is sorted by path.
func ReadSnapshot(repo Repository, tree OID) (Snapshot, error) {
	var files []File
	if err := walkTree(repo, tree, "", &files); err != nil {
		return Snapshot{}, fmt.Errorf("git: read snapshot: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	snap := Snapshot{Files: files}
	if err := ValidateSnapshot(snap); err != nil {
		return Snapshot{}, fmt.Errorf("git: read snapshot: %w", err)
	}
	return snap, nil
}

func walkTree(repo Repository, tree OID, prefix string, files *[]File) error {
	entries, err := repo.ReadTree(tree)
	if err != nil {
		return fmt.Errorf("tree %s: %w", tree, err)
	}
	for _, e := range entries {
		path := prefix + e.Name
		switch e.Mode {
		case ModeTree:
			if err := walkTree(repo, e.ID, path+"/", files); err != nil {
				return err
			}
		case ModeBlob:
			data, err := repo.ReadBlob(e.ID)
			if err != nil {
				return fmt.Errorf("blob %q (%s): %w", path, e.ID, err)
			}
			*files = append(*files, File{Path: path, Data: data})
		default:
			return fmt.Errorf("unsupported file mode %o for %q", e.Mode, path)
		}
	}
	return nil
}

// EmptyTree returns the empty tree, creating it in the repository on first
// use. Pull uses the empty tree as the merge base for the first pull.
func EmptyTree(repo Repository) (OID, error) {
	id, err := repo.WriteTree(nil)
	if err != nil {
		return OID{}, fmt.Errorf("git: empty tree: %w", err)
	}
	return id, nil
}

// treeEntryLess orders tree entries in Git tree order: byte-wise by name,
// with a tree entry comparing as if its name carried a trailing slash. This
// matches git_tree_entry_cmp in the pinned libgit2, so BuildTree output is
// identical to libgit2's own sorting.
func treeEntryLess(a, b TreeEntry) bool {
	min := len(a.Name)
	if len(b.Name) < min {
		min = len(b.Name)
	}
	if cmp := strings.Compare(a.Name[:min], b.Name[:min]); cmp != 0 {
		return cmp < 0
	}
	var ca, cb byte
	if len(a.Name) > min {
		ca = a.Name[min]
	} else if a.Mode == ModeTree {
		ca = '/'
	}
	if len(b.Name) > min {
		cb = b.Name[min]
	} else if b.Mode == ModeTree {
		cb = '/'
	}
	return ca < cb
}
