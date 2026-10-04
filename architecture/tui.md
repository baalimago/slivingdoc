# Terminal presentation

How slivingdoc looks to a person at a terminal, and how the same commands stay plain for scripts, pipes, CI and `NO_COLOR`. One package, `internal/tui`, holds the palette, the status marks, aligned columns, the progress spinner and the list picker; the router, the pull/commit/status/log reports, `serve`, `login`, `space` and `logout` all render through it. It answers "why does this line look like this on my terminal, and what does a script see instead?"

Read this when: changing any line a command prints for a person, adding a command's output, adding a colour or a mark, touching the home screen, the help layout, the progress line, or a picker.

## Key files

| File | Purpose |
|------|---------|
| `internal/tui/style.go` | `Style` (`Mode`: `Plain`/`Styled`; `Depth`: `Basic`/`TrueColor`), `New`, `Detect`, `IsTerminal`, the palette (`Brand`, `Good`, `Warn`, `Bad`, `Dim`, `Bold`), `Mark` (`Done` ✓, `Failed` ✗, `Caution` ▲, `Next` →, `Brand` ◆), `Header` |
| `internal/tui/columns.go` | `Cell`, `Style.Columns` (left-aligned columns measured on the unpainted text; trailing empty cells dropped) |
| `internal/tui/spinner.go` | `Style.Spin`, `Spinner.Stop`, `SpinnerInterval` |
| `internal/tui/guard.go` | `Guard`, `NewGuard`: the stderr writer that the spinner and the logger share, so a log line first clears the progress line |
| `internal/tui/picker.go` | `Picker`, `Style.Pick`, `ErrSkipped`: a numbered list over `go_away_boilerplate/pkg/table` (the same table TUI as clai) |
| `internal/cli/cli.go` | `Run`: wraps `ProcessOptions` stderr in a `tui.Guard` before it builds the logger and routes |
| `internal/cli/route.go` | `router`: routing, `fail` (the one-line error), `help`, `styleHelp` |
| `internal/cli/home.go` | `commandGroups`, `router.home` (the home screen), `router.usage`, `router.loginStatus`, `router.commandTable` |
| `internal/app/present.go` | `ProcessOptions.errStyle`/`outStyle`/`styleOf` (the `Style` seam), `pickable`, `spacesTable`, `spaceRows`, `pickSpace`, `pickLogins`, `skipped`, `line` |
| `internal/app/config.go` | `EnvLookup`: the `getenv` that `Detect` reads, over `ProcessOptions.Env` |
| `internal/app/status.go` | `LoginStatus`, `StoredLogins` (the home screen's login lines, from the credentials file only) |
| `internal/app/progress.go` | `Runtime.WithProgress` (the pull/commit progress line), `Runtime.Target`; `statuslog.go`: `ReportStatus` (whose `space: remembered space` trailer is dimmed like every other detail line), `ReportLog` render through the same style |
| `internal/app/command.go` | `Report`, `writeSuccess`, `writeError`, `writePathSets` render through a `tui.Style` |
| `internal/app/login.go` | `showApproval`, `countdown`, `approvedBy`, `loggedIn`, `Login.offerDefault`, `ProcessOptions.ErrOut`, the styled `Login.report` and `Logout.Run` lines |
| `internal/app/space.go` | `Space.Run` (the picker), `Space.list` (the styled table or the plain `*` list) |
| `internal/app/app.go` | `ProcessOptions.Style` (test seam), `Runtime.Serve` (the ready header) |

## Flow

```text
every stream decides once:  tui.Detect(w, getenv)
  NO_COLOR non-empty             → Plain
  w not a terminal (IsTerminal)  → Plain   (pipe, file, /dev/null, buffer)
  otherwise                      → Styled, TrueColor if COLORTERM=truecolor|24bit, else Basic

slivingdoc                       router.home: Styled → home screen; Plain → Usage   (exit 1)
slivingdoc -h|-help|--help|help  router.usage                                       (exit 0)
slivingdoc help <cmd>            router.help for that command; unknown <cmd>: fail + usage (exit 0 / 1)
slivingdoc <cmd> -h              router.help → styleHelp(command.Help())            (exit 0)
slivingdoc --flag (no command)   router.fail "give a command before --flag" + home  (exit 1)
unknown command                  router.fail + usage                                (exit 1)
bad flag                         router.fail "<cmd>: <err>; run 'slivingdoc <cmd> -h' for its flags" (exit 1)
Setup or Run error               router.fail: Plain "error: <msg>", Styled "✗ <msg>"; always one line

pull / commit    Runtime.WithProgress: spinner on stderr "Pulling|Publishing <path> · space|bucket <name> · 1.2s"
                 app.Report on stdout (Styled adds ✓/✗ and → before the next step)
serve            Styled stderr → "◆ slivingdoc serve · <space|bucket> · <root> · waiting for an MCP client on stdio"
login            header, Open/Code rows, the ▲ warning, a countdown spinner, the approval block with the spaces table,
                 the y/N prompt, then (on a terminal, no default chosen, 2+ spaces) the space picker
space            on a terminal: the picker with the default marked; elsewhere the list (Styled: table; Plain: "* name …")
logout           on a terminal with 2+ logins and no --site/SLIVINGDOC_SITE: a picker of the logins (several allowed)
```

## Behavior

**Plain is the script form.** A plain rendering carries no escape codes and no marks: `OK  generation N`, `next:`, the `*` space list and `Logged in as …` are the lines scripts read. A mark in a plain line is the empty string, so `s.Mark(tui.Done) + "text"` is just `text`. `NO_COLOR` makes every line plain, but it does not turn pickers off: a picker depends on whether a person can answer (below), not on colour, and on the terminal it still clears itself with cursor escapes once answered.

**A report gains a line only for what the operator cannot infer.** `status` prints one extra line, `space: remembered space`, and only when the notebook directory's own record named the space ([config.md](./config.md)); the store it already printed on the line above it. A source the operator set by hand needs no explanation, so it adds nothing, and a pipe reads the trailer as the same plain words, since it is rendered through the one style of the stream like every other line (`TestScenarioStatusTrailerOnTerminal`).

**Styled adds, never removes, what a person must check.** The login's "Only approve it if you started this login in your own terminal." warning, the account that approved the code, the storage endpoint, the site, every space with its owner's email, and the `Store this login? [y/N]` prompt all appear in both forms. The owner column never says "you": whoever approves a code decides the login, so the owner email is how someone else's approval shows (architecture/login.md, Threat model).

**The router owns its lines.** Every line of `internal/cli`'s `router` goes to the injected `ProcessOptions` streams: the usage (sorted command table) on stdout, a refusal as exactly one `error: <message>` line on stderr (`✗ <message>` on a terminal), a command's `Help()` text as is on stdout (the header and painted section titles on a terminal). `help`, `-h` and `--help` without a command exit 0; a bare `slivingdoc` exits 1 as before, showing the home screen on a terminal and the usage elsewhere.

**Home screen.** Header with the version, two lines per stored login (`Logged in as <account>`, followed by ` at <endpoint>` when it is not the default endpoint, with access and expiry; then `Default space <name>` or "No default space · → slivingdoc space"), one line for an expired login (the account and "the login expired · → slivingdoc login"), a ▲ line when the credentials file cannot be read, or "Not logged in to hosted storage · → slivingdoc login", then the commands under `commandGroups` (Sync, Agents, Account, About). It reads the credentials file only (`app.StoredLogins`); nothing is sent. `TestCommandGroupsCoverTheMap` fails when a command is added to `cli.Commands` without a group.

**Progress lines.** `Style.Spin` redraws `\r\x1b[K  <frame> <label>` every `SpinnerInterval` on stderr and clears the line on `Stop`; a plain style gets a spinner that writes nothing. The label is read on every frame and cut to the terminal's width, read once when the spinner starts, with "…" (`fit`, escape sequences kept whole; a rune counts as one column, so wide characters can still wrap), because `\r\x1b[K` clears only the last row of a wrapped line. Reading it per frame lets login's line counts down to the code's expiry (`countdown`) and pull/commit show the elapsed seconds. `cli.Run` hands the logger and every command the same `tui.Guard` over stderr: the spinner draws through it, and any other write (a log record, a notice) first clears the progress line, so the two never share a line. A spinner's write failure is returned by `Stop`, and the callers discard it on purpose: the line is decoration and never changes the outcome. Stdout, where the report goes, never carries a spinner.

**Pickers.** `Style.Pick` prints the title, then runs the go_away_boilerplate table: rows numbered from 0, a typed number (or the list `0,2` and the range `0:2` when `Many`), `[n]ext`/`[p]rev` paging, `/text` filtering. Rows reach the table with every `Cell.Paint` dropped (`unpainted`), so a filter never matches an escape code; only the table's theme colours the header and the prompt. A number past the last row prints "There is no row N; pick again." and asks again; several rows in a single-row picker print "Pick one row." and ask again; a row typed twice counts once. `q`, `b`, Ctrl-C and end of input return `ErrSkipped`; any other failure (a broken terminal, a failed write) is an error. With `Picker.In` nil the table reads the terminal named by `$TTY`, else `/dev/tty`, and clears itself once answered; tests pass a reader, one line per answer. A picker appears only when a person can answer: login's space picker needs stdin and stderr on a terminal (`ProcessOptions.terminal`); `space` also needs stdout on a terminal (`pickable`), so `slivingdoc space | cat` keeps the list; logout's needs a terminal and no `--site`/`SLIVINGDOC_SITE`. Leaving a picker changes nothing: login keeps the stored login without a default space and prints the `slivingdoc space <name>` hint, `space` prints "Nothing was changed", logout keeps every login. A picker error after login stored its key is printed as a ▲ line, not returned, because the login stands. Before that picker, login revokes the key the new login replaced (`Login.revokeReplaced`), so an interrupt at the picker, which ends the process, never skips that revocation; it skips the picker when releasing the credentials lock failed, since the picker's store would wait on the lock this process still holds. Then login stops listening for termination signals and detaches its context from them (`context.WithoutCancel`), so the picker reads the terminal on its own terms; it is not shown at all when the context already ended.

**Colours.** The palette is the site's (`--glow` #5aa2ff brand blue, `--ok` #19b876 green, `--warn` #f0a03c amber) plus slivingdoc's own failure red #f26d6d, which the site does not use, in 24-bit when `COLORTERM` announces it, else the 16-colour codes 34, 32, 33, 31; dim is `2`, bold `1`.

## Gotchas

- Measure before you paint: `Columns` pads on `Cell.Text` and paints afterwards. Padding a painted string counts escape bytes as width and breaks the alignment.
- A picker and the login prompt read different sources: the prompt reads `ProcessOptions.stdin()` (the process stdin) through a `bufio.Reader`, the picker reads `ProcessOptions.Stdin` or, when nil, `/dev/tty`. A test feeding both through one reader must hand it over one byte per read (`iotest.OneByteReader`), or the prompt's buffer swallows the picker's line.
- `ProcessOptions.Style` is the test seam for login, space and logout; `Report`, `WithProgress` and the serve header detect on their streams directly and are proven on a pseudo-terminal (`TestScenarioCLIColourOnTerminal` for the report, `TestScenarioCLIHomeOnTerminal` for the home screen and help, `TestScenarioCLIStderrOnTerminal` for the pull progress line and the serve header).
- `IsTerminal` asks the terminal itself (`golang.org/x/term`), not the file mode: `/dev/null` is a character device but not a terminal, so it gets the plain form.
- The go_away_boilerplate table colours through its own `Colorize`, which reads `NO_COLOR` from the process environment; `Pick` passes an empty theme for a plain style, so both agree.
- The router still parses a command's flags with the Go `flag` package, which stops at the first positional argument; `pull` and `commit` read later flags through `app.OperationPath` (architecture/cli.md).

## Related

- [cli.md](./cli.md): routing, the report, exit codes.
- [login.md](./login.md): the login flow and its threat model, which the styled login must keep.
- [product-contract.md](./product-contract.md): the CLI report's plain form.
- [errors.md](./errors.md): exit codes and what each stream carries.
