package notebook

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git/gittest"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// fakeEngine is an in-memory Engine for pure notebook tests: it keeps one
// fakeData store per private repository path, so a reopened workspace sees
// the objects it persisted before the close, exactly like objects on disk.
// The fake repository is complete — commits, merges, and packs — so two
// workspaces can transfer objects exactly like real repositories.
type fakeEngine struct {
	data map[string]*fakeData
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{data: map[string]*fakeData{}}
}

func (e *fakeEngine) CreateRepo(path string) (git.Repository, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, err
	}
	d := newFakeData()
	e.data[path] = d
	return &fakeRepository{data: d}, nil
}

func (e *fakeEngine) OpenRepo(path string) (git.Repository, error) {
	d, ok := e.data[path]
	if !ok {
		return nil, fmt.Errorf("fake: repository %q not found", path)
	}
	return &fakeRepository{data: d}, nil
}

var _ workspace.Engine = (*fakeEngine)(nil)

// fakeData is the object store shared by every handle of one repository:
// parsed objects for reads plus the raw serialized bytes packs export.
type fakeData struct {
	blobs   map[git.OID][]byte
	trees   map[git.OID][]git.TreeEntry
	commits map[git.OID]git.Commit
	raw     map[git.OID][]byte
	shallow []git.OID
}

func newFakeData() *fakeData {
	return &fakeData{
		blobs:   map[git.OID][]byte{},
		trees:   map[git.OID][]git.TreeEntry{},
		commits: map[git.OID]git.Commit{},
		raw:     map[git.OID][]byte{},
	}
}

// fakeRepository is a complete in-memory Repository: deterministic OIDs,
// a real file-level three-tree merge, exact conflict markers, and a
// self-contained binary pack format so two fake repositories transfer
// objects exactly like real packs. Close marks one handle closed; the
// underlying data survives, so a reopened workspace reads the persisted
// objects.
type fakeRepository struct {
	data   *fakeData
	closed bool
}

var _ git.Repository = (*fakeRepository)(nil)

func (f *fakeRepository) WriteBlob(data []byte) (git.OID, error) {
	if f.closed {
		return git.OID{}, errors.New("fake: repository closed")
	}
	oid := gittest.ObjectID("blob", data)
	f.data.blobs[oid] = append([]byte(nil), data...)
	f.data.raw[oid] = append([]byte(nil), data...)
	return oid, nil
}

