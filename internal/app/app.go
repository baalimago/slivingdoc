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
	"os"
	"os/signal"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/s3store"
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
}

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
		if cfg, err = resolveHostedSpace(probeCtx, cfg); err != nil {
			return nil, config{}, err
		}
	}
	store, err := storeFactory(context.Background(), cfg)
	if err != nil {
		return nil, config{}, err
	}
	if err := checkStore(probeCtx, store); err != nil {
		return nil, config{}, err
	}
	svc, err := NewService(p.engine, store, cfg.serviceConfig(), p.hooks)
	if err != nil {
		return nil, config{}, err
	}
	return svc, cfg, nil
}

// resolveHostedSpace asks the hosted API which space the token reaches
// (architecture/hosted-mode.md). With no --bucket that space is used; a
// --bucket naming another space is refused rather than either one
// preferred. A server that cannot answer keeps a given --bucket, whose
// access check then proves the token, and refuses a missing one.
func resolveHostedSpace(ctx context.Context, cfg config) (config, error) {
	info, err := httpstore.DescribeToken(ctx, httpstore.Config{
		Endpoint:  cfg.endpoint,
		Token:     cfg.token,
		UserAgent: "slivingdoc/" + Version,
	})
	switch {
	case errors.Is(err, httpstore.ErrTokenLookupUnsupported) && cfg.bucket != "":
		return cfg, nil
	case errors.Is(err, httpstore.ErrTokenLookupUnsupported):
		return config{}, errors.New("app: hosted storage cannot name the token's space; pass the space name as --bucket or SLIVINGDOC_BUCKET")
	case err != nil:
		return config{}, hostedCheckError(err)
	case cfg.bucket == "":
		cfg.bucket = info.Space
		return cfg, nil
	case cfg.bucket != info.Space:
		return config{}, fmt.Errorf("app: the token reaches hosted space %q, not %q from --bucket or SLIVINGDOC_BUCKET; drop --bucket to use the token's space, or use a token made for %q",
			info.Space, cfg.bucket, cfg.bucket)
	default:
		return cfg, nil
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
func checkStore(ctx context.Context, store storage.ObjectStore) error {
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
		return hostedCheckError(err)
	}
	return nil
}

// hostedCheckError is the startup refusal for a failed hosted check.
func hostedCheckError(err error) error {
	switch {
	case errors.Is(err, storage.ErrAccessDenied):
		return fmt.Errorf("app: hosted storage refused the token: %s; check SLIVINGDOC_TOKEN and --bucket", mcp.Redact(err.Error()))
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
