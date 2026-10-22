# Phase Five: conversion

**Status:** Complete

README: [README.md](./README.md)

## Goal

Convert plaintext notebooks by publishing an encrypted checkpoint before the encrypted manifest.

## Specification

Implement `encrypt init` for both an empty remote and a notebook with accepted plaintext history. Snapshot the visible tree and build a whole-state checkpoint. Generate the notebook key and recovery door, encrypt the checkpoint, upload it, then publish `current` with the existing conditional-write rules. For an empty notebook, create the encrypted root checkpoint before `current`. Print the recovery code only after acceptance.

After accepted conversion, cleanup old unreferenced packs on a best-effort basis. Do not claim that cleanup erases object versions, backups, or external copies. Preserve the current state if upload, size checks, or conditional publication fail. Do not add a plaintext rollback path.

Conversion capacity checks must allow the checkpoint upload to require up to
twice the configured storage quota. Envelope overhead counts against the
existing ciphertext object limit; refuse an oversized encrypted pack before
publishing `current`.

For a non-empty remote, require an established workspace baseline. If none exists, return `PULL_REQUIRED` before writing. Use the existing merge-and-retry cycle to combine the baseline, visible snapshot, and accepted remote state. On conflict, preserve the existing conflict-marker behavior and do not publish. Reject unresolved markers and apply the configured read-only and writable path policy before publication. A conditional-write loss re-reads and merges; retry exhaustion returns `REMOTE_BUSY` without replacing newer accepted state.

Files owned by this phase:

```text
internal/notebook/encryption.go
internal/notebook/encryption_test.go
internal/integrationtest/main_test.go
internal/integrationtest/cli/encrypt_integrationtest_test.go
```

| Invariant | Mechanism | Proof |
| --- | --- | --- |
| Active encrypted state contains no plaintext note content | Whole-state encrypted checkpoint precedes manifest publication | `TestEncryptInitConvertsHistory` |
| Empty notebooks have an encrypted root checkpoint before `current` | Upload ordering in initialization | `TestEncryptInitEmptyNotebook` |
| Failed conversion does not change accepted state | Upload then conditional manifest publication | `TestEncryptInitFailurePreservesCurrent` |
| Cleanup never changes a successful conversion result | Best-effort cleanup after accepted publication | `TestEncryptInitCleanupFailure` |
| Existing remote state requires a pulled workspace baseline before conversion | Baseline check before upload | `TestEncryptInitRequiresCurrentBaseline` |
| Conversion rejects unresolved markers and protected-path edits | Existing content-marker and composed path policy | `TestEncryptInitRejectsMarkersAndProtectedChanges` |
| Conversion retries a conditional-write loss through the existing merge cycle | Conditional publication and bounded merge retry | `TestEncryptInitContention` |
| A merge conflict does not publish a checkpoint | Existing conflict materialization | `TestEncryptInitMergeConflict` |

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| Black-box init on an empty notebook | Visible workspace and reference store | Encrypted root checkpoint and accepted encrypted `current` | Checkpoint upload before `current` creation | Plaintext pack or manifest |
| Black-box init on plaintext history | Visible workspace, established baseline, and reference store | Merged visible and accepted state represented by an encrypted root checkpoint | Conditional manifest replacement, then best-effort cleanup | Dropped accepted files, protected-path changes, or unresolved markers |
| Black-box init when account capacity is insufficient | Hosted reference gateway | Storage-full refusal and unchanged accepted state | None | Manifest publication or recovery-code success output |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| Empty notebook conversion creates the root checkpoint before `current` | `TestEncryptInitEmptyNotebook` |
| Existing plaintext history converts to the current tree without plaintext in active objects | `TestEncryptInitConvertsHistory` |
| Recovery output follows accepted publication | `TestEncryptInitRecoveryCodeTiming` |
| Conversion refusal preserves accepted `current` and active descriptors | `TestEncryptInitFailurePreservesCurrent` |
| Cleanup failure does not change successful command result | `TestEncryptInitCleanupFailure` |
| The conversion observes the configured headroom budget | `TestEncryptInitCapacityBoundary` |
| A non-empty remote requires an established local baseline before conversion | `TestEncryptInitRequiresCurrentBaseline` |
| Protected-path edits and unresolved markers block conversion | `TestEncryptInitRejectsMarkersAndProtectedChanges` |
| Concurrent non-conflicting changes merge before checkpoint publication | `TestEncryptInitContention` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| Store refuses checkpoint upload for capacity | `STORAGE_FAILURE` / `STORAGE_FULL`; `current` unchanged | `TestEncryptInitFailurePreservesCurrent` |
| Ciphertext exceeds the hosted pack limit | `STORAGE_FAILURE` / `OBJECT_TOO_LARGE`; `current` unchanged | `TestEncryptInitPackLimit` |
| Conditional publication loses contention until retry exhaustion | `REMOTE_BUSY` / `RETRIES_EXHAUSTED`; newer state remains accepted | `TestEncryptInitContention` |
| Local and remote changes conflict | `CONTENT_CONFLICT` / `MERGE_CONFLICT`; no checkpoint publication | `TestEncryptInitMergeConflict` |
| Cleanup fails after accepted publication | Success remains success with a cleanup warning | `TestEncryptInitCleanupFailure` |
| Local snapshot or encryption fails before publication | Error with no accepted-state change | `TestEncryptInitFailurePreservesCurrent` |
| Workspace baseline is absent for a non-empty remote | `INVALID_REQUEST` / `PULL_REQUIRED` before upload | `TestEncryptInitRequiresCurrentBaseline` |
| Conversion would publish unresolved markers or protected-path edits | `INVALID_REQUEST` / `UNRESOLVED_MARKERS` or `READ_ONLY_PATH`; protected paths restore from baseline | `TestEncryptInitRejectsMarkersAndProtectedChanges` |

