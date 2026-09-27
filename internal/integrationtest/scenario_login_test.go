package integrationtest

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

const (
	loginAccount = "ada@example.test"
	loginOwner   = "bob@example.test"
	// closedS3 is a loopback address nothing listens on, with one SDK
	// attempt: a process that picks S3 fails its probe at once, without
	// ever reaching the gateway.
	closedS3 = "http://127.0.0.1:1"
)

// loginEnv starts a reference storage API with the hosted space and a
// reference site whose approvals issue tokens for it, and returns the
// environment of CLI processes that share one credentials directory, the
// shared workspace root, and both servers. No process in it carries
// SLIVINGDOC_TOKEN or SLIVINGDOC_BUCKET.
func loginEnv(t *testing.T) (*gatewaytest.Gateway, *sitetest.Site, []string, string) {
	t.Helper()
	g := gatewaytest.Start(t)
	g.AddSpace(hostedSpace, 1<<20)
	g.Grant(hostedToken, hostedSpace, false)
	g.Grant(hostedReader, hostedSpace, true)
	site := sitetest.Start(t)
	env, root := cliRoots(t)
	env = append(env,
		// A directory login creates, 0700 whatever the umask.
		credentials.DirEnv+"="+filepath.Join(t.TempDir(), "cfg"),
		"SLIVINGDOC_SITE="+site.URL(),
		"SLIVINGDOC_BUCKET=",
	)
	return g, site, env, root
}

// approve scripts the site's next approval to issue token for the hosted
// space at the gateway after pending polls.
func approve(site *sitetest.Site, g *gatewaytest.Gateway, token, access string, expires time.Time, pending ...string) {
	site.Next(sitetest.Script{Pending: pending, Issue: sitetest.Issue{
		Token: token, Space: hostedSpace, Access: access, Endpoint: g.URL(), ExpiresAt: expires,
		Account: loginAccount, Owner: loginAccount,
	}})
}

var loginExpiry = time.Date(2026, 12, 26, 9, 0, 0, 0, time.UTC)

// runLogin runs one login process and asserts it succeeded with the
// result line on stdout, and the approval page and code on stderr.
func runLogin(t *testing.T, env []string, site *sitetest.Site, want string, args ...string) {
	t.Helper()
	code, stdout, stderr := runCLI(t, "real", env, append([]string{"login"}, args...)...)
	if code != 0 || stdout != want+"\n" {
		t.Fatalf("login = exit %d, stdout %q; want 0 and %q; stderr: %s", code, stdout, want, stderr)
	}
	for _, line := range []string{
		"Logging in through " + site.URL() + " (from SLIVINGDOC_SITE).",
		"BCDF-GHJK", site.URL() + "/cli/login#BCDF-GHJK", "(or open " + site.URL() + "/cli/login and enter the code)",
		"your own terminal", "Approved by " + loginAccount,
	} {
		if !strings.Contains(stderr, line) {
			t.Fatalf("login stderr = %q, want it to contain %q", stderr, line)
		}
	}
	if strings.Contains(stdout+stderr, hostedToken) || strings.Contains(stdout+stderr, hostedReader) {
		t.Fatal("login printed the token")
	}
	if strings.Contains(stderr, "Store this login?") {
		t.Fatal("login asked a question without a terminal")
	}
}

// loggedIn is the login result line for the hosted space at the gateway,
// which is never the default endpoint, so the line names it.
func loggedIn(g *gatewaytest.Gateway, access, rest string) string {
	return `Logged in as ada@example.test to space "team-notes" at ` + g.URL() + " (" + access + ") " + rest
}

// TestScenarioLoginThenPullAndCommit proves the login workflow end to end:
// a browser approval whose polls see authorization_pending and slow_down
// stores the token, the failing browser is not fatal, and later pull and
// commit processes use hosted storage with neither SLIVINGDOC_TOKEN nor
// --bucket, because the stored login is the default.
func TestScenarioLoginThenPullAndCommit(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry, "authorization_pending", "slow_down", "authorization_pending")
	code, stdout, stderr := runCLI(t, "real", env, "login", "--space", hostedSpace)
	want := loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC") + "\n"
	if code != 0 || stdout != want {
		t.Fatalf("login = exit %d, stdout %q; want %q; stderr: %s", code, stdout, want, stderr)
	}
	if !strings.Contains(stderr, "Could not open a browser") || !strings.Contains(stderr, "open the page yourself") {
		t.Fatalf("login stderr = %q, want the browser fallback", stderr)
	}
	if starts := site.Starts(); len(starts) != 1 || starts[0].Space != hostedSpace || starts[0].Access != "write" || starts[0].Client == "" {
		t.Fatalf("start requests = %+v, want one for the space with a client label", starts)
	}
	if polls := site.Polls(); polls != 4 {
		t.Fatalf("token polls = %d, want 4 (three waits, then the token)", polls)
	}

	notes := filepath.Join(root, "notes")
	runCLIExact(t, "real", env,
		"OK  generation 0  "+notes+"\n0 files changed, 0 insertions(+), 0 deletions(-)\n",
		"pull", notes)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "logged in\n")
	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "through a login")
	if g.Stored(hostedSpace) == 0 {
		t.Fatal("the space holds no pack bytes after a commit through a login")
	}
	if runtime.GOOS != "windows" {
		assertPerm(t, credentialsDir(env), 0o700)
		assertPerm(t, credentialsPath(env), 0o600)
	}

	// An MCP server started with the same environment reaches the space
	// with the stored token, and only with it.
	h := spawnHelper(t, "real", env, "serve")
	cs := h.connectClient(t)
	served := filepath.Join(root, "served")
	assertProcessCallOK(t, cs, toolPull, served, "")
	writeCLIFile(t, filepath.Join(served, "b.md"), "over MCP\n")
	assertProcessCallOK(t, cs, toolCommit, served, "served through a login")
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("serve exit = %d; stderr: %s", code, h.stderrText(t))
	}
	if used := g.Used(); len(used) != 1 || used[hostedToken] == 0 {
		t.Fatalf("space requests per token = %v, want the stored token alone", used)
	}
}

