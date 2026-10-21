package workspace

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestScenarioReadOnlyCommitRefusedAndReset: a commit touching a read-only
// path is refused and reset without remote mutation
// (architecture/product-contract.md, Read-only and writable paths).
func TestScenarioReadOnlyCommitRefusedAndReset(t *testing.T) {
	t.Parallel()
	writer, _ := SeedReadOnlyBaseline(t)
	agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs"})
	agent.AssertOK(t, agent.Pull(""))

	packsBefore := len(agent.ListObjects("packs/"))
	generationBefore := agent.Manifest().Generation

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")

	res := agent.Commit("", "docs edit")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "docs edit",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Action:    "EDIT_FILES",
			Retryable: new(false),
			Files:     []FileExpectation{{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"docs"},
		},
	}, res)
	env := DecodeEnvelope(t, ToolCall{Tool: ToolCommit, Path: pathAgent}, res)
	wantMsg := "docs is read-only in this server. Your changes there were discarded and the files reset. Write outside the read-only paths, then commit again."
	if env.Message != wantMsg {
		t.Fatalf("refusal message = %q, want %q", env.Message, wantMsg)
	}

	AssertVisibleFiles(t, agent, map[string]string{
		"docs/faq.md": "answer: 1\n",
		"notes/a.md":  "y\n",
	})
	if got := agent.Manifest().Generation; got != generationBefore {
		t.Fatalf("manifest generation changed by the refused commit: %d -> %d", generationBefore, got)
	}
	if got := len(agent.ListObjects("packs/")); got != packsBefore {
		t.Fatalf("pack namespace changed by the refused commit: %d -> %d objects", packsBefore, got)
	}

	res2 := agent.Commit("", "publish the rest")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "publish the rest",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "notes/a.md", Insertions: 1, Deletions: 1}},
			},
			ReadOnly: []string{"docs"},
		},
	}, res2)
	AssertVisibleFiles(t, agent, map[string]string{
		"docs/faq.md": "answer: 1\n",
		"notes/a.md":  "y\n",
	})
}

// TestScenarioReadOnlyAddAndDeleteRefused: adding or deleting under a
// read-only path is a violation; the add is dropped and the delete restored.
func TestScenarioReadOnlyAddAndDeleteRefused(t *testing.T) {
	t.Parallel()
	writer, _ := SeedReadOnlyBaseline(t)
	agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs"})
	agent.AssertOK(t, agent.Pull(""))

	agent.WriteFile(pathAgent+"/docs/new.md", "n\n")
	agent.RemoveFile(pathAgent + "/docs/faq.md")

	res := agent.Commit("", "add and delete under docs")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "add and delete under docs",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files: []FileExpectation{
				{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
				{Path: "docs/new.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
			},
			ReadOnly: []string{"docs"},
		},
	}, res)
	AssertVisibleFiles(t, agent, map[string]string{
		"docs/faq.md": "answer: 1\n",
		"notes/a.md":  "x\n",
	})
}

// TestScenarioReadOnlyMultipleViolatedEntries: violating two entries in one
// commit names both, sorted, with the plural message.
func TestScenarioReadOnlyMultipleViolatedEntries(t *testing.T) {
	t.Parallel()
	writer, pathWriter := SeedReadOnlyBaseline(t)
	// Publish a root faq.md so the second entry has something to violate.
	writer.WriteFile(pathWriter+"/faq.md", "root: 1\n")
	writer.AssertOK(t, writer.Commit("", "add root faq"))

	agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs", "faq.md"})
	agent.AssertOK(t, agent.Pull(""))

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/faq.md", "root: 2\n")

	res := agent.Commit("", "two violations")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "two violations",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Action:    "EDIT_FILES",
			Retryable: new(false),
			Files: []FileExpectation{
				{Path: "docs/faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
				{Path: "faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}},
			},
			ReadOnly: []string{"docs", "faq.md"},
		},
	}, res)
	env := DecodeEnvelope(t, ToolCall{Tool: ToolCommit, Path: pathAgent}, res)
	wantMsg := "docs, faq.md are read-only in this server. Your changes there were discarded and the files reset. Write outside the read-only paths, then commit again."
	if env.Message != wantMsg {
		t.Fatalf("refusal message = %q, want %q", env.Message, wantMsg)
	}

	AssertVisibleFiles(t, agent, map[string]string{
		"docs/faq.md": "answer: 1\n",
		"faq.md":      "root: 1\n",
		"notes/a.md":  "x\n",
	})
}

