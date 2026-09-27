package integrationtest

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	// loginKey and secondKey are account login keys the reference site
	// issues. A key never reaches a storage endpoint: the site trades it
	// for short-lived space tokens, which the gateway grants as they are
	// minted (grantMints).
	loginKey  = "sld_1010101010101010_YWNjb3VudC1sb2dpbi1rZXktZm9yLWludGVncmF0aW9uLXRlc3Q"
	secondKey = "sld_2020202020202020_c2Vjb25kLWFjY291bnQtbG9naW4ta2V5LWZvci1zY2VuYXJpb3M"
	// closedS3 is a loopback address nothing listens on, with one SDK
	// attempt: a process that picks S3 fails its probe at once, without
	// ever reaching the gateway.
	closedS3 = "http://127.0.0.1:1"
)

// grantMints makes every token site mints usable on the gateway of its
// endpoint, as the real site's rows are for the real gateway.
func grantMints(site *sitetest.Site, gateways ...*gatewaytest.Gateway) {
	site.OnMint(func(m sitetest.Minted) {
		for _, g := range gateways {
			if g.URL() == m.Endpoint {
				g.Grant(m.Token, m.Space, m.Access == "read")
			}
		}
	})
}

// loginEnv starts a reference storage API with the hosted space and a
// reference site whose minted tokens it grants, and returns the
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
	grantMints(site, g)
	env, root := cliRoots(t)
	env = append(env,
		// A directory login creates, 0700 whatever the umask.
		credentials.DirEnv+"="+filepath.Join(t.TempDir(), "cfg"),
		"SLIVINGDOC_SITE="+site.URL(),
		"SLIVINGDOC_BUCKET=",
	)
	return g, site, env, root
}

var teamNotes = sitetest.Space{Name: hostedSpace, Owner: loginAccount, Access: "write"}

