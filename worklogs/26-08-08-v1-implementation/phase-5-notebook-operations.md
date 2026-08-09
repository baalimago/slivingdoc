# Phase 5 — Notebook operations

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 2 (L26), 4 (L116), 8.2 (L333), 10–15 (L603–998)

## Goal

Compose workspaces, Git state, and storage into safe pull and optimistic commit
operations.

## Specification

Create `internal/notebook` with dependencies expressed through consumer-owned
interfaces. Orchestration unit tests use the fake Git engine, fake object
store, deterministic ID source, and deterministic retry waiter.

`Pull` validates and ingests L, reads and validates `current`, downloads only
missing packs, validates every checksum and size, and imports R into P. It then
merges changes from P's accepted baseline to L onto R, rewrites L with the
result, and records R as the new baseline. Pull does not revert L on conflict.

Store cached pack bytes by SHA-256. Recompute size and SHA-256 for every cache
hit. After import, walk the accepted commit, full tree, and all blobs before L
changes. Treat a missing object or invalid text blob as `STORAGE_INTEGRITY`.

The first pull uses the canonical empty tree as the baseline. Existing valid
text files in L are local additions and merge with R.
The first commit creates a root commit with no parent, the initial
state-complete checkpoint pack, and `current` with `If-None-Match: *`.

`Commit` validates all files in L and rejects every complete conflict-marker
block with exact paths and marker ranges. It builds the local tree, loads the
accepted baseline from P and current remote tree R, and performs the three-tree
merge. A clean result creates a commit whose parent is the accepted R head
when one exists, and no parent for the first commit. It uploads one immutable
increment pack, and replaces `current` by ETag. After acceptance, it rewrites L
to the accepted merged tree and advances P's baseline before returning `OK`.

Require one successful or conflicting pull to initialize P before the first
commit. A commit without a baseline returns `INVALID_REQUEST`. A no-change
commit creates no publication ID, commit, pack, or CAS request. It synchronizes
L and P to R and returns `OK`.

A precondition failure causes the operation to import the new remote tail,
merge again, and retry with bounded randomized backoff. The retry limit is part
of application configuration. Each retry uses a new publication ID, target
generation, pack key, commit, and pack built against the newly observed
manifest. It never publishes a losing attempt's pack at a later generation.

Each attempt has one unique publication ID. If a manifest request has an
ambiguous response, read `current`. Return success only when the publication ID
is present in an active or retained checkpoint or increment descriptor. If the
ID cannot be found after those descriptors expire, return `STORAGE_FAILURE`,
preserve the visible workspace, and do not automatically republish the
proposal. The remote operation can have succeeded even though its result can no
longer be proved.

Production publication and checkpoint IDs use UUIDv7. Tests inject a
deterministic UUID source. Protocol ordering never depends on UUID timestamps.

On content conflict, materialize the merge result in L and return exact paths
and marker ranges. Materialize the full result, not only conflicted paths. Use
the exact marker grammar and one-based inclusive ranges in architecture
section 12 (L763).
A later commit of resolved files merges again if R moved.

An expected failure before local mutation leaves L unchanged. An unexpected
failure after local mutation starts returns `RECOVERY_FAILURE` and invokes one
generic recovery path: reread authoritative `current`, reconstruct P and L,
and report the failed stage, whether remote acceptance is known, and whether
resynchronization succeeded. If repair cannot finish, mark P as requiring
recovery and retry it before the next normal operation. Recovery can replace L.

## Integration contract

