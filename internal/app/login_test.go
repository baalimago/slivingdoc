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

// loginRig is one login or logout invocation's injected environment.
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
		Sleep:    func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		Hostname: func() (string, error) { return "laptop", nil },
		Terminal: func() TerminalState { return r.terminal },
		Stdin:    r.answer,
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

// operation is a prepared login or logout.
type operation interface {
	Run(ctx context.Context) error
}

// invoke parses args into a flag holder through bind, prepares the
// operation, and runs it.
func invoke[F any](t *testing.T, flags F, bind func(*flag.FlagSet), prepare func(F) (operation, error), args []string) error {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bind(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	op, err := prepare(flags)
	if err != nil {
		return err
	}
	return op.Run(context.Background())
}

func (r *loginRig) login(t *testing.T, args ...string) error {
	t.Helper()
	f := NewLoginFlags()
	return invoke(t, f, f.Bind, func(f *LoginFlags) (operation, error) { return PrepareLogin(f, r.opts()) }, args)
}

func (r *loginRig) logout(t *testing.T, args ...string) error {
	t.Helper()
	f := NewLogoutFlags()
	return invoke(t, f, f.Bind, func(f *LogoutFlags) (operation, error) { return PrepareLogout(f, r.opts()) }, args)
}

func approved(space, token, access, endpoint string) sitetest.Script {
	return sitetest.Script{
		Pending: []string{"authorization_pending", "slow_down"},
		Issue: sitetest.Issue{
			Token: token, Space: space, Access: access, Endpoint: endpoint,
			ExpiresAt: time.Date(2026, 12, 26, 8, 0, 0, 0, time.UTC),
			Account:   "ada@example.test", Owner: "ada@example.test",
		},
	}
}

func TestLoginStoresTheDefaultLogin(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved("notes", loginToken, "write", "https://API.slivingdoc.dev/"))
	if err := r.login(t, "--bucket", "notes"); err != nil {
		t.Fatalf("login = %v", err)
	}
	if got, want := r.out.String(), "Logged in as ada@example.test to space \"notes\" (read and write) until 2026-12-26 08:00 UTC\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	prompt := r.errOut.String()
	for _, want := range []string{
		"Logging in through " + r.site.URL() + " (from SLIVINGDOC_SITE).\n",
		"approve the code BCDF-GHJK:\n  " + r.site.URL() + "/cli/login#BCDF-GHJK\n",
		"(or open " + r.site.URL() + "/cli/login and enter the code)\n",
		"your own terminal", "Waiting for approval",
		"Approved by ada@example.test for space \"notes\" (read and write), owned by ada@example.test.\n" +
			"  Storage endpoint: https://API.slivingdoc.dev/\n  Site: " + r.site.URL() + "\n",
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
	if starts := r.site.Starts(); len(starts) != 1 || starts[0] != (sitetest.StartBody{Space: "notes", Access: "write", Client: "laptop"}) {
		t.Fatalf("start requests = %+v", starts)
	}
	def, err := r.logins(t).Default()
	if err != nil || def.Space != "notes" || def.Endpoint != DefaultHostedEndpoint || def.Token != loginToken || def.Site != r.site.URL() {
		t.Fatalf("stored default = %+v, %v", def, err)
	}
	if strings.Contains(r.out.String()+r.errOut.String(), loginToken) {
		t.Fatal("the login printed the token")
	}
}

func TestLoginAgainRevokesTheReplacedToken(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("first login = %v", err)
	}
	r.site.Next(approved("notes", otherToken, "read", DefaultHostedEndpoint))
	if err := r.login(t, "--read-only", "--no-browser"); err != nil {
		t.Fatalf("second login = %v", err)
	}
	if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
		t.Fatalf("revoked = %v, want the replaced token", got)
	}
	if len(r.opened) != 1 {
		t.Fatalf("opened = %v, want no browser for --no-browser", r.opened)
	}
	if !strings.Contains(r.out.String(), `Logged in as ada@example.test to space "notes" (read only)`) {
		t.Fatalf("stdout = %q", r.out.String())
	}
	set := r.logins(t)
	if n := len(set.Logins()); n != 1 {
		t.Fatalf("stored logins = %d, want the replacement only", n)
	}
	if l, _ := set.Default(); l.Token != otherToken || l.Access != credentials.AccessRead {
		t.Fatalf("default = %+v", l)
	}

	// A replaced token the site already forgot counts as revoked, and
	// someone else approving the code shows as another owner.
	r.site.Revoke(otherToken)
	script := approved("notes", loginToken, "write", DefaultHostedEndpoint)
	script.Issue.Owner = "bob@example.test"
	r.site.Next(script)
	r.out.Reset()
	if err := r.login(t); err != nil {
		t.Fatalf("third login = %v", err)
	}
	if got := r.site.Revoked(); len(got) != 1 {
		t.Fatalf("revoked = %v, want no second revocation", got)
	}
	if !strings.HasSuffix(r.out.String(), "until 2026-12-26 08:00 UTC, owned by bob@example.test\n") {
		t.Fatalf("stdout = %q, want the other owner named", r.out.String())
	}
	if l, _ := r.logins(t).Default(); l.Account != "ada@example.test" || l.Owner != "bob@example.test" {
		t.Fatalf("stored account and owner = %q, %q", l.Account, l.Owner)
	}
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
	t.Run("write token for a read-only login", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
		err := r.login(t, "--read-only")
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("login = %v", err)
		}
		if len(r.logins(t).Logins()) != 0 {
			t.Fatal("a refused token was stored")
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want the refused token", got)
		}
	})
	t.Run("browser failure is not fatal", func(t *testing.T) {
		r := newLoginRig(t)
		r.openErr = errors.New("no display")
		r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
		if err := r.login(t); err != nil {
			t.Fatalf("login = %v", err)
		}
		if !strings.Contains(r.errOut.String(), "Could not open a browser (no display); open the page yourself") {
			t.Fatalf("stderr = %q", r.errOut.String())
		}
	})
	for _, tt := range []struct {
		name string
		env  []string
		args []string
		want string
	}{
		{"invalid space", nil, []string{"--bucket", "Bad_Space"}, "invalid space name"},
		{"plain http site", nil, []string{"--site", "http://www.example.test"}, "must use https"},
		{"site with a path", nil, []string{"--site", "https://www.example.test/x"}, "origin without a path"},
		{"site not a URL", nil, []string{"--site", "ftp://x"}, "absolute http or https"},
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
		if err := os.MkdirAll(r.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.dir, credentials.FileName), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := r.login(t); !errors.Is(err, credentials.ErrMalformed) {
			t.Fatalf("login = %v, want ErrMalformed", err)
		}
		if err := r.logout(t); !errors.Is(err, credentials.ErrMalformed) {
			t.Fatalf("logout = %v, want ErrMalformed", err)
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
	})
}

