package hosted

import (
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/settings"
)

// The recorder: what a successful pull or commit of a path-taking command
// stores in the scoped settings file of the configuration directory
// (architecture/config.md, The remembered target of a directory).

// TestScenarioPullRecordsTheHostedNotebook proves the whole feature from the
// outside: one pull with --space records the notebook it reached, and every
// later bare command in that directory reaches the same space with no space
// and no storage flag, while a sibling directory keeps the login's default
// space and records nothing.
func TestScenarioPullRecordsTheHostedNotebook(t *testing.T) {
	t.Parallel()
	g, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")

	RunCLIOK(t, "real", env, nil, "pull", "--space", SecondSpace, notes)
	AssertReachedSpace(t, site, SecondSpace)
	AssertRecords(t, env, Recorded(notes, SecondSpace))
	AssertRecordedFileNamesNoCredential(t, env)

	// Every later command in that directory reaches the same notebook with
	// no flag, and none of them changes what the first pull recorded.
	before := ReadSettings(t, env)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "in the remembered space\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "remembered")
	AssertReachedSpace(t, site, SecondSpace)
	if g.Stored(SecondSpace) == 0 || g.Stored(HostedSpace) != 0 {
		t.Fatalf("stored bytes: remembered space %d, default space %d; want the commit in the remembered space",
			g.Stored(SecondSpace), g.Stored(HostedSpace))
	}
	RunCLIOK(t, "real", env, nil, "pull", notes)
	code, stdout, stderr := RunCLI(t, "real", env, "status", notes)
	if code != 0 || !strings.Contains(stdout, "space "+SecondSpace) {
		t.Fatalf("status = exit %d, stdout %q, stderr %q; want the recorded space named", code, stdout, stderr)
	}
	// The directory now remembers its space, so status names the source it
	// did not have to be told about.
	if !strings.HasPrefix(stdout, notes+"  space "+SecondSpace+"\nspace: remembered space\n") {
		t.Fatalf("status = %q, want the remembered-space source named", stdout)
	}
	if code, _, stderr = RunCLI(t, "real", env, "log", notes); code != 0 {
		t.Fatalf("log = exit %d, stderr %q; want 0 in the remembered space", code, stderr)
	}
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after four bare commands = %s, want it unchanged (%s)", after, before)
	}

	// A sibling directory has no record, so it keeps the login's default
	// space, which no directory may pin.
	other := filepath.Join(root, "other")
	RunCLIOK(t, "real", env, nil, "pull", other)
	AssertReachedSpace(t, site, HostedSpace)
	if g.Stored(HostedSpace) != 0 {
		t.Fatal("a pull wrote into the default space")
	}
	AssertRecords(t, env, Recorded(notes, SecondSpace))
}

