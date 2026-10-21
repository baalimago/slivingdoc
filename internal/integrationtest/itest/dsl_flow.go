package itest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// assertOneRedactedDiagnostic proves a startup refusal is the documented
// shape (architecture/cli.md): exit nonzero, an empty stdout, and
// exactly one redacted diagnostic line on stderr. forbidden names values
// that must never reach the diagnostic.
func AssertOneRedactedDiagnostic(t *testing.T, code int, stdout, stderr string, forbidden ...string) {
	t.Helper()
	if code != 1 {
		t.Fatalf("invalid configuration exit = %d, want 1; stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("invalid configuration wrote stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "invalid configuration") {
		t.Fatalf("invalid configuration stderr = %q, want a startup diagnostic", stderr)
	}
	if strings.Count(strings.TrimSpace(stderr), "\n") != 0 {
		t.Fatalf("invalid configuration stderr = %q, want exactly one line", stderr)
	}
	for _, secret := range forbidden {
		if strings.Contains(stderr, secret) {
			t.Fatalf("invalid configuration stderr = %q, must not carry %q", stderr, secret)
		}
	}
}

// processError calls one process tool and returns its complete stable error
// envelope. It uses the SDK transport exactly as an MCP host does.
func ProcessError(t *testing.T, cs *sdk.ClientSession, tool, message string) Envelope {
	t.Helper()
	args := map[string]any{}
	if tool == ToolCommit {
		args["message"] = message
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s(%q) = %v", tool, message, err)
	}
	return DecodeEnvelope(t, ToolCall{Tool: tool, Message: message}, res)
}

// NewFakeHarness wires a harness over a fresh deterministic fake store. The
// catalog runs most scenarios over the fake; the rows whose observable
// behavior depends on real HTTP conditional writes (CAS races, the stale
// reader, checkpoint contention) pass an explicit real-backend
// configuration instead.
func NewFakeHarness(t *testing.T, cfg HarnessConfig) *Harness {
	t.Helper()
	if cfg.Store == nil {
		cfg.Store = fake.New("scenario")
		cfg.Prefix = "scenario"
	}
	return NewHarness(t, cfg)
}

// newSharedHarness wires a second harness over the same store and prefix
// as an existing harness, so two callers observe each other's
// publications. The harness isolates its own workspace, private root,
// recorder, fault wrapper, and log capture; only the object store is
// shared.
func NewSharedHarness(t *testing.T, store storage.ObjectStore, prefix string, cfg HarnessConfig) *Harness {
	t.Helper()
	cfg.Store = store
	cfg.Prefix = prefix
	return NewHarness(t, cfg)
}

// callWithArgs performs one tool call with arbitrary Arguments through the
// named session, returning the raw result. It bypasses the canonical Call
// wrapper for strict-schema rows that must send extra, null, oversized, or
// otherwise unusual fields.
func CallWithArgs(t *testing.T, h *Harness, name, tool string, args any) *sdk.CallToolResult {
	t.Helper()
	res, err := CallWithArgsRaw(h.Client(name), tool, args)
	if err != nil {
		t.Fatalf("%s with arbitrary args = %v", tool, err)
	}
	return res
}

// callProtocolError performs one tool call that must fail at the protocol
// level (a malformed JSON frame) and returns the error the SDK reports.
func CallProtocolError(t *testing.T, h *Harness, name, tool string, args any) error {
	t.Helper()
	_, err := CallWithArgsRaw(h.Client(name), tool, args)
	if err == nil {
		t.Fatalf("%s with %v = nil, want a protocol error", tool, args)
	}
	return err
}

func CallWithArgsRaw(cs *sdk.ClientSession, tool string, args any) (*sdk.CallToolResult, error) {
	return cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
}

// callResult is one raw tool-call outcome forwarded from a scenario
// goroutine; the main goroutine asserts it after the coordination finished.
type CallResult struct {
	res *sdk.CallToolResult
	err error
}

// runCall starts one tool call on the named harness session in a goroutine
// and returns the channel the result arrives on, so the main goroutine can
// perform mid-call actions (store barriers, advance a second writer) and
// then assert the envelope. The session is resolved on the calling
// goroutine, so two concurrent calls of one scenario can use two distinct
// sessions. The call carries a bounded context: a scenario whose barrier is
// never released fails its own call instead of hanging the package until
// the binary timeout.
func RunCall(t *testing.T, h *Harness, session string, tool, message string) chan CallResult {
	t.Helper()
	cs := h.Client(session)
	args := map[string]any{}
	if tool == ToolCommit {
		args["message"] = message
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*SettleTimeout)
	ch := make(chan CallResult, 1)
	go func() {
		defer cancel()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
		ch <- CallResult{res: res, err: err}
	}()
	return ch
}

// awaitCall waits for one goroutine tool call and asserts the expected
// envelope on the main goroutine.
func (h *Harness) AwaitCall(t *testing.T, ch chan CallResult, call ToolCall) {
	t.Helper()
	out := <-ch
	if out.err != nil {
		t.Fatalf("call %s(%s) = %v", call.Tool, call.Path, out.err)
	}
	h.AssertEnvelope(t, call, out.res)
}

// commitFirst performs the canonical publication used by most scenarios:
// write one file, pull (initializing P from the empty remote), and commit
// the root publication.
func CommitFirst(t *testing.T, h *Harness, name, data, message string) {
	t.Helper()
	h.WriteFile(h.WorkspaceRoot()+"/"+name, data)
	h.AssertOK(t, h.Pull(""))
	h.AssertOK(t, h.Commit("", message))
}

// commitNext writes one more file into the server's notebook directory and
// publishes an increment.
func CommitNext(t *testing.T, h *Harness, name, data, message string) {
	t.Helper()
	h.WriteFile(h.WorkspaceRoot()+"/"+name, data)
	h.AssertOK(t, h.Commit("", message))
}

// putJunk seeds a raw object into the harness store without any manifest
// reference, for the cleanup-fence and malformed-key scenarios.
func PutJunk(t *testing.T, h *Harness, key string) {
	t.Helper()
	if err := h.Raw().PutObject(context.Background(), key, strings.NewReader("junk"), storage.Metadata{}); err != nil {
		t.Fatalf("seed junk %s: %v", key, err)
	}
}

// tokenEnvelopeResult builds a synthetic error result with the given structured content.
func TokenEnvelopeResult(content map[string]any) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, StructuredContent: content}
}

