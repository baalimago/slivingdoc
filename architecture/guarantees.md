# Failure guarantees and concurrency

What slivingdoc promises when something fails partway, how it recovers local state, and how it behaves with many concurrent writers. The answers rest on one ordering (local state, then immutable pack upload, then conditional `current` replacement) and one recovery path (reread `current` and rebuild P and L from it). This doc answers "what state is the notebook in after failure X, and which code guarantees it".

Read this when: adding a new failure point, touching recovery or failpoints, changing the order of publication steps, or reasoning about throughput and contention.

## Key files

| File | Purpose |
|------|---------|
| `internal/notebook/commit.go` | `attemptPublication` (publication order), `publish` (CAS outcomes), `uploadProposal`, `mapUploadError` |
| `internal/notebook/remote.go` | `recoverState`, `recoveryReport`, `lookupPublication`, `readRemote` (stale-manifest restart) |
| `internal/notebook/notebook.go` | `entryRecovery`, `applyLocal`, `failAfterAccept`, stage constants `stageEntry` … `stageReadOnly` |
| `internal/notebook/errors.go` | `recoveryFailure`, `RecoveryReport`, `RemoteAccepted` (`yes`/`no`/`unknown`) |
| `internal/notebook/failpoints.go` | `Failpoints.CAS` |
| `internal/workspace/materialize.go` | `applyLocked`, `markRecoveryRequired`, `Failpoints` (`Scan`, `Stage`, `Replace`, `Baseline`, `Recover`) |
| `internal/workspace/workspace.go` | `Recover`, `withOpLock`, `openPrivateState` (recovery-required on open) |
| `internal/storage/upload.go` | `UploadUnique`, `VerifyObject`: pack-upload ambiguity |
| `internal/app/service.go` | `ServiceHooks` threads both failpoint sets in from tests |
| `internal/notebook/load_test.go` | `BenchmarkCommitLoadDistributed`, `BenchmarkCommitLoadBurst`, `BenchmarkPullCold`, `BenchmarkPullWarm`, `runWriterLoad` |

## Flow

```text
Publication order (load-bearing):
  create local commit + pack → storage.UploadUnique(pack) → CAS current → Workspace.Accept

Generic recovery:
  any local mutation fails after the durable recoveryRequired=true (applyLocal),
  or any failure after a proved CAS (failAfterAccept)
    → recoverState
        → readRemote (reread current, import packs, validate)
        → Workspace.Recover(baseline)   (only op allowed while recovery is required)
    → RECOVERY_FAILURE{stage, remoteAccepted, resynchronized}
  next call: Pull/Commit → RecoveryRequired()? → entryRecovery (stage "entry", remoteAccepted "unknown")
    → always RECOVERY_FAILURE; the call does no work of its own
```

## Behavior

### Failure table

| Failure point | Result | Enforced by |
|---------------|--------|-------------|
| Before pack upload | Remote unchanged | `attemptPublication` order |
| During pack upload | Remote unchanged; `STORAGE_FAILURE`/`PACK_UPLOAD`, or the store refusal's own reason (`STORAGE_FULL`, `REQUEST_LIMIT`, `RATE_LIMITED`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE`) | `mapUploadError`, `storageFailure` |
| Increment upload refused, space full | Publish the commit as a smaller whole-state checkpoint if one exists, else `STORAGE_FULL`; L and P unchanged until acceptance | `buildCompactingProposal` ([hosted-mode.md](./hosted-mode.md)) |
| Pack upload response lost | Read the unique key back, prove size and SHA-256; match continues, absent is a transport failure, different bytes is `STORAGE_INTEGRITY` | `UploadUnique`, `VerifyObject` |
| After upload, before CAS | Pack is an unreferenced proposal; cleanup may delete it later | [checkpoints.md](./checkpoints.md) |
| CAS precondition failure | Another writer won; merge again and retry | `publish` returns `errCASLost` |
| CAS response lost | Reread `current`, search active and retained descriptors for the publication ID | `publish`, `lookupPublication` |
| CAS accepted, local accept fails | `RECOVERY_FAILURE`, stage `commit.accept`, `remoteAccepted=yes`, even if resync succeeds, and also when the failure came before L mutation began (reading the target tree, staging, a cancelled request); an accepted compacting commit still runs its best-effort cleanup | `failAfterPublish`, `failAfterAccept` |
| Merge conflict | Remote unchanged; L rewritten with markers | [conflicts.md](./conflicts.md) |
| Retry exhaustion | `REMOTE_BUSY`; caller files untouched | `Commit` loop |
| Checkpoint failure | Accepted state unchanged; metrics + warning | `failCheckpoint` |
| Cleanup failure | Accepted state unchanged; obsolete objects remain | `failCleanup` |
| Corrupt pack or checksum | Refuse import; `STORAGE_INTEGRITY` | `ensurePack`, `git.ImportPack`, `git.ValidateHistory` |

