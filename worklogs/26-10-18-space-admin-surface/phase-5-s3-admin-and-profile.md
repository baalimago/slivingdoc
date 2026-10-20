# Phase 5: S3 admin and profile

**Status:** Complete (2026-10-19, session 4)

README: [README.md](./README.md)

## Goal

Add the profile selector, the resolved S3 identity, and the bucket administration
calls that the S3 `space` backend needs.

## Specification

### `s3store.Config.Profile`

`Config` gains `Profile string`. `New` passes
`awsconfig.WithSharedConfigProfile(cfg.Profile)` when it is non-empty, so a
profile beats `AWS_PROFILE` for the run. The profile is a selector, never a
secret.

`app.Flags` gains `--profile`:

| Layer | Value |
| --- | --- |
| Flag | `--profile` |
| Environment | `AWS_PROFILE` |
| Default | empty, the ambient chain |

`config.profile` carries the resolved value. `FlagReference` gets a row, and
`realStoreFactory` passes it to `s3store.Config`. `--profile` counts as an S3
signal in `s3Signals`, so `space` and the auto-mode ambiguity refusal see it.

### Region resolution

The shared `--region` flag has an empty default. Use `--region` when set.
Otherwise, use `AWS_REGION`, then `AWS_DEFAULT_REGION`. The flag wins over both
environment values. `cfg.Region` stays empty when only a profile may name a
region, so pass `WithRegion` only for a non-empty configured region and let the
AWS SDK resolve the profile's region. `New` then applies `us-east-1` to the
loaded configuration when the effective region is still empty, before it builds
the S3 client. `config.finish` stops refusing an empty S3 region: the value is
legal, and the store applies the fallback. `Resolve` reports the effective
region as it loaded it, and Phase 6 normalizes an empty value to `us-east-1`
for display and for the stored target. Update the `FlagReference` region row to name both environment
variables, the precedence, and the empty default.

### One loader

`Resolve`, `New`, and `NewAdmin` share one unexported loader that builds the
AWS configuration:

- `WithSharedConfigProfile` when `cfg.Profile` is set;
- `WithRegion` when `cfg.Region` is set;
- `WithBaseEndpoint` when `cfg.Endpoint` is set;
- `WithCredentialsProvider` for `cfg.AccessKey` and `cfg.SecretKey`, as `New` does
today; the one loader keeps it, so `Resolve` and `NewAdmin` take the same
configuration and still retrieve no credential;
- the test-only `httpClient` and retry options.

`Resolve` builds the configuration, then a throwaway `s3.Client` only to read
`client.Options().BaseEndpoint` and `client.Options().Region`. It sends no
request and retrieves no credentials. This is the only way to obtain the SDK's
own endpoint order (`AWS_ENDPOINT_URL_S3`, `AWS_ENDPOINT_URL`, a profile's
`services.s3.endpoint_url`) without reimplementing it.

```go
type Resolved struct {
    Profile   string
    Endpoint  string
    Region    string
    PathStyle bool
}

func Resolve(ctx context.Context, cfg Config, opts ...Options) (Resolved, error)
func (s *Store) Resolved() Resolved
```

| Field | Source |
| --- | --- |
| `Profile` | `cfg.Profile`; the app resolves `AWS_PROFILE` before calling |
| `Endpoint` | `aws.ToString(client.Options().BaseEndpoint)`, normalized by the caller |
| `Region` | `client.Options().Region` |
| `PathStyle` | the effective client option |

`New` sets path style when `cfg.Endpoint != ""`, the resolved
`BaseEndpoint != ""`, or `Options.ForcePathStyle` is true. It sets
`o.BaseEndpoint = cfg.Endpoint` when configured, to defeat
`ignore_configured_endpoint_urls`. It stores the resolved values for
`Store.Resolved()`.

`Resolved.Endpoint` and `Store.Resolved().Endpoint` are the same value. An
empty endpoint means the default AWS endpoint.

### `Admin`

