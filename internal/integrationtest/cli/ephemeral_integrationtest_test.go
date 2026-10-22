package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

// TestScenarioEphemeralNotebook proves the unconfigured serve default
// through the public process: with no root configured the server owns one
// temporary notebook directory, both tools operate at it, every result
// reports it, and shutdown removes the whole session. The durable notebook
// is the remote, so nothing of value is in the removed directory.
func TestScenarioEphemeralNotebook(t *testing.T) {
	t.Parallel()
	// A scratch server owns no directory that survives it, so it neither
	// reads nor writes the settings file. A file this build cannot read
	// would refuse a durable command, and must not stop this server.
	env := append(append([]string(nil), NoRoots...), credentials.DirEnv+"="+filepath.Join(t.TempDir(), "cfg"))
	PlantSettings(t, env, `{"version":0,"entries":[]}`)
	before := ReadSettings(t, env)
	h := SpawnHelper(t, "fake", env, "serve")
	cs := h.ConnectClient(t)

	// The instructions name the directory before any tool call returns.
	instructions := cs.InitializeResult().Instructions
	pulled := AssertProcessArgsOK(t, cs, ToolPull, "", map[string]any{})
	if pulled.Path == "" {
		t.Fatal("notes_pull success envelope carries no notebook path")
	}
	if !strings.Contains(instructions, pulled.Path) {
		t.Fatalf("instructions = %q, want the notebook directory %q", instructions, pulled.Path)
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

	// A commit publishes the edits in that same directory.
	WriteCLIFile(t, filepath.Join(pulled.Path, "a.md"), "ephemeral notes\n")
	committed := AssertProcessCallOK(t, cs, ToolCommit, pulled.Path, "ephemeral commit")
	if committed.Path != pulled.Path {
		t.Fatalf("commit path = %q, want the pull path %q", committed.Path, pulled.Path)
	}
	if committed.Generation != 1 || committed.FilesChanged != 1 {
		t.Fatalf("commit envelope = %+v, want generation 1 with one changed file", committed)
	}

	h.CloseClient(t, cs)
	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Fatalf("Stat(%s) = %v, want the session directory removed at shutdown", session, err)
	}
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after a scratch serve = %q, want it unchanged (%q)", after, before)
	}
}

// TestScenarioEphemeralNotebooksAreDisjoint proves the race-avoidance
// property the default exists for: two servers started the same way never
// share a notebook directory or a private root, so no two agents contend
// for one operation lock.
func TestScenarioEphemeralNotebooksAreDisjoint(t *testing.T) {
	t.Parallel()
	first := SpawnHelper(t, "fake", NoRoots, "serve")
	second := SpawnHelper(t, "fake", NoRoots, "serve")
	firstClient, secondClient := first.ConnectClient(t), second.ConnectClient(t)

	a := AssertProcessArgsOK(t, firstClient, ToolPull, "", map[string]any{})
	b := AssertProcessArgsOK(t, secondClient, ToolPull, "", map[string]any{})
	if a.Path == b.Path {
		t.Fatalf("both servers chose the notebook directory %q, want disjoint directories", a.Path)
	}

	for _, pair := range []struct {
		client interface{ Close() error }
		Proc   *HelperProc
	}{{firstClient, first}, {secondClient, second}} {
		if err := pair.client.Close(); err != nil {
			t.Fatalf("close MCP client: %v", err)
		}
		if code := pair.Proc.WaitExit(t); code != 0 {
			t.Fatalf("process exit = %d, want 0; stderr: %s", code, pair.Proc.StderrText(t))
		}
	}
}
