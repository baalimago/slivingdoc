package integrationtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/testminio"
	"github.com/baalimago/slivingdoc/internal/workspace"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// codeRecoveryFailure is the only error category whose envelope may carry
// the recovery report (architecture section 2, L26).
const codeRecoveryFailure = "RECOVERY_FAILURE"

// HarnessConfig wires one black-box harness. The zero store builds the
// real s3store adapter against the shared MinIO suite on a fresh per-test
// prefix; scenarios substitute the deterministic fake or the fault
// wrappers through Store.
type HarnessConfig struct {
	// Store is the base object store. Nil builds real MinIO.
	Store storage.ObjectStore
	// Prefix is the S3 protocol prefix of the store. For MinIO the empty
	// value uses a fresh per-test prefix; for an injected store the caller
	// must set the prefix the store was built with.
	Prefix string
	// Bucket and Endpoint feed the storage identity; they default to the
	// MinIO suite values when Store is nil and are required for an
	// injected store (the fake uses "test-bucket" and no endpoint).
	Bucket   string
	Endpoint string
	// Hooks are the service failpoints; nil leaves them disabled.
	Hooks *app.ServiceHooks
	// RetryLimit, CheckpointPacks, and RetainedCheckpoints override the
	// documented defaults (8, 1024, 1). They are pointers because zero is a
	// documented value of two of them (no retries, no retained generation),
	// which a plain int cannot distinguish from "unset".
	RetryLimit       *int
	CheckpointPacks  *int
	RetainedCheckpts *int
}

// setting returns the pointed-to override, or def when the field is unset.
func setting(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}

// Harness is one black-box server wiring: the real app service over the
// recorded and fault-injecting store seam, one MCP server, named client
// sessions, a scoped log capture, and per-test workspace and private
// roots. The only entry into the server is MCP JSON-RPC; scenarios never
// call notebook, git, workspace, or storage functions directly.
type Harness struct {
	t      *testing.T
	engine git.Engine

	raw    storage.ObjectStore // base store for assertions
	faults *faultStore         // injection and barrier layer
	store  *Recorder           // the seam served to the app

	cfg   app.ServiceConfig
	hooks *app.ServiceHooks
	svc   *app.Service
	srv   *mcp.Server
	logs  *LogCapture

	mu             sync.Mutex // guards sessions
	sessions       map[string]*sdk.ClientSession
	serverSessions []*sdk.ServerSession

	workspaceRoot string
	privateRoot   string
}

