package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/cli"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The writable set inverts the default: everything the operator did not
// declare writable is protected, and the two sets compose by longest match
// (architecture/product-contract.md, Read-only and writable paths).

// TestScenarioWritableCommitInsideWritableSucceeds: an edit inside the
// writable set is published like any other commit.
func TestScenarioWritableCommitInsideWritableSucceeds(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "writable edit",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "notes/a.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Commit("", "writable edit"))
	if got := writer.Manifest().Generation; got != 2 {
		t.Fatalf("manifest generation = %d, want the published 2", got)
	}
}

// TestScenarioWritableCommitRefusesOutsideWritable: an edit in a sibling
// directory of the writable set is refused, reset, and never published.
func TestScenarioWritableCommitRefusesOutsideWritable(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	packsBefore := len(agent.ListObjects("packs/"))
	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/team/b.md", "changed\n")

	res := agent.Commit("", "sibling edit")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "sibling edit",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Action:    "EDIT_FILES",
			Retryable: new(false),
			Files:     []FileExpectation{{Path: "team/b.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{},
		},
	}, res)
	env := DecodeEnvelope(t, ToolCall{Tool: ToolCommit, Path: pathAgent}, res)
	if want := WritableRefusal("notes"); env.Message != want {
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

// TestScenarioWritableCommitRefusesRootFile: a file created at the notebook
// root is protected by the unmatched default and removed, since the
// baseline does not hold it.
func TestScenarioWritableCommitRefusesRootFile(t *testing.T) {
	t.Parallel()
	AssertAddedPathRefused(t, "root.md", "root\n", "root file")
}

// TestScenarioWritableCommitRefusesNewDirectory: a directory that did not
// exist when the process started is protected by the unmatched default.
func TestScenarioWritableCommitRefusesNewDirectory(t *testing.T) {
	t.Parallel()
	AssertAddedPathRefused(t, "fresh/x.md", "fresh\n", "new directory")
}

// TestScenarioWritableComposedWritableBelowReadOnlySucceeds: the longer
// entry wins, so a writable subdirectory opens a hole in a protected one
// (decision D1).
func TestScenarioWritableComposedWritableBelowReadOnlySucceeds(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, []string{"docs"}, []string{"docs/open"})

	agent.WriteFile(pathAgent+"/docs/open/c.md", "open: 2\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "edit under the writable hole",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "docs/open/c.md", Insertions: 1, Deletions: 1}},
			},
			ReadOnly: []string{"docs"},
		},
	}, agent.Commit("", "edit under the writable hole"))
}

// TestScenarioWritableComposedSiblingUnderReadOnlyRefused: the hole opens
// only what it names; a sibling under the read-only entry stays protected.
func TestScenarioWritableComposedSiblingUnderReadOnlyRefused(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, []string{"docs"}, []string{"docs/open"})

	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "sibling under the read-only entry",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"docs"},
		},
	}, agent.Commit("", "sibling under the read-only entry"))
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
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, []string{"notes/locked"}, []string{"notes"})

	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/notes/locked/d.md", "locked: 2\n")
	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "read-only below writable",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "notes/locked/d.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"notes/locked"},
		},
	}, agent.Commit("", "read-only below writable"))
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

