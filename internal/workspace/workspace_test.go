package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
)

// testConfig builds a workspace config with the private root outside the
// workspace root, as the architecture requires.
func testConfig(t *testing.T, engine Engine, notes string) Config {
	t.Helper()
	root := t.TempDir()
	notesPath := filepath.Join(root, notes)
	return Config{
		WorkspaceRoot: root,
		Path:          notesPath,
		PrivateRoot:   t.TempDir(),
		Identity:      testIdentity(),
		Engine:        engine,
	}
}

func openWorkspace(t *testing.T, cfg Config) *Workspace {
	t.Helper()
	w, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// buildTree writes a snapshot into the workspace repository and returns
// its tree OID.
func buildTree(t *testing.T, w *Workspace, files map[string]string) git.OID {
	t.Helper()
	var snap git.Snapshot
	for path, data := range files {
		snap.Files = append(snap.Files, git.File{Path: path, Data: []byte(data)})
	}
	tree, err := git.BuildTree(w.Repo(), snap)
	if err != nil {
		t.Fatalf("BuildTree() = %v", err)
	}
	return tree
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v", path, err)
	}
	return data
}

func TestOpenCreatesPrivateStateAndVisibleDir(t *testing.T) {
	cfg := testConfig(t, newFakeEngine(), "notes")
	w := openWorkspace(t, cfg)

	// The private directory is derived; the visible path contains no
	// repository and no private metadata.
	if !strings.HasPrefix(w.privDir, cfg.PrivateRoot) {
		t.Fatalf("privDir %q is not below private root %q", w.privDir, cfg.PrivateRoot)
	}
	if _, err := os.Stat(filepath.Join(w.privDir, stateFileName)); err != nil {
		t.Fatalf("state.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(w.privDir, repoDirName)); err != nil {
		t.Fatalf("private repository missing: %v", err)
	}
	entries, err := os.ReadDir(cfg.Path)
	if err != nil {
		t.Fatalf("visible directory missing: %v", err)
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			t.Fatal("visible directory exposes .git")
		}
	}

	// The initial record is a generation-0 empty-tree baseline.
	st, err := readStateFile(w.privDir)
	if err != nil {
		t.Fatalf("readStateFile() = %v", err)
	}
	if st.Version != 1 || st.RemoteGeneration != 0 || st.BaselineHead != "" || st.BaselineTree != EmptyTreeID.String() || st.RecoveryRequired {
		t.Fatalf("initial state = %+v", st)
	}
	if got := w.Baseline(); got != (Baseline{RemoteGeneration: 0, Tree: EmptyTreeID}) {
		t.Fatalf("Baseline() = %+v", got)
	}
	if w.RecoveryRequired() {
		t.Fatal("fresh workspace requires recovery")
	}
	if w.Path() != cfg.Path {
		t.Fatalf("Path() = %q, want %q", w.Path(), cfg.Path)
	}
}

func TestOpenCreatesMissingVisiblePath(t *testing.T) {
	cfg := testConfig(t, newFakeEngine(), "deep/nested/notes")
	openWorkspace(t, cfg)
	if _, err := os.Stat(cfg.Path); err != nil {
		t.Fatalf("visible directory was not created: %v", err)
	}
}

func TestOpenRejectsBadConfig(t *testing.T) {
	engine := newFakeEngine()
	root := t.TempDir()
	notes := filepath.Join(root, "notes")
	priv := filepath.Join(root, "private")
	id := testIdentity()
	cases := map[string]Config{
		"relative root":         {WorkspaceRoot: "rel", Path: notes, PrivateRoot: priv, Identity: id, Engine: engine},
		"relative path":         {WorkspaceRoot: root, Path: "notes", PrivateRoot: priv, Identity: id, Engine: engine},
		"escaping path":         {WorkspaceRoot: root, Path: filepath.Join(t.TempDir(), "x"), PrivateRoot: priv, Identity: id, Engine: engine},
		"relative private root": {WorkspaceRoot: root, Path: notes, PrivateRoot: "rel", Identity: id, Engine: engine},
		"private below root":    {WorkspaceRoot: root, Path: notes, PrivateRoot: filepath.Join(root, "private"), Identity: id, Engine: engine},
		"nil engine":            {WorkspaceRoot: root, Path: notes, PrivateRoot: priv, Identity: id},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Open(context.Background(), cfg); err == nil {
				t.Fatal("Open() succeeded, want error")
			}
		})
	}
}

