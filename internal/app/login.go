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
	bucket    stringFlag
	space     stringFlag
	readOnly  boolFlag
	site      stringFlag
	noBrowser boolFlag
	force     boolFlag
}

// NewLoginFlags returns an unbound login flag holder.
func NewLoginFlags() *LoginFlags { return &LoginFlags{} }

// Bind registers the login flags on fs.
func (f *LoginFlags) Bind(fs *flag.FlagSet) {
	fs.Var(&f.space, "space", "space to make the default; the login must reach it")
	fs.Var(&f.bucket, "bucket", "the same as --space")
	fs.Var(&f.readOnly, "read-only", "ask for a read-only login")
	fs.Var(&f.site, "site", "site that approves the login")
	fs.Var(&f.noBrowser, "no-browser", "print the approval page without opening a browser")
	fs.Var(&f.force, "force", "without a terminal, replace a login another account approved")
}

// LogoutFlags are the logout command's flags.
type LogoutFlags struct {
	site stringFlag
}

// NewLogoutFlags returns an unbound logout flag holder.
func NewLogoutFlags() *LogoutFlags { return &LogoutFlags{} }

// Bind registers the logout flags on fs.
func (f *LogoutFlags) Bind(fs *flag.FlagSet) {
	fs.Var(&f.site, "site", "only log out of logins this site issued")
}

// Login is a prepared login: the flags are valid and the credentials file
// is readable (or is an earlier build's, which the login replaces), so
// the approval a person gives in the browser can be stored. Nothing has
// been sent yet.
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
	force      bool
	hostname   string
	// hostErr is why the host name could not label the key, if so.
	hostErr error
	// unlock releases the credentials lock; nil is Lock.Unlock. Tests
	// make it fail.
	unlock func(*credentials.Lock) error
}

