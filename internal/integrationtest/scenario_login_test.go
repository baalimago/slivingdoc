package integrationtest

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
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
	for _, line := range []string{"BCDF-GHJK", site.URL() + "/cli/login?code=BCDF-GHJK", "your own terminal"} {
		if !strings.Contains(stderr, line) {
			t.Fatalf("login stderr = %q, want it to contain %q", stderr, line)
		}
	}
	if strings.Contains(stdout+stderr, hostedToken) || strings.Contains(stdout+stderr, hostedReader) {
		t.Fatal("login printed the token")
	}
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
	code, stdout, stderr := runCLI(t, "real", env, "login", "--bucket", hostedSpace)
	want := `Logged in as ada@example.test to space "team-notes" (read and write) until 2026-12-26 09:00 UTC` + "\n"
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
}

// TestScenarioLoginAgainRevokesTheOldToken proves a second login for the
// same space replaces the stored token and revokes the old one, names the
// space owner when someone else owns it, and that the replacement is what
// later processes send: a read-only token refuses the commit.
func TestScenarioLoginAgainRevokesTheOldToken(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, `Logged in as ada@example.test to space "team-notes" (read and write) until 2026-12-26 09:00 UTC`)

	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Token: hostedReader, Space: hostedSpace, Access: "read", Endpoint: g.URL(), ExpiresAt: loginExpiry,
		Account: loginAccount, Owner: loginOwner,
	}})
	runLogin(t, env, site,
		`Logged in as ada@example.test to space "team-notes" (read only) until 2026-12-26 09:00 UTC, owned by bob@example.test`,
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
// from the outside: SLIVINGDOC_TOKEN beats a stored login; a login plus
// AWS settings is refused as ambiguous until --storage decides; s3 never
// sends a token or a login to the hosted API; hosted without a login is
// refused; and a login is only used for the endpoint it was issued for.
func TestScenarioStorageSelection(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, `Logged in as ada@example.test to space "team-notes" (read and write) until 2026-12-26 09:00 UTC`, "--no-browser")
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
			name: "a login and AWS settings are ambiguous",
			env:  with(s3...),
			want: []string{`stored login for space "team-notes"`, "AWS_ACCESS_KEY_ID", "--storage hosted or --storage s3"},
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
			want: []string{"needs SLIVINGDOC_TOKEN or a stored login", "slivingdoc login --bucket team-notes"},
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

	// Choosing hosted resolves the ambiguity with the same AWS settings.
	runCLIOK(t, "real", with(s3...), nil, "pull", "--storage", "hosted", notes)
}

// TestScenarioExpiredLoginIsRefused proves a stored login past its expiry
// refuses startup with the fix, before any request reaches the storage
// API.
func TestScenarioExpiredLoginIsRefused(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	runLogin(t, env, site, `Logged in as ada@example.test to space "team-notes" (read and write) until 2001-01-01 00:00 UTC`, "--no-browser")
	code, stdout, stderr := runCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
	if code != 1 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("pull with an expired login = exit %d, stdout %q; want a startup refusal", code, stdout)
	}
	for _, want := range []string{"stored login expired", "run 'slivingdoc login --bucket team-notes'"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
	if g.Requests() != 0 {
		t.Fatal("an expired login reached the storage API")
	}
}

// TestScenarioLogout proves logout revokes the default login's token at
// the site and removes it, so hosted storage is refused afterwards, and
// that logging out again is a refusal.
func TestScenarioLogout(t *testing.T) {
	t.Parallel()
	g, site, env, root := loginEnv(t)
	approve(site, g, hostedToken, "write", loginExpiry)
	runLogin(t, env, site, `Logged in as ada@example.test to space "team-notes" (read and write) until 2026-12-26 09:00 UTC`, "--no-browser")

	code, stdout, stderr := runCLI(t, "real", env, "logout")
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
