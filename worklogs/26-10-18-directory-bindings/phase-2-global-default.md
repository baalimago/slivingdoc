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

Not started.

## Review findings

None.
