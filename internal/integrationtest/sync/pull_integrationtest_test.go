package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// TestScenarioPullFirstPull proves the first pull into a nonempty visible
// directory against an empty remote: OK with an empty success stat (the
// local additions are retained unchanged, so nothing changed on disk),
// local additions retained, the empty-tree generation-0 baseline recorded,
// and no remote state created (architecture/pull.md).
func TestScenarioPullFirstPull(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	path := h.WorkspaceRoot()
	h.WriteFile(path+"/local.md", "keep me")

	h.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: path,
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 0, FilesChanged: 0, Insertions: 0, Deletions: 0, Files: []FileStatExpectation{},
			},
		},
	}, h.Pull(""))
	AssertPulledMarker(t, h)
	AssertRemoteGeneration(t, h, 0)
	rec := h.StateRecord(t)
	if rec.BaselineHead != "" {
		t.Fatalf("state baselineHead = %q, want the empty remote head", rec.BaselineHead)
	}
	if rec.BaselineTree != EmptyTreeID {
		t.Fatalf("state baselineTree = %q, want the canonical empty tree %q", rec.BaselineTree, EmptyTreeID)
	}
	if got := h.FSSnapshot(path); len(got) != 1 || got["local.md"] != "keep me" {
		t.Fatalf("L after the first pull = %v, want local.md retained", got)
	}
	h.AssertExpectations(t, Expectations{
		S3: S3Assertions{
			NoCurrent: new(true),
			NoPacks:   new(true),
			Counts:    &CountExpectation{Ops: map[Op]int{OpGet: 1}},
		},
	})
}

// TestScenarioPullWarmPull proves that a pull with no local changes and an
// unchanged remote advances the baseline and reuses the cached pack bytes:
// a second pull performs no pack download (architecture/pull.md).
func TestScenarioPullWarmPull(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, h.Raw(), h.Cfg.Prefix, HarnessConfig{})
	pathB := b.WorkspaceRoot()

	CommitFirst(t, h, "a.md", "alpha", "c1")
	packKey := h.Manifest().Checkpoint.Key.String()

	// Cold pull: the empty cache downloads the checkpoint pack once.
	b.AssertOK(t, b.Pull(""))
	if got := b.Recorder().CountKey(OpGet, packKey); got != 1 {
		t.Fatalf("cold pull pack gets = %d, want 1", got)
	}
	// Warm pull: the cache hit keeps the pack gets at one.
	b.AssertOK(t, b.Pull(""))
	if got := b.Recorder().CountKey(OpGet, packKey); got != 1 {
		t.Fatalf("warm pull pack gets = %d, want 1", got)
	}
	AssertRemoteGeneration(t, b, 1)
	if got := b.FSSnapshot(pathB); got["a.md"] != "alpha" {
		t.Fatalf("L after warm pull = %v, want a.md", got)
	}
}

// TestScenarioPullAfterRemoteAdvance proves that a pull rebases local
// additions, modifications, and deletions on the remote head without
// discarding any mergeable local change (architecture/pull.md).
func TestScenarioPullAfterRemoteAdvance(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, h.Raw(), h.Cfg.Prefix, HarnessConfig{})
	pathB := b.WorkspaceRoot()

	CommitFirst(t, h, "a.md", "A1", "c1")
	CommitNext(t, h, "c.md", "C", "c2")
	b.AssertOK(t, b.Pull(""))

	// B modifies a.md, deletes c.md, and adds b.md; A pulls clean.
	b.WriteFile(pathB+"/a.md", "B edit")
	b.RemoveFile(pathB + "/c.md")
	b.WriteFile(pathB+"/b.md", "B add")
	h.AssertOK(t, h.Pull(""))

	h.AssertOK(t, b.Pull(""))
	got := b.FSSnapshot(pathB)
	if got["a.md"] != "B edit" || got["b.md"] != "B add" {
		t.Fatalf("L after the rebased pull = %v, want the local changes on top of R", got)
	}
	if _, ok := got["c.md"]; ok {
		t.Fatalf("L after the rebased pull still contains the deleted c.md")
	}
	AssertRemoteGeneration(t, b, 2)
}

