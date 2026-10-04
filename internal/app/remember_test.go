package app

// The recorder: what a successful pull or commit of a path-taking command
// stores in the scoped settings file of the configuration directory
// (architecture/config.md, The remembered notebook of a directory).

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// errRecordFailed is a write of the scoped settings that cannot happen: a
// file another user owns, a lock another process holds. The record is best
// effort, so such a failure changes no result.
var errRecordFailed = errors.New("settings: the settings file is not private")

// failingRecorder is the recorder a test refuses the write with.
func failingRecorder() func(context.Context, settings.Entry) error {
	return func(context.Context, settings.Entry) error { return errRecordFailed }
}

// recording collects the entries one runtime stored, so a test proves which
// operations recorded without reading the file behind them.
type recording struct {
	entries []settings.Entry
}

// record is the ProcessOptions.Record of this recording.
func (r *recording) record(_ context.Context, entry settings.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

// recorded proves the entries stored so far are exactly these.
func (r *recording) recorded(t *testing.T, entries ...settings.Entry) {
	t.Helper()
	if !slices.Equal(r.entries, entries) {
		t.Fatalf("recorded = %v, want %v", r.entries, entries)
	}
}

// rigs is a hosted account of an operator, with what several path-taking
// commands of one test share: one credentials file, one workspace root, one
// private-state parent, and one account that reaches the default space notes
// and the space second-space. The records are planted once, so a command
// never overwrites what an earlier command of the same test recorded.
type rigs struct {
	env     []string
	root    string
	private string
}

// hostedRigs starts that account: a reference site and a reference storage
// API, a stored login whose key the site trades for space tokens, and the
// records the commands of this test read.
func hostedRigs(t *testing.T, records ...settings.Entry) *rigs {
	t.Helper()
	site := sitetest.Start(t)
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	g.AddSpace(rememberedSpace, 1<<20)
	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Key: loginToken, Access: "write", Endpoint: g.URL(), ExpiresAt: loginExpiry, Account: "ada@example.test",
	}})
	site.OnMint(func(m sitetest.Minted) { g.GrantAs(m.Token, m.SpaceID, m.Space, false) })
	site.SetSpaces(loginToken, notesSpace, space(rememberedSpace, "ada@example.test", "write"))
	site.Issued(loginToken, g.URL())
	rig := &rigs{
		env: []string{writeLogins(t, defaults(g.URL(), "notes"), storedLogin{
			Site: site.URL(), Endpoint: g.URL(), Key: loginToken, Access: "write", Account: "ada@example.test",
		})},
		root:    t.TempDir(),
		private: t.TempDir(),
	}
	for _, record := range records {
		rig.plant(t, record)
	}
	return rig
}

// runtime is one path-taking command of this account: path is the notebook
// directory it resolved, empty for a bare command, and args are its flags.
// rec replaces the process's recorder when it is set.
func (r *rigs) runtime(t *testing.T, rec func(context.Context, settings.Entry) error, path string, args ...string) *Runtime {
	t.Helper()
	return r.start(t, associatedProcess(r.env, path, args...), rec)
}

// runtimeOverAnUnreadableFile is one path-taking command that consults no
// record: the write-side rows need a settings file this build cannot read,
// and such a file refuses the startup that would read it, so only the write
// is left to answer for it.
func (r *rigs) runtimeOverAnUnreadableFile(t *testing.T, path string, args ...string) *Runtime {
	t.Helper()
	p := associatedProcess(r.env, path, args...)
	p.association.load = nil
	return r.start(t, p, nil)
}

