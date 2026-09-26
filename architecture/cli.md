# CLI and process body

The command surface and the process body: how `slivingdoc <command>` is routed, how a command line becomes a validated `Runtime`, how `serve` runs and shuts down, and how `pull`/`commit` print their candid report. It answers "what happens between `main()` and the first notebook operation, and how does the process exit?"

Read this when: adding or changing a command, a startup refusal, the shutdown path, the CLI result report or its colour, the `DEBUG_PERF` capture, or the injection seams (`ProcessOptions`, `StoreFactory`) that tests use.

## Key files

| File | Purpose |
|------|---------|
| `main.go` | `main()`: one call, `os.Exit(cli.Run(ctx, os.Args, git2.New(), app.ProcessOptions{Stdout, Stderr}))` |
| `internal/cli/cli.go` | `Run` (router entry, returns the exit code), `Commands` (the command map), `Usage`, `withProcessEnv` (stores the resolved environment in the options), `setupConsole`/`noColor` (ancli console state, guarded by `consoleOnce`) |
| `cmd/serve/serve.go` | `serve.Command`: sets `opts.Ephemeral = true`; `Setup` = `app.Setup`; `Run` = `Runtime.Serve`; help is `app.HelpText` |
| `cmd/pull/pull.go` | `pull.Command`: `Setup` resolves the path with `app.OperationPath`, then `app.Setup`; `Run` calls `Runtime.Pull` and `app.Report` |
| `cmd/commit/commit.go` | `commit.Command`: same as pull plus the required `-m`/`--message` (`messageSet`); `Run` calls `Runtime.Commit` and `app.Report` |
| `cmd/version/version.go` | `version.Command`: prints exactly `slivingdoc <app.Version>\n`; `Setup` touches nothing |
| `internal/app/app.go` | `Version`, `ProcessOptions`, `StoreFactory`, `Setup` (defaults), `setup` (config, logger rebuild, engine open, `buildService`), `Runtime` (`Serve`, `Pull`, `Commit`, `Close`), `serve` (signal and shutdown loop), `closeTransport`, `realStoreFactory` |
| `internal/app/service.go` | `Service` (lazy per-path workspace + notebook, `notebookFor`), `ServiceConfig`, `ServiceHooks`, `NewService`, `identity` |
| `internal/app/command.go` | `OperationPath`/`positionals`/`resolvePath` (positional path), `Report`, `writeSuccess`, `writeError`, `writePathSets`, `fileReasonWords`, `reasonNextSteps`, `nextStep`, `actionWordings`, `ProcessOptions.Out` |
| `internal/app/colour.go` | `painter`, `colourEnabled` (real terminal and empty `NO_COLOR`) |
| `internal/app/perf.go` | `StartPerf`, `perfBase`, `startPerf`, `perfCapture.stop` (the `DEBUG_PERF` capture) |
| `internal/app/platform_unix.go`, `platform_windows.go` | `terminationSignals` (SIGINT, SIGTERM through `x/sys`) |
| `internal/app/config.go` | `Flags`, `FlagReference`, `HelpText`; see [config.md](./config.md) |

## Flow

```text
main.go:main()
  → cli.Run(ctx, os.Args, git2.New(), opts)
      app.NewLogger(env, stderr) → slog.SetDefault; opts.Logger = logger
      consoleOnce.Do(setupConsole)                 # ancli colour/newline/slog
      stopPerf := app.StartPerf(env, cliLogger)    # DEBUG_PERF, whole command
      cmd.Run(ctx, args, Commands(engine, opts), Usage)   # go_away_boilerplate/pkg/cmd
        → <command>.Flagset().Parse(...)          # flag set bound by app.Flags.Bind
        → <command>.Setup(context.Background())   # not the caller's ctx
        → <command>.Run(ctx)
      stopPerf()

serve:  serve.Command.Setup → app.Setup(engine, flags, opts)
          → setup(process)
              loadConfig → Flags.resolve → config.finish          # refusal: "app: invalid configuration: ..."
              runtimeLogger (only when cfg.logConfigured)
              engine.Open()                                        # pinned libgit2 check
              buildService → storeFactory(ctx, cfg)                # realStoreFactory → s3store.New
                           → storage.Probe (30 s, probeTimeout)    # refusal: "app: INCOMPATIBLE_STORE: ..."
                           → NewService(engine, store, cfg.serviceConfig(), hooks)
        serve.Command.Run → Runtime.Serve → mcp.NewServer(svc, Version, mcpLogger)
                          → app.serve(ctx, p, srv, logger)         # stdio until EOF, ctx, or signal

pull:   pull.Command.Setup → app.OperationPath(flagset, cwd) → app.Setup(...)
        pull.Command.Run   → Runtime.Pull(ctx, path) → Service.Pull → notebook.Pull
                           → app.Report(out, result, err, path, env, readOnly, writable)
commit: same as pull, with messageSet check before app.Setup, and Runtime.Commit
```

