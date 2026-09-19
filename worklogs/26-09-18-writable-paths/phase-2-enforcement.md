# Phase 2 — Enforcement over the policy

**Status:** Complete

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

`Notebook`, `Service`, and the `Runtime` wrapper that forwards to the service
each keep their existing read-only entry accessor unchanged and gain a writable
entry accessor beside it. All three return copies of the normalized, sorted
entries and are never nil. This phase owns all three, including the `Runtime`
wrapper, so Phase 4 finds the accessor already present on every type it reads;
the existing `Runtime` accessor is asserted by `internal/app/readonly_test.go`
and the new one is asserted beside it.

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

The two counted-call rows below measure a **difference**, not an absolute. Pull
already reads baseline content for a reason that predates this work: its
three-way merge takes `n.ws.Baseline().Tree` as the base side, and commit merges
and diffs against the remote tree, which shares objects with it. So an assertion
of the form "this operation read no baseline blob" would hold only against a fake
whose merge is scripted, and would silently stop meaning anything the moment the
seam moved.
Each test therefore runs the same fixture twice, once with the policy configured
and once without, and asserts the blob-read counts are equal. That oracle is true
of the real engine as well as of the fake, and it is exactly README invariant 6.

| Invariant | Mechanism | Test |
| --- | --- | --- |
| A commit from a configured process never publishes a change to a protected path, at any nesting depth of the two sets | The refusal returns before any proposal is built, over the paths resolution answers for from the entries the operator wrote | `TestScenarioWritableCommitRefusesOutsideWritable`, `TestScenarioWritableThreeLevelCommitRefusesNestedProtected` |
| The refused call never returns success and never changes remote state | The refusal path is the existing one, which returns before the CAS | `TestScenarioWritableRefusalLeavesGenerationUnchanged` |
| The touched protected files are reset to the baseline | Restore, build, and the existing local-mutation apply | `TestScenarioWritableRefusalResetsProtectedFiles` |
| The agent's edits to writable paths stay on disk after a refusal, with one structural exception: where the baseline holds a file at a path the visible tree replaced with a directory, the accepted file comes back and every local file under that directory goes with it, writable ones included | Restore touches only the named paths; a snapshot cannot hold a blob and a tree at one path, so the reset's local mutation fails there and the recovery resynchronizes the visible directory to the accepted state | `TestScenarioWritableRefusalKeepsWritableEdits`, `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob` |
| A failure during the reset is a recovery failure with the existing stage | The apply is the existing call with the existing stage | `TestCommitWritePolicyResetRecoveryFailure` |
| A commit with no protected change reads no blob on the policy's account | Detection reads tree objects only and restore is not called, so the fake's blob-read count equals the count for the identical commit with no policy configured | `TestCommitCleanCommitPolicyAddsNoBlobRead` |
| A commit with no protected change is byte-for-byte what it is today | The short-circuit returns before any new work | `TestScenarioWritableCleanCommitUnchanged` |

### Pull

Pull pins protected paths to the baseline before the merge so the merge takes the
remote side there. Today it pins unconditionally, because reading the baseline
content is the only way it can learn what to pin. With detection it pins only
when a protected path actually differs: when none does, the pinned tree would
equal the local tree, so the merge runs against the local tree exactly as an
unconfigured process does.

| Invariant | Mechanism | Test |
| --- | --- | --- |
| A local edit to a protected path is discarded and the remote side taken, at any nesting depth of the two sets | The pinned tree replaces the local side of the merge | `TestScenarioWritablePullRestoresProtectedPath`, `TestScenarioWritableThreeLevelPullRestoresNestedProtected` |
| A local edit to a writable path survives the pull and merges normally | Restore touches only the named paths | `TestScenarioWritablePullKeepsWritableEdits` |
| The restore is visible in the ordinary diffstat | The diffstat is raw local against merged, as today | `TestScenarioWritablePullRestoreAppearsInDiffstat` |
| A pull with no protected change reads no blob on the policy's account | Detection reads tree objects only and restore is not called, so the fake's blob-read count equals the count for the identical pull with no policy configured | `TestPullCleanPullPolicyAddsNoBlobRead` |
| A pull with no protected change builds no extra tree | The pin is skipped, so the merge tree is the local tree | `TestPullCleanPullBuildsNoExtraTree` |
| An invalid file under a protected path is refused before the restore runs | The existing content precondition is unmoved | `TestScenarioWritablePullInvalidContentUnderProtected` |

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
| Writable set non-empty | The writable entries, as where the agent may write, plus that its changes elsewhere were discarded and reset | `TestWritePolicyRefusalNamesWritableEntries` |
| Writable set empty | The violated read-only entries, in today's wording, byte-for-byte | `TestWritePolicyRefusalReadOnlyWordingUnchanged` |
| Both sets non-empty | The writable entries, since those remain the actionable list | `TestWritePolicyRefusalComposedNamesWritable` |
| Both sets non-empty, with a read-only entry a writable entry splits from its own-set ancestor | The writable entries, so the entry set the notebook retains for the read-only wording — collapsed within its own set — is never the one that answers | `TestReadOnlyRefusalSetMatchesPolicyEntries` |

The file entries list the paths the agent actually changed in both cases, so the
refusal always says which of its edits were undone.

That list is unbounded, and this phase leaves it unbounded deliberately. Under a
default-protected policy every changed path outside the writable region is
protected, so one stray recursive copy produces one file entry per copied file.
Truncating it would make the refusal lie about which edits were reset, which
matters more than the size of a message an agent reads once; the read-only
refusal has the same shape today, and capping both is a separate decision.

### Configuration fields

This phase introduces the fields that carry the writable set inward, and the
harness field that lets the scenarios above run before the CLI flag exists. Each
is a row in the README parameters table with this phase as its owner. Phase 3
introduces the flag that resolves into the outermost of them.

