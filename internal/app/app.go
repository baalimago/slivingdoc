// Package app wires configuration, dependency construction, startup, and
// shutdown for the slivingdoc process body: it parses flags and the
// environment, opens the pinned native engine, runs the startup store check
// (the S3 compatibility probe or the hosted access check), and serves the
// two MCP tools over stdio until the client
// disconnects or a termination signal starts the bounded shutdown
// (architecture/cli.md, product-contract.md, config.md, and security.md).
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/storage"
)

// Version is the slivingdoc release version. Release builds override it
// with the tag-derived version through the linker (-X
// github.com/baalimago/slivingdoc/internal/app.Version); development
// builds keep the -dev suffix.
var Version = "0.1.0-dev"

// probeTimeout bounds the startup store check: the S3 compatibility probe
// or the hosted access check.
const probeTimeout = 30 * time.Second

// StoreFactory builds the semantic object-store boundary from a resolved
// service configuration. The default is realStoreFactory; the integration
// harness and process scenarios inject deterministic or fault-injecting
// factories.
type StoreFactory func(ctx context.Context, cfg ServiceConfig) (storage.ObjectStore, error)

// ProcessOptions is the injectable environment of the process body. The
// zero value substitutes the operating-system defaults: os.Args, the
// environment, the working directory, the user cache directory, the real
// store factory, and OS termination signals. Stdout carries only MCP
// protocol messages and --help/--version output; logs go to Stderr.
type ProcessOptions struct {
	Args             []string
	Env              []string
	Cwd              string
	CacheDir         string
	Stdout           io.Writer
	Stderr           io.Writer
	Signals          <-chan os.Signal
	StoreFactory     StoreFactory
	ShutdownDeadline time.Duration

	// Ephemeral asks for process-owned temporary roots when neither the
	// workspace root nor the private root is configured
	// (architecture/config.md). The serve command sets it; the one-shot
	// subcommands address a real directory and leave it false.
	Ephemeral bool

	// NewSessionDir creates the ephemeral session directory. Nil uses the
	// operating-system temporary directory.
	NewSessionDir func() (string, error)

	// Logger is the process logger. Nil builds one from the environment
	// (LOG_LEVEL and NO_COLOR) over Stderr.
	Logger *slog.Logger

	// OpenBrowser opens the login approval page. Nil starts the platform
	// opener (xdg-open, open, or the Windows URL handler); tests inject one
	// so no browser ever starts (architecture/login.md).
	OpenBrowser func(url string) error

	// SiteClient sends the site's login, token and revoke requests. Nil
	// uses a client that never follows a redirect; the integration helper
	// routes the default site to a reference site through it.
	SiteClient sitelogin.Doer

	// Sleep waits between two login polls. Nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error

	// Hostname labels a login's token. Nil is os.Hostname.
	Hostname func() (string, error)

	// Stdin is where login reads the answer to its confirmation prompt.
	// Nil is the process stdin.
	Stdin io.Reader

	// Terminal says whether a person can answer that prompt. Nil checks
	// that the process stdin and stderr are both terminals.
	Terminal func() TerminalState
}

// TerminalState says whether login can ask the person at the keyboard.
type TerminalState int

const (
	// NoTerminal is a script, a pipe or a service: nobody can answer.
	NoTerminal TerminalState = iota
	// OnTerminal is stdin and stderr both on a terminal.
	OnTerminal
)

// process is the resolved environment of the process body. Setup fills
// every field from the options and the operating system; tests
// substitute fields directly through run.
type process struct {
	args     []string
	env      []string
	cwd      string
	cacheDir string

	// flags carries the already-parsed serve flags when the command router
	// owns the command line. When nil, args is parsed instead.
	flags *Flags

	engine git.Engine
	stdout io.Writer
	stderr io.Writer

	logger           *slog.Logger
	signals          <-chan os.Signal
	transport        sdk.Transport
	storeFactory     func(ctx context.Context, cfg config) (storage.ObjectStore, error)
	hooks            *ServiceHooks
	shutdownDeadline time.Duration

	ephemeral     bool
	newSessionDir func() (string, error)
}

