package git2

/*
#cgo pkg-config: --static libgit2
// On Windows the link keeps the mingw-w64 compiler runtime static, so the
// release binary depends only on Windows system DLLs (the dependency
// baseline enforced by scripts/check-deps-windows.sh). The Go link line
// never passes -pthread, so winpthread is not linked at all and needs no
// static-forcing flag (this gcc rejects -static-libwinpthread).
#cgo windows LDFLAGS: -static-libgcc
#include <git2.h>
#include <stdlib.h>
#include <string.h>

// Copy helpers keep git_oid out of the Go-facing API. A git_oid is exactly
// 20 bytes; git.OID has the same layout.
static void sl_oid_to_bytes(unsigned char *out, const git_oid *in) {
	memcpy(out, in->id, 20);
}

static void sl_oid_from_bytes(git_oid *out, const unsigned char *in) {
	memcpy(out->id, in, 20);
}

// sl_merge_file_input fills one merge-file input from raw bytes. mode 0
// marks a deleted side; the caller keeps the data alive during the merge.
static void sl_merge_file_input(git_merge_file_input *in, const char *ptr, size_t size, unsigned int mode) {
	memset(in, 0, sizeof(*in));
	in->version = GIT_MERGE_FILE_INPUT_VERSION;
	in->ptr = ptr;
	in->size = size;
	in->mode = mode;
}

// sl_merge_file runs the whole file merge inside C. The input structs hold
// pointers into Go memory; building them in C keeps those pointers out of
// any struct that crosses the cgo boundary, which the pointer rules
// require. The data pointers themselves are only valid for this call.
static int sl_merge_file(
	const char *base, size_t base_size, int base_exists,
	const char *local, size_t local_size, int local_exists,
	const char *remote, size_t remote_size, int remote_exists,
	git_merge_file_result *out)
{
	git_merge_file_input ancestor, ours, theirs;
	git_merge_file_options opts = GIT_MERGE_FILE_OPTIONS_INIT;

	sl_merge_file_input(&ancestor, base_exists ? base : NULL, base_size, base_exists ? GIT_FILEMODE_BLOB : 0);
	sl_merge_file_input(&ours, local_exists ? local : NULL, local_size, local_exists ? GIT_FILEMODE_BLOB : 0);
	sl_merge_file_input(&theirs, remote_exists ? remote : NULL, remote_size, remote_exists ? GIT_FILEMODE_BLOB : 0);

	opts.ancestor_label = "base";
	opts.our_label = "local";
	opts.their_label = "remote";

	return git_merge_file(out, &ancestor, &ours, &theirs, &opts);
}

// sl_odb_writepack_* forward to the writepack vtable. The stats argument is
// required non-NULL by libgit2's indexer, so each call passes a local
// progress struct; slivingdoc reports import progress through Go errors,
// not indexer callbacks.
static int sl_odb_writepack_append(git_odb_writepack *wp, const void *data, size_t size) {
	git_indexer_progress stats = { 0 };
	return wp->append(wp, data, size, &stats);
}

static int sl_odb_writepack_commit(git_odb_writepack *wp) {
	git_indexer_progress stats = { 0 };
	return wp->commit(wp, &stats);
}

static void sl_odb_writepack_free(git_odb_writepack *wp) {
	wp->free(wp);
}
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/baalimago/slivingdoc/internal/git"
)

// The seam functions are package variables so white-box tests can inject
// failures deterministically. Production values call the pinned libgit2
// directly; a test override must restore the original value.
var (
	initFn         = func() int32 { return int32(C.git_libgit2_init()) }
	shutdownFn     = func() int32 { return int32(C.git_libgit2_shutdown()) }
	versionFn      = libgit2Version
	featuresFn     = func() int { return int(C.git_libgit2_features()) }
	errorLastFn    = libgit2ErrorLast
	createRepoFn   = libgit2CreateRepo
	openRepoFn     = libgit2OpenRepo
	repoODBFn      = libgit2RepoODB
	odbWriteFn     = libgit2ODBWrite
	odbReadFn      = libgit2ODBRead
	writeTreeFn    = libgit2WriteTree
	readTreeFn     = libgit2ReadTree
	createCommitFn = libgit2CreateCommit
	readCommitFn   = libgit2ReadCommit
	mergeTreesFn   = libgit2MergeTrees
	mergeFileFn    = libgit2MergeFile
	writePackFn    = libgit2WritePack
	importPackFn   = libgit2ImportPack
	markShallowFn  = libgit2MarkShallow
)

type repoHandle struct{ ptr *C.git_repository }

func (h *repoHandle) free() {
	C.git_repository_free(h.ptr)
	h.ptr = nil
}

type odbHandle struct{ ptr *C.git_odb }

func (h *odbHandle) free() {
	C.git_odb_free(h.ptr)
	h.ptr = nil
}

func libgit2Version() (major, minor, rev int) {
	var maj, min, rv C.int
	C.git_libgit2_version(&maj, &min, &rv)
	return int(maj), int(min), int(rv)
}

// libgit2ErrorLast copies the thread-local error before returning, so the
// caller keeps valid detail after further native calls.
func libgit2ErrorLast() (class int, message string) {
	e := C.git_error_last()
	if e == nil {
		return 0, ""
	}
	return int(e.klass), C.GoString(e.message)
}

func libgit2CreateRepo(path string, bare bool) (*repoHandle, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var repo *C.git_repository
	flag := C.uint(0)
	if bare {
		flag = C.uint(1)
	}
	if rc := C.git_repository_init(&repo, cpath, flag); rc < 0 {
		return nil, nativeError("create repository")
	}
	return &repoHandle{ptr: repo}, nil
}

func libgit2OpenRepo(path string) (*repoHandle, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var repo *C.git_repository
	if rc := C.git_repository_open(&repo, cpath); rc < 0 {
		return nil, nativeError("open repository")
	}
	return &repoHandle{ptr: repo}, nil
}

func libgit2RepoODB(repo *repoHandle) (*odbHandle, error) {
	var odb *C.git_odb
	if rc := C.git_repository_odb(&odb, repo.ptr); rc < 0 {
		return nil, nativeError("open object database")
	}
	return &odbHandle{ptr: odb}, nil
}

func libgit2ODBWrite(odb *odbHandle, data []byte) (git.OID, error) {
	var (
		cid C.git_oid
		p   unsafe.Pointer
	)
	if len(data) > 0 {
		p = unsafe.Pointer(&data[0])
	}
	if rc := C.git_odb_write(&cid, odb.ptr, p, C.size_t(len(data)), C.GIT_OBJECT_BLOB); rc < 0 {
		return git.OID{}, nativeError("write blob")
	}
	var id git.OID
	C.sl_oid_to_bytes((*C.uchar)(unsafe.Pointer(&id[0])), &cid)
	return id, nil
}

func libgit2ODBRead(odb *odbHandle, id git.OID) ([]byte, error) {
	var (
		cid C.git_oid
		obj *C.git_odb_object
	)
	C.sl_oid_from_bytes(&cid, (*C.uchar)(unsafe.Pointer(&id[0])))
	if rc := C.git_odb_read(&obj, odb.ptr, &cid); rc < 0 {
		err := nativeError("read blob") // captures the error before the free below
		if obj != nil {
			C.git_odb_object_free(obj)
		}
		return nil, err
	}
	defer C.git_odb_object_free(obj)
	size := C.git_odb_object_size(obj)
	if size > C.size_t(math.MaxInt32) {
		return nil, &git.NativeError{Op: "read blob", Message: "blob exceeds maximum supported size"}
	}
	return C.GoBytes(C.git_odb_object_data(obj), C.int(size)), nil
}

// libgit2WriteTree writes a tree from its entries, rejecting every mode
// outside the slivingdoc subset (100644 blobs, 040000 trees). libgit2 itself
// rejects unsafe entry names and missing referenced objects; the tree is
// written in canonical Git tree order regardless of insertion order.
func libgit2WriteTree(repo *repoHandle, entries []git.TreeEntry) (git.OID, error) {
	var bld *C.git_treebuilder
	if rc := C.git_treebuilder_new(&bld, repo.ptr, nil); rc < 0 {
		return git.OID{}, nativeError("create tree builder")
	}
	defer C.git_treebuilder_free(bld)

	for _, e := range entries {
		switch e.Mode {
		case git.ModeBlob, git.ModeTree:
		default:
			return git.OID{}, &git.NativeError{
				Op:      "write tree",
				Message: fmt.Sprintf("unsupported file mode %o for %q", e.Mode, e.Name),
			}
		}
		cname := C.CString(e.Name)
		var cid C.git_oid
		C.sl_oid_from_bytes(&cid, (*C.uchar)(unsafe.Pointer(&e.ID[0])))
		rc := C.git_treebuilder_insert(nil, bld, cname, &cid, C.git_filemode_t(e.Mode))
		C.free(unsafe.Pointer(cname))
		if rc < 0 {
			return git.OID{}, nativeError("insert tree entry")
		}
	}
	var oid C.git_oid
	if rc := C.git_treebuilder_write(&oid, bld); rc < 0 {
		return git.OID{}, nativeError("write tree")
	}
	var id git.OID
	C.sl_oid_to_bytes((*C.uchar)(unsafe.Pointer(&id[0])), &oid)
	return id, nil
}

// libgit2ReadTree returns the entries of a tree in Git tree order with their
// raw libgit2 modes, so policy can reject symlinks, submodules, and
// executable blobs.
func libgit2ReadTree(repo *repoHandle, id git.OID) ([]git.TreeEntry, error) {
	var (
		cid  C.git_oid
		tree *C.git_tree
	)
	C.sl_oid_from_bytes(&cid, (*C.uchar)(unsafe.Pointer(&id[0])))
	if rc := C.git_tree_lookup(&tree, repo.ptr, &cid); rc < 0 {
		return nil, nativeError("lookup tree")
	}
	defer C.git_tree_free(tree)

	count := int(C.git_tree_entrycount(tree))
	entries := make([]git.TreeEntry, 0, count)
	for i := 0; i < count; i++ {
		e := C.git_tree_entry_byindex(tree, C.size_t(i))
		var oid git.OID
		C.sl_oid_to_bytes((*C.uchar)(unsafe.Pointer(&oid[0])), C.git_tree_entry_id(e))
		entries = append(entries, git.TreeEntry{
			Name: C.GoString(C.git_tree_entry_name(e)),
			Mode: git.FileMode(C.git_tree_entry_filemode(e)),
			ID:   oid,
		})
	}
	return entries, nil
}

// libgit2CreateCommit writes a commit with the fixed slivingdoc identity,
// the spec time in UTC with one-second precision and offset zero, and zero
// or more parents. Every parent must already exist in the repository, so a
// checkpoint head must be imported before a later increment names it.
func libgit2CreateCommit(repo *repoHandle, spec git.CommitSpec) (git.OID, error) {
	tree, err := lookupTree(repo, spec.Tree)
	if err != nil {
		return git.OID{}, fmt.Errorf("lookup commit tree: %w", err)
	}
	defer C.git_tree_free(tree)

	cname := C.CString(git.AuthorName)
	defer C.free(unsafe.Pointer(cname))
	cemail := C.CString(git.AuthorEmail)
	defer C.free(unsafe.Pointer(cemail))
	var sig *C.git_signature
	if rc := C.git_signature_new(&sig, cname, cemail, C.git_time_t(spec.Time.Unix()), 0); rc < 0 {
		return git.OID{}, nativeError("create signature")
	}
	defer C.git_signature_free(sig)

	parents := make([]*C.git_commit, len(spec.Parents))
	for i, pid := range spec.Parents {
		var cid C.git_oid
		C.sl_oid_from_bytes(&cid, (*C.uchar)(unsafe.Pointer(&pid[0])))
		if rc := C.git_commit_lookup(&parents[i], repo.ptr, &cid); rc < 0 {
			freeCommits(parents[:i])
			return git.OID{}, nativeError("lookup parent commit")
		}
	}
	defer freeCommits(parents)

	var parentsPtr **C.git_commit
	if len(parents) > 0 {
		parentsPtr = &parents[0]
	}
	cmessage := C.CString(spec.Message)
	defer C.free(unsafe.Pointer(cmessage))

	var oid C.git_oid
	if rc := C.git_commit_create(&oid, repo.ptr, nil, sig, sig, nil, cmessage, tree, C.size_t(len(parents)), parentsPtr); rc < 0 {
		return git.OID{}, nativeError("create commit")
	}
	var id git.OID
	C.sl_oid_to_bytes((*C.uchar)(unsafe.Pointer(&id[0])), &oid)
	return id, nil
}

// libgit2ReadCommit returns the tree, parent chain, and message of a commit.
// Parent IDs come from the commit object itself, so a shallow commit with
// absent history still reads; libgit2 grafts declared shallow roots to zero
// parents once the shallow file is loaded.
func libgit2ReadCommit(repo *repoHandle, id git.OID) (git.Commit, error) {
	var (
		cid    C.git_oid
		commit *C.git_commit
	)
	C.sl_oid_from_bytes(&cid, (*C.uchar)(unsafe.Pointer(&id[0])))
	if rc := C.git_commit_lookup(&commit, repo.ptr, &cid); rc < 0 {
		return git.Commit{}, nativeError("lookup commit")
	}
	defer C.git_commit_free(commit)

	var tree git.OID
	C.sl_oid_to_bytes((*C.uchar)(unsafe.Pointer(&tree[0])), C.git_commit_tree_id(commit))
	count := int(C.git_commit_parentcount(commit))
	parents := make([]git.OID, 0, count)
	for i := 0; i < count; i++ {
		var p git.OID
		C.sl_oid_to_bytes((*C.uchar)(unsafe.Pointer(&p[0])), C.git_commit_parent_id(commit, C.uint(i)))
		parents = append(parents, p)
	}
	return git.Commit{Tree: tree, Parents: parents, Message: C.GoString(C.git_commit_message(commit))}, nil
}

// libgit2MergeTrees merges three explicit trees without history-based
// merge-base discovery. Rename detection is disabled (a rename is a
// deletion plus an addition) and no external merge driver or configuration
// is consulted. The index entries carry their stages so policy can structure
// conflicts; the merged tree is written only when the index is conflict-free.
func libgit2MergeTrees(repo *repoHandle, base, local, remote git.OID) (git.MergeIndex, error) {
	ancestor, err := lookupTree(repo, base)
	if err != nil {
		return git.MergeIndex{}, fmt.Errorf("merge base tree: %w", err)
	}
	defer C.git_tree_free(ancestor)
	ours, err := lookupTree(repo, local)
	if err != nil {
		return git.MergeIndex{}, fmt.Errorf("merge local tree: %w", err)
	}
	defer C.git_tree_free(ours)
	theirs, err := lookupTree(repo, remote)
	if err != nil {
		return git.MergeIndex{}, fmt.Errorf("merge remote tree: %w", err)
	}
	defer C.git_tree_free(theirs)

	var opts C.git_merge_options
	if rc := C.git_merge_options_init(&opts, C.GIT_MERGE_OPTIONS_VERSION); rc < 0 {
		return git.MergeIndex{}, nativeError("init merge options")
	}
	opts.flags = 0 // no rename detection, no fail-on-conflict (architecture 8.2)

	var idx *C.git_index
	if rc := C.git_merge_trees(&idx, repo.ptr, ancestor, ours, theirs, &opts); rc < 0 {
		return git.MergeIndex{}, nativeError("merge trees")
	}
	defer C.git_index_free(idx)

	merged := git.MergeIndex{}
	count := int(C.git_index_entrycount(idx))
	for i := 0; i < count; i++ {
		e := C.git_index_get_byindex(idx, C.size_t(i))
		if e == nil {
			continue
		}
		var oid git.OID
		C.sl_oid_to_bytes((*C.uchar)(unsafe.Pointer(&oid[0])), &e.id)
		merged.Entries = append(merged.Entries, git.IndexEntry{
			Path:  C.GoString(e.path),
			Mode:  git.FileMode(e.mode),
			ID:    oid,
			Stage: int(C.git_index_entry_stage(e)),
		})
	}
	for _, e := range merged.Entries {
		if e.Stage != 0 {
			return merged, nil // unmerged entries: no resolvable tree
		}
	}
	var treeOID C.git_oid
	if rc := C.git_index_write_tree_to(&treeOID, idx, repo.ptr); rc < 0 {
		return git.MergeIndex{}, nativeError("write merged tree")
	}
	var id git.OID
	C.sl_oid_to_bytes((*C.uchar)(unsafe.Pointer(&id[0])), &treeOID)
	merged.Tree = id
	return merged, nil
}

// libgit2MergeFile performs a file-level three-way merge with the exact
// `local` and `remote` conflict-marker labels. A nil side is a deleted file
// (mode 0); a non-nil side is a regular file, even when empty.
func libgit2MergeFile(base, local, remote []byte) (git.MergeFileResult, error) {
	var result C.git_merge_file_result
	rc := C.sl_merge_file(
		bytesPtr(base), C.size_t(len(base)), exists(base),
		bytesPtr(local), C.size_t(len(local)), exists(local),
		bytesPtr(remote), C.size_t(len(remote)), exists(remote),
		&result,
	)
	if rc < 0 {
		return git.MergeFileResult{}, nativeError("merge file")
	}
	defer C.git_merge_file_result_free(&result)

	out := git.MergeFileResult{Automergeable: result.automergeable != 0}
	if result.len > C.size_t(math.MaxInt32) {
		return git.MergeFileResult{}, &git.NativeError{Op: "merge file", Message: "merge result exceeds maximum supported size"}
	}
	if result.len > 0 {
		out.Content = C.GoBytes(unsafe.Pointer(result.ptr), C.int(result.len))
	}
	return out, nil
}

// libgit2WritePack writes one complete pack containing exactly the listed
// objects and returns the number of objects it contains. The packbuilder is
// single-threaded by default, so the exported bytes are deterministic for
// the pinned libgit2 release. Delta bases always live inside the pack: an
// incremental pack never depends on an object outside it.
func libgit2WritePack(repo *repoHandle, objects []git.OID, w io.Writer) (int, error) {
	var pb *C.git_packbuilder
	if rc := C.git_packbuilder_new(&pb, repo.ptr); rc < 0 {
		return 0, nativeError("create pack builder")
	}
	defer C.git_packbuilder_free(pb)

	for _, oid := range objects {
		var cid C.git_oid
		C.sl_oid_from_bytes(&cid, (*C.uchar)(unsafe.Pointer(&oid[0])))
		if rc := C.git_packbuilder_insert(pb, &cid, nil); rc < 0 {
			return 0, nativeError("insert pack object")
		}
	}
	count := int(C.git_packbuilder_object_count(pb))

	var buf C.git_buf
	if rc := C.git_packbuilder_write_buf(&buf, pb); rc < 0 {
		return 0, nativeError("write pack")
	}
	defer C.git_buf_dispose(&buf)

	if buf.size > C.size_t(math.MaxInt32) {
		return 0, &git.NativeError{Op: "write pack", Message: "pack exceeds maximum supported size"}
	}
	if _, err := w.Write(C.GoBytes(unsafe.Pointer(buf.ptr), C.int(buf.size))); err != nil {
		return 0, fmt.Errorf("write pack bytes: %w", err)
	}
	return count, nil
}

// libgit2ImportPack validates and imports a complete pack into the object
// database. A truncated pack or a corrupt trailer fails without importing
// any of its objects.
func libgit2ImportPack(odb *odbHandle, data []byte) error {
	var wp *C.git_odb_writepack
	if rc := C.git_odb_write_pack(&wp, odb.ptr, nil, nil); rc < 0 {
		return nativeError("start pack import")
	}
	var (
		p  unsafe.Pointer
		rc C.int
	)
	if len(data) > 0 {
		p = unsafe.Pointer(&data[0])
	}
	if rc = C.sl_odb_writepack_append(wp, p, C.size_t(len(data))); rc >= 0 {
		rc = C.sl_odb_writepack_commit(wp)
	}
	C.sl_odb_writepack_free(wp)
	if rc < 0 {
		return nativeError("import pack")
	}
	return nil
}

// libgit2MarkShallow records a commit as a shallow history boundary: its
// parents may be absent because the checkpoint pack omits pre-checkpoint
// history. The shallow file lives in the repository git directory, exactly
// where libgit2 and Git read it; a subsequent read refreshes the in-memory
// graft table so the current session agrees with a freshly opened repository.
func libgit2MarkShallow(repo *repoHandle, oid git.OID) error {
	path := filepath.Join(C.GoString(C.git_repository_path(repo.ptr)), "shallow")
	line := oid.String() + "\n"

	switch existing, err := os.ReadFile(path); {
	case err == nil:
		if bytes.Contains(existing, []byte(line)) {
			return refreshShallow(repo)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("read shallow file: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open shallow file: %w", err)
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return fmt.Errorf("write shallow file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close shallow file: %w", err)
	}
	return refreshShallow(repo)
}

// refreshShallow reloads the shallow file into the repository graft table.
// git_repository_is_shallow is the public entry point whose implementation
// reloads grafts; its negative return is the only failure mode.
func refreshShallow(repo *repoHandle) error {
	if rc := C.git_repository_is_shallow(repo.ptr); rc < 0 {
		return nativeError("refresh shallow roots")
	}
	return nil
}

// lookupTree loads one tree object from the repository.
func lookupTree(repo *repoHandle, id git.OID) (*C.git_tree, error) {
	var (
		cid  C.git_oid
		tree *C.git_tree
	)
	C.sl_oid_from_bytes(&cid, (*C.uchar)(unsafe.Pointer(&id[0])))
	if rc := C.git_tree_lookup(&tree, repo.ptr, &cid); rc < 0 {
		return nil, nativeError("lookup tree")
	}
	return tree, nil
}

// freeCommits releases the non-nil commit handles in a parent slice.
func freeCommits(commits []*C.git_commit) {
	for _, c := range commits {
		if c != nil {
			C.git_commit_free(c)
		}
	}
}

// bytesPtr returns a C pointer to a byte slice, or nil for an empty slice.
func bytesPtr(data []byte) *C.char {
	if len(data) == 0 {
		return nil
	}
	return (*C.char)(unsafe.Pointer(&data[0]))
}

// blobMode marks a present merge side as a regular file; a nil side keeps
// mode 0 so libgit2 treats it as deleted.
func exists(data []byte) C.int {
	if data == nil {
		return 0
	}
	return 1
}

// nativeError captures the libgit2 error detail while it is still valid.
func nativeError(op string) error {
	class, message := errorLastFn()
	return &git.NativeError{Op: op, Class: class, Message: message}
}
