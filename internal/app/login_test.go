package app

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"golang.org/x/term"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// loginRig is one login, logout or space invocation's injected
// environment.
type loginRig struct {
	site    *sitetest.Site
	dir     string
	out     strings.Builder
	errOut  strings.Builder
	opened  []string
	openErr error
	// terminal is what the rig's Terminal reports, and answer what a
	// person types at the prompt.
	terminal TerminalState
	answer   io.Reader
	// doer, when set, sends the site requests instead of the default
	// client.
	doer sitelogin.Doer
}

func newLoginRig(t *testing.T) *loginRig {
	t.Helper()
	return &loginRig{site: sitetest.Start(t), dir: filepath.Join(t.TempDir(), "cfg")}
}

func (r *loginRig) opts() ProcessOptions {
	return ProcessOptions{
		Env:    []string{credentials.DirEnv + "=" + r.dir, SiteEnv + "=" + r.site.URL()},
		Stdout: &r.out,
		Stderr: &r.errOut,
		OpenBrowser: func(url string) error {
			r.opened = append(r.opened, url)
			return r.openErr
		},
		SiteClient: r.doer,
		Sleep:      func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		Hostname:   func() (string, error) { return "laptop", nil },
		Terminal:   func() TerminalState { return r.terminal },
		Stdin:      r.answer,
	}
}

