package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioToolListing proves the acceptance criterion that the tool
// listing advertises exactly the two public tools, each bound to the
// server's one notebook directory: notes_pull takes no field, notes_commit
// takes only its message, and neither declares a per-call target
// (architecture/product-contract.md).
func TestScenarioToolListing(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	res, err := h.Client("").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("tools = %d, want exactly 2", len(res.Tools))
	}
	wantFields := map[string][]string{
		ToolPull:   {},
		ToolCommit: {"message"},
	}
	for _, tool := range res.Tools {
		want, ok := wantFields[tool.Name]
		if !ok {
			t.Fatalf("unexpected tool %q in the listing", tool.Name)
		}
		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("%s input schema marshal: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("%s input schema = %s: %v", tool.Name, data, err)
		}
		if len(schema.Properties) != len(want) {
			t.Fatalf("%s input schema properties = %v, want %v", tool.Name, schema.Properties, want)
		}
		for _, field := range want {
			if _, ok := schema.Properties[field]; !ok {
				t.Fatalf("%s input schema lacks %q: %s", tool.Name, field, data)
			}
		}
	}
}

// TestScenarioStrictSchema proves that every invalid input shape maps to
// the INVALID_REQUEST envelope before any Git or S3 work
// (architecture/product-contract.md). Structural decode failures carry
// MALFORMED_INPUT; the message rows carry the notebook.ValidateMessage
// reason. A present path of any JSON type is refused before an unknown
// member, and its refusal names --workspace-root without echoing the
// attempted path.
//
// The notebook is pulled first, so it is managed for the rest of the test.
// Without that, every notes_commit row is also a commit-without-pull
// request, and the rejection cannot be attributed to the message rule under
// test. The store counters are compared against the post-pull snapshot,
// which is the "no further access" evidence.
func TestScenarioStrictSchema(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	h.AssertOK(t, h.Pull(""))
	before := h.Recorder().Snapshot()

	outside := "/tmp/not-this-server"
	rows := []struct {
		name     string
		tool     string
		args     map[string]any
		reason   string
		wantText string
		noText   []string
	}{
		{name: "unknown field", tool: ToolPull, args: map[string]any{"extra": 1}, reason: "MALFORMED_INPUT"},
		{name: "path string", tool: ToolPull, args: map[string]any{"path": outside}, reason: "MALFORMED_INPUT", wantText: "--workspace-root", noText: []string{outside}},
		{name: "path null", tool: ToolPull, args: map[string]any{"path": nil}, reason: "MALFORMED_INPUT", wantText: "--workspace-root"},
		{name: "path number", tool: ToolPull, args: map[string]any{"path": 1}, reason: "MALFORMED_INPUT", wantText: "--workspace-root"},
		{name: "path bool", tool: ToolPull, args: map[string]any{"path": true}, reason: "MALFORMED_INPUT", wantText: "--workspace-root"},
		{name: "path array", tool: ToolPull, args: map[string]any{"path": []any{outside}}, reason: "MALFORMED_INPUT", wantText: "--workspace-root", noText: []string{outside}},
		{name: "path object", tool: ToolPull, args: map[string]any{"path": map[string]any{"path": outside}}, reason: "MALFORMED_INPUT", wantText: "--workspace-root", noText: []string{outside}},
		{name: "path beside unknown field", tool: ToolPull, args: map[string]any{"path": outside, "extra": 1}, reason: "MALFORMED_INPUT", wantText: "--workspace-root", noText: []string{outside}},
		{name: "missing message", tool: ToolCommit, args: map[string]any{}, reason: "MALFORMED_INPUT"},
		{name: "path on commit", tool: ToolCommit, args: map[string]any{"path": outside, "message": "m"}, reason: "MALFORMED_INPUT", wantText: "--workspace-root", noText: []string{outside}},
		{name: "null message", tool: ToolCommit, args: map[string]any{"message": nil}, reason: "MALFORMED_INPUT"},
		{name: "non-string message", tool: ToolCommit, args: map[string]any{"message": 1}, reason: "MALFORMED_INPUT"},
		{name: "oversized message", tool: ToolCommit, args: map[string]any{"message": strings.Repeat("m", 16385)}, reason: "MESSAGE_TOO_LONG"},
		{name: "blank message", tool: ToolCommit, args: map[string]any{"message": " \t "}, reason: "MESSAGE_BLANK"},
		{name: "nul in message", tool: ToolCommit, args: map[string]any{"message": "m\x00"}, reason: "MESSAGE_INVALID"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			call := ToolCall{
				Tool: row.tool,
				Expect: CallExpectation{
					ErrorCode: "INVALID_REQUEST", Reason: row.reason, Action: "FIX_INPUT", NoText: row.noText,
				},
			}
			res := CallWithArgs(t, h, row.name, row.tool, row.args)
			h.AssertEnvelope(t, call, res)
			if row.wantText != "" {
				if env := DecodeEnvelope(t, call, res); !strings.Contains(env.Message, row.wantText) {
					t.Fatalf("refusal message = %q, want it to name %q", env.Message, row.wantText)
				}
			}
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
	if got := h.FSSnapshot(h.WorkspaceRoot()); len(got) != 0 {
		t.Fatalf("rejected requests wrote into the notebook: %v", got)
	}
}

// TestScenarioContentRules proves the notebook content rule through the
// public API: the visible directory can hold valid UTF-8 text without
// U+0000 only, and a file that breaks the rule is refused as
// INVALID_REQUEST without publishing anything (architecture/workspace.md).
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
			h := NewFakeHarness(t, HarnessConfig{})
			path := h.WorkspaceRoot()
			h.AssertOK(t, h.Pull(""))
			before := h.Recorder().Snapshot()

			h.WriteFile(path+"/bad.md", row.data)
			res := h.Commit("", "publish an unsupported file")
			h.AssertEnvelope(t, ToolCall{
				Tool: ToolCommit, Path: path, Message: "publish an unsupported file",
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

	// A binary file and a symlink must name the offending file with INVALID_CONTENT.
	t.Run("binary file", func(t *testing.T) {
		t.Parallel()
		h := NewFakeHarness(t, HarnessConfig{})
		path := h.WorkspaceRoot()
		h.AssertOK(t, h.Pull(""))
		before := h.Recorder().Snapshot()

		binary := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01"
		h.WriteFile(path+"/bin/blob", binary)
		res := h.Commit("", "publish a binary file")
		h.AssertEnvelope(t, ToolCall{
			Tool: ToolCommit, Path: path, Message: "publish a binary file",
			Expect: CallExpectation{
				ErrorCode: "INVALID_REQUEST", Retryable: new(false),
				Reason: "INVALID_CONTENT", Action: "EDIT_FILES",
				Files: []FileExpectation{{Path: "bin/blob", Reason: "INVALID_CONTENT", Ranges: []RangeExpectation{}}},
			},
		}, res)

		after := h.Recorder().Snapshot()
		for _, op := range []Op{OpPut, OpCreate, OpReplace, OpDelete} {
			if after[op] != before[op] {
				t.Fatalf("a binary file mutated the store: %s %d -> %d", op, before[op], after[op])
			}
		}
		if got := h.ReadFile(path + "/bin/blob"); got != binary {
			t.Fatalf("L after the refusal = %q, want the caller's bytes untouched", got)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs privileges on Windows")
		}
		t.Parallel()
		h := NewFakeHarness(t, HarnessConfig{})
		path := h.WorkspaceRoot()
		h.AssertOK(t, h.Pull(""))
		before := h.Recorder().Snapshot()

		target := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
			t.Fatalf("write symlink target: %v", err)
		}
		if err := os.Symlink(target, path+"/link"); err != nil {
			t.Fatalf("Symlink(): %v", err)
		}
		res := h.Commit("", "publish a symlink")
		h.AssertEnvelope(t, ToolCall{
			Tool: ToolCommit, Path: path, Message: "publish a symlink",
			Expect: CallExpectation{
				ErrorCode: "INVALID_REQUEST", Retryable: new(false),
				Reason: "INVALID_CONTENT", Action: "EDIT_FILES",
				Files: []FileExpectation{{Path: "link", Reason: "INVALID_CONTENT", Ranges: []RangeExpectation{}}},
			},
		}, res)

		after := h.Recorder().Snapshot()
		for _, op := range []Op{OpPut, OpCreate, OpReplace, OpDelete} {
			if after[op] != before[op] {
				t.Fatalf("a symlink mutated the store: %s %d -> %d", op, before[op], after[op])
			}
		}
		info, err := os.Lstat(path + "/link")
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("L after the refusal: symlink missing or replaced (err %v, mode %v)", err, info)
		}
	})
}

// TestScenarioResultShape proves the success envelope of both tools: one
// text item carrying the resolved notebook path plus a structured
// SuccessInfo, with no commit ID, pack key, or internal value anywhere in
// the result (architecture/product-contract.md).
func TestScenarioResultShape(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	path := h.WorkspaceRoot()
	CommitFirst(t, h, "a.md", "alpha", "first commit")

	// The success envelope is one text item carrying the resolved notebook
	// path plus the structured SuccessInfo, which assertOK enforces. The
	// scan below is the belt-and-braces check that neither the text item nor
	// the structured content carries a Git object ID, pack key, or other
	// internal value.
	h.WriteFile(path+"/b.md", "beta")
	for _, res := range []*sdk.CallToolResult{
		h.Pull(""),
		h.Commit("", "second"),
	} {
		h.AssertOK(t, res)
		text := res.Content[0].(*sdk.TextContent).Text
		if GitObjectID.MatchString(text) || strings.Contains(text, "packs/") {
			t.Fatalf("success text leaks an internal value: %q", text)
		}
		data, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("marshal structured content: %v", err)
		}
		if GitObjectID.MatchString(string(data)) || strings.Contains(string(data), "packs/") {
			t.Fatalf("structured success content leaks an internal value: %s", data)
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
// No reason/action is pinned: the SDK client rejects unparsable JSON before
// any ToolError envelope exists (see TestScenarioStrictSchema for unknown fields).
func TestScenarioMalformedToolJSON(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	if err := CallProtocolError(t, h, "malformed", ToolPull, json.RawMessage(`{"path":`)); err == nil {
		t.Fatal("CallTool(malformed JSON) = nil, want a protocol error")
	}
	h.AssertExpectations(t, Expectations{
		S3: S3Assertions{Counts: &CountExpectation{AllZero: true}},
	})
}

// TestScenarioMCPReqIDScoping proves the mcpReqID log-attr contract: every
// tool-call record of one harness carries its own request ID and tool, two
// parallel harnesses keep their request IDs disjoint, and no capture leaks
// another harness's records.
func TestScenarioMCPReqIDScoping(t *testing.T) {
	t.Parallel()
	h1 := NewFakeHarness(t, HarnessConfig{})
	h2 := NewFakeHarness(t, HarnessConfig{})
	CommitFirst(t, h1, "a.md", "one", "h1 commit")
	CommitFirst(t, h2, "b.md", "two", "h2 commit")

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
	h3 := NewFakeHarness(t, HarnessConfig{
		Hooks: &app.ServiceHooks{
			Workspace: &workspace.Failpoints{},
			Notebook:  &notebook.Failpoints{},
		},
	})
	path := h3.WorkspaceRoot()
	CommitFirst(t, h3, "a.md", "alpha", "first")
	h3.WriteFile(path+"/b.md", "beta")
	h3.NotebookFailpoints().CAS = func() error { return fmt.Errorf("injected cas failure") }
	res := h3.Commit("", "second")
	h3.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: path, Message: "second",
		Expect: CallExpectation{
			ErrorCode: CodeRecoveryFailure,
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

// TestScenarioAbsentOrNullArguments proves that a call whose arguments
// member is absent or null is the empty argument object
// (architecture/mcp-server.md): notes_pull pulls the server's notebook
// directory, and notes_commit is refused only for its missing message.
func TestScenarioAbsentOrNullArguments(t *testing.T) {
	t.Parallel()
	for name, args := range map[string]any{"absent": nil, "null": json.RawMessage("null")} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := NewFakeHarness(t, HarnessConfig{})
			root := h.WorkspaceRoot()
			res := CallWithArgs(t, h, "", ToolPull, args)
			h.AssertEnvelope(t, ToolCall{Tool: ToolPull, Path: root, Expect: CallExpectation{OK: true}}, res)
			AssertPulledMarker(t, h)

			res = CallWithArgs(t, h, "", ToolCommit, args)
			h.AssertEnvelope(t, ToolCall{
				Tool: ToolCommit, Path: root,
				Expect: CallExpectation{ErrorCode: "INVALID_REQUEST", Reason: "MALFORMED_INPUT", Action: "FIX_INPUT"},
			}, res)
		})
	}
}
