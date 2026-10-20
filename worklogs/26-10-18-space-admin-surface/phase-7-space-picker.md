# Phase 7: interactive picker

**Status:** Complete (2026-10-20, session 13). R2-02 and R2-05 are verified/fixed; the focused tests, full quality gate, and dupl triage passed.

README: [README.md](./README.md)

## Goal

Let a person pick the default target from one table and create a target from a
create row, with the same store path as `space <name>`.

## Specification

### Terminal harness

The picker scenarios run the CLI under a pseudo-terminal, because
`ProcessOptions.pickable()` is `OnTerminal` only while stdin, stdout, and stderr
are all terminals.

Phase 7 owns the harness in
`internal/integrationtest/scenario_space_tty_linux_test.go`:

| Helper | Contract |
| --- | --- |
| `openPTY` (existing, `scenario_colour_linux_test.go`) | Reused unchanged |
| `runCLIOnTypedTTY` (new) | One PTY with the slave on the child's stdin, stdout, and stderr; scripted bytes written only after the picker prompt is observed; bounded master read with a deadline that returns every byte seen; the exit code and the terminal bytes returned; a `signal func(os.Signal) error` for the running child, so the interrupt scenario sends `os.Interrupt` after the table is drawn; a host without `/dev/ptmx` names that capability and skips |
| `runCLIOnTTY` (existing, `scenario_colour_linux_test.go`) | Not used by the picker scenarios, because it gives the child a pipe on stdin, so `OnTerminal` never holds |

| Rule | Mechanism | Test |
| --- | --- | --- |
| Picker scenarios run under a real PTY with stdin, stdout, and stderr on the slave | `runCLIOnTypedTTY` | `TestTerminalHarnessRunCLIOnTypedTTY` |

### The table

One `tui.Picker` table, no group headers. The space picker is a new picker and
does not reuse `pickSpace`; `pickSpace` and `spacesTable` remain the login
flow's helpers in `internal/app/present.go`. Columns:

| Column | Hosted row | S3 row |
| --- | --- | --- |
| `TARGET` | `hosted <storage endpoint> <account>` | `s3 <endpoint or (AWS)> <region> profile=<ambient or name> key=<id>` |
| `NAME` | the space name, prefixed `! ` when unreachable | the bucket name, prefixed `! ` when unreachable |
| `DIRECTORIES` | Phase 4's display bound | the same |
| `ACCESS` | `write` or `read` | empty |
| `OWNER` | email or `a team` | empty |
| `""` | `default` on the default row | `default` on the default row |

Rows keep the grouping order of the plain listing: hosted backends in
credentials-file order, then S3 backends with the current chain first. Within
one backend, rows are sorted by name.

After the rows of each configured backend comes one create row:

```text
TARGET: hosted https://api.slivingdoc.dev ada@example.com
NAME:   + Create a new hosted space

TARGET: s3 (AWS) eu-north-1 profile=dev key=AKIAIOSFODNN7EXAMPLE
NAME:   + Create a new S3 bucket
```

An S3 create row uses the current identity, never a recorded identity (README,
`space new`). A backend with no rows still contributes its create row. A
backend that could not list contributes its default and recorded rows plus the
create row, so a degraded configuration is still usable.

An unreachable row remains selectable: setting it as the default is the
operator's explicit choice, and the hint that explains it has already been
written to stderr. The picker does not repeat the hint.

The picker title is:

```text
Pick the default target
```

Every picker line goes to stderr. The picker is drawn only when
`ProcessOptions.pickable()` is `OnTerminal`; a pipe keeps the plain listing.

### Choosing a row

Each picker index maps to one `spaceChoice`:

```go
type spaceChoice struct {
    backend spaceBackend
    row     SpaceRow
    create  bool
}
```

A reachable target row calls `backend.SetDefault(ctx, row.Name)` on the
backend its row came from: the hosted login and the space name, or the S3
identity and the bucket name. The backend re-checks its credential and
writes the default under its own lock. The line is Phase 4's or Phase 6's
default line.

