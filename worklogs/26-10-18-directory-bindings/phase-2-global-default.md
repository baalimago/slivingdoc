# Phase 2 — Global default

**Parent:** root · **Class:** contract · [README](README.md)

## Goal

Give `space` one global default, selected by the last hosted or S3 choice, so a bare command needs no repeated profile, bucket, or endpoint.

This phase replaces the store-key default clause of
`26-10-18-space-admin-surface:space` (its Phase 6) and the per-key default rule
of its version-2 settings contract (its Phase 1). An existing version-2 file
stays readable: its `defaults` are read in file order and the last one is
active. No member outside `space` selects a stored default by store key after
this phase.

## Interface

Written to `internal/app` and exposed by the `space` command in `cmd/login`.

```text
slivingdoc space [<target flags>] [<name> | new <name>]

target flags: --storage auto|hosted|s3, --bucket|--space <name>, --endpoint <url>,
              --region <region>, --profile <name>, --prefix <prefix>, --path-style
```

1. A hosted selection, `space <name>`, sets the selected login's default space and clears every stored S3 default. The hosted default becomes active. (I5)
2. An S3 selection, `space --storage s3 ... <name>`, stores one S3 default target with kind `s3`, the effective endpoint, region, profile, name, prefix, and path style. It replaces every previous S3 default. (I5)
3. The active global default is the S3 default when one exists, else the selected login's default space. (I5)
4. `space` lists and marks only the active default. A hosted default that is inactive because an S3 default exists is shown without the default mark.
5. `space` uses the active S3 default's profile, endpoint, and region to list the S3 group when no S3 flag or variable names an identity.
6. `space new` and `space new <name>` change no default.
7. A failed write leaves the previously active default active.
8. A version-2 settings file with several S3 defaults reads the last one as active and no other stored default is active; the next successful default write collapses them to one.
9. A stored default carries no credential, key, token, or private path, and no `space` output displays one. (I2)

The S3 default lives in `workspaces.json`; the hosted default continues to live in `credentials.json`. Selecting hosted clears the S3 default, so the two never compete after a selection.

Consumes:

- `settings:Target`, `settings:StoreKey`, `settings:Set`, `settings:Set.Defaults (import)`: the stored defaults
- `settings:Set.Entries`, `settings:Set.Put`, `settings:Set.PutDefault (import)`: the rebuild that keeps every directory entry and leaves one S3 default, or none
- `settings:File.Load`, `settings:File.Save`, `settings:File.Lock (import)`: the settings read-modify-write
- `credentials:Set`, `credentials:Login`, `credentials:File (import)`: the stored login
- `credentials:Set.DefaultSpace`, `credentials:Set.SetDefaultSpace`, `credentials:Set.ClearDefaultSpace (import)`: the selected login's default space
- `s3store:Resolved`, `s3store:DefaultRegion (import)`: the effective S3 identity
- `storage:ValidatePrefix (import)`: the stored prefix

## Acceptance criteria

| #   | Criterion                                                                 | Proves    | Proof                                                                           |
| --- | ------------------------------------------------------------------------- | --------- | ------------------------------------------------------------------------------- |
| 1   | A hosted selection leaves no S3 default behind.                           | `space.1` | `go test ./internal/integrationtest -run '^TestScenarioSpaceHosted$'`           |
| 2   | An S3 selection stores the complete target and replaces the previous one. | `space.2` | `go test ./internal/integrationtest -run '^TestScenarioSpaceS3$'`               |
| 3   | Exactly one default is active, the S3 one when it exists.                 | `space.3` | `go test ./internal/integrationtest -run '^TestScenarioSpaceS3$'`               |
| 4   | Only the active default carries the mark.                                 | `space.4` | `go test ./internal/integrationtest -run '^TestScenarioSpaceActiveMark$'`       |
| 5   | A version-2 file with several S3 defaults keeps working.                  | `space.8` | `go test ./internal/integrationtest -run '^TestScenarioSpaceLegacyDefaults$'`   |
| 6   | `space new` changes no default.                                           | `space.6` | `go test ./internal/integrationtest -run '^TestScenarioSpaceNewKeepsDefault$'`  |
| 7   | A failed write keeps the previous default active.                         | `space.7` | `go test ./internal/integrationtest -run '^TestScenarioSpaceSettingsFailures$'` |
| 8   | No stored default and no output carries a secret.                         | `space.9` | `go test ./internal/integrationtest -run '^TestScenarioSpaceRedaction$'`        |

