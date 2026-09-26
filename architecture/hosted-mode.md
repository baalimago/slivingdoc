# Hosted mode

A process configured with an API token (`SLIVINGDOC_TOKEN`) stores the
notebook through the slivingdoc hosted storage API instead of S3. The API is
the six `storage.ObjectStore` operations over HTTPS with a bearer token,
addressed to one named space. Its contract is `cloud/API.md` in the
baalimago/slivingdoc-web repository (being renamed to slivingdoc-cloud); it
names nothing provider-specific. The object layout, the manifest, and the publication protocol are unchanged: hosted mode
is a second adapter, `internal/httpstore`, below the same notebook code.

## Configuration

| Setting             | Flag         | Environment           | Default                      |
| ------------------- | ------------ | --------------------- | ---------------------------- |
| Hosted API token    | none         | `SLIVINGDOC_TOKEN`    | empty (S3 mode)              |
| Space               | `--bucket`   | `SLIVINGDOC_BUCKET`   | required                     |
| Hosted API endpoint | `--endpoint` | `SLIVINGDOC_ENDPOINT` | `https://api.slivingdoc.dev` |
| Prefix in the space | `--prefix`   | `SLIVINGDOC_PREFIX`   | `slivingdoc`                 |

A non-empty `SLIVINGDOC_TOKEN` selects hosted mode (`config.hosted()` in
`internal/app/config.go`). In hosted mode:

- `--bucket` names the space. It must match the space grammar
  (`httpstore.ValidateSpace`: 1 to 63 lowercase letters, digits and inner
  hyphens).
- The endpoint is `--endpoint`, else `SLIVINGDOC_ENDPOINT`, else the default.
  `AWS_ENDPOINT_URL_S3`, `AWS_REGION` and the other AWS variables are
  ignored, so they can never redirect a token. The `--endpoint` flag is
  shared with S3 mode, though: an S3 command line that passes
  `--endpoint` sends the token to that host if `SLIVINGDOC_TOKEN` is also
  set. `serve` logs `hosted=true` when a token selected hosted mode.
- The token is read from the environment only, never from a flag, so it does
  not appear in a process listing. It must be printable ASCII without white
  space (`httpstore.ValidateToken`).
- The endpoint must be `https`, except to a loopback address (for tests and
  local gateways). `httpstore.ValidateEndpoint` enforces this in the adapter
  too. The token travels only in the `Authorization` header.
- The client never follows a redirect. Followed, a `PUT` would become a
  body-less `GET` whose success looks like a stored write, and the token
  would travel with it. A 3xx answer is a plain refusal.

## Startup check

A hosted server promises conditional writes in its description, so startup
does not run the S3 write probe (`storage.Probe`). `checkStore` in
`internal/app/app.go` calls `httpstore.Store.CheckAccess` instead:

1. `GET /v1` without the token. The answer must carry `api`
   `slivingdoc-storage`, `version` 1, and `conditionalWrites` true; anything
   else is `INCOMPATIBLE_STORE`, except a 429, a 5xx or an unreachable
   server, which fail startup as a plain check failure.
2. `GET /v1/spaces/{space}/usage` with the token. This is read-only, so a
   read-only token passes. 401, 403 or 404 is a startup refusal that names
   `SLIVINGDOC_TOKEN` and `--bucket`.

## Status mapping

`statusError` in `internal/httpstore/store.go` maps a refusal to a semantic
storage error. The error body is `{"error", "reason", "message"}`. The
status or the error code picks the category (a `quota_exceeded` code counts
as a 507 and a `rate_limited` code as a 429, whatever the status), and the
reason splits the two quota limits.

| Answer                                         | Storage error           | Notebook reason    | Action     | Retryable |
| ---------------------------------------------- | ----------------------- | ------------------ | ---------- | --------- |
| 507 `quota_exceeded`, reason `storage_full`    | `ErrQuotaExceeded`      | `STORAGE_FULL`     | `OPERATOR` | no        |
| 507 `quota_exceeded`, reason `request_limit`   | `ErrRequestLimit`       | `REQUEST_LIMIT`    | `OPERATOR` | no        |
| 429 `rate_limited`                             | `ErrRateLimited`        | `RATE_LIMITED`     | `RETRY`    | yes       |
| 401, 403                                       | `ErrAccessDenied`       | `ACCESS_DENIED`    | `OPERATOR` | no        |
| 404 on a read                                  | `ErrNotFound`           | (protocol)         |            |           |
| 404 on a write, list or delete                 | `ErrAccessDenied`       | `ACCESS_DENIED`    | `OPERATOR` | no        |
| 412 (also `If-Match` on an absent object)      | `ErrPreconditionFailed` | (protocol)         |            |           |
| 413                                            | `ErrTooLarge`           | `OBJECT_TOO_LARGE` | `OPERATOR` | no        |
| other 5xx                                      | `ErrTransport`          | (protocol)         |            |           |