| Trigger                          | Collaborators                      | Observable result                     | Required side effect         | Prohibited side effect                  |
| -------------------------------- | ---------------------------------- | ------------------------------------- | ---------------------------- | --------------------------------------- |
| Pull with local changes          | Fake or MinIO plus real Git engine | L contains local changes rebased on R | P baseline advances to R     | No mergeable local change is discarded  |
| Commit against unchanged current | Real Git engine and storage        | `OK` and independent pull sees change | Pack precedes manifest CAS   | No full checkpoint for normal increment |
| Two disjoint commits race        | CAS store and real Git engine      | Both eventually return success        | Loser merges and retries     | No lost update                          |
| Two overlapping commits race     | CAS store and real Git engine      | One conflict includes exact path      | Accepted state remains valid | No false success                        |
| CAS response is lost             | Ambiguous fake                     | Result follows publication lookup     | No duplicate logical change  | No uncertain `OK`                       |

## Acceptance criteria

- [ ] Empty pull and first commit have explicit tests.
- [ ] Pull downloads only descriptors absent from the local cache.
- [ ] Cache corruption causes a fresh download or `STORAGE_INTEGRITY`, never a false hit.
- [ ] Reachability validation finds a missing commit, tree, or blob before L changes.
- [ ] Pull rebases local additions, modifications, and deletions onto R.
- [ ] First pull merges nonempty L from the canonical empty baseline.
- [ ] A clean no-change commit returns `OK` without a remote mutation and synchronizes L and P to R.
- [ ] A normal commit uploads its pack before the manifest references it.
- [ ] Independent pull verifies every accepted commit result.
- [ ] Deterministic barriers prove one winner and one retry for a two-writer race.
- [ ] A CAS loser creates a new generation, ID, key, commit, and pack before retry.
- [ ] Disjoint concurrent edits produce one linear accepted state with both changes.
- [ ] Text conflicts rewrite L and return stable paths and marker ranges.
- [ ] Pull conflict tests prove that L changes and pre-call bytes are not restored.
- [ ] Commit rejects complete conflict-marker blocks in any candidate file.
- [ ] Marker tests cover LF, CRLF, multiple blocks, near matches, and literal examples.
- [ ] Invalid UTF-8 and U+0000 content is rejected before Git or S3 mutation.
- [ ] Resolved conflicts can publish after further remote movement.
- [ ] Retry exhaustion preserves visible files and returns `REMOTE_BUSY`.
- [ ] Ambiguous accepted and ambiguous rejected CAS responses are distinguished.
- [ ] An unprovable CAS result returns `STORAGE_FAILURE` without automatic republication.
- [ ] An unprovable CAS result preserves the visible workspace for caller recovery.
- [ ] Corrupt remote data never reaches visible files.
- [ ] Core orchestration tests require no CGo, Docker, or network service.
- [ ] Real Git plus MinIO integration tests prove the complete publication order.
- [ ] Deterministic failpoints at each orchestration boundary exercise generic recovery.

## Error coverage

| Failure                           | Expected outcome                                 | Required test                               |
| --------------------------------- | ------------------------------------------------ | ------------------------------------------- |
| Blank message                     | Invalid request before scan or S3 access         | Unit test with collaborator call assertions |
| Commit without managed pull       | `INVALID_REQUEST` before S3 access               | Unit and component test                     |
| Pull merge conflicts              | Content-conflict error and marker files in L     | Workspace integration test                  |
| Commit input has marker block     | Content-conflict error before S3 access          | Marker table with exact path and ranges     |
| Pack GET fails                    | Storage failure and unchanged baseline           | Injected fake failure                       |
| Pack checksum fails               | Storage-integrity error and no import            | Corrupt-pack test                           |
| Merge conflicts                   | Content-conflict error, visible resolution files | Real merge test                             |
| Pack upload fails                 | Storage failure, current unchanged               | Failure-order test                          |
| CAS repeatedly loses              | Remote-busy error after exact bound              | Deterministic contention test               |
| CAS succeeds but response is lost | Publication lookup returns success               | Ambiguous-write test                        |
| CAS status cannot be determined   | Storage failure, never `OK`                      | Read-after-ambiguity failure test           |
| Restoring caller files fails      | Recovery-failure error                           | Injected workspace recovery failure         |

## Implementation notes