## Test cases

| Test                                | Level                | Proves                                    | Given                                                                      | Then                                                                                                                      | Must not                      |
| ----------------------------------- | -------------------- | ----------------------------------------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| `TestScenarioSpaceHosted`           | integration scenario | `space.1`, `space.3`, criterion 1         | a selected login whose default space changes beside an existing S3 default | the hosted default is active and no S3 default remains                                                                    | leave an S3 default beside it |
| `TestScenarioSpaceS3`               | integration scenario | `space.2`, `space.3`, `I5`, criteria 2, 3 | an S3 selection naming a bucket, an endpoint, and a region                 | one stored S3 default carries kind, endpoint, region, profile, name, prefix, and path style, and replaces the earlier one | leave two S3 defaults         |
| `TestScenarioSpaceActiveMark`       | integration scenario | `space.4`, `space.5`, criterion 4         | an inactive hosted default beside an active S3 default                     | only the S3 default carries the mark, and the S3 group is listed with the default's own profile, endpoint, and region     | read the ambient AWS identity |
| `TestScenarioSpaceNewKeepsDefault`  | integration scenario | `space.6`, criterion 6                    | `space new` and `space new <name>` beside an active default                | the active default is unchanged                                                                                           | write the settings file       |
| `TestScenarioSpaceSettingsFailures` | integration scenario | `space.7`, criterion 7                    | an unreadable settings file, and a write that fails                        | the command exits nonzero and the previous default stays active                                                           | leave a partial default       |
| `TestScenarioSpaceLegacyDefaults`   | integration scenario | `space.8`, criterion 5                    | a version-2 file holding several S3 defaults                               | the last one is active, and the next successful default write collapses them to one                                       | activate an earlier default   |
| `TestScenarioSpaceRedaction`        | integration scenario | `space.9`, `I2`, criterion 8              | stored S3 and hosted defaults with logging captured                        | the file and every output carry no credential, key, token, or private path                                                | display a secret              |

## Error management

| Failure                                                   | Expected outcome                                                            | Test                                |
| --------------------------------------------------------- | --------------------------------------------------------------------------- | ----------------------------------- |
| The settings file cannot be read                          | the existing unusable-settings refusal; nonzero exit; the file is untouched | `TestScenarioSpaceSettingsFailures` |
| The settings write fails                                  | nonzero exit with the structured report; the previous default stays active  | `TestScenarioSpaceSettingsFailures` |
| No flag, variable, or stored default names an S3 identity | the existing S3 identity refusal; no default is written                     | `TestScenarioSpaceS3`               |

## Golden diff

None.

## Removes

None.

## Documentation

