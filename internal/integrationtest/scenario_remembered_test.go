package integrationtest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// secondSpace is another space of the login's account, so a resolved space
// tells the remembered source apart from the login's default space.
const secondSpace = "second-space"

// rememberedEnv starts the reference storage API with the login's default
// space and a second one, a reference site whose minted tokens the gateway
// grants, and a stored login for that account whose default space is the
// first one. The configuration directory holds the credentials file and the
// scoped settings file beside each other, so one environment selects both
// (architecture/config.md, The scoped settings store).
func rememberedEnv(t *testing.T) (*gatewaytest.Gateway, *sitetest.Site, []string, string) {
	t.Helper()
	g, site, env, root := loginEnv(t)
	g.AddSpace(secondSpace, 1<<20)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")
	site.SetSpaces(loginKey, teamNotes, secondTeamNotes)
	return g, site, env, root
}

// secondTeamNotes is the second space of the login's account, whose id the
// gateway and the site share.
var secondTeamNotes = sitetest.Space{Name: secondSpace, Owner: loginAccount, Access: "write"}

// remember plants one entry in the settings file of env's processes: the
// hosted notebook a directory holds. It answers the bytes it wrote, so a
// scenario can prove a later command left the file untouched.
func remember(t *testing.T, env []string, path, space, prefix string) string {
	t.Helper()
	file := settingsFile(t, env)
	set, err := file.Load()
	if err != nil {
		t.Fatalf("load the settings file: %v", err)
	}
	entry := settings.Entry{Path: path, Target: settings.Target{Space: space, Prefix: prefix}}
	if err := file.Save(set.Put(entry)); err != nil {
		t.Fatalf("save the settings file: %v", err)
	}
	return readSettings(t, env)
}

// settingsFile is the scoped settings file of env's processes, beside the
// credentials file of the same configuration directory.
func settingsFile(t *testing.T, env []string) settings.File {
	t.Helper()
	file, err := settings.Locate(func(string) string { return credentialsDir(env) }, runtime.GOOS)
	if err != nil {
		t.Fatalf("locate the settings file: %v", err)
	}
	return file
}

// settingsPath is the settings file of env's processes.
func settingsPath(env []string) string { return filepath.Join(credentialsDir(env), settings.FileName) }

// plantSettings writes the settings file of env's processes exactly as
// given, for the rows that need bytes this build cannot read.
func plantSettings(t *testing.T, env []string, body string) {
	t.Helper()
	path := settingsPath(env)
	writeCLIFile(t, path, body)
	mustDo(t, os.Chmod(path, 0o600))
}

// readSettings is the settings file of env's processes as it stands.
func readSettings(t *testing.T, env []string) string {
	t.Helper()
	data, err := os.ReadFile(settingsPath(env))
	if err != nil {
		t.Fatalf("read the settings file: %v", err)
	}
	return string(data)
}

// assertRefusal is the black-box refusal contract of a startup: a nonzero
// exit, nothing on stdout, and every cause and fix the row names in stderr.
func assertRefusal(t *testing.T, args []string, code int, stdout, stderr string, want ...string) {
	t.Helper()
	if code != 1 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("%v = exit %d, stdout %q; want a startup refusal", args, code, stdout)
	}
	for _, want := range want {
		if !strings.Contains(stderr, want) {
			t.Fatalf("%v stderr = %q, want it to contain %q", args, stderr, want)
		}
	}
}

// assertReachedSpace proves which space a process really used: the last
// token the site minted was for it.
func assertReachedSpace(t *testing.T, site *sitetest.Site, space string) {
	t.Helper()
	mints := site.Mints()
	if len(mints) == 0 {
		t.Fatal("the process reached the site without minting a token")
	}
	if last := mints[len(mints)-1].Space; last != space {
		t.Fatalf("the last minted token is for space %q, want %q", last, space)
	}
}