// TestScenarioReadOnlyCaseFoldedEntry: a case variant of a covered path is
// still covered and named as written.
func TestScenarioReadOnlyCaseFoldedEntry(t *testing.T) {
	t.Parallel()
	writer, _ := SeedReadOnlyBaseline(t)
	agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs"})
	agent.AssertOK(t, agent.Pull(""))

	agent.WriteFile(pathAgent+"/Docs/x.md", "n\n")
	res := agent.Commit("", "case variant path")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "case variant path",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "Docs/x.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"docs"},
		},
	}, res)
	AssertVisibleFiles(t, agent, map[string]string{
		"docs/faq.md": "answer: 1\n",
		"notes/a.md":  "x\n",
	})
}

// TestScenarioReadOnlyPullRestores: pull restores covered files and keeps
// other local edits, for an existing and a fresh workspace
// (architecture/pull.md).
func TestScenarioReadOnlyPullRestores(t *testing.T) {
	t.Parallel()
	writer, _ := SeedReadOnlyBaseline(t)

	t.Run("restores after local edit and keeps other changes", func(t *testing.T) {
		t.Parallel()
		agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs"})
		agent.AssertOK(t, agent.Pull(""))

		agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
		agent.WriteFile(pathAgent+"/notes/a.md", "y\n")

		putsBefore := agent.Recorder().Count(OpPut)
		replacesBefore := agent.Recorder().Count(OpReplace)
		res := agent.Pull("")
		agent.AssertEnvelope(t, ToolCall{
			Tool: ToolPull, Path: pathAgent,
			Expect: CallExpectation{
				OK: true,
				Success: &SuccessExpectation{
					Generation: 1, FilesChanged: 1, Insertions: 1, Deletions: 1,
					Files: []FileStatExpectation{{Path: "docs/faq.md", Insertions: 1, Deletions: 1}},
				},
				ReadOnly: []string{"docs"},
			},
		}, res)
		AssertVisibleFiles(t, agent, map[string]string{
			"docs/faq.md": "answer: 1\n",
			"notes/a.md":  "y\n",
		})
		if got := agent.Recorder().Count(OpPut) - putsBefore; got != 0 {
			t.Fatalf("pull restore made %d PUT calls, want none", got)
		}
		if got := agent.Recorder().Count(OpReplace) - replacesBefore; got != 0 {
			t.Fatalf("pull restore made %d manifest replace calls, want none", got)
		}
	})

	t.Run("fresh workspace restores pre-existing covered files on first pull", func(t *testing.T) {
		t.Parallel()
		agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs"})
		agent.WriteFile(pathAgent+"/docs/faq.md", "local only\n")

		res := agent.Pull("")
		agent.AssertEnvelope(t, ToolCall{
			Tool: ToolPull, Path: pathAgent,
			Expect: CallExpectation{OK: true, ReadOnly: []string{"docs"}},
		}, res)
		AssertVisibleFiles(t, agent, map[string]string{
			"docs/faq.md": "answer: 1\n",
			"notes/a.md":  "x\n",
		})
	})
}

