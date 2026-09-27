package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
)

// SiteEnv names the site a login or logout talks to when --site is not
// given (architecture/login.md).
const SiteEnv = "SLIVINGDOC_SITE"

// LoginFlags are the login command's flags.
type LoginFlags struct {
	bucket    stringFlag
	readOnly  boolFlag
	site      stringFlag
	noBrowser boolFlag
}

// NewLoginFlags returns an unbound login flag holder.
func NewLoginFlags() *LoginFlags { return &LoginFlags{} }

// Bind registers the login flags on fs.
func (f *LoginFlags) Bind(fs *flag.FlagSet) {
	fs.Var(&f.bucket, "bucket", "space to preselect on the approval page")
	fs.Var(&f.readOnly, "read-only", "ask for a read-only token")
	fs.Var(&f.site, "site", "site that approves the login")
	fs.Var(&f.noBrowser, "no-browser", "print the approval page without opening a browser")
}

// LogoutFlags are the logout command's flags.
type LogoutFlags struct {
	bucket stringFlag
	site   stringFlag
}

// NewLogoutFlags returns an unbound logout flag holder.
func NewLogoutFlags() *LogoutFlags { return &LogoutFlags{} }

// Bind registers the logout flags on fs.
func (f *LogoutFlags) Bind(fs *flag.FlagSet) {
	fs.Var(&f.bucket, "bucket", "space to log out of (default: the default login's space)")
	fs.Var(&f.site, "site", "only log out of logins this site issued")
}

// Login is a prepared login: the flags are valid and the credentials file
// is readable, so the approval a person gives in the browser can be
// stored. Nothing has been sent yet.
type Login struct {
	opts     ProcessOptions
	file     credentials.File
	client   *sitelogin.Client
	space    string
	access   credentials.Access
	browser  bool
	hostname string
	// hostErr is why the host name could not label the token, if so.
	hostErr error
}

// PrepareLogin validates the login flags against the environment and
// reads the credentials file, so a login that could not be stored is
// refused before the site is asked for anything.
func PrepareLogin(f *LoginFlags, opts ProcessOptions) (*Login, error) {
	env := environ(opts.Env)
	file, err := credentialsFile(env)
	if err != nil {
		return nil, err
	}
	if _, err := file.Load(); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	if err := file.CheckDir(); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	space := f.bucket.value
	if space != "" {
		if err := httpstore.ValidateSpace(space); err != nil {
			return nil, fmt.Errorf("login: --bucket names the hosted space: %w", err)
		}
	}
	client, err := siteClient(resolveString(&f.site, env[SiteEnv], sitelogin.DefaultSite), opts)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	access := credentials.AccessWrite
	if f.readOnly.value {
		access = credentials.AccessRead
	}
	hostname := opts.Hostname
	if hostname == nil {
		hostname = os.Hostname
	}
	host, hostErr := hostname()
	if hostErr != nil {
		// The label is optional in the contract: the site then labels the
		// token "CLI login". Run tells the person why.
		host = ""
	}
	return &Login{
		opts: opts, file: file, client: client, space: space, access: access,
		browser: !f.noBrowser.value, hostname: host, hostErr: hostErr,
	}, nil
}