// reach is how far every process of a scenario has got: the spaces the site
// minted for and the requests the gateway served. A refusal must not change
// it.
func reach(g *gatewaytest.Gateway, site *sitetest.Site) string {
	spaces := make([]string, 0, len(site.Mints()))
	for _, m := range site.Mints() {
		spaces = append(spaces, m.Space)
	}
	return fmt.Sprintf("mints=%s requests=%d", strings.Join(spaces, ","), g.Requests())
}

// TestScenarioRememberedSpaceReachesTheHostedStore proves, from the outside,
// the property the feature exists for: a directory whose settings file
// remembers a hosted space reaches that space with a bare pull, commit,
// status and log, with no space and no storage flag, while a sibling
// directory without an entry keeps the login's default space. Reading the
// file writes nothing.
func TestScenarioRememberedSpaceReachesTheHostedStore(t *testing.T) {
	t.Parallel()
	g, site, env, root := rememberedEnv(t)
	notes := filepath.Join(root, "notes")
	before := remember(t, env, notes, secondSpace, hostedPrefix)

	runCLIOK(t, "real", env, nil, "pull", notes)
	assertReachedSpace(t, site, secondSpace)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "in the remembered space\n")
	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "remembered")
	if g.Stored(secondSpace) == 0 || g.Stored(hostedSpace) != 0 {
		t.Fatalf("stored bytes: remembered space %d, default space %d; want the commit in the remembered space",
			g.Stored(secondSpace), g.Stored(hostedSpace))
	}
	code, stdout, stderr := runCLI(t, "real", env, "status", notes)
	if code != 0 || !strings.Contains(stdout, "space "+secondSpace) {
		t.Fatalf("status = exit %d, stdout %q, stderr %q; want the remembered space named", code, stdout, stderr)
	}
	// log prints its own report, so only its exit proves the read worked.
	if code, _, stderr = runCLI(t, "real", env, "log", notes); code != 0 {
		t.Fatalf("log = exit %d, stderr %q; want 0 in the remembered space", code, stderr)
	}

	// A sibling directory has no entry, so it keeps the login's default
	// space and never reaches the remembered one.
	other := filepath.Join(root, "other")
	runCLIOK(t, "real", env, nil, "pull", other)
	assertReachedSpace(t, site, hostedSpace)
	if g.Stored(hostedSpace) != 0 {
		t.Fatal("a pull wrote into the default space")
	}
	if after := readSettings(t, env); after != before {
		t.Fatalf("settings file after five commands = %q, want it unchanged (%q)", after, before)
	}
}

// TestScenarioExplicitChoiceBeatsTheRememberedSpace proves the precedence
// the configuration documents: a flag or a variable naming a space wins over
// the record, and neither is written back.
func TestScenarioExplicitChoiceBeatsTheRememberedSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := rememberedEnv(t)
	notes := filepath.Join(root, "notes")
	before := remember(t, env, notes, secondSpace, hostedPrefix)
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }
	for _, row := range []struct {
		name string
		env  []string
		args []string
	}{
		{name: "--space", args: []string{"--space", hostedSpace}},
		{name: "--bucket", args: []string{"--bucket", hostedSpace}},
		{name: "SLIVINGDOC_SPACE", env: with("SLIVINGDOC_SPACE=" + hostedSpace)},
		{name: "SLIVINGDOC_BUCKET", env: with("SLIVINGDOC_BUCKET=" + hostedSpace)},
	} {
		t.Run(row.name, func(t *testing.T) {
			rowEnv := row.env
			if rowEnv == nil {
				rowEnv = env
			}
			runCLIOK(t, "real", rowEnv, nil, append(append([]string{"pull"}, row.args...), notes)...)
			assertReachedSpace(t, site, hostedSpace)
		})
	}
	if g.Stored(secondSpace) != 0 {
		t.Fatal("a pull wrote into the remembered space")
	}
	if after := readSettings(t, env); after != before {
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
	_, site, env, root := rememberedEnv(t)
	notes := filepath.Join(root, "notes")
	remember(t, env, notes, secondSpace, hostedPrefix)
	home := t.TempDir()
	writeCLIFile(t, filepath.Join(home, ".aws", "credentials"), "[default]\naws_access_key_id = key\n")
	awsEnv := append(append([]string(nil), env...),
		"HOME="+home, "AWS_REGION=eu-north-1",
		"AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret")

	runCLIOK(t, "real", awsEnv, nil, "pull", notes)
	assertReachedSpace(t, site, secondSpace)
	// The same machine, told which store it means, takes the remembered
	// space instead of asking for a space.
	runCLIOK(t, "real", awsEnv, nil, "pull", "--storage", "hosted", notes)
	assertReachedSpace(t, site, secondSpace)
}

// TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace proves the two store
// choices that consult no record: SLIVINGDOC_TOKEN takes the space the API
// says the token reaches, and --storage s3 asks for a bucket. Both leave the
// entry untouched.
func TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := rememberedEnv(t)
	notes := filepath.Join(root, "notes")
	before := remember(t, env, notes, secondSpace, hostedPrefix)

	// The token reaches the first space only. The record names the other
	// one, so a process that consulted it would refuse with a mismatch.
	tokenEnv := append(append([]string(nil), env...),
		"SLIVINGDOC_TOKEN="+hostedToken, "SLIVINGDOC_ENDPOINT="+g.URL())
	runCLIOK(t, "real", tokenEnv, nil, "pull", notes)
	if mints := site.Mints(); len(mints) != 0 {
		t.Fatalf("mints = %+v, want none from a token process", mints)
	}
	if used := g.Used(); used[hostedToken] == 0 {
		t.Fatalf("space requests per token = %v, want the token's own space", used)
	}

	// S3 mode asks for its bucket: a space is not one, so a bucketless S3
	// process is refused for that reason and not by the hosted rules.
	args := []string{"pull", "--storage", "s3", notes}
	code, stdout, stderr := runCLI(t, "fake", env, args...)
	assertRefusal(t, args, code, stdout, stderr, "bucket is required")
	runCLIOK(t, "fake", env, nil, "pull", "--storage", "s3", "--bucket", "a-bucket", notes)
	if after := readSettings(t, env); after != before {
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
				remember(t, other, dir, secondSpace, hostedPrefix)
				// The machine configures S3 on purpose, so an empty answer
				// would have pulled a bucket and hidden the record.
				return append(other,
					"AWS_ENDPOINT_URL_S3="+closedS3, "AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret")
			},
			want: []string{`remembers hosted space "second-space"`, "run 'slivingdoc login'", "pass --space"},
		},
		{
			name: "the only stored login is for another endpoint",
			plant: func(t *testing.T, env []string, dir string) []string {
				remember(t, env, dir, secondSpace, hostedPrefix)
				return append(append([]string(nil), env...), "SLIVINGDOC_ENDPOINT="+closedS3)
			},
			want: []string{`remembers hosted space "second-space"`, "at " + closedS3, "the stored login is for"},
		},
		{
			name:   "the stored login expired",
			expire: true,
			plant: func(t *testing.T, env []string, dir string) []string {
				remember(t, env, dir, secondSpace, hostedPrefix)
				return env
			},
			want: []string{"stored login expired", "run 'slivingdoc login' again"},
		},
		{
			name: "the account holds no such space",
			plant: func(t *testing.T, env []string, dir string) []string {
				remember(t, env, dir, "not-my-space", hostedPrefix)
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
				remember(t, env, dir, "Second_Space", hostedPrefix)
				return env
			},
			want: []string{"invalid space name", `"Second_Space"`},
		},
		{
			name: "the variable names another notebook of the space",
			plant: func(t *testing.T, env []string, dir string) []string {
				remember(t, env, dir, secondSpace, "other-prefix")
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
				remember(t, env, dir, secondSpace, "other-prefix")
				return env
			},
			want: []string{
				`remembers hosted space "second-space" with prefix "other-prefix"`,
				`"` + hostedPrefix + `" names another notebook`, "--prefix other-prefix",
			},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, root := rememberedEnv(t)
			if row.expire {
				expireStoredLogin(t, g, site, env)
			}
			notes := filepath.Join(root, "notes")
			rowEnv := row.plant(t, env, notes)
			before := reach(g, site)
			args := []string{"pull", notes}
			code, stdout, stderr := runCLI(t, "real", rowEnv, args...)
			assertRefusal(t, args, code, stdout, stderr, row.want...)
			if after := reach(g, site); after != before {
				t.Fatalf("the refusal reached a store: %q then %q", before, after)
			}
		})
	}
}