`notes_commit` returns `OK` only when the CAS succeeded or a later manifest records its publication ID. An uncertain outcome is `STORAGE_FAILURE`/`PUBLICATION_UNPROVEN` (action `PULL`), or the reread's own error (`MANIFEST_READ`, `MANIFEST_INVALID`) if the lookup read fails; the visible directory is preserved, and the proposal is never republished automatically (it may still have landed).

### Local mutation and recovery

- There is no per-interruption recovery algorithm. Every mutation of L goes through `Workspace.applyLocked`, which stages the full target tree in P first (failure there leaves L intact and needs no recovery), then durably writes `recoveryRequired=true`, then rewrites L in place, then persists the new baseline with `recoveryRequired=false`.
- `applyLocal` inspects `ws.RecoveryRequired()` after a failed mutation: set means the mutation had started, so it runs `recoverState` and returns `RECOVERY_FAILURE`; clear means nothing changed, so the plain workspace error passes through: `mcp.MapError` reports a non-context failure (reading the target tree, staging) as retryable `STORAGE_FAILURE`/`INTERNAL`, while a staging step ended by cancellation or a deadline stays a protocol error over MCP (the CLI returns the raw error). The lock is not taken here: it is held from the start of the operation (`holdWorkspace`), and failing to take it there, a cancelled or expired lock wait included, is `STORAGE_FAILURE`/`LOCAL_STATE` through `mapLocalError` before anything changed.
- `recoverState` reports `stage` (`entry`, `pull.accept`, `commit.accept`, `commit.cas`, `merge.materialize`, `commit.readonly`), whether remote acceptance is known, and whether resync succeeded. A successful repair never turns the anomalous call into `OK`; action is `PULL` when resynchronized, else `RETRY`.
- If repair fails, P stays marked. Every normal workspace operation then returns `ErrRecoveryRequired` (`withOpLock`), and the next `Pull` or `Commit` runs `entryRecovery` instead of its own work.
- `workspace.Open` also enters recovery-required mode on a missing or corrupt `state.json`, a leftover `state.json.tmp`, an identity mismatch, or an unopenable repository (rebuilt empty and refilled from R).
- Recovery may overwrite L: L is not a durability boundary, but every overwrite is reported. Recovery during a failing call is reported through that call's `RECOVERY_FAILURE`. Entry recovery at the start of the next call is reported too: it rewrites L to the accepted state, discarding edits made since the failure (or before an open-time anomaly), so `entryRecovery` always returns `RECOVERY_FAILURE` stage `entry` and never runs the call's own pull or commit. A successful repair is `resynchronized=true` with action `PULL` and a message (`entryRecovered`) saying edits were discarded; the following call runs normally (`TestPullEntryRecoveryRunsBeforeWork`, `TestScenarioRecoveryRepairImpossible`).
- A read-only reset is an ordinary local mutation, so its failure once L mutation began is `RECOVERY_FAILURE` with stage `commit.readonly`, never a bare `READ_ONLY_PATH` over a half-reset directory.

### Failpoints

Deterministic injection at operation boundaries, wired through `app.ServiceHooks{Workspace, Notebook}`:

- `workspace.Failpoints.Scan`: before a scan; no mutation.
- `.Stage`: before staging; L and state untouched.
- `.Replace`: after the durable recovery flag, before L is touched; L unchanged but recovery required.
- `.Baseline`: after L is replaced, before the baseline record; recovery required.
- `.Recover`: inside `Workspace.Recover` after the flag is set.
- `notebook.Failpoints.CAS`: after the manifest CAS accepted, before local acceptance; exercises `remoteAccepted=yes`.