// start finishes one command of this account: the roots and the store the
// account shares, then rec in place of the recorder when a test injects one.
func (r *rigs) start(t *testing.T, p process, rec func(context.Context, settings.Entry) error) *Runtime {
	t.Helper()
	p.cwd, p.cacheDir, p.engine = r.root, r.private, git2.New()
	p.storeFactory = realStoreFactory
	if rec != nil {
		p.association.record = rec
	}
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

// plant adds one record to the settings file, the way an earlier command of
// the same test recorded it.
func (r *rigs) plant(t *testing.T, record settings.Entry) {
	t.Helper()
	file := r.settingsFile(t)
	set, err := file.Load()
	if err != nil {
		t.Fatal(err)
	}
	mustDo(t, file.Save(set.Put(record)))
}

// settingsFile is the scoped settings file of that credentials directory.
func (r *rigs) settingsFile(t *testing.T) settings.File {
	t.Helper()
	file, err := settings.Locate(func(string) string { return configDirOf(t, r.env) }, runtime.GOOS)
	if err != nil {
		t.Fatalf("locate the settings file: %v", err)
	}
	return file
}

// readSettings is the settings file as it stands.
func (r *rigs) readSettings(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(r.settingsFile(t).Path())
	if err != nil {
		t.Fatalf("read the settings file: %v", err)
	}
	return string(body)
}

// replaceSettings writes the settings file exactly as given, which this build
// refuses to read.
func (r *rigs) replaceSettings(t *testing.T, body string) {
	t.Helper()
	mustDo(t, os.MkdirAll(configDirOf(t, r.env), 0o700))
	mustDo(t, os.WriteFile(filepath.Join(configDirOf(t, r.env), settings.FileName), []byte(body), 0o600))
}

// remembered is the record of the directory at path.
func remembered(path, space, prefix string) settings.Entry {
	return settings.Entry{Path: path, Target: settings.Target{Space: space, Prefix: prefix}}
}

// TestRecordsSpaceAfterPull proves a successful pull records the notebook it
// proved: the space it reached and the prefix it addressed, under the key of
// the directory, which is the workspace root for a bare command and the
// cleaned argument otherwise.
func TestRecordsSpaceAfterPull(t *testing.T) {
	for _, row := range []struct {
		name string
		path func(root string) string
		key  func(root string) string
	}{
		{
			name: "the workspace root for a bare command",
			path: func(root string) string { return "" },
			key:  func(root string) string { return root },
		},
		{
			name: "the cleaned argument for a named directory",
			path: func(root string) string { return filepath.Join(root, "notes", "sub", "..") },
			key:  func(root string) string { return filepath.Join(root, "notes") },
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			rec := &recording{}
			rig := hostedRigs(t)
			rt := rig.runtime(t, rec.record, row.path(rig.root), "--space", rememberedSpace)
			if _, err := rt.Pull(context.Background(), row.path(rig.root)); err != nil {
				t.Fatalf("Pull() = %v", err)
			}
			rec.recorded(t, remembered(row.key(rig.root), rememberedSpace, defaultPrefix))
		})
	}
}

// TestRecordsSpaceAfterCommit proves the commit-side record, which repairs a
// directory whose entry was lost with the file: a commit against the notebook
// a pull already recorded rewrites nothing, and the same commit without that
// entry stores it again.
func TestRecordsSpaceAfterCommit(t *testing.T) {
	rig := hostedRigs(t)
	rt := rig.runtime(t, nil, "", "--space", rememberedSpace)
	if _, err := rt.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	afterPull := rig.readSettings(t)
	writeNote(t, rig.root, "a.md", "hello")
	if _, err := rt.Commit(context.Background(), "", "first note"); err != nil {
		t.Fatalf("Commit() = %v", err)
	}
	if after := rig.readSettings(t); after != afterPull {
		t.Fatalf("the settings file after a commit = %s, want it unchanged (%s)", after, afterPull)
	}
	// The entry is gone with the file, so the next commit records the
	// notebook this directory proved it holds.
	mustDo(t, os.Remove(rig.settingsFile(t).Path()))
	if _, err := rt.Commit(context.Background(), "", "second note"); err != nil {
		t.Fatalf("Commit() = %v", err)
	}
	stored, err := rig.settingsFile(t).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []settings.Entry{remembered(rig.root, rememberedSpace, defaultPrefix)}
	if !slices.Equal(stored.Entries(), want) {
		t.Fatalf("the settings file holds %v, want the commit's entry %v", stored.Entries(), want)
	}
}

// TestRememberedOnlyOnSuccess proves the gate: an operation that returned an
// error recorded nothing, whatever the space was and wherever it came from.
func TestRememberedOnlyOnSuccess(t *testing.T) {
	for _, row := range []struct {
		name string
		run  func(t *testing.T, rig *rigs, rec *recording)
	}{
		{
			name: "a pull refused by a directory that holds other files",
			run: func(t *testing.T, rig *rigs, rec *recording) {
				rt := rig.runtime(t, rec.record, "", "--space", rememberedSpace)
				if _, err := rt.Pull(context.Background(), ""); err != nil {
					t.Fatalf("the seeding pull = %v", err)
				}
				writeNote(t, rig.root, "published.md", "in the notebook\n")
				if _, err := rt.Commit(context.Background(), "", "seed"); err != nil {
					t.Fatalf("the seeding commit = %v", err)
				}
				// A second directory has no pulled marker, and the file it
				// holds is not in the notebook.
				rec.entries = nil
				other := filepath.Join(rig.root, "other")
				writeNote(t, other, "todo.md", "someone else's file\n")
				if _, err := rt.Pull(context.Background(), other); err == nil {
					t.Fatal("the first pull into a foreign directory = nil, want the refusal")
				}
			},
		},
		{
			name: "a commit before a pull",
			run: func(t *testing.T, rig *rigs, rec *recording) {
				rt := rig.runtime(t, rec.record, "", "--space", rememberedSpace)
				if _, err := rt.Commit(context.Background(), "", "too early"); err == nil {
					t.Fatal("the commit before a pull = nil, want the refusal")
				}
			},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			rec := &recording{}
			row.run(t, hostedRigs(t), rec)
			rec.recorded(t)
		})
	}
}

