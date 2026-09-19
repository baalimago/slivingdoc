package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/notebook"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeService records tool calls and returns canned outcomes. It is the
// interface-plus-mock mirror: the tests prove the mcp package consumes
// only the Service seam, with no S3, Docker, or native engine. result is
// the success summary returned on a clean call; the zero value stands for
// a no-op synchronization.
type fakeService struct {
	mu        sync.Mutex
	pulls     []string
	commits   []commitCall
	result    notebook.Result
	pullErr   error
	commitErr error
	block     chan struct{}
	root      string
	readOnly  []string
	writable  []string
}

type commitCall struct {
	path    string
	message string
}

// fakeRoot is the notebook directory an omitted request path resolves to.
const fakeRoot = "/fake/notebook"

func (f *fakeService) Root() string {
	if f.root == "" {
		return fakeRoot
	}
	return f.root
}

// ReadOnlyPaths returns the configured set, always non-nil.
func (f *fakeService) ReadOnlyPaths() []string {
	if f.readOnly == nil {
		return []string{}
	}
	return f.readOnly
}

// WritablePaths returns the configured set, always non-nil.
func (f *fakeService) WritablePaths() []string {
	if f.writable == nil {
		return []string{}
	}
	return f.writable
}

func (f *fakeService) Pull(ctx context.Context, path string) (notebook.Result, error) {
	f.mu.Lock()
	f.pulls = append(f.pulls, path)
	result := f.result
	err := f.pullErr
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return notebook.Result{}, ctx.Err()
		}
	}
	return result, err
}

func (f *fakeService) Commit(ctx context.Context, path, message string) (notebook.Result, error) {
	f.mu.Lock()
	f.commits = append(f.commits, commitCall{path: path, message: message})
	result := f.result
	err := f.commitErr
	f.mu.Unlock()
	return result, err
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

// requiredFields is the documented required set of each tool schema. path
// is optional on both tools: an omitted path is the server's notebook root.
var requiredFields = map[string][]string{
	toolPull:   {},
	toolCommit: {"message"},
}

// TestListToolsExactlyTwo proves the acceptance criterion that exactly two
// tools appear in the MCP tool listing, with strict schemas that require
// the documented fields, declare the optional ones, and reject unknown
// ones.
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
		if !ok {
			t.Fatalf("tool %q schema has no required list", name)
		}
		got := make([]string, 0, len(required))
		for _, field := range required {
			got = append(got, field.(string))
		}
		if !slices.Equal(got, requiredFields[name]) {
			t.Fatalf("tool %q required = %v, want %v", name, got, requiredFields[name])
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %q schema has no properties", name)
		}
		if _, ok := properties["path"]; !ok {
			t.Fatalf("tool %q schema must declare the optional path", name)
		}
		for _, field := range got {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %q requires %q but does not declare it", name, field)
			}
		}
	}
}

// TestPullSuccessReturnsOK proves the success envelope of a clean pull:
// one text item carrying the resolved notebook path, a structured
// SuccessInfo with code OK, no error, and the exact request path reaching
// the service.
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

// TestCommitSuccessReturnsOK proves the success envelope of a clean
// commit and that the exact message reaches the service; the response
// never contains a commit ID.
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

// TestPullSuccessEnvelope proves the structured success object of a clean
// pull: the accepted generation, the per-file change stat with normalized
// relative paths, and the totals all survive the SDK envelope exactly.
func TestPullSuccessEnvelope(t *testing.T) {
	svc := &fakeService{result: sampleResult()}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertSuccessInfo(t, res, &SuccessInfo{
		Path:         "/abs/notes",
		Code:         "OK",
		Generation:   18,
		FilesChanged: 3,
		Insertions:   3,
		Deletions:    4,
		Files: []ChangeFile{
			{Path: "notes/a.md", Insertions: 1, Deletions: 1},
			{Path: "notes/c.md", Insertions: 2, Deletions: 0},
			{Path: "archive/old.md", Insertions: 0, Deletions: 3},
		},
		ReadOnly: []string{}, Writable: []string{},
	})
}

