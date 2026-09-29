# S3 adapter and the test S3 backend

`internal/s3store` is the production `storage.ObjectStore` over the AWS SDK for Go v2 (the other production adapter, used when `SLIVINGDOC_TOKEN` is set, is `internal/httpstore`, [hosted-mode.md](./hosted-mode.md)): it joins the configured prefix, maps S3 conditional writes and errors to the storage sentinels, streams uploads and downloads, and writes the pack metadata headers. `internal/tests3` provides the pinned S3-compatible container (SeaweedFS) that the real-S3 tests run against. This doc answers "how does a protocol operation become an S3 request, and how do tests get a real S3?"

Read this when: changing S3 request shape, addressing, multipart thresholds, error mapping, credential loading, metadata headers, the SeaweedFS pin, or the shared test-container lease.

## Key files

| File | Purpose |
|------|---------|
| `internal/s3store/store.go` | `Store`, `Config` (`Bucket`, `Prefix`, `Region`, `Endpoint`, `AccessKey`, `SecretKey`, test-only `httpClient`, `retryMaxAttempts`), `Options` (`MultipartThreshold`, `MultipartPartSize`, `ForcePathStyle`), `New`, `ReadObject`, `PutObject`, `putSingle`, `putMultipart`, `abortTimeout`, `CreateObject`, `ReplaceObject`, `ListObjects`, `DeleteObjects`, `mapError`, `apiDetail`, `errDetail`, `httpErrorDetail`, `bodySnippet` |
| `internal/s3store/store_test.go` | No-network unit tests: validation, `withDefaults`, key join, `mapError`, non-JSON error pages, `TestAddressingProvesPathStyle`, `TestConfiguredEndpointBeatsServiceEndpointSettings`, `TestMultipartAbortsOnEveryFailure` (recording HTTP client) |
| `internal/storage/metadata.go` | `Metadata.Fields` and `ParseMetadata`, the metadata encoding this adapter shares with `httpstore` (round trip in `internal/storage/metadata_test.go`) |
| `internal/s3store/integration_test.go` | Real-S3 tests: `TestContractSuite` (runs `contract.Run`), `TestMultipartUpload`, `TestPrefixIsolation`, `TestConcurrentCAS`; `TestMain` calls `tests3.Terminate` |
| `internal/tests3/s3.go` | `Image` (`chrislusf/seaweedfs:4.42`), `User`/`Pass`/`Bucket`/`Region`, `EndpointEnv`, `EndpointFileEnv`, `Suite`, `StoreConfig`, `Start`, `Ensure`, `Endpoint`, `Terminate`, `FreshPrefix`, `attach`, `attachFromFile`, `endpointFromFile`, `loopbackEndpoint`, `isLoopbackHost`, `require`, `dockerAvailable`, `startTimeout`, `suiteLabel`, `start`, `createBucket`, `bucketCreated`, `newRawClient` |
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
      s3.NewFromConfig(BaseEndpoint = Endpoint when set; UsePathStyle = Endpoint != "" || ForcePathStyle)

ReadObject(key)     → GetObject(bucket, prefix/key)           → storage.ParseMetadata
PutObject(key,r,m)  → size < threshold ? putSingle (PutObject + m.Fields())
                                       : putMultipart (Create → UploadPart* → Complete; best-effort Abort on part, source-read, length or Complete failure)
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

**Addressing.** A custom endpoint always uses path style (SeaweedFS and similar resolve buckets only that way); `ForcePathStyle` (`--path-style`) requests it for the default AWS endpoint. `TestAddressingProvesPathStyle` proves the request URL shape without a network, and `TestConfiguredEndpointBeatsServiceEndpointSettings` proves a configured endpoint addresses the request even when `AWS_ENDPOINT_URL_S3` or a profile `endpoint_url` names another, or `AWS_IGNORE_CONFIGURED_ENDPOINT_URLS` / `ignore_configured_endpoint_urls` would drop it: `New` sets the endpoint both as a load option and as the client's `BaseEndpoint` ([config.md](./config.md)). The unit tests that inject `httpClient` call `isolateAWSEnv` first, which unsets `AWS_CA_BUNDLE` (the SDK refuses a custom CA bundle with a non-default HTTP client), the profile variables, `AWS_USE_FIPS_ENDPOINT`, `AWS_USE_DUALSTACK_ENDPOINT`, and every `AWS_ENDPOINT_URL*`, and points the config and credentials files at a missing path.