A create row prompts on stderr:

```text
Name for the new hosted space: 
Name for the new S3 bucket: 
```

The prompt reads one line through the same context-aware reader as
`Login.confirm`: a read runs beside the context, and a cancelled context is a
skip. Only that read runs under `ProcessOptions.interruptible`, so the first
termination signal cancels the read instead of ending the process ahead of the
skip line. The picker table itself is not wrapped: `tui.Style.Pick` reads
`Picker.In` with no context, so `signal.NotifyContext` would swallow the signal
while that read blocked. Ctrl-C at the table therefore keeps today's behavior,
ends the process, and writes nothing. An empty line, EOF, or a cancelled prompt,
and `q` at the table, print:

```text
Nothing was changed
```

and exits zero. A read error is not a skip: it is returned, prints no line, and
exits nonzero (below).

A name is created through `backend.Create`. The create line is printed exactly
as the non-interactive `space new` path prints it. After a successful create
the command makes the new target the default with the same `SetDefault` path
and prints the default line, because choosing is what the picker is for. A
refresh of the table is best-effort display work and must never turn a
successful create into a failure. A create refusal is printed and exits
nonzero; the default is unchanged.

### Plain output

The plain listing, the create lines, the default lines, the hints, and the exit
codes are Phase 4's and Phase 6's. The picker adds no fact the plain form
omits and changes none of its text.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| One table, no group headers | `tui.Picker` unchanged | `TestSpacePickerRows` |
| One create row per configured backend | row builder | `TestSpacePickerCreateRows` |
| An S3 create row uses the current identity | backend selection | `TestSpacePickerS3CreateUsesCurrentIdentity` |
| A chosen row carries its identity | `spaceChoice` | `TestSpacePickerChoosesTheRightBackend` |
| A skip changes nothing and exits zero | `tui.ErrSkipped` | `TestSpacePickerSkip` |
| An empty line, EOF, and Ctrl-C at the prompt skip | context-aware reader under `interruptible` | `TestSpacePickerPromptSkips` |
| Ctrl-C at the table ends the process and writes nothing | the table's read takes no context; only the prompt read does | `TestScenarioSpacePickerInterruptAtTable` |
| An empty backend still offers create | row builder | `TestSpacePickerEmptyGroupStillOffersCreate` |
| A pipe never opens the picker | `pickable` | `TestSpacePickerNotOnAPipe` |
| Plain output is unchanged | both paths use the same renderer | `TestSpacePlainUnchanged` |

### Documentation

| Doc | What must become true |
| --- | --- |
| `architecture/tui.md` | The `space` picker table is documented with the columns `TARGET`, `NAME`, `DIRECTORIES`, `ACCESS`, `OWNER`, and the empty header column that shows `default` on the default row, plus the create row and the name prompt. |
| `architecture/cli.md` | The `space` command section says that with no positional argument on a terminal the command opens the picker, that every picker line goes to stderr, and that a create row selects the new target. |
| `architecture/testing.md` | The terminal harness `runCLIOnTypedTTY` is named as the PTY helper of the picker scenarios, with the capability skip it inherits from `openPTY`. |

### Files