// approve scripts the site's next approval to issue key for the gateway
// after pending polls; the key reaches the hosted space only.
func approve(site *sitetest.Site, g *gatewaytest.Gateway, key, access string, expires time.Time, pending ...string) {
	site.SetSpaces(key, teamNotes)
	site.Next(sitetest.Script{Pending: pending, Issue: sitetest.Issue{
		Key: key, Access: access, Endpoint: g.URL(), ExpiresAt: expires, Account: loginAccount,
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
	if strings.Contains(stdout+stderr, loginKey) || strings.Contains(stdout+stderr, secondKey) {
		t.Fatal("login printed the key")
	}
	if strings.Contains(stderr, "Store this login?") {
		t.Fatal("login asked a question without a terminal")
	}
}

// loggedIn is the login result line for a key of the gateway, which is
// never the default endpoint, so the line names it.
func loggedIn(g *gatewaytest.Gateway, access, rest string) string {
	return "Logged in as ada@example.test at " + g.URL() + " (" + access + ") " + rest
}

// withDefault is the result line's end for a login whose only space,
// team-notes, became the default.
const withDefault = `until 2026-12-26 09:00 UTC; default space "team-notes"`

// assertKeyStayedAtTheSite proves no storage request carried a login key
// and every token the gateway saw was minted by the site, and that no
// minted token reached the credentials file.
func assertKeyStayedAtTheSite(t *testing.T, g *gatewaytest.Gateway, site *sitetest.Site, env []string) {
	t.Helper()
	used := g.Used()
	if used[loginKey] != 0 || used[secondKey] != 0 {
		t.Fatalf("space requests per token = %v; a login key reached the storage endpoint", used)
	}
	var minted []string
	for _, m := range site.Mints() {
		minted = append(minted, m.Token)
	}
	for token := range used {
		if !slices.Contains(minted, token) {
			t.Fatalf("the gateway saw a token the site never minted")
		}
	}
	data, err := os.ReadFile(credentialsPath(env))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range minted {
		if strings.Contains(string(data), token) {
			t.Fatal("a minted token reached the credentials file")
		}
	}
}

// TestScenarioLoginThenPullAndCommit proves the login workflow end to end:
// a browser approval whose polls see authorization_pending and slow_down
// stores the account key with its only space as the default, the failing
// browser is not fatal, and later pull, commit and serve processes use
// hosted storage with neither SLIVINGDOC_TOKEN nor --space, through tokens
// the site mints from the key, which itself never reaches the storage
// endpoint.
func TestScenarioLoginThenPullAndCommit(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, loginKey, "write", loginExpiry, "authorization_pending", "slow_down", "authorization_pending")
	code, stdout, stderr := runCLI(t, "real", env, "login")
	want := loggedIn(g, "read and write", withDefault) + "\n"
	if code != 0 || stdout != want {
		t.Fatalf("login = exit %d, stdout %q; want %q; stderr: %s", code, stdout, want, stderr)
	}
	if !strings.Contains(stderr, "Could not open a browser") || !strings.Contains(stderr, "open the page yourself") ||
		!strings.Contains(stderr, "    team-notes (read and write), owned by ada@example.test\n") {
		t.Fatalf("login stderr = %q, want the browser fallback and the space list", stderr)
	}
	if starts := site.Starts(); len(starts) != 1 || starts[0].Space != "" || starts[0].Access != "write" || starts[0].Client == "" {
		t.Fatalf("start requests = %+v, want one without a space, with a client label", starts)
	}
	if polls := site.Polls(); polls != 4 {
		t.Fatalf("token polls = %d, want 4 (three waits, then the key)", polls)
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
	if mints := site.Mints(); len(mints) != 3 || mints[0].Space != hostedSpace || mints[0].Key != loginKey {
		t.Fatalf("mints = %+v, want one per process for the default space", mints)
	}
	assertKeyStayedAtTheSite(t, g, site, env)
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
	g, site, env, root := loginEnv(t)
	const second = "second-space"
	g.AddSpace(second, 1<<20)
	approve(site, g, loginKey, "write", loginExpiry)
	site.SetSpaces(loginKey, teamNotes, sitetest.Space{Name: second, Owner: "bob@example.test", Access: "read"})
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")

	notes := filepath.Join(root, "notes")
	code, stdout, stderr := runCLI(t, "real", env, "pull", notes)
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "has no default space") ||
		!strings.Contains(stderr, "'slivingdoc space <name>'") {
		t.Fatalf("pull without a default space = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}

	runCLIExact(t, "real", env,
		"  team-notes (read and write), owned by ada@example.test\n  second-space (read only), owned by bob@example.test\n",
		"space")
	code, _, stderr = runCLI(t, "real", env, "space", "missing-space")
	if code != 1 || !strings.Contains(stderr, `does not reach space "missing-space" (it reaches team-notes, second-space)`) {
		t.Fatalf("space for a space the login does not reach = exit %d, stderr %s", code, stderr)
	}
	runCLIExact(t, "real", env, "The default space is now team-notes (read and write), owned by ada@example.test\n", "space", hostedSpace)
	runCLIExact(t, "real", env,
		"* team-notes (read and write), owned by ada@example.test\n  second-space (read only), owned by bob@example.test\n",
		"space")
	// The space of another owner is reachable too, read only.
	runCLIOK(t, "real", env, nil, "pull", "--space", second, filepath.Join(root, "second"))

	site.SetMintLifetime(time.Second)
	h := spawnHelper(t, "real", env, "serve")
	cs := h.connectClient(t)
	assertProcessCallOK(t, cs, toolPull, notes, "")
	before := len(site.Mints())
	time.Sleep(900 * time.Millisecond)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "after a renewal\n")
	assertProcessCallOK(t, cs, toolCommit, notes, "after a renewal")
	if after := len(site.Mints()); after <= before {
		t.Fatalf("mints = %d before and %d after 80 %% of the lifetime, want a renewal", before, after)
	}

	mints := site.Mints()
	refused := mints[len(mints)-1].Token
	g.Revoke(refused)
	writeCLIFile(t, filepath.Join(notes, "b.md"), "after a refusal\n")
	assertProcessCallOK(t, cs, toolCommit, notes, "after a refusal")
	if after := site.Mints(); len(after) <= len(mints) {
		t.Fatalf("mints = %d after the gateway refused the token, want another", len(after))
	}
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("serve exit = %d; stderr: %s", code, h.stderrText(t))
	}
	assertKeyStayedAtTheSite(t, g, site, env)

	code, stdout, stderr = runCLI(t, "real", env, "logout")
	if code != 0 || stdout != "Logged out of ada@example.test at "+g.URL()+"; the login key and its tokens were revoked\n" {
		t.Fatalf("logout = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	revoked := site.Revoked()
	if len(revoked) == 0 || revoked[0] != loginKey {
		t.Fatalf("revoked = %v, want the key first", revoked)
	}
	for _, m := range site.Mints() {
		if m.Token != refused && !slices.Contains(revoked, m.Token) {
			t.Fatalf("revoked = %v; a minted token of the key survived the logout", revoked)
		}
	}
	code, _, stderr = runCLI(t, "real", env, "space")
	if code != 1 || !strings.Contains(stderr, "not logged in") {
		t.Fatalf("space after logout = exit %d, stderr %s", code, stderr)
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
			code, _, stderr = runCLI(t, "real", env, "pull", "--storage", "hosted", "--space", hostedSpace, filepath.Join(root, "notes"))
			if code != 1 || !strings.Contains(stderr, "needs SLIVINGDOC_TOKEN or a stored login") {
				t.Fatalf("pull after a failed login = exit %d, stderr %q; want the missing-login refusal", code, stderr)
			}
			if g.Requests() != 0 {
				t.Fatal("a process without a login reached the storage API")
			}
			assertNothingStored(t, env)
		})
	}
}

// TestScenarioStorageSelection proves --storage and its automatic choice
// from the outside: SLIVINGDOC_TOKEN beats a stored login and is refused
// only beside an S3 endpoint; a login beside a named bucket plus S3
// settings is refused as ambiguous until --storage decides; s3 never
// sends a token or a login to the hosted API; hosted without a login is
// refused; and a login is only used for its own endpoint.
func TestScenarioStorageSelection(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")
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
			want: []string{"a stored login and S3 settings (AWS_ACCESS_KEY_ID", `for "team-notes"`, "--storage hosted or --storage s3"},
		},
		{
			name: "a login and the shared AWS files are ambiguous for an explicit bucket",
			env:  with("HOME=" + awsHome),
			args: []string{"--bucket", hostedSpace},
			want: []string{"a stored login and S3 settings", "~/.aws/config", "--storage hosted or --storage s3"},
		},
		{
			name: "a login and --region are ambiguous for an explicit space",
			args: []string{"--space", hostedSpace, "--region", "eu-north-1"},
			want: []string{"a stored login and S3 settings (--region)", "--storage hosted or --storage s3"},
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
			name: "an endpoint without a login keeps S3",
			env:  with(append([]string{"SLIVINGDOC_BUCKET=" + hostedSpace}, s3...)...),
			args: []string{"--endpoint", closedS3},
			want: []string{"S3 compatibility probe failed"},
		},
		{
			name: "an endpoint without a login never borrows the default space as a bucket",
			env:  with(s3...),
			args: []string{"--endpoint", closedS3},
			want: []string{"bucket is required"},
		},
		{
			name: "hosted at an endpoint without a login",
			args: []string{"--storage", "hosted", "--endpoint", closedS3},
			want: []string{"needs SLIVINGDOC_TOKEN or a stored login for " + closedS3, "the stored login is for " + g.URL()},
		},
		{
			name: "hosted without a login",
			env:  with(credentials.DirEnv + "=" + t.TempDir()),
			args: []string{"--storage", "hosted", "--space", hostedSpace},
			want: []string{"needs SLIVINGDOC_TOKEN or a stored login", "run 'slivingdoc login'"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			rowEnv := row.env
			if rowEnv == nil {
				rowEnv = env
			}
			before := g.Requests()
			mints := len(site.Mints())
			code, stdout, stderr := runCLI(t, "real", rowEnv, append([]string{"pull"}, append(row.args, notes)...)...)
			if code != 1 || strings.TrimSpace(stdout) != "" {
				t.Fatalf("pull = exit %d, stdout %q; want a startup refusal; stderr %s", code, stdout, stderr)
			}
			for _, want := range row.want {
				if !strings.Contains(stderr, want) {
					t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
				}
			}
			if strings.Contains(stderr, hostedToken) || strings.Contains(stderr, loginKey) {
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

	// Choosing hosted resolves the ambiguity with the same AWS settings,
	// and the stored default space needs no choice: S3 has no bucket to
	// use.
	runCLIOK(t, "real", with(append([]string{"SLIVINGDOC_BUCKET=" + hostedSpace}, s3...)...), nil, "pull", "--storage", "hosted", notes)
	code, stdout, stderr := runCLI(t, "real", with(s3...), "pull", notes)
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

// TestScenarioLoginSpaceRules proves which space a login process uses:
// the stored default, else --space or SLIVINGDOC_SPACE when given; that a
// space the login does not reach is refused at the site before any
// storage request; and that SLIVINGDOC_TOKEN beside a login is used alone,
// with its own space.
func TestScenarioLoginSpaceRules(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	const second = "second-space"
	g.AddSpace(second, 1<<20)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")
	site.SetSpaces(loginKey, teamNotes, sitetest.Space{Name: second, Owner: loginAccount, Access: "write"})
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }

	runCLIOK(t, "real", with("SLIVINGDOC_SPACE="+second), nil, "pull", filepath.Join(root, "second"))
	if mints := site.Mints(); len(mints) != 1 || mints[0].Space != second {
		t.Fatalf("mints = %+v, want one for the space SLIVINGDOC_SPACE named", mints)
	}

	before := g.Requests()
	code, stdout, stderr := runCLI(t, "real", env, "pull", "--space", "other-space", filepath.Join(root, "other"))
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
	runCLIOK(t, "real", with("SLIVINGDOC_TOKEN="+otherToken, "SLIVINGDOC_ENDPOINT="+g.URL()), nil, "pull", filepath.Join(root, "token"))
	if used := g.Used(); used[otherToken] == 0 || len(site.Mints()) != mints {
		t.Fatalf("space requests per token = %v after %d mints, want the variable's token alone", used, len(site.Mints())-mints)
	}
}

// TestScenarioExpiredLoginIsRefused proves a stored login past its expiry
// refuses startup with the fix, before any request reaches the site or
// the storage API.
func TestScenarioExpiredLoginIsRefused(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, loginKey, "write", time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	runLogin(t, env, site, loggedIn(g, "read and write", `until 2001-01-01 00:00 UTC; default space "team-notes"`), "--no-browser")
	code, stdout, stderr := runCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
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
	g, site, env, root := loginEnv(t)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")

	// A 401 that does not say the key is invalid is not proof that it is
	// gone, so the login stays stored and still works.
	site.RefuseRevoke(http.StatusUnauthorized, "unauthorized")
	code, stdout, stderr := runCLI(t, "real", env, "logout")
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "could not be revoked and stays stored") {
		t.Fatalf("logout refused with another 401 = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	runCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "kept"))

	code, stdout, stderr = runCLI(t, "real", env, "logout")
	if code != 0 || stdout != "Logged out of ada@example.test at "+g.URL()+"; the login key and its tokens were revoked\n" {
		t.Fatalf("logout = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 2 || got[0] != loginKey || got[1] != site.Mints()[0].Token {
		t.Fatalf("revoked = %v, want the key and the token minted from it", got)
	}
	code, _, stderr = runCLI(t, "real", env, "pull", "--storage", "hosted", "--space", hostedSpace, filepath.Join(root, "notes"))
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
// another account (revoked unless --force, and the replaced key of another
// account is never revoked), and a login for an endpoint another site's
// login holds (revoked even with --force).
func TestScenarioLoginGuardsWhatIsStored(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")
	notes := filepath.Join(root, "notes")

	eve := sitetest.Script{Issue: sitetest.Issue{Key: secondKey, Access: "write", Endpoint: g.URL(), ExpiresAt: loginExpiry, Account: "eve@example.test"}}
	site.SetSpaces(secondKey, teamNotes)
	site.Next(eve)
	code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, "Approved by eve@example.test") ||
		!strings.Contains(stderr, "approved by ada@example.test, this one by eve@example.test") ||
		!strings.Contains(stderr, "--force") {
		t.Fatalf("login approved by another account = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != secondKey {
		t.Fatalf("revoked = %v, want only the refused key", got)
	}
	// The stored login is untouched: a pull still mints from ada's key.
	runCLIOK(t, "real", env, nil, "pull", notes)
	if m := site.Mints(); len(m) != 1 || m[0].Key != loginKey {
		t.Fatalf("mints = %+v, want one from the stored key", m)
	}

	site.SetSpaces(secondKey, teamNotes)
	site.Next(eve)
	code, stdout, stderr = runCLI(t, "real", env, "login", "--no-browser", "--force")
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
	other.SetSpaces(loginKey, teamNotes)
	other.Next(sitetest.Script{Issue: sitetest.Issue{Key: loginKey, Access: "read", Endpoint: g.URL(), ExpiresAt: loginExpiry, Account: "eve@example.test"}})
	otherEnv := append(append([]string(nil), env...), "SLIVINGDOC_SITE="+other.URL())
	code, _, stderr = runCLI(t, "real", otherEnv, "login", "--no-browser", "--force")
	if code != 1 || !strings.Contains(stderr, "was issued by "+site.URL()+", not "+other.URL()) ||
		!strings.Contains(stderr, "log out of that login first") {
		t.Fatalf("login through another site = exit %d, stderr %s", code, stderr)
	}
	if got := other.Revoked(); len(got) != 1 || got[0] != loginKey {
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
	g, site, env, root := loginEnv(t)
	g2 := gatewaytest.Start(t)
	g2.AddSpace(hostedSpace, 1<<20)
	grantMints(site, g, g2)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")
	approve(site, g2, secondKey, "read", loginExpiry)
	runLogin(t, env, site, loggedIn(g2, "read only", withDefault), "--no-browser")

	code, stdout, stderr := runCLI(t, "real", env, "pull", filepath.Join(root, "either"))
	if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "several stored logins match") ||
		!strings.Contains(stderr, "pass --endpoint") {
		t.Fatalf("pull with two logins = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	code, _, stderr = runCLI(t, "real", env, "space")
	if code != 1 || !strings.Contains(stderr, "pass --endpoint") {
		t.Fatalf("space with two logins = exit %d, stderr %s", code, stderr)
	}
	runCLIOK(t, "real", env, nil, "pull", "--endpoint", g2.URL(), filepath.Join(root, "second"))
	runCLIExact(t, "real", env, "* team-notes (read only), owned by ada@example.test\n", "space", "--endpoint", g2.URL())
	if mints := site.Mints(); len(mints) != 1 || mints[0].Key != secondKey || mints[0].Endpoint != g2.URL() || mints[0].Access != "read" {
		t.Fatalf("mints = %+v, want one read token of the second login", mints)
	}
	if g.Requests() != 0 {
		t.Fatal("the first endpoint saw a request meant for the second")
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
			approve(site, g, loginKey, "write", loginExpiry)
			runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")
			row.plant(t, credentialsPath(env))
			code, stdout, stderr := runCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
			if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, row.want) {
				t.Fatalf("pull with %s = exit %d, stdout %q, stderr %s; want a refusal naming %q", row.name, code, stdout, stderr, row.want)
			}
			if g.Requests() != 0 || len(site.Mints()) != 0 {
				t.Fatalf("pull with %s reached the site or the storage API", row.name)
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

// TestScenarioEarlierCredentialsFile proves a credentials file an earlier
// build wrote, with one token per space, refuses startup and the space
// command with the fix, and that a new login replaces it without sending
// its tokens anywhere.
func TestScenarioEarlierCredentialsFile(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	writeCLIFile(t, credentialsPath(env), `{"version":1,"logins":[{"endpoint":"`+g.URL()+`","space":"team-notes","site":"`+
		site.URL()+`","token":"`+hostedToken+`","access":"write"}]}`)
	mustDo(t, os.Chmod(credentialsDir(env), 0o700))
	mustDo(t, os.Chmod(credentialsPath(env), 0o600))
	for _, args := range [][]string{{"pull", filepath.Join(root, "notes")}, {"space"}, {"logout"}} {
		code, stdout, stderr := runCLI(t, "real", env, args...)
		if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "from an earlier slivingdoc; run 'slivingdoc login' again") ||
			strings.Contains(stderr, hostedToken) {
			t.Fatalf("%v with an earlier file = exit %d, stdout %q, stderr %s", args, code, stdout, stderr)
		}
	}
	approve(site, g, loginKey, "write", loginExpiry)
	code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
	if code != 0 || stdout != loggedIn(g, "read and write", withDefault)+"\n" ||
		!strings.Contains(stderr, "The credentials file of an earlier slivingdoc was replaced; its tokens were not revoked") {
		t.Fatalf("login over an earlier file = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if g.Requests() != 0 || len(site.Revoked()) != 0 {
		t.Fatal("the earlier file's token was sent somewhere")
	}
	runCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "notes"))
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
			g, site, env, _ := loginEnv(t)
			script := row.script
			script.Issue = sitetest.Issue{Key: loginKey, Access: "write", Endpoint: g.URL(), Account: loginAccount}
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
// issued the key it never delivered, and stores nothing.
func TestScenarioLoginInterruptedMidPoll(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to another process is not implemented on Windows")
	}
	g, site, env, _ := loginEnv(t)
	site.Next(sitetest.Script{Stall: true, Issue: sitetest.Issue{Key: loginKey, Access: "write", Endpoint: g.URL(), Account: loginAccount}})
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
			setup:   func(site *sitetest.Site, g *gatewaytest.Gateway) { approve(site, g, loginKey, "write", loginExpiry) },
			args:    []string{"--read-only"},
			want:    "issued a read and write login for a read-only login; nothing was stored",
			revoked: true,
		},
		{
			name:    "a key that does not reach the space",
			setup:   func(site *sitetest.Site, g *gatewaytest.Gateway) { approve(site, g, loginKey, "write", loginExpiry) },
			args:    []string{"--space", "other-space"},
			want:    `does not reach space "other-space" (it reaches team-notes); nothing was stored`,
			revoked: true,
		},
		{
			name: "a code with a vowel",
			setup: func(site *sitetest.Site, g *gatewaytest.Gateway) {
				approve(site, g, loginKey, "write", loginExpiry)
				site.SetUserCode("BCDA-GHJK")
			},
			want: "the user code is not two groups of four consonants",
		},
		{
			name: "a filled-in page for another code",
			setup: func(site *sitetest.Site, g *gatewaytest.Gateway) {
				approve(site, g, loginKey, "write", loginExpiry)
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
			if row.revoked && (len(revoked) != 1 || revoked[0] != loginKey) {
				t.Fatalf("revoked = %v, want the unrequested key", revoked)
			}
			if !row.revoked && (len(revoked) != 0 || site.Polls() != 0) {
				t.Fatalf("revoked = %v after %d polls, want no poll at all", revoked, site.Polls())
			}
			assertNothingStored(t, env)
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
	g, site, env, _ := loginEnv(t)
	site.SetOrigin(sitelogin.DefaultSite)
	env = append(env, "SLIVINGDOC_SITE=", helperSiteRouteEnv+"="+site.URL())

	approve(site, g, loginKey, "write", loginExpiry)
	code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, sitelogin.DefaultSite+" issued a login for "+g.URL()+", not "+app.DefaultHostedEndpoint) {
		t.Fatalf("default-site login for another endpoint = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != loginKey {
		t.Fatalf("revoked = %v, want the refused key", got)
	}
	assertNothingStored(t, env)

	site.SetSpaces(secondKey, teamNotes)
	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Key: secondKey, Access: "write", Endpoint: app.DefaultHostedEndpoint, ExpiresAt: loginExpiry, Account: loginAccount,
	}})
	code, stdout, stderr = runCLI(t, "real", env, "login", "--no-browser")
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
	g, site, env, root := loginEnv(t)
	g2 := gatewaytest.Start(t)
	g2.AddSpace(hostedSpace, 1<<20)
	grantMints(site, g, g2)
	issue := func(key string, gw *gatewaytest.Gateway) sitetest.Script {
		site.SetSpaces(key, teamNotes)
		return sitetest.Script{Issue: sitetest.Issue{Key: key, Access: "write", Endpoint: gw.URL(), ExpiresAt: loginExpiry, Account: loginAccount}}
	}
	site.Queue(issue(loginKey, g), issue(secondKey, g2))
	site.HoldIssues(2)
	t.Run("logins", func(t *testing.T) {
		for _, name := range []string{"first", "second"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				code, stdout, stderr := runCLI(t, "real", env, "login", "--no-browser")
				if code != 0 || !strings.HasPrefix(stdout, "Logged in as ") {
					t.Fatalf("concurrent login = exit %d, stdout %q, stderr %s", code, stdout, stderr)
				}
			})
		}
	})
	for i, gw := range []*gatewaytest.Gateway{g, g2} {
		runCLIOK(t, "real", env, nil, "pull", "--endpoint", gw.URL(), filepath.Join(root, []string{"first", "second"}[i]))
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

// TestScenarioOldServerTakesTheStoredDefaultSpace proves that against a
// server without the token lookup, SLIVINGDOC_TOKEN with no bucket uses
// the default space stored for the same endpoint, and that without one
// for that endpoint it is a refusal naming the fix.
func TestScenarioOldServerTakesTheStoredDefaultSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")
	g.DisableTokenLookup()
	with := func(extra ...string) []string { return append(append([]string(nil), env...), extra...) }

	const envToken = "sld_5555555555555555_b2xkLXNlcnZlci10b2tlbi1iZXNpZGUtdGhlLWxvZ2luLXh4eA"
	g.Grant(envToken, hostedSpace, false)
	code, stdout, stderr := runCLI(t, "real", with("SLIVINGDOC_TOKEN="+envToken, "SLIVINGDOC_ENDPOINT="+g.URL()), "pull", filepath.Join(root, "same"))
	if code != 0 || !strings.HasPrefix(stdout, "OK  generation ") ||
		!strings.Contains(stderr, "space="+hostedSpace) || !strings.Contains(stderr, `from="default space"`) {
		t.Fatalf("pull on an old server with a default space for its endpoint = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if used := g.Used(); used[envToken] == 0 || len(site.Mints()) != 0 {
		t.Fatalf("space requests per token = %v, want the variable's token alone", used)
	}

	other := gatewaytest.Start(t)
	other.AddSpace(hostedSpace, 1<<20)
	other.Grant(envToken, hostedSpace, false)
	other.DisableTokenLookup()
	code, stdout, stderr = runCLI(t, "real", with("SLIVINGDOC_TOKEN="+envToken, "SLIVINGDOC_ENDPOINT="+other.URL()), "pull", filepath.Join(root, "other"))
	if code != 1 || strings.TrimSpace(stdout) != "" ||
		!strings.Contains(stderr, "no default space is stored for "+other.URL()) ||
		!strings.Contains(stderr, "pass the space name as --space or SLIVINGDOC_SPACE") ||
		strings.Contains(stderr, envToken) || strings.Contains(stderr, loginKey) {
		t.Fatalf("pull on an old server without a default space for it = exit %d, stdout %q, stderr %s", code, stdout, stderr)
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