// TestCommitSuccessEnvelope proves the same structured success envelope
// for a clean commit, and that no Git ID, pack key, or credential appears
// anywhere in the result.
func TestCommitSuccessEnvelope(t *testing.T) {
	svc := &fakeService{result: sampleResult()}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "update notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertSuccessInfo(t, res, &SuccessInfo{
		Path:         "/abs/notes",
		Code:         "OK",
		Generation:   18,
		FilesChanged: 3,
		Insertions:   3,
		Deletions:    4,
		Files: []ChangeFile{
			{Path: "notes/a.md", Insertions: 1, Deletions: 1},
			{Path: "notes/c.md", Insertions: 2, Deletions: 0},
			{Path: "archive/old.md", Insertions: 0, Deletions: 3},
		},
		ReadOnly: []string{}, Writable: []string{},
	})
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	for _, leaked := range []string{"packs/", "probe/", "AKIA"} {
		if strings.Contains(string(data), leaked) {
			t.Fatalf("success envelope leaked %q: %s", leaked, data)
		}
	}
	if gitIDRE.MatchString(string(data)) {
		t.Fatalf("success envelope leaked a Git object ID: %s", data)
	}
}

// TestNoOpCommitReturnsEmptyStatEnvelope proves that a no-op
// synchronization returns code OK with the remote generation and an empty
// stat: filesChanged 0 and an empty (non-nil) files array.
func TestNoOpCommitReturnsEmptyStatEnvelope(t *testing.T) {
	svc := &fakeService{result: notebook.Result{Generation: 7}}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "no changes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertSuccessInfo(t, res, &SuccessInfo{
		Path: "/abs/notes",
		Code: "OK", Generation: 7, FilesChanged: 0, Insertions: 0, Deletions: 0, Files: []ChangeFile{},
		ReadOnly: []string{}, Writable: []string{},
	})
}

// TestZeroResultWithNoErrorNeverPanics proves the contract-bug guard: a
// zero result paired with nil error (which the notebook never produces)
// maps to code OK with a zero generation and an empty stat instead of a
// panic or a protocol error.
func TestZeroResultWithNoErrorNeverPanics(t *testing.T) {
	client, _ := newTestPair(t, nil) // the fake returns the zero result
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertSuccessInfo(t, res, &SuccessInfo{
		Path: "/abs/notes",
		Code: "OK", Generation: 0, FilesChanged: 0, Insertions: 0, Deletions: 0, Files: []ChangeFile{},
		ReadOnly: []string{}, Writable: []string{},
	})
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
// envelope, with isError set, one candid text item, and no success-only
// field anywhere in the error object.
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
	if !ok {
		t.Fatalf("text item = %#v, want text content", res.Content[0])
	}
	for _, want := range []string{
		"CONTENT_CONFLICT · MERGE_CONFLICT", "Resolve the conflict blocks",
		"action: EDIT_FILES", "retryable: false", "diagnosticId:",
	} {
		if !strings.Contains(text.Text, want) {
			t.Fatalf("text item = %q, want %q", text.Text, want)
		}
	}
	if res.StructuredContent == nil {
		t.Fatal("structured content missing")
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	for _, successOnly := range []string{"generation", "filesChanged", "insertions", "deletions"} {
		if strings.Contains(string(data), successOnly) {
			t.Fatalf("error envelope carries the success-only field %q: %s", successOnly, data)
		}
	}
	var got ToolError
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content is not the tool-error shape: %v", err)
	}
	if got.Code != "CONTENT_CONFLICT" || got.Retryable {
		t.Fatalf("structured = %+v, want CONTENT_CONFLICT not retryable", got)
	}
	if len(got.Files) != 2 || got.Files[0].Path != "notes/today.md" || got.Files[0].Reason != "TEXT_CONFLICT" ||
		len(got.Files[0].Ranges) != 2 || got.Files[0].Ranges[0] != (ErrorRange{12, 18}) {
		t.Fatalf("structured files = %+v, want the exact conflict data", got.Files)
	}
	if got.Reason != "MERGE_CONFLICT" || got.Action != "EDIT_FILES" {
		t.Fatalf("structured reason/action = %q/%q, want MERGE_CONFLICT/EDIT_FILES", got.Reason, got.Action)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(got.DiagnosticID) {
		t.Fatalf("diagnosticId = %q, want 16 lowercase hex characters", got.DiagnosticID)
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
	var got ToolError
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content: %v", err)
	}
	if got.Code != "RECOVERY_FAILURE" || !got.Retryable || got.Recovery == nil {
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

// TestOmittedPathResolvesToNotebookRoot proves the default-directory
// contract: a call without path reaches the service as the server's
// notebook root, and the result reports that directory, so an agent that
// never configured a path still knows where to edit.
func TestOmittedPathResolvesToNotebookRoot(t *testing.T) {
	svc := &fakeService{root: "/session/notebook"}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertSuccessInfo(t, res, &SuccessInfo{
		Path: "/session/notebook", Code: "OK", Files: []ChangeFile{}, ReadOnly: []string{}, Writable: []string{},
	})
	commitRes, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"message": "notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertSuccessInfo(t, commitRes, &SuccessInfo{
		Path: "/session/notebook", Code: "OK", Files: []ChangeFile{}, ReadOnly: []string{}, Writable: []string{},
	})
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.pulls) != 1 || svc.pulls[0] != "/session/notebook" {
		t.Fatalf("service pulls = %v, want the notebook root", svc.pulls)
	}
	if len(svc.commits) != 1 || svc.commits[0].path != "/session/notebook" {
		t.Fatalf("service commits = %v, want the notebook root", svc.commits)
	}
}

