# Phase 6: S3 `space` backend

**Status:** Complete (review 2 remediation, 2026-10-20, session 12).

README: [README.md](./README.md)

## Goal

Give `space` the S3 backend: list buckets across reachable S3 identities,
create a bucket, set a store-keyed default, and show recorded targets that no
listing returned.

## Specification

### Identities

An S3 identity is `{profile, endpoint, region, pathStyle}` plus one `Admin`.
The command builds:

1. the current chain (or the explicit `--profile`), from `--profile`,
   `--endpoint`, `--region`, and their environment variables;
2. every distinct S3 store key from `settings.entries` and
   `settings.defaults` that differs from the current chain.

`space` resolves each identity with `s3store.Resolve`, so the effective
endpoint, region, and path style are known before a listing. A resolved empty
region becomes `us-east-1` for display and for the stored target. A resolved
empty endpoint is shown as `(AWS)`.

Only `ListBuckets` is called for a listing. A bucket is never probed. An
identity whose `Admin.Identity` fails is reported as unreachable with its hint;
no `ListBuckets` request follows.

### Listing

One group per reachable identity, hosted groups first and S3 groups after them.
S3 identities are ordered current chain first, then recorded profiles in
settings-file order. The header is:

```text
S3  endpoint=(AWS)  region=eu-north-1  profile=dev  key=AKIAIOSFODNN7EXAMPLE
```

The field separators and the row shape are Phase 4's and the README examples',
so an S3 row renders like a hosted row.

- `endpoint` is the normalized effective endpoint, or `(AWS)`;
- `profile` is the profile name, or `(ambient)`;
- `key` is the full access key id, display only.

Rows are sorted by name. A row is `[markers]<name>[(<directories>)]`; the
access and owner columns are empty because the header names the account. The
marker rules are Phase 4's.

Directories come from S3 settings entries that match the name and the exact
identity fields `{endpoint, region, profile}`: an empty endpoint is the
provider default, an empty profile the ambient chain, and an empty region
normalizes to `us-east-1`, so none is a wildcard. Recorded store keys are
resolved before they are appended, and keys that resolve to one identity form
one group. Entries are in settings-file order; the display bound and the row
shape are Phase 4's (README,
`Directory display in a space row`), so an S3 row renders like a hosted row.

A configured default or a recorded target that the listing did not return is
synthesized with `Reachable: false` and marked `!`, so a binding never
disappears. A row that is both the default and unreachable prints `*!`.

An empty group prints:

```text
No buckets; run 'slivingdoc space new <name>' to create one.
```

A failed list shows the configured default and the recorded rows marked `!`,
writes the hint to stderr, and exits zero even under `--storage s3`. Only an
identity that cannot be resolved refuses, and only when `--storage s3` or a
flag names it; an identity an environment variable selected under `auto`
degrades its group instead. Only a reachable row can become the default.

The hint is one line, sanitized, and names no key, secret, or private path:

```text
! this login may not list buckets (s3:ListAllMyBuckets is denied); the configured default is shown
```

The `! ` is part of the printed line in plain and styled output, not a `tui`
mark (Phase 4).

### `space new`

An S3 create addresses the identity the flags select, never a recorded or
default identity (README, `space new`). The command validates the name with
`storage.ValidateNewSpaceName` before any request, then calls
`Admin.CreateBucket`.

| Outcome | Refusal |
| --- | --- |
| `s3store.ErrBucketOwned` | `space new: bucket analytics already exists and this account owns it; run 'slivingdoc space --profile dev analytics' to use it` |
| `s3store.ErrBucketExists` | `space new: bucket analytics already exists in another account; choose another name` |
| `storage.ErrAccessDenied` | `space new: this login may not create a bucket (s3:CreateBucket is denied); check the profile or endpoint` |
| `storage.ErrTransport` | `space new: could not create bucket analytics; try again later` |

The success lines are:

```text
Created bucket analytics (endpoint=(AWS), region=eu-north-1, profile=dev).
It is not the default; run 'slivingdoc space --profile dev analytics' to make it the default.
```

The follow-up hint repeats the flags that selected the identity:
`--endpoint`, `--region`, `--profile`, and `--path-style` when each was set.
The flags precede the name: `slivingdoc space --profile dev analytics`.
The create changes no default. Because a create stores no default, it refuses
`--prefix` instead of ignoring it (README, `space` flag table).

### `space <name>`

Before any listing, `PrepareSpace` validates the resolved prefix with
`storage.ValidatePrefix`. This function defines the accepted prefix grammar.
An invalid prefix refuses the command with a nonzero exit and writes nothing.

The command searches every backend's listing. One identity listing the name
stores the default; more than one refuses. If an ambiguity includes an S3
identity, each S3 candidate lists endpoint, normalized region, and profile.
The refusal names the selector flags that differ, in this order: `--profile`,
`--region`, `--endpoint`. If none of those fields differs, it names all three
as choices. `(AWS)` and `(ambient)` in that list mean the value is left unset,
exactly as in a header. The stored target is:

```go
settings.Target{
    Kind:      settings.KindS3,
    Endpoint:  identity.endpoint,
    Region:    identity.region,
    Profile:   identity.profile,
    Name:      name,
    Prefix:    validatedPrefix, // validated --prefix or empty
    PathStyle: effectivePathStyle,
}
```

The stored prefix is the value that `storage.ValidatePrefix` validated.

`effectivePathStyle` is true when the identity resolved a custom endpoint, when
`--path-style` is set, or when the name contains a dot on the default AWS
endpoint. A dotted legacy bucket therefore records `pathStyle=true`.

The store is a read-modify-write of `settings.Set.PutDefault` under
`workspaces.lock`. The line is:

```text
The default bucket for s3 endpoint=(AWS), region=eu-north-1, profile=dev is now analytics.
```

### Auto mode

Hosted groups come first, then S3. An S3 group appears only when an S3 signal
exists or a record or default names an S3 target. A machine with only a stored
hosted login never loads AWS configuration. When no backend is reachable, the
command refuses naming both fixes: `slivingdoc login`, and an AWS profile or
endpoint.

This phase replaces the phase-local S3-unavailable refusal introduced in
`phase-4-space-new-hosted.md`. The phase-local refusal rows are rewritten to
assert the S3 backend.

### Test seam

`internal/app` gains one unexported seam so pure unit tests can list and create
without AWS:

```go
type s3Admin interface {
    ListBuckets(ctx context.Context) ([]s3store.Bucket, error)
    CreateBucket(ctx context.Context, name string) error
    Identity(ctx context.Context) (s3store.Identity, error)
}

// ProcessOptions carries an unexported factory for these tests.
```

The seam stays phase-local. Integration tests plant `AWS_CONFIG_FILE`,
`AWS_SHARED_CREDENTIALS_FILE`, `AWS_ENDPOINT_URL_S3`, and test credentials that
point at the shared SeaweedFS container.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| Only `ListBuckets` lists | no probe, no `HeadBucket` | `TestSpaceS3UsesListBucketsOnly` |
| A create uses the flag-selected identity | current chain only | `TestSpaceS3CreateUsesCurrentIdentity` |
| A recorded target stays visible | synthesized `Reachable=false` rows | `TestSpaceS3ShowsUnreachableRecord` |
| An auto failure never hides another backend | per-identity hints | `TestSpaceS3DegradedList` |
| An explicit failure refuses | `--storage s3` / explicit profile | `TestSpaceS3ExplicitFailureRefuses` |
| A dotted bucket records path style | effective rule | `TestSpaceS3DottedBucketPathStyle` |
| The key id is display only | header only; never stored or logged | `TestSpaceS3KeyIDIsDisplayOnly` |
| No hosted-only machine loads AWS config | signal check before admin construction | `TestSpaceAutoHostedOnlyLoadsNoAWS` |

### Documentation

| Doc | What must become true |
| --- | --- |
| `architecture/login.md` | The `space` command's flow and prose gain the S3 backend: the `S3  endpoint=… region=… profile=… key=…` header, rows with directory annotations and `*`/`!`, `No buckets; …`, the four create refusals (`ErrBucketOwned`, `ErrBucketExists`, the `s3:CreateBucket` denial, a transport failure), the S3 default line, the degraded-list hint, the auto rule (an S3 group only with a signal, record, or default), the `space new` prefix refusal, and the dotted-bucket path style. The hosted sentences Phase 4 wrote stay. |
| `architecture/cli.md` | The `Space.Run` flow names both backends and `--storage s3`, and says the create and default paths belong to the backend the flags select. |
| `architecture/s3store.md` | The identity is documented as `{profile, endpoint, region, pathStyle}`, `s3store.Resolve` provides the effective endpoint, region, and path style without a bucket or a credential retrieval, and `Admin` maps bucket create and list failures as the phase specifies. |
| `architecture/errors.md` | No new category: a `space` refusal is plain CLI text outside the taxonomy, which carries reason and action tokens for MCP results only; the S3 refusals name `s3:ListAllMyBuckets` and `s3:CreateBucket`. |
| `architecture/security.md` | The access key id is display only; it is never stored, logged, or returned over MCP. |

### Files