| File | Change |
| --- | --- |
| `internal/app/space.go` | Add the interactive picker path that builds `spaceChoice` rows, prompts for create, and calls `SetDefault` on selection. |
| `internal/app/present.go` | Add the `tui.Picker` table for the `space` command, with the documented columns, create rows, and prompt text. |
| `internal/app/login.go` | Extract the context-aware line reader that `Login.confirm` uses into the shared helper the create prompt calls; the confirm prompt and its output stay unchanged. |
| `internal/integrationtest/scenario_space_tty_linux_test.go` (new) | Add `runCLIOnTypedTTY` and the real PTY scenarios for picker skip and create. |
| `internal/app/space_test.go` | Add unit tests for picker rows, create rows, backend choice mapping, and the pipe path where `TestSpacePlainUnchanged` asserts plain output is unchanged. |

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `space` on a terminal with two spaces | sitetest, injected `Stdin`, `Style` | the table on stderr | none | a plain listing on stdout |
| choose the second row | sitetest | the default changes to that space | one `SetDefaultSpace` | a create |
| choose a create row and type a name | sitetest | create line, then default line | one create, one default write | a second create |
| choose a create row and type an empty line | sitetest | `Nothing was changed`, exit 0 | none | a create |
| choose a create row and press Ctrl-C | sitetest | `Nothing was changed`, exit 0 | none | a create, a default write |
| `space` on a terminal with no spaces | sitetest | only the create row | none | a refusal |
| `space | cat` | sitetest | the plain listing on stdout, exit 0 | a picker |
| `space` on a terminal, S3 backend | fake admin | the S3 create row uses the current identity | none | a recorded identity |
| `space` on a terminal, degraded backend | fake admin error | rows, create row, hint on stderr | none | a hidden group |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| The table has the documented columns and cells | `TestSpacePickerRows` |
| Hosted rows carry access and owner; S3 rows do not | `TestSpacePickerRows` |
| Unreachable rows print `!` in `NAME` | `TestSpacePickerUnreachable` |
| One create row follows each configured backend | `TestSpacePickerCreateRows` |
| An empty backend still shows its create row | `TestSpacePickerEmptyGroupStillOffersCreate` |
| Choosing a row stores that backend's default | `TestSpacePickerChoosesTheRightBackend` |
| A create row prints the create line and then the default line | `TestSpacePickerCreatesAndSelects` |
| An empty, EOF, or cancelled prompt prints `Nothing was changed` and exits 0 | `TestSpacePickerPromptSkips` |
| A pipe keeps the plain listing and never draws the picker | `TestSpacePickerNotOnAPipe` |
| Plain output is byte-identical to the non-picker path | `TestSpacePlainUnchanged` |
| The S3 create row uses the current identity | `TestSpacePickerS3CreateUsesCurrentIdentity` |
| A real PTY skip exits 0 and prints `Nothing was changed` | `TestScenarioSpacePickerSkipOnTerminal` |
| A real PTY create selects the new target | `TestScenarioSpacePickerCreateOnTerminal` |
| Ctrl-C at the table ends the process with exit 130 and writes nothing | `TestScenarioSpacePickerInterruptAtTable` |
| The terminal harness attaches stdin, stdout, and stderr to a PTY and returns the exit code and terminal bytes | `TestTerminalHarnessRunCLIOnTypedTTY` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| the table cannot be drawn | the picker error, nonzero | `TestSpacePickerDrawFailure` |
| the prompt read fails | the read error, nonzero, nothing created | `TestSpacePickerPromptReadError` |
| the context ends at the prompt | `Nothing was changed`, exit 0 | `TestSpacePickerPromptSkips` |
| Ctrl-C at the table | the process ends on SIGINT (exit 130); nothing written | `TestScenarioSpacePickerInterruptAtTable` |
| create refused (`name_taken`, `space_limit`, read-only) | the Phase 4 refusal, nonzero | `TestSpacePickerCreateRefused` |
| create refused (`ErrBucketOwned`, `ErrBucketExists`, denied) | the Phase 6 refusal, nonzero | `TestSpacePickerS3CreateRefused` |
| `SetDefault` fails after a create | the create line, then the default error, nonzero | `TestSpacePickerDefaultWriteFailure` |
| the backend changed between listing and store | “run the command again” | `TestSpacePickerLoginChanged` |
| a refresh fails after a successful create | the create and default lines, exit 0 | `TestSpacePickerRefreshFailure` |

## Implementation notes