**Credentials.** Production passes no `AccessKey`/`SecretKey`, so `LoadDefaultConfig` uses the AWS default credential chain. Static credentials are used only when both fields are set, which only test code does (values from `tests3.Suite.StoreConfig`); `app.realStoreFactory` never sets them. A credential-resolution failure surfaces through the startup probe as a redacted `S3 storage refused the credentials or the bucket` refusal naming the reason ([config.md](./config.md)), except when a recorded proof in the shared pack cache is reused ([storage.md](./storage.md)): the proof does not bind credentials, so a process whose credentials fail then starts and refuses the first request as `STORAGE_FAILURE`.

**Uploads.** Single PUT below `MultipartThreshold` (default 64 MiB), multipart above it with `MultipartPartSize` (default 16 MiB, minimum 5 MiB, enforced by `withDefaults`). Multipart calls a best-effort `AbortMultipartUpload` (error ignored) on a part, source-read, length-mismatch, or `CompleteMultipartUpload` failure. The abort runs on `context.WithoutCancel(ctx)` bounded by `abortTimeout` (10 s), so a cancelled request still sends it (`TestMultipartAbortsOnEveryFailure`). The call does not return until the abort answers or times out, so a caller whose request was cancelled mid-upload can wait up to 10 s more for its result. An abort after a Complete whose response was lost finds the upload already completed and leaves the object alone. An incomplete upload can still remain when the abort itself fails or the process dies mid-upload (nothing in slivingdoc or the Terraform module removes it; an operator can add an `AbortIncompleteMultipartUpload` lifecycle rule). A body shorter or longer than `Metadata.Size` is `ErrTransport`. Packs are `application/octet-stream`; the manifest and probe objects are `application/json`.

**Conditional writes.** `CreateObject` sends `If-None-Match: *`; `ReplaceObject` sends `If-Match: <etag>`. Both require a non-empty returned ETag, else `ErrTransport`. The pinned SeaweedFS answers a conditional PUT against a missing key with `NoSuchKey` instead of 412, so `ReplaceObject` normalizes `ErrNotFound` to `ErrPreconditionFailed`: the precondition did not hold and nothing was mutated.

**Error mapping (`mapError`).** S3 has no quota refusals of its own, so `ErrQuotaExceeded` and the hosted-mode compaction never apply to S3. A refusal of the credentials or the bucket is `ErrAccessDenied`, a `*storage.Refusal` whose `Detail` names the operation and S3 code and whose `Message` is the service's one-line words (whitespace collapsed, truncated to 200 runes plus `...`; `accessDenied`): the codes in `accessDeniedCodes` (`AccessDenied`, `AllAccessDisabled`, `AccountProblem`, `ExpiredToken`, `InvalidAccessKeyId`, `InvalidToken`, `NoSuchBucket`, `SignatureDoesNotMatch`, `TokenRefreshRequired`), an HTTP 401 or 403 with no code of its own on a read (`deniedStatus`, `isRead`, whether the body decodes or is an HTML page; a write could have been forwarded by a proxy, so its outcome stays unknown, and `RequestTimeTooSkewed` and `RequestExpired` are clock and request problems, `notADenial`), and the SDK's failure to resolve credentials unless a network or server failure caused it (`isCredentialFailure`, `transientCause`). The notebook reports it as `STORAGE_FAILURE`/`ACCESS_DENIED`, not retryable, with the service's words after `The storage says:`; the startup probe reports it as `app: S3 storage refused the credentials or the bucket: ...` rather than `INCOMPATIBLE_STORE` ([cli.md](./cli.md)). API code `NoSuchKey` or `NotFound` wraps `ErrNotFound`; `PreconditionFailed` wraps `ErrPreconditionFailed` and is the only CAS loss; any other conditional-write refusal (for example 409 `ConditionalRequestConflict`) maps to `ErrTransport` and is resolved by the read-back path; everything else (server errors, timeouts, connection errors) wraps `ErrTransport` and keeps a one-line reason: `CODE: message` (`apiDetail`), a status plus a tag-stripped, 160-rune body snippet for non-JSON error pages (`httpErrorDetail`, `bodySnippet`), or the collapsed error text (`errDetail`). The reason reaches only the startup diagnostic and the server log, never a tool result.

