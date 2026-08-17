package integrationtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newFakeHarness wires a harness over a fresh deterministic fake store. The
// catalog runs most scenarios over the fake; the rows whose observable
// behavior depends on real HTTP conditional writes (CAS races, the stale
// reader, checkpoint contention) pass an explicit real-backend
// configuration instead.
func newFakeHarness(t *testing.T, cfg HarnessConfig) *Harness {
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
func newSharedHarness(t *testing.T, store storage.ObjectStore, prefix string, cfg HarnessConfig) *Harness {
	t.Helper()
	cfg.Store = store
	cfg.Prefix = prefix
	return NewHarness(t, cfg)
}

// callWithArgs performs one tool call with arbitrary Arguments through the
// named session, returning the raw result. It bypasses the canonical Call
// wrapper for strict-schema rows that must send extra, null, oversized, or
// otherwise unusual fields.
func callWithArgs(t *testing.T, h *Harness, name, tool string, args any) *sdk.CallToolResult {
	t.Helper()
	res, err := callWithArgsRaw(h.Client(name), tool, args)
	if err != nil {
		t.Fatalf("%s with arbitrary args = %v", tool, err)
	}
	return res
}

// callProtocolError performs one tool call that must fail at the protocol
// level (a malformed JSON frame) and returns the error the SDK reports.
func callProtocolError(t *testing.T, h *Harness, name, tool string, args any) error {
	t.Helper()
	_, err := callWithArgsRaw(h.Client(name), tool, args)
	if err == nil {
		t.Fatalf("%s with %v = nil, want a protocol error", tool, args)
	}
	return err
}

func callWithArgsRaw(cs *sdk.ClientSession, tool string, args any) (*sdk.CallToolResult, error) {
	return cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
}

// callResult is one raw tool-call outcome forwarded from a scenario
// goroutine; the main goroutine asserts it after the coordination finished.
type callResult struct {
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
func runCall(t *testing.T, h *Harness, session string, tool, path, message string) chan callResult {
	t.Helper()
	cs := h.Client(session)
	args := map[string]any{"path": path}
	if tool == toolCommit {
		args["message"] = message
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*settleTimeout)
	ch := make(chan callResult, 1)
	go func() {
		defer cancel()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
		ch <- callResult{res: res, err: err}
	}()
	return ch
}

// awaitCall waits for one goroutine tool call and asserts the expected
// envelope on the main goroutine.
func (h *Harness) awaitCall(t *testing.T, ch chan callResult, call ToolCall) {
	t.Helper()
	out := <-ch
	if out.err != nil {
		t.Fatalf("call %s(%s) = %v", call.Tool, call.Path, out.err)
	}
	h.assertEnvelope(t, call, out.res)
}

// commitFirst performs the canonical publication used by most scenarios:
// write one file, pull (initializing P from the empty remote), and commit
// the root publication.
func commitFirst(t *testing.T, h *Harness, path, name, data, message string) {
	t.Helper()
	h.WriteFile(path+"/"+name, data)
	h.assertOK(t, h.Pull("", path))
	h.assertOK(t, h.Commit("", path, message))
}

// commitNext writes one more file into an existing path and publishes an
// increment.
func commitNext(t *testing.T, h *Harness, path, name, data, message string) {
	t.Helper()
	h.WriteFile(path+"/"+name, data)
	h.assertOK(t, h.Commit("", path, message))
}

// putJunk seeds a raw object into the harness store without any manifest
// reference, for the cleanup-fence and malformed-key scenarios.
func putJunk(t *testing.T, h *Harness, key string) {
	t.Helper()
	if err := h.Raw().PutObject(context.Background(), key, strings.NewReader("junk"), storage.Metadata{}); err != nil {
		t.Fatalf("seed junk %s: %v", key, err)
	}
}

// objectGone reports whether a raw object is absent. It is called from
// inside polling closures, so a transient read failure is reported as
// "not yet gone" and retried rather than aborting the scenario.
func objectGone(h *Harness, key string) (bool, error) {
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
func requireObjectGone(t *testing.T, h *Harness, key string) {
	t.Helper()
	h.eventually(t, settleTimeout, func() error {
		gone, err := objectGone(h, key)
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
// of a request path.
func assertRemoteGeneration(t *testing.T, h *Harness, path string, want uint64) {
	t.Helper()
	rec := h.StateRecord(t, path)
	if rec.RemoteGeneration != want {
		t.Fatalf("state remoteGeneration = %d, want %d", rec.RemoteGeneration, want)
	}
}

// assertPulledMarker asserts the durable pull marker of a request path.
func assertPulledMarker(t *testing.T, h *Harness, path string) {
	t.Helper()
	if _, err := os.Stat(h.PrivateDir(path) + "/pulled"); err != nil {
		t.Fatalf("pulled marker for %s: %v", path, err)
	}
}

// assertVisibleFiles asserts the exact visible-file set of one request
// path, keyed by slash-form relative path.
func assertVisibleFiles(t *testing.T, h *Harness, path string, want map[string]string) {
	t.Helper()
	got := h.FSSnapshot(path)
	if len(got) != len(want) {
		t.Fatalf("visible files at %s = %v, want %v", path, got, want)
	}
	for name, data := range want {
		if got[name] != data {
			t.Fatalf("visible file %s = %q, want %q", name, got[name], data)
		}
	}
}
