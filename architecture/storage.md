# Storage protocol and the ObjectStore seam

The S3 storage model and its Go boundary. `internal/storage` owns the object layout, the protocol key grammar, the versioned `current` manifest, pack integrity values, the startup compatibility probe, the ambiguity-resolving upload, and the `ObjectStore` interface every backend implements. It answers "what is stored where, what makes stored state valid, and what must a new backend guarantee?"

Read this when: implementing a new `ObjectStore` backend (this is the seam; a hosted backend is in progress in a separate PR), changing the manifest schema or its validation, the key grammar, pack metadata, the probe, the upload retry rule, the contract suite, or the in-memory fake.

## Key files

| File | Purpose |
|------|---------|
| `internal/storage/store.go` | `ObjectStore` interface; sentinels `ErrNotFound`, `ErrPreconditionFailed`, `ErrTransport`, `ErrIncompatible`; `ETag`, `Metadata`, `ObjectInfo` |
| `internal/storage/key.go` | `PackKind` (`KindCheckpoint`, `KindIncrement`), `Key` + `String`/`MarshalJSON`, `ParseKey`, `ValidatePrefix`, `JoinKey`, `ErrInvalidKey`, `ErrInvalidPrefix` |
| `internal/storage/manifest.go` | `CurrentKey`, `Manifest`, `Checkpoint`, `Increment`, `Retained`, `DecodeManifest`, `EncodeManifest`, `validateManifest`, `checkDescriptorKey`, `ErrIntegrity` |
| `internal/storage/probe.go` | `Probe` (startup compatibility proof) |
| `internal/storage/upload.go` | `UploadUnique` (ambiguous-put resolution), `VerifyObject` (streamed size + SHA-256 proof) |
| `internal/storage/sha256.go` | `SHA256`, `ParseSHA256` (canonical lowercase 64-hex), `ErrInvalidSHA256` |
| `internal/storage/uuid.go` | `UUID`, `NewUUIDv7`, `ParseUUIDv7` (canonical lowercase v7, RFC 4122 variant), `ErrInvalidUUID` |
| `internal/strictjson/json.go` | `Parse`, `Value`, `Field`, `RejectUnknown` (strict tree shared with the workspace state record) |
| `internal/storage/contract/suite.go` | `contract.Run(t, Factory)`: the behavioral contract every backend must pass |
| `internal/storage/fake/fake.go` | `fake.New(prefix)`: in-memory `Store` with real conditional-write semantics; `Op`, `FailNext`, `FailNextKey`, `AmbiguousNext`, `BlockNext`, `Unblock`, `Waiting`, `Bump`, `Calls`, `ObjectCount`, `etagFor` |
| `internal/storage/fake/inject.go` | `Injector` (shared fault engine: one-shot and permanent failures by key or prefix, accept-then-error ambiguity, barriers with a bounded wait), `ErrBarrierTimeout` |
| `internal/s3store/store.go` | The production AWS implementation ([s3store.md](./s3store.md)) |

Consumers: `internal/notebook/remote.go` (`readCurrent`, `readRemote`), `internal/notebook/commit.go` (`uploadProposal`, `publish`), `internal/notebook/checkpoint.go` (`runCheckpoint`, `cleanup`), and `internal/app/app.go` (`buildService` runs `Probe`).

## Flow

```text
startup:  app.buildService → storeFactory → storage.Probe(ctx, store)
            CreateObject(probe/<uuid>) → CreateObject again (want ErrPreconditionFailed)
            → ReadObject (bytes + ETag) → ReplaceObject("wrong-etag") (want ErrPreconditionFailed)
            → ReadObject (unchanged) → ReplaceObject(etag) (new ETag) → ReadObject (new bytes)
            → defer DeleteObjects([probe/<uuid>])

pull:     notebook.readRemote → readCurrent → store.ReadObject("current")
            ErrNotFound → generation 0 (empty notebook)
          → storage.DecodeManifest → download each descriptor's pack → size + SHA-256 check

commit:   notebook.uploadProposal → storage.UploadUnique(store, key, pack, meta)
            PutObject → on ErrTransport: VerifyObject (streamed GET) decides landed / absent / corrupt
          notebook.publish → storage.EncodeManifest
            → CreateObject("current")            # generation 1, If-None-Match: *
            → ReplaceObject("current", etag)     # later generations, If-Match
              ErrPreconditionFailed → merge and retry (see commit.md)

checkpoint: runCheckpoint → UploadUnique(checkpoint pack) → ReplaceObject("current", etag)
cleanup:    ListObjects(packs/...) → ParseKey → DeleteObjects(unreferenced keys at or before cutoff)
```

