//go:build !windows && !plan9

package itest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/unix"
)

// exerciseOptionalPathSecurity extends the compatible stdio transport
// process with the filesystem attack surface (architecture/security.md).
// The parent only arranges host fixtures inside the server's one notebook
// directory; every observation enters through notes_pull and its MCP error
// envelope.
//
// Each fixture is planted alone and removed after its call, so a rejection
// is attributable to exactly one rule: a symbolic link or a special file
// inside the notebook directory.
func ExerciseOptionalPathSecurity(t *testing.T, h *HelperProc, cs *sdk.ClientSession) {
	t.Helper()

	outside := t.TempDir()
	link := filepath.Join(h.WorkspaceRoot, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create traversal symlink: %v", err)
	}
	AssertProcessPullRefused(t, cs, "link")
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove traversal symlink: %v", err)
	}

	// No writer ever opens the FIFO: a server that tries to read it blocks
	// and fails the test by timeout instead of answering.
	fifo := filepath.Join(h.WorkspaceRoot, "input.fifo")
	// golang.org/x/sys/unix, not syscall: the phase-2 seam scan forbids a
	// syscall import anywhere in the module as the no-Git-invocation proof.
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("create FIFO: %v", err)
	}
	AssertProcessPullRefused(t, cs, "input.fifo")
	if err := os.Remove(fifo); err != nil {
		t.Fatalf("remove FIFO: %v", err)
	}

	// Prohibited side effects: the link was never followed and nothing was
	// written outside the root.
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("directory outside the root = %v (err %v), want untouched and empty", entries, err)
	}
	// The notebook keeps the file the transport scenario published.
	if got, err := os.ReadFile(filepath.Join(h.WorkspaceRoot, "stdio.md")); err != nil || string(got) != "over stdio\n" {
		t.Fatalf("stdio.md = %q (err %v), want the untouched fixture", got, err)
	}
}

// assertProcessPullRefused proves a pull over a notebook directory that
// carries one violating entry is refused as INVALID_CONTENT and names only
// that entry.
func AssertProcessPullRefused(t *testing.T, cs *sdk.ClientSession, entry string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name: ToolPull, Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("notes_pull with %s = %v", entry, err)
	}
	env := DecodeEnvelope(t, ToolCall{Tool: ToolPull}, res)
	if env.Code != "INVALID_REQUEST" || env.Reason != "INVALID_CONTENT" || env.Action != "EDIT_FILES" || env.Retryable {
		t.Fatalf("notes_pull with %s envelope = %s/%s/%s retryable %v, want INVALID_REQUEST/INVALID_CONTENT/EDIT_FILES",
			entry, env.Code, env.Reason, env.Action, env.Retryable)
	}
	if len(env.Files) != 1 || env.Files[0].Path != entry || env.Files[0].Reason != "INVALID_CONTENT" {
		t.Fatalf("notes_pull with %s files = %+v, want only %s as INVALID_CONTENT", entry, env.Files, entry)
	}
}

// mkfifo plants a FIFO at path, for the credentials file scenarios.
func Mkfifo(path string) error { return unix.Mkfifo(path, 0o600) }
