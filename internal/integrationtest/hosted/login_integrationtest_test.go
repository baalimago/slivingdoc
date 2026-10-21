package hosted

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// TestScenarioLoginThenPullAndCommit proves the login workflow end to end:
// a browser approval whose polls see authorization_pending and slow_down
// stores the account key with its only space as the default, the failing
// browser is not fatal, and later pull, commit and serve processes use
// hosted storage with neither SLIVINGDOC_TOKEN nor --space, through tokens
// the site mints from the key, which itself never reaches the storage
// endpoint.
func TestScenarioLoginThenPullAndCommit(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry, "authorization_pending", "slow_down", "authorization_pending")
	code, stdout, stderr := RunCLI(t, "real", env, "login")
	want := LoggedIn(g, "read and write", WithDefault) + "\n"
	if code != 0 || stdout != want {
		t.Fatalf("login = exit %d, stdout %q; want %q; stderr: %s", code, stdout, want, stderr)
	}
	if !strings.Contains(stderr, "Could not open a browser") || !strings.Contains(stderr, "open the page yourself") ||
		!strings.Contains(stderr, "    team-notes (read and write), owned by ada@example.test\n") {
		t.Fatalf("login stderr = %q, want the browser fallback and the space list", stderr)
	}
	if starts := site.Starts(); len(starts) != 1 || starts[0].Access != "write" || starts[0].Client == "" {
		t.Fatalf("start requests = %+v, want one with the access and a client label and no other field", starts)
	}
	if polls := site.Polls(); polls != 4 {
		t.Fatalf("token polls = %d, want 4 (three waits, then the key)", polls)
	}

	notes := filepath.Join(root, "notes")
	RunCLIExact(t, "real", env,
		"OK  generation 0  "+notes+"\n0 files changed, 0 insertions(+), 0 deletions(-)\n",
		"pull", notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "logged in\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "through a login")
	if g.Stored(HostedSpace) == 0 {
		t.Fatal("the space holds no pack bytes after a commit through a login")
	}
	if runtime.GOOS != "windows" {
		AssertPerm(t, CredentialsDir(env), 0o700)
		AssertPerm(t, CredentialsPath(env), 0o600)
	}

	h := SpawnHelper(t, "real", append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+filepath.Join(root, "served")), "serve")
	cs := h.ConnectClient(t)
	served := filepath.Join(root, "served")
	AssertProcessCallOK(t, cs, ToolPull, served, "")
	WriteCLIFile(t, filepath.Join(served, "b.md"), "over MCP\n")
	AssertProcessCallOK(t, cs, ToolCommit, served, "served through a login")
	h.CloseClient(t, cs)
	if mints := site.Mints(); len(mints) != 3 || mints[0].Space != HostedSpace || mints[0].Key != LoginKey {
		t.Fatalf("mints = %+v, want one per process for the default space", mints)
	}
	AssertKeyStayedAtTheSite(t, g, site, env)
}