The working tree already carried the picker implementation when this session
began: `Space.list` opened the picker, `Space.pick` mapped a `spaceChoice` to
`SetDefault` or `Create`, `promptName` read the create name under
`ProcessOptions.interruptible`, and `present.go` held the documented table.
This session audited every acceptance and error row against the tree, reran
the picker tests under the race detector, and completed the missing
documentation:

- `architecture/testing.md` now names
  `internal/integrationtest/scenario_space_tty_linux_test.go` as the picker's
  PTY scenarios, with `runCLIOnTypedTTY` and the `openPTY` capability skip,
  and lists `runCLIOnTypedTTY` beside `runCLITTY` and `runCLIOnTTY` in the
  terminal seam row.
- The status board, the phase status, and the session journal were updated in
  the same change.

Material decisions confirmed in this session:

- The picker's rows are the plain listing's configured backends: one row per
  target and one create row after each backend. The synthetic no-login hosted
  group has no backend to call `SetDefault` or `Create` on, so the picker
  leaves its rows out; the plain listing still shows them, and its `! ` hint
  is written to stderr before the table so the operator knows to log in.
- An unreachable row of a configured backend stays selectable. The backend
  writes the default under its own lock, so the explicit choice is the
  operator's; the picker does not repeat the listing's hint.
- Only the create-name read is wrapped by `ProcessOptions.interruptible`. The
  table reads `ProcessOptions.stdin()` directly with no context, so Ctrl-C at
  the table keeps the process's default SIGINT behavior and no result line is
  written.
- A create refresh is display-only: its failure never changes the create
  line, the default line, or the exit status.

Decision 53 in `architecture/decisions.md` records the picker and the
no-login-group rule; `architecture/cli.md`, `architecture/tui.md`, and
`architecture/testing.md` state the same behavior.

Gates, in order:

- `go test ./internal/app/ ./internal/integrationtest/ -count=1` passed before
  the documentation edits; the picker scenarios passed again in the table
  below.
- `go test -race -count=1 -timeout=120s -run
  'TestSpacePicker|TestSpacePlainUnchanged|TestTerminalHarnessRunCLIOnTypedTTY|TestScenarioSpacePicker'
  ./internal/app/ ./internal/integrationtest/ -v` passed every picker test,
  including the real PTY skip, create, and interrupt scenarios.
- `go test -race -count=3 -timeout=30s ./internal/app/ ./internal/integrationtest/`
  passed.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .`, `go vet ./...`, and
  `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` printed nothing.
- `make qa` exited zero; coverage 89.3 % against the 70 % floor and all 35 npm
  tests passed. One earlier full `make qa` run hit the 30 s test binary
  timeout during the parallel run; reruns passed, and no timeout, count, skip,
  or race setting changed.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` reported the same three clone
  groups as the phase-6 gate; all are acceptable under the duplication policy.

The acceptance and error tests are the phase's names in
`internal/app/space_test.go`; the process scenarios are
`TestScenarioSpacePickerSkipOnTerminal`, `TestScenarioSpacePickerCreateOnTerminal`,
`TestScenarioSpacePickerInterruptAtTable`, and
`TestTerminalHarnessRunCLIOnTypedTTY` in
`internal/integrationtest/scenario_space_tty_linux_test.go`.

## Review findings

Session 7 found one documentation defect, fixed in the same change:

1. `architecture/testing.md` listed the hosted and S3 `space` scenarios but
   not the picker's PTY file, and its terminal seam row named only
   `runCLITTY` and `runCLIOnTTY`. It now names
   `runCLIOnTypedTTY`, its three-stream attachment, and the inherited
   `openPTY` capability skip.

The session also made two existing docs exact: `architecture/cli.md` and
`architecture/tui.md` had said the picker names every target, but the
synthetic no-login hosted group is not a configured backend and has no
backend to act on. Both now say the picker leaves those rows out while the
plain listing keeps them and prints their `! ` hint before the table, and
decision 53 in `architecture/decisions.md` records the rule.

