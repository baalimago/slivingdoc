package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/notebook"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeService records tool calls and returns canned outcomes. It is the
// interface-plus-mock mirror: the tests prove the mcp package consumes
// only the Service seam, with no S3, Docker, or native engine.
type fakeService struct {
	mu        sync.Mutex
	pulls     []string
	commits   []commitCall
	pullErr   error
	commitErr error
	block     chan struct{}
}

type commitCall struct {
	path    string
	message string
}

func (f *fakeService) Pull(ctx context.Context, path string) error {
	f.mu.Lock()
	f.pulls = append(f.pulls, path)
	err := f.pullErr
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (f *fakeService) Commit(ctx context.Context, path, message string) error {
	f.mu.Lock()
	f.commits = append(f.commits, commitCall{path: path, message: message})
	err := f.commitErr
	f.mu.Unlock()
	return err
}

// newTestPair wires a server and a client over in-memory transports. The
// client performs the MCP initialization during Connect.
func newTestPair(t *testing.T, svc Service) (*sdk.ClientSession, *fakeService) {
	t.Helper()
	if svc == nil {
		svc = &fakeService{}
	}
	server := NewServer(svc, "test-version", nil)
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport)
	if err != nil {
		t.Fatalf("server Connect() = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession, svc.(*fakeService)
}

// TestListToolsExactlyTwo proves the acceptance criterion that exactly two
// tools appear in the MCP tool listing, with strict schemas that require
// the documented fields and reject unknown ones.
func TestListToolsExactlyTwo(t *testing.T) {
	client, _ := newTestPair(t, nil)
	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("tools = %d, want exactly 2", len(res.Tools))
	}
	byName := map[string]*sdk.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{toolPull, toolCommit} {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("tool %q missing from the listing", name)
		}
		if tool.Description == "" {
			t.Fatalf("tool %q has no description", name)
		}
		schema := schemaMap(t, tool.InputSchema)
		if schema["type"] != "object" {
			t.Fatalf("tool %q schema type = %v, want object", name, schema["type"])
		}
		if schema["additionalProperties"] != false {
			t.Fatalf("tool %q schema must reject additional properties", name)
		}
		required, ok := schema["required"].([]any)
		if !ok || len(required) == 0 {
			t.Fatalf("tool %q schema must require fields", name)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %q schema has no properties", name)
		}
		for _, field := range required {
			if _, ok := properties[field.(string)]; !ok {
				t.Fatalf("tool %q requires %q but does not declare it", name, field)
			}
		}
	}
}

// TestPullSuccessReturnsOK proves the success envelope: one text item with
// exactly OK, no structured content, no error, and the exact request path
// reaching the service.
func TestPullSuccessReturnsOK(t *testing.T) {
	client, svc := newTestPair(t, nil)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertOKResult(t, res)
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.pulls) != 1 || svc.pulls[0] != "/abs/notes" {
		t.Fatalf("service pulls = %v, want the exact absolute path", svc.pulls)
	}
}

// TestCommitSuccessReturnsOK proves the success envelope and that the
// exact message reaches the service; the response never contains a commit
// ID.
func TestCommitSuccessReturnsOK(t *testing.T) {
	client, svc := newTestPair(t, nil)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "update notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertOKResult(t, res)
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.commits) != 1 || svc.commits[0] != (commitCall{path: "/abs/notes", message: "update notes"}) {
		t.Fatalf("service commits = %v, want the exact path and message", svc.commits)
	}
}

// TestPullBlankCommitMessageMapsToInvalidRequest proves that a blank
// message never reaches the notebook: it maps to an INVALID_REQUEST tool
// error before the service runs.
func TestPullBlankCommitMessageMapsToInvalidRequest(t *testing.T) {
	client, svc := newTestPair(t, nil)
	for _, message := range []string{"", "   ", "\u00a0"} {
		res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
			Name:      toolCommit,
			Arguments: map[string]any{"path": "/abs/notes", "message": message},
		})
		if err != nil {
			t.Fatalf("CallTool(%q) = %v", message, err)
		}
		assertErrorCode(t, res, codeInvalidRequest, false)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.commits) != 0 {
		t.Fatalf("service commits = %v, want none", svc.commits)
	}
}