// TestScenarioWritableThreeLevelCommitRefusesNestedProtected: read-only
// notes, a writable agent directory inside it, a read-only directory inside
// that. A commit that tampers with the innermost region is refused and
// reset, while the agent's edit in the writable middle stays on disk.
func TestScenarioWritableThreeLevelCommitRefusesNestedProtected(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer,
		[]string{"notes", "notes/agent-a/locked"}, []string{"notes/agent-a"})

	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/notes/agent-a/locked/secret.md", "secret: 2\n")
	agent.WriteFile(pathAgent+"/notes/agent-a/free.md", "free: 2\n")

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "tamper under the nested read-only entry",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "notes/agent-a/locked/secret.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"notes", "notes/agent-a/locked"},
			Writable:  []string{"notes/agent-a"},
		},
	}, agent.Commit("", "tamper under the nested read-only entry"))
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
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer,
		[]string{"docs/open"}, []string{"docs", "docs/open/deep"})

	agent.WriteFile(pathAgent+"/docs/open/deep/e.md", "deep: 2\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "edit the innermost writable entry",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "docs/open/deep/e.md", Insertions: 1, Deletions: 1}},
			},
			ReadOnly: []string{"docs/open"},
			Writable: []string{"docs", "docs/open/deep"},
		},
	}, agent.Commit("", "edit the innermost writable entry"))

	agent.WriteFile(pathAgent+"/docs/open/c.md", "open: 2\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "edit the read-only middle",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "docs/open/c.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
		},
	}, agent.Commit("", "edit the read-only middle"))
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
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer,
		[]string{"notes", "notes/agent-a/locked"}, []string{"notes/agent-a"})

	agent.WriteFile(pathAgent+"/notes/agent-a/locked/secret.md", "secret: 2\n")
	agent.WriteFile(pathAgent+"/notes/agent-a/free.md", "free: 2\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true},
	}, agent.Pull(""))

	want := WritableBaseline()
	want["notes/agent-a/free.md"] = "free: 2\n"
	AssertVisibleFiles(t, agent, want)
}

// TestScenarioWritableThreeLevelPullKeepsNestedWritable: the pull half of
// the mirror nesting. The read-only middle is restored and the innermost
// writable edit survives.
func TestScenarioWritableThreeLevelPullKeepsNestedWritable(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer,
		[]string{"docs/open"}, []string{"docs", "docs/open/deep"})

	agent.WriteFile(pathAgent+"/docs/open/c.md", "open: 2\n")
	agent.WriteFile(pathAgent+"/docs/open/deep/e.md", "deep: 2\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true},
	}, agent.Pull(""))

	want := WritableBaseline()
	want["docs/open/deep/e.md"] = "deep: 2\n"
	AssertVisibleFiles(t, agent, want)
}

// TestScenarioWritableRefusalLeavesGenerationUnchanged: the refused call
// returns no success and leaves the remote exactly as it found it, so the
// next legitimate commit still publishes generation 2.
func TestScenarioWritableRefusalLeavesGenerationUnchanged(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	packsBefore := len(agent.ListObjects("packs/"))
	agent.WriteFile(pathAgent+"/team/b.md", "changed\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "protected edit",
		Expect: CallExpectation{ErrorCode: "INVALID_REQUEST", Reason: "READ_ONLY_PATH"},
	}, agent.Commit("", "protected edit"))
	if got := agent.Manifest().Generation; got != 1 {
		t.Fatalf("manifest generation after the refusal = %d, want 1", got)
	}
	if got := len(agent.ListObjects("packs/")); got != packsBefore {
		t.Fatalf("pack namespace changed by the refused commit: %d -> %d objects", packsBefore, got)
	}

	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "writable edit",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "notes/a.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Commit("", "writable edit"))
}

// TestScenarioWritableRefusalResetsProtectedFiles: a modification is reset,
// an addition is removed, and a deletion is restored, whichever way the
// protected path changed.
func TestScenarioWritableRefusalResetsProtectedFiles(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/docs/new.md", "new\n")
	agent.RemoveFile(pathAgent + "/team/b.md")

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "modify, add, and delete under protected paths",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files: []FileExpectation{
				{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
				{Path: "docs/new.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
				{Path: "team/b.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
			},
		},
	}, agent.Commit("", "modify, add, and delete under protected paths"))
	AssertVisibleFiles(t, agent, WritableBaseline())
}