// TestScenarioLoginAgainRevokesTheOldToken proves a second login for the
// same space replaces the stored token and revokes the old one, names the
// space owner when someone else owns it, and that the replacement is what
// later processes send: a read-only token refuses the commit.
func TestScenarioLoginAgainRevokesTheOldToken(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"))

	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Token: hostedReader, Space: hostedSpace, Access: "read", Endpoint: g.URL(), ExpiresAt: loginExpiry,
		Account: loginAccount, Owner: loginOwner,
	}})
	runLogin(t, env, site,
		loggedIn(g, "read only", "until 2026-12-26 09:00 UTC, owned by bob@example.test"),
		"--read-only", "--no-browser")
	if got := site.Revoked(); len(got) != 1 || got[0] != hostedToken {
		t.Fatalf("revoked = %v, want the replaced token", got)
	}
	if starts := site.Starts(); len(starts) != 2 || starts[1].Access != "read" || starts[1].Space != "" {
		t.Fatalf("start requests = %+v, want a read-only second start without a space", starts)
	}

	notes := filepath.Join(root, "notes")
	runCLIOK(t, "real", env, nil, "pull", notes)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "read only\n")
	code, stdout, _ := runCLI(t, "real", env, "commit", notes, "-m", "refused")
	if code != 1 || !strings.Contains(stdout, "STORAGE_FAILURE · ACCESS_DENIED") {
		t.Fatalf("commit with the read-only login = exit %d, stdout %q; want ACCESS_DENIED", code, stdout)
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
			g, site, env, root := loginEnv(t)
			site.Next(sitetest.Script{Pending: []string{"authorization_pending"}, Final: row.final})
			code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, row.want) {
				t.Fatalf("login = exit %d, stdout %q, stderr %q; want exit 1 naming %q", code, stdout, stderr, row.want)
			}
			code, _, stderr = runCLI(t, "real", env, "pull", "--storage", "hosted", "--bucket", hostedSpace, filepath.Join(root, "notes"))
			if code != 1 || !strings.Contains(stderr, "needs SLIVINGDOC_TOKEN or a stored login") {
				t.Fatalf("pull after a failed login = exit %d, stderr %q; want the missing-login refusal", code, stderr)
			}
			if g.Requests() != 0 {
				t.Fatal("a process without a login reached the storage API")
			}
		})
	}
}