// PrepareLogin validates the login flags against the environment and
// reads the credentials file, so a login that could not be stored is
// refused before the site is asked for anything. A file an earlier build
// wrote (credentials.ErrOutdated) is no refusal: the login replaces it.
func PrepareLogin(f *LoginFlags, opts ProcessOptions) (*Login, error) {
	env := environ(opts.Env)
	file, err := credentialsFile(env)
	if err != nil {
		return nil, err
	}
	if _, err := file.Load(); err != nil && !errors.Is(err, credentials.ErrOutdated) {
		return nil, fmt.Errorf("login: %w", err)
	}
	if err := file.CheckDir(); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	space, from, err := flagSpace(&f.bucket, &f.space)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	if space != "" {
		if err := httpstore.ValidateSpace(space); err != nil {
			return nil, fmt.Errorf("login: %s names the hosted space: %w", from, err)
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
		// key "CLI login". Run tells the person why.
		host = ""
	}
	return &Login{
		opts: opts, file: file, client: client, siteSource: siteSource, space: space, access: access,
		browser: !f.noBrowser.value, force: f.force.value, hostname: host, hostErr: hostErr,
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
// browser when allowed, waits for the person to approve or deny it, lists
// the spaces the issued key reaches, shows who approved it and those
// spaces with their owners, asks for confirmation on a terminal, and
// stores the key with the default space (architecture/login.md, Threat
// model). Every issued key it does not store is revoked. A login that
// replaced an earlier key of the same account for the same site and
// endpoint revokes that key, best effort, after the new one is stored.
func (l *Login) Run(ctx context.Context) error {
	ctx, stop := l.opts.interruptible(ctx)
	defer stop()
	errOut := l.opts.errOut()
	if l.client.Site() != sitelogin.DefaultSite {
		fmt.Fprintf(errOut, "Logging in through %s (from %s).\n", l.client.Site(), l.siteSource)
	}
	approval, err := l.client.Start(ctx, sitelogin.StartRequest{Access: l.access, Client: l.hostname})
	if err != nil {
		return fmt.Errorf("login: %s", mcp.Redact(err.Error()))
	}
	if l.hostErr != nil {
		fmt.Fprintf(errOut, "The host name is unknown (%s); the login is labelled \"CLI login\" without it.\n", mcp.Redact(l.hostErr.Error()))
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
	var rejected *sitelogin.RejectedError
	if errors.As(err, &rejected) {
		return l.discard(ctx, rejected.Credential, fmt.Errorf("login: %s; nothing was stored", mcp.Redact(err.Error())))
	}
	if err != nil {
		return fmt.Errorf("login: %s", mcp.Redact(err.Error()))
	}
	stored, err := l.accept(issued)
	if err != nil {
		return l.discard(ctx, issued.Key, err)
	}
	spaces, err := l.client.Spaces(ctx, stored.Key)
	if err != nil {
		return l.discard(ctx, stored.Key, fmt.Errorf("login: list the spaces the login reaches: %s; nothing was stored", mcp.Redact(err.Error())))
	}
	if l.space != "" && !slices.ContainsFunc(spaces, func(s sitelogin.Space) bool { return s.Name == l.space }) {
		return l.discard(ctx, stored.Key, fmt.Errorf("login: %s approved a login that does not reach space %q (it reaches %s); nothing was stored",
			stored.Account, l.space, spaceNames(spaces)))
	}
	fmt.Fprint(errOut, approvedBy(stored, spaces))
	agreed := notConfirmed
	if l.force {
		agreed = confirmed
	}
	if l.opts.terminal() == OnTerminal {
		if agreed, err = l.confirm(ctx, errOut); err != nil {
			return l.discard(ctx, stored.Key, err)
		}
		if agreed != confirmed {
			return l.discard(ctx, stored.Key, errors.New("login: not confirmed; nothing was stored and the new login key was revoked"))
		}
	}
	outcome, err := l.store(ctx, stored, spaces, agreed)
	if err != nil {
		return l.discard(ctx, stored.Key, err)
	}
	l.report(ctx, stored, spaces, outcome)
	fmt.Fprintln(l.opts.Out(), loggedIn(stored, outcome.defaultSpace))
	if outcome.unlockErr != nil {
		// The key is stored and usable; only the lock beside it could not
		// be released, which the next login, logout or space waits on.
		return fmt.Errorf("login: the login was stored, but %w; remove %s if no other login, logout or space command runs",
			outcome.unlockErr, credentials.LockName)
	}
	return nil
}

// accept checks an issued key against what this login asked for and
// where it came from, and returns the login to store.
func (l *Login) accept(issued sitelogin.Issued) (credentials.Login, error) {
	if l.access == credentials.AccessRead && issued.Access != credentials.AccessRead {
		return credentials.Login{}, errors.New("login: the site issued a read and write login for a read-only login; nothing was stored")
	}
	endpoint, err := normalizeEndpoint(issued.Endpoint)
	if err != nil {
		return credentials.Login{}, fmt.Errorf("login: the site's storage endpoint: %w; nothing was stored", err)
	}
	if l.client.Site() == sitelogin.DefaultSite && endpoint != DefaultHostedEndpoint {
		return credentials.Login{}, fmt.Errorf("login: %s issued a login for %s, not %s; nothing was stored", sitelogin.DefaultSite, endpoint, DefaultHostedEndpoint)
	}
	return credentials.Login{
		ID:      credentials.ID{Site: l.client.Site(), Endpoint: endpoint},
		Key:     issued.Key,
		Access:  issued.Access,
		Expires: issued.Expires,
		Account: issued.Account,
	}, nil
}

// approvedBy is what the person checks before a login is stored: whoever
// submits a code first decides it, so this is how someone else's approval
// shows, together with the spaces, and their owners, that it reaches.
func approvedBy(l credentials.Login, spaces []sitelogin.Space) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Approved by %s (%s) %s.\n  Storage endpoint: %s\n  Site: %s\n", l.Account, l.Access.Describe(), l.Expires.Describe(), l.Endpoint, l.Site)
	if len(spaces) == 0 {
		b.WriteString("  Spaces: none yet\n")
		return b.String()
	}
	b.WriteString("  Spaces:\n")
	for _, s := range spaces {
		fmt.Fprintf(&b, "    %s\n", describeSpace(s))
	}
	return b.String()
}

// describeSpace is one listed space: its name, access and owner, "a team"
// when no person owns it.
func describeSpace(s sitelogin.Space) string {
	owner := s.Owner
	if owner == "" {
		owner = "a team"
	}
	return fmt.Sprintf("%s (%s), owned by %s", s.Name, s.Access.Describe(), owner)
}

// spaceNames lists the space names for a refusal; "no space" for none.
func spaceNames(spaces []sitelogin.Space) string {
	if len(spaces) == 0 {
		return "no space"
	}
	names := make([]string, 0, len(spaces))
	for _, s := range spaces {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
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

// storeOutcome is what storing a login changed: the login it replaced,
// nil when there was none; the default space before and after, empty for
// none; whether an earlier build's file was replaced; and why the lock
// could not be released after the login was saved, if so.
type storeOutcome struct {
	replaced     *credentials.Login
	priorDefault string
	defaultSpace string
	file         fileOutcome
	// earlier is what revoking the replaced earlier file's tokens did.
	earlier   earlierRevocation
	unlockErr error
}

// earlierRevocation counts the tokens of a replaced earlier file that were
// revoked, and holds why each other one was not.
type earlierRevocation struct {
	revoked int
	failed  []error
}

// fileOutcome is whether storing a login replaced an earlier build's
// credentials file.
type fileOutcome int

const (
	fileKept fileOutcome = iota
	fileReplaced
)

// store writes the login under the credentials lock. It refuses a login
// for an endpoint another site's login holds, and, unless the person
// agreed, one that would replace a login another account approved. The
// default space becomes --space, else the only space listed, else the
// stored default when the login still reaches it; otherwise there is none.
func (l *Login) store(ctx context.Context, stored credentials.Login, spaces []sitelogin.Space, agreed consent) (out storeOutcome, errOut error) {
	lock, err := l.file.Lock(ctx)
	if err != nil {
		return storeOutcome{}, fmt.Errorf("login: %w; nothing was stored", err)
	}
	saved := false
	defer func() {
		unlock := l.unlock
		if unlock == nil {
			unlock = (*credentials.Lock).Unlock
		}
		err := unlock(lock)
		switch {
		case err == nil:
		case saved:
			// The key is on disk: failing the login now would revoke a
			// key the file holds, so the error is reported beside it.
			out.unlockErr = err
		case errOut == nil:
			errOut = fmt.Errorf("login: %w", err)
		}
	}()
	// Read under the lock, so a login that finished in another terminal
	// meanwhile keeps its entry.
	set, err := l.file.Load()
	var outdated *credentials.OutdatedFileError
	if errors.As(err, &outdated) {
		set, out.file = credentials.Set{}, fileReplaced
		out.earlier = l.revokeEarlier(ctx, outdated.Tokens)
	} else if err != nil {
		return storeOutcome{}, fmt.Errorf("login: %w; nothing was stored", err)
	}
	for _, other := range set.Logins() {
		if other.Endpoint == stored.Endpoint && other.Site != stored.Site {
			return storeOutcome{}, fmt.Errorf("login: the stored login for %s was issued by %s, not %s; log out of that login first (slivingdoc logout --site %s); nothing was stored",
				mcp.Redact(other.Endpoint), mcp.Redact(other.Site), stored.Site, mcp.Redact(other.Site))
		}
	}
	if old, err := set.Lookup(stored.ID); err == nil {
		if old.Account != stored.Account && agreed != confirmed {
			return storeOutcome{}, fmt.Errorf("login: the stored login for %s was approved by %s, this one by %s; run login in a terminal to confirm, or pass --force; nothing was stored",
				mcp.Redact(old.Endpoint), old.Account, stored.Account)
		}
		out.replaced = &old
	}
	out.priorDefault, _ = set.DefaultSpace(stored.Endpoint)
	set.Put(stored)
	out.defaultSpace = chooseDefault(l.space, spaces, out.priorDefault)
	if out.defaultSpace == "" {
		set.ClearDefaultSpace(stored.Endpoint)
	} else if err := set.SetDefaultSpace(stored.Endpoint, out.defaultSpace); err != nil {
		return storeOutcome{}, fmt.Errorf("login: %w; nothing was stored", err)
	}
	if err := l.file.Save(set); err != nil {
		return storeOutcome{}, fmt.Errorf("login: store the login: %w", err)
	}
	saved = true
	return out, nil
}

// revokeEarlier revokes, best effort, each token of an earlier build's
// file at the site that issued it, before the file is replaced: the new
// login does not own them, and nothing would list them afterwards.
func (l *Login) revokeEarlier(ctx context.Context, tokens []credentials.OutdatedToken) earlierRevocation {
	rctx, cancel := revocationContext(ctx)
	defer cancel()
	var out earlierRevocation
	for _, t := range tokens {
		client, err := siteClient(t.Site, l.opts)
		if err == nil {
			err = client.Revoke(rctx, t.Token)
		}
		if err != nil {
			out.failed = append(out.failed, fmt.Errorf("at %s: %s", mcp.Redact(t.Site), mcp.Redact(err.Error())))
			continue
		}
		out.revoked++
	}
	return out
}

// chooseDefault is the default space after a login: the one asked for,
// else the only one listed, else the prior default when still listed.
func chooseDefault(asked string, spaces []sitelogin.Space, prior string) string {
	switch {
	case asked != "":
		return asked
	case len(spaces) == 1:
		return spaces[0].Name
	case prior != "" && slices.ContainsFunc(spaces, func(s sitelogin.Space) bool { return s.Name == prior }):
		return prior
	default:
		return ""
	}
}

// report tells the person what the stored login changed besides itself:
// the default space, an earlier build's file it replaced, and the key it
// replaced, which is revoked only when the same account approved it.
func (l *Login) report(ctx context.Context, stored credentials.Login, spaces []sitelogin.Space, out storeOutcome) {
	errOut := l.opts.errOut()
	switch {
	case out.defaultSpace != "" && out.priorDefault != "" && out.priorDefault != out.defaultSpace:
		fmt.Fprintf(errOut, "The default space changed from %q to %q.\n", out.priorDefault, out.defaultSpace)
	case out.defaultSpace != "":
		fmt.Fprintf(errOut, "The default space is %q; 'slivingdoc space <name>' changes it.\n", out.defaultSpace)
	case len(spaces) == 0:
		fmt.Fprintln(errOut, "The login reaches no space yet; create one on the site, then run 'slivingdoc space <name>' to make it the default.")
	default:
		if out.priorDefault != "" {
			fmt.Fprintf(errOut, "The default space %q is not among the login's spaces, so it was cleared.\n", out.priorDefault)
		}
		fmt.Fprintln(errOut, "Run 'slivingdoc space <name>' to choose the default space, or pass --space to serve, pull and commit.")
	}
	if out.file == fileReplaced {
		fmt.Fprintf(errOut, "The credentials file of an earlier slivingdoc was replaced; %d of its tokens were revoked.\n", out.earlier.revoked)
		if len(out.earlier.failed) > 0 {
			fmt.Fprintf(errOut, "%d could not be revoked (%s); revoke them on the Tokens page.\n",
				len(out.earlier.failed), errors.Join(out.earlier.failed...))
		}
	}
	replaced := out.replaced
	if replaced == nil || replaced.Key == stored.Key {
		return
	}
	if replaced.Account != stored.Account {
		fmt.Fprintf(errOut, "The earlier login key for %s was approved by %s, not %s, so it was not revoked; revoke it on the Tokens page if it is no longer needed.\n",
			mcp.Redact(replaced.Endpoint), replaced.Account, stored.Account)
		return
	}
	rctx, cancel := revocationContext(ctx)
	defer cancel()
	if err := revoke(rctx, *replaced, l.opts); err != nil {
		fmt.Fprintf(errOut, "The earlier login key for %s could not be revoked (%s); revoke it on the Tokens page.\n",
			mcp.Redact(replaced.Endpoint), mcp.Redact(err.Error()))
	}
}

// revocationTimeout bounds a revocation that runs after the login's own
// context may have ended.
const revocationTimeout = 10 * time.Second

// revocationContext keeps ctx's values but not its cancellation, so an
// interrupt that refused a login still revokes the key it was given,
// within revocationTimeout.
func revocationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), revocationTimeout)
}

// discard revokes an issued key the login refuses to store, so it does not
// stay valid unseen, and returns cause; a failed revocation is added to
// it.
func (l *Login) discard(ctx context.Context, key string, cause error) error {
	rctx, cancel := revocationContext(ctx)
	defer cancel()
	if err := l.client.Revoke(rctx, key); err != nil {
		return fmt.Errorf("%w; revoking the issued login key failed too (%s), revoke it on the Tokens page", cause, mcp.Redact(err.Error()))
	}
	return cause
}

// loggedIn is the login's result line. It names the approving account, the
// endpoint when it is not the default, and the default space when there
// is one.
func loggedIn(l credentials.Login, defaultSpace string) string {
	line := "Logged in as " + l.Account
	if l.Endpoint != DefaultHostedEndpoint {
		line += " at " + l.Endpoint
	}
	line += fmt.Sprintf(" (%s) %s", l.Access.Describe(), l.Expires.Describe())
	if defaultSpace != "" {
		line += fmt.Sprintf("; default space %q", defaultSpace)
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

// PrepareLogout chooses the stored logins to withdraw: every one, or
// those the configured site issued when --site or SLIVINGDOC_SITE names
// one.
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
	logins := set.Logins()
	if len(logins) == 0 {
		return nil, fmt.Errorf("logout: not logged in: %w", credentials.ErrNoLogin)
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
			return nil, fmt.Errorf("logout: %w issued by %s (from --site or %s)", credentials.ErrNoLogin, client.Site(), SiteEnv)
		}
		logins = kept
	}
	return &Logout{opts: opts, file: file, logins: logins}, nil
}

// Run revokes each chosen key, and so every token minted from it, at the
// site that issued it, and removes the login with its endpoint's default
// space from the credentials file; a key the site no longer knows counts
// as revoked. A login whose revocation fails stays stored, so the logout
// can be repeated.
func (l *Logout) Run(ctx context.Context) error {
	var failed []error
	var revoked []credentials.Login
	for _, login := range l.logins {
		if err := revoke(ctx, login, l.opts); err != nil {
			failed = append(failed, fmt.Errorf("%s at %s: %s", login.Account, mcp.Redact(login.Endpoint), mcp.Redact(err.Error())))
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
			if slices.Contains(kept, login.ID) {
				fmt.Fprintf(l.opts.Out(), "Revoked the login key of %s at %s; a newer login for it was kept\n", login.Account, mcp.Redact(login.Endpoint))
				continue
			}
			fmt.Fprintf(l.opts.Out(), "Logged out of %s at %s; the login key and its tokens were revoked\n", login.Account, mcp.Redact(login.Endpoint))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("logout: the login key could not be revoked and stays stored; retry, or revoke it on the Tokens page: %w", errors.Join(failed...))
	}
	return nil
}

// remove deletes the revoked logins from the file under the credentials
// lock. A login another process removed meanwhile is already gone; one
// another process replaced with a newer key since it was read is kept,
// and its ID returned, because deleting it would orphan a live key.
func (l *Logout) remove(ctx context.Context, revoked []credentials.Login) (kept []credentials.ID, errOut error) {
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
		current, err := set.Lookup(login.ID)
		if errors.Is(err, credentials.ErrNoLogin) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("logout: %w", err)
		}
		if current.Key != login.Key {
			kept = append(kept, login.ID)
			continue
		}
		if err := set.Remove(login.ID); err != nil {
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
	return client.Revoke(ctx, login.Key)
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