// TestScenarioPullColdPull proves that a cold pull downloads only the
// missing descriptor packs and never reconstructs state by LIST
// (architecture/pull.md): the list counter stays zero.
func TestScenarioPullColdPull(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, h.Raw(), h.Cfg.Prefix, HarnessConfig{})
	pathB := b.WorkspaceRoot()

	CommitFirst(t, h, "a.md", "alpha", "c1")
	packKey := h.Manifest().Checkpoint.Key.String()
	CommitNext(t, h, "b.md", "beta", "c2")
	m := h.Manifest()
	incKey := m.Increments[0].Key.String()

	b.AssertOK(t, b.Pull(""))
	rec := b.Recorder()
	if got := rec.Count(OpList); got != 0 {
		t.Fatalf("cold pull list count = %d, want 0 (state is never reconstructed by LIST)", got)
	}
	for _, key := range []string{packKey, incKey} {
		if got := rec.CountKey(OpGet, key); got != 1 {
			t.Fatalf("cold pull gets of %s = %d, want exactly one", key, got)
		}
	}
	if got := b.FSSnapshot(pathB); got["a.md"] != "alpha" || got["b.md"] != "beta" {
		t.Fatalf("L after the cold pull = %v, want both files", got)
	}
}

// TestScenarioPullConflict proves a conflicting pull: CONTENT_CONFLICT with
// the exact relative path and marker ranges, markers materialized into L,
// R recorded as the new baseline, local-only files preserved, and L never
// reverted (architecture/pull.md).
func TestScenarioPullConflict(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, h.Raw(), h.Cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.WorkspaceRoot(), b.WorkspaceRoot()

	CommitFirst(t, h, "shared.md", "base\nline2\nline3\n", "c1")
	b.AssertOK(t, b.Pull(""))
	b.WriteFile(pathB+"/local-only.md", "mine")
	b.WriteFile(pathB+"/shared.md", "B\nline2\nline3\n")

	h.WriteFile(pathA+"/shared.md", "A\nline2\nline3\n")
	h.AssertOK(t, h.Commit("", "c2"))

	res := b.Pull("")
	b.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathB,
		Expect: CallExpectation{
			ErrorCode: "CONTENT_CONFLICT",
			Reason:    "MERGE_CONFLICT",
			Action:    "EDIT_FILES",
			Files:     []FileExpectation{{Path: "shared.md", Reason: "TEXT_CONFLICT", Ranges: []RangeExpectation{{Start: 1, End: 5}}}},
		},
	}, res)

	// L carries the exact marker grammar with the local side first and the
	// remote side second, over the reported one-based inclusive range.
	// Asserting the exact bytes is what makes the reported range meaningful.
	AssertVisibleFiles(t, b, map[string]string{
		"local-only.md": "mine",
		"shared.md": "<<<<<<< local\n" +
			"B\n" +
			"=======\n" +
			"A\n" +
			">>>>>>> remote\n" +
			"line2\nline3\n",
	})
	AssertRemoteGeneration(t, b, 2)
	AssertPulledMarker(t, b)
}

// TestScenarioPullStaleReader proves the stale-reader restart
// (architecture/pull.md): a reader blocked on a pack GET observes the pack
// deleted by a concurrent writer's checkpoint cleanup, rereads current,
// restarts, and completes with the current head. The barrier keeps the race
// deterministic over the real S3 backend.
func TestScenarioPullStaleReader(t *testing.T) {
	t.Parallel()
	a := NewHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	b := NewSharedHarness(t, a.Raw(), a.Cfg.Prefix, HarnessConfig{CheckpointPacks: new(1024)})
	pathB := b.WorkspaceRoot()

	CommitFirst(t, a, "f1.md", "v1", "c1")
	p1Key := a.Manifest().Checkpoint.Key.String()

	// B's pull blocks on the GET of P1; by then B's readCurrent already
	// observed the manifest referencing P1.
	b.Faults().BlockNext(OpGet, p1Key)
	pullCh := RunCall(t, b, "", ToolPull, "")
	b.Eventually(t, SettleTimeout, func() error {
		if !b.Faults().Waiting(OpGet, p1Key) {
			return fmt.Errorf("B is not waiting on the %s get", p1Key)
		}
		return nil
	})

	// A advances through two checkpoints; the second cleanup deletes P1.
	CommitNext(t, a, "f2.md", "v2", "c2")
	CommitNext(t, a, "f3.md", "v3", "c3")
	CommitNext(t, a, "f4.md", "v4", "c4")
	CommitNext(t, a, "f5.md", "v5", "c5")
	RequireObjectGone(t, a, p1Key)

	currentReads := b.Recorder().CountKey(OpGet, storage.CurrentKey)
	listsBefore := b.Recorder().Count(OpList)
	b.Faults().Release(OpGet, p1Key)
	b.AwaitCall(t, pullCh, ToolCall{Tool: ToolPull, Path: pathB, Expect: CallExpectation{OK: true}})

	// The restart is driven by rereading the authoritative manifest, never
	// by inferring state from the object names still in the bucket.
	if got := b.Recorder().CountKey(OpGet, storage.CurrentKey); got <= currentReads {
		t.Fatalf("current reads after the deletion = %d, want a reread that restarts the pull", got-currentReads)
	}
	if got := b.Recorder().Count(OpList) - listsBefore; got != 0 {
		t.Fatalf("LIST calls during the stale restart = %d, want none", got)
	}

	// The restarted pull lands exactly on the current head.
	AssertVisibleFiles(t, b, map[string]string{
		"f1.md": "v1", "f2.md": "v2", "f3.md": "v3", "f4.md": "v4", "f5.md": "v5",
	})
	AssertRemoteGeneration(t, b, 7)
}

