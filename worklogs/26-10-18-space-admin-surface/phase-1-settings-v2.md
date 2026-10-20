# Phase 1: settings version 2

**Status:** Complete (2026-10-19, session 1)

README: [README.md](./README.md)

## Goal

Store a backend-aware `Target` and a store-keyed defaults list in
`workspaces.json`, read every version-1 file without rewriting it, and write
version 2 from the next successful save.

## Specification

### Types

`internal/settings` gains the shared types the README fixes:

```go
type Kind string

const (
    KindHosted Kind = "hosted"
    KindS3     Kind = "s3"
)

type Target struct {
    Kind      Kind
    Endpoint  string
    Region    string
    Profile   string
    Name      string
    Prefix    string
    PathStyle bool
}

type StoreKey struct {
    Kind     Kind
    Endpoint string
    Region   string
    Profile  string
}

type Entry struct {
    Path   string
    Target Target
}

func (t Target) Key() StoreKey
```

`KindNamed` is not introduced: a value outside the two constants is malformed,
so a caller never needs a third name.

### Set

`Set` keeps entries in file order and defaults in file order. Its methods are:

| Method | Contract |
| --- | --- |
| `Entries() []Entry` | a copy in file order |
| `Lookup(path) (Target, bool)` | exact, byte-bound path |
| `Put(Entry) Set` | replace the same path where it stands, else append |
| `Defaults() []Target` | a copy in file order |
| `Default(StoreKey) (Target, bool)` | exact store-key match |
| `PutDefault(Target) Set` | replace the same `Target.Key()` where it stands, else append |

The zero `Set` is an empty v2 file: no entries, no defaults. `Put` and
`PutDefault` stay pure; the read-modify-write belongs to the caller under the
settings lock.

### Encoding

`FormatVersion = 2`. The encoder always writes every field in this order:

```text
version, entries, defaults
entries[i]: path, target
target: kind, endpoint, region, profile, name, prefix, pathStyle
defaults[i]: target
```

`defaults` is present as an empty array when none is stored. The encoding is
compact JSON, HTML escaping off, no trailing newline, like the workspace
private-state record.

### Decoding

The decoder parses the root strictly and dispatches on `version`:

| Version | Path | Result |
| --- | --- | --- |
| `1` | `decodeV1` | `entries[].target{space,prefix}` reads as `Target{Kind:KindHosted, Endpoint:"", Name:space, Prefix:prefix}`; `defaults` is empty |
| `2` | `decodeV2` | the full shape above |
| anything else | `versionError` | `ErrUnsupportedVersion`, read no further, never rewritten |

Reading a version-1 file sets no dirty flag and writes nothing. The next
`File.Save` always writes version 2.

Version-2 strictness is unchanged from version 1: an unknown, duplicate,
missing, or null member is refused at every level; a value of the wrong JSON
type is refused. Additional rules:

| Rule | Failure |
| --- | --- |
| `defaults` missing or not an array | malformed |
| a default target is `hosted` | malformed, because hosted defaults live in `credentials.json` |
| a hosted target has non-empty `region`, `profile`, or `pathStyle` | malformed |
| `kind` is neither `hosted` nor `s3` | malformed |
| `name` is empty | malformed |
| `prefix` fails `storage.ValidatePrefix` | malformed |
| two entries share a path | malformed |
| two defaults share a store key | malformed |
| a version-1 entry has an empty `space` | malformed |

A version-2 target with an empty `endpoint` means the provider default. A
version-1 entry keeps an empty endpoint because the selected login supplies it
at resolution time. The next successful write stores a version-2 hosted target
with `kind`, an empty `endpoint`, the same `name`, and the same `prefix`, so it
reads back exactly as before; storing the effective endpoint belongs to the
sibling worklog `26-10-18-directory-bindings`.

### File handling

`FileName`, `LockName`, the 1 MiB bound, `0600` file mode, `0700` directory
mode, the hardened `credentials.PrivateFile` path, and `workspaces.lock` are
unchanged. `ErrNoConfigDir`, `ErrMalformed`, `ErrUnsupportedVersion`,
`ErrExposed`, and `ErrTooLarge` keep their meanings. `versionError.withFix`
now names version 2.

### Compile-site updates

This phase renames `Target.Space` to `Target.Name` and updates every caller
mechanically. No resolution behavior changes.

| File | Change |
| --- | --- |
| `internal/app/association.go` | `rememberSpace` builds the version-2 hosted target; reads use `Name` |
| `internal/app/storage.go` | `storageSelection.remembered` stays a `settings.Target`; hosted selection reads `Name` |
| `internal/app/config.go` | `applyRememberedPrefix` and `finish` read `Name` |
| `internal/app/association_test.go` | the record fixtures build a hosted target |
| `internal/app/remember_test.go` | the `remember` helper builds a hosted target |
| `internal/integrationtest/scenario_remembered_test.go` | the planted record uses the v2 target |
| `internal/integrationtest/scenario_recording_test.go` | the recorded target assertions use `Name` |
| `internal/settings/settings_test.go` | every fixture and assertion uses the v2 shape |