func (f *fakeRepository) ReadBlob(id git.OID) ([]byte, error) {
	data, ok := f.data.blobs[id]
	if !ok {
		return nil, fmt.Errorf("fake: blob %s not found", id)
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeRepository) WriteTree(entries []git.TreeEntry) (git.OID, error) {
	if f.closed {
		return git.OID{}, errors.New("fake: repository closed")
	}
	sorted := append([]git.TreeEntry(nil), entries...)
	git.SortTreeEntries(sorted)
	raw := serializeTreeEntries(sorted)
	oid := gittest.ObjectID("tree", raw)
	f.data.trees[oid] = sorted
	f.data.raw[oid] = raw
	return oid, nil
}

func (f *fakeRepository) ReadTree(id git.OID) ([]git.TreeEntry, error) {
	entries, ok := f.data.trees[id]
	if !ok {
		return nil, fmt.Errorf("fake: tree %s not found", id)
	}
	return entries, nil
}

func (f *fakeRepository) CreateCommit(spec git.CommitSpec) (git.OID, error) {
	if f.closed {
		return git.OID{}, errors.New("fake: repository closed")
	}
	raw := serializeCommit(spec)
	oid := gittest.ObjectID("commit", raw)
	f.data.commits[oid] = git.Commit{
		Tree:    spec.Tree,
		Parents: append([]git.OID(nil), spec.Parents...),
		Message: spec.Message,
	}
	f.data.raw[oid] = raw
	return oid, nil
}

func (f *fakeRepository) ReadCommit(id git.OID) (git.Commit, error) {
	commit, ok := f.data.commits[id]
	if !ok {
		return git.Commit{}, fmt.Errorf("fake: commit %s not found", id)
	}
	// libgit2 grafts declared shallow roots to zero parents once the
	// shallow file is loaded (internal/git2/native.go); the fake must
	// mirror that so history walks stop at the checkpoint boundary.
	if slices.Contains(f.data.shallow, id) {
		commit.Parents = nil
	}
	return commit, nil
}

// MergeTrees performs a real file-level three-tree merge: identical sides
// resolve, one-sided changes win, and differing two-sided changes become
// index conflicts with stages 1/2/3. A file-versus-directory replacement
// keeps the file side's stage at the path and the directory side's content
// below it (local stage 2, remote stage 0), mirroring the index shapes the
// policy structures. A conflict-free merge builds its tree from the
// resolved stage-0 blobs.
func (f *fakeRepository) MergeTrees(base, local, remote git.OID) (git.MergeIndex, error) {
	if f.closed {
		return git.MergeIndex{}, errors.New("fake: repository closed")
	}
	bm, bd, err := f.treeMap(base)
	if err != nil {
		return git.MergeIndex{}, err
	}
	lm, ld, err := f.treeMap(local)
	if err != nil {
		return git.MergeIndex{}, err
	}
	rm, rd, err := f.treeMap(remote)
	if err != nil {
		return git.MergeIndex{}, err
	}

	paths := make(map[string]bool, len(bm)+len(lm)+len(rm))
	for p := range bm {
		paths[p] = true
	}
	for p := range lm {
		paths[p] = true
	}
	for p := range rm {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	dfAny := map[string]bool{}   // file-versus-directory conflict paths
	var entries []git.IndexEntry // resolved and conflicted entries
	conflicted := false

	for _, p := range sorted {
		b, hasB := bm[p]
		l, hasL := lm[p]
		r, hasR := rm[p]

		if (hasB || hasL || hasR) && (bd[p] || ld[p] || rd[p]) {
			// File-versus-directory replacement: the file side stays a
			// conflicted entry at its own stage.
			conflicted = true
			dfAny[p] = true
			switch {
			case hasL:
				entries = append(entries, git.IndexEntry{Path: p, Mode: l.Mode, ID: l.ID, Stage: 2})
			case hasR:
				entries = append(entries, git.IndexEntry{Path: p, Mode: r.Mode, ID: r.ID, Stage: 3})
			default:
				entries = append(entries, git.IndexEntry{Path: p, Mode: b.Mode, ID: b.ID, Stage: 1})
			}
			continue
		}
		if belowDirFileConflict(p, dfAny) {
			// Directory content below a file-versus-directory conflict:
			// local entries keep stage 2 so they survive materialization,
			// remote entries resolve cleanly and are omitted below the
			// conflicted path.
			if hasL {
				entries = append(entries, git.IndexEntry{Path: p, Mode: l.Mode, ID: l.ID, Stage: 2})
			} else if hasR {
				entries = append(entries, git.IndexEntry{Path: p, Mode: r.Mode, ID: r.ID, Stage: 0})
			}
			continue
		}
		if !hasB && !hasL && !hasR {
			continue // a pure directory path; its content is handled below
		}

		lUnchanged := hasL == hasB && (!hasL || (l.ID == b.ID && l.Mode == b.Mode))
		rUnchanged := hasR == hasB && (!hasR || (r.ID == b.ID && r.Mode == b.Mode))
		lrSame := (!hasL && !hasR) || (hasL && hasR && l.ID == r.ID && l.Mode == r.Mode)
		switch {
		case lrSame:
			if hasL {
				entries = append(entries, git.IndexEntry{Path: p, Mode: l.Mode, ID: l.ID, Stage: 0})
			}
			// both sides deleted: a clean deletion leaves no entry
		case lUnchanged:
			if hasR {
				entries = append(entries, git.IndexEntry{Path: p, Mode: r.Mode, ID: r.ID, Stage: 0})
			}
		case rUnchanged:
			if hasL {
				entries = append(entries, git.IndexEntry{Path: p, Mode: l.Mode, ID: l.ID, Stage: 0})
			}
		default:
			conflicted = true
			if hasB {
				entries = append(entries, git.IndexEntry{Path: p, Mode: b.Mode, ID: b.ID, Stage: 1})
			}
			if hasL {
				entries = append(entries, git.IndexEntry{Path: p, Mode: l.Mode, ID: l.ID, Stage: 2})
			}
			if hasR {
				entries = append(entries, git.IndexEntry{Path: p, Mode: r.Mode, ID: r.ID, Stage: 3})
			}
		}
	}

	if conflicted {
		return git.MergeIndex{Entries: entries}, nil
	}
	snap := git.Snapshot{}
	for _, e := range entries {
		data, err := f.ReadBlob(e.ID)
		if err != nil {
			return git.MergeIndex{}, err
		}
		snap.Files = append(snap.Files, git.File{Path: e.Path, Data: data})
	}
	tree, err := git.BuildTree(f, snap)
	if err != nil {
		return git.MergeIndex{}, fmt.Errorf("fake: merge tree: %w", err)
	}
	return git.MergeIndex{Tree: tree, Entries: entries}, nil
}

// MergeFile performs a file-level three-way merge. Identical content
// automerges; differing local and remote sides produce a conflict with the
// exact marker grammar and the full-file sides between the markers.
func (f *fakeRepository) MergeFile(base, local, remote []byte) (git.MergeFileResult, error) {
	switch {
	case bytes.Equal(base, local):
		return git.MergeFileResult{Content: remote, Automergeable: true}, nil
	case bytes.Equal(base, remote), bytes.Equal(local, remote):
		return git.MergeFileResult{Content: local, Automergeable: true}, nil
	default:
		return git.MergeFileResult{Content: formatConflictMarkers(local, remote), Automergeable: false}, nil
	}
}

// packMagic identifies the fake pack format; the format is a magic header,
// an object count, and one kind/oid/size/body record per object, so a pack
// written by one fake repository imports into another exactly like a real
// pack. Import verifies every object OID against its content before any
// object lands, so a corrupt pack is rejected atomically.
const packMagic = "SLIVFAKE1"

const (
	kindBlob   = 1
	kindTree   = 2
	kindCommit = 3
)

func (f *fakeRepository) WritePack(objects []git.OID, w io.Writer) (int, error) {
	if f.closed {
		return 0, errors.New("fake: repository closed")
	}
	sorted := append([]git.OID(nil), objects...)
	sort.Slice(sorted, func(i, j int) bool { return bytes.Compare(sorted[i][:], sorted[j][:]) < 0 })
	if _, err := io.WriteString(w, packMagic); err != nil {
		return 0, err
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(sorted)))
	if _, err := w.Write(header[:]); err != nil {
		return 0, err
	}
	for _, oid := range sorted {
		raw, kind, err := f.objectBytes(oid)
		if err != nil {
			return 0, err
		}
		buf := []byte{kind}
		buf = append(buf, oid[:]...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(raw)))
		buf = append(buf, size[:]...)
		buf = append(buf, raw...)
		if _, err := w.Write(buf); err != nil {
			return 0, err
		}
	}
	return len(sorted), nil
}

