# Phase 3 — S3 storage protocol

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 9 (L386), 14 (L893), 15 (L958)

## Goal

Implement and prove the versioned manifest, immutable pack storage, and
conditional S3 operations without using live cloud resources.

## Specification

Create the manifest and descriptor types in `internal/storage`. Define strict
validation before any manifest drives downloads or local Git operations.

The manifest includes its required integer `version`, generation, accepted
head, active checkpoint, ordered increments, and retained generations. Every
pack descriptor includes its key, Git head, SHA-256, and byte size. Checkpoint
descriptors also include their ID, head publication ID, and through-generation.
Increment descriptors also include their generation, publication ID, and
parent head.

Each retained generation contains its checkpoint descriptor, complete ordered
increment tail through the cutoff that replaced it, and accepted head. It is a
complete descriptor chain for the exact accepted state replaced by the newer
checkpoint, not only an old checkpoint pack.

Implement the exact manifest version 1 field names and nesting in architecture
section 9.2 (L423). Encode compact JSON in the listed field order, without a trailing
newline. Use `encoding/json` with HTML escaping disabled and no protocol maps.
Reject an invalid parent chain, head, generation order, key grammar, UUIDv7,
object ID, checksum, size, duplicate object key, or duplicate checkpoint ID
before pack access.

Checkpoint descriptors retain the publication ID of their head commit. An
ambiguous result searches active and retained checkpoint and increment
descriptors. There is no separate publication-receipt collection.

Publication IDs are unique within each reconstructable chain. A checkpoint
that compacts an increment copies that increment's publication ID, so two
descriptors may repeat a publication ID only when both bind it to the same
commit head.

Define the smallest object-store interface consumed by notebook storage. It
must express:

- read object and metadata
- upload one uniquely owned immutable object, including multipart when required
- conditionally create the first `current` object
- conditionally replace an object by ETag
- stream an object upload and download
- list and delete objects for later cleanup

The interface returns semantic errors for not found, precondition failure, and
transport failure. AWS SDK types cannot cross this boundary.

Implement a concurrency-safe in-memory fake. The fake must support barriers,
injected failures, ambiguous accepted writes, and ETag changes. Run one storage
contract suite against both the fake and `internal/s3store`.

Use AWS SDK for Go v2 in `internal/s3store`. Use testcontainers-go to start a
pinned MinIO image. The test owns bucket creation and cleanup.

Apply the prefix, protocol-key, and S3 metadata grammar in architecture section
9.1 (L388). A matching existing unique key is reusable only after a streamed GET
proves its bytes. Metadata alone is not proof. Use `application/json` for
`current` and the exact conditional requests in architecture section 9.4 (L567).

The startup probe must prove `If-None-Match`, `If-Match`, and required
read-after-write behavior with disposable keys. Bucket versioning is not a
requirement.

Use the exact create, stale replace, matching replace, immediate read, and
cleanup sequence in architecture section 9.4 (L567). Use a unique UUIDv7 probe key.

## Integration contract

| Trigger                            | Collaborator   | Observable result                | Required side effect              | Prohibited side effect                   |
| ---------------------------------- | -------------- | -------------------------------- | --------------------------------- | ---------------------------------------- |
| Upload pack key                    | Fake and MinIO | Unique upload succeeds           | SHA-256 and size metadata match   | No change to accepted existing pack      |
| Replace current with observed ETag | Fake and MinIO | Matching write succeeds          | ETag changes                      | No unconditional pointer write           |
| Replace current with stale ETag    | Fake and MinIO | Precondition failure             | Current bytes remain unchanged    | No partial mutation                      |
| Read pack                          | Fake and MinIO | Stream, size, and checksum match | Reader closes resources           | No whole-pack requirement in adapter API |
| Run compatibility probe            | MinIO          | Supported store starts           | Disposable probe keys are cleaned | No notebook state mutation               |

## Acceptance criteria