Both fields are `[]string`, and every layer that holds them builds its own
policy from them: the resolved process configuration, `ServiceConfig`, and
`notebook.Config` each carry the **normalized** entries, so the policy is
constructed three times down one startup (review 2, R2-03). That is safe
because the collapse keeps every entry resolution needs, which makes the pair
of entry accessors a lossless input to `NewPolicy` and every rebuild a fixed
point: no construction is load-bearing, and a layer configured from another
layer's accessors protects exactly what the entries the operator wrote
protect. The property is stated on `PathPolicy.ReadOnly` and at each rebuild
site, and pinned at this phase's own boundary by
`TestServiceNestedEntriesSurviveRebuild`. The black-box harness is the one
caller that feeds raw written entries straight into `ServiceConfig`, so a
configuration reaches the service unnormalized there and already normalized
under the CLI; the fixed point is what makes those two indistinguishable.

### Files

- `internal/notebook/notebook.go` — hold the policy; add the writable entry
  accessor; carry the new config field.
- `internal/notebook/commit.go` — the enforcement step over the policy, the new
  local-tree argument, and the refusal message branch.
- `internal/notebook/pull.go` — the conditional pin.
- `internal/app/service.go` — carry the config field through to the notebook; add
  the writable entry accessor.
- `internal/app/app.go` — the `Runtime` accessor forwarding to the service.
- `internal/app/readonly_test.go` — the `Runtime` and `Service` accessor
  assertions, beside the existing read-only ones.
- `internal/integrationtest/harness.go` — the harness config field.
- `internal/integrationtest/scenario_writable_test.go` — new: the scenarios below.
- `internal/notebook/commit_test.go`, `internal/notebook/pull_test.go` — the
  counted-call and recovery unit tests.
- `internal/app/config.go` — the comment recording that the resolved fields
  carry the normalized entries and that every rebuild answers alike; the flag
  itself is Phase 3's.

## Integration contract

Black-box MCP scenarios through the harness, which configures both sets directly.
The existing read-only scenarios are the regression guard and must pass unedited:
a read-only-only configuration is one policy shape among several, and this phase
changes none of its behavior.

