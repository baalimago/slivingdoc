package app

// The first-use sign-in of serve (architecture/login.md, Sign in on first
// use). A serve process that finds no credential for hosted storage starts
// anyway, with the two tools, and signs in when a tool is first called: it
// runs the device login of 'slivingdoc login' in the background, answers
// the call with the approval page while the person has not approved it,
// and builds the store once the credentials file holds a usable login.
// Several servers on one machine share one sign-in through the sign-in
// lock of the credentials directory.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
)

// LoginEnv is the variable of serve's --login.
const LoginEnv = "SLIVINGDOC_LOGIN"

// The values of --login: auto signs in on first use, never keeps the
// startup refusal.
const (
	loginAuto  = "auto"
	loginNever = "never"
)

func parseLoginMode(s string) (string, error) {
	switch s {
	case loginAuto, loginNever:
		return s, nil
	default:
		return "", fmt.Errorf("--login must be auto or never, not %q", s)
	}
}

// needsSignIn marks a startup refusal that a stored login resolves: no
// credential reaches hosted storage, the stored one has expired, or S3 has
// no bucket. Its text is the refusal's own.
type needsSignIn struct{ err error }

func (e needsSignIn) Error() string { return e.err.Error() }
func (e needsSignIn) Unwrap() error { return e.err }

// noDefaultSpace marks the refusal of a usable stored login that names no
// default space. serve resolves it on the first tool call by choosing one
// (adoptDefault); every other command keeps the refusal.
type noDefaultSpace struct {
	login credentials.Login
	err   error
}

func (e noDefaultSpace) Error() string { return e.err.Error() }
func (e noDefaultSpace) Unwrap() error { return e.err }

// pendsOnFirstUse reports whether serve starts with err pending and
// resolves it when a tool is first called.
func pendsOnFirstUse(err error) bool {
	return errors.As(err, new(needsSignIn)) || errors.As(err, new(noDefaultSpace))
}

// signInSpace is the space a first sign-in creates when the account has
// none yet, so a new account works without a visit to the site.
const signInSpace = "notes"

// The tool call that meets a pending sign-in waits up to signInWait for
// the approval, checking every signInPoll, before it answers with the page;
// signInStart bounds the wait for the site's first answer.
const (
	signInWait  = 60 * time.Second
	signInPoll  = 2 * time.Second
	signInStart = 15 * time.Second
)

// signIn is the pending state of a serve process that started without a
// credential. The service is built at most once; until then every tool
// call goes through await.
type signIn struct {
	rt *Runtime
	// turn admits one tool call at a time into await; a caller whose
	// context ends while it waits for its turn gives up.
	turn chan struct{}
	// ready is set once rt.svc and rt.cfg hold the built service.
	ready bool
	// flow is the device login this process runs, nil when none runs.
	flow *signInFlow
	// lastErr is why the last sign-in of this process ended without a
	// login, told once with the next page.
	lastErr error
	// ctx bounds the background login: the serve lifetime.
	ctx context.Context

	root     string
	readOnly []string
	writable []string
}

// signInFlow is one device login this process runs while it holds the
// sign-in lock.
type signInFlow struct {
	lock *credentials.SignInLock
	// shown is closed once page is set; done once err is.
	shown chan struct{}
	page  credentials.SignIn
	done  chan struct{}
	err   error
}

func newSignIn(rt *Runtime) *signIn {
	s := &signIn{rt: rt, turn: make(chan struct{}, 1), root: rt.cfg.workspaceRoot, ctx: context.Background()}
	s.readOnly = append([]string{}, rt.cfg.readOnlyPaths...)
	s.writable = append([]string{}, rt.cfg.writablePaths...)
	return s
}

// signInService is the MCP view of a process that has not signed in: the
// directory and the path sets are known from the configuration, and every
// operation first waits for the store.
type signInService struct{ s *signIn }

func (v signInService) Root() string            { return v.s.root }
func (v signInService) ReadOnlyPaths() []string { return v.s.readOnly }
func (v signInService) WritablePaths() []string { return v.s.writable }

func (v signInService) Pull(ctx context.Context) (notebook.Result, error) {
	if err := v.s.await(ctx); err != nil {
		return notebook.Result{}, err
	}
	return rememberingService{v.s.rt}.Pull(ctx)
}

func (v signInService) Commit(ctx context.Context, message string) (notebook.Result, error) {
	if err := v.s.await(ctx); err != nil {
		return notebook.Result{}, err
	}
	return rememberingService{v.s.rt}.Commit(ctx, message)
}