// Setup resolves the configuration, opens the pinned native engine, builds
// the object store, runs the startup store check, and wires the MCP
// server. Every failure is a startup refusal: no transport runs and no tool
// call is accepted. A non-nil flags holder is an already-parsed command
// line, which is how the serve command supplies it; nil parses opts.Args.
//
// The caller owns the returned Runtime and must Close it.
func Setup(engine git.Engine, flags *Flags, opts ProcessOptions) (*Runtime, error) {
	if engine == nil {
		return nil, errors.New("app: engine is required")
	}
	args := opts.Args
	if args == nil {
		args = os.Args[1:]
	}
	env := opts.Env
	if env == nil {
		env = os.Environ()
	}
	sig := opts.Signals
	if sig == nil {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, terminationSignals...)
		sig = ch
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		var err error
		cacheDir, err = os.UserCacheDir()
		if err != nil {
			cacheDir = ""
		}
	}
	cwd := opts.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			cwd = ""
		}
	}
	storeFactory := opts.StoreFactory
	var wrapped func(ctx context.Context, cfg config) (storage.ObjectStore, error)
	if storeFactory != nil {
		wrapped = func(ctx context.Context, cfg config) (storage.ObjectStore, error) {
			return storeFactory(ctx, cfg.serviceConfig())
		}
	} else {
		wrapped = realStoreFactory
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	deadline := opts.ShutdownDeadline
	if deadline == 0 {
		deadline = 30 * time.Second
	}
	return setup(process{
		args:             args,
		flags:            flags,
		logger:           opts.Logger,
		env:              env,
		cwd:              cwd,
		cacheDir:         cacheDir,
		engine:           engine,
		stdout:           stdout,
		stderr:           stderr,
		signals:          sig,
		storeFactory:     wrapped,
		shutdownDeadline: deadline,
		ephemeral:        opts.Ephemeral,
		newSessionDir:    opts.NewSessionDir,
	})
}

// Runtime is a constructed process body: the configuration is validated,
// the native engine is open, and the object store has passed the startup
// check (the S3 compatibility probe or the hosted access check). Serve runs
// the MCP server over it; Pull and Commit run the same notebook operations
// directly for the CLI subcommands. Close releases the service and the
// engine.
type Runtime struct {
	p      process
	svc    *Service
	cfg    config
	base   *slog.Logger // process logger; module loggers derive from it
	logger *slog.Logger
}

// Serve runs the MCP server until the client disconnects, ctx is cancelled,
// or a termination signal starts the bounded shutdown.
func (r *Runtime) Serve(ctx context.Context) error {
	r.logger.Info("serving",
		"bucket", r.cfg.bucket,
		"hosted", r.cfg.hosted(),
		"notebookRoot", r.cfg.workspaceRoot,
		"ephemeral", r.cfg.sessionDir != "")
	srv := mcp.NewServer(r.svc, Version, Module(r.base, ModuleMCP))
	return serve(ctx, r.p, srv, r.logger)
}

// Pull writes the current notebook into path for one CLI invocation and
// returns the operation result. An empty path is the workspace root.
func (r *Runtime) Pull(ctx context.Context, path string) (notebook.Result, error) {
	return r.svc.Pull(notebook.WithLogger(ctx, Module(r.base, ModuleNotebook)), r.resolve(path))
}

// Commit publishes the caller's changes at path for one CLI invocation and
// returns the operation result. An empty path is the workspace root.
func (r *Runtime) Commit(ctx context.Context, path, message string) (notebook.Result, error) {
	return r.svc.Commit(notebook.WithLogger(ctx, Module(r.base, ModuleNotebook)), r.resolve(path), message)
}

// ReadOnlyPaths returns the service's normalized, sorted read-only entries.
func (r *Runtime) ReadOnlyPaths() []string { return r.svc.ReadOnlyPaths() }

// WritablePaths returns the service's normalized, sorted writable entries.
func (r *Runtime) WritablePaths() []string { return r.svc.WritablePaths() }

// resolve maps an omitted CLI path to the workspace root.
func (r *Runtime) resolve(path string) string {
	if path == "" {
		return r.cfg.workspaceRoot
	}
	return path
}

