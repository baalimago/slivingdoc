package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

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
	bucket     stringFlag
	readOnly   boolFlag
	site       stringFlag
	noBrowser  boolFlag
	setDefault boolFlag
	force      boolFlag
}

// NewLoginFlags returns an unbound login flag holder.
func NewLoginFlags() *LoginFlags { return &LoginFlags{} }

// Bind registers the login flags on fs.
func (f *LoginFlags) Bind(fs *flag.FlagSet) {
	fs.Var(&f.bucket, "bucket", "space to preselect on the approval page; the token must be for it")
	fs.Var(&f.readOnly, "read-only", "ask for a read-only token")
	fs.Var(&f.site, "site", "site that approves the login")
	fs.Var(&f.noBrowser, "no-browser", "print the approval page without opening a browser")
	fs.Var(&f.setDefault, "default", "make this login the default even when another one is")
	fs.Var(&f.force, "force", "without a terminal, replace a login or default another account approved")
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
	opts   ProcessOptions
	file   credentials.File
	client *sitelogin.Client
	// siteSource names where a non-default site came from, for the
	// warning printed before anything is sent.
	siteSource string
	space      string
	access     credentials.Access
	browser    bool
	setDefault bool
	force      bool
	hostname   string
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
	siteSource := "--site"
	if !f.site.set {
		siteSource = SiteEnv
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
		opts: opts, file: file, client: client, siteSource: siteSource, space: space, access: access,
		browser: !f.noBrowser.value, setDefault: f.setDefault.value, force: f.force.value,
		hostname: host, hostErr: hostErr,
	}, nil
}

// consent is whether a person confirmed storing a login at the prompt, or
// --force stands in for them.
type consent int

const (
	notConfirmed consent = iota
	confirmed
)

// Run asks the site for an approval, shows the page and the code, opens a
// browser when allowed, waits for the person to approve or deny it, shows
// who approved it, asks for confirmation on a terminal, and stores the
// issued token (architecture/login.md, Threat model). Every issued token
// it does not store is revoked. A login that replaced an earlier token of
// the same account for the same endpoint and space revokes that token,
// best effort, after the new one is stored.
func (l *Login) Run(ctx context.Context) error {
	ctx, stop := l.opts.interruptible(ctx)
	defer stop()
	errOut := l.opts.errOut()
	if l.client.Site() != sitelogin.DefaultSite {
		fmt.Fprintf(errOut, "Logging in through %s (from %s).\n", l.client.Site(), l.siteSource)
	}
	approval, err := l.client.Start(ctx, sitelogin.StartRequest{Space: l.space, Access: l.access, Client: l.hostname})
	if err != nil {
		return fmt.Errorf("login: %s", mcp.Redact(err.Error()))
	}
	if l.hostErr != nil {
		fmt.Fprintf(errOut, "The host name is unknown (%s); the token is labelled \"CLI login\" without it.\n", mcp.Redact(l.hostErr.Error()))
	}
	fmt.Fprintf(errOut, "To log in, open this page and approve the code %s:\n  %s\n", approval.UserCode, approval.CompleteURI)
	fmt.Fprintf(errOut, "(or open %s and enter the code)\n", approval.URI)
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
	fmt.Fprint(errOut, approvedBy(issued, l.client.Site()))
	stored, err := l.accept(issued)
	if err != nil {
		return l.discard(ctx, issued.Token, err)
	}
	agreed := notConfirmed
	if l.force {
		agreed = confirmed
	}
	if l.opts.terminal() == OnTerminal {
		if agreed, err = l.confirm(ctx, errOut); err != nil {
			return l.discard(ctx, stored.Token, err)
		}
		if agreed != confirmed {
			return l.discard(ctx, stored.Token, errors.New("login: not confirmed; nothing was stored and the new token was revoked"))
		}
	}
	outcome, err := l.store(ctx, stored, agreed)
	if err != nil {
		return l.discard(ctx, stored.Token, err)
	}
	l.report(ctx, stored, outcome)
	fmt.Fprintln(l.opts.Out(), loggedIn(stored))
	return nil
}

