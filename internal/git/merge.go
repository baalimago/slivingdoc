package git

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Marker signature lines at column zero, in order, form one complete
// conflict-marker block (architecture/conflicts.md). The labels are exact.
const (
	markerOpen  = "<<<<<<< local"
	markerSep   = "======="
	markerClose = ">>>>>>> remote"
)

// Merge performs a three-tree merge of base, local, and remote trees with
// the pinned libgit2: local change is base -> local, remote change is
// base -> remote, and the result merges both. When the merge is
// conflict-free it returns the merged tree; otherwise it returns structured
// conflicts with conflict-marker content and one-based marker ranges for
// every text conflict. A text conflict below a file/directory conflict
// whose local side is a file is not reported (dropBelowLocalFiles). A
// conflict never creates a commit.
func Merge(repo Repository, base, local, remote OID) (MergeResult, error) {
	idx, err := repo.MergeTrees(base, local, remote)
	if err != nil {
		return MergeResult{}, fmt.Errorf("git: merge: %w", err)
	}
	// The engine accepts only modes 100644 and 040000, so a hostile pack
	// cannot smuggle a symlink or submodule mode into a merged result.
	byPath := make(map[string][]IndexEntry, len(idx.Entries))
	for _, e := range idx.Entries {
		if e.Mode != ModeBlob && e.Mode != ModeTree {
			return MergeResult{}, fmt.Errorf("git: merge: %w", &UnsupportedModeError{Name: e.Path, Mode: e.Mode})
		}
		byPath[e.Path] = append(byPath[e.Path], e)
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var conflicts []Conflict
	for _, path := range paths {
		entries := byPath[path]
		conflicted := false
		for _, e := range entries {
			if e.Stage != 0 {
				conflicted = true
				break
			}
		}
		if !conflicted {
			continue
		}
		// A file-versus-directory conflict: the other side replaced the
		// path with a directory, which libgit2 represents as resolved
		// entries below the conflicted path. There is no text to merge,
		// so the conflict carries no marker content.
		if isDirFileConflict(path, byPath) {
			conflicts = append(conflicts, Conflict{Path: path})
			continue
		}
		c := Conflict{Path: path}
		content, ranges, err := materializeConflict(repo, entries)
		if err != nil {
			return MergeResult{}, fmt.Errorf("git: merge: conflict %q: %w", path, err)
		}
		c.Content = content
		c.Ranges = ranges
		conflicts = append(conflicts, c)
	}

	conflicts, err = dropBelowLocalFiles(repo, local, conflicts)
	if err != nil {
		return MergeResult{}, fmt.Errorf("git: merge: %w", err)
	}

	if len(conflicts) == 0 {
		if idx.Tree.IsZero() {
			return MergeResult{}, fmt.Errorf("git: merge: conflict-free index produced no tree")
		}
		return MergeResult{Tree: idx.Tree, Index: idx}, nil
	}
	return MergeResult{Index: idx, Conflicts: conflicts}, nil
}

// dropBelowLocalFiles removes the text conflicts that lie below a
// file/directory conflict whose local side is a file. Such a path cannot
// exist beside the local file in L, so materializing its markers would
// either lose the file or the markers; the file/directory conflict at the
// path already reports it. The local tree decides which side is the file.
func dropBelowLocalFiles(repo Repository, local OID, conflicts []Conflict) ([]Conflict, error) {
	dfPaths := make(map[string]bool)
	for _, c := range conflicts {
		if c.Content == nil {
			dfPaths[c.Path] = true
		}
	}
	if len(dfPaths) == 0 {
		return conflicts, nil
	}
	localSide, err := ReadSnapshot(repo, local)
	if err != nil {
		return nil, fmt.Errorf("read local side: %w", err)
	}
	localFiles := make(map[string]bool)
	for _, f := range localSide.Files {
		if dfPaths[f.Path] {
			localFiles[f.Path] = true
		}
	}
	if len(localFiles) == 0 {
		return conflicts, nil
	}
	kept := make([]Conflict, 0, len(conflicts))
	for _, c := range conflicts {
		if c.Content != nil && belowDirFileConflict(c.Path, localFiles) {
			continue
		}
		kept = append(kept, c)
	}
	return kept, nil
}

// materializeConflict produces the conflict-marker content for one conflicted
// path and the marker ranges inside it. It returns nil content when the
// conflict is not a text merge (a file/directory conflict has a tree stage
// and cannot carry markers).
func materializeConflict(repo Repository, entries []IndexEntry) (content []byte, ranges []MarkerRange, err error) {
	var base, local, remote *IndexEntry
	for i := range entries {
		switch entries[i].Stage {
		case 1:
			base = &entries[i]
		case 2:
			local = &entries[i]
		case 3:
			remote = &entries[i]
		}
	}
	for _, e := range []*IndexEntry{base, local, remote} {
		if e != nil && e.Mode != ModeBlob {
			return nil, nil, fmt.Errorf("unsupported file mode %o for conflicted path", e.Mode)
		}
	}
	read := func(e *IndexEntry) ([]byte, error) {
		if e == nil {
			return nil, nil // deleted side
		}
		data, err := repo.ReadBlob(e.ID)
		if err != nil {
			return nil, fmt.Errorf("read stage %d blob %s: %w", e.Stage, e.ID, err)
		}
		return data, nil
	}
	b, err := read(base)
	if err != nil {
		return nil, nil, err
	}
	l, err := read(local)
	if err != nil {
		return nil, nil, err
	}
	r, err := read(remote)
	if err != nil {
		return nil, nil, err
	}
	// libgit2's merge-file wrapper drops the marker label of a side whose
	// input is empty, so a delete conflict ends in a bare `>>>>>>>`
	// line. The marker contract requires exact signatures, so the deleted
	// side is formatted here with the exact labels, matching Git's own
	// merge-file output.
	if local == nil || remote == nil {
		content := formatDeleteConflict(l, r)
		return content, FindConflictBlocks(content), nil
	}
	res, err := repo.MergeFile(b, l, r)
	if err != nil {
		return nil, nil, err
	}
	if !res.Automergeable {
		ranges = FindConflictBlocks(res.Content)
	}
	return res.Content, ranges, nil
}

// formatDeleteConflict renders the conflict-marker block for a modify/delete
// or delete/modify conflict: the surviving side's content between the exact
// `local` and `remote` labels, the deleted side empty.
func formatDeleteConflict(local, remote []byte) []byte {
	var b bytes.Buffer
	b.WriteString(markerOpen)
	b.WriteByte('\n')
	b.Write(local)
	if len(local) == 0 || local[len(local)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString(markerSep)
	b.WriteByte('\n')
	b.Write(remote)
	if len(remote) == 0 || remote[len(remote)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString(markerClose)
	b.WriteByte('\n')
	return b.Bytes()
}

// isDirFileConflict reports whether a conflicted path is a file-versus-
// directory replacement: the merge index carries entries below the path,
// which only happens when a side occupies the path as a directory. libgit2
// represents such conflicts with the file side's stages at the path and the
// directory side's one-sided entries resolved to stage 0 under it, whichever
// side holds the directory.
func isDirFileConflict(path string, byPath map[string][]IndexEntry) bool {
	prefix := path + "/"
	for other := range byPath {
		if strings.HasPrefix(other, prefix) {
			return true
		}
	}
	return false
}

// belowDirFileConflict reports whether path lies below a file/directory
// conflict path. Materialization takes such paths from the local tree.
func belowDirFileConflict(path string, dfPaths map[string]bool) bool {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' && dfPaths[path[:i]] {
			return true
		}
	}
	return false
}

// MaterializeTree builds the complete materialized snapshot of a merge
// result. A conflict-free result reads its merged tree. A conflicted result
// materializes the full index: resolved stage-0 entries keep their merged
// blobs and text conflicts carry their marker content. A file/directory
// conflict shows the local side in both directions, read from the local
// tree rather than from index stages: the local file when the remote side
// made the path a directory, the local subtree when the local side did. The
// remote side is omitted there and stays only in R. local is the tree that
// was merged as the local side. The
// returned snapshot is the exact visible state a caller must see, so a
// conflict never loses the caller's local intent (architecture/conflicts.md).
func MaterializeTree(repo Repository, res MergeResult, local OID) (Snapshot, error) {
	if !res.Tree.IsZero() {
		snap, err := ReadSnapshot(repo, res.Tree)
		if err != nil {
			return Snapshot{}, fmt.Errorf("git: materialize tree: %w", err)
		}
		return snap, nil
	}

	// A conflicted path without marker content is a file/directory
	// replacement. libgit2 resolves the directory side's one-sided entries
	// to stage 0 whichever side holds the directory, so the stages cannot
	// tell the local side from the remote one: the local tree does.
	dfPaths := make(map[string]bool)
	text := make(map[string][]byte)
	for _, c := range res.Conflicts {
		if c.Content == nil {
			dfPaths[c.Path] = true
		} else {
			text[c.Path] = c.Content
		}
	}

	byPath := make(map[string][]IndexEntry, len(res.Index.Entries))
	for _, e := range res.Index.Entries {
		byPath[e.Path] = append(byPath[e.Path], e)
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var files []File
	for _, path := range paths {
		if content, ok := text[path]; ok {
			files = append(files, File{Path: path, Data: content})
			continue
		}
		if dfPaths[path] || belowDirFileConflict(path, dfPaths) {
			continue // the local side is read from the local tree below
		}
		// A resolved path has exactly one stage-0 blob entry.
		entries := byPath[path]
		if len(entries) != 1 || entries[0].Stage != 0 || entries[0].Mode != ModeBlob {
			return Snapshot{}, fmt.Errorf("git: materialize tree: unexpected index shape at %q", path)
		}
		data, err := repo.ReadBlob(entries[0].ID)
		if err != nil {
			return Snapshot{}, fmt.Errorf("git: materialize tree: %q: %w", path, err)
		}
		files = append(files, File{Path: path, Data: data})
	}

	if len(dfPaths) > 0 {
		localSide, err := ReadSnapshot(repo, local)
		if err != nil {
			return Snapshot{}, fmt.Errorf("git: materialize tree: read local side: %w", err)
		}
		for _, f := range localSide.Files {
			if _, marked := text[f.Path]; marked {
				continue
			}
			if dfPaths[f.Path] || belowDirFileConflict(f.Path, dfPaths) {
				files = append(files, f)
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	}

	snap := Snapshot{Files: files}
	if err := ValidateSnapshot(snap); err != nil {
		return Snapshot{}, fmt.Errorf("git: materialize tree: %w", err)
	}
	return snap, nil
}

// FindConflictBlocks returns the one-based, inclusive row ranges of every
// complete, non-nested conflict-marker block in data. A complete block is
// exactly the lines `<<<<<<< local`, `=======`, and `>>>>>>> remote` at
// column zero in that order. LF and CRLF line endings are accepted and the
// line terminator is ignored during comparison. A block whose signature
// differs in any character is ordinary text. A nested opener inside a block
// is content, so scanning resumes after the closing line.
func FindConflictBlocks(data []byte) []MarkerRange {
	var (
		ranges []MarkerRange
		start  int
		state  int // 0 outside a block, 1 after the opener, 2 after the separator
		row    = 1
	)
	for len(data) > 0 {
		var line []byte
		if before, after, ok := bytes.Cut(data, []byte{'\n'}); ok {
			line, data = before, after
		} else {
			line, data = data, nil
		}
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		switch string(line) {
		case markerOpen:
			// A nested opener inside a block is content; only the first
			// opener starts a block.
			if state == 0 {
				start, state = row, 1
			}
		case markerSep:
			if state == 1 {
				state = 2
			}
		case markerClose:
			if state == 2 {
				ranges = append(ranges, MarkerRange{Start: start, End: row})
				state = 0
			}
		}
		row++
	}
	return ranges
}
