# Phase 3: recording the association

**Status:** Complete

README: [README.md](./README.md)

## Goal

Record the hosted notebook a successful `pull` or `commit` proved the directory
belongs to, so the next bare command in that directory finds it.

## Specification

### Where the write happens

The write is in `internal/app`, not in `internal/notebook`: the notebook knows
the result and nothing about the process configuration, the space source, or
the settings store. `Runtime.Pull` and `Runtime.Commit` hold the key the
operation used and the resolved space beside the resolved source, so both
wrap the service call:

```text
Runtime.Pull   → svc.Pull(key, message-less) → remember(space) → result
Runtime.Commit → svc.Commit(key, message)    → remember(space) → result
```

The recorder is injected into `Runtime` through `ProcessOptions`, so a test
can count calls and fail one without touching the filesystem. It is one
function, `rememberSpace`, and it is called **only** when every condition in the
table below holds.

### The conditions

| Condition | Why | Test |
| --- | --- | --- |
| The operation returned no error | a failed operation proved nothing about the space | `TestRememberedOnlyOnSuccess` |
| The process is hosted | an S3 process never records; a bucket is not a space | `TestS3ModeNeverRecords` |
| The source is `bucketFromRemembered` or a flag or the environment | a space that came from the login's default must not be pinned to a directory, because changing the default must keep working everywhere | `TestDefaultSpaceIsNotRecorded` |
| The space is non-empty | a space that only a token names is not the operator's choice, and D11 says a token ignores the entry; recording it would contradict that | `TestTokenSpaceIsNotRecorded` |
| The command takes a path | `serve` has no notebook path and never records | `TestServeNeverRecords` |
| The key is a path this process resolved | the workspace root when no argument is given, otherwise the cleaned argument | `TestRecordedKeyIsTheResolvedPath` |

### The write

The write is a read-modify-write under the settings lock: load the file, `Put`
the entry, `Save`. A write failure logs one warning naming the key and the
error, and changes no return value (D8, F23). A file that cannot be read is the
same case: the warning names it, and the operation result stands.

### What is recorded

The resolved space and the resolved prefix, never an endpoint and never a
credential (D6, D9). The prefix is recorded because a bare pull that used the
default prefix after a prefixed first pull would silently address a different
notebook in the same space (D12).

### Re-pointing

An existing entry is replaced only by a successful operation against the space
the new resolution produced (D13). There is no merge: `Put` replaces the one
entry whose key is byte-identical and leaves every other entry alone, so a
second directory's successful pull cannot disturb the first. `guardFirstPull`
remains the guard that refuses a re-point into a populated directory for a
non-empty space; Phase 3 does not weaken or widen it, and the invariant is
asserted at the boundary rather than reimplemented here.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `pull` in an empty directory with `--space` | settings store, reference gateway | the space is recorded | one entry written | a credential |
| `pull` in a directory with an entry | settings store | the entry is byte-identical | none | a rewrite |
| `commit` after `pull` | settings store | the entry is byte-identical | none | a rewrite |
| `pull` refused as `DIRECTORY_NOT_EMPTY` | settings store | no entry | none | a partial write |
| `pull` returning `CONTENT_CONFLICT` | settings store | no entry | none | a partial write |
| `commit` before `pull` | settings store | no entry | none | a partial write |
| `pull` in S3 mode | settings store | no entry | none | an empty entry |
| the recorder failing | injected recorder | the operation result stands | one warning | a changed result or exit code |
| two directories pulled in turn | settings store | both entries present | one file rewritten | one entry lost |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| A successful pull records the space and the prefix | `TestRecordsSpaceAfterPull` |
| A successful commit after a pull records nothing new | `TestRecordsSpaceAfterCommit` |
| A refused, conflicted or `PULL_REQUIRED` operation records nothing | `TestRememberedOnlyOnSuccess` |
| An S3 process never records | `TestS3ModeNeverRecords` |
| A space that came from the login's default is never recorded | `TestDefaultSpaceIsNotRecorded` |
| A token's own space is never recorded | `TestTokenSpaceIsNotRecorded` |
| `serve` never records | `TestServeNeverRecords` |
| The recorded key is the workspace root for a bare command and the cleaned argument otherwise | `TestRecordedKeyIsTheResolvedPath` |
| The recorded file holds only the version, the paths, the spaces and the prefixes | `TestRecordedFileHoldsNoCredential` |
| A recorder failure warns and changes no result | `TestRecordFailureChangesNoResult` |
| An existing entry is replaced only by a pull that succeeded against its space | `TestSecondDirectoryReplacesOnlyItsOwnEntry` |
| The recorded file is byte-identical when nothing changed | `TestSecondDirectoryReplacesOnlyItsOwnEntry`, `TestScenarioPullRecordsTheHostedNotebook` |