// Run asks the site for an approval, shows the page and the code, opens a
// browser when allowed, waits for the person to approve or deny it, and
// stores the issued token as the default login. A login that replaced an
// earlier token for the same endpoint and space revokes that token, best
// effort, after the new one is stored.
func (l *Login) Run(ctx context.Context) error {
	approval, err := l.client.Start(ctx, sitelogin.StartRequest{Space: l.space, Access: l.access, Client: l.hostname})
	if err != nil {
		return fmt.Errorf("login: %s", mcp.Redact(err.Error()))
	}
	errOut := l.opts.errOut()
	if l.hostErr != nil {
		fmt.Fprintf(errOut, "The host name is unknown (%s); the token is labelled \"CLI login\" without it.\n", mcp.Redact(l.hostErr.Error()))
	}
	fmt.Fprintf(errOut, "To log in, open this page and approve the code %s:\n  %s\n", approval.UserCode, approval.CompleteURI)
	fmt.Fprintln(errOut, "Only approve it if you started this login in your own terminal.")
	if l.browser {
		if err := l.opts.openBrowser(approval.CompleteURI); err != nil {
			fmt.Fprintf(errOut, "Could not open a browser (%s); open the page yourself.\n", mcp.Redact(err.Error()))
		}
	}
	fmt.Fprintf(errOut, "Waiting for approval (the code expires at %s)...\n", approval.Deadline.UTC().Format("15:04:05 UTC"))
	issued, err := l.client.Wait(ctx, approval)
	var rejected *sitelogin.RejectedTokenError
	if errors.As(err, &rejected) {
		return l.discard(ctx, rejected.Token, fmt.Errorf("login: %s; nothing was stored", mcp.Redact(err.Error())))
	}
	if err != nil {
		return fmt.Errorf("login: %s", mcp.Redact(err.Error()))
	}
	if l.access == credentials.AccessRead && issued.Access != credentials.AccessRead {
		return l.discard(ctx, issued.Token, errors.New("login: the site issued a read and write token for a read-only login; nothing was stored"))
	}
	endpoint, err := normalizeEndpoint(issued.Endpoint)
	if err != nil {
		return l.discard(ctx, issued.Token, fmt.Errorf("login: the site's storage endpoint: %w; nothing was stored", err))
	}
	stored := credentials.Login{
		Key:     credentials.Key{Endpoint: endpoint, Space: issued.Space},
		Site:    l.client.Site(),
		Token:   issued.Token,
		Access:  issued.Access,
		Expires: issued.Expires,
		Account: issued.Account,
		Owner:   issued.Owner,
	}
	// Read again right before writing, so a login that finished in another
	// terminal meanwhile keeps its entry.
	set, err := l.file.Load()
	if err != nil {
		return l.discard(ctx, stored.Token, fmt.Errorf("login: %w; nothing was stored", err))
	}
	replaced, putErr := set.Put(stored)
	if err := l.file.Save(set); err != nil {
		return l.discard(ctx, stored.Token, fmt.Errorf("login: store the token: %w", err))
	}
	if putErr == nil && replaced.Token != stored.Token {
		if err := revoke(ctx, replaced, l.opts); err != nil {
			fmt.Fprintf(errOut, "The earlier token for space %q could not be revoked (%s); revoke it on the Tokens page.\n",
				replaced.Space, mcp.Redact(err.Error()))
		}
	}
	fmt.Fprintln(l.opts.Out(), loggedIn(stored))
	return nil
}

// discard revokes an issued token the login refuses to store, so it does
// not stay valid unseen, and returns cause; a failed revocation is added to
// it.
func (l *Login) discard(ctx context.Context, token string, cause error) error {
	if err := l.client.Revoke(ctx, token); err != nil {
		return fmt.Errorf("%w; revoking the issued token failed too (%s), revoke it on the Tokens page", cause, mcp.Redact(err.Error()))
	}
	return cause
}

// loggedIn is the login's result line. It names the approver, and the
// space's owner when that is someone else, because whoever submits a code
// first decides it: this line is how a person notices that someone else
// approved their login.
func loggedIn(l credentials.Login) string {
	line := fmt.Sprintf("Logged in as %s to space %q (%s) %s", l.Account, l.Space, l.Access.Describe(), l.Expires.Describe())
	if l.Owner != l.Account {
		line += ", owned by " + l.Owner
	}
	return line
}

// Logout is a prepared logout: the logins to withdraw are chosen and the
// credentials file is readable.
type Logout struct {
	opts   ProcessOptions
	file   credentials.File
	logins []credentials.Login
}

// PrepareLogout chooses the stored logins to withdraw: every login for
// --bucket, or for the default login's space, narrowed to those the
// configured site issued when --site or SLIVINGDOC_SITE names one.
func PrepareLogout(f *LogoutFlags, opts ProcessOptions) (*Logout, error) {
	env := environ(opts.Env)
	file, err := credentialsFile(env)
	if err != nil {
		return nil, err
	}
	set, err := file.Load()
	if err != nil {
		return nil, fmt.Errorf("logout: %w", err)
	}
	space := f.bucket.value
	if space == "" {
		def, err := set.Default()
		if err != nil {
			return nil, fmt.Errorf("logout: not logged in: %w; pass --bucket to name a space", err)
		}
		space = def.Space
	}
	logins := set.Space(space)
	if len(logins) == 0 {
		return nil, fmt.Errorf("logout: %w for space %q", credentials.ErrNoLogin, space)
	}
	if site := resolveString(&f.site, env[SiteEnv], ""); site != "" {
		client, err := siteClient(site, opts)
		if err != nil {
			return nil, fmt.Errorf("logout: %w", err)
		}
		var kept []credentials.Login
		for _, l := range logins {
			if l.Site == client.Site() {
				kept = append(kept, l)
			}
		}
		if len(kept) == 0 {
			return nil, fmt.Errorf("logout: %w for space %q issued by %s (from --site or %s)", credentials.ErrNoLogin, space, client.Site(), SiteEnv)
		}
		logins = kept
	}
	return &Logout{opts: opts, file: file, logins: logins}, nil
}

