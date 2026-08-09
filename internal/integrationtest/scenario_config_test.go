package integrationtest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioConfigPrecedence observes configuration precedence through
// the public process: a request below a flag-selected root succeeds even
// when the environment selects another root, and an environment-selected
// root succeeds when there is no flag (architecture section 17, L1040).
func TestScenarioConfigPrecedence(t *testing.T) {
	t.Parallel()
	t.Run("flag wins over environment", func(t *testing.T) {
		t.Parallel()
		flagRoot, envRoot := t.TempDir(), t.TempDir()
		flagPrivate, envPrivate := t.TempDir(), t.TempDir()
		h := spawnHelper(t, "fake", []string{
			"SLIVINGDOC_WORKSPACE_ROOT=" + envRoot,
			"SLIVINGDOC_PRIVATE_ROOT=" + envPrivate,
		}, "serve", "--workspace-root="+flagRoot, "--private-root="+flagPrivate)
		cs := h.connectClient(t)
		assertProcessCallOK(t, cs, toolPull, filepath.Join(flagRoot, "notes"), "")
		if err := cs.Close(); err != nil {
			t.Fatalf("close MCP client: %v", err)
		}
		if code := h.waitExit(t); code != 0 {
			t.Fatalf("process exit = %d, want 0; stderr: %s", code, h.stderrText(t))
		}
	})

	t.Run("environment wins over default", func(t *testing.T) {
		t.Parallel()
		envRoot, envPrivate := t.TempDir(), t.TempDir()
		h := spawnHelper(t, "fake", []string{
			"SLIVINGDOC_WORKSPACE_ROOT=" + envRoot,
			"SLIVINGDOC_PRIVATE_ROOT=" + envPrivate,
		}, "serve")
		cs := h.connectClient(t)
		assertProcessCallOK(t, cs, toolPull, filepath.Join(envRoot, "notes"), "")
		if err := cs.Close(); err != nil {
			t.Fatalf("close MCP client: %v", err)
		}
		if code := h.waitExit(t); code != 0 {
			t.Fatalf("process exit = %d, want 0; stderr: %s", code, h.stderrText(t))
		}
	})

	// Negative control for the subtest above: without the environment
	// variable the workspace root is the documented default (the startup
	// working directory), which does not contain a temporary-directory path.
	// Accepting the path is therefore evidence that the environment value
	// was used, not that every root accepts every absolute path.
	t.Run("default root rejects the environment root path", func(t *testing.T) {
		t.Parallel()
		envRoot, envPrivate := t.TempDir(), t.TempDir()
		h := spawnHelper(t, "fake", []string{
			"SLIVINGDOC_WORKSPACE_ROOT=", // unset: an empty value resolves to the default
			"SLIVINGDOC_PRIVATE_ROOT=" + envPrivate,
		}, "serve")
		cs := h.connectClient(t)
		path := filepath.Join(envRoot, "notes")
		env := processError(t, cs, toolPull, path, "")
		if env.Code != "INVALID_REQUEST" || env.Retryable {
			t.Fatalf("notes_pull(%s) envelope = %+v, want non-retryable INVALID_REQUEST", path, env)
		}
		if strings.Contains(env.Message, envRoot) {
			t.Fatalf("envelope message = %q, must not echo the request root", env.Message)
		}
		if err := cs.Close(); err != nil {
			t.Fatalf("close MCP client: %v", err)
		}
		if code := h.waitExit(t); code != 0 {
			t.Fatalf("process exit = %d, want 0; stderr: %s", code, h.stderrText(t))
		}
	})

	t.Run("empty flag does not fall back to environment", func(t *testing.T) {
		t.Parallel()
		h := spawnHelper(t, "fake", []string{"SLIVINGDOC_WORKSPACE_ROOT=" + t.TempDir()}, "serve", "--workspace-root=")
		code, stdout, stderr := h.runStdioProcess(t, nil)
		assertOneRedactedDiagnostic(t, code, stdout, stderr)
	})
}