// TestScenarioAccountLoginLifecycle proves the account login from end to
// end: a login that reaches two spaces stores no default, so hosted
// processes are refused with the command that sets one; 'slivingdoc
// space' lists the spaces and 'slivingdoc space <name>' sets the default;
// serve then mints a token, mints another once 80 % of its lifetime has
// passed and once more after the gateway refuses the current one; and
// logout revokes the key with every token minted from it.
func TestScenarioAccountLoginLifecycle(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	const second = "second-space"
	g.AddSpace(second, 1<<20)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	site.SetSpaces(LoginKey, TeamNotes, sitetest.Space{Name: second, Owner: "bob@example.test", Access: "read"})
	RunLogin(t, env, site, LoggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")

	notes := filepath.Join(root, "notes")
	code, stdout, stderr := RunCLI(t, "real", env, "pull", notes)
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "has no default space") ||
		!strings.Contains(stderr, "'slivingdoc space <name>'") {
		t.Fatalf("pull without a default space = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}

	listing := "HOSTED  " + g.URL() + "  " + LoginAccount + "\n" +
		"  second-space  read  bob@example.test\n" +
		"  team-notes  write  " + LoginAccount + "\n"
	RunCLIExact(t, "real", env, listing, "space")
	RunCLIExact(t, "real", env, "The default space is now team-notes (write, ada@example.test).\n", "space", HostedSpace)
	marked := "HOSTED  " + g.URL() + "  " + LoginAccount + "\n" +
		"  second-space  read  bob@example.test\n" +
		"* team-notes  write  " + LoginAccount + "\n"
	RunCLIExact(t, "real", env, marked, "space")
	// The space of another owner is reachable too, read only.
	RunCLIOK(t, "real", env, nil, "pull", "--space", second, filepath.Join(root, "second"))

	// A short lifetime makes renewal due within a tenth of a second; the
	// internal/app unit tests pin the 80 % point over a fake clock.
	const lifetime = 100 * time.Millisecond
	site.SetMintLifetime(lifetime)
	served := filepath.Join(root, "served")
	h := SpawnHelper(t, "real", append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+served), "serve")
	cs := h.ConnectClient(t)
	AssertProcessCallOK(t, cs, ToolPull, served, "")
	before := len(site.Mints())
	time.Sleep(lifetime)
	WriteCLIFile(t, filepath.Join(served, "a.md"), "after a renewal\n")
	AssertProcessCallOK(t, cs, ToolCommit, served, "after a renewal")
	if after := len(site.Mints()); after <= before {
		t.Fatalf("mints = %d before and %d after 80 %% of the lifetime, want a renewal", before, after)
	}

	mints := site.Mints()
	refused := mints[len(mints)-1].Token
	g.Revoke(refused)
	WriteCLIFile(t, filepath.Join(served, "b.md"), "after a refusal\n")
	AssertProcessCallOK(t, cs, ToolCommit, served, "after a refusal")
	if after := site.Mints(); len(after) <= len(mints) {
		t.Fatalf("mints = %d after the gateway refused the token, want another", len(after))
	}
	h.CloseClient(t, cs)
	AssertKeyStayedAtTheSite(t, g, site, env)

	code, stdout, stderr = RunCLI(t, "real", env, "logout")
	if code != 0 || stdout != "Logged out of ada@example.test at "+g.URL()+"; the login key and its tokens were revoked\n" {
		t.Fatalf("logout = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	revoked := site.Revoked()
	if len(revoked) == 0 || revoked[0] != LoginKey {
		t.Fatalf("revoked = %v, want the key first", revoked)
	}
	for _, m := range site.Mints() {
		if m.Token != refused && !slices.Contains(revoked, m.Token) {
			t.Fatalf("revoked = %v; a minted token of the key survived the logout", revoked)
		}
	}
	if data, err := os.ReadFile(CredentialsPath(env)); err != nil || strings.Contains(string(data), LoginKey) ||
		strings.Contains(string(data), HostedSpace) {
		t.Fatalf("credentials file after logout = %q (%v), want neither the key nor its default space", data, err)
	}
}

// TestScenarioLoginPollOutcomes proves a denied and an expired approval end
// the login nonzero with the reason, store nothing, and leave hosted
// storage refused for want of a login.
func TestScenarioLoginPollOutcomes(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		final string
		want  string
	}{
		{"access_denied", "denied in the browser"},
		{"expired_token", "expired before it was approved"},
		{"invalid_request", "HTTP 400 invalid_request"},
	} {
		t.Run(row.final, func(t *testing.T) {
			t.Parallel()
			g, site, env, root := LoginEnv(t)
			site.Next(sitetest.Script{Pending: []string{"authorization_pending"}, Final: row.final})
			code, stdout, stderr := RunCLI(t, "real", env, "login", "--no-browser")
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, row.want) {
				t.Fatalf("login = exit %d, stdout %q, stderr %q; want exit 1 naming %q", code, stdout, stderr, row.want)
			}
			code, _, stderr = RunCLI(t, "real", env, "pull", "--storage", "hosted", "--space", HostedSpace, filepath.Join(root, "notes"))
			if code != 1 || !strings.Contains(stderr, "needs SLIVINGDOC_TOKEN or a stored login") {
				t.Fatalf("pull after a failed login = exit %d, stderr %q; want the missing-login refusal", code, stderr)
			}
			if g.Requests() != 0 {
				t.Fatal("a process without a login reached the storage API")
			}
			AssertNothingStored(t, env)
		})
	}
}

// TestScenarioStorageSelection proves --storage and its automatic choice
// from the outside: SLIVINGDOC_TOKEN beats a stored login and S3 settings.
// A login beside a named space plus S3 settings is refused as ambiguous
// until --storage decides, while the bucket spelling of that setting beside
// S3 settings keeps S3; s3 never sends a token or a login to the hosted
// API; hosted without a login is refused; a login is only used for its own
// endpoint.
func TestScenarioStorageSelection(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	notes := filepath.Join(root, "notes")
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }
	s3 := []string{"AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret", "AWS_MAX_ATTEMPTS=1", "AWS_REGION=us-east-1"}

	for _, row := range []struct {
		name string
		env  []string
		args []string
		want []string // stderr
		sent bool     // the gateway saw a request
	}{
		{
			name: "the token beats the login",
			env:  with("SLIVINGDOC_TOKEN=sld_unknown_token", "SLIVINGDOC_ENDPOINT="+g.URL()),
			want: []string{"hosted storage refused the token", "check SLIVINGDOC_TOKEN"},
			sent: true,
		},
		{
			name: "a login and AWS settings are ambiguous for an explicit space",
			env:  with(append([]string{"SLIVINGDOC_SPACE=" + HostedSpace}, s3...)...),
			want: []string{"a stored login and S3 settings (AWS_ACCESS_KEY_ID", `for "team-notes"`, "--storage hosted or --storage s3"},
		},
		{
			name: "--space and AWS settings are ambiguous",
			env:  with(s3...),
			args: []string{"--space", HostedSpace},
			want: []string{"a stored login and S3 settings (AWS_ACCESS_KEY_ID", "--storage hosted or --storage s3"},
		},
		{
			name: "SLIVINGDOC_BUCKET beside AWS settings keeps S3 despite a login",
			env:  with(append([]string{"SLIVINGDOC_BUCKET=" + HostedSpace, "AWS_ENDPOINT_URL_S3=" + ClosedS3}, s3...)...),
			want: []string{"backend=s3", "token=none", "S3 compatibility probe failed"},
		},
		{
			name: "--bucket beside AWS settings keeps S3 despite a login",
			env:  with(append([]string{"AWS_ENDPOINT_URL_S3=" + ClosedS3}, s3...)...),
			args: []string{"--bucket", HostedSpace},
			want: []string{"backend=s3", "token=none", "S3 compatibility probe failed"},
		},
		{
			name: "s3 ignores the token and the login",
			env:  with(append([]string{"SLIVINGDOC_TOKEN=" + HostedToken, "AWS_ENDPOINT_URL_S3=" + ClosedS3, "SLIVINGDOC_BUCKET=" + HostedSpace}, s3...)...),
			args: []string{"--storage", "s3"},
			want: []string{"S3 compatibility probe failed"},
		},
		{
			name: "SLIVINGDOC_STORAGE=s3 ignores the token",
			env:  with(append([]string{"SLIVINGDOC_STORAGE=s3", "SLIVINGDOC_TOKEN=" + HostedToken, "SLIVINGDOC_ENDPOINT=" + g.URL(), "AWS_ENDPOINT_URL_S3=" + ClosedS3}, s3...)...),
			args: []string{"--bucket", "a-bucket"},
			want: []string{"S3 compatibility probe failed"},
		},
		{
			name: "an endpoint without a login keeps S3",
			env:  with(append([]string{"SLIVINGDOC_BUCKET=" + HostedSpace}, s3...)...),
			args: []string{"--endpoint", ClosedS3},
			want: []string{"S3 compatibility probe failed"},
		},
		{
			name: "an endpoint without a login never borrows the default space as a bucket",
			env:  with(s3...),
			args: []string{"--endpoint", ClosedS3},
			want: []string{"bucket is required"},
		},
		{
			name: "hosted at an endpoint without a login",
			args: []string{"--storage", "hosted", "--endpoint", ClosedS3},
			want: []string{"needs SLIVINGDOC_TOKEN or a stored login for " + ClosedS3, "the stored login is for " + g.URL()},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			rowEnv := row.env
			if rowEnv == nil {
				rowEnv = env
			}
			before := g.Requests()
			mints := len(site.Mints())
			code, stdout, stderr := RunCLI(t, "real", rowEnv, append([]string{"pull"}, append(row.args, notes)...)...)
			if code != 1 || strings.TrimSpace(stdout) != "" {
				t.Fatalf("pull = exit %d, stdout %q; want a startup refusal; stderr %s", code, stdout, stderr)
			}
			for _, want := range row.want {
				if !strings.Contains(stderr, want) {
					t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
				}
			}
			if strings.Contains(stderr, HostedToken) || strings.Contains(stderr, LoginKey) {
				t.Fatal("a refusal echoed a token or the key")
			}
			if sent := g.Requests() != before; sent != row.sent {
				t.Fatalf("the gateway saw a request = %v, want %v", sent, row.sent)
			}
			if len(site.Mints()) != mints {
				t.Fatal("a refused process minted a token")
			}
		})
	}

	// With a token, auto chooses hosted even when AWS credentials and an S3
	// endpoint are configured. --endpoint then names the hosted API, and the
	// startup record makes clear that S3 was not selected.
	tokenEnv := with(append([]string{
		"SLIVINGDOC_TOKEN=" + HostedToken,
		"SLIVINGDOC_BUCKET=" + HostedSpace,
		"AWS_ENDPOINT_URL_S3=" + ClosedS3,
	}, s3...)...)
	code, stdout, stderr := RunCLI(t, "real", tokenEnv, "pull", "--endpoint", g.URL(), notes)
	if code != 0 || !strings.HasPrefix(stdout, "OK  generation ") {
		t.Fatalf("pull with token and S3 settings = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	for _, want := range []string{"storage selected", "backend=hosted", "endpoint=" + g.URL(), "token=env", "s3=ignored"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("pull stderr = %q, want it to contain %q", stderr, want)
		}
	}

	// Choosing hosted resolves the ambiguity with the same AWS settings,
	// even when the directory record names S3.
	notesWithS3Record := filepath.Join(root, "notes-with-s3-record")
	file := SettingsFile(t, env)
	set, err := file.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if err := file.Save(set.Put(settings.Entry{Path: notesWithS3Record, Target: settings.Target{
		Kind: settings.KindS3, Name: "remembered-bucket", Prefix: "slivingdoc",
	}})); err != nil {
		t.Fatalf("write S3 record: %v", err)
	}
	RunCLIOK(t, "real", with(append([]string{"SLIVINGDOC_BUCKET=" + HostedSpace}, s3...)...), nil, "pull", "--storage", "hosted", notesWithS3Record)
	AssertReachedSpace(t, site, HostedSpace)
	// The stored default space needs no choice, even with AWS settings:
	// S3 has no bucket to use.
	code, stdout, stderr = RunCLI(t, "real", with(s3...), "pull", notes)
	if code != 0 || !strings.HasPrefix(stdout, "OK  generation ") {
		t.Fatalf("pull with the default space and AWS settings = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	// The startup log says which store and credential won.
	for _, want := range []string{"storage selected", "backend=hosted", "endpoint=" + g.URL(), "space=team-notes", "token=login"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("pull stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

// TestScenarioTokenServesBesideAWSSettings proves the README's CI setup
// on an ordinary machine: SLIVINGDOC_TOKEN with serve --bucket goes hosted
// even though a region, AWS credentials and the shared AWS files are
// configured, since none of them names a host the token could reach.
func TestScenarioTokenServesBesideAWSSettings(t *testing.T) {
	t.Parallel()
	g := gatewaytest.Start(t)
	g.AddSpace(HostedSpace, 1<<20)
	g.Grant(HostedToken, HostedSpace, false)
	env, root := CliRoots(t)
	home := t.TempDir()
	WriteCLIFile(t, filepath.Join(home, ".aws", "config"), "[default]\nregion = eu-north-1\n")
	env = append(
		env,
		"HOME="+home, "AWS_REGION=eu-north-1", "AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret",
		"SLIVINGDOC_TOKEN="+HostedToken, "SLIVINGDOC_ENDPOINT="+g.URL(), "SLIVINGDOC_BUCKET=",
	)
	h := SpawnHelper(t, "real", env, "serve", "--bucket", HostedSpace)
	cs := h.ConnectClient(t)
	notes := root
	AssertProcessCallOK(t, cs, ToolPull, notes, "")
	WriteCLIFile(t, filepath.Join(notes, "ci.md"), "from CI\n")
	AssertProcessCallOK(t, cs, ToolCommit, notes, "beside AWS settings")
	h.CloseClient(t, cs)
	if used := g.Used(); used[HostedToken] == 0 || g.Stored(HostedSpace) == 0 {
		t.Fatalf("space requests per token = %v, stored %d; want the commit through the token", used, g.Stored(HostedSpace))
	}
	if stderr := h.StderrText(t); !strings.Contains(stderr, "backend=hosted") || !strings.Contains(stderr, "token=env") {
		t.Fatalf("serve stderr = %s, want the hosted startup record", stderr)
	}
}

// TestScenarioLoginSpaceRules proves which space a login process uses:
// the stored default, else --space or SLIVINGDOC_SPACE when given; that a
// space the login does not reach is refused at the site before any
// storage request; and that SLIVINGDOC_TOKEN beside a login is used alone,
// with its own space.
func TestScenarioLoginSpaceRules(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	const second = "second-space"
	g.AddSpace(second, 1<<20)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	site.SetSpaces(LoginKey, TeamNotes, sitetest.Space{Name: second, Owner: LoginAccount, Access: "write"})
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }

	RunCLIOK(t, "real", with("SLIVINGDOC_SPACE="+second), nil, "pull", filepath.Join(root, "second"))
	if mints := site.Mints(); len(mints) != 1 || mints[0].Space != second {
		t.Fatalf("mints = %+v, want one for the space SLIVINGDOC_SPACE named", mints)
	}

	before := g.Requests()
	code, stdout, stderr := RunCLI(t, "real", env, "pull", "--space", "other-space", filepath.Join(root, "other"))
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, `could not mint a token for space "other-space"`) ||
		!strings.Contains(stderr, `the login reaches no space "other-space"`) ||
		!strings.Contains(stderr, "check --space, run 'slivingdoc space'") {
		t.Fatalf("pull for a space the login does not reach = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if g.Requests() != before {
		t.Fatal("a refused mint still reached the storage API")
	}

	// SLIVINGDOC_TOKEN for another space beside the login: the token alone
	// is used, with its own space, and nothing is minted.
	const otherSpace, otherToken = "other-notes", "sld_4444444444444444_b3RoZXItc3BhY2UtdG9rZW4tYmVzaWRlLXRoZS1sb2dpbi14eA"
	g.AddSpace(otherSpace, 1<<20)
	g.Grant(otherToken, otherSpace, false)
	mints := len(site.Mints())
	RunCLIOK(t, "real", with("SLIVINGDOC_TOKEN="+otherToken, "SLIVINGDOC_ENDPOINT="+g.URL()), nil, "pull", filepath.Join(root, "token"))
	if used := g.Used(); used[otherToken] == 0 || len(site.Mints()) != mints {
		t.Fatalf("space requests per token = %v after %d mints, want the variable's token alone", used, len(site.Mints())-mints)
	}
}

// TestScenarioExpiredLoginIsRefused proves a stored login past its expiry
// refuses startup with the fix, before any request reaches the site or
// the storage API.
func TestScenarioExpiredLoginIsRefused(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	RunLogin(t, env, site, LoggedIn(g, "read and write", `until 2001-01-01 00:00 UTC; default space "team-notes"`), "--no-browser")
	code, stdout, stderr := RunCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
	if code != 1 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("pull with an expired login = exit %d, stdout %q; want a startup refusal", code, stdout)
	}
	for _, want := range []string{"stored login expired", "run 'slivingdoc login' again"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
	if g.Requests() != 0 || len(site.Mints()) != 0 {
		t.Fatal("an expired login reached the site or the storage API")
	}
}

// TestScenarioLogout proves logout keeps a login whose revocation the
// site refused with anything but invalid_token, revokes the key and the
// tokens minted from it at the site and removes the login, so hosted
// storage is refused afterwards, and that logging out again is a refusal.
func TestScenarioLogout(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")

	// A 401 that does not say the key is invalid is not proof that it is
	// gone, so the login stays stored and still works.
	site.RefuseRevoke(http.StatusUnauthorized, "unauthorized")
	code, stdout, stderr := RunCLI(t, "real", env, "logout")
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "could not be revoked and stays stored") {
		t.Fatalf("logout refused with another 401 = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	RunCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "kept"))

	code, stdout, stderr = RunCLI(t, "real", env, "logout")
	if code != 0 || stdout != "Logged out of ada@example.test at "+g.URL()+"; the login key and its tokens were revoked\n" {
		t.Fatalf("logout = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 2 || got[0] != LoginKey || got[1] != site.Mints()[0].Token {
		t.Fatalf("revoked = %v, want the key and the token minted from it", got)
	}
	code, _, stderr = RunCLI(t, "real", env, "pull", "--storage", "hosted", "--space", HostedSpace, filepath.Join(root, "notes"))
	if code != 1 || !strings.Contains(stderr, "needs SLIVINGDOC_TOKEN or a stored login") {
		t.Fatalf("pull after logout = exit %d, stderr %q; want the missing-login refusal", code, stderr)
	}
	code, _, stderr = RunCLI(t, "real", env, "logout")
	if code != 1 || !strings.Contains(stderr, "not logged in") {
		t.Fatalf("second logout = exit %d, stderr %q; want a refusal", code, stderr)
	}
}

// TestScenarioLoginGuardsWhatIsStored proves, from the outside, what a
// login refuses to store without a person at a terminal: an approval by
// another account (revoked unless --force, and the replaced key of another
// account is never revoked), and a login for an endpoint another site's
// login holds (revoked even with --force).
func TestScenarioLoginGuardsWhatIsStored(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	notes := filepath.Join(root, "notes")

	eve := sitetest.Script{Issue: sitetest.Issue{Key: SecondKey, Access: "write", Endpoint: g.URL(), ExpiresAt: LoginExpiry, Account: "eve@example.test"}}
	site.SetSpaces(SecondKey, TeamNotes)
	site.Next(eve)
	code, stdout, stderr := RunCLI(t, "real", env, "login", "--no-browser")
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, "Approved by eve@example.test") ||
		!strings.Contains(stderr, "approved by ada@example.test, this one by eve@example.test") ||
		!strings.Contains(stderr, "--force") {
		t.Fatalf("login approved by another account = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != SecondKey {
		t.Fatalf("revoked = %v, want only the refused key", got)
	}
	// The stored login is untouched: a pull still mints from ada's key.
	RunCLIOK(t, "real", env, nil, "pull", notes)
	if m := site.Mints(); len(m) != 1 || m[0].Key != LoginKey {
		t.Fatalf("mints = %+v, want one from the stored key", m)
	}

	site.SetSpaces(SecondKey, TeamNotes)
	site.Next(eve)
	code, stdout, stderr = RunCLI(t, "real", env, "login", "--no-browser", "--force")
	if code != 0 || !strings.HasPrefix(stdout, "Logged in as eve@example.test") ||
		!strings.Contains(stderr, "was approved by ada@example.test, not eve@example.test, so it was not revoked") {
		t.Fatalf("login --force = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 {
		t.Fatalf("revoked = %v; another account's replaced key must stay valid", got)
	}

	// A second site's approval for the same endpoint never replaces the
	// first site's login, whatever the flags.
	other := sitetest.Start(t)
	other.SetSpaces(LoginKey, TeamNotes)
	other.Next(sitetest.Script{Issue: sitetest.Issue{Key: LoginKey, Access: "read", Endpoint: g.URL(), ExpiresAt: LoginExpiry, Account: "eve@example.test"}})
	otherEnv := append(append([]string(nil), env...), "SLIVINGDOC_SITE="+other.URL())
	code, _, stderr = RunCLI(t, "real", otherEnv, "login", "--no-browser", "--force")
	if code != 1 || !strings.Contains(stderr, "was issued by "+site.URL()+", not "+other.URL()) ||
		!strings.Contains(stderr, "log out of that login first") {
		t.Fatalf("login through another site = exit %d, stderr %s", code, stderr)
	}
	if got := other.Revoked(); len(got) != 1 || got[0] != LoginKey {
		t.Fatalf("other site revoked = %v, want its own refused key", got)
	}
	if got := site.Revoked(); len(got) != 1 {
		t.Fatalf("first site revoked = %v; the stored login must be left alone", got)
	}
}

// TestScenarioTwoLoginsNeedAnEndpoint proves two stored logins, for two
// storage endpoints, are refused as ambiguous until --endpoint chooses
// one, and that each then uses its own default space and mints for its
// own endpoint only.
func TestScenarioTwoLoginsNeedAnEndpoint(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	g2 := gatewaytest.Start(t)
	g2.AddSpace(HostedSpace, 1<<20)
	GrantMints(site, g, g2)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	Approve(site, g2, SecondKey, "read", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g2, "read only", WithDefault), "--no-browser")

	code, stdout, stderr := RunCLI(t, "real", env, "pull", filepath.Join(root, "either"))
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "several stored logins match") ||
		!strings.Contains(stderr, "pass --endpoint") {
		t.Fatalf("pull with two logins = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	RunCLIOK(t, "real", env, nil, "pull", "--endpoint", g2.URL(), filepath.Join(root, "second"))
	RunCLIExact(t, "real", env, "HOSTED  "+g2.URL()+"  "+LoginAccount+"\n* team-notes  read  "+LoginAccount+"\n", "space", "--endpoint", g2.URL())
	if mints := site.Mints(); len(mints) != 1 || mints[0].Key != SecondKey || mints[0].Endpoint != g2.URL() || mints[0].Access != "read" {
		t.Fatalf("mints = %+v, want one read token of the second login", mints)
	}
	if g.Requests() != 0 {
		t.Fatal("the first endpoint saw a request meant for the second")
	}
}

// TestScenarioCredentialsFileMustBeTheUsers proves a credentials file or
// directory another user could read, write or have planted refuses
// startup before any request reaches the storage API: a symbolic link, a
// FIFO, a file group or other can read, a directory group can write, and
// a file over 1 MiB. The owner check is pinned by the credentials unit
// tests, which need no root to fake another owner.
func TestScenarioCredentialsFileMustBeTheUsers(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX owners, modes, symbolic links and FIFOs are what these rows plant; Windows has none of them")
	}
	for _, row := range []struct {
		name  string
		plant func(t *testing.T, file string)
		want  string
	}{
		{"a symbolic link", func(t *testing.T, file string) {
			moved := filepath.Join(t.TempDir(), "planted.json")
			MustDo(t, os.Rename(file, moved))
			MustDo(t, os.Symlink(moved, file))
		}, "is a symbolic link"},
		{"a FIFO", func(t *testing.T, file string) {
			MustDo(t, os.Remove(file))
			MustDo(t, Mkfifo(file))
		}, "is not a regular file"},
		{"a group-readable file", func(t *testing.T, file string) {
			MustDo(t, os.Chmod(file, 0o640))
		}, "chmod 600"},
		{"a group-writable directory", func(t *testing.T, file string) {
			MustDo(t, os.Chmod(filepath.Dir(file), 0o770))
		}, "chmod go-w"},
		{"a file over 1 MiB", func(t *testing.T, file string) {
			MustDo(t, os.WriteFile(file, []byte(strings.Repeat(" ", 1<<20+1)), 0o600))
		}, "larger than 1048576 bytes"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, root := LoginEnv(t)
			Approve(site, g, LoginKey, "write", LoginExpiry)
			RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
			row.plant(t, CredentialsPath(env))
			code, stdout, stderr := RunCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, row.want) {
				t.Fatalf("pull with %s = exit %d, stdout %q, stderr %s; want a refusal naming %q", row.name, code, stdout, stderr, row.want)
			}
			if g.Requests() != 0 || len(site.Mints()) != 0 {
				t.Fatalf("pull with %s reached the site or the storage API", row.name)
			}
		})
	}
}

// TestScenarioUnsupportedCredentialsFile proves a credentials file of any
// version but the current one refuses startup and login alike with the
// fix, contacts neither the site nor the storage API, and stays as it was;
// once it is removed, login works.
func TestScenarioUnsupportedCredentialsFile(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	other := `{"version":1,"logins":[{"site":"` + site.URL() + `","token":"` + HostedToken + `"}]}`
	WriteCLIFile(t, CredentialsPath(env), other)
	MustDo(t, os.Chmod(CredentialsDir(env), 0o700))
	MustDo(t, os.Chmod(CredentialsPath(env), 0o600))
	const fix = "and run 'slivingdoc login'"
	for _, args := range [][]string{{"pull", filepath.Join(root, "notes")}, {"login", "--no-browser"}} {
		code, stdout, stderr := RunCLI(t, "real", env, args...)
		if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "unsupported credentials file version") ||
			!strings.Contains(stderr, fix) || strings.Contains(stderr, HostedToken) {
			t.Fatalf("%s with a version 1 file = exit %d, stdout %q, stderr %s", args[0], code, stdout, stderr)
		}
	}
	if g.Requests() != 0 || len(site.Starts()) != 0 || len(site.Revoked()) != 0 {
		t.Fatal("a refused command contacted the site or the storage API")
	}
	if data, err := os.ReadFile(CredentialsPath(env)); err != nil || string(data) != other {
		t.Fatalf("credentials file = %q, %v; want it untouched", data, err)
	}
	MustDo(t, os.Remove(CredentialsPath(env)))
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
}

// TestScenarioLoginBrokenAnswers proves every way a login can lose a key
// the site may have issued ends nonzero with the hint to revoke it on the
// Tokens page, and stores nothing: an issuing answer that breaks off, is
// larger than 16 KiB, or is not JSON.
func TestScenarioLoginBrokenAnswers(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name   string
		script sitetest.Script
		want   string
	}{
		{"truncated", sitetest.Script{Truncate: true}, "broke off"},
		{"oversize", sitetest.Script{Body: `{"key":"` + strings.Repeat("a", 17<<10) + `"}`}, "larger than 16384 bytes"},
		{"undecodable", sitetest.Script{Body: "{not json"}, "not the expected JSON"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, _ := LoginEnv(t)
			script := row.script
			script.Issue = sitetest.Issue{Key: LoginKey, Access: "write", Endpoint: g.URL(), Account: LoginAccount}
			site.Next(script)
			code, stdout, stderr := RunCLI(t, "real", env, "login", "--no-browser")
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, row.want) || !strings.Contains(stderr, sitelogin.TokenHint) {
				t.Fatalf("login = exit %d, stdout %q, stderr %s; want exit 1 naming %q with the hint", code, stdout, stderr, row.want)
			}
			if polls := site.Polls(); polls != 1 {
				t.Fatalf("token polls = %d, want 1: a claimed code is never polled again", polls)
			}
			AssertNothingStored(t, env)
		})
	}
}

