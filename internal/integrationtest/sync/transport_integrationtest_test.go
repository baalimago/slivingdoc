package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"
)

// TestScenarioTransportStdioProcess drives the public server through a
// spawned process and real stdio pipes. On platforms that support the
// filesystem attack fixtures it continues with that compatible path-security
// chain before shutting down, so both contracts share one identical server
// bootstrap (architecture/product-contract.md and security.md).
func TestScenarioTransportStdioProcess(t *testing.T) {
	t.Parallel()
	h := SpawnHelper(t, "fake", nil, "serve")
	cs := h.ConnectClient(t)

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	if len(tools.Tools) != 2 {
		t.Fatalf("listed tools = %d, want exactly 2", len(tools.Tools))
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	if !names[ToolPull] || !names[ToolCommit] {
		t.Fatalf("listed tools = %v, want %q and %q", names, ToolPull, ToolCommit)
	}

	path := h.WorkspaceRoot
	AssertProcessCallOK(t, cs, ToolPull, path, "")
	if err := os.WriteFile(filepath.Join(path, "stdio.md"), []byte("over stdio\n"), 0o644); err != nil {
		t.Fatalf("write visible test file: %v", err)
	}
	AssertProcessCallOK(t, cs, ToolCommit, path, "stdio commit")
	// This uses the same unconfigured fake server and untouched workspace
	// paths, so it can extend the process chain without coupling accepted
	// remote state to the path-security assertions.
	ExerciseOptionalPathSecurity(t, h, cs)

	h.CloseClient(t, cs)
	AssertProtocolOnlyStdout(t, h.Record.Bytes())
	// Logs go to stderr only (architecture/logging.md): stdout was proven
	// protocol-only above, so the tool-call records must be on stderr. A
	// non-empty stderr is also satisfied by an unrelated line, so
	// assert the correlated records of the calls that were actually made.
	stderr := h.StderrText(t)
	AssertToolCallLogged(t, stderr, ToolPull)
	AssertToolCallLogged(t, stderr, ToolCommit)
}