// objectGone reports whether a raw object is absent. It is called from
// inside polling closures, so a transient read failure is reported as
// "not yet gone" and retried rather than aborting the scenario.
func ObjectGone(h *Harness, key string) (bool, error) {
	rc, _, err := h.Raw().ReadObject(context.Background(), key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	rc.Close()
	return false, nil
}

// requireObjectGone polls until the object is absent, retrying transient
// read failures.
func RequireObjectGone(t *testing.T, h *Harness, key string) {
	t.Helper()
	h.Eventually(t, SettleTimeout, func() error {
		gone, err := ObjectGone(h, key)
		if err != nil {
			return err
		}
		if !gone {
			return fmt.Errorf("object %s still exists", key)
		}
		return nil
	})
}

// assertRemoteGeneration asserts the recorded private baseline generation
// of the server's notebook directory.
func AssertRemoteGeneration(t *testing.T, h *Harness, want uint64) {
	t.Helper()
	rec := h.StateRecord(t)
	if rec.RemoteGeneration != want {
		t.Fatalf("state remoteGeneration = %d, want %d", rec.RemoteGeneration, want)
	}
}

// assertPulledMarker asserts the durable pull marker of the server's
// notebook directory.
func AssertPulledMarker(t *testing.T, h *Harness) {
	t.Helper()
	if _, err := os.Stat(h.PrivateDir() + "/pulled"); err != nil {
		t.Fatalf("pulled marker: %v", err)
	}
}

// assertVisibleFiles asserts the exact visible-file set of the server's
// notebook directory, keyed by slash-form relative path.
func AssertVisibleFiles(t *testing.T, h *Harness, want map[string]string) {
	t.Helper()
	got := h.FSSnapshot(h.WorkspaceRoot())
	if len(got) != len(want) {
		t.Fatalf("visible files = %v, want %v", got, want)
	}
	for name, data := range want {
		if got[name] != data {
			t.Fatalf("visible file %s = %q, want %q", name, got[name], data)
		}
	}
}

// MCPReqIDRE matches the correlation ID the MCP server attaches to every
// tool-call record (internal/mcp: a 16-hex-char mcpReqID attribute).
var MCPReqIDRE = regexp.MustCompile(`mcpReqID=([0-9a-f]{16})`)

// assertToolCallLogged proves the helper stderr carries the correlated
// record pair of one tool call: the MCP server logs a start record and a
// completion record that share one mcpReqID and name the tool.
func AssertToolCallLogged(t *testing.T, stderr, tool string) {
	t.Helper()
	started, completed := map[string]bool{}, map[string]bool{}
	for line := range strings.SplitSeq(stderr, "\n") {
		if !strings.Contains(line, " tool="+tool) {
			continue
		}
		id := MCPReqIDRE.FindStringSubmatch(line)
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
// tool call: no error, one text item carrying the server's notebook
// directory (suffixed with the read-only set when configured), and a
// structured SuccessInfo whose code is OK. It returns the decoded success
// envelope.
func AssertProcessCallOK(t *testing.T, cs *sdk.ClientSession, tool, path, message string) mcp.SuccessInfo {
	t.Helper()
	args := map[string]any{}
	if tool == ToolCommit {
		args["message"] = message
	}
	return AssertProcessArgsOK(t, cs, tool, path, args)
}

// assertProcessArgsOK is assertProcessCallOK over an explicit argument
// object, so a scenario can send an unusual input. A non-empty path pins
// the reported directory; a diagnostic placeholder does not. It returns the
// decoded success envelope.
func AssertProcessArgsOK(t *testing.T, cs *sdk.ClientSession, tool, path string, args map[string]any) mcp.SuccessInfo {
	t.Helper()
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
	if path != "" && got.Path != path {
		t.Fatalf("%s reports the directory %q, want %q", tool, got.Path, path)
	}
	// A configured read-only set suffixes the text item
	// (architecture/product-contract.md).
	wantText := SuccessText(got)
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok || MaskFileCount(text.Text) != wantText {
		t.Fatalf("%s(%s) text = %#v, want %q", tool, path, res.Content[0], wantText)
	}
	return got
}