| File | Change |
| --- | --- |
| `internal/app/space.go` | Add the S3 `spaceBackend`, including identity resolution, `ListBuckets` listing, create through `Admin.CreateBucket`, and `Set.Default` storage by store key. |
| `internal/app/app.go` | Add the unexported `s3Admin` factory field to `ProcessOptions` for the unit tests. |
| `internal/app/present.go` | Extend the `space` plain renderer to print S3 headers, rows, and unreachable `!` markers with directory annotations. |
| `internal/app/space_test.go` | Add unit tests for the S3 listing, create, default store, dotted bucket path-style rule, and degraded behavior using the injected admin seam. |
| `internal/app/space_s3_test.go` | Add the S3 identity, listing, create, default, and refusal proofs, including `TestSpaceS3RecordWithoutRegion`. |
| `internal/integrationtest/scenario_space_s3_test.go` (new) | Add the S3 space scenario test that runs against the SeaweedFS container with planted AWS configuration. |

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `space --storage s3` with a reachable identity | fake admin | header and sorted rows | one `ListBuckets` | a probe, a create |
| `space` in auto with hosted and S3 | sitetest + fake admin | hosted group first, S3 second | one spaces request, one list | an S3 load for a hosted-only machine |
| a recorded target not listed | planted settings, fake admin | row with `!`, hint | none | dropping the record |
| a failed list in auto | fake admin error | default and records with `!`, exit 0 | none | hiding the hosted group |
| an explicit S3 failure | fake admin error | nonzero refusal | none | a listing |
| `space --profile dev new analytics` | fake admin | create lines | one `CreateBucket` | a default change |
| `ErrBucketOwned` | fake admin error | the owned refusal | none | a create retry |
| `ErrBucketExists` | fake admin error | the other-account refusal | none | a create retry |
| `space --profile dev analytics` | planted settings, fake admin | the default line | one `SetDefault`, file written under the lock | a second default |
| a name listed by two identities | two fake admins | refusal listing endpoint, region, and profile and naming differing selectors (`--profile`, `--region`, `--endpoint` order) | none | a write |
| a dotted legacy bucket | fake admin, default endpoint | target stored with `pathStyle=true` | one default write | a create |
| a real list and create | SeaweedFS, planted AWS env | the bucket exists | one list, one create | a probe |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| The header prints endpoint, region, profile, and key id | `TestSpaceS3Header` |
| A listing calls only `ListBuckets` | `TestSpaceS3UsesListBucketsOnly` |
| Rows are sorted and carry matching directories | `TestSpaceS3Directories` |
| A recorded target not listed is `Reachable=false` and marked `!` | `TestSpaceS3ShowsUnreachableRecord` |
| A failed auto list keeps the default and records visible and exits 0 | `TestSpaceS3DegradedList` |
| An explicit failure refuses | `TestSpaceS3ExplicitFailureRefuses` |
| A hosted-only auto listing loads no AWS configuration | `TestSpaceAutoHostedOnlyLoadsNoAWS` |
| Create uses the flag-selected identity | `TestSpaceS3CreateUsesCurrentIdentity` |
| Create prints the exact lines and no default line | `TestSpaceS3Create` |
| `ErrBucketOwned` prints the owned refusal | `TestSpaceS3CreateOwned` |
| `ErrBucketExists` prints the other-account refusal | `TestSpaceS3CreateExists` |
| A denied create names `s3:CreateBucket` | `TestSpaceS3CreateDenied` |
| `space <name>` stores the full S3 target | `TestSpaceS3SetDefault` |
| The stored prefix is the validated `--prefix` or empty | `TestSpaceS3SetDefaultPrefix` |
| `space new` with `--prefix` refuses and creates nothing | `TestSpaceS3CreateRefusesPrefix` |
| An invalid resolved prefix refuses before listing or writing | `TestSpaceS3InvalidPrefix` |
| A dotted bucket records path style | `TestSpaceS3DottedBucketPathStyle` |
| Two identities listing the name refuse | `TestSpaceS3SetDefaultAmbiguous` |
| The store-key default is written under `workspaces.lock` | `TestSpaceS3SetDefaultLocks` |
| A real list and create work against SeaweedFS | `TestScenarioSpaceS3` |
| An unknown profile prints the refusal naming `~/.aws/config` | `TestSpaceS3UnknownProfile` |
| The docs name the S3 `space` surface | `rg -n -e 'S3  endpoint=' -e 's3:CreateBucket' architecture/login.md` hits both |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| no reachable S3 identity in auto | hint, other groups still shown, exit 0 | `TestSpaceS3DegradedList` |
| explicit identity cannot authenticate | nonzero refusal | `TestSpaceS3ExplicitFailureRefuses` |
| `ListBuckets` denied | hint naming `s3:ListAllMyBuckets` | `TestSpaceS3ListDenied` |
| `ListBuckets` transport failure | hint, exit 0 in auto | `TestSpaceS3ListTransport` |
| `Identity` fails | identity marked unreachable, hint | `TestSpaceS3IdentityDenied` |
| create name invalid | `storage.ValidateNewSpaceName` message | `TestSpaceS3CreateInvalidName` |
| create denied | refusal naming `s3:CreateBucket` | `TestSpaceS3CreateDenied` |
| bucket owned | the owned refusal | `TestSpaceS3CreateOwned` |
| bucket exists elsewhere | the other-account refusal | `TestSpaceS3CreateExists` |
| create transport failure | retryable refusal | `TestSpaceS3CreateTransport` |
| `space <name>` absent | refusal naming what is listed | `TestSpaceS3SetUnknown` |
| `space <name>` listed twice | refusal listing endpoint, region, and profile and naming differing selectors (`--profile`, `--region`, `--endpoint` order) | `TestSpaceS3SetDefaultAmbiguous` |
| resolved prefix fails `storage.ValidatePrefix` | nonzero refusal; no listing or settings write | `TestSpaceS3InvalidPrefix` |
| `space new` with `--prefix` | refusal naming `--prefix`; no create | `TestSpaceS3CreateRefusesPrefix` |
| `PutDefault` fails | the default is unchanged and the error returned | `TestSpaceS3SetDefaultWriteFailure` |
| no backend reachable at all | refusal naming `slivingdoc login` and an AWS profile or endpoint | `TestSpaceNoBackendReachable` |
| unknown profile | refusal naming the profile and `~/.aws/config` | `TestSpaceS3UnknownProfile` |