// accept checks an issued token against what this login asked for and
// where it came from, and returns the login to store.
func (l *Login) accept(issued sitelogin.Issued) (credentials.Login, error) {
	if l.access == credentials.AccessRead && issued.Access != credentials.AccessRead {
		return credentials.Login{}, errors.New("login: the site issued a read and write token for a read-only login; nothing was stored")
	}
	if l.space != "" && issued.Space != l.space {
		return credentials.Login{}, fmt.Errorf("login: the site issued a token for space %q, not the requested %q; nothing was stored", issued.Space, l.space)
	}
	endpoint, err := normalizeEndpoint(issued.Endpoint)
	if err != nil {
		return credentials.Login{}, fmt.Errorf("login: the site's storage endpoint: %w; nothing was stored", err)
	}
	if l.client.Site() == sitelogin.DefaultSite && endpoint != DefaultHostedEndpoint {
		return credentials.Login{}, fmt.Errorf("login: %s issued a token for %s, not %s; nothing was stored", sitelogin.DefaultSite, endpoint, DefaultHostedEndpoint)
	}
	return credentials.Login{
		Key:     credentials.Key{Endpoint: endpoint, Space: issued.Space},
		Site:    l.client.Site(),
		Token:   issued.Token,
		Access:  issued.Access,
		Expires: issued.Expires,
		Account: issued.Account,
		Owner:   issued.Owner,
	}, nil
}

// approvedBy is what the person checks before a login is stored: whoever
// submits a code first decides it, so this is how someone else's approval
// shows.
func approvedBy(issued sitelogin.Issued, site string) string {
	return fmt.Sprintf("Approved by %s for space %q (%s), owned by %s.\n  Storage endpoint: %s\n  Site: %s\n",
		issued.Account, issued.Space, issued.Access.Describe(), issued.Owner, issued.Endpoint, site)
}

// confirm asks the person at the terminal whether to store the login; only
// y or yes, in any case, agrees.
func (l *Login) confirm(ctx context.Context, errOut io.Writer) (consent, error) {
	fmt.Fprint(errOut, "Store this login? [y/N] ")
	type answer struct {
		line string
		err  error
	}
	answered := make(chan answer, 1)
	go func() {
		line, err := bufio.NewReader(l.opts.stdin()).ReadString('\n')
		answered <- answer{line, err}
	}()
	var got answer
	select {
	case <-ctx.Done():
		// An interrupt at the prompt is a no; the read is left to the
		// process exit.
		fmt.Fprintln(errOut)
		return notConfirmed, nil
	case got = <-answered:
	}
	if got.err != nil && !errors.Is(got.err, io.EOF) {
		return notConfirmed, fmt.Errorf("login: read the answer: %w; nothing was stored", got.err)
	}
	switch strings.ToLower(strings.TrimSpace(got.line)) {
	case "y", "yes":
		return confirmed, nil
	default:
		return notConfirmed, nil
	}
}

// storeOutcome is what storing a login changed: the login it replaced and
// the default before it, each nil when there was none, and whether the new
// login became the default.
type storeOutcome struct {
	replaced     *credentials.Login
	priorDefault *credentials.Login
	isDefault    defaultChoice
}

// defaultChoice is whether a stored login is now the default.
type defaultChoice int

const (
	defaultKept defaultChoice = iota
	defaultTaken
)