func (r *loginRig) logins(t *testing.T) credentials.Set {
	t.Helper()
	file, err := credentials.Locate(func(n string) string {
		if n == credentials.DirEnv {
			return r.dir
		}
		return ""
	}, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	set, err := file.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	return set
}

// stored is the one stored login.
func (r *loginRig) stored(t *testing.T) credentials.Login {
	t.Helper()
	l, err := r.logins(t).Only()
	if err != nil {
		t.Fatalf("Only() = %v", err)
	}
	return l
}

// defaultSpace is the default space stored for endpoint, or "" for none.
func (r *loginRig) defaultSpace(t *testing.T, endpoint string) string {
	t.Helper()
	space, _ := r.logins(t).DefaultSpace(endpoint)
	return space
}

func (r *loginRig) writeFile(t *testing.T, data string) {
	t.Helper()
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, credentials.FileName), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// operation is a prepared login, logout or space command.
type operation interface {
	Run(ctx context.Context) error
}

// invoke parses args into a flag holder through bind, prepares the
// operation from the flags and the positional arguments, and runs it.
func invoke[F any](t *testing.T, flags F, bind func(*flag.FlagSet), prepare func(F, []string) (operation, error), args []string) error {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bind(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	op, err := prepare(flags, fs.Args())
	if err != nil {
		return err
	}
	return op.Run(context.Background())
}

func (r *loginRig) login(t *testing.T, args ...string) error {
	t.Helper()
	f := NewLoginFlags()
	return invoke(t, f, f.Bind, func(f *LoginFlags, _ []string) (operation, error) { return PrepareLogin(f, r.opts()) }, args)
}

func (r *loginRig) logout(t *testing.T, args ...string) error {
	t.Helper()
	f := NewLogoutFlags()
	return invoke(t, f, f.Bind, func(f *LogoutFlags, _ []string) (operation, error) { return PrepareLogout(f, r.opts()) }, args)
}

func (r *loginRig) space(t *testing.T, args ...string) error {
	t.Helper()
	f := NewSpaceFlags()
	return invoke(t, f, f.Bind, func(f *SpaceFlags, rest []string) (operation, error) {
		name := ""
		if len(rest) > 0 {
			name = rest[0]
		}
		return PrepareSpace(f, name, r.opts())
	}, args)
}

var loginExpiry = time.Date(2026, 12, 26, 8, 0, 0, 0, time.UTC)

// approved is an approval by ada@example.test that issues key after two
// pending polls.
func approved(key, access, endpoint string) sitetest.Script {
	return sitetest.Script{
		Pending: []string{"authorization_pending", "slow_down"},
		Issue:   sitetest.Issue{Key: key, Access: access, Endpoint: endpoint, ExpiresAt: loginExpiry, Account: "ada@example.test"},
	}
}

// eve is an approval by another account.
func eve(key string) sitetest.Script {
	script := approved(key, "write", DefaultHostedEndpoint)
	script.Issue.Account = "eve@example.test"
	return script
}

func space(name, owner, access string) sitetest.Space {
	return sitetest.Space{Name: name, Owner: owner, Access: access}
}

// notesSpace is the one space most tests' keys reach.
var notesSpace = space("notes", "ada@example.test", "write")

// loginOnce scripts a login of key that reaches spaces and runs it.
func (r *loginRig) loginOnce(t *testing.T, key string, spaces []sitetest.Space, args ...string) {
	t.Helper()
	r.site.SetSpaces(key, spaces...)
	r.site.Next(approved(key, "write", DefaultHostedEndpoint))
	if err := r.login(t, args...); err != nil {
		t.Fatalf("login = %v", err)
	}
	r.out.Reset()
	r.errOut.Reset()
}

func TestLoginStoresTheKeyAndItsOnlySpace(t *testing.T) {
	r := newLoginRig(t)
	r.site.SetSpaces(loginToken, notesSpace)
	r.site.Next(approved(loginToken, "write", "https://API.slivingdoc.dev/"))
	if err := r.login(t); err != nil {
		t.Fatalf("login = %v", err)
	}
	if got, want := r.out.String(), "Logged in as ada@example.test (read and write) until 2026-12-26 08:00 UTC; default space \"notes\"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	prompt := r.errOut.String()
	for _, want := range []string{
		"Logging in through " + r.site.URL() + " (from SLIVINGDOC_SITE).\n",
		"approve the code BCDF-GHJK:\n  " + r.site.URL() + "/cli/login#BCDF-GHJK\n",
		"(or open " + r.site.URL() + "/cli/login and enter the code)\n",
		"your own terminal", "Waiting for approval",
		"Approved by ada@example.test (read and write) until 2026-12-26 08:00 UTC.\n" +
			"  Storage endpoint: https://api.slivingdoc.dev\n  Site: " + r.site.URL() + "\n" +
			"  Spaces:\n    notes (read and write), owned by ada@example.test\n",
		`The default space is "notes"; 'slivingdoc space <name>' changes it.`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("stderr = %q, want it to contain %q", prompt, want)
		}
	}
	if strings.Contains(prompt, "Store this login?") {
		t.Fatalf("stderr = %q, want no prompt without a terminal", prompt)
	}
	if len(r.opened) != 1 || r.opened[0] != r.site.URL()+"/cli/login#BCDF-GHJK" {
		t.Fatalf("opened = %v, want the approval page once", r.opened)
	}
	if starts := r.site.Starts(); len(starts) != 1 || starts[0] != (sitetest.StartBody{Access: "write", Client: "laptop"}) {
		t.Fatalf("start requests = %+v, want access and client only", starts)
	}
	l := r.stored(t)
	if l.Endpoint != DefaultHostedEndpoint || l.Key != loginToken || l.Site != r.site.URL() || l.Account != "ada@example.test" {
		t.Fatalf("stored = %+v", l)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" {
		t.Fatalf("default space = %q, want notes", got)
	}
	if strings.Contains(r.out.String()+r.errOut.String(), loginToken) {
		t.Fatal("the login printed the key")
	}
}

func TestLoginChoosesTheDefaultSpace(t *testing.T) {
	two := []sitetest.Space{notesSpace, space("team", "bob@example.test", "read")}
	t.Run("several spaces leave it to the space command", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.SetSpaces(loginToken, two...)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		if err := r.login(t); err != nil {
			t.Fatalf("login = %v", err)
		}
		if r.defaultSpace(t, DefaultHostedEndpoint) != "" || strings.Contains(r.out.String(), "default space") {
			t.Fatalf("stdout = %q, want no default space", r.out.String())
		}
		if !strings.Contains(r.errOut.String(), "    team (read only), owned by bob@example.test\n") ||
			!strings.Contains(r.errOut.String(), "Run 'slivingdoc space <name>' to choose the default space") {
			t.Fatalf("stderr = %q, want the spaces and the hint", r.errOut.String())
		}
	})
	t.Run("--space sets it", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.SetSpaces(loginToken, two...)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		if err := r.login(t, "--space", "team"); err != nil {
			t.Fatalf("login = %v", err)
		}
		if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
			t.Fatalf("default space = %q, want team", got)
		}
	})
	t.Run("--space the login does not reach", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.SetSpaces(loginToken, two...)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		err := r.login(t, "--bucket", "other")
		if err == nil || !strings.Contains(err.Error(), `does not reach space "other" (it reaches notes, team); nothing was stored`) {
			t.Fatalf("login = %v, want the refusal naming the spaces", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want the unstored key", got)
		}
		if _, err := os.Stat(filepath.Join(r.dir, credentials.FileName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a refused login wrote the credentials file")
		}
	})
	t.Run("no space yet", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		if err := r.login(t); err != nil {
			t.Fatalf("login = %v", err)
		}
		if !strings.Contains(r.errOut.String(), "  Spaces: none yet\n") || !strings.Contains(r.errOut.String(), "reaches no space yet") {
			t.Fatalf("stderr = %q", r.errOut.String())
		}
		if got := r.site.Revoked(); len(got) != 0 {
			t.Fatalf("revoked = %v; a login without spaces is still stored", got)
		}
	})
	t.Run("the earlier default is kept while the login reaches it", func(t *testing.T) {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, two, "--space", "team")
		r.loginOnce(t, otherToken, two)
		if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
			t.Fatalf("default space = %q, want team kept", got)
		}
		r.site.SetSpaces(loginToken, two...)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		if err := r.login(t, "--space", "notes"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(r.errOut.String(), `The default space changed from "team" to "notes".`) {
			t.Fatalf("stderr = %q", r.errOut.String())
		}
	})
	t.Run("an earlier default the login no longer reaches is cleared", func(t *testing.T) {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, two, "--space", "team")
		r.site.SetSpaces(otherToken, notesSpace, space("more", "ada@example.test", "write"))
		r.site.Next(approved(otherToken, "write", DefaultHostedEndpoint))
		if err := r.login(t); err != nil {
			t.Fatal(err)
		}
		if r.defaultSpace(t, DefaultHostedEndpoint) != "" || !strings.Contains(r.errOut.String(), `The default space "team" is not among the login's spaces, so it was cleared.`) {
			t.Fatalf("stderr = %q, want the default cleared", r.errOut.String())
		}
	})
}

