package integrationtest

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/cli"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The writable set inverts the default: everything the operator did not
// declare writable is protected, and the two sets compose by longest match
// (architecture section 2, Read-only paths).

// writableRefusal is the refusal text of a policy whose writable set is the
// given entries: it names where the agent may write, not the protected
// region.
func writableRefusal(writable string) string {
	return "Only " + writable + " is writable in this server. Your changes elsewhere were discarded and the files reset. " +
		"Write under the writable paths, then commit again."
}

// seedWritableBaseline publishes the fixture the writable scenarios start
// from at generation 1, through a writer with no policy configured.
func seedWritableBaseline(t *testing.T) (writer *Harness, pathWriter string) {
	t.Helper()
	writer = newFakeHarness(t, HarnessConfig{})
	pathWriter = writer.Path("notes")
	for name, data := range writableBaseline() {
		writer.WriteFile(pathWriter+"/"+name, data)
	}
	writer.assertOK(t, writer.Pull("", pathWriter))
	writer.assertOK(t, writer.Commit("", pathWriter, "seed"))
	return writer, pathWriter
}

// writableBaseline is the accepted state every writable scenario starts
// from: a protected directory holding a writable subdirectory, a writable
// directory holding a protected subdirectory, one sibling of each, and a
// third level below both, so a set can hold an entry below another one.
func writableBaseline() map[string]string {
	return map[string]string{
		"docs/faq.md":                    "answer: 1\n",
		"docs/open/c.md":                 "open: 1\n",
		"docs/open/deep/e.md":            "deep: 1\n",
		"notes/a.md":                     "x\n",
		"notes/agent-a/free.md":          "free: 1\n",
		"notes/agent-a/locked/secret.md": "secret: 1\n",
		"notes/locked/d.md":              "locked: 1\n",
		"team/b.md":                      "t\n",
	}
}

// newWritableAgent wires an agent harness with the two entry sets over the
// writer's store and pulls the accepted state into its visible directory.
func newWritableAgent(t *testing.T, writer *Harness, readOnly, writable []string) (agent *Harness, pathAgent string) {
	t.Helper()
	agent = newSharedHarness(t, writer.Raw(), writer.cfg.Prefix, HarnessConfig{
		ReadOnlyPaths: readOnly,
		WritablePaths: writable,
	})
	pathAgent = agent.Path("notes")
	agent.assertOK(t, agent.Pull("", pathAgent))
	return agent, pathAgent
}

// TestScenarioWritableCommitInsideWritableSucceeds: an edit inside the
// writable set is published like any other commit.
func TestScenarioWritableCommitInsideWritableSucceeds(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "writable edit",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "notes/a.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Commit("", pathAgent, "writable edit"))
	if got := writer.Manifest().Generation; got != 2 {
		t.Fatalf("manifest generation = %d, want the published 2", got)
	}
}

// TestScenarioWritableCommitRefusesOutsideWritable: an edit in a sibling
// directory of the writable set is refused, reset, and never published.
func TestScenarioWritableCommitRefusesOutsideWritable(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	packsBefore := len(agent.ListObjects("packs/"))
	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/team/b.md", "changed\n")

	res := agent.Commit("", pathAgent, "sibling edit")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "sibling edit",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Action:    "EDIT_FILES",
			Retryable: new(false),
			Files:     []FileExpectation{{Path: "team/b.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{},
		},
	}, res)
	env := decodeEnvelope(t, ToolCall{Tool: toolCommit, Path: pathAgent}, res)
	if want := writableRefusal("notes"); env.Message != want {
		t.Fatalf("refusal message = %q, want %q", env.Message, want)
	}
	if got := agent.ReadFile(pathAgent + "/team/b.md"); got != "t\n" {
		t.Fatalf("team/b.md after the refusal = %q, want the baseline content", got)
	}
	if got := agent.Manifest().Generation; got != generationBefore {
		t.Fatalf("manifest generation changed by the refused commit: %d -> %d", generationBefore, got)
	}
	if got := len(agent.ListObjects("packs/")); got != packsBefore {
		t.Fatalf("pack namespace changed by the refused commit: %d -> %d objects", packsBefore, got)
	}
}

// assertAddedPathRefused commits one file the writable set does not cover
// and asserts the refusal, the removal of the added file, and an unchanged
// generation.
func assertAddedPathRefused(t *testing.T, relPath, content, message string) {
	t.Helper()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/"+relPath, content)

	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: message,
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: relPath, Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
		},
	}, agent.Commit("", pathAgent, message))
	assertVisibleFiles(t, agent, pathAgent, writableBaseline())
	if got := agent.Manifest().Generation; got != generationBefore {
		t.Fatalf("manifest generation changed by the refused commit: %d -> %d", generationBefore, got)
	}
}

// TestScenarioWritableCommitRefusesRootFile: a file created at the notebook
// root is protected by the unmatched default and removed, since the
// baseline does not hold it.
func TestScenarioWritableCommitRefusesRootFile(t *testing.T) {
	t.Parallel()
	assertAddedPathRefused(t, "root.md", "root\n", "root file")
}