// TestScenarioPullCacheCorruption proves that a corrupt cached pack is
// never a false hit: a pull that needs the pack discards the entry,
// re-downloads the verified bytes, and heals the cache
// (architecture/pull.md). The private repository is emptied and reopened
// as a new process would, because a repository that already holds the
// pack's objects never consults the cache for it.
func TestScenarioPullCacheCorruption(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, h.Raw(), h.Cfg.Prefix, HarnessConfig{})

	CommitFirst(t, h, "a.md", "alpha", "c1")
	m := h.Manifest()
	packKey := m.Checkpoint.Key.String()
	b.AssertOK(t, b.Pull(""))

	// Corrupt the cached bytes of the checkpoint pack and drop the imported
	// objects, so the next process must obtain the pack again.
	cacheFile := filepath.Join(b.PackCacheDir(), m.Checkpoint.SHA256.String())
	b.WriteFile(cacheFile, "corrupt cache entry")
	for _, f := range PackFiles(t, filepath.Join(b.PrivateDir(), "repo", ".git", "objects", "pack")) {
		if err := os.Remove(f); err != nil {
			t.Fatalf("remove an imported pack: %v", err)
		}
	}
	c := NewSharedHarness(t, h.Raw(), h.Cfg.Prefix, HarnessConfig{
		WorkspaceRoot: b.WorkspaceRoot(), PrivateRoot: b.PrivateRoot(),
	})

	c.AssertOK(t, c.Pull(""))
	if got := c.Recorder().CountKey(OpGet, packKey); got != 1 {
		t.Fatalf("pack gets after cache corruption = %d, want the one re-download", got)
	}
	wantBytes, err := c.ReadObject(packKey)
	if err != nil {
		t.Fatalf("read pack through the raw store: %v", err)
	}
	gotBytes, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("cache entry after the pull: %v", err)
	}
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("cache entry is not healed to the verified pack bytes")
	}
	AssertVisibleFiles(t, c, map[string]string{"a.md": "alpha"})
}

