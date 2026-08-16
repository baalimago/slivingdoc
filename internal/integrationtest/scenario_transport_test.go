package integrationtest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioTransportStdioProcess drives the public server through a
// spawned process and real stdio pipes. It proves initialization, listing,
// both tools, protocol-only stdout, and clean shutdown when the client
// disconnects (architecture sections 2 (L26) and 18.1 (L1117)).
func TestScenarioTransportStdioProcess(t *testing.T) {
	t.Parallel()
	h := spawnHelper(t, "fake", nil, "serve")
	cs := h.connectClient(t)

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	if len(tools.Tools) != 2 {
		t.Fatalf("listed tools = %d, want exactly 2", len(tools.Tools))
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	if !names[toolPull] || !names[toolCommit] {
		t.Fatalf("listed tools = %v, want %q and %q", names, toolPull, toolCommit)
	}

	path := filepath.Join(h.workspaceRoot, "notes")
	assertProcessCallOK(t, cs, toolPull, path, "")
	if err := os.WriteFile(filepath.Join(path, "stdio.md"), []byte("over stdio\n"), 0o644); err != nil {
		t.Fatalf("write visible test file: %v", err)
	}
	assertProcessCallOK(t, cs, toolCommit, path, "stdio commit")

	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("stdio process exit = %d, want 0; stderr: %s", code, h.stderrText(t))
	}
	assertProtocolOnlyStdout(t, h.record.Bytes())
	// Logs go to stderr only (architecture section 17, L1040): stdout was proven
	// protocol-only above, so the tool-call records must be on stderr. A
	// non-empty stderr is also satisfied by an unrelated line, so
	// assert the correlated records of the calls that were actually made.
	stderr := h.stderrText(t)
	assertToolCallLogged(t, stderr, toolPull)
	assertToolCallLogged(t, stderr, toolCommit)
}

// mcpReqIDRE matches the correlation ID the MCP server attaches to every
// tool-call record (internal/mcp: a 16-hex-char mcpReqID attribute).
var mcpReqIDRE = regexp.MustCompile(`mcpReqID=([0-9a-f]{16})`)

// assertToolCallLogged proves the helper stderr carries the correlated
// record pair of one tool call: the MCP server logs a start record and a
// completion record that share one mcpReqID and name the tool.
func assertToolCallLogged(t *testing.T, stderr, tool string) {
	t.Helper()
	started, completed := map[string]bool{}, map[string]bool{}
	for line := range strings.SplitSeq(stderr, "\n") {
		if !strings.Contains(line, " tool="+tool) {
			continue
		}
		id := mcpReqIDRE.FindStringSubmatch(line)
		if id == nil {
			t.Fatalf("tool-call record carries no mcpReqID: %q", line)
		}
		switch {
		case strings.Contains(line, `msg="tool call started"`):
			started[id[1]] = true
		case strings.Contains(line, `msg="tool call completed"`) && strings.Contains(line, "outcome=ok"):
			completed[id[1]] = true
		}
	}
	for id := range started {
		if completed[id] {
			return
		}
	}
	t.Fatalf("stderr carries no correlated start/ok record pair for %s; stderr: %s", tool, stderr)
}

// assertProcessCallOK asserts the success envelope of one process-level
// tool call: no error, one text item exactly OK, and a structured
// SuccessInfo whose code is OK.
func assertProcessCallOK(t *testing.T, cs *sdk.ClientSession, tool, path, message string) {
	t.Helper()
	args := map[string]any{"path": path}
	if tool == toolCommit {
		args["message"] = message
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s(%s) = %v", tool, path, err)
	}
	if res.IsError || res.StructuredContent == nil || len(res.Content) != 1 {
		t.Fatalf("%s(%s) result = %#v, want the structured OK envelope", tool, path, res)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("%s(%s) marshal structured content: %v", tool, path, err)
	}
	var got mcp.SuccessInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%s(%s) structured content = %s: %v", tool, path, data, err)
	}
	if got.Code != "OK" {
		t.Fatalf("%s(%s) structured code = %q, want OK", tool, path, got.Code)
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok || text.Text != "OK" {
		t.Fatalf("%s(%s) text = %#v, want exactly OK", tool, path, res.Content[0])
	}
}
