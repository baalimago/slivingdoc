package git

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"sort"
)

// ExportIncrement exports one incremental pack containing exactly the objects
// reachable from head that are not already reachable from base. Importing
// the pack into a repository that holds base reconstructs head. The
// descriptor of the pack must identify base as the exact indexed base.
func ExportIncrement(repo Repository, head, base OID) (Pack, error) {
	headSet, err := reachableFromCommit(repo, head, true)
	if err != nil {
		return Pack{}, fmt.Errorf("git: export increment: %w", err)
	}
	baseSet, err := reachableFromCommit(repo, base, true)
	if err != nil {
		return Pack{}, fmt.Errorf("git: export increment: %w", err)
	}
	objects := make([]OID, 0, len(headSet))
	for id := range headSet {
		if _, ok := baseSet[id]; !ok {
			objects = append(objects, id)
		}
	}
	if len(objects) == 0 {
		return Pack{}, fmt.Errorf("git: export increment: no new objects")
	}
	return writePack(repo, objects)
}

// ExportCheckpoint exports a state-complete checkpoint pack: the checkpoint
// commit, its complete tree closure, and every referenced blob. Commit
// ancestors before the checkpoint are intentionally omitted, so the pack
// imports into an empty repository. The caller records the head as the
// shallow history boundary with MarkShallow.
func ExportCheckpoint(repo Repository, head OID) (Pack, error) {
	commit, err := repo.ReadCommit(head)
	if err != nil {
		return Pack{}, fmt.Errorf("git: export checkpoint: %w", err)
	}
	set := map[OID]struct{}{head: {}}
	if err := treeClosure(repo, commit.Tree, set); err != nil {
		return Pack{}, fmt.Errorf("git: export checkpoint: %w", err)
	}
	return writePack(repo, sortedSet(set))
}

// ImportPack validates and imports a complete pack into the repository
// object store. A truncated pack or a corrupt trailer fails without
// importing any of its objects. Pack bytes are already validated against the
// manifest SHA-256 by the storage layer before this call.
func ImportPack(repo Repository, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("git: import pack: empty pack")
	}
	if err := repo.ImportPack(data); err != nil {
		return fmt.Errorf("git: import pack: %w", err)
	}
	return nil
}

// MarkShallow records a commit as a shallow history boundary: its parent
// commits can be absent. A checkpoint pack imports into an empty repository
// because it intentionally omits pre-checkpoint history; MarkShallow declares
// exactly that gap.
func MarkShallow(repo Repository, head OID) error {
	if head.IsZero() {
		return fmt.Errorf("git: mark shallow: commit is required")
	}
	if err := repo.MarkShallow(head); err != nil {
		return fmt.Errorf("git: mark shallow: %w", err)
	}
	return nil
}

// ValidateHistory walks every commit reachable from head and verifies that
// the commit chain and the complete tree closure resolve. The single
// permitted gap is the declared shallow boundary: the parents of shallow
// can be missing because the checkpoint pack omits them intentionally. Any other
// missing commit, tree, or blob fails the walk.
//
// One seen set spans the whole walk. Consecutive generations share almost
// all of their trees and blobs, so an already-proven subtree short-circuits
// at its root OID and the walk costs the number of unique objects, not
// commits times files. An OID names one object whatever its type, so
// commits, trees, and blobs share the set safely.
func ValidateHistory(repo Repository, head, shallow OID) error {
	queue := []OID{head}
	seen := map[OID]struct{}{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		commit, err := repo.ReadCommit(id)
		if err != nil {
			return fmt.Errorf("git: validate history: commit %s: %w", id, err)
		}
		if err := treeClosureValidate(repo, commit.Tree, seen); err != nil {
			return fmt.Errorf("git: validate history: commit %s: %w", id, err)
		}
		for _, parent := range commit.Parents {
			if _, err := repo.ReadCommit(parent); err != nil {
				if id == shallow {
					continue // the declared boundary can name missing history
				}
				return fmt.Errorf("git: validate history: parent %s of %s unavailable: %w", parent, id, err)
			}
			queue = append(queue, parent)
		}
	}
	return nil
}

