# slivingdoc hosted storage worklog

**Status:** Complete for the client. The hosted gateway lives in
`cloud/` of baalimago/slivingdoc-web (being renamed to slivingdoc-cloud).

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
| `internal/storage` | Five semantic errors: `ErrQuotaExceeded`, `ErrRequestLimit`, `ErrRateLimited`, `ErrAccessDenied`, `ErrTooLarge`, and `Refusal`, which carries the server's message. The metadata field names and codec moved here from `s3store`, so both adapters share one implementation. |
| `internal/notebook`, `internal/mcp` | Five `STORAGE_FAILURE` reasons: `STORAGE_FULL`, `REQUEST_LIMIT`, `RATE_LIMITED`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE`. Each message names the fix and, when the server sent one, ends with the server's own message. `retryable` is false for all but `RATE_LIMITED`. `sld_` tokens are redacted. |
| `internal/app` | `SLIVINGDOC_TOKEN` selects hosted mode. `--bucket` names the space, and `--endpoint` or `SLIVINGDOC_ENDPOINT` names the API, by default `https://api.slivingdoc.dev`. The AWS variables are ignored, and the token is sent only over HTTPS or to loopback. Startup runs `CheckAccess` instead of the write probe. |
| Contract suite | Missing-key rows use valid protocol keys, and the probe cleanup is observed through the keys the probe created rather than a `LIST` of `probe/`, which a hosted store refuses. |
| Compaction on a full space | `internal/notebook/commit.go`: a commit whose increment is refused as `STORAGE_FULL` publishes as a checkpoint of the whole state when that is smaller than everything the manifest references, retains no previous generation, deletes its own checkpoint if the manifest definitely refused it, and cleans up, so deleting notes frees room. See `architecture/hosted-mode.md`, Quota behaviour. |
| Scenarios | `scenario_hosted_test.go`: round trip, storage full, deleting to make room, request limit, read-only token, and startup refusals, all over one-shot CLI processes against the reference server. |

## Open items

1. **No multipart.** Packs larger than the gateway's 95 MiB `maxPackBytes`
   can't upload. The client reports `OBJECT_TOO_LARGE`.