// store writes the login under the credentials lock. It refuses to replace
// a login another site issued, and, unless the person agreed, to replace a
// login or a default another account approved.
func (l *Login) store(ctx context.Context, stored credentials.Login, agreed consent) (out storeOutcome, errOut error) {
	lock, err := l.file.Lock(ctx)
	if err != nil {
		return storeOutcome{}, fmt.Errorf("login: %w; nothing was stored", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil && errOut == nil {
			errOut = fmt.Errorf("login: %w", err)
		}
	}()
	// Read under the lock, so a login that finished in another terminal
	// meanwhile keeps its entry.
	set, err := l.file.Load()
	if err != nil {
		return storeOutcome{}, fmt.Errorf("login: %w; nothing was stored", err)
	}
	if old, err := set.Lookup(stored.Key); err == nil {
		if old.Site != stored.Site {
			return storeOutcome{}, fmt.Errorf("login: the stored login for space %q at %s was issued by %s, not %s; log out of that login first (slivingdoc logout --bucket %s --site %s); nothing was stored",
				old.Space, mcp.Redact(old.Endpoint), mcp.Redact(old.Site), stored.Site, old.Space, mcp.Redact(old.Site))
		}
		if old.Account != stored.Account && agreed != confirmed {
			return storeOutcome{}, fmt.Errorf("login: the stored login for space %q at %s was approved by %s, this one by %s; run login in a terminal to confirm, or pass --force; nothing was stored",
				old.Space, mcp.Redact(old.Endpoint), account(old), account(stored))
		}
	}
	isDefault := defaultKept
	def, err := set.Default()
	if err != nil || l.setDefault || def.Key == stored.Key {
		isDefault = defaultTaken
	}
	if err == nil {
		out.priorDefault = &def
		if l.setDefault && def.Key != stored.Key && def.Account != stored.Account && agreed != confirmed {
			return storeOutcome{}, fmt.Errorf("login: the default login (space %q at %s) was approved by %s, this one by %s; run login in a terminal to confirm, or pass --force; nothing was stored",
				def.Space, mcp.Redact(def.Endpoint), account(def), account(stored))
		}
	}
	if replaced, err := set.Put(stored); err == nil {
		out.replaced = &replaced
	}
	if isDefault == defaultTaken {
		if err := set.SetDefault(stored.Key); err != nil {
			return storeOutcome{}, fmt.Errorf("login: %w; nothing was stored", err)
		}
	}
	out.isDefault = isDefault
	if err := l.file.Save(set); err != nil {
		return storeOutcome{}, fmt.Errorf("login: store the token: %w", err)
	}
	return out, nil
}

// report tells the person what the stored login changed besides itself:
// the default, and the token it replaced, which is revoked only when the
// same account approved it.
func (l *Login) report(ctx context.Context, stored credentials.Login, out storeOutcome) {
	errOut := l.opts.errOut()
	if prior := out.priorDefault; prior != nil && prior.Key != stored.Key {
		if out.isDefault == defaultTaken {
			fmt.Fprintf(errOut, "The default login changed from %s to %s.\n", describeKey(prior.Key), describeKey(stored.Key))
		} else {
			fmt.Fprintf(errOut, "The default login stays %s; use --default to switch.\n", describeKey(prior.Key))
		}
	}
	replaced := out.replaced
	if replaced == nil || replaced.Token == stored.Token {
		return
	}
	if replaced.Account == "" || replaced.Account != stored.Account {
		fmt.Fprintf(errOut, "The earlier token for space %q was approved by %s, not %s, so it was not revoked; revoke it on the Tokens page if it is no longer needed.\n",
			replaced.Space, account(*replaced), account(stored))
		return
	}
	rctx, cancel := revocationContext(ctx)
	defer cancel()
	if err := revoke(rctx, *replaced, l.opts); err != nil {
		fmt.Fprintf(errOut, "The earlier token for space %q could not be revoked (%s); revoke it on the Tokens page.\n",
			replaced.Space, mcp.Redact(err.Error()))
	}
}

// account names who approved a login; a file written before the site
// reported it has none.
func account(l credentials.Login) string {
	if l.Account == "" {
		return "an unknown account"
	}
	return l.Account
}

func describeKey(k credentials.Key) string {
	return fmt.Sprintf("space %q at %s", k.Space, mcp.Redact(k.Endpoint))
}

// revocationTimeout bounds a revocation that runs after the login's own
// context may have ended.
const revocationTimeout = 10 * time.Second

// revocationContext keeps ctx's values but not its cancellation, so an
// interrupt that refused a login still revokes the token it was given,
// within revocationTimeout.
func revocationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), revocationTimeout)
}

// discard revokes an issued token the login refuses to store, so it does
// not stay valid unseen, and returns cause; a failed revocation is added to
// it.
func (l *Login) discard(ctx context.Context, token string, cause error) error {
	rctx, cancel := revocationContext(ctx)
	defer cancel()
	if err := l.client.Revoke(rctx, token); err != nil {
		return fmt.Errorf("%w; revoking the issued token failed too (%s), revoke it on the Tokens page", cause, mcp.Redact(err.Error()))
	}
	return cause
}

