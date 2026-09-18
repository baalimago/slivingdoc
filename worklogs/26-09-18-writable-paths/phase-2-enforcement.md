# Phase 2 — Enforcement over the policy

**Status:** Not Started

[← README](README.md)

## Goal

Make `Notebook.Pull` and `Notebook.Commit` enforce the policy instead of the
read-only set, so a protected path is refused on commit and restored on pull
whichever set protects it, and so neither operation reads baseline file content
unless a protected path actually changed.

## Specification

### What the two operations hold

`Notebook` holds a `git.PathPolicy` in place of its read-only set, built once at
construction from the two configured entry sets. A construction error — an
invalid entry or a path named by both sets — fails `Notebook` construction, which
fails `Service` construction, which fails startup. Phase 3 gives that failure its
operator-facing message; this phase only guarantees it propagates rather than
being swallowed.

`Notebook` and `Service` each keep their existing read-only entry accessor
unchanged and gain a writable entry accessor beside it. Both return copies of the
normalized, sorted entries and are never nil. Phase 4 is their only consumer.

### Commit

The existing enforcement step keeps its position in the sequence — last, after
the message, the pulled marker, the snapshot, and conflict markers — and keeps
its recovery behavior exactly. Only how it learns what changed is new. It now
also receives the local tree the commit has already built, so no tree is built
twice.

The sequence is: short-circuit on an unconfigured policy; ask the policy which
protected paths differ between the local tree and the baseline tree; return
without refusing when none do; otherwise restore exactly those paths from the
baseline, build the restored tree, apply it through the existing local-mutation
mechanism, and return the refusal.

| Invariant | Mechanism | Test |
| --- | --- | --- |
| A commit from a configured process never publishes a change to a protected path | The refusal returns before any proposal is built | `TestIntegration_Writable_CommitRefusesOutsideWritable` |
| The refused call never returns success and never changes remote state | The refusal path is the existing one, which returns before the CAS | `TestIntegration_Writable_RefusalLeavesGenerationUnchanged` |
| The touched protected files are reset to the baseline | Restore, build, and the existing local-mutation apply | `TestIntegration_Writable_RefusalResetsProtectedFiles` |
| The agent's edits to writable paths stay on disk after a refusal | Restore touches only the named paths | `TestIntegration_Writable_RefusalKeepsWritableEdits` |
| A failure during the reset is a recovery failure with the existing stage | The apply is the existing call with the existing stage | `TestCommit_WritePolicyResetRecoveryFailure` |
| A commit with no protected change reads no baseline blob | Detection reads tree objects only; restore is not called | `TestCommit_CleanCommitReadsNoBaselineBlob` |
| A commit with no protected change is byte-for-byte what it is today | The short-circuit returns before any new work | `TestIntegration_Writable_CleanCommitUnchanged` |

### Pull

Pull pins protected paths to the baseline before the merge so the merge takes the
remote side there. Today it pins unconditionally, because reading the baseline
content is the only way it can learn what to pin. With detection it pins only
when a protected path actually differs: when none does, the pinned tree would
equal the local tree, so the merge runs against the local tree exactly as an
unconfigured process does.

| Invariant | Mechanism | Test |
| --- | --- | --- |
| A local edit to a protected path is discarded and the remote side taken | The pinned tree replaces the local side of the merge | `TestIntegration_Writable_PullRestoresProtectedPath` |
| A local edit to a writable path survives the pull and merges normally | Restore touches only the named paths | `TestIntegration_Writable_PullKeepsWritableEdits` |
| The restore is visible in the ordinary diffstat | The diffstat is raw local against merged, as today | `TestIntegration_Writable_PullRestoreAppearsInDiffstat` |
| A pull with no protected change reads no baseline blob | Detection reads tree objects only; restore is not called | `TestPull_CleanPullReadsNoBaselineBlob` |
| A pull with no protected change builds no extra tree | The pin is skipped, so the merge tree is the local tree | `TestPull_CleanPullBuildsNoExtraTree` |
| An invalid file under a protected path is refused before the restore runs | The existing content precondition is unmoved | `TestIntegration_Writable_PullInvalidContentUnderProtected` |

### The refusal

The refusal keeps the existing category, the existing reason token, and one file
entry per changed path carrying the existing per-file reason. Inventing a
parallel vocabulary would force every consumer to learn a second way to branch on
the same event, and the error taxonomy in `AGENTS.md` treats those tokens as
stable API.

Only the message text is new, and only when there is a writable set to name. An
agent under a default-protected policy cannot act on a list of protected paths,
because that list is nearly the whole notebook; it can act on the short list of
places it may write.

| Configuration | Message names | Test |
| --- | --- | --- |
| Writable set non-empty | The writable entries, as where the agent may write, plus that its changes elsewhere were discarded and reset | `TestWritePolicyRefusal_NamesWritableEntries` |
| Writable set empty | The violated read-only entries, in today's wording, byte-for-byte | `TestWritePolicyRefusal_ReadOnlyWordingUnchanged` |
| Both sets non-empty | The writable entries, since those remain the actionable list | `TestWritePolicyRefusal_ComposedNamesWritable` |

The file entries list the paths the agent actually changed in both cases, so the
refusal always says which of its edits were undone.

### Configuration fields