// TestScenarioReadOnlyPullInvalidContentUnderEntry: an invalid file under a
// read-only path is INVALID_CONTENT on pull and commit; the restore does not run.
func TestScenarioReadOnlyPullInvalidContentUnderEntry(t *testing.T) {
	t.Parallel()
	writer, _ := SeedReadOnlyBaseline(t)
	agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs"})
	agent.AssertOK(t, agent.Pull(""))

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.WriteFile(pathAgent+"/docs/blob", "\x00\x01binary")

	invalid := CallExpectation{
		ErrorCode: "INVALID_REQUEST",
		Reason:    "INVALID_CONTENT",
		Action:    "EDIT_FILES",
		Retryable: new(false),
		Files:     []FileExpectation{{Path: "docs/blob", Reason: "INVALID_CONTENT", Ranges: []RangeExpectation{}}},
		ReadOnly:  []string{"docs"},
	}
	agent.AssertEnvelope(t, ToolCall{Tool: ToolPull, Path: pathAgent, Expect: invalid}, agent.Pull(""))
	agent.AssertEnvelope(t, ToolCall{Tool: ToolCommit, Path: pathAgent, Message: "m", Expect: invalid}, agent.Commit("", "m"))
	if got := agent.ReadFile(pathAgent + "/docs/faq.md"); got != "answer: 2\n" {
		t.Fatalf("docs/faq.md after the refused calls = %q, want the untouched local edit", got)
	}

	agent.RemoveFile(pathAgent + "/docs/blob")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 1, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "docs/faq.md", Insertions: 1, Deletions: 1}},
			},
			ReadOnly: []string{"docs"},
		},
	}, agent.Pull(""))
	AssertVisibleFiles(t, agent, map[string]string{
		"docs/faq.md": "answer: 1\n",
		"notes/a.md":  "x\n",
	})
}

// TestScenarioReadOnlyWriterUpdatesFlowToAgent: a writer's update to a
// read-only path reaches the agent by a conflict-free pull.
func TestScenarioReadOnlyWriterUpdatesFlowToAgent(t *testing.T) {
	t.Parallel()
	writer, pathWriter := SeedReadOnlyBaseline(t)
	agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs"})
	agent.AssertOK(t, agent.Pull(""))

	writer.WriteFile(pathWriter+"/docs/faq.md", "answer: 3\n")
	writer.AssertOK(t, writer.Commit("", "writer update"))

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 2, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "docs/faq.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Pull(""))
	if got := agent.ReadFile(pathAgent + "/docs/faq.md"); got != "answer: 3\n" {
		t.Fatalf("agent docs/faq.md after the writer's update = %q, want answer: 3", got)
	}

	agent.WriteFile(pathAgent+"/notes/a.md", "y\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "notes edit",
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 3, FilesChanged: 1, Insertions: 1, Deletions: 1,
				Files: []FileStatExpectation{{Path: "notes/a.md", Insertions: 1, Deletions: 1}},
			},
		},
	}, agent.Commit("", "notes edit"))
}

// TestScenarioReadOnlyFileEntry: an entry naming one root file protects exactly it.
func TestScenarioReadOnlyFileEntry(t *testing.T) {
	t.Parallel()
	writer := NewFakeHarness(t, HarnessConfig{})
	pathWriter := writer.WorkspaceRoot()
	writer.WriteFile(pathWriter+"/faq.md", "root: 1\n")
	writer.WriteFile(pathWriter+"/notes/a.md", "x\n")
	writer.AssertOK(t, writer.Pull(""))
	writer.AssertOK(t, writer.Commit("", "seed"))

	agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"faq.md"})
	agent.AssertOK(t, agent.Pull(""))

	agent.WriteFile(pathAgent+"/faq.md", "root: 2\n")
	res := agent.Commit("", "edit root file")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "edit root file",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: "faq.md", Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
			ReadOnly:  []string{"faq.md"},
		},
	}, res)
	AssertVisibleFiles(t, agent, map[string]string{
		"faq.md":     "root: 1\n",
		"notes/a.md": "x\n",
	})
}

