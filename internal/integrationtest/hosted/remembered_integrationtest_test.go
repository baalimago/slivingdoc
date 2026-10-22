package hosted

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/settings"
)

// TestScenarioRememberedSpaceReachesTheHostedStore proves, from the outside,
// the property the feature exists for: a directory whose settings file
// remembers a hosted space reaches that space with a bare pull, commit,
// status and log, with no space and no storage flag, while a sibling
// directory without an entry keeps the login's default space. Reading the
// file writes nothing.
func TestScenarioRememberedSpaceReachesTheHostedStore(t *testing.T) {
	t.Parallel()
	g, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	before := Remember(t, env, notes, SecondSpace, HostedPrefix)

	RunCLIOK(t, "real", env, nil, "pull", notes)
	AssertReachedSpace(t, site, SecondSpace)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "in the remembered space\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "remembered")
	if g.Stored(SecondSpace) == 0 || g.Stored(HostedSpace) != 0 {
		t.Fatalf("stored bytes: remembered space %d, default space %d; want the commit in the remembered space",
			g.Stored(SecondSpace), g.Stored(HostedSpace))
	}
	code, stdout, stderr := RunCLI(t, "real", env, "status", notes)
	if code != 0 || !strings.Contains(stdout, "space "+SecondSpace) {
		t.Fatalf("status = exit %d, stdout %q, stderr %q; want the remembered space named", code, stdout, stderr)
	}
	// log prints its own report, so only its exit proves the read worked.
	if code, _, stderr = RunCLI(t, "real", env, "log", notes); code != 0 {
		t.Fatalf("log = exit %d, stderr %q; want 0 in the remembered space", code, stderr)
	}

	// A sibling directory has no entry, so it keeps the login's default
	// space and never reaches the remembered one.
	other := filepath.Join(root, "other")
	RunCLIOK(t, "real", env, nil, "pull", other)
	AssertReachedSpace(t, site, HostedSpace)
	if g.Stored(HostedSpace) != 0 {
		t.Fatal("a pull wrote into the default space")
	}
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("settings file after five commands = %q, want it unchanged (%q)", after, before)
	}
}

// TestScenarioExplicitChoiceBeatsTheRememberedSpace proves the precedence
// the configuration documents: a flag or a variable naming a space wins over
// the record, and neither is written back.
func TestScenarioExplicitChoiceBeatsTheRememberedSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	before := Remember(t, env, notes, SecondSpace, HostedPrefix)
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }
	for _, row := range []struct {
		name string
		env  []string
		args []string
	}{
		{name: "--space", args: []string{"--space", HostedSpace}},
		{name: "--bucket", args: []string{"--bucket", HostedSpace}},
		{name: "SLIVINGDOC_SPACE", env: with("SLIVINGDOC_SPACE=" + HostedSpace)},
		{name: "SLIVINGDOC_BUCKET", env: with("SLIVINGDOC_BUCKET=" + HostedSpace)},
	} {
		t.Run(row.name, func(t *testing.T) {
			rowEnv := row.env
			if rowEnv == nil {
				rowEnv = env
			}
			RunCLIOK(t, "real", rowEnv, nil, append(append([]string{"pull"}, row.args...), notes)...)
			AssertReachedSpace(t, site, HostedSpace)
		})
	}
	if g.Stored(SecondSpace) != 0 {
		t.Fatal("a pull wrote into the remembered space")
	}
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("settings file after four commands = %q, want it unchanged (%q)", after, before)
	}
}