// TestS3ModeNeverRecords proves an S3 process records nothing: a bucket is
// not a space, and the file holds hosted notebooks only.
func TestS3ModeNeverRecords(t *testing.T) {
	rec := &recording{}
	p := testProcess([]string{"SLIVINGDOC_BUCKET=a-bucket"})
	p.cwd, p.cacheDir, p.engine = t.TempDir(), t.TempDir(), git2.New()
	p.association.record = rec.record
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if _, err := rt.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	rec.recorded(t)
}

// TestDefaultSpaceIsNotRecorded proves the login's default space is never
// pinned to a directory: changing the default must keep working everywhere.
func TestDefaultSpaceIsNotRecorded(t *testing.T) {
	rec := &recording{}
	rt := hostedRigs(t).runtime(t, rec.record, "")
	if rt.SpaceSource() != "default space" {
		t.Fatalf("space source = %q, want the login's default space", rt.SpaceSource())
	}
	if _, err := rt.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	rec.recorded(t)
}

// TestTokenSpaceIsNotRecorded proves a token's own space is never recorded:
// a token names exactly one space, and it ignores the record for that run.
func TestTokenSpaceIsNotRecorded(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	g.Grant(hostedTestToken, "notes", false)
	rec := &recording{}
	p := testProcess([]string{
		"SLIVINGDOC_TOKEN=" + hostedTestToken,
		"SLIVINGDOC_ENDPOINT=" + g.URL(),
	})
	p.cwd, p.cacheDir, p.engine = t.TempDir(), t.TempDir(), git2.New()
	p.association.record = rec.record
	p.storeFactory = realStoreFactory
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if rt.SpaceSource() != "token" {
		t.Fatalf("space source = %q, want the token's own space", rt.SpaceSource())
	}
	if _, err := rt.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	rec.recorded(t)
}

// TestExplicitChoiceRecordsAnEmptyDirectory proves the recording half of F15
// and F16: a flag or a variable records the notebook of a directory that
// remembers nothing. The beside-a-record half is
// TestScenarioExplicitChoiceBeatsTheRememberedSpace.
func TestExplicitChoiceRecordsAnEmptyDirectory(t *testing.T) {
	rig := hostedRigs(t)
	rec := &recording{}
	if _, err := rig.runtime(t, rec.record, "", "--space", rememberedSpace).Pull(context.Background(), ""); err != nil {
		t.Fatalf("the pull into a directory that remembers nothing = %v", err)
	}
	rec.recorded(t, remembered(rig.root, rememberedSpace, defaultPrefix))
}

// TestServeNeverRecords proves a server records nothing: its process carries
// no notebook path, so its store is fixed before any directory is known
// (architecture/config.md).
func TestServeNeverRecords(t *testing.T) {
	rt := hostedRigs(t).runtime(t, nil, "")
	rt.p.association = settingsAssociation{}
	if _, err := rt.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull() = %v", err)
	}
}

// TestRecordedKeyIsTheResolvedPath proves the key over the file rather than
// over a recorder: the entry a pull stores is keyed by the path its next
// process reads the record by, so a bare pull in that directory reaches the
// same space with no flag.
func TestRecordedKeyIsTheResolvedPath(t *testing.T) {
	rig := hostedRigs(t)
	if _, err := rig.runtime(t, nil, "", "--space", rememberedSpace).Pull(context.Background(), ""); err != nil {
		t.Fatalf("the pull that records = %v", err)
	}
	rec := &recording{}
	next := rig.runtime(t, rec.record, "")
	if next.SpaceSource() != "remembered space" {
		t.Fatalf("the second process took its space from %q, want the remembered space", next.SpaceSource())
	}
	if _, err := next.Pull(context.Background(), ""); err != nil {
		t.Fatalf("the bare pull that reads the record = %v", err)
	}
	rec.recorded(t, remembered(rig.root, rememberedSpace, defaultPrefix))
}

// TestRecordFailureChangesNoResult proves D8 and F23: a recorder that fails
// warns once and changes neither the result of the operation nor its exit.
func TestRecordFailureChangesNoResult(t *testing.T) {
	var logs strings.Builder
	rt := hostedRigs(t).runtime(t, failingRecorder(), "", "--space", rememberedSpace)
	rt.base, rt.logger = warningLogger(&logs)
	res, err := rt.Pull(context.Background(), "")
	if err != nil || res.Generation != 0 {
		t.Fatalf("Pull() = %+v, %v; want the unchanged result of the pull", res, err)
	}
	if got := logs.String(); !strings.Contains(got, "remembering the hosted notebook failed") ||
		!strings.Contains(got, errRecordFailed.Error()) {
		t.Fatalf("the warning = %s, want it to name the failed record", got)
	}
}