This phase introduces the fields that carry the writable set inward, and the
harness field that lets the scenarios above run before the CLI flag exists. Each
is a row in the README parameters table with this phase as its owner. Phase 3
introduces the flag that resolves into the outermost of them.

### Files

- `internal/notebook/notebook.go` — hold the policy; add the writable entry
  accessor; carry the new config field.
- `internal/notebook/commit.go` — the enforcement step over the policy, the new
  local-tree argument, and the refusal message branch.
- `internal/notebook/pull.go` — the conditional pin.
- `internal/app/service.go` — carry the config field through to the notebook; add
  the writable entry accessor.
- `internal/integrationtest/harness.go` — the harness config field.
- `internal/integrationtest/scenario_writable_test.go` — new: the scenarios below.
- `internal/notebook/commit_test.go`, `internal/notebook/pull_test.go` — the
  counted-call and recovery unit tests.

## Integration contract

Black-box MCP scenarios through the harness, which configures both sets directly.
The existing read-only scenarios are the regression guard and must pass unedited:
a read-only-only configuration is one policy shape among several, and this phase
changes none of its behavior.

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| Writable set configured; agent edits a file inside it; commit | Real engine, fake store | Success, generation advanced | The edit is published | None |
| Writable set configured; agent edits a file in a sibling directory; commit | Real engine, fake store | Refusal with the existing category and reason, the changed path listed, the message naming the writable entries | The sibling file is reset to baseline content | Remote generation unchanged; no pack uploaded |
| Writable set configured; agent creates a file at the notebook root; commit | Real engine, fake store | Refusal listing that path | The file is removed, since the baseline does not hold it | Remote generation unchanged |
| Writable set configured; agent creates a directory that did not exist at startup; commit | Real engine, fake store | Refusal listing the path under it | The created file is removed | Remote generation unchanged |
| Read-only entry with a writable entry below it; agent edits under the writable entry; commit | Real engine, fake store | Success, generation advanced | The edit is published | None |
| Read-only entry with a writable entry below it; agent edits a sibling under the read-only entry; commit | Real engine, fake store | Refusal listing the sibling path | The sibling is reset | Remote generation unchanged |
| Writable entry with a read-only entry below it; agent edits the read-only file; commit | Real engine, fake store | Refusal listing that path | The file is reset | Remote generation unchanged; the agent's other edits under the writable entry stay on disk |
| Writable set configured; a protected path is edited locally; pull | Real engine, fake store | Success; the protected path holds the remote content | The restore appears in the diffstat | The writable edits are not touched |
| Neither set configured; commit and pull | Real engine, fake store | Identical to the current behavior | None beyond today's | No policy work is performed |
| Read-only set only, no writable set; the existing read-only scenarios | Real engine, fake store | Unchanged, assertions unedited | Unchanged | Unchanged |

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| A configured process refuses and resets a commit that changed a protected path, whichever set protects it | the commit invariant table |
| A configured process restores a protected path on pull and leaves writable edits alone | the pull invariant table |
| The refusal names where the agent may write when a writable set is configured | `TestWritePolicyRefusal_NamesWritableEntries` |
| The refusal wording for a read-only-only configuration is unchanged | `TestWritePolicyRefusal_ReadOnlyWordingUnchanged` |
| The refusal keeps the existing category, reason token, and per-file reason | `TestWritePolicyRefusal_TokensUnchanged` |
| An operation with no protected change reads no baseline blob | `TestCommit_CleanCommitReadsNoBaselineBlob`, `TestPull_CleanPullReadsNoBaselineBlob` |
| Paths that did not exist when the process started are protected by the unmatched default | the root and new-directory scenarios |
| Recovery semantics during the reset are unchanged | `TestCommit_WritePolicyResetRecoveryFailure` |
| A policy construction error fails startup rather than being swallowed | `TestService_PolicyConstructionFailsSetup` |
| Every existing read-only scenario passes unedited | `make test` over `internal/integrationtest/scenario_readonly_test.go` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| Detection fails while reading a tree | Storage integrity with the existing engine-failure reason; no local mutation has begun, so the call is not a recovery failure | `TestCommit_DetectionFailureIsIntegrity` |
| Restore fails while reading a blob | Storage integrity with the existing engine-failure reason; no local mutation has begun | `TestCommit_RestoreReadFailureIsIntegrity` |
| The restored snapshot cannot be represented as notebook state | Invalid request with the existing invalid-content reason, as the current path returns | `TestCommit_RestoredSnapshotInvalid` |
| The local mutation of the reset fails | Recovery failure with the existing commit-reset stage, and the call never returns success | `TestCommit_WritePolicyResetRecoveryFailure` |
| A protected path holds a file that violates the content rules on pull | The existing invalid-content refusal names the file before the restore runs | `TestIntegration_Writable_PullInvalidContentUnderProtected` |
| The policy cannot be constructed from the configured sets | `Notebook` construction fails, `Service` construction fails, and the error names the offending entry or the overlapping path | `TestService_PolicyConstructionFailsSetup` |
| A commit changes a protected path while another changes a writable one | Refusal listing only the protected path; the writable edit stays on disk and is not published | `TestIntegration_Writable_RefusalKeepsWritableEdits` |

## Implementation notes

Not started.

## Review findings

None.
