package integrationtest

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/tests3"
)

// cliRoots builds the shared-root environment of a CLI scenario. Every
// spawn with the same entries operates on the same workspace and private
// roots, which is how one human alternates one-shot pull and commit
// processes over one directory.
func cliRoots(t *testing.T) (env []string, workspaceRoot string) {
	t.Helper()
	workspaceRoot = t.TempDir()
	env = []string{
		"SLIVINGDOC_WORKSPACE_ROOT=" + workspaceRoot,
		"SLIVINGDOC_PRIVATE_ROOT=" + t.TempDir(),
	}
	return env, workspaceRoot
}

// writeCLIFile writes one UTF-8 text file, creating parent directories.
func writeCLIFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) = %v", path, err)
	}
}

// runCLI runs one one-shot CLI process to completion.
func runCLI(t *testing.T, mode string, env []string, args ...string) (int, string, string) {
	t.Helper()
	return spawnHelper(t, mode, env, args...).runStdioProcess(t, nil)
}

// runCLIOK runs one one-shot CLI process and asserts the success
// contract: exit zero and the plain OK-prefixed result report on stdout —
// the status token and generation summary, one indented line per changed
// file with its insertion and deletion counts, and the totals trailer as
// the final line — with no ANSI escapes, because the spawned process
// writes to a pipe.
func runCLIOK(t *testing.T, mode string, env []string, args ...string) {
	t.Helper()
	code, stdout, stderr := runCLI(t, mode, env, args...)
	if code != 0 {
		t.Fatalf("%v = exit %d, want 0; stdout: %q stderr: %s", args, code, stdout, stderr)
	}
	if !strings.HasPrefix(stdout, "OK  generation ") {
		t.Fatalf("%v stdout = %q, want the OK-prefixed result report", args, stdout)
	}
	trailer := regexp.MustCompile(`\n\d+ files changed, \d+ insertions\(\+\), \d+ deletions\(-\)\n$`)
	if !trailer.MatchString(stdout) {
		t.Fatalf("%v stdout = %q, want the totals trailer as the final line", args, stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("%v stdout = %q, want no ANSI escapes on a pipe", args, stdout)
	}
}

// runCLIExact runs one one-shot CLI process and asserts the exact success
// report on stdout: exit zero and stdout equal to want, the documented
// plain form of the unified report on a pipe.
func runCLIExact(t *testing.T, mode string, env []string, want string, args ...string) {
	t.Helper()
	code, stdout, stderr := runCLI(t, mode, env, args...)
	if code != 0 {
		t.Fatalf("%v = exit %d, want 0; stdout: %q stderr: %s", args, code, stdout, stderr)
	}
	if stdout != want {
		t.Fatalf("%v stdout = %q, want exactly %q", args, stdout, want)
	}
}

// TestScenarioCLIPullCommitRoundTrip proves the direct human workflow over
// separate one-shot processes: pull materializes the notebook and records
// the pulled state in P, so a later commit process publishes the edit and
// both print the exact plain OK-prefixed report — the first pull has no
// on-disk delta, the first commit reports the new file — with exit zero.
func TestScenarioCLIPullCommitRoundTrip(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	notes := filepath.Join(root, "notes")

	runCLIExact(t, "fake", env,
		"OK  generation 0  "+notes+"\n0 files changed, 0 insertions(+), 0 deletions(-)\n",
		"pull", notes)

	writeCLIFile(t, filepath.Join(notes, "a.md"), "cli notes\n")
	runCLIExact(t, "fake", env,
		"OK  generation 1  "+notes+"\n  a.md  +1\n1 files changed, 1 insertions(+), 0 deletions(-)\n",
		"commit", notes, "-m", "cli commit")
}

// TestScenarioCLIPullKeepsDirectoryIdentity proves that repeated pulls
// never replace the notebook directory itself. A human keeps a shell open
// in that directory between pulls; a shell's working directory is a
// reference to the directory inode, not to its name, so replacing the
// directory silently orphans the shell — getcwd(2) starts failing and
// every relative path resolves to nothing, while the prompt still shows
// the old path.
//
// The workspace and private roots here share a filesystem, which is the
// case that used to take the directory-swap path.
func TestScenarioCLIPullKeepsDirectoryIdentity(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	env, root := cliRoots(t)
	env = append(env,
		"AWS_ACCESS_KEY_ID="+tests3.User,
		"AWS_SECRET_ACCESS_KEY="+tests3.Pass,
		"AWS_ENDPOINT_URL_S3="+suite.Endpoint,
		"SLIVINGDOC_PATH_STYLE=true",
		"SLIVINGDOC_BUCKET="+tests3.Bucket,
		"SLIVINGDOC_PREFIX="+suite.FreshPrefix("integrationtest-identity"),
	)
	writer, reader := filepath.Join(root, "writer"), filepath.Join(root, "reader")

	// Publish from one workspace so the other has real content to
	// materialize; an empty tree would not exercise the replacement.
	runCLIOK(t, "real", env, "pull", writer)
	writeCLIFile(t, filepath.Join(writer, "note.md"), "first\n")
	runCLIOK(t, "real", env, "commit", writer, "-m", "first")

	runCLIOK(t, "real", env, "pull", reader)
	before, err := os.Stat(reader)
	if err != nil {
		t.Fatalf("stat after the first pull: %v", err)
	}
	// The open descriptor stands in for the human's working directory.
	held, err := os.Open(reader)
	if err != nil {
		t.Fatalf("open the notebook directory: %v", err)
	}
	defer held.Close()

	// A second publication gives the reader's next pull a real change to
	// apply on top of an existing tree.
	writeCLIFile(t, filepath.Join(writer, "note.md"), "second\n")
	writeCLIFile(t, filepath.Join(writer, "added.md"), "added\n")
	runCLIOK(t, "real", env, "commit", writer, "-m", "second")
	runCLIOK(t, "real", env, "pull", reader)

	if got, err := os.ReadFile(filepath.Join(reader, "note.md")); err != nil || string(got) != "second\n" {
		t.Fatalf("reader/note.md = %q, %v; want the republished bytes", got, err)
	}
	after, err := os.Stat(reader)
	if err != nil {
		t.Fatalf("stat after the second pull: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("pull replaced the notebook directory; a shell or editor sitting in it is now orphaned")
	}
	// Reading through the descriptor opened before the pull must observe
	// the materialized tree. A replaced directory leaves this descriptor on
	// the removed inode, which reads as empty.
	names, err := held.Readdirnames(-1)
	if err != nil {
		t.Fatalf("read the held directory: %v", err)
	}
	if !slices.Contains(names, "added.md") {
		t.Fatalf("the held directory lists %v, want the materialized added.md; it was unlinked and replaced", names)
	}
}

// TestScenarioCLIRelativePathResolvesAgainstCwd proves a relative notebook
// path resolves against the process working directory, which is the
// primary human invocation from inside the shared directory.
func TestScenarioCLIRelativePathResolvesAgainstCwd(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	h := spawnHelperIn(t, root, "fake", env, "pull", "notes")
	code, stdout, stderr := h.runStdioProcess(t, nil)
	if code != 0 || !strings.HasPrefix(stdout, "OK  generation ") {
		t.Fatalf("pull notes = exit %d stdout %q, want the OK-prefixed report; stderr: %s", code, stdout, stderr)
	}
	if fi, err := os.Stat(filepath.Join(root, "notes")); err != nil || !fi.IsDir() {
		t.Fatalf("pull did not materialize the notebook directory: %v", err)
	}
}

// TestScenarioCLIMarkerConflictReport proves the candid domain-error
// contract: a complete conflict-marker block makes commit exit nonzero and
// print the structured report — the category, the retryable verdict, and
// the conflicted file with its one-based inclusive line ranges — on stdout.
func TestScenarioCLIMarkerConflictReport(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	notes := filepath.Join(root, "notes")
	runCLIOK(t, "fake", env, "pull", notes)

	writeCLIFile(t, filepath.Join(notes, "a.md"), "<<<<<<< local\na\n=======\nb\n>>>>>>> remote\n")
	code, stdout, stderr := runCLI(t, "fake", env, "commit", notes, "-m", "markers")
	if code != 1 {
		t.Fatalf("commit with markers = exit %d, want 1; stderr: %s", code, stderr)
	}
	for _, want := range []string{"CONTENT_CONFLICT", "retryable: false", "a.md: lines 1-5"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("report %q does not contain %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("conflict report %q carries ANSI escapes on a pipe", stdout)
	}
	if strings.Contains(stdout, root) {
		t.Fatalf("report %q echoes the absolute workspace root; paths are relative", stdout)
	}
}

// TestScenarioCLICommitBeforePull proves a commit without a managed pull is
// a nonzero INVALID_REQUEST report, mirroring the notes_commit contract.
func TestScenarioCLICommitBeforePull(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	notes := filepath.Join(root, "notes")
	writeCLIFile(t, filepath.Join(notes, "a.md"), "unpulled\n")
	code, stdout, stderr := runCLI(t, "fake", env, "commit", notes, "-m", "no pull")
	if code != 1 {
		t.Fatalf("commit before pull = exit %d, want 1; stderr: %s", code, stderr)
	}
	for _, want := range []string{"INVALID_REQUEST", "retryable: false"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("report %q does not contain %q", stdout, want)
		}
	}
}

// TestScenarioCLIUsageRefusals proves the argument surface of the two
// commands. Every row runs against the probe-failing store: a refusal that
// exits before INCOMPATIBLE_STORE can appear proves no startup dependency
// was touched, and the help rows exit zero the same way serve -h does.
func TestScenarioCLIUsageRefusals(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string // required stdout substring, empty skips
		wantErr  string // required stderr substring, empty skips
	}{
		{name: "pull with two paths", args: []string{"pull", "a", "b"}, wantExit: 1, wantErr: "at most one notebook path"},
		{name: "commit with two paths", args: []string{"commit", "-m", "m", "a", "b"}, wantExit: 1, wantErr: "at most one notebook path"},
		{name: "commit without a message", args: []string{"commit", "notes"}, wantExit: 1, wantErr: "-m"},
		{name: "pull help skips startup dependencies", args: []string{"pull", "-h"}, wantExit: 0, wantOut: "slivingdoc pull"},
		{name: "commit help skips startup dependencies", args: []string{"commit", "-h"}, wantExit: 0, wantOut: "slivingdoc commit"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := runCLI(t, "bad-store", nil, row.args...)
			if code != row.wantExit {
				t.Fatalf("%v = exit %d, want %d; stdout: %q stderr: %s", row.args, code, row.wantExit, stdout, stderr)
			}
			if row.wantOut != "" && !strings.Contains(stdout, row.wantOut) {
				t.Fatalf("%v stdout = %q, want it to contain %q", row.args, stdout, row.wantOut)
			}
			if row.wantErr != "" && !strings.Contains(stderr, row.wantErr) {
				t.Fatalf("%v stderr = %q, want it to contain %q", row.args, stderr, row.wantErr)
			}
			if strings.Contains(stderr, "INCOMPATIBLE_STORE") {
				t.Fatalf("%v touched the store before the argument refusal: %s", row.args, stderr)
			}
		})
	}
}