// TestInstructionsNameNotebookRoot proves the server tells the caller which
// directory to edit before any tool call returns.
func TestInstructionsNameNotebookRoot(t *testing.T) {
	svc := &fakeService{root: "/session/notebook"}
	client, _ := newTestPair(t, svc)
	got := client.InitializeResult().Instructions
	if !strings.Contains(got, "/session/notebook") {
		t.Fatalf("instructions = %q, want the notebook root", got)
	}
}

// TestToolDescriptionsAdvertiseEmptyPathDefault keeps the MCP discovery text
// aligned with the accepted empty-string request behavior.
func TestToolDescriptionsAdvertiseEmptyPathDefault(t *testing.T) {
	for name, description := range map[string]string{
		toolPull:   pullDescription,
		toolCommit: commitDescription,
	} {
		if !strings.Contains(description, "empty string") {
			t.Errorf("%s description = %q, want empty-path guidance", name, description)
		}
	}
	if got := pathProperty["minLength"]; got != 0 {
		t.Fatalf("path schema minLength = %v, want 0", got)
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
		{name: "missing message", tool: toolCommit, arguments: map[string]any{"path": "/abs/n"}},
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

// assertOKResult proves the exact success envelope: one text item
// carrying the resolved notebook path, no isError, and a structured
// SuccessInfo whose code is OK. The full structured values are asserted
// per test with assertSuccessInfo.
func assertOKResult(t *testing.T, res *sdk.CallToolResult) {
	t.Helper()
	if res.IsError {
		t.Fatal("success result must not set isError")
	}
	if res.StructuredContent == nil {
		t.Fatal("success result carries no structured content")
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got SuccessInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content is not the success shape: %v", err)
	}
	if got.Code != "OK" {
		t.Fatalf("structured code = %q, want OK", got.Code)
	}
	if got.Files == nil {
		t.Fatal("files must always be present")
	}
	if len(res.Content) != 1 {
		t.Fatalf("content items = %d, want exactly one", len(res.Content))
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok || text.Text != got.Path {
		t.Fatalf("text item = %#v, want the resolved notebook path %q", res.Content[0], got.Path)
	}
}

// assertSuccessInfo proves the exact structured success object after its
// round trip through the SDK envelope.
func assertSuccessInfo(t *testing.T, res *sdk.CallToolResult, want *SuccessInfo) {
	t.Helper()
	assertOKResult(t, res)
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got SuccessInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content: %v", err)
	}
	if !reflect.DeepEqual(&got, want) {
		t.Fatalf("structured = %+v, want %+v", &got, want)
	}
}

// sampleResult is the canonical success summary shared by the envelope
// tests: generation 18 and the three-file stat of the documented example.
func sampleResult() notebook.Result {
	return notebook.Result{
		Generation: 18,
		Stat: git.DiffStat{
			Files: []git.FileStat{
				{Path: "notes/a.md", Insertions: 1, Deletions: 1},
				{Path: "notes/c.md", Insertions: 2, Deletions: 0},
				{Path: "archive/old.md", Insertions: 0, Deletions: 3},
			},
			Insertions: 3,
			Deletions:  4,
		},
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
	var got ToolError
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

// The advertisement tests below cover the writable set on every surface an
// agent reads (architecture section 2, Read-only paths). Each surface names
// the actionable list: the writable entries when one is configured, the
// read-only entries otherwise.
const (
	wantReadOnlyInstructions = " Read-only paths: docs, faq.md. notes_commit refuses any change under them, " +
		"resets those files, and reports READ_ONLY_PATH; write elsewhere."
	wantReadOnlyDescription  = " Read-only paths in this server: docs, faq.md; changes under them are refused and reset."
	wantWritableInstructions = " Writable paths: notes, team.md. notes_commit refuses any change elsewhere, " +
		"resets those files, and reports READ_ONLY_PATH; write only under them."
	wantWritableDescription = " Writable paths in this server: notes, team.md; changes elsewhere are refused and reset."
)

// The composed forms: with both sets configured the read-only sentence
// cannot end in "write elsewhere" — a non-empty writable set protects
// everything it does not cover, and the sets may nest — so it ends in the
// rule that reconciles the two (review 2, R2-02).
const (
	wantComposedReadOnlyInstructions = " Read-only paths: docs, faq.md. notes_commit refuses any change under them, " +
		"resets those files, and reports READ_ONLY_PATH; where the two sets nest, the longest matching entry decides."
	wantComposedReadOnlyDescription = " Read-only paths in this server: docs, faq.md; changes under them are refused " +
		"and reset, and where the two sets nest the longest matching entry decides."
)

// advertisedSurfaces is what one configured server says on the three text
// surfaces: the instructions, both tool descriptions, and the success text
// item of a pull.
type advertisedSurfaces struct {
	instructions string
	descriptions map[string]string
	text         string
}

// surfacesOf initializes a client against svc and reads every text surface
// the agent sees, so one helper serves every set-state row.
func surfacesOf(t *testing.T, svc *fakeService) advertisedSurfaces {
	t.Helper()
	client, _ := newTestPair(t, svc)
	tools, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	descriptions := make(map[string]string, len(tools.Tools))
	for _, tool := range tools.Tools {
		descriptions[tool.Name] = tool.Description
	}
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("text item = %#v, want text content", res.Content[0])
	}
	return advertisedSurfaces{
		instructions: client.InitializeResult().Instructions,
		descriptions: descriptions,
		text:         text.Text,
	}
}

// TestAdvertisementReadOnlyWordingUnchanged proves a server with only a
// read-only set says exactly what it says today, byte for byte.
func TestAdvertisementReadOnlyWordingUnchanged(t *testing.T) {
	got := surfacesOf(t, &fakeService{readOnly: []string{"docs", "faq.md"}})
	if !strings.HasSuffix(got.instructions, wantReadOnlyInstructions) {
		t.Fatalf("instructions = %q, want it to end with %q", got.instructions, wantReadOnlyInstructions)
	}
	if strings.Contains(got.instructions, "Writable paths") {
		t.Fatalf("instructions = %q, want no writable sentence with an empty writable set", got.instructions)
	}
	for name, description := range got.descriptions {
		if !strings.HasSuffix(description, wantReadOnlyDescription) {
			t.Fatalf("%s description = %q, want it to end with %q", name, description, wantReadOnlyDescription)
		}
		if strings.Contains(description, "Writable paths") {
			t.Fatalf("%s description = %q, want no writable sentence", name, description)
		}
	}
	if want := "/abs/notes (read-only: docs, faq.md)"; got.text != want {
		t.Fatalf("text item = %q, want %q", got.text, want)
	}
}

// TestAdvertisementNamesWritableEntries proves every agent-facing text
// surface names the writable entries and the consequence of writing
// elsewhere.
func TestAdvertisementNamesWritableEntries(t *testing.T) {
	got := surfacesOf(t, &fakeService{writable: []string{"notes", "team.md"}})
	if !strings.HasSuffix(got.instructions, wantWritableInstructions) {
		t.Fatalf("instructions = %q, want it to end with %q", got.instructions, wantWritableInstructions)
	}
	for name, description := range got.descriptions {
		if !strings.HasSuffix(description, wantWritableDescription) {
			t.Fatalf("%s description = %q, want it to end with %q", name, description, wantWritableDescription)
		}
	}
	if want := "/abs/notes (writable: notes, team.md)"; got.text != want {
		t.Fatalf("text item = %q, want %q", got.text, want)
	}
}

// TestAdvertisementBothSetsWritableFirst proves both sets appear, writable
// first: the writable region is the frame and the read-only entries are the
// exceptions inside it.
func TestAdvertisementBothSetsWritableFirst(t *testing.T) {
	got := surfacesOf(t, &fakeService{
		readOnly: []string{"docs", "faq.md"},
		writable: []string{"notes", "team.md"},
	})
	assertWritableFirst(t, "instructions", got.instructions, wantWritableInstructions, wantComposedReadOnlyInstructions)
	for name, description := range got.descriptions {
		assertWritableFirst(t, name+" description", description, wantWritableDescription, wantComposedReadOnlyDescription)
	}
	if want := "/abs/notes (writable: notes, team.md; read-only: docs, faq.md; longest match decides)"; got.text != want {
		t.Fatalf("text item = %q, want %q", got.text, want)
	}
}

// assertWritableFirst proves one surface carries both sentences with the
// writable one first.
func assertWritableFirst(t *testing.T, surface, got, writable, readOnly string) {
	t.Helper()
	writableAt, readOnlyAt := strings.Index(got, writable), strings.Index(got, readOnly)
	if writableAt < 0 || readOnlyAt < 0 {
		t.Fatalf("%s = %q, want both %q and %q", surface, got, writable, readOnly)
	}
	if writableAt > readOnlyAt {
		t.Fatalf("%s = %q, want the writable set before the read-only set", surface, got)
	}
}

// TestAdvertisementNestedSetsStateRule proves the composed advertisement of
// a three-level configuration — one set holding an entry below an entry of
// the other, and a third entry below that, which is the composition D1
// exists for — reads truthfully on every surface. A two-level pair is not
// enough: it is the shape that hid R2-01, and the entry that decides is the
// third level (review 2, R2-02).
func TestAdvertisementNestedSetsStateRule(t *testing.T) {
	got := surfacesOf(t, &fakeService{
		readOnly: []string{"notes", "notes/agent-a/locked"},
		writable: []string{"notes/agent-a"},
	})
	wantInstructions := " Writable paths: notes/agent-a. notes_commit refuses any change elsewhere, " +
		"resets those files, and reports READ_ONLY_PATH; write only under them." +
		" Read-only paths: notes, notes/agent-a/locked. notes_commit refuses any change under them, " +
		"resets those files, and reports READ_ONLY_PATH; where the two sets nest, the longest matching entry decides."
	if !strings.HasSuffix(got.instructions, wantInstructions) {
		t.Fatalf("instructions = %q, want it to end with %q", got.instructions, wantInstructions)
	}
	wantDescription := " Writable paths in this server: notes/agent-a; changes elsewhere are refused and reset." +
		" Read-only paths in this server: notes, notes/agent-a/locked; changes under them are refused and reset, " +
		"and where the two sets nest the longest matching entry decides."
	for name, description := range got.descriptions {
		if !strings.HasSuffix(description, wantDescription) {
			t.Fatalf("%s description = %q, want it to end with %q", name, description, wantDescription)
		}
	}
	wantText := "/abs/notes (writable: notes/agent-a; read-only: notes, notes/agent-a/locked; longest match decides)"
	if got.text != wantText {
		t.Fatalf("text item = %q, want %q", got.text, wantText)
	}
	// The contradiction R2-02 names: "write elsewhere" beside "write only
	// under them", where elsewhere is exactly what the writable set
	// protects. No surface may carry it while a writable set is configured.
	surfaces := map[string]string{"instructions": got.instructions, "text item": got.text}
	for name, description := range got.descriptions {
		surfaces[name+" description"] = description
	}
	for name, text := range surfaces {
		if strings.Contains(text, "write elsewhere") {
			t.Fatalf("%s = %q, want no \"write elsewhere\" beside a writable set that protects elsewhere", name, text)
		}
	}
}

// errorTextOf calls notes_commit against svc and returns the text item of
// the domain error it produces, which is all a host that drops structured
// content forwards.
func errorTextOf(t *testing.T, svc *fakeService) string {
	t.Helper()
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "conflicted"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("text item = %#v, want text content", res.Content[0])
	}
	return text.Text
}

// TestErrorTextCarriesBothSets proves the error text item names the
// writable set and the composition rule beside the read-only trailer, so a
// text-only host learns where writing is allowed from every domain error
// rather than from the refusal alone (review 1, R1-06). With no writable
// set the trailer is byte-for-byte what it was.
func TestErrorTextCarriesBothSets(t *testing.T) {
	both := errorTextOf(t, &fakeService{
		commitErr: conflictError(),
		readOnly:  []string{"notes", "notes/agent-a/locked"},
		writable:  []string{"notes/agent-a"},
	})
	wantTrailers := "\nwritable: notes/agent-a\nread-only: notes, notes/agent-a/locked\npath-rule: longest match decides"
	if !strings.HasSuffix(both, wantTrailers) {
		t.Fatalf("error text = %q, want it to end with %q", both, wantTrailers)
	}
	readOnlyOnly := errorTextOf(t, &fakeService{commitErr: conflictError(), readOnly: []string{"docs", "faq.md"}})
	if !strings.HasSuffix(readOnlyOnly, "\nread-only: docs, faq.md") {
		t.Fatalf("read-only-only error text = %q, want the unchanged read-only trailer last", readOnlyOnly)
	}
	for _, absent := range []string{"writable:", "path-rule:"} {
		if strings.Contains(readOnlyOnly, absent) {
			t.Fatalf("read-only-only error text = %q, want no %q", readOnlyOnly, absent)
		}
	}
}

// TestAdvertisementUnconfiguredTextUnchanged proves a server with neither
// set is the bare form it is today: no mention of either set anywhere.
func TestAdvertisementUnconfiguredTextUnchanged(t *testing.T) {
	got := surfacesOf(t, &fakeService{})
	wantInstructions := "The notebook directory is " + fakeRoot + ". Call notes_pull, edit " +
		"UTF-8 text files (without U+0000) there, then call notes_commit to " +
		"publish. Both tools default to that directory; pass path only to " +
		"address a subdirectory of it."
	if got.instructions != wantInstructions {
		t.Fatalf("instructions = %q, want the bare form %q", got.instructions, wantInstructions)
	}
	for name, want := range map[string]string{toolPull: pullDescription, toolCommit: commitDescription} {
		if got.descriptions[name] != want {
			t.Fatalf("%s description = %q, want the bare form %q", name, got.descriptions[name], want)
		}
	}
	if got.text != "/abs/notes" {
		t.Fatalf("text item = %q, want the bare path", got.text)
	}
}

// TestAdvertisementOneSetEmpty proves that only the non-empty set appears
// in the text while both arrays stay present.
func TestAdvertisementOneSetEmpty(t *testing.T) {
	for _, row := range []struct {
		name     string
		svc      *fakeService
		wantText string
		absent   string
	}{
		{
			name:     "writable only",
			svc:      &fakeService{writable: []string{"notes", "team.md"}},
			wantText: "/abs/notes (writable: notes, team.md)",
			absent:   "read-only:",
		},
		{
			name:     "read-only only",
			svc:      &fakeService{readOnly: []string{"docs", "faq.md"}},
			wantText: "/abs/notes (read-only: docs, faq.md)",
			absent:   "writable:",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			client, _ := newTestPair(t, row.svc)
			res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
				Name:      toolPull,
				Arguments: map[string]any{"path": "/abs/notes"},
			})
			if err != nil {
				t.Fatalf("CallTool() = %v", err)
			}
			text := res.Content[0].(*sdk.TextContent).Text
			if text != row.wantText {
				t.Fatalf("text item = %q, want %q", text, row.wantText)
			}
			if strings.Contains(text, row.absent) {
				t.Fatalf("text item = %q, want no %q for an empty set", text, row.absent)
			}
			got := decodeSuccess(t, res)
			if got.ReadOnly == nil || got.Writable == nil {
				t.Fatalf("arrays = %v/%v, want both present", got.ReadOnly, got.Writable)
			}
		})
	}
}