func TestBaselineSurvivesReopen(t *testing.T) {
	engine := newFakeEngine()
	cfg := testConfig(t, engine, "notes")
	w := openWorkspace(t, cfg)
	tree := buildTree(t, w, map[string]string{"a.md": "one", "sub/b.md": "two"})
	baseline := Baseline{RemoteGeneration: 7, Head: oidTest("c"), Tree: tree}
	if err := w.Accept(context.Background(), baseline); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	reopened, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reopen Open() = %v", err)
	}
	defer reopened.Close()
	if reopened.RecoveryRequired() {
		t.Fatal("reopened workspace requires recovery")
	}
	if got := reopened.Baseline(); got != baseline {
		t.Fatalf("reopened Baseline() = %+v, want %+v", got, baseline)
	}
	// L equals the baseline after the reopen, so no local changes exist.
	diff, err := reopened.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff() = %v", err)
	}
	if len(diff.Added)+len(diff.Modified)+len(diff.Deleted) != 0 {
		t.Fatalf("Diff() = %+v, want empty", diff)
	}
}

func TestDiffReportsLocalChanges(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	tree := buildTree(t, w, map[string]string{"a.md": "one", "b.md": "two", "sub/c.md": "three"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}

	if err := os.WriteFile(filepath.Join(w.Path(), "b.md"), []byte("changed"), 0o644); err != nil {
		t.Fatalf("modify b.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.Path(), "new.md"), []byte("new"), 0o644); err != nil {
		t.Fatalf("add new.md: %v", err)
	}
	if err := os.Remove(filepath.Join(w.Path(), "sub", "c.md")); err != nil {
		t.Fatalf("delete c.md: %v", err)
	}

	diff, err := w.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff() = %v", err)
	}
	if len(diff.Added) != 1 || diff.Added[0] != "new.md" {
		t.Fatalf("Diff().Added = %v", diff.Added)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "b.md" {
		t.Fatalf("Diff().Modified = %v", diff.Modified)
	}
	if len(diff.Deleted) != 1 || diff.Deleted[0] != "sub/c.md" {
		t.Fatalf("Diff().Deleted = %v", diff.Deleted)
	}
}

func TestFirstUseTreatsFilesAsAdditionsFromEmptyBaseline(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	if err := os.WriteFile(filepath.Join(w.Path(), "existing.md"), []byte("local"), 0o644); err != nil {
		t.Fatalf("write existing.md: %v", err)
	}
	diff, err := w.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff() = %v", err)
	}
	if len(diff.Added) != 1 || diff.Added[0] != "existing.md" {
		t.Fatalf("Diff().Added = %v, want [existing.md]", diff.Added)
	}
	snap, err := w.BaselineSnapshot(context.Background())
	if err != nil {
		t.Fatalf("BaselineSnapshot() = %v", err)
	}
	if len(snap.Files) != 0 {
		t.Fatalf("BaselineSnapshot() = %+v, want empty", snap)
	}
}

func TestAcceptMaterializesTree(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	tree := buildTree(t, w, map[string]string{
		"a.md":         "hello\r\nworld\n", // line endings preserved
		"sub/b.md":     "nested",
		"empty.txt":    "",
		"unicode/é.md": "café",
	})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), "a.md")); string(got) != "hello\r\nworld\n" {
		t.Fatalf("a.md = %q", got)
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), "sub", "b.md")); string(got) != "nested" {
		t.Fatalf("sub/b.md = %q", got)
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), "empty.txt")); len(got) != 0 {
		t.Fatalf("empty.txt = %q", got)
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), "unicode", "é.md")); string(got) != "café" {
		t.Fatalf("unicode/é.md = %q", got)
	}
}