func (f *fakeRepository) ImportPack(data []byte) error {
	if f.closed {
		return errors.New("fake: repository closed")
	}
	if !bytes.HasPrefix(data, []byte(packMagic)) {
		return errors.New("fake: not a slivingdoc fake pack")
	}
	rest := data[len(packMagic):]
	if len(rest) < 4 {
		return errors.New("fake: truncated pack header")
	}
	count := binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]

	blobs := map[git.OID][]byte{}
	trees := map[git.OID][]git.TreeEntry{}
	commits := map[git.OID]git.Commit{}
	raw := map[git.OID][]byte{}

	for range count {
		if len(rest) < 25 {
			return errors.New("fake: truncated pack object header")
		}
		kind := rest[0]
		var oid git.OID
		copy(oid[:], rest[1:21])
		size := binary.BigEndian.Uint32(rest[21:25])
		rest = rest[25:]
		if uint64(len(rest)) < uint64(size) {
			return errors.New("fake: truncated pack object body")
		}
		body := rest[:size]
		rest = rest[size:]

		kindName, ok := map[byte]string{kindBlob: "blob", kindTree: "tree", kindCommit: "commit"}[kind]
		if !ok {
			return fmt.Errorf("fake: unknown pack object kind %d", kind)
		}
		if want := gittest.ObjectID(kindName, body); want != oid {
			return fmt.Errorf("fake: pack object %s does not match its content (%s)", oid, want)
		}
		raw[oid] = append([]byte(nil), body...)
		switch kind {
		case kindBlob:
			blobs[oid] = append([]byte(nil), body...)
		case kindTree:
			entries, err := parseTreeEntries(body)
			if err != nil {
				return err
			}
			trees[oid] = entries
		case kindCommit:
			commit, err := parseCommit(body)
			if err != nil {
				return err
			}
			commits[oid] = commit
		}
	}
	// Atomic: the whole pack parsed before any object lands.
	maps.Copy(f.data.blobs, blobs)
	maps.Copy(f.data.trees, trees)
	maps.Copy(f.data.commits, commits)
	maps.Copy(f.data.raw, raw)
	return nil
}

