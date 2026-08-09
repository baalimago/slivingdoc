# Phase 6 — Checkpoints and scale

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 9.2 (L423), 13–16 (L813–998)

## Goal

Bound cold-start pack count without blocking writers, then measure the target
concurrency workload.

## Specification

Add automatic checkpoint scheduling to `internal/notebook`. The default trigger
is 1,024 active increments. Configuration can replace this positive count.
Schedule one effort when an accepted commit makes the active tail length
greater than or equal to the threshold. Retained tails do not count. Select the
oldest threshold increments as the stable prefix.

Checkpoint work is opportunistic and does not determine commit success. A
worker selects an immutable accepted prefix, builds and uploads a complete
checkpoint pack, and then reads the latest manifest.

If the selected prefix remains present, the worker replaces that prefix with
the checkpoint while preserving every later increment. It uses normal ETag CAS.
If the CAS loses, it rewrites only the manifest proposal against the new tail.
It reuses the pack only while the latest chain contains the exact selected
prefix. If another checkpoint removes that prefix, discard the proposal.

The checkpoint contains its exact head commit and complete tree. It declares a
shallow history boundary and has no unresolved external pack-delta base.
Its through-generation is the last compacted increment generation. Its pack
contains the commit, full tree closure, and blobs, but no tags, refs, or older
commit ancestors.

Retain the active and previous checkpoint generations by default. Active and
retained manifest data are the cleanup roots. After checkpoint CAS success,
cleanup considers only objects whose target or through-generation is at or
before that checkpoint's stable cutoff. It retains every candidate referenced
by active or retained data and can delete any other candidate, including a
never-accepted proposal. It never considers an object after the cutoff.

A retained generation contains its old checkpoint descriptor, complete ordered
tail through the replaced cutoff, and accepted head. It reconstructs the exact
accepted state replaced by the newer checkpoint.

Cleanup lists only the checkpoint and increment namespaces after checkpoint
CAS success. It parses candidate generations from valid keys. Before each
delete batch, reread and validate `current` and rebuild all active and retained
roots. Ignore malformed keys. Never delete `current`. Upload code, not cleanup,
aborts incomplete multipart uploads.

Follow LIST pagination to completion. Delete at most 1,000 keys per request.
Record per-key failures for metrics and a later checkpoint retry.

A reader that observes an old manifest and then encounters a missing object
must reread `current` and restart. Cleanup failure must remain observable in
logs and metrics but must not fail a commit.

Create benchmark and sustained-load harnesses for warm and cold readers. The
primary planning workload is 100 writers that each add approximately 1 kB once
per minute. Test distributed timing and one synchronized burst.

## Integration contract

| Trigger                        | Collaborators          | Observable result                                   | Required side effect               | Prohibited side effect          |
| ------------------------------ | ---------------------- | --------------------------------------------------- | ---------------------------------- | ------------------------------- |
| Tail reaches threshold         | Git engine and storage | Checkpoint is eventually indexed                    | Complete pack uploaded first       | No writer lock                  |
| Writers advance during build   | CAS store              | Later increments remain after checkpoint            | Stable prefix alone is replaced    | No accepted increment loss      |
| Two checkpoint workers race    | CAS store              | One physical index wins safely                      | Loser object remains unreferenced  | No duplicate logical commit     |
| Next checkpoint succeeds       | Storage cleanup        | Oldest unretained generation is deleted best effort | Current and previous stay readable | No active descriptor deletion   |
| Stale reader sees deleted pack | Storage                | Reader restarts from current                        | Final files equal current head     | No state reconstruction by LIST |

## Acceptance criteria