func TestLoginAgainRevokesTheReplacedKey(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.SetSpaces(otherToken, notesSpace)
	r.site.Next(approved(otherToken, "read", DefaultHostedEndpoint))
	if err := r.login(t, "--read-only", "--no-browser"); err != nil {
		t.Fatalf("second login = %v", err)
	}
	if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
		t.Fatalf("revoked = %v, want the replaced key", got)
	}
	if len(r.opened) != 1 {
		t.Fatalf("opened = %v, want no browser for --no-browser", r.opened)
	}
	if !strings.Contains(r.out.String(), "Logged in as ada@example.test (read only)") {
		t.Fatalf("stdout = %q", r.out.String())
	}
	if l := r.stored(t); l.Key != otherToken || l.Access != credentials.AccessRead {
		t.Fatalf("stored = %+v, want the replacement only", l)
	}

	// A replaced key the site already forgot counts as revoked.
	r.site.Revoke(otherToken)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	if got := r.site.Revoked(); len(got) != 1 {
		t.Fatalf("revoked = %v, want no second revocation", got)
	}
}

// failingSpaces answers every space list with a 502 and sends every other
// request to the site.
type failingSpaces struct{}

func (failingSpaces) Do(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/spaces") {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader(`{"error":"bad_gateway"}`)), Request: req}, nil
	}
	return http.DefaultClient.Do(req)
}

func TestLoginRefusals(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(sitetest.Script{Final: "access_denied"})
		err := r.login(t)
		if err == nil || !strings.Contains(err.Error(), "denied") {
			t.Fatalf("login = %v, want the denial", err)
		}
		if _, statErr := os.Stat(filepath.Join(r.dir, credentials.FileName)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("a denied login wrote the credentials file")
		}
	})
	t.Run("expired", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(sitetest.Script{Final: "expired_token"})
		if err := r.login(t); err == nil || !strings.Contains(err.Error(), "expired") {
			t.Fatalf("login = %v, want the expiry", err)
		}
	})
	t.Run("write key for a read-only login", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		err := r.login(t, "--read-only")
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("login = %v", err)
		}
		if len(r.logins(t).Logins()) != 0 {
			t.Fatal("a refused key was stored")
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want the refused key", got)
		}
	})
	t.Run("the spaces cannot be listed", func(t *testing.T) {
		r := newLoginRig(t)
		r.doer = failingSpaces{}
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		err := r.login(t)
		if err == nil || !strings.Contains(err.Error(), "list the spaces the login reaches") || !strings.Contains(err.Error(), "nothing was stored") {
			t.Fatalf("login = %v, want the listing refusal", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want the unstored key", got)
		}
		if strings.Contains(r.errOut.String(), "Approved by") {
			t.Fatal("the approval was shown before the spaces were known")
		}
	})
	t.Run("browser failure is not fatal", func(t *testing.T) {
		r := newLoginRig(t)
		r.openErr = errors.New("no display")
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		if err := r.login(t); err != nil {
			t.Fatalf("login = %v", err)
		}
		if !strings.Contains(r.errOut.String(), "Could not open a browser (no display); open the page yourself") {
			t.Fatalf("stderr = %q", r.errOut.String())
		}
	})
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"invalid space", []string{"--bucket", "Bad_Space"}, "invalid space name"},
		{"plain http site", []string{"--site", "http://www.example.test"}, "must use https"},
		{"site with a path", []string{"--site", "https://www.example.test/x"}, "origin without a path"},
		{"site not a URL", []string{"--site", "ftp://x"}, "absolute http or https"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newLoginRig(t)
			err := r.login(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("login = %v, want %q", err, tt.want)
			}
			if len(r.site.Starts()) != 0 {
				t.Fatal("a refused login contacted the site")
			}
		})
	}
	t.Run("malformed credentials file", func(t *testing.T) {
		r := newLoginRig(t)
		r.writeFile(t, "{")
		if err := r.login(t); !errors.Is(err, credentials.ErrMalformed) {
			t.Fatalf("login = %v, want ErrMalformed", err)
		}
		if err := r.logout(t); !errors.Is(err, credentials.ErrMalformed) {
			t.Fatalf("logout = %v, want ErrMalformed", err)
		}
		if err := r.space(t); !errors.Is(err, credentials.ErrMalformed) {
			t.Fatalf("space = %v, want ErrMalformed", err)
		}
		if len(r.site.Starts()) != 0 {
			t.Fatal("a refused login contacted the site")
		}
	})
	t.Run("a directory other users can write", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows has no group or other permission bits")
		}
		r := newLoginRig(t)
		if err := os.Mkdir(r.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(r.dir, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := r.login(t, "--bucket", "team-notes"); !errors.Is(err, credentials.ErrExposed) {
			t.Fatalf("login = %v, want ErrExposed", err)
		}
		if len(r.site.Starts()) != 0 || r.site.Polls() != 0 || len(r.opened) != 0 {
			t.Fatal("a refused login contacted the site or opened the browser")
		}
		if _, err := os.Stat(filepath.Join(r.dir, credentials.FileName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a refused login wrote the credentials file: %v", err)
		}
	})
	t.Run("no configuration directory", func(t *testing.T) {
		r := newLoginRig(t)
		opts := r.opts()
		opts.Env = []string{SiteEnv + "=" + r.site.URL()}
		if _, err := PrepareLogin(NewLoginFlags(), opts); !errors.Is(err, credentials.ErrNoConfigDir) {
			t.Fatalf("PrepareLogin() = %v, want ErrNoConfigDir", err)
		}
		if _, err := PrepareLogout(NewLogoutFlags(), opts); !errors.Is(err, credentials.ErrNoConfigDir) {
			t.Fatalf("PrepareLogout() = %v, want ErrNoConfigDir", err)
		}
		if _, err := PrepareSpace(NewSpaceFlags(), "", opts); !errors.Is(err, credentials.ErrNoConfigDir) {
			t.Fatalf("PrepareSpace() = %v, want ErrNoConfigDir", err)
		}
	})
}