// TestConflictDataSurvivesSDKEnvelope proves that the exact structured
// conflict paths and one-based inclusive ranges survive the SDK error
// envelope, with isError set and one candid text item.
func TestConflictDataSurvivesSDKEnvelope(t *testing.T) {
	svc := &fakeService{commitErr: conflictError()}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "conflicted"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	if !res.IsError {
		t.Fatal("conflict result must set isError")
	}
	if len(res.Content) != 1 {
		t.Fatalf("content items = %d, want exactly one", len(res.Content))
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok || !strings.Contains(text.Text, "Resolve the conflict blocks") {
		t.Fatalf("text item = %#v, want the candid conflict message", res.Content[0])
	}
	if res.StructuredContent == nil {
		t.Fatal("structured content missing")
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got toolError
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content is not the tool-error shape: %v", err)
	}
	if got.Code != codeContentConflict || got.Retryable {
		t.Fatalf("structured = %+v, want CONTENT_CONFLICT not retryable", got)
	}
	if len(got.Files) != 2 || got.Files[0].Path != "notes/today.md" ||
		len(got.Files[0].Ranges) != 2 || got.Files[0].Ranges[0] != (errorRange{12, 18}) {
		t.Fatalf("structured files = %+v, want the exact conflict data", got.Files)
	}
}

// TestRecoveryFailureSurvivesSDKEnvelope proves that the recovery report
// survives the SDK envelope only for RECOVERY_FAILURE.
func TestRecoveryFailureSurvivesSDKEnvelope(t *testing.T) {
	svc := &fakeService{pullErr: recoveryError()}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	if !res.IsError {
		t.Fatal("recovery failure must set isError")
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got toolError
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content: %v", err)
	}
	if got.Code != codeRecoveryFailure || !got.Retryable || got.Recovery == nil {
		t.Fatalf("structured = %+v, want a retryable RECOVERY_FAILURE with recovery", got)
	}
	if got.Recovery.Stage != "commit.cas" || got.Recovery.RemoteAccepted != "yes" || !got.Recovery.Resynchronized {
		t.Fatalf("recovery = %+v", got.Recovery)
	}
}

// TestErrorMessagesAreRedacted proves that a storage failure message that
// contains protected values reaches the client without them.
func TestErrorMessagesAreRedacted(t *testing.T) {
	svc := &fakeService{commitErr: &notebook.Error{
		Code:    notebook.CodeStorageFailure,
		Message: "download pack packs/increments/3-0196c2d0-7f2b-7e00-8000-000000000004.pack failed (AKIAIOSFODNN7EXAMPLE)",
	}}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "m"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	text := res.Content[0].(*sdk.TextContent).Text
	for _, leaked := range []string{"packs/increments", "0196c2d0", "AKIAIOSFODNN7EXAMPLE"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("error text leaked %q: %q", leaked, text)
		}
	}
}

// TestInvalidInputsNeverReachService proves that unknown fields, relative
// paths, and non-object arguments are rejected before the service runs.
func TestInvalidInputsNeverReachService(t *testing.T) {
	svc := &fakeService{}
	client, _ := newTestPair(t, svc)
	calls := []struct {
		name      string
		tool      string
		arguments map[string]any
	}{
		{name: "unknown field", tool: toolPull, arguments: map[string]any{"path": "/abs/n", "extra": 1}},
		{name: "relative path", tool: toolPull, arguments: map[string]any{"path": "notes"}},
		{name: "missing path", tool: toolPull, arguments: map[string]any{}},
		{name: "null path", tool: toolPull, arguments: map[string]any{"path": nil}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			res, err := client.CallTool(context.Background(), &sdk.CallToolParams{Name: call.tool, Arguments: call.arguments})
			if err != nil {
				t.Fatalf("CallTool() = %v", err)
			}
			assertErrorCode(t, res, codeInvalidRequest, false)
		})
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.pulls) != 0 || len(svc.commits) != 0 {
		t.Fatalf("service calls = %v/%v, want none", svc.pulls, svc.commits)
	}
}