// TestScenarioWritableCommitRefusesNewDirectory: a directory that did not
// exist when the process started is protected by the unmatched default.
func TestScenarioWritableCommitRefusesNewDirectory(t *testing.T) {
	t.Parallel()
	assertAddedPathRefused(t, "fresh/x.md", "fresh\n", "new directory")
}

// TestScenarioWritableComposedWritableBelowReadOnlySucceeds: the longer
// entry wins, so a writable subdirectory opens a hole in a protected one
// (decision D1).
func TestScenarioWritableComposedWritableBelowReadOnlySucceeds(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, []string{"docs"}, []string{"docs/open"})

	agent.WriteFile(pathAgent+"/docs/open/c.md", "open: 2\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "edit under the writable hole",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "docs/open/c.md", Insertions: 1, Deletions: 1}},
			},
			ReadOnly: []string{"docs"},
		},
	}, agent.Commit("", pathAgent, "edit under the writable hole"))
}

// TestScenarioWritableComposedSiblingUnderReadOnlyRefused: the hole opens
// only what it names; a sibling under the read-only entry stays protected.
func TestScenarioWritableComposedSiblingUnderReadOnlyRefused(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, []string{"docs"}, []string{"docs/open"})

	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")

	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "sibling under the read-only entry",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"docs"},
		},
	}, agent.Commit("", pathAgent, "sibling under the read-only entry"))
	if got := agent.ReadFile(pathAgent + "/docs/faq.md"); got != "answer: 1\n" {
		t.Fatalf("docs/faq.md after the refusal = %q, want the baseline content", got)
	}
	if got := agent.Manifest().Generation; got != generationBefore {
		t.Fatalf("manifest generation changed by the refused commit: %d -> %d", generationBefore, got)
	}
}

// TestScenarioWritableComposedReadOnlyBelowWritableRefused: the composition
// nests the other way too, and the refusal resets only the protected file.
func TestScenarioWritableComposedReadOnlyBelowWritableRefused(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, []string{"notes/locked"}, []string{"notes"})

	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/notes/locked/d.md", "locked: 2\n")
	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")

	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "read-only below writable",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "notes/locked/d.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"notes/locked"},
		},
	}, agent.Commit("", pathAgent, "read-only below writable"))
	if got := agent.ReadFile(pathAgent + "/notes/locked/d.md"); got != "locked: 1\n" {
		t.Fatalf("notes/locked/d.md after the refusal = %q, want the baseline content", got)
	}
	if got := agent.ReadFile(pathAgent + "/notes/a.md"); got != "y\n" {
		t.Fatalf("notes/a.md after the refusal = %q, want the agent's edit kept", got)
	}
	if got := agent.Manifest().Generation; got != generationBefore {
		t.Fatalf("manifest generation changed by the refused commit: %d -> %d", generationBefore, got)
	}
}

// The three-level compositions: an entry of one set lying between two
// entries of the other. Resolution answers from the entries the operator
// wrote, so the innermost entry decides its own region whichever set holds
// it, and a broader entry added to a set never silences a narrower entry of
// that same set (architecture section 2, Writable paths).

// TestScenarioWritableThreeLevelCommitRefusesNestedProtected: read-only
// notes, a writable agent directory inside it, a read-only directory inside
// that. A commit that tampers with the innermost region is refused and
// reset, while the agent's edit in the writable middle stays on disk.
func TestScenarioWritableThreeLevelCommitRefusesNestedProtected(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer,
		[]string{"notes", "notes/agent-a/locked"}, []string{"notes/agent-a"})

	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/notes/agent-a/locked/secret.md", "secret: 2\n")
	agent.WriteFile(pathAgent+"/notes/agent-a/free.md", "free: 2\n")

	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "tamper under the nested read-only entry",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "notes/agent-a/locked/secret.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"notes", "notes/agent-a/locked"},
			Writable:  []string{"notes/agent-a"},
		},
	}, agent.Commit("", pathAgent, "tamper under the nested read-only entry"))
	if got := agent.ReadFile(pathAgent + "/notes/agent-a/locked/secret.md"); got != "secret: 1\n" {
		t.Fatalf("notes/agent-a/locked/secret.md after the refusal = %q, want the baseline content", got)
	}
	if got := agent.ReadFile(pathAgent + "/notes/agent-a/free.md"); got != "free: 2\n" {
		t.Fatalf("notes/agent-a/free.md after the refusal = %q, want the agent's edit kept", got)
	}
	if got := agent.Manifest().Generation; got != generationBefore {
		t.Fatalf("manifest generation changed by the refused commit: %d -> %d", generationBefore, got)
	}
}