// TestScenarioStorageSelection proves --storage and its automatic choice
// from the outside: SLIVINGDOC_TOKEN beats a stored login and is refused
// only beside an S3 endpoint; a login for a named bucket plus S3 settings
// is refused as ambiguous until --storage decides; s3 never
// sends a token or a login to the hosted API; hosted without a login is
// refused; and a login is only used for the endpoint it was issued for.
func TestScenarioStorageSelection(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")
	notes := filepath.Join(root, "notes")
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }
	s3 := []string{"AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret", "AWS_MAX_ATTEMPTS=1", "AWS_REGION=us-east-1"}
	awsHome := t.TempDir()
	writeCLIFile(t, filepath.Join(awsHome, ".aws", "config"), "[default]\n")

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
			name: "a login and AWS settings are ambiguous for an explicit bucket",
			env:  with(append([]string{"SLIVINGDOC_BUCKET=" + hostedSpace}, s3...)...),
			want: []string{`stored login for space "team-notes"`, "AWS_ACCESS_KEY_ID", "--storage hosted or --storage s3"},
		},
		{
			name: "a login and the shared AWS files are ambiguous for an explicit bucket",
			env:  with("HOME=" + awsHome),
			args: []string{"--bucket", hostedSpace},
			want: []string{`stored login for space "team-notes"`, "~/.aws/config", "--storage hosted or --storage s3"},
		},
		{
			name: "a login and --region are ambiguous for an explicit bucket",
			args: []string{"--bucket", hostedSpace, "--region", "eu-north-1"},
			want: []string{`stored login for space "team-notes"`, "(--region)", "--storage hosted or --storage s3"},
		},
		{
			name: "the token and an S3 endpoint flag are ambiguous",
			env:  with("SLIVINGDOC_TOKEN="+hostedToken, "SLIVINGDOC_BUCKET="+hostedSpace),
			args: []string{"--endpoint", closedS3},
			want: []string{"SLIVINGDOC_TOKEN and an S3 endpoint (--endpoint)", "--storage hosted to use hosted storage (the token goes only to the hosted endpoint)"},
		},
		{
			name: "the token and an AWS endpoint variable are ambiguous",
			env:  with(append([]string{"SLIVINGDOC_TOKEN=" + hostedToken, "SLIVINGDOC_ENDPOINT=" + g.URL(), "AWS_ENDPOINT_URL_S3=" + closedS3}, s3...)...),
			want: []string{"SLIVINGDOC_TOKEN and an S3 endpoint (AWS_ENDPOINT_URL_S3)"},
		},
		{
			name: "s3 ignores the token and the login",
			env:  with(append([]string{"SLIVINGDOC_TOKEN=" + hostedToken, "AWS_ENDPOINT_URL_S3=" + closedS3, "SLIVINGDOC_BUCKET=" + hostedSpace}, s3...)...),
			args: []string{"--storage", "s3"},
			want: []string{"S3 compatibility probe failed"},
		},
		{
			name: "an endpoint the login was not issued for keeps S3",
			env:  with(append([]string{"SLIVINGDOC_BUCKET=" + hostedSpace}, s3...)...),
			args: []string{"--endpoint", closedS3},
			want: []string{"S3 compatibility probe failed"},
		},
		{
			name: "an endpoint the login was not issued for never borrows its space as a bucket",
			env:  with(s3...),
			args: []string{"--endpoint", closedS3},
			want: []string{"bucket is required"},
		},
		{
			name: "hosted at an endpoint the login was not issued for",
			args: []string{"--storage", "hosted", "--endpoint", closedS3},
			want: []string{"needs SLIVINGDOC_TOKEN or a stored login", "issued for " + g.URL()},
		},
		{
			name: "hosted without a login",
			env:  with(credentials.DirEnv + "=" + t.TempDir()),
			args: []string{"--storage", "hosted", "--bucket", hostedSpace},
			want: []string{"needs SLIVINGDOC_TOKEN or a stored login", "slivingdoc login --space team-notes"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			rowEnv := row.env
			if rowEnv == nil {
				rowEnv = env
			}
			before := g.Requests()
			code, stdout, stderr := runCLI(t, "real", rowEnv, append([]string{"pull"}, append(row.args, notes)...)...)
			if code != 1 || strings.TrimSpace(stdout) != "" {
				t.Fatalf("pull = exit %d, stdout %q; want a startup refusal; stderr %s", code, stdout, stderr)
			}
			for _, want := range row.want {
				if !strings.Contains(stderr, want) {
					t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
				}
			}
			if strings.Contains(stderr, hostedToken) {
				t.Fatal("a refusal echoed the stored token")
			}
			if sent := g.Requests() != before; sent != row.sent {
				t.Fatalf("the gateway saw a request = %v, want %v", sent, row.sent)
			}
		})
	}

	// Choosing hosted resolves the ambiguity with the same AWS settings,
	// and a bucket taken from the default login needs no choice: S3 has
	// no bucket to use.
	runCLIOK(t, "real", with(append([]string{"SLIVINGDOC_BUCKET=" + hostedSpace}, s3...)...), nil, "pull", "--storage", "hosted", notes)
	code, stdout, stderr := runCLI(t, "real", with(s3...), "pull", notes)
	if code != 0 || !strings.HasPrefix(stdout, "OK  generation ") {
		t.Fatalf("pull with the default login and AWS settings = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	// The startup log says which store and token source won.
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
	g.AddSpace(hostedSpace, 1<<20)
	g.Grant(hostedToken, hostedSpace, false)
	env, root := cliRoots(t)
	home := t.TempDir()
	writeCLIFile(t, filepath.Join(home, ".aws", "config"), "[default]\nregion = eu-north-1\n")
	env = append(env,
		"HOME="+home, "AWS_REGION=eu-north-1", "AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret",
		"SLIVINGDOC_TOKEN="+hostedToken, "SLIVINGDOC_ENDPOINT="+g.URL(), "SLIVINGDOC_BUCKET=",
	)
	h := spawnHelper(t, "real", env, "serve", "--bucket", hostedSpace)
	cs := h.connectClient(t)
	notes := filepath.Join(root, "notes")
	assertProcessCallOK(t, cs, toolPull, notes, "")
	writeCLIFile(t, filepath.Join(notes, "ci.md"), "from CI\n")
	assertProcessCallOK(t, cs, toolCommit, notes, "beside AWS settings")
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("serve exit = %d; stderr: %s", code, h.stderrText(t))
	}
	if used := g.Used(); used[hostedToken] == 0 || g.Stored(hostedSpace) == 0 {
		t.Fatalf("space requests per token = %v, stored %d; want the commit through the token", used, g.Stored(hostedSpace))
	}
	if stderr := h.stderrText(t); !strings.Contains(stderr, "backend=hosted") || !strings.Contains(stderr, "token=env") {
		t.Fatalf("serve stderr = %s, want the hosted startup record", stderr)
	}
}