// TestScenarioReadOnlyAdvertised: instructions, both descriptions, and both
// envelopes name the same normalized entries.
func TestScenarioReadOnlyAdvertised(t *testing.T) {
	t.Parallel()
	writer, _ := SeedReadOnlyBaseline(t)
	agent, pathAgent := NewReadOnlyAgent(t, writer, []string{"docs", "faq.md"})

	client := agent.Client("")
	instructions := client.InitializeResult().Instructions
	wantInstrSuffix := " Read-only paths: docs, faq.md. notes_commit refuses any change under them, resets those files, and reports READ_ONLY_PATH; write elsewhere."
	if !strings.HasSuffix(instructions, wantInstrSuffix) {
		t.Fatalf("instructions = %q, want it to end with %q", instructions, wantInstrSuffix)
	}

	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	wantDescSuffix := " Read-only paths in this server: docs, faq.md; changes under them are refused and reset."
	for _, tool := range res.Tools {
		if !strings.HasSuffix(tool.Description, wantDescSuffix) {
			t.Fatalf("%s description = %q, want it to end with %q", tool.Name, tool.Description, wantDescSuffix)
		}
	}

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathAgent,
		Expect: CallExpectation{OK: true, ReadOnly: []string{"docs", "faq.md"}},
	}, agent.Pull(""))

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "docs edit",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			ReadOnly:  []string{"docs", "faq.md"},
		},
	}, agent.Commit("", "docs edit"))
}

// TestScenarioReadOnlyEmptySetUnchanged: with no set, every surface is
// unchanged except the always-present empty readOnly array.
func TestScenarioReadOnlyEmptySetUnchanged(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	path := h.WorkspaceRoot()

	client := h.Client("")
	instructions := client.InitializeResult().Instructions
	if strings.Contains(strings.ToLower(instructions), "read-only") {
		t.Fatalf("instructions with no read-only set = %q, want no read-only mention", instructions)
	}
	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	for _, tool := range res.Tools {
		if strings.Contains(strings.ToLower(tool.Description), "read-only") {
			t.Fatalf("%s description with no read-only set = %q, want no read-only mention", tool.Name, tool.Description)
		}
	}

	h.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: path, Message: "too soon",
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "PULL_REQUIRED",
			ReadOnly:  []string{},
		},
	}, h.Commit("", "too soon"))

	h.WriteFile(path+"/a.md", "hello")
	h.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: path,
		Expect: CallExpectation{OK: true, ReadOnly: []string{}},
	}, h.Pull(""))
}

// TestScenarioReadOnlyResetFailureIsRecovery: a failure inside the reset is
// RECOVERY_FAILURE at stage commit.readonly (architecture/guarantees.md).
func TestScenarioReadOnlyResetFailureIsRecovery(t *testing.T) {
	t.Parallel()
	writer, _ := SeedReadOnlyBaseline(t)
	agent := NewSharedHarness(t, writer.Raw(), writer.Cfg.Prefix, HarnessConfig{
		ReadOnlyPaths: []string{"docs"},
		Hooks:         &app.ServiceHooks{Workspace: &workspace.Failpoints{}},
	})
	pathAgent := agent.WorkspaceRoot()
	agent.AssertOK(t, agent.Pull(""))

	agent.WriteFile(pathAgent+"/docs/faq.md", "answer: 2\n")

	// The failpoint fires once, so the recovery's own resync succeeds.
	var fired atomic.Bool
	agent.WorkspaceFailpoints().Replace = func() error {
		if !fired.CompareAndSwap(false, true) {
			return nil
		}
		return errors.New("injected read-only reset failure")
	}

	res := agent.Commit("", "docs edit")
	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: "docs edit",
		Expect: CallExpectation{
			ErrorCode: CodeRecoveryFailure,
			Retryable: new(true),
			Recovery: &RecoveryExpectation{
				Stage:          "commit.readonly",
				RemoteAccepted: "no",
				Resynchronized: new(true),
			},
		},
	}, res)
	if got := agent.ReadFile(pathAgent + "/docs/faq.md"); got != "answer: 1\n" {
		t.Fatalf("docs/faq.md after recovery = %q, want the resynchronized baseline", got)
	}
	if got := agent.Manifest().Generation; got != 1 {
		t.Fatalf("manifest generation after the recovery = %d, want 1 unchanged", got)
	}
}

