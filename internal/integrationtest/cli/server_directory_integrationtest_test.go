package cli

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"
)

// TestScenarioServerDirectory proves the one-directory serve binding through
// the public process: a configured --workspace-root names the notebook
// directory, and with no root configured the process serves its working
// directory. Both tools operate at and report that one directory, and a tool
// call has no field that could switch it (architecture/mcp-server.md).
func TestScenarioServerDirectory(t *testing.T) {
	t.Parallel()
	t.Run("configured workspace root", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		h := SpawnHelper(t, "fake", nil, "serve", "--workspace-root="+root)
		cs := h.ConnectClient(t)

		AssertProcessCallOK(t, cs, ToolPull, root, "")
		WriteCLIFile(t, filepath.Join(root, "a.md"), "configured root\n")
		committed := AssertProcessCallOK(t, cs, ToolCommit, root, "first commit")
		if committed.Generation != 1 || committed.FilesChanged != 1 {
			t.Fatalf("commit envelope = %+v, want generation 1 with one changed file", committed)
		}
		if entries, err := os.ReadDir(h.WorkspaceRoot); err != nil || len(entries) != 0 {
			t.Fatalf("the unused workspace root = %v (err %v), want it untouched", entries, err)
		}
		h.CloseClient(t, cs)
	})

	t.Run("working directory when no root is configured", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		h := SpawnHelperIn(t, dir, "fake", []string{"SLIVINGDOC_WORKSPACE_ROOT="}, "serve")
		cs := h.ConnectClient(t)

		AssertProcessCallOK(t, cs, ToolPull, dir, "")
		WriteCLIFile(t, filepath.Join(dir, "a.md"), "working directory\n")
		committed := AssertProcessCallOK(t, cs, ToolCommit, dir, "first commit")
		if committed.Generation != 1 || committed.FilesChanged != 1 {
			t.Fatalf("commit envelope = %+v, want generation 1 with one changed file", committed)
		}
		h.CloseClient(t, cs)
	})
}
