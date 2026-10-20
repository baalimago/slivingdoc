# Phase 2: cloud create route

**Status:** Complete (2026-10-19, session 1)

README: [README.md](./README.md)

## Goal

Add `POST /cli/v1/spaces` to the cloud site so a CLI key can create one space
for its account, with one portable name grammar and the documented refusals.

## Specification

The environment is ready, so no step waits for a person: the cloud checkout is
`/home/imago/Projects/private/slivingdoc-cloud`, present and writable, with its
Node dependencies and the Playwright browsers under `~/.cache/ms-playwright`
installed. Export `CLOUD_REPO` to that root for the gate commands. The
end-to-end spec runs on the local stack and its result is recorded in this
phase.

### Portable creation grammar

`cloud/src/keys.ts` gains `validNewSpaceName(name: string): boolean`:

| Rule | Value |
| --- | --- |
| Length | 3 through 63 characters |
| Characters | lowercase ASCII letters, digits, and inner hyphens |
| Ends | a letter or a digit |
| Reserved prefixes | `xn--`, `sthree-` |
| Reserved suffixes | `-s3alias`, `--ol-s3`, `--x-s3`, `--table-s3` |

`validSpaceName` stays as it is and keeps serving listing, selecting, and
minting. `Accounts.createSpace` calls `validNewSpaceName`; the account create
form uses that same grammar and explains the 3 to 63 character limit and the
reserved prefixes and suffixes. `renameSpace`, mint, and list keep
`validSpaceName`, so an existing short hosted space and a legacy dotted bucket
stay usable.

### `CliLogins.create`

`cloud/site/cli-login.ts` gains one method:

```ts
async create(
  key: CliKey,
  name: string,
): Promise<
  | { kind: "created"; name: string; owner: string; access: "write" }
  | { kind: "invalid_name" }
  | { kind: "access_denied" }
  | { kind: "limit" }
  | { kind: "taken" }
  | { kind: "pool_empty" }
>;
```

The order is fixed:

1. `key.readOnly` is refused as `access_denied` before calling
   `Accounts.createSpace` or writing.
2. `this.accounts.createSpace(key.principalId, name)` answers `ok`, `invalid`,
   `taken`, `limit`, or `pool_empty`; the first is `created`, and the rest map
   one to one. Its `invalid` result maps to `invalid_name`. It is the only
   creation-grammar check.
3. `await this.used(key)` marks the key used on success only.
4. The owner is the account email, or `key.principalId` when the account has no
   email. The access is `"write"`; the route only creates for a writable key.

`Accounts.createSpace` already inserts the space, its admin grant, and one
`space_created` event in one batch. The route adds no second event.

### `POST /cli/v1/spaces`

`cliSpaces` becomes the sole owner of the path and branches on the method:

| Method | Result |
| --- | --- |
| `GET` | the existing spaces list |
| `POST` | create |
| anything else | 405 `method_not_allowed`, message `Use GET or POST.` |

The POST order:

1. `deps.cliLogins.key(authorization)`; null → 401 `invalid_token`.
2. `cliLimited(deps.cliKeyLimiter, \`create:${key.id}\`)`; false → 429 `busy`.
3. Content type `application/json` (split on `;`, trimmed, lowercased); else
   415 `invalid_request`, exactly as the other JSON routes.
4. `readCapped(req, CLI_BODY_MAX)`; null → 413 `invalid_request`.
5. Parse one strict object with exactly one string `name`; else 400
   `invalid_request`.
6. `deps.cliLogins.create(key, name)` and the status map below.

| Outcome | Status | Error |
| --- | --- | --- |
| created | 201 | — |
| read-only key | 403 | `access_denied` |
| invalid name from `Accounts.createSpace` | 400 | `invalid_name` |
| bad body | 400 | `invalid_request` |
| bad key | 401 | `invalid_token` |
| entitlement | 403 | `space_limit` |
| name in use | 409 | `name_taken` |
| busy | 429 | `busy` |
| no capacity | 503 | `pool_empty` |

The 201 body is exactly:

```json
{"space":{"name":"team-notes","owner":"ada@example.com","access":"write"}}
```

The happy-path test needs an account with room for a second space. `user()`
already consumes the free entitlement of one through `ensureDefaultSpace`, so
the test inserts an entitlement row with `spaces = 2` before creating (or uses
a principal that never called `ensureDefaultSpace`).

### Duplicate JSON members

`JSON.parse` silently keeps the last member of a repeated top-level key, so a
body such as `{"name":"a","name":"b"}` would otherwise create a space the
caller did not name. The route therefore scans the raw body before parsing.

`duplicateTopLevelKey(raw: string): boolean` walks the bytes:

- skip whitespace to the opening `{`;
- at depth 1, read a JSON string key, skipping escapes, then `:`;
- scan the value, tracking strings, nested `{`/`[` depth, and stop at the
  next top-level `,` or `}`;
- record each key; a repeat returns `true`.

Nested objects and arrays are skipped as values, so their keys never count.
Malformed input is left to `JSON.parse`; the scanner answers `false` rather
than inventing a second parser. A repeated key is 400 `invalid_request`.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| One creation grammar, mirrored in Go | `Accounts.createSpace` uses `validNewSpaceName` over the same vectors as `storage.ValidateNewSpaceName` | `cloud/test/keys.test.ts`, `cloud/test/cli-login.test.ts`, `cloud/test/app.test.ts` |
| The site form refuses names shorter than 3 characters | It calls `Accounts.createSpace` and shows the creation grammar | `refuses short names on the site form` |
| A read-only key creates nothing | `CliLogins.create` checks `key.readOnly` first | `creates nothing with a read-only key` |
| Creation is one account event | `Accounts.createSpace` | `records one space_created event` |
| The limiter is per key | `create:<key id>` | `rate limits creation per key` |
| Duplicate members are refused | `duplicateTopLevelKey` | `refuses a duplicate name member` |
| No credential or key reaches the body | response names name, owner, access only | `creates a space` |

### Documentation

| Doc | What must become true |
| --- | --- |
| `cloud:architecture/cli-login.md` | The CLI route section names `POST /cli/v1/spaces`, says that `cloud/site/app.ts` owns the path through `cliSpaces`, and that `cliSpaces` branches on the method (`GET` list, `POST` create, otherwise `405 method_not_allowed` with message `Use GET or POST.`). Its key-file table names `e2e/tests/space-create.spec.ts`. |
| `cloud:cloud/API.md` | The API reference documents `POST /cli/v1/spaces`: it creates one space for the account the CLI key belongs to, returns `201` with `{space:{name,owner,access}}`, and lists the refusals `invalid_token`, `busy`, `invalid_request`, `invalid_name`, `access_denied`, `space_limit`, `name_taken`, and `pool_empty`. |
| `cloud:architecture/e2e.md` | The key-file tables name `space-create.spec.ts` and the production exclusion list, the flow section states what the create spec checks, and the admin-pages gotcha counts it with the other production-excluded specs. |

### Files