// TestScenarioLoginAndTokenAgreeOnTheSpace proves the space rule when a
// stored login, SLIVINGDOC_TOKEN and the API's own answer meet: every
// hosted process asks the API which space its token reaches, uses it when
// nothing else names a space, and refuses a disagreement instead of
// picking one.
func TestScenarioLoginAndTokenAgreeOnTheSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }

	// A stored login and no bucket: the default login's space, which the
	// API confirms for its token.
	runCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "login"))

	// SLIVINGDOC_TOKEN beside the login, for the same space, no bucket:
	// the variable's token wins and the space agrees.
	const sameSpace = "sld_3333333333333333_c2FtZS1zcGFjZS10b2tlbi1iZXNpZGUtdGhlLWxvZ2luLXh4"
	g.Grant(sameSpace, hostedSpace, true)
	runCLIOK(t, "real", with("SLIVINGDOC_TOKEN="+sameSpace, "SLIVINGDOC_ENDPOINT="+g.URL()), nil, "pull", filepath.Join(root, "same"))
	if used := g.Used(); used[sameSpace] == 0 {
		t.Fatalf("space requests per token = %v, want the variable's token used", used)
	}

	// SLIVINGDOC_TOKEN for another space and no bucket: the token alone
	// is enough, so its own space is used, not the default login's.
	const otherSpace, otherToken = "other-notes", "sld_4444444444444444_b3RoZXItc3BhY2UtdG9rZW4tYmVzaWRlLXRoZS1sb2dpbi14eA"
	g.AddSpace(otherSpace, 1<<20)
	g.Grant(otherToken, otherSpace, false)
	tokenEnv := with("SLIVINGDOC_TOKEN="+otherToken, "SLIVINGDOC_ENDPOINT="+g.URL())
	runCLIOK(t, "real", tokenEnv, nil, "pull", filepath.Join(root, "other"))
	// A --bucket that names another space is refused with the flag's fix.
	code, stdout, stderr := runCLI(t, "real", tokenEnv, "pull", "--bucket", hostedSpace, filepath.Join(root, "named"))
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, `the token reaches hosted space "other-notes", not "team-notes" from --bucket`) {
		t.Fatalf("pull with --bucket for another space than the token's = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}

	// A stored login whose token now reaches another space (moved on the
	// site) is refused with the fix, not silently redirected.
	g.Grant(hostedToken, otherSpace, false)
	code, stdout, stderr = runCLI(t, "real", env, "pull", filepath.Join(root, "moved"))
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, `the stored login for space "team-notes" holds a token that reaches hosted space "other-notes"`) ||
		!strings.Contains(stderr, "run 'slivingdoc login --space team-notes' again") {
		t.Fatalf("pull with a login whose token moved = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
}

// TestScenarioExpiredLoginIsRefused proves a stored login past its expiry
// refuses startup with the fix, before any request reaches the storage
// API.
func TestScenarioExpiredLoginIsRefused(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2001-01-01 00:00 UTC"), "--no-browser")
	code, stdout, stderr := runCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
	if code != 1 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("pull with an expired login = exit %d, stdout %q; want a startup refusal", code, stdout)
	}
	for _, want := range []string{"stored login expired", "run 'slivingdoc login --space team-notes'"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
	if g.Requests() != 0 {
		t.Fatal("an expired login reached the storage API")
	}
}

// TestScenarioLogout proves logout keeps a login whose revocation the
// site refused with anything but invalid_token, revokes the default
// login's token at the site and removes it, so hosted storage is refused
// afterwards, and that logging out again is a refusal.
func TestScenarioLogout(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")

	// A 401 that does not say the token is invalid is not proof that it is
	// gone, so the login stays stored and still works.
	site.RefuseRevoke(http.StatusUnauthorized, "unauthorized")
	code, stdout, stderr := runCLI(t, "real", env, "logout")
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "could not be revoked and stays stored") {
		t.Fatalf("logout refused with another 401 = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	runCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "kept"))

	code, stdout, stderr = runCLI(t, "real", env, "logout")
	if code != 0 || stdout != "Logged out of space \"team-notes\" at "+g.URL()+"; the token was revoked\n" {
		t.Fatalf("logout = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != hostedToken {
		t.Fatalf("revoked = %v, want the stored token", got)
	}
	code, _, stderr = runCLI(t, "real", env, "pull", "--storage", "hosted", "--bucket", hostedSpace, filepath.Join(root, "notes"))
	if code != 1 || !strings.Contains(stderr, "needs SLIVINGDOC_TOKEN or a stored login") {
		t.Fatalf("pull after logout = exit %d, stderr %q; want the missing-login refusal", code, stderr)
	}
	code, _, stderr = runCLI(t, "real", env, "logout")
	if code != 1 || !strings.Contains(stderr, "not logged in") {
		t.Fatalf("second logout = exit %d, stderr %q; want a refusal", code, stderr)
	}
}

// TestScenarioLoginGuardsWhatIsStored proves, from the outside, what a
// login refuses to store without a person at a terminal: an approval by
// another account for a stored space (revoked unless --force, and the
// replaced token of another account is never revoked), and a login for a
// space another site's login holds (revoked even with --force).
func TestScenarioLoginGuardsWhatIsStored(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")
	notes := filepath.Join(root, "notes")

	const eveToken = "sld_eeeeeeeeeeeeeeee_ZXZlLWFwcHJvdmVkLXRoZS1jb2RlLWZpcnN0LXRva2Vu"
	g.Grant(eveToken, hostedSpace, false)
	eve := sitetest.Issue{
		Token: eveToken, Space: hostedSpace, Access: "write", Endpoint: g.URL(), ExpiresAt: loginExpiry,
		Account: "eve@example.test", Owner: loginAccount,
	}
	site.Next(sitetest.Script{Issue: eve})
	code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, "Approved by eve@example.test") ||
		!strings.Contains(stderr, "approved by ada@example.test, this one by eve@example.test") ||
		!strings.Contains(stderr, "--force") {
		t.Fatalf("login approved by another account = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != eveToken {
		t.Fatalf("revoked = %v, want only the refused token", got)
	}
	// The stored login is untouched: a commit still goes through ada's.
	runCLIOK(t, "real", env, nil, "pull", notes)

	site.Next(sitetest.Script{Issue: eve})
	code, stdout, stderr = runCLI(t, "real", env, "login", "--no-browser", "--force")
	if code != 0 || !strings.HasPrefix(stdout, "Logged in as eve@example.test") ||
		!strings.Contains(stderr, "was approved by ada@example.test, not eve@example.test, so it was not revoked") {
		t.Fatalf("login --force = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 {
		t.Fatalf("revoked = %v; another account's replaced token must stay valid", got)
	}

	// A second site's approval for the same space and endpoint never
	// replaces the first site's login, whatever the flags.
	other := sitetest.Start(t)
	other.Next(sitetest.Script{Issue: sitetest.Issue{
		Token: hostedReader, Space: hostedSpace, Access: "read", Endpoint: g.URL(), ExpiresAt: loginExpiry,
		Account: "eve@example.test", Owner: loginAccount,
	}})
	otherEnv := append(append([]string(nil), env...), "SLIVINGDOC_SITE="+other.URL())
	code, _, stderr = runCLI(t, "real", otherEnv, "login", "--no-browser", "--force")
	if code != 1 || !strings.Contains(stderr, "was issued by "+site.URL()+", not "+other.URL()) ||
		!strings.Contains(stderr, "log out of that login first") {
		t.Fatalf("login through another site = exit %d, stderr %s", code, stderr)
	}
	if got := other.Revoked(); len(got) != 1 || got[0] != hostedReader {
		t.Fatalf("other site revoked = %v, want its own refused token", got)
	}
	if got := site.Revoked(); len(got) != 1 {
		t.Fatalf("first site revoked = %v; the stored login must be left alone", got)
	}
}

// TestScenarioLoginKeepsTheDefault proves the first login is the default
// and a later one only with --default, which later processes then follow,
// and that a space with logins at two
// endpoints is refused as ambiguous even though the default names it.
func TestScenarioLoginKeepsTheDefault(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")
	notes := filepath.Join(root, "notes")

	const second = "second-space"
	g.AddSpace(second, 1<<20)
	const secondToken = "sld_5555555555555555_c2Vjb25kLXNwYWNlLXRva2VuLWZvci10aGUtZGVmYXVsdA"
	g.Grant(secondToken, second, false)
	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Token: secondToken, Space: second, Access: "write", Endpoint: g.URL(), ExpiresAt: loginExpiry,
		Account: loginAccount, Owner: loginAccount,
	}})
	code, _, stderr := runCLI(t, "real", env, "login", "--no-browser")
	if code != 0 || !strings.Contains(stderr, `The default login stays space "team-notes" at `+g.URL()+"; use --default to switch.") {
		t.Fatalf("second login = exit %d, stderr %s", code, stderr)
	}
	// Without --bucket the default space is still the first one.
	runCLIOK(t, "real", env, nil, "pull", notes)
	if g.Stored(second) != 0 {
		t.Fatal("the second space was touched")
	}
	writeCLIFile(t, filepath.Join(notes, "a.md"), "default\n")
	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "to the default")
	if g.Stored(hostedSpace) == 0 {
		t.Fatal("the commit did not reach the default space")
	}

	// A login for the default space at a second endpoint makes the space
	// ambiguous without --endpoint.
	g2 := gatewaytest.Start(t)
	g2.AddSpace(hostedSpace, 1<<20)
	g2.Grant(hostedReader, hostedSpace, true)
	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Token: hostedReader, Space: hostedSpace, Access: "read", Endpoint: g2.URL(), ExpiresAt: loginExpiry,
		Account: loginAccount, Owner: loginAccount,
	}})
	if code, _, stderr := runCLI(t, "real", env, "login", "--no-browser"); code != 0 {
		t.Fatalf("login at a second endpoint = exit %d, stderr %s", code, stderr)
	}
	code, stdout, stderr := runCLI(t, "real", env, "pull", filepath.Join(root, "other"))
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "several stored logins match") ||
		!strings.Contains(stderr, "pass --endpoint") {
		t.Fatalf("pull of an ambiguous default space = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	runCLIOK(t, "real", env, nil, "pull", "--endpoint", g2.URL(), filepath.Join(root, "other"))

	// --default moves the default to the new login, and later processes
	// without --bucket follow it.
	const renewed = "sld_6666666666666666_cmVuZXdlZC1zZWNvbmQtc3BhY2UtdG9rZW4tZm9yLWRlZmF1bHQ"
	g.Grant(renewed, second, false)
	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Token: renewed, Space: second, Access: "write", Endpoint: g.URL(), ExpiresAt: loginExpiry,
		Account: loginAccount, Owner: loginAccount,
	}})
	code, _, stderr = runCLI(t, "real", env, "login", "--no-browser", "--default")
	if code != 0 || !strings.Contains(stderr, `The default login changed from space "team-notes" at `+g.URL()+` to space "second-space" at `+g.URL()+".") {
		t.Fatalf("login --default = exit %d, stderr %s", code, stderr)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != secondToken {
		t.Fatalf("revoked = %v, want the replaced token of the same account", got)
	}
	runCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "moved"))
	if used := g.Used(); used[renewed] == 0 {
		t.Fatalf("space requests per token = %v, want the new default's token used", used)
	}
}

