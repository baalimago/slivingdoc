package hosted

// The notebook a bare command addresses: the nearest directory at or above
// its working directory that remembers a target, and the space a token
// proved (architecture/config.md, The remembered target of a directory).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"
)

// TestScenarioBarePullFromASubdirectoryTakesTheEnclosingNotebook proves the
// walk from the outside: a pull run inside the notebook but not in its root
// materializes the notebook the directory above remembers, reports that
// directory, and records nothing of its own.
func TestScenarioBarePullFromASubdirectoryTakesTheEnclosingNotebook(t *testing.T) {
	t.Parallel()
	_, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	deep := filepath.Join(notes, "projects", "deep")
	RunCLIOK(t, "real", env, nil, "pull", "--space", SecondSpace, notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "the published note\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "the published note")
	if err := os.Remove(filepath.Join(notes, "a.md")); err != nil {
		t.Fatalf("remove the local note: %v", err)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("create the subdirectory: %v", err)
	}
	before := ReadSettings(t, env)

	// The bare pull stands in a directory of its own and names no root, so
	// only the walk can find the notebook.
	bare := append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT=", "SLIVINGDOC_PRIVATE_ROOT=")
	code, stdout, stderr := SpawnHelperIn(t, deep, "real", bare, "pull").RunStdioProcess(t, nil)
	if code != 0 {
		t.Fatalf("the bare pull from the subdirectory = exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, notes+"\n") {
		t.Fatalf("the bare pull reported %q, want the enclosing notebook %q", stdout, notes)
	}
	AssertReachedSpace(t, site, SecondSpace)
	if _, err := os.Stat(filepath.Join(notes, "a.md")); err != nil {
		t.Fatalf("the notebook root after the pull = %v, want the published note materialized there", err)
	}
	if _, err := os.Stat(filepath.Join(deep, "a.md")); err == nil {
		t.Fatal("the pull materialized the note in the working directory, want the enclosing notebook")
	}
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after the enclosed pull = %s, want it unchanged (%s)", after, before)
	}
}

// TestScenarioDefaultSpaceRootIsAdoptedFromASubdirectory proves the walk
// finds a root whose first pull used the login's default space: the record
// that pull wrote is what the bare command below reads, so the notebook
// materializes at the root instead of nesting a second one.
func TestScenarioDefaultSpaceRootIsAdoptedFromASubdirectory(t *testing.T) {
	t.Parallel()
	_, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	deep := filepath.Join(notes, "projects", "deep")
	RunCLIOK(t, "real", env, nil, "pull", notes)
	AssertReachedSpace(t, site, HostedSpace)
	AssertRecords(t, env, Recorded(notes, HostedSpace))
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "the default's note\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "the default's note")
	if err := os.Remove(filepath.Join(notes, "a.md")); err != nil {
		t.Fatalf("remove the local note: %v", err)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("create the subdirectory: %v", err)
	}
	before := ReadSettings(t, env)

	bare := append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT=", "SLIVINGDOC_PRIVATE_ROOT=")
	code, stdout, stderr := SpawnHelperIn(t, deep, "real", bare, "pull").RunStdioProcess(t, nil)
	if code != 0 {
		t.Fatalf("the bare pull from the subdirectory = exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, notes+"\n") {
		t.Fatalf("the bare pull reported %q, want the enclosing notebook %q", stdout, notes)
	}
	if _, err := os.Stat(filepath.Join(notes, "a.md")); err != nil {
		t.Fatalf("the notebook root after the pull = %v, want the published note materialized there", err)
	}
	if _, err := os.Stat(filepath.Join(deep, "a.md")); err == nil {
		t.Fatal("the pull materialized the note in the working directory, want the enclosing notebook")
	}
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after the enclosed pull = %s, want it unchanged (%s)", after, before)
	}
}

// TestScenarioNamedPathInsideANotebookStaysThere proves the other half of the
// rule: a positional path is used exactly as given, so a pull of a directory
// inside a recorded notebook materializes and records that directory instead
// of the notebook above it (architecture/config.md, The remembered target of
// a directory).
func TestScenarioNamedPathInsideANotebookStaysThere(t *testing.T) {
	t.Parallel()
	_, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	nested := filepath.Join(notes, "projects")
	RunCLIOK(t, "real", env, nil, "pull", "--space", SecondSpace, notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "the published note\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "the published note")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create the nested directory: %v", err)
	}
	code, stdout, stderr := RunCLI(t, "real", env, "pull", nested)
	if code != 0 {
		t.Fatalf("the pull of the nested directory = exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, nested+"\n") {
		t.Fatalf("the pull reported %q, want the named directory %q", stdout, nested)
	}
	AssertReachedSpace(t, site, HostedSpace)
	AssertRecords(t, env, Recorded(notes, SecondSpace), Recorded(nested, HostedSpace))
	if _, err := os.Stat(filepath.Join(notes, "a.md")); err != nil {
		t.Fatalf("the notebook root after the nested pull = %v, want the note untouched", err)
	}
	if _, err := os.Stat(filepath.Join(nested, "a.md")); err == nil {
		t.Fatal("the named pull materialized the notebook above it, want its own directory")
	}
}

// TestScenarioHomeDirectoryIsNoImplicitNotebook proves the boundary of the
// walk: a home directory that remembers a target is never adopted by a bare
// command standing below it, so the login's default space applies and the
// operator's own home is never materialized over.
func TestScenarioHomeDirectoryIsNoImplicitNotebook(t *testing.T) {
	t.Parallel()
	_, site, env, root := RememberedEnv(t)
	home := filepath.Join(root, "home")
	work := filepath.Join(home, "notes")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("create the working directory: %v", err)
	}
	Remember(t, env, home, SecondSpace, HostedPrefix)
	bare := append(append([]string(nil), env...), "HOME="+home, "SLIVINGDOC_WORKSPACE_ROOT=", "SLIVINGDOC_PRIVATE_ROOT=")
	code, stdout, stderr := SpawnHelperIn(t, work, "real", bare, "pull").RunStdioProcess(t, nil)
	if code != 0 {
		t.Fatalf("the bare pull below home = exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, work+"\n") {
		t.Fatalf("the bare pull reported %q, want the working directory %q", stdout, work)
	}
	AssertReachedSpace(t, site, HostedSpace)
	AssertRecords(t, env, Recorded(home, SecondSpace), Recorded(work, HostedSpace))
}

// TestScenarioTokenPullRecordsTheSpace proves the space a token proved is
// recorded like a flagged one: the next run, which carries no token, reads
// the record and refuses by name instead of resolving another notebook.
func TestScenarioTokenPullRecordsTheSpace(t *testing.T) {
	t.Parallel()
	g, env, root := HostedEnv(t, 1<<20)
	env = append(WithoutBucket(env), credentials.DirEnv+"="+filepath.Join(t.TempDir(), "cfg"))
	notes := filepath.Join(root, "notes")

	// The bare pull is the only source of the space, so the record it writes
	// is the one the next run reads.
	RunCLIOK(t, "real", env, nil, "pull", notes)
	if g.Stored(HostedSpace) != 0 {
		t.Fatalf("stored bytes = %d, want the token's own space addressed", g.Stored(HostedSpace))
	}
	AssertRecords(t, env, Recorded(notes, HostedSpace))

	tokenless := append(append([]string(nil), env...), "SLIVINGDOC_TOKEN=")
	code, stdout, stderr := RunCLI(t, "real", tokenless, "pull", notes)
	AssertRefusal(t, []string{"pull", notes}, code, stdout, stderr,
		`remembers hosted space "`+HostedSpace+`"`, "run 'slivingdoc login'")
}