| File | Change |
| --- | --- |
| `cloud/src/keys.ts` | Add `validNewSpaceName` for the portable creation grammar and keep `validSpaceName` for existing flows. |
| `cloud/site/accounts.ts` | Make `Accounts.createSpace` apply `validNewSpaceName`, the single creation-grammar check. |
| `cloud/site/cli-login.ts` | Add `CliLogins.create`; refuse a read-only key first, then map `Accounts.createSpace` outcomes to `{created, invalid_name, access_denied, limit, taken, pool_empty}`. |
| `cloud/site/app.ts` | Make `cliSpaces` own `GET` and `POST /cli/v1/spaces`, with `405 method_not_allowed` for other methods. Update the create-form message for 3 to 63 characters and the reserved prefixes and suffixes. |
| `cloud/test/keys.test.ts` | Add table tests for `validNewSpaceName` acceptance and refusal vectors. |
| `cloud/test/cli-login.test.ts` | Add route and login tests for create outcomes, limiter `429 busy`, and duplicate `name` member refusal. |
| `cloud/test/app.test.ts` | Add a site-form test that refuses names with 1 or 2 characters. |
| `e2e/tests/space-create.spec.ts` | Hold the create step: `POST /cli/v1/spaces` with a writable key, then list and see the new space. It needs the admin comp routes, so it is production-excluded. |
| `e2e/tests/cli-login.spec.ts` | Keep the existing flow, unchanged by the create step. |
| `e2e/playwright.config.ts` | Leave `space-create.spec.ts` out when `target.production` is true, like the other admin-page specs. |

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `POST /cli/v1/spaces` with a writable key and a free entitlement | site app, D1, bucket pool | 201 with `{space:{name,owner,access}}` | one space, one admin grant, one `space_created` event, key marked used | a second event, a default-space change |
| the same with a read-only key | site app, D1 | 403 `access_denied` | none | a pool row consumed |
| the same with an invalid name and a writable key | site app | 400 `invalid_name` | none | a pool row consumed |
| `POST /account/spaces/new` with a 1- or 2-character name | site app | 400 with the creation-grammar guidance | none | a pool row consumed |
| the same with an existing grant name | site app, D1 | 409 `name_taken` | none | a pool row consumed |
| the same with the entitlement exhausted | site app, D1 | 403 `space_limit` | none | a pool row consumed |
| the same with an empty bucket pool | site app, D1 | 503 `pool_empty` | none | a partial space |
| the same with a duplicate `name` member | site app | 400 `invalid_request` | none | a space |
| the same with another field, null, or a non-string | site app | 400 `invalid_request` | none | a space |
| the same with a wrong content type | site app | 415 | none | a space |
| the same over `CLI_BODY_MAX` | site app | 413 | none | a space |
| a method other than GET or POST | site app | 405, message `Use GET or POST.` | none | a space |
| an unknown, expired, or revoked key | site app | 401 `invalid_token` | none | a space |
| the limiter refuses | injected `cliKeyLimiter` | 429 `busy` | none | a space |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| A writable key creates a space and gets 201 | `creates a space` |
| The database holds the space, its admin grant, and one event | `creates a space` |
| The key is marked used | `creates a space` |
| A read-only key is refused before creation | `creates nothing with a read-only key` |
| A writable key with an invalid name gets 400 `invalid_name` from the account creation grammar | `refuses invalid names` |
| The site form refuses names shorter than 3 characters | `refuses short names on the site form` |
| An existing name is 409 `name_taken` | `refuses a name in use` |
| An exhausted entitlement is 403 `space_limit` | `refuses past the entitlement` |
| An empty pool is 503 `pool_empty` | `answers pool_empty` |
| A duplicate `name` member is 400 `invalid_request` | `refuses a duplicate name member` |
| Every other bad body is 400 `invalid_request` | `refuses a bad body` |
| A wrong content type is 415 | `refuses a non-JSON body` |
| A body over `CLI_BODY_MAX` is 413 | `refuses an oversized body` |
| PUT and DELETE are 405 with the new message | `answers only GET and POST` |
| A bad key is 401 `invalid_token` | `refuses an unknown key` |
| The per-key limiter is 429 `busy` | `rate limits creation per key` |
| GET `/cli/v1/spaces` still lists | `lists the spaces a key reaches` (unchanged) |
| The existing POST→405 assertion is rewritten to the new behavior | `answers only GET and POST` |
| `validNewSpaceName` accepts and refuses the documented names | `cloud/test/keys.test.ts` |
| The end-to-end create spec creates a space through the route | `e2e/tests/space-create.spec.ts` on the local stack |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| read-only key | 403 `access_denied`, no write | `creates nothing with a read-only key` |
| writable key with a name outside the grammar | 400 `invalid_name` from `Accounts.createSpace` | `refuses invalid names` |
| site-form name shorter than 3 characters | 400 with creation-grammar guidance, no write | `refuses short names on the site form` |
| name already granted | 409 `name_taken` | `refuses a name in use` |
| entitlement exhausted | 403 `space_limit` | `refuses past the entitlement` |
| empty bucket pool | 503 `pool_empty`, no partial space | `answers pool_empty` |
| duplicate member | 400 `invalid_request` | `refuses a duplicate name member` |
| missing, null, or non-string `name` | 400 `invalid_request` | `refuses a bad body` |
| another field | 400 `invalid_request` | `refuses a bad body` |
| not an object | 400 `invalid_request` | `refuses a bad body` |
| wrong content type | 415 `invalid_request` | `refuses a non-JSON body` |
| body over `CLI_BODY_MAX` | 413 `invalid_request` | `refuses an oversized body` |
| unknown, expired, or revoked key | 401 `invalid_token` | `refuses an unknown key` |
| limiter refuses | 429 `busy` | `rate limits creation per key` |
| account created with no email | owner is the principal id | `creates a space without an email` |

## Implementation notes

Worker: phase-2 agent, 2026-10-19, session 1.

### What shipped

The cloud checkout already carried an uncommitted implementation of this phase
when the session started. This session checked it against the phase file, made
the corrections below, and ran the gates.

`cloud/src/keys.ts` owns `validNewSpaceName`: 3 through 63 characters, lowercase
letters, digits and inner hyphens, alphanumeric ends, no `xn--` or `sthree-`
prefix, and no `-s3alias`, `--ol-s3`, `--x-s3` or `--table-s3` suffix.
`validSpaceName` is unchanged and still serves listing, selecting, renaming and
minting. `Accounts.createSpace` applies `validNewSpaceName` and stays the one
creation-grammar check for both the account form and the CLI route.
`CliLogins.create` refuses a read-only key before any write, maps the
`Accounts.createSpace` outcomes to `invalid_name`, `name_taken`, `space_limit`
and `pool_empty`, marks the key used only on success, and names the account
email or the principal id as the owner.

