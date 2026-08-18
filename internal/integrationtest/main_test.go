package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/cli"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/tests3"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain intercepts the helper modes spawned by the process scenarios
// and terminates the shared S3-compatible container after the suite. The
// helper runs the real process body inside the test binary, so scenarios
// never invoke an external executable.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SLIVINGDOC_INTEGRATION_HELPER"); mode != "" {
		os.Exit(helperMain(mode))
	}
	code := m.Run()
	tests3.Terminate()
	os.Exit(code)
}

// helperMain runs the process body inside the spawned helper: the real
// libgit2 engine and an injected store factory (the deterministic fake, or
// a probe-failing fake for the startup-refusal scenario).
func helperMain(mode string) int {
	opts := app.ProcessOptions{
		Env: os.Environ(),
		Cwd: mustGetwd(),
		// The cache dir is the default private root, so it must be private to
		// this helper: a shared fixed path carries private state across
		// tests, across -count reruns, and across concurrent go test
		// invocations. spawnHelper supplies a per-helper temporary directory.
		CacheDir: os.Getenv(helperCacheEnv),
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Signals:  make(chan os.Signal, 1), // the helper never receives OS signals
	}
	switch mode {
	case "fake":
		opts.StoreFactory = func(ctx context.Context, cfg app.ServiceConfig) (storage.ObjectStore, error) {
			return fake.New(cfg.Prefix), nil
		}
	case "real":
		// The real S3 adapter against the environment-configured endpoint:
		// the CLI scenarios point it at the shared S3-compatible suite, so
		// state survives across one-shot pull and commit processes.
	case "bad-store":
		opts.StoreFactory = func(ctx context.Context, cfg app.ServiceConfig) (storage.ObjectStore, error) {
			store := fake.New(cfg.Prefix)
			store.FailNext(fake.OpCreate, storage.ErrTransport)
			return store, nil
		}
	default:
		fmt.Fprintln(os.Stderr, "slivingdoc: unknown integration helper mode:", mode)
		return 2
	}
	// The helper routes through the same command surface as the released
	// binary, so the scenarios cover the real entry point: subcommand
	// selection, flag parsing, and the exit code the router returns.
	return cli.Run(context.Background(), os.Args, git2.New(), opts)
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// helperCacheEnv carries the per-helper user-cache directory to the spawned
// helper, which cannot call t.TempDir itself.
const helperCacheEnv = "SLIVINGDOC_INTEGRATION_CACHE"

// helperProc is one spawned process scenario helper: the process, the
// parent's pipe ends, the stderr capture file, the helper roots, and the
// recorded stdout bytes for protocol-only-stdout proof.
type helperProc struct {
	proc          *os.Process
	stdin         *os.File // parent write end: protocol requests
	stdout        *os.File // parent read end: protocol responses
	stderr        *os.File // helper stderr: diagnostics and logs
	workspaceRoot string
	privateRoot   string
	record        *syncBuffer // every byte the SDK client read from stdout

	// waitOnce makes reaping idempotent: waitExit and the spawn cleanup both
	// reap, and a second os.Process.Wait on a reaped child fails.
	waitOnce sync.Once
	waitCode int
	waitErr  error
}

// spawnHelper starts the test binary as the process body with the given
// helper mode, a sanitized environment (never the developer's AWS
// credentials), the given extra environment entries, and the given extra
// process arguments (the helper parses them as flags). It returns the
// parent's ends of the pipes.
func spawnHelper(t *testing.T, mode string, extraEnv []string, args ...string) *helperProc {
	t.Helper()
	return spawnHelperIn(t, "", mode, extraEnv, args...)
}

// spawnHelperIn is spawnHelper with an explicit working directory, so the
// CLI scenarios can prove relative-path resolution against it. The empty
// dir inherits the test process working directory.
func spawnHelperIn(t *testing.T, dir, mode string, extraEnv []string, args ...string) *helperProc {
	t.Helper()
	workspaceRoot := t.TempDir()
	privateRoot := t.TempDir()
	cacheDir := t.TempDir()
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
	env := append(sanitizedEnv(),
		"SLIVINGDOC_INTEGRATION_HELPER="+mode,
		helperCacheEnv+"="+cacheDir,
		"SLIVINGDOC_BUCKET=integration-bucket",
		"SLIVINGDOC_PREFIX=integration-prefix",
		"SLIVINGDOC_WORKSPACE_ROOT="+workspaceRoot,
		"SLIVINGDOC_PRIVATE_ROOT="+privateRoot,
	)
	env = overrideEnv(env, extraEnv)
	argv := append([]string{os.Args[0]}, args...)
	proc, err := os.StartProcess(os.Args[0], argv, &os.ProcAttr{
		Dir:   dir,
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
		record:        &syncBuffer{},
	}
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = h.reap()
		stdin.Close()
		stdout.Close()
		stderr.Close()
	})
	return h
}

// overrideEnv applies caller-provided variables as real environment
// overrides, replacing an inherited name instead of passing duplicate
// NAME=value entries to exec. Some platforms resolve duplicate entries
// before os.Environ reaches the process, which makes configuration
// precedence scenarios test the host's implementation detail rather than
// slivingdoc's documented flag/env/default order.
func overrideEnv(env, overrides []string) []string {
	out := append([]string(nil), env...)
	for _, override := range overrides {
		name, _, ok := strings.Cut(override, "=")
		if !ok || name == "" {
			continue
		}
		filtered := out[:0]
		for _, entry := range out {
			if !strings.HasPrefix(entry, name+"=") {
				filtered = append(filtered, entry)
			}
		}
		out = append(filtered, override)
	}
	return out
}