// await returns nil once the store is built. Until then it resolves the
// storage again, starts or joins a sign-in, and waits for the approval up
// to the process's wait; it then answers SIGN_IN_REQUIRED with the page to
// open, or SETUP_REQUIRED when the stored login cannot reach a space.
func (s *signIn) await(ctx context.Context) error {
	select {
	case s.turn <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.turn }()
	if s.ready {
		return nil
	}
	// The wait is counted in polls rather than read off the clock, so the
	// injected sleep of a test scales it with the login's own polls.
	for polls := s.rt.p.signInWait / s.rt.p.signInPoll; ; polls-- {
		err := s.build()
		if err == nil {
			return nil
		}
		var none noDefaultSpace
		if errors.As(err, &none) {
			if err := s.adoptDefault(ctx, none.login); err != nil {
				return setupRequired(err)
			}
			if err := s.build(); err != nil {
				return setupRequired(err)
			}
			return nil
		}
		if !errors.As(err, new(needsSignIn)) {
			return setupRequired(err)
		}
		if s.ended() {
			// Tell why at once rather than start the next login within
			// this call: a site that refuses every start is asked once per
			// call, not once per poll.
			return s.signInRequired(credentials.SignIn{})
		}
		page, err := s.pending(ctx)
		if err != nil {
			return err
		}
		if polls <= 0 {
			return s.signInRequired(page)
		}
		if err := s.pause(ctx); err != nil {
			return err
		}
	}
}

// ended reports whether this process's login ended and build still found no
// usable login; it then forgets that login and keeps why it ended, to tell
// once.
func (s *signIn) ended() bool {
	if s.flow == nil {
		return false
	}
	select {
	case <-s.flow.done:
	default:
		return false
	}
	s.lastErr, s.flow = s.flow.err, nil
	if s.lastErr == nil {
		s.lastErr = errors.New("a login was stored, but it does not apply to this server")
	}
	return true
}

// build resolves the storage again and, when a credential now reaches a
// space, builds the store and swaps it in.
func (s *signIn) build() error {
	cfg, err := s.rt.cfg.reresolve()
	if err != nil {
		return err
	}
	svc, resolved, check, err := buildService(s.rt.p, cfg, s.rt.gate)
	if err != nil {
		return err
	}
	s.rt.svc, s.rt.cfg, s.ready = svc, resolved, true
	logStorage(s.rt.logger, cfg, environ(s.rt.p.env))
	s.rt.logger.Info("signed in", "space", resolved.bucket, "from", resolved.bucketFrom.String())
	logStoreCheck(s.rt.logger, check)
	return nil
}

// adoptDefault stores a default space for a stored login that has none:
// its only space, else the space every account starts with
// (signInSpace) when it is listed, else that space, created, when the
// login reaches none. A login reaching several spaces without it is left
// for the person to choose.
func (s *signIn) adoptDefault(ctx context.Context, login credentials.Login) error {
	client, err := siteClient(login.Site, s.rt.p.login)
	if err != nil {
		return err
	}
	spaces, err := client.Spaces(ctx, login.Key)
	if err != nil {
		return fmt.Errorf("list the spaces of the stored login for %s: %w", login.Endpoint, err)
	}
	name := chooseDefault("", spaces, "")
	switch {
	case name != "":
	case len(spaces) == 0:
		created, err := client.CreateSpace(ctx, login.Key, signInSpace)
		if err != nil {
			return fmt.Errorf("create the space %q for the stored login for %s: %w", signInSpace, login.Endpoint, err)
		}
		name = created.Name
	case slices.ContainsFunc(spaces, func(sp sitelogin.Space) bool { return sp.Name == signInSpace }):
		name = signInSpace
	default:
		names := make([]string, len(spaces))
		for i, sp := range spaces {
			names[i] = sp.Name
		}
		return fmt.Errorf("the stored login for %s has no default space and reaches several (%s); run 'slivingdoc space <name>' to choose one",
			login.Endpoint, strings.Join(names, ", "))
	}
	file, err := credentialsFile(environ(s.rt.p.env))
	if err != nil {
		return err
	}
	if err := storeDefault(ctx, file, login, name); err != nil {
		return err
	}
	s.rt.logger.Info("default space chosen", "space", name)
	return nil
}