func TestLogout(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved("team", otherToken, "write", devEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("login team = %v", err)
	}
	r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t, "--default"); err != nil {
		t.Fatalf("login notes = %v", err)
	}
	r.out.Reset()

	if err := r.logout(t); err != nil {
		t.Fatalf("logout = %v", err)
	}
	if r.out.String() != "Logged out of space \"notes\" at https://api.slivingdoc.dev; the token was revoked\n" {
		t.Fatalf("stdout = %q", r.out.String())
	}
	set := r.logins(t)
	if _, err := set.Default(); !errors.Is(err, credentials.ErrNoDefault) {
		t.Fatalf("default after logging out of it = %v, want none", err)
	}
	if len(set.Logins()) != 1 {
		t.Fatalf("logins = %+v, want team only", set.Logins())
	}
	if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
		t.Fatalf("revoked = %v", got)
	}

	if err := r.logout(t); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("logout without a default = %v", err)
	}
	if err := r.logout(t, "--bucket", "notes"); !errors.Is(err, credentials.ErrNoLogin) {
		t.Fatalf("logout of a removed space = %v, want ErrNoLogin", err)
	}
	if err := r.logout(t, "--bucket", "team", "--site", "https://other.example.test"); !errors.Is(err, credentials.ErrNoLogin) ||
		!strings.Contains(err.Error(), "issued by https://other.example.test (from --site or SLIVINGDOC_SITE)") {
		t.Fatalf("logout for another site = %v, want ErrNoLogin", err)
	}
	if err := r.logout(t, "--bucket", "team", "--site", "http://www.example.test"); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("logout with a plain http site = %v", err)
	}

	// A token the site already forgot counts as revoked.
	r.site.Revoke(otherToken)
	if err := r.logout(t, "--bucket", "team", "--site", r.site.URL()+"/"); err != nil {
		t.Fatalf("logout of a forgotten token = %v", err)
	}
	if len(r.logins(t).Logins()) != 0 {
		t.Fatal("logout left the login stored")
	}
}