`cloud/site/app.ts` makes `cliSpaces` the owner of `/cli/v1/spaces` for both
methods: `GET` keeps the listing, `POST` creates, anything else is `405` with
`Use GET or POST.` The POST order is the documented one: key, then the per-key
limiter with `create:<key id>`, then the content type, then the capped body,
then the shape, then `CliLogins.create`. A repeated top-level member is refused
before `JSON.parse`, which would keep the last one. The account form and its
page explain the new grammar.

The site tests, the route tests, the form test and the keys tests cover every
row of the phase file's acceptance and error tables. `make qa` is green, and the
end-to-end create flow passes on the local stack.

### Decisions

1. **The end-to-end create flow is its own spec file** (worker: phase-2 agent,
   2026-10-19, session 1). It needs the admin comp routes, which production
   does not expose, so it cannot run against production. The repository's lint
   rule `T1-lint-rejects-skipped-tests` refuses a conditional `test.skip`, and
   the convention for a flow that needs the admin pages is a file that
   `e2e/playwright.config.ts` leaves out when `target.production` is true. The
   flow therefore lives in `e2e/tests/space-create.spec.ts`, is added to that
   list beside `space-delete.spec.ts`, and matches the other admin-page specs
   by turning traces off. `cli-login.spec.ts` keeps its unchanged flows, and
   `architecture/e2e.md` and `architecture/cli-login.md` name the new file.
2. **The body reader answers `null`, not an exception** (worker: phase-2 agent,
   2026-10-19, session 1). `oneName` returns `string | null`, and the route
   answers one `400 invalid_request`. The first shape of this code threw for a
   bad body and caught it in the caller, which is the exception-as-control-flow
   pattern the repository avoids where a typed result is available.
3. **The 415 and the capped body are shared helpers** (worker: phase-2 agent,
   2026-10-19, session 1). `nonJson` and `cliPayload` hold the two rules that
   the CLI JSON routes and the create route both apply, so the message and the
   bound have one implementation.
4. **The duplicate-member scan is a separate exported function** (worker:
   phase-2 agent, 2026-10-19, session 1). `duplicateTopLevelKey` walks the raw
   bytes, skips nested values whole, and answers `false` for anything it does
   not understand, so `JSON.parse` stays the only parser. Exported, it is
   tested directly over accepted and refused bodies.

### Commands and results

Before the corrections, with the uncommitted implementation in the checkout:

```bash
cd "$CLOUD_REPO/cloud" && npx vitest run test/keys.test.ts test/cli-login.test.ts test/app.test.ts
```

passed 3 files and 284 tests.

After the corrections:

```bash
cd "$CLOUD_REPO/cloud" && npx tsc --noEmit
cd "$CLOUD_REPO/cloud" && npx vitest run test/keys.test.ts test/cli-login.test.ts test/app.test.ts
cd "$CLOUD_REPO" && npx prettier --check .
cd "$CLOUD_REPO" && npx eslint e2e/tests/space-create.spec.ts e2e/tests/cli-login.spec.ts
cd "$CLOUD_REPO" && npx knip
cd "$CLOUD_REPO/e2e" && npm run typecheck
```

All are clean; the three cloud test files pass 284 tests.

```bash
cd "$CLOUD_REPO" && make qa
```

exits zero: prettier, `astro check`, ESLint, Stylelint and Knip pass; the build
and the fixture build pass; 33 site test files and 26 cloud test files pass;
`scripts/check-links.mjs` passes; jscpd finds the two clones it already reported
in `tests/unit/shell.test.ts`, untouched by this phase; and the Workers'
`tsc --noEmit` plus tests pass at 95.2 % statements and 97.13 % lines, above the
90 % floor.

```bash
cd "$CLOUD_REPO/e2e" && E2E_DOMAIN=slivingdoc.localhost:8080 npx playwright test tests/space-create.spec.ts --project flows
cd "$CLOUD_REPO/e2e" && E2E_DOMAIN=slivingdoc.localhost:8080 npx playwright test tests/cli-login.spec.ts --project flows
```

the first runs the new create flow against the local stack and passes (1 test,
21.0 s); the second re-runs the unchanged login flows after the extraction and
passes (3 tests, 20.6 s).

## Review findings

### V6-01 (note, repository fact)

The `Files` table lists `cloud/e2e/tests/space-create.spec.ts`,
`cloud/e2e/tests/cli-login.spec.ts`, and `cloud/e2e/playwright.config.ts`.
Those files are at the cloud repository root under `e2e/`, not under
`cloud/e2e/`. Correct the three paths in the table.

### Resolution (2026-10-20, session 20)

V6-01 is resolved. The `Files` paths now start at `e2e/`. The local cloud
checkout contains all three paths; the cloud gate was not run. Phase 2 remains
Complete.
