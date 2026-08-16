package integrationtest

import (
	"testing"
)

// TestScenarioPullDeltaStat proves the pull success envelope carries the
// on-disk delta: the diffstat between the visible state the pull observed
// and the materialized result (architecture section 2 diff semantics).
// A remote advance plus a local edit yields exactly the changed paths with
// their line counts, and untouched files contribute no entry.
func TestScenarioPullDeltaStat(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathA, "a.md", "A1\n", "c1")
	h.WriteFile(pathA+"/b.md", "B1\n")
	h.assertOK(t, h.Commit("", pathA, "c2"))
	b.assertOK(t, b.Pull("", pathB))

	// A advances the remote: a.md changes and c.md appears, b.md stays.
	h.WriteFile(pathA+"/a.md", "A2\n")
	h.WriteFile(pathA+"/c.md", "C\n")
	h.assertOK(t, h.Commit("", pathA, "c3"))

	// B edits b.md locally before pulling: the pull merges the remote
	// change with the local edit and reports the on-disk delta.
	b.WriteFile(pathB+"/b.md", "B2\n")
	res := b.Pull("", pathB)
	b.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathB,
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation:   3,
				FilesChanged: 2,
				Insertions:   2,
				Deletions:    1,
				Files: []FileStatExpectation{
					{Path: "a.md", Insertions: 1, Deletions: 1},
					{Path: "c.md", Insertions: 1, Deletions: 0},
				},
			},
		},
	}, res)

	// The reported delta is exactly the before/after materialized state.
	assertVisibleFiles(t, b, pathB, map[string]string{
		"a.md": "A2\n", "b.md": "B2\n", "c.md": "C\n",
	})
	assertRemoteGeneration(t, b, pathB, 3)
}

// TestScenarioCommitIncrementStat proves the commit success envelope
// carries the published increment: the diffstat between the observed
// remote parent tree and the accepted merged tree (architecture section 2
// diff semantics). The first publication reports every file as new; a
// later increment reports exactly the paths the commit changed.
func TestScenarioCommitIncrementStat(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	h.assertOK(t, h.Pull("", path))

	h.WriteFile(path+"/a.md", "alpha\n")
	h.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: path, Message: "c1",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation:   1,
				FilesChanged: 1,
				Insertions:   1,
				Deletions:    0,
				Files:        []FileStatExpectation{{Path: "a.md", Insertions: 1, Deletions: 0}},
			},
		},
	}, h.Commit("", path, "c1"))

	h.WriteFile(path+"/a.md", "changed\n")
	h.WriteFile(path+"/b.md", "beta\n")
	h.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: path, Message: "c2",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation:   2,
				FilesChanged: 2,
				Insertions:   2,
				Deletions:    1,
				Files: []FileStatExpectation{
					{Path: "a.md", Insertions: 1, Deletions: 1},
					{Path: "b.md", Insertions: 1, Deletions: 0},
				},
			},
		},
	}, h.Commit("", path, "c2"))

	assertRemoteGeneration(t, h, path, 2)
}
