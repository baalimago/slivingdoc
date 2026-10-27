package itest

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/baalimago/slivingdoc/internal/mcp"
)

// CallToolError calls tool over cs, with a message for notes_commit, and
// returns the structured tool error it must answer, beside the text item
// a client that drops the structure forwards to its model.
func CallToolError(t *testing.T, cs *sdk.ClientSession, tool string) (mcp.ToolError, string) {
	t.Helper()
	args := map[string]any{}
	if tool == ToolCommit {
		args["message"] = "a commit that meets an error"
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s = %v", tool, err)
	}
	if !res.IsError || res.StructuredContent == nil || len(res.Content) != 1 {
		t.Fatalf("%s result = %#v, want the structured error envelope", tool, res)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("%s marshal structured content: %v", tool, err)
	}
	var te mcp.ToolError
	if err := json.Unmarshal(data, &te); err != nil {
		t.Fatalf("%s structured content = %s: %v", tool, data, err)
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("%s content = %#v, want one text item", tool, res.Content[0])
	}
	return te, text.Text
}

// AwaitSignIn calls notes_pull over cs until the serve process has signed
// in, then asserts the pull succeeded at path. A call may answer
// SIGN_IN_REQUIRED while the background login is still storing the key,
// because the helper's scaled sleep makes each call wait well under a
// second; any other error fails at once.
func AwaitSignIn(t *testing.T, cs *sdk.ClientSession, path string) mcp.SuccessInfo {
	t.Helper()
	for range 50 {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: ToolPull, Arguments: map[string]any{}})
		if err != nil {
			t.Fatalf("%s = %v", ToolPull, err)
		}
		if !res.IsError {
			break
		}
		data, _ := json.Marshal(res.StructuredContent)
		var te mcp.ToolError
		if err := json.Unmarshal(data, &te); err != nil || te.Reason != "SIGN_IN_REQUIRED" {
			t.Fatalf("%s while signing in = %s, want SIGN_IN_REQUIRED or OK", ToolPull, data)
		}
	}
	return AssertProcessCallOK(t, cs, ToolPull, path, "")
}
