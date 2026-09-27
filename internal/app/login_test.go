package app

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
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
	for _, want := range []string{"BCDF-GHJK", r.site.URL() + "/cli/login?code=BCDF-GHJK", "your own terminal", "Waiting for approval"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("stderr = %q, want it to contain %q", prompt, want)
		}
	}
	if len(r.opened) != 1 || r.opened[0] != r.site.URL()+"/cli/login?code=BCDF-GHJK" {
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
	if err := r.login(t); err != nil {
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