```go
type Bucket struct {
    Name    string
    Created time.Time
}

type Identity struct {
    Profile     string
    AccessKeyID string
    Endpoint    string
    Region      string
}

type Admin struct { /* unexported */ }

func NewAdmin(ctx context.Context, cfg Config, opts ...Options) (*Admin, error)
func (a *Admin) ListBuckets(ctx context.Context) ([]Bucket, error)
func (a *Admin) CreateBucket(ctx context.Context, name string) error
func (a *Admin) Identity(ctx context.Context) (Identity, error)
```

`NewAdmin` ignores `Config.Bucket` and `Config.Prefix`; it never validates
either. `ListBuckets` calls `ListBuckets` once. `CreateBucket` sends a bucket
create request with `CreateBucketConfiguration` only when the effective
endpoint is the default AWS endpoint and the effective region is not
`us-east-1`; a custom endpoint sends no configuration. `Identity` retrieves the
credential with `awsCfg.Credentials.Retrieve` and returns its access key id with
the profile, endpoint, and region; it never returns or logs the secret.

### Error mapping

| Cause | Error |
| --- | --- |
| `BucketAlreadyExists` | `ErrBucketExists` |
| `BucketAlreadyOwnedByYou` | `ErrBucketOwned` |
| access denied or an unusable credential | `storage.ErrAccessDenied` |
| any other transport or service failure | `storage.ErrTransport` |

`ErrBucketExists` and `ErrBucketOwned` are exported sentinels. `ErrBucketOwned`
wins when the service names both. The refusal text belongs to the `space`
command, not this package.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| All AWS SDK use stays in `s3store` | `Admin` and `Resolve` live there | `rg -l 'aws-sdk-go-v2' --glob '*.go' --glob '!internal/s3store/**' --glob '!internal/tests3/**'` prints nothing; `internal/tests3` is test-only |
| A profile is a selector | `WithSharedConfigProfile`; no secret field | `TestProfileSelectsTheSharedSection` |
| The shared region flag has an empty default | flag binding and resolution | `TestRegionDefaultIsEmpty` |
| An empty region is not a startup refusal | `config.finish` | `TestNoRegionFallsBackToUSEast1` |
| Region precedence is flag, `AWS_REGION`, then `AWS_DEFAULT_REGION` | app configuration resolution | `TestRegionFlagBeatsEnvironment`, `TestRegionReadsAWSDefaultRegion` |
| A profile region wins when no flag or environment names a region | conditional `WithRegion` | `TestProfileRegionWins` |
| The effective endpoint and region are reported | client options | `TestResolveReportsEffectiveValues` |
| Creation sends configuration only where it must | `CreateBucket` branch | `TestCreateBucketBody` |
| No call retrieves a credential unless asked | `Resolve` uses no `Retrieve` | `TestResolveDoesNotRetrieveCredentials` |
| Administration never creates a bucket | only `Admin.CreateBucket` creates; `New`, `NewAdmin`, and `Resolve` send no create request and never validate a bucket | `TestAdminNeverCreates` |
| No live AWS endpoint is reached | recording client / SeaweedFS | `TestAdminIntegration` |

### Documentation

| Doc | What must become true |
| --- | --- |
| `architecture/s3store.md` | `Config.Profile`, `Resolve`, `Resolved`, `Store.Resolved`, `Admin`, the create-configuration rule, and the `ErrBucketExists`/`ErrBucketOwned` mapping are documented. |
| `architecture/config.md` | The region default is empty; the resolution order is `--region`, `AWS_REGION`, `AWS_DEFAULT_REGION`, the profile's region, then `us-east-1`; startup no longer refuses an empty S3 region; the flow diagram, the signal sentence, and the region bullet name `--profile` and the new order. |
| `architecture/login.md` | `--profile` joins the S3-only signals in the signal diagram and in the `resolveStorage` signal list. |
| `architecture/running.md` | `--profile` and `AWS_PROFILE` appear as an S3 selector whose default is the ambient chain. The region table row and the paragraph “The region is always explicit” name the empty flag default, `AWS_REGION`, `AWS_DEFAULT_REGION`, the profile's region, and `us-east-1`; the paragraph's sentence that “`AWS_DEFAULT_REGION` and a profile's `region` setting are ignored” is deleted, not kept beside the new order; the sentence about which flags shape the endpoint names `--profile` as a credential selector, and the Choosing-the-storage signal list names the flag. |
| `architecture/security.md` | A profile is a selector, never a secret, and `Admin.Identity` returns the access key id without the secret. |