// Run revokes each chosen token at the site that issued it and removes it
// from the credentials file; a token the site no longer knows counts as
// revoked. A login whose revocation fails stays stored, so the logout can
// be repeated.
func (l *Logout) Run(ctx context.Context) error {
	var failed []error
	var removed []credentials.Key
	for _, login := range l.logins {
		if err := revoke(ctx, login, l.opts); err != nil {
			failed = append(failed, fmt.Errorf("space %q at %s: %s", login.Space, login.Endpoint, mcp.Redact(err.Error())))
			continue
		}
		removed = append(removed, login.Key)
	}
	if len(removed) > 0 {
		set, err := l.file.Load()
		if err != nil {
			return fmt.Errorf("logout: %w", err)
		}
		for _, k := range removed {
			// A login another process removed meanwhile is already gone.
			if err := set.Remove(k); err != nil && !errors.Is(err, credentials.ErrNoLogin) {
				return fmt.Errorf("logout: %w", err)
			}
		}
		if err := l.file.Save(set); err != nil {
			return fmt.Errorf("logout: %w", err)
		}
		for _, k := range removed {
			fmt.Fprintf(l.opts.Out(), "Logged out of space %q at %s; the token was revoked\n", k.Space, k.Endpoint)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("logout: the token could not be revoked and stays stored; retry, or revoke it on the Tokens page: %w", errors.Join(failed...))
	}
	return nil
}

func revoke(ctx context.Context, login credentials.Login, opts ProcessOptions) error {
	client, err := siteClient(login.Site, opts)
	if err != nil {
		return err
	}
	return client.Revoke(ctx, login.Token)
}

func credentialsFile(env map[string]string) (credentials.File, error) {
	file, err := credentials.Locate(func(name string) string { return env[name] }, runtime.GOOS)
	if err != nil {
		return credentials.File{}, fmt.Errorf("cannot find the credentials file: %w", err)
	}
	return file, nil
}

// siteClient builds the site client from a raw --site value; a trailing
// slash and upper-case letters are tolerated like an endpoint's.
func siteClient(raw string, opts ProcessOptions) (*sitelogin.Client, error) {
	site, err := normalizeEndpoint(raw)
	if err != nil {
		return nil, fmt.Errorf("site: %w", err)
	}
	return sitelogin.New(sitelogin.Config{Site: site, UserAgent: "slivingdoc/" + Version, Sleep: opts.Sleep})
}

// errOut is the stream of a login's prompts: Stderr, or the process
// stderr when unset.
func (o ProcessOptions) errOut() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

// openBrowser opens url with the injected opener, or the platform's.
func (o ProcessOptions) openBrowser(url string) error {
	if o.OpenBrowser != nil {
		return o.OpenBrowser(url)
	}
	return platformBrowser(runtime.GOOS, environ(o.Env), url)
}

// platformBrowser starts the browser opener of goos for url — open on
// macOS, the URL handler of rundll32 on Windows, xdg-open elsewhere — and
// does not wait for the browser. The url is an approval page the site
// client already confined to the site's own origin. The process starts
// through os.StartProcess: os/exec is banned module-wide so nothing can
// shell out to Git (TestNoGitExecutableOrGit2goImport).
func platformBrowser(goos string, env map[string]string, url string) error {
	var path string
	var argv []string
	switch goos {
	case "darwin":
		path, argv = "/usr/bin/open", []string{"open", url}
	case "windows":
		root := lookupFold(env, "SystemRoot")
		if !filepath.IsAbs(root) {
			// A relative or missing root would run whatever rundll32.exe
			// the working directory holds.
			return errors.New("app: SystemRoot is not an absolute path")
		}
		path = filepath.Join(root, "System32", "rundll32.exe")
		argv = []string{"rundll32", "url.dll,FileProtocolHandler", url}
	default:
		found, err := findOnPath("xdg-open", env["PATH"])
		if err != nil {
			return err
		}
		path, argv = found, []string{"xdg-open", url}
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("app: open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()
	proc, err := os.StartProcess(path, argv, &os.ProcAttr{Files: []*os.File{devNull, devNull, devNull}})
	if err != nil {
		return fmt.Errorf("app: start the browser: %w", err)
	}
	if err := proc.Release(); err != nil {
		return fmt.Errorf("app: release the browser process: %w", err)
	}
	return nil
}

// lookupFold returns the value of name in env, ignoring case, as Windows
// does for environment variable names.
func lookupFold(env map[string]string, name string) string {
	if v, ok := env[name]; ok {
		return v
	}
	for k, v := range env {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// findOnPath returns the first executable regular file called name in the
// PATH list.
func findOnPath(name, pathList string) (string, error) {
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("app: %s is not on PATH", name)
}
