package git

import "time"

// FileMode is a Git tree-entry file mode. Slivingdoc accepts exactly two
// modes — regular files (100644) and directories (040000) — and rejects
// every other mode on every host (architecture section 8.2).
type FileMode uint32

const (
	// ModeBlob is the only accepted file mode for notebook content.
	ModeBlob FileMode = 0o100644
	// ModeTree is the mode of every directory tree.
	ModeTree FileMode = 0o040000
)

// TreeEntry is one entry of a Git tree object: a single path segment with its
// mode and object ID. Callers store entries in Git tree order; the native
// boundary re-sorts defensively on write.
type TreeEntry struct {
	Name string
	Mode FileMode
	ID   OID
}

// Commit is the Go-facing view of a Git commit object: its tree, parent
// chain, and message. The engine always writes the fixed slivingdoc identity,
// so that identity is not part of the value.
type Commit struct {
	Tree    OID
	Parents []OID
	Message string
}

// CommitSpec describes a commit the engine should create. Author and
// committer are the fixed slivingdoc identity; Time is the operation-attempt
// start time and is stored in UTC with one-second precision and offset zero.
type CommitSpec struct {
	Message string
	Tree    OID
	Parents []OID
	Time    time.Time
}

// AuthorName and AuthorEmail form the fixed commit identity from
// architecture section 8.2. Every commit uses it; there is no
// user-configurable identity.
const (
	AuthorName  = "slivingdoc"
	AuthorEmail = "slivingdoc@localhost"
)

// IndexEntry is one entry of a merge index: a path with its stage. Stage 0
// marks a resolved entry; stages 1, 2, and 3 mark the base, local, and
// remote variants of a conflict.
type IndexEntry struct {
	Path  string
	Mode  FileMode
	ID    OID
	Stage int
}

// MergeIndex is the raw result of a three-tree merge: the resolved tree when
// the merge is conflict-free and every index entry (resolved and conflicted)
// for conflict reporting. Tree is the zero OID when the index contains
// conflicts.
type MergeIndex struct {
	Tree    OID
	Entries []IndexEntry
}

// MergeFileResult is the result of a file-level three-way merge. When
// Automergeable is false, Content carries conflict-marker text with the
// exact `local` and `remote` labels.
type MergeFileResult struct {
	Content       []byte
	Automergeable bool
}

// MarkerRange identifies one complete conflict-marker block inside a file.
// Rows are one-based and inclusive, counted from the first line of the file.
type MarkerRange struct {
	Start int
	End   int
}

// Conflict describes one conflicted path after a three-tree merge. Content
// holds the conflict-marker text for text conflicts and is nil for
// file/directory conflicts; Ranges lists every complete marker block in
// Content.
type Conflict struct {
	Path    string
	Content []byte
	Ranges  []MarkerRange
}

// MergeResult is the policy-level result of a three-tree merge. Tree is the
// merged tree (the zero OID when the merge has conflicts); Index carries the
// raw merge index so callers can materialize the full conflicted result;
// Conflicts describes every conflicted path in the merged result.
type MergeResult struct {
	Tree      OID
	Index     MergeIndex
	Conflicts []Conflict
}

// Snapshot is a complete file snapshot: an ordered set of normalized,
// slash-separated relative paths with their content. BuildTree and
// ReadSnapshot sort the files by path, so a snapshot is deterministic for
// the same set of paths.
type Snapshot struct {
	Files []File
}

// File is one text file in a snapshot.
type File struct {
	Path string
	Data []byte
}

// Pack is an exported Git pack: the exact pack bytes, the SHA-256 of those
// bytes, and the number of objects the pack contains. The SHA-256 is the
// pack-integrity checksum the storage layer records in manifest descriptors
// (architecture section 9.3).
type Pack struct {
	Data        []byte
	SHA256      [32]byte
	ObjectCount int
}
