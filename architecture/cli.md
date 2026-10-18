# CLI and process body

The command surface and the process body: how `slivingdoc <command>` is routed, how a command line becomes a validated `Runtime`, how `serve` runs and shuts down, and how `pull`/`commit` print their candid report and `status`/`log` their plain ones. `login`, `logout` and `space` are routed the same way; their flow is in [login.md](./login.md). It answers "what happens between `main()` and the first notebook operation, and how does the process exit?"

Read this when: adding or changing a command, a startup refusal, the shutdown path, the CLI result report or its colour, the `DEBUG_PERF` capture, or the injection seams (`ProcessOptions`, `StoreFactory`) that tests use.

## Key files

| File | Purpose |
|------|---------|
| `main.go` | `main()`: one call, `os.Exit(cli.Run(ctx, os.Args, git2.New(), app.ProcessOptions{Stdout, Stderr}))` |
| `internal/cli/cli.go` | `Run` (router entry, returns the exit code), `Commands` (the command map), `Usage`, `withProcessEnv` (stores the resolved environment in the options) |
| `internal/cli/route.go`, `internal/cli/home.go` | `router` (`newRouter`, `run`, `lookup`, `fail`, `help`, `styleHelp`), the home screen and the usage ([tui.md](./tui.md)) |
| `cmd/serve/serve.go` | `serve.Command`: sets `opts.Ephemeral = true`; `Setup` = `app.Setup`; `Run` = `Runtime.Serve`; help is `app.HelpText` |
| `cmd/pull/pull.go` | `pull.Command`: `Setup` is `app.OperationSetup`; `Run` asks for a missing secret (`Runtime.AskForSecret`), then calls `Runtime.Pull` inside `Runtime.WithProgress` and `app.Report` |
| `cmd/commit/commit.go` | `commit.Command`: same as pull plus the required `-m`/`--message` (`messageSet`); `Run` calls `Runtime.Commit` and `app.Report` |
| `cmd/status/status.go`, `cmd/log/log.go` | `status.Command` and `log.Command` (`log` only: `--limit`, default 20): the read-only commands; `Setup` is pull's (`log` checks `--limit` first); `Run` calls `Runtime.Status`/`Runtime.Log` and `app.ReportStatus`/`app.ReportLog` (`internal/app/statuslog.go`) |
| `cmd/login/login.go` | `login.Command` (`login`), `login.LogoutCommand` (`logout`) and `login.SpaceCommand` (`space`): `Setup` refuses more positional arguments than the command takes (none, except `space`'s `<name>` or `new <name>`), then `app.PrepareLogin`/`app.PrepareLogout`/`app.PrepareSpace`; `Run` runs the prepared operation ([login.md](./login.md)) |
| `cmd/encrypt/encrypt.go`, `internal/app/encryption.go` | `encrypt.Command`: the five subcommands `init`, `status`, `passwd`, `recovery`, `rekey`; `Setup` resolves the subcommand and path (`app.EncryptArgs`), validates the key-record flags and runs `app.Setup`; `Run` calls `Runtime.Encrypt` and prints the unified report, the status report and the one-time recovery code ([encryption.md](./encryption.md)) |
| `cmd/version/version.go` | `version.Command`: prints exactly `slivingdoc <app.Version>\n`; `Setup` touches nothing |
| `internal/app/app.go` | `Version`, `ProcessOptions` (including the login seams `SiteClient`, `OpenBrowser`, `Sleep`, `Hostname`, `Stdin` (the prompt, the pickers and the passphrase ask), `Terminal`, `Style` (the `tui.Style` seam of login, space and logout), and the test-only `newS3Admin` factory of the S3 space backend; `Signals` also interrupts a login), `StoreFactory`, `Setup` (defaults), `setup` (config, logger rebuild, `logStorage`, engine open, `buildService`), `Runtime` (`Serve`, `Pull`, `Commit` (both record the complete target the directory proved on success, [config.md](./config.md)), `AskForSecret` ([encryption.md](./encryption.md), The terminal ask of `pull` and `commit`), `Status`, `Log`, `Target`, `Space`, `SpaceSource`, `Close`), `serve` (signal and shutdown loop), `closeTransport`, `realStoreFactory`, `checkStore`, `accessChecker`, `logStoreCheck`; `probeproof.go`: `checkStoreWithProof`, `probeProofStore` |
| `internal/app/statuslog.go` | `ReportStatus` (its `source` argument adds the remembered-target source trailer), `ReportLog`: the plain status and log reports; a domain error renders through `reportError` like `Report` |
| `internal/app/service.go` | `Service` (`Pull`, `Commit`, `Status`, `Log`; lazy per-path workspace + notebook, `notebookFor`), `ServiceConfig`, `ServiceHooks`, `NewService`, `AskForSecret`, `storedCurrent`, `identity` |
| `internal/app/command.go` | `OperationPath`/`positionals`/`resolvePath` (positional path), `OperationSetup` (the positional path plus `Setup` over its association), `Report`, `writeSuccess`, `writeError`, `writePathSets`, `fileReasonWords`, `reasonNextSteps`, `nextStep`, `actionWordings`, `ProcessOptions.Out` |
| `internal/tui/` | `Style`, `Detect`, marks, columns, spinner, picker: the presentation every command renders through ([tui.md](./tui.md)) |
| `internal/app/progress.go` | `Runtime.WithProgress`, the pull/commit progress line on stderr |
| `internal/app/perf.go` | `StartPerf`, `perfBase`, `startPerf`, `perfCapture.stop` (the `DEBUG_PERF` capture) |
| `internal/app/platform_unix.go`, `platform_windows.go` | `terminationSignals` (SIGINT, SIGTERM through `x/sys`) |
| `internal/app/config.go` | `Flags`, `FlagReference`, `HelpText`; see [config.md](./config.md) |

## Flow

```text
main.go:main()
  → cli.Run(ctx, os.Args, git2.New(), opts)
      app.NewLogger(env, stderr) → slog.SetDefault; opts.Logger = logger
      stopPerf := app.StartPerf(env, cliLogger)    # DEBUG_PERF, whole command
      newRouter(Commands(engine, opts), opts).run(ctx, args)   # internal/cli/route.go
        → router.lookup(name)                     # name or shortcut
        → <command>.Flagset().Parse(...)          # flag set bound by app.Flags.Bind
        → <command>.Setup(context.Background())   # not the caller's ctx
        → <command>.Run(ctx)
      stopPerf()

serve:  serve.Command.Setup → app.Setup(engine, flags, opts)
          → setup(process)
              loadConfig → Flags.resolve → config.finish          # refusal: "app: invalid configuration: ..."
              runtimeLogger (only when cfg.logConfigured)
              engine.Open()                                        # pinned libgit2 check
              buildService → storeFactory(ctx, cfg)                # realStoreFactory → s3store.New | httpstore.New (token)
                           → checkStoreWithProof (30 s, probeTimeout) # storage.Probe unless the shared pack cache holds a fresh
                                                                   # probe-ok.json (storage.md); CheckAccess for a hosted store
                                                                   # refusal: "app: INCOMPATIBLE_STORE: ..." or "app: hosted storage ..."
                           → NewService(engine, store, cfg.serviceConfig(), hooks)
        serve.Command.Run → Runtime.Serve → mcp.NewServer(rememberingService{rt}, Version, mcpLogger)
                          → app.serve(ctx, p, srv, logger)         # stdio until EOF, ctx, or signal

pull:   pull.Command.Setup → app.OperationSetup("pull", flagset, engine, flags, opts)
                            → app.OperationPath(flagset, cwd) → app.Setup(engine, flags, opts.WithAssociation(path))
        pull.Command.Run   → Runtime.WithProgress("Pulling", path, Runtime.Pull) → Service.Pull → notebook.Pull
                           → rememberSpace on success (config.md: Recording the association)
                           → app.Report(out, result, err, path, env, readOnly, writable)
commit: same as pull, with messageSet check before app.Setup, and Runtime.Commit

login:  login.Command.Setup → app.PrepareLogin (flags, credentials file, site) → Login.Run (login.md)
logout: login.LogoutCommand.Setup → app.PrepareLogout → Logout.Run
space:  login.SpaceCommand.Setup → app.PrepareSpace (the flags and the positional words)
                              → Space.Run: list every selected backend (hosted
        groups first, then S3 groups), or create or set the default through the
        backend the flags select; --storage s3 restricts the run to S3, while
        --storage hosted restricts it to hosted storage; a hosted selection
        sets the login's default space and clears every stored S3 default, an
        S3 selection replaces them with one, and new <name> writes no default;
        with no positional argument on a terminal, Space.list opens the picker
        (stderr) instead of printing the listing, and a create row creates the
        target and makes it the default
        the Go flag parser stops at the first positional word, so the flags of
        `space [flags] [<name> | new <name>]` precede it; --storage, --endpoint,
        --profile, --region, --path-style and --prefix bind like the shared flags
```

## Behavior

**Command map.** `cli.Commands` is the single definition: `serve|s`, `pull|p`, `commit|c`, `status`, `log`, `login`, `logout`, `space`, `encrypt`, `version|v`. The `encrypt` command is a subcommand CLI of its own (`encrypt <subcommand> [flags] [path]`): `Setup` resolves the subcommand and the optional path before `app.Setup`, refuses an unknown subcommand or an invalid key-record flag combination, and `Run` reports through the same `app.Report` as `pull` and `commit`; its own help is `cmd/encrypt`'s `helpText` followed by `app.FlagReference` ([encryption.md](./encryption.md)). The black-box process scenarios in `internal/integrationtest/{cli,workspace,sync,hosted}` route through the same map, so a second copy cannot drift. A missing or unknown command prints the usage (with the sorted command table) on stdout and exits 1; an unknown command also prints `error: "<name>" is not a slivingdoc command` on stderr. On a terminal a bare `slivingdoc` shows the home screen instead of the usage ([tui.md](./tui.md)). `help`, `-h` and `--help` with no command print the usage and exit 0; `help <command>` prints that command's help and exits 0, or, for an unknown name, the error line and the usage with exit 1 (`TestRouterRefusals`, `TestRouterPlainSurface`, `TestReleaseBinaryCommandSurface`).

**Help and version touch nothing.** `-h` before any positional argument makes the router's `fs.Parse` return `flag.ErrHelp`; the router prints the command's `Help()` text on stdout (with the header and painted section titles on a terminal) and exits 0 before `Setup`. For `pull`/`commit`/`status`/`log`, a `-h` after the path is parsed later in `Setup` by `positionals` and is a refusal (exit 1). `login -h`, `logout -h` and `space -h` exit the same way, before the credentials file is located (`TestLoginCommandsTouchNothingEarly`). `version.Setup` returns nil and `Run` writes one line; the npm launcher and the release smoke test depend on that exact line (`TestVersionTouchesNoStartupDependency`). `--version` as the only argument makes the router run the version command, so `slivingdoc --version` prints the same line and exits 0 (`TestScenarioCLIVersionFlag`); beside any other argument it is an unknown flag, and after a command it is that command's.

**The space picker.** `space` with no positional argument on a terminal (stdin, stdout, and stderr all terminals) opens one picker instead of printing the listing: the rows name every target of a configured backend plus one create row per configured backend, every picker line goes to stderr, and stdout carries only the create and default lines the chosen action prints. The synthetic no-login hosted group is not a configured backend, so the picker leaves its rows out; the listing's `! ` hint is still printed before the table, so the operator knows to log in. Choosing a reachable target row stores that backend's default and clears the other backend's: a hosted choice clears every stored S3 default, an S3 choice becomes the only one. A `!` row is informational, so choosing it is refused as an unlisted target, like the plain path. Choosing a create row prompts for a name on stderr, creates the target, prints the create line, makes the new target the default, and prints the default line; a skipped prompt changes nothing and prints `Nothing was changed`, and a prompt read error exits nonzero without creating anything. The rows, columns, and signal rules are in [tui.md](./tui.md).

**Status and log.** `status [path]` compares the directory with the accepted state under the operation lock (`Notebook.Status`): the accepted generation, and one line per added, modified or deleted file with its line counts, ignored files excluded. Changes under protected (read-only or non-writable) paths are listed too, though a commit refuses them. It reads no remote state and changes no file (like pull, it creates a missing directory); a directory that is not pulled says so, and one that needs recovery says so instead of listing changes. Below the path line it names the source of the target only when the directory remembered it: `space: remembered space` for a hosted record and `bucket: remembered bucket` for an S3 record (`Runtime.SpaceSource` into `ReportStatus`, [config.md](./config.md), The operator surface of the record). A flag, a variable or the global default gets no trailer. `log [path] [--limit N]` prints the messages of the accepted publications this machine holds, newest first (`Notebook.Log`, walking the baseline head's first parents); history dropped by a checkpoint ends the walk and the report says older publications exist. Both take pull's flags and run the same startup (including the store check), so they need the store configuration pull needs (`TestScenarioCLIStatusAndLog`, `TestScenarioStatusNamesTheRememberedSource`).

**Argument refusals come first.** `pull`, `commit`, `status` and `log` resolve the positional path (`app.OperationPath`, which `app.OperationSetup` calls for `pull`, `status` and `log`) and, for commit, the message, before `app.Setup` (`log` also refuses `--limit` below 1 first: `log: --limit must be at least 1, got N`), so a bad command line never opens the engine or the store (`TestPullRefusesExtraPaths`, `TestCommitRefusesWithoutMessage`). `OperationSetup` is that resolve plus `Setup` over `opts.WithAssociation(path)`, which is how a path-taking command consults the scoped settings of its notebook directory ([config.md](./config.md)); `commit` keeps its own two steps because its message refusal sits between them. `OperationPath` accepts zero or one positional path; flags after the path are parsed by `positionals`, and `--` ends flag parsing. `resolvePath` expands a leading `~` or `~/` (`pathutil.ExpandHome`), cleans absolute paths, and joins relative ones to `cwd`. An empty path means the workspace root (`Runtime.resolve`), which is also the key the association is read by. The at-or-below-root rule is enforced later by `workspace.Open`.

**Which commands remember a directory.** `pull`, `commit`, `status`, `log` and `serve` carry an association, so they read the scoped settings of the directory they address: the path-taking commands key it by their resolved path, and `serve` keys it by its workspace root. `pull`, `commit` and a successful `serve` tool call write the record ([config.md](./config.md), Recording the association); `status` and `log` change nothing. A `serve` with no configured root owns a per-process scratch directory, so `Flags.resolve` skips its association and it neither reads nor writes the file. The help of the four path-taking commands says so in one sentence each, `serve`'s help names the durable/scratch split, and the shared reference states the order once.

**Startup refusal surface.** `app.setup` runs in this order: config resolution (including `resolveStorage`, which may read the credentials file, [login.md](./login.md)), logger rebuild, the Info record of the chosen store (`logStorage`), `engine.Open()`, the first minted token of a stored login (`mintedTokens`, `mintRefusal`) or the hosted space lookup (`resolveHostedSpace`, `SLIVINGDOC_TOKEN` only), store factory, `checkStoreWithProof` (`storage.Probe` unless a fresh recorded proof stands in, or `CheckAccess` for a hosted store, [storage.md](./storage.md), [hosted-mode.md](./hosted-mode.md)), `NewService`. Every failure returns before any transport starts. Config diagnostics pass through `mcp.Redact`; the probe diagnostic is prefixed `INCOMPATIBLE_STORE` and also redacted, except a probe that the S3 service refused for its credentials or bucket (`ErrAccessDenied`), which is `app: S3 storage refused the credentials or the bucket: ...; check the AWS credentials, --bucket, --region and --endpoint`; the hosted diagnostics are redacted too (`app: hosted storage refused the token: ...; check SLIVINGDOC_TOKEN and --space` (or the spelling that named the space: `--bucket`, `SLIVINGDOC_SPACE`, `SLIVINGDOC_BUCKET`), `app: hosted storage refused the token minted for space ...: ...; run 'slivingdoc space' to list the login's spaces, or 'slivingdoc login' again`, `app: the stored login could not mint a token for space ... at <site>: ...; <fix>`, `app: the token reaches hosted space ...`, `app: hosted storage cannot name the token's space, and ...; ... pass the space name as --space or SLIVINGDOC_SPACE`, `app: hosted storage refused the token: ...; check SLIVINGDOC_TOKEN, or pass --space: the space ... is the stored default space`, `app: hosted storage refused the token: ...; check SLIVINGDOC_TOKEN and SLIVINGDOC_BUCKET`, `app: hosted storage refused the token: ...; check SLIVINGDOC_TOKEN: it named space ... but was then refused`, `app: INCOMPATIBLE_STORE: hosted storage check failed: ...`, `app: hosted storage check failed: ...`, `app: create hosted store: ...`). Engine-open (`app: open native engine: ...`) and S3 store-construction (`app: create object store: ...`) errors are not redacted. A refusal after the ephemeral session directory exists removes it (`Flags.resolve` defer, `removeSessionDir` in `setup`). Any error returned from `Setup` (argument, config, engine, store, probe or hosted check) makes the router print one stderr line `error: <diagnostic>` (`✗ <diagnostic>` when stderr is a terminal) and exit 1 with an empty stdout (`assertOneRedactedDiagnostic`). A flag-value or unknown-flag error is caught earlier by the router's `fs.Parse`: exit 1 and one stderr line `error: <command>: <flag error>; run 'slivingdoc <command> -h' for its flags`.

**Readonly mode.** There is no global read-only switch. "Read-only" is the per-process path policy from `--read-only-paths` and `--writable-paths` (see [config.md](./config.md)). `NewService` builds a `git.PathPolicy`; `Runtime.ReadOnlyPaths` / `WritablePaths` return the normalized sets, and `app.Report` prints them as `writable:`, `read-only:` and `path-rule:` trailers (`writePathSets`). Enforcement lives in the notebook ([commit.md](./commit.md), [pull.md](./pull.md)).

**Service lifecycle.** `Service.notebookFor` opens one `workspace.Workspace` and one `notebook.Notebook` per requested directory on first use, under `Service.mu`, and caches them in `opened`, keyed by `filepath.Clean` of the path so two spellings of one directory share one workspace. `Service.Close` closes every workspace and makes later calls fail with `app: service is closed`. `Runtime.Close` closes the service, closes the engine, and removes the ephemeral session directory.

**Serve and shutdown.** `app.serve` runs `srv.Serve` over `sdk.StdioTransport` wrapped in `closeTransport`. Client EOF returns cleanly (exit 0). Context cancellation or a signal from `terminationSignals` logs `shutting down`, cancels the context, closes the live connection (which makes the SDK cancel in-flight handler contexts), and waits up to `ShutdownDeadline` (default 30 s). Expiry returns `app: shutdown deadline expired`, a nonzero exit.

**CLI report.** `app.Report` reuses the MCP mappers (`mcp.MapSuccess`, `mcp.MapError`) so the CLI and the tools say the same thing. Success writes `OK  generation <n>  <path>`, one line per changed file with `+ins -del` (a zero side omitted), and the totals line, or for a materialization the outline of `SuccessInfo` through `writeOutline` ([product-contract.md](./product-contract.md#materialization)). A domain error writes `CODE · REASON`, the message, one aligned line per file with its reason in words (`fileReasonWords`) and `lines a-b` ranges, `next:` (`nextStep`: a reason's own wording from `reasonNextSteps`, such as `DIRECTORY_NOT_EMPTY`, else `actionWordings`), `retryable:`, `recovery:` when present, and the path-set trailers. `Report` returns `errors.New(te.Code)`, so the router exits nonzero with the terse category. A non-domain error (cancellation) is returned unchanged.

A refusal that happens in `Setup`, before any operation runs, reaches the router's one-line error instead. A domain error there renders through `app.RefusalLine` as `CODE · REASON` and its message on stderr, so a refused `slivingdoc pull` prints the same stable tokens an operation report does, without its cause or a path. Every other startup failure keeps the plain `error: <message>` form ([tui.md](./tui.md)).

**Colour.** `tui.Detect` requires a writer with a file descriptor (an `*os.File`, or the `tui.Guard` over one) that `term.IsTerminal` accepts, so `/dev/null` does not count, and an empty `NO_COLOR`; otherwise the style is plain and the report is byte-identical plain text. On a terminal the success line gains a green ✓ before `OK`, the error line a red ✗ before the code, and `next:` becomes a blue →; the totals, `retryable:` and `recovery:` lines are dimmed ([tui.md](./tui.md)). `main.go` passes `ProcessOptions.Env == nil`; `cli.Run` resolves it to `os.Environ()` through `withProcessEnv` and stores it in `opts.Env` before building the commands, so the router, `cmd/pull`/`cmd/commit` and `app.Report` read the real `NO_COLOR` and `COLORTERM`.

**Progress.** `pull` and `commit` run their operation inside `Runtime.WithProgress`, which draws a spinner line on stderr only when stderr is a terminal: `Pulling <path> · space <name> · 1.2s` (`Publishing` for commit, `bucket <name>` for S3). It is cleared before the report prints; `serve` prints one header line on a terminal's stderr when it is ready.

**The passphrase ask.** Before that progress line, `pull` and `commit` call `Runtime.AskForSecret`: when stdin and stderr are terminals, the process resolved no secret, and the stored notebook is encrypted, the command asks `Passphrase or recovery code: ` on stderr and reads one line from stdin ([encryption.md](./encryption.md), The terminal ask of `pull` and `commit`). The typed characters show as one `*` each, never as themselves ([tui.md](./tui.md), The masked read). The answer stays in memory for that run and is never written to the key record; an empty answer, Ctrl-C, or Ctrl-D with nothing typed reports `STORAGE_FAILURE · KEY_REQUIRED` and exits nonzero. `status`, `log`, `serve` and `encrypt` never call it.

**Perf.** `StartPerf` is read from `DEBUG_PERF` only: empty, `0`, `false` disable it; `1`, `true` use `<tmp>/slivingdoc-perf`; any other value is the base directory. Each run gets a timestamped directory with `cpu.pprof`, `trace.out` and `heap.pprof`. Every failure is a warning; the capture never changes the exit code. It deliberately does not default to the working directory, because a binary profile inside a notebook would be refused by the next commit.

**Streams.** Stdout carries MCP messages, help, the version line and the CLI report only. `ProcessOptions.Stdout` never reaches the MCP stream: `serve` uses `sdk.StdioTransport`, bound to `os.Stdin`/`os.Stdout` (tests inject `process.transport`), and `Setup`'s `io.Discard` default for `Stdout` is unused by it. Command output uses `ProcessOptions.Out()`, which falls back to `os.Stdout`.

## Gotchas

- The router parses a command's flags with the Go `flag` package, which stops at the first positional argument. Any new positional-taking command must call `app.OperationPath` or `app.OperationSetup` (or equivalent) so trailing flags are honoured, and must set the association of its notebook path when it should remember a hosted space.
- A new command needs a place in `commandGroups` (`internal/cli/home.go`), or `TestCommandGroupsCoverTheMap` fails.
- `serve.Command` sets `Ephemeral` on its copy of `opts`; `pull` and `commit` must not, since a human needs the directory after exit.
- Records emitted before flags resolve (routing, the level-fallback warning) use the environment logger; only after `setup` rebuilds it do `--log-level`/`--log-timestamp` apply. The refusal line itself is not a log record: the router prints it.
- Router lines (the error line, usage, command help, the home screen) carry no timestamp and go to the injected `ProcessOptions` streams, so the unit tests read them. Every `Setup`/`Run` error exits exactly 1, and `Setup` receives `context.Background()`, not the caller's context.
- `ProcessOptions.StoreFactory` receives the exported `ServiceConfig`, not the internal `config`. Tests inject `fake.New(prefix)` here.
- `Runtime.Serve` passes `Module(r.base, ModuleMCP)`; do not pass `r.logger` (bound to `app`), or `LOG_LEVEL=mcp=...` stops working.

## Related

- [config.md](./config.md): flags, environment, precedence, the session directory.
- [mcp-server.md](./mcp-server.md): what `Runtime.Serve` registers.
- [errors.md](./errors.md): codes, reasons, actions, and exit codes.
- [logging.md](./logging.md): `NewLogger`, `Module`, the logger rebuild.
- [tui.md](./tui.md): the terminal presentation, the home screen, the pickers.
- [encryption.md](./encryption.md): the `encrypt` subcommands, flags and reports.
- [running.md](./running.md): operator view of the commands and the report.
- [notebook.md](./notebook.md), [workspace.md](./workspace.md): what `Service` opens.
- [AGENTS.md, Startup Wiring](../AGENTS.md#startup-wiring-maingo): the ordered startup list.