// TestSuccessTextWritableForm proves the text item alone names where
// writing is allowed, for a host that forwards only the text item and drops
// the structured content.
func TestSuccessTextWritableForm(t *testing.T) {
	client, _ := newTestPair(t, &fakeService{writable: []string{"notes"}})
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "m"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content items = %d, want exactly one", len(res.Content))
	}
	if want := "/abs/notes (writable: notes)"; res.Content[0].(*sdk.TextContent).Text != want {
		t.Fatalf("text item = %#v, want %q", res.Content[0], want)
	}
}

// TestSuccessResultCarriesWritableArray proves the writable array rides on
// every success result, on both tools.
func TestSuccessResultCarriesWritableArray(t *testing.T) {
	svc := &fakeService{result: sampleResult(), readOnly: []string{"docs"}, writable: []string{"notes", "team.md"}}
	client, _ := newTestPair(t, svc)
	for _, call := range []*sdk.CallToolParams{
		{Name: toolPull, Arguments: map[string]any{"path": "/abs/notes"}},
		{Name: toolCommit, Arguments: map[string]any{"path": "/abs/notes", "message": "m"}},
	} {
		res, err := client.CallTool(context.Background(), call)
		if err != nil {
			t.Fatalf("CallTool(%s) = %v", call.Name, err)
		}
		got := decodeSuccess(t, res)
		if want := []string{"notes", "team.md"}; !slices.Equal(got.Writable, want) {
			t.Fatalf("%s success writable = %v, want %v", call.Name, got.Writable, want)
		}
	}
}