A 507 without a reason counts as a full space. Both quota limits belong to
the account that owns the space, shared by all its spaces, so the messages
tell the owner to act. The notebook reasons are
`STORAGE_FAILURE` reasons, set by `storeRefusal` in
`internal/notebook/errors.go`; `retryable` lives in `internal/mcp/errors.go`.

The server's `message` is written for the person running the client. The
adapter carries it, sanitized to one printable line, in `storage.Refusal`,
and the notebook appends it to its own message after "The storage says:".
Every other server text becomes diagnostic detail only. No error, log record
or tool result carries the token; `mcp.Redact` also removes anything shaped
like an `sld_` token.

## Retries

`GET`, `LIST` and `DELETE` retry a transport failure or a 5xx other than 507
twice, with a short backoff. Writes never retry: their outcome may be
ambiguous, and the publication protocol already resolves an ambiguous write.
A 429 is not retried inside the adapter; the tool result says `RETRY`.

## Quota behaviour

When a commit would take the account past its storage limit, the pack upload
gets `507 storage_full` before the manifest moves, so nothing is published.
The edited files stay in place and pulls keep working.

A full space can shrink itself by deleting notes. The gateway lets checkpoint
uploads take the account up to twice its limit, and the notebook uses that
(`buildCompactingProposal` and `acceptCompaction` in
`internal/notebook/commit.go`):

1. The commit's increment upload is refused with `ErrQuotaExceeded`.
2. The notebook exports the same commit as a checkpoint pack of the whole
   state. If that pack is not smaller than everything the manifest
   references (the active checkpoint and tail plus every retained
   generation), compaction would not shrink the space, so the commit stays
   refused as `STORAGE_FULL`. Stored bytes are at least that referenced
   total, so an accepted compaction always shrinks the space, and the 2x
   checkpoint margin never lifts the limit.
3. Otherwise it uploads the checkpoint, through the generation the increment
   would have had, carrying the commit's own publication ID, and publishes a
   manifest whose active chain is that checkpoint alone. It keeps no retained
   generation, whatever `--retained-checkpoints` says: the retained packs are
   the bytes the space needs back, and readers already restart when a pack
   disappears.
4. After the manifest CAS accepts it, the notebook records the shallow
   boundary and runs the normal cleanup, which deletes every pack the
   manifest no longer references. A failure to record the boundary is
   logged and does not skip the cleanup.

When the manifest definitely did not accept the compaction (a lost CAS or a
definite manifest error), the notebook deletes its own checkpoint pack at
once. An orphan that large could otherwise leave no room under the 2x margin
for the next compaction. When acceptance is unproven, the pack stays for a
later cleanup. A lost CAS then retries the whole commit as usual.

Because the compacted manifest keeps no retained generation, another
writer's publication whose CAS response was lost just before the compaction
can no longer be found by its publication ID, and that writer reports
`PUBLICATION_UNPROVEN` for a commit that was accepted. The protocol already
allows this once descriptors expire; compaction makes the window immediate.

This is a client behaviour for any store that refuses with
`ErrQuotaExceeded`; S3 never does.

When the account uses its monthly request allowance, writes get
`507 request_limit` and reads continue slowly until they get
`429 rate_limited`. The allowance resets on the first of the month, UTC.

## Limits the client knows about

- There is no multipart upload. A pack larger than the gateway's
  `maxPackBytes` fails with `OBJECT_TOO_LARGE`.
- Listing is only allowed below `packs/`. The shared contract suite in
  `internal/storage/contract` therefore checks probe cleanup by reading the
  keys the probe created, not by listing.

## Tests

- `internal/httpstore/gatewaytest` is a test-only reference server of the API
  over the in-memory fake store: key grammar before authentication, per-space
  grants, read-only grants, a pack quota, cursor listing, and injected
  refusals (`RefuseNext`, `RefuseNextWithReason`).
- `internal/httpstore/store_test.go` runs the shared contract suite against
  it, with and without a prefix, plus the status table, retries, paging and
  misbehaving-server cases.
- `internal/integrationtest/scenario_hosted_test.go` runs the CLI end to end
  against the reference server: round trip, storage full, a read-only token,
  and startup refusals.