func (f *fakeRepository) MarkShallow(oid git.OID) error {
	if f.closed {
		return errors.New("fake: repository closed")
	}
	f.data.shallow = append(f.data.shallow, oid)
	return nil
}

func (f *fakeRepository) Close() error {
	f.closed = true
	return nil
}

// objectBytes returns the raw serialized bytes and the pack kind byte of
// one object.
func (f *fakeRepository) objectBytes(oid git.OID) ([]byte, byte, error) {
	raw, ok := f.data.raw[oid]
	if !ok {
		return nil, 0, fmt.Errorf("fake: object %s not found", oid)
	}
	kind := byte(kindBlob)
	if _, ok := f.data.trees[oid]; ok {
		kind = kindTree
	}
	if _, ok := f.data.commits[oid]; ok {
		kind = kindCommit
	}
	return raw, kind, nil
}

// treeMap walks a tree and returns its blob entries by path and its
// directory paths. A missing tree is an error, exactly like the native
// boundary.
func (f *fakeRepository) treeMap(tree git.OID) (map[string]git.TreeEntry, map[string]bool, error) {
	blobs := map[string]git.TreeEntry{}
	dirs := map[string]bool{}
	var walk func(id git.OID, prefix string) error
	walk = func(id git.OID, prefix string) error {
		entries, err := f.ReadTree(id)
		if err != nil {
			return err
		}
		for _, e := range entries {
			path := prefix + e.Name
			if e.Mode == git.ModeTree {
				dirs[path] = true
				if err := walk(e.ID, path+"/"); err != nil {
					return err
				}
			} else {
				blobs[path] = e
			}
		}
		return nil
	}
	if err := walk(tree, ""); err != nil {
		return nil, nil, fmt.Errorf("fake: merge tree map: %w", err)
	}
	return blobs, dirs, nil
}

// serializeTreeEntries renders tree entries in Git tree order with the
// same serialization as the native boundary: "%o name\0" followed by the
// 20-byte object ID.
func serializeTreeEntries(entries []git.TreeEntry) []byte {
	var b bytes.Buffer
	for _, e := range entries {
		fmt.Fprintf(&b, "%o %s\x00", e.Mode, e.Name)
		b.Write(e.ID[:])
	}
	return b.Bytes()
}