// TestScenarioFailedOperationRecordsNothing proves the gate of the
// recorder: a refused pull, a conflicted pull and a commit before any pull
// prove nothing about the space, so none of them records and none of them
// disturbs the entries of other directories.
func TestScenarioFailedOperationRecordsNothing(t *testing.T) {
	t.Parallel()
	_, _, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	conflicted := filepath.Join(root, "conflicted")
	early := filepath.Join(root, "early")

	// The second directory pulls the notebook while it is still empty, then
	// the first publishes a note the second one holds with other content.
	RunCLIOK(t, "real", env, nil, "pull", "--space", SecondSpace, conflicted)
	RunCLIOK(t, "real", env, nil, "pull", "--space", SecondSpace, notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "the published note\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "the first note")
	WriteCLIFile(t, filepath.Join(conflicted, "a.md"), "another note\n")

	full := filepath.Join(root, "full")
	WriteCLIFile(t, filepath.Join(full, "todo.md"), "someone else's file\n")
	before := ReadSettings(t, env)
	for _, row := range []struct {
		name string
		args []string
		want string
	}{
		{name: "a refused pull", args: []string{"pull", "--space", SecondSpace, full}, want: "DIRECTORY_NOT_EMPTY"},
		{name: "a conflicted pull", args: []string{"pull", "--space", SecondSpace, conflicted}, want: "MERGE_CONFLICT"},
		{name: "a commit before a pull", args: []string{"commit", "--space", SecondSpace, early, "-m", "too early"}, want: "PULL_REQUIRED"},
	} {
		t.Run(row.name, func(t *testing.T) {
			code, stdout, stderr := RunCLI(t, "real", env, row.args...)
			if code != 1 || !strings.Contains(stdout, row.want) {
				t.Fatalf("%v = exit %d, stdout %q, stderr %q; want the %s refusal", row.args, code, stdout, stderr, row.want)
			}
			if after := ReadSettings(t, env); after != before {
				t.Fatalf("the settings file after a failed command = %s, want it unchanged (%s)", after, before)
			}
		})
	}
}

// TestScenarioEachDirectoryKeepsItsOwnEntry proves that a record belongs to
// its directory: a second directory's successful pull adds its own entry and
// leaves the first where it was, and an operation against the notebook a
// directory already holds changes nothing.
func TestScenarioEachDirectoryKeepsItsOwnEntry(t *testing.T) {
	t.Parallel()
	_, _, env, root := RememberedEnv(t)
	notes, other := filepath.Join(root, "notes"), filepath.Join(root, "other")
	RunCLIOK(t, "real", env, nil, "pull", "--space", SecondSpace, notes)
	RunCLIOK(t, "real", env, nil, "pull", "--space", HostedSpace, other)
	want := []settings.Entry{Recorded(notes, SecondSpace), Recorded(other, HostedSpace)}
	AssertRecords(t, env, want...)

	before := ReadSettings(t, env)
	RunCLIOK(t, "real", env, nil, "pull", notes)
	RunCLIOK(t, "real", env, nil, "commit", other, "-m", "the sibling's first note")
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after two recorded notebooks = %s, want it unchanged (%s)", after, before)
	}
	AssertRecords(t, env, want...)
}

// TestScenarioFlagNamingTheRememberedSpaceReachesItsNotebook proves what a
// flag beside a record means at the command line: naming the space the
// directory already remembers reaches the notebook of that record, because
// the record is what says which notebook of the space the directory holds.
// Without that a `pull --space <remembered>` would address a second notebook
// of the same space under the default prefix, and the notes would split.
func TestScenarioFlagNamingTheRememberedSpaceReachesItsNotebook(t *testing.T) {
	t.Parallel()
	_, _, env, root := RememberedEnv(t)
	remembered := filepath.Join(root, "remembered")
	Remember(t, env, remembered, SecondSpace, "other-prefix")
	// The harness gives every process a prefix; an empty value resolves as
	// unset, so the default prefix stands.
	noPrefix := append(append([]string(nil), env...), "SLIVINGDOC_PREFIX=")

	RunCLIOK(t, "real", noPrefix, nil, "pull", remembered)
	WriteCLIFile(t, filepath.Join(remembered, "a.md"), "in the remembered notebook\n")
	RunCLIOK(t, "real", noPrefix, nil, "commit", remembered, "-m", "the remembered note")

	// The flag names the same space and no prefix, so it must reach the
	// notebook the directory already holds: its publication is the next one
	// of that notebook, which the bare log then reports.
	WriteCLIFile(t, filepath.Join(remembered, "a.md"), "a second line\n")
	RunCLIOK(t, "real", noPrefix, []string{}, "commit", "--space", SecondSpace, remembered, "-m", "the flagged note")
	code, stdout, stderr := RunCLI(t, "real", noPrefix, "log", remembered)
	if code != 0 || !strings.Contains(stdout, "the flagged note") {
		t.Fatalf("log of the remembered notebook = exit %d, stdout %q, stderr %q; want the flagged publication there", code, stdout, stderr)
	}
}

