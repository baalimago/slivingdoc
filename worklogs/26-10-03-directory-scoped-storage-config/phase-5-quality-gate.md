# Phase 5: the quality gate

**Status:** Complete

README: [README.md](./README.md)

## Goal

Prove the whole effort against the repository's own gate, with no skipped test
and no narrowed run.

## Specification

This phase changes no behavior. It runs the gate, reads its output against the
criteria below, and records the result in this file's implementation notes.

### The sweep

| Step | Command | Passes when |
| --- | --- | --- |
| Format | `go run mvdan.cc/gofumpt@v0.11.0 -l .` | nothing is listed |
| Vet | `go vet ./...` | no output |
| Fix | `go fix -diff ./...` | nothing is printed |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` | no diagnostics |
| Test | `make test` | every package passes at the coverage floor |
| npm | `npm test --prefix npm/slivingdoc` | every check passes |
| Duplication signal | `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | every reported clone carries one of its acceptance clauses, or is fixed |

Tool versions are the repository's pinned ones and are not substituted. There
is no build tag, environment variable or flag that runs a subset.

### What the sweep must also confirm

| Check | Evidence |
| --- | --- |
| Every test name in Phases 1 to 4 exists and passes | the test output |
| Every test name is declared in exactly one phase | this worklog's tables |
| No phase file carries a numeral with a unit outside an oracle row | the checklist's grep over the phase files |
| Every architecture doc row in Phase 4 is present in the tree | the docs' own diff |
| Coverage is at or above the floor | the `make test` output |
| The new package appears in the package map | `AGENTS.md` |

## Integration contract

unit-test-only: the sweep proves the earlier phases at their own boundaries and
adds no event chain of its own.

## Acceptance criteria

| Criterion | Evidence |
| --- | --- |
| Every step above passes | the recorded output |
| Coverage is at or above the floor | `make test` |
| The duplication signal is read, and each clone is either fixed or justified by a clause of the duplication policy | this phase's notes |
| The readiness checklist passes against the tree as it stands | this phase's notes |

## Error coverage

unit-test-only: a failing gate is the failure path, and it is fixed in the
phase that owns the code, never worked around here.

## Implementation notes

### 2026-10-04: the quality gate, worker session (agent, clai)

The sweep is green and the effort is finished. The phase changed no behavior:
it ran every step of its table, read the output against its acceptance
criteria, and corrected the four places where the worklog's own tables had
drifted from the tree. The only code change is a duplicated phrase in one
comment, so the gate was run again after it and passed on the fixed tree.

**Commands run, and their results.** Every command is the repository's own, at
its pinned version, and nothing narrowed the run.

```bash
go run mvdan.cc/gofumpt@v0.11.0 -l .                # nothing listed
go vet ./...                                         # no output
go fix -diff ./...                                   # prints nothing
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...  # no diagnostics
make test                                            # ok: every package, coverage 88.9 % (floor 70 %)
npm test --prefix npm/slivingdoc                     # ok: 35 tests, 35 pass, 0 fail
go run github.com/mibk/dupl@v1.0.0 -t 80 .           # 2 clone groups, both pre-existing
make qa                                              # ok: lint, test, npm-test; coverage 88.9 %
```

`make test` ran four times during this session: once per gate step, once inside
`make qa`, and once more on the fixed tree. The slowest package,
`internal/integrationtest`, finished at 30.1 s of its own test time in the run
that passed, so the Phase 4 note about that package's near-bound budget did not
reproduce as a failure. The gate is the same one the earlier phases ran.

One run did fail, and it was the host rather than the tree. The last `make qa`
on the fixed tree hit the 30 s package timeout in `internal/git`,
`internal/integrationtest` and `internal/notebook` at once, while the load
average on this machine stood near twice its core count; the dump names running
tests of each package and no failure. The run was repeated after the load fell
and passed, which is the same evidence Phase 4 recorded for the same package.
The timeout, the count and the race flag were not touched.

**The duplication signal, read row by row.** Both groups are outside this
effort's code and both carry an acceptance clause of the duplication policy,
so neither is fixed here.