// TestScenarioLoginInterruptedMidPoll proves an interrupt while a poll is
// in flight ends the login nonzero with the hint, since the site may have
// issued the key it never delivered, and stores nothing.
func TestScenarioLoginInterruptedMidPoll(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to another process is not implemented on Windows")
	}
	g, site, env, _ := LoginEnv(t)
	site.Next(sitetest.Script{Stall: true, Issue: sitetest.Issue{Key: LoginKey, Access: "write", Endpoint: g.URL(), Account: LoginAccount}})
	h := SpawnHelper(t, "real", append(append([]string(nil), env...), HelperSignalsEnv+"=1"), "login", "--no-browser")
	interrupted := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for site.Polls() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		interrupted <- h.Proc.Signal(os.Interrupt)
	}()
	code, stdout, stderr := h.RunStdioProcess(t, nil)
	if err := <-interrupted; err != nil {
		t.Fatalf("interrupt the login: %v", err)
	}
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "stopped while a poll was in flight") ||
		!strings.Contains(stderr, sitelogin.TokenHint) {
		t.Fatalf("interrupted login = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	AssertNothingStored(t, env)
}

// TestScenarioLoginRefusesWhatItDidNotAskFor proves a login stores nothing
// the person did not ask for, and revokes any key it was given: a write
// key for --read-only, a key that does not reach the --space asked for;
// and that an approval page the client cannot trust (a code outside the
// grammar, a filled-in page that is not the code's own) is refused before
// any poll.
func TestScenarioLoginRefusesWhatItDidNotAskFor(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name    string
		setup   func(site *sitetest.Site, g *gatewaytest.Gateway)
		args    []string
		want    string
		revoked bool
	}{
		{
			name:    "a write key for a read-only login",
			setup:   func(site *sitetest.Site, g *gatewaytest.Gateway) { Approve(site, g, LoginKey, "write", LoginExpiry) },
			args:    []string{"--read-only"},
			want:    "issued a read and write login for a read-only login; nothing was stored",
			revoked: true,
		},
		{
			name:    "a key that does not reach the space",
			setup:   func(site *sitetest.Site, g *gatewaytest.Gateway) { Approve(site, g, LoginKey, "write", LoginExpiry) },
			args:    []string{"--space", "other-space"},
			want:    `does not reach space "other-space" (it reaches team-notes); nothing was stored`,
			revoked: true,
		},
		{
			name: "a code with a vowel",
			setup: func(site *sitetest.Site, g *gatewaytest.Gateway) {
				Approve(site, g, LoginKey, "write", LoginExpiry)
				site.SetUserCode("BCDA-GHJK")
			},
			want: "the user code is not two groups of four consonants",
		},
		{
			name: "a filled-in page for another code",
			setup: func(site *sitetest.Site, g *gatewaytest.Gateway) {
				Approve(site, g, LoginKey, "write", LoginExpiry)
				site.SetCompleteURI(site.URL() + "/cli/login#ZZZZ-ZZZZ")
			},
			want: "verificationUriComplete is not " + "SITE/cli/login#BCDF-GHJK",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, _ := LoginEnv(t)
			row.setup(site, g)
			code, stdout, stderr := RunCLI(t, "real", env, append([]string{"login", "--no-browser"}, row.args...)...)
			want := strings.ReplaceAll(row.want, "SITE", site.URL())
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, want) {
				t.Fatalf("login = exit %d, stdout %q, stderr %s; want exit 1 naming %q", code, stdout, stderr, want)
			}
			revoked := site.Revoked()
			if row.revoked && (len(revoked) != 1 || revoked[0] != LoginKey) {
				t.Fatalf("revoked = %v, want the unrequested key", revoked)
			}
			if !row.revoked && (len(revoked) != 0 || site.Polls() != 0) {
				t.Fatalf("revoked = %v after %d polls, want no poll at all", revoked, site.Polls())
			}
			AssertNothingStored(t, env)
		})
	}
}