// TestErrorResultCarriesWritableArray proves the writable array rides on
// every domain error, whichever construction site built it.
func TestErrorResultCarriesWritableArray(t *testing.T) {
	svc := &fakeService{commitErr: conflictError(), readOnly: []string{"docs"}, writable: []string{"notes", "team.md"}}
	client, _ := newTestPair(t, svc)
	for _, call := range []*sdk.CallToolParams{
		{Name: toolCommit, Arguments: map[string]any{"path": "/abs/notes", "message": "conflicted"}},
		{Name: toolPull, Arguments: map[string]any{"path": "notes"}},
		{Name: toolCommit, Arguments: map[string]any{"path": "/abs/notes", "message": "  "}},
	} {
		res, err := client.CallTool(context.Background(), call)
		if err != nil {
			t.Fatalf("CallTool(%s) = %v", call.Name, err)
		}
		got := decodeToolError(t, res)
		if want := []string{"notes", "team.md"}; !slices.Equal(got.Writable, want) {
			t.Fatalf("%s error writable = %v, want %v", call.Name, got.Writable, want)
		}
	}
}

// TestErrorResultArraysOnEarlyFailure proves the arrays are attached to an
// error raised before the policy is consulted: they come from the service,
// not from the failing operation.
func TestErrorResultArraysOnEarlyFailure(t *testing.T) {
	svc := &fakeService{readOnly: []string{"docs"}, writable: []string{"notes"}}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes", "extra": 1},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	got := decodeToolError(t, res)
	if got.Code != codeInvalidRequest {
		t.Fatalf("code = %q, want %q", got.Code, codeInvalidRequest)
	}
	if !slices.Equal(got.Writable, []string{"notes"}) || !slices.Equal(got.ReadOnly, []string{"docs"}) {
		t.Fatalf("arrays = %v/%v, want the service's sets", got.Writable, got.ReadOnly)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.pulls) != 0 {
		t.Fatalf("service pulls = %v, want none for a decode failure", svc.pulls)
	}
}