// Close releases the notebook service and the native engine, then removes
// the ephemeral session directory when the process owns one. It is safe to
// call once per successful Setup.
func (r *Runtime) Close() error {
	r.svc.Close()
	err := r.p.engine.Close()
	if rmErr := removeSessionDir(r.cfg.sessionDir); rmErr != nil && err == nil {
		err = fmt.Errorf("app: remove session directory: %w", rmErr)
	}
	return err
}

// setup is the startup half of the process body: validate the
// configuration, open the native engine (the pinned-version check), then
// build the store, run the startup store check, and wire the service.
func setup(p process) (*Runtime, error) {
	base := p.logger
	if base == nil {
		var levelErr error
		base, levelErr = NewLogger(p.env, p.stderr)
		if levelErr != nil {
			Module(base, ModuleApp).Warn("falling back to the default log level", "error", levelErr)
		}
	}
	logger := Module(base, ModuleApp)

	cfg, err := loadConfig(p)
	if err != nil {
		return nil, fmt.Errorf("app: invalid configuration: %s", mcp.Redact(err.Error()))
	}
	if cfg.logConfigured {
		// A logging flag (or SLIVINGDOC_LOG_TIMESTAMP) resolves only after
		// the flags parse, so the runtime logger is rebuilt here; records
		// before this point follow the environment configuration.
		rebuilt, levelErr := runtimeLogger(cfg, p.env, p.stderr)
		if levelErr != nil {
			Module(rebuilt, ModuleApp).Warn("falling back to the default log level", "error", levelErr)
		}
		base = rebuilt
		logger = Module(base, ModuleApp)
	}
	logStorage(logger, cfg, environ(p.env))
	if err := p.engine.Open(); err != nil {
		removeSessionDir(cfg.sessionDir)
		return nil, fmt.Errorf("app: open native engine: %w", err)
	}
	logger.Debug("native engine open", "pinned", true)
	svc, resolved, err := buildService(p, cfg)
	if err != nil {
		p.engine.Close()
		removeSessionDir(cfg.sessionDir)
		return nil, err
	}
	if resolved.hosted() {
		logger.Info("hosted space resolved", "space", resolved.bucket, "from", resolved.bucketFrom.String())
	}
	return &Runtime{p: p, svc: svc, cfg: resolved, base: base, logger: logger}, nil
}

// run is the whole process body in one call, used where the caller does not
// need the startup and serving phases apart.
func run(p process) error {
	rt, err := setup(p)
	if err != nil {
		return err
	}
	defer rt.Close()
	return rt.Serve(context.Background())
}

