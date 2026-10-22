package sync

import (
	"context"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// TestScenarioUpgradeRequiredForNewerManifest proves a notebook written by a
// newer slivingdoc is refused as UPGRADE_REQUIRED, not as corruption, for a
// pull and a commit alike, and that nothing local or remote changes: the
// draft stays, the newer manifest stays byte-identical.
func TestScenarioUpgradeRequiredForNewerManifest(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	path := h.WorkspaceRoot()
	CommitFirst(t, h, "a.md", "alpha", "first")
	newer := []byte(`{"version":2,"generation":7,"someFieldThisBuildNeverHeardOf":true}`)
	etag := CurrentETag(t, h)
	if _, err := h.Raw().ReplaceObject(context.Background(), storage.CurrentKey, etag, newer); err != nil {
		t.Fatalf("install the newer manifest: %v", err)
	}
	h.WriteFile(path+"/draft.md", "unsent")

	for _, call := range []ToolCall{
		{Tool: ToolPull, Path: path},
		{Tool: ToolCommit, Path: path, Message: "second"},
	} {
		res := h.CallTool("", call.Tool, call.Message)
		env := DecodeEnvelope(t, call, res)
		if env.Code != "STORAGE_FAILURE" || env.Reason != "UPGRADE_REQUIRED" || env.Action != "OPERATOR" || env.Retryable {
			t.Fatalf("%s = %s/%s/%s retryable %v, want STORAGE_FAILURE/UPGRADE_REQUIRED/OPERATOR not retryable; message: %s",
				call.Tool, env.Code, env.Reason, env.Action, env.Retryable, env.Message)
		}
		for _, want := range []string{"newer slivingdoc", "notebook format 2", "Upgrade slivingdoc"} {
			if !strings.Contains(env.Message, want) {
				t.Fatalf("%s message %q lacks %q", call.Tool, env.Message, want)
			}
		}
	}
	if got := h.ReadFile(path + "/draft.md"); got != "unsent" {
		t.Fatalf("draft = %q, want it untouched", got)
	}
	data, err := h.ReadObject(storage.CurrentKey)
	if err != nil || string(data) != string(newer) {
		t.Fatalf("current = %q, %v; want the newer manifest untouched", data, err)
	}
}
