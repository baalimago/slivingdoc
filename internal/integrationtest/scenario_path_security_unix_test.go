//go:build !windows && !plan9

package integrationtest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/unix"
)

// TestScenarioPathSecurityProcess exercises the filesystem attack surface
// through a real stdio process (architecture section 18.2, L1131). The
// parent only arranges host fixtures; every observation enters through
// notes_pull and its MCP error envelope.
//
// Each fixture lives in its own directory, so a rejection is attributable
// to exactly one rule: a symlinked path component, a special file inside
// the requested directory, or a request path outside the workspace root.
func TestScenarioPathSecurityProcess(t *testing.T) {
	t.Parallel()
	h := spawnHelper(t, "fake", nil, "serve")
	cs := h.connectClient(t)

	outside := t.TempDir()
	link := filepath.Join(h.workspaceRoot, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create traversal symlink: %v", err)
	}

	// The FIFO is the only irregular entry of its own directory, beside one
	// valid note, so the rejection can name no other cause. No writer ever
	// opens it: a server that tried to read it would block and fail the
	// test by timeout instead of answering.
	special := filepath.Join(h.workspaceRoot, "special")
	if err := os.MkdirAll(special, 0o755); err != nil {
		t.Fatalf("create special directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(special, "note.md"), []byte("valid"), 0o644); err != nil {
		t.Fatalf("write valid note: %v", err)
	}
	fifo := filepath.Join(special, "input.fifo")
	// golang.org/x/sys/unix, not syscall: the phase-2 seam scan forbids a
	// syscall import anywhere in the module as the no-Git-invocation proof.
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("create FIFO: %v", err)
	}

	for _, row := range []struct {
		name string
		path string
	}{
		{name: "path outside the workspace root", path: filepath.Join(outside, "escape")},
		{name: "symlinked path component", path: filepath.Join(link, "notes")},
		{name: "special file inside the request path", path: special},
	} {
		t.Run(row.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
				Name: toolPull, Arguments: map[string]any{"path": row.path},
			})
			if err != nil {
				t.Fatalf("notes_pull(%s) = %v", row.path, err)
			}
			env := decodeEnvelope(t, ToolCall{Tool: toolPull, Path: row.path}, res)
			if env.Code != "INVALID_REQUEST" || env.Retryable {
				t.Fatalf("notes_pull(%s) envelope = %+v, want non-retryable INVALID_REQUEST", row.path, env)
			}
		})
	}

	// Prohibited side effects: the link was never followed and nothing was
	// written outside the root, the special directory keeps exactly the
	// fixture entries, and the FIFO is untouched.
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("directory outside the root = %v (err %v), want untouched and empty", entries, err)
	}
	assertSpecialDirUntouched(t, special)

	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("path-security process exit = %d, want 0; stderr: %s", code, h.stderrText(t))
	}
}

// assertSpecialDirUntouched proves the rejected directory was neither
// rewritten nor drained: the FIFO is still a FIFO and the valid note keeps
// its bytes.
func assertSpecialDirUntouched(t *testing.T, dir string) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(dir, "input.fifo"))
	if err != nil {
		t.Fatalf("lstat FIFO: %v", err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO mode = %s, want an untouched named pipe", info.Mode())
	}
	data, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil || string(data) != "valid" {
		t.Fatalf("valid note = %q (err %v), want the untouched fixture", data, err)
	}
}