// sanitizedEnv returns the test process environment without AWS credential
// and endpoint variables, so a spawned helper can never observe the
// developer's cloud configuration. It also drops NO_COLOR: terminal-colour
// scenarios model their own environment and must not inherit a user's output
// preference.
func sanitizedEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		name := strings.SplitN(kv, "=", 2)[0]
		switch name {
		case "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
			"AWS_PROFILE", "AWS_DEFAULT_REGION", "AWS_REGION", "AWS_ENDPOINT_URL_S3",
			"AWS_ENDPOINT_URL", "AWS_CA_BUNDLE", "AWS_SHARED_CREDENTIALS_FILE",
			"AWS_CONFIG_FILE", "SLIVINGDOC_BUCKET", "SLIVINGDOC_PREFIX",
			"SLIVINGDOC_WORKSPACE_ROOT", "SLIVINGDOC_PRIVATE_ROOT", "NO_COLOR":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// reap waits for the helper exactly once and returns the recorded outcome
// on every later call. Both waitExit and the spawn cleanup reap, and the
// second os.Process.Wait otherwise fails with "waitid: no child
// processes" and silently discard the exit status.
func (h *helperProc) reap() (int, error) {
	h.waitOnce.Do(func() {
		state, err := h.proc.Wait()
		if err != nil {
			h.waitCode, h.waitErr = -1, err
			return
		}
		h.waitCode = state.ExitCode()
	})
	return h.waitCode, h.waitErr
}

// waitExit waits for the helper to exit within the deadline and returns
// the exit code.
func (h *helperProc) waitExit(t *testing.T) int {
	t.Helper()
	done := make(chan int, 1)
	go func() {
		code, err := h.reap()
		if err != nil {
			done <- -1
			return
		}
		done <- code
	}()
	select {
	case code := <-done:
		return code
	case <-time.After(30 * time.Second):
		t.Fatal("helper did not exit within 30s")
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

// connectClient connects an SDK client to the helper over the real pipes.
// Every byte read from the helper stdout is recorded on the helper, so the
// transport scenarios can prove protocol-only stdout.
func (h *helperProc) connectClient(t *testing.T) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "integration-process", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.IOTransport{
		Reader: &recordingReadCloser{r: h.stdout, b: h.record},
		Writer: h.stdin,
	}, nil)
	if err != nil {
		t.Fatalf("client Connect() = %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// syncBuffer is a mutex-guarded byte sink. recordingReadCloser fills it
// from the SDK receive goroutine while the test goroutine reads it back, so
// a plain bytes.Buffer is a data race even after the session closes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// Bytes returns a copy of the recorded bytes, so the caller never holds a
// slice the writer can grow underneath it.
func (s *syncBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b.Bytes()...)
}

// recordingReadCloser forwards reads to the SDK and records every byte, so
// the process scenarios can prove that stdout carries protocol JSON only.
type recordingReadCloser struct {
	r io.Reader
	b *syncBuffer
}

func (r *recordingReadCloser) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.b.Write(p[:n])
	}
	return n, err
}

func (r *recordingReadCloser) Close() error { return nil }

// runStdioProcess runs the helper to completion with the given stdin bytes
// and returns the exit code, the stdout, and the stderr. It is the
// one-shot mode for configuration and startup-refusal scenarios.
func (h *helperProc) runStdioProcess(t *testing.T, stdin []byte) (int, string, string) {
	t.Helper()
	go func() {
		if len(stdin) > 0 {
			_, _ = h.stdin.Write(stdin)
		}
		_ = h.stdin.Close()
	}()
	// Drain concurrently with the wait: the pipe buffer is 64 KiB, so a
	// helper writing more than that (--help output plus protocol traffic)
	// blocks in write until the 30s waitExit deadline if the read only
	// starts after exit. ReadAll returns when the child's write end closes.
	type readResult struct {
		data []byte
		err  error
	}
	drained := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(h.stdout)
		drained <- readResult{data: data, err: err}
	}()
	code := h.waitExit(t)
	out := <-drained
	if out.err != nil {
		t.Fatalf("read helper stdout: %v", out.err)
	}
	return code, string(out.data), h.stderrText(t)
}

// assertProtocolOnlyStdout proves that every stdout line is a complete
// JSON-RPC 2.0 message object. "Protocol-only" is stricter than "valid
// JSON": a bare scalar or null line is valid JSON and still proves
// that something other than the transport wrote to stdout.
func assertProtocolOnlyStdout(t *testing.T, data []byte) {
	t.Helper()
	text := string(data)
	for line := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !jsonValid(line) {
			t.Fatalf("stdout line is not valid JSON: %q", line)
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil || msg == nil {
			t.Fatalf("stdout line is not a JSON object: %q", line)
		}
		if msg["jsonrpc"] != "2.0" {
			t.Fatalf("stdout line is not a JSON-RPC 2.0 message: %q", line)
		}
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("stdout carries no protocol messages")
	}
}