## Behavior

**Command map.** `cli.Commands` is the single definition: `serve|s`, `pull|p`, `commit|c`, `version|v`. The black-box process scenarios in `internal/integrationtest` route through the same map, so a second copy cannot drift. A missing or unknown command prints the command listing on stdout and exits 1; an unknown command also prints `'<name>' is not a valid argument` on stderr (`TestRouterRefusals`, `TestReleaseBinaryCommandSurface`).

**Help and version touch nothing.** `-h` before any positional argument makes the router's `fs.Parse` return `flag.ErrHelp`; `cmd.Run` prints `[command help]: <Help()>` as a timestamped ancli notice on stdout and exits 0 before `Setup`. For `pull`/`commit`, a `-h` after the path is parsed later in `Setup` by `positionals` and is a refusal (exit 1). `version.Setup` returns nil and `Run` writes one line; the npm launcher and the release smoke test depend on that exact line (`TestVersionTouchesNoStartupDependency`). The router has no `--version` flag.

**Argument refusals come first.** `pull` and `commit` resolve the positional path (`app.OperationPath`) and, for commit, the message, before `app.Setup`, so a bad command line never opens the engine or the store (`TestPullRefusesExtraPaths`, `TestCommitRefusesWithoutMessage`). `OperationPath` accepts zero or one positional path; flags after the path are parsed by `positionals`, and `--` ends flag parsing. `resolvePath` expands a leading `~` or `~/` (`pathutil.ExpandHome`), cleans absolute paths, and joins relative ones to `cwd`. An empty path means the workspace root (`Runtime.resolve`). The at-or-below-root rule is enforced later by `workspace.Open`.

**Startup refusal surface.** `app.setup` runs in this order: config resolution, logger rebuild, `engine.Open()`, store factory, `storage.Probe`, `NewService`. Every failure returns before any transport starts. Config diagnostics pass through `mcp.Redact`; the probe diagnostic is prefixed `INCOMPATIBLE_STORE` and also redacted. Engine-open (`app: open native engine: ...`) and store-construction (`app: create object store: ...`) errors are not redacted. A refusal after the ephemeral session directory exists removes it (`Flags.resolve` defer, `removeSessionDir` in `setup`). Any error returned from `Setup` (argument, config, engine, store, probe) makes `cmd.Run` print one timestamped stderr line `<time> error: failed to setup command: <diagnostic>` (the `error` token is ANSI-coloured unless `NO_COLOR` is set, even on a pipe) and exit 1 with an empty stdout (`assertOneRedactedDiagnostic`). A flag-value or unknown-flag error before the first positional is caught earlier by the router's `fs.Parse`: exit 1, `unknown error: failed to parse flagset: ...` on stderr, and the command listing on stdout.

**Readonly mode.** There is no global read-only switch. "Read-only" is the per-process path policy from `--read-only-paths` and `--writable-paths` (see [config.md](./config.md)). `NewService` builds a `git.PathPolicy`; `Runtime.ReadOnlyPaths` / `WritablePaths` return the normalized sets, and `app.Report` prints them as `writable:`, `read-only:` and `path-rule:` trailers (`writePathSets`). Enforcement lives in the notebook ([commit.md](./commit.md), [pull.md](./pull.md)).

**Service lifecycle.** `Service.notebookFor` opens one `workspace.Workspace` and one `notebook.Notebook` per requested directory on first use, under `Service.mu`, and caches them in `opened`, keyed by `filepath.Clean` of the path so two spellings of one directory share one workspace. `Service.Close` closes every workspace and makes later calls fail with `app: service is closed`. `Runtime.Close` closes the service, closes the engine, and removes the ephemeral session directory.

**Serve and shutdown.** `app.serve` runs `srv.Serve` over `sdk.StdioTransport` wrapped in `closeTransport`. Client EOF returns cleanly (exit 0). Context cancellation or a signal from `terminationSignals` logs `shutting down`, cancels the context, closes the live connection (which makes the SDK cancel in-flight handler contexts), and waits up to `ShutdownDeadline` (default 30 s). Expiry returns `app: shutdown deadline expired`, a nonzero exit.