// TestScenarioWritableThreeLevelCommitAllowsNestedWritable: the mirror
// nesting — writable docs, a read-only directory inside it, a writable
// directory inside that. The innermost edit publishes, and the read-only
// middle is still refused and reset under the same configuration.
func TestScenarioWritableThreeLevelCommitAllowsNestedWritable(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer,
		[]string{"docs/open"}, []string{"docs", "docs/open/deep"})

	agent.WriteFile(pathAgent+"/docs/open/deep/e.md", "deep: 2\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "edit the innermost writable entry",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "docs/open/deep/e.md", Insertions: 1, Deletions: 1}},
			},
			ReadOnly: []string{"docs/open"},
			Writable: []string{"docs", "docs/open/deep"},
		},
	}, agent.Commit("", pathAgent, "edit the innermost writable entry"))

	agent.WriteFile(pathAgent+"/docs/open/c.md", "open: 2\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "edit the read-only middle",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "docs/open/c.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
		},
	}, agent.Commit("", pathAgent, "edit the read-only middle"))
	if got := agent.ReadFile(pathAgent + "/docs/open/c.md"); got != "open: 1\n" {
		t.Fatalf("docs/open/c.md after the refusal = %q, want the baseline content", got)
	}
	if got := agent.Manifest().Generation; got != 2 {
		t.Fatalf("manifest generation = %d, want the 2 the first commit published", got)
	}
}

// TestScenarioWritableThreeLevelPullRestoresNestedProtected: the pull half
// of the first nesting. The local edit under the innermost read-only entry
// is discarded for the accepted content, and the edit in the writable
// middle merges normally.
func TestScenarioWritableThreeLevelPullRestoresNestedProtected(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer,
		[]string{"notes", "notes/agent-a/locked"}, []string{"notes/agent-a"})

	agent.WriteFile(pathAgent+"/notes/agent-a/locked/secret.md", "secret: 2\n")
	agent.WriteFile(pathAgent+"/notes/agent-a/free.md", "free: 2\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true},
	}, agent.Pull("", pathAgent))

	want := writableBaseline()
	want["notes/agent-a/free.md"] = "free: 2\n"
	assertVisibleFiles(t, agent, pathAgent, want)
}

// TestScenarioWritableThreeLevelPullKeepsNestedWritable: the pull half of
// the mirror nesting. The read-only middle is restored and the innermost
// writable edit survives.
func TestScenarioWritableThreeLevelPullKeepsNestedWritable(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer,
		[]string{"docs/open"}, []string{"docs", "docs/open/deep"})

	agent.WriteFile(pathAgent+"/docs/open/c.md", "open: 2\n")
	agent.WriteFile(pathAgent+"/docs/open/deep/e.md", "deep: 2\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true},
	}, agent.Pull("", pathAgent))

	want := writableBaseline()
	want["docs/open/deep/e.md"] = "deep: 2\n"
	assertVisibleFiles(t, agent, pathAgent, want)
}

// TestScenarioWritableRefusalLeavesGenerationUnchanged: the refused call
// returns no success and leaves the remote exactly as it found it, so the
// next legitimate commit still publishes generation 2.
func TestScenarioWritableRefusalLeavesGenerationUnchanged(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	packsBefore := len(agent.ListObjects("packs/"))
	agent.WriteFile(pathAgent+"/team/b.md", "changed\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "protected edit",
		Expect: CallExpectation{ErrorCode: "INVALID_REQUEST", Reason: "READ_ONLY_PATH"},
	}, agent.Commit("", pathAgent, "protected edit"))
	if got := agent.Manifest().Generation; got != 1 {
		t.Fatalf("manifest generation after the refusal = %d, want 1", got)
	}
	if got := len(agent.ListObjects("packs/")); got != packsBefore {
		t.Fatalf("pack namespace changed by the refused commit: %d -> %d objects", packsBefore, got)
	}

	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "writable edit",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "notes/a.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Commit("", pathAgent, "writable edit"))
}

// TestScenarioWritableRefusalResetsProtectedFiles: a modification is reset,
// an addition is removed, and a deletion is restored, whichever way the
// protected path changed.
func TestScenarioWritableRefusalResetsProtectedFiles(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/docs/new.md", "new\n")
	agent.RemoveFile(pathAgent + "/team/b.md")

	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "modify, add, and delete under protected paths",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files: []FileExpectation{
				{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
				{Path: "docs/new.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
				{Path: "team/b.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
			},
		},
	}, agent.Commit("", pathAgent, "modify, add, and delete under protected paths"))
	assertVisibleFiles(t, agent, pathAgent, writableBaseline())
}

// TestScenarioWritableRefusalKeepsWritableEdits: the reset touches only the
// protected paths, so the agent's work inside the writable set survives and
// is still unpublished.
func TestScenarioWritableRefusalKeepsWritableEdits(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")

	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "one protected and one writable edit",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
		},
	}, agent.Commit("", pathAgent, "one protected and one writable edit"))
	if got := agent.ReadFile(pathAgent + "/notes/a.md"); got != "y\n" {
		t.Fatalf("notes/a.md after the refusal = %q, want the agent's edit kept", got)
	}
	if got := agent.ReadFile(pathAgent + "/docs/faq.md"); got != "answer: 1\n" {
		t.Fatalf("docs/faq.md after the refusal = %q, want the baseline content", got)
	}
	if got := agent.Manifest().Generation; got != 1 {
		t.Fatalf("manifest generation after the refusal = %d, want 1", got)
	}
}

