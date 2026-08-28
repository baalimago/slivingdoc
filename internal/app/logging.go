package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/slogcolor"
)

// Log module names. Every logger in the process binds one, so LOG_LEVEL can
// address them individually:
//
//	LOG_LEVEL="cli=warn,mcp=debug,info"
const (
	ModuleCLI      = "cli"      // command routing and process exit
	ModuleApp      = "app"      // startup, shutdown, and the store probe
	ModuleMCP      = "mcp"      // tool calls, one record per request
	ModuleNotebook = "notebook" // best-effort checkpoint and cleanup records
)

// logEnvLevel and logEnvNoColor are read from the process environment, not
// from flags: logging is an operator concern that must work before flags are
// parsed and identically for every command.
const (
	logEnvLevel   = "LOG_LEVEL"
	logEnvNoColor = "NO_COLOR"
)

// NewLogger builds the process logger over w from the environment.
//
// LOG_LEVEL takes the per-module grammar "cli=warn,mcp=debug,info": a bare
// level is the default and "module=level" overrides one module. NO_COLOR
// disables the ANSI level color when set to any non-empty value, following
// the NO_COLOR convention.
//
// A malformed LOG_LEVEL is not fatal. Logging is diagnostic plumbing, and
// refusing to start over it turns a typo into an outage. The returned
// logger falls back to the Info default and the returned error names the
// problem so the caller can report it through that same logger.
func NewLogger(environment []string, w io.Writer) (*slog.Logger, error) {
	env := environ(environment)
	return buildLogger(env[logEnvLevel], env[logEnvNoColor] != "", true, w)
}

// runtimeLogger rebuilds the process logger from the resolved logging
// configuration: the flag-over-environment level spec and timestamp
// toggle, with the color gate still read from the environment. setup
// calls it after flags resolve, so --log-level and --log-timestamp reach
// the runtime's records.
func runtimeLogger(cfg config, environment []string, w io.Writer) (*slog.Logger, error) {
	env := environ(environment)
	return buildLogger(cfg.logLevel, env[logEnvNoColor] != "", cfg.logTimestamp, w)
}

// buildLogger constructs the logger from resolved settings: the level
// spec in the LOG_LEVEL grammar, the color gate, and whether records
// carry the time= field. A malformed spec falls back to the Info default
// with the returned error naming the problem (see NewLogger).
func buildLogger(levelSpec string, noColor, timestamp bool, w io.Writer) (*slog.Logger, error) {
	if w == nil {
		w = io.Discard
	}
	var parseErr error
	levels, err := slogcolor.ParseLevels(levelSpec)
	if err != nil {
		parseErr = fmt.Errorf("app: invalid %s %q: %w", logEnvLevel, levelSpec, err)
		levels = slogcolor.NewLevels(slog.LevelInfo)
	}

	var handler slog.Handler = slogcolor.New(w, &slogcolor.Options{
		Levels:  levels,
		NoColor: noColor,
	})
	if !timestamp {
		handler = noTimeHandler{inner: handler}
	}
	return slog.New(handler), parseErr
}

// noTimeHandler suppresses the time= field by zeroing each record's time,
// which the text handler renders as no field at all. Embedders whose log
// pipeline stamps lines itself ask for it with --log-timestamp=false.
type noTimeHandler struct {
	inner slog.Handler
}

func (h noTimeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h noTimeHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Time = time.Time{}
	return h.inner.Handle(ctx, r)
}

func (h noTimeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return noTimeHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h noTimeHandler) WithGroup(name string) slog.Handler {
	return noTimeHandler{inner: h.inner.WithGroup(name)}
}

// Module returns logger bound to a module name, which selects that module's
// LOG_LEVEL entry. A nil logger yields a discarding one, so callers never
// need a nil check.
func Module(logger *slog.Logger, name string) *slog.Logger {
	if logger == nil {
		return slog.New(slogcolor.New(io.Discard, nil))
	}
	return logger.With(slogcolor.DefaultModuleKey, name)
}