## Implementation notes

### 2026-10-19, session 5: partial implementation and handover

Worker: phase-6 agent, 2026-10-19, session 5. The session stopped at the
context limit with the worklog tree uncompilable. Nothing is committed; the
working tree carries all of the work below.

#### Complete and verified

`internal/s3store`:

- `defaultRegion` was exported as `DefaultRegion` (used by `admin.go` and the
  tests).
- `ErrProfileNotFound` was added; `loadAWSConfig` maps the SDK's
  `awsconfig.SharedConfigProfileNotExistError` to it; `TestResolveUnknownProfile`
  asserts `errors.Is`.
- `go test ./internal/s3store/ -count=1` passes (13 s).

`internal/app` (compiles only after the remaining items below):

- `app.go`: `ProcessOptions.newS3Admin s3AdminFactory` (unexported, test-only).
- `space.go` now contains: the `s3Admin` interface (`ListBuckets`,
  `CreateBucket`, `Identity`), `s3AdminFactory`, `realS3Admin` (calls
  `s3store.Resolve` then `s3store.NewAdmin`), `spaceS3Settings`,
  `resolveSpaceS3Settings`, `firstNonEmpty`, the rewritten `PrepareSpace`,
  `resolveBackends`, `chooseLogins`, `hostedRefusal`, `s3Selected`,
  `s3CreateSelected`, `s3EnvSelected`, `s3FlagSelected`, `hasS3Record`,
  `resolveCreate`, `prefixSource`, the `s3Identity` type and `s3IdentityOf`,
  `s3Endpoint`, `recordedS3Keys`, `prepare(ctx)`, `newS3Backend`,
  `normalizeS3Resolved`, `s3HeaderOf`, `SpaceHeader.storeKey`,
  `s3IdentityRefusal`, `endpointDisplay`, `profileDisplay`, `regionDefault`,
  `command`, `noBackendRefusal`, a backend-aware `create`, `createHintParts`,
  `ambiguousRefusal`, `printDefault(header, row)`, and the `Run` call to
  `prepare(ctx)`.

#### Remaining compile errors (current `go build ./internal/app/`)

```text
space.go:549 undefined: s3Space (in newS3Backend)
space.go:550 undefined: s3Space
space.go:657 *hostedSpace does not implement spaceBackend (SetDefault still
           takes settings.Target; the interface now takes name string)
space.go:745/758/809 listing still returns ([]spaceGroup, bool), the callers
           expect ([]spaceGroup, error)
space.go:822 spaceCandidate still has endpoint string; setDefaultFrom now
           builds {backend, header, row}
```

#### Remaining work, in order

1. `space.go`: change `hostedSpace.SetDefault` to
   `SetDefault(ctx context.Context, name string) error` and call
   `storeDefault(ctx, h.s.file, h.login, name)`. Delete its
   `settings.Target` handling.
2. `space.go`: add the `s3Space` backend and its methods:
   - fields `s *Space`, `id s3Identity`, `admin s3Admin`,
     `resolved s3store.Resolved`, `header SpaceHeader`,
     `key settings.StoreKey`, `buildErr error`.
   - `Kind`, `Header`, `Default(ctx)` = `h.s.set.Default(h.key)`.
   - `List(ctx)`: if `h.admin == nil`, return `h.degraded(h.buildHint())`;
     call `Identity` first and set `h.header.KeyID` from `AccessKeyID` (no
     `ListBuckets` after an identity failure). An explicit identity failure
     returns `h.s.s3IdentityRefusal(h.id, err)`; otherwise
     `h.degraded(h.identityHint(err))`. `ListBuckets` failure always degrades
     with `h.listHint(err)` (denied hint is the exact phase text naming
     `s3:ListAllMyBuckets`; transport is a sanitized one-line hint). One
     `ListBuckets` only, no probe.
   - rows: sorted by name, directories by `matches`, and the default +
     recorded targets the answer did not return appended with
     `Reachable: false` plus a hint, returning a `spaceListError{hints}` (not
     failed) when there are hints.
   - `matches(t)`: `KindS3`, same name, and the exact fields
     `{endpoint, region, profile}` equal the header's after an empty region
     normalizes to `us-east-1`.
   - `Create(ctx, name)`: `Admin.CreateBucket`, then the four refusals from
     the phase table (`ErrBucketOwned` with `createHint`, `ErrBucketExists`,
     `storage.ErrAccessDenied` naming `s3:CreateBucket`, `storage.ErrTransport`
     retryable text); success row with `Reachable: true`.
   - `SetDefault(ctx, name)`: build
     `settings.Target{Kind: KindS3, Endpoint: header.Endpoint,
     Region: header.Region, Profile: header.Profile, Name: name,
     Prefix: s.s3.prefix, PathStyle: h.pathStyleFor(name)}` where
     `pathStyleFor` is `resolved.PathStyle || (header.Endpoint == "" &&
     strings.Contains(name, "."))`, then `storeS3Default`.
   - `storeS3Default(ctx, env, target)`: locate the file with
     `settings.Locate(EnvLookup(env), runtime.GOOS)`, `file.Lock(ctx)`,
     `file.Load()`, `file.Save(set.PutDefault(target))`, and release the lock
     in a deferred error join (copy the shape of `storeDefault`).
