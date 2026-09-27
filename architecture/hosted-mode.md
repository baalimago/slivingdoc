# Hosted storage mode and the HTTP adapter

A process configured with an API token (`SLIVINGDOC_TOKEN`) stores the notebook through the slivingdoc hosted storage API instead of S3. `internal/httpstore` is the second production `storage.ObjectStore`: the six operations over HTTPS with a bearer token, addressed to one named space: the one space the token reaches, which the process asks the server for when `--bucket` is not given. The object layout, the manifest, and the publication protocol are unchanged; hosted mode is an adapter below the same notebook code, plus a notebook-side compaction that lets a full space shrink. The API contract is `cloud/API.md` in [baalimago/slivingdoc-cloud](https://github.com/baalimago/slivingdoc-cloud) (formerly slivingdoc-web); it names nothing provider-specific. This doc answers "what changes when a token is set, how does an HTTP answer become a notebook error, and what happens when the space is full?"

Read this when: changing hosted-mode selection or its settings, the HTTP request shape, the status-to-error mapping, the startup access check, token handling, retries, quota handling or compaction, the reference gateway, or the hosted scenarios.

## Key files

| File | Purpose |
|------|---------|
| `internal/httpstore/store.go` | `API` (`slivingdoc-storage`), `Version` (1), `Config` (`Endpoint`, `Space`, `Prefix`, `Token`, `UserAgent`, `Client`, `Retries`, `Backoff`), `Doer`, `Store`, `New`, `newClient`, `DescribeToken`, `TokenInfo`, `Access` (`AccessRead`, `AccessWrite`), `ErrTokenLookupUnsupported`, `tokenLookupError`, `ValidateSpace`, `ValidateToken`, `ValidateEndpoint`, `IsLoopback`, `ErrInvalidSpace`, `ErrInvalidToken`, `CheckAccess`, `ReadObject`, `PutObject`, `CreateObject`, `ReplaceObject`, `putSmall`, `ListObjects`, `DeleteObjects`, `objectURL`, `request`, `send`, `do`, `statusError`, `readAPIError`, `newRefusal`, `reasonNoObject`, `unreachable`, `usageNotFound`, `writeError`, `sanitize` |
| `internal/httpstore/gatewaytest/gateway.go` | Test-only reference server of the API over the in-memory fake: `Start`, `URL`, `AddSpace`, `Grant`, `DeleteSpace`, `DisableTokenLookup`, `SetQuota`, `SetPageSize`, `RefuseNext`, `RefuseNextWithReason`, `BeforeNextObject`, `Stored`, `Requests`, `MaxPackBytes`, `MaxSmallBytes`; `operation`, `authorize`, `describeToken` (`GET /v1/token`), `notFound` (the 404 reasons) |
| `internal/httpstore/store_test.go` | Contract suite against the gateway (with and without a prefix), `CheckAccess`, status table, retries, redirects, paging, delete batches, misbehaving servers, request headers, the 404 reasons (`TestRead404MapsByReason`, `TestGateway404Reasons`, `TestGatewayNoSpaceIsOneBody`, `TestCheckAccessUsage404ByReason`, `TestGatewayDescriptionAndListPrefix`), the gateway's `BeforeNextObject` hook (`TestGatewayBeforeNextObject`), and `DescribeToken` (`TestDescribeTokenNamesTheTokensSpace`, `TestDescribeTokenRefusals`, `TestDescribeTokenRefusesInvalidConfig`, `TestDescribeTokenAnswers`) |
| `internal/app/config.go` | `config.hosted`, `DefaultHostedEndpoint`, `validateHosted`, the hosted branch of `Flags.resolve`, `FlagReference` |
| `internal/app/app.go` | `buildService`, `resolveHostedSpace` (the token's space, or the `--bucket` mismatch refusal), `realStoreFactory` (hosted branch builds `httpstore.New`), `accessChecker`, `checkStore`, `hostedCheckError`, the `hosted` field of the `serving` record |
| `internal/storage/store.go` | `ErrQuotaExceeded`, `ErrRequestLimit`, `ErrRateLimited`, `ErrAccessDenied`, `ErrTooLarge`, `Refusal` |
| `internal/storage/metadata.go` | `MetaSHA256`, `MetaSize`, `MetaKind`, `MetaGeneration`, `Metadata.Fields`, `ParseMetadata` (pack metadata as HTTP headers of the same names) |
| `internal/notebook/errors.go` | `storageFailure`, `storeRefusal`; reasons `STORAGE_FULL`, `REQUEST_LIMIT`, `RATE_LIMITED`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE` |
| `internal/notebook/commit.go` | `attemptPublication` (compaction branch), `proposal.compacting`, `buildCompactingProposal`, `acceptCompaction`, `failAfterPublish`, `discardCompaction`, `chainSize`; `publish` wraps a refused manifest write in `errManifestRefused` (`errors.go`) |
| `internal/notebook/compact_test.go` | Compaction unit tests over the fake |
| `internal/notebook/hosted_test.go` | Notebook over `httpstore` and `gatewaytest`: a space that becomes unreachable (grant moved, space deleted) is `ACCESS_DENIED` on pull, first pull and entry recovery, and L is untouched |
| `internal/mcp/errors.go` | `retryable` (per code and reason), `Redact` (`apiTokenRE`) |
| `internal/app/hosted_test.go` | Hosted configuration, refusals, the optional space (`TestLoadConfigHostedSpaceIsOptional`), `resolveHostedSpace` (`TestResolveHostedSpace`), factory, and `checkStore` tests |
| `internal/integrationtest/scenario_hosted_test.go` | CLI and `serve` end to end against the reference gateway, including the token as the whole configuration (`TestScenarioHostedSpaceFromToken`), a server without `GET /v1/token` (`TestScenarioHostedServerWithoutTokenLookup`), the startup refusals (`TestScenarioHostedStartupRefusals`, among them a `--bucket` naming another space), a space that becomes unreachable mid-session (`hostedCuts`), plus entry recovery through the in-process harness over the real hosted adapter |

## Flow

```text
Flags.resolve: SLIVINGDOC_TOKEN non-empty → config.hosted()
  endpoint = --endpoint | SLIVINGDOC_ENDPOINT | DefaultHostedEndpoint   (AWS variables ignored)
  normalizeEndpoint → validateHosted (space grammar when --bucket is given, token grammar, https unless loopback)

app.buildService → resolveHostedSpace → httpstore.DescribeToken
                     GET  /v1/token                 (token)     {space, access, expiresAt}
                     no --bucket          → bucket = space
                     --bucket == space    → keep
                     --bucket != space    → refuse (names both)
                     ErrTokenLookupUnsupported (404 other than no_space, or 405):
                       --bucket given → keep it; none → refuse, asking for --bucket
                     other error          → hostedCheckError
                 → realStoreFactory → httpstore.New(Config{Endpoint, Space: bucket, Prefix, Token,
                                                            UserAgent: "slivingdoc/<version>"})
                 → checkStore: store is an accessChecker → CheckAccess (no storage.Probe)
                     GET  /v1                       (no token)  api, version 1, conditionalWrites
                     GET  /v1/spaces/{space}/usage  (token)     200, else refusal

operations (<space> = <endpoint>/v1/spaces/{space}, keys joined with the prefix):
  ReadObject    GET  <space>/objects/<key>          metadata headers → ParseMetadata
  PutObject     PUT  <space>/objects/<key>          Content-Length, Metadata.Fields headers
  CreateObject  PUT  <space>/objects/<key>          If-None-Match: *
  ReplaceObject PUT  <space>/objects/<key>          If-Match: <etag>
  ListObjects   GET  <space>/objects?prefix=&cursor=   until the cursor is null or empty
  DeleteObjects POST <space>/delete  {"keys": [...]}   batches of 1000
  refusal → statusError / writeError → storage sentinel (+ storage.Refusal)

commit on a full space (notebook, any store):
  uploadProposal(increment) → ErrQuotaExceeded
    → buildCompactingProposal: ExportCheckpoint(head); smaller than every referenced pack?
        no  → STORAGE_FULL
        yes → uploadProposal(checkpoint) → publish (CAS)
                accepted → acceptCompaction: MarkShallow + cleanup(through generation)
                accepted, local accept fails → failAfterPublish: recovery, then cleanup(through generation)
                lost CAS or refused manifest write → discardCompaction (delete the checkpoint) → usual retry/error
```

## Behavior

**Selection and settings.** A non-empty `SLIVINGDOC_TOKEN` selects hosted mode (`config.hosted`); the token has no flag, so it never appears in a process listing.

| Setting             | Flag         | Environment           | Default                      |
| ------------------- | ------------ | --------------------- | ---------------------------- |
| Hosted API token    | none         | `SLIVINGDOC_TOKEN`    | empty (S3 mode)              |
| Space               | `--bucket`   | `SLIVINGDOC_BUCKET`   | the token's own space        |
| Hosted API endpoint | `--endpoint` | `SLIVINGDOC_ENDPOINT` | `https://api.slivingdoc.dev` |
| Prefix in the space | `--prefix`   | `SLIVINGDOC_PREFIX`   | `slivingdoc`                 |

In hosted mode `--bucket` is optional; when given it names the space and must match `httpstore.ValidateSpace` (1 to 63 lowercase letters, digits and inner hyphens). The endpoint is `--endpoint`, else `SLIVINGDOC_ENDPOINT`, else `DefaultHostedEndpoint`; `AWS_ENDPOINT_URL_S3` and `AWS_REGION` are not read, so they can never redirect a token, and the region check is skipped (`--region` is accepted and unused; `--path-style` is still parsed and validated, then unused). The endpoint goes through the same `normalizeEndpoint` as in S3 mode, then `validateHosted` requires `https` unless the host is loopback (`localhost` or a loopback IP, `httpstore.IsLoopback`), and requires the token to be non-empty printable ASCII without white space (`httpstore.ValidateToken`; the refusal names `SLIVINGDOC_TOKEN` and never echoes the value). `httpstore.New` repeats the endpoint, space, token and prefix checks. `Runtime.Serve` logs `hosted=true|false` in its `serving` record.

**Space resolution.** Every token reaches exactly one space, so the token alone is the configuration. Before building the store, `buildService` runs `resolveHostedSpace` within `probeTimeout`: `httpstore.DescribeToken` sends `GET /v1/token` with the token and decodes `{space, access, expiresAt}` into a `TokenInfo`. The server's answer is untrusted: a space outside `ValidateSpace`, an access other than `read` or `write`, an `expiresAt` that is neither `null` nor an RFC 3339 time, or an undecodable body is `ErrIncompatible`. A 404 `no_space` is `ErrAccessDenied` (the token reaches no space: it was made without one, or its space is gone); any other 404, or a 405, is `ErrTokenLookupUnsupported` (a server that predates the endpoint); every other refusal maps through `statusError`, so a 401 or 403 is `ErrAccessDenied`. With no `--bucket`, the returned name becomes `config.bucket`, which then feeds the store, the storage identity and the private-state key exactly as an explicit one does, so adding or dropping `--bucket` for the same space never forks a workspace. A `--bucket` equal to the name is kept; any other is refused with `app: the token reaches hosted space "<space>", not "<bucket>" from --bucket or SLIVINGDOC_BUCKET; ...`, never silently preferring either side. A server without the endpoint keeps a given `--bucket` (the access check below then proves the token reaches it) and otherwise refuses with `app: hosted storage cannot name the token's space; pass the space name as --bucket or SLIVINGDOC_BUCKET`. Other failures go through `hostedCheckError`, the same mapping `checkStore` uses. `DescribeToken` ignores `Config.Space` and `Config.Prefix` and validates only the endpoint and the token. The resolved configuration is what `setup` keeps, so the `serving` record logs the resolved `bucket`.

**Startup check.** A hosted server promises conditional writes in its description, and a read-only token could not run the write probe, so `checkStore` calls `CheckAccess` on any store that implements `accessChecker` instead of `storage.Probe`:

1. `GET /v1` without the token. A 200 answer must decode to `api` `slivingdoc-storage`, `version` 1, and `conditionalWrites` true; anything else, including a non-200 answer or an undecodable body, wraps `ErrIncompatible`, except a 429 or a 5xx, which is a plain check failure. An unreachable server is a plain check failure too. A 404, whatever its reason, is `ErrIncompatible` alone: there is no storage API at this endpoint. Any other non-200 answer also wraps its `statusError` sentinel, so a 401 or 403 here is reported as a token refusal, which `checkStore` tests first.
2. `GET /v1/spaces/{space}/usage` with the token. It is read-only, so a read-only token passes. A 404 is `ErrAccessDenied` whatever its reason (`usageNotFound`): `no_space` says the space does not exist or the token was not granted it, and anything else (`no_endpoint`, no reason, even `no_object`) says the endpoint has no such space API (check `--endpoint`); the gateway's message travels as the refusal's `Message`, as for every other refusal. A 401 or 403 is `ErrAccessDenied` too, through `statusError`; any other non-200 is a plain check failure.

`checkStore` (through `hostedCheckError`) turns `ErrAccessDenied` into `app: hosted storage refused the token: ...; check SLIVINGDOC_TOKEN and --bucket`, `ErrIncompatible` into `app: INCOMPATIBLE_STORE: hosted storage check failed: ...`, and anything else into `app: hosted storage check failed: ...`, each through `mcp.Redact`. All run within `probeTimeout` (30 s) before any transport serves.

**Requests.** Every request carries `Accept-Encoding: identity` (a transparently decompressed body would lose its length) and `User-Agent: slivingdoc/<version>`; every request except `GET /v1` carries `Authorization: Bearer <token>`, the only place the token travels. `objectURL` path-escapes each key segment and percent-encodes a `.` or `..` segment so nothing resolves out of the space. Pack uploads are one streamed `PUT` with an exact `Content-Length` and `application/octet-stream`; the four metadata fields travel as HTTP headers named by `storage.MetaSHA256` and its siblings, and `ReadObject` parses them back with `storage.ParseMetadata` (a malformed field is `ErrIntegrity`). `ReadObject` takes `Size` from `Content-Length` and the ETag from the `ETag` header. `CreateObject`/`ReplaceObject` send the bytes with `If-None-Match: *` / `If-Match`, and a success without an `ETag` header is `ErrTransport`. `ListObjects` joins the prefix, strips it from each returned key, refuses a key outside the requested prefix (`ErrIntegrity`) and a repeated cursor (`ErrTransport`). A transport failure before any response is `ErrTransport` (ambiguous for a write).

**No redirects.** The default client never follows a redirect (`CheckRedirect` returns `http.ErrUseLastResponse`). Followed, a `PUT` would become a body-less `GET` whose success looks like a stored write, and the token would travel with it. A 3xx answer is an unclassified refusal.

**Status mapping.** `statusError` reads at most 4 KiB of the JSON error body and uses its `error` code, `reason` and `message` fields (other fields are ignored). The status or the code picks the category: a `quota_exceeded` code counts as a 507 and a `rate_limited` code as a 429, whatever the status; the reason splits the two quota limits, and a 507 without the `request_limit` reason counts as a full space. Every 404 of the gateway is `not_found` with a reason: `no_object` (the object is absent and the space reachable), `no_space` (no such space for this token: deleted, never granted, or the grant moved) or `no_endpoint` (unknown route). Only a 404 whose raw code is exactly `not_found` and whose raw reason is exactly `no_object` (`reasonNoObject`) is `ErrNotFound`; the lowercased code and reason appear only in the diagnostic detail, so `NOT_FOUND`/`NO_OBJECT` is not an absent object. Every other 404, including one without a body, a code or a reason, is `ErrAccessDenied`, wrapped in a description from `unreachable` (`no_space`: the space does not exist or the token was not granted it; `no_endpoint`: check `--endpoint`; anything else: the server did not say what is missing). This fails closed on purpose: an older gateway that answers 404 without a reason is refused as `ACCESS_DENIED` even for an object that really is absent, so on such a gateway even the first pull of an empty space fails; the alternative, an unreachable space read as an empty notebook, would make a pull delete the caller's notes. `writeError` (used by `PutObject`, `CreateObject`, `ReplaceObject`, `ListObjects`, `DeleteObjects`) also turns a `no_object` 404 into `ErrAccessDenied`, because a request addressed to the space never means an absent object.

| Answer                                          | Storage error           | Notebook reason    | Action     | Retryable |
| ----------------------------------------------- | ----------------------- | ------------------ | ---------- | --------- |
| 507 or `quota_exceeded`, reason `request_limit` | `ErrRequestLimit`       | `REQUEST_LIMIT`    | `OPERATOR` | no        |
| 507 or `quota_exceeded`, any other reason       | `ErrQuotaExceeded`      | `STORAGE_FULL`     | `OPERATOR` | no        |
| 429 or `rate_limited`                           | `ErrRateLimited`        | `RATE_LIMITED`     | `RETRY`    | yes       |
| 401, 403                                        | `ErrAccessDenied`       | `ACCESS_DENIED`    | `OPERATOR` | no        |
| 404 `not_found` with reason `no_object`, on a read | `ErrNotFound`        | (protocol)         |            |           |
| any other 404 on a read (`no_space`, `no_endpoint`, no reason) | `ErrAccessDenied` | `ACCESS_DENIED` | `OPERATOR` | no |
| any 404 on a write, list or delete              | `ErrAccessDenied`       | `ACCESS_DENIED`    | `OPERATOR` | no        |
| 404 on `GET /v1` (startup check)                | `ErrIncompatible`       | (startup)          |            |           |
| 412 (also `If-Match` on an absent object)       | `ErrPreconditionFailed` | (protocol)         |            |           |
| 413                                             | `ErrTooLarge`           | `OBJECT_TOO_LARGE` | `OPERATOR` | no        |
| other 5xx                                       | `ErrTransport`          | (protocol)         |            |           |
| any other status (3xx, 400, 409, 428, ...)      | none (plain error)      | the operation's    | its own    | yes       |

The notebook reasons are `STORAGE_FAILURE` reasons, set by `storeRefusal` inside `storageFailure` (`internal/notebook/errors.go`), which replaces the operation's own reason and message; `retryable` in `internal/mcp/errors.go` makes those four `OPERATOR` reasons non-retryable. The messages of `STORAGE_FULL` and `REQUEST_LIMIT` address the account that owns the space, because the gateway contract bills both limits to the account, shared by all its spaces.

**The server's message.** A classified refusal is a `*storage.Refusal`: `Err` is the sentinel, `Detail` is `HTTP <status> <code> (<reason>)`, and `Message` is the server's `message`, which is written for the person running the client, sanitized to one printable ASCII line cut at 300 bytes (plus `...`). `storageFailure` appends it after `. The storage says: `. Every other piece of server text reaches only diagnostics and logs. No error, log record or tool result carries the token, and `mcp.Redact` also removes anything shaped like an `sld_` token.

**Retries.** `do` retries the idempotent requests (`GET /v1`, the usage check, `GET` of an object, list pages, and the `POST` delete) after a transport failure or a 5xx other than 507, twice by default (`Config.Retries`), with a 200 ms × attempt backoff; a cancelled context stops the loop. Writes (`PutObject`, `CreateObject`, `ReplaceObject`) never retry: their outcome may be ambiguous, and the publication protocol already resolves an ambiguous write. A 429 is not retried inside the adapter; the tool result says `RETRY`.

**Quota.** When a commit would take the account past its storage limit, the pack upload gets 507 before the manifest moves. A commit refused as `STORAGE_FULL` published nothing; the edited files stay in place and pulls keep working.

**Compaction of a full space.** The gateway contract lets checkpoint uploads take the account up to twice its limit, and the notebook uses that so deleting notes can shrink a full space. It is a notebook behavior for any store that refuses with `ErrQuotaExceeded`; S3 never does.

1. The increment upload of an incremental commit is refused with `ErrQuotaExceeded`. (A first publication is already a checkpoint and is refused as `STORAGE_FULL`.)
2. `buildCompactingProposal` exports the same commit as a checkpoint pack of the whole state. If that pack is not smaller than everything the manifest references (the active checkpoint and tail plus every retained generation, `chainSize`), compaction would not shrink the space, and the commit stays refused as `STORAGE_FULL`. Stored bytes are at least that referenced total, so an accepted compaction whose cleanup completes shrinks the space, and the 2x checkpoint margin never lifts the limit.
3. Otherwise it uploads the checkpoint, through the generation the increment would have had, carrying the commit's own publication ID, and publishes a manifest whose active chain is that checkpoint alone. It keeps no retained generation, whatever `--retained-checkpoints` says: the retained packs are the bytes the space needs back, and readers already restart when a pack disappears (`readRemote`). A refusal of this upload is reported as usual (for example `STORAGE_FULL`).
4. After the manifest CAS accepts it (directly or through `lookupPublication`), `acceptCompaction` counts a checkpoint run, records the shallow boundary (`git.MarkShallow`), and runs the normal `cleanup` with the checkpoint's generation as the cutoff, which deletes every pack the manifest no longer references. A failure to record the boundary is logged and does not skip the cleanup. When the local acceptance fails after the CAS (`Workspace.Accept`, or the `Failpoints.CAS` hook), `failAfterPublish` returns the usual `RECOVERY_FAILURE` (`failAfterAccept`) and still runs the same `cleanup`: the replaced chain is unreferenced whatever happens locally, and the recovery's resynchronizing read records the boundary. That cleanup is best effort and never changes the result.

When the publication fails with a lost CAS (`errCASLost`) or a manifest write the store answered with a refusal, `discardCompaction` deletes its own checkpoint pack at once (best effort): an orphan that large could otherwise leave no room under the 2x margin for the next compaction. `publish` marks every such write, from a plain failure (`MANIFEST_WRITE`) to a classified store refusal whose reason `storageFailure` replaces (`RATE_LIMITED`, `STORAGE_FULL`, `REQUEST_LIMIT`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE`), with `errManifestRefused`, and `discardCompaction` tests that sentinel rather than the reason. On every other error the pack stays, because the manifest may have accepted it or the failure says nothing about the store: a lost response (`ErrTransport`) whose follow-up read failed or was refused, or whose follow-up read succeeded without finding the publication ID (`PUBLICATION_UNPROVEN`: a later writer may have replaced the accepted manifest), and an `EncodeManifest` failure before any write (`ENGINE_FAILED`), which leaves the pack to a later cleanup. A lost CAS then retries the whole commit as usual.

Some failures leave dead bytes that only a later checkpoint cleanup removes: a kept pack of unknown acceptance, a compaction upload whose response was lost, and a cleanup that fails after an accepted compaction. Until then the space can stay `STORAGE_FULL` even for a commit that deletes notes.

Because the compacted manifest keeps no retained generation, another writer's publication whose CAS response was lost just before the compaction can no longer be found by its publication ID, and that writer reports `PUBLICATION_UNPROVEN` for a commit that was accepted. The protocol already allows this once descriptors expire; compaction makes the window immediate.

**Request allowance.** Per the gateway contract, when the account uses its monthly request allowance, uploads, creates and replaces get `507 quota_exceeded` with reason `request_limit`; reads, lists and deletes continue slowly, then get `429 rate_limited` with reason `slow_reads` and a `Retry-After` header, which the client does not read. The allowance resets on the first of the month, UTC, which the `REQUEST_LIMIT` message says.

**Limits the client knows about.** There is no multipart upload; a pack larger than the gateway's `maxPackBytes` is refused with 413 and fails as `OBJECT_TOO_LARGE`. The client does not read the advertised limits. Listing is only allowed below `packs/`, which is all `cleanup` lists; the contract suite therefore checks the probe's cleanup by reading back the keys the probe created (`createRecorder`), not by listing.

**Reference gateway.** `gatewaytest` serves the API over one `fake` store per space: the description at `/v1` and `/v1/`, the object-key grammar and the list prefix (`[<prefix>/]packs/...`, `validListPrefix`) checked before authentication (400 `invalid_key`), the gateway's routing (an unknown route is 404 `no_endpoint`, a known route with another method 405 `method_not_allowed`, an invalid space name 404 `no_space`), bearer tokens granted per space (401 for an unknown token, 404 `no_space` for a space that does not exist, was not granted, has an invalid name, or was deleted with `DeleteSpace`, always with a body byte-identical to every other `no_space` of the same gateway, so names cannot be probed; the JSON key order differs from the real gateway's, so the bodies of the two are not byte-identical to each other), `GET /v1/token` (the token's granted space, `access` `read` or `write`, `expiresAt` null; 401 for an unknown token, 404 `no_space` when its space is gone; `DisableTokenLookup` makes it 404 `no_endpoint`, like a server that predates it), a missing object 404 `no_object` only after the token and grant checks passed, any other backend failure 502 `backend_unavailable` (`ErrTransport`), read-only grants (403 on any non-`GET`), pack `PUT`s with exact metadata and length, a per-space quota that a checkpoint pack may exceed up to twice (507 `quota_exceeded`/`storage_full`), `MaxPackBytes` and `MaxSmallBytes` (413), conditional small writes (428 without a condition), cursor listing below `packs/` only, batched deletes of at most 1000 keys, one-shot injected refusals (`RefuseNext`, `RefuseNextWithReason`) whose `message` is `refused: <code>`, plus ` (<reason>)` when a reason is given, and one-shot hooks (`BeforeNextObject`) that run a function just before the gateway serves the next request with a given method for a given full object key, so a test can delete a space or move a grant between a one-shot command's startup access check and its read of `current`. Its 404s carry the gateway's own messages (`no such object`, `no such space for this token`, `no such endpoint`).

## Gotchas

- `--endpoint` is shared with S3 mode: an S3 command line that passes `--endpoint` sends the token to that host if `SLIVINGDOC_TOKEN` is also set in the environment. Only the AWS variables are ignored.
- `ReplaceObject` relies on the API answering `If-Match` on an absent object with 412; a 404 there means the space is gone or the grant was revoked (`ErrAccessDenied`), never CAS contention.
- A 507 must never be retried and never become `ErrTransport`: `do` excludes it explicitly, and the compaction branch matches `ErrQuotaExceeded` only.
- A space that stops being reachable while `serve` runs (deleted, or the grant revoked or moved) answers 404 `no_space`, which is `ACCESS_DENIED` on every read: a pull, a first pull, or an entry recovery is refused with L and P untouched (`TestHostedUnreachableSpacePullKeepsNotes`, `TestHostedUnreachableSpaceFirstPullKeepsFiles`, `TestHostedUnreachableSpaceEntryRecoveryKeepsNotes`). The black-box scenarios pin the same contract through the public entries, with the notebook directory byte-identical: a `serve` process whose `notes_pull` and `notes_commit` are refused after `DeleteSpace` or a moved grant (`TestScenarioHostedSpaceGoneMidSession`), a one-shot `pull` whose space goes between the access check and the read of `current` (`TestScenarioHostedSpaceGoneDuringOneShotPull`), and a failed replacement followed by entry recovery, both `ACCESS_DENIED` and not resynchronized until the grant is back (`TestScenarioHostedSpaceGoneEntryRecovery`); `CheckAccess` alone could only catch it at startup. Do not loosen the `no_object` test: `readCurrent` reads `ErrNotFound` on `current` as the empty notebook.
- A store refusal can also stop a recovery: when the resynchronizing `readRemote` of `entryRecovery`, `applyLocal` or `failAfterAccept` is refused (a revoked token, throttling), the call is still `RECOVERY_FAILURE` with `resynchronized=false`, but it carries the refusal's reason and action and a recovery message that makes no claim about publication (`recoveryFailure`, `recoveryRefusalMessages`), and it is not retryable unless the reason is `RATE_LIMITED` ([guarantees.md](./guarantees.md), [errors.md](./errors.md)).
- `internal/integrationtest` drops `SLIVINGDOC_TOKEN` and `SLIVINGDOC_ENDPOINT` from spawned helpers (`sanitizedEnv`), so a developer's token never turns an S3 scenario into a hosted one.
- A custom `Config.Client` bypasses the no-redirect client; only tests set it.

## Related

- [storage.md](./storage.md): the `ObjectStore` seam, the semantic errors, the metadata fields, and the contract suite.
- [s3store.md](./s3store.md): the other production adapter.
- [config.md](./config.md) and [running.md](./running.md): every flag and variable, including the hosted ones.
- [commit.md](./commit.md) and [checkpoints.md](./checkpoints.md): publication, cleanup and shallow history that compaction reuses.
- [errors.md](./errors.md): how the store refusals become tool results.
- [security.md](./security.md): token handling and redaction.
- [testing.md](./testing.md): where the gateway, the contract run and the hosted scenarios sit.
