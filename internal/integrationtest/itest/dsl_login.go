package itest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

const (
	LoginAccount = "ada@example.test"
	// loginKey and secondKey are account login keys the reference site
	// issues. A key never reaches a storage endpoint: the site trades it
	// for short-lived space tokens, which the gateway grants as they are
	// minted (grantMints).
	LoginKey  = "sld_1010101010101010_YWNjb3VudC1sb2dpbi1rZXktZm9yLWludGVncmF0aW9uLXRlc3Q"
	SecondKey = "sld_2020202020202020_c2Vjb25kLWFjY291bnQtbG9naW4ta2V5LWZvci1zY2VuYXJpb3M"
	// closedS3 is a loopback address nothing listens on, with one SDK
	// attempt: a process that picks S3 fails its probe at once, without
	// ever reaching the gateway.
	ClosedS3 = "http://127.0.0.1:1"
)

// grantMints makes every token site mints usable on the gateway of its
// endpoint, as the real site's rows are for the real gateway.
func GrantMints(site *sitetest.Site, gateways ...*gatewaytest.Gateway) {
	site.OnMint(func(m sitetest.Minted) {
		for _, g := range gateways {
			if g.URL() == m.Endpoint {
				g.GrantAs(m.Token, m.SpaceID, m.Space, m.Access == "read")
			}
		}
	})
}

// loginEnv starts a reference storage API with the hosted space and a
// reference site whose minted tokens it grants, and returns the
// environment of CLI processes that share one credentials directory, the
// shared workspace root, and both servers. No process in it carries
// SLIVINGDOC_TOKEN or SLIVINGDOC_BUCKET.
func LoginEnv(t *testing.T) (*gatewaytest.Gateway, *sitetest.Site, []string, string) {
	t.Helper()
	g := gatewaytest.Start(t)
	g.AddSpace(HostedSpace, 1<<20)
	g.Grant(HostedToken, HostedSpace, false)
	g.Grant(HostedReader, HostedSpace, true)
	site := sitetest.Start(t)
	GrantMints(site, g)
	env, root := CliRoots(t)
	env = append(
		env,
		// A directory login creates, 0700 whatever the umask.
		credentials.DirEnv+"="+filepath.Join(t.TempDir(), "cfg"),
		"SLIVINGDOC_SITE="+site.URL(),
		"SLIVINGDOC_BUCKET=",
	)
	return g, site, env, root
}

var TeamNotes = sitetest.Space{Name: HostedSpace, Owner: LoginAccount, Access: "write"}

// approve scripts the site's next approval to issue key for the gateway
// after pending polls; the key reaches the hosted space only.
func Approve(site *sitetest.Site, g *gatewaytest.Gateway, key, access string, expires time.Time, pending ...string) {
	site.SetSpaces(key, TeamNotes)
	site.Next(sitetest.Script{Pending: pending, Issue: sitetest.Issue{
		Key: key, Access: access, Endpoint: g.URL(), ExpiresAt: expires, Account: LoginAccount,
	}})
}

var LoginExpiry = time.Date(2026, 12, 26, 9, 0, 0, 0, time.UTC)

// runLogin runs one login process and asserts it succeeded with the
// result line on stdout, and the approval page and code on stderr.
func RunLogin(t *testing.T, env []string, site *sitetest.Site, want string, args ...string) {
	t.Helper()
	code, stdout, stderr := RunCLI(t, "real", env, append([]string{"login"}, args...)...)
	if code != 0 || stdout != want+"\n" {
		t.Fatalf("login = exit %d, stdout %q; want 0 and %q; stderr: %s", code, stdout, want, stderr)
	}
	for _, line := range []string{
		"Logging in through " + site.URL() + " (from SLIVINGDOC_SITE).",
		"BCDF-GHJK", site.URL() + "/cli/login#BCDF-GHJK", "(or open " + site.URL() + "/cli/login and enter the code)",
		"your own terminal", "Approved by " + LoginAccount,
	} {
		if !strings.Contains(stderr, line) {
			t.Fatalf("login stderr = %q, want it to contain %q", stderr, line)
		}
	}
	if strings.Contains(stdout+stderr, LoginKey) || strings.Contains(stdout+stderr, SecondKey) {
		t.Fatal("login printed the key")
	}
	if strings.Contains(stderr, "Store this login?") {
		t.Fatal("login asked a question without a terminal")
	}
}

// loggedIn is the login result line for a key of the gateway, which is
// never the default endpoint, so the line names it.
func LoggedIn(g *gatewaytest.Gateway, access, rest string) string {
	return "Logged in as ada@example.test at " + g.URL() + " (" + access + ") " + rest
}

// withDefault is the result line's end for a login whose only space,
// team-notes, became the default.
const WithDefault = `until 2026-12-26 09:00 UTC; default space "team-notes"`

// assertKeyStayedAtTheSite proves no storage request carried a login key
// and every token the gateway saw was minted by the site, and that no
// minted token reached the credentials file.
func AssertKeyStayedAtTheSite(t *testing.T, g *gatewaytest.Gateway, site *sitetest.Site, env []string) {
	t.Helper()
	used := g.Used()
	if used[LoginKey] != 0 || used[SecondKey] != 0 {
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
	data, err := os.ReadFile(CredentialsPath(env))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range minted {
		if strings.Contains(string(data), token) {
			t.Fatal("a minted token reached the credentials file")
		}
	}
}

// credentialsDir is the credentials directory env gives every process.
func CredentialsDir(env []string) string {
	dir := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, credentials.DirEnv+"="); ok {
			dir = v
		}
	}
	return dir
}

// credentialsPath is the credentials file of env's processes.
func CredentialsPath(env []string) string {
	return filepath.Join(CredentialsDir(env), credentials.FileName)
}

func AssertPerm(t *testing.T, path string, want fs.FileMode) {
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
func AssertNothingStored(t *testing.T, env []string) {
	t.Helper()
	if _, err := os.Stat(CredentialsPath(env)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("credentials file after a failed login: %v, want none", err)
	}
}

func MustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
