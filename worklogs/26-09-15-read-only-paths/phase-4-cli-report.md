# Phase 4 — CLI report

**Status:** Not Started

**README:** [README.md](README.md)

## Goal

The `pull` and `commit` subcommands render the README's CLI report
skeleton: code and reason on the status line, per-file reasons in an
aligned column, the next step, and the read-only trailer, plain and
coloured.

## Specification

### Rendering (`internal/app/command.go`, `internal/app/colour.go`)

- `Report` gains a `readOnly []string` argument; `cmd/pull` and
  `cmd/commit` pass `Runtime.ReadOnlyPaths()`. `Report` sets the
  envelope's `ReadOnly` before rendering so the CLI and MCP paths agree.
- `writeSuccess` is unchanged except for the trailer line `read-only:
<entries>` after the totals line when the set is non-empty, entries joined
  by the README's read-only list separator.
- `writeError` renders, in order:
  - the status line: the code, the README's CLI status separator, the
    reason token;
  - the message on its own line;
  - one line per file: two-space indent, the path padded with spaces to the
    longest path in the report plus two, the file reason as the lower-case
    words the README lists, and, when ranges exist, two more spaces and
    `lines ` followed by `start-end` pairs joined by a comma and a space;
  - `next: ` and the README's CLI wording for the action;
  - `retryable: ` and the flag;
  - `recovery: ` as today, only for `RECOVERY_FAILURE`;
  - `read-only: <entries>` only when the set is non-empty.
- `painter` gains `dim` using the README's dim colour code. Colour map:
  code red, reason dim, path yellow, file reason dim, `next:` cyan,
  `read-only:` dim. With colour off every line is byte-identical to the
  plain form.
- The file reason word table and the action wording table are two package
  maps; a token missing from a map renders the raw token, never a panic.

### Contract and documents

- `docs/slivingdoc-v1.md`: the CLI report paragraph of the product-contract
  section describes the skeleton and shows the read-only refusal example
  from the README.
- `docs/running.md`: the report examples are replaced with the new plain
  output, including one read-only refusal.

### Invariant: colour is presentation-only

| Bound actor                 | Mechanism                                   | Test                                                                   |
| --------------------------- | ------------------------------------------- | ---------------------------------------------------------------------- |
| piped stdout                | `colourEnabled` false                       | `TestReport` (extended)                                                |
| `NO_COLOR` on a terminal    | `colourEnabled` false                       | `TestScenarioCLIColourOnTerminal` (extended)                           |
| terminal without `NO_COLOR` | painter on; stripped bytes equal plain form | `TestWriteErrorColoured` (extended), `TestScenarioCLIColourOnTerminal` |

## Integration contract

R starts with `docs/faq.md` = `Q: a\nA: 1\n` and `notes/a.md` = `x\n`.
Every CLI process below runs with `--read-only-paths docs` unless the row
says otherwise, against the fake backend.

| Trigger                                                                 | Collaborators or fakes | Observable result (exact plain stdout)                                                                                                                                                                              | Required side effects                                | Prohibited side effects    |
| ----------------------------------------------------------------------- | ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- | -------------------------- |
| `pull` into a fresh directory                                           | spawned process        | `OK  generation 1  <path>` then `  docs/faq.md  +2` then `  notes/a.md  +1` then `2 files changed, 3 insertions(+), 0 deletions(-)` then `read-only: docs`; exit 0                                                  | files written                                        | any error line             |
| edit `docs/faq.md` to `A: 2\n` and `notes/a.md` to `y\n`; `commit -m m` | spawned process        | `INVALID_REQUEST · READ_ONLY_PATH` then the README refusal message for `docs` then `  docs/faq.md  read-only` then `next: edit the files, then commit` then `retryable: false` then `read-only: docs`; exit nonzero | `docs/faq.md` = `Q: a\nA: 1\n`; `notes/a.md` = `y\n` | manifest change            |
| `commit -m m` again                                                     | spawned process        | `OK  generation 2  <path>` then `  notes/a.md  +1 -1` then `1 files changed, 1 insertions(+), 1 deletions(-)` then `read-only: docs`; exit 0                                                                        | manifest generation 2                                | any docs change in R       |
| `commit -m m` before any pull, no flag                                  | spawned process        | `INVALID_REQUEST · PULL_REQUIRED` then the message then `next: pull, then continue` then `retryable: false`; exit nonzero                                                                                           | none                                                 | a `read-only:` line        |
| two directories edit line 1 of `notes/a.md`; second `commit`, no flag   | spawned process        | `CONTENT_CONFLICT · MERGE_CONFLICT` then the message then `  notes/a.md  conflict  lines 1-5` then `next: edit the files, then commit` then `retryable: false`; exit nonzero                                        | markers in `notes/a.md`                              | a `read-only:` line        |
| the refusal above on a pseudo-terminal without `NO_COLOR`               | PTY helper             | the same lines with the code red, reason dim, path yellow, file reason dim, `next:` cyan, `read-only:` dim; stripping escapes yields the plain bytes                                                                | none                                                 | colour with `NO_COLOR` set |

## Acceptance criteria

| Outcome                                                      | Evidence                                                                                                                                                                                                                    |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Status line, message, aligned file rows, trailer, in order   | `TestReport` (extended fixtures), `TestWriteErrorReadOnly`, `TestWriteErrorAlignsPathColumn`                                                                                                                                |
| Read-only trailer on success and error only when non-empty   | `TestWriteSuccessReadOnlyTrailer`, `TestWriteErrorReadOnly`, `TestWriteSuccessEmptyStat` (unchanged)                                                                                                                        |
| File reason words and action wording match the README tables | `TestFileReasonWords`, `TestActionWording`                                                                                                                                                                                  |
| Coloured output strips to the plain bytes                    | `TestWriteErrorColoured` (extended), `TestWriteSuccessColoured` (extended)                                                                                                                                                  |
| Subcommands pass the runtime set and print the report        | `TestCommitReportsReadOnlyRefusal`, `TestCommitReportsMarkerConflict` (fixture updated), `TestPullPrintsReport` (fixture updated)                                                                                           |
| Black-box byte-exact reports                                 | `TestScenarioCLIReadOnlyCommit`, `TestScenarioCLIMarkerConflictReport` (updated), `TestScenarioCLICommitBeforePull` (updated), `TestScenarioCLISharedRemoteConflict` (updated), `TestScenarioCLIColourOnTerminal` (updated) |
| Contract and running docs show the new report                | reviewer reads the CLI report paragraph and `docs/running.md` against `TestScenarioCLIReadOnlyCommit`                                                                                                                       |

## Error coverage

| Failure                         | Expected outcome                                    | Test                           |
| ------------------------------- | --------------------------------------------------- | ------------------------------ |
| unknown file reason token       | raw token rendered, no panic                        | `TestFileReasonWords`          |
| unknown action token            | raw token rendered, no panic                        | `TestActionWording`            |
| error with no files             | no file lines; trailer follows the message directly | `TestReport`                   |
| non-domain error (cancellation) | returned unchanged, nothing printed                 | `TestReport` (existing branch) |
| `RECOVERY_FAILURE`              | `recovery:` line before `read-only:`                | `TestReport`                   |

## Implementation notes

Not started.

## Review findings

None.