### Concurrency and scaling

- One notebook has one ordered accepted state; the CAS on `current` is the only serialized publication step. Scans, merges, pack builds, uploads, and downloads of different writers run concurrently. There is no lock object, lease, or clock-based expiry in S3.
- Contention is normal: a lost CAS rereads R, imports the new tail, merges against the new head, waits with bounded full-jitter backoff (25 ms doubling to 2 s, `exponentialBackoff`), and rebuilds the whole proposal with a new publication ID, generation, key, commit, and pack. Default bound 8 retries (`DefaultRetryLimit`, 0..100).
- Planning workload: 100 agents, one commit per agent per minute, about 1 kB each, about 1.67 accepted commits per second. `BenchmarkCommitLoadDistributed` and `BenchmarkCommitLoadBurst` cover evenly spread and synchronized bursts; `runWriterLoad` records throughput, latency percentiles, CAS attempts per commit, and conflicts. No fixed throughput is promised before benchmarking against the target S3.
- Throughput depends on note layout: separate files merge without caller action; overlapping edits to one file produce semantic conflicts that storage cannot remove. Prefer many focused files over one global append file. No agent namespaces are enforced.
- `notebook.Metrics` holds in memory, read only by tests: active tail count and bytes, checkpoint runs/failures/CAS attempts/size/duration, cleanup runs/candidates/deleted/errors. CAS attempts per commit and commit latency are measured only by `runWriterLoad`; pull time only by `BenchmarkPullCold`/`BenchmarkPullWarm`. Merge and pack-build timings are not measured.
- A checkpoint (and its cleanup) runs synchronously inside the `notes_commit` call whose tail reached the threshold, after local acceptance. It never changes that call's `OK` but delays that caller; other writers are not blocked.
- Pack downloads within one read overlap up to `packFetchConcurrency` (16) in flight; imports stay sequential in manifest order.
- The architecture avoids quadratic full-history uploads (one increment per commit, periodic checkpoints) and long writer critical sections (no lock held across the network).

## Gotchas

- The workspace operation lock is held across a whole `Pull` or `Commit` (`Workspace.Hold`), so two concurrent operations on the same path, in one process or across processes, run one after the other; the second sees the first's result in L and P. In one process both operations share one `Workspace`; across processes each has its own in-memory copy of `state.json`, and `acquire` rereads the record once the file lock is held (`refreshState`), so the second process works from the baseline and recovery flag the first one wrote (`TestOperationsOnOneWorkspaceDoNotInterleave`, `TestScenarioOperationsOnOnePathDoNotInterleave`, `TestLockHolderRereadsState`).
- `MarkPulled` runs outside `applyLocal`; its failure maps to `STORAGE_FAILURE`/`LOCAL_STATE` even though L was already rewritten (L and P are consistent at that point).
- Known bug: a store refusal met while recovering (the resynchronizing `readRemote` of `entryRecovery`, `applyLocal` or `failAfterAccept`) does not surface with its own reason: `entryRecovery` wraps it and the other two drop it, so the result is retryable `RECOVERY_FAILURE` even for `ACCESS_DENIED` ([hosted-mode.md](./hosted-mode.md)).
- A new failure boundary that mutates L must go through `applyLocked` (so the flag is durable first) and its caller must wrap it in `applyLocal` with the right stage and `RemoteAccepted` value.

## Related

- [notebook.md](./notebook.md), [commit.md](./commit.md), [pull.md](./pull.md), [workspace.md](./workspace.md), [checkpoints.md](./checkpoints.md)
- [product-contract.md](./product-contract.md) for the `RECOVERY_FAILURE` envelope
- [running.md](./running.md#conflict-recovery), [running.md](./running.md#checkpoints-and-retention), [running.md](./running.md#configuration) (`--commit-retries`)
- `../AGENTS.md`: [Event Flow](../AGENTS.md#event-flow), [Conventions](../AGENTS.md#conventions) (invariants)
- Other concerns: [errors.md](./errors.md), [storage.md](./storage.md), [testing.md](./testing.md)
