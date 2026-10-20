# Phase 2: backend-aware bindings

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Resolve and record both backends through the same layers, complete the S3
store key before a default is read, and make the profile part of the notebook
identity and the compatibility proof.

## Specification

### Configuration fields

`config` gains:

| Field | Meaning |
| --- | --- |
| `kind settings.Kind` | `KindHosted` or `KindS3` |
| `profile string` | the effective AWS profile; empty is the ambient chain. The flag and `config.profile` ship with `26-10-18-space-admin-surface`; this phase makes the value a layered one |
| `pathStyleFrom settingKind` | whether a flag, the environment, or the record named the addressing mode |
| `bucketFromDefault bool` | the name came from `settings.defaults` |

`config.hosted()` becomes `cfg.kind == settings.KindHosted`. `token != ""` and
`login != nil` still describe the credential, not the kind.

`bucketSource` gains `bucketFromDefault`, whose `String()` is `default target`.
`ServiceConfig` gains `Profile`.

### Reading settings

`Flags.resolve` resolves the roots first, then passes the directory key to
`resolveStorage`. `settingsAssociation` gains:

```go
func (a settingsAssociation) set() (settings.Set, error)
func (a settingsAssociation) read(workspaceRoot string) (settings.Target, bool, error)
```

`set` returns the process's cached set, or an empty set when the process has no
association. `read` looks the key up in that set. `unusableSettings` still
refuses any command that consults an unreadable file.

`storageInputs` replaces its single `remembered` field with two lookups:
`remembered func() (settings.Target, bool, error)` and
`defaultTarget func(settings.StoreKey) (settings.Target, bool, error)`. Both
read the same cached set, so the S3 default lookup does not read the file a
second time.

Settings are read before the kind branch, with one exception: when
`SLIVINGDOC_TOKEN` is set, the token names its own space and the settings file
is not read at all. `--storage s3` now reads it and refuses a malformed file.

### Kind

| Order | Rule |
| --- | --- |
| 1 | `--storage hosted` → hosted |
| 2 | `--storage s3` → S3; the record does not apply |
| 3 | `auto` and `SLIVINGDOC_TOKEN` set → hosted |
| 4 | `auto` and the record's kind is set → that kind |
| 5 | `auto`, a usable stored login, and no S3 signal → hosted |
| 6 | otherwise → S3 |

A record’s kind is an S3 signal in `auto`. `--storage` naming another kind makes
the record not apply, so the run must name its own target. The existing
`--space`/`SLIVINGDOC_SPACE` + S3-signal ambiguity refusal is unchanged.

### Name, profile, endpoint, region, path style, prefix

| Field | Order |
| --- | --- |
| `name` | `--bucket`/`--space`, their variables, the record (kind matches), the default, then a refusal naming `space` and `--bucket` |
| `profile` | `--profile`, the record, `AWS_PROFILE`, empty |
| `endpoint` | `--endpoint`/`AWS_ENDPOINT_URL_S3`, the record, then empty (the SDK resolves) |
| `region` | `--region`, the record, `AWS_REGION`, `AWS_DEFAULT_REGION`, then empty |
| `pathStyle` | `--path-style`/`SLIVINGDOC_PATH_STYLE`, the record, then the effective endpoint or a dotted name on the default endpoint |
| `prefix` | `--prefix`/`SLIVINGDOC_PREFIX`, the record, the default, the built-in prefix |

Hosted keeps its endpoint order (`--endpoint`, `SLIVINGDOC_ENDPOINT`, the
login’s endpoint, `DefaultHostedEndpoint`) and reads no AWS region or profile.
A hosted record with an empty endpoint means the selected login’s endpoint.

`finish` no longer requires a non-empty S3 region. It keeps every other
validation and the `prefixMismatch` rule, which becomes kind-neutral:

```text
this directory remembers target "x" with prefix "y", and "z" names another notebook in it; pass --prefix y, or use another directory
```

### S3 default lookup

When the kind is S3 and no flag, variable, or record names the bucket:

1. call `s3store.Resolve(ctx, s3store.Config{Profile, Endpoint, Region}, s3store.Options{ForcePathStyle})`;
2. fill the effective endpoint, region, and path style;
3. normalize the endpoint;
4. call `settings.Default(Target{Kind:KindS3, Endpoint, Region, Profile}.Key())`;
5. use the default’s `Name`, `Prefix`, and `PathStyle`, and set
   `bucketFromDefault`.

