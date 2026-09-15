package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestInstructionsNameReadOnlyPaths checks the instructions name a non-empty
// set.
func TestInstructionsNameReadOnlyPaths(t *testing.T) {
	svc := &fakeService{root: "/session/notebook", readOnly: []string{"docs", "faq.md"}}
	client, _ := newTestPair(t, svc)
	got := client.InitializeResult().Instructions
	if !strings.Contains(got, "/session/notebook") {
		t.Fatalf("instructions = %q, want the notebook root", got)
	}
	want := " Read-only paths: docs, faq.md. notes_commit refuses any change under them, resets those files, and reports READ_ONLY_PATH; write elsewhere."
	if !strings.HasSuffix(got, want) {
		t.Fatalf("instructions = %q, want it to end with %q", got, want)
	}
}

// TestToolDescriptionsAdvertiseReadOnlyPaths checks both descriptions name a
// non-empty set.
func TestToolDescriptionsAdvertiseReadOnlyPaths(t *testing.T) {
	svc := &fakeService{readOnly: []string{"docs", "faq.md"}}
	client, _ := newTestPair(t, svc)
	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	want := " Read-only paths in this server: docs, faq.md; changes under them are refused and reset."
	for _, tool := range res.Tools {
		if !strings.HasSuffix(tool.Description, want) {
			t.Fatalf("%s description = %q, want it to end with %q", tool.Name, tool.Description, want)
		}
	}
}

// TestSuccessTextItemCarriesReadOnly checks the success text item with and
// without a set.
func TestSuccessTextItemCarriesReadOnly(t *testing.T) {
	svc := &fakeService{readOnly: []string{"docs", "faq.md"}}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	want := "/abs/notes (read-only: docs, faq.md)"
	if !ok || text.Text != want {
		t.Fatalf("text item = %#v, want %q", res.Content[0], want)
	}

	empty, _ := newTestPair(t, &fakeService{})
	res, err = empty.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	text, ok = res.Content[0].(*sdk.TextContent)
	if !ok || text.Text != "/abs/notes" {
		t.Fatalf("text item with an empty set = %#v, want the bare path", res.Content[0])
	}
}

// TestEnvelopesCarryReadOnlyAlways checks both envelopes carry the set, empty
// but non-nil when unconfigured.
func TestEnvelopesCarryReadOnlyAlways(t *testing.T) {
	svc := &fakeService{result: sampleResult(), commitErr: conflictError(), readOnly: []string{"docs", "faq.md"}}
	client, _ := newTestPair(t, svc)

	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool(pull) = %v", err)
	}
	got := decodeSuccess(t, res)
	if want := []string{"docs", "faq.md"}; !slices.Equal(got.ReadOnly, want) {
		t.Fatalf("success readOnly = %v, want %v", got.ReadOnly, want)
	}

	errRes, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "conflicted"},
	})
	if err != nil {
		t.Fatalf("CallTool(commit) = %v", err)
	}
	gotErr := decodeToolError(t, errRes)
	if want := []string{"docs", "faq.md"}; !slices.Equal(gotErr.ReadOnly, want) {
		t.Fatalf("error readOnly = %v, want %v", gotErr.ReadOnly, want)
	}

	emptySvc := &fakeService{result: sampleResult(), commitErr: conflictError()}
	emptyClient, _ := newTestPair(t, emptySvc)
	res, err = emptyClient.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool(pull, empty) = %v", err)
	}
	got = decodeSuccess(t, res)
	if got.ReadOnly == nil || len(got.ReadOnly) != 0 {
		t.Fatalf("success readOnly with an empty set = %v, want an empty non-nil array", got.ReadOnly)
	}
	errRes, err = emptyClient.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "conflicted"},
	})
	if err != nil {
		t.Fatalf("CallTool(commit, empty) = %v", err)
	}
	gotErr = decodeToolError(t, errRes)
	if gotErr.ReadOnly == nil || len(gotErr.ReadOnly) != 0 {
		t.Fatalf("error readOnly with an empty set = %v, want an empty non-nil array", gotErr.ReadOnly)
	}
}

func decodeSuccess(t *testing.T, res *sdk.CallToolResult) SuccessInfo {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got SuccessInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content is not the success shape: %v", err)
	}
	return got
}

func decodeToolError(t *testing.T, res *sdk.CallToolResult) ToolError {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got ToolError
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content is not the error shape: %v", err)
	}
	return got
}
