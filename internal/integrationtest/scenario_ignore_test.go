package integrationtest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestScenarioIgnoreJunkNeverBlocks proves that the files an operating
// system or editor leaves in a notebook directory, and the operator's own
// patterns, neither refuse an operation nor reach the notebook, while a
// binary file that no rule names is still refused (architecture/workspace.md,
// Ignored paths).
func TestScenarioIgnoreJunkNeverBlocks(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{Ignore: []string{"*.log", "private/scratch"}})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")

	a.WriteFile(pathA+"/picture.png", "\x00\x89PNG")
	if res := a.Pull("", pathA); !res.IsError {
		t.Fatal("pull accepted a binary file that no rule ignores")
	}
	a.RemoveFile(pathA + "/picture.png")

	a.WriteFile(pathA+"/plan.md", "plan")
	a.WriteFile(pathA+"/run.log", "\x00binary log")
	a.WriteFile(pathA+"/deep/er/run.log", "\x00")
	a.WriteFile(pathA+"/private/scratch/big.bin", "\x00")
	a.WriteFile(pathA+"/private/kept.md", "kept")
	a.WriteFile(pathA+"/.DS_Store", "\x00\x00\x00\x01Bud1\xff")
	a.WriteFile(pathA+"/._plan.md", "\x00\x05\x16\x07")
	a.WriteFile(pathA+"/sub/Thumbs.db", "\xff\xd8\xff")
	a.WriteFile(pathA+"/sub/.plan.md.swp", "\x00swap")
	if err := os.MkdirAll(filepath.Join(pathA, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.WriteFile(pathA+"/.git/config", "[core]")
	a.assertOK(t, a.Pull("", pathA))
	a.assertOK(t, a.Commit("", pathA, "first"))

	b.assertOK(t, b.Pull("", pathB))
	if got, want := b.FSSnapshot(pathB), map[string]string{"plan.md": "plan", "private/kept.md": "kept"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a second checkout holds %v, want only %v: junk must not be published", got, want)
	}
	if got := a.ReadFile(pathA + "/.DS_Store"); got != "\x00\x00\x00\x01Bud1\xff" {
		t.Fatalf(".DS_Store = %q, want it untouched by the commit", got)
	}
}

// TestScenarioIgnoreSurvivesPull proves a pull that removes a directory
// from the notebook keeps the ignored files inside it, and never deletes
// them from the notebook's side either.
func TestScenarioIgnoreSurvivesPull(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")
	commitFirst(t, a, pathA, "d/x.md", "x", "first")
	commitNext(t, a, pathA, "keep.md", "keep", "second")
	b.assertOK(t, b.Pull("", pathB))
	b.WriteFile(pathB+"/d/.DS_Store", "finder")

	a.RemoveFile(pathA + "/d/x.md")
	a.assertOK(t, a.Commit("", pathA, "drop d"))
	b.assertOK(t, b.Pull("", pathB))

	want := map[string]string{"keep.md": "keep", "d/.DS_Store": "finder"}
	if got := b.FSSnapshot(pathB); !reflect.DeepEqual(got, want) {
		t.Fatalf("after the pull the directory holds %v, want %v", got, want)
	}
	b.assertOK(t, b.Commit("", pathB, "nothing to say"))
	a.assertOK(t, a.Pull("", pathA))
	if got := a.FSSnapshot(pathA); !reflect.DeepEqual(got, map[string]string{"keep.md": "keep"}) {
		t.Fatalf("the first checkout holds %v, want the ignored file to stay on the other machine", got)
	}
}

// TestScenarioIgnoreBlocksDirectoryReplacement proves a pull that would put
// a file where this machine keeps ignored files inside a directory is
// refused with a clear, permanent error naming the path, and changes
// nothing.
func TestScenarioIgnoreBlocksDirectoryReplacement(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")
	commitFirst(t, a, pathA, "d/x.md", "x", "first")
	b.assertOK(t, b.Pull("", pathB))
	b.WriteFile(pathB+"/d/.DS_Store", "finder")

	if err := os.RemoveAll(filepath.Join(pathA, "d")); err != nil {
		t.Fatal(err)
	}
	a.WriteFile(pathA+"/d", "now a file")
	a.assertOK(t, a.Commit("", pathA, "d is a file"))

	call := ToolCall{Tool: toolPull, Path: pathB}
	env := decodeEnvelope(t, call, b.Pull("", pathB))
	if env.Code != "INVALID_REQUEST" || env.Reason != "IGNORED_CONFLICT" || env.Action != "EDIT_FILES" || env.Retryable {
		t.Fatalf("pull = %s/%s/%s retryable %v, want INVALID_REQUEST/IGNORED_CONFLICT/EDIT_FILES not retryable; message: %s",
			env.Code, env.Reason, env.Action, env.Retryable, env.Message)
	}
	if !strings.Contains(env.Message, `"d"`) && !strings.Contains(env.Message, "d must become") {
		t.Fatalf("message %q does not name the path", env.Message)
	}
	want := map[string]string{"d/x.md": "x", "d/.DS_Store": "finder"}
	if got := b.FSSnapshot(pathB); !reflect.DeepEqual(got, want) {
		t.Fatalf("after the refused pull the directory holds %v, want %v", got, want)
	}
}
