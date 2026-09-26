# S3 adapter and the test S3 backend

`internal/s3store` is the production `storage.ObjectStore` over the AWS SDK for Go v2: it joins the configured prefix, maps S3 conditional writes and errors to the storage sentinels, streams uploads and downloads, and writes the pack metadata headers. `internal/tests3` provides the pinned S3-compatible container (SeaweedFS) that the real-S3 tests run against. This doc answers "how does a protocol operation become an S3 request, and how do tests get a real S3?"

Read this when: changing S3 request shape, addressing, multipart thresholds, error mapping, credential loading, metadata headers, the SeaweedFS pin, or the shared test-container lease.

## Key files

| File | Purpose |
|------|---------|
| `internal/s3store/store.go` | `Store`, `Config` (`Bucket`, `Prefix`, `Region`, `Endpoint`, `AccessKey`, `SecretKey`, test-only `httpClient`, `retryMaxAttempts`), `Options` (`MultipartThreshold`, `MultipartPartSize`, `ForcePathStyle`), `New`, `ReadObject`, `PutObject`, `putSingle`, `putMultipart`, `CreateObject`, `ReplaceObject`, `ListObjects`, `DeleteObjects`, `mapError`, `apiDetail`, `errDetail`, `httpErrorDetail`, `bodySnippet`, `encodeMeta`, `decodeMeta` |
| `internal/s3store/store_test.go` | No-network unit tests: validation, `withDefaults`, key join, `mapError`, non-JSON error pages, metadata round trip, `TestAddressingProvesPathStyle` (recording HTTP client) |
| `internal/s3store/integration_test.go` | Real-S3 tests: `TestContractSuite` (runs `contract.Run`), `TestMultipartUpload`, `TestPrefixIsolation`, `TestConcurrentCAS`; `TestMain` calls `tests3.Terminate` |
| `internal/tests3/s3.go` | `Image` (`chrislusf/seaweedfs:4.42`), `User`/`Pass`/`Bucket`/`Region`, `EndpointEnv`, `EndpointFileEnv`, `Suite`, `StoreConfig`, `Start`, `Ensure`, `Endpoint`, `Terminate`, `FreshPrefix`, `attach`, `attachFromFile`, `endpointFromFile`, `loopbackEndpoint`, `require`, `dockerAvailable`, `start`, `newRawClient` |
| `internal/tests3/lease/main.go` | `tests3-lease`: owns one container for the whole `make test` run and writes its endpoint to `--ready-file` |
| `internal/app/app.go` | `realStoreFactory`: the only production caller of `s3store.New` |
| `examples/seaweedfs/` | Compose file and README for a local SeaweedFS to run against by hand |
| `terraform/` | Module that creates the bucket (public-access block, deny-all-except-user bucket policy) and a scoped IAM user plus access key whose policy is the one in [running.md, S3 requirements](./running.md#s3-requirements) |

## Flow

```text
app.realStoreFactory(cfg)
  → s3store.New(ctx, Config{Bucket, Prefix, Region, Endpoint}, Options{ForcePathStyle})
      validate bucket, storage.ValidatePrefix, Options.withDefaults
      awsconfig.LoadDefaultConfig(WithRegion, WithBaseEndpoint, [static creds])
      s3.NewFromConfig(UsePathStyle = Endpoint != "" || ForcePathStyle)

ReadObject(key)     → GetObject(bucket, prefix/key)           → decodeMeta
PutObject(key,r,m)  → size < threshold ? putSingle (PutObject + metadata)
                                       : putMultipart (Create → UploadPart* → Complete; best-effort Abort on part, source-read or length failure)
CreateObject(key)   → PutObject(IfNoneMatch: "*", application/json)
ReplaceObject(k,e)  → PutObject(IfMatch: e, application/json); NoSuchKey → ErrPreconditionFailed
ListObjects(p, fn)  → ListObjectsV2(prefix/p) with continuation → fn(key without prefix)
DeleteObjects(keys) → DeleteObjects in batches of 1000, Quiet; any per-key error → ErrTransport
all errors          → mapError(op, err)

tests:  make test → tests3-lease --ready-file F → SLIVINGDOC_TESTS3_ENDPOINT_FILE=F go test ...
        test binary → tests3.Start → attachFromFile(F) | attach(SLIVINGDOC_TESTS3_ENDPOINT) | start()
        test → tests3.Ensure(t) → Suite.FreshPrefix(ns) → s3store.New(... StoreConfig creds)
```

## Behavior

**Boundary.** No AWS SDK type crosses `internal/s3store`; `Config` holds plain strings, and every returned error wraps a `storage` sentinel. All AWS SDK use in production stays in this package (an AGENTS invariant). The adapter never creates or configures the bucket.

**Addressing.** A custom endpoint always uses path style (SeaweedFS and similar resolve buckets only that way); `ForcePathStyle` (`--path-style`) requests it for the default AWS endpoint. `TestAddressingProvesPathStyle` proves the request URL shape without a network.

**Credentials.** Production passes no `AccessKey`/`SecretKey`, so `LoadDefaultConfig` uses the AWS default credential chain. Static credentials are used only when both fields are set, which only test code does (values from `tests3.Suite.StoreConfig`); `app.realStoreFactory` never sets them. A credential-resolution failure surfaces through the startup probe as a redacted `INCOMPATIBLE_STORE` refusal naming the reason ([config.md](./config.md)).

**Uploads.** Single PUT below `MultipartThreshold` (default 64 MiB), multipart above it with `MultipartPartSize` (default 16 MiB, minimum 5 MiB, enforced by `withDefaults`). Multipart calls a best-effort `AbortMultipartUpload` (same context, error ignored) on a part, source-read or length-mismatch failure; a `CompleteMultipartUpload` failure is not aborted, and a cancelled context makes the abort fail, so an incomplete upload can remain (nothing in slivingdoc or the Terraform module removes it; an operator can add an `AbortIncompleteMultipartUpload` lifecycle rule). A body shorter or longer than `Metadata.Size` is `ErrTransport`. Packs are `application/octet-stream`; the manifest and probe objects are `application/json`.

**Conditional writes.** `CreateObject` sends `If-None-Match: *`; `ReplaceObject` sends `If-Match: <etag>`. Both require a non-empty returned ETag, else `ErrTransport`. The pinned SeaweedFS answers a conditional PUT against a missing key with `NoSuchKey` instead of 412, so `ReplaceObject` normalizes `ErrNotFound` to `ErrPreconditionFailed`: the precondition did not hold and nothing was mutated.

**Error mapping (`mapError`).** API code `NoSuchKey` or `NotFound` wraps `ErrNotFound`; `PreconditionFailed` wraps `ErrPreconditionFailed` and is the only CAS loss; any other conditional-write refusal (for example 409 `ConditionalRequestConflict`) maps to `ErrTransport` and is resolved by the read-back path; everything else (access denied, server errors, timeouts, connection errors, credential failures) wraps `ErrTransport` and keeps a one-line reason: `CODE: message` (`apiDetail`), a status plus a tag-stripped, 160-rune body snippet for non-JSON error pages (`httpErrorDetail`, `bodySnippet`), or the collapsed error text (`errDetail`). The reason reaches only the startup diagnostic and the server log, never a tool result.

**Metadata.** `encodeMeta` writes `slivingdoc-sha256`, `slivingdoc-size`, `slivingdoc-kind`, `slivingdoc-generation` (the SDK adds `x-amz-meta-`). `decodeMeta` leaves zero metadata when absent and returns an error, wrapped by `ReadObject` as `ErrIntegrity`, when a header is present but malformed.

**Listing and delete.** `ListObjects` strips `prefix/` from each key so callers get protocol keys to feed back into `DeleteObjects`. `DeleteObjects` sends at most 1,000 keys per request in quiet mode; absent keys are not errors.

**Required permissions.** Objects: `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`, `s3:CreateMultipartUpload`, `s3:UploadPart`, `s3:CompleteMultipartUpload`, `s3:AbortMultipartUpload`, `s3:ListMultipartUploadParts`. Bucket: `s3:ListBucket`, `s3:ListBucketMultipartUploads`. The Terraform module grants exactly this.

**Test backend (`tests3`).** The contract is the S3 protocol that the probe and suites exercise, not a vendor feature set; SeaweedFS is the current implementation. `Start` runs once per process: it attaches to the endpoint file written by the lease (`EndpointFileEnv`, waiting up to 2 minutes), else to `EndpointEnv`, else pings Docker and starts its own container. `attach` accepts only an `http` loopback URL with an explicit port and no path, so a stray variable cannot point tests at a real bucket. `require` fails the test (never skips) when Docker is unavailable (`TestRequireFailsWhenDockerIsUnavailable`). `start` runs the pinned image with a static identity (`slivingdoc`/`slivingdoc-secret`), waits for the S3 log line, and creates the `slivingdoc` bucket. `FreshPrefix(ns)` returns `ns/<uuidv7>`, which isolates parallel tests. `Terminate` stops an owned container asynchronously (Ryuk reaps it otherwise); attached binaries own nothing.

**Lease.** `internal/tests3/lease` calls `tests3.Endpoint`, atomically writes the endpoint (or `error: <msg>`) to `--ready-file`, then waits for SIGINT and terminates the container. The Makefile `test` target starts it in the background and kills it on exit ([testing.md](./testing.md)).

## Gotchas

- The SeaweedFS image is pinned in two places: `tests3.Image` and `S3_IMAGE` (plus the `prerun-step-cmd`) in `.github/workflows/ci.yml`. Change both.
- `Config.httpClient` and `retryMaxAttempts` are unexported test seams; production cannot set them.
- A `ReadObject` metadata error closes the body before returning; any new early return must do the same.
- `mapError` must never downgrade a semantic code to `ErrTransport` or upgrade a transport failure to a precondition failure; the CAS and ambiguity logic depend on the distinction.
- Bucket versioning, replication, object lock and lifecycle rules are deployment policy; nothing here depends on them.

## Related

- [storage.md](./storage.md): the interface, protocol, contract suite and fake.
- [config.md](./config.md): where bucket, prefix, region, endpoint and path style come from.
- [testing.md](./testing.md): how the real-S3 suites fit the test layers.
- [running.md, S3 requirements](./running.md#s3-requirements).
