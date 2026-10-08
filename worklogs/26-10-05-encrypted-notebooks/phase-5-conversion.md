# Phase Five: conversion

**Status:** Not Started

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
internal/integrationtest/scenario_encrypt_conversion_test.go
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

Not started.

## Review findings

None.