// earlierFile is a version 1 credentials file, one token per space.
const earlierFile = `{"version":1,"logins":[{"endpoint":"https://api.slivingdoc.dev","space":"notes","site":"https://www.slivingdoc.dev","token":"` +
	otherToken + `","access":"write"}]}`

func TestLoginReplacesAnEarlierFile(t *testing.T) {
	r := newLoginRig(t)
	r.writeFile(t, earlierFile)
	if err := r.logout(t); !errors.Is(err, credentials.ErrOutdated) || !strings.Contains(err.Error(), "run 'slivingdoc login' again") {
		t.Fatalf("logout of an earlier file = %v, want ErrOutdated", err)
	}
	if err := r.space(t); !errors.Is(err, credentials.ErrOutdated) {
		t.Fatalf("space with an earlier file = %v, want ErrOutdated", err)
	}
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.writeFile(t, earlierFile)
	r.site.SetSpaces(loginToken, notesSpace)
	r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("login over an earlier file = %v", err)
	}
	if !strings.Contains(r.errOut.String(), "The credentials file of an earlier slivingdoc was replaced; its tokens were not revoked") {
		t.Fatalf("stderr = %q, want the note about the earlier tokens", r.errOut.String())
	}
	if l := r.stored(t); l.Key != loginToken {
		t.Fatalf("stored = %+v", l)
	}
	if got := r.site.Revoked(); len(got) != 0 {
		t.Fatalf("revoked = %v; the earlier file's tokens are never sent", got)
	}
}

func TestLogout(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.SetSpaces(otherToken, notesSpace)
	r.site.Next(approved(otherToken, "write", devEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("second login = %v", err)
	}
	r.out.Reset()
	if err := r.logout(t, "--site", "https://other.example.test"); !errors.Is(err, credentials.ErrNoLogin) ||
		!strings.Contains(err.Error(), "issued by https://other.example.test (from --site or SLIVINGDOC_SITE)") {
		t.Fatalf("logout for another site = %v, want ErrNoLogin", err)
	}
	if err := r.logout(t, "--site", "http://www.example.test"); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("logout with a plain http site = %v", err)
	}

	// A key the site already forgot counts as revoked.
	r.site.Revoke(otherToken)
	if err := r.logout(t, "--site", r.site.URL()+"/"); err != nil {
		t.Fatalf("logout = %v", err)
	}
	want := "Logged out of ada@example.test at https://api.slivingdoc.dev; the login key and its tokens were revoked\n" +
		"Logged out of ada@example.test at " + devEndpoint + "; the login key and its tokens were revoked\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	set := r.logins(t)
	if len(set.Logins()) != 0 {
		t.Fatalf("logins = %+v, want none", set.Logins())
	}
	if _, err := set.DefaultSpace(DefaultHostedEndpoint); !errors.Is(err, credentials.ErrNoDefault) {
		t.Fatalf("default space after logout = %v, want none", err)
	}
	if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
		t.Fatalf("revoked = %v", got)
	}
	if err := r.logout(t); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("logout without a login = %v", err)
	}
}

func TestLogoutKeepsALoginItCannotRevoke(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.Close()
	err := r.logout(t)
	if err == nil || !strings.Contains(err.Error(), "stays stored") || strings.Contains(err.Error(), loginToken) {
		t.Fatalf("logout against an unreachable site = %v", err)
	}
	if len(r.logins(t).Logins()) != 1 {
		t.Fatal("a login that was not revoked was removed")
	}
}

func TestLoginRevokesAKeyItCannotStore(t *testing.T) {
	t.Run("an answer outside the contract", func(t *testing.T) {
		r := newLoginRig(t)
		script := approved(loginToken, "write", DefaultHostedEndpoint)
		script.Issue.Account = ""
		r.site.Next(script)
		err := r.login(t)
		if err == nil || !strings.Contains(err.Error(), "no usable account") || strings.Contains(err.Error(), loginToken) {
			t.Fatalf("login = %v, want the protocol refusal without the key", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want the rejected key", got)
		}
	})
	t.Run("a credentials file that breaks while waiting", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		l, err := PrepareLogin(NewLoginFlags(), r.opts())
		if err != nil {
			t.Fatalf("PrepareLogin() = %v", err)
		}
		r.writeFile(t, "{")
		if err := l.Run(context.Background()); !errors.Is(err, credentials.ErrMalformed) {
			t.Fatalf("Run() = %v, want ErrMalformed", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want the unstored key", got)
		}
	})
	t.Run("a revocation that fails too", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		r.site.RefuseRevoke(http.StatusInternalServerError, "internal")
		err := r.login(t, "--read-only")
		if err == nil || !strings.Contains(err.Error(), "revoking the issued login key failed too") || !strings.Contains(err.Error(), "Tokens page") {
			t.Fatalf("login = %v, want both failures named", err)
		}
	})
}