// parseTreeEntries parses the serialized tree-entry format.
func parseTreeEntries(data []byte) ([]git.TreeEntry, error) {
	var entries []git.TreeEntry
	for len(data) > 0 {
		idx := bytes.IndexByte(data, 0)
		if idx < 0 {
			return nil, errors.New("fake: malformed tree entry")
		}
		seg := string(data[:idx])
		data = data[idx+1:]
		if len(data) < 20 {
			return nil, errors.New("fake: malformed tree entry id")
		}
		before, after, ok := strings.Cut(seg, " ")
		if !ok {
			return nil, errors.New("fake: malformed tree entry mode")
		}
		mode, err := strconv.ParseUint(before, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("fake: malformed tree entry mode: %w", err)
		}
		var id git.OID
		copy(id[:], data[:20])
		data = data[20:]
		entries = append(entries, git.TreeEntry{Name: after, Mode: git.FileMode(mode), ID: id})
	}
	return entries, nil
}

// serializeCommit renders a commit exactly as the deterministic fake OID
// hashes it: tree, parents, fixed identity, the spec time in UTC with
// offset zero, and the message.
func serializeCommit(spec git.CommitSpec) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "tree %s\n", spec.Tree)
	for _, p := range spec.Parents {
		fmt.Fprintf(&b, "parent %s\n", p)
	}
	fmt.Fprintf(&b, "author %s <%s> %d +0000\n", git.AuthorName, git.AuthorEmail, spec.Time.Unix())
	fmt.Fprintf(&b, "committer %s <%s> %d +0000\n\n", git.AuthorName, git.AuthorEmail, spec.Time.Unix())
	b.WriteString(spec.Message)
	return b.Bytes()
}

// parseCommit parses the serialized commit format into the Go-facing
// commit view.
func parseCommit(data []byte) (git.Commit, error) {
	head, message, ok := bytes.Cut(data, []byte("\n\n"))
	if !ok {
		return git.Commit{}, errors.New("fake: malformed commit: missing message separator")
	}
	var c git.Commit
	c.Message = string(message)
	for line := range bytes.SplitSeq(head, []byte("\n")) {
		switch {
		case bytes.HasPrefix(line, []byte("tree ")):
			oid, err := git.ParseOID(string(line[5:]))
			if err != nil {
				return git.Commit{}, fmt.Errorf("fake: malformed commit tree: %w", err)
			}
			c.Tree = oid
		case bytes.HasPrefix(line, []byte("parent ")):
			oid, err := git.ParseOID(string(line[7:]))
			if err != nil {
				return git.Commit{}, fmt.Errorf("fake: malformed commit parent: %w", err)
			}
			c.Parents = append(c.Parents, oid)
		}
	}
	if c.Tree.IsZero() {
		return git.Commit{}, errors.New("fake: malformed commit: missing tree")
	}
	return c, nil
}

// formatConflictMarkers renders a text conflict with the exact marker
// grammar of architecture section 12 and the full local and remote sides
// between the markers.
func formatConflictMarkers(local, remote []byte) []byte {
	var b bytes.Buffer
	b.WriteString("<<<<<<< local\n")
	b.Write(local)
	if len(local) == 0 || local[len(local)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("=======\n")
	b.Write(remote)
	if len(remote) == 0 || remote[len(remote)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString(">>>>>>> remote\n")
	return b.Bytes()
}

// belowDirFileConflict reports whether path lies below a file-versus-
// directory conflict path, matching the policy's materialization rule.
func belowDirFileConflict(path string, dfPaths map[string]bool) bool {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' && dfPaths[path[:i]] {
			return true
		}
	}
	return false
}

// testIDSource is a concurrency-safe deterministic source of UUIDv7
// protocol IDs. The values are unique per call within one test and valid
// version-7 RFC 4122 UUIDs, so manifest round trips succeed.
type testIDSource struct {
	mu sync.Mutex
	n  uint64
}

func (s *testIDSource) next() (storage.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return testUUIDv7(s.n), nil
}

// testUUIDv7 builds a deterministic RFC 9562 version-7 UUID with the RFC
// 4122 variant from a 48-bit counter value.
func testUUIDv7(n uint64) storage.UUID {
	var u storage.UUID
	u[0] = byte(n >> 40)
	u[1] = byte(n >> 32)
	u[2] = byte(n >> 24)
	u[3] = byte(n >> 16)
	u[4] = byte(n >> 8)
	u[5] = byte(n)
	u[6] = 0x70 // version 7, zero rand_a
	u[8] = 0x80 // RFC 4122 variant, zero rand_b
	return u
}