The implementation defect the first `make qa` found was real: the PTY harness
imported `syscall` to read a signal exit, which
`TestNoGitExecutableOrGit2goImport` refuses process-wide. The harness now reaps
its child with `unix.Wait4` in `ttyProcess`, so it keeps the `128+signal` exit
code without the forbidden import.

No acceptance or error row is unimplemented. `architecture/tui.md` and
`architecture/cli.md` describe the picker table, columns, create row, prompt,
skip, and signal rules, and `TestSpacePlainUnchanged` proves the plain bytes
are unchanged.

### Review 2 (2026-10-20, session 11)

Round 2 re-ran `make qa` (exit 0, coverage 89.4 %, 35 npm tests) and rechecked
the picker. The skip, prompt, unreachable refusal, create-and-select, refresh
failure, login-changed, interrupt-at-table and plain-parity scenarios hold
(`TestSpacePickerSkip`, `TestSpacePickerPromptSkips`,
`TestSpacePickerRefusesUnreachable`, `TestSpacePickerCreatesAndSelects`,
`TestScenarioSpacePickerInterruptAtTable`, `TestSpacePlainUnchanged` pass).
Two findings keep the phase reopened:

- **R2-02 (major, shared with Phase 6).** The create path calls
  `backend.Create` with the typed name and never runs
  `storage.ValidateNewSpaceName`. The hosted backend is protected only inside
  `sitelogin.CreateSpace`, and the S3 backend sends the name straight to the
  service: a fake-admin demonstration created `Bad_Name`, and a dotted legacy
  name would be created by S3. The plain `space new` path validates, so the two
  creation paths disagree. Validate before the backend call and map the
  refusal to the `space new:` grammar line; add
  `TestSpacePickerRefusesAnInvalidS3Name`.
- **R2-05 (minor).** The table title is
  `Pick the default target for serve, pull and commit`. This build uses only a
  hosted default for those commands; an S3 default is stored for the sibling
  worklog. Scope the title to what the build does, or change it to a neutral
  "Pick the default target", and update the README sketch and
  `architecture/tui.md`.

No other picker behavior changed in this review.

### Final verification (2026-10-20, session 13)

- **R2-02:** The shared `Space.pick` path validates the typed name before
  `choice.backend.Create` and wraps a refusal as `space new: %w`. The test
  `TestSpacePickerRefusesAnInvalidS3Name` asserts the exact refusal, zero fake
  create calls, and no created names. `TestSpacePickerCreatesAndSelects`
  confirms that a valid name is created and selected.
- **R2-05:** The picker title is `Pick the default target`. The README sketch,
  `architecture/tui.md`, and the unit-test assertion use the same title. Plain
  output parity remains unchanged.
- The requested focused app tests passed:

  ```bash
  go test ./internal/app -run 'TestSpacePickerRefusesAnInvalidS3Name|TestSpacePickerCreatesAndSelects|TestSpacePickerSkip|TestSpacePickerPromptSkips|TestSpacePickerRefusesUnreachable|TestScenarioSpacePickerInterruptAtTable|TestSpacePlainUnchanged' -count=1 -v
  ```

  The interrupt scenario is in `internal/integrationtest`, not
  `internal/app`; its direct run also passed:

  ```bash
  go test ./internal/integrationtest -run '^TestScenarioSpacePickerInterruptAtTable$' -count=1 -v
  ```

- The coordinator ran `make qa` after the phase edits on this identical tree.
  It exited 0 with 89.4% coverage against the 70% floor; all 35 npm tests
  passed with none skipped, and all lint steps were silent. This verification
  session did not rerun `make qa`.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` found three clone groups, the
  same groups recorded for Phase 6. The `login.go` and `space.go` clones are
  declarative registration of different command-specific flags. One test
  fixture table is repeated at the public integration and notebook boundaries;
  the tests prove separate contracts. The other test clone is two rows in the
  Git merge table, with distinct conflict shapes and expected results. These
  are acceptable test tables, not duplicated test bodies. None is actionable
  under the duplication policy.