// TestScenarioReadOnlyFlagProcess: --read-only-paths reaches the envelope,
// text item, and instructions of a spawned serve process.
func TestScenarioReadOnlyFlagProcess(t *testing.T) {
	t.Parallel()
	h := SpawnHelper(t, "fake", nil, "serve", "--read-only-paths=docs,faq.md")
	cs := h.ConnectClient(t)

	instructions := cs.InitializeResult().Instructions
	if !strings.Contains(instructions, "docs, faq.md") {
		t.Fatalf("instructions = %q, want it to name the configured read-only entries", instructions)
	}

	path := h.WorkspaceRoot
	got := AssertProcessCallOK(t, cs, ToolPull, path, "")
	if want := []string{"docs", "faq.md"}; !reflect.DeepEqual(got.ReadOnly, want) {
		t.Fatalf("readOnly = %v, want %v", got.ReadOnly, want)
	}
	h.CloseClient(t, cs)
}

// TestScenarioReadOnlyEnvPrecedence: the environment variable configures a
// spawned process and an explicitly empty flag clears it
// (architecture/config.md).
func TestScenarioReadOnlyEnvPrecedence(t *testing.T) {
	t.Parallel()
	t.Run("environment alone configures the set", func(t *testing.T) {
		t.Parallel()
		h := SpawnHelper(t, "fake", []string{"SLIVINGDOC_READ_ONLY_PATHS=docs"}, "serve")
		cs := h.ConnectClient(t)
		path := h.WorkspaceRoot
		got := AssertProcessCallOK(t, cs, ToolPull, path, "")
		if want := []string{"docs"}; !reflect.DeepEqual(got.ReadOnly, want) {
			t.Fatalf("readOnly = %v, want %v", got.ReadOnly, want)
		}
		h.CloseClient(t, cs)
	})

	t.Run("explicitly empty flag beats the inherited environment value", func(t *testing.T) {
		t.Parallel()
		h := SpawnHelper(t, "fake", []string{"SLIVINGDOC_READ_ONLY_PATHS=docs"}, "serve", "--read-only-paths=")
		cs := h.ConnectClient(t)
		path := h.WorkspaceRoot
		got := AssertProcessCallOK(t, cs, ToolPull, path, "")
		if len(got.ReadOnly) != 0 {
			t.Fatalf("readOnly = %v, want the empty set, not the inherited environment value", got.ReadOnly)
		}
		h.CloseClient(t, cs)
	})
}

// TestScenarioReadOnlyInvalidFlagRefusesStartup: an invalid entry refuses
// startup before any backend call (architecture/config.md).
func TestScenarioReadOnlyInvalidFlagRefusesStartup(t *testing.T) {
	t.Parallel()
	for _, row := range []struct{ name, value string }{
		{name: "dot-dot segment", value: ".."},
		{name: "absolute path", value: "/abs"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := SpawnHelper(t, "fake", nil, "serve", "--read-only-paths="+row.value)
			code, stdout, stderr := h.RunStdioProcess(t, nil)
			AssertOneRedactedDiagnostic(t, code, stdout, stderr)
			if !strings.Contains(stderr, "read-only paths:") {
				t.Fatalf("stderr = %q, want the read-only paths refusal prefix", stderr)
			}
			if !strings.Contains(stderr, row.value) {
				t.Fatalf("stderr = %q, want it to name the entry %q", stderr, row.value)
			}
		})
	}
}