// TestScenarioRememberedSpaceBeatsS3Settings proves the two places where
// ambient AWS configuration would otherwise refuse the process or pick S3:
// a remembered space is the space this directory means, so --storage auto
// keeps hosted beside the shared AWS files and the AWS variables, and
// --storage hosted with no space takes the remembered one.
func TestScenarioRememberedSpaceBeatsS3Settings(t *testing.T) {
	t.Parallel()
	_, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	Remember(t, env, notes, SecondSpace, HostedPrefix)
	home := t.TempDir()
	WriteCLIFile(t, filepath.Join(home, ".aws", "credentials"), "[default]\naws_access_key_id = key\n")
	awsEnv := append(append([]string(nil), env...),
		"HOME="+home, "AWS_REGION=eu-north-1",
		"AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret")

	RunCLIOK(t, "real", awsEnv, nil, "pull", notes)
	AssertReachedSpace(t, site, SecondSpace)
	// The same machine, told which store it means, takes the remembered
	// space instead of asking for a space.
	RunCLIOK(t, "real", awsEnv, nil, "pull", "--storage", "hosted", notes)
	AssertReachedSpace(t, site, SecondSpace)
}

// TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace proves the two store
// choices that consult no record: SLIVINGDOC_TOKEN takes the space the API
// says the token reaches, and --storage s3 asks for a bucket. Both leave the
// entry untouched.
func TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	before := Remember(t, env, notes, SecondSpace, HostedPrefix)

	// The token reaches the first space only. The record names the other
	// one, so a process that consulted it would refuse with a mismatch.
	tokenEnv := append(append([]string(nil), env...),
		"SLIVINGDOC_TOKEN="+HostedToken, "SLIVINGDOC_ENDPOINT="+g.URL())
	RunCLIOK(t, "real", tokenEnv, nil, "pull", notes)
	if mints := site.Mints(); len(mints) != 0 {
		t.Fatalf("mints = %+v, want none from a token process", mints)
	}
	if used := g.Used(); used[HostedToken] == 0 {
		t.Fatalf("space requests per token = %v, want the token's own space", used)
	}

	// S3 mode asks for its bucket: a space is not one, so a bucketless S3
	// process is refused for that reason and not by the hosted rules.
	args := []string{"pull", "--storage", "s3", notes}
	code, stdout, stderr := RunCLI(t, "fake", env, args...)
	AssertRefusal(t, args, code, stdout, stderr, "bucket is required")
	RunCLIOK(t, "fake", env, nil, "pull", "--storage", "s3", "--bucket", "a-bucket", notes)
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("settings file after three commands = %q, want it unchanged (%q)", after, before)
	}
}