// TestScenarioWritableRefusalDropsWritableUnderRestoredBlob: the exception
// to "writable edits stay on disk". A protected path the baseline holds as
// a file cannot be restored while a directory stands there, so the reset's
// local mutation fails, the recovery resynchronizes the visible directory
// to the accepted state, and the file a writable entry below that path
// names goes with the directory. The read-only-only row runs the same
// script with no writable set: the outcome is the existing reset's, not the
// writable set's.
func TestScenarioWritableRefusalDropsWritableUnderRestoredBlob(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name               string
		readOnly, writable []string
	}{
		{name: "writable entry below the accepted file", writable: []string{"docs/faq.md/w.md"}},
		{name: "read-only entry above it", readOnly: []string{"docs"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			writer, _ := seedWritableBaseline(t)
			agent, pathAgent := newWritableAgent(t, writer, tt.readOnly, tt.writable)

			generationBefore := agent.Manifest().Generation
			agent.RemoveFile(pathAgent + "/docs/faq.md")
			agent.WriteFile(pathAgent+"/docs/faq.md/w.md", "writable below the accepted file\n")
			agent.WriteFile(pathAgent+"/docs/faq.md/p.md", "protected below the accepted file\n")

			agent.assertEnvelope(t, ToolCall{
				Tool: toolCommit, Path: pathAgent, Message: "replace an accepted file with a directory",
				Expect: CallExpectation{
					ErrorCode: codeRecoveryFailure,
					Retryable: new(true),
					Recovery: &RecoveryExpectation{
						Stage:          "commit.readonly",
						RemoteAccepted: "no",
						Resynchronized: new(true),
					},
				},
			}, agent.Commit("", pathAgent, "replace an accepted file with a directory"))
			// docs/faq.md/w.md is writable in the first row and is gone all
			// the same: the accepted file stands where its directory did.
			assertVisibleFiles(t, agent, pathAgent, writableBaseline())
			if got := agent.Manifest().Generation; got != generationBefore {
				t.Fatalf("manifest generation changed by the refused commit: %d -> %d", generationBefore, got)
			}
		})
	}
}

// TestScenarioWritableCleanCommitUnchanged: a commit that changed no
// protected path returns the envelope the same commit returns with no
// policy configured.
func TestScenarioWritableCleanCommitUnchanged(t *testing.T) {
	t.Parallel()
	commit := func(t *testing.T, readOnly, writable []string) mcp.SuccessInfo {
		t.Helper()
		writer, _ := seedWritableBaseline(t)
		agent, pathAgent := newWritableAgent(t, writer, readOnly, writable)
		agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
		res := agent.Commit("", pathAgent, "writable edit")
		agent.assertOK(t, res)
		got := agent.successInfo(t, res)
		got.Path = "" // the per-test temporary root is not part of the envelope shape
		return got
	}
	configured := commit(t, nil, []string{"notes"})
	unconfigured := commit(t, nil, nil)
	if want := []string{"notes"}; !reflect.DeepEqual(configured.Writable, want) {
		t.Fatalf("configured clean commit writable = %v, want %v", configured.Writable, want)
	}
	// The advertised sets are the process's configuration, not the commit's
	// result: everything the operation produced must still match.
	configured.ReadOnly, configured.Writable = unconfigured.ReadOnly, unconfigured.Writable
	if !reflect.DeepEqual(configured, unconfigured) {
		t.Fatalf("configured clean commit envelope = %v, want the unconfigured %v", configured, unconfigured)
	}
}

// TestScenarioWritablePullRestoresProtectedPath: a local edit to a
// protected path is discarded and the accepted remote content taken.
func TestScenarioWritablePullRestoresProtectedPath(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	putsBefore := agent.Recorder().Count(OpPut)
	agent.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true},
	}, agent.Pull("", pathAgent))
	if got := agent.ReadFile(pathAgent + "/docs/faq.md"); got != "answer: 1\n" {
		t.Fatalf("docs/faq.md after the pull = %q, want the accepted remote content", got)
	}
	if got := agent.Recorder().Count(OpPut) - putsBefore; got != 0 {
		t.Fatalf("pull restore made %d PUT calls, want none", got)
	}
}

// TestScenarioWritablePullKeepsWritableEdits: the restore touches only the
// protected paths, so a writable edit survives the pull and is still there
// to commit.
func TestScenarioWritablePullKeepsWritableEdits(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
	agent.assertOK(t, agent.Pull("", pathAgent))

	want := writableBaseline()
	want["notes/a.md"] = "y\n"
	assertVisibleFiles(t, agent, pathAgent, want)
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "writable edit",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "notes/a.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Commit("", pathAgent, "writable edit"))
}

// TestScenarioWritablePullRestoreAppearsInDiffstat: the diffstat is the raw
// local state against the merged result, so the restore is reported like
// any other on-disk change.
func TestScenarioWritablePullRestoreAppearsInDiffstat(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathAgent,
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 1, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "docs/faq.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Pull("", pathAgent))
}