Every row names its test. Phase 5's evidence check resolves test names out of
these files, so a scenario with no name is a scenario nothing proves — and the
composition rows below are where decisions D1 and D2 are cashed out.

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects | Test |
| --- | --- | --- | --- | --- | --- |
| Writable set configured; agent edits a file inside it; commit | Real engine, fake store | Success, generation advanced | The edit is published | None | `TestScenarioWritableCommitInsideWritableSucceeds` |
| Writable set configured; agent edits a file in a sibling directory; commit | Real engine, fake store | Refusal with the existing category and reason, the changed path listed, the message naming the writable entries | The sibling file is reset to baseline content | Remote generation unchanged; no pack uploaded | `TestScenarioWritableCommitRefusesOutsideWritable` |
| Writable set configured; agent creates a file at the notebook root; commit | Real engine, fake store | Refusal listing that path | The file is removed, since the baseline does not hold it | Remote generation unchanged | `TestScenarioWritableCommitRefusesRootFile` |
| Writable set configured; agent creates a directory that did not exist at startup; commit | Real engine, fake store | Refusal listing the path under it | The created file is removed | Remote generation unchanged | `TestScenarioWritableCommitRefusesNewDirectory` |
| Read-only entry with a writable entry below it; agent edits under the writable entry; commit | Real engine, fake store | Success, generation advanced | The edit is published | None | `TestScenarioWritableComposedWritableBelowReadOnlySucceeds` |
| Read-only entry with a writable entry below it; agent edits a sibling under the read-only entry; commit | Real engine, fake store | Refusal listing the sibling path | The sibling is reset | Remote generation unchanged | `TestScenarioWritableComposedSiblingUnderReadOnlyRefused` |
| Writable entry with a read-only entry below it; agent edits the read-only file; commit | Real engine, fake store | Refusal listing that path | The file is reset | Remote generation unchanged; the agent's other edits under the writable entry stay on disk | `TestScenarioWritableComposedReadOnlyBelowWritableRefused` |
| Read-only entry, a writable entry below it, a read-only entry below that; agent tampers with a file under the innermost entry; commit | Real engine, fake store | Refusal listing that path, the innermost read-only entry advertised | The tampered file is reset; the agent's edit in the writable middle stays on disk | Remote generation unchanged | `TestScenarioWritableThreeLevelCommitRefusesNestedProtected` |
| Writable entry, a read-only entry below it, a writable entry below that; agent edits under the innermost entry, then the read-only middle; commit | Real engine, fake store | The innermost edit publishes; the middle edit is refused | The middle file is reset | The published generation does not advance on the refusal | `TestScenarioWritableThreeLevelCommitAllowsNestedWritable` |
| Read-only entry, a writable entry below it, a read-only entry below that; a file under the innermost entry and one in the writable middle are edited locally; pull | Real engine, fake store | Success | The innermost file holds the accepted content | The writable middle edit is not touched | `TestScenarioWritableThreeLevelPullRestoresNestedProtected` |
| Writable entry, a read-only entry below it, a writable entry below that; the read-only middle and the innermost writable file are edited locally; pull | Real engine, fake store | Success | The read-only middle holds the accepted content | The innermost writable edit is not touched | `TestScenarioWritableThreeLevelPullKeepsNestedWritable` |
| Writable entry naming a path below a file the baseline holds; agent replaces that file with a directory; commit | Real engine, fake store | Recovery failure at the existing commit.readonly stage, resynchronized | The accepted file stands again and every local file under the directory is gone, the writable one included | Remote generation unchanged | `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob` |
| Writable set configured; a protected path is edited locally; pull | Real engine, fake store | Success; the protected path holds the remote content | The restore appears in the diffstat | The writable edits are not touched | `TestScenarioWritablePullRestoresProtectedPath` |
| Neither set configured; commit and pull | Real engine, fake store | Identical to the current behavior | None beyond today's | No policy work is performed | `TestScenarioWritableUnconfiguredCommitAndPullUnchanged` |
| Read-only set only, no writable set; the existing read-only scenarios | Real engine, fake store | Unchanged, assertions unedited | Unchanged | Unchanged | `internal/integrationtest/scenario_readonly_test.go`, run unedited |

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| A configured process refuses and resets a commit that changed a protected path, whichever set protects it | `TestScenarioWritableCommitRefusesOutsideWritable`, `TestScenarioWritableRefusalResetsProtectedFiles`, `TestScenarioWritableRefusalLeavesGenerationUnchanged` |
| A configured process restores a protected path on pull and leaves writable edits alone | `TestScenarioWritablePullRestoresProtectedPath`, `TestScenarioWritablePullKeepsWritableEdits` |
| The refusal names where the agent may write when a writable set is configured | `TestWritePolicyRefusalNamesWritableEntries` |
| The refusal wording for a read-only-only configuration is unchanged | `TestWritePolicyRefusalReadOnlyWordingUnchanged` |
| The refusal keeps the existing category, reason token, and per-file reason | `TestWritePolicyRefusalTokensUnchanged` |
| An operation with no protected change reads no blob the unconfigured operation would not read | `TestCommitCleanCommitPolicyAddsNoBlobRead`, `TestPullCleanPullPolicyAddsNoBlobRead` |
| Paths that did not exist when the process started are protected by the unmatched default | `TestScenarioWritableCommitRefusesRootFile`, `TestScenarioWritableCommitRefusesNewDirectory` |
| The two sets compose by longest match, in both nesting directions | `TestScenarioWritableComposedWritableBelowReadOnlySucceeds`, `TestScenarioWritableComposedSiblingUnderReadOnlyRefused`, `TestScenarioWritableComposedReadOnlyBelowWritableRefused` |
| A three-level composition enforces from the entries the operator wrote: the innermost region decides, on commit and on pull, in both nesting directions | `TestScenarioWritableThreeLevelCommitRefusesNestedProtected`, `TestScenarioWritableThreeLevelCommitAllowsNestedWritable`, `TestScenarioWritableThreeLevelPullRestoresNestedProtected`, `TestScenarioWritableThreeLevelPullKeepsNestedWritable` |
| A policy rebuilt from a layer's entry accessors protects what the written entries protect, so none of the three constructions on the startup path is load-bearing | `TestServiceNestedEntriesSurviveRebuild` |
| The reset's one departure from "writable edits stay on disk" is the restored-blob type change, and it is pinned rather than assumed | `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob` |
| The tree build above enforcement changes no refusal an operator can reach: invalid content is still the scan's refusal, and the one branch the build adds is a repository write fault that publishes nothing | `TestCommitInvalidContentRefusedBeforeTreeBuild`, `TestCommitTreeBuildWriteFailureIsInvalidContent` |
| A process with neither set configured is unchanged on both operations | `TestScenarioWritableUnconfiguredCommitAndPullUnchanged` |
| Recovery semantics during the reset are unchanged | `TestCommitWritePolicyResetRecoveryFailure` |
| A policy construction error fails startup rather than being swallowed | `TestServicePolicyConstructionFailsSetup` |
| Every existing read-only scenario passes unedited | `make test` over `internal/integrationtest/scenario_readonly_test.go` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| Detection fails while reading a tree | Storage integrity with the existing engine-failure reason; no local mutation has begun, so the call is not a recovery failure | `TestCommitDetectionFailureIsIntegrity` |
| Restore fails while reading a blob | Storage integrity with the existing engine-failure reason; no local mutation has begun | `TestCommitRestoreReadFailureIsIntegrity` |
| The restored snapshot cannot be represented as notebook state | Invalid request with the existing invalid-content reason, as the current path returns | `TestCommitRestoredSnapshotInvalid` |
| The local mutation of the reset fails | Recovery failure with the existing commit-reset stage, and the call never returns success | `TestCommitWritePolicyResetRecoveryFailure` |
| A protected path holds a file that violates the content rules on pull | The existing invalid-content refusal names the file before the restore runs | `TestScenarioWritablePullInvalidContentUnderProtected` |
| The policy cannot be constructed from the configured sets | `Notebook` construction fails, `Service` construction fails, and the error names the offending entry or the overlapping path | `TestServicePolicyConstructionFailsSetup` |
| A commit changes a protected path while another changes a writable one | Refusal listing only the protected path; the writable edit stays on disk and is not published | `TestScenarioWritableRefusalKeepsWritableEdits` |
| A commit carries invalid visible content and a protected change together | The workspace scan refuses first with invalid content, naming the offending file; the tree build that now sits above enforcement names none, which is what tells the two apart | `TestCommitInvalidContentRefusedBeforeTreeBuild` |
| The local tree build fails on a repository write fault | Invalid request with the existing invalid-content reason and no file named; no local mutation begins, no recovery report, and nothing is published | `TestCommitTreeBuildWriteFailureIsInvalidContent` |
| A protected path the baseline holds as a file is a directory in the visible tree when the reset runs | Recovery failure at the existing commit.readonly stage with the visible directory resynchronized to the accepted state | `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob` |

## Implementation notes

### 2026-09-18 — Phase 2 executed — Claude Opus 5 (1M context), [[worklog-work]]

Deltas only; the specification is not restated.

**Deviations and decisions**