### 2026-08-09 — Design and handover (imago, worker session 6)

Session budget exhausted during design; no code landed yet. The full design is
worked out below so the next session implements straight from it. Baseline
verified before this session: `CGO_ENABLED=0 go test -timeout=30s -count=1
./...` passes; Docker is available; `.build/libgit2` is built.

#### Design decisions (recorded for the README Decisions table)

1. **Pull-first marker is notebook policy, not state schema.** A fresh
   workspace and a pulled-empty-remote workspace produce identical
   `state.json` records (gen 0, empty head, empty tree), so the architecture
   section 11.1 "commit without baseline" check cannot use the record. The
   workspace gains a durable `<P>/pulled` marker (empty file, tmp+rename) with
   `Pulled() bool` / `MarkPulled(ctx) error`; commit refuses with
   `INVALID_REQUEST` when it is absent. The state schema stays locked.
2. **Workspace gains `Materialize(ctx, baseline, tree)`.** Pull always needs
   L = merged result while P records baseline R (different trees when local
   changes exist), and commit needs the same for conflicts. Accept cannot
   express this (tree is taken from baseline), and Replace + Accept would
   double-rewrite L with a crash window that loses marker content. One
   failure-atomic `applyLocked(tree, &baseline)` operation preserves the
   workspace recovery-flag contract.
3. **Workspace gains `CacheDir() string`** (`<P>/pack-cache`). The notebook
   owns the pack-byte cache protocol (SHA-256-keyed files, size + fresh
   SHA-256 on every hit, corrupt cache = fresh download, never a false hit);
   only the workspace knows P.
4. **`git.MergeResult` gains `Index MergeIndex`; new `git.MaterializeTree(repo, res)`.**
   Materializing the full conflicted result needs the raw index. Rule: stage-0
   entries keep merged blobs, text conflicts carry marker content, D/F
   conflicts keep the local file side and omit the remote directory subtree
   (it survives in R and returns after resolution).
5. **Notebook consumes a consumer-owned `Workspace` interface** (Path-free:
   Snapshot, Baseline, Repo, Replace, Accept, Materialize, Recover,
   RecoveryRequired, CacheDir, Pulled, MarkPulled). Tests use the REAL
   workspace over a fake engine — only the Git engine and S3 store are fake.
6. **Fake store `AmbiguousNext` becomes `AmbiguousNext(op, key)`** to also
   cover Create/Replace (CAS response lost after landing). The rejected-ambiguous
   case is `FailNext(OpReplace, ErrTransport)` (write never lands).
7. **Retries = attempts after the first CAS** (app resolves the default 8;
   notebook validates 0..100). Default wait = full-jitter exponential backoff,
   first ceiling 25 ms, max 2 s; tests inject a deterministic waiter.
8. **Stale-pack restart** (section 10): pack GET `ErrNotFound` → re-read
   `current`; unchanged ETag → `STORAGE_INTEGRITY`; changed → retry, bounded.
9. **Recovery** (`recoverState`): reads `current` (absent → gen 0), imports,
   reconstructs the head tree, `workspace.Recover`. Report carries stage,
   `remoteAccepted` (yes/no/unknown from the publication-ID search in active +
   retained descriptors), `resynchronized`. Entry recovery runs before any
   pull/commit work when `RecoveryRequired()`.
10. **Notebook failpoints: single `CAS` hook** (fires after manifest acceptance,
    before local acceptance). L-mutation boundaries use the existing workspace
    failpoints; notebook maps `ErrPartial`/`ErrRecoveryRequired`/mutation-call
    errors to `RECOVERY_FAILURE` + generic recovery.
11. **First publication**: root commit, `ExportCheckpoint`, `MarkShallow`,
    TWO IDs (publication ID + checkpoint ID), `CreateObject(If-None-Match: *)`;
    manifest gen 1, checkpoint descriptor, empty tail. Normal commits append
    one increment, `ReplaceObject(If-Match)`. Upload precedes CAS always.
