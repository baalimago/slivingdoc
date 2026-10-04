package integrationtest

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/settings"
)

// The recorder: what a successful pull or commit of a path-taking command
// stores in the scoped settings file of the configuration directory
// (architecture/config.md, The remembered notebook of a directory).

// recorded is one directory's remembered notebook, keyed by the notebook path
// the command resolved.
func recorded(path, space string) settings.Entry {
	return settings.Entry{Path: path, Target: settings.Target{Space: space, Prefix: hostedPrefix}}
}

// assertRecords proves the settings file of env's processes holds exactly
// these entries, in this order.
func assertRecords(t *testing.T, env []string, want ...settings.Entry) {
	t.Helper()
	set, err := settingsFile(t, env).Load()
	if err != nil {
		t.Fatalf("load the settings file: %v", err)
	}
	if got := set.Entries(); !slices.Equal(got, want) {
		t.Fatalf("the settings file holds %v, want %v", got, want)
	}
}

// assertRecordedFileNamesNoCredential proves the recorded file names the
// notebook and nothing that opens it.
func assertRecordedFileNamesNoCredential(t *testing.T, env []string) {
	t.Helper()
	body := strings.ToLower(readSettings(t, env))
	for _, forbidden := range []string{loginKey, secondKey, "token", "key", "endpoint", "account", "site"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the settings file = %s, want no %q in it", readSettings(t, env), forbidden)
		}
	}
}

// TestScenarioPullRecordsTheHostedNotebook proves the whole feature from the
// outside: one pull with --space records the notebook it reached, and every
// later bare command in that directory reaches the same space with no space
// and no storage flag, while a sibling directory keeps the login's default
// space and records nothing.
func TestScenarioPullRecordsTheHostedNotebook(t *testing.T) {
	t.Parallel()
	g, site, env, root := rememberedEnv(t)
	notes := filepath.Join(root, "notes")

	runCLIOK(t, "real", env, nil, "pull", "--space", secondSpace, notes)
	assertReachedSpace(t, site, secondSpace)
	assertRecords(t, env, recorded(notes, secondSpace))
	assertRecordedFileNamesNoCredential(t, env)

	// Every later command in that directory reaches the same notebook with
	// no flag, and none of them changes what the first pull recorded.
	before := readSettings(t, env)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "in the remembered space\n")
	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "remembered")
	assertReachedSpace(t, site, secondSpace)
	if g.Stored(secondSpace) == 0 || g.Stored(hostedSpace) != 0 {
		t.Fatalf("stored bytes: remembered space %d, default space %d; want the commit in the remembered space",
			g.Stored(secondSpace), g.Stored(hostedSpace))
	}
	runCLIOK(t, "real", env, nil, "pull", notes)
	code, stdout, stderr := runCLI(t, "real", env, "status", notes)
	if code != 0 || !strings.Contains(stdout, "space "+secondSpace) {
		t.Fatalf("status = exit %d, stdout %q, stderr %q; want the recorded space named", code, stdout, stderr)
	}
	// The directory now remembers its space, so status names the source it
	// did not have to be told about.
	if !strings.HasPrefix(stdout, notes+"  space "+secondSpace+"\nspace: remembered space\n") {
		t.Fatalf("status = %q, want the remembered-space source named", stdout)
	}
	if code, _, stderr = runCLI(t, "real", env, "log", notes); code != 0 {
		t.Fatalf("log = exit %d, stderr %q; want 0 in the remembered space", code, stderr)
	}
	if after := readSettings(t, env); after != before {
		t.Fatalf("the settings file after four bare commands = %s, want it unchanged (%s)", after, before)
	}

	// A sibling directory has no record, so it keeps the login's default
	// space, which no directory may pin.
	other := filepath.Join(root, "other")
	runCLIOK(t, "real", env, nil, "pull", other)
	assertReachedSpace(t, site, hostedSpace)
	if g.Stored(hostedSpace) != 0 {
		t.Fatal("a pull wrote into the default space")
	}
	assertRecords(t, env, recorded(notes, secondSpace))
}