| # | Where | What and why |
| --- | --- | --- |
| 1 | `internal/notebook/commit.go` | The local tree build moved above the enforcement step, because the step now takes the tree the commit already built. The step's position relative to the message, the pulled marker, the snapshot and the conflict markers is unchanged, and the tree build cannot change an outcome that used to be a refusal: invalid visible content is already refused by the workspace scan one step earlier (`TestScenarioReadOnlyPullInvalidContentUnderEntry`, `TestScenarioWritablePullInvalidContentUnderProtected`). |
| 2 | `internal/notebook/notebook.go` | `Notebook` holds the policy **and** keeps a `git.EntrySet` of the read-only side. `PathPolicy` answers everything except one question: which read-only entry a violated path falls under, which the unchanged read-only wording names. Widening `PathPolicy` with a covering-entry accessor is forbidden without a README change, and refolding the entries per refusal would duplicate the matching rule, so the set is retained and its single purpose is commented. |
| 3 | `internal/notebook/commit.go` | `restoreProtected` passes the repository through a read-error recorder. `PathPolicy.RestoreProtected` validates the restored snapshot itself, so a failed baseline blob read and a snapshot the notebook cannot represent both come back as one wrapped error; only the boundary the failure crossed separates engine failure from invalid content. The recorder is the cheapest honest discriminator that does not change Phase 1's code. |
| 4 | `internal/notebook/fake_test.go` | Gained the `writable` field of `nbConfig`, which the Files list did not name. It is the unit-test twin of the harness field this phase owns; the notebook unit tests cannot configure a writable set without it. |
| 5 | `internal/notebook/commit_test.go` | The unit fixtures seed the accepted state through a **second, unconfigured** notebook over the same store. A configured process whose first pull meets a protected path it does not yet hold restores that path away, so seeding in place would have given the configured and unconfigured runs different fixtures — which is exactly what the first draft of `TestCommitCleanCommitPolicyAddsNoBlobRead` measured (4 blob reads against 8). |
| 6 | `docs/slivingdoc-v1.md` | Untouched. The accepted contract's writable subsection is Phase 4's file list and its configuration-table row is Phase 3's, so writing either here would author text another phase owns. |

**New wording.** The refusal under a writable set is `Only <entries> is|are writable in this server. Your changes elsewhere were discarded and the files reset. Write under the writable paths, then commit again.`, built beside `readOnlyMessage` and joined with `ReadOnlyListSeparator`. The read-only wording is untouched and asserted byte-for-byte.

**Tests beyond the named ones.** `TestNewNotebookWritablePaths` (the notebook half of the construction-failure row: the accessor, the normalized non-nil set, and both unbuildable policies), `TestNewServiceNormalizesWritablePaths` and `TestRuntimeWritablePaths` (the two other accessors the phase owns; `Runtime`'s set is empty until Phase 3's flag resolves into it).

**Invariant and limit rows, executed**

| Table | Row | Test | Result |
| --- | --- | --- | --- |
| Commit | Never publishes a change to a protected path | `TestScenarioWritableCommitRefusesOutsideWritable` | PASS ×3 |
| Commit | Refused call returns no success, no remote change | `TestScenarioWritableRefusalLeavesGenerationUnchanged` | PASS ×3 |
| Commit | Touched protected files reset to the baseline | `TestScenarioWritableRefusalResetsProtectedFiles` | PASS ×3 |
| Commit | Writable edits stay on disk after a refusal | `TestScenarioWritableRefusalKeepsWritableEdits` | PASS ×3 |
| Commit | Reset failure is a recovery failure at the existing stage | `TestCommitWritePolicyResetRecoveryFailure` | PASS ×3 |
| Commit | Clean commit reads no blob on the policy's account | `TestCommitCleanCommitPolicyAddsNoBlobRead` | PASS ×3 |
| Commit | Clean commit is byte-for-byte what it is today | `TestScenarioWritableCleanCommitUnchanged` | PASS ×3 |
| Pull | Protected local edit discarded, remote side taken | `TestScenarioWritablePullRestoresProtectedPath` | PASS ×3 |
| Pull | Writable local edit survives the pull | `TestScenarioWritablePullKeepsWritableEdits` | PASS ×3 |
| Pull | Restore appears in the ordinary diffstat | `TestScenarioWritablePullRestoreAppearsInDiffstat` | PASS ×3 |
| Pull | Clean pull reads no blob on the policy's account | `TestPullCleanPullPolicyAddsNoBlobRead` | PASS ×3 |
| Pull | Clean pull builds no extra tree | `TestPullCleanPullBuildsNoExtraTree` | PASS ×3 |
| Pull | Invalid content under a protected path refused first | `TestScenarioWritablePullInvalidContentUnderProtected` | PASS ×3 |
| Refusal | Writable set non-empty names the writable entries | `TestWritePolicyRefusalNamesWritableEntries` | PASS ×3 |
| Refusal | Writable set empty keeps today's wording | `TestWritePolicyRefusalReadOnlyWordingUnchanged` | PASS ×3 |
| Refusal | Both sets non-empty names the writable entries | `TestWritePolicyRefusalComposedNamesWritable` | PASS ×3 |
| Errors | Detection tree-read failure is integrity | `TestCommitDetectionFailureIsIntegrity` | PASS ×3 |
| Errors | Restore blob-read failure is integrity | `TestCommitRestoreReadFailureIsIntegrity` | PASS ×3 |
| Errors | Unrepresentable restored snapshot is invalid content | `TestCommitRestoredSnapshotInvalid` | PASS ×3 |
| Errors | Reset local mutation failure is a recovery failure | `TestCommitWritePolicyResetRecoveryFailure` | PASS ×3 |
| Errors | Policy cannot be constructed | `TestServicePolicyConstructionFailsSetup`, `TestNewNotebookWritablePaths` | PASS ×3 |
| Errors | Protected and writable path changed in one commit | `TestScenarioWritableRefusalKeepsWritableEdits` | PASS ×3 |

Every integration-contract row is one of the `TestScenarioWritable*` tests above or one of
`TestScenarioWritableCommitInsideWritableSucceeds`,
`TestScenarioWritableCommitRefusesRootFile`,
`TestScenarioWritableCommitRefusesNewDirectory`,
`TestScenarioWritableComposedWritableBelowReadOnlySucceeds`,
`TestScenarioWritableComposedSiblingUnderReadOnlyRefused`,
`TestScenarioWritableComposedReadOnlyBelowWritableRefused`,
`TestScenarioWritableUnconfiguredCommitAndPullUnchanged`; all passed ×3 under `-race`.

**Acceptance criteria, checked**