// NewHarness wires one harness: the engine, the store stack, the app
// service, the MCP server, and the scoped logger. The startup compatibility
// probe does not run here (it lives in the process body), so store counters
// start at zero.
func NewHarness(t *testing.T, cfg HarnessConfig) *Harness {
	t.Helper()
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	var raw storage.ObjectStore
	bucket := cfg.Bucket
	endpoint := cfg.Endpoint
	prefix := cfg.Prefix
	if cfg.Store == nil {
		suite := testminio.Ensure(t)
		if prefix == "" {
			prefix = suite.FreshPrefix("integrationtest")
		}
		if bucket == "" {
			bucket = testminio.Bucket
		}
		if endpoint == "" {
			endpoint = suite.Endpoint
		}
		mc := suite.StoreConfig()
		st, err := s3store.New(context.Background(), s3store.Config{
			Bucket: bucket, Prefix: prefix, Region: mc.Region,
			Endpoint: mc.Endpoint, AccessKey: mc.AccessKey, SecretKey: mc.SecretKey,
		})
		if err != nil {
			t.Fatalf("s3store.New(%q) = %v", prefix, err)
		}
		raw = st
	} else {
		raw = cfg.Store
		if bucket == "" {
			bucket = "test-bucket"
		}
	}
	if prefix == "" {
		t.Fatal("a protocol prefix is required for an injected store")
	}

	workspaceRoot := t.TempDir()
	privateRoot := t.TempDir()
	serviceCfg := app.ServiceConfig{
		Bucket:              bucket,
		Prefix:              prefix,
		Region:              "us-east-1",
		Endpoint:            endpoint,
		WorkspaceRoot:       workspaceRoot,
		PrivateRoot:         privateRoot,
		CommitRetries:       setting(cfg.RetryLimit, notebook.DefaultRetryLimit),
		CheckpointPacks:     setting(cfg.CheckpointPacks, notebook.DefaultCheckpointPacks),
		RetainedCheckpoints: setting(cfg.RetainedCheckpts, notebook.DefaultRetainedCheckpoints),
	}
	hooks := cfg.Hooks
	if hooks == nil {
		hooks = &app.ServiceHooks{}
	}
	faults := NewFaultStore(raw)
	store := NewRecorder(faults)
	svc, err := app.NewService(eng, store, serviceCfg, hooks)
	if err != nil {
		t.Fatalf("app.NewService() = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	logs := NewLogCapture()
	h := &Harness{
		t:             t,
		engine:        eng,
		raw:           raw,
		faults:        faults,
		store:         store,
		cfg:           serviceCfg,
		hooks:         hooks,
		svc:           svc,
		logs:          logs,
		sessions:      map[string]*sdk.ClientSession{},
		workspaceRoot: workspaceRoot,
		privateRoot:   privateRoot,
	}
	h.srv = mcp.NewServer(svc, app.Version, logs.Logger())
	return h
}

// Client returns the named in-memory MCP client session, connecting it on
// first use. The empty name is the "default" session. Sessions close with
// the harness cleanup.
func (h *Harness) Client(name string) *sdk.ClientSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name == "" {
		name = "default"
	}
	if cs, ok := h.sessions[name]; ok {
		return cs
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := h.srv.Connect(context.Background(), serverTransport)
	if err != nil {
		h.t.Fatalf("server Connect() = %v", err)
	}
	h.serverSessions = append(h.serverSessions, serverSession)
	client := sdk.NewClient(&sdk.Implementation{Name: "integrationtest", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		h.t.Fatalf("client Connect() = %v", err)
	}
	h.sessions[name] = cs
	return cs
}

// PrivateRoot returns the per-test configured private root.
func (h *Harness) PrivateRoot() string { return h.privateRoot }

// Path returns the request path below the workspace root.
func (h *Harness) Path(rel string) string {
	return filepath.Join(h.workspaceRoot, rel)
}

// Recorder returns the store recorder served to the app.
func (h *Harness) Recorder() *Recorder { return h.store }

// Faults returns the fault-injecting store layer.
func (h *Harness) Faults() *faultStore { return h.faults }

// Raw returns the base store, used for assertions that must not see
// injected faults (corrupt reads, missing objects).
func (h *Harness) Raw() storage.ObjectStore { return h.raw }

// Logs returns the scoped log capture of this harness.
func (h *Harness) Logs() *LogCapture { return h.logs }

// WorkspaceFailpoints returns the workspace failpoint hooks of the
// harness service.
func (h *Harness) WorkspaceFailpoints() *workspace.Failpoints {
	return h.hooks.Workspace
}

// NotebookFailpoints returns the notebook failpoint hooks of the harness
// service.
func (h *Harness) NotebookFailpoints() *notebook.Failpoints {
	return h.hooks.Notebook
}

// Identity returns the storage identity of the harness service, for
// derived-key assertions.
func (h *Harness) Identity() workspace.Identity {
	return workspace.Identity{
		Endpoint:        h.cfg.Endpoint,
		Region:          h.cfg.Region,
		Bucket:          h.cfg.Bucket,
		Prefix:          h.cfg.Prefix,
		ManifestVersion: workspace.ManifestVersion,
	}
}

// PrivateDir returns the derived private directory of a request path.
func (h *Harness) PrivateDir(path string) string {
	return filepath.Join(h.privateRoot, workspace.DerivedKey(path, h.Identity()))
}

// CallTool performs one MCP tool call on the named session and returns the
// raw result; a protocol error is a test failure.
func (h *Harness) CallTool(name, tool, path, message string) *sdk.CallToolResult {
	cs := h.Client(name)
	args := map[string]any{"path": path}
	if tool == toolCommit {
		args["message"] = message
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		h.t.Fatalf("%s(%s) = %v", tool, path, err)
	}
	return res
}

// Pull performs one notes_pull and returns the raw result.
func (h *Harness) Pull(name, path string) *sdk.CallToolResult {
	return h.CallTool(name, toolPull, path, "")
}

// Commit performs one notes_commit and returns the raw result.
func (h *Harness) Commit(name, path, message string) *sdk.CallToolResult {
	return h.CallTool(name, toolCommit, path, message)
}

// WriteFile writes one UTF-8 text file at an absolute host path.
func (h *Harness) WriteFile(path, data string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatalf("MkdirAll(%s) = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		h.t.Fatalf("WriteFile(%s) = %v", path, err)
	}
}

// ReadFile reads one file and fails the test when it is absent.
func (h *Harness) ReadFile(path string) string {
	h.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatalf("ReadFile(%s) = %v", path, err)
	}
	return string(data)
}

// RemoveFile removes one file.
func (h *Harness) RemoveFile(path string) {
	h.t.Helper()
	if err := os.Remove(path); err != nil {
		h.t.Fatalf("RemoveFile(%s) = %v", path, err)
	}
}

// FSSnapshot returns every file under dir with its exact bytes, keyed by
// slash-form path relative to dir. A walk failure fails the test: an empty
// snapshot must mean "the directory is empty", never "the directory was
// not readable". Otherwise an assertion of the form "no file was
// imported" passes on a missing directory.
func (h *Harness) FSSnapshot(dir string) map[string]string {
	h.t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		key, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(key)] = string(data)
		return nil
	})
	if err != nil {
		h.t.Fatalf("snapshot %s: %v", dir, err)
	}
	return out
}