**CLI report.** `app.Report` reuses the MCP mappers (`mcp.MapSuccess`, `mcp.MapError`) so the CLI and the tools say the same thing. Success writes `OK  generation <n>  <path>`, one line per changed file with `+ins -del` (a zero side omitted), and the totals line. A domain error writes `CODE · REASON`, the message, one aligned line per file with its reason in words (`fileReasonWords`) and `lines a-b` ranges, `next:` (`nextStep`: a reason's own wording from `reasonNextSteps`, such as `DIRECTORY_NOT_EMPTY`, else `actionWordings`), `retryable:`, `recovery:` when present, and the path-set trailers. `Report` returns `errors.New(te.Code)`, so the router exits nonzero with the terse category. A non-domain error (cancellation) is returned unchanged.

**Colour.** `colourEnabled` requires an `*os.File` that is a character device and an empty `NO_COLOR`; otherwise `painter` passes tokens through and the report is byte-identical plain text. `main.go` passes `ProcessOptions.Env == nil`; `cli.Run` resolves it to `os.Environ()` through `withProcessEnv` and stores it in `opts.Env` before building the commands, so `cmd/pull`/`cmd/commit` hand the real environment to `app.Report`. The router's own diagnostics use ancli; `setupConsole` sets `ancli.UseColor` from `NO_COLOR` (any non-empty value), because ancli's default recognizes only `"true"`.

**Perf.** `StartPerf` is read from `DEBUG_PERF` only: empty, `0`, `false` disable it; `1`, `true` use `<tmp>/slivingdoc-perf`; any other value is the base directory. Each run gets a timestamped directory with `cpu.pprof`, `trace.out` and `heap.pprof`. Every failure is a warning; the capture never changes the exit code. It deliberately does not default to the working directory, because a binary profile inside a notebook would be refused by the next commit.

**Streams.** Stdout carries MCP messages, help, the version line and the CLI report only. `ProcessOptions.Stdout` never reaches the MCP stream: `serve` uses `sdk.StdioTransport`, bound to `os.Stdin`/`os.Stdout` (tests inject `process.transport`), and `Setup`'s `io.Discard` default for `Stdout` is unused by it. Command output uses `ProcessOptions.Out()`, which falls back to `os.Stdout`.

## Gotchas

- The router (`go_away_boilerplate/pkg/cmd`) parses flags only up to the first positional argument. Any new positional-taking command must call `app.OperationPath` (or equivalent) so trailing flags are honoured.
- `consoleOnce` exists because ancli keeps package-level state; configuring it per call races between concurrently routed commands in tests.
- `serve.Command` sets `Ephemeral` on its copy of `opts`; `pull` and `commit` must not, since a human needs the directory after exit.
- Records emitted before flags resolve (routing, the level-fallback warning) use the environment logger; only after `setup` rebuilds it do `--log-level`/`--log-timestamp` apply. The refusal line itself is not a log record: the router prints it through ancli.
- Router lines (`failed to setup command:`, `failed to run:`, usage, `[command help]`) all print as `<RFC3339> <status>: <msg>` (errors as `<time> error: ...` on stderr, the command listing as `<time> ok: ...` on stdout) and come from ancli's own handler writing to the real `os.Stderr`/`os.Stdout`, not to the injected `ProcessOptions` streams. Every `Setup`/`Run` error exits exactly 1, and `Setup` receives `context.Background()`, not the caller's context.
- `ProcessOptions.StoreFactory` receives the exported `ServiceConfig`, not the internal `config`. Tests inject `fake.New(prefix)` here.
- `Runtime.Serve` passes `Module(r.base, ModuleMCP)`; do not pass `r.logger` (bound to `app`), or `LOG_LEVEL=mcp=...` stops working.

## Related

- [config.md](./config.md): flags, environment, precedence, the session directory.
- [mcp-server.md](./mcp-server.md): what `Runtime.Serve` registers.
- [errors.md](./errors.md): codes, reasons, actions, and exit codes.
- [logging.md](./logging.md): `NewLogger`, `Module`, the logger rebuild.
- [running.md](./running.md): operator view of the commands and the report.
- [notebook.md](./notebook.md), [workspace.md](./workspace.md): what `Service` opens.
- [AGENTS.md, Startup Wiring](../AGENTS.md#startup-wiring-maingo): the ordered startup list.