func TestLoginExplainsAnUnknownHostName(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
	opts := r.opts()
	opts.Hostname = func() (string, error) { return "", errors.New("uname failed") }
	l, err := PrepareLogin(NewLoginFlags(), opts)
	if err != nil {
		t.Fatalf("PrepareLogin() = %v", err)
	}
	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.Contains(r.errOut.String(), `The host name is unknown (uname failed); the login is labelled "CLI login" without it.`) {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
	if starts := r.site.Starts(); len(starts) != 1 || starts[0].Client != "" {
		t.Fatalf("start requests = %+v, want no client label", starts)
	}
}

func TestLoginAsksOnATerminal(t *testing.T) {
	for _, tt := range []struct {
		name   string
		answer io.Reader
		stored bool
	}{
		{"yes", strings.NewReader("y\n"), true},
		{"YES with blanks", strings.NewReader("  YES  \n"), true},
		{"no", strings.NewReader("n\n"), false},
		{"nothing typed", strings.NewReader(""), false},
		{"a broken terminal", iotest.ErrReader(errors.New("input/output error")), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newLoginRig(t)
			r.terminal, r.answer = OnTerminal, tt.answer
			r.site.SetSpaces(loginToken, notesSpace)
			r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
			err := r.login(t)
			if !strings.Contains(r.errOut.String(), "Approved by ada@example.test") || !strings.Contains(r.errOut.String(), "owned by ada@example.test\nStore this login? [y/N] ") {
				t.Fatalf("stderr = %q, want the approval and the spaces shown, then the question", r.errOut.String())
			}
			if tt.stored {
				if err != nil || len(r.logins(t).Logins()) != 1 || len(r.site.Revoked()) != 0 {
					t.Fatalf("login = %v, revoked %v; want it stored", err, r.site.Revoked())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "nothing was stored") {
				t.Fatalf("login = %v, want a refusal", err)
			}
			if len(r.logins(t).Logins()) != 0 || r.out.String() != "" {
				t.Fatal("an unconfirmed login was stored")
			}
			if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
				t.Fatalf("revoked = %v, want the unconfirmed key", got)
			}
		})
	}
}