Hosted behavior is unchanged in this phase: a v1 file reads as a hosted target,
and a hosted v2 target behaves as the v1 one did.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| No credential can be stored | `Target` has no secret field; strict unknown-field refusal | `TestDecodeRejectsCredentialField` |
| Reading never rewrites | `File.Load` is read-only; no save on decode | `TestV1ReadDoesNotRewrite` |
| The next write is version 2 | `encode` always writes `FormatVersion` | `TestV1WriteBecomesV2` |
| `defaults` is always present | encoder writes it; decoder requires it | `TestDefaultsAlwaysEncoded`, `TestDecodeRequiresDefaults` |
| A default is found by its exact store key | `Target.Key()`; `Set.Default` | `TestDefaultByStoreKey` |
| The file stays private and bounded | `credentials.PrivateFile` | `TestSaveModes`, `TestBoundRefusesOverSizeFile`, `TestLoadRefusesExposedFileAndDir` |

### Documentation

| Doc | What must become true |
| --- | --- |
| `architecture/config.md` | `workspaces.json` version 2 is documented: the target fields, the store-keyed defaults list, the version-1 read path, and the rule that a hosted target keeps an empty endpoint in this build. The scoped-settings paragraph's `strictjson`, `version 1` wording becomes a version-1 read path over the version-2 target, and its sentence `the schema has a space and a prefix only` is replaced by the version-2 target fields. |
| `architecture/hosted-mode.md` | The hosted record is documented as a version-2 target with an empty endpoint whose login supplies the site, and the effective site is named as the sibling worklog's write. |

### Files

| File | Change |
| --- | --- |
| `internal/settings/settings.go` | Rename `Target.Space` to `Target.Name` and add `Kind`, `Endpoint`, `Region`, `Profile`, `PathStyle`, `StoreKey`, `Set.Defaults`, `Set.Default`, and `Set.PutDefault`. |
| `internal/settings/codec.go` | Encode and decode workspaces format version 2, with `defaults` required and strict `kind` and field rules. |
| `internal/settings/settings_test.go` | Add table tests for version-1 read, version-2 round-trip, strict decoding, and store-key default lookup. |
| `internal/app/association.go` | Update remembered target construction and reads to use `Target.Name` and `Target.Kind`. |
| `internal/app/storage.go` | Update `storageSelection.remembered` to use the version-2 `settings.Target` shape. |
| `internal/app/config.go` | Update `applyRememberedPrefix` and `finish` to read `Target.Name` and preserve the hosted empty endpoint meaning. |
| `internal/app/association_test.go` | Update record fixtures to build version-2 hosted targets and to assert `Name` instead of `Space`. |
| `internal/app/remember_test.go` | Update the remember helper to build a version-2 hosted target and assert `Set.Put` effects. |
| `internal/integrationtest/scenario_remembered_test.go` | Update planted record JSON to the version-2 target shape for hosted spaces. |
| `internal/integrationtest/scenario_recording_test.go` | Update assertions to read `Target.Name` and the version-2 target fields. |

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| load a written v1 file | `strictjson` | a hosted entry with an empty endpoint | none | rewriting the file |
| save after loading v1 | `flock`, temp file, rename | the file is version 2 with `defaults: []` | entries preserved | losing an entry |
| save a v2 entry and a default | `flock` | both survive a reload | defaults in file order | reordering entries |
| load a hosted target in `defaults` | `strictjson` | `ErrMalformed` | none | rewriting |
| load a v2 target missing `defaults` | `strictjson` | `ErrMalformed` | none | rewriting |
| two writers save different paths | `flock` | both entries present | none | a lost entry |
| pull a hosted notebook after the change | `sitetest`, `gatewaytest` | the same space as before | the record loads | a v1 rewrite on read |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| A v1 file reads as a hosted target with an empty endpoint and its prefix | `TestV1ReadsAsHostedTarget` |
| Reading a v1 file leaves it byte-identical | `TestV1ReadDoesNotRewrite` |
| The next save after a v1 read writes version 2 | `TestV1WriteBecomesV2` |
| A v2 file round-trips entries, targets, and defaults | `TestV2RoundTrip` |
| `defaults` is present as `[]` when empty | `TestDefaultsAlwaysEncoded` |
| A default is found by `{kind,endpoint,region,profile}` and not by another key | `TestDefaultByStoreKey` |
| `PutDefault` replaces one key in place and appends another | `TestPutDefaultReplacesAndAppends` |
| Duplicate entry paths are refused | `TestDecodeRejectsDuplicatePaths` |
| Duplicate default store keys are refused | `TestDecodeRejectsDuplicateDefaultKeys` |
| A hosted target in `defaults` is refused | `TestDecodeRejectsHostedDefault` |
| A hosted target with region, profile, or path style is refused | `TestDecodeRejectsHostedS3Fields` |
| An unknown kind is refused | `TestDecodeRejectsUnknownKind` |
| Unknown, duplicate, missing, and null members are refused at every level | `TestDecodeRejectsStrictViolations` |
| A prefix outside `storage.ValidatePrefix` is refused | `TestDecodeRejectsInvalidPrefix` |
| A v1 file rewrites no hosted resolution | `TestScenarioRememberedSpaceReachesTheHostedStore` (unchanged) |
| The old `workspaces.json` schema sentence is gone | `rg -n 'a space and a prefix only' architecture/config.md` prints nothing |
| File mode, directory mode, and bound are unchanged | `TestSaveModes`, `TestBoundRefusesOverSizeFile` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| version 3 | `ErrUnsupportedVersion`, file untouched | `TestUnsupportedVersionIsRefusedUnwritten` |
| malformed JSON | `ErrMalformed` naming the file | `TestDecodeRejectsMalformedJSON` |
| missing `defaults` in v2 | `ErrMalformed` | `TestDecodeRequiresDefaults` |
| hosted default | `ErrMalformed` | `TestDecodeRejectsHostedDefault` |
| hosted target with an S3 field | `ErrMalformed` | `TestDecodeRejectsHostedS3Fields` |
| unknown kind | `ErrMalformed` | `TestDecodeRejectsUnknownKind` |
| invalid prefix | `ErrMalformed` | `TestDecodeRejectsInvalidPrefix` |
| duplicate entry path | `ErrMalformed` | `TestDecodeRejectsDuplicatePaths` |
| duplicate default key | `ErrMalformed` | `TestDecodeRejectsDuplicateDefaultKeys` |
| exposed file or directory | `ErrExposed` | `TestLoadRefusesExposedFileAndDir` |
| oversized file | `ErrTooLarge` | `TestBoundRefusesOverSizeFile` |
| lock held by another writer | the lock error | `TestSaveRefusesWhileLocked` |