### Files

| File | Change |
| --- | --- |
| `internal/s3store/store.go` | Add `Config.Profile` and share one AWS configuration loader with `New` and `NewAdmin`, so `Store.Resolved()` reports the values the store was built with. |
| `internal/s3store/resolve.go` (new) | Add `Resolve` and `Resolved`, which read the effective endpoint, region, and path style without a bucket or a credential retrieval. |
| `internal/s3store/admin.go` (new) | Add `Admin` with `ListBuckets`, `CreateBucket`, and `Identity`, the create-configuration rule, and the `ErrBucketExists`/`ErrBucketOwned` sentinels. |
| `internal/s3store/store_test.go` | Add unit tests for profile selection, profile region precedence, resolved endpoint and region reporting, create request shape, and error mapping. |
| `internal/s3store/admin_test.go` | Test the profile, `Resolve`, `Admin`, and bucket creation behavior named by this phase. |
| `internal/s3store/integration_test.go` | Add SeaweedFS integration tests for `ListBuckets`, `CreateBucket`, and `Identity` without reaching a live AWS endpoint. |
| `internal/app/config.go` | Bind `--profile`, resolve `AWS_PROFILE`, add the flag-reference row, resolve region from `--region`, `AWS_REGION`, and `AWS_DEFAULT_REGION` with an empty flag default, stop refusing an empty S3 region in `finish`, and add the profile and region to `config`. |
| `internal/app/app.go` | Pass `config.profile` and `config.region` to `s3store.Config` in `realStoreFactory`. |
| `internal/app/config_test.go` | Test the empty region flag default and flag/environment precedence, and rewrite `TestLoadConfigRegionRequired` as `TestEmptyRegionFlagIsLegal`: an explicitly empty `--region` is legal and the store falls back to `us-east-1`. |
| `internal/app/storage_test.go` | Test that `--profile` and `AWS_PROFILE` are S3 signals. |
| `internal/app/storage.go` | Add `--profile` to `s3Signals` and to the narrow flag-set view Phase 4 introduced (`profileSet()`), so `serve` and `space` both count it. |

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `Resolve` over a planted profile | `isolateAWSEnv`, planted `AWS_CONFIG_FILE` | the profile's endpoint and region | none | a network request, a credential retrieval |
| `Resolve` with `AWS_ENDPOINT_URL_S3` | `isolateAWSEnv` | that endpoint, path style true | none | a network request |
| `New` over a profile | recording transport | requests address the profile's endpoint | path style true | a request to AWS |
| `NewAdmin` + `ListBuckets` | recording transport | the listed buckets | one request | a bucket probe |
| `CreateBucket` on the default endpoint, `eu-north-1` | recording transport | a body with `CreateBucketConfiguration` | none | a body on a custom endpoint |
| `CreateBucket` on the default endpoint, `us-east-1` | recording transport | no configuration body | none | a configuration |
| `CreateBucket` on a custom endpoint | recording transport | no configuration body | none | a configuration |
| create answers `BucketAlreadyExists` | recording transport | `ErrBucketExists` | none | `ErrBucketOwned` |
| create answers `BucketAlreadyOwnedByYou` | recording transport | `ErrBucketOwned` | none | `ErrBucketExists` |
| `Identity` with the test credentials | SeaweedFS | the access key id, endpoint, region | none | the secret in the answer |
| list and create against SeaweedFS | `tests3` | a bucket exists after create | one create request | a probe, a second bucket |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| A profile selects its shared section | `TestProfileSelectsTheSharedSection` |
| An explicit profile beats `AWS_PROFILE` | `TestExplicitProfileBeatsAWSProfile` |
| The shared region flag default is empty | `TestRegionDefaultIsEmpty` |
| `AWS_DEFAULT_REGION` supplies a region when `AWS_REGION` and the flag are empty | `TestRegionReadsAWSDefaultRegion` |
| The region flag beats both environment values | `TestRegionFlagBeatsEnvironment` |
| A profile region wins when no flag or environment value names a region | `TestProfileRegionWins` |
| No flag, environment, or profile region falls back to `us-east-1` | `TestNoRegionFallsBackToUSEast1` |
| An explicitly empty `--region` is legal and falls back to `us-east-1` | `TestEmptyRegionFlagIsLegal` |
| `Resolve` reports the profile, endpoint, region, and path style | `TestResolveReportsEffectiveValues` |
| `Resolve` reports `AWS_ENDPOINT_URL_S3` | `TestResolveReadsServiceEndpoint` |
| `Resolve` retrieves no credential | `TestResolveDoesNotRetrieveCredentials` |
| `New` stores the resolved values | `TestStoreResolved` |
| `Admin` ignores bucket and prefix | `TestNewAdminIgnoresBucketAndPrefix` |
| `ListBuckets` maps the answer | `TestAdminListBuckets` |
| A default-endpoint create in `eu-north-1` sends the configuration | `TestCreateBucketBody` |
| A `us-east-1` create sends no configuration | `TestCreateBucketBody` |
| A custom-endpoint create sends no configuration | `TestCreateBucketBody` |
| `BucketAlreadyExists` maps to `ErrBucketExists` | `TestCreateBucketErrorMapping` |
| `BucketAlreadyOwnedByYou` maps to `ErrBucketOwned` | `TestCreateBucketErrorMapping` |
| A denial maps to `storage.ErrAccessDenied` | `TestAdminDenied` |
| A transport failure maps to `storage.ErrTransport` | `TestAdminTransport` |
| `Identity` returns the access key id without the secret | `TestAdminIdentity` |
| List and create work against SeaweedFS | `TestAdminIntegration` |
| Administration never creates a bucket | `TestAdminNeverCreates` |
| `--profile` is bound, resolved, documented, and an S3 signal | `TestProfileFlag`, `TestS3SignalsIncludeProfile` |
| The old region sentences are gone and `--profile` is a signal | `rg -n -e 'always explicit' -e 'setting are ignored' architecture/running.md` prints nothing; `rg -n 'us-east-1' architecture/config.md architecture/running.md` shows only the new order and the final default; `rg -n -- '--profile' architecture/config.md architecture/login.md architecture/running.md` hits all three; `rg -n 'region is required' internal/` prints nothing |
| No production AWS SDK import exists outside `internal/s3store` | `rg -l 'aws-sdk-go-v2' --glob '*.go' --glob '!internal/s3store/**' --glob '!internal/tests3/**'` prints nothing; `internal/tests3` is test-only |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| unknown profile | the SDK's profile error, wrapped | `TestResolveUnknownProfile` |
| unreadable shared config | the SDK's configuration error | `TestResolveUnreadableConfig` |
| no credentials for `Identity` | `storage.ErrAccessDenied` | `TestAdminIdentityDenied` |
| `ListBuckets` denied | `storage.ErrAccessDenied` | `TestAdminDenied` |
| `ListBuckets` transport failure | `storage.ErrTransport` | `TestAdminTransport` |
| `BucketAlreadyExists` | `ErrBucketExists` | `TestCreateBucketErrorMapping` |
| `BucketAlreadyOwnedByYou` | `ErrBucketOwned` | `TestCreateBucketErrorMapping` |
| `CreateBucket` denied | `storage.ErrAccessDenied` | `TestCreateBucketErrorMapping` |
| `CreateBucket` transport failure | `storage.ErrTransport` | `TestCreateBucketErrorMapping` |
| an empty bucket name | the SDK or a local refusal, never a create | `TestCreateBucketEmptyName` |
| `New`, `NewAdmin`, or `Resolve` attempts a create | no create request, no bucket validation | `TestAdminNeverCreates` |

