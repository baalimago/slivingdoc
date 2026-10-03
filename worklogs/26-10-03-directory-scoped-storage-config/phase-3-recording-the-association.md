# Phase 3: recording the association

**Status:** Not Started

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
| The recorded file is byte-identical when nothing changed | `TestUnchangedEntryIsNotRewritten` |

## Error coverage

| Row | Failure | Expected outcome | Test |
| --- | --- | --- | --- |
| F23 | the file cannot be written | one warning, the result stands | `TestRecordFailureChangesNoResult` |
| F24 | the file would exceed its bound | the write is refused, the warning names the bound, the result stands | `TestRecordRefusedOverBoundChangesNoResult` |
| F3 | the file cannot be read | one warning naming it, the result stands | `TestUnreadableSettingsFileWarnsAndContinues` |

## Implementation notes

Not started.

## Review findings

None.