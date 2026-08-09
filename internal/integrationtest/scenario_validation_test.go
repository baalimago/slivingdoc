package integrationtest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioToolListing proves the acceptance criterion that the tool
// listing advertises exactly the two public tools and no third tool,
// prompt, or resource (architecture section 2, L26).
func TestScenarioToolListing(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	res, err := h.Client("").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("tools = %d, want exactly 2", len(res.Tools))
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{toolPull, toolCommit} {
		if !names[name] {
			t.Fatalf("tool %q missing from the listing", name)
		}
	}
}

// TestScenarioStrictSchema proves that every invalid input shape maps to
// the INVALID_REQUEST envelope before any Git or S3 work (architecture
// section 2, L26).
//
// The path is pulled first, so it is a managed notebook for the rest of the
// test. Without that, every notes_commit row would also be a
// commit-without-pull request and the rejection could not be attributed to
// the message rule under test. The store counters are compared against the
// post-pull snapshot, which is the "no further access" evidence.
func TestScenarioStrictSchema(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	h.assertOK(t, h.Pull("", path))
	before := h.Recorder().Snapshot()

	oversizedPath := "/" + strings.Repeat("a", 4096) // 4,097 bytes
	rows := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"unknown field", toolPull, map[string]any{"path": path, "extra": 1}},
		{"null path", toolPull, map[string]any{"path": nil}},
		{"relative path", toolPull, map[string]any{"path": "notes"}},
		{"oversized path", toolPull, map[string]any{"path": oversizedPath}},
		{"missing message", toolCommit, map[string]any{"path": path}},
		{"oversized message", toolCommit, map[string]any{"path": path, "message": strings.Repeat("m", 16385)}},
		{"blank message", toolCommit, map[string]any{"path": path, "message": " \t "}},
		{"nul in path", toolPull, map[string]any{"path": path + "\x00"}},
		{"nul in message", toolCommit, map[string]any{"path": path, "message": "m\x00"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			res := callWithArgs(t, h, row.name, row.tool, row.args)
			h.assertEnvelope(t, ToolCall{
				Tool:   row.tool,
				Path:   path,
				Expect: CallExpectation{ErrorCode: "INVALID_REQUEST"},
			}, res)
		})
	}

	// The strict decode runs before any service or store access: no counter
	// moved past the setup pull, and nothing new was written to disk.
	after := h.Recorder().Snapshot()
	for _, op := range AllOps {
		if after[op] != before[op] {
			t.Fatalf("rejected requests reached the store: %s %d -> %d", op, before[op], after[op])
		}
	}
	if got := h.FSSnapshot(path); len(got) != 0 {
		t.Fatalf("rejected requests wrote into the notebook: %v", got)
	}
}

// TestScenarioContentRules proves the notebook content rule through the
// public API: the visible directory may hold valid UTF-8 text without
// U+0000 only, and a file that breaks the rule is refused as
// INVALID_REQUEST without publishing anything (architecture section 7.1,
// L188).
//
// This is where the UTF-8 contract is observable. A malformed byte in a
// REQUEST cannot be tested through MCP at all: JSON transport coerces
// invalid bytes to U+FFFD, so the server never sees them. File bytes are
// read from disk and reach the server exactly as written.
func TestScenarioContentRules(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		data string
	}{
		{name: "invalid utf8", data: "text \xff\xfe more"},
		{name: "nul byte", data: "text \x00 more"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := newFakeHarness(t, HarnessConfig{})
			path := h.Path("notes")
			h.assertOK(t, h.Pull("", path))
			before := h.Recorder().Snapshot()

			h.WriteFile(path+"/bad.md", row.data)
			res := h.Commit("", path, "publish an unsupported file")
			h.assertEnvelope(t, ToolCall{
				Tool: toolCommit, Path: path, Message: "publish an unsupported file",
				Expect: CallExpectation{ErrorCode: "INVALID_REQUEST", Retryable: new(false)},
			}, res)

			// Nothing was published, and the caller's file is left alone for
			// them to fix.
			after := h.Recorder().Snapshot()
			for _, op := range []Op{OpPut, OpCreate, OpReplace, OpDelete} {
				if after[op] != before[op] {
					t.Fatalf("an unsupported file mutated the store: %s %d -> %d", op, before[op], after[op])
				}
			}
			if got := h.ReadFile(path + "/bad.md"); got != row.data {
				t.Fatalf("L after the refusal = %q, want the caller's bytes untouched", got)
			}
		})
	}
}