3. `space.go`: change `listing` to return `([]spaceGroup, error)`; a backend
   error that is not a `*spaceListError` is returned as the command's fatal
   error (do not sanitize it again; `s3IdentityRefusal` already redacts).
   Insert the orphan group after the last hosted group, not at the end:
   count hosted groups and `slices.Insert(groups, hostedCount, *orphan)`.
4. `space.go`: change `spaceCandidate` from `{backend, endpoint, row}` to
   `{backend, header SpaceHeader, row}`.
5. `space.go`: `hints` prints `No default bucket; ...` for an S3 group and
   the existing `No default space; ...` for hosted.
6. `present.go`: `render` must print a backend-aware header:
   `HOSTED  <endpoint>  <account>` as today, and
   `S3  endpoint=<(AWS)|url>  region=<r>  profile=<(ambient)|p>[  key=<id>]`
   with the `key=` field omitted when the identity was never reached.
   Empty groups print `No buckets; run 'slivingdoc space new <name>' to create
   one.` for S3. `spaceRowLine` takes the kind and omits the access and owner
   for an S3 row. Use the existing `endpointDisplay`/`profileDisplay` helpers
   and `style.Brand("S3")`.
7. `cmd/login/login.go`: the `space` Describe and `spaceHelp` still say
   hosted-only / "an S3 signal is refused"; update them to both backends.
8. Tests: add a fake factory to `loginRig` (`newS3Admin` in `opts()`),
   keyed by `cfg.Profile`, returning a chosen `s3store.Resolved` and a fake
   whose `ListBuckets`/`CreateBucket`/`Identity` record calls and return
   injected errors. Then write the phase acceptance tests named in the tables
   above (header, list-only, directories, unreachable record, degraded list,
   explicit refusal, hosted-only loads no AWS, create identity/lines, owned /
   exists / denied / transport, set default full target + prefix, create
   prefix refusal, invalid prefix, dotted path style, ambiguous, lock held,
    write failure, unknown profile, key id display only, no backend). Add the
    explicit-backend and auto-signal tests for the S3 behavior.
   Update `TestSpaceWithoutLogin`, `TestSpaceSetUnknown`,
   `TestSpaceSetIncomplete` and `TestSpaceSetUnlistedDefault` for the new
   refusal text (`no backend lists ...`, `no reachable backend listed ...`,
   and the both-fixes no-backend refusal).
9. Integration: add `internal/integrationtest/scenario_space_s3_test.go`
   using `tests3.Ensure(t)`, `cliRoots(t)`, and
   `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_ENDPOINT_URL_S3`/`AWS_REGION`
   (+ `credentials.DirEnv`). Create a fresh bucket with
   `space --storage s3 new <name>`, check the exact two create lines, list
   and assert the header plus the new row, set it as default, and read
   `workspaces.json` to prove the stored target. Delete the bucket in
   `t.Cleanup` through `suite.Raw`.
10. Docs: `architecture/login.md` (the S3 space surface and remove the
    phase-4 "S3 targets are not available" sentences),
    `architecture/cli.md` (Space.Run names both backends), `architecture/
    s3store.md` (identity, `Resolve`, `DefaultRegion`, `ErrProfileNotFound`),
    `architecture/errors.md` (space refusals are plain CLI text outside the
    taxonomy), `architecture/security.md` (key id display only, never stored,
    logged, or returned over MCP).
11. Run `go run mvdan.cc/gofumpt@v0.11.0 -w -l .`, `go vet ./...`,
    `staticcheck ./...`, `go fix -diff ./...`, `make qa`, and the dupl
    signal; record the exact commands and results here and in the README
    journal.

#### 2026-10-19, session 6: completion