// TestScenarioWritablePullInvalidContentUnderProtected: the content
// precondition is unmoved, so an invalid file under a protected path is
// refused before the restore runs and the local edits are untouched.
func TestScenarioWritablePullInvalidContentUnderProtected(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/docs/blob", "\x00\x01binary")

	invalid := CallExpectation{
		ErrorCode: "INVALID_REQUEST",
		Reason:    "INVALID_CONTENT",
		Action:    "EDIT_FILES",
		Retryable: new(false),
		Files:     []FileExpectation{{Path: "docs/blob", Reason: "INVALID_CONTENT", Ranges: []RangeExpectation{}}},
	}
	agent.assertEnvelope(t, ToolCall{Tool: toolPull, Path: pathAgent, Expect: invalid}, agent.Pull("", pathAgent))
	agent.assertEnvelope(t, ToolCall{Tool: toolCommit, Path: pathAgent, Message: "m", Expect: invalid}, agent.Commit("", pathAgent, "m"))
	if got := agent.ReadFile(pathAgent + "/docs/faq.md"); got != "answer: 2\n" {
		t.Fatalf("docs/faq.md after the refused calls = %q, want the untouched local edit", got)
	}

	agent.RemoveFile(pathAgent + "/docs/blob")
	agent.assertOK(t, agent.Pull("", pathAgent))
	assertVisibleFiles(t, agent, pathAgent, writableBaseline())
}

// TestScenarioWritableUnconfiguredCommitAndPullUnchanged: with neither set
// configured no policy work runs, so a root file and a directory that did
// not exist at startup publish exactly as they do today.
func TestScenarioWritableUnconfiguredCommitAndPullUnchanged(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, nil, nil)

	agent.WriteFile(pathAgent+"/root.md", "root\n")
	agent.WriteFile(pathAgent+"/fresh/x.md", "fresh\n")
	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")

	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "write anywhere",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 3, Insertions: 3, Deletions: 1,
				Files: []FileStatExpectation{
					{Path: "docs/faq.md", Insertions: 1, Deletions: 1},
					{Path: "fresh/x.md", Insertions: 1},
					{Path: "root.md", Insertions: 1},
				},
			},
			ReadOnly: []string{},
		},
	}, agent.Commit("", pathAgent, "write anywhere"))

	want := writableBaseline()
	want["docs/faq.md"] = "answer: 2\n"
	want["root.md"] = "root\n"
	want["fresh/x.md"] = "fresh\n"
	agent.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true, ReadOnly: []string{}},
	}, agent.Pull("", pathAgent))
	assertVisibleFiles(t, agent, pathAgent, want)
}

// The flag surface of the writable set: --writable-paths resolves through
// the shared flag set of serve, pull, and commit, and a path named by both
// settings refuses startup before any dependency loads (architecture
// section 17).

// TestCommandsShareWritablePathsFlag: the flag joins the shared flag set,
// so no command rejects it as unknown.
func TestCommandsShareWritablePathsFlag(t *testing.T) {
	t.Parallel()
	commands := cli.Commands(git2.New(), app.ProcessOptions{})
	for _, name := range []string{"serve|s", "pull|p", "commit|c"} {
		command, ok := commands[name]
		if !ok {
			t.Fatalf("cli.Commands() has no %q command", name)
		}
		if command.Flagset().Lookup("writable-paths") == nil {
			t.Fatalf("%q does not accept --writable-paths", name)
		}
	}
}

// TestScenarioWritableFlagCommitInside: a one-shot commit process confined
// by the flag publishes an edit inside the writable set and reports the
// accepted generation and the resolved notebook path.
func TestScenarioWritableFlagCommitInside(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	notes := filepath.Join(root, "notes")
	runCLIOK(t, "fake", env, []string{"writable: work"}, "pull", "--writable-paths=work", notes)

	writeCLIFile(t, filepath.Join(notes, "work", "a.md"), "inside\n")
	runCLIExact(t, "fake", env,
		"OK  generation 1  "+notes+"\n"+
			"  work/a.md  +1\n"+
			"1 files changed, 1 insertions(+), 0 deletions(-)\n"+
			"writable: work\n",
		"commit", "--writable-paths=work", notes, "-m", "inside the writable set")
}