// TestScenarioWritableRefusalKeepsWritableEdits: the reset touches only the
// protected paths, so the agent's work inside the writable set survives and
// is still unpublished.
func TestScenarioWritableRefusalKeepsWritableEdits(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "one protected and one writable edit",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
		},
	}, agent.Commit("", "one protected and one writable edit"))
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
			writer, _ := SeedWritableBaseline(t)
			agent, pathAgent := NewWritableAgent(t, writer, tt.readOnly, tt.writable)

			generationBefore := agent.Manifest().Generation
			agent.RemoveFile(pathAgent + "/docs/faq.md")
			agent.WriteFile(pathAgent+"/docs/faq.md/w.md", "writable below the accepted file\n")
			agent.WriteFile(pathAgent+"/docs/faq.md/p.md", "protected below the accepted file\n")

			agent.AssertEnvelope(t, ToolCall{
				Tool: ToolCommit, Path: pathAgent, Message: "replace an accepted file with a directory",
				Expect: CallExpectation{
					ErrorCode: CodeRecoveryFailure,
					Retryable: new(true),
					Recovery: &RecoveryExpectation{
						Stage:          "commit.readonly",
						RemoteAccepted: "no",
						Resynchronized: new(true),
					},
				},
			}, agent.Commit("", "replace an accepted file with a directory"))
			// docs/faq.md/w.md is writable in the first row and is gone all
			// the same: the accepted file stands where its directory did.
			AssertVisibleFiles(t, agent, WritableBaseline())
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
		writer, _ := SeedWritableBaseline(t)
		agent, pathAgent := NewWritableAgent(t, writer, readOnly, writable)
		agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
		res := agent.Commit("", "writable edit")
		agent.AssertOK(t, res)
		got := agent.SuccessInfo(t, res)
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
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	putsBefore := agent.Recorder().Count(OpPut)
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true},
	}, agent.Pull(""))
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
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
	agent.AssertOK(t, agent.Pull(""))

	want := WritableBaseline()
	want["notes/a.md"] = "y\n"
	AssertVisibleFiles(t, agent, want)
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "writable edit",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "notes/a.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Commit("", "writable edit"))
}

// TestScenarioWritablePullRestoreAppearsInDiffstat: the diffstat is the raw
// local state against the merged result, so the restore is reported like
// any other on-disk change.
func TestScenarioWritablePullRestoreAppearsInDiffstat(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 1, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "docs/faq.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Pull(""))
}

// TestScenarioWritablePullInvalidContentUnderProtected: the content
// precondition is unmoved, so an invalid file under a protected path is
// refused before the restore runs and the local edits are untouched.
func TestScenarioWritablePullInvalidContentUnderProtected(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/docs/blob", "\x00\x01binary")

	invalid := CallExpectation{
		ErrorCode: "INVALID_REQUEST",
		Reason:    "INVALID_CONTENT",
		Action:    "EDIT_FILES",
		Retryable: new(false),
		Files:     []FileExpectation{{Path: "docs/blob", Reason: "INVALID_CONTENT", Ranges: []RangeExpectation{}}},
	}
	agent.AssertEnvelope(t, ToolCall{Tool: ToolPull, Path: pathAgent, Expect: invalid}, agent.Pull(""))
	agent.AssertEnvelope(t, ToolCall{Tool: ToolCommit, Path: pathAgent, Message: "m", Expect: invalid}, agent.Commit("", "m"))
	if got := agent.ReadFile(pathAgent + "/docs/faq.md"); got != "answer: 2\n" {
		t.Fatalf("docs/faq.md after the refused calls = %q, want the untouched local edit", got)
	}

	agent.RemoveFile(pathAgent + "/docs/blob")
	agent.AssertOK(t, agent.Pull(""))
	AssertVisibleFiles(t, agent, WritableBaseline())
}

