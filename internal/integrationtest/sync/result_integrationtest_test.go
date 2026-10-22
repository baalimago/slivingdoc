package sync

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioPullMaterializesThenListsTheDeltaInFull: a first pull reports a bounded outline, a later one every changed file.
func TestScenarioPullMaterializesThenListsTheDeltaInFull(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, h.Raw(), h.Cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.WorkspaceRoot(), b.WorkspaceRoot()

	h.AssertOK(t, h.Pull(""))
	h.WriteFile(pathA+"/CONTEXT.md", "one\ntwo\n")
	for i := range 150 {
		h.WriteFile(fmt.Sprintf("%s/log/f%03d.md", pathA, i), "entry\n")
	}
	h.AssertOK(t, h.Commit("", "seed"))

	// The log entry lists its 6 largest files and counts the rest.
	one, two, share := 1, 2, 99
	largest := make([]mcp.OutlineItem, 6)
	for i := range largest {
		largest[i] = mcp.OutlineItem{Path: fmt.Sprintf("log/f%03d.md", i), Lines: &one}
	}
	outline := &mcp.Outline{Files: 151, Lines: 152, Entries: []mcp.OutlineEntry{
		{Path: "log/", Files: 150, Share: &share, Largest: largest, More: 144},
		{Path: "CONTEXT.md", Lines: &two},
	}}
	res := b.Pull("")
	b.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathB,
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 1, FilesChanged: 151, Insertions: 152, Files: []FileStatExpectation{},
				Materialized: true, Outline: outline,
			},
		},
	}, res)
	encoded, err := json.Marshal(outline)
	if err != nil {
		t.Fatal(err)
	}
	header := "first pull: 151 files now on disk; outline of the largest entries, as JSON; list or read the directory for more\n"
	if text := res.Content[0].(*sdk.TextContent).Text; text != pathB+"\n"+header+string(encoded) {
		t.Fatalf("text item = %q, want the path line, the header and the outline", text)
	}

	files := make([]FileStatExpectation, 0, 150)
	for i := range 150 {
		h.WriteFile(fmt.Sprintf("%s/log/f%03d.md", pathA, i), "entry\nmore\n")
		files = append(files, FileStatExpectation{Path: fmt.Sprintf("log/f%03d.md", i), Insertions: 1})
	}
	h.AssertOK(t, h.Commit("", "grow every entry"))
	res = b.Pull("")
	b.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathB,
		Expect: CallExpectation{
			OK:      true,
			Success: &SuccessExpectation{Generation: 2, FilesChanged: 150, Insertions: 150, Files: files},
		},
	}, res)
	if text := res.Content[0].(*sdk.TextContent).Text; text != pathB {
		t.Fatalf("text item = %q, want the bare path", text)
	}
}

// TestScenarioPullDeltaStat proves the pull success envelope carries the
// on-disk delta: the diffstat between the visible state the pull observed
// and the materialized result (architecture/product-contract.md, Success
// envelope). A remote advance plus a local edit yields exactly the changed
// paths with their line counts, and untouched files contribute no entry.
func TestScenarioPullDeltaStat(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, h.Raw(), h.Cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.WorkspaceRoot(), b.WorkspaceRoot()

	CommitFirst(t, h, "a.md", "A1\n", "c1")
	h.WriteFile(pathA+"/b.md", "B1\n")
	h.AssertOK(t, h.Commit("", "c2"))
	b.AssertOK(t, b.Pull(""))

	// A advances the remote: a.md changes and c.md appears, b.md stays.
	h.WriteFile(pathA+"/a.md", "A2\n")
	h.WriteFile(pathA+"/c.md", "C\n")
	h.AssertOK(t, h.Commit("", "c3"))

	// B edits b.md locally before pulling: the pull merges the remote
	// change with the local edit and reports the on-disk delta.
	b.WriteFile(pathB+"/b.md", "B2\n")
	res := b.Pull("")
	b.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathB,
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
	AssertVisibleFiles(t, b, map[string]string{
		"a.md": "A2\n", "b.md": "B2\n", "c.md": "C\n",
	})
	AssertRemoteGeneration(t, b, 3)
}

// TestScenarioCommitIncrementStat proves the commit success envelope
// carries the published increment: the diffstat between the observed
// remote parent tree and the accepted merged tree
// (architecture/product-contract.md, Success envelope). The first
// publication reports every file as new; a later increment reports exactly
// the paths the commit changed.
func TestScenarioCommitIncrementStat(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	path := h.WorkspaceRoot()
	h.AssertOK(t, h.Pull(""))

	h.WriteFile(path+"/a.md", "alpha\n")
	h.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: path, Message: "c1",
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
	}, h.Commit("", "c1"))

	h.WriteFile(path+"/a.md", "changed\n")
	h.WriteFile(path+"/b.md", "beta\n")
	h.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: path, Message: "c2",
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
	}, h.Commit("", "c2"))

	AssertRemoteGeneration(t, h, 2)
}
