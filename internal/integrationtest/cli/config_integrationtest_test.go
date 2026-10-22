package cli

import (
	"os"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"
)

// TestScenarioConfigPrecedence observes configuration precedence through
// the public process: a flag-selected root serves the notebook even when
// the environment selects another root, and an environment-selected root
// serves it when there is no flag (architecture/config.md).
func TestScenarioConfigPrecedence(t *testing.T) {
	t.Parallel()
	t.Run("flag wins over environment", func(t *testing.T) {
		t.Parallel()
		flagRoot, envRoot := t.TempDir(), t.TempDir()
		flagPrivate, envPrivate := t.TempDir(), t.TempDir()
		h := SpawnHelper(t, "fake", []string{
			"SLIVINGDOC_WORKSPACE_ROOT=" + envRoot,
			"SLIVINGDOC_PRIVATE_ROOT=" + envPrivate,
		}, "serve", "--workspace-root="+flagRoot, "--private-root="+flagPrivate)
		cs := h.ConnectClient(t)
		AssertProcessCallOK(t, cs, ToolPull, flagRoot, "")
		if entries, err := os.ReadDir(envRoot); err != nil || len(entries) != 0 {
			t.Fatalf("environment root = %v (err %v), want it untouched", entries, err)
		}
		h.CloseClient(t, cs)
	})

	t.Run("environment wins over default", func(t *testing.T) {
		t.Parallel()
		envRoot, envPrivate := t.TempDir(), t.TempDir()
		h := SpawnHelper(t, "fake", []string{
			"SLIVINGDOC_WORKSPACE_ROOT=" + envRoot,
			"SLIVINGDOC_PRIVATE_ROOT=" + envPrivate,
		}, "serve")
		cs := h.ConnectClient(t)
		AssertProcessCallOK(t, cs, ToolPull, envRoot, "")
		h.CloseClient(t, cs)
	})

	t.Run("empty flag does not fall back to environment", func(t *testing.T) {
		t.Parallel()
		h := SpawnHelper(t, "fake", []string{"SLIVINGDOC_WORKSPACE_ROOT=" + t.TempDir()}, "serve", "--workspace-root=")
		code, stdout, stderr := h.RunStdioProcess(t, nil)
		AssertOneRedactedDiagnostic(t, code, stdout, stderr)
	})
}

// TestScenarioConfigInvalidAndEarlyExit proves invalid configuration is a
// redacted one-line startup failure, while the version command and the
// serve help exit before the incompatible-store probe can refuse the
// process (architecture/config.md).
func TestScenarioConfigInvalidAndEarlyExit(t *testing.T) {
	t.Parallel()
	// The redaction is only observable when the diagnostic echoes the
	// offending value. Every endpoint rule in architecture/config.md
	// reports a constant string (see normalizeEndpoint), so none of them can
	// prove it; the prefix validator quotes the rejected prefix, so a
	// credential-shaped prefix reaches the redactor. Both the AWS access key
	// ID and the URL user information must be gone from the diagnostic.
	t.Run("invalid configuration is redacted", func(t *testing.T) {
		t.Parallel()
		accessKey, password := "AKIA1234567890ABCDEF", "hunter2"
		h := SpawnHelper(t, "fake", []string{
			"SLIVINGDOC_PREFIX=http://" + accessKey + ":" + password + "@example.invalid",
		}, "serve")
		code, stdout, stderr := h.RunStdioProcess(t, nil)
		AssertOneRedactedDiagnostic(t, code, stdout, stderr, accessKey, password)
		if !strings.Contains(stderr, "[redacted]") {
			t.Fatalf("invalid configuration stderr = %q, want the echoed value replaced by the redaction marker", stderr)
		}
	})

	// Per architecture/config.md, a custom endpoint carries no user
	// information. This proves the refusal itself; its diagnostic is a
	// constant string, so it says nothing about the redaction.
	t.Run("endpoint user information is refused", func(t *testing.T) {
		t.Parallel()
		secret := "AKIA1234567890ABCDEF"
		h := SpawnHelper(t, "fake", []string{"AWS_ENDPOINT_URL_S3=http://" + secret + ":password@example.invalid"}, "serve")
		code, stdout, stderr := h.RunStdioProcess(t, nil)
		AssertOneRedactedDiagnostic(t, code, stdout, stderr, secret, "password")
		if !strings.Contains(stderr, "user information") {
			t.Fatalf("endpoint stderr = %q, want the user-information refusal", stderr)
		}
	})

	// Both rows run against the probe-failing store, so reaching exit zero
	// proves no startup dependency was touched. The second row additionally
	// supplies an invalid configuration in the environment (an empty bucket
	// and a checkpoint threshold below the documented minimum), which the
	// serve command refuses; a clean version line therefore proves the
	// version command resolves no configuration at all.
	for _, row := range []struct {
		name string
		env  []string
	}{
		{name: "version skips startup dependencies"},
		{
			name: "version resolves no configuration",
			env:  []string{"SLIVINGDOC_BUCKET=", "SLIVINGDOC_CHECKPOINT_PACKS=0"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := SpawnHelper(t, "bad-store", row.env, "version")
			code, stdout, stderr := h.RunStdioProcess(t, nil)
			if code != 0 || !strings.HasPrefix(stdout, "slivingdoc ") ||
				strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
				t.Fatalf("version = exit %d stdout %q, want exactly one version line", code, stdout)
			}
			if stderr != "" {
				t.Fatalf("version stderr = %q, want empty", stderr)
			}
		})
	}

	// TestScenarioIntegrityStartupProbeFailure is the complementary negative
	// control: the same probe-failing serve process refuses before transport
	// startup. Keeping its stronger redaction assertions there prevents this
	// scenario from booting an identical helper a second time.

	t.Run("serve help skips startup dependencies", func(t *testing.T) {
		t.Parallel()
		h := SpawnHelper(t, "bad-store", nil, "serve", "-h")
		code, stdout, stderr := h.RunStdioProcess(t, nil)
		if code != 0 || !strings.Contains(stdout, "--bucket") || !strings.Contains(stdout, "--retained-checkpoints") ||
			!strings.Contains(stdout, "--read-only-paths") {
			t.Fatalf("serve -h = exit %d stdout %q, want flag reference", code, stdout)
		}
		if stderr != "" {
			t.Fatalf("serve -h stderr = %q, want empty", stderr)
		}
	})

	// The router itself is part of the public surface: an unknown command
	// and a missing command are refusals, not a server that starts anyway.
	for _, row := range []struct {
		name string
		args []string
	}{
		{name: "no command", args: nil},
		{name: "unknown command", args: []string{"frobnicate"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := SpawnHelper(t, "bad-store", nil, row.args...)
			code, stdout, _ := h.RunStdioProcess(t, nil)
			if code == 0 {
				t.Fatalf("%v = exit 0, want a usage refusal", row.args)
			}
			if !strings.Contains(stdout, "serve") || !strings.Contains(stdout, "version") {
				t.Fatalf("%v stdout = %q, want the command listing", row.args, stdout)
			}
		})
	}
}
