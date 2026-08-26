package integrationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noRoots blanks both root variables of the helper base environment, which
// is how a scenario reaches the unconfigured serve default. An empty
// environment value is unset (architecture section 17).
var noRoots = []string{
	"SLIVINGDOC_WORKSPACE_ROOT=",
	"SLIVINGDOC_PRIVATE_ROOT=",
}

// TestScenarioEphemeralNotebook proves the unconfigured serve default
// through the public process: with no root configured the server owns one
// temporary notebook directory, both tools default to it, every result
// reports it, and shutdown removes the whole session. The durable notebook
// is the remote, so nothing of value is in the removed directory.
func TestScenarioEphemeralNotebook(t *testing.T) {
	t.Parallel()
	h := spawnHelper(t, "fake", noRoots, "serve")
	cs := h.connectClient(t)

	// The instructions name the directory before any tool call returns.
	instructions := cs.InitializeResult().Instructions
	pulled := assertProcessArgsOK(t, cs, toolPull, "(default)", map[string]any{})
	if pulled.Path == "" {
		t.Fatal("notes_pull success envelope carries no notebook path")
	}
	if !strings.Contains(instructions, pulled.Path) {
		t.Fatalf("instructions = %q, want the notebook directory %q", instructions, pulled.Path)
	}
	emptyPulled := assertProcessArgsOK(t, cs, toolPull, "(empty)", map[string]any{"path": ""})
	if emptyPulled.Path != pulled.Path {
		t.Fatalf("empty-path pull path = %q, want the default path %q", emptyPulled.Path, pulled.Path)
	}

	// The directory is a per-process temporary one, not the working
	// directory: the negative control is that no configured root was in
	// scope, so a stable path would prove the default never fired.
	session := filepath.Dir(pulled.Path)
	if filepath.Base(pulled.Path) != "notebook" || !strings.Contains(filepath.Base(session), "slivingdoc-") {
		t.Fatalf("notebook path = %q, want <tmp>/slivingdoc-*/notebook", pulled.Path)
	}
	if fi, err := os.Stat(pulled.Path); err != nil || !fi.IsDir() {
		t.Fatalf("Stat(%s) = %v, want the materialized notebook directory", pulled.Path, err)
	}
	if _, err := os.Stat(filepath.Join(session, "private")); err != nil {
		t.Fatalf("private root is not a sibling of the notebook: %v", err)
	}

	// A commit with an empty path publishes the edits in that same directory.
	writeCLIFile(t, filepath.Join(pulled.Path, "a.md"), "ephemeral notes\n")
	committed := assertProcessArgsOK(t, cs, toolCommit, "(empty)", map[string]any{"path": "", "message": "empty path"})
	if committed.Path != pulled.Path {
		t.Fatalf("commit path = %q, want the pull path %q", committed.Path, pulled.Path)
	}
	if committed.Generation != 1 || committed.FilesChanged != 1 {
		t.Fatalf("commit envelope = %+v, want generation 1 with one changed file", committed)
	}

	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("process exit = %d, want 0; stderr: %s", code, h.stderrText(t))
	}
	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Fatalf("Stat(%s) = %v, want the session directory removed at shutdown", session, err)
	}
}

// TestScenarioEphemeralNotebooksAreDisjoint proves the race-avoidance
// property the default exists for: two servers started the same way never
// share a notebook directory or a private root, so no two agents contend
// for one operation lock.
func TestScenarioEphemeralNotebooksAreDisjoint(t *testing.T) {
	t.Parallel()
	first := spawnHelper(t, "fake", noRoots, "serve")
	second := spawnHelper(t, "fake", noRoots, "serve")
	firstClient, secondClient := first.connectClient(t), second.connectClient(t)

	a := assertProcessArgsOK(t, firstClient, toolPull, "(default)", map[string]any{})
	b := assertProcessArgsOK(t, secondClient, toolPull, "(default)", map[string]any{})
	if a.Path == b.Path {
		t.Fatalf("both servers chose the notebook directory %q, want disjoint directories", a.Path)
	}

	for _, pair := range []struct {
		client interface{ Close() error }
		proc   *helperProc
	}{{firstClient, first}, {secondClient, second}} {
		if err := pair.client.Close(); err != nil {
			t.Fatalf("close MCP client: %v", err)
		}
		if code := pair.proc.waitExit(t); code != 0 {
			t.Fatalf("process exit = %d, want 0; stderr: %s", code, pair.proc.stderrText(t))
		}
	}
}