## Implementation notes

Worker: phase-5 agent, 2026-10-19, session 4. The worktree already carried
the phase-5 code and its no-network tests at the start of the session; this
session audited them row by row against this phase file, completed the
documentation and the exact acceptance test names, and proved the gate.

### What landed

- `internal/s3store/store.go` carries `Config.Profile` and the one
  `loadAWSConfig` loader that `New`, `Resolve`, and `NewAdmin` share. `New`
  applies `us-east-1` when the loaded configuration names no region, builds
  one client with path style for a configured or resolved base endpoint, and
  stores the `Resolved` values.
- `internal/s3store/resolve.go` adds `Resolved{Profile, Endpoint, Region,
  PathStyle}` and `Resolve`, which reads the SDK's own client options without
  a bucket, a request, or a credential retrieval. Its `Region` is the SDK's
  value as loaded and may be empty.
- `internal/s3store/admin.go` adds `Admin`, `NewAdmin`, `ListBuckets`,
  `CreateBucket`, `Identity`, `Bucket`, `ErrBucketExists`, and
  `ErrBucketOwned`. `NewAdmin` ignores the notebook bucket and prefix;
  `CreateBucket` sends `CreateBucketConfiguration` only on the default AWS
  endpoint outside `us-east-1`; `Identity` returns the access key id with the
  resolved profile, endpoint, and region and never the secret.
