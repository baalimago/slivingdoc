package sync

import (
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// TestScenarioIntegrityCorruptManifest proves that current is strict
// authoritative state: malformed bytes cannot be replaced by object-name
// discovery or a stale local guess (architecture/storage.md and pull.md).
func TestScenarioIntegrityCorruptManifest(t *testing.T) {
	t.Parallel()
	h := NewFakeHarness(t, HarnessConfig{})
	path := h.WorkspaceRoot()
	CommitFirst(t, h, "a.md", "alpha", "first")

	listsBefore := h.Recorder().Count(OpList)
	h.Faults().CorruptRead(storage.CurrentKey)
	res := h.Pull("")
	h.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: path,
		Expect: CallExpectation{ErrorCode: "STORAGE_INTEGRITY", Reason: "MANIFEST_INVALID", Action: "OPERATOR", Retryable: new(false)},
	}, res)
	if got := h.ReadFile(path + "/a.md"); got != "alpha" {
		t.Fatalf("corrupt-manifest pull changed L to %q", got)
	}
	// The old index is retained: the recorded baseline still names the
	// accepted generation, so a later readable manifest resumes from it.
	AssertRemoteGeneration(t, h, 1)
	// No state is guessed from object names.
	if got := h.Recorder().Count(OpList) - listsBefore; got != 0 {
		t.Fatalf("LIST calls during the corrupt-manifest pull = %d, want none", got)
	}

	// Once the manifest reads cleanly again the pull resumes normally,
	// proving the failure was refusal, not a poisoned local state.
	h.Faults().RestoreRead(storage.CurrentKey)
	h.AssertOK(t, h.Pull(""))
}

// TestScenarioIntegrityPackTransportFailure proves that a pack GET failing
// with a transport error is a retryable STORAGE_FAILURE, not an integrity
// verdict: nothing is known to be wrong with the stored bytes, so the
// baseline stays where it was and the next pull succeeds
// (architecture/pull.md).
func TestScenarioIntegrityPackTransportFailure(t *testing.T) {
	t.Parallel()
	a := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, a.Raw(), a.Cfg.Prefix, HarnessConfig{})
	pathB := b.WorkspaceRoot()
	CommitFirst(t, a, "a.md", "alpha", "first")

	key := a.Manifest().Checkpoint.Key.String()
	b.Faults().FailNext(OpGet, key, storage.ErrTransport)
	res := b.Pull("")
	b.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathB,
		Expect: CallExpectation{ErrorCode: "STORAGE_FAILURE", Reason: "PACK_DOWNLOAD", Action: "RETRY", Retryable: new(true)},
	}, res)
	if got := b.FSSnapshot(pathB); len(got) != 0 {
		t.Fatalf("a failed pack download changed L: %v", got)
	}
	AssertRemoteGeneration(t, b, 0)

	// The injection was one-shot: the retry the category promises works.
	b.AssertOK(t, b.Pull(""))
	AssertVisibleFiles(t, b, map[string]string{"a.md": "alpha"})
}

// TestScenarioIntegrityCorruptPack proves a descriptor checksum failure
// prevents a cold reader from importing corrupt bytes or rewriting L
// (architecture/storage.md and pull.md).
func TestScenarioIntegrityCorruptPack(t *testing.T) {
	t.Parallel()
	a := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, a.Raw(), a.Cfg.Prefix, HarnessConfig{})
	pathB := b.WorkspaceRoot()
	CommitFirst(t, a, "a.md", "alpha", "first")

	key := a.Manifest().Checkpoint.Key.String()
	b.Faults().CorruptRead(key)
	res := b.Pull("")
	b.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathB,
		Expect: CallExpectation{ErrorCode: "STORAGE_INTEGRITY", Reason: "PACK_INVALID", Action: "OPERATOR", Retryable: new(false)},
	}, res)
	if got := b.FSSnapshot(pathB); len(got) != 0 {
		t.Fatalf("corrupt pack reached L: %v", got)
	}
	AssertRemoteGeneration(t, b, 0)

	// The refusal is about the bytes, not the reader: uncorrupted reads
	// import the same descriptor cleanly.
	b.Faults().RestoreRead(key)
	b.AssertOK(t, b.Pull(""))
	AssertVisibleFiles(t, b, map[string]string{"a.md": "alpha"})
}