The store is built only after the name is known. The default’s endpoint,
region, and profile are the completed key, so a default stored for another
profile or endpoint is not found. The resolution call uses
`context.Background()`: it reads local AWS configuration and sends no service
request. Tests that exercise the SDK resolution plant `AWS_CONFIG_FILE`,
`AWS_SHARED_CREDENTIALS_FILE`, or `AWS_ENDPOINT_URL_S3` with `t.Setenv` and do
not run in parallel.

### After the store is built

`realStoreFactory` passes `Profile` and
`ForcePathStyle: cfg.pathStyle || strings.Contains(cfg.bucket, ".")`. The
profile is already passed by the sibling's phase; this phase adds path style.

`buildService`, immediately after `storeFactory` and before
`checkStoreWithProof` and `NewService`:

```go
if cfg.kind == settings.KindS3 {
    if resolved, ok := store.(interface{ Resolved() s3store.Resolved }); ok {
        cfg = cfg.withResolved(resolved.Resolved())
    }
}
```

`withResolved` sets the normalized endpoint, the resolved region (or
`us-east-1` when still empty), the profile, and
`pathStyle = cfg.pathStyle || resolved.PathStyle`. An injected store that does
not implement `Resolved` keeps the values resolved so far, and `us-east-1`
fills an empty region so the harness keeps working.

### Recording a target

`recordsSpace` applies to both kinds:

- the process carries an association, and the name is non-empty;
- the target did not come from `SLIVINGDOC_TOKEN`;
- the target did not come from a default;
- an explicit flag or variable records only where the directory remembers
  nothing;
- a record-sourced target rewrites the same entry, and `storeEntry` leaves it
  byte for byte;
- `serve` carries no recorder and never writes.

`rememberSpace` writes the full target:

```go
settings.Target{
    Kind: cfg.kind, Endpoint: cfg.endpoint, Region: cfg.region,
    Profile: cfg.profile, Name: cfg.bucket, Prefix: cfg.prefix,
    PathStyle: cfg.pathStyle,
}
```

A failed operation records nothing. A write failure still warns and changes no
result or exit code.

### Probe proof version 2

`probeProofVersion = 2`. `probeProof` and `probedStore` gain a required
`profile` field, and `check` compares it with the store’s profile. A version-1
record is “no recorded proof”, so the process probes once. The JSON field order
is `version`, `slivingdoc`, `probedAt`, `endpoint`, `region`, `bucket`,
`prefix`, `profile`, `pathStyle`.

### Notebook identity

`workspace.Identity` gains `Profile`. `writeIdentity` appends it after
`SpaceID` only when it is non-empty, exactly like `SpaceID`, so every existing
S3 workspace keeps its private-directory key and shared cache directory.
`Service.identity` and the integration harness’s `Identity` set it.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| The record wins over ambient AWS | layer order | `TestRecordBeatsAmbientAWS` |
| An explicit `--profile` wins for the run | layer order | `TestExplicitProfileWinsOverRecord` |
| `--storage` naming another kind drops the record | kind branch | `TestStorageFlagDropsTheRecord` |
| A record’s kind is an auto signal | kind branch | `TestRecordKindSelectsTheBackend` |
| The store key is complete before a default is read | `s3store.Resolve` first | `TestDefaultLookupUsesResolvedKey` |
| A default-sourced target is never recorded | `bucketFromDefault` | `TestDefaultIsNotRecorded` |
| A record is written only after success | `rememberSpace` call site | `TestFailedOperationRecordsNothing` |
| An explicit choice records only where nothing is remembered | `recordsSpace` | `TestExplicitChoiceDoesNotOverwriteARecord` |
| `serve` never writes | no recorder | `TestServeRecordsNothing` |
| A profile changes the identity; an empty profile changes no key | conditional append | `TestProfileIdentity` |
| An old proof costs one probe | version 2 | `TestOldProofIsIgnored` |
| No credential enters the record | `Target` schema | `TestRecordHasNoCredential` |

### Human required