// credentialsDir is the credentials directory env gives every process.
func credentialsDir(env []string) string {
	dir := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, credentials.DirEnv+"="); ok {
			dir = v
		}
	}
	return dir
}

// credentialsPath is the credentials file of env's processes.
func credentialsPath(env []string) string {
	return filepath.Join(credentialsDir(env), credentials.FileName)
}

func assertPerm(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %v, want %v", path, got, want)
	}
}

// assertNothingStored proves a login left no credentials file behind.
func assertNothingStored(t *testing.T, env []string) {
	t.Helper()
	if _, err := os.Stat(credentialsPath(env)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("credentials file after a failed login: %v, want none", err)
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
			mustDo(t, os.Rename(file, moved))
			mustDo(t, os.Symlink(moved, file))
		}, "is a symbolic link"},
		{"a FIFO", func(t *testing.T, file string) {
			mustDo(t, os.Remove(file))
			mustDo(t, mkfifo(file))
		}, "is not a regular file"},
		{"a group-readable file", func(t *testing.T, file string) {
			mustDo(t, os.Chmod(file, 0o640))
		}, "chmod 600"},
		{"a group-writable directory", func(t *testing.T, file string) {
			mustDo(t, os.Chmod(filepath.Dir(file), 0o770))
		}, "chmod go-w"},
		{"a file over 1 MiB", func(t *testing.T, file string) {
			mustDo(t, os.WriteFile(file, []byte(strings.Repeat(" ", 1<<20+1)), 0o600))
		}, "larger than 1048576 bytes"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, root := loginEnv(t)
			approve(site, g, hostedToken, "write", loginExpiry)
			runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")
			row.plant(t, credentialsPath(env))
			code, stdout, stderr := runCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, row.want) {
				t.Fatalf("pull with %s = exit %d, stdout %q, stderr %s; want a refusal naming %q", row.name, code, stdout, stderr, row.want)
			}
			if g.Requests() != 0 {
				t.Fatalf("pull with %s reached the storage API", row.name)
			}
		})
	}
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestScenarioLoginBrokenAnswers proves every way a login can lose a token
// the site may have minted ends nonzero with the hint to revoke it on the
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
		{"oversize", sitetest.Script{Body: `{"token":"` + strings.Repeat("a", 17<<10) + `"}`}, "larger than 16384 bytes"},
		{"undecodable", sitetest.Script{Body: "{not json"}, "not the expected JSON"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, _ := loginEnv(t)
			script := row.script
			script.Issue = sitetest.Issue{Token: hostedToken, Space: hostedSpace, Access: "write", Endpoint: g.URL(), Account: loginAccount, Owner: loginAccount}
			site.Next(script)
			code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, row.want) || !strings.Contains(stderr, sitelogin.TokenHint) {
				t.Fatalf("login = exit %d, stdout %q, stderr %s; want exit 1 naming %q with the hint", code, stdout, stderr, row.want)
			}
			if polls := site.Polls(); polls != 1 {
				t.Fatalf("token polls = %d, want 1: a claimed code is never polled again", polls)
			}
			assertNothingStored(t, env)
		})
	}
}