- [x] The threshold is configurable and defaults to 1,024.
- [x] An invalid zero or negative threshold fails configuration.
- [x] The threshold schedules one bounded checkpoint effort per notebook state.
- [x] A checkpoint alone reconstructs its exact head and complete files.
- [x] New increments can descend from and import after the shallow checkpoint.
- [x] Stable-prefix replacement preserves increments accepted during the build.
- [x] Repeated checkpoint CAS loss does not rebuild the checkpoint pack.
- [x] Pack reuse stops when another checkpoint removes the selected prefix.
- [x] Competing checkpoint workers cannot lose or reorder accepted increments.
- [x] Active plus one previous checkpoint generation remain readable by default.
- [x] Each retained generation reconstructs the exact head replaced by its successor checkpoint.
- [x] Cleanup runs only after checkpoint CAS success.
- [x] Cleanup treats active and retained manifest descriptors as its complete root set.
- [x] Cleanup considers only objects at or before the successful checkpoint cutoff.
- [x] Cleanup rereads current and rebuilds roots before each delete batch.
- [x] Malformed keys and multipart uploads are not checkpoint cleanup candidates.
- [x] Old unreferenced proposals are collected while newer proposals are untouched.
- [x] Cleanup failure does not alter commit or checkpoint acceptance.
- [x] Stale-reader restart is proven with a deterministic deletion barrier.
- [x] Orphan collection cannot delete active or retained descriptors.
- [x] Metrics expose tail count, tail bytes, checkpoint size, duration, and cleanup results.
- [x] A MinIO test covers checkpoint publication, reader restart, and cleanup.
- [x] Load results record throughput, latency percentiles, CAS attempts, and conflicts.

## Error coverage

| Failure                                       | Expected outcome                                          | Required test                 |
| --------------------------------------------- | --------------------------------------------------------- | ----------------------------- |
| Checkpoint pack creation fails                | Accepted current remains unchanged                        | Fake Git failure test         |
| Checkpoint upload fails                       | Current remains unchanged                                 | Fake storage failure test     |
| Prefix disappeared through another checkpoint | Discard or re-evaluate proposal safely                    | Competing-worker test         |
| Checkpoint CAS repeatedly loses               | Stop bounded effort and retry on later trigger            | Deterministic contention test |
| Cleanup lists stale data                      | Revalidate active and retained references before deletion | List/CAS race test            |
| Cleanup sees proposal after cutoff            | Proposal is not considered or deleted                     | Generation-fence race test    |
| Cleanup delete fails                          | Warning and retry opportunity, no commit failure          | Injected delete failure       |
| Reader pack disappears                        | Reread current and restart                                | Barrier-controlled test       |
| Checkpoint checksum is corrupt                | Storage-integrity failure, old index retained             | Corrupt-upload fixture        |

## Implementation notes

### 2026-08-10 — Completion session (imago, worker session 9)

An interrupted earlier session had written `checkpoint.go`, `checkpoint_test.go`,
and the checkpoint trigger in `commit.go` without documenting or validating
them; the suite failed on five tests. This session completed phase 6: it fixed
the production and test defects, added the MinIO checkpoint test, added the
load harness and benchmarks, and updated the worklog.

Production defects fixed:

1. **Increment generation after a compaction.** `buildIncrementProposal`
   numbered the increment and its pack key from the manifest generation
   counter (`remote.generation + 1`). The counter also advances on every
   checkpoint replacement, so after a compaction the counter diverges from
   the increment-chain position and the manifest validator rejected the
   proposal ("3 followed by 5"). The increment's target generation now
   continues the active chain from the checkpoint cutoff:
   `checkpoint.ThroughGeneration + len(active tail) + 1`. The manifest
   generation counter keeps its CAS-counter meaning (architecture 9.2,
   rules 5–7, and 13.3).
2. **Fake shallow graft.** The native boundary grafts declared shallow roots
   to zero parents at `ReadCommit` once the `shallow` file is loaded
   (internal/git2). The fake repository stored the boundary but still
   returned the raw parents, so `ExportIncrement` walked across the boundary
   and failed on the missing ancestor. The fake now mirrors the graft, so
   the extend-after-checkpoint test passes over the deterministic engine and
   the real libgit2 behaves identically.
3. **Stale tail metrics.** The triggering commit recorded the tail of its own
   (pre-checkpoint) proposal; a successful compaction left the metrics
   showing the compacted tail. `runCheckpoint` now re-records the tail from
   the compacted manifest after the CAS succeeds (and after the
   ambiguous-accepted path).

Test defects fixed:

4. Two competing-worker tests never wrote a local change for the writer whose
   commit was supposed to publish the next generation, so the commit was a
   no-change synchronization and no checkpoint was ever scheduled
   (`waitBlocked` timed out). Both tests now write the change before the
   blocked commit.