// TestScenarioWritableUnconfiguredCommitAndPullUnchanged: with neither set
// configured no policy work runs, so a root file and a directory that did
// not exist at startup publish exactly as they do today.
func TestScenarioWritableUnconfiguredCommitAndPullUnchanged(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, nil)

	agent.WriteFile(pathAgent+"/root.md", "root\n")
	agent.WriteFile(pathAgent+"/fresh/x.md", "fresh\n")
	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "write anywhere",
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
	}, agent.Commit("", "write anywhere"))

	want := WritableBaseline()
	want["docs/faq.md"] = "answer: 2\n"
	want["root.md"] = "root\n"
	want["fresh/x.md"] = "fresh\n"
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true, ReadOnly: []string{}},
	}, agent.Pull(""))
	AssertVisibleFiles(t, agent, want)
}

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
	env, root := CliRoots(t)
	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "fake", env, []string{"writable: work"}, "pull", "--writable-paths=work", notes)

	WriteCLIFile(t, filepath.Join(notes, "work", "a.md"), "inside\n")
	RunCLIExact(t, "fake", env,
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
	env, root, prefix := RealCLIEnv(t, "integrationtest-writable-commit")
	writer := SeedWritableCLIBaseline(t, prefix)

	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "real", env, []string{"writable: work"}, "pull", "--writable-paths=work", notes)

	WriteCLIFile(t, filepath.Join(notes, "shared", "b.md"), "changed\n")
	WriteCLIFile(t, filepath.Join(notes, "work", "a.md"), "y\n")
	code, stdout, stderr := RunCLI(t, "real", env, "commit", "--writable-paths=work", notes, "-m", "outside")
	if code != 1 {
		t.Fatalf("commit outside the writable set = exit %d, want 1; stderr: %s", code, stderr)
	}
	want := "INVALID_REQUEST · READ_ONLY_PATH\n" +
		WritableRefusal("work") + "\n" +
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
	env, root, prefix := RealCLIEnv(t, "integrationtest-writable-pull")
	SeedWritableCLIBaseline(t, prefix)

	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "real", env, []string{"writable: work"}, "pull", "--writable-paths=work", notes)

	WriteCLIFile(t, filepath.Join(notes, "shared", "b.md"), "local\n")
	WriteCLIFile(t, filepath.Join(notes, "work", "a.md"), "mine\n")
	RunCLIExact(t, "real", env,
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
			code, stdout, stderr := RunCLI(t, "bad-store", nil, args...)
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
	env, root := CliRoots(t)
	env = append(env, "SLIVINGDOC_WRITABLE_PATHS=work")
	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "fake", env, nil, "pull", "--writable-paths=", notes)

	WriteCLIFile(t, filepath.Join(notes, "elsewhere", "b.md"), "outside the inherited set\n")
	RunCLIExact(t, "fake", env,
		"OK  generation 1  "+notes+"\n"+
			"  elsewhere/b.md  +1\n"+
			"1 files changed, 1 insertions(+), 0 deletions(-)\n",
		"commit", "--writable-paths=", notes, "-m", "unconfined")
}

// TestScenarioWritableInstructionsNameSet: an agent that initializes against
// a confined process learns where it may write before its first edit, and
// the instructions never enumerate the protected region.
func TestScenarioWritableInstructionsNameSet(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, _ := NewWritableAgent(t, writer, nil, []string{"notes"})

	instructions := agent.Client("").InitializeResult().Instructions
	if !strings.HasSuffix(instructions, WantWritableInstructions) {
		t.Fatalf("instructions = %q, want it to end with %q", instructions, WantWritableInstructions)
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
	writer, _ := SeedWritableBaseline(t)
	agent, _ := NewWritableAgent(t, writer, nil, []string{"notes"})

	res, err := agent.Client("").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("tools = %d, want exactly 2", len(res.Tools))
	}
	for _, tool := range res.Tools {
		if !strings.HasSuffix(tool.Description, WantWritableDescription) {
			t.Fatalf("%s description = %q, want it to end with %q", tool.Name, tool.Description, WantWritableDescription)
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
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer,
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

	res := agent.Pull("")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{
			OK:       true,
			ReadOnly: []string{"notes", "notes/agent-a/locked"},
			Writable: []string{"notes/agent-a"},
		},
	}, res)
	wantText := pathAgent + " (writable: notes/agent-a; read-only: notes, notes/agent-a/locked; longest match decides)"
	if text, ok := res.Content[0].(*sdk.TextContent); !ok || FirstLine(text.Text) != wantText {
		t.Fatalf("text item = %#v, want the path line %q", res.Content[0], wantText)
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
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, []string{"notes/locked"}, []string{"notes"})

	res := agent.Pull("")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{
			OK:       true,
			ReadOnly: []string{"notes/locked"},
			Writable: []string{"notes"},
		},
	}, res)
	text, ok := res.Content[0].(*sdk.TextContent)
	want := pathAgent + " (writable: notes; read-only: notes/locked; longest match decides)"
	if !ok || FirstLine(text.Text) != want {
		t.Fatalf("text item = %#v, want the path line %q", res.Content[0], want)
	}
}