func TestLoginGuardsAnotherAccountsLogin(t *testing.T) {
	setup := func(t *testing.T) *loginRig {
		t.Helper()
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
		r.site.SetSpaces(otherToken, notesSpace)
		return r
	}
	t.Run("refused without a terminal", func(t *testing.T) {
		r := setup(t)
		r.site.Next(eve(otherToken))
		err := r.login(t)
		if err == nil || !strings.Contains(err.Error(), "approved by ada@example.test, this one by eve@example.test") ||
			!strings.Contains(err.Error(), "--force") {
			t.Fatalf("login = %v, want the account refusal", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != otherToken {
			t.Fatalf("revoked = %v, want only the new key", got)
		}
		if l := r.stored(t); l.Key != loginToken {
			t.Fatal("the refused login replaced the stored one")
		}
	})
	for _, tt := range []struct {
		name     string
		args     []string
		terminal TerminalState
	}{
		{"with --force", []string{"--force"}, NoTerminal},
		{"confirmed on a terminal", nil, OnTerminal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := setup(t)
			r.terminal, r.answer = tt.terminal, strings.NewReader("y\n")
			r.site.Next(eve(otherToken))
			if err := r.login(t, tt.args...); err != nil {
				t.Fatalf("login = %v", err)
			}
			if got := r.site.Revoked(); len(got) != 0 {
				t.Fatalf("revoked = %v; another account's key is never revoked", got)
			}
			if !strings.Contains(r.errOut.String(), "The earlier login key for https://api.slivingdoc.dev was approved by ada@example.test, not eve@example.test, so it was not revoked; revoke it on the Tokens page") {
				t.Fatalf("stderr = %q, want the note about the kept key", r.errOut.String())
			}
			if l := r.stored(t); l.Key != otherToken || l.Account != "eve@example.test" {
				t.Fatalf("stored = %+v, want the new login", l)
			}
		})
	}
}

func TestLoginRefusesWhatItDidNotAskFor(t *testing.T) {
	t.Run("a login another site issued for the endpoint", func(t *testing.T) {
		r := newLoginRig(t)
		r.dir = strings.TrimPrefix(writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, otherToken)), credentials.DirEnv+"=")
		r.site.SetSpaces(loginToken, notesSpace)
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		err := r.login(t, "--force")
		if err == nil || !strings.Contains(err.Error(), "was issued by https://www.slivingdoc.dev, not "+r.site.URL()) ||
			!strings.Contains(err.Error(), "log out of that login first (slivingdoc logout --site https://www.slivingdoc.dev)") {
			t.Fatalf("login = %v, want the site refusal", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want only the new key", got)
		}
		if l := r.stored(t); l.Key != otherToken {
			t.Fatal("the other site's login was replaced")
		}
	})
	t.Run("the default site with another endpoint", func(t *testing.T) {
		// The default port and a trailing dot still name the default site.
		client, err := siteClient("https://www.slivingdoc.dev.:443", ProcessOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if client.Site() != sitelogin.DefaultSite {
			t.Fatalf("site = %q, want the default site", client.Site())
		}
		l := &Login{client: client, access: credentials.AccessWrite}
		issued := sitelogin.Issued{Key: loginToken, Access: credentials.AccessWrite, Endpoint: devEndpoint, Account: "ada@example.test"}
		if _, err := l.accept(issued); err == nil || !strings.Contains(err.Error(), "issued a login for "+devEndpoint+", not "+DefaultHostedEndpoint) {
			t.Fatalf("accept() = %v, want the endpoint refusal", err)
		}
		issued.Endpoint = "https://API.slivingdoc.dev/"
		if got, err := l.accept(issued); err != nil || got.Endpoint != DefaultHostedEndpoint || got.Site != sitelogin.DefaultSite {
			t.Fatalf("accept() = %+v, %v", got, err)
		}
		issued.Endpoint = "ftp://x"
		if _, err := l.accept(issued); err == nil || !strings.Contains(err.Error(), "the site's storage endpoint") {
			t.Fatalf("accept() of a bad endpoint = %v", err)
		}
	})
	t.Run("the credentials lock is held", func(t *testing.T) {
		r := newLoginRig(t)
		file, err := credentialsFile(environ(r.opts().Env))
		if err != nil {
			t.Fatal(err)
		}
		lock, err := file.Lock(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Unlock()
		r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
		l, err := PrepareLogin(NewLoginFlags(), r.opts())
		if err != nil {
			t.Fatalf("PrepareLogin() = %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if err := l.Run(ctx); err == nil || !strings.Contains(err.Error(), "nothing was stored") {
			t.Fatalf("Run() = %v, want the lock to hold the login off", err)
		}
		if len(r.logins(t).Logins()) != 0 {
			t.Fatal("a login was stored while another held the lock")
		}
	})
}

func TestLoggedInNamesAnotherEndpointAndTheDefault(t *testing.T) {
	l := credentials.Login{ID: credentials.ID{Endpoint: devEndpoint}, Access: credentials.AccessRead, Account: "a@x"}
	if got, want := loggedIn(l, ""), `Logged in as a@x at `+devEndpoint+` (read only) with no expiry`; got != want {
		t.Fatalf("loggedIn() = %q, want %q", got, want)
	}
	if got, want := loggedIn(l, "notes"), `Logged in as a@x at `+devEndpoint+` (read only) with no expiry; default space "notes"`; got != want {
		t.Fatalf("loggedIn() = %q, want %q", got, want)
	}
}

func TestPlatformBrowserOnWindowsNeedsAnAbsoluteSystemRoot(t *testing.T) {
	for _, env := range []map[string]string{
		nil,
		{"SystemRoot": `Windows`},
		{"SYSTEMROOT": `relative`},
	} {
		err := platformBrowser("windows", env, "https://www.slivingdoc.dev/cli/login")
		if err == nil || !strings.Contains(err.Error(), "app: SystemRoot is not an absolute path") {
			t.Fatalf("platformBrowser(%v) = %v, want the absolute-path refusal", env, err)
		}
	}
	// The name is matched without regard to case, as Windows does; this
	// root exists nowhere, so the start itself fails.
	err := platformBrowser("windows", map[string]string{"SYSTEMROOT": filepath.Join(t.TempDir(), "win")}, "https://x")
	if err == nil || !strings.Contains(err.Error(), "app: start the browser") {
		t.Fatalf("platformBrowser(SYSTEMROOT) = %v, want the start to be tried", err)
	}
	if got := lookupFold(map[string]string{"systemroot": "a"}, "SystemRoot"); got != "a" {
		t.Fatalf("lookupFold() = %q", got)
	}
}

func TestFindOnPath(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := findOnPath("plain", "relative"+string(filepath.ListSeparator)+dir); err == nil {
		t.Fatal("findOnPath found a file that is not executable")
	}
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := findOnPath("tool", string(filepath.ListSeparator)+dir); err != nil || got != tool {
		t.Fatalf("findOnPath() = %q, %v; want %q", got, err, tool)
	}
	if err := platformBrowser("linux", map[string]string{"PATH": dir}, "https://www.slivingdoc.dev/cli/login"); err == nil ||
		!strings.Contains(err.Error(), "app: xdg-open is not on PATH") {
		t.Fatalf("platformBrowser without xdg-open = %v", err)
	}
}

func TestProcessStreamsAreTheDefaults(t *testing.T) {
	var opts ProcessOptions
	if opts.stdin() != os.Stdin {
		t.Fatal("stdin() is not the process stdin")
	}
	want := NoTerminal
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())) {
		want = OnTerminal
	}
	if got := opts.terminal(); got != want {
		t.Fatalf("terminal() = %v, want %v for the process streams", got, want)
	}
}

// signalOnPoll is a site client that delivers a termination signal as it
// sends a token poll, so the poll is in flight when the login is
// interrupted.
type signalOnPoll struct {
	sig chan os.Signal
}

func (s signalOnPoll) Do(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/token") {
		s.sig <- os.Interrupt
	}
	return http.DefaultClient.Do(req)
}