func TestAcceptRemovesObsoleteFilesAndEmptyDirs(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	first := buildTree(t, w, map[string]string{"keep.md": "k", "old.md": "o", "gone/sub/x.md": "x", "emptydir/sub2/y.md": "y"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: first}); err != nil {
		t.Fatalf("first Accept() = %v", err)
	}
	second := buildTree(t, w, map[string]string{"keep.md": "k", "emptydir/new.md": "n"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 2, Head: oidTest("d"), Tree: second}); err != nil {
		t.Fatalf("second Accept() = %v", err)
	}
	for _, gone := range []string{"old.md", "gone", "gone/sub"} {
		if _, err := os.Stat(filepath.Join(w.Path(), filepath.FromSlash(gone))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("obsolete %q still present: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(w.Path(), "keep.md")); err != nil {
		t.Fatalf("keep.md missing: %v", err)
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), "emptydir", "new.md")); string(got) != "n" {
		t.Fatalf("emptydir/new.md = %q", got)
	}
}

func TestReplaceKeepsBaseline(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	tree := buildTree(t, w, map[string]string{"conflict.md": "conflicted"})
	baseline := Baseline{RemoteGeneration: 3, Head: oidTest("e"), Tree: EmptyTreeID}
	if err := w.Accept(context.Background(), baseline); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	if err := w.Replace(context.Background(), tree); err != nil {
		t.Fatalf("Replace() = %v", err)
	}
	if got := w.Baseline(); got != baseline {
		t.Fatalf("Baseline() = %+v, want unchanged %+v", got, baseline)
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), "conflict.md")); string(got) != "conflicted" {
		t.Fatalf("conflict.md = %q", got)
	}
	if w.RecoveryRequired() {
		t.Fatal("Replace() left the workspace requiring recovery")
	}
}