5. The corrupt-checkpoint fixture corrupted the store object of a checkpoint
   whose bytes the writer's cache already held byte-identical: an increment
   pack and the checkpoint pack of the same head contain the same sorted
   object set, so their bytes and SHA-256 are identical and the cache
   legitimately satisfied the descriptor. The fixture now pulls through a
   fresh cold reader whose cache cannot satisfy the descriptor.
6. `lastCommitFailEngine` failed the first read of the newly created commit,
   which the commit flow itself performs while exporting its increment pack.
   It now fails the second read of that commit, which is the checkpoint's
   pack export.
7. The preserves-increments test asserted a total put-count delta as one
   upload, but the delta included the writer's increment and the competing
   commit's increment. A key-counting store wrapper now asserts exactly one
   upload of the checkpoint key, and the CAS-attempts expectation was
   corrected to the single winning attempt against the later manifest.
8. The second-compaction expectations were updated to the fixed generation
   semantics: after a checkpoint through 3, the increments are 4 and 5, and
   the second checkpoint compacts through 5 (previously the test encoded the
   counter-based numbering 5 and 6).

New coverage:

- `internal/notebook/minio_test.go`:
  `TestMinioNotebookCheckpointCleansAndReaderRestarts` proves checkpoint
  publication, cleanup (retention 0 deletes the replaced generations from
  MinIO), and the stale-reader restart over real HTTP conditional writes and
  real libgit2. A `gatedStore` wrapper blocks the reader's download of the
  increment pack that cleanup deletes; the reader's GET 404s, it rereads
  `current`, and it reconstructs the final head.
- `internal/notebook/load_test.go`: the sustained-load harness
  (`runWriterLoad`) runs the planning workload over the deterministic fake
  store and fake engine — concurrent writers each adding ~1 kB, with
  distributed timing (the once-per-minute cadence compressed) or a
  synchronized burst. `loadResult` records accepted commits, failures,
  conflicts, throughput, latency percentiles (nearest rank over every
  completed commit), CAS attempts per accepted commit, the final tail shape,
  and the checkpoint and cleanup metrics.
  `TestLoadHarnessRecordsThroughputLatencyCASAndConflicts` proves the
  acceptance criterion deterministically (9 writers fit the production retry
  bound even in the fully overlapping worst case) and reconstructs the final
  state with a fresh reader. Benchmarks: `BenchmarkCommitLoadDistributed`
  (100 writers, 60 s window compressed to 2 s, threshold 50),
  `BenchmarkCommitLoadBurst` (100 writers at once with the production retry
  bound, recording accepted/failed and the contention cost),
  `BenchmarkPullCold` (fresh workspace downloads checkpoint plus tail), and
  `BenchmarkPullWarm` (cached reader downloads exactly one new increment).
  The load test helpers accept `testing.TB` so the benchmarks reuse the exact
  harness.

Gates run in this session (all pass):

- `CGO_ENABLED=0 go test -timeout=120s -count=1 ./...`
- `PKG_CONFIG_PATH=... go test -race -cover -timeout=30s -count=3 -p 1 ./...`
- `go vet ./...`, staticcheck v0.7.0, `go fix -diff ./...` (clean),
  gofumpt v0.11.0 (clean), dupl -t 80 (only the acceptable fake-mirror and
  pre-existing merge_test groups)
- `make integration-test` — 0 skips
- `go test -run '^$' -bench Benchmark -benchtime 2x ./internal/notebook/`
  — sample results: distributed 50 commits/sec, 1.01 cas/commit, 0 conflicts,
  p50 7.7 ms / p90 10.2 ms / p99 17.4 ms; burst 176.8 commits/sec, 7.55
  cas/commit, 26 failed within the production retry bound; cold pull
  13.7 ms/op; warm pull 13.4 ms/op.

Per-key cleanup failure recording stays at the batch granularity the
`DeleteObjects` semantic boundary exposes (a partial batch returns one
`ErrTransport`; per-key names never cross the boundary). The batch error is
recorded in `CleanupErrors` and retried on a later checkpoint cleanup, as the
phase-3 contract requires.

The `--checkpoint-packs` and `--retained-checkpoints` flags are application
configuration (architecture section 17) and land with the Phase 7 application
wiring; the notebook accepts the values and validates the ranges today.

## Review findings

No reviews recorded.