// TestScenarioLoginThroughTheDefaultSite proves the default site's logins
// are bound to the default storage endpoint: a key it issues for any
// other endpoint is refused and revoked, and one for the default endpoint
// is stored with no endpoint in the result line and no site warning. The
// helper routes the default site's requests to a reference site that
// answers with the default site's addresses.
func TestScenarioLoginThroughTheDefaultSite(t *testing.T) {
	t.Parallel()
	g, site, env, _ := LoginEnv(t)
	site.SetOrigin(sitelogin.DefaultSite)
	env = append(env, "SLIVINGDOC_SITE=", HelperSiteRouteEnv+"="+site.URL())

	Approve(site, g, LoginKey, "write", LoginExpiry)
	code, stdout, stderr := RunCLI(t, "real", env, "login", "--no-browser")
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, sitelogin.DefaultSite+" issued a login for "+g.URL()+", not "+app.DefaultHostedEndpoint) {
		t.Fatalf("default-site login for another endpoint = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != LoginKey {
		t.Fatalf("revoked = %v, want the refused key", got)
	}
	AssertNothingStored(t, env)

	site.SetSpaces(SecondKey, TeamNotes)
	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Key: SecondKey, Access: "write", Endpoint: app.DefaultHostedEndpoint, ExpiresAt: LoginExpiry, Account: LoginAccount,
	}})
	code, stdout, stderr = RunCLI(t, "real", env, "login", "--no-browser")
	want := `Logged in as ada@example.test (read and write) until 2026-12-26 09:00 UTC; default space "team-notes"` + "\n"
	if code != 0 || stdout != want {
		t.Fatalf("default-site login = exit %d, stdout %q, stderr %s; want %q", code, stdout, stderr, want)
	}
	if strings.Contains(stderr, "Logging in through") || !strings.Contains(stderr, sitelogin.DefaultSite+"/cli/login#BCDF-GHJK") {
		t.Fatalf("default-site login stderr = %s, want the default page and no site warning", stderr)
	}
}