// TestMaterializeRecordsBaselineAndTree proves the pull and conflict path:
// L shows the merged tree while P records the remote state the merge
// observed, which the two trees can express only together.
func TestMaterializeRecordsBaselineAndTree(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	tree := buildTree(t, w, map[string]string{"merged.md": "merged"})
	baseline := Baseline{RemoteGeneration: 5, Head: oidTest("f"), Tree: buildTree(t, w, map[string]string{"remote.md": "remote"})}
	if err := w.Materialize(context.Background(), baseline, tree); err != nil {
		t.Fatalf("Materialize() = %v", err)
	}
	if got := w.Baseline(); got != baseline {
		t.Fatalf("Baseline() = %+v, want %+v", got, baseline)
	}
	if got := readFileBytes(t, filepath.Join(w.Path(), "merged.md")); string(got) != "merged" {
		t.Fatalf("L shows %q, want the merged tree", got)
	}
	if _, err := os.Stat(filepath.Join(w.Path(), "remote.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("L shows the baseline tree, want the merged tree: %v", err)
	}
	if w.RecoveryRequired() {
		t.Fatal("Materialize() left the workspace requiring recovery")
	}
}

// TestPulledMarkerSurvivesReopen proves the pull-first marker: a fresh
// workspace has none, MarkPulled durably records it, and a reopened
// workspace sees it.
func TestPulledMarkerSurvivesReopen(t *testing.T) {
	engine := newFakeEngine()
	cfg := testConfig(t, engine, "notes")
	w := openWorkspace(t, cfg)
	if w.Pulled() {
		t.Fatal("fresh workspace reports Pulled()")
	}
	if err := w.MarkPulled(context.Background()); err != nil {
		t.Fatalf("MarkPulled() = %v", err)
	}
	if !w.Pulled() {
		t.Fatal("MarkPulled() did not set the marker")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	reopened, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reopen Open() = %v", err)
	}
	defer reopened.Close()
	if !reopened.Pulled() {
		t.Fatal("reopened workspace lost the pulled marker")
	}
}

// TestCacheDirUnderPrivateRoot proves the pack-byte cache lives inside P.
func TestCacheDirUnderPrivateRoot(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	if got := w.CacheDir(); !strings.HasPrefix(got, w.privDir) || !strings.HasSuffix(got, string(filepath.Separator)+"pack-cache") {
		t.Fatalf("CacheDir() = %q, want <P>/pack-cache", got)
	}
}

func TestRecoveryRequiredRefusesNormalWork(t *testing.T) {
	engine := newFakeEngine()
	cfg := testConfig(t, engine, "notes")
	w := openWorkspace(t, cfg)
	tree := buildTree(t, w, map[string]string{"a.md": "a"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	// Force the recovery flag durably, as a crashed replacement does.
	st := w.state
	st.RecoveryRequired = true
	if _, err := persistState(w.privDir, w.derivedKey, st); err != nil {
		t.Fatalf("persistState() = %v", err)
	}

	reopened, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reopen Open() = %v", err)
	}
	defer reopened.Close()
	if !reopened.RecoveryRequired() {
		t.Fatal("reopened workspace does not require recovery")
	}
	if _, err := reopened.Snapshot(context.Background()); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("Snapshot() error = %v, want ErrRecoveryRequired", err)
	}
	if _, err := reopened.Diff(context.Background()); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("Diff() error = %v, want ErrRecoveryRequired", err)
	}
	if err := reopened.Accept(context.Background(), Baseline{RemoteGeneration: 2, Head: oidTest("d"), Tree: tree}); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("Accept() error = %v, want ErrRecoveryRequired", err)
	}
	if err := reopened.Replace(context.Background(), tree); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("Replace() error = %v, want ErrRecoveryRequired", err)
	}

	// Recovery reconstructs L from the supplied baseline and clears the
	// flag; the normal operations work again.
	recovered := Baseline{RemoteGeneration: 5, Head: oidTest("f"), Tree: tree}
	if err := reopened.Recover(context.Background(), recovered); err != nil {
		t.Fatalf("Recover() = %v", err)
	}
	if reopened.RecoveryRequired() {
		t.Fatal("Recover() left the recovery flag set")
	}
	if got := reopened.Baseline(); got != recovered {
		t.Fatalf("Baseline() = %+v, want %+v", got, recovered)
	}
	snap, err := reopened.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() after recovery = %v", err)
	}
	if len(snap.Files) != 1 || snap.Files[0].Path != "a.md" {
		t.Fatalf("Snapshot() after recovery = %+v", snap)
	}
}

func TestOpenCorruptStateForcesRecovery(t *testing.T) {
	engine := newFakeEngine()
	cfg := testConfig(t, engine, "notes")
	w := openWorkspace(t, cfg)
	marker := filepath.Join(w.Path(), "caller.md")
	if err := os.WriteFile(marker, []byte("caller"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.privDir, stateFileName), []byte("not json"), 0o600); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}

	reopened, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open() with corrupt state = %v", err)
	}
	defer reopened.Close()
	if !reopened.RecoveryRequired() {
		t.Fatal("corrupt state did not force recovery")
	}
	// No visible overwrite on open: the caller's file is untouched.
	if got := readFileBytes(t, marker); string(got) != "caller" {
		t.Fatalf("caller file was overwritten: %q", got)
	}
}

func TestOpenMismatchedIdentityForcesRecovery(t *testing.T) {
	engine := newFakeEngine()
	cfg := testConfig(t, engine, "notes")
	w := openWorkspace(t, cfg)
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	// Rewrite the record with a different derived key: the private
	// directory now claims a different binding.
	st := fixtureState()
	st.Identity = strings.Repeat("d", 64)
	if _, err := persistState(w.privDir, st.Identity, st); err != nil {
		t.Fatalf("persistState() = %v", err)
	}
	reopened, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open() with mismatched identity = %v", err)
	}
	defer reopened.Close()
	if !reopened.RecoveryRequired() {
		t.Fatal("mismatched identity did not force recovery")
	}
}

