# Phase 4: the operator surface and the docs

**Status:** Complete

README: [README.md](./README.md)

## Goal

Make the selected space and its source visible to the person who ran the
command, and record the design where the architecture docs keep it.

## Specification

### The `status` line

`status` already prints the resolved space through `Runtime.Target()`, so the
only new information is where that space came from. The report gains one
dimmed trailer after the path line, and only when the source is the
association.

Every other source is today's output, unchanged, because a source the operator
set by hand needs no explanation. `log` prints no space and gains none: its
report has no target to name.

The line is plain when stdout is not a terminal, exactly like every other line
the report prints, so a script sees one extra line only when the association
selected the space.

### Help text

The shared `app.FlagReference` gains one paragraph describing the order, and
each of `pull`, `commit`, `status` and `log` says that a directory with a
remembered space needs no space flag. The help is printed by `-h`, which
returns before any dependency loads, so the text costs nothing at startup.

`TestReleaseBinaryCommandSurface` and the `-h` scenario assert the text, so
the new paragraph is pinned there too.

### The docs, each in the change that touches its code

| Doc | Change |
| --- | --- |
| [`../../architecture/config.md`](../../architecture/config.md) | the settings table gains the association row, the precedence position and the refusal rows |
| [`../../architecture/login.md`](../../architecture/login.md) | the space-source list gains `remembered space`, and the `--storage auto` rules gain the remembered source beside the default |
| [`../../architecture/cli.md`](../../architecture/cli.md) | which commands read and write the association, and that `serve` does neither |
| [`../../architecture/decisions.md`](../../architecture/decisions.md) | one new recorded decision covering D1, D3, D6 and D10 together |
| [`../../architecture/README.md`](../../architecture/README.md) | one line for the new concern and its reading-order entry |
| [`../../architecture/running.md`](../../architecture/running.md) | the operator view: how a directory is remembered, how to see it, how to change it |
| [`../../architecture/workspace.md`](../../architecture/workspace.md) | one sentence that the association is not private state, because `DerivedKey` cannot key it |
| [`../../AGENTS.md`](../../AGENTS.md) | the package map gains `internal/settings`, and the runtime layout note says where the file lives |

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `status` in a directory with a remembered space | `Runtime.SpaceSource` | the source trailer | none | colour when not a terminal |
| `status` with a flag-named space | `Runtime.SpaceSource` | today's output | none | a trailer |
| `log` in any directory | `ReportLog` | today's output | none | a trailer |
| `pull -h` | `app.FlagReference` | the paragraph | none | touching a dependency |
| `serve -h` | `app.FlagReference` | the paragraph | none | touching a dependency |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| `status` names the remembered source when the association selected the space | `TestStatusNamesRememberedSpaceSource`, `TestScenarioStatusNamesTheRememberedSource` |
| `status` prints no trailer when a flag or the environment selected it | `TestStatusOmitsSourceForExplicitChoices`, `TestScenarioStatusNamesTheRememberedSource` |
| `log` prints no trailer in any case | `TestLogNeverNamesTheAssociation`, `TestScenarioStatusNamesTheRememberedSource` |
| The trailer is absent when stdout is not a terminal | `TestStatusTrailerIsPlainOffATTY`, `TestScenarioStatusTrailerOnTerminal` |
| `pull -h` and `serve -h` contain the precedence paragraph | `TestHelpTextNamesTheOrder`, `TestReleaseBinaryCommandSurface`, `TestScenarioCLIUsageRefusals` |
| Every doc row above is in the change | this phase's own diff |

## Error coverage

unit-test-only: this phase adds no new failure path. It renders what Phases 2
and 3 resolved, and a refusal already printed its message.

## Implementation notes

### 2026-10-04: phase 4, worker session (agent, clai)

**Deviation 1: the trailer compares the source against one named constant,
and the constant is the source's own wording.** `bucketSource.String()` now
returns `rememberedSource` for `bucketFromRemembered`, and `ReportStatus`
prints its trailer when the source it is given equals that constant. So
there is one word in the code for the remembered source, used by the
refusals, the startup record, the trailer and the tests
(`internal/app/storage.go`, `internal/app/statuslog.go`).

**Deviation 2: the trailer is a whole extra line, not a trailer on the
path line.** The phase said "one dimmed trailer after the path line", and
the path line is the `target` line that the report already builds. Joining
the word to it would change every existing `status` line's width and
column, so the word became its own line below it; the report's other
trailers (`writable:`, `read-only:`) are separate lines for the same
reason, so this follows the existing shape.

**Deviation 3: `TestHelpTextNamesTheOrder` asserts the shared reference
and the four command helps from `internal/cli`.** The phase named
`app.FlagReference` as the place the paragraph lives, but `internal/app`
cannot import `cmd/...` (the dependency runs the other way), so the test
that reads the five helps together belongs to `internal/cli`, which builds
the whole command map. The unit files beside the text assert their own
piece: `internal/app/config_test.go` keeps its `FlagReference` assertions,
and the `-h` process rows prove the same text over the real binary.

**Deviation 4: the phase's five unit tests became four unit tests, one
scenario and one terminal scenario.** `ReportStatus` is a pure renderer
over its arguments, so its rows are unit tests in
`internal/app/statuslog_test.go`; the same behaviour at the command line
is one table of `status` runs and one `log` run in
`TestScenarioStatusNamesTheRememberedSource`
(`internal/integrationtest/scenario_recording_test.go`); and the coloured
side of the trailer needs a character device, so
`TestScenarioStatusTrailerOnTerminal` lives in the Linux colour file
beside the other pseudo-terminal scenarios.

**Surprise: the status report has no source today.** The phase said
`status` "already prints the resolved space through `Runtime.Target()`",
which it does (`space <name>`), but it had no source at all, so the phase
also had to thread `Runtime.SpaceSource()` from `cmd/status` into
`ReportStatus`. That is the only signature change in this phase, and no
MCP envelope changed.

**Commands run, and their results.**

```bash
go build ./...                                  # ok
go test -count=1 ./internal/app/ ./internal/cli/ ./cmd/...   # ok
go test -count=1 ./internal/integrationtest/    # ok
go run mvdan.cc/gofumpt@v0.11.0 -w -l .         # nothing to format after the first run
go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go fix -diff ./...                              # prints nothing
make qa                                         # ok: lint, test, npm-test; coverage 88.9 %; npm 35/35
go run github.com/mibk/dupl@v1.0.0 -t 80 .    # 2 pre-existing clone groups, none in this phase
```

Two of the four `make qa` runs failed in `internal/integrationtest` with
the suite's 30 s package timeout while every package was loaded at once;
the dump is the MCP SDK's transport goroutines parked on a read, not a
failed test, and the load average on that host was near its core count.
The package is green on its own (`go test -count=1`, then `-count=3`,
then `-race -count=3`) and two full runs were green, so this is the
Makefile's documented near-bound budget rather than a defect of this
phase. Phase 5 must still watch it: this effort added spawns to the
package that the whole suite measures at once.

## Review findings

None. Review 2 re-read the report surface (`cmd/status`, `app.Report*`,
`app.FlagReference`) and raised nothing against this phase.