## Implementation notes

Worker: phase-1 agent, 2026-10-19, session 1.

### What shipped

`internal/settings` now owns `Kind`, `Target`, `StoreKey`, `Target.Key`,
`Set.Defaults`, `Set.Default`, and `Set.PutDefault`; the file format is version
2 with a required `defaults` array, and version 1 is read through its own
`decodeV1` path without a rewrite. The encoder always writes every target field
in the normative order, and the decoder refuses an unknown kind, a hosted
target that carries `region`, `profile` or `pathStyle`, a hosted default, a
prefix outside `storage.ValidatePrefix`, duplicate entry paths, and duplicate
default store keys.

The app call sites moved from `Target.Space` to `Target.Name`; the recorder
writes `{Kind: hosted, Name, Prefix}`. `rememberedTarget` keeps only a hosted
record for this build, so a version-2 S3 record resolves exactly as no record
did; how the commands resolve an S3 record stays with the sibling worklog
`26-10-18-directory-bindings`.

### Decisions

1. `internal/settings` imports `internal/storage` for `ValidatePrefix` instead
   of duplicating the grammar. There is no import cycle, and the prefix rule
   then has one implementation for files and flags alike.
2. The version-1 path validates the prefix with the same grammar. A version-1
   record with an invalid prefix was refused later by `config.finish` anyway,
   so decoding it as malformed is not a new outcome, only an earlier one.
3. Every target field is required in version 2, including an empty `endpoint`,
   `region`, `profile` and a `pathStyle`, because strict decoding refuses a
   missing member. The encoder writes all of them for both kinds, so a hosted
   target and an S3 target have one shape.
4. The credential-freedom assertions now test values, not field names: a
   version-2 hosted target legitimately contains the member names `endpoint`,
   `region` and `profile`, so those words must not be forbidden. They forbid
   `sld_`, the account keys, `@` (an account) and `http` (a site endpoint).
5. `architecture/decisions.md` decision 49, `architecture/overview.md`,
   `architecture/running.md` and `architecture/testing.md` also stated the old
   "space and prefix only" record; they are updated in this phase because the
   code wins. Phase 5 still owns the region-precedence sentences.

### Commands and results

Before the change:

```bash
go test ./internal/settings/... ./internal/app/... ./internal/integrationtest/...
```

passed all three packages.

After the change:

```bash
go build ./...
go vet ./...
go run mvdan.cc/gofumpt@v0.11.0 -w -l internal/settings internal/app internal/integrationtest
go fix -diff ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./internal/settings/... ./internal/app/... ./internal/integrationtest/...
go test -count=1 ./internal/settings/... ./internal/app/... ./internal/integrationtest/...
```

All are clean, and the three test packages pass.

```bash
make test
```

runs the race suite with `-count=3` and the coverage floor; the result is
recorded below.

`make test`: see the session journal entry in
[README.md](./README.md#2026-10-19-phase-1-implemented).

## Review findings

### V6-05 (note, evidence ownership)

The Acceptance criteria and Error coverage tables use `existing settings rows`
instead of naming the tests for the unchanged file and directory modes, size
bound, exposed paths, and lock failures. The tests exist in
`internal/settings/settings_test.go`: `TestSaveModes`,
`TestBoundRefusesOverSizeFile`, `TestLoadRefusesExposedFileAndDir`, and
`TestSaveRefusesWhileLocked`. Name them in the relevant rows.

### Resolution (2026-10-20, session 20)

V6-05 is resolved. The Acceptance criteria and Error coverage rows now name the
existing tests for file and directory modes, the size bound, exposed paths,
and lock failures. Phase 1 remains Complete.