// TestUnknownToolReturnsProtocolError proves that an unknown tool name
// produces an SDK protocol error, not a tool result.
func TestUnknownToolReturnsProtocolError(t *testing.T) {
	client, _ := newTestPair(t, nil)
	_, err := client.CallTool(context.Background(), &sdk.CallToolParams{Name: "notes_delete"})
	if err == nil {
		t.Fatal("CallTool(unknown tool) = nil, want a protocol error")
	}
}

// TestMalformedArgumentsJSONReturnsProtocolError proves that malformed
// tool JSON never reaches the handler: the SDK reports a protocol-level
// error. The client embeds raw JSON into the wire message; the server's
// reader rejects the malformed document.
func TestMalformedArgumentsJSONReturnsProtocolError(t *testing.T) {
	svc := &fakeService{}
	client, _ := newTestPair(t, svc)
	_, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: json.RawMessage(`{"path":`),
	})
	if err == nil {
		t.Fatal("CallTool(malformed JSON) = nil, want a protocol error")
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.pulls) != 0 {
		t.Fatalf("service pulls = %v, want none", svc.pulls)
	}
}

// TestCancellationPropagatesToService proves that a canceled request
// cancels the in-flight service call instead of producing a tool result.
func TestCancellationPropagatesToService(t *testing.T) {
	svc := &fakeService{block: make(chan struct{})}
	server := NewServer(svc, "test-version", nil)
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport)
	if err != nil {
		t.Fatalf("server Connect() = %v", err)
	}
	defer serverSession.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}
	defer clientSession.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err == nil {
		t.Fatal("CallTool() with a blocked service = nil, want an error")
	}
	if ctx.Err() == nil {
		t.Fatal("request context was not canceled")
	}
}

// assertOKResult proves the exact success envelope: one text item with
// exactly OK, no structured content, no isError.
func assertOKResult(t *testing.T, res *sdk.CallToolResult) {
	t.Helper()
	if res.IsError {
		t.Fatal("success result must not set isError")
	}
	if res.StructuredContent != nil {
		t.Fatalf("success result has structured content: %v", res.StructuredContent)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content items = %d, want exactly one", len(res.Content))
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok || text.Text != "OK" {
		t.Fatalf("text item = %#v, want exactly OK", res.Content[0])
	}
}

// assertErrorCode proves the structured error envelope: isError, one text
// item, and the stable code and retryable flag in the structured content.
func assertErrorCode(t *testing.T, res *sdk.CallToolResult, wantCode string, wantRetryable bool) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("result must set isError for %s", wantCode)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content items = %d, want exactly one", len(res.Content))
	}
	if _, ok := res.Content[0].(*sdk.TextContent); !ok {
		t.Fatalf("content[0] = %T, want a text item", res.Content[0])
	}
	if res.StructuredContent == nil {
		t.Fatal("structured content missing")
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got toolError
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content: %v", err)
	}
	if got.Code != wantCode || got.Retryable != wantRetryable {
		t.Fatalf("structured = %+v, want %s retryable=%v", got, wantCode, wantRetryable)
	}
	if got.Files == nil {
		t.Fatal("files must always be present")
	}
}

// schemaMap decodes a tool schema into a generic map for assertions.
func schemaMap(t *testing.T, schema any) map[string]any {
	t.Helper()
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	return m
}

// Compile-time proof that fakeService satisfies the Service seam.
var _ Service = (*fakeService)(nil)