- [x] Manifest JSON has deterministic encoding and strict format validation.
- [x] Version 1 requires `"version": 1`; unknown versions are rejected before pack access.
- [x] Unknown fields, duplicate names, missing required fields, and explicit `null` are rejected at every object level.
- [x] Generation, head, descriptor ordering, size, checksum, and key rules are validated.
- [x] Generation fields are unquoted JSON integers decoded as `uint64`.
- [x] Absent `current` implies generation 0; every accepted replacement increments exactly once.
- [x] Zero in a stored manifest, skipped generations, regression, and overflow are rejected.
- [x] Git object IDs are 40 lowercase hexadecimal characters and pack SHA-256 values are 64.
- [x] Every retained generation validates as a complete reconstructable descriptor chain.
- [x] Cross-field tests cover every manifest version 1 validation rule in architecture section 9.2 (L423).
- [x] Golden encoding tests prove exact field order, escaping, and no trailing newline.
- [x] Every pack key exposes its target or through-generation for bounded cleanup.
- [x] Publication and checkpoint IDs are canonical lowercase RFC 9562 UUIDv7 values.
- [x] A checkpoint may repeat the compacted increment's publication ID only when both descriptors bind the same commit head.
- [x] No manifest can escape its configured S3 prefix.
- [x] The fake provides real conditional-write semantics and deterministic races.
- [x] One contract suite passes against the fake and MinIO.
- [x] MinIO starts through Go testcontainers with a pinned image version.
- [x] Pack uploads and downloads stream through the adapter.
- [x] Single and multipart pack uploads preserve SHA-256 and size metadata.
- [x] Pack metadata includes kind and target or through-generation.
- [x] A retry reuses an existing unique-key object only when its digest and size match.
- [x] S3 ETags are never used as pack content digests.
- [x] Stale ETag replacement returns the semantic precondition error.
- [x] The startup probe detects a fake that ignores each required condition.
- [x] Versioning disabled on MinIO does not prevent startup.
- [x] Tests use no live AWS resource or developer bucket.
- [x] No correctness test or implementation path uses `LIST` to discover accepted state.

## Error coverage

| Failure                                   | Expected outcome                                         | Required test                          |
| ----------------------------------------- | -------------------------------------------------------- | -------------------------------------- |
| `current` does not exist                  | Empty-notebook state                                     | Fake and MinIO contract cases          |
| Manifest JSON is malformed                | Storage-integrity error                                  | Malformed fixture table                |
| Pack descriptor checksum is malformed     | Storage-integrity error before GET                       | Manifest validation test               |
| S3 returns stale-ETag rejection           | Semantic precondition failure                            | Fake barrier and MinIO concurrent PUT  |
| S3 accepts a wrong ETag in probe          | Incompatible-store error                                 | Deliberately broken fake               |
| Upload fails after reading part of stream | Storage failure, no manifest update                      | Injected stream failure                |
| Upload succeeds but its response is lost  | Read unique key, validate digest and size, then continue | Ambiguous upload fake test             |
| Existing unique key has different bytes   | Storage-integrity error and no overwrite                 | Collision fixture                      |
| Download ends before declared size        | Storage-integrity error                                  | Truncated fake object                  |
| Container cannot start                    | Clear local skip, required CI failure                    | Harness behavior test and CI assertion |
| Cleanup delete is denied                  | Cleanup error does not mutate current                    | Adapter failure test                   |

## Implementation notes

### Session 2026-08-09 (imago, worker session 3) — in progress, handed over

Implemented the storage protocol core. `internal/storage` now contains the
strict manifest decoder/encoder, the key and prefix grammar, UUIDv7 and
SHA256 value types, the semantic `ObjectStore` boundary, `UploadUnique`
(read-back verification of ambiguous uploads), `VerifyObject`, and `Probe`.
`internal/storage/fake` provides the concurrency-safe in-memory store with
barriers, injected failures, ambiguous accepted writes, and ETag bumps.
`internal/storage/contract` hosts the one shared contract suite.
`internal/s3store/store.go` implements the AWS SDK v2 adapter with
streaming single/multipart uploads, conditional create/replace, list,
delete, and error mapping. `git.OID` gained `MarshalJSON`.

Storage unit tests pass (`go test ./internal/storage/`), including the
golden encoding test and the cross-field validation tables.

Remaining before phase sign-off:

1. `internal/storage/probe_test.go`: probe passes on the fake; probe
   returns `ErrIncompatible` for wrappers that ignore If-None-Match,
   If-Match, or read-after-write.
2. `internal/storage/fake/fake_test.go`: run the contract suite against
   the fake; test barriers (stale-ETag race), `AmbiguousNext` upload
   recovery, injected stream failure, collision (no overwrite), truncated
   object detection, `Bump`, failpoints, and concurrent-safety.
3. `internal/s3store/store_test.go`: prefix/options validation units.
4. `internal/s3store/minio_test.go`: testcontainers MinIO (pinned image
   `minio/minio:RELEASE.2025-09-07T16-13-09Z`), one container per `go
   test` invocation via a package-level `sync.Once`, per-test prefixes;
   contract suite + multipart (threshold 1, part size 5 MiB, body
   5 MiB+1) + prefix-isolation via raw client + concurrent-CAS race +
   skip harness that names Docker. Skip detection: `NewDockerClient` +
   `client.Ping`.
5. Makefile `integration-test` target (fails on `"Action":"skip"` in
   `-json` output) and CI: new `integration` job + `docker pull` step in
   the `native` job so `make native-test` (strict 30s×3 gate) stays fast.
6. Full gates: `gofumpt`, `staticcheck`, `go vet`, `go fix -diff`, `dupl
   -t 80`, `make native-test`, `make qa`.
7. Worklog: mark phase Complete, record decisions below, update the README
   status board and session journal.

Design decisions recorded for this phase (see README Decisions): add AWS
SDK v2 (core v1.43.4, s3 v1.107.0), testcontainers-go v0.44.0, and
google/uuid v1.6.0 as pinned direct dependencies; the object-store
interface takes protocol keys and adapters own the prefix join; ambiguous
uploads are resolved by read-back verification, never by error
classification; the fake and the contract suite live in their own
packages so Phase 10 can reuse them; `Probe` is a storage-policy function
over interface primitives so deliberately broken stores can be tested.

Note on manifest generation semantics: the validator enforces consecutive
active/retained increment generations (skipped generations rejected) but
only the written rules 1–15 otherwise; the architecture's worked example
(active generation 8121 with tail [8120]) is illustrative and would not
round-trip under stricter "last increment == generation" rules, so those
were deliberately not enforced.

### Session 2026-08-09 (imago, worker session 3) — handover state

The tree builds (`go build ./internal/storage/... ./internal/s3store/...`)
and `go test ./internal/storage/` passes. The remaining test files listed
above were not yet written when the session was handed over; dependency
versions are pinned in `go.mod`/`go.sum`.

### Session 2026-08-09 (imago, worker session 4) — phase complete

Completed the phase. Defects found and fixed while writing the missing
suites (each fix is covered by the passing tests):

- `probe_test.go` lived in `package storage` and imported the fake, which
  imports storage: an import cycle. Moved the file to the external test
  package `storage_test`; it exercises only the exported surface.
- The fake's one-shot barrier consumed its channel before blocking, so
  `Unblock` could never release an in-flight operation. Added a `waiting`
  registry: an operation moves the barrier there while blocked, and
  `Unblock` closes the channel from either registry. The stale-ETag race
  test blocks a replace, bumps the ETag, unblocks, and proves the
  precondition failure without touching the bytes.
- The contract suite expected `packs/` LIST results in insertion order;
  S3 LIST returns ascending UTF-8 binary order, so "checkpoints" sorts
  before "increments". Fixed the expectation (the fake already sorted
  correctly). The same suite also wrote bodies with zero-size metadata,
  which the fake accepted but real S3 rejects; the fixtures now carry
  size and SHA-256.
- The adapter mapped MinIO's `NoSuchKey` answer to a conditional PUT on
  a missing key to `ErrNotFound`; the protocol semantics require
  `ErrPreconditionFailed` for a lost CAS (the object was not in the
  observed state). `ReplaceObject` now normalizes that one case.