## Error coverage

| Row | Failure | Expected outcome | Test |
| --- | --- | --- | --- |
| F23 | the file cannot be written | one warning, the result stands | `TestRecordFailureChangesNoResult` |
| F24 | the file would exceed its bound | the write is refused, the warning names the bound, the result stands | `TestRecordRefusedOverBoundChangesNoResult` |
| F3 | the file cannot be read | one warning naming it, the result stands | `TestUnreadableSettingsFileWarnsAndContinues` |

## Implementation notes

### 2026-10-03: phase 3, worker session (agent, clai)

**Deviation 1: the recorder is one `ProcessOptions.Record` field, and the
conditions are a switch on the resolved source.** The phase named the field
`ProcessOptions.Record` implicitly and a condition table; the code keeps both
in `Runtime.recordsSpace` and `Runtime.rememberSpace`
(`internal/app/association.go`). One row of the table needs an explanation the
code had to settle: an explicit choice (flag or variable) beside an existing
record records nothing, because the record says which notebook the directory
already holds and a flag is a choice for this run alone (F15, F16). The table
in the phase only said "the source is a flag or the environment", which would
have replaced a record. That rule is `recordsSpace`'s second case, and
`TestScenarioExplicitChoiceBeatsTheRememberedSpace` proves it.

**Deviation 2: the token's own space is not recorded, and that is D11 read the
other way.** `bucketFromToken` is `kindOther`, so the first table row would
have recorded it. A token names its own space for this run and ignores the
record, so recording it would pin a directory to a token the next command does
not carry. `recordsSpace` therefore records only the remembered source and an
explicit choice over an empty record; `TestTokenSpaceIsNotRecorded` proves it.

**Deviation 3: an unchanged entry is not rewritten.** The phase asked for
`Load`, `Put`, `Save`. `storeEntry` compares the loaded entry with the new one
first and returns without a `Save` when they are equal, so a repeated pull or
commit leaves the file byte for byte (`TestSecondDirectoryReplacesOnlyItsOwnEntry`,
`TestScenarioPullRecordsTheHostedNotebook`). The read-modify-write still
happens under `workspaces.lock`, so a second writer's entry is never lost.

**Deviation 4: a write over the bound and an unreadable file are two branches
of one case.** F24 and F3 both fail inside `storeEntry`, so both rows of the
error coverage table reach the recorder through a process whose reader is nil:
a file this build cannot read refuses the startup that would read it, so
`rigs.runtimeOverAnUnreadableFile` builds that process. Both prove one warning
and an unchanged result.

**Deviation 5: the named acceptance tests became a unit file and a scenario
file.** `internal/app/remember_test.go` holds the unit rows over the injected
recorder and a reference gateway, and
`internal/integrationtest/scenario_recording_test.go` holds the black-box rows
the recorder must also satisfy at the command line: one `pull --space`
followed by four bare commands, the three failing operations, and two
directories recorded in turn. The tables above now name both files where a row
has one proof on each side.