Worker: phase-6 agent, 2026-10-19, session 6. The working tree already
carried the handover items, and `go build ./...`, `go vet ./...`, and the
app and S3 tests were clean when the session began. This session checked
each acceptance and error row against the tree, found no missing behavior,
and completed the documentation. It also strengthened
`TestSpaceS3DegradedList` with a hosted login, so the test proves a failed
S3 listing hides no other backend. No production code changed; the review
below records the doc corrections.

Gates, in order:

- `go test ./internal/app/ ./internal/s3store/ ./internal/integrationtest/ -count=1` passed.
- `go test -race -count=3 -timeout=30s ./internal/app/ ./internal/s3store/ ./internal/integrationtest/` passed.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .` and `go vet ./...` printed nothing.
- `make qa` exited zero; coverage 89.2 % against the 70 % floor, and all 35 npm tests passed.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` reported three clone groups: the declarative flag registration pair in `internal/app/login.go` and `internal/app/space.go`, and two pre-existing test tables. All are acceptable under the duplication policy.
- `rg -n -e 'S3  endpoint=' -e 's3:CreateBucket' architecture/login.md` hits the flow and the S3 prose, and the stale `S3 targets are not available in this build` sentence is gone.

The acceptance and error tests are the phase's names in
`internal/app/space_s3_test.go`; `TestScenarioSpaceS3` runs a real create,
list, and store-keyed default against the shared SeaweedFS suite. The test
seam is `ProcessOptions.newS3Admin`, injected by `loginRig`; it returns
the fake administration and the `s3store.Resolved` values.

Material decisions confirmed in this session:

- The header's access key id is display only. It is printed from the
  identity the listing reached, never stored, never logged, and never part
  of an MCP result.
- A listing calls `ListBuckets` only. `Identity` is a credential read, not
  a bucket call, and no bucket is probed.
- A `--storage s3` choice or a flag that names an identity and cannot be
  resolved refuses the command; a list failure, a recorded identity's
  failure, and an environment-selected identity's failure under `auto`
  degrade the group with a literal `! ` hint, so `--storage s3` over a denied
  listing still exits zero while an unresolvable flag-named profile refuses.
- A dotted bucket on the default AWS endpoint records `pathStyle=true`,
  and a custom endpoint always resolves path style through the SDK.

No behavior, test, flag, error category, or protocol contract changed in
this session.

#### Material decisions made in session 5

- S3 identities are resolved in `Space.Run` via `prepare(ctx)`, not in
  `PrepareSpace`: `PrepareSpace` has no context, and a hosted-only command
  must never load AWS configuration.
- The injected factory returns `(s3Admin, s3store.Resolved, error)` because
  the phase's `s3Admin` interface has no path-style accessor and `Resolve`
  must stay the source of the effective values.
- An explicit identity's authentication/construction failure refuses the
  command; a `ListBuckets` failure always degrades (the README's
  `--storage s3 --profile dev` denied-list example exits zero), and a
  recorded identity's failure always degrades.
- In auto, a create selects S3 only when a flag or a named AWS environment
  variable selects it (profile, region, path style, prefix, or an S3
  endpoint). The shared AWS files are a listing signal only, so a machine
  with a hosted login keeps creating hosted spaces.
- `spaceBackend.SetDefault` takes the name, not a `settings.Target`; each
  backend composes its own target (the S3 one adds prefix and path style).
- All-hosted ambiguity keeps the phase-4 text (`logins`, `--endpoint`); any
  S3 candidate changes it to `identities`, lists endpoint, normalized region,
  and profile, and names the differing selectors in the order `--profile`,
  `--region`, `--endpoint`.

## Review findings

Session 6 found three documentation defects, all fixed in the same change:

1. `architecture/login.md` still carried the phase-4 paragraph that said
   `--storage s3` was refused by the build and that an S3 signal in `auto`
   produced a phase-local refusal. It now describes the S3 listing, the
   four create refusals, the store-keyed default with prefix and path
   style, the display-only key id, and the auto rule.
2. `architecture/s3store.md` named the removed `defaultRegion` symbol and
   omitted `ErrProfileNotFound` from the `admin.go` row. It also lacked the
   display-only key-id sentence.
3. `architecture/errors.md`, `architecture/security.md`,
   `architecture/cli.md`, and `architecture/testing.md` did not state the
   `space` refusal boundary, the key-id rule, the `Space.Run` backend flow,
   or the new S3 scenario. Each now does.

No remaining acceptance or error row is unimplemented.

#### Post-close review remediation (2026-10-20, session 10)

[review-2026-10-20.md](./review-2026-10-20.md) found ten items after the
worklog closed. This session resolved all of them:

1. S3 grouping. `s3Space.matches` compares the recorded endpoint, region and
   profile exactly after normalizing an empty region to `us-east-1`; an empty
   endpoint is the provider default and an empty profile the ambient chain,
   never a wildcard. `Space.prepare` dedupes on the resolved `backend.key`, so
   two raw keys that resolve to one identity produce one group. This replaces
   the phase-6 sentence that made every non-empty field a match. The chosen
   semantics are decision 54. `TestSpaceS3Directories` now asserts the
   complete stdout and stderr for the full case and a partial-record case.
