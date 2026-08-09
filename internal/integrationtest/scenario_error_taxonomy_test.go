package integrationtest

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioErrorTaxonomy proves every tool-level category reaches an MCP
// caller as the stable, complete envelope. INCOMPATIBLE_STORE is necessarily
// a process-startup category and is covered by
// TestScenarioIntegrityStartupProbeFailure (architecture section 2, L26).
func TestScenarioErrorTaxonomy(t *testing.T) {
	t.Parallel()
	type outcome struct {
		h       *Harness
		call    ToolCall
		result  *sdk.CallToolResult
		code    string
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
				h := newFakeHarness(t, HarnessConfig{})
				path := h.Path("notes")
				call := ToolCall{Tool: toolCommit, Path: path, Message: "without pull"}
				return outcome{h: h, call: call, result: h.Commit("", path, call.Message), code: "INVALID_REQUEST"}
			},
		},
		{
			name: "content conflict",
			run: func(t *testing.T) outcome {
				h := newFakeHarness(t, HarnessConfig{})
				path := h.Path("notes")
				h.assertOK(t, h.Pull("", path))
				h.WriteFile(path+"/conflict.md", "<<<<<<< local\nleft\n=======\nright\n>>>>>>> remote\n")
				call := ToolCall{Tool: toolCommit, Path: path, Message: "markers"}
				return outcome{h: h, call: call, result: h.Commit("", path, call.Message), code: "CONTENT_CONFLICT"}
			},
		},
		{
			name: "remote busy",
			run: func(t *testing.T) outcome {
				h := newFakeHarness(t, HarnessConfig{RetryLimit: new(0)})
				path := h.Path("notes")
				commitFirst(t, h, path, "a.md", "alpha", "first")
				h.WriteFile(path+"/b.md", "beta")
				h.Faults().FailAlways(OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
				call := ToolCall{Tool: toolCommit, Path: path, Message: "busy"}
				return outcome{h: h, call: call, result: h.Commit("", path, call.Message), code: "REMOTE_BUSY", retry: true}
			},
		},
		{
			name: "storage failure",
			run: func(t *testing.T) outcome {
				h := newFakeHarness(t, HarnessConfig{})
				path := h.Path("notes")
				commitFirst(t, h, path, "a.md", "alpha", "first")
				h.WriteFile(path+"/b.md", "beta")
				h.Faults().UnprovableNext(storage.CurrentKey)
				call := ToolCall{Tool: toolCommit, Path: path, Message: "unknown acceptance"}
				return outcome{h: h, call: call, result: h.Commit("", path, call.Message), code: "STORAGE_FAILURE", retry: true}
			},
		},
		{
			name: "storage integrity",
			run: func(t *testing.T) outcome {
				h := newFakeHarness(t, HarnessConfig{})
				path := h.Path("notes")
				commitFirst(t, h, path, "a.md", "alpha", "first")
				h.Faults().CorruptRead(storage.CurrentKey)
				call := ToolCall{Tool: toolPull, Path: path}
				return outcome{h: h, call: call, result: h.Pull("", path), code: "STORAGE_INTEGRITY"}
			},
		},
		{
			name: "recovery failure",
			run: func(t *testing.T) outcome {
				h := newRecoveryHarness(t)
				path := h.Path("notes")
				h.assertOK(t, h.Pull("", path))
				h.WriteFile(path+"/a.md", "alpha")
				h.NotebookFailpoints().CAS = func() error { return errors.New("injected recovery") }
				call := ToolCall{Tool: toolCommit, Path: path, Message: "recover"}
				return outcome{h: h, call: call, result: h.Commit("", path, call.Message), code: codeRecoveryFailure, retry: true, recover: true}
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			out := row.run(t)
			env := decodeEnvelope(t, out.call, out.result)
			if env.Code != out.code || env.Retryable != out.retry {
				t.Fatalf("envelope = %+v, want code=%s retryable=%v", env, out.code, out.retry)
			}
			if (env.Recovery != nil) != out.recover {
				t.Fatalf("recovery field = %+v, want present=%v", env.Recovery, out.recover)
			}
			for _, file := range env.Files {
				if filepath.IsAbs(file.Path) || filepath.ToSlash(filepath.Clean(file.Path)) != file.Path {
					t.Fatalf("error file path = %q, want normalized relative path", file.Path)
				}
			}
			assertTaxonomyRedaction(t, out.h, out.result)
		})
	}
}

// gitObjectID matches a bare 40-hex Git object ID. Git history is internal
// (architecture section 12, L763), so no object ID may reach a caller.
var gitObjectID = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

// gitVocabulary is Git terminology that may never reach a caller. Recovery
// instructions are expressed in ordinary file terms: a caller resolves
// conflicts with a text editor and never runs Git. Each term is
// unambiguous — none of them occurs in ordinary slivingdoc prose.
var gitVocabulary = []string{"git ", "rebase", "merge-base", "refs/", "packfile", "checkout"}

// assertTaxonomyRedaction proves that neither the caller-facing envelope nor
// the harness's own log records leak an S3 key, a private path, or a Git
// object ID, and that no caller-facing text speaks Git (architecture
// section 2, L26).
//
// Credential redaction is proven where a credential actually exists: the
// in-process harness constructs its own store, so no secret ever reaches
// this configuration. TestScenarioConfigInvalidAndEarlyExit drives a real
// credential through the process startup path instead.
func assertTaxonomyRedaction(t *testing.T, h *Harness, res *sdk.CallToolResult) {
	t.Helper()
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal tool result: %v", err)
	}
	logs := h.Logs().String()
	for _, forbidden := range []string{h.PrivateRoot(), "packs/"} {
		if forbidden == "" {
			t.Fatal("redaction fixture is empty; the scan would be vacuous")
		}
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("tool result leaks %q: %s", forbidden, data)
		}
		if strings.Contains(logs, forbidden) {
			t.Fatalf("log capture leaks %q: %s", forbidden, logs)
		}
	}
	if id := gitObjectID.FindString(string(data)); id != "" {
		t.Fatalf("tool result leaks the Git object ID %q", id)
	}
	if id := gitObjectID.FindString(logs); id != "" {
		t.Fatalf("log capture leaks the Git object ID %q", id)
	}
	lower := strings.ToLower(string(data))
	for _, word := range gitVocabulary {
		if strings.Contains(lower, word) {
			t.Fatalf("caller-facing text uses the Git term %q: %s", word, data)
		}
	}
}
