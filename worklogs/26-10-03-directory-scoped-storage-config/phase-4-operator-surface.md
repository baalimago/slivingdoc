# Phase 4: the operator surface and the docs

**Status:** Not Started

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
| `status` names the remembered source when the association selected the space | `TestStatusNamesRememberedSpaceSource` |
| `status` prints no trailer when a flag or the environment selected it | `TestStatusOmitsSourceForExplicitChoices` |
| `log` prints no trailer in any case | `TestLogNeverNamesTheAssociation` |
| The trailer is absent when stdout is not a terminal | `TestStatusTrailerIsPlainOffATTY` |
| `pull -h` and `serve -h` contain the precedence paragraph | `TestHelpTextNamesTheOrder` |
| Every doc row above is in the change | this phase's own diff |

## Error coverage

unit-test-only: this phase adds no new failure path. It renders what Phases 2
and 3 resolved, and a refusal already printed its message.

## Implementation notes

Not started.

## Review findings

None.