// buildService resolves the hosted space, constructs the S3 or hosted
// store, runs the startup check, and wires the notebook service. It returns
// the configuration with the resolved space. Any failure is a startup
// refusal: no transport runs and no operation is accepted.
func buildService(p process, cfg config) (*Service, config, error) {
	storeFactory := p.storeFactory
	if storeFactory == nil {
		storeFactory = realStoreFactory
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	if cfg.hosted() {
		var err error
		logins := func() (credentials.Set, error) { return loadLogins(environ(p.env), runtime.GOOS) }
		if cfg, err = resolveHostedSpace(probeCtx, cfg, logins); err != nil {
			return nil, config{}, err
		}
	}
	store, err := storeFactory(context.Background(), cfg)
	if err != nil {
		return nil, config{}, err
	}
	if err := checkStore(probeCtx, store, cfg); err != nil {
		return nil, config{}, err
	}
	svc, err := NewService(p.engine, store, cfg.serviceConfig(), p.hooks)
	if err != nil {
		return nil, config{}, err
	}
	return svc, cfg, nil
}

// resolveHostedSpace asks the hosted API which space the token reaches
// (architecture/hosted-mode.md, architecture/login.md). With no bucket
// that space is used. A bucket that names another space is refused rather
// than either one preferred, whether it came from --space or --bucket,
// SLIVINGDOC_SPACE or SLIVINGDOC_BUCKET, or the stored login the token belongs to. A server
// that cannot answer keeps a given bucket, whose access check then proves
// the token. Without one, a SLIVINGDOC_TOKEN process falls back to the
// default login's space when that login is for the same endpoint; logins
// reads the stored logins only then.
func resolveHostedSpace(ctx context.Context, cfg config, logins func() (credentials.Set, error)) (config, error) {
	info, err := httpstore.DescribeToken(ctx, httpstore.Config{
		Endpoint:  cfg.endpoint,
		Token:     cfg.token,
		UserAgent: "slivingdoc/" + Version,
	})
	switch {
	case errors.Is(err, httpstore.ErrTokenLookupUnsupported) && cfg.bucket != "":
		return cfg, nil
	case errors.Is(err, httpstore.ErrTokenLookupUnsupported):
		return defaultLoginSpace(cfg, logins)
	case err != nil:
		return config{}, hostedCheckError(err, cfg)
	case cfg.bucket == "":
		cfg.bucket, cfg.bucketFrom = info.Space, bucketFromToken
		return cfg, nil
	case cfg.bucket != info.Space:
		return config{}, spaceMismatch(cfg, info.Space)
	default:
		return cfg, nil
	}
}

// defaultLoginSpace is the fallback for a server that cannot name the
// token's space: the default login's space, when that login was issued for
// the endpoint the token goes to. The refusals name what is missing: a
// readable credentials file, a default login, or one for this endpoint.
func defaultLoginSpace(cfg config, logins func() (credentials.Set, error)) (config, error) {
	const refusal = "app: hosted storage cannot name the token's space"
	const fix = "pass the space name as --space or SLIVINGDOC_SPACE"
	set, err := logins()
	if err != nil {
		return config{}, fmt.Errorf("%s, and the stored logins cannot supply it: %w; fix or remove the credentials file, or %s",
			refusal, redactCause(err, credentials.ErrMalformed, credentials.ErrExposed), fix)
	}
	def, err := set.Default()
	switch {
	case err != nil:
		return config{}, fmt.Errorf("%s, and no default login names one; %s", refusal, fix)
	case def.Endpoint != cfg.endpoint:
		// A hand-edited credentials file can hold user information in an
		// endpoint; the refusal never echoes it.
		return config{}, fmt.Errorf("%s, and the default login is for %s, not %s; %s", refusal, mcp.Redact(def.Endpoint), cfg.endpoint, fix)
	}
	cfg.bucket, cfg.bucketFrom = def.Space, bucketFromLogin
	return cfg, nil
}

// redactedError is a cause reduced to its redacted text. It unwraps only to
// the sentinel the cause matched, so errors.Is still classifies it while
// the raw cause never travels in the chain.
type redactedError struct {
	text string
	kind error
}

func (e redactedError) Error() string { return e.text }
func (e redactedError) Unwrap() error { return e.kind }

func redactCause(err error, kinds ...error) error {
	out := redactedError{text: mcp.Redact(err.Error())}
	for _, kind := range kinds {
		if errors.Is(err, kind) {
			out.kind = kind
			break
		}
	}
	return out
}

// spaceMismatch is the refusal for a token that reaches another space than
// the bucket the configuration named, worded for the setting that named it.
func spaceMismatch(cfg config, tokenSpace string) error {
	switch {
	case cfg.tokenOrigin == originLogin:
		return fmt.Errorf("app: the stored login for space %q holds a token that reaches hosted space %q; run 'slivingdoc login --space %s' again",
			cfg.bucket, tokenSpace, cfg.bucket)
	case cfg.bucketFrom.kind() == kindEnv:
		return fmt.Errorf("app: the token reaches hosted space %q, not %q from %s; unset %s to use the token's space, or use a token made for %q",
			tokenSpace, cfg.bucket, cfg.bucketFrom, cfg.bucketFrom, cfg.bucket)
	default:
		name := cfg.bucketFrom.String()
		if cfg.bucketFrom.kind() != kindFlag {
			name = "--space"
		}
		return fmt.Errorf("app: the token reaches hosted space %q, not %q from %s; drop %s to use the token's space, or use a token made for %q",
			tokenSpace, cfg.bucket, name, name, cfg.bucket)
	}
}

// accessChecker is a store that proves itself without the write probe: a
// hosted store whose server promises the conditional-write semantics and
// whose token may be read-only.
type accessChecker interface {
	CheckAccess(ctx context.Context) error
}

// checkStore proves the store before any request is served: the hosted
// access check when the store offers one, else the S3 compatibility probe.
// cfg names where a hosted token and its space came from, so a refused
// token points at the fix that applies (hostedCheckError).
func checkStore(ctx context.Context, store storage.ObjectStore, cfg config) error {
	checker, ok := store.(accessChecker)
	if !ok {
		if err := storage.Probe(ctx, store); err != nil {
			// The probe names its disposable protocol key; the startup
			// diagnostic never echoes it.
			return fmt.Errorf("app: INCOMPATIBLE_STORE: S3 compatibility probe failed: %s", mcp.Redact(err.Error()))
		}
		return nil
	}
	if err := checker.CheckAccess(ctx); err != nil {
		return hostedCheckError(err, cfg)
	}
	return nil
}

// hostedCheckError is the startup refusal for a failed hosted check. A
// refused token points at the fix that applies: logging in again for a
// stored login, else the variable and the setting the space came from.
func hostedCheckError(err error, cfg config) error {
	denied := errors.Is(err, storage.ErrAccessDenied)
	switch {
	case denied && cfg.tokenOrigin == originLogin:
		return fmt.Errorf("app: hosted storage refused the stored login: %s; run 'slivingdoc login' again, or check --space", mcp.Redact(err.Error()))
	case denied && cfg.bucketFrom == bucketFromLogin:
		return fmt.Errorf("app: hosted storage refused the token: %s; check SLIVINGDOC_TOKEN, or pass --space: the space %q came from the default login", mcp.Redact(err.Error()), cfg.bucket)
	case denied && cfg.bucketFrom == bucketFromToken:
		return fmt.Errorf("app: hosted storage refused the token: %s; check SLIVINGDOC_TOKEN: it named space %q but was then refused", mcp.Redact(err.Error()), cfg.bucket)
	case denied && cfg.bucketFrom.kind() != kindOther:
		return fmt.Errorf("app: hosted storage refused the token: %s; check SLIVINGDOC_TOKEN and %s", mcp.Redact(err.Error()), cfg.bucketFrom)
	case denied:
		return fmt.Errorf("app: hosted storage refused the token: %s; check SLIVINGDOC_TOKEN and --space", mcp.Redact(err.Error()))
	case errors.Is(err, storage.ErrIncompatible):
		return fmt.Errorf("app: INCOMPATIBLE_STORE: hosted storage check failed: %s", mcp.Redact(err.Error()))
	default:
		return fmt.Errorf("app: hosted storage check failed: %s", mcp.Redact(err.Error()))
	}
}

// serve runs the MCP server until the transport ends, the caller's context
// is cancelled, or a termination signal arrives. Either cancellation stops
// new requests, cancels in-flight request contexts, and starts the bounded
// shutdown; the server must stop within the shutdown deadline, or the
// process reports a forced shutdown (architecture/cli.md).
func serve(parent context.Context, p process, srv *mcp.Server, logger *slog.Logger) error {
	transport := p.transport
	if transport == nil {
		transport = &sdk.StdioTransport{}
	}
	closing := &closeTransport{Transport: transport}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, closing) }()

	var reason string
	select {
	case err := <-done:
		// The client closed the transport: clean shutdown.
		return err
	case <-parent.Done():
		reason = "context cancelled"
	case sig := <-p.signals:
		reason = "termination signal " + sig.String()
	}

	logger.Info("shutting down", "reason", reason)
	cancel()
	// The SDK cancels in-flight request contexts only when the transport
	// read or write fails; closing the connection makes that happen, so
	// handlers unwind promptly.
	_ = closing.Close()
	timer := time.NewTimer(p.shutdownDeadline)
	defer timer.Stop()
	select {
	case <-done:
		// The shutdown was initiated by us: the transport failure that
		// ended the session is the expected outcome.
		return nil
	case <-timer.C:
		return errors.New("app: shutdown deadline expired")
	}
}