func TestLogoutKeepsALoginItCannotRevoke(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("login = %v", err)
	}
	r.site.Close()
	err := r.logout(t)
	if err == nil || !strings.Contains(err.Error(), "stays stored") || strings.Contains(err.Error(), loginToken) {
		t.Fatalf("logout against an unreachable site = %v", err)
	}
	if len(r.logins(t).Logins()) != 1 {
		t.Fatal("a login that was not revoked was removed")
	}
}

func TestLoginRevokesATokenItCannotStore(t *testing.T) {
	t.Run("an answer outside the contract", func(t *testing.T) {
		r := newLoginRig(t)
		script := approved("notes", loginToken, "write", DefaultHostedEndpoint)
		script.Issue.Account = ""
		r.site.Next(script)
		err := r.login(t)
		if err == nil || !strings.Contains(err.Error(), "no usable account") || strings.Contains(err.Error(), loginToken) {
			t.Fatalf("login = %v, want the protocol refusal without the token", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want the rejected token", got)
		}
	})
	t.Run("a credentials file that breaks while waiting", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
		l, err := PrepareLogin(NewLoginFlags(), r.opts())
		if err != nil {
			t.Fatalf("PrepareLogin() = %v", err)
		}
		if err := os.MkdirAll(r.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.dir, credentials.FileName), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := l.Run(context.Background()); !errors.Is(err, credentials.ErrMalformed) {
			t.Fatalf("Run() = %v, want ErrMalformed", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want the unstored token", got)
		}
	})
}

func TestLoginExplainsAnUnknownHostName(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
	opts := r.opts()
	opts.Hostname = func() (string, error) { return "", errors.New("uname failed") }
	l, err := PrepareLogin(NewLoginFlags(), opts)
	if err != nil {
		t.Fatalf("PrepareLogin() = %v", err)
	}
	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.Contains(r.errOut.String(), `The host name is unknown (uname failed); the token is labelled "CLI login" without it.`) {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
	if starts := r.site.Starts(); len(starts) != 1 || starts[0].Client != "" {
		t.Fatalf("start requests = %+v, want no client label", starts)
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

// eve is an approval of the same space by another account.
func eve(space, token string) sitetest.Script {
	script := approved(space, token, "write", DefaultHostedEndpoint)
	script.Issue.Account = "eve@example.test"
	return script
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
			r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
			err := r.login(t)
			if !strings.Contains(r.errOut.String(), "Approved by ada@example.test") ||
				!strings.HasSuffix(r.errOut.String(), "Store this login? [y/N] ") {
				t.Fatalf("stderr = %q, want the approval shown, then the question", r.errOut.String())
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
				t.Fatalf("revoked = %v, want the unconfirmed token", got)
			}
		})
	}
}

func TestLoginGuardsAnotherAccountsLogin(t *testing.T) {
	setup := func(t *testing.T) *loginRig {
		t.Helper()
		r := newLoginRig(t)
		r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
		if err := r.login(t); err != nil {
			t.Fatalf("first login = %v", err)
		}
		r.out.Reset()
		r.errOut.Reset()
		return r
	}
	t.Run("refused without a terminal", func(t *testing.T) {
		r := setup(t)
		r.site.Next(eve("notes", otherToken))
		err := r.login(t)
		if err == nil || !strings.Contains(err.Error(), "approved by ada@example.test, this one by eve@example.test") ||
			!strings.Contains(err.Error(), "--force") {
			t.Fatalf("login = %v, want the account refusal", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != otherToken {
			t.Fatalf("revoked = %v, want only the new token", got)
		}
		if l, _ := r.logins(t).Default(); l.Token != loginToken {
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
			r.site.Next(eve("notes", otherToken))
			if err := r.login(t, tt.args...); err != nil {
				t.Fatalf("login = %v", err)
			}
			if got := r.site.Revoked(); len(got) != 0 {
				t.Fatalf("revoked = %v; another account's token is never revoked", got)
			}
			if !strings.Contains(r.errOut.String(), `The earlier token for space "notes" was approved by ada@example.test, not eve@example.test, so it was not revoked; revoke it on the Tokens page`) {
				t.Fatalf("stderr = %q, want the note about the kept token", r.errOut.String())
			}
			if l, _ := r.logins(t).Default(); l.Token != otherToken {
				t.Fatalf("stored = %+v, want the new login", l)
			}
		})
	}
	t.Run("another account's default", func(t *testing.T) {
		r := setup(t)
		r.site.Next(eve("team", otherToken))
		err := r.login(t, "--default")
		if err == nil || !strings.Contains(err.Error(), `the default login (space "notes" at https://api.slivingdoc.dev) was approved by ada@example.test`) {
			t.Fatalf("login --default = %v, want the default refusal", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != otherToken {
			t.Fatalf("revoked = %v", got)
		}
		r.site.Next(eve("team", otherToken))
		if err := r.login(t, "--default", "--force"); err != nil {
			t.Fatalf("login --default --force = %v", err)
		}
		if l, _ := r.logins(t).Default(); l.Space != "team" {
			t.Fatalf("default = %+v, want team", l)
		}
	})
}

func TestLoginKeepsTheDefault(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("first login = %v", err)
	}
	if strings.Contains(r.errOut.String(), "default") {
		t.Fatalf("stderr = %q, want no default note for the first login", r.errOut.String())
	}
	r.site.Next(approved("team", otherToken, "write", DefaultHostedEndpoint))
	r.errOut.Reset()
	if err := r.login(t); err != nil {
		t.Fatalf("second login = %v", err)
	}
	if !strings.Contains(r.errOut.String(), `The default login stays space "notes" at https://api.slivingdoc.dev; use --default to switch.`) {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
	if l, _ := r.logins(t).Default(); l.Space != "notes" {
		t.Fatalf("default = %+v, want notes kept", l)
	}
	r.site.Next(approved("team", otherToken, "write", DefaultHostedEndpoint))
	r.errOut.Reset()
	if err := r.login(t, "--default"); err != nil {
		t.Fatalf("login --default = %v", err)
	}
	if !strings.Contains(r.errOut.String(), `The default login changed from space "notes" at https://api.slivingdoc.dev to space "team" at https://api.slivingdoc.dev.`) {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
	if l, _ := r.logins(t).Default(); l.Space != "team" {
		t.Fatalf("default = %+v, want team", l)
	}
}

func TestLoginRefusesWhatItDidNotAskFor(t *testing.T) {
	t.Run("another space than --bucket", func(t *testing.T) {
		r := newLoginRig(t)
		r.site.Next(approved("other", loginToken, "write", DefaultHostedEndpoint))
		err := r.login(t, "--bucket", "notes")
		if err == nil || !strings.Contains(err.Error(), `a token for space "other", not the requested "notes"`) {
			t.Fatalf("login = %v", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v", got)
		}
	})
	t.Run("a login another site issued", func(t *testing.T) {
		r := newLoginRig(t)
		other := entry(DefaultHostedEndpoint, "notes", otherToken)
		r.dir = strings.TrimPrefix(writeLogins(t, &storedKey{DefaultHostedEndpoint, "notes"}, other), credentials.DirEnv+"=")
		r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
		err := r.login(t, "--force")
		if err == nil || !strings.Contains(err.Error(), "was issued by https://www.slivingdoc.dev, not "+r.site.URL()) ||
			!strings.Contains(err.Error(), "log out of that login first") {
			t.Fatalf("login = %v, want the site refusal", err)
		}
		if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
			t.Fatalf("revoked = %v, want only the new token", got)
		}
		if l, _ := r.logins(t).Default(); l.Token != otherToken {
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
		issued := sitelogin.Issued{Token: loginToken, Space: "notes", Access: credentials.AccessWrite, Endpoint: devEndpoint}
		if _, err := l.accept(issued); err == nil || !strings.Contains(err.Error(), "issued a token for "+devEndpoint+", not "+DefaultHostedEndpoint) {
			t.Fatalf("accept() = %v, want the endpoint refusal", err)
		}
		issued.Endpoint = "https://API.slivingdoc.dev/"
		if got, err := l.accept(issued); err != nil || got.Endpoint != DefaultHostedEndpoint || got.Site != sitelogin.DefaultSite {
			t.Fatalf("accept() = %+v, %v", got, err)
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
		r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
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

func TestLoggedInNamesAnotherEndpoint(t *testing.T) {
	l := credentials.Login{Key: credentials.Key{Endpoint: devEndpoint, Space: "notes"}, Access: credentials.AccessRead, Account: "a@x", Owner: "a@x"}
	if got, want := loggedIn(l), `Logged in as a@x to space "notes" at `+devEndpoint+` (read only) with no expiry`; got != want {
		t.Fatalf("loggedIn() = %q, want %q", got, want)
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
	script := approved("notes", loginToken, "write", "https://api.slivingdoc.dev")
	script.Pending, script.Stall = nil, true
	r.site.Next(script)
	opts := r.opts()
	sig := make(chan os.Signal, 1)
	opts.Signals, opts.SiteClient = sig, signalOnPoll{sig: sig}
	f := NewLoginFlags()
	err := invoke(t, f, f.Bind, func(f *LoginFlags) (operation, error) { return PrepareLogin(f, opts) }, nil)
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
	r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
	sig := make(chan os.Signal, 1)
	stdin := interruptingStdin{sig: sig, ended: make(chan struct{})}
	t.Cleanup(func() { close(stdin.ended) })
	opts := r.opts()
	opts.Terminal = func() TerminalState { return OnTerminal }
	opts.Stdin, opts.Signals = stdin, sig
	f := NewLoginFlags()
	err := invoke(t, f, f.Bind, func(f *LoginFlags) (operation, error) { return PrepareLogin(f, opts) }, nil)
	if err == nil || !strings.Contains(err.Error(), "not confirmed") || strings.Contains(err.Error(), "revoking the issued token failed") {
		t.Fatalf("login interrupted at the prompt = %v, want the refusal with the token revoked", err)
	}
	if got := r.site.Revoked(); len(got) != 1 || got[0] != loginToken {
		t.Fatalf("revoked = %v, want the unconfirmed token despite the interrupt", got)
	}
	if len(r.logins(t).Logins()) != 0 {
		t.Fatal("an interrupted login was stored")
	}
}

func TestLogoutKeepsANewerLogin(t *testing.T) {
	r := newLoginRig(t)
	r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("login = %v", err)
	}
	// The logout reads the login, then another process logs in again for
	// the same space before the logout removes it.
	logout, err := PrepareLogout(NewLogoutFlags(), r.opts())
	if err != nil {
		t.Fatalf("PrepareLogout() = %v", err)
	}
	r.site.Next(approved("notes", otherToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("second login = %v", err)
	}
	r.out.Reset()
	if err := logout.Run(context.Background()); err != nil {
		t.Fatalf("logout = %v", err)
	}
	if want := "Revoked the token for space \"notes\" at https://api.slivingdoc.dev; a newer login for it was kept\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	got, err := r.logins(t).Lookup(credentials.Key{Endpoint: DefaultHostedEndpoint, Space: "notes"})
	if err != nil || got.Token != otherToken {
		t.Fatalf("stored = %+v, %v; want the newer login kept", got, err)
	}
}

// TestLoginAndLogoutNeverPrintStoredUserInformation proves a hand-edited
// credentials file whose endpoint carries user information is refused at
// load, and that neither login nor logout prints the secret on the way.
func TestLoginAndLogoutNeverPrintStoredUserInformation(t *testing.T) {
	const secretEndpoint = "https://user:secret@api.example.test"
	stored := `{"endpoint":"` + secretEndpoint + `","space":"notes","site":"https://www.slivingdoc.dev","token":"` + loginToken + `","access":"write"}`
	for _, row := range []struct {
		name string
		data string
	}{
		{"one login", `{"version":1,"logins":[` + stored + `]}`},
		{"a second login", `{"version":1,"logins":[` + stored + `,` + stored + `]}`},
		{"a default without its login", `{"version":1,"logins":[],"default":{"endpoint":"` + secretEndpoint + `","space":"notes"}}`},
	} {
		t.Run(row.name, func(t *testing.T) {
			for _, op := range []string{"login", "logout"} {
				r := newLoginRig(t)
				if err := os.MkdirAll(r.dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(r.dir, credentials.FileName), []byte(row.data), 0o600); err != nil {
					t.Fatal(err)
				}
				var err error
				if op == "login" {
					r.site.Next(approved("notes", hostedTestToken, "write", DefaultHostedEndpoint))
					err = r.login(t)
				} else {
					err = r.logout(t, "--bucket", "notes")
				}
				if !errors.Is(err, credentials.ErrMalformed) {
					t.Fatalf("%s = %v, want ErrMalformed", op, err)
				}
				for _, text := range []string{err.Error(), r.out.String(), r.errOut.String()} {
					if strings.Contains(text, "secret") || strings.Contains(text, loginToken) {
						t.Fatalf("%s printed %q, want no user information or token", op, text)
					}
				}
			}
		})
	}
}

// TestLoginAndLogoutTakeSpaceOrBucket proves --space and --bucket are one
// setting on login and logout: either spelling works, and both with
// different values are refused before the site is contacted.
func TestLoginAndLogoutTakeSpaceOrBucket(t *testing.T) {
	for _, op := range []string{"login", "logout"} {
		r := newLoginRig(t)
		var err error
		if op == "login" {
			err = r.login(t, "--bucket", "notes", "--space", "other")
		} else {
			err = r.logout(t, "--bucket", "notes", "--space", "other")
		}
		if err == nil || !strings.Contains(err.Error(), `--bucket "notes" and --space "other" name different spaces`) {
			t.Fatalf("%s with two spaces = %v, want the refusal naming both", op, err)
		}
		if len(r.site.Starts()) != 0 {
			t.Fatalf("%s with two spaces contacted the site", op)
		}
	}
	r := newLoginRig(t)
	if err := r.login(t, "--space", "No_Space"); err == nil || !strings.Contains(err.Error(), "login: --space names the hosted space") {
		t.Fatalf("login --space with an invalid name = %v, want the refusal naming --space", err)
	}
	for _, args := range [][]string{{"--space", "notes"}, {"--bucket", "notes"}, {"--space", "notes", "--bucket", "notes"}} {
		r := newLoginRig(t)
		r.site.Next(approved("notes", loginToken, "write", DefaultHostedEndpoint))
		if err := r.login(t, args...); err != nil {
			t.Fatalf("login %v = %v", args, err)
		}
		if _, err := r.logins(t).Lookup(credentials.Key{Endpoint: DefaultHostedEndpoint, Space: "notes"}); err != nil {
			t.Fatalf("login %v stored no login for notes: %v", args, err)
		}
		if err := r.logout(t, args...); err != nil {
			t.Fatalf("logout %v = %v", args, err)
		}
	}
}