// TestScenarioConcurrentLoginsKeepBoth shows two logins for two endpoints
// whose keys the site issues together both end up stored and usable.
// Whether their writes overlap depends on scheduling, so this does not
// prove the lock; TestLockSerializesLogins in internal/credentials does.
func TestScenarioConcurrentLoginsKeepBoth(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	g2 := gatewaytest.Start(t)
	g2.AddSpace(HostedSpace, 1<<20)
	GrantMints(site, g, g2)
	issue := func(key string, gw *gatewaytest.Gateway) sitetest.Script {
		site.SetSpaces(key, TeamNotes)
		return sitetest.Script{Issue: sitetest.Issue{Key: key, Access: "write", Endpoint: gw.URL(), ExpiresAt: LoginExpiry, Account: LoginAccount}}
	}
	site.Queue(issue(LoginKey, g), issue(SecondKey, g2))
	site.HoldIssues(2)
	// Both processes run before either is waited for, so the site answers
	// their issuing polls together; the test holds one parallel slot.
	logins := []*HelperProc{
		SpawnHelper(t, "real", env, "login", "--no-browser"),
		SpawnHelper(t, "real", env, "login", "--no-browser"),
	}
	for _, h := range logins {
		code, stdout, stderr := h.RunStdioProcess(t, nil)
		if code != 0 || !strings.HasPrefix(stdout, "Logged in as ") {
			t.Fatalf("concurrent login = exit %d, stdout %q, stderr %s", code, stdout, stderr)
		}
	}
	for i, gw := range []*gatewaytest.Gateway{g, g2} {
		RunCLIOK(t, "real", env, nil, "pull", "--endpoint", gw.URL(), filepath.Join(root, []string{"first", "second"}[i]))
		if gw.Requests() == 0 {
			t.Fatalf("gateway %d saw no request", i)
		}
	}
	if got := site.Revoked(); len(got) != 0 {
		t.Fatalf("revoked = %v, want nothing", got)
	}
}