// TestScenarioResultShape proves the success envelope of both tools: one
// text item exactly OK, no structured content, and no commit ID, pack key,
// or internal value anywhere in the result (architecture section 2, L26).
func TestScenarioResultShape(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "first commit")

	// The success envelope is exactly one text item "OK" with no structured
	// content, which assertOK enforces. That alone rules out a commit ID or
	// pack key in the result: there is nowhere for one to be. The scan below
	// is the belt-and-braces check that the single text item is the literal
	// OK and carries no Git object ID or key.
	h.WriteFile(path+"/b.md", "beta")
	for _, res := range []*sdk.CallToolResult{
		h.Pull("", path),
		h.Commit("", path, "second"),
	} {
		h.assertOK(t, res)
		text := res.Content[0].(*sdk.TextContent).Text
		if text != "OK" {
			t.Fatalf("success text = %q, want exactly OK", text)
		}
		if gitObjectID.MatchString(text) || strings.Contains(text, "packs/") {
			t.Fatalf("success text leaks an internal value: %q", text)
		}
	}
	// The accepted state really advanced, so the bare OK is not the result
	// of the calls having done nothing.
	if m := h.Manifest(); m.Generation != 2 {
		t.Fatalf("manifest generation = %d, want the second publication", m.Generation)
	}
}

// TestScenarioMalformedToolJSON proves that malformed tool JSON never
// reaches the handler: the SDK reports a protocol-level error and the
// store stays untouched (a mirror of the mcp-level test through the
// harness). A dedicated session isolates the frame-level failure.
func TestScenarioMalformedToolJSON(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	if err := callProtocolError(t, h, "malformed", toolPull, json.RawMessage(`{"path":`)); err == nil {
		t.Fatal("CallTool(malformed JSON) = nil, want a protocol error")
	}
	h.assertExpectations(t, Expectations{
		S3: S3Assertions{Counts: &CountExpectation{AllZero: true}},
	})
}

// TestScenarioMCPReqIDScoping proves the mcpReqID log-attr contract: every
// tool-call record of one harness carries its own request ID and tool, two
// parallel harnesses keep their request IDs disjoint, and no capture leaks
// another harness's records.
func TestScenarioMCPReqIDScoping(t *testing.T) {
	t.Parallel()
	h1 := newFakeHarness(t, HarnessConfig{})
	h2 := newFakeHarness(t, HarnessConfig{})
	commitFirst(t, h1, h1.Path("notes"), "a.md", "one", "h1 commit")
	commitFirst(t, h2, h2.Path("notes"), "b.md", "two", "h2 commit")

	ids1 := h1.Logs().DistinctReqIDs()
	ids2 := h2.Logs().DistinctReqIDs()
	if len(ids1) == 0 || len(ids2) == 0 {
		t.Fatalf("distinct request ids: h1=%v h2=%v, want at least one each", ids1, ids2)
	}
	seen := map[string]bool{}
	for _, id := range ids1 {
		seen[id] = true
	}
	for _, id := range ids2 {
		if seen[id] {
			t.Fatalf("request id %q is shared between the two harnesses", id)
		}
	}
	for _, r := range h1.Logs().ToolCalls() {
		if r.Attrs["mcpReqID"] == nil || r.Attrs["mcpReqID"] == "" || r.Attrs["tool"] == nil {
			t.Fatalf("tool-call record lacks mcpReqID or tool attrs: %+v", r)
		}
	}
	for _, id := range ids2 {
		if strings.Contains(h1.Logs().String(), id) {
			t.Fatalf("harness 1 capture leaks harness 2 request id %q", id)
		}
	}
	for _, id := range ids1 {
		if strings.Contains(h2.Logs().String(), id) {
			t.Fatalf("harness 2 capture leaks harness 1 request id %q", id)
		}
	}
	// The completion record of a failed call carries the same request ID;
	// the error taxonomy proves that through the envelope shape.
	h3 := newFakeHarness(t, HarnessConfig{
		Hooks: &app.ServiceHooks{
			Workspace: &workspace.Failpoints{},
			Notebook:  &notebook.Failpoints{},
		},
	})
	path := h3.Path("notes")
	commitFirst(t, h3, path, "a.md", "alpha", "first")
	h3.WriteFile(path+"/b.md", "beta")
	h3.NotebookFailpoints().CAS = func() error { return fmt.Errorf("injected cas failure") }
	res := h3.Commit("", path, "second")
	h3.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: path, Message: "second",
		Expect: CallExpectation{
			ErrorCode: codeRecoveryFailure,
			Retryable: new(true),
			Recovery: &RecoveryExpectation{
				Stage:          "commit.cas",
				RemoteAccepted: "yes",
				Resynchronized: new(true),
			},
		},
	}, res)
	if got := h3.Logs().DistinctReqIDs(); len(got) != 3 {
		t.Fatalf("harness 3 distinct request ids = %v, want the three calls", got)
	}
}