Update every architecture doc that states the store-key default, in this
phase's commit: `architecture/login.md` (the S3 default, `space <name>`, and
the hosted default it clears), `architecture/config.md` (the scoped settings
store, the `defaults` list, and the version-2 decode rule),
`architecture/cli.md` (the `space` behaviour), `architecture/running.md` (the
default paragraph), `architecture/overview.md` (the `settings` package map),
`architecture/testing.md`, and `architecture/decisions.md` (append a decision;
decision 54's store-key default is superseded, not rewritten).

## Local facts

### Invariants

None.

### Parameters

| Parameter | Default | Decided by |
| --------- | ------- | ---------- |
| Active global default | the S3 default when one exists, else the selected login's default space | maintainer |
| Several S3 defaults in a version-2 file | the last one is active; the next write collapses them to one | maintainer |
| An S3 selection | replaces every previous S3 default | maintainer |
| A hosted selection | sets the login's default space and clears every S3 default | maintainer |

## Human required

None.

## Implementation notes

Complete. Sessions 2–4, 2026-10-21.

### Production

- `internal/settings`: `Set.PutDefault` stores exactly one default,
  `Set.ClearDefaults` stores none, `Set.S3Default` returns the last stored
  default, the strict decoder no longer refuses several defaults, and
  `Set.Default(StoreKey)` is gone.
- `internal/app/space.go`: a hosted selection sets the login's default space
  and then clears every stored S3 default under the settings lock, and an S3
  selection writes the complete target as the only default. Only the active
  default carries the mark; a hosted default beside an active S3 default is
  listed without it and still suppresses the no-default hint. A bare listing
  uses the stored default's own endpoint, region, profile, and path style
  unless a flag, `--endpoint`/`SLIVINGDOC_ENDPOINT`, or an AWS variable names
  an identity. A bare `space new` refuses with the create form and changes
  nothing. A failed write keeps the previous default active.
- `cmd/login/login.go`: `spaceHelp` states the one global default, the create
  form, and the identity rule of a bare listing.

### Tests

- `internal/integrationtest/scenario_space_default_test.go` (new):
  `TestScenarioSpaceActiveMark`, `TestScenarioSpaceNewKeepsDefault`,
  `TestScenarioSpaceSettingsFailures` (two subtests),
  `TestScenarioSpaceLegacyDefaults`, `TestScenarioSpaceRedaction`; helpers
  `s3DefaultEnv`, `s3Credentials`, `s3DefaultTarget`, `freshS3Bucket`,
  `createS3Bucket`, `newS3Bucket`, `privateRootOf`. Listing assertions use
  containment because the shared SeaweedFS suite holds buckets from parallel
  scenarios.
- `TestScenarioSpaceHosted` plants an S3 default with the closed endpoint and
  proves `space --storage hosted team-notes` clears it.
- `TestScenarioSpaceS3` asserts the stored default's endpoint and region.
- Unit tests in `internal/settings/settings_test.go`, `internal/app/space_test.go`,
  `internal/app/space_s3_test.go`.

### Decisions worth keeping

- `Space.HasDefault` reports a stored default even while another backend's
  default is active, so an inactive default suppresses the "No default" hint
  without carrying a mark.
- `settings.Set.S3Default` is the phase-3 entry point for the active global
  default.
- A target named `new` is reachable only through the picker, because
  `space new` is the create form (`space.6`).
- The S3 access key id stays a deliberate display (`architecture/security.md`);
  the redaction scenario forbids the secret, the login key, `sld_` tokens, and
  private paths instead.

### Documentation

`architecture/login.md`, `config.md`, `cli.md`, `running.md`, `overview.md`,
`testing.md`, `decisions.md` (decision 56 supersedes decision 54's store-key
rule, which is not rewritten), and the `settings` package row of `AGENTS.md`.

### Commands and results

```sh
go test ./internal/settings ./internal/app -count=1
# ok .../internal/settings 0.154s
# ok .../internal/app 1.175s

go test ./internal/integrationtest -run '^(TestScenarioSpaceHosted|TestScenarioSpaceS3|TestScenarioSpaceActiveMark|TestScenarioSpaceNewKeepsDefault|TestScenarioSpaceSettingsFailures|TestScenarioSpaceLegacyDefaults|TestScenarioSpaceRedaction)$' -count=1
# ok .../internal/integrationtest 14.156s

make qa
# lint: gofumpt, go vet, staticcheck, go fix clean
# test: every package ok, coverage 89.5 % (floor 70 %)
# npm-test: 35 pass, 0 fail

# One earlier `make qa` failed only on the 30 s package timeout while the host
# carried a load average of 11.5 on 8 cores. The same integration package, run
# alone against the shared S3 lease, took 20.2 s for -race -count=3, and the
# next `make qa` passed. Treat that failure as host contention, not suite cost.

go run github.com/mibk/dupl@v1.0.0 -t 80 .
# 3 clone groups, all pre-existing and structural: two test-table bodies,
# the per-flag Bind boilerplate of two flag sets, and two merge assertions
```

## Review findings

### Review 1, 2026-10-21

No phase-2 `R` finding remains. Hosted selection clears the S3 default; S3
selection stores one complete default. Listing marks only the active default.
The legacy version-2 and failed-write scenarios exercise these paths.

Root review filed R1-02 for backend-precedence text in
`architecture/login.md`; that fix is in the shared architecture docs.

### Review 2, 2026-10-21

Verified good: every phase-2 acceptance row names its matching scenario, and
every error row is exercised by `TestScenarioSpaceSettingsFailures` or
`TestScenarioSpaceS3`. The scenarios cover hosted selection clearing S3,
complete S3 target replacement, the single active mark, legacy version-2
defaults, create-without-selection, failed writes, and redaction. The settings
write order leaves the previous S3 default active if either hosted-default
write fails.

R1-01 and R1-02 remain closed; the gate policy lists `dupl` separately from
`make qa`, and the storage-selection documents were checked against the
implementation and the authoritative table in `architecture/login.md`.