// testNow is the deterministic attempt clock of the notebook tests.
var testNow = time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

// waiterFunc adapts a function to the BackoffWaiter interface.
type waiterFunc func(ctx context.Context, attempt int) error

func (f waiterFunc) Wait(ctx context.Context, attempt int) error { return f(ctx, attempt) }

// noSleepWaiter makes CAS retries instant so bounds are exact.
func noSleepWaiter() waiterFunc {
	return func(context.Context, int) error { return nil }
}

func testIdentity() workspace.Identity {
	return workspace.Identity{
		Endpoint:        "https://s3.example.com",
		Region:          "eu-central-1",
		Bucket:          "notes",
		Prefix:          "slivingdoc",
		ManifestVersion: workspace.ManifestVersion,
	}
}

// nbConfig wires one notebook harness. A zero retryLimit resolves to
// DefaultRetryLimit; a zero checkpointPacks resolves to
// DefaultCheckpointPacks; retained resolves to DefaultRetainedCheckpoints
// unless retainedSet is true (a test may explicitly configure 0); a nil
// engine resolves to a fresh fake engine.
type nbConfig struct {
	store           storage.ObjectStore
	engine          workspace.Engine
	ids             *testIDSource
	retryLimit      int
	checkpointPacks int
	retained        int
	retainedSet     bool
	wsFail          *workspace.Failpoints
	nbFail          *Failpoints
}

// newNotebook builds one real workspace over the engine and one notebook
// over the store with deterministic IDs, time, and waiter. It returns the
// notebook, the workspace, and the workspace config so a test can reopen
// the same workspace (for entry recovery) with different failpoints.
// It accepts testing.TB so the load benchmarks reuse the exact harness.
func newNotebook(tb testing.TB, cfg nbConfig) (*Notebook, *workspace.Workspace, workspace.Config) {
	tb.Helper()
	if cfg.store == nil {
		tb.Fatal("newNotebook: store is required")
	}
	if cfg.ids == nil {
		tb.Fatal("newNotebook: id source is required")
	}
	retryLimit := cfg.retryLimit
	if retryLimit == 0 {
		retryLimit = DefaultRetryLimit
	}
	checkpointPacks := cfg.checkpointPacks
	if checkpointPacks == 0 {
		checkpointPacks = DefaultCheckpointPacks
	}
	retained := cfg.retained
	if !cfg.retainedSet {
		retained = DefaultRetainedCheckpoints
	}
	engine := cfg.engine
	if engine == nil {
		engine = newFakeEngine()
	}
	root := tb.TempDir()
	wcfg := workspace.Config{
		WorkspaceRoot: root,
		Path:          filepath.Join(root, "notes"),
		PrivateRoot:   tb.TempDir(),
		Identity:      testIdentity(),
		Engine:        engine,
		Failpoints:    cfg.wsFail,
	}
	w, err := workspace.Open(context.Background(), wcfg)
	if err != nil {
		tb.Fatalf("workspace.Open() = %v", err)
	}
	tb.Cleanup(func() { _ = w.Close() })
	nb, err := New(Config{
		Workspace:           w,
		Store:               cfg.store,
		RetryLimit:          retryLimit,
		CheckpointPacks:     checkpointPacks,
		RetainedCheckpoints: retained,
		NewID:               cfg.ids.next,
		Now:                 func() time.Time { return testNow },
		Waiter:              noSleepWaiter(),
		Failpoints:          cfg.nbFail,
	})
	if err != nil {
		tb.Fatalf("New() = %v", err)
	}
	return nb, w, wcfg
}

// writeLocal writes files into the visible directory.
func writeLocal(tb testing.TB, w *workspace.Workspace, files map[string]string) {
	tb.Helper()
	for path, data := range files {
		host := filepath.Join(w.Path(), filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
			tb.Fatalf("MkdirAll(%q) = %v", host, err)
		}
		if err := os.WriteFile(host, []byte(data), 0o644); err != nil {
			tb.Fatalf("WriteFile(%q) = %v", host, err)
		}
	}
}