// closeTransport wraps a transport and records the live connection, so the
// process body can terminate the session from the signal path. Closing the
// connection unblocks the SDK read loop, which cancels every in-flight
// request context (SDK behavior on transport failure).
type closeTransport struct {
	sdk.Transport
	mu   sync.Mutex
	conn sdk.Connection
}

func (t *closeTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	conn, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()
	return conn, nil
}

// SupportsProtocolVersion delegates to the wrapped transport so the SDK
// version negotiation sees the inner transport's capability.
func (t *closeTransport) SupportsProtocolVersion(version string) bool {
	if ps, ok := t.Transport.(sdk.ProtocolVersionSupporter); ok {
		return ps.SupportsProtocolVersion(version)
	}
	return true
}

// Close closes the live connection. It is safe before Connect and safe to
// call repeatedly.
func (t *closeTransport) Close() error {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

// realStoreFactory builds the object store from the resolved
// configuration. A token selects the hosted storage API; otherwise it
// builds the S3 adapter: region and base endpoint from the configuration,
// path-style addressing per --path-style, and the bucket and prefix join
// owned by the adapter. The AWS SDK stays inside internal/s3store.
func realStoreFactory(ctx context.Context, cfg config) (storage.ObjectStore, error) {
	if cfg.hosted() {
		store, err := httpstore.New(httpstore.Config{
			Endpoint:  cfg.endpoint,
			Space:     cfg.bucket,
			Prefix:    cfg.prefix,
			Token:     cfg.token,
			UserAgent: "slivingdoc/" + Version,
		})
		if err != nil {
			// %s over the redacted text, not %w: the cause can quote the
			// token, and a wrapped error would carry it past the redaction
			// to any caller that prints the chain.
			return nil, fmt.Errorf("app: create hosted store: %s", mcp.Redact(err.Error()))
		}
		return store, nil
	}
	store, err := s3store.New(ctx, s3store.Config{
		Bucket:   cfg.bucket,
		Prefix:   cfg.prefix,
		Region:   cfg.region,
		Endpoint: cfg.endpoint,
	}, s3store.Options{ForcePathStyle: cfg.pathStyle})
	if err != nil {
		return nil, fmt.Errorf("app: create object store: %w", err)
	}
	return store, nil
}

// endpointForLog is an endpoint URL reduced to its scheme and host, so
// neither user information nor a path reaches the log; a value without a
// host (unparsable, or "user:secret@host" with no scheme, which parses as
// an opaque URL) is logged as unparsable.
func endpointForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return "an unparsable URL"
	}
	host, _, _ := strings.Cut(u.Hostname(), "%")
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	return u.Scheme + "://" + host
}