// writePack writes one complete pack for the given objects, hashing the pack
// bytes while writing so the returned SHA-256 covers exactly the exported
// bytes. Objects are sorted by OID so the exported bytes are stable for the
// pinned libgit2 release.
func writePack(repo Repository, objects []OID) (Pack, error) {
	sort.Slice(objects, func(i, j int) bool { return bytes.Compare(objects[i][:], objects[j][:]) < 0 })
	var buf bytes.Buffer
	hasher := sha256.New()
	count, err := repo.WritePack(objects, io.MultiWriter(&buf, hasher))
	if err != nil {
		return Pack{}, fmt.Errorf("git: write pack: %w", err)
	}
	var sum [32]byte
	copy(sum[:], hasher.Sum(nil))
	return Pack{Data: buf.Bytes(), SHA256: sum, ObjectCount: count}, nil
}

func sortedSet(set map[OID]struct{}) []OID {
	objects := make([]OID, 0, len(set))
	for id := range set {
		objects = append(objects, id)
	}
	sort.Slice(objects, func(i, j int) bool { return bytes.Compare(objects[i][:], objects[j][:]) < 0 })
	return objects
}

// reachableFromCommit collects every object reachable from a commit: the
// commit, its tree closure, and — when includeParents is set — its complete
// ancestor history.
func reachableFromCommit(repo Repository, head OID, includeParents bool) (map[OID]struct{}, error) {
	set := map[OID]struct{}{}
	seen := map[OID]struct{}{}
	queue := []OID{head}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		commit, err := repo.ReadCommit(id)
		if err != nil {
			return nil, fmt.Errorf("commit %s: %w", id, err)
		}
		set[id] = struct{}{}
		if err := treeClosure(repo, commit.Tree, set); err != nil {
			return nil, fmt.Errorf("commit %s: %w", id, err)
		}
		if includeParents {
			queue = append(queue, commit.Parents...)
		}
	}
	return set, nil
}

// treeClosure records a tree, every subtree, and every referenced blob in
// set. It rejects modes outside the slivingdoc subset.
func treeClosure(repo Repository, tree OID, set map[OID]struct{}) error {
	seen := map[OID]struct{}{}
	queue := []OID{tree}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		entries, err := repo.ReadTree(id)
		if err != nil {
			return fmt.Errorf("tree %s: %w", id, err)
		}
		set[id] = struct{}{}
		for _, e := range entries {
			switch e.Mode {
			case ModeTree:
				queue = append(queue, e.ID)
			case ModeBlob:
				set[e.ID] = struct{}{}
			default:
				return fmt.Errorf("unsupported file mode %o for %q", e.Mode, e.Name)
			}
		}
	}
	return nil
}

// treeClosureValidate verifies that a tree, every subtree, and every
// referenced blob resolve in the repository object store. Objects already
// in seen are proven and skipped; every object proven here joins the set.
// Presence is answered by HasObject, never ReadBlob: validation needs
// existence, and reading would inflate every blob only to discard it.
func treeClosureValidate(repo Repository, tree OID, seen map[OID]struct{}) error {
	queue := []OID{tree}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		entries, err := repo.ReadTree(id)
		if err != nil {
			return fmt.Errorf("tree %s: %w", id, err)
		}
		for _, e := range entries {
			switch e.Mode {
			case ModeTree:
				queue = append(queue, e.ID)
			case ModeBlob:
				if _, ok := seen[e.ID]; ok {
					continue
				}
				present, err := repo.HasObject(e.ID)
				if err != nil {
					return fmt.Errorf("blob %s: %w", e.ID, err)
				}
				if !present {
					return fmt.Errorf("blob %s: missing from the object store", e.ID)
				}
				seen[e.ID] = struct{}{}
			default:
				return fmt.Errorf("unsupported file mode %o for %q", e.Mode, e.Name)
			}
		}
	}
	return nil
}
