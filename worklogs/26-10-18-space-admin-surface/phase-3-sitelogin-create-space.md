# Phase 3: `sitelogin.CreateSpace`

**Status:** Complete (2026-10-19, session 2)

README: [README.md](./README.md)

## Goal

Teach the site client to create a space over the new route, introduce the Go
owner of the portable creation grammar, and extend the reference site so the
call is proven without a live cloud.

## Specification

### `storage.ValidateNewSpaceName`

`internal/storage` gains the Go owner of the README's creation grammar:

```go
// ErrInvalidNewSpaceName reports a name outside the portable creation grammar.
var ErrInvalidNewSpaceName = errors.New("storage: invalid new space name")

func ValidateNewSpaceName(name string) error
```

The rule is the README's: 3 through 63 characters, lowercase ASCII letters,
digits, and inner hyphens, a letter or digit at each end, no `xn--` or
`sthree-` prefix, and no `-s3alias`, `--ol-s3`, `--x-s3`, or `--table-s3`
suffix. The refusal names the fix, for example:

```text
storage: invalid new space name: "x" must be 3 to 63 lowercase letters, digits, or inner hyphens
```

The function is pure and has one table test over the same name vectors as the
cloud mirror, so both implementations are checked against the same names.
Phase 2 mirrors the rule in `cloud:`; Phases 5 and 7 consume this one.

### `Client.CreateSpace`

`internal/sitelogin/client.go` gains:

```go
const createPath = "/cli/v1/spaces"

func (c *Client) CreateSpace(ctx context.Context, key, name string) (Space, error)
```

The order is fixed:

1. `httpstore.ValidateToken(key)`; a key that cannot travel is refused before
   anything is sent.
2. `storage.ValidateNewSpaceName(name)`; an invalid name is refused before
   anything is sent, so the site never sees it.
3. `call(ctx, http.MethodPost, createPath, key, createBody{Name: name}, &ans)`.
4. Any 2xx with a body is accepted; a 204 or a missing body is
   `ErrProtocol`.
5. The answer must name the space that was asked for, carry a usable owner
   (empty is allowed for a team-owned space), and carry a parseable access.
6. The access must be `credentials.AccessWrite`; the route only creates for a
   writable key, and a read access would mean the site and client disagree.
   Anything else is `ErrProtocol`.

The request body is exactly `{"name":"<name>"}`. `call` already refuses a body
at or over `requestLimit`.

### `call` accepts any 2xx body

`call` currently decodes an answer body only for `200 OK`. It changes to:

| Response | `out == nil` | `out != nil` |
| --- | --- | --- |
| any 2xx | success, body ignored | body decoded as JSON |
| any 2xx with a body over the bound | `ErrProtocol` | `ErrProtocol` |
| any non-2xx | `*Refusal` | `*Refusal` |

A broken 2xx body stays `ErrBrokenAnswer`; a 5xx that breaks off stays
`ErrUnreachable`. The 204/no-body form of `Revoke` is unchanged. One behavior
changes for the `out == nil` callers: a 2xx that carries more than the bound is
`ErrProtocol` instead of being read and ignored. The spaces list keeps its
`spacesLimit`; every other route keeps `bodyLimit`.

### Reference site

`internal/sitelogin/sitetest/site.go` gains the create route:

| Change | Contract |
| --- | --- |
| `key.account` | the approver's email, set from `Issue.Account`; `Issued` leaves it empty |
| `issue(k, endpoint, access, account)` | the one place a key is recorded |
| `POST /cli/v1/spaces` | `jsonOnly`; strict body with the single field `name` |
| unknown or malformed body | 400 `invalid_request` |
| no live key | 401 `invalid_token` |
| a read-only key | 403 `access_denied` |
| a name outside the grammar | 400 `invalid_name` |
| a name already among the key's spaces | 409 `name_taken` |
| success | 201 `{"space":{"name":...,"owner":<key account>,"access":"write"}}`, and the space is appended to the key's list |
| `RefuseCreate(status, code)` | the next create answers that status and code |
| `decodeStrict` | also refuses a repeated top-level key, so a duplicate `name` is 400 `invalid_request` as the cloud route answers it; no existing route sends one |

`sitetest` keeps a local `validNewSpaceName` mirror, so the reference site stays
independent of the client package. The owner is the key's account; an empty
account yields an empty owner, which `Client.CreateSpace` accepts as a
team-owned space.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| An invalid name never reaches the site | validation before `call` | `TestCreateSpaceRejectsInvalidNameBeforeSending` |
| An unsendable key never reaches the site | `httpstore.ValidateToken` before `call` | `TestCreateSpaceRejectsUnsendableKey` |
| The answer must name the space asked for | `ans.Space.Name != name` → `ErrProtocol` | `TestCreateSpaceRejectsAnotherName` |
| The answer must carry write access | `AccessWrite` required | `TestCreateSpaceRejectsReadAccess` |
| Any 2xx body is decoded | `call` branch | `TestCallDecodesAny2xxBody` |
| No 2xx body is required for `out == nil` | `call` branch | `TestCallAcceptsAny2xxWithoutBody` |
| An oversized 2xx body is refused for `out == nil` too | `call` branch | `TestCallRejectsOversized2xxWithoutBody` |