// TestScenarioPullFirstPullGuard proves the first-pull guard
// (architecture/pull.md): a first pull proceeds into an empty directory,
// into one whose every file is already in the notebook with identical
// bytes, and into any directory when the remote notebook is empty
// (seeding it). Into a directory holding any other file it is refused as
// INVALID_REQUEST/DIRECTORY_NOT_EMPTY/FIX_INPUT naming each offending file
// (NOT_IN_NOTEBOOK or DIFFERS_FROM_NOTEBOOK) before L, the pulled marker,
// or state.json changes. A protected file the notebook holds is restored
// rather than compared (TestScenarioReadOnlyPullRestores); one it lacks is
// refused, since the pull would delete it.
func TestScenarioPullFirstPullGuard(t *testing.T) {
	t.Parallel()
	published := map[string]string{"a.md": "alpha\n", "docs/b.md": "beta\n"}
	seed := func(t *testing.T) (*Harness, *Harness) {
		t.Helper()
		writer := NewFakeHarness(t, HarnessConfig{})
		path := writer.WorkspaceRoot()
		writer.AssertOK(t, writer.Pull(""))
		for name, data := range published {
			writer.WriteFile(path+"/"+name, data)
		}
		writer.AssertOK(t, writer.Commit("", "seed"))
		return writer, NewSharedHarness(t, writer.Raw(), writer.Cfg.Prefix, HarnessConfig{})
	}

	for _, row := range []struct {
		name  string
		local map[string]string
	}{
		{name: "empty directory", local: map[string]string{}},
		{name: "matching subset", local: map[string]string{"docs/b.md": "beta\n"}},
		{name: "matching copy", local: published},
	} {
		t.Run("proceeds into "+row.name, func(t *testing.T) {
			t.Parallel()
			_, reader := seed(t)
			path := reader.WorkspaceRoot()
			for name, data := range row.local {
				reader.WriteFile(path+"/"+name, data)
			}
			reader.AssertOK(t, reader.Pull(""))
			AssertPulledMarker(t, reader)
			AssertVisibleFiles(t, reader, published)
		})
	}

	t.Run("seeds an empty remote", func(t *testing.T) {
		t.Parallel()
		h := NewFakeHarness(t, HarnessConfig{})
		path := h.WorkspaceRoot()
		h.WriteFile(path+"/mine.md", "seed\n")
		h.AssertOK(t, h.Pull(""))
		h.AssertOK(t, h.Commit("", "seed"))
		AssertRemoteGeneration(t, h, 1)
	})

	t.Run("refuses a protected file the notebook lacks", func(t *testing.T) {
		t.Parallel()
		writer, _ := seed(t)
		agent := NewSharedHarness(t, writer.Raw(), writer.Cfg.Prefix, HarnessConfig{WritablePaths: []string{"docs"}})
		path := agent.WorkspaceRoot()
		local := map[string]string{"docs/b.md": "beta\n", "todo.md": "mine\n"}
		for name, data := range local {
			agent.WriteFile(path+"/"+name, data)
		}
		agent.AssertEnvelope(t, ToolCall{
			Tool: ToolPull, Path: path,
			Expect: CallExpectation{
				ErrorCode: "INVALID_REQUEST", Retryable: new(false),
				Reason: "DIRECTORY_NOT_EMPTY", Action: "FIX_INPUT",
				Files:    []FileExpectation{{Path: "todo.md", Reason: "NOT_IN_NOTEBOOK", Ranges: []RangeExpectation{}}},
				Writable: []string{"docs"},
			},
		}, agent.Pull(""))
		AssertVisibleFiles(t, agent, local)
	})

	for _, row := range []struct {
		name  string
		local map[string]string
		files []FileExpectation
	}{
		{
			name:  "an unrelated file",
			local: map[string]string{"a.md": "alpha\n", "other.md": "not in the notebook\n"},
			files: []FileExpectation{{Path: "other.md", Reason: "NOT_IN_NOTEBOOK", Ranges: []RangeExpectation{}}},
		},
		{
			name:  "a file with other content",
			local: map[string]string{"a.md": "my own alpha\n"},
			files: []FileExpectation{{Path: "a.md", Reason: "DIFFERS_FROM_NOTEBOOK", Ranges: []RangeExpectation{}}},
		},
		{
			name:  "both kinds",
			local: map[string]string{"a.md": "my own alpha\n", "docs/b.md": "beta\n", "z.md": "mine\n"},
			files: []FileExpectation{
				{Path: "a.md", Reason: "DIFFERS_FROM_NOTEBOOK", Ranges: []RangeExpectation{}},
				{Path: "z.md", Reason: "NOT_IN_NOTEBOOK", Ranges: []RangeExpectation{}},
			},
		},
	} {
		t.Run("refuses "+row.name, func(t *testing.T) {
			t.Parallel()
			_, reader := seed(t)
			path := reader.WorkspaceRoot()
			for name, data := range row.local {
				reader.WriteFile(path+"/"+name, data)
			}
			reader.AssertEnvelope(t, ToolCall{
				Tool: ToolPull, Path: path,
				Expect: CallExpectation{
					ErrorCode: "INVALID_REQUEST", Retryable: new(false),
					Reason: "DIRECTORY_NOT_EMPTY", Action: "FIX_INPUT",
					Files: row.files,
				},
			}, reader.Pull(""))
			AssertVisibleFiles(t, reader, row.local)
			if _, err := os.Stat(filepath.Join(reader.PrivateDir(), "pulled")); err == nil {
				t.Fatal("a refused first pull wrote the pulled marker")
			}
			if rec := reader.StateRecord(t); rec.RemoteGeneration != 0 || rec.BaselineTree != EmptyTreeID {
				t.Fatalf("state after the refusal = %+v, want the untouched generation-0 record", rec)
			}
			// The refusal leaves the directory unmanaged: commit still
			// requires a pull, and emptying the directory lets it succeed.
			reader.AssertEnvelope(t, ToolCall{
				Tool: ToolCommit, Path: path, Message: "too soon",
				Expect: CallExpectation{ErrorCode: "INVALID_REQUEST", Reason: "PULL_REQUIRED"},
			}, reader.Commit("", "too soon"))
			for name := range row.local {
				reader.RemoveFile(path + "/" + name)
			}
			reader.AssertOK(t, reader.Pull(""))
			AssertVisibleFiles(t, reader, published)
		})
	}
}