// removeLocal deletes one visible file.
func removeLocal(t *testing.T, w *workspace.Workspace, path string) {
	t.Helper()
	if err := os.Remove(filepath.Join(w.Path(), filepath.FromSlash(path))); err != nil {
		t.Fatalf("Remove(%q) = %v", path, err)
	}
}

// readLocal reads one visible file.
func readLocal(t *testing.T, w *workspace.Workspace, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(w.Path(), filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v", path, err)
	}
	return string(data)
}

// localSnapshot reads the complete visible directory through the workspace
// scan, so the result is the normalized notebook state.
func localSnapshot(tb testing.TB, w *workspace.Workspace) map[string]string {
	tb.Helper()
	snap, err := w.Snapshot(context.Background())
	if err != nil {
		tb.Fatalf("Snapshot() = %v", err)
	}
	out := make(map[string]string, len(snap.Files))
	for _, f := range snap.Files {
		out[f.Path] = string(f.Data)
	}
	return out
}

// readManifest reads and decodes the authoritative current manifest.
func readManifest(tb testing.TB, store storage.ObjectStore) storage.Manifest {
	tb.Helper()
	rc, _, err := store.ReadObject(context.Background(), storage.CurrentKey)
	if err != nil {
		tb.Fatalf("read current = %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		tb.Fatalf("read current body = %v", err)
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		tb.Fatalf("DecodeManifest() = %v", err)
	}
	return m
}

// assertErrorCode fails the test when err is not a notebook error with the
// given code, and returns the error for report assertions.
func assertErrorCode(t *testing.T, err error, code Code) *Error {
	t.Helper()
	var ne *Error
	if !errors.As(err, &ne) {
		t.Fatalf("error = %v, want notebook error %s", err, code)
	}
	if ne.Code != code {
		t.Fatalf("error code = %s, want %s (error: %v)", ne.Code, code, err)
	}
	return ne
}

// pullOK and commitOK run the notebook operations and fail on error.
func pullOK(tb testing.TB, nb *Notebook) {
	tb.Helper()
	if err := nb.Pull(context.Background()); err != nil {
		tb.Fatalf("Pull() = %v", err)
	}
}

func commitOK(tb testing.TB, nb *Notebook, message string) {
	tb.Helper()
	if err := nb.Commit(context.Background(), message); err != nil {
		tb.Fatalf("Commit(%q) = %v", message, err)
	}
}

// casGateStore blocks the first manifest CAS (create or replace) until the
// test releases it, so tests can observe the state at the acceptance
// boundary.
type casGateStore struct {
	storage.ObjectStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *casGateStore) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.ObjectStore.CreateObject(ctx, key, data)
}

func (s *casGateStore) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.ObjectStore.ReplaceObject(ctx, key, etag, data)
}

// flakyStore fails every manifest replacement with a fixed error and
// counts the attempts.
type flakyStore struct {
	*fake.Store
	failReplace  error
	replaceCalls int
}

func (s *flakyStore) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	s.replaceCalls++
	if s.failReplace != nil {
		return "", s.failReplace
	}
	return s.Store.ReplaceObject(ctx, key, etag, data)
}

// restartStore makes the first pack read report ErrNotFound while restoring
// the pack and bumping the current ETag in the same call, so a stale
// manifest observation restarts and then succeeds on the fresh read.
type restartStore struct {
	*fake.Store
	key  string
	data []byte
	done bool
}

func (s *restartStore) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	if key == s.key && !s.done {
		s.done = true
		if err := s.PutObject(ctx, s.key, bytes.NewReader(s.data), storage.Metadata{}); err != nil {
			return nil, storage.ObjectInfo{}, err
		}
		s.Bump(storage.CurrentKey)
		return nil, storage.ObjectInfo{}, storage.ErrNotFound
	}
	return s.Store.ReadObject(ctx, key)
}