func TestOpenInterruptedStateWriteForcesRecovery(t *testing.T) {
	engine := newFakeEngine()
	cfg := testConfig(t, engine, "notes")
	w := openWorkspace(t, cfg)
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	// A leftover temporary file means a previous state write never
	// landed: the durable record can predate a replacement.
	if err := os.WriteFile(filepath.Join(w.privDir, stateTmpName), []byte("x"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	reopened, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open() with interrupted state write = %v", err)
	}
	defer reopened.Close()
	if !reopened.RecoveryRequired() {
		t.Fatal("interrupted state write did not force recovery")
	}
}

func TestOpenMissingRepoForcesRecovery(t *testing.T) {
	engine := newFakeEngine()
	cfg := testConfig(t, engine, "notes")
	w := openWorkspace(t, cfg)
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	// Remove the repository from the engine store, as deleting the private
	// repository directory does on a real filesystem.
	delete(engine.data, filepath.Join(w.privDir, repoDirName))
	reopened, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open() with missing repo = %v", err)
	}
	defer reopened.Close()
	if !reopened.RecoveryRequired() {
		t.Fatal("missing repository did not force recovery")
	}
	if _, err := os.Stat(filepath.Join(reopened.privDir, repoDirName)); err != nil {
		t.Fatalf("repository was not recreated: %v", err)
	}
}

// TestFailpointsEveryBoundary proves that each mutation boundary has a
// deterministic failure injection and that the observable state after each
// failure matches the contract.
func TestFailpointsEveryBoundary(t *testing.T) {
	triggered := errors.New("injected failure")
	snapshot := map[string]string{"a.md": "a", "b.md": "b"}
	newBaseline := func(tree git.OID) Baseline { return Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree} }

	t.Run("scan", func(t *testing.T) {
		w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
		w.failpoints = &Failpoints{Scan: func() error { return triggered }}
		if _, err := w.Snapshot(context.Background()); !errors.Is(err, triggered) {
			t.Fatalf("Snapshot() error = %v, want injected failure", err)
		}
		if _, err := w.Diff(context.Background()); !errors.Is(err, triggered) {
			t.Fatalf("Diff() error = %v, want injected failure", err)
		}
	})

	t.Run("stage leaves L and state unchanged", func(t *testing.T) {
		w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
		tree := buildTree(t, w, snapshot)
		marker := filepath.Join(w.Path(), "caller.md")
		if err := os.WriteFile(marker, []byte("caller"), 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		w.failpoints = &Failpoints{Stage: func() error { return triggered }}
		if err := w.Accept(context.Background(), newBaseline(tree)); !errors.Is(err, triggered) {
			t.Fatalf("Accept() error = %v, want injected failure", err)
		}
		if got := readFileBytes(t, marker); string(got) != "caller" {
			t.Fatalf("caller file changed after staged failure: %q", got)
		}
		if w.RecoveryRequired() {
			t.Fatal("staging failure must not set the recovery flag")
		}
		if st, err := readStateFile(w.privDir); err != nil || st.RecoveryRequired {
			t.Fatalf("durable state after staging failure = %+v, %v", st, err)
		}
	})

	t.Run("replace marks recovery and leaves L", func(t *testing.T) {
		w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
		tree := buildTree(t, w, snapshot)
		marker := filepath.Join(w.Path(), "caller.md")
		if err := os.WriteFile(marker, []byte("caller"), 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		w.failpoints = &Failpoints{Replace: func() error { return triggered }}
		if err := w.Accept(context.Background(), newBaseline(tree)); !errors.Is(err, ErrPartial) {
			t.Fatalf("Accept() error = %v, want ErrPartial", err)
		}
		if got := readFileBytes(t, marker); string(got) != "caller" {
			t.Fatalf("caller file changed before replacement: %q", got)
		}
		if !w.RecoveryRequired() {
			t.Fatal("replace failure must set the recovery flag")
		}
		if st, err := readStateFile(w.privDir); err != nil || !st.RecoveryRequired {
			t.Fatalf("durable state after replace failure = %+v, %v", st, err)
		}
	})

	t.Run("baseline leaves L replaced and marks recovery", func(t *testing.T) {
		w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
		tree := buildTree(t, w, snapshot)
		w.failpoints = &Failpoints{Baseline: func() error { return triggered }}
		if err := w.Accept(context.Background(), newBaseline(tree)); !errors.Is(err, ErrPartial) {
			t.Fatalf("Accept() error = %v, want ErrPartial", err)
		}
		if got := readFileBytes(t, filepath.Join(w.Path(), "a.md")); string(got) != "a" {
			t.Fatalf("L was not replaced before the baseline failure: %q", got)
		}
		if !w.RecoveryRequired() {
			t.Fatal("baseline failure must set the recovery flag")
		}
	})

	t.Run("recover", func(t *testing.T) {
		w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
		w.state.RecoveryRequired = true
		w.failpoints = &Failpoints{Recover: func() error { return triggered }}
		tree := buildTree(t, w, snapshot)
		if err := w.Recover(context.Background(), newBaseline(tree)); !errors.Is(err, triggered) {
			t.Fatalf("Recover() error = %v, want injected failure", err)
		}
	})
}

// TestSamePathOperationsSerialize proves that two operations on one path
// never overlap, even when the failpoint barrier allows it.
func TestSamePathOperationsSerialize(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	tree := buildTree(t, w, map[string]string{"a.md": "a"})

	entered := make(chan struct{})
	release := make(chan struct{})
	w.failpoints = &Failpoints{Replace: func() error {
		close(entered)
		<-release
		return nil
	}}

	var wg sync.WaitGroup
	wg.Go(func() {
		w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree})
	})
	<-entered // the first operation holds the operation lock inside Replace

	blocked := make(chan error, 1)
	go func() {
		_, err := w.Snapshot(context.Background())
		blocked <- err
	}()
	select {
	case err := <-blocked:
		t.Fatalf("second operation completed while the first held the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("Snapshot() after release = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second operation did not resume after the lock release")
	}
}