**Test names, as declared.** `TestRecordsSpaceAfterPull`,
`TestRecordsSpaceAfterCommit`, `TestRememberedOnlyOnSuccess`,
`TestS3ModeNeverRecords`, `TestDefaultSpaceIsNotRecorded`,
`TestTokenSpaceIsNotRecorded`, `TestExplicitChoiceRecordsAnEmptyDirectory`,
`TestServeNeverRecords`, `TestRecordedKeyIsTheResolvedPath`,
`TestRecordedFileHoldsNoCredential`, `TestRecordFailureChangesNoResult`,
`TestSecondDirectoryReplacesOnlyItsOwnEntry` (which also records the same
entry twice to prove that an unchanged entry is not rewritten),
`TestRecordRefusedOverBoundChangesNoResult`,
`TestUnreadableSettingsFileWarnsAndContinues`;
`TestScenarioPullRecordsTheHostedNotebook`,
`TestScenarioFailedOperationRecordsNothing`,
`TestScenarioEachDirectoryKeepsItsOwnEntry`.

**Commands run, and their results.**

```bash
go build ./...                                  # ok
go test -count=1 ./internal/app/... ./internal/settings/... ./internal/credentials/... ./cmd/...
                                                # ok, before and after the change
go test -count=1 ./internal/integrationtest/... # ok
go run mvdan.cc/gofumpt@v0.11.0 -w -l .         # one file formatted
go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go fix -diff ./...                              # prints nothing
make qa                                         # ok: lint, test, npm-test
go run github.com/mibk/dupl@v1.0.0 -t 80 .    # 2 pre-existing clone groups, none in this phase
```

`make qa` reported coverage 88.9 % against the floor of 70 %. The two clone
groups are the pre-existing ones (a conflict scenario against a pull unit
test, and two merge rows), both outside the code of this phase.

## Review findings

### Review 2 — 2026-10-04 — status `Complete` (one major doc contradiction, one nit)

**R2-01 — major — `architecture/config.md:110`.** The doc says the recorder's
sources include "the token's own space". `runtime.recordsSpace`
(`internal/app/association.go`) never records it: `bucketFromToken.kind()` is
`kindOther`, so the switch answers `bucketFrom == bucketFromRemembered`, which is
false for a token. Deviation 2 and `TestTokenSpaceIsNotRecorded` say the same
thing, and the code is right. A later agent reading the doc could "fix" the code
to match it and pin a directory to a credential the next command does not carry.

- [x] Remove the token's own space from the recorder's sources in
      `architecture/config.md`. Closed by Phase 6.

**R2-04 — nit — `internal/app/remember_test.go:352`.**
`TestExplicitChoiceLeavesARecordAlone` is named for leaving an existing record
alone, and its comment claims it proves that beside a record a flag changes
nothing, but its body only records into a directory that remembers nothing. The
beside-a-record case is proved by `TestRecordsSpaceAfterCommit` and the
black-box `TestScenarioExplicitChoiceBeatsTheRememberedSpace`, so deviation 1
cites the wrong test for the rule it states.

- [x] Rename the test and its comment to what it proves, and point deviation 1
      at the test that proves the beside-a-record rule. Closed by Phase 6.

### Verified good (review 2)

`recordsSpace` records the record's own source and a flag or variable beside a
directory that remembers nothing; it excludes `bucketFromToken` and
`bucketFromLogin`, and `TestTokenSpaceIsNotRecorded` and
`TestDefaultSpaceIsNotRecorded` pin both. `rememberSpace` runs only after the
operation returned no error and only for a path-taking process, so `serve`
records nothing (`TestServeNeverRecords`) and a refused or conflicted operation
records nothing (`TestRememberedOnlyOnSuccess`, `TestScenarioFailedOperationRecordsNothing`).
`storeEntry` locks, compares and skips an unchanged entry, so a repeated
operation rewrites nothing and a concurrent writer's entry survives
(`TestSecondDirectoryReplacesOnlyItsOwnEntry`,
`TestScenarioEachDirectoryKeepsItsOwnEntry`). A write failure warns at the
`notebook` level and changes neither the result nor the exit code
(`TestRecordFailureChangesNoResult`, `TestRecordRefusedOverBoundChangesNoResult`,
`TestUnreadableSettingsFileWarnsAndContinues`).