// TestScenarioRememberedSpaceRefusals proves every way a remembered space
// can fail at the command line: no credential reaches it, the stored login
// is for another endpoint or expired, the account does not hold the space,
// the space is outside the API grammar, and a named prefix addresses
// another notebook of the space. No refusal reaches a store.
func TestScenarioRememberedSpaceRefusals(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name   string
		expire bool
		plant  func(t *testing.T, env []string, dir string) []string
		want   []string
	}{
		{
			name: "no stored login reaches the space",
			plant: func(t *testing.T, env []string, dir string) []string {
				other := append(append([]string(nil), env...),
					credentials.DirEnv+"="+filepath.Join(t.TempDir(), "cfg"))
				Remember(t, other, dir, SecondSpace, HostedPrefix)
				// The machine configures S3 on purpose, so an empty answer
				// would have pulled a bucket and hidden the record.
				return append(other,
					"AWS_ENDPOINT_URL_S3="+ClosedS3, "AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret")
			},
			want: []string{`remembers hosted space "second-space"`, "run 'slivingdoc login'", "pass --space"},
		},
		{
			name: "the only stored login is for another endpoint",
			plant: func(t *testing.T, env []string, dir string) []string {
				Remember(t, env, dir, SecondSpace, HostedPrefix)
				return append(append([]string(nil), env...), "SLIVINGDOC_ENDPOINT="+ClosedS3)
			},
			want: []string{`remembers hosted space "second-space"`, "at " + ClosedS3, "the stored login is for"},
		},
		{
			name:   "the stored login expired",
			expire: true,
			plant: func(t *testing.T, env []string, dir string) []string {
				Remember(t, env, dir, SecondSpace, HostedPrefix)
				return env
			},
			want: []string{"stored login expired", "run 'slivingdoc login' again"},
		},
		{
			name: "the account holds no such space",
			plant: func(t *testing.T, env []string, dir string) []string {
				Remember(t, env, dir, "not-my-space", HostedPrefix)
				return env
			},
			want: []string{
				`could not mint a token for space "not-my-space"`,
				`the login reaches no space "not-my-space"`, "check remembered space",
			},
		},
		{
			name: "a space outside the API grammar",
			plant: func(t *testing.T, env []string, dir string) []string {
				Remember(t, env, dir, "Second_Space", HostedPrefix)
				return env
			},
			want: []string{"invalid space name", `"Second_Space"`},
		},
		{
			name: "the variable names another notebook of the space",
			plant: func(t *testing.T, env []string, dir string) []string {
				Remember(t, env, dir, SecondSpace, "other-prefix")
				return append(append([]string(nil), env...), "SLIVINGDOC_PREFIX=other-prefix-2")
			},
			want: []string{
				`remembers hosted space "second-space" with prefix "other-prefix"`,
				`"other-prefix-2" names another notebook`, "--prefix other-prefix",
			},
		},
		{
			name: "a flag names another notebook of the space",
			plant: func(t *testing.T, env []string, dir string) []string {
				Remember(t, env, dir, SecondSpace, "other-prefix")
				return env
			},
			want: []string{
				`remembers hosted space "second-space" with prefix "other-prefix"`,
				`"` + HostedPrefix + `" names another notebook`, "--prefix other-prefix",
			},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, root := RememberedEnv(t)
			if row.expire {
				ExpireStoredLogin(t, g, site, env)
			}
			notes := filepath.Join(root, "notes")
			rowEnv := row.plant(t, env, notes)
			before := Reach(g, site)
			args := []string{"pull", notes}
			code, stdout, stderr := RunCLI(t, "real", rowEnv, args...)
			AssertRefusal(t, args, code, stdout, stderr, row.want...)
			if after := Reach(g, site); after != before {
				t.Fatalf("the refusal reached a store: %q then %q", before, after)
			}
		})
	}
}

// TestScenarioSeveralStoredLoginsRefuseTheRememberedSpace proves the
// ambiguity refusal keeps its meaning beside a record: two stored logins
// need --endpoint, whatever the directory remembers.
func TestScenarioSeveralStoredLoginsRefuseTheRememberedSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := RememberedEnv(t)
	g2 := gatewaytest.Start(t)
	g2.AddSpace(SecondSpace, 1<<20)
	GrantMints(site, g2)
	Approve(site, g2, SecondKey, "read", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g2, "read only", WithDefault), "--no-browser")
	notes := filepath.Join(root, "notes")
	Remember(t, env, notes, SecondSpace, HostedPrefix)

	args := []string{"pull", notes}
	code, stdout, stderr := RunCLI(t, "real", env, args...)
	AssertRefusal(t, args, code, stdout, stderr, "several stored logins match", "pass --endpoint")
	if g.Requests() != 0 || g2.Requests() != 0 {
		t.Fatal("a refused process reached a storage endpoint")
	}
}

// TestScenarioUnusableSettingsFileRefusesStartup proves the rows of the
// settings file that need no POSIX facility: bytes this build cannot parse,
// a credential beside the space, a file of another version in either
// direction, and a file over the bound the credentials file already
// accepts. Each refusal names the file and the way to run without it, and
// the file stays as it was.
func TestScenarioUnusableSettingsFileRefusesStartup(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		body string
		want string
	}{
		{name: "bytes this build cannot parse", body: `{"version":1,"entries":[{"path":"/notes"}]}`, want: "malformed settings file"},
		{name: "a credential beside the space", body: `{"version":1,"entries":[{"path":"/notes","target":{"space":"team-notes","token":"sld_secret"}}]}`, want: "malformed settings file"},
		{name: "an older version", body: `{"version":0,"entries":[]}`, want: "unsupported settings file version"},
		{name: "a newer build's version", body: `{"version":99,"entries":[]}`, want: "a newer slivingdoc wrote it"},
		{name: "a file over the bound", body: `{"version":1,"entries":[]}` + strings.Repeat(" ", 1<<20), want: "larger than 1048576 bytes"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, root := RememberedEnv(t)
			PlantSettings(t, env, row.body)
			before := Reach(g, site)
			args := []string{"pull", filepath.Join(root, "notes")}
			code, stdout, stderr := RunCLI(t, "real", env, args...)
			AssertRefusal(t, args, code, stdout, stderr,
				row.want, "workspaces.json", "remove it to choose the space with --space")
			if after := Reach(g, site); after != before {
				t.Fatalf("the refusal reached a store: %q then %q", before, after)
			}
			if after := ReadSettings(t, env); after != row.body {
				t.Fatalf("settings file = %q, want it untouched", after)
			}
		})
	}
}