- `putMultipart` treated `io.ErrUnexpectedEOF` (last partial part) as a
  source failure and aborted; a body of 5 MiB+1 with 5 MiB parts failed
  against real MinIO. A clean end-of-stream now breaks on both `io.EOF`
  and `io.ErrUnexpectedEOF`.
- The pinned MinIO image `RELEASE.2025-10-15T17-29-55Z` does not exist
  on Docker Hub (`docker pull` and the registry API return not-found).
  Re-pinned to `minio/minio:RELEASE.2025-09-07T16-13-09Z`, the newest
  published release (same digest as `latest`); decision recorded in the
  README.
- Staticcheck rejects deprecated `testcontainers.NewDockerClient`; the
  skip harness uses `NewDockerClientWithOpts` (same `Ping` evidence).

New suites and harness pieces:

- `internal/storage/fake/fake_test.go`: shared contract suite against the
  fake; deterministic stale-ETag barrier race; concurrent CAS with
  exactly one winner; ambiguous-upload recovery, never-landed, and
  collision (no overwrite) cases; injected mid-stream source failure;
  truncated-object detection; `Bump`; one-shot failpoints; denied-delete
  leaves state; and a race-detector mixed workload on disjoint keys.
- `internal/s3store/store_test.go`: bucket/prefix/part-size validation,
  upload-strategy defaults, prefix join, semantic error mapping, and the
  metadata header round trip with malformed-value rejection.
- `internal/s3store/minio_test.go`: one pinned MinIO container per `go
  test` invocation (`sync.Once` + `TestMain` termination), per-test
  prefixes, the shared contract suite (versioning disabled by default,
  so the probe proves startup without versioning), multipart upload
  (threshold 1, part size 5 MiB, body 5 MiB+1, ETag `-2` evidence),
  prefix isolation via the raw client, a real-HTTP concurrent-CAS race,
  and a skip harness naming Docker (`NewDockerClientWithOpts` +
  `Ping`); container start failure is a clear local skip.
- Makefile `integration-test` target: `go test -race -json -count=1` on
  `./internal/s3store/`; fails when `"Action":"skip"` appears in the
  JSON output. CI gained an `integration` job running it, and both the
  `validate` and `native` jobs pull the pinned MinIO image first so the
  strict 30 s × 3 gate never pays for an image pull.

Gates (exact commands and results):

- `go test ./internal/storage/... -count=1` → ok (baseline before the
  new suites; the probe file then had the import cycle and was fixed
  first).
- `go test ./internal/storage/... ./internal/s3store/ -race -count=3
  -timeout=120s -p 1` → ok.
- `go test ./... -race -timeout=30s -count=3` (strict native gate) →
  ok, every package; the MinIO suite ran (3 containers, ~4.3 s).
- `make integration-test` → exit 0, no skip events in the JSON output;
  the skip-detection grep was verified against synthetic JSON.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .` → no output.
- `CGO_ENABLED=0 go vet ./...` → ok.
- `CGO_ENABLED=0 go run honnef.co/go/tools/cmd/staticcheck@v0.7.0
  ./...` → ok (after moving off the deprecated Docker client
  constructor).
- `CGO_ENABLED=0 go fix -diff ./...` → clean (applied the Go 1.22+
  loop/append idioms).
- `go run github.com/mibk/dupl@latest -t 80 .` → 0 clone groups.
- `make qa` → all gates pass, including `native-smoke` (the release
  binary starts, reports libgit2 1.9.6, and its only runtime dependency
  is libc).

`go mod tidy` promoted the now-direct imports (aws core/s3/smithy,
`google/uuid`, `testcontainers-go`, `moby/moby/client`, `x/text`) into
one `require` block; versions match the pinned baseline.

## Review findings

### Review 1 (2026-08-09)

- [x] R1-02 (Major): The validator rejected any duplicate ID, but manifest rule
  9.2 permits a repeated publication ID when a checkpoint copies the final
  compacted increment's publication ID. Scoped the rejection to duplicate
  object keys and checkpoint IDs and stated the allowed repetition.
- Verified good: compact JSON encoding rules, field ordering, uint64
  generations, retention chain validation, probe sequence, and the fake's
  conditional-write semantics match architecture sections 9 and 14.
