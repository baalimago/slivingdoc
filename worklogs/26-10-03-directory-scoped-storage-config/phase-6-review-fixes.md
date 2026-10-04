# Phase 6: review 2 fixes

**Status:** Complete

README: [README.md](./README.md)

## Goal

Close the five findings review 2 filed against the worklog's own prose and one
comment, so the recorded contract matches the code.

## Specification

The fixes change no behavior. Four are worklog and architecture prose; the fifth
is a test name and its comment.

| Finding | File | Fix |
| --- | --- | --- |
| R2-01 | `architecture/config.md` | The recorder's sources are a flag, a variable and the record itself; the token's own space and the login's default are never recorded, and the paragraph says why |
| R2-02 | `README.md`, failure surface F14, F15 and F16 | F14 loses the `spaceMismatch` claim and says the record is never read; F15 and F16 name `recordsSpace` and `applyRememberedPrefix` rather than claiming the entry is unused |
| R2-03 | `phase-5-quality-gate.md` | The numeral check names three matches, one in `phase-4-operator-surface.md` and two in the file itself, all package timeouts, so no unit numeral appears outside an oracle row |
| R2-04 | `internal/app/remember_test.go`, `phase-3-recording-the-association.md` | The misnamed test is renamed `TestExplicitChoiceRecordsAnEmptyDirectory`, its comment states what it proves, and deviation 1 cites `TestScenarioExplicitChoiceBeatsTheRememberedSpace` |
| R2-05 | `internal/credentials/platform_other.go` | The comment names `ChecksOwners` |

The recording and using conditions are stated once, in `runtime.recordsSpace`
and `config.applyRememberedPrefix`. The README and `architecture/config.md` name
what those two functions do; they do not restate the conditions in their own
words.

## Integration contract

unit-test-only: the fixes change no behavior and add no event chain, so the
existing scenario suite is the contract.

## Acceptance criteria

| Criterion | Evidence |
| --- | --- |
| Every finding's edit is in place | the diff |
| No behavior changes | `make test` |
| The lint gate stays clean | `make lint` |
| The effort's gate is green | `make qa` |

## Error coverage

unit-test-only: the fixes open no failure path.

## Implementation notes

### 2026-10-04: review 2 fixes, worker session (agent, clai)

All five findings are closed by the edits in the specification table.

**R2-01.** `architecture/config.md` now lists a flag, a variable and the record
itself as the recorder's sources, and names the login's default and the token's
own space as never recorded, with the reason for each. The code
(`Runtime.recordsSpace`) is unchanged; it was already right.

**R2-02.** F14's token column is `none` and its behavior says the record is
never read. F15 and F16 name `applyRememberedPrefix` for the prefix and
`recordsSpace` for the recording rule, and keep that the entry is never
rewritten. The code is unchanged.

**R2-03.** The sweep row now says the checklist grep matches three lines, one in
`phase-4-operator-surface.md` and two in `phase-5-quality-gate.md`, all package
timeouts. This wording carries no unit numeral, so the grep still matches only
those three oracle lines.

**R2-04.** `TestExplicitChoiceLeavesARecordAlone` became
`TestExplicitChoiceRecordsAnEmptyDirectory`, with a comment that names the
recording half of F15 and F16 and points at
`TestScenarioExplicitChoiceBeatsTheRememberedSpace` for the beside-a-record
half. Phase 3's deviation 1 now cites that scenario.

**R2-05.** The comment in `internal/credentials/platform_other.go` names
`ChecksOwners`.

**Commands run, and their results.**

```bash
make lint      # clean: gofumpt, go vet, staticcheck, go fix
make test      # ok: every package, coverage 88.9 % (floor 70 %)
make qa        # exit 0: lint, test, npm-test; npm 35 of 35 pass
```

The gate is the one Phases 1 to 5 ran, at the same flags. No test was skipped,
and the coverage is unchanged from the pre-fix run.
