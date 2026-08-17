package integrationtest

import (
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// TestScenarioIntegrityCorruptManifest proves that current is strict
// authoritative state: malformed bytes cannot be replaced by object-name
// discovery or a stale local guess (architecture sections 9.2 (L423) and 10 (L603)).
func TestScenarioIntegrityCorruptManifest(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "first")

	listsBefore := h.Recorder().Count(OpList)
	h.Faults().CorruptRead(storage.CurrentKey)
	res := h.Pull("", path)
	h.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: path,
		Expect: CallExpectation{ErrorCode: "STORAGE_INTEGRITY", Retryable: new(false)},
	}, res)
	if got := h.ReadFile(path + "/a.md"); got != "alpha" {
		t.Fatalf("corrupt-manifest pull changed L to %q", got)
	}
	// The old index is retained: the recorded baseline still names the
	// accepted generation, so a later readable manifest resumes from it.
	assertRemoteGeneration(t, h, path, 1)
	// No state is guessed from object names.
	if got := h.Recorder().Count(OpList) - listsBefore; got != 0 {
		t.Fatalf("LIST calls during the corrupt-manifest pull = %d, want none", got)
	}

	// Once the manifest reads cleanly again the pull resumes normally,
	// proving the failure was refusal, not a poisoned local state.
	h.Faults().RestoreRead(storage.CurrentKey)
	h.assertOK(t, h.Pull("", path))
}

// TestScenarioIntegrityPackTransportFailure proves that a pack GET failing
// with a transport error is a retryable STORAGE_FAILURE, not an integrity
// verdict: nothing is known to be wrong with the stored bytes, so the
// baseline stays where it was and the next pull succeeds (architecture
// section 10, L603).
func TestScenarioIntegrityPackTransportFailure(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")
	commitFirst(t, a, pathA, "a.md", "alpha", "first")

	key := a.Manifest().Checkpoint.Key.String()
	b.Faults().FailNext(OpGet, key, storage.ErrTransport)
	res := b.Pull("", pathB)
	b.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathB,
		Expect: CallExpectation{ErrorCode: "STORAGE_FAILURE", Retryable: new(true)},
	}, res)
	if got := b.FSSnapshot(pathB); len(got) != 0 {
		t.Fatalf("a failed pack download changed L: %v", got)
	}
	assertRemoteGeneration(t, b, pathB, 0)

	// The injection was one-shot: the retry the category promises works.
	b.assertOK(t, b.Pull("", pathB))
	assertVisibleFiles(t, b, pathB, map[string]string{"a.md": "alpha"})
}

// TestScenarioIntegrityCorruptPack proves a descriptor checksum failure
// prevents a cold reader from importing corrupt bytes or rewriting L
// (architecture sections 9.3 (L537) and 10 (L603)).
func TestScenarioIntegrityCorruptPack(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")
	commitFirst(t, a, pathA, "a.md", "alpha", "first")

	key := a.Manifest().Checkpoint.Key.String()
	b.Faults().CorruptRead(key)
	res := b.Pull("", pathB)
	b.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathB,
		Expect: CallExpectation{ErrorCode: "STORAGE_INTEGRITY", Retryable: new(false)},
	}, res)
	if got := b.FSSnapshot(pathB); len(got) != 0 {
		t.Fatalf("corrupt pack reached L: %v", got)
	}
	assertRemoteGeneration(t, b, pathB, 0)

	// The refusal is about the bytes, not the reader: uncorrupted reads
	// import the same descriptor cleanly.
	b.Faults().RestoreRead(key)
	b.assertOK(t, b.Pull("", pathB))
	assertVisibleFiles(t, b, pathB, map[string]string{"a.md": "alpha"})
}

// TestScenarioIntegrityMissingPackWithUnchangedManifest proves a reader
// only restarts after it observes current move. When the ETag remains the
// same, the missing referenced pack is storage corruption, not a retry loop
// or an inference from LIST (architecture section 10, L603).
func TestScenarioIntegrityMissingPackWithUnchangedManifest(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")
	commitFirst(t, a, pathA, "a.md", "alpha", "first")

	key := a.Manifest().Checkpoint.Key.String()
	b.Faults().FailNext(OpGet, key, storage.ErrNotFound)
	res := b.Pull("", pathB)
	b.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathB,
		Expect: CallExpectation{ErrorCode: "STORAGE_INTEGRITY", Retryable: new(false)},
	}, res)
	if got := b.Recorder().CountKey(OpGet, storage.CurrentKey); got != 2 {
		t.Fatalf("current reads after missing pack = %d, want initial read plus unchanged-ETag proof", got)
	}
}

// TestScenarioIntegrityStartupProbeFailure proves an incompatible store is
// refused before the stdio transport starts. The diagnostic is a category,
// not a disposable probe key or private protocol detail (architecture
// sections 9.4 (L567) and 17 (L1040)).
func TestScenarioIntegrityStartupProbeFailure(t *testing.T) {
	t.Parallel()
	h := spawnHelper(t, "bad-store", nil, "serve")
	code, stdout, stderr := h.runStdioProcess(t, nil)
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
// error code instead of the blanket incompatible-store verdict.
func TestScenarioIntegrityStartupProbeAuthReason(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	env, _ := cliRoots(t)
	env = append(env,
		"AWS_ACCESS_KEY_ID=slivingdoc-bad",
		"AWS_SECRET_ACCESS_KEY=definitely-not-the-secret",
		"AWS_ENDPOINT_URL_S3="+suite.Endpoint,
		"SLIVINGDOC_BUCKET="+tests3.Bucket,
		"SLIVINGDOC_PREFIX="+suite.FreshPrefix("integrationtest-auth"),
	)
	h := spawnHelper(t, "real", env, "serve")
	code, stdout, stderr := h.runStdioProcess(t, nil)
	if code != 1 {
		t.Fatalf("startup exit code = %d, want 1; stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("startup auth refusal wrote protocol stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "INCOMPATIBLE_STORE") {
		t.Fatalf("stderr = %q, want the INCOMPATIBLE_STORE category", stderr)
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
