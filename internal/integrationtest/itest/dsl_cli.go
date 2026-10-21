package itest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/tests3"
)

// cliRoots builds the shared-root environment of a CLI scenario. Every
// spawn with the same entries operates on the same workspace and private
// roots, which is how one human alternates one-shot pull and commit
// processes over one directory.
func CliRoots(t *testing.T) (env []string, workspaceRoot string) {
	t.Helper()
	workspaceRoot = t.TempDir()
	env = []string{
		"SLIVINGDOC_WORKSPACE_ROOT=" + workspaceRoot,
		"SLIVINGDOC_PRIVATE_ROOT=" + t.TempDir(),
	}
	return env, workspaceRoot
}

// realCLIEnv returns the environment entries and workspace root that point
// one-shot CLI processes at the shared real S3 suite on one fresh prefix.
// The prefix is also returned so an in-process writer harness can share the
// same remote state, which lets a scenario publish through the cheap MCP
// path while still proving the CLI one-shot process boundary where the
// property under test lives.
func RealCLIEnv(t *testing.T, namespace string) (env []string, root, prefix string) {
	t.Helper()
	suite := tests3.Ensure(t)
	prefix = suite.FreshPrefix(namespace)
	env, root = CliRoots(t)
	env = append(
		env,
		"AWS_ACCESS_KEY_ID="+tests3.User,
		"AWS_SECRET_ACCESS_KEY="+tests3.Pass,
		"AWS_ENDPOINT_URL_S3="+suite.Endpoint,
		"SLIVINGDOC_PATH_STYLE=true",
		"SLIVINGDOC_BUCKET="+tests3.Bucket,
		"SLIVINGDOC_PREFIX="+prefix,
	)
	return env, root, prefix
}

// writeCLIFile writes one UTF-8 text file, creating parent directories.
func WriteCLIFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) = %v", path, err)
	}
}

// runCLI runs one one-shot CLI process to completion.
func RunCLI(t *testing.T, mode string, env []string, args ...string) (int, string, string) {
	t.Helper()
	return SpawnHelper(t, mode, env, args...).RunStdioProcess(t, nil)
}

// runCLIOK runs one one-shot CLI process and asserts the success
// contract: exit zero and the plain OK-prefixed result report on stdout —
// the status token and generation summary, then one indented line per
// changed file with its insertion and deletion counts and the totals
// trailer, or a materialization's totals and outline rows, followed by
// exactly the trailers lines, in that order and no other —
// with no ANSI escapes, because the spawned process writes to a pipe. A
// process configured with neither path set passes nil, which forbids every
// path-set trailer rather than tolerating one (review 1, R1-07).
func RunCLIOK(t *testing.T, mode string, env []string, trailers []string, args ...string) {
	t.Helper()
	code, stdout, stderr := RunCLI(t, mode, env, args...)
	if code != 0 {
		t.Fatalf("%v = exit %d, want 0; stdout: %q stderr: %s", args, code, stdout, stderr)
	}
	if !strings.HasPrefix(stdout, "OK  generation ") {
		t.Fatalf("%v stdout = %q, want the OK-prefixed result report", args, stdout)
	}
	var want strings.Builder
	want.WriteString(`(\n\d+ files changed, \d+ insertions\(\+\), \d+ deletions\(-\)\n|\n  \d+ files? · \d+ lines?\n(  .*\n)+)`)
	for _, line := range trailers {
		want.WriteString(regexp.QuoteMeta(line + "\n"))
	}
	if !regexp.MustCompile(want.String() + `$`).MatchString(stdout) {
		t.Fatalf("%v stdout = %q, want the totals line followed by exactly the trailers %q", args, stdout, trailers)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("%v stdout = %q, want no ANSI escapes on a pipe", args, stdout)
	}
}

// runCLIExact runs one one-shot CLI process and asserts the exact success
// report on stdout: exit zero and stdout equal to want, the documented
// plain form of the unified report on a pipe.
func RunCLIExact(t *testing.T, mode string, env []string, want string, args ...string) {
	t.Helper()
	code, stdout, stderr := RunCLI(t, mode, env, args...)
	if code != 0 {
		t.Fatalf("%v = exit %d, want 0; stdout: %q stderr: %s", args, code, stdout, stderr)
	}
	if stdout != want {
		t.Fatalf("%v stdout = %q, want exactly %q", args, stdout, want)
	}
}