func TestLoginStopsOnASignal(t *testing.T) {
	r := newLoginRig(t)
	script := approved(loginToken, "write", DefaultHostedEndpoint)
	script.Pending, script.Stall = nil, true
	r.site.Next(script)
	opts := r.opts()
	sig := make(chan os.Signal, 1)
	opts.Signals, opts.SiteClient = sig, signalOnPoll{sig: sig}
	f := NewLoginFlags()
	err := invoke(t, f, f.Bind, func(f *LoginFlags, _ []string) (operation, error) { return PrepareLogin(f, opts) }, nil)
	if err == nil || !strings.Contains(err.Error(), "stopped while a poll was in flight") || !strings.Contains(err.Error(), sitelogin.TokenHint) {
		t.Fatalf("interrupted login = %v, want the stop with the token hint", err)
	}
	if got := r.logins(t).Logins(); len(got) != 0 {
		t.Fatalf("stored = %+v, want nothing", got)
	}
}

func TestInterruptibleWithoutSignalsWatchesTheOS(t *testing.T) {
	ctx, stop := ProcessOptions{}.interruptible(context.Background())
	if ctx.Err() != nil {
		t.Fatal("the context ended before any signal")
	}
	stop()
	if ctx.Err() == nil {
		t.Fatal("stop did not end the context")
	}
}

// interruptingStdin delivers a termination signal when the prompt reads
// the answer, then blocks as a person who never types would, until the
// test ends.
type interruptingStdin struct {
	sig   chan os.Signal
	ended chan struct{}
}

func (r interruptingStdin) Read([]byte) (int, error) {
	r.sig <- os.Interrupt
	<-r.ended
	return 0, io.EOF
}

func TestLoginInterruptedAtThePromptRevokes(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
	sig := make(chan os.Signal, 1)
	stdin := interruptingStdin{sig: sig, ended: make(chan struct{})}
	t.Cleanup(func() { close(stdin.ended) })
	opts := r.opts()
	opts.Terminal = func() TerminalState { return OnTerminal }
	opts.Stdin, opts.Signals = stdin, sig
	f := NewLoginFlags()
	err := invoke(t, f, f.Bind, func(f *LoginFlags, _ []string) (operation, error) { return PrepareLogin(f, opts) }, nil)
	if err == nil || !strings.Contains(err.Error(), "not confirmed") || strings.Contains(err.Error(), "revoking the issued login key failed") {
		t.Fatalf("login interrupted at the prompt = %v, want the refusal with the key revoked", err)
	}
	if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
		t.Fatalf("revoked = %v, want the unconfirmed key despite the interrupt", got)
	}
	if len(r.logins(t).Logins()) != 0 {
		t.Fatal("an interrupted login was stored")
	}
}

func TestLogoutKeepsANewerLogin(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	// The logout reads the login, then another process logs in again
	// before the logout removes it.
	logout, err := PrepareLogout(NewLogoutFlags(), r.opts())
	if err != nil {
		t.Fatalf("PrepareLogout() = %v", err)
	}
	r.loginOnce(t, otherToken, []sitetest.Space{notesSpace})
	if err := logout.Run(context.Background()); err != nil {
		t.Fatalf("logout = %v", err)
	}
	if want := "Revoked the login key of ada@example.test at https://api.slivingdoc.dev; a newer login for it was kept\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if got := r.stored(t); got.Key != otherToken {
		t.Fatalf("stored = %+v; want the newer login kept", got)
	}
}

// TestLoginLogoutAndSpaceNeverPrintStoredUserInformation proves a
// hand-edited credentials file whose endpoint carries user information is
// refused at load, and that no command prints the secret on the way.
func TestLoginLogoutAndSpaceNeverPrintStoredUserInformation(t *testing.T) {
	const secretEndpoint = "https://user:secret@api.example.test"
	stored := `{"site":"https://www.slivingdoc.dev","endpoint":"` + secretEndpoint + `","key":"` + loginToken + `","access":"write","account":"a@x"}`
	for _, row := range []struct {
		name string
		data string
	}{
		{"one login", `{"version":2,"logins":[` + stored + `]}`},
		{"a second login", `{"version":2,"logins":[` + stored + `,` + stored + `]}`},
		{"a default without its login", `{"version":2,"logins":[],"defaultSpaces":[{"endpoint":"` + secretEndpoint + `","space":"notes"}]}`},
	} {
		t.Run(row.name, func(t *testing.T) {
			for _, op := range []string{"login", "logout", "space"} {
				r := newLoginRig(t)
				r.writeFile(t, row.data)
				var err error
				switch op {
				case "login":
					r.site.Next(approved(hostedTestToken, "write", DefaultHostedEndpoint))
					err = r.login(t)
				case "logout":
					err = r.logout(t)
				default:
					err = r.space(t)
				}
				if !errors.Is(err, credentials.ErrMalformed) {
					t.Fatalf("%s = %v, want ErrMalformed", op, err)
				}
				for _, text := range []string{err.Error(), r.out.String(), r.errOut.String()} {
					if strings.Contains(text, "secret") || strings.Contains(text, loginToken) {
						t.Fatalf("%s printed %q, want no user information or key", op, text)
					}
				}
			}
		})
	}
}

