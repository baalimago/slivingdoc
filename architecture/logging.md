# Logging

How the process logs: one `slog` logger built from the environment (and later from `--log-level`/`--log-timestamp`), per-module level selection, the request-scoped logger that carries `mcpReqID` into the notebook, and the SDK log demotion. It answers "where does a log record come from, which module and level gate it, and why is it (not) on stderr?"

Read this when: adding log records, a new module, changing level parsing, timestamps or colour, request correlation, or the SDK logger wrapper.

## Key files

| File | Purpose |
|------|---------|
| `internal/app/logging.go` | `ModuleCLI`, `ModuleApp`, `ModuleMCP`, `ModuleNotebook`; `logEnvLevel` (`LOG_LEVEL`), `logEnvNoColor` (`NO_COLOR`); `NewLogger`, `runtimeLogger`, `buildLogger`, `noTimeHandler`, `Module` |
| `internal/app/config.go` | `--log-level`, `--log-timestamp`, `SLIVINGDOC_LOG_TIMESTAMP` resolution; `config.logLevel`, `logTimestamp`, `logConfigured` |
| `internal/app/app.go` | `setup` rebuilds the logger when `cfg.logConfigured`; `Runtime.base` vs `Runtime.logger`; `Runtime.Serve` passes the `mcp` module logger; `Runtime.Pull`/`Commit` attach the `notebook` module logger |
| `internal/cli/cli.go` | `Run`: builds the environment logger, `slog.SetDefault`, `opts.Logger`, the `cli` module records; `setupConsole` (ancli) |
| `internal/mcp/server.go` | `handler.requestLogger` (`mcpReqID`, `tool`), `tool call started`/`completed` records, `redactValues` on `cause` |
| `internal/mcp/sdklog.go` | `sdkLogger`, `sdkLogHandler` (`Enabled`, `Handle`, `WithAttrs`, `WithGroup`), `emptyStringAttr` |
| `internal/notebook/logger.go` | `WithLogger`, `LoggerFrom` (context-carried logger; discard fallback) |
| `internal/app/perf.go` | `StartPerf` reports capture paths through the `cli` logger |
| `internal/integrationtest/logcapture.go` | `LogCapture` (`Records`, `Warnings`, `ToolCalls`, `DistinctReqIDs`) for scenario assertions |

## Flow

```text
cli.Run
  logger, levelErr := app.NewLogger(env, stderr)       # LOG_LEVEL, NO_COLOR, timestamps on
  slog.SetDefault(logger); opts.Logger = logger
  log := app.Module(logger, "cli")                     # "routing command", "command finished"
  levelErr → log.Warn("falling back to the default log level")

app.setup(p)
  base := p.logger (or NewLogger)                      # environment configuration
  cfg := loadConfig(p)
  cfg.logConfigured → base = runtimeLogger(cfg, env, stderr)   # --log-level / --log-timestamp
  logger := Module(base, "app")                        # "native engine open", "serving", "shutting down"

Runtime.Serve → mcp.NewServer(svc, Version, Module(base, "mcp"))
  sdk ServerOptions.Logger = sdkLogger(mcpLogger)      # SDK records → DEBUG
  handler.requestLogger → mcpLogger.With("mcpReqID", id, "tool", name)
                        → notebook.WithLogger(ctx, reqLogger)
  notebook: notebook.LoggerFrom(ctx).Warn(...)         # checkpoint / cleanup, same mcpReqID

Runtime.Pull/Commit (CLI) → notebook.WithLogger(ctx, Module(base, "notebook"))
```

## Behavior

**Output.** Records are structured `key=value` text from `go_away_boilerplate/pkg/slogcolor` on stderr. Stdout never carries logs; it is the MCP protocol stream and command output. Router lines (setup and run failures, usage, command help) are not slogcolor records: `pkg/cmd.Run` prints them through ancli as timestamped `<status>: <msg>` lines, errors to `os.Stderr` and usage/help to `os.Stdout`, unaffected by `LOG_LEVEL` and `--log-timestamp`.