## Behavior

**Object layout.** One configured prefix holds one notebook:

```text
<prefix>/current
<prefix>/packs/checkpoints/<through-generation>-<checkpoint-id>.pack
<prefix>/packs/increments/<generation>-<publication-id>.pack
```

S3 holds no bare repository and no `.git` directory. Protocol keys are relative to the prefix; the backend joins them with one slash (`JoinKey`), so the notebook layer never sees the prefix. The prefix grammar is enforced by `ValidatePrefix` (empty, or relative segments without leading/trailing slash, empty segment, backslash, `.` or `..`).

**Key grammar.** `Key{Kind, Generation, ID}` renders as above. `ParseKey` rejects any other namespace, a non-canonical generation (leading zeros), a malformed layout, and an ID that is not a canonical UUIDv7. Every pack key exposes its generation, which is what bounded cleanup filters on. Generation fields, not UUID ordering, define protocol order. One writer owns each key and accepted pack bytes never change.

**IDs and digests.** Publication and checkpoint IDs are UUIDv7 in canonical lowercase `8-4-4-4-12` form with the RFC 4122 variant (`NewUUIDv7`, `ParseUUIDv7`). Pack checksums are the SHA-256 of the complete pack in canonical lowercase 64-hex (`ParseSHA256`). Git object IDs are SHA-1, 40 lowercase hex (`git.OID`). An ETag is only a concurrency token for `current`, never a content digest; multipart ETags are not digests either.

**Pack metadata.** Every pack upload carries `Metadata{SHA256, Size, Kind, Generation}`, written by the backend as user metadata (`slivingdoc-sha256`, `slivingdoc-size`, `slivingdoc-kind`, `slivingdoc-generation` in S3). Metadata diagnoses and resumes uploads; the manifest descriptor is authoritative, and metadata alone never proves an object.

**`current` manifest.** `current` is the only authoritative state index. An absent `current` is the empty notebook at generation 0; the first successful `CreateObject` writes generation 1; every successful replacement, including a checkpoint, increments the generation by exactly one. Generations never reset; a generation that wrapped to 0 fails `validateManifest` on encode, so the operation is rejected. Correctness never uses `ListObjects` to discover accepted state. Version 1 shape (field order is normative and is the struct order in `manifest.go`):

```json
{
  "version": 1, "generation": 8121, "head": "<git-oid>",
  "checkpoint": {"id": "<uuid>", "publication": "<uuid>", "throughGeneration": 8119,
                 "head": "<git-oid>", "key": "packs/checkpoints/8119-<uuid>.pack",
                 "sha256": "<hex64>", "size": 123456},
  "increments": [{"generation": 8120, "publication": "<uuid>", "parent": "<git-oid>",
                  "head": "<git-oid>", "key": "packs/increments/8120-<uuid>.pack",
                  "sha256": "<hex64>", "size": 4096}],
  "retained": [{"retiredAtGeneration": 8121, "head": "<git-oid>",
                "checkpoint": {...}, "increments": []}]
}
```

**Decode.** `DecodeManifest` builds a `strictjson` tree first, so duplicate names, explicit `null`, unknown fields, missing fields and non-`uint64` numbers are rejected before any Go value is decoded. `version` must be 1; a future schema uses another value and is rejected before any pack is touched. Every failure wraps `ErrIntegrity`, which the notebook maps to `STORAGE_INTEGRITY`/`MANIFEST_INVALID`.

**Validation (`validateManifest`).** Applied on decode and on encode:

1. `generation` is at least 1.
2. Every pack `size` is positive.
3. Every object key and checkpoint ID is unique across the manifest.
4. The active checkpoint `throughGeneration` is not greater than `generation`.
5. Increment generations are consecutive and start at checkpoint cutoff + 1.
6. The first increment `parent` equals the checkpoint `head`; each later `parent` equals the preceding `head`.
7. Top-level `head` equals the final tail head (the checkpoint head for an empty tail).
8. Each descriptor key names the right kind and binds its own generation and ID (`checkDescriptorKey`).
9. Each retained entry validates as an independent checkpoint + tail chain with its own `head`.
10. `retained` is ordered by strictly decreasing `retiredAtGeneration`, each at most `generation` and greater than its final content generation.
11. A publication ID may repeat across chains only when every descriptor binds it to the same commit head (a checkpoint copies the publication ID of the increment it compacted). This is what lets an ambiguous commit search the active and retained descriptors as publication receipts.

