package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeEngine records operations for the app without any native calls. The
// interface-plus-mock mirror is deliberate: it proves the app consumes only
// the internal/git seam.
type fakeEngine struct {
	openErr  error
	opened   bool
	closed   bool
	version  string
	features git.Features
}

func (f *fakeEngine) Open() error {
	f.opened = true
	if f.openErr != nil {
		return f.openErr
	}
	return nil
}

func (f *fakeEngine) Close() error {
	f.closed = true
	return nil
}

func (f *fakeEngine) Version() (string, error) { return f.version, nil }

func (f *fakeEngine) Features() (git.Features, error) { return f.features, nil }

// errEngineFailed is the sentinel the fake engine returns for failures.
var errEngineFailed = errors.New("app test: fake engine failed")

func (f *fakeEngine) CreateRepo(string) (git.Repository, error) { return nil, errEngineFailed }

func (f *fakeEngine) OpenRepo(string) (git.Repository, error) { return nil, errEngineFailed }

// fakeService records calls and returns nil, so serve tests exercise the
// transport lifecycle without a notebook.
type fakeService struct{}

func (fakeService) Pull(context.Context, string) error { return nil }

func (fakeService) Commit(context.Context, string, string) error { return nil }

// blockingService blocks every call until the release channel closes and
// ignores the request context, so shutdown tests can force the deadline.
// started signals that a call entered the service.
type blockingService struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingService) Pull(context.Context, string) error {
	close(b.started)
	<-b.release
	return nil
}

func (b *blockingService) Commit(context.Context, string, string) error {
	close(b.started)
	<-b.release
	return nil
}

// cancelingService blocks each call until the request context ends, so
// shutdown tests can prove that a signal cancels in-flight requests.
type cancelingService struct {
	started chan struct{}
}

func (c *cancelingService) Pull(ctx context.Context, _ string) error {
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}

func (c *cancelingService) Commit(ctx context.Context, _, _ string) error {
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}

// TestRunPropagatesInvalidConfiguration proves that invalid configuration
// returns before the engine opens.
func TestRunPropagatesInvalidConfiguration(t *testing.T) {
	engine := &fakeEngine{}
	p := testProcess(nil)
	p.engine = engine
	err := run(p)
	if err == nil {
		t.Fatal("run() = nil, want a configuration error")
	}
	if engine.opened {
		t.Fatal("invalid configuration opened the native engine")
	}
}

// TestRunPropagatesEngineOpenError proves that an engine failure is a
// startup refusal before the store or the transport runs.
func TestRunPropagatesEngineOpenError(t *testing.T) {
	engine := &fakeEngine{openErr: errEngineFailed}
	p := testProcess([]string{"SLIVINGDOC_BUCKET=bucket"})
	p.engine = engine
	err := run(p)
	if !errors.Is(err, errEngineFailed) {
		t.Fatalf("run() error = %v, want errEngineFailed", err)
	}
	if engine.closed {
		t.Fatal("run() must not close an engine it could not open")
	}
}

// TestRunServesMCPOverInjectedTransport proves the full wiring: valid
// configuration, engine open, store probe, and the two-tool MCP server
// over an injected transport. The fake engine cannot create repositories,
// so a pull reports the mapped storage failure; initialization and tool
// listing prove transport readiness.
func TestRunServesMCPOverInjectedTransport(t *testing.T) {
	engine := &fakeEngine{version: "1.9.6"}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	p := testProcess([]string{"SLIVINGDOC_BUCKET=bucket"})
	p.engine = engine
	p.transport = serverTransport

	done := make(chan error, 1)
	go func() { done <- run(p) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "app-test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() = %v", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("tools = %d, want exactly 2", len(res.Tools))
	}

	// The fake engine cannot open repositories: the service maps the
	// failure to the stable storage category.
	call, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "notes_pull",
		Arguments: map[string]any{"path": "/work/notes"},
	})
	if err != nil {
		t.Fatalf("CallTool() = %v", err)
	}
	if !call.IsError {
		t.Fatal("pull with an unavailable engine must be an error result")
	}
	data, err := json.Marshal(call.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if !containsCode(data, "STORAGE_FAILURE") {
		t.Fatalf("structured content = %s, want STORAGE_FAILURE", data)
	}

	if err := cs.Close(); err != nil {
		t.Fatalf("client Close() = %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() = %v, want a clean exit on client EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after the client disconnected")
	}
	if !engine.closed {
		t.Fatal("run() must close the engine")
	}
}

// TestServeClientEOFReturnsCleanly proves that a client closing the
// transport ends the server without error.
func TestServeClientEOFReturnsCleanly(t *testing.T) {
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	server := mcp.NewServer(&fakeService{}, Version, discardLogger())
	p := process{
		transport:        serverTransport,
		signals:          make(chan os.Signal, 1),
		shutdownDeadline: 30 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- serve(context.Background(), p, server, discardLogger()) }()

	client := sdk.NewClient(&sdk.Implementation{Name: "app-test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}
	if err := cs.Close(); err != nil {
		t.Fatalf("client Close() = %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve() = %v, want a clean exit on client EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not return after the client disconnected")
	}
}

// TestServeSignalCancelsInFlightRequests proves that a termination signal
// cancels in-flight request contexts and ends the server cleanly.
func TestServeSignalCancelsInFlightRequests(t *testing.T) {
	svc := &cancelingService{started: make(chan struct{})}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	server := mcp.NewServer(svc, Version, discardLogger())
	signals := make(chan os.Signal, 1)
	p := process{
		transport:        serverTransport,
		signals:          signals,
		shutdownDeadline: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- serve(context.Background(), p, server, discardLogger()) }()

	client := sdk.NewClient(&sdk.Implementation{Name: "app-test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}
	defer cs.Close()

	callDone := make(chan struct{})
	go func() {
		defer close(callDone)
		_, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{
			Name:      "notes_pull",
			Arguments: map[string]any{"path": "/work/notes"},
		})
	}()
	<-svc.started

	signals <- os.Interrupt
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve() = %v, want a clean signal shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not stop after the signal")
	}
	<-callDone
}

// TestServeShutdownDeadlineExpires proves that an in-flight operation that
// ignores cancellation forces the bounded shutdown and a nonzero
// diagnostic.
func TestServeShutdownDeadlineExpires(t *testing.T) {
	svc := &blockingService{started: make(chan struct{}), release: make(chan struct{})}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	server := mcp.NewServer(svc, Version, discardLogger())
	signals := make(chan os.Signal, 1)
	p := process{
		transport:        serverTransport,
		signals:          signals,
		shutdownDeadline: 150 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() { done <- serve(context.Background(), p, server, discardLogger()) }()
	t.Cleanup(func() { close(svc.release) })

	client := sdk.NewClient(&sdk.Implementation{Name: "app-test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}
	defer cs.Close()

	go func() {
		_, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{
			Name:      "notes_pull",
			Arguments: map[string]any{"path": "/work/notes"},
		})
	}()
	<-svc.started // the handler is blocked and ignores cancellation

	signals <- os.Interrupt
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("serve() = nil, want a shutdown-deadline error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not stop after the deadline")
	}
}

// discardLogger drops server activity, keeping serve tests quiet.
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// containsCode reports whether the marshaled structured error carries the
// given code.
func containsCode(data []byte, want string) bool {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	return m["code"] == want
}