// Manifest reads and decodes the authoritative current manifest through
// the raw store, failing the test when it is absent or invalid.
func (h *Harness) Manifest() storage.Manifest {
	h.t.Helper()
	data, err := h.ReadObject(storage.CurrentKey)
	if err != nil {
		h.t.Fatalf("read current manifest: %v", err)
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		h.t.Fatalf("decode current manifest: %v", err)
	}
	return m
}

// ManifestAbsent reports whether the current manifest object is absent.
func (h *Harness) ManifestAbsent() bool {
	_, err := h.ReadObject(storage.CurrentKey)
	return err != nil
}

// ReadObject reads one object through the raw store.
func (h *Harness) ReadObject(key string) ([]byte, error) {
	rc, _, err := h.raw.ReadObject(context.Background(), key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// ObjectExists reports whether an object exists in the raw store.
func (h *Harness) ObjectExists(key string) bool {
	rc, _, err := h.raw.ReadObject(context.Background(), key)
	if err != nil {
		return false
	}
	rc.Close()
	return true
}

// ListObjects lists the protocol keys below a prefix through the raw
// store.
func (h *Harness) ListObjects(prefix string) []string {
	var keys []string
	if err := h.raw.ListObjects(context.Background(), prefix, func(key string) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		h.t.Fatalf("ListObjects(%q) = %v", prefix, err)
	}
	return keys
}

// settleTimeout bounds every polled assertion. Checkpoint and cleanup run
// inline on the commit call path, so a correct scenario settles in
// milliseconds; a bound well below the package test timeout turns a genuine
// regression into one failed scenario instead of a whole-binary panic that
// hides which scenario broke.
const settleTimeout = 3 * time.Second

// eventually polls fn until it returns nil or the deadline passes, and
// reports the last observed error. It must only be called from the test
// goroutine it is given.
func (h *Harness) eventually(t *testing.T, timeout time.Duration, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := fn()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition did not settle within %s (last error: %v)", timeout, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// assertOK asserts the exact success envelope: one text item exactly OK
// and no structured content.
func (h *Harness) assertOK(t *testing.T, res *sdk.CallToolResult) {
	t.Helper()
	if res.IsError {
		t.Fatalf("result is an error: %v", res.StructuredContent)
	}
	if res.StructuredContent != nil {
		t.Fatalf("result carries structured content: %v", res.StructuredContent)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content items = %d, want exactly one", len(res.Content))
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok || text.Text != "OK" {
		t.Fatalf("text item = %#v, want exactly OK", res.Content[0])
	}
}

// assertEnvelope asserts the full envelope expectation of one call result.
// Every error envelope is also checked for the shape invariants: a
// non-empty message, the files key always present, and no recovery report
// outside RECOVERY_FAILURE (architecture section 2).
func (h *Harness) assertEnvelope(t *testing.T, call ToolCall, res *sdk.CallToolResult) {
	t.Helper()
	exp := call.Expect
	if exp.OK {
		h.assertOK(t, res)
		for _, sub := range exp.NoText {
			text, ok := res.Content[0].(*sdk.TextContent)
			if !ok || strings.Contains(text.Text, sub) {
				t.Fatalf("call %s(%s) result contains forbidden substring %q", call.Tool, call.Path, sub)
			}
		}
		return
	}
	env := decodeEnvelope(t, call, res)
	if env.Code != exp.ErrorCode {
		t.Fatalf("call %s(%s) code = %q, want %q", call.Tool, call.Path, env.Code, exp.ErrorCode)
	}
	if exp.Retryable != nil && env.Retryable != *exp.Retryable {
		t.Fatalf("call %s(%s) retryable = %v, want %v", call.Tool, call.Path, env.Retryable, *exp.Retryable)
	}
	if exp.Files != nil {
		got := env.Files
		if len(got) != len(exp.Files) {
			t.Fatalf("call %s(%s) files = %v, want %v", call.Tool, call.Path, got, exp.Files)
		}
		for i := range exp.Files {
			if got[i].Path != exp.Files[i].Path {
				t.Fatalf("call %s(%s) file %d path = %q, want %q", call.Tool, call.Path, i, got[i].Path, exp.Files[i].Path)
			}
			if exp.Files[i].Ranges == nil {
				continue // the row asserts the path only
			}
			if len(got[i].Ranges) != len(exp.Files[i].Ranges) {
				t.Fatalf("call %s(%s) file %d ranges = %v, want %v", call.Tool, call.Path, i, got[i].Ranges, exp.Files[i].Ranges)
			}
			for j := range exp.Files[i].Ranges {
				if got[i].Ranges[j].Start != exp.Files[i].Ranges[j].Start || got[i].Ranges[j].End != exp.Files[i].Ranges[j].End {
					t.Fatalf("call %s(%s) file %d range %d = %+v, want %+v", call.Tool, call.Path, i, j, got[i].Ranges[j], exp.Files[i].Ranges[j])
				}
			}
		}
	}
	if exp.Recovery != nil {
		if env.Recovery == nil {
			t.Fatalf("call %s(%s) carries no recovery report", call.Tool, call.Path)
		}
		if env.Recovery.Stage != exp.Recovery.Stage || env.Recovery.RemoteAccepted != exp.Recovery.RemoteAccepted {
			t.Fatalf("call %s(%s) recovery = %+v, want stage=%s accepted=%s", call.Tool, call.Path, env.Recovery, exp.Recovery.Stage, exp.Recovery.RemoteAccepted)
		}
		if exp.Recovery.Resynchronized != nil && env.Recovery.Resynchronized != *exp.Recovery.Resynchronized {
			t.Fatalf("call %s(%s) resynchronized = %v, want %v", call.Tool, call.Path, env.Recovery.Resynchronized, *exp.Recovery.Resynchronized)
		}
	}
	for _, sub := range exp.NoText {
		data, _ := json.Marshal(res.StructuredContent)
		if strings.Contains(string(data), sub) || strings.Contains(env.Message, sub) {
			t.Fatalf("call %s(%s) result contains forbidden substring %q", call.Tool, call.Path, sub)
		}
	}
}

// envelope is the structured error object of the tool-error shape
// (architecture section 2): code, retryable, message, and files are always
// present; recovery appears only for RECOVERY_FAILURE.
type envelope struct {
	Code      string            `json:"code"`
	Retryable bool              `json:"retryable"`
	Message   string            `json:"message"`
	Files     []envelopeFile    `json:"files"`
	Recovery  *envelopeRecovery `json:"recovery"`
}

type envelopeFile struct {
	Path   string          `json:"path"`
	Ranges []envelopeRange `json:"ranges"`
}

type envelopeRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type envelopeRecovery struct {
	Stage          string `json:"stage"`
	RemoteAccepted string `json:"remoteAccepted"`
	Resynchronized bool   `json:"resynchronized"`
}

// decodeEnvelope unmarshals the structured content of an error result and
// proves the envelope shape invariants: non-empty message, files always
// present, and recovery only for RECOVERY_FAILURE.
func decodeEnvelope(t *testing.T, call ToolCall, res *sdk.CallToolResult) envelope {
	t.Helper()
	if !res.IsError {
		t.Fatalf("call %s(%s) must be an error result", call.Tool, call.Path)
	}
	if res.StructuredContent == nil {
		t.Fatalf("call %s(%s) error result carries no structured content", call.Tool, call.Path)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("structured content = %s: %v", data, err)
	}
	if env.Message == "" {
		t.Fatalf("call %s(%s) error envelope carries an empty message", call.Tool, call.Path)
	}
	if env.Files == nil {
		t.Fatalf("call %s(%s) error envelope carries no files key", call.Tool, call.Path)
	}
	if env.Code != codeRecoveryFailure && env.Recovery != nil {
		t.Fatalf("call %s(%s) carries a recovery report for %s", call.Tool, call.Path, env.Code)
	}
	return env
}

// assertExpectations asserts the final state of a scenario: the visible
// files, the authoritative manifest, the store counters, and the log
// capture. Every condition polls until it settles; polling removes the
// checked-too-early race, it does not mask a wrong final value.
func (h *Harness) assertExpectations(t *testing.T, exp Expectations) {
	t.Helper()
	h.eventually(t, settleTimeout, func() error {
		for path, want := range exp.FS.Files {
			got, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("file %s: %v", path, err)
			}
			if string(got) != want {
				return fmt.Errorf("file %s = %q, want %q", path, got, want)
			}
		}
		for path, sub := range exp.FS.Contains {
			got, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("file %s: %v", path, err)
			}
			if !strings.Contains(string(got), sub) {
				return fmt.Errorf("file %s does not contain %q", path, sub)
			}
		}
		for _, path := range exp.FS.Missing {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("file %s exists, want absent", path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat %s: %v", path, err)
			}
		}
		if exp.S3.NoCurrent != nil && *exp.S3.NoCurrent != h.ManifestAbsent() {
			return fmt.Errorf("current presence mismatch")
		}
		if exp.S3.NoPacks != nil && *exp.S3.NoPacks {
			if len(h.ListObjects("packs/")) > 0 {
				return fmt.Errorf("packs exist, want none")
			}
		}
		if c := exp.S3.Counts; c != nil {
			snap := h.store.Snapshot()
			if c.AllZero {
				for _, op := range AllOps {
					if snap[op] != 0 {
						return fmt.Errorf("store counter %s = %d, want 0", op, snap[op])
					}
				}
			}
			for op, want := range c.Ops {
				if snap[op] != want {
					return fmt.Errorf("store counter %s = %d, want %d", op, snap[op], want)
				}
			}
		}
		if logs := exp.Logs; logs != nil {
			for _, sub := range logs.WarnContains {
				if !anyContains(h.logs.Warnings(), sub) {
					return fmt.Errorf("no warning contains %q", sub)
				}
			}
			for _, sub := range logs.NoSubstring {
				if strings.Contains(h.logs.String(), sub) {
					return fmt.Errorf("log capture contains forbidden substring %q", sub)
				}
			}
		}
		return nil
	})
}

// anyContains reports whether any record's message or attribute text
// contains sub.
func anyContains(records []Record, sub string) bool {
	for _, r := range records {
		if strings.Contains(r.Msg, sub) {
			return true
		}
		for _, v := range r.Attrs {
			if strings.Contains(fmt.Sprint(v), sub) {
				return true
			}
		}
	}
	return false
}