// TestScenarioTokenIgnoresABrokenCredentialsFile proves SLIVINGDOC_TOKEN
// alone is enough: with no bucket, a credentials file that cannot be read
// does not stop serve, which uses the token's own space and leaves the file
// as it was.
func TestScenarioTokenIgnoresABrokenCredentialsFile(t *testing.T) {
	t.Parallel()
	g, _, env, root := LoginEnv(t)
	if err := os.MkdirAll(CredentialsDir(env), 0o700); err != nil {
		t.Fatal(err)
	}
	const broken = "{not json"
	if err := os.WriteFile(CredentialsPath(env), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	tokenEnv := append(append([]string(nil), env...), "SLIVINGDOC_TOKEN="+HostedToken, "SLIVINGDOC_ENDPOINT="+g.URL())

	h := SpawnHelper(t, "real", tokenEnv, "serve")
	cs := h.ConnectClient(t)
	served := root
	AssertProcessCallOK(t, cs, ToolPull, served, "")
	WriteCLIFile(t, filepath.Join(served, "a.md"), "token alone\n")
	AssertProcessCallOK(t, cs, ToolCommit, served, "beside a broken credentials file")
	h.CloseClient(t, cs)
	if stderr := h.StderrText(t); !strings.Contains(stderr, "hosted space resolved") ||
		!strings.Contains(stderr, "space="+HostedSpace) || !strings.Contains(stderr, "from=token") {
		t.Fatalf("serve stderr = %s, want the token's space logged as resolved from the token", stderr)
	}
	if g.Stored(HostedSpace) == 0 {
		t.Fatal("the token's space holds no pack bytes after a commit")
	}
	if data, err := os.ReadFile(CredentialsPath(env)); err != nil || string(data) != broken {
		t.Fatalf("credentials file = %q, %v; want it untouched", data, err)
	}
}

// TestScenarioOldServerTokenNeedsASpace proves a stored login plays no
// part in a SLIVINGDOC_TOKEN process: against a server without the token
// lookup, the token with no space is refused naming --space even when the
// login stores a default space for that endpoint, and with --space it
// reaches the space with the variable's token alone.
func TestScenarioOldServerTokenNeedsASpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	g.DisableTokenLookup()
	const envToken = "sld_5555555555555555_b2xkLXNlcnZlci10b2tlbi1iZXNpZGUtdGhlLWxvZ2luLXh4eA"
	g.Grant(envToken, HostedSpace, false)
	tokenEnv := append(append([]string(nil), env...), "SLIVINGDOC_TOKEN="+envToken, "SLIVINGDOC_ENDPOINT="+g.URL())

	code, stdout, stderr := RunCLI(t, "real", tokenEnv, "pull", filepath.Join(root, "unnamed"))
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, "hosted storage cannot name the token's space; pass the space name as --space or SLIVINGDOC_SPACE") ||
		strings.Contains(stderr, envToken) || strings.Contains(stderr, LoginKey) {
		t.Fatalf("pull on an old server without a space = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	code, stdout, stderr = RunCLI(t, "real", tokenEnv, "pull", "--space", HostedSpace, filepath.Join(root, "named"))
	if code != 0 || !strings.HasPrefix(stdout, "OK  generation ") || !strings.Contains(stderr, "from=--space") {
		t.Fatalf("pull on an old server with --space = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if used := g.Used(); used[envToken] == 0 || len(site.Mints()) != 0 {
		t.Fatalf("space requests per token = %v after %d mints, want the variable's token alone", used, len(site.Mints()))
	}
}

// TestScenarioTokenRefusesAnotherSpaceFromTheVariable proves a
// SLIVINGDOC_BUCKET that names another space than the token's is refused
// with the fix for the variable.
func TestScenarioTokenRefusesAnotherSpaceFromTheVariable(t *testing.T) {
	t.Parallel()
	g, _, env, root := LoginEnv(t)
	g.AddSpace("other-notes", 1<<20)
	tokenEnv := append(append([]string(nil), env...),
		"SLIVINGDOC_TOKEN="+HostedToken, "SLIVINGDOC_ENDPOINT="+g.URL(), "SLIVINGDOC_BUCKET=other-notes")
	code, stdout, stderr := RunCLI(t, "real", tokenEnv, "pull", filepath.Join(root, "named"))
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, `the token reaches hosted space "`+HostedSpace+`", not "other-notes" from SLIVINGDOC_BUCKET; unset SLIVINGDOC_BUCKET`) {
		t.Fatalf("pull with SLIVINGDOC_BUCKET for another space = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
}