**Levels by module.** `LOG_LEVEL` uses the grammar `cli=warn,mcp=debug,info`: `module=level` sets one module and a bare level is the default. Levels are `debug`, `info`, `warn` (or `warning`), `error`, case-insensitive; a second default level or a repeated module is a parse error. `Module(logger, name)` binds `slogcolor.DefaultModuleKey`, which is what the handler uses to pick the level. Modules: `cli` (routing, exit, perf), `app` (startup, probe, shutdown), `mcp` (one pair of records per tool call), `notebook` (best-effort checkpoint, cleanup and pack-cache-write warnings from `checkpoint.go` and `remote.go`).

**Lenient environment, strict flag.** A malformed `LOG_LEVEL` falls back to Info and returns an error that the caller logs as a warning (`buildLogger`); logging never refuses startup. An invalid `--log-level` flag refuses startup in `Flags.resolve`, like any other flag.

**Rebuild after flags.** Records before flags resolve (routing, level-fallback warnings) follow the environment. A config refusal is not a log record but a router line. `setup` rebuilds the base logger with `runtimeLogger` only when `cfg.logConfigured` is true: `--log-level` or `--log-timestamp` was set, or `SLIVINGDOC_LOG_TIMESTAMP` is non-empty. The colour gate is always read from `NO_COLOR`.

**Timestamps.** `--log-timestamp=false` (or `SLIVINGDOC_LOG_TIMESTAMP=false`) wraps the handler in `noTimeHandler`, which zeroes each record's time so the text handler omits `time=`. For hosts that stamp lines themselves.

**Colour.** Any non-empty `NO_COLOR` disables ANSI level colour. The same variable disables the CLI report colour ([cli.md](./cli.md)): `cli.Run` stores the resolved environment in `ProcessOptions.Env`, which the commands pass to `app.Report`.

**Request correlation.** Each tool call gets a 16-hex `mcpReqID` from `newRequestID`. `requestLogger` binds `mcpReqID` and `tool`, logs `tool call started` and `tool call completed` (`outcome`, `duration`, and on error `cause` via `redactValues`), and attaches the same logger to the context with `notebook.WithLogger`. Background efforts scheduled by that call log through `LoggerFrom(ctx)` and so share the ID. The same ID is the `diagnosticId` in the tool result, which is how an operator joins a caller report to the full cause.

**SDK demotion.** `sdkLogHandler.Handle` re-emits every SDK record at DEBUG, and `Enabled` gates on the inner handler's DEBUG threshold. Attributes whose resolved value is an empty string (the SDK's absent stdio `session_id`, including named string types stored as `KindAny`) are dropped in `Handle` and `WithAttrs`. So `mcp=info` shows only slivingdoc's own records.

**Nil safety.** `Module(nil, ...)` returns a discarding logger; `LoggerFrom` returns a discarding logger when none is attached; `mcp.NewServer` with a nil logger discards. Callers never nil-check.

## Gotchas

- Bind a module once per component with `Module`, not per call: slog consults `Enabled` before building a record, so the level can only be resolved from an attribute bound on the logger.
- `Runtime.logger` is the `app` module; passing it to the MCP server or notebook would put their records under the wrong module.
- Reusable packages (`storage`, `s3store`, `git`, `git2`, `workspace`) do not log; they return typed errors (AGENTS code style). Only `cli`, `app`, `mcp`, and the notebook's best-effort paths log.
- Never log raw `err.Error()` of a notebook error at a caller-visible place; in logs, use `redactValues`.
- `LogCapture` handlers accept every level; scenario assertions on log content do not depend on `LOG_LEVEL`.

## Related

- [config.md](./config.md): the logging settings in the precedence table.
- [mcp-server.md](./mcp-server.md): per-call records and `diagnosticId`.
- [cli.md](./cli.md): `DEBUG_PERF`, colour, router diagnostics.
- [running.md, Logging](./running.md#logging): operator view.
- [AGENTS.md, Logging](../AGENTS.md#logging).