### Documentation

| Doc | What must become true |
| --- | --- |
| `architecture/login.md` | The wire contract carries `POST /cli/v1/spaces` with its request, `201` answer, and refusal codes, and `sitelogin.CreateSpace` is listed with its validation order. |
| `architecture/storage.md` | `storage.ValidateNewSpaceName` and `ErrInvalidNewSpaceName` are named as the portable creation grammar; the notebook storage contract stays unchanged and stays the sibling worklog's. |

### Files

| File | Change |
| --- | --- |
| `internal/sitelogin/client.go` | Add `Client.CreateSpace` that validates with `httpstore.ValidateToken` and `storage.ValidateNewSpaceName`, then calls `POST /cli/v1/spaces`. |
| `internal/sitelogin/client_test.go` | Add unit tests for `CreateSpace` validation order, refusal mapping, and `ErrProtocol` on broken 2xx answers. |
| `internal/sitelogin/sitetest/site.go` | Add the reference-site `POST /cli/v1/spaces` route with strict body parsing and the documented refusal codes. |
| `internal/sitelogin/sitetest/site_test.go` (new) | Add the reference-site route tests for create, refusals, and the duplicate `name` member. |
| `internal/storage/name.go` (new) | Add `ValidateNewSpaceName` and `ErrInvalidNewSpaceName` as the Go owner of the portable creation grammar. |
| `internal/storage/name_test.go` (new) | Add `TestValidateNewSpaceName` over the same name vectors as the cloud mirror. |

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `CreateSpace` with a valid key and name | `sitetest` | the returned `Space` | one `POST /cli/v1/spaces`, one appended space | another route, a token mint |
| `CreateSpace` with an invalid name | `sitetest` | `storage.ErrInvalidNewSpaceName` | none | any request |
| `CreateSpace` with an unsendable key | `sitetest` | `httpstore.ErrInvalidToken` | none | any request |
| `CreateSpace` refused with 403 `access_denied` | `sitetest.RefuseCreate` | `*sitelogin.Refusal` with status 403 and code | none | a stored space |
| `CreateSpace` refused with 409 `name_taken` | `sitetest.RefuseCreate` | `*sitelogin.Refusal` | none | a stored space |
| `CreateSpace` refused with 400 `invalid_name` | `sitetest.RefuseCreate` | `*sitelogin.Refusal` | none | a stored space |
| a 201 answer naming another space | scripted site answer | `ErrProtocol` | none | acceptance |
| a 201 answer with read access | scripted site answer | `ErrProtocol` | none | acceptance |
| a 201 answer with an empty owner | `sitetest.Issued` with no account | accepted `Space` | none | a refusal |
| `Revoke` answering 204 | `sitetest` | no error | none | a decode failure |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| The grammar accepts and refuses every documented name shape | `TestValidateNewSpaceName` |
| A valid create returns the name, owner, and write access | `TestCreateSpace` |
| The request body is one strict `name` object | `TestCreateSpaceRequestBody` |
| An invalid name is refused before any request | `TestCreateSpaceRejectsInvalidNameBeforeSending` |
| An unsendable key is refused before any request | `TestCreateSpaceRejectsUnsendableKey` |
| A refusal maps to `*Refusal` with its status and code | `TestCreateSpaceRefusal` |
| An answer naming another space is `ErrProtocol` | `TestCreateSpaceRejectsAnotherName` |
| An answer with read access is `ErrProtocol` | `TestCreateSpaceRejectsReadAccess` |
| An answer with an empty owner is accepted | `TestCreateSpaceAcceptsTeamOwner` |
| `call` decodes a 201 body | `TestCallDecodesAny2xxBody` |
| `call` accepts a 204 with no body | `TestCallAcceptsAny2xxWithoutBody` |
| A 2xx body over the bound is `ErrProtocol` even without an answer target | `TestCallRejectsOversized2xxWithoutBody` |
| The reference site creates, refuses invalid names, duplicates, and read-only keys | `TestSiteCreatesASpace`, `TestSiteRefusesCreate` |
| The reference site rejects a duplicate `name` member | `TestSiteRefusesDuplicateName` |
| The architecture doc states the new wire contract | `rg -n 'POST /cli/v1/spaces' architecture/login.md` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| name empty or too short | `storage.ErrInvalidNewSpaceName` | `TestValidateNewSpaceName` |
| name too long | `storage.ErrInvalidNewSpaceName` | `TestValidateNewSpaceName` |
| uppercase, underscore, dot, or edge hyphen | `storage.ErrInvalidNewSpaceName` | `TestValidateNewSpaceName` |
| reserved prefix or suffix | `storage.ErrInvalidNewSpaceName` | `TestValidateNewSpaceName` |
| unsendable key | `httpstore.ErrInvalidToken` before sending | `TestCreateSpaceRejectsUnsendableKey` |
| 401 `invalid_token` | `*Refusal` 401 | `TestCreateSpaceRefusal` |
| 403 `access_denied` | `*Refusal` 403 | `TestCreateSpaceRefusal` |
| 400 `invalid_name` | `*Refusal` 400 | `TestCreateSpaceRefusal` |
| 409 `name_taken` | `*Refusal` 409 | `TestCreateSpaceRefusal` |
| 429 `busy` | `*Refusal` 429 | `TestCreateSpaceRefusal` |
| 503 `pool_empty` | `*Refusal` 503 | `TestCreateSpaceRefusal` |
| answer without a body | `ErrProtocol` | `TestCreateSpaceRejectsEmptyAnswer` |
| answer with another name | `ErrProtocol` | `TestCreateSpaceRejectsAnotherName` |
| answer with an unusable owner | `ErrProtocol` | `TestCreateSpaceRejectsBadOwner` |
| answer with an unknown access | `ErrProtocol` | `TestCreateSpaceRejectsBadAccess` |
| answer with read access | `ErrProtocol` | `TestCreateSpaceRejectsReadAccess` |
| 2xx body larger than the bound | `ErrProtocol` | `TestCallRejectsOversized2xxBody` |
| 2xx body larger than the bound with no answer target | `ErrProtocol`, not silence | `TestCallRejectsOversized2xxWithoutBody` |