12. **No-change commit**: `merged == r.tree` → `Accept(gen, head, r.tree)`, no
    ID/commit/pack/CAS.
13. **Notebook fake repository**: full `git.Repository` over shared fakeData
    with deterministic OIDs; MergeTrees implements a real file-level three-way
    merge (disjoint/one-sided/add-add/modify-delete, D/F detection); MergeFile
    produces exact markers for overlapping text; WritePack/ImportPack use a
    small self-contained binary pack format so two fake repos can transfer
    objects (writer exports, reader imports) exactly like real packs.
14. **MinIO notebook integration tests**: own container helper mirroring
    s3store's (decided NOT to extract a shared package to avoid churning the
    completed phase-3 suite); Makefile `integration-test` gains the libgit2
    build stamp and `./internal/notebook/`; CI integration job adds
    `make libgit2`.

#### Remaining work

- Workspace: `Pulled`/`MarkPulled`, `Materialize`, `CacheDir` + tests.
- git: `MergeResult.Index`, `MaterializeTree` + unit tests; ValidateHistory
  missing-object tests.
- Fake store: `AmbiguousNext(op, key)` + tests.
- `internal/notebook`: errors.go, notebook.go, remote.go (readRemote /
  importRemote / pack cache), pull.go, commit.go (retry + publication
  lookup), recover.go, backoff.go, failpoints.go.
- Notebook tests: fake_test.go (full fake repo), pull_test.go, commit_test.go,
  recover_test.go, native_test.go (cgo, real git2 + fake store), minio_test.go
  (cgo + Docker, real git2 + MinIO).
- Makefile + CI wiring; README status board, Decisions table, session journal.
- Gates: `go test -race -timeout=30s -count=3 -p 1 ./...`, vet, staticcheck,
  gofumpt, go fix, dupl, `make integration-test`.

No implementation or tests landed in this session.

### 2026-08-10 — Session journal (imago, worker session 7, HANDOVER)

Context budget exhausted mid-phase; production code compiles and the full
pure suite passes, but no notebook test files or wiring landed yet. Continue
from this state.

Completed and green:

- git: `MergeResult.Index` carries the raw merge index; new
  `git.MaterializeTree(repo, res)` materializes the full conflict result
  (resolved stage-0 blobs, marker content for text conflicts, local file
  side + local subtree for D/F conflicts) with unit tests in merge_test.go.
- workspace: `Materialize(ctx, baseline, tree)`, `CacheDir()`, `Pulled()`,
  `MarkPulled(ctx)` (tmp+rename durable `<P>/pulled` marker) + tests.
- storage/fake: `AmbiguousNext(op, key)` now covers Put/Create/Replace
  (mutation lands, ErrTransport returned) + tests.
- NEW `internal/testminio` shared MinIO bootstrap (Ensure/Terminate/
  Config/FreshPrefix), and `internal/s3store/minio_test.go` refactored to
  consume it. **Decision-log deviation from design decision 14**: the
  phase-3 suite was originally told NOT to extract a shared package, but
  duplicating ~150 lines of container bootstrap would trip the `dupl -t 80`
  gate; only the bootstrap moved, every s3store semantic test is unchanged.
- `internal/notebook` production files: errors.go (stable Code taxonomy +
  Error type + RecoveryReport), notebook.go (consumer-owned Workspace
  interface WITHOUT Replace — unused, leaner than design decision 5;
  Config/New; entryRecovery; applyLocal; failAfterAccept; validateMessage;
  rejectMarkers; materializeTree), remote.go (readRemote with stale-pack
  restart bound by RetryLimit, importRemote checkpoint+increments order,
  pack cache with size+fresh-SHA-256 on every hit and corrupt-cache
  discard, lookupPublication over active+retained descriptors,
  recoverState), pull.go, commit.go (retry loop: buildProposal per attempt
  with fresh IDs/key/generation, UploadUnique before CAS, publish maps
  precondition→retry and transport→publication lookup, no-change commit,
  first-publication checkpoint path), backoff.go (full-jitter exponential,
  25ms base / 2s max, WaiterFunc for deterministic tests), failpoints.go
  (single CAS hook).