**Encode.** `EncodeManifest` replaces a nil `increments`, `retained`, or nested `retained[i].increments` with an empty slice (copying rather than modifying the caller's slices), so every tail encodes as `[]`, validates, and writes compact JSON with HTML escaping disabled and no trailing newline. Protocol objects are structs, never maps, so field order is stable.

**Pack integrity.** The notebook verifies size and SHA-256 of every downloaded pack against its descriptor before import (`notebook/remote.go`); a mismatch is a storage-integrity error and nothing is advanced or materialized. Checkpoint packs are closed for state reconstruction (commit, trees and blobs, no delta base outside the pack); the checkpoint commit may name an omitted parent, which is the declared shallow boundary. An increment pack may omit objects present in its base chain; import fails if a required object is missing. See [checkpoints.md](./checkpoints.md) and [git-engine.md](./git-engine.md).

**Ambiguous uploads.** `UploadUnique` first rejects an invalid key kind with `ErrIntegrity` before any PUT, then calls `PutObject`. On `ErrTransport` it re-reads the unique key with `VerifyObject`: matching bytes mean the response was lost after acceptance (success); an absent key or a failed read-back means acceptance is unproven (`ErrTransport`); different bytes mean a collision (`ErrIntegrity`, never overwritten). Any other `PutObject` error is returned wrapped.

**Probe.** `Probe` uses a fresh `probe/<uuidv7>` key and proves `If-None-Match: *` (second create fails), read-after-create with a non-empty ETag, `If-Match` (a wrong ETag fails without mutation), a matching replace yields a new ETag, and immediate read-after-write. Any deviation returns `ErrIncompatible`; an operational failure wraps both `ErrIncompatible` and its cause, so the startup diagnostic names the real reason. The key is deleted on every path (`defer`), and a denied delete does not fail startup. Bucket versioning is not required.

## Implementing a new backend

A backend is any type satisfying `storage.ObjectStore`, safe for concurrent use, that owns its prefix join. It must:

- `ReadObject`: stream bytes and return `ObjectInfo{Size, ETag, Meta}`; wrap `ErrNotFound` for an absent key.
- `PutObject`: store immutable bytes with `Metadata`, streaming; a failure after bytes may have been accepted wraps `ErrTransport`.
- `CreateObject`: create only if absent; wrap `ErrPreconditionFailed` if the key exists; return a non-empty ETag.
- `ReplaceObject`: replace only if the ETag matches; wrap `ErrPreconditionFailed` for a stale ETag and for an absent key, without mutating; return a new non-empty ETag.
- `ListObjects`: call `fn` with protocol keys (prefix stripped) for every object under the protocol prefix, following continuations; stop on `fn` error.
- `DeleteObjects`: delete in bounded batches; an absent key is not an error; a partial failure wraps `ErrTransport`.
- Map every other failure to `ErrTransport` and keep no SDK or HTTP type in the returned errors' public API.

Prove it by calling `contract.Run(t, factory)` from the backend's tests with a fresh isolated prefix per subtest, as `internal/storage/fake/fake_test.go` and `internal/s3store/integration_test.go` do. The suite covers conditional create, conditional replace, stale replace without mutation, replace of a missing key, read of a missing key, put/read round trip with metadata, `VerifyObject`, `UploadUnique`, list filtering and key form, delete tolerance, and `Probe`. Then wire it into `app.realStoreFactory` (or a sibling factory) so `app.Setup` probes it.

## Gotchas

- `ReplaceObject` on a missing key must be `ErrPreconditionFailed`, not `ErrNotFound`; the CAS loop treats only the former as contention. `s3store` normalizes this explicitly.
- A timeout after request bytes were sent is ambiguous (`ErrTransport`), never a precondition failure. The notebook resolves ambiguous manifest writes by reading `current` back (see [commit.md](./commit.md)).
- `Key.String()` returns `""` for an invalid kind; always build keys from validated descriptors or `ParseKey`.
- The fake's ETag is the quoted MD5 of the bytes, like a single-part S3 PUT. Tests must not depend on that; it is a token.
- `fake.Store.DeleteObjects` barriers and failures key on `""`, not on the deleted keys.
- `internal/integrationtest/faults.go` (`faultStore`) reuses `fake.Injector` around any base store, including the real S3 adapter ([testing.md](./testing.md)).

## Related

- [s3store.md](./s3store.md): the AWS adapter and its S3 specifics.
- [commit.md](./commit.md), [pull.md](./pull.md), [checkpoints.md](./checkpoints.md): the notebook side of the protocol.
- [errors.md](./errors.md): how storage sentinels become notebook codes.
- [testing.md](./testing.md): contract suite and fault layers.