// TestScenarioWritableFlagCommitOutsideRefused: an edit outside the
// writable set is the structured refusal naming where the process may
// write; the touched file is reset to the accepted content, the edit inside
// survives, and nothing is published. R is pre-seeded on the real backend
// because spawned processes cannot share the fake store.
func TestScenarioWritableFlagCommitOutsideRefused(t *testing.T) {
	t.Parallel()
	env, root, prefix := realCLIEnv(t, "integrationtest-writable-commit")
	writer := seedWritableCLIBaseline(t, prefix)

	notes := filepath.Join(root, "notes")
	runCLIOK(t, "real", env, []string{"writable: work"}, "pull", "--writable-paths=work", notes)

	writeCLIFile(t, filepath.Join(notes, "shared", "b.md"), "changed\n")
	writeCLIFile(t, filepath.Join(notes, "work", "a.md"), "y\n")
	code, stdout, stderr := runCLI(t, "real", env, "commit", "--writable-paths=work", notes, "-m", "outside")
	if code != 1 {
		t.Fatalf("commit outside the writable set = exit %d, want 1; stderr: %s", code, stderr)
	}
	want := "INVALID_REQUEST · READ_ONLY_PATH\n" +
		writableRefusal("work") + "\n" +
		"  shared/b.md  read-only\n" +
		"next: edit the files, then commit\n" +
		"retryable: false\n" +
		"writable: work\n"
	if stdout != want {
		t.Fatalf("refusal stdout = %q, want %q", stdout, want)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "shared", "b.md")); err != nil || string(got) != "t\n" {
		t.Fatalf("shared/b.md = %q, %v; want the reset accepted content", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "work", "a.md")); err != nil || string(got) != "y\n" {
		t.Fatalf("work/a.md = %q, %v; want the edit inside the writable set preserved", got, err)
	}
	if got := writer.Manifest().Generation; got != 1 {
		t.Fatalf("manifest generation after the refusal = %d, want the seeded 1", got)
	}
}

// TestScenarioWritableFlagPullRestores: a one-shot pull process confined by
// the flag takes the accepted content for the protected path, reports the
// restore in its diffstat, and leaves the writable edit alone.
func TestScenarioWritableFlagPullRestores(t *testing.T) {
	t.Parallel()
	env, root, prefix := realCLIEnv(t, "integrationtest-writable-pull")
	seedWritableCLIBaseline(t, prefix)

	notes := filepath.Join(root, "notes")
	runCLIOK(t, "real", env, []string{"writable: work"}, "pull", "--writable-paths=work", notes)

	writeCLIFile(t, filepath.Join(notes, "shared", "b.md"), "local\n")
	writeCLIFile(t, filepath.Join(notes, "work", "a.md"), "mine\n")
	runCLIExact(t, "real", env,
		"OK  generation 1  "+notes+"\n"+
			"  shared/b.md  +1 -1\n"+
			"1 files changed, 1 insertions(+), 1 deletions(-)\n"+
			"writable: work\n",
		"pull", "--writable-paths=work", notes)
	if got, err := os.ReadFile(filepath.Join(notes, "shared", "b.md")); err != nil || string(got) != "t\n" {
		t.Fatalf("shared/b.md after the pull = %q, %v; want the accepted remote content", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "work", "a.md")); err != nil || string(got) != "mine\n" {
		t.Fatalf("work/a.md after the pull = %q, %v; want the writable edit untouched", got, err)
	}
}

// seedWritableCLIBaseline publishes generation 1 on the given real-backend
// prefix: one file inside the writable set and one outside it. The writer
// publishes through the in-process harness, since the property under test
// lives in the one-shot CLI processes rather than in the seeding.
func seedWritableCLIBaseline(t *testing.T, prefix string) *Harness {
	t.Helper()
	writer := NewHarness(t, HarnessConfig{Prefix: prefix})
	seed := writer.Path("seed")
	writer.WriteFile(filepath.Join(seed, "work", "a.md"), "x\n")
	writer.WriteFile(filepath.Join(seed, "shared", "b.md"), "t\n")
	writer.assertOK(t, writer.Pull("", seed))
	writer.assertOK(t, writer.Commit("", seed, "seed"))
	return writer
}

// TestScenarioWritableOverlapRefusesStartup: a path named by both settings
// is a configuration error for every command, refused before the native
// engine opens and before the object store is built and probed. The
// probe-failing store is the ordering oracle: its own diagnostic never
// appears, because the refusal precedes it.
func TestScenarioWritableOverlapRefusesStartup(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		args []string
	}{
		{name: "serve", args: []string{"serve"}},
		{name: "pull", args: []string{"pull", "notes"}},
		{name: "commit", args: []string{"commit", "notes", "-m", "m"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{row.args[0], "--read-only-paths=docs", "--writable-paths=docs"}, row.args[1:]...)
			code, stdout, stderr := runCLI(t, "bad-store", nil, args...)
			if code != 1 {
				t.Fatalf("%v = exit %d, want 1; stderr: %s", args, code, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("%v wrote stdout: %q", args, stdout)
			}
			for _, want := range []string{"docs", "--read-only-paths", "--writable-paths"} {
				if !strings.Contains(stderr, want) {
					t.Fatalf("%v stderr = %q, want it to name %q", args, stderr, want)
				}
			}
			if strings.Contains(stderr, "INCOMPATIBLE_STORE") {
				t.Fatalf("%v touched the object store before the overlap refusal: %s", args, stderr)
			}
		})
	}
}

// TestScenarioWritableFlagExplicitEmptyIgnoresEnvironment: the idiom that
// defeats an inherited environment value. The process behaves as if no
// writable set were configured, so a path the inherited value would have
// protected publishes normally.
func TestScenarioWritableFlagExplicitEmptyIgnoresEnvironment(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	env = append(env, "SLIVINGDOC_WRITABLE_PATHS=work")
	notes := filepath.Join(root, "notes")
	runCLIOK(t, "fake", env, nil, "pull", "--writable-paths=", notes)

	writeCLIFile(t, filepath.Join(notes, "elsewhere", "b.md"), "outside the inherited set\n")
	runCLIExact(t, "fake", env,
		"OK  generation 1  "+notes+"\n"+
			"  elsewhere/b.md  +1\n"+
			"1 files changed, 1 insertions(+), 0 deletions(-)\n",
		"commit", "--writable-paths=", notes, "-m", "unconfined")
}