| Outcome | Evidence |
| --- | --- |
| Refuses and resets a protected commit, whichever set protects it | `TestScenarioWritableCommitRefusesOutsideWritable`, `TestScenarioWritableRefusalResetsProtectedFiles`, `TestScenarioWritableRefusalLeavesGenerationUnchanged` — PASS ×3 |
| Restores on pull and leaves writable edits alone | `TestScenarioWritablePullRestoresProtectedPath`, `TestScenarioWritablePullKeepsWritableEdits` — PASS ×3 |
| The refusal names where the agent may write | `TestWritePolicyRefusalNamesWritableEntries` — PASS ×3 |
| Read-only-only wording unchanged | `TestWritePolicyRefusalReadOnlyWordingUnchanged` — PASS ×3 |
| Category, reason token, and per-file reason kept | `TestWritePolicyRefusalTokensUnchanged` — PASS ×3 |
| No blob the unconfigured operation would not read | `TestCommitCleanCommitPolicyAddsNoBlobRead`, `TestPullCleanPullPolicyAddsNoBlobRead` — PASS ×3 |
| Paths absent at startup are protected by the unmatched default | `TestScenarioWritableCommitRefusesRootFile`, `TestScenarioWritableCommitRefusesNewDirectory` — PASS ×3 |
| The two sets compose by longest match, both nesting directions | `TestScenarioWritableComposedWritableBelowReadOnlySucceeds`, `TestScenarioWritableComposedSiblingUnderReadOnlyRefused`, `TestScenarioWritableComposedReadOnlyBelowWritableRefused` — PASS ×3 |
| A process with neither set configured is unchanged | `TestScenarioWritableUnconfiguredCommitAndPullUnchanged` — PASS ×3 |
| Recovery semantics unchanged | `TestCommitWritePolicyResetRecoveryFailure` — PASS ×3, stage `commit.readonly`, remote accepted `no` |
| A policy construction error fails startup | `TestServicePolicyConstructionFailsSetup` — PASS ×3 |
| Every existing read-only scenario passes unedited | `git status --porcelain internal/integrationtest/scenario_readonly_test.go` prints nothing; all 16 `TestScenarioReadOnly*` PASS ×3 |

**Verification commands**

| Command | Result |
| --- | --- |
| `make lint` | clean: gofumpt, `go vet`, staticcheck v0.7.0, `go fix -diff` |
| `make test` | all packages ok, coverage 84.4 % (floor 70 %) |
| `go test ./internal/notebook/ ./internal/app/ -race -count=3 -run '…'` | every named unit test PASS ×3 |
| `go test ./internal/integrationtest/ -race -count=3 -run 'TestScenarioWritable\|TestScenarioReadOnly'` | 16 writable and 16 read-only scenarios PASS ×3 |
| `git status --porcelain internal/integrationtest/scenario_readonly_test.go` | empty — the regression guard is unedited |

### 2026-09-19 — Phase 2 review-2 fixes — Claude Opus 5 (1M context), [[worklog-work]]

Deltas only. R2-01's Phase 2 half, R1-05, R1-08 and R2-03 are closed; no
production behaviour changed in this pass, only two comments, nine tests and
this file.

**Resumed work.** A previous session on this phase was killed mid-pass by an
API rate limit and left uncommitted work in the tree with no worklog record.
Audited before continuing: the four three-level scenarios, the restored-blob
scenario, the two tree-build unit tests, the rebuild guard test and the two
rebuild comments were already written and complete, and are kept as they
stand. Added in this session: the retained-set guard
(`TestReadOnlyRefusalSetMatchesPolicyEntries`), the two comment corrections
the Phase 1 hand-off asks for, every worklog edit, and every verification run
below — nothing inherited was verified, so all of it was re-run and probed
from scratch.

**Deviations and decisions**

| # | Where | What and why |
| --- | --- | --- |
| 7 | R2-01 boundary proof | Four scenarios rather than the two the checkbox asks for. Commit and pull are separate enforcement points and the finding defeats both, and the two nesting directions are different code paths through the collapse: read-only over writable over read-only exercises a *read-only* entry the collapse would drop, writable over read-only over writable exercises a *writable* one. Two scenarios would have proved one of the four cells. |
| 8 | `internal/integrationtest/scenario_writable_test.go` | `writableBaseline` gained a third level on both sides — `notes/agent-a/free.md`, `notes/agent-a/locked/secret.md`, `docs/open/deep/e.md`. Every existing scenario asserting the whole visible tree against it therefore changed its expectation; the existing read-only scenarios are in their own file and are untouched. |
| 9 | R2-03 | **Decided: document and pin the fixed point, do not collapse the rebuilds.** Collapsing them means carrying a `git.PathPolicy` value through `ServiceConfig` and `notebook.Config` in place of the two `[]string` fields the README's Parameters table owns, which would put a type from `internal/git` in two configuration structs, would strand the black-box harness that feeds raw entries, and would remove each layer's own validation of what it is configured with — the property `TestNewNotebookWritablePaths` and `TestServicePolicyConstructionFailsSetup` assert, and the convention the notebook already followed for the read-only set before this effort. The hazard R2-03 names is gone for a better reason than call order: Phase 1's collapse is lossless for resolution, so a rebuild from the accessors is a fixed point and no construction is load-bearing. What was missing was that this is nowhere a future caller would look, so it is now stated on `PathPolicy.ReadOnly` (Phase 1), at both rebuild sites (`internal/app/config.go`, `internal/app/service.go`), and in this phase's Configuration fields section, and pinned at this phase's own boundary by `TestServiceNestedEntriesSurviveRebuild` rather than only in `internal/git`. |
| 10 | `internal/notebook/notebook.go`, `commit.go` | The two Phase 1 hand-offs. The retained `readOnly` entry set is collapsed within its own set while the policy collapses each set against the other, so the two can disagree — but only when the writable set is non-empty, and there `refusalMessage` names the writable entries and never reads the retained set. The field comment says so and `TestReadOnlyRefusalSetMatchesPolicyEntries` pins both halves: the sets are identical with no writable set, they genuinely differ with one, and the refusal under that configuration is the writable wording. `violatedEntries` documents that `CoveringEntry` now answers with the most specific entry rather than "the" entry. |
| 11 | `docs/slivingdoc-v1.md`, `docs/running.md` | Untouched. No observable behaviour moved in this pass: the resolution fix that changes what an operator sees is Phase 1's, and both documents already carry it. |