// expireStoredLogin replaces the stored login of env with one whose key has
// expired, so the next process refuses it before any store is reached.
func expireStoredLogin(t *testing.T, g *gatewaytest.Gateway, site *sitetest.Site, env []string) {
	t.Helper()
	past := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	approve(site, g, loginKey, "write", past)
	runLogin(t, env, site, loggedIn(g, "read and write", `until 2001-01-01 00:00 UTC; default space "team-notes"`), "--no-browser")
	site.SetSpaces(loginKey, teamNotes, secondTeamNotes)
}

// TestScenarioSeveralStoredLoginsRefuseTheRememberedSpace proves the
// ambiguity refusal keeps its meaning beside a record: two stored logins
// need --endpoint, whatever the directory remembers.
func TestScenarioSeveralStoredLoginsRefuseTheRememberedSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := rememberedEnv(t)
	g2 := gatewaytest.Start(t)
	g2.AddSpace(secondSpace, 1<<20)
	grantMints(site, g2)
	approve(site, g2, secondKey, "read", loginExpiry)
	runLogin(t, env, site, loggedIn(g2, "read only", withDefault), "--no-browser")
	notes := filepath.Join(root, "notes")
	remember(t, env, notes, secondSpace, hostedPrefix)

	args := []string{"pull", notes}
	code, stdout, stderr := runCLI(t, "real", env, args...)
	assertRefusal(t, args, code, stdout, stderr, "several stored logins match", "pass --endpoint")
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
			g, site, env, root := rememberedEnv(t)
			plantSettings(t, env, row.body)
			before := reach(g, site)
			args := []string{"pull", filepath.Join(root, "notes")}
			code, stdout, stderr := runCLI(t, "real", env, args...)
			assertRefusal(t, args, code, stdout, stderr,
				row.want, "workspaces.json", "remove it to choose the space with --space")
			if after := reach(g, site); after != before {
				t.Fatalf("the refusal reached a store: %q then %q", before, after)
			}
			if after := readSettings(t, env); after != row.body {
				t.Fatalf("settings file = %q, want it untouched", after)
			}
		})
	}
}

// TestScenarioServeIgnoresTheRememberedSpace proves the server neither
// reads nor writes the record: a serve process in a directory with an entry
// behaves exactly as it does without one, so a settings file this build
// cannot read never stops a running server and the entry is byte-identical
// after. The space the server uses is the login's default one.
func TestScenarioServeIgnoresTheRememberedSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := rememberedEnv(t)
	notes := filepath.Join(root, "notes")
	remember(t, env, notes, secondSpace, hostedPrefix)

	// A settings file this build cannot read stops a pull, and never a
	// server: a running server's store is fixed before any notebook
	// directory is known (architecture/config.md).
	plantSettings(t, env, `{"version":0,"entries":[]}`)
	before := readSettings(t, env)
	h := spawnHelper(t, "real", env, "serve")
	cs := h.connectClient(t)
	served := filepath.Join(root, "served")
	assertProcessCallOK(t, cs, toolPull, served, "")
	writeCLIFile(t, filepath.Join(served, "a.md"), "published by the server\n")
	assertProcessCallOK(t, cs, toolCommit, served, "served without the record")
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("serve exit = %d; stderr: %s", code, h.stderrText(t))
	}
	for _, m := range site.Mints() {
		if m.Space != hostedSpace {
			t.Fatalf("the server minted for space %q, want the login's default space", m.Space)
		}
	}
	if g.Stored(hostedSpace) == 0 {
		t.Fatal("the server published nothing into the login's default space")
	}
	if after := readSettings(t, env); after != before {
		t.Fatalf("settings file after a serve = %q, want it unchanged (%q)", after, before)
	}
}