// TestSecondDirectoryReplacesOnlyItsOwnEntry proves D13 over the file: a
// record of one directory leaves every other entry where it was, an unchanged
// entry rewrites nothing, and a record of another notebook replaces the one
// entry of that key.
func TestSecondDirectoryReplacesOnlyItsOwnEntry(t *testing.T) {
	rig := hostedRigs(t)
	store := ProcessOptions{Env: rig.env}.WithAssociation("").Record
	first := remembered("/work/notes", "notes", "team")
	second := remembered("/work/other", rememberedSpace, "team")
	for _, entry := range []settings.Entry{first, second} {
		if err := store(context.Background(), entry); err != nil {
			t.Fatalf("record %s = %v", entry.Path, err)
		}
	}
	stored, err := rig.settingsFile(t).Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []settings.Entry{first, second}; !slices.Equal(stored.Entries(), want) {
		t.Fatalf("the settings file holds %v, want both entries in order: %v", stored.Entries(), want)
	}
	before := rig.readSettings(t)
	if err := store(context.Background(), first); err != nil {
		t.Fatalf("record the first directory again = %v", err)
	}
	if after := rig.readSettings(t); after != before {
		t.Fatalf("the settings file after an unchanged record = %s, want it unchanged (%s)", after, before)
	}
	moved := first
	moved.Target.Space = rememberedSpace
	if err := store(context.Background(), moved); err != nil {
		t.Fatalf("record the moved notebook = %v", err)
	}
	again, err := rig.settingsFile(t).Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []settings.Entry{moved, second}; !slices.Equal(again.Entries(), want) {
		t.Fatalf("the settings file holds %v, want the replaced entry beside the other: %v", again.Entries(), want)
	}
}

// TestRecordedFileHoldsNoCredential proves the file the recorder writes
// carries the version, the paths, the spaces and the prefixes only: no key,
// no token and no endpoint, in any spelling.
func TestRecordedFileHoldsNoCredential(t *testing.T) {
	rig := hostedRigs(t)
	if _, err := rig.runtime(t, nil, "", "--space", rememberedSpace).Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	body := rig.readSettings(t)
	for _, want := range []string{
		`"version":1`,
		`"path":"` + rig.root + `"`,
		`"space":"` + rememberedSpace + `"`,
		`"prefix":"slivingdoc"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the settings file = %s, want it to contain %s", body, want)
		}
	}
	for _, forbidden := range []string{"sld_", loginToken, hostedTestToken, "token", "key", "endpoint", "site", "account"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("the settings file = %s, want no %q in it", body, forbidden)
		}
	}
}

// TestRecordRefusedOverBoundChangesNoResult proves F24: a file over the bound
// the credentials file already accepts refuses the write, the warning names
// the bound, and the pull keeps its result.
func TestRecordRefusedOverBoundChangesNoResult(t *testing.T) {
	rig := hostedRigs(t)
	rig.replaceSettings(t, `{"version":1,"entries":[]}`+strings.Repeat(" ", credentials.MaxFileSize))
	var logs strings.Builder
	rt := rig.runtimeOverAnUnreadableFile(t, "", "--space", rememberedSpace)
	rt.base, rt.logger = warningLogger(&logs)
	res, err := rt.Pull(context.Background(), "")
	if err != nil || res.Generation != 0 {
		t.Fatalf("Pull() = %+v, %v; want the unchanged result of the pull", res, err)
	}
	if got := logs.String(); !strings.Contains(got, "remembering the hosted notebook failed") ||
		!strings.Contains(got, "larger than") {
		t.Fatalf("the warning = %s, want it to name the bound", got)
	}
}

// TestUnreadableSettingsFileWarnsAndContinues proves F3 at the write: a
// recorder whose file cannot be read warns once and the pull keeps its
// result, because the notebook is already stored.
func TestUnreadableSettingsFileWarnsAndContinues(t *testing.T) {
	rig := hostedRigs(t)
	rig.replaceSettings(t, `{"version":1,"entries":[]}`)
	mustDo(t, os.Chmod(rig.settingsFile(t).Path(), 0o644))
	var logs strings.Builder
	rt := rig.runtimeOverAnUnreadableFile(t, "", "--space", rememberedSpace)
	rt.base, rt.logger = warningLogger(&logs)
	if _, err := rt.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	if got := logs.String(); !strings.Contains(got, "remembering the hosted notebook failed") ||
		!strings.Contains(got, "chmod 600") {
		t.Fatalf("the warning = %s, want it to name the file and the fix", got)
	}
}

// warningLogger is the process logger a test reads: the warn level and above,
// without colour.
func warningLogger(out *strings.Builder) (*slog.Logger, *slog.Logger) {
	logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return logger, logger
}

// mustDo fails the test on an error the caller cannot act on.
func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