// TestScenarioLoginInterruptedMidPoll proves an interrupt while a poll is
// in flight ends the login nonzero with the hint, since the site may have
// issued the token it never delivered, and stores nothing.
func TestScenarioLoginInterruptedMidPoll(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to another process is not implemented on Windows")
	}
	g, site, env, _ := loginEnv(t)
	site.Next(sitetest.Script{Stall: true, Issue: sitetest.Issue{
		Token: hostedToken, Space: hostedSpace, Access: "write", Endpoint: g.URL(), Account: loginAccount, Owner: loginAccount,
	}})
	h := spawnHelper(t, "real", append(append([]string(nil), env...), helperSignalsEnv+"=1"), "login", "--no-browser")
	interrupted := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for site.Polls() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		interrupted <- h.proc.Signal(os.Interrupt)
	}()
	code, stdout, stderr := h.runStdioProcess(t, nil)
	if err := <-interrupted; err != nil {
		t.Fatalf("interrupt the login: %v", err)
	}
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "stopped while a poll was in flight") ||
		!strings.Contains(stderr, sitelogin.TokenHint) {
		t.Fatalf("interrupted login = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	assertNothingStored(t, env)
}

// TestScenarioLoginRefusesWhatItDidNotAskFor proves a login stores nothing
// the person did not ask for, and revokes any token it was given: a write
// token for --read-only, a token for another space than --bucket; and
// that an approval page the client cannot trust (a code outside the
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
			name:    "a write token for a read-only login",
			setup:   func(site *sitetest.Site, g *gatewaytest.Gateway) { approve(site, g, hostedToken, "write", loginExpiry) },
			args:    []string{"--read-only"},
			want:    "issued a read and write token for a read-only login; nothing was stored",
			revoked: true,
		},
		{
			name:    "a token for another space",
			setup:   func(site *sitetest.Site, g *gatewaytest.Gateway) { approve(site, g, hostedToken, "write", loginExpiry) },
			args:    []string{"--bucket", "other-space"},
			want:    `issued a token for space "team-notes", not the requested "other-space"; nothing was stored`,
			revoked: true,
		},
		{
			name: "a code with a vowel",
			setup: func(site *sitetest.Site, g *gatewaytest.Gateway) {
				approve(site, g, hostedToken, "write", loginExpiry)
				site.SetUserCode("BCDA-GHJK")
			},
			want: "the user code is not two groups of four consonants",
		},
		{
			name: "a filled-in page for another code",
			setup: func(site *sitetest.Site, g *gatewaytest.Gateway) {
				approve(site, g, hostedToken, "write", loginExpiry)
				site.SetCompleteURI(site.URL() + "/cli/login#ZZZZ-ZZZZ")
			},
			want: "verificationUriComplete is not " + "SITE/cli/login#BCDF-GHJK",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, _ := loginEnv(t)
			row.setup(site, g)
			code, stdout, stderr := runCLI(t, "real", env, append([]string{"login", "--no-browser"}, row.args...)...)
			want := strings.ReplaceAll(row.want, "SITE", site.URL())
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, want) {
				t.Fatalf("login = exit %d, stdout %q, stderr %s; want exit 1 naming %q", code, stdout, stderr, want)
			}
			revoked := site.Revoked()
			if row.revoked && (len(revoked) != 1 || revoked[0] != hostedToken) {
				t.Fatalf("revoked = %v, want the unrequested token", revoked)
			}
			if !row.revoked && (len(revoked) != 0 || site.Polls() != 0) {
				t.Fatalf("revoked = %v after %d polls, want no poll at all", revoked, site.Polls())
			}
			assertNothingStored(t, env)
		})
	}
}

