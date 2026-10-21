package sync

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/storage"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioErrorTaxonomy proves every tool-level category reaches an MCP
// caller as the stable, complete envelope. INCOMPATIBLE_STORE is necessarily
// a process-startup category and is covered by
// TestScenarioIntegrityStartupProbeFailure (architecture/product-contract.md).
func TestScenarioErrorTaxonomy(t *testing.T) {
	t.Parallel()
	type outcome struct {
		h       *Harness
		call    ToolCall
		result  *sdk.CallToolResult
		code    string
		reason  string
		action  string
		retry   bool
		recover bool
	}
	rows := []struct {
		name string
		run  func(t *testing.T) outcome
	}{
		{
			name: "invalid request",
			run: func(t *testing.T) outcome {
				h := NewFakeHarness(t, HarnessConfig{})
				path := h.WorkspaceRoot()
				call := ToolCall{Tool: ToolCommit, Path: path, Message: "without pull"}
				return outcome{h: h, call: call, result: h.Commit("", call.Message), code: "INVALID_REQUEST", reason: "PULL_REQUIRED", action: "PULL"}
			},
		},
		{
			name: "content conflict",
			run: func(t *testing.T) outcome {
				h := NewFakeHarness(t, HarnessConfig{})
				path := h.WorkspaceRoot()
				h.AssertOK(t, h.Pull(""))
				h.WriteFile(path+"/conflict.md", "<<<<<<< local\nleft\n=======\nright\n>>>>>>> remote\n")
				call := ToolCall{Tool: ToolCommit, Path: path, Message: "markers"}
				return outcome{h: h, call: call, result: h.Commit("", call.Message), code: "CONTENT_CONFLICT", reason: "UNRESOLVED_MARKERS", action: "EDIT_FILES"}
			},
		},
		{
			name: "remote busy",
			run: func(t *testing.T) outcome {
				h := NewFakeHarness(t, HarnessConfig{RetryLimit: new(0)})
				path := h.WorkspaceRoot()
				CommitFirst(t, h, "a.md", "alpha", "first")
				h.WriteFile(path+"/b.md", "beta")
				h.Faults().FailAlways(OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
				call := ToolCall{Tool: ToolCommit, Path: path, Message: "busy"}
				return outcome{h: h, call: call, result: h.Commit("", call.Message), code: "REMOTE_BUSY", reason: "RETRIES_EXHAUSTED", action: "RETRY", retry: true}
			},
		},
		{
			name: "storage failure",
			run: func(t *testing.T) outcome {
				h := NewFakeHarness(t, HarnessConfig{})
				path := h.WorkspaceRoot()
				CommitFirst(t, h, "a.md", "alpha", "first")
				h.WriteFile(path+"/b.md", "beta")
				// An unlanded CAS with a successful lookup miss is PUBLICATION_UNPROVEN.
				h.Faults().FailNext(OpReplace, storage.CurrentKey, storage.ErrTransport)
				call := ToolCall{Tool: ToolCommit, Path: path, Message: "unknown acceptance"}
				return outcome{h: h, call: call, result: h.Commit("", call.Message), code: "STORAGE_FAILURE", reason: "PUBLICATION_UNPROVEN", action: "PULL", retry: true}
			},
		},
		{
			name: "storage integrity",
			run: func(t *testing.T) outcome {
				h := NewFakeHarness(t, HarnessConfig{})
				path := h.WorkspaceRoot()
				CommitFirst(t, h, "a.md", "alpha", "first")
				h.Faults().CorruptRead(storage.CurrentKey)
				call := ToolCall{Tool: ToolPull, Path: path}
				return outcome{h: h, call: call, result: h.Pull(""), code: "STORAGE_INTEGRITY", reason: "MANIFEST_INVALID", action: "OPERATOR"}
			},
		},
		{
			name: "recovery failure",
			run: func(t *testing.T) outcome {
				h := NewRecoveryHarness(t)
				path := h.WorkspaceRoot()
				h.AssertOK(t, h.Pull(""))
				h.WriteFile(path+"/a.md", "alpha")
				h.NotebookFailpoints().CAS = func() error { return errors.New("injected recovery") }
				call := ToolCall{Tool: ToolCommit, Path: path, Message: "recover"}
				return outcome{h: h, call: call, result: h.Commit("", call.Message), code: CodeRecoveryFailure, reason: "LOCAL_MUTATION_FAILED", action: "PULL", retry: true, recover: true}
			},
		},
		{
			// The store refuses the resynchronizing read: the call stays a
			// RECOVERY_FAILURE but carries the refusal's reason and action,
			// and is not retryable.
			name: "recovery refused by the store",
			run: func(t *testing.T) outcome {
				h := NewRecoveryHarness(t)
				path := h.WorkspaceRoot()
				h.AssertOK(t, h.Pull(""))
				h.WriteFile(path+"/a.md", "alpha")
				h.NotebookFailpoints().CAS = func() error {
					h.Faults().FailNext(OpGet, storage.CurrentKey, &storage.Refusal{Err: storage.ErrAccessDenied, Detail: "HTTP 403 forbidden"})
					return errors.New("injected recovery")
				}
				call := ToolCall{Tool: ToolCommit, Path: path, Message: "recover"}
				return outcome{h: h, call: call, result: h.Commit("", call.Message), code: CodeRecoveryFailure, reason: "ACCESS_DENIED", action: "OPERATOR", retry: false, recover: true}
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			out := row.run(t)
			env := DecodeEnvelope(t, out.call, out.result)
			text, ok := out.result.Content[0].(*sdk.TextContent)
			if !ok {
				t.Fatalf("error content = %#v, want text", out.result.Content[0])
			}
			for _, want := range []string{out.code + " · " + out.reason, "action: " + out.action, "retryable:"} {
				if !strings.Contains(text.Text, want) {
					t.Fatalf("error text = %q, want %q", text.Text, want)
				}
			}
			if env.Code != out.code || env.Retryable != out.retry {
				t.Fatalf("envelope = %+v, want code=%s retryable=%v", env, out.code, out.retry)
			}
			if env.Reason != out.reason || env.Action != out.action {
				t.Fatalf("envelope = %+v, want reason=%s action=%s", env, out.reason, out.action)
			}
			if (env.Recovery != nil) != out.recover {
				t.Fatalf("recovery field = %+v, want present=%v", env.Recovery, out.recover)
			}
			for _, file := range env.Files {
				if filepath.IsAbs(file.Path) || filepath.ToSlash(filepath.Clean(file.Path)) != file.Path {
					t.Fatalf("error file path = %q, want normalized relative path", file.Path)
				}
			}
			AssertTaxonomyRedaction(t, out.h, out.result)
		})
	}
}