// TestErrorResultArraysOnRecoveryFailure proves a recovery failure carries
// both arrays with the recovery stage untouched beside them.
func TestErrorResultArraysOnRecoveryFailure(t *testing.T) {
	svc := &fakeService{pullErr: recoveryError(), readOnly: []string{"docs"}, writable: []string{"notes"}}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	got := decodeToolError(t, res)
	if !slices.Equal(got.Writable, []string{"notes"}) || !slices.Equal(got.ReadOnly, []string{"docs"}) {
		t.Fatalf("arrays = %v/%v, want the service's sets", got.Writable, got.ReadOnly)
	}
	if got.Recovery == nil || got.Recovery.Stage != "commit.cas" {
		t.Fatalf("recovery = %+v, want the unchanged recovery report", got.Recovery)
	}
}

// TestResultsArraysEmptyWhenUnconfigured proves both arrays are present and
// empty rather than absent or null when neither set is configured.
func TestResultsArraysEmptyWhenUnconfigured(t *testing.T) {
	svc := &fakeService{result: sampleResult(), commitErr: conflictError()}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool(pull) = %v", err)
	}
	assertEmptyArrays(t, res, decodeSuccess(t, res).ReadOnly, decodeSuccess(t, res).Writable)
	errRes, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "conflicted"},
	})
	if err != nil {
		t.Fatalf("CallTool(commit) = %v", err)
	}
	gotErr := decodeToolError(t, errRes)
	assertEmptyArrays(t, errRes, gotErr.ReadOnly, gotErr.Writable)
}