// TestScenarioServeReadsTheRecord proves the durable server's half of the
// binding: it reads the record of its workspace root before the transport
// starts, reaches that notebook with no target flag, and records the target
// a flag-named operation proved. A record write that fails after a
// successful call changes no result and is logged. A scratch server is the
// other half, in TestScenarioEphemeralNotebook (I3, I6, I7, I8).
func TestScenarioServeReadsTheRecord(t *testing.T) {
	t.Parallel()
	g, site, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "notes")
	before := Remember(t, env, notes, SecondSpace, HostedPrefix)

	h := SpawnHelper(t, "real", env, "serve", "--workspace-root="+notes)
	cs := h.ConnectClient(t)
	AssertProcessCallOK(t, cs, ToolPull, notes, "")
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "published in the recorded space\n")
	AssertProcessCallOK(t, cs, ToolCommit, notes, "served in the recorded space")
	h.CloseClient(t, cs)
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after a record-sourced serve = %q, want it unchanged (%q)", after, before)
	}
	if g.Stored(SecondSpace) == 0 || g.Stored(HostedSpace) != 0 {
		t.Fatalf("stored bytes: recorded space %d, default space %d; want the commit in the recorded space",
			g.Stored(SecondSpace), g.Stored(HostedSpace))
	}
	for _, m := range site.Mints() {
		if m.Space != SecondSpace {
			t.Fatalf("the server minted for space %q, want the recorded space", m.Space)
		}
	}

	// A durable directory with no record takes the target a flag names and
	// records it after the first successful operation.
	fresh := filepath.Join(root, "fresh")
	h = SpawnHelper(t, "real", env, "serve", "--workspace-root="+fresh, "--space", SecondSpace)
	cs = h.ConnectClient(t)
	AssertProcessCallOK(t, cs, ToolPull, fresh, "")
	h.CloseClient(t, cs)
	AssertRecords(t, env, settings.Entry{Path: notes, Target: settings.Target{Kind: settings.KindHosted, Name: SecondSpace, Prefix: HostedPrefix}},
		settings.Entry{Path: fresh, Target: settings.Target{Kind: settings.KindHosted, Name: SecondSpace, Prefix: HostedPrefix}})

	// A record write that fails after a successful call leaves the call
	// result unchanged: the server still serves, and the failure is logged.
	configDir := CredentialsDir(env)
	MustDo(t, os.Chmod(configDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(configDir, 0o700) })
	beforeFailure := ReadSettings(t, env)
	failed := filepath.Join(root, "failed")
	h = SpawnHelper(t, "real", env, "serve", "--workspace-root="+failed, "--space", SecondSpace)
	cs = h.ConnectClient(t)
	AssertProcessCallOK(t, cs, ToolPull, failed, "")
	h.CloseClient(t, cs)
	if !strings.Contains(h.StderrText(t), "remembering the notebook failed") {
		t.Fatalf("the server stderr = %q, want the failed record write logged", h.StderrText(t))
	}
	if after := ReadSettings(t, env); after != beforeFailure {
		t.Fatalf("the settings file after a failed record write = %q, want it unchanged (%q)", after, beforeFailure)
	}
}