## Implementation notes

The conversion operations live in `internal/notebook/encryption.go`. `EncryptInit`
and `EncryptRekey` both delegate to `publishRoot`, which owns the whole
conversion: it snapshots the visible tree, rejects unresolved markers
(`UNRESOLVED_MARKERS`) and protected-path edits through the composed path
policy, merges the baseline, the visible tree and the accepted remote state
through the existing cycle, and on conflict materializes markers without
publishing. `buildRootProposal` creates the root commit, exports the
state-complete checkpoint, records the shallow boundary and builds the root
manifest with empty increments and retained generations. `uploadEncrypted`
seals the checkpoint pack under the new key and stores it before
`publishEncrypted` conditionally creates or replaces `current`; a lost
conditional write re-reads and merges within the bounded retry, and an
exhausted budget is `REMOTE_BUSY` / `RETRIES_EXHAUSTED`. An accepted root
publication accepts the baseline, marks the workspace pulled, and runs
`cleanupCurrent` best-effort, which re-reads and decrypts the accepted
`current` before every delete batch and never changes the result.
`rootRemote` refuses `init` on an already encrypted notebook before any write
(`ENCRYPTION_ALREADY_ENABLED`) and refuses a non-empty plaintext remote this
workspace has not pulled (`PULL_REQUIRED`).

The notebook key and the recovery door stay in `internal/app` (Phase 4): the
command mints them and passes the unlocked sealer and the door list in
`Cipher`, so the notebook layer never generates key material and never
repeats a cryptographic operation.

The capacity headroom needs no notebook change. The hosted gateway already
lets a checkpoint pack take the space up to twice its quota, and the
conversion uploads its whole-state pack as `KindCheckpoint`, so the rule
applies to conversion exactly as it does to a compacting commit. The
`internal/notebook/encryption_test.go` conversion units cover the eight
error rows: capacity refusal, pack limit, contention, merge conflict, cleanup
failure, pre-publication failure, missing baseline and marker or
protected-path refusal.

The black-box conversion scenarios are
`internal/integrationtest/cli/encrypt_integrationtest_test.go`, which spawn the
real one-shot CLI process over the hosted reference gateway:
`TestEncryptInitRecoveryCodeTiming` and `TestEncryptInitCapacityBoundary`. This
is the spawned-process model Phase 4 deviated from for its command scenarios
(D16); the two limits that drove that choice do not apply here. The hosted
store is external, so consecutive helpers observe one notebook, and
`main_test.go` gains the test-only `helperKDFEnv` knob, which makes a helper
write and accept the cheap Argon2id cost while every other helper keeps
`crypt.DefaultKDFParams`. `sanitizedEnv` now also drops
`SLIVINGDOC_PASSPHRASE` and `SLIVINGDOC_PASSPHRASE_FILE`, so a developer's
secret can never reach a helper.

## Review findings

None. The Phase 4 finding that the process never constructs
`storage.Encrypted` for `serve`, `pull` and `commit` is unchanged and was
already recorded there; no phase of this effort owns it.

## Commands and results

```text
go test ./internal/notebook/ -run 'TestEncryptInit|TestEncryptRekey|TestEncryptStatus|TestEncryptDoors' -count=1 -timeout=60s
ok  github.com/baalimago/slivingdoc/internal/notebook  0.056s

go test -race -count=3 -timeout=120s ./internal/integrationtest/ -run 'TestEncryptInit'
ok  github.com/baalimago/slivingdoc/internal/integrationtest  5.293s

go test -race -count=3 -timeout=60s ./internal/notebook/ -run 'TestEncryptInit'
ok  github.com/baalimago/slivingdoc/internal/notebook  1.204s

go test -race -count=3 -timeout=30s ./internal/integrationtest/
ok  github.com/baalimago/slivingdoc/internal/integrationtest  22.168s

make qa
== gofumpt == / == go vet == / == staticcheck == / == go fix ==  (clean)
== coverage: 87.1% (floor 70%) ==
npm test: tests 35, pass 35, fail 0
```

The `go run github.com/mibk/dupl@v1.0.0 -t 80 .` signal reports the five
pre-existing clone groups and none of this phase's files.

The capacity scenario measures the headroom on the reference gateway: after
two plaintext generations the space holds 7626 bytes, and the whole-state
checkpoint the conversion uploads is 2709 bytes. At a quota of exactly 7626
bytes the normal limit refuses any pack write (`7626 + 2709 > 7626`), while the
checkpoint limit accepts the conversion (`7626 + 2709 <= 2*7626`); a quota of 1
byte refuses it as `STORAGE_FAILURE` / `STORAGE_FULL` with the stored bytes
unchanged.