**Metadata.** `putSingle` and `putMultipart` send `storage.Metadata.Fields()` as user metadata: `slivingdoc-sha256`, `slivingdoc-size`, `slivingdoc-kind`, `slivingdoc-generation` (the SDK adds `x-amz-meta-`). `ReadObject` decodes them with `storage.ParseMetadata`, which leaves zero metadata when absent and returns an error, wrapped by `ReadObject` as `ErrIntegrity`, when a field is present but malformed ([storage.md](./storage.md)).

**Listing and delete.** `ListObjects` strips `prefix/` from each key so callers get protocol keys to feed back into `DeleteObjects`. `DeleteObjects` sends at most 1,000 keys per request in quiet mode; absent keys are not errors.

**Required permissions.** Objects: `s3:GetObject`, `s3:PutObject` (which IAM also checks for `CreateMultipartUpload`, `UploadPart`, and `CompleteMultipartUpload`; they have no actions of their own), `s3:DeleteObject`, `s3:AbortMultipartUpload`, `s3:ListMultipartUploadParts`. Bucket: `s3:ListBucket`, `s3:ListBucketMultipartUploads`. The Terraform module grants exactly this.

**Test backend (`tests3`).** The contract is the S3 protocol that the probe and suites exercise, not a vendor feature set; SeaweedFS is the current implementation. `Start` runs once per process: it attaches to the endpoint file written by the lease (`EndpointFileEnv`, waiting up to 2 minutes), else to `EndpointEnv`, else pings Docker and starts its own container. `attach` accepts only an `http` loopback URL with an explicit port and no path, so a stray variable cannot point tests at a real bucket. `require` fails the test (never skips) when Docker is unavailable (`TestRequireFailsWhenDockerIsUnavailable`). Docker is reached through `dockerClient` (`docker.go`), a minimal Engine API client over `net/http`: `DOCKER_HOST` (`unix://`, or plain `tcp://` to a loopback host), else the first existing socket among `/var/run/docker.sock`, `$XDG_RUNTIME_DIR/docker.sock` and `~/.docker/run/docker.sock`. The daemon must run on this host: `dockerDaemon` refuses a `tcp://` host that is not loopback (`isLoopbackHost`, the check `loopbackEndpoint` applies to an attached endpoint) and a TLS daemon, because the container's port is always published on and reached at `127.0.0.1` (`publishHost`), so the endpoint the lease hands to attached binaries passes `loopbackEndpoint`. `start` pulls the pinned image when the daemon lacks it (`ensureImage`; `splitImage` takes the tag after the last `:` that follows the final `/`, so a registry port is not a tag), runs it with a static identity (`slivingdoc`/`slivingdoc-secret`), and creates the `slivingdoc` bucket, retrying until the gateway accepts it (`createBucket`, 30 s); `BucketAlreadyOwnedByYou` and `BucketAlreadyExists` both count as created (`bucketCreated`), because only an earlier attempt of this process, whose answer was lost, can have made a bucket in its new, private container. The container's lifetime is its stdin (`container`): it is created with `OpenStdin`, `StdinOnce` and `AutoRemove`, its script serves S3 in the background and then reads stdin to its end, and the owning process holds the attached stdin stream. `AutoRemove` acts only on a container that started, so `run` force-removes any container it could not bring up (`discard`: a lost create answer, a refused attach or start, a failed inspect, no published port) and joins a failed removal into its error (`dockerClient.remove`; a 404 or 409, already gone or being removed, is not a failure). Each container gets a unique `slivingdoc-tests3-<hex>` name (`containerName`) so a create whose answer was lost can be removed by name. That removal can reach the daemon before a create still in flight, so the guarantee is at the next start instead: `start` first calls `removeStale`, which removes every container labelled `org.slivingdoc.tests3` (`suiteLabel`) in the `created` or `exited` state that was created more than `startTimeout` (2 minutes) ago. A live `start` creates and starts its container within its own `startTimeout`, so such a container belongs to no live run; a newer one is left alone, since it may be another process's start in progress. A leftover of this kind therefore lives until the next start that begins at least `startTimeout` after it was created. `Terminate` closes the stdin stream, so the container exits and the daemon removes it; a process that dies without `Terminate` (a test timeout's panic) closes it too, which is what testcontainers' reaper used to provide. `FreshPrefix(ns)` returns `ns/<uuidv7>`, which isolates parallel tests. Attached binaries own nothing.

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