// pending returns the approval page a sign-in waits for, starting this
// process's own sign-in when no other process runs one. The page is empty
// while another process runs one that has not shown its page yet.
func (s *signIn) pending(ctx context.Context) (credentials.SignIn, error) {
	if s.flow != nil {
		select {
		case <-s.flow.shown:
			return s.flow.page, nil
		default:
			// The site has not answered yet; page is still being written.
			return credentials.SignIn{}, nil
		}
	}
	file, err := credentialsFile(environ(s.rt.p.env))
	if err != nil {
		return credentials.SignIn{}, setupRequired(err)
	}
	lock, err := file.TrySignIn()
	if errors.Is(err, credentials.ErrSignInHeld) {
		page, _, err := file.PendingSignIn(time.Now())
		if err != nil {
			return credentials.SignIn{}, setupRequired(err)
		}
		return page, nil
	}
	if err != nil {
		return credentials.SignIn{}, setupRequired(err)
	}
	flow, err := s.start(lock, file)
	if err != nil {
		lock.Release()
		return credentials.SignIn{}, setupRequired(err)
	}
	s.flow = flow
	timer := time.NewTimer(signInStart)
	defer timer.Stop()
	select {
	case <-flow.shown:
		return flow.page, nil
	case <-flow.done:
		// The site refused to start the login; the next turn of await
		// reports it and starts again.
		return credentials.SignIn{}, nil
	case <-timer.C:
		return credentials.SignIn{}, nil
	case <-ctx.Done():
		return credentials.SignIn{}, ctx.Err()
	}
}

// start runs the device login of 'slivingdoc login' in the background,
// under the held sign-in lock, as a login nobody at a terminal confirms.
func (s *signIn) start(lock *credentials.SignInLock, file credentials.File) (*signInFlow, error) {
	opts := s.rt.p.login
	env := environ(opts.Env)
	site := env[SiteEnv]
	if site == "" {
		site = sitelogin.DefaultSite
	}
	client, err := siteClient(site, opts)
	if err != nil {
		return nil, err
	}
	hostname := opts.Hostname
	if hostname == nil {
		hostname = os.Hostname
	}
	host, hostErr := hostname()
	if hostErr != nil {
		host = ""
	}
	flow := &signInFlow{lock: lock, shown: make(chan struct{}), done: make(chan struct{})}
	login := &Login{
		opts: opts, file: file, client: client, siteSource: SiteEnv, space: s.rt.cfg.bucket,
		access: credentials.AccessWrite, browser: true, hostname: host, hostErr: hostErr,
		createSpace: signInSpace,
		started: func(a sitelogin.Approval) {
			flow.page = credentials.SignIn{Page: a.CompleteURI, Code: a.UserCode, Expires: a.Deadline}
			if err := lock.Publish(flow.page); err != nil {
				s.rt.logger.Warn("sign-in record write failed", "error", mcp.Redact(err.Error()))
			}
			close(flow.shown)
		},
	}
	s.rt.logger.Info("signing in", "site", client.Site())
	go func() {
		err := login.Run(s.ctx)
		if relErr := lock.Release(); relErr != nil {
			s.rt.logger.Warn("sign-in lock release failed", "error", mcp.Redact(relErr.Error()))
		}
		if err != nil {
			s.rt.logger.Warn("sign-in ended without a login", "error", mcp.Redact(err.Error()))
		}
		flow.err = err
		close(flow.done)
	}()
	return flow, nil
}

// pause waits one poll, through the login's sleep when one is injected.
func (s *signIn) pause(ctx context.Context) error {
	if sleep := s.rt.p.login.Sleep; sleep != nil {
		return sleep(ctx, s.rt.p.signInPoll)
	}
	timer := time.NewTimer(s.rt.p.signInPoll)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// signInRequired is the answer while the approval is pending: the page and
// code the agent passes to the person, and why the last sign-in ended when
// one did.
func (s *signIn) signInRequired(page credentials.SignIn) error {
	msg := "slivingdoc is not signed in to its hosted storage yet. "
	if s.lastErr != nil {
		msg = fmt.Sprintf("The last sign-in ended without a login (%s). ", s.lastErr.Error()) + msg
	}
	switch {
	case page.Page == "" && s.lastErr != nil:
		msg += "Call this tool again to start a new sign-in."
	case page.Page == "":
		msg += "A sign-in is starting; call this tool again in a moment to get the page the person must open."
	default:
		msg += fmt.Sprintf("Ask the person to open %s and approve the code %s before %s (a browser window may already show it), then call this tool again.",
			page.Page, page.Code, page.Expires.UTC().Format("15:04 UTC"))
	}
	s.lastErr = nil
	return &notebook.Error{
		Code: notebook.CodeStorageFailure, Reason: notebook.ReasonSignInRequired, Action: notebook.ActionRetry,
		Message: msg,
	}
}

// setupRequired is the answer when the process cannot sign in, or signed in
// and still cannot reach a space: the refusal startup would have printed,
// for the person to fix.
func setupRequired(err error) error {
	return &notebook.Error{
		Code: notebook.CodeStorageFailure, Reason: notebook.ReasonSetupRequired, Action: notebook.ActionOperator,
		Message: "slivingdoc cannot reach its hosted storage: " + err.Error() + "; fix that, then call this tool again",
		Cause:   err,
	}
}