// assertEmptyArrays proves both arrays decode as empty non-nil slices and
// appear as empty JSON arrays in the raw structured content.
func assertEmptyArrays(t *testing.T, res *sdk.CallToolResult, readOnly, writable []string) {
	t.Helper()
	if readOnly == nil || len(readOnly) != 0 || writable == nil || len(writable) != 0 {
		t.Fatalf("arrays = %v/%v, want two empty non-nil arrays", readOnly, writable)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	for _, want := range []string{`"readOnly":[]`, `"writable":[]`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("structured content = %s, want %s present", data, want)
		}
	}
}

// TestResultsReadOnlyArrayKeepsMeaning proves the read-only array still
// carries the read-only entries only: it never becomes a stand-in for the
// protected region and never holds a writable entry.
func TestResultsReadOnlyArrayKeepsMeaning(t *testing.T) {
	svc := &fakeService{
		result:    sampleResult(),
		commitErr: conflictError(),
		readOnly:  []string{"docs"},
		writable:  []string{"notes", "team.md"},
	}
	client, _ := newTestPair(t, svc)
	res, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolPull,
		Arguments: map[string]any{"path": "/abs/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool(pull) = %v", err)
	}
	if got := decodeSuccess(t, res).ReadOnly; !slices.Equal(got, []string{"docs"}) {
		t.Fatalf("success readOnly = %v, want only the read-only entries", got)
	}
	errRes, err := client.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      toolCommit,
		Arguments: map[string]any{"path": "/abs/notes", "message": "conflicted"},
	})
	if err != nil {
		t.Fatalf("CallTool(commit) = %v", err)
	}
	if got := decodeToolError(t, errRes).ReadOnly; !slices.Equal(got, []string{"docs"}) {
		t.Fatalf("error readOnly = %v, want only the read-only entries", got)
	}
}