// TestDifferentPathsOperateConcurrently proves that workspaces on
// different visible paths never share a lock.
func TestDifferentPathsOperateConcurrently(t *testing.T) {
	engine := newFakeEngine()
	w1 := openWorkspace(t, testConfig(t, engine, "notes-a"))
	w2 := openWorkspace(t, testConfig(t, engine, "notes-b"))
	tree1 := buildTree(t, w1, map[string]string{"a.md": "a"})
	tree2 := buildTree(t, w2, map[string]string{"b.md": "b"})

	entered := make(chan struct{}, 2)
	both := make(chan struct{})
	hook := func() error {
		entered <- struct{}{}
		<-both
		return nil
	}
	w1.failpoints = &Failpoints{Replace: hook}
	w2.failpoints = &Failpoints{Replace: hook}

	var wg sync.WaitGroup
	for _, op := range []func() error{
		func() error {
			return w1.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree1})
		},
		func() error {
			return w2.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree2})
		},
	} {
		wg.Add(1)
		go func(fn func() error) {
			defer wg.Done()
			if err := fn(); err != nil {
				t.Errorf("concurrent Accept() = %v", err)
			}
		}(op)
	}
	// Both operations must reach the Replace boundary before either
	// releases: different paths never serialize on one another.
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("operations on different paths did not run concurrently")
		}
	}
	close(both)
	wg.Wait()
}

func TestContextCancellationWhileWaitingForLock(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	tree := buildTree(t, w, map[string]string{"a.md": "a"})

	entered := make(chan struct{})
	release := make(chan struct{})
	w.failpoints = &Failpoints{Replace: func() error {
		close(entered)
		<-release
		return nil
	}}
	var wg sync.WaitGroup
	wg.Go(func() {
		w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree})
	})
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := w.Replace(ctx, tree); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Replace() with canceled context = %v, want deadline exceeded", err)
	}
	close(release)
	wg.Wait()
	// The canceled attempt must not have leaked the lock.
	if _, err := w.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() after cancellation = %v", err)
	}
}

// oidTest parses a deterministic OID from a short hex string.
func oidTest(hex string) git.OID {
	id, err := git.ParseOID(strings.Repeat("0", 40-len(hex)) + hex)
	if err != nil {
		panic(err)
	}
	return id
}
