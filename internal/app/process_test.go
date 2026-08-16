package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain intercepts the helper modes spawned by the process tests: the
// child test binary runs the real process body instead of the test suite.
// os.StartProcess spawns the test binary itself, so the process never
// invokes an external executable.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SLIVINGDOC_PROCESS_HELPER"); mode != "" {
		os.Exit(helperMain(mode))
	}
	os.Exit(m.Run())
}

// helperMain runs the process body inside the spawned helper. The store
// stays faked; the engine is real libgit2 for the server mode and a
// failing fake for the startup-refusal modes.
func helperMain(mode string) int {
	p := process{
		args:     os.Args[1:],
		env:      os.Environ(),
		cwd:      mustGetwd(),
		cacheDir: filepath.Join(os.TempDir(), "slivingdoc-cache"),
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		signals:  make(chan os.Signal, 1), // the helper never receives OS signals
		storeFactory: func(context.Context, config) (storage.ObjectStore, error) {
			return fake.New(""), nil
		},
		shutdownDeadline: 30 * time.Second,
	}
	switch mode {
	case "server":
		p.engine = git2.New()
	case "bad-version":
		p.engine = &fakeEngine{openErr: &git.VersionMismatchError{
			Pinned: git2.PinnedVersion, Runtime: "0.0.0",
		}}
	case "bad-store":
		p.engine = git2.New()
		p.storeFactory = func(context.Context, config) (storage.ObjectStore, error) {
			store := fake.New("")
			store.FailNext(fake.OpCreate, storage.ErrTransport)
			return store, nil
		}
	default:
		fmt.Fprintln(os.Stderr, "slivingdoc: unknown helper mode:", mode)
		return 2
	}
	if err := run(p); err != nil {
		fmt.Fprintln(os.Stderr, "slivingdoc:", err)
		return 1
	}
	return 0
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// helperProc is one spawned helper: the process, the parent's pipe ends,
// the stderr capture file, and the helper's workspace and private roots.
type helperProc struct {
	proc          *os.Process
	stdin         *os.File // parent write end: protocol requests
	stdout        *os.File // parent read end: protocol responses
	stderr        *os.File // helper stderr: diagnostics and logs
	workspaceRoot string
	privateRoot   string
}

// spawnHelper starts the test binary as the process body with the given
// helper mode and returns the parent's ends of the pipes.
func spawnHelper(t *testing.T, mode string) *helperProc {
	t.Helper()
	workspaceRoot := t.TempDir()
	privateRoot := t.TempDir()
	stderr, err := os.CreateTemp(t.TempDir(), "helper-stderr")
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	childIn, stdin, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	// os.Pipe returns the read end first: the child writes its stdout into
	// childOut (the write end) and the parent reads the other end.
	stdout, childOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	env := append(os.Environ(),
		"SLIVINGDOC_PROCESS_HELPER="+mode,
		"SLIVINGDOC_BUCKET=process-bucket",
		"SLIVINGDOC_PREFIX=process-prefix",
		"SLIVINGDOC_WORKSPACE_ROOT="+workspaceRoot,
		"SLIVINGDOC_PRIVATE_ROOT="+privateRoot,
	)
	proc, err := os.StartProcess(os.Args[0], []string{os.Args[0]}, &os.ProcAttr{
		Env:   env,
		Files: []*os.File{childIn, childOut, stderr},
	})
	if err != nil {
		t.Fatalf("start helper: %v", err)
	}
	childIn.Close()
	childOut.Close()
	h := &helperProc{
		proc:          proc,
		stdin:         stdin,
		stdout:        stdout,
		stderr:        stderr,
		workspaceRoot: workspaceRoot,
		privateRoot:   privateRoot,
	}
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = proc.Wait()
		stdin.Close()
		stdout.Close()
		stderr.Close()
	})
	return h
}

// waitExit waits for the helper to exit within the deadline and returns
// the exit code.
func (h *helperProc) waitExit(t *testing.T) int {
	t.Helper()
	done := make(chan int, 1)
	go func() {
		state, err := h.proc.Wait()
		if err != nil {
			done <- -1
			return
		}
		done <- state.ExitCode()
	}()
	select {
	case code := <-done:
		return code
	case <-time.After(15 * time.Second):
		t.Fatal("helper did not exit within 15s")
		return -1
	}
}

// stderrText returns the captured helper stderr.
func (h *helperProc) stderrText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(h.stderr.Name())
	if err != nil {
		t.Fatalf("read stderr capture: %v", err)
	}
	return string(data)
}

