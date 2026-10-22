package sync

import (
	"bytes"
	"context"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// TestScenarioUpgradeRequiredForNewerManifest proves a notebook written by a
// newer slivingdoc is refused as UPGRADE_REQUIRED, not as corruption, for a
// pull and a commit alike, and that nothing local or remote changes: the
// draft stays, the newer manifest stays byte-identical. The stored current
// is a wrapper version above this build's, which the encrypted decorator
// refuses before it reads a pack (architecture/storage.md, Encrypted
// current).
func TestScenarioUpgradeRequiredForNewerManifest(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{Keyring: emptyRing(t)})
	path := h.WorkspaceRoot()
	CommitFirst(t, h, "a.md", "alpha", "first")
	newer := []byte(`{"version":3,"generation":7,"someFieldThisBuildNeverHeardOf":true}`)
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
		for _, want := range []string{"newer slivingdoc", "notebook format 3", "Upgrade slivingdoc"} {
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

// assertKeyMaterialRefusal proves one pull and one commit are refused with
// the given key-material reason before any pack read or write, leaving the
// draft and the accepted current untouched.
func assertKeyMaterialRefusal(t *testing.T, h *Harness, reason, messageContains string, current []byte) {
	t.Helper()
	path := h.WorkspaceRoot()
	writes := func() int {
		return h.Recorder().Count(OpPut) + h.Recorder().Count(OpCreate) +
			h.Recorder().Count(OpReplace) + h.Recorder().Count(OpDelete)
	}
	packs, stored := h.Recorder().CountKeyPrefix(OpGet, "packs/"), writes()
	h.WriteFile(path+"/draft.md", "unsent")

	for _, call := range []ToolCall{
		{Tool: ToolPull},
		{Tool: ToolCommit, Message: "second"},
	} {
		res := h.CallTool("", call.Tool, call.Message)
		env := DecodeEnvelope(t, call, res)
		if env.Code != "STORAGE_FAILURE" || env.Reason != reason || env.Action != "OPERATOR" || env.Retryable {
			t.Fatalf("%s = %s/%s/%s retryable %v, want STORAGE_FAILURE/%s/OPERATOR not retryable; message: %s",
				call.Tool, env.Code, env.Reason, env.Action, env.Retryable, reason, env.Message)
		}
		if !strings.Contains(env.Message, messageContains) {
			t.Fatalf("%s message %q lacks %q", call.Tool, env.Message, messageContains)
		}
	}
	if got := h.Recorder().CountKeyPrefix(OpGet, "packs/"); got != packs {
		t.Fatalf("the refused calls touched %d pack(s) in all, want the %d read before them", got, packs)
	}
	if got := writes(); got != stored {
		t.Fatalf("the refused calls made %d write(s) in all, want the %d written before them", got, stored)
	}
	if got := h.ReadFile(path + "/draft.md"); got != "unsent" {
		t.Fatalf("draft = %q, want it untouched", got)
	}
	storedCurrent, err := h.ReadObject(storage.CurrentKey)
	if err != nil || !bytes.Equal(storedCurrent, current) {
		t.Fatalf("current changed under the refused calls: %v", err)
	}
}

// encryptedAtRestNotebook publishes one plaintext generation through a
// harness, converts every stored object to ciphertext exactly as encrypt
// init writes it, and returns the harness and its base store. The harness
// keeps its workspace, so its process has a baseline and a pulled marker
// and its commit reaches the stored current.
func encryptedAtRestNotebook(t *testing.T, prefix string, cfg HarnessConfig) (*Harness, []byte) {
	t.Helper()
	base := fake.New(prefix)
	cfg.Store, cfg.Prefix, cfg.Bucket = base, prefix, "test-bucket"
	h := NewHarness(t, cfg)
	CommitFirst(t, h, "a.md", "alpha\n", "first")
	encryptNotebookAtRest(t, base, newEncryptedKeys(t))
	current, err := h.ReadObject(storage.CurrentKey)
	if err != nil {
		t.Fatalf("read the encrypted current: %v", err)
	}
	if !bytes.HasPrefix(current, []byte(`{"version":2,"envelope":`)) {
		t.Fatalf("current is not the version-two wrapper: %s", current)
	}
	return h, current
}

// TestScenarioEncryptedNotebookRequiresKeyMaterial proves a process that
// holds no secret at all refuses an encrypted notebook as KEY_REQUIRED for
// a pull and a commit alike, before it reads a pack or writes an object.
func TestScenarioEncryptedNotebookRequiresKeyMaterial(t *testing.T) {
	t.Parallel()
	h, current := encryptedAtRestNotebook(t, "encrypted-needs-key", HarnessConfig{Keyring: emptyRing(t)})
	assertKeyMaterialRefusal(t, h, "KEY_REQUIRED", "no passphrase was supplied", current)
}

// TestScenarioEncryptedNotebookRejectsWrongSecret proves a process whose
// supplied secret opens no door refuses an encrypted notebook as
// KEY_REJECTED, not as corruption, and changes nothing.
func TestScenarioEncryptedNotebookRejectsWrongSecret(t *testing.T) {
	t.Parallel()
	h, current := encryptedAtRestNotebook(t, "encrypted-wrong-secret", HarnessConfig{Keyring: wrongRing(t)})
	assertKeyMaterialRefusal(t, h, "KEY_REJECTED", "no supplied secret opens this encrypted notebook", current)
}