2. Help text. `spaceHelp` names the hosted default as what `serve`, `pull` and
   `commit` use, and says only that an S3 default is stored under the
   identity's store key, so it promises no S3 default resolution this build
   does not have.
3. `architecture/config.md` and decision 49 now say `space` reads the settings
   for its directory column and the S3 defaults it marks; the "only
   directories" wording is gone.
4. `architecture/running.md` names the S3 listing, create, and default surface
   in its command table and in the `space` section.
5. The README and phase-4 `spaceBackend` snippets show `Default()` and
   `SetDefault(ctx, name)`.
6. The `present.go` preamble limits its no-change claim to the login and
   logout renderings; the space listing is new.
7. `s3Space.pathStyleFor` records path style for any custom endpoint, so a
   degraded recorded identity with a custom endpoint still stores it.
   `TestSpaceS3FailedIdentityKeepsCustomEndpointPathStyle` proves it.
8. The picker now refuses an unreachable `!` row through
   `Space.unreachableRefusal`, the same refusal `space <name>` gives, and
   changes nothing. `TestSpacePickerRefusesUnreachable` proves it.
9. `spaceBackend.Default` returns `(settings.Target, bool)`; it cannot fail,
   so `Space.listing` no longer discards an error.
10. In `auto`, an identity selected by an environment variable degrades on a
    resolution failure and keeps the other groups; `--storage s3` or a flag
    that names the identity refuses. `s3IdentityFlagSelected` is the flag test.
    `TestSpaceAutoS3IdentityFailureDegrades` covers the environment case and
    `TestSpaceS3UnknownProfile` still covers the flag case.

Gates: `go test ./internal/app ./internal/settings ./internal/s3store -count=1`
passed before the change. After it, `go test ./internal/app ./internal/cli
./internal/integrationtest ./internal/settings ./internal/s3store -count=1`
passed, and `make qa` was green with the same 70 % coverage floor and npm
suite as the phase-8 gate. The details and commands are in
[review-2026-10-20.md](./review-2026-10-20.md).

#### Review 2 (2026-10-20, session 11)

Round 2 re-ran `make qa` (exit 0, coverage 89.4 %, 35 npm tests) and rechecked
every round-1 resolution in this phase; all hold. Four findings concern the S3
surface. Two are majors and keep the phase reopened; the others are a minor and
a note:

- **R2-01 (major).** A recorded or default S3 target whose `region` is empty is
  dropped from the listing. `Space.prepare` builds a recorded identity with
  `Region: key.Region` raw, so the selected profile or the ambient chain
  supplies its region; the resolved key then equals the current identity's and
  the dedupe skips the recorded group, while `s3Space.matches` compares
  `regionDefault("") = us-east-1` against the command's region and cannot bind
  the record. Proven with a record for `/a`, profile `dev` resolved to
  `eu-north-1`: the output was the header and `  analytics`, with no `(/a)` and
  no hint. This contradicts decision 54 and invariant 7. Fix: resolve a
  recorded identity with `regionDefault(key.Region)` (an empty region is a
  value, `us-east-1`), and add `TestSpaceS3RecordWithoutRegion` with a
  region-less record beside a non-default profile region.