// TestScenarioIntegrityMissingPackWithUnchangedManifest proves a reader
// only restarts after it observes current move. When the ETag remains the
// same, the missing referenced pack is storage corruption, not a retry loop
// or an inference from LIST (architecture/pull.md).
func TestScenarioIntegrityMissingPackWithUnchangedManifest(t *testing.T) {
	t.Parallel()
	a := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, a.Raw(), a.Cfg.Prefix, HarnessConfig{})
	pathB := b.WorkspaceRoot()
	CommitFirst(t, a, "a.md", "alpha", "first")

	key := a.Manifest().Checkpoint.Key.String()
	b.Faults().FailNext(OpGet, key, storage.ErrNotFound)
	res := b.Pull("")
	b.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: pathB,
		Expect: CallExpectation{ErrorCode: "STORAGE_INTEGRITY", Reason: "PACK_INVALID", Action: "OPERATOR", Retryable: new(false)},
	}, res)
	if got := b.Recorder().CountKey(OpGet, storage.CurrentKey); got != 2 {
		t.Fatalf("current reads after missing pack = %d, want initial read plus unchanged-ETag proof", got)
	}
}

// TestScenarioIntegrityStartupProbeFailure proves an incompatible store is
// refused before the stdio transport starts. The diagnostic is a category,
// not a disposable probe key or private protocol detail
// (architecture/storage.md and cli.md).
func TestScenarioIntegrityStartupProbeFailure(t *testing.T) {
	t.Parallel()
	h := SpawnHelper(t, "bad-store", nil, "serve")
	code, stdout, stderr := h.RunStdioProcess(t, nil)
	if code != 1 {
		t.Fatalf("startup exit code = %d, want 1; stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("startup probe failure wrote protocol stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "INCOMPATIBLE_STORE") {
		t.Fatalf("stderr = %q, want the INCOMPATIBLE_STORE category", stderr)
	}
	if strings.Contains(stderr, "probe/") {
		t.Fatalf("startup diagnostic leaks probe key: %q", stderr)
	}
}

// TestScenarioIntegrityStartupProbeAuthReason proves the real S3 reason
// behind a probe failure reaches the startup diagnostic, redacted of the
// probe key and the secret: an authentication refusal names its server
// error code and how to fix it instead of the blanket incompatible-store
// verdict.
func TestScenarioIntegrityStartupProbeAuthReason(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	env, _ := CliRoots(t)
	env = append(
		env,
		"AWS_ACCESS_KEY_ID=slivingdoc-bad",
		"AWS_SECRET_ACCESS_KEY=definitely-not-the-secret",
		"AWS_ENDPOINT_URL_S3="+suite.Endpoint,
		"SLIVINGDOC_BUCKET="+tests3.Bucket,
		"SLIVINGDOC_PREFIX="+suite.FreshPrefix("integrationtest-auth"),
	)
	h := SpawnHelper(t, "real", env, "serve")
	code, stdout, stderr := h.RunStdioProcess(t, nil)
	if code != 1 {
		t.Fatalf("startup exit code = %d, want 1; stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("startup auth refusal wrote protocol stdout: %q", stdout)
	}
	if strings.Contains(stderr, "INCOMPATIBLE_STORE") {
		t.Fatalf("stderr = %q, a refused credential is not an incompatible store", stderr)
	}
	if !strings.Contains(stderr, "refused the credentials") || !strings.Contains(stderr, "--bucket") {
		t.Fatalf("stderr = %q, want the credentials refusal with its fix", stderr)
	}
	if !strings.Contains(stderr, "InvalidAccessKeyId") {
		t.Fatalf("stderr = %q, want the S3 InvalidAccessKeyId reason", stderr)
	}
	if strings.Contains(stderr, "definitely-not-the-secret") {
		t.Fatalf("stderr = %q leaks the secret access key", stderr)
	}
	if strings.Contains(stderr, "probe/") {
		t.Fatalf("stderr = %q leaks the probe key", stderr)
	}
}