// TestLoginTakesSpaceOrBucket proves --space and --bucket are one setting
// on login: either spelling works, and both with different values are
// refused before the site is contacted.
func TestLoginTakesSpaceOrBucket(t *testing.T) {
	r := newLoginRig(t)
	if err := r.login(t, "--bucket", "notes", "--space", "other"); err == nil || !strings.Contains(err.Error(), `--bucket "notes" and --space "other" name different spaces`) {
		t.Fatalf("login with two spaces = %v, want the refusal naming both", err)
	}
	if err := r.login(t, "--space", "No_Space"); err == nil || !strings.Contains(err.Error(), "login: --space names the hosted space") {
		t.Fatalf("login --space with an invalid name = %v, want the refusal naming --space", err)
	}
	if len(r.site.Starts()) != 0 {
		t.Fatal("a refused login contacted the site")
	}
	for _, args := range [][]string{{"--space", "team"}, {"--bucket", "team"}, {"--space", "team", "--bucket", "team"}} {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, space("team", "bob@example.test", "write")}, args...)
		if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
			t.Fatalf("login %v stored default %q, want team", args, got)
		}
	}
}

func TestSpaceListsAndSetsTheDefault(t *testing.T) {
	r := newLoginRig(t)
	team := space("team", "bob@example.test", "read")
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, team})

	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if want := "  notes (read and write), owned by ada@example.test\n  team (read only), owned by bob@example.test\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if !strings.Contains(r.errOut.String(), "No default space; run 'slivingdoc space <name>' to choose one.") {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
	r.out.Reset()
	if err := r.space(t, "team"); err != nil {
		t.Fatalf("space team = %v", err)
	}
	if want := "The default space is now team (read only), owned by bob@example.test\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
		t.Fatalf("default space = %q, want team", got)
	}
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if want := "  notes (read and write), owned by ada@example.test\n* team (read only), owned by bob@example.test\n"; r.out.String() != want || r.errOut.String() != "" {
		t.Fatalf("stdout = %q, stderr = %q; want the default marked", r.out.String(), r.errOut.String())
	}

	err := r.space(t, "other")
	if err == nil || !strings.Contains(err.Error(), `does not reach space "other" (it reaches notes, team); nothing was changed`) {
		t.Fatalf("space other = %v, want the refusal", err)
	}
	if err := r.space(t, "No_Space"); err == nil || !strings.Contains(err.Error(), "invalid space name") {
		t.Fatalf("space with an invalid name = %v", err)
	}

	// The site dropped the default space: the listing says so.
	r.site.SetSpaces(loginToken, notesSpace)
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.errOut.String(), `The default space "team" is not among them`) {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
	r.site.SetSpaces(loginToken)
	r.errOut.Reset()
	if err := r.space(t); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.errOut.String(), "reaches no space yet") {
		t.Fatalf("stderr = %q", r.errOut.String())
	}

	r.site.Revoke(loginToken)
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "no longer accepts the stored login") || !strings.Contains(err.Error(), "'slivingdoc login' again") {
		t.Fatalf("space with a revoked key = %v", err)
	}
	r.site.Close()
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "list the spaces the login reaches") || strings.Contains(err.Error(), loginToken) {
		t.Fatalf("space against a closed site = %v", err)
	}
}

func TestSpaceChoosesTheLogin(t *testing.T) {
	r := newLoginRig(t)
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "not logged in") || !strings.Contains(err.Error(), "slivingdoc login") {
		t.Fatalf("space without a login = %v", err)
	}
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.SetSpaces(otherToken, space("dev", "ada@example.test", "write"))
	r.site.Next(approved(otherToken, "write", devEndpoint))
	if err := r.login(t); err != nil {
		t.Fatal(err)
	}
	r.out.Reset()
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "several stored logins") || !strings.Contains(err.Error(), "pass --endpoint") {
		t.Fatalf("space with two logins = %v", err)
	}
	if err := r.space(t, "--endpoint", "https://API.dev.slivingdoc.dev/"); err != nil {
		t.Fatalf("space --endpoint = %v", err)
	}
	if want := "* dev (read and write), owned by ada@example.test\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if err := r.space(t, "--endpoint", "ftp://x"); err == nil {
		t.Fatal("space with an invalid endpoint = nil")
	}
	if err := r.space(t, "--endpoint", "https://other.example.test"); !errors.Is(err, credentials.ErrNoLogin) {
		t.Fatalf("space for an endpoint without a login = %v, want ErrNoLogin", err)
	}
}

func TestSpaceRefusesAnExpiredOrChangedLogin(t *testing.T) {
	r := newLoginRig(t)
	expired := entry(DefaultHostedEndpoint, loginToken)
	expired.ExpiresAt = longAgo
	r.dir = strings.TrimPrefix(writeLogins(t, nil, expired), credentials.DirEnv+"=")
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "expired") || !strings.Contains(err.Error(), "'slivingdoc login' again") {
		t.Fatalf("space with an expired login = %v", err)
	}

	r = newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	f := NewSpaceFlags()
	op, err := PrepareSpace(f, "notes", r.opts())
	if err != nil {
		t.Fatal(err)
	}
	r.site.SetSpaces(otherToken, notesSpace)
	r.site.Next(eve(otherToken))
	if err := r.login(t, "--force"); err != nil {
		t.Fatal(err)
	}
	if err := op.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "the stored login changed") {
		t.Fatalf("space after another login = %v, want the refusal", err)
	}
}