// TestScenarioLoginThroughTheDefaultSite proves the default site's logins
// are bound to the default storage endpoint: a token it issues for any
// other endpoint is refused and revoked, and one for the default endpoint
// is stored with no endpoint in the result line and no site warning. The
// helper routes the default site's requests to a reference site that
// answers with the default site's addresses.
func TestScenarioLoginThroughTheDefaultSite(t *testing.T) {
	t.Parallel()
	g, site, env, _ := loginEnv(t)
	site.SetOrigin(sitelogin.DefaultSite)
	env = append(env, "SLIVINGDOC_SITE=", helperSiteRouteEnv+"="+site.URL())

	approve(site, g, hostedToken, "write", loginExpiry)
	code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, sitelogin.DefaultSite+" issued a token for "+g.URL()+", not "+app.DefaultHostedEndpoint) {
		t.Fatalf("default-site login for another endpoint = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != hostedToken {
		t.Fatalf("revoked = %v, want the refused token", got)
	}
	assertNothingStored(t, env)

	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Token: hostedReader, Space: hostedSpace, Access: "write", Endpoint: app.DefaultHostedEndpoint, ExpiresAt: loginExpiry,
		Account: loginAccount, Owner: loginAccount,
	}})
	code, stdout, stderr = runCLI(t, "real", env, "login", "--no-browser")
	want := `Logged in as ada@example.test to space "team-notes" (read and write) until 2026-12-26 09:00 UTC` + "\n"
	if code != 0 || stdout != want {
		t.Fatalf("default-site login = exit %d, stdout %q, stderr %s; want %q", code, stdout, stderr, want)
	}
	if strings.Contains(stderr, "Logging in through") || !strings.Contains(stderr, sitelogin.DefaultSite+"/cli/login#BCDF-GHJK") {
		t.Fatalf("default-site login stderr = %s, want the default page and no site warning", stderr)
	}
}