- `internal/app/config.go` binds `--profile` (then `AWS_PROFILE`), resolves
  the region as `--region`, `AWS_REGION`, `AWS_DEFAULT_REGION`, and leaves it
  empty for the profile's region and the adapter fallback. `finish` no
  longer refuses an empty S3 region, and `FlagReference` names the new order
  and the profile selector.
- `internal/app/storage.go` adds `--profile` to `s3FlagSignals`, `s3Signals`,
  and the narrow flag-set view, so `serve` and `space` both see it. `internal/app/app.go`
  passes `config.profile` to `s3store.Config` in `realStoreFactory`.
- Tests: `internal/s3store/admin_test.go` covers every acceptance and error
  row without a network; `internal/s3store/integration_test.go` covers
  `ListBuckets`, `CreateBucket`, and `Identity` against SeaweedFS. In
  `internal/app/config_test.go` the region cases gained the named
  `TestRegionDefaultIsEmpty`, `TestRegionReadsAWSDefaultRegion`, and
  `TestRegionFlagBeatsEnvironment`, and `TestEmptyRegionFlagIsLegal` proves
  the empty flag is legal and the identity keeps `us-east-1`.
- Docs: `architecture/s3store.md` describes the shared loader, `Resolve`,
  `Store.Resolved`, `Admin`, the create-configuration rule, and the two
  create sentinels; `architecture/config.md` and `architecture/running.md`
  carry the empty region default, the new order, and `--profile`;
  `architecture/login.md` adds `--profile` to the signal list;
  `architecture/security.md` records that a profile is a selector and
  `Identity` returns no secret; `architecture/hosted-mode.md` records that
  `--profile` is parsed and unused there; and `architecture/decisions.md`
  records decision 52.

### Decisions

Recorded by the phase-5 agent, 2026-10-19, session 4; also decision 52 in
`architecture/decisions.md`.

1. `Resolve` reports the region as the SDK loaded it, including an empty
   value, because the `space` backend needs the effective value to display
   and store. `New` and `NewAdmin` apply `us-east-1` instead, so
   `Store.Resolved().Region` is always concrete and the create body has a
   definite region.
2. `NewAdmin` shares the loader but never validates or reads `Bucket` and
   `Prefix`: listing and creating need no notebook configuration, and only
   `Admin.CreateBucket` creates a bucket.
3. `CreateBucketConfiguration` is sent only on the default AWS endpoint
   outside `us-east-1`; the default region rejects it, and a custom endpoint
   names its own location.
4. `--profile` is an S3 signal even when explicitly empty, like `--region`
   and `--path-style`, so the auto-mode ambiguity refusal and the `space`
   backend see the operator's choice.
5. The old region documentation (`always explicit`, `setting are ignored`,
   the `us-east-1` default row, and `region is required`) was deleted rather
   than kept beside the new order, because every sentence was false after
   the change.

### Commands