**Non-vacuity probes.** Every new oracle was run against the shape it guards
before being trusted:

| Throwaway edit | Effect |
| --- | --- |
| `entryBetween` forced to `false` (the pre-fix intra-set collapse) | All four three-level scenarios fail. `TestScenarioWritableThreeLevelCommitRefusesNestedProtected` fails with `call notes_commit(…) must be an error result` — the tampered commit publishes, which is exactly what R2-01 describes — and the pull scenario fails with `notes/agent-a/locked/secret.md = "secret: 2\n", want "secret: 1\n"`. |
| `EntrySet.covering` reverted to first match | The same four fail, the mirror direction failing the other way round: `TestScenarioWritableThreeLevelCommitAllowsNestedWritable` is refused where it must publish, naming `docs/open/deep/e.md`. |
| `refusalMessage` forced down the read-only branch | `TestReadOnlyRefusalSetMatchesPolicyEntries` fails with an empty entry list in the message, which is what the retained set answers for a path no read-only entry covers. |

Both `readonly.go` reverts were restored and the scenarios re-run green before
anything was recorded.

**Rows executed in this pass**

| Table | Row | Test | Result |
| --- | --- | --- | --- |
| Commit | Never publishes a protected change, at any nesting depth | `TestScenarioWritableThreeLevelCommitRefusesNestedProtected` | PASS ×3 |
| Commit | Writable edits stay on disk, and the restored-blob exception | `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob` | PASS ×3, both rows |
| Pull | Protected local edit discarded, at any nesting depth | `TestScenarioWritableThreeLevelPullRestoresNestedProtected` | PASS ×3 |
| Refusal | Both sets non-empty with a split read-only entry | `TestReadOnlyRefusalSetMatchesPolicyEntries` | PASS ×3 |
| Integration | Mirror nesting, commit | `TestScenarioWritableThreeLevelCommitAllowsNestedWritable` | PASS ×3 |
| Integration | Mirror nesting, pull | `TestScenarioWritableThreeLevelPullKeepsNestedWritable` | PASS ×3 |
| Acceptance | A rebuild from a layer's accessors protects alike | `TestServiceNestedEntriesSurviveRebuild` | PASS ×3 |
| Errors | Invalid content beside a protected change | `TestCommitInvalidContentRefusedBeforeTreeBuild` | PASS ×3 |
| Errors | Tree-build write fault | `TestCommitTreeBuildWriteFailureIsInvalidContent` | PASS ×3 |

**Verification commands**

| Command | Result |
| --- | --- |
| `make lint` | clean: gofumpt listed no file, `go vet` clean, staticcheck v0.7.0 clean, `go fix -diff` silent |
| `make test` | exit 0 — 18 `ok` packages, 0 `FAIL`, `== coverage: 84.6% (floor 70%) ==` |
| `go test ./internal/notebook/ -race -count=3 -run 'TestCommitInvalidContentRefusedBeforeTreeBuild\|TestCommitTreeBuildWriteFailureIsInvalidContent\|TestReadOnlyRefusalSetMatchesPolicyEntries'` | 9 `--- PASS` lines, three tests ×3 |
| `go test ./internal/app/ -race -count=3 -run TestServiceNestedEntriesSurviveRebuild` | PASS ×3 |
| `go test ./internal/integrationtest/ -race -count=3 -run 'TestScenarioWritable\|TestScenarioReadOnly'` | `ok` — 32 writable and 15 read-only scenarios, 141 `--- PASS` lines with their subtests |
| `git status --porcelain internal/integrationtest/scenario_readonly_test.go` | empty — the regression guard is still unedited |

Phase 2 is `Complete`. Phase 4 still owns R2-02; the nine test names cited
above are new, so Phase 5's evidence check sees them for the first time.

## Review findings

### Review 1 — 2026-09-19 — status `Complete` (notes only)

**R1-05 — note — `internal/git/policy.go:206-216`; this phase's Commit invariant
table, row "The agent's edits to writable paths stay on disk after a refusal".**
The row states the guarantee without the exception Phase 1's own restore table
records. When a protected path is a blob in the baseline and a directory in the
visible tree, `RestoreProtected` drops **every** local file beneath that path —
`namedOrUnder(f.Path, changed)` matches on the `name + "/"` prefix — including
files the policy considers writable. Concretely, with `--writable-paths=dir/w.md`
over a baseline holding a blob at `dir`, an agent that replaces `dir` with a
directory containing `dir/w.md` and `dir/p.md` has *both* removed by the refusal
reset, and only the baseline blob `dir` comes back. The drop is forced — the two
shapes cannot coexist in one snapshot — but the invariant row promises otherwise
and no test exercises the case at this phase's boundary.

- [x] State the type-change exception on the invariant row, or add a scenario
      that pins what the reset does to a writable file beneath a restored blob.

**R1-08 — note — `internal/notebook/commit.go:54-60`.**
Deviation 1 moved the enforcement step below `git.BuildTree(local)`. Two
consequences are unpinned. First, a commit that both fails tree construction and
touched a protected path now returns `INVALID_REQUEST`/`INVALID_CONTENT` where it
returned `READ_ONLY_PATH` before; the phase argues the branch is unreachable
because the workspace scan validates first, and the argument looks right, but
nothing in the worklog or the suite pins it, so it rests on the reader agreeing.
Second, the refusal path now writes the agent's protected-path blobs into the
private repository before refusing. Neither breaks an invariant — nothing is
published — but both are behaviour the phase changed without an oracle.

- [x] Record the reachability argument as a claim with its evidence, or add a
      test that proves the scan refuses everything `BuildTree` would.

### Verified good

- **Invariant 3 traced through every commit branch.** `enforcePolicy` runs on
  every `Commit` before the publication loop and short-circuits only on an
  unconfigured policy. Inside the loop, the merge takes `baseline` as its base
  and `localTree` as ours; at a protected path ours equals base by construction,
  so the merge takes the remote side on the clean branch, the conflict branch
  (no conflict can arise where ours equals base), the no-op branch
  (`merged.Tree == remote.tree`), and every CAS retry against a newly observed
  remote. No branch reaches `buildProposal` with a protected change.