- **R2-02 (major, shared with Phase 7).** The picker's S3 create path calls
  `backend.Create` without `storage.ValidateNewSpaceName`. A fake-admin
  demonstration created `Bad_Name`; S3 also accepts dotted legacy names, so a
  name the portable grammar forbids can be created. This breaks invariant 2
  and "Creation is the only place it applies". Fix in `Space.pick` (or in both
  backends' `Create`) and add `TestSpacePickerRefusesAnInvalidS3Name`.
- **R2-03 (minor).** `ambiguousRefusal` prints S3 candidates as
  `endpoint=... profile=...`, omitting the region, and names only `--profile`
  or `--endpoint`. Two identities that differ only in region print identical
  entries and the selecting field, `--region`, is never named. Include the
  region and name `--region` when the candidates differ by it; update the
  README and `architecture/login.md`.
- **R2-10 (note).** `SpaceHeader`/`Header()` is named after the `space`
  command but carries a store-identity group header; for S3 it names a bucket
  identity, not a space. Rename to `TargetHeader`/`GroupHeader`, or state the
  command-prefix convention once in `architecture/login.md`.

Verified good in this round: exact endpoint/profile matching (including the
partial-record case), the resolved-key dedupe for identical identities, the
degraded S3 group with its `!` rows and hint, `ListBuckets` as the only bucket
read, the create refusals, the store-keyed default with prefix and path style,
and the display-only key id. The tests named in the phase and
`TestSpaceS3Directories`, `TestSpaceS3SetDefault`, `TestSpaceS3Create*`,
`TestSpaceS3FailedIdentityKeepsCustomEndpointPathStyle` pass.

#### Remediation (2026-10-20, session 12)

- **R2-01.** `Space.prepare` resolves an empty recorded region with
  `regionDefault`, and the test fake now honors an explicit region as the
  resolver does. `go test ./internal/app -run TestSpaceS3RecordWithoutRegion
  -count=1 -v` passed; the row lists `/a` under `us-east-1` without `!`.
- **R2-02.** `Space.pick` applies `storage.ValidateNewSpaceName` before any
  backend create and returns the `space new` refusal wrapper. The injected S3
  admin test proves no create was called and checks the portable grammar line.
  `go test ./internal/app -run TestSpacePickerRefusesAnInvalidS3Name -count=1
  -v` passed. Phase 7 must re-run its shared check.
- **R2-03.** S3 ambiguity candidates now show endpoint, normalized region, and
  profile; the refusal names only the selector fields that differ. The README
  example and `architecture/login.md` describe this behavior.
  `go test ./internal/app -run TestSpaceS3SetDefaultAmbiguous -count=1 -v`
  passed for identities that differ only by region.
- **R2-10.** Kept `SpaceHeader` and documented the command-prefix naming
  convention once in `architecture/login.md`.
  `rg -n 'Command-prefixed names in' architecture/login.md` found the statement.

Focused package verification passed:
`go test ./internal/app ./internal/cli ./internal/integrationtest -count=1`.
The required focused remediation check passed:
`go test ./internal/app -run 'TestSpaceS3RecordWithoutRegion|TestSpacePickerRefusesAnInvalidS3Name|TestSpaceS3SetDefaultAmbiguous' -count=1 -v`.
`make qa` exited 0. Formatting, vet, staticcheck, `go fix -diff`, the full
race test suite, and build passed; coverage was 89.3% against the 70% floor.
All 35 npm tests passed, with none skipped. The earlier integration test
timeout did not recur. The dupl check reported the same three clone groups
recorded at initial completion; all are acceptable under the repository's
duplication policy.

#### Review 3 (2026-10-20, session 16)

- **R3-03 (minor).** The Phase 6 specification retains the pre-remediation S3
  ambiguity description: it says the refusal names `--profile` or
  `--endpoint` and lists only endpoint/profile (`phase-6-space-s3.md:122-125`),
  and its integration and error rows repeat those selectors
  (`phase-6-space-s3.md:230`, `:276`). The shipped
  `ambiguousRefusal` prints endpoint, normalized region, and profile, then names
  the selector fields that differ (`internal/app/space.go:980-1019`).
  `TestSpaceS3SetDefaultAmbiguous` proves the region-only case and its
  `--region` refusal (`internal/app/space_s3_test.go:690-697`). The README and
  `architecture/login.md` already state the current behavior. Update the Phase
  6 specification and the related integration/error rows to match. This is a
  stale worklog contract, not a code defect; the phase remains Complete under
  the minor-finding rule.

#### Remediation (2026-10-20, session 18)

- **R3-03.** Updated the setting, integration-contract, and error-coverage
  wording to match `ambiguousRefusal`: S3 candidates list endpoint, normalized
  region, and profile; differing selectors are named in `--profile`,
  `--region`, `--endpoint` order. The region-only refusal matches
  `TestSpaceS3SetDefaultAmbiguous` and the README example. The Phase 6 status
  remains Complete.
- The field and selector check passed:

  ```sh
  rg -n 'endpoint, normalized region|--profile.*--region.*--endpoint' worklogs/26-10-18-space-admin-surface/phase-6-space-s3.md
  ```

  The following check found no old wording in the current specification,
  integration contract, or error coverage. Historical review findings retain
  their original wording:

  ```sh
  sed -n '116,285p' worklogs/26-10-18-space-admin-surface/phase-6-space-s3.md | rg -n 'names `--profile` or|names only `--profile`|endpoint=\.\.\. profile=\.\.\.|refusal naming `--profile`/`--endpoint`'
  ```

  `git diff --check` passed. No source, test, or architecture file changed, so
  `make qa` was not rerun; session 17's green gate on the identical code tree
  remains the test evidence (89.3% coverage, 35 npm tests).

### V6-03 (note, evidence ownership)

The `Files` table omits `internal/app/space_s3_test.go`. That file exists and
contains `TestSpaceS3RecordWithoutRegion`, the ambiguity test, and the S3
listing and create tests. Add the file to the table.

### V6-07 (note, doc falsification)

The README invariant says `space` reads a record only for directory display.
`Space.recordedS3Keys`, `Space.prepare`, and `s3Space.Default` also use stored
S3 targets and defaults to prepare listing groups. The invariant should say
that records may add listing groups but do not select the target of a create
or set-default operation.

### Resolution (2026-10-20, session 20)

V6-03 is resolved. `Files` now includes `internal/app/space_s3_test.go` and
names `TestSpaceS3RecordWithoutRegion`. Phase 6 remains Complete.
