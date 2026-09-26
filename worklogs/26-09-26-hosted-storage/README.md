# slivingdoc hosted storage worklog

**Status:** Complete for the client. Open items below depend on the hosted
gateway, which lives in the slivingdoc-cloud repository.

**Architecture:** [`../../architecture/hosted-mode.md`](../../architecture/hosted-mode.md)

## Goal

Let a user store a notebook at slivingdoc.dev with nothing but an API token
and a space name. `SLIVINGDOC_TOKEN` selects a second object-store adapter
that speaks the hosted storage API, version 1: the six `ObjectStore`
operations over HTTPS with a bearer token. The notebook protocol above the
adapter does not change. When the space is full, a commit must say so and
say how to fix it, and reads must keep working.

## What shipped

| Area | Outcome |
| ---- | ------- |
| `internal/httpstore` | The adapter. It maps HTTP statuses to the semantic storage errors, retries only idempotent requests, and never puts the token in an error. It passes the shared contract suite with and without a notebook prefix. |
| `internal/httpstore/gatewaytest` | A test-only reference server of the API over the in-memory store: key grammar before authentication, per-space grants, read-only grants, pack quota, cursor listing, and injected refusals. |
| `internal/storage` | Four semantic errors: `ErrQuotaExceeded`, `ErrRateLimited`, `ErrAccessDenied`, `ErrTooLarge`. The metadata field names and codec moved here from `s3store`, so both adapters share one implementation. |
| `internal/notebook`, `internal/mcp` | Four `STORAGE_FAILURE` reasons: `STORAGE_FULL`, `REQUEST_LIMIT`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE`. Each message names the fix. `retryable` is false for the three that a retry cannot change. `sld_` tokens are redacted. |
| `internal/app` | `SLIVINGDOC_TOKEN` selects hosted mode. `--bucket` names the space, and `--endpoint` or `SLIVINGDOC_ENDPOINT` names the API, by default `https://api.slivingdoc.dev`. The AWS variables are ignored, and the token is sent only over HTTPS or to loopback. Startup runs `CheckAccess` instead of the write probe. |
| Contract suite | Missing-key rows use valid protocol keys, and the probe cleanup is observed through the keys the probe created rather than a `LIST` of `probe/`, which a hosted store refuses. |
| Scenarios | `scenario_hosted_test.go`: round trip, storage full, read-only token, and startup refusals, all over one-shot CLI processes against the reference server. |

## Open items (gateway side)

These were sent to the gateway owner. None blocks this client.

1. **Over-quota deadlock.** Deleting notes to get under quota needs a pack
   upload (an increment), and reclaiming space needs a checkpoint upload plus
   cleanup. If every pack upload past quota is refused, a full space can't
   shrink itself. Until the gateway allows a grace margin or checkpoint
   uploads, the `STORAGE_FULL` message only tells the user to add storage.
2. **No multipart.** Packs larger than `maxPackBytes` (95 MiB) can't upload.
   The client reports `OBJECT_TOO_LARGE`.
3. **`/usage` for read-only tokens.** The startup check assumes it answers
   200 for any grant and 404 for an unknown space.
4. **Account-wide limits.** The client maps 507 or `storage_full` to a full
   space, and 429 or `request_limit` to the request limit, whatever the final
   codes are.
5. **Replace of a missing key.** The client reads 404 on an `If-Match` PUT as
   a lost precondition, as the storage contract requires.