// TestScenarioCLISharedRemoteConflict proves the full multi-writer story
// over the real S3 protocol: two workspaces alternate one-shot CLI
// processes against one shared S3 prefix; the divergent edit is a
// nonzero CONTENT_CONFLICT report with the exact relative path and line
// range, the visible file carries the markers, and the resolution
// publishes and reaches the other workspace on its next pull.
func TestScenarioCLISharedRemoteConflict(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	env, root := cliRoots(t)
	env = append(env,
		"AWS_ACCESS_KEY_ID="+tests3.User,
		"AWS_SECRET_ACCESS_KEY="+tests3.Pass,
		"AWS_ENDPOINT_URL_S3="+suite.Endpoint,
		"SLIVINGDOC_PATH_STYLE=true",
		"SLIVINGDOC_BUCKET="+tests3.Bucket,
		"SLIVINGDOC_PREFIX="+suite.FreshPrefix("integrationtest-cli"),
	)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	shared := "shared.md"

	runCLIOK(t, "real", env, "pull", a)
	writeCLIFile(t, filepath.Join(a, shared), "base\n")
	runCLIOK(t, "real", env, "commit", a, "-m", "first")

	runCLIOK(t, "real", env, "pull", b)
	if got, err := os.ReadFile(filepath.Join(b, shared)); err != nil || string(got) != "base\n" {
		t.Fatalf("b/%s after pull = %q, %v; want the published base", shared, got, err)
	}
	writeCLIFile(t, filepath.Join(b, shared), "B-v2\n")
	runCLIOK(t, "real", env, "commit", b, "-m", "second")

	// The divergent edit conflicts against the moved remote.
	writeCLIFile(t, filepath.Join(a, shared), "A-v2\n")
	code, stdout, stderr := runCLI(t, "real", env, "commit", a, "-m", "third")
	if code != 1 {
		t.Fatalf("conflicting commit = exit %d, want 1; stdout: %q stderr: %s", code, stdout, stderr)
	}
	for _, want := range []string{"CONTENT_CONFLICT", "retryable: false", shared + ": lines 1-5"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("conflict report %q does not contain %q", stdout, want)
		}
	}
	if strings.Contains(stdout, root) {
		t.Fatalf("conflict report %q echoes the absolute workspace root; paths are relative", stdout)
	}
	if got, err := os.ReadFile(filepath.Join(a, shared)); err != nil || !strings.Contains(string(got), "<<<<<<<") {
		t.Fatalf("a/%s = %q, %v; want the materialized conflict markers", shared, got, err)
	}

	// Resolving the markers publishes, and the other workspace observes
	// exactly the resolved bytes.
	writeCLIFile(t, filepath.Join(a, shared), "resolved\n")
	runCLIOK(t, "real", env, "commit", a, "-m", "resolved")
	runCLIOK(t, "real", env, "pull", b)
	if got, err := os.ReadFile(filepath.Join(b, shared)); err != nil || string(got) != "resolved\n" {
		t.Fatalf("b/%s after the resolution = %q, %v; want exactly the resolved bytes", shared, got, err)
	}
}