// logStorage records the store the configuration chose, so an operator
// can see at startup whether a stored login or SLIVINGDOC_TOKEN turned the
// process hosted (architecture/login.md). It never logs the token. An S3
// process with no endpoint of its own names the variable the AWS SDK will
// read instead (AWS_ENDPOINT_URL_S3, then AWS_ENDPOINT_URL: an explicitly
// empty --endpoint does not clear them), and otherwise says the SDK may
// still take one from the environment or a shared profile.
func logStorage(logger *slog.Logger, cfg config, env map[string]string) {
	backend, source, endpoint := "s3", "none", cfg.endpoint
	if cfg.hosted() {
		backend = "hosted"
		source = "env"
		if cfg.tokenOrigin == originLogin {
			source = "login"
		}
	}
	if endpoint == "" {
		endpoint = "aws-default (SDK: env or profile)"
		for _, name := range []string{"AWS_ENDPOINT_URL_S3", "AWS_ENDPOINT_URL"} {
			if env[name] != "" {
				endpoint = endpointForLog(env[name]) + " (" + name + ")"
				break
			}
		}
	}
	space := cfg.bucket
	if space == "" && cfg.hosted() {
		// resolveHostedSpace asks the API after this record, and setup
		// logs the space it resolved.
		space = "the token's own"
	}
	logger.Info("storage selected", "backend", backend, "endpoint", endpoint, "space", space, "token", source)
}