// TestStdioProcessServe proves the complete stdio contract over a real
// spawned process: initialization, the two-tool listing, both tool calls,
// protocol-only stdout, stderr logs, and a clean exit when stdin closes.
func TestStdioProcessServe(t *testing.T) {
	h := spawnHelper(t, "server")
	record := &bytes.Buffer{}
	client := sdk.NewClient(&sdk.Implementation{Name: "process-test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.IOTransport{
		Reader: &recordingReadCloser{r: h.stdout, b: record},
		Writer: h.stdin,
	}, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(context.Background(), nil)
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
	for _, name := range []string{"notes_pull", "notes_commit"} {
		if !names[name] {
			t.Fatalf("tool %q missing from the listing", name)
		}
	}

	path := filepath.Join(h.workspaceRoot, "notes")
	pull := func() *sdk.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
			Name:      "notes_pull",
			Arguments: map[string]any{"path": path},
		})
		if err != nil {
			t.Fatalf("notes_pull = %v", err)
		}
		return res
	}
	assertProcessOK(t, pull())

	if err := os.WriteFile(filepath.Join(path, "a.md"), []byte("stdio notes"), 0o644); err != nil {
		t.Fatalf("write a.md = %v", err)
	}
	commit := func() *sdk.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
			Name:      "notes_commit",
			Arguments: map[string]any{"path": path, "message": "stdio commit"},
		})
		if err != nil {
			t.Fatalf("notes_commit = %v", err)
		}
		return res
	}
	assertProcessOK(t, commit())

	if err := cs.Close(); err != nil {
		t.Fatalf("client Close() = %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("helper exit code = %d, want 0; stderr:\n%s", code, h.stderrText(t))
	}

	assertProtocolOnlyStdout(t, record.Bytes())
	if got := h.stderrText(t); got == "" {
		t.Fatal("helper stderr is empty, want server logs")
	}
	if data, err := os.ReadFile(filepath.Join(path, "a.md")); err != nil || string(data) != "stdio notes" {
		t.Fatalf("a.md after the round trip = %q, %v", data, err)
	}
}

// TestStdioProcessRefusesBadVersion proves that an incompatible libgit2
// version refuses startup: the process exits nonzero with a stderr
// diagnostic and no stdout protocol bytes.
func TestStdioProcessRefusesBadVersion(t *testing.T) {
	h := spawnHelper(t, "bad-version")
	if code := h.waitExit(t); code != 1 {
		t.Fatalf("helper exit code = %d, want 1", code)
	}
	if got := h.stderrText(t); !strings.Contains(got, "does not match pinned") {
		t.Fatalf("stderr = %q, want the version-mismatch diagnostic", got)
	}
}

// TestStdioProcessRefusesBadStore proves that a failing compatibility
// probe refuses startup before any transport runs and that the diagnostic
// never echoes the disposable probe key.
func TestStdioProcessRefusesBadStore(t *testing.T) {
	h := spawnHelper(t, "bad-store")
	if code := h.waitExit(t); code != 1 {
		t.Fatalf("helper exit code = %d, want 1", code)
	}
	stderr := h.stderrText(t)
	if !strings.Contains(stderr, "compatibility probe failed") {
		t.Fatalf("stderr = %q, want the probe-failure diagnostic", stderr)
	}
	if strings.Contains(stderr, "probe/") {
		t.Fatalf("stderr leaks the probe key: %q", stderr)
	}
}

// recordingReadCloser forwards reads to the SDK and records every byte, so
// the process test can prove that stdout carries protocol JSON only.
type recordingReadCloser struct {
	r io.Reader
	b *bytes.Buffer
}

func (r *recordingReadCloser) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.b.Write(p[:n])
	}
	return n, err
}

func (r *recordingReadCloser) Close() error { return nil }

// assertProcessOK asserts the success envelope of a process-level tool
// result: one text item with exactly OK and a structured SuccessInfo
// whose code is OK with the files key always present.
func assertProcessOK(t *testing.T, res *sdk.CallToolResult) {
	t.Helper()
	if res.IsError {
		t.Fatalf("result is an error: %v", res.StructuredContent)
	}
	if res.StructuredContent == nil {
		t.Fatal("result carries no structured content")
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got mcp.SuccessInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content = %s: %v", data, err)
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
	if !ok || text.Text != "OK" {
		t.Fatalf("text item = %#v, want exactly OK", res.Content[0])
	}
}

// assertProtocolOnlyStdout proves that every stdout line is a complete
// JSON protocol message.
func assertProtocolOnlyStdout(t *testing.T, data []byte) {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if text == "" {
			continue
		}
		if !json.Valid([]byte(text)) {
			t.Fatalf("stdout line %d is not a protocol JSON message: %q", line, text)
		}
	}
	if line == 0 {
		t.Fatal("stdout carries no protocol messages")
	}
}