// TestScenarioConcurrentLoginsKeepBoth shows two logins whose tokens the
// site issues together both end up stored and usable. Whether their writes
// overlap depends on scheduling, so this does not prove the lock;
// TestLockSerializesLogins in internal/credentials does.
func TestScenarioConcurrentLoginsKeepBoth(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	const second = "second-space"
	const secondToken = "sld_7777777777777777_Y29uY3VycmVudC1sb2dpbi1mb3ItdGhlLXNlY29uZC1zcGFjZQ"
	g.AddSpace(second, 1<<20)
	g.Grant(secondToken, second, false)
	issue := func(token, space string) sitetest.Script {
		return sitetest.Script{Issue: sitetest.Issue{
			Token: token, Space: space, Access: "write", Endpoint: g.URL(), ExpiresAt: loginExpiry,
			Account: loginAccount, Owner: loginAccount,
		}}
	}
	site.Queue(issue(hostedToken, hostedSpace), issue(secondToken, second))
	site.HoldIssues(2)
	t.Run("logins", func(t *testing.T) {
		for _, space := range []string{hostedSpace, second} {
			t.Run(space, func(t *testing.T) {
				t.Parallel()
				code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
				if code != 0 || !strings.HasPrefix(stdout, "Logged in as ") {
					t.Fatalf("concurrent login = exit %d, stdout %q, stderr %s", code, stdout, stderr)
				}
			})
		}
	})
	for _, space := range []string{hostedSpace, second} {
		runCLIOK(t, "real", env, nil, "pull", "--bucket", space, filepath.Join(root, space))
	}
	if used := g.Used(); used[hostedToken] == 0 || used[secondToken] == 0 {
		t.Fatalf("space requests per token = %v, want both stored tokens used", used)
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
	g, _, env, root := loginEnv(t)
	if err := os.MkdirAll(credentialsDir(env), 0o700); err != nil {
		t.Fatal(err)
	}
	const broken = "{not json"
	if err := os.WriteFile(credentialsPath(env), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	tokenEnv := append(append([]string(nil), env...), "SLIVINGDOC_TOKEN="+hostedToken, "SLIVINGDOC_ENDPOINT="+g.URL())

	h := spawnHelper(t, "real", tokenEnv, "serve")
	cs := h.connectClient(t)
	served := filepath.Join(root, "served")
	assertProcessCallOK(t, cs, toolPull, served, "")
	writeCLIFile(t, filepath.Join(served, "a.md"), "token alone\n")
	assertProcessCallOK(t, cs, toolCommit, served, "beside a broken credentials file")
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("serve exit = %d; stderr: %s", code, h.stderrText(t))
	}
	if stderr := h.stderrText(t); !strings.Contains(stderr, "hosted space resolved") ||
		!strings.Contains(stderr, "space="+hostedSpace) || !strings.Contains(stderr, "from=token") {
		t.Fatalf("serve stderr = %s, want the token's space logged as resolved from the token", stderr)
	}
	if g.Stored(hostedSpace) == 0 {
		t.Fatal("the token's space holds no pack bytes after a commit")
	}
	if data, err := os.ReadFile(credentialsPath(env)); err != nil || string(data) != broken {
		t.Fatalf("credentials file = %q, %v; want it untouched", data, err)
	}
}

// TestScenarioOldServerTakesTheDefaultLoginsSpace proves that against a
// server without the token lookup, SLIVINGDOC_TOKEN with no bucket uses
// the default login's space only when that login is for the same endpoint,
// and that a login for another endpoint is a refusal naming it.
func TestScenarioOldServerTakesTheDefaultLoginsSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")
	g.DisableTokenLookup()
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }

	const envToken = "sld_5555555555555555_b2xkLXNlcnZlci10b2tlbi1iZXNpZGUtdGhlLWxvZ2luLXh4eA"
	g.Grant(envToken, hostedSpace, false)
	code, stdout, stderr := runCLI(t, "real", with("SLIVINGDOC_TOKEN="+envToken, "SLIVINGDOC_ENDPOINT="+g.URL()), "pull", filepath.Join(root, "same"))
	if code != 0 || !strings.HasPrefix(stdout, "OK  generation ") ||
		!strings.Contains(stderr, "space="+hostedSpace) || !strings.Contains(stderr, `from="default login"`) {
		t.Fatalf("pull on an old server with the default login at its endpoint = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if used := g.Used(); used[envToken] == 0 {
		t.Fatalf("space requests per token = %v, want the variable's token used", used)
	}

	other := gatewaytest.Start(t)
	other.AddSpace(hostedSpace, 1<<20)
	other.Grant(envToken, hostedSpace, false)
	other.DisableTokenLookup()
	code, stdout, stderr = runCLI(t, "real", with("SLIVINGDOC_TOKEN="+envToken, "SLIVINGDOC_ENDPOINT="+other.URL()), "pull", filepath.Join(root, "other"))
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, "the default login is for "+g.URL()+", not "+other.URL()) ||
		!strings.Contains(stderr, "pass the space name as --space or SLIVINGDOC_SPACE") ||
		strings.Contains(stderr, envToken) || strings.Contains(stderr, hostedToken) {
		t.Fatalf("pull on an old server with the default login elsewhere = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
}

// TestScenarioTokenRefusesAnotherSpaceFromTheVariable proves a
// SLIVINGDOC_BUCKET that names another space than the token's is refused
// with the fix for the variable.
func TestScenarioTokenRefusesAnotherSpaceFromTheVariable(t *testing.T) {
	t.Parallel()
	g, _, env, root := loginEnv(t)
	g.AddSpace("other-notes", 1<<20)
	tokenEnv := append(append([]string(nil), env...),
		"SLIVINGDOC_TOKEN="+hostedToken, "SLIVINGDOC_ENDPOINT="+g.URL(), "SLIVINGDOC_BUCKET=other-notes")
	code, stdout, stderr := runCLI(t, "real", tokenEnv, "pull", filepath.Join(root, "named"))
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, `the token reaches hosted space "`+hostedSpace+`", not "other-notes" from SLIVINGDOC_BUCKET; unset SLIVINGDOC_BUCKET`) {
		t.Fatalf("pull with SLIVINGDOC_BUCKET for another space = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
}