## Implementation notes

Worker: phase-3 agent, 2026-10-19, session 2.

### What shipped

The phase's production code was already present in the worktree, uncommitted, when
this session started: `storage.ValidateNewSpaceName`,
`Client.CreateSpace`, the any-2xx branch of `call`, and the reference site's
create route with `Issue.Account` and `RefuseCreate`. This session checked each
against the phase file, added the missing client tests, put the dangling test
imports to work, and updated the two architecture docs.

`internal/sitelogin/client_test.go` now proves the create call end to end over
`sitetest` and over a scripted recorder: the request is one `POST
/cli/v1/spaces` with exactly `{"name":"<name>"}`; an invalid name or an
unsendable key sends nothing; each refusal keeps its status and code; a team
owner is accepted; and another name, read access, a broken owner, a broken
access, or an empty answer is `ErrProtocol`. The `call` tests show a 201 body
decoded for a destination, any 2xx accepted without one, and an oversized body
refused for both shapes.

`architecture/login.md` documents `POST /cli/v1/spaces` in the wire contract,
`Client.CreateSpace` with its validation order, `Issue.Account` and the create
route of the reference site. `architecture/storage.md` names
`storage.ValidateNewSpaceName` as the portable creation grammar and states that
the notebook storage protocol is unchanged.

### Commands

| Command | Result |
| --- | --- |
| `go test ./internal/sitelogin/... ./internal/storage/...` before the tests | build failed: `encoding/json`, `httpstore`, and `storage` were imported but unused in `client_test.go` |
| `go test ./internal/sitelogin/... ./internal/storage/...` after the tests | pass |
| `go test -race -count=1 ./internal/sitelogin/... ./internal/storage/...` | pass |
| `go test -race -count=3 -timeout=30s ./internal/sitelogin/... ./internal/storage/...` | pass |
| `make qa` (gofumpt, vet, staticcheck, go fix, the full race/count=3 suite, npm) | pass; coverage 89.0 % against the 70 % floor |

### Decisions

1. `CreateSpace` wraps the `httpstore.ValidateToken` failure with `%w`
   (`sitelogin: the stored key cannot be sent: httpstore: ...`) instead of
   replacing it with a fixed message as `Spaces`, `Mint` and `Revoke` do. The
   phase's integration contract names `httpstore.ErrInvalidToken`, and the
   wrapped text still never carries the key.
2. "Refused before sending" is asserted with a recording site in
   `client_test.go`, because `sitetest` records only approvals, starts, mints
   and revocations. The reference site's own route tests stay in
   `internal/sitelogin/sitetest/site_test.go`.
3. The any-2xx `call` test includes a 201 body that must be ignored when no
   answer target is given, not only a 204. A 204-only test would not prove the
   new branch, since 204 was already the `Revoke` case.

### Notes for later phases

Phase 4 can call `Client.CreateSpace` directly. The client returns
`storage.ErrInvalidNewSpaceName` unchanged for an invalid name, and the caller
should render its message as the fix. Phases 5 and 7 share the same validator
and should not duplicate the grammar.

## Review findings

None.
