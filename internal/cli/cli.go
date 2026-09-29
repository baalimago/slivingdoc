// Package cli is the slivingdoc command surface: the command map, the
// usage text, and the router entry point. It exists so the process body and
// the black-box process scenarios route through exactly the same commands;
// a second copy of the map in either place can drift from the other.
package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/baalimago/go_away_boilerplate/pkg/cmd"

	"github.com/baalimago/slivingdoc/cmd/commit"
	logcmd "github.com/baalimago/slivingdoc/cmd/log"
	"github.com/baalimago/slivingdoc/cmd/login"
	"github.com/baalimago/slivingdoc/cmd/pull"
	"github.com/baalimago/slivingdoc/cmd/serve"
	"github.com/baalimago/slivingdoc/cmd/status"
	"github.com/baalimago/slivingdoc/cmd/version"
	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// Usage is the router help. The %v is the command description table.
const Usage = `slivingdoc - shared UTF-8 text notebook over MCP stdio

Many agents edit one notebook directory; slivingdoc merges concurrent
changes and resolves conflicts with visible text markers, over an
S3-compatible bucket.

Commands:
%v
Run 'slivingdoc serve -h' for the full flag and environment reference.

Humans sync the shared directory directly with 'slivingdoc pull <path>'
and 'slivingdoc commit <path> -m <message>'; 'slivingdoc status' shows what
changed locally and 'slivingdoc log' the recent publications.

For hosted storage, 'slivingdoc login' logs in to your account through the
browser, so serve, pull and commit need no SLIVINGDOC_TOKEN; 'slivingdoc
space' lists the login's spaces and 'slivingdoc space <name>' sets the
default one; --storage auto|hosted|s3 chooses between a login and S3
explicitly. 'slivingdoc logout' revokes the login.

Logging is configured by the environment; serve, pull, commit, status and log
also take --log-level and --log-timestamp, which override it:
  LOG_LEVEL   per-module levels, for example "cli=warn,mcp=debug,info".
              A bare level is the default; modules are cli, app, mcp, notebook.
  NO_COLOR    any non-empty value disables colour: of log levels and
              of the terminal output (marks, spinners, the home screen).
  DEBUG_PERF  capture CPU, heap, and execution-trace profiles across the
              whole command: 1 writes under the system temporary
              directory, any other value is the base directory itself.`

// Commands is the complete command surface over the given native engine and
// process environment. The engine and the options are injected because the
// process scenarios substitute a deterministic store factory and their own
// streams for the same commands the released binary runs.
func Commands(engine git.Engine, opts app.ProcessOptions) map[string]cmd.Command {
	return map[string]cmd.Command{
		"serve|s":   serve.Command(engine, opts),
		"pull|p":    pull.Command(engine, opts),
		"commit|c":  commit.Command(engine, opts),
		"status":    status.Command(engine, opts),
		"log":       logcmd.Command(engine, opts),
		"login":     login.Command(opts),
		"logout":    login.LogoutCommand(opts),
		"space":     login.SpaceCommand(opts),
		"version|v": version.Command(opts.Stdout),
	}
}

// Run routes args to a command and returns the process exit code.
func Run(ctx context.Context, args []string, engine git.Engine, opts app.ProcessOptions) int {
	opts = withProcessEnv(opts)
	environment := opts.Env
	// Every writer of stderr, the logger included, shares one guard, so
	// a log record never lands on the end of a progress line
	// (architecture/tui.md).
	guard := tui.NewGuard(opts.ErrOut())
	opts.Stderr = guard

	logger, levelErr := app.NewLogger(environment, guard)
	slog.SetDefault(logger)
	opts.Logger = logger

	log := app.Module(logger, app.ModuleCLI)
	if levelErr != nil {
		log.Warn("falling back to the default log level", "error", levelErr)
	}
	log.Debug("routing command", "args", args[1:])

	// The capture brackets the whole command — startup refusals, the
	// native engine, the operation, and shutdown — because a slow pull is
	// diagnosed end to end, not from the operation alone.
	stopPerf := app.StartPerf(environment, log)
	code := newRouter(Commands(engine, opts), opts).run(ctx, args)
	stopPerf()
	log.Debug("command finished", "exit", code)
	return code
}

// withProcessEnv resolves a nil Env to the process environment and stores
// it in the options, so every command — and the report colour gate behind
// it, which reads NO_COLOR from Env — sees the same environment the router
// read.
func withProcessEnv(opts app.ProcessOptions) app.ProcessOptions {
	if opts.Env == nil {
		opts.Env = os.Environ()
	}
	return opts
}
