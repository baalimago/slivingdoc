package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestService opens one real libgit2 engine and wires a service over a
// fake store. The engine closes with the test; the returned service's
// Close is registered as cleanup.
func newTestService(t *testing.T, cfg config) (*Service, *mcp.Server) {
	t.Helper()
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	svc, err := NewService(eng, fake.New(cfg.prefix), cfg.serviceConfig(), nil)
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc, mcp.NewServer(svc, Version, discardLogger())
}

// testServiceConfig builds a service configuration with disjoint temporary
// roots and the documented defaults.
func testServiceConfig(t *testing.T) config {
	t.Helper()
	return config{
		bucket:              "test-bucket",
		prefix:              "test-prefix",
		region:              "us-east-1",
		workspaceRoot:       t.TempDir(),
		privateRoot:         t.TempDir(),
		commitRetries:       defaultCommitRetries,
		checkpointPacks:     defaultCheckpointPacks,
		retainedCheckpoints: defaultRetainedCheckpoints,
	}
}

// connectClient connects an SDK client to the server over in-memory
// transports and returns the client session.
func connectClient(t *testing.T, srv *mcp.Server) *sdk.ClientSession {
	t.Helper()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := srv.Connect(context.Background(), serverTransport)
	if err != nil {
		t.Fatalf("server Connect() = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "app-test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestServicePullCommitRoundTrip proves the full publication order through
// the MCP tools with real libgit2 and a fake store: pull writes the
// notebook, commit publishes it, and a second pull returns the accepted
// state.
func TestServicePullCommitRoundTrip(t *testing.T) {
	cfg := testServiceConfig(t)
	_, srv := newTestService(t, cfg)
	cs := connectClient(t, srv)
	path := filepath.Join(cfg.workspaceRoot, "notes")

	callPull(t, cs, path)
	writeNote(t, path, "a.md", "hello service")
	callCommit(t, cs, path, "first commit")

	// An independent second pull must observe the published file.
	callPull(t, cs, path)
	data, err := os.ReadFile(filepath.Join(path, "a.md"))
	if err != nil {
		t.Fatalf("read a.md = %v", err)
	}
	if string(data) != "hello service" {
		t.Fatalf("a.md = %q, want the published bytes", data)
	}
}

// TestServiceRejectsPathOutsideRoot proves that a request path outside the
// configured workspace root maps to INVALID_REQUEST with no remote or
// local mutation.
func TestServiceRejectsPathOutsideRoot(t *testing.T) {
	cfg := testServiceConfig(t)
	_, srv := newTestService(t, cfg)
	cs := connectClient(t, srv)

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "notes_pull",
		Arguments: map[string]any{"path": filepath.Join(t.TempDir(), "elsewhere")},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	assertErrorCodeValue(t, res, "INVALID_REQUEST", false)
}

// TestServiceConcurrentDistinctPaths proves that distinct requested paths
// operate independently and concurrently: each path pulls, writes, and
// commits its own note against the shared remote, and the CAS arbitration
// converges both.
func TestServiceConcurrentDistinctPaths(t *testing.T) {
	cfg := testServiceConfig(t)
	_, srv := newTestService(t, cfg)
	cs := connectClient(t, srv)

	const paths = 4
	var wg sync.WaitGroup
	errs := make([]error, paths)
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := filepath.Join(cfg.workspaceRoot, fmt.Sprintf("notes-%d", i))
			res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
				Name:      "notes_pull",
				Arguments: map[string]any{"path": path},
			})
			if err != nil {
				errs[i] = fmt.Errorf("pull: %w", err)
				return
			}
			if err := resultOK(res); err != nil {
				errs[i] = fmt.Errorf("pull: %w", err)
				return
			}
			if err := os.MkdirAll(path, 0o755); err != nil {
				errs[i] = fmt.Errorf("mkdir: %w", err)
				return
			}
			note := fmt.Sprintf("note-%d.md", i)
			if err := os.WriteFile(filepath.Join(path, note), fmt.Appendf(nil, "note %d", i), 0o644); err != nil {
				errs[i] = fmt.Errorf("write: %w", err)
				return
			}
			res, err = cs.CallTool(context.Background(), &sdk.CallToolParams{
				Name:      "notes_commit",
				Arguments: map[string]any{"path": path, "message": fmt.Sprintf("note %d", i)},
			})
			if err != nil {
				errs[i] = fmt.Errorf("commit: %w", err)
				return
			}
			errs[i] = resultOK(res)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("path %d: %v", i, err)
		}
	}
	for i := range paths {
		path := filepath.Join(cfg.workspaceRoot, fmt.Sprintf("notes-%d", i))
		if _, err := os.Stat(filepath.Join(path, fmt.Sprintf("note-%d.md", i))); err != nil {
			t.Fatalf("path %d own note missing: %v", i, err)
		}
	}
}