// TestScenarioFailedOperationRecordsNothing proves the gate of the
// recorder: a refused pull, a conflicted pull and a commit before any pull
// prove nothing about the space, so none of them records and none of them
// disturbs the entries of other directories.
func TestScenarioFailedOperationRecordsNothing(t *testing.T) {
	t.Parallel()
	_, _, env, root := rememberedEnv(t)
	notes := filepath.Join(root, "notes")
	conflicted := filepath.Join(root, "conflicted")
	early := filepath.Join(root, "early")

	// The second directory pulls the notebook while it is still empty, then
	// the first publishes a note the second one holds with other content.
	runCLIOK(t, "real", env, nil, "pull", "--space", secondSpace, conflicted)
	runCLIOK(t, "real", env, nil, "pull", "--space", secondSpace, notes)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "the published note\n")
	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "the first note")
	writeCLIFile(t, filepath.Join(conflicted, "a.md"), "another note\n")

	full := filepath.Join(root, "full")
	writeCLIFile(t, filepath.Join(full, "todo.md"), "someone else's file\n")
	before := readSettings(t, env)
	for _, row := range []struct {
		name string
		args []string
		want string
	}{
		{name: "a refused pull", args: []string{"pull", "--space", secondSpace, full}, want: "DIRECTORY_NOT_EMPTY"},
		{name: "a conflicted pull", args: []string{"pull", "--space", secondSpace, conflicted}, want: "MERGE_CONFLICT"},
		{name: "a commit before a pull", args: []string{"commit", "--space", secondSpace, early, "-m", "too early"}, want: "PULL_REQUIRED"},
	} {
		t.Run(row.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, "real", env, row.args...)
			if code != 1 || !strings.Contains(stdout, row.want) {
				t.Fatalf("%v = exit %d, stdout %q, stderr %q; want the %s refusal", row.args, code, stdout, stderr, row.want)
			}
			if after := readSettings(t, env); after != before {
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
	_, _, env, root := rememberedEnv(t)
	notes, other := filepath.Join(root, "notes"), filepath.Join(root, "other")
	runCLIOK(t, "real", env, nil, "pull", "--space", secondSpace, notes)
	runCLIOK(t, "real", env, nil, "pull", "--space", hostedSpace, other)
	want := []settings.Entry{recorded(notes, secondSpace), recorded(other, hostedSpace)}
	assertRecords(t, env, want...)

	before := readSettings(t, env)
	runCLIOK(t, "real", env, nil, "pull", notes)
	runCLIOK(t, "real", env, nil, "commit", other, "-m", "the sibling's first note")
	if after := readSettings(t, env); after != before {
		t.Fatalf("the settings file after two recorded notebooks = %s, want it unchanged (%s)", after, before)
	}
	assertRecords(t, env, want...)
}

// TestScenarioFlagNamingTheRememberedSpaceReachesItsNotebook proves what a
// flag beside a record means at the command line: naming the space the
// directory already remembers reaches the notebook of that record, because
// the record is what says which notebook of the space the directory holds.
// Without that a `pull --space <remembered>` would address a second notebook
// of the same space under the default prefix, and the notes would split.
func TestScenarioFlagNamingTheRememberedSpaceReachesItsNotebook(t *testing.T) {
	t.Parallel()
	_, _, env, root := rememberedEnv(t)
	remembered := filepath.Join(root, "remembered")
	remember(t, env, remembered, secondSpace, "other-prefix")
	// The harness gives every process a prefix; an empty value resolves as
	// unset, so the default prefix stands.
	noPrefix := append(append([]string(nil), env...), "SLIVINGDOC_PREFIX=")

	runCLIOK(t, "real", noPrefix, nil, "pull", remembered)
	writeCLIFile(t, filepath.Join(remembered, "a.md"), "in the remembered notebook\n")
	runCLIOK(t, "real", noPrefix, nil, "commit", remembered, "-m", "the remembered note")

	// The flag names the same space and no prefix, so it must reach the
	// notebook the directory already holds: its publication is the next one
	// of that notebook, which the bare log then reports.
	writeCLIFile(t, filepath.Join(remembered, "a.md"), "a second line\n")
	runCLIOK(t, "real", noPrefix, []string{}, "commit", "--space", secondSpace, remembered, "-m", "the flagged note")
	code, stdout, stderr := runCLI(t, "real", noPrefix, "log", remembered)
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
			_, site, env, root := rememberedEnv(t)
			work := filepath.Join(root, "work")
			writeCLIFile(t, filepath.Join(work, ".keep"), "")
			rowEnv := append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+row.root(work))

			first := spawnHelperIn(t, work, "real", rowEnv, "pull", "--space", secondSpace)
			if code, stdout, stderr := first.runStdioProcess(t, nil); code != 0 {
				t.Fatalf("the first bare pull = exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
			assertReachedSpace(t, site, secondSpace)
			assertRecords(t, rowEnv, recorded(filepath.Join(work, "notes"), secondSpace))

			second := spawnHelperIn(t, work, "real", rowEnv, "status")
			code, stdout, stderr := second.runStdioProcess(t, nil)
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
	_, _, env, root := rememberedEnv(t)
	notes := filepath.Join(root, "notes")
	runCLIOK(t, "real", env, nil, "pull", "--space", secondSpace, notes)
	remembered := filepath.Join(root, "remembered")
	remember(t, env, remembered, secondSpace, hostedPrefix)
	runCLIOK(t, "real", env, nil, "pull", remembered)
	// A directory that never named a space keeps the login's default one,
	// and records nothing.
	other := filepath.Join(root, "other")
	runCLIOK(t, "real", env, nil, "pull", other)

	for _, row := range []struct {
		name string
		args []string
		env  []string
		want string
	}{
		{
			name: "the remembered space names its source",
			args: []string{"status", remembered},
			want: remembered + "  space " + secondSpace + "\nspace: remembered space\n",
		},
		{
			name: "a flag names no source",
			args: []string{"status", "--space", secondSpace, remembered},
			want: remembered + "  space " + secondSpace + "\ngeneration ",
		},
		{
			name: "a variable names no source",
			args: []string{"status", remembered},
			env:  []string{"SLIVINGDOC_SPACE=" + secondSpace},
			want: remembered + "  space " + secondSpace + "\ngeneration ",
		},
		{
			name: "the login's default space names no source",
			args: []string{"status", other},
			want: other + "  space " + hostedSpace + "\ngeneration ",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			rowEnv := env
			if row.env != nil {
				rowEnv = append(append([]string(nil), env...), row.env...)
			}
			code, stdout, stderr := runCLI(t, "real", rowEnv, row.args...)
			if code != 0 || !strings.HasPrefix(stdout, row.want) {
				t.Fatalf("%v = exit %d, stdout %q, stderr %q; want it to start with %q", row.args, code, stdout, stderr, row.want)
			}
		})
	}

	code, stdout, stderr := runCLI(t, "real", env, "log", remembered)
	if code != 0 || strings.Contains(stdout, "space") {
		t.Fatalf("log = exit %d, stdout %q, stderr %q; want no space named", code, stdout, stderr)
	}
}