// The advertisement scenarios below prove the writable set reaches every
// surface an agent or an operator reads (architecture section 2, Read-only
// paths). Each one runs against the real engine over the fake store.

// wantWritableInstructions and wantWritableDescription are the sentences a
// process configured with the single writable entry "notes" adds to its
// instructions and to both tool descriptions.
const (
	wantWritableInstructions = " Writable paths: notes. notes_commit refuses any change elsewhere, " +
		"resets those files, and reports READ_ONLY_PATH; write only under them."
	wantWritableDescription = " Writable paths in this server: notes; changes elsewhere are refused and reset."
)

// TestScenarioWritableInstructionsNameSet: an agent that initializes against
// a confined process learns where it may write before its first edit, and
// the instructions never enumerate the protected region.
func TestScenarioWritableInstructionsNameSet(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, _ := newWritableAgent(t, writer, nil, []string{"notes"})

	instructions := agent.Client("").InitializeResult().Instructions
	if !strings.HasSuffix(instructions, wantWritableInstructions) {
		t.Fatalf("instructions = %q, want it to end with %q", instructions, wantWritableInstructions)
	}
	for _, protected := range []string{"docs", "team"} {
		if strings.Contains(instructions, protected+",") || strings.Contains(instructions, " "+protected+".") {
			t.Fatalf("instructions = %q, want no enumeration of the protected region", instructions)
		}
	}
}

// TestScenarioWritableToolDescriptionsNameSet: an agent that lists tools
// reads the same set on both descriptions, so a host that drops the
// instructions still advertises it.
func TestScenarioWritableToolDescriptionsNameSet(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, _ := newWritableAgent(t, writer, nil, []string{"notes"})

	res, err := agent.Client("").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("tools = %d, want exactly 2", len(res.Tools))
	}
	for _, tool := range res.Tools {
		if !strings.HasSuffix(tool.Description, wantWritableDescription) {
			t.Fatalf("%s description = %q, want it to end with %q", tool.Name, tool.Description, wantWritableDescription)
		}
	}
}

// TestScenarioWritableThreeLevelAdvertisementStatesRule: the composition
// decision D1 exists for, advertised over the real boundary. The operator
// configures three levels — a read-only root, a writable directory inside
// it, and a protected directory inside that — and the process must say so
// without telling the agent two opposite things about the same directory:
// the read-only sentence may not end in "write elsewhere", which is exactly
// where the writable set confines the agent (review 2, R2-02). The
// advertised entries are the ones the operator wrote, so the surfaces also
// prove the collapse kept the third level.
func TestScenarioWritableThreeLevelAdvertisementStatesRule(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer,
		[]string{"notes", "notes/agent-a/locked"}, []string{"notes/agent-a"})

	client := agent.Client("")
	wantInstructions := " Writable paths: notes/agent-a. notes_commit refuses any change elsewhere, " +
		"resets those files, and reports READ_ONLY_PATH; write only under them." +
		" Read-only paths: notes, notes/agent-a/locked. notes_commit refuses any change under them, " +
		"resets those files, and reports READ_ONLY_PATH; where the two sets nest, the longest matching entry decides."
	instructions := client.InitializeResult().Instructions
	if !strings.HasSuffix(instructions, wantInstructions) {
		t.Fatalf("instructions = %q, want it to end with %q", instructions, wantInstructions)
	}
	wantDescription := " Writable paths in this server: notes/agent-a; changes elsewhere are refused and reset." +
		" Read-only paths in this server: notes, notes/agent-a/locked; changes under them are refused and reset, " +
		"and where the two sets nest the longest matching entry decides."
	tools, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	for _, tool := range tools.Tools {
		if !strings.HasSuffix(tool.Description, wantDescription) {
			t.Fatalf("%s description = %q, want it to end with %q", tool.Name, tool.Description, wantDescription)
		}
	}

	res := agent.Pull("", pathAgent)
	agent.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathAgent,
		Expect: CallExpectation{
			OK:       true,
			ReadOnly: []string{"notes", "notes/agent-a/locked"},
			Writable: []string{"notes/agent-a"},
		},
	}, res)
	wantText := pathAgent + " (writable: notes/agent-a; read-only: notes, notes/agent-a/locked; longest match decides)"
	if text, ok := res.Content[0].(*sdk.TextContent); !ok || text.Text != wantText {
		t.Fatalf("text item = %#v, want %q", res.Content[0], wantText)
	}
	for surface, text := range map[string]string{"instructions": instructions, "text item": wantText} {
		if strings.Contains(text, "write elsewhere") {
			t.Fatalf("%s = %q, want no \"write elsewhere\" while the writable set protects elsewhere", surface, text)
		}
	}
}