Verified: `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go vet
./internal/notebook/`, `CGO_ENABLED=0 go test -timeout=30s -count=1 ./...`
all pass (notebook has no tests yet).

Remaining to implement:

1. `internal/notebook/fake_test.go` — full fake repo per design decision 13:
   deterministic OIDs (copy workspace/fake_test.go hashing), real file-level
   MergeTrees (disjoint/one-sided/add-add/modify-delete, D/F detection;
   resolve via stage-0 entries, text conflicts via stages 1/2/3, merged tree
   via git.BuildTree over stage-0 blobs), MergeFile with exact markers,
   binary WritePack/ImportPack (magic + count + per-object kind/oid/size/
   data) so two fake repos transfer packs; plus fake engine implementing
   workspace.Engine.
2. pull_test.go / commit_test.go / recover_test.go per the phase acceptance
   criteria list (empty pull, first commit, cache corruption, reachability,
   rebase, no-change commit, pack-before-CAS via BlockNext(OpReplace),
   two-writer race with one winner + one retry, CAS loser rebuilds
   proposal, marker rejection table LF/CRLF/multiple/near/literal, invalid
   UTF-8/U+0000, REMOTE_BUSY bound via pre-injected FailNextKey(OpReplace)
   x N with RetryLimit=N-1, ambiguous accepted vs rejected via
   AmbiguousNext(OpReplace/OpCreate) vs FailNext(OpReplace, ErrTransport),
   CAS failpoint → RECOVERY_FAILURE with resynchronized=true, Recover
   failpoint → resynchronized=false, entry recovery). Use the REAL workspace
   with the notebook fake engine + fake store; deterministic NewID/Now
   (sequence of storage.UUID, fixed time) and WaiterFunc(no sleep).
3. native_test.go (cgo: real git2 engine + real workspace + fake store:
   first-commit/pull round trip, real concurrent race, pack-before-CAS).
4. minio_test.go (cgo + Docker: real git2 + real workspace + MinIO via
   internal/testminio: independent pull sees change, two-writer race).
5. Makefile: `integration-test` gains the libgit2 build stamp and
   `./internal/notebook/`; CI `integration` job adds `make libgit2`.
6. Worklog: README status board, Decisions table (incl. the testminio
   deviation and the Workspace-interface-without-Replace deviation), this
   session journal, phase-5 status flip; run `make qa` + `make
   integration-test`.

Notes for the continuing session:

- remoteState fields are lowercase (tree, head, generation, manifest,
  etag, present); `remote.Tree` is a compile error.
- recoveryFailure takes the PUBLIC RecoveryReport; convert the internal
  recoveryReport via `.public()`.
- `ensurePack` maps pack GET ErrNotFound to errStaleManifest (wrapped with
  %w); readRemote re-reads current, unchanged ETag → STORAGE_INTEGRITY,
  changed → restart, bound = RetryLimit.
- RetryLimit semantics: literal 0..100 (0 = single CAS attempt); the app
  layer passes DefaultRetryLimit=8. Tests needing an exact bound set it
  explicitly.
- The workspace `applyLocked` sets the durable recovery flag BEFORE L
  mutation; applyLocal keys off `ws.RecoveryRequired()` to distinguish
  pre- vs post-mutation failures. The `record baseline` persistState error
  path leaves the flag set, so post-mutation detection holds without
  touching phase-4 code.
- Marker rejection happens before BuildTree and any S3 access; commit
  requires Pulled() before Snapshot.
- MinIO tests must name the Docker dependency in skip messages; the
  integration gate fails on any skip. Docker is available on this host;
  `.build/libgit2` is built.