// TestScenarioConfigInvalidAndEarlyExit proves invalid configuration is a
// redacted one-line startup failure, while the version command and the
// serve help exit before the incompatible-store probe could refuse the
// process (architecture section 17, L1040).
func TestScenarioConfigInvalidAndEarlyExit(t *testing.T) {
	t.Parallel()
	// The redaction is only observable when the diagnostic echoes the
	// offending value. Every endpoint rule of architecture section 17, L1040,
	// reports a constant string (see normalizeEndpoint), so none of them can
	// prove it; the prefix validator quotes the rejected prefix, so a
	// credential-shaped prefix reaches the redactor. Both the AWS access key
	// ID and the URL user information must be gone from the diagnostic.
	t.Run("invalid configuration is redacted", func(t *testing.T) {
		t.Parallel()
		accessKey, password := "AKIA1234567890ABCDEF", "hunter2"
		h := spawnHelper(t, "fake", []string{
			"SLIVINGDOC_PREFIX=http://" + accessKey + ":" + password + "@example.invalid",
		}, "serve")
		code, stdout, stderr := h.runStdioProcess(t, nil)
		assertOneRedactedDiagnostic(t, code, stdout, stderr, accessKey, password)
		if !strings.Contains(stderr, "[redacted]") {
			t.Fatalf("invalid configuration stderr = %q, want the echoed value replaced by the redaction marker", stderr)
		}
	})

	// Architecture section 17, L1040: a custom endpoint carries no user
	// information. This proves the refusal itself; its diagnostic is a
	// constant string, so it says nothing about the redaction.
	t.Run("endpoint user information is refused", func(t *testing.T) {
		t.Parallel()
		secret := "AKIA1234567890ABCDEF"
		h := spawnHelper(t, "fake", []string{"AWS_ENDPOINT_URL_S3=http://" + secret + ":password@example.invalid"}, "serve")
		code, stdout, stderr := h.runStdioProcess(t, nil)
		assertOneRedactedDiagnostic(t, code, stdout, stderr, secret, "password")
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
			h := spawnHelper(t, "bad-store", row.env, "version")
			code, stdout, stderr := h.runStdioProcess(t, nil)
			if code != 0 || !strings.HasPrefix(stdout, "slivingdoc ") ||
				strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
				t.Fatalf("version = exit %d stdout %q, want exactly one version line", code, stdout)
			}
			if stderr != "" {
				t.Fatalf("version stderr = %q, want empty", stderr)
			}
		})
	}

	// Negative control for the rows above: the same probe-failing store and
	// the same environment refuse the serve command, so exit zero is
	// evidence about the version command rather than about a store that
	// happens to accept everything.
	t.Run("serve refuses the store the version command ignores", func(t *testing.T) {
		t.Parallel()
		h := spawnHelper(t, "bad-store", nil, "serve")
		code, stdout, stderr := h.runStdioProcess(t, nil)
		if code == 0 {
			t.Fatalf("serve against the probe-failing store = exit 0, want a startup refusal; stdout %q", stdout)
		}
		if !strings.Contains(stderr, "INCOMPATIBLE_STORE") {
			t.Fatalf("serve stderr = %q, want the incompatible-store refusal", stderr)
		}
	})

	t.Run("serve help skips startup dependencies", func(t *testing.T) {
		t.Parallel()
		h := spawnHelper(t, "bad-store", nil, "serve", "-h")
		code, stdout, stderr := h.runStdioProcess(t, nil)
		if code != 0 || !strings.Contains(stdout, "--bucket") || !strings.Contains(stdout, "--retained-checkpoints") {
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
			h := spawnHelper(t, "bad-store", nil, row.args...)
			code, stdout, _ := h.runStdioProcess(t, nil)
			if code == 0 {
				t.Fatalf("%v = exit 0, want a usage refusal", row.args)
			}
			if !strings.Contains(stdout, "serve") || !strings.Contains(stdout, "version") {
				t.Fatalf("%v stdout = %q, want the command listing", row.args, stdout)
			}
		})
	}
}

// assertOneRedactedDiagnostic proves a startup refusal is the documented
// shape (architecture section 17, L1040): exit nonzero, an empty stdout, and
// exactly one redacted diagnostic line on stderr. forbidden names values
// that must never reach the diagnostic.
func assertOneRedactedDiagnostic(t *testing.T, code int, stdout, stderr string, forbidden ...string) {
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
func processError(t *testing.T, cs *sdk.ClientSession, tool, path, message string) envelope {
	t.Helper()
	args := map[string]any{"path": path}
	if tool == toolCommit {
		args["message"] = message
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s(%s) = %v", tool, path, err)
	}
	return decodeEnvelope(t, ToolCall{Tool: tool, Path: path, Message: message}, res)
}