// TestScenarioWritableSuccessEnvelopeCarriesBothArrays: a successful pull
// carries the writable array beside the read-only one, which still means
// the read-only entries, and the text item names both sets.
func TestScenarioWritableSuccessEnvelopeCarriesBothArrays(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, []string{"notes/locked"}, []string{"notes"})

	res := agent.Pull("", pathAgent)
	agent.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathAgent,
		Expect: CallExpectation{
			OK:       true,
			ReadOnly: []string{"notes/locked"},
			Writable: []string{"notes"},
		},
	}, res)
	text, ok := res.Content[0].(*sdk.TextContent)
	want := pathAgent + " (writable: notes; read-only: notes/locked; longest match decides)"
	if !ok || text.Text != want {
		t.Fatalf("text item = %#v, want %q", res.Content[0], want)
	}
}

// TestScenarioWritableErrorEnvelopeCarriesBothArrays: a refused commit
// carries both arrays alongside the unchanged category, reason, action, and
// file entries.
func TestScenarioWritableErrorEnvelopeCarriesBothArrays(t *testing.T) {
	t.Parallel()
	writer, _ := seedWritableBaseline(t)
	agent, pathAgent := newWritableAgent(t, writer, []string{"notes/locked"}, []string{"notes"})

	agent.WriteFile(pathAgent+"/notes/locked/d.md", "locked: 2\n")
	agent.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathAgent, Message: "locked edit",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Action:    "EDIT_FILES",
			Retryable: new(false),
			Files:     []FileExpectation{{Path: "notes/locked/d.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"notes/locked"},
			Writable:  []string{"notes"},
		},
	}, agent.Commit("", pathAgent, "locked edit"))
}

// TestScenarioWritableUnconfiguredSurfacesUnchanged: with neither set, every
// surface is the bare form it is today and both arrays are present and
// empty.
func TestScenarioWritableUnconfiguredSurfacesUnchanged(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")

	client := h.Client("")
	// The notebook root is the per-test temporary directory, whose name
	// carries the test name, so the check is for the sentence rather than
	// for the word.
	instructions := client.InitializeResult().Instructions
	if strings.Contains(instructions, "Writable paths") {
		t.Fatalf("instructions with no writable set = %q, want no writable mention", instructions)
	}
	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	for _, tool := range res.Tools {
		if strings.Contains(strings.ToLower(tool.Description), "writable") {
			t.Fatalf("%s description with no writable set = %q, want no writable mention", tool.Name, tool.Description)
		}
	}

	pulled := h.Pull("", path)
	h.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: path,
		Expect: CallExpectation{OK: true, ReadOnly: []string{}, Writable: []string{}},
	}, pulled)
	if text, ok := pulled.Content[0].(*sdk.TextContent); !ok || text.Text != path {
		t.Fatalf("text item = %#v, want the bare path %q", pulled.Content[0], path)
	}

	h.WriteFile(path+"/a.md", "hello")
	h.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: path, Message: "",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "MESSAGE_BLANK",
			ReadOnly:  []string{},
			Writable:  []string{},
		},
	}, h.Commit("", path, ""))
}

// TestScenarioWritableReportTrailerOnRefusal: the operator's one-shot
// commit is refused over the process boundary, and the report renders the
// existing status line, message, file entry, and next step plus the trailer
// naming where the process may write. The following success report carries
// the same trailer.
func TestScenarioWritableReportTrailerOnRefusal(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	env = append(env, "SLIVINGDOC_WRITABLE_PATHS=notes")
	notes := filepath.Join(root, "notes")

	code, stdout, stderr := runCLI(t, "fake", env, "pull", notes)
	if code != 0 {
		t.Fatalf("pull = exit %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "OK  generation ") || !strings.HasSuffix(stdout, "writable: notes\n") {
		t.Fatalf("pull stdout = %q, want the OK report with the writable trailer", stdout)
	}

	writeCLIFile(t, filepath.Join(notes, "notes", "a.md"), "inside\n")
	writeCLIFile(t, filepath.Join(notes, "team", "b.md"), "outside\n")
	code, stdout, stderr = runCLI(t, "fake", env, "commit", notes, "-m", "outside edit")
	if code != 1 {
		t.Fatalf("refused commit = exit %d, want 1; stderr: %s", code, stderr)
	}
	want := "INVALID_REQUEST · READ_ONLY_PATH\n" +
		writableRefusal("notes") + "\n" +
		"  team/b.md  read-only\n" +
		"next: edit the files, then commit\n" +
		"retryable: false\n" +
		"writable: notes\n"
	if stdout != want {
		t.Fatalf("refusal stdout = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(filepath.Join(notes, "team", "b.md")); !os.IsNotExist(err) {
		t.Fatalf("team/b.md after the refusal = %v, want it reset away", err)
	}

	runCLIExact(t, "fake", env,
		"OK  generation 1  "+notes+"\n"+
			"  notes/a.md  +1\n"+
			"1 files changed, 1 insertions(+), 0 deletions(-)\n"+
			"writable: notes\n",
		"commit", notes, "-m", "inside edit")
}