| Command | Result |
| --- | --- |
| `go test ./internal/s3store/ ./internal/app/ -count=1` before the audit | pass |
| `go test ./internal/s3store/ ./internal/app/ -count=1` after the audit and test additions | pass |
| `go test ./internal/app/ -run 'TestRegion|TestProfileFlag|TestEmptyRegion' -count=1` | pass |
| `make qa` | pass: gofumpt, vet, staticcheck, `go fix`, the race suite at `-count=3`, npm tests, and coverage 89.1 % against the 70 % floor |
| `rg -n -e 'always explicit' -e 'setting are ignored' -e 'region is required' architecture internal` | prints nothing |
| `rg -n 'us-east-1' architecture/config.md architecture/running.md` | only the new order and the final default |
| `rg -n -- '--profile' architecture/config.md architecture/login.md architecture/running.md` | hits all three |
| `rg -l 'aws-sdk-go-v2' --glob '*.go' --glob '!internal/s3store/**' --glob '!internal/tests3/**' .` | prints nothing |

## Review findings

Self-review by the phase-5 agent, 2026-10-19, session 4.

1. The worktree held a complete-looking phase-5 implementation with no
   worklog status or documentation. Each acceptance and error row was checked
   against the code; the missing pieces were the documentation corrections and
   three acceptance test names, not the production paths.
2. The region documentation still claimed the flag was always explicit and
   that `AWS_DEFAULT_REGION` and a profile `region` were ignored. Those
   sentences were false after the empty default and were deleted from
   `architecture/config.md` and `architecture/running.md`.
3. `architecture/security.md` first said `Admin.Identity` was the only call
   that retrieves a credential. A normal S3 request also retrieves lazily, so
   the sentence now says it is the one explicit retrieval.
4. `architecture/s3store.md` listed `Identity` twice in the key-files table;
   the duplicate was removed.
5. `TestRegionResolution` covered the empty default and `AWS_DEFAULT_REGION`
   only as table rows. The acceptance criteria name three tests, so the cases
   were split into named top-level tests and the remaining order stayed in the
   table.

### Review 2 (2026-10-20, session 11)

Round 2 re-ran `make qa` (exit 0, coverage 89.4 %, 35 npm tests) and rechecked
the phase's surface. The profile precedence, the shared loader, the resolved
values, and the create request shape hold (`TestProfileRegionWins`,
`TestResolveReportsEffectiveValues`, `TestCreateBucketBody`,
`TestAdminNeverCreates` pass). One note is open:

- **R2-08 (note).** `Resolved.Profile` (`internal/s3store/resolve.go`) and the
  `Identity` comment (`internal/s3store/admin.go`) say the profile that
  selected the credentials, but the value is `Config.Profile`; an empty
  value with `AWS_PROFILE` set reports `""`. The app resolves `AWS_PROFILE`
  before calling, so no user-visible defect. Fix the comments, or resolve the
  effective profile. No test is required while the value stays `Config.Profile`.

The phase's `--profile` and empty-region changes need no behavioral rework;
the note changes prose only.

### Remediation (2026-10-20, session 15)

Closed R2-08 by changing the `Resolved.Profile` and `Identity.Profile` comments.
They now describe the shared-config selector named by the configuration,
empty when the ambient chain applies, and state that `internal/app` fills the
value from `--profile` or `AWS_PROFILE`. No test changed; `TestResolveReportsEffectiveValues`
already pins the configured selector. The wording in `architecture/s3store.md`
and the README's S3 identity surface row remains accurate.

| Command | Result |
| --- | --- |
| `go test ./internal/s3store -count=1` | pass |
| `make qa` | pass; coverage 89.3% against the 70% floor, all 35 npm tests passed |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | three clone groups; all acceptable under the duplication policy |

The conflict-case table is repeated at the black-box integration and notebook
boundaries to prove separate contracts. Flag registrations are command-specific,
and the merge test rows are distinct conflict shapes. No actionable duplication
was found. Phase 5 remains Complete.

### V6-02 (note, evidence ownership)

The `Files` table omits `internal/s3store/admin_test.go`. That file exists and
contains the profile, `Resolve`, `Admin`, and create tests named by this phase.
Add the file to the table.

### Resolution (2026-10-20, session 20)

V6-02 is resolved. `Files` now includes `internal/s3store/admin_test.go`, which
contains the named profile, `Resolve`, `Admin`, and create tests. Phase 5
remains Complete.