// loggedIn is the login's result line. It names the approving account, the
// endpoint when it is not the default, and the space's owner when that is
// someone else.
func loggedIn(l credentials.Login) string {
	line := fmt.Sprintf("Logged in as %s to space %q", l.Account, l.Space)
	if l.Endpoint != DefaultHostedEndpoint {
		line += " at " + l.Endpoint
	}
	line += fmt.Sprintf(" (%s) %s", l.Access.Describe(), l.Expires.Describe())
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
	var revoked []credentials.Login
	for _, login := range l.logins {
		if err := revoke(ctx, login, l.opts); err != nil {
			failed = append(failed, fmt.Errorf("space %q at %s: %s", login.Space, mcp.Redact(login.Endpoint), mcp.Redact(err.Error())))
			continue
		}
		revoked = append(revoked, login)
	}
	if len(revoked) > 0 {
		kept, err := l.remove(ctx, revoked)
		if err != nil {
			return err
		}
		for _, login := range revoked {
			if slices.Contains(kept, login.Key) {
				fmt.Fprintf(l.opts.Out(), "Revoked the token for space %q at %s; a newer login for it was kept\n", login.Space, mcp.Redact(login.Endpoint))
				continue
			}
			fmt.Fprintf(l.opts.Out(), "Logged out of space %q at %s; the token was revoked\n", login.Space, mcp.Redact(login.Endpoint))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("logout: the token could not be revoked and stays stored; retry, or revoke it on the Tokens page: %w", errors.Join(failed...))
	}
	return nil
}

// remove deletes the revoked logins from the file under the credentials
// lock. A login another process removed meanwhile is already gone; one
// another process replaced with a newer token since it was read is kept,
// and its key returned, because deleting it would orphan a live token.
func (l *Logout) remove(ctx context.Context, revoked []credentials.Login) (kept []credentials.Key, errOut error) {
	lock, err := l.file.Lock(ctx)
	if err != nil {
		return nil, fmt.Errorf("logout: %w", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil && errOut == nil {
			errOut = fmt.Errorf("logout: %w", err)
		}
	}()
	set, err := l.file.Load()
	if err != nil {
		return nil, fmt.Errorf("logout: %w", err)
	}
	for _, login := range revoked {
		current, err := set.Lookup(login.Key)
		if errors.Is(err, credentials.ErrNoLogin) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("logout: %w", err)
		}
		if current.Token != login.Token {
			kept = append(kept, login.Key)
			continue
		}
		if err := set.Remove(login.Key); err != nil {
			return nil, fmt.Errorf("logout: %w", err)
		}
	}
	if err := l.file.Save(set); err != nil {
		return nil, fmt.Errorf("logout: %w", err)
	}
	return kept, nil
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
	return sitelogin.New(sitelogin.Config{Site: site, UserAgent: "slivingdoc/" + Version, Client: opts.SiteClient, Sleep: opts.Sleep})
}

// interruptible returns ctx ended by the first termination signal, from
// Signals or, when unset, from the operating system, so an interrupted
// login stops polling and says a token may have been issued.
func (o ProcessOptions) interruptible(ctx context.Context) (context.Context, context.CancelFunc) {
	if o.Signals == nil {
		return signal.NotifyContext(ctx, terminationSignals...)
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-o.Signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// errOut is the stream of a login's prompts: Stderr, or the process
// stderr when unset.
func (o ProcessOptions) errOut() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

// stdin is where a confirmation answer is read: Stdin, or the process
// stdin when unset.
func (o ProcessOptions) stdin() io.Reader {
	if o.Stdin != nil {
		return o.Stdin
	}
	return os.Stdin
}

// terminal reports whether a person can answer the login prompt.
func (o ProcessOptions) terminal() TerminalState {
	if o.Terminal != nil {
		return o.Terminal()
	}
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())) {
		return OnTerminal
	}
	return NoTerminal
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
// client built from the site origin itself. The process starts
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