// TestScenarioWritableErrorEnvelopeCarriesBothArrays: a refused commit
// carries both arrays alongside the unchanged category, reason, action, and
// file entries.
func TestScenarioWritableErrorEnvelopeCarriesBothArrays(t *testing.T) {
	t.Parallel()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, []string{"notes/locked"}, []string{"notes"})

	agent.WriteFile(pathAgent+"/notes/locked/d.md", "locked: 2\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "locked edit",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Action:    "EDIT_FILES",
			Retryable: new(false),
			Files:     []FileExpectation{{Path: "notes/locked/d.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"notes/locked"},
			Writable:  []string{"notes"},
		},
	}, agent.Commit("", "locked edit"))
}

// TestScenarioWritableUnconfiguredSurfacesUnchanged: with neither set, every
// surface is the bare form it is today and both arrays are present and
// empty.
func TestScenarioWritableUnconfiguredSurfacesUnchanged(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	path := h.WorkspaceRoot()

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

	pulled := h.Pull("")
	h.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: path,
		Expect: CallExpectation{OK: true, ReadOnly: []string{}, Writable: []string{}},
	}, pulled)
	if text, ok := pulled.Content[0].(*sdk.TextContent); !ok || text.Text != path {
		t.Fatalf("text item = %#v, want the bare path %q", pulled.Content[0], path)
	}

	h.WriteFile(path+"/a.md", "hello")
	h.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: path, Message: "",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "MESSAGE_BLANK",
			ReadOnly:  []string{},
			Writable:  []string{},
		},
	}, h.Commit("", ""))
}

// TestScenarioWritableReportTrailerOnRefusal: the operator's one-shot
// commit is refused over the process boundary, and the report renders the
// existing status line, message, file entry, and next step plus the trailer
// naming where the process may write. The following success report carries
// the same trailer.
func TestScenarioWritableReportTrailerOnRefusal(t *testing.T) {
	t.Parallel()
	env, root := CliRoots(t)
	env = append(env, "SLIVINGDOC_WRITABLE_PATHS=notes")
	notes := filepath.Join(root, "notes")

	code, stdout, stderr := RunCLI(t, "fake", env, "pull", notes)
	if code != 0 {
		t.Fatalf("pull = exit %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "OK  generation ") || !strings.HasSuffix(stdout, "writable: notes\n") {
		t.Fatalf("pull stdout = %q, want the OK report with the writable trailer", stdout)
	}

	WriteCLIFile(t, filepath.Join(notes, "notes", "a.md"), "inside\n")
	WriteCLIFile(t, filepath.Join(notes, "team", "b.md"), "outside\n")
	code, stdout, stderr = RunCLI(t, "fake", env, "commit", notes, "-m", "outside edit")
	if code != 1 {
		t.Fatalf("refused commit = exit %d, want 1; stderr: %s", code, stderr)
	}
	want := "INVALID_REQUEST · READ_ONLY_PATH\n" +
		WritableRefusal("notes") + "\n" +
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

	RunCLIExact(t, "fake", env,
		"OK  generation 1  "+notes+"\n"+
			"  notes/a.md  +1\n"+
			"1 files changed, 1 insertions(+), 0 deletions(-)\n"+
			"writable: notes\n",
		"commit", notes, "-m", "inside edit")
}