// TestScenarioBareCommandRemembersUnderItsResolvedRoot proves that a bare
// command records and reads the record under the workspace root as it stands
// after resolution: a root named relative to the working directory or with a
// home abbreviation becomes the absolute path the recorder writes and the
// next process reads, so the directory keeps reaching its own notebook.
func TestScenarioBareCommandRemembersUnderItsResolvedRoot(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		root func(work string) string
	}{
		{name: "an absolute root", root: func(work string) string { return filepath.Join(work, "notes") }},
		{name: "a relative root", root: func(string) string { return "notes" }},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, site, env, root := RememberedEnv(t)
			work := filepath.Join(root, "work")
			WriteCLIFile(t, filepath.Join(work, ".keep"), "")
			rowEnv := append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+row.root(work))

			first := SpawnHelperIn(t, work, "real", rowEnv, "pull", "--space", SecondSpace)
			if code, stdout, stderr := first.RunStdioProcess(t, nil); code != 0 {
				t.Fatalf("the first bare pull = exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
			AssertReachedSpace(t, site, SecondSpace)
			AssertRecords(t, rowEnv, Recorded(filepath.Join(work, "notes"), SecondSpace))

			second := SpawnHelperIn(t, work, "real", rowEnv, "status")
			code, stdout, stderr := second.RunStdioProcess(t, nil)
			if code != 0 || !strings.Contains(stdout, "space: remembered space") {
				t.Fatalf("the next bare status = exit %d, stdout %q, stderr %q; want the remembered space named", code, stdout, stderr)
			}
		})
	}
}

// TestScenarioStatusNamesTheRememberedSource proves the operator surface of
// the record at the command line: status in a directory that remembers its
// space names the source, a source the operator set by hand names nothing,
// and log names no source at all, because its report has no line to carry
// one.
func TestScenarioStatusNamesTheRememberedSource(t *testing.T) {
	t.Parallel()
	_, _, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "real", env, nil, "pull", "--space", SecondSpace, notes)
	remembered := filepath.Join(root, "remembered")
	Remember(t, env, remembered, SecondSpace, HostedPrefix)
	RunCLIOK(t, "real", env, nil, "pull", remembered)
	// A directory that never named a space keeps the login's default one,
	// and records nothing.
	other := filepath.Join(root, "other")
	RunCLIOK(t, "real", env, nil, "pull", other)

	for _, row := range []struct {
		name string
		args []string
		env  []string
		want string
	}{
		{
			name: "the remembered space names its source",
			args: []string{"status", remembered},
			want: remembered + "  space " + SecondSpace + "\nspace: remembered space\n",
		},
		{
			name: "a flag names no source",
			args: []string{"status", "--space", SecondSpace, remembered},
			want: remembered + "  space " + SecondSpace + "\ngeneration ",
		},
		{
			name: "a variable names no source",
			args: []string{"status", remembered},
			env:  []string{"SLIVINGDOC_SPACE=" + SecondSpace},
			want: remembered + "  space " + SecondSpace + "\ngeneration ",
		},
		{
			name: "the login's default space names no source",
			args: []string{"status", other},
			want: other + "  space " + HostedSpace + "\ngeneration ",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			rowEnv := env
			if row.env != nil {
				rowEnv = append(append([]string(nil), env...), row.env...)
			}
			code, stdout, stderr := RunCLI(t, "real", rowEnv, row.args...)
			if code != 0 || !strings.HasPrefix(stdout, row.want) {
				t.Fatalf("%v = exit %d, stdout %q, stderr %q; want it to start with %q", row.args, code, stdout, stderr, row.want)
			}
		})
	}

	code, stdout, stderr := RunCLI(t, "real", env, "log", remembered)
	if code != 0 || strings.Contains(stdout, "space") {
		t.Fatalf("log = exit %d, stdout %q, stderr %q; want no space named", code, stdout, stderr)
	}
}