- **Invariant 3 traced through pull.** `pinProtected` restores protected paths to
  the baseline before the merge, so the merge takes the remote side there on both
  the clean and the conflicting branch, and `Materialize` records the remote as
  the new baseline in both. A first operation at generation 0 is safe because the
  baseline tree is `workspace.EmptyTreeID`, which the workspace creates in the
  repository at init, so `ReadTree` on it succeeds rather than failing as an
  engine error.
- **Invariant 6 re-derived.** `ChangedProtected` calls `ReadTree` only, and
  `RestoreProtected` is not called when nothing changed. The difference oracle in
  `policyAccess` is honest: the same fixture and the same operation run twice
  with the entry sets as the only difference, so the fake's scripted merge
  cancels out. `countingRepo.ReadBlob` counts across every repository handle the
  engine hands out, so a policy blob read could not escape the count.
- **Invariant 7.** The reset still runs through `applyLocal(ctx, stageReadOnly,
  RemoteAcceptedNo, …)`; `TestCommitWritePolicyResetRecoveryFailure` pins
  `RECOVERY_FAILURE`, stage `commit.readonly`, `remoteAccepted=no`, and a
  resynchronized baseline on disk.
- **The read-only regression guard is genuine.** `git status --porcelain
  internal/integrationtest/scenario_readonly_test.go` prints nothing and its 15
  scenarios pass; the sixteenth `TestScenarioCLIReadOnly*` lives in
  `scenario_cli_test.go`, whose oracle Phase 4 widened (see R1-07).
- **The error discriminator works on both branches.** `readErrorRepo` intercepts
  `ReadTree` and `ReadBlob`, which are the only repository methods
  `RestoreProtected` calls, so a read failure is `STORAGE_INTEGRITY` and a
  snapshot the notebook cannot represent is `INVALID_REQUEST` — the two the
  error-coverage table asks for. P2-N2 is a fair description of the cost.
- **P2-N1 is real and its resolution is the right one.** `Notebook` keeps a
  `git.EntrySet` of the read-only side solely so `violatedEntries` can name the
  covering entry in the unchanged read-only wording. With an empty writable set
  the unmatched default is writable, so every reported path is covered by a
  read-only entry and the message is never empty.
- **P4-N3 is real and correctly handled.** `TestScenarioWritableCleanCommitUnchanged`
  now factors the advertised sets out of the comparison and asserts the writable
  array separately, which is the strongest oracle available once D7 makes the
  envelopes legitimately different.

### Review 2 — 2026-09-19 — reopened; status `Reopened (review 2)`

**R2-01 — blocker — this phase's Commit invariant row "A commit from a
configured process never publishes a change to a protected path", and README
invariant 3.** The full finding is filed against Phase 1, which owns resolution;
this row is where it becomes observable. With
`--read-only-paths notes,notes/agent-a/locked --writable-paths notes/agent-a`,
the intra-set collapse drops `notes/agent-a/locked` from the read-only set
before `Protects` ever runs, so `PathPolicy.ChangedProtected` returns the empty
list for a changed `notes/agent-a/locked/secret.md`, `enforcePolicy` returns at
`internal/notebook/commit.go:364-366`, and the change is published. Nothing in
this phase's code is wrong — the short-circuit is exactly what the specification
asks for — but the invariant the row states does not hold for that
configuration, so the row is not proven and the phase is not done.

The same configuration also defeats the pull half: `pinProtected`
(`internal/notebook/pull.go:106-108`) skips the pin on an empty `changed`, so a
local edit under `notes/agent-a/locked` survives the merge instead of being
restored.

- [x] Once Phase 1 resolves over the written entries, add one commit scenario
      and one pull scenario for a three-level composition to
      `internal/integrationtest/scenario_writable_test.go`, beside the existing
      `TestScenarioWritableComposed*` rows, and add the matching
      integration-contract and acceptance rows here. The existing composed rows
      are all two-level, which is why this class had no boundary test.

**R1-05 — still open, still non-blocking.** Re-verified in the code:
`RestoreProtected` drops every local file under a named path via
`namedOrUnder` (`internal/git/policy.go:216`), so a writable file beneath a
protected path that the baseline holds as a blob is discarded with the
directory. Traced the reachable shape — baseline blob at `X`, local directory
`X/` containing a file a writable entry below `X` permits — and confirmed the
drop is structurally forced: a snapshot cannot hold both shapes at `X`. The
judgement that this is a note stands; the invariant row still promises otherwise
and no test exercises it, so the checkbox stays open.

**R1-08 — still open, still non-blocking.** Re-verified: `git.BuildTree` still
runs above `enforcePolicy` (`internal/notebook/commit.go:54-60`). Traced the
reachability argument independently — `BuildTree` fails only on
`ValidateSnapshot` (which `n.ws.Snapshot` has already applied to the same
snapshot one step earlier) or on a repository `WriteBlob` failure, which is an
I/O fault rather than the invalid content the mapping names. So the
argument holds for the content half and there is a second, unmentioned branch on
which the mapping is questionable. Nothing is published either way, so the note
does not block; both the reachability claim and the write-failure mapping are
still unpinned.

**R2-03 — note — `internal/app/config.go:273-274`, `internal/app/service.go:186-187`,
`internal/notebook/notebook.go:153`.**
The policy is constructed three times on the production path, and each
construction after the first is fed the *output* of the previous one:
`finish` overwrites `cfg.readOnlyPaths`/`cfg.writablePaths` with
`policy.ReadOnly()`/`policy.Writable()`, `serviceConfig()` copies those into
`ServiceConfig`, `NewService` calls `git.NewPolicy` over them, and
`Service.open` passes `s.policy.ReadOnly()`/`.Writable()` into `notebook.New`,
which calls `git.NewPolicy` a third time. Every one of those accessors returns
the **collapsed** set, so the entries the operator wrote exist only inside the
first call. Two consequences, neither a fault today:

