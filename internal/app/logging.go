package app

import (
	"fmt"
	"io"
	"log/slog"

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

// Modules is every module name the process logs under, for diagnostics.
var Modules = []string{ModuleCLI, ModuleApp, ModuleMCP, ModuleNotebook}

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
// refusing to start over it would turn a typo into an outage; the returned
// logger falls back to the Info default and the returned error names the
// problem so the caller can report it through that same logger.
func NewLogger(environment []string, w io.Writer) (*slog.Logger, error) {
	if w == nil {
		w = io.Discard
	}
	env := environ(environment)

	var parseErr error
	levels, err := slogcolor.ParseLevels(env[logEnvLevel])
	if err != nil {
		parseErr = fmt.Errorf("app: invalid %s %q: %w", logEnvLevel, env[logEnvLevel], err)
		levels = slogcolor.NewLevels(slog.LevelInfo)
	}

	handler := slogcolor.New(w, &slogcolor.Options{
		Levels:  levels,
		NoColor: env[logEnvNoColor] != "",
	})
	return slog.New(handler), parseErr
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