## Review findings

No reviews recorded.

### 2026-08-10 — Session journal (imago, worker session 8, COMPLETED)

Finished phase 5: the notebook test suite, native and MinIO integration,
wiring, and the worklog updates landed.

Landed in this session:

- `internal/notebook/fake_test.go`: complete in-memory fake repository
  (deterministic OIDs, real file-level MergeTrees with
  disjoint/one-sided/add-add/modify-delete/D-F classification,
  exact-marker MergeFile, self-contained binary WritePack/ImportPack with
  OID verification and atomic import), the fake engine, deterministic
  UUIDv7 ID source, the harness (newNotebook over the REAL workspace), and
  store wrappers (casGateStore, flakyStore, restartStore).
- `pull_test.go`: empty pull, first-pull nonempty L, rebase of
  additions/modifications/deletions, download-only-missing-packs cache
  accounting, cache corruption fresh download, reachability failure before
  L mutation, conflict markers with exact path/ranges, pack GET failure,
  corrupt pack rejection, stale-pack restart, invalid content, and entry
  recovery via reopen.
- `commit_test.go`: config validation, commit-without-pull, blank-message
  table, marker rejection table (LF/CRLF/multiple/near/literal/EOF),
  near-match acceptance, invalid content, first checkpoint publication
  (root commit + shallow boundary + If-None-Match create), no-change
  synchronization, increment pack, pack-before-CAS via casGateStore,
  two-writer race with one winner + one retry and an orphaned losing pack,
  overlapping conflict, REMOTE_BUSY bound, ambiguous accepted vs rejected
  CAS, pack upload failure and ambiguity, and retained-descriptor
  publication lookup.
- `recover_test.go`: CAS failpoint → RECOVERY_FAILURE with
  resynchronized=true; Recover failpoint → resynchronized=false and
  self-heal on the next call.
- `native_test.go` (cgo): real git2 + fake store first-commit/pull round
  trip, two-writer race, pack-before-CAS.
- `minio_test.go` (cgo): real git2 + MinIO via internal/testminio:
  independent pull sees change, concurrent two-writer race converges.
- Production fix in `pull.go`: a CONFLICTING pull now also calls
  MarkPulled — the phase spec requires a successful OR conflicting pull to
  initialize P for the first commit, and the conflict branch previously
  skipped the marker (caught by TestPullConflictWritesMarkersAndKeepsL).
- Removed the unused `codeOf` helper from errors.go (staticcheck U1000).
- `Makefile` integration-test gains the libgit2 build stamp and
  `./internal/notebook/`; CI integration job adds `make libgit2`.
- `go fix ./...` modernizations applied across the tree (range-over-int,
  maps.Copy, SplitSeq, strings.Cut); gofumpt formatting applied.

Gates run in this session (all pass):

- `CGO_ENABLED=0 go test -timeout=30s -count=1 ./...` — full pure suite
  green.
- `PKG_CONFIG_PATH=... go test -race -timeout=30s -count=3 -p 1 ./...`
  — the strict race gate green with the real libgit2 boundary and MinIO.
- `make integration-test` — 85 pass actions, 0 skips.
- `go vet ./...`, staticcheck v0.7.0, `go fix -diff ./...` (clean),
  gofumpt v0.11.0 (clean), dupl -t 80 (only the acceptable fake-mirror
  and pre-existing git merge_test clone groups remain).

Acceptance mapping: every phase-5 acceptance criterion and error coverage
row has an explicit test. The dupl group between notebook/fake_test.go and
workspace/fake_test.go is the required same-format deterministic-OID
mirror (interface + mock mirroring per the duplication policy); the git
merge_test.go group predates this phase. `make qa` (native-smoke included)
was not re-run this session beyond its components because the strict race
gate, vet, staticcheck, gofumpt, go fix, dupl, and `make integration-test`
all passed individually; the next phase re-runs `make qa` as its gate.