None.

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| pull an S3 directory with a flag target | fake store, S3 record reader | success, record written | one `workspaces.json` entry | a second write |
| pull the same directory with no flags | planted S3 record | the recorded bucket | none | a default lookup, a rewrite |
| pull with a default-only S3 target | planted default, fake resolver | the default bucket, no record | none | a record write |
| pull with a broken settings file | planted bytes | `unusableSettings` refusal | none | a fallback to S3 |
| `--storage s3` with a broken settings file | planted bytes | the same refusal | none | a silent S3 run |
| `serve --workspace-root <dir>` with an S3 record | planted record, fake admin/store | the recorded bucket | none | a settings write |
| a profile-bound directory | planted record with profile | private state under the profile identity | a different derived key | sharing the ambient key |
| an old probe proof | planted `probe-ok.json` version 1 | one probe | a version-2 proof | reuse of the old record |
| a failed pull | failing store | no record | none | a record write |
| a flag target beside an existing record | planted record, flag | the flag target for this run | none | a record rewrite |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| A record’s kind selects hosted or S3 in auto | `TestRecordKindSelectsTheBackend` |
| `--storage s3` reads settings and refuses a malformed file | `TestS3ModeReadsSettings` |
| The record beats ambient AWS settings | `TestRecordBeatsAmbientAWS` |
| An explicit `--profile` beats the record | `TestExplicitProfileWinsOverRecord` |
| `--storage` naming another kind drops the record | `TestStorageFlagDropsTheRecord` |
| The profile order is flag, record, `AWS_PROFILE`, empty | `TestProfileOrder` |
| The region order leaves an empty region before `Resolve` | `TestRegionOrder` |
| The default is looked up by the completed store key | `TestDefaultLookupUsesResolvedKey` |
| A default-sourced target is not recorded | `TestDefaultIsNotRecorded` |
| An explicit flag records only where nothing is remembered | `TestExplicitChoiceDoesNotOverwriteARecord` |
| A successful S3 pull writes a record; a failure writes none | `TestS3RecordAfterSuccess`, `TestFailedOperationRecordsNothing` |
| `serve` reads and never writes | `TestServeRecordsNothing` |
| The probe proof requires a profile and ignores version 1 | `TestProbeProofProfile`, `TestOldProofIsIgnored` |
| A profile changes the notebook identity; an empty profile does not | `TestProfileIdentity` |
| A hosted v1 record with an empty endpoint uses the login’s endpoint | `TestHostedV1RecordApplies` |
| `prefixMismatch` is kind-neutral | `TestPrefixMismatch` |
| A host with a hosted login and no S3 signal loads no AWS config | `TestHostedOnlyLoadsNoAWS` |
| A real S3 record reaches its bucket through the CLI and `serve` | `TestScenarioS3RecordReachesItsBucket`, `TestScenarioServeUsesTheS3Record` |
| A default-only S3 target is used and never recorded | `TestScenarioDefaultSpaceUsed` |
| A flag target beside an existing record leaves the record byte for byte | `TestScenarioRecordIsNeverRewritten` |
| The resolved profile is part of the shared cache directory | `TestSharedCacheIncludesProfile` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| unreadable settings file | `unusableSettings`, no fallback | `TestUnusableSettingsRefusesS3Mode` |
| hosted record with no credential | `noCredentialRefusal`, never S3 | `TestHostedRecordWithoutCredential` |
| a named prefix contradicting the record | `prefixMismatch` | `TestPrefixMismatch` |
| a named prefix contradicting a default | `prefixMismatch` | `TestDefaultPrefixMismatch` |
| no name after record and default | the existing `space`/`--bucket` refusal | `TestNameRequiredAfterDefault` |
| `s3store.Resolve` fails | the SDK’s configuration error, wrapped | `TestDefaultLookupResolveFailure` |
| no resolved region | `us-east-1` after `Resolved` | `TestResolvedRegionFallback` |
| a `Resolved` endpoint that does not normalize | the configuration refusal | `TestResolvedEndpointNormalizes` |
| a record write fails | one warning, the operation still succeeds | `TestRecordWriteFailureWarns` |
| a failed operation | no record | `TestFailedOperationRecordsNothing` |
| a version-1 proof | ignored, one probe | `TestOldProofIsIgnored` |
| a proof with another profile | not reused | `TestProbeProofProfile` |

## Implementation notes

Not started.

## Review findings

None.