| Clone | Clause | Verdict |
| --- | --- | --- |
| `internal/integrationtest/scenario_conflict_test.go` against `internal/notebook/pull_test.go` | test-setup boilerplate: both are the table-driven file-directory conflict case, one at the command line and one beside the notebook | acceptable, pre-existing |
| `internal/git/merge_test.go` against itself | table-driven test loop: two rows of one table differ only in their index entries | acceptable, pre-existing |

**The sweep's second table, one row at a time.**

| Check | Result |
| --- | --- |
| Every test name in Phases 1 to 4 exists and passes | the 59 declared names that name a test (the one row that names only the `TestScenario` prefix is a command, not a claim); three `go test -run` sets over `internal/settings`, `internal/app`, `internal/cli`, the root package and `internal/integrationtest` report every one PASS, and `make test` runs them under `-race -count=3` |
| Every test name is declared in exactly one phase | three names are declared in two phases, all of them cross-references from a failure-surface row to the proof another phase owns; the checklist's item 2 now says so |
| No phase file carries a numeral with a unit outside an oracle row | the checklist's grep matches three lines, all package timeouts and therefore oracle values: one in `phase-4-operator-surface.md` and two in this file |
| Every architecture doc row in Phase 4 is present in the tree | the diff touches all nine documents it names, plus `overview.md`, `pull.md`, `commit.md`, `compatibility.md` and `security.md` |
| Coverage is at or above the floor | 88.9 % against a floor of 70 % |
| The new package appears in the package map | `AGENTS.md` names `internal/settings` in the runtime layout and the package map |

**Four worklog rows were wrong, and the worklog wins over them.** Each fix
changed a worklog file and no code, because in every case the code was right
and the table was not.

1. Phase 3's deviation 3 named `TestUnchangedEntryIsNotRewritten`, which is
   not a test. The behavior it describes is proved by
   `TestSecondDirectoryReplacesOnlyItsOwnEntry`, which its own note already
   says, and by `TestScenarioPullRecordsTheHostedNotebook`.
2. The README's limits table named two parameters that exist in no file,
   `maxSettingsFileSize` and `minSettingsEntries`. The rows now name
   `settings.maxFileSize`, the bound the code actually reads, and the size
   bound as the only limit on entries, with the two tests that prove each.
3. The README's parameters table had no owner for the association's own
   fields on `ProcessOptions`, and the checklist's citation for shared file
   handling pointed at `internal/credentials/credentials.go`, which Phase 1
   superseded with `privatefile.go` (D14). Both rows now name what the code
   exports.
4. Phase 2's specification named `ProcessOptions.Lookup`, a field the code
   never gained, beside the reader it did. The specification now names
   `NotebookPath` and `Load`, which is what Phase 2's own deviation 1 and the
   README's shared-interface section already said.

**One defect in the tree itself, fixed.** `Runtime.recordsSpace` repeated "the
record itself" in the bullet that lists the sources its switch accepts. The
comment now names each source once, so the same gate was run again on the fixed
tree.

**What Phase 5 deliberately did not do.** It added no test and no gate. Every
scenario the effort claims was already written by the phase that owns it, and
a second copy would be the duplication the policy calls out. The three test
names the tree carries that no phase declares (`TestMissingFileIsEmpty`,
`TestConfigBoundAppliesToTheFile`, and the `TestMain` of the package) are the
parts of the F1 and F24 rows that need no separate phase, and the worklog names
the behaviors rather than every function.

## Review findings

### Review 2 — 2026-10-04 — status `Complete` (one nit)

**R2-03 — nit — this phase's sweep table, the row "No phase file carries a
numeral with a unit outside an oracle row".** The row says the checklist grep
matches one line. It matches three: one in `phase-4-operator-surface.md` and two
in this file. Every match is a package timeout, which is oracle data, so the
checklist holds; only the count in the sentence is stale.

- [x] Correct the count to three and name the matches. Closed by Phase 6.

### Verified good (review 2)

The gate re-ran green with no skip and no narrowed run: `make test` at 88.9 %
coverage against the floor of 70 %, `make lint` clean, `npm test` 35 of 35, and
`dupl -t 80` reporting the same two pre-existing clone groups, both carrying an
acceptance clause of the duplication policy. The three numeral matches are
oracle rows, not violations.