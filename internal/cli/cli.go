// Package cli is the slivingdoc command surface: the command map, the
// usage text, and the router entry point. It exists so the process body and
// the black-box process scenarios route through exactly the same commands;
// a second copy of the map in either place could drift from the other.
package cli

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/cmd"

	"github.com/baalimago/slivingdoc/cmd/serve"
	"github.com/baalimago/slivingdoc/cmd/version"
	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git"
)

// Usage is the router help. The %v is the command description table.
const Usage = `slivingdoc - shared UTF-8 text notebook over MCP stdio

Many agents edit one notebook directory; slivingdoc merges concurrent
changes and resolves conflicts with visible text markers, over an
S3-compatible bucket.

Commands:
%v
Run 'slivingdoc serve -h' for the full flag and environment reference.

Logging is configured by the environment, not by flags:
  LOG_LEVEL   per-module levels, for example "cli=warn,mcp=debug,info".
              A bare level is the default; modules are cli, app, mcp, notebook.
  NO_COLOR    any non-empty value disables the ANSI level colour.`

// Commands is the complete command surface over the given native engine and
// process environment. The engine and the options are injected because the
// process scenarios substitute a deterministic store factory and their own
// streams for the same commands the released binary runs.
func Commands(engine git.Engine, opts app.ProcessOptions) map[string]cmd.Command {
	return map[string]cmd.Command{
		"serve|s":   serve.Command(engine, opts),
		"version|v": version.Command(opts.Stdout),
	}
}

// consoleOnce configures the shared console state of the router exactly
// once. ancli keeps its presentation in package variables, so repeated
// writes would be a data race between concurrently routed commands.
var consoleOnce sync.Once

// Run routes args to a command and returns the process exit code.
func Run(ctx context.Context, args []string, engine git.Engine, opts app.ProcessOptions) int {
	environment := opts.Env
	if environment == nil {
		environment = os.Environ()
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	logger, levelErr := app.NewLogger(environment, stderr)
	slog.SetDefault(logger)
	opts.Logger = logger

	consoleOnce.Do(func() { setupConsole(environment) })

	log := app.Module(logger, app.ModuleCLI)
	if levelErr != nil {
		log.Warn("falling back to the default log level", "error", levelErr)
	}
	log.Debug("routing command", "args", args[1:])

	code := cmd.Run(ctx, args, Commands(engine, opts), Usage)
	log.Debug("command finished", "exit", code)
	return code
}

// setupConsole makes the router's own diagnostics readable: one line each,
// timestamped, and coloured unless NO_COLOR asks otherwise.
//
// ancli routes through slog once SetupSlog runs, which is what supplies the
// timestamp and the trailing newline; without it a startup refusal prints an
// unterminated, unstamped line. Errors reach stderr and usage reaches
// stdout, matching the documented split.
func setupConsole(environment []string) {
	ancli.UseColor = noColor(environment) == ""
	ancli.Newline = true
	ancli.SetupSlog()
}

// noColor reads NO_COLOR from the injected environment. Any non-empty value
// disables colour, which is the NO_COLOR convention; ancli's own default
// only recognizes the literal "true".
func noColor(environment []string) string {
	for _, kv := range environment {
		if name, value, ok := strings.Cut(kv, "="); ok && name == "NO_COLOR" {
			return value
		}
	}
	return ""
}