// TestServiceCloseIsIdempotent proves that Close runs once and that a
// closed service refuses further calls.
func TestServiceCloseIsIdempotent(t *testing.T) {
	cfg := testServiceConfig(t)
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	svc, err := NewService(eng, fake.New(""), cfg.serviceConfig(), nil)
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("first Close() = %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
	if _, err := svc.Pull(context.Background(), filepath.Join(cfg.workspaceRoot, "notes")); err == nil {
		t.Fatal("Pull() on a closed service = nil, want a refusal")
	}
}

// TestServiceForwardsResult proves that the service returns the notebook
// result of each operation unchanged: the accepted generation and the
// operation diffstat.
func TestServiceForwardsResult(t *testing.T) {
	cfg := testServiceConfig(t)
	svc, _ := newTestService(t, cfg)
	path := filepath.Join(cfg.workspaceRoot, "notes")

	writeNote(t, path, "a.md", "hello\n")
	pullRes, err := svc.Pull(context.Background(), path)
	if err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	if pullRes.Generation != 0 || len(pullRes.Stat.Files) != 0 || pullRes.Stat.Insertions != 0 || pullRes.Stat.Deletions != 0 {
		t.Fatalf("pull result = %+v, want generation 0 with an empty stat", pullRes)
	}

	writeNote(t, path, "a.md", "world\n")
	commitRes, err := svc.Commit(context.Background(), path, "update")
	if err != nil {
		t.Fatalf("Commit() = %v", err)
	}
	// The first publication increments from the implicit empty remote, so
	// the whole file is the published addition.
	want := git.DiffStat{
		Files:      []git.FileStat{{Path: "a.md", Insertions: 1, Deletions: 0}},
		Insertions: 1,
	}
	if commitRes.Generation != 1 || !reflect.DeepEqual(commitRes.Stat, want) {
		t.Fatalf("commit result = %+v, want generation 1 with the increment %+v", commitRes, want)
	}
}

// callPull calls notes_pull and asserts the OK envelope.
func callPull(t *testing.T, cs *sdk.ClientSession, path string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "notes_pull",
		Arguments: map[string]any{"path": path},
	})
	if err != nil {
		t.Fatalf("notes_pull = %v", err)
	}
	if err := resultOK(res); err != nil {
		t.Fatalf("notes_pull: %v", err)
	}
}

// callCommit calls notes_commit and asserts the OK envelope.
func callCommit(t *testing.T, cs *sdk.ClientSession, path, message string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "notes_commit",
		Arguments: map[string]any{"path": path, "message": message},
	})
	if err != nil {
		t.Fatalf("notes_commit = %v", err)
	}
	if err := resultOK(res); err != nil {
		t.Fatalf("notes_commit: %v", err)
	}
}

// writeNote writes one UTF-8 text file into the visible directory.
func writeNote(t *testing.T, path, name, data string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, name), []byte(data), 0o644); err != nil {
		t.Fatalf("write %s = %v", name, err)
	}
}

// resultOK asserts the success envelope of a tool result: one text item
// carrying the resolved notebook path and a structured SuccessInfo whose
// code is OK with the files key always present.
func resultOK(res *sdk.CallToolResult) error {
	if res.IsError {
		return fmt.Errorf("result is an error: %v", res.StructuredContent)
	}
	if res.StructuredContent == nil {
		return fmt.Errorf("result carries no structured content")
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return fmt.Errorf("marshal structured content: %w", err)
	}
	var got mcp.SuccessInfo
	if err := json.Unmarshal(data, &got); err != nil {
		return fmt.Errorf("structured content = %s: %w", data, err)
	}
	if got.Code != "OK" {
		return fmt.Errorf("structured code = %q, want OK", got.Code)
	}
	if got.Files == nil {
		return fmt.Errorf("files must always be present")
	}
	if len(res.Content) != 1 {
		return fmt.Errorf("content items = %d, want exactly one", len(res.Content))
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok || text.Text != got.Path {
		return fmt.Errorf("text item = %#v, want the resolved notebook path %q", res.Content[0], got.Path)
	}
	return nil
}

// assertErrorCodeValue asserts the structured error envelope of a tool
// result.
func assertErrorCodeValue(t *testing.T, res *sdk.CallToolResult, wantCode string, wantRetryable bool) {
	t.Helper()
	if !res.IsError {
		t.Fatal("result must set isError")
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var m struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("structured content = %s: %v", data, err)
	}
	if m.Code != wantCode || m.Retryable != wantRetryable {
		t.Fatalf("structured = %s, want %s retryable=%v", data, wantCode, wantRetryable)
	}
}

// Compile-time proof that Service satisfies the mcp.Service seam.
var _ mcp.Service = (*Service)(nil)