- The first construction is load-bearing and nothing says so. The second and
  third could not raise the overlap refusal for a configuration the first
  accepted, because the evidence is gone by then.
- Any fix for R2-01 that keeps the written entries must keep them **inside**
  `PathPolicy`, or thread them through `ServiceConfig` and `notebook.Config` as
  their own fields. Re-deriving them from the accessors is impossible.

The black-box harness is the exception: `HarnessConfig` feeds raw entries
straight into `ServiceConfig`, so its `NewService` call does see the written
entries. That asymmetry between the harness and the CLI is worth a sentence
wherever the fix lands.

- [x] Record on this phase's "Configuration fields" section that the two config
      fields carry the *collapsed* entries and that `PathPolicy` is the only
      place the written entries survive.

### Verified good (review 2)

- **The enforcement code is unchanged since review 1** (`git diff` over
  `internal/notebook/commit.go` and `pull.go` is what review 1 traced), and
  invariants 3, 6 and 7 still hold on every branch *for every configuration the
  policy resolves correctly*. R2-01 is a fault in what the policy answers, not
  in where or how this phase asks it.
- **Invariant 6 re-confirmed at the gate.** `make qa` green with the difference
  oracles in it; `TestCommitCleanCommitPolicyAddsNoBlobRead` and
  `TestPullCleanPullPolicyAddsNoBlobRead` still compare a configured run against
  an unconfigured one over the same fixture, which is the only form of the
  claim that is true of the real engine.
- **The read-only regression guard is still unedited**: `git status --porcelain
  internal/integrationtest/scenario_readonly_test.go` prints nothing.
- **The accessor chain never returns nil.** `Notebook`, `Service` and `Runtime`
  all delegate to `PathPolicy.ReadOnly()`/`Writable()`, which copy from a
  `make`d slice.

### Review 3 — 2026-09-19 — not reopened; one note filed against this phase

**R3-02 — note — `internal/notebook/commit.go:362`, `:395`,
`internal/notebook/pull.go:104`.** All three engine-failure sites of the new
enforcement carry the message the read-only implementation used —
`"read the baseline snapshot for the read-only check"` — and it now describes an
operation the code does not perform and a setting the operator may not have set.

`Notebook.Error.Message` is caller-facing: `internal/mcp/errors.go:129` forwards
it through `Redact` into the `message` field and the candid text item, so the
sentence reaches an agent verbatim. Two things in it are wrong after Phase 2:

- **"read the baseline snapshot"** — detection no longer reads a snapshot. That
  is the whole point of D4 and of invariant 6: `ChangedProtected` compares tree
  objects and `RestoreProtected` reads only the named paths. A `ReadTree` failure
  during detection reports reading something the policy deliberately does not
  read, which is exactly the diagnostic a later reader would use to look in the
  wrong place.
- **"the read-only check"** — a process configured with `--writable-paths
  agents/scout` and no `--read-only-paths` has no read-only setting. The refusal
  wording was corrected for that case in this phase (`writableMessage`) and the
  advertisement in Phase 4; these three strings were carried over unedited.

Failure scenario: a process run with `--writable-paths agents/scout` hits a
repository read failure on the detection walk. The agent receives
`STORAGE_INTEGRITY` / `ENGINE_FAILED` with the text "read the baseline snapshot
for the read-only check" and an `OPERATOR` action; the operator then looks for a
read-only setting that does not exist in their configuration. Nothing else is
wrong — the code, the reason token, the action token and the `diagnosticId` are
all correct, and no test pins the string
(`grep -rn "read the baseline snapshot" --include='*.go' .` returns only the
three production sites).

- [ ] Replace the detection message with one that names what it did — comparing
      the baseline and local trees for protected changes — and the restore
      message with one that names reading the baseline content of the protected
      paths, in both `enforcePolicy` and `pinProtected`.
- [ ] Say "protected" rather than "read-only" in all three, matching the wording
      this phase already chose for the refusal and Phase 4 for the advertisement.

Not a reopen: the category, the reason token, the action token and the recovery
stage are all unchanged and correct, and `docs/slivingdoc-v1.md` pins the stage
name `commit.readonly` deliberately, not this sentence.

### Verified good (review 3)

- **Invariant 3 re-traced on the branches this review added to the enumeration.**
  A pull whose baseline is empty (generation 0) pins nothing it cannot read:
  `readBlobAt` reports absence rather than failing, so a protected path present
  only locally is dropped and one present on neither side is skipped. A pull that
  conflicts materializes R plus markers with the protected paths already equal to
  the merge base, so the merge takes R there and no marker can land on a
  protected path.
- **Detection compares against the baseline, never the remote, on both call
  sites**, which is what makes the pin idempotent across the commit retry loop:
  `enforcePolicy` runs once before the loop and the loop changes neither `local`
  nor the baseline, so a CAS retry cannot smuggle a protected change in.
- **The two error classifications of `restoreProtected` are still discriminated
  by the boundary crossed, not by the error value.** `readErrorRepo` wraps both
  `ReadTree` and `ReadBlob` and records the first failure, so a repository
  failure is `STORAGE_INTEGRITY` and a snapshot the notebook cannot represent is
  `INVALID_REQUEST`; `TestCommitRestoreReadFailureIsIntegrity` and
  `TestCommitRestoredSnapshotInvalid` pin the two sides.
- **A deletion under a protected path is covered at the black-box boundary**, not
  only in the unit table: `internal/integrationtest/scenario_writable_test.go:424`
  removes a protected file inside `TestScenarioWritableRefusalResetsProtectedFiles`,
  and the read-only suite's `TestScenarioReadOnlyAddAndDeleteRefused` is unedited.
- **Subdirectory addressing needs no entry translation, and the contract says so.**
  A request `path` below the workspace root opens its own workspace that still
  mirrors the whole notebook, so the notebook-relative entries mean the same thing
  for every `path`; `docs/slivingdoc-v1.md:293-296` states it. Checked because
  under the inverted default a per-request re-rooting would have confined the
  process out of its own writable region.
