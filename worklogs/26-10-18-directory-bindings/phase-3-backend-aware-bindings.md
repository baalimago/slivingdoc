# Phase 3 — Backend-aware bindings

**Parent:** root · **Class:** composition · [README](README.md)

## Goal

Give every command of one notebook directory one target by implementing the `bind` command contract over the shipped settings and S3 members and Phase 2's global default, and record the target a successful pull or commit proved.

## Interface

The interface is `bind`: one target for every command of one notebook directory. It is written to the CLI: `cmd/serve`, `cmd/pull`, `cmd/commit`, `cmd/status`, `cmd/log`.

```text
serve  [--workspace-root <dir>] [--private-root <dir>] <the target flags> [<the shared flags>]
pull   [<dir>] <the target flags>
commit [<dir>] -m <message> <the target flags>
status [<dir>] <the target flags>
log    [<dir>] [--limit <n>] <the target flags>

the target flags: --storage auto|hosted|s3, --bucket|--space <name>, --endpoint <url>,
                  --region <region>, --profile <name>, --prefix <prefix>, --path-style
the slivingdoc variables mirror them: SLIVINGDOC_STORAGE, SLIVINGDOC_BUCKET,
                  SLIVINGDOC_SPACE, SLIVINGDOC_ENDPOINT, SLIVINGDOC_PREFIX,
                  SLIVINGDOC_PATH_STYLE
```

`pull`:

1. An omitted directory is the workspace root.
2. The backend resolves in this order: `SLIVINGDOC_TOKEN` selects hosted; `--storage` or `SLIVINGDOC_STORAGE` selects its named backend; a `--bucket` or `SLIVINGDOC_BUCKET` name selects S3; otherwise the directory record's kind; otherwise an S3 default selects S3; otherwise a usable stored login selects hosted; otherwise S3. The existing `--space` or `SLIVINGDOC_SPACE` ambiguity refusal beside an S3 record, S3 default, or S3 setting is unchanged, and the shipped `--space`/login rules fill the remaining cases. (I4, I5)
3. Each target field resolves per field:
   | Field | Order, highest first |
   | --- | --- |
   | name | `--bucket`/`--space`, `SLIVINGDOC_BUCKET`/`SLIVINGDOC_SPACE`, record, global default |
   | profile | `--profile`, record, global default, `AWS_PROFILE`, the ambient chain |
   | endpoint | `--endpoint`, record, global default, `AWS_ENDPOINT_URL_S3`, the SDK or profile |
   | region | `--region`, record, global default, `AWS_REGION`, `AWS_DEFAULT_REGION`, the profile or SDK |
   | prefix | `--prefix`, `SLIVINGDOC_PREFIX`, record, global default, the built-in default |
   | path style | `--path-style`, `SLIVINGDOC_PATH_STYLE`, record, global default, the effective endpoint or dotted name |

   A record supplies a field only where its kind is the kind the earlier resolution selected. A `--storage` or `SLIVINGDOC_STORAGE` choice of another kind leaves the record out of every field, and that run's own flags, variables, global default, and ambient configuration fill them.

   For hosted, the selected login's endpoint supplies the endpoint and the S3 fields stay empty. (I4)
4. The S3 default is a complete target: its profile, endpoint, region, prefix, and path style apply without any environment variable. (I5)
5. A hosted record with no endpoint uses the selected login's endpoint, and a hosted target with no credential is refused naming the space, never read as an S3 bucket.
6. A named prefix that contradicts the record is refused with the `prefixMismatch` wording.
7. An unreadable settings file refuses the command with the existing `unusableSettings` wording.
8. After the store is built, the effective endpoint, region, profile, and path style come from the built store when it reports them. The endpoint, region, and path style feed the notebook identity and the probe proof, and every one of the four feeds the record; the profile is configuration and reaches neither the identity nor the proof. A record's profile can supply a region that no flag or variable named; the effective region then differs from the one the directory used before, so the notebook identity changes once and the first operation under that profile builds a fresh private state and imports the notebook's packs again. No note is lost, because `current` is the only accepted-state authority. (I9)
9. A successful pull records the full effective target when the directory remembered nothing and the name came from a flag or a variable. It records nothing when the name came from the global default or from `SLIVINGDOC_TOKEN`. (I3, I6, I7)
10. A failed pull records nothing. (I3)
11. A record-sourced target is not rewritten; a flag or variable beside a record applies to that run alone. (I7)
12. A refusal exits nonzero with the structured error report; a success prints the OK line with the generation and the resolved directory on stdout; neither carries a credential, key, token, or private path. (I2)
13. A record write that fails after a successful publish leaves the operation's result unchanged: the command still prints its OK line and writes no record, and the failure goes to stderr. (I3)

`commit`:

1. `-m`/`--message` is required; a missing message is `INVALID_REQUEST`.
2. It resolves, refuses, records, and reports exactly as `pull` does (`pull.1` through `pull.13`).

`status`:

1. It resolves as `pull` does and reads the record, and it writes none. (I3)

`log`:

1. `--limit` bounds the number of accepted publications it reports.
2. It resolves and reads as `status` does (`status.1`). (I3)

`serve`:

1. It binds one notebook directory as Phase 1 defines. (I1)
2. A scratch directory carries no record: the server neither reads nor writes one. (I8)
3. A durable directory reads its record before the transport starts and writes one after a successful pull or commit, under the same rules as the CLI. (I3, I6, I7, I8)
4. It resolves one target before the transport starts, as `pull` does, and no tool call switches it. (I4, I5)
5. The store check uses the existing probe proof. (I9)
6. A record write that fails after a successful tool call leaves the result unchanged and is logged against the request. (I3)

Consumes:

- `notes_pull`, `notes_commit (phase 1)`: the two tools the server serves
- `global default (phase 2)`: the active S3 or hosted default
- `settings:Target`, `settings:Entry`, `settings:Set`, `settings:Set.Lookup`, `settings:Set.Put`, `settings:Set.Entries`, `settings:Set.PutDefault`, `settings:File.Load`, `settings:File.Save`, `settings:File.Lock (import)`: the record
- `credentials:Set`, `credentials:Login (import)`: the hosted credential
- `credentials:Set.ForEndpoint`, `credentials:Set.Only`, `credentials:Set.DefaultSpace (import)`: the selected login, its endpoint, and its default space
- `s3store:Config.Profile`, `s3store:Resolved`, `s3store:Store.Resolved (import)`: the effective S3 identity
- `storage:ValidatePrefix (import)`: the prefix grammar
- `storage:Probe (import)`: the compatibility check
- `workspace:Identity (import)`: the identity the effective values feed

## Acceptance criteria

| #   | Criterion                                                                                                  | Proves                          | Proof                                                                                           |
| --- | ---------------------------------------------------------------------------------------------------------- | ------------------------------- | ----------------------------------------------------------------------------------------------- |
| 1   | A bare command uses the record of its own directory.                                                       | `pull.2`                        | `go test ./internal/integrationtest -run '^TestScenarioRememberedSpaceReachesTheHostedStore$'`  |
| 2   | A flag or variable beats the record and is not written back.                                               | `pull.3`, `I4`                  | `go test ./internal/integrationtest -run '^TestScenarioExplicitChoiceBeatsTheRememberedSpace$'` |
| 3   | A bare command with no record uses the active S3 default as a complete target.                             | `pull.4`                        | `go test ./internal/integrationtest -run '^TestScenarioBareCommandUsesTheS3Default$'`           |
| 4   | A successful first pull records the target its flags named.                                                | `pull.9`, `I3`, `I7`            | `go test ./internal/integrationtest -run '^TestScenarioRecordFirstTarget$'`                     |
| 5   | A target named by the global default or a token is never recorded.                                         | `pull.9`, `I6`                  | `go test ./internal/integrationtest -run '^TestScenarioRecordFirstTarget$'`                     |
| 6   | A failed pull, a record-sourced pull, and a failed record write leave the record untouched.                | `pull.10`, `pull.11`, `pull.13` | `go test ./internal/integrationtest -run '^TestScenarioRecordWriteFailures$'`                   |
| 7   | An unreadable settings file refuses the command and leaves it byte-identical.                              | `pull.7`                        | `go test ./internal/integrationtest -run '^TestScenarioUnusableSettingsFileRefusesStartup$'`    |
| 8   | A durable server reads its record before the transport starts and writes one after a successful operation. | `serve.3`                       | `go test ./internal/integrationtest -run '^TestScenarioServeReadsTheRecord$'`                   |
| 9   | A scratch server carries no record.                                                                        | `serve.2`, `I8`                 | `go test ./internal/integrationtest -run '^TestScenarioEphemeralNotebook$'`                     |
| 10  | A record profile that changes the region changes the identity once and loses no note.                      | `pull.8`                        | `go test ./internal/integrationtest -run '^TestScenarioResolvedEffectiveValues$'`               |

## Test cases

| Test                                                      | Level                | Proves                                                          | Given                                                                                                                    | Then                                                                                                                                          | Must not                                                  |
| --------------------------------------------------------- | -------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| `TestScenarioRememberedSpaceReachesTheHostedStore`        | integration scenario | `pull.2`, `pull.9`, `pull.11`, `status.1`, `log.2`, criterion 1 | a durable directory that remembers a hosted space                                                                        | bare pull, commit, status, and log reach it, a sibling keeps the login's default, and the settings bytes are unchanged                        | write a record                                            |
| `TestScenarioExplicitChoiceBeatsTheRememberedSpace`       | integration scenario | `pull.3`, `pull.11`, `I4`, criterion 2                          | a record beside `--space`, `--bucket`, `SLIVINGDOC_SPACE`, and `SLIVINGDOC_BUCKET`                                       | each run reaches the named space and the settings bytes are unchanged                                                                         | rewrite the record                                        |
| `TestScenarioRememberedSpaceBeatsS3Settings`              | integration scenario | `pull.2`, `pull.5`                                              | a hosted record beside shared AWS files and variables                                                                    | `--storage auto` and `--storage hosted` both reach the record                                                                                 | reach the S3 endpoint                                     |
| `TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace`      | integration scenario | `pull.2`, `pull.3`                                              | a hosted record beside `SLIVINGDOC_TOKEN`, and beside `--storage s3`                                                     | the token reaches its own space, and the S3 run asks for a bucket                                                                             | consult the record                                        |
| `TestScenarioRememberedSpaceRefusals`                     | integration scenario | `pull.5`, `pull.6`                                              | each way a remembered space can fail                                                                                     | each refusal names its cause and fix, reaches no store, and changes no bytes                                                                  | reach a store                                             |
| `TestScenarioSeveralStoredLoginsRefuseTheRememberedSpace` | integration scenario | `pull.5`                                                        | two stored logins beside a record                                                                                        | the run refuses for `--endpoint` and reaches neither endpoint                                                                                 | reach a store                                             |
| `TestScenarioUnusableSettingsFileRefusesStartup`          | integration scenario | `pull.7`, criterion 7                                           | each settings row this build cannot read                                                                                 | each refusal names the file and the way out, and the bytes are unchanged                                                                      | reach a store                                             |
| `TestScenarioServeReadsTheRecord`                         | integration scenario | `serve.3`, `serve.4`, `serve.6`, criterion 8                    | a durable directory with a record                                                                                        | the server reaches the recorded space, records a target after a successful commit, and leaves the call result unchanged when that write fails | read a record from a scratch directory                    |
| `TestScenarioEphemeralNotebook`                           | integration scenario | `serve.1`, `serve.2`, `I1`, `I8`, criterion 9                   | a server with neither root configured                                                                                    | the tools operate in the scratch directory, and no record is read or written                                                                  | touch the settings file                                   |
| `TestScenarioSharedPackCacheReusesStoreProof`             | integration scenario | `serve.5`                                                       | a proof recorded by the same version and store configuration                                                             | the next process skips the probe                                                                                                              | probe again                                               |
| `TestScenarioCLIPullCommitRoundTrip`                      | integration scenario | `pull.12`, `commit.2`, `I2`                                     | one-shot pull and commit over a fresh directory                                                                          | both print the plain OK report with the generation and directory, and exit zero                                                               | print a key or a private path                             |
| `TestScenarioCLIStatusAndLog`                             | integration scenario | `status.1`, `log.1`, `log.2`, `pull.12`                         | a pulled notebook with edits and two publications                                                                        | status lists what a commit would publish and changes nothing; log lists newest first and honours `--limit`                                    | write the record                                          |
| `TestScenarioCLIUsageRefusals`                            | integration scenario | `commit.1`, `serve.3`                                           | a commit with no message against a probe-failing store, and the help output of `pull` and `status`                       | the message refusal exits nonzero before any startup dependency loads, and the help names `serve` among the commands that read the record     | touch the store                                           |
| `TestScenarioCLIOmittedDirectory`                         | integration scenario | `pull.1`                                                        | pull, commit, status, and log with no directory argument                                                                 | each uses the workspace root                                                                                                                  | use the working directory when a root is configured       |
| `TestScenarioRecordFirstTarget`                           | integration scenario | `pull.9`, `I3`, `I6`, `I7`, criteria 4, 5                       | a directory that remembers nothing, walked through a flag-named target, a global-default target, and a token target      | only the flag-named run records the full target                                                                                               | record a default-named or token-named target              |
| `TestScenarioBareCommandUsesTheS3Default`                 | integration scenario | `pull.2`, `pull.4`, `I5`, criterion 3                           | a global S3 default and a directory that remembers nothing, with no S3 variable                                          | a bare pull reaches that bucket with its profile, endpoint, region, prefix, and path style                                                    | require an environment variable                           |
| `TestScenarioRecordWriteFailures`                         | integration scenario | `pull.10`, `pull.11`, `pull.13`, `I3`, criterion 6              | a pull that fails at the store, a pull whose target came from the record, and a successful pull whose record write fails | no record changes on any of them, and the successful pull still prints its OK line                                                            | write a partial record or turn the success into a refusal |
| `TestScenarioResolvedEffectiveValues`                     | integration scenario | `pull.8`, `I9`, criterion 10                                    | a record whose profile supplies a region no flag named                                                                   | the identity changes once, the packs are imported again, every note survives, and the profile appears in the record only                      | lose a note                                               |

## Error management

| Failure                                                           | Expected outcome                                                              | Test                                                 |
| ----------------------------------------------------------------- | ----------------------------------------------------------------------------- | ---------------------------------------------------- |
| `SLIVINGDOC_TOKEN` beside an S3 endpoint, or naming another space | the shipped startup refusal; nothing is recorded                              | `TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace` |
| A run whose named prefix contradicts the record                   | the `prefixMismatch` refusal; nothing is recorded                             | `TestScenarioRememberedSpaceRefusals`                |
| The settings file cannot be read                                  | the `unusableSettings` refusal; the file is untouched                         | `TestScenarioUnusableSettingsFileRefusesStartup`     |
| The record write fails after a successful pull                    | the OK line still prints and the failure goes to stderr; no record is written | `TestScenarioRecordWriteFailures`                    |
| `--storage s3` beside a hosted record                             | the record supplies no field, so the run asks for a bucket                    | `TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace` |
| The built store reports a region no flag named                    | the identity changes once and the first operation imports the packs again     | `TestScenarioResolvedEffectiveValues`                |

## Golden diff

None.

## Removes

None.

## Documentation

Update every architecture doc that states the old resolution and the old
record rules, in this phase's commit: `architecture/config.md` (the record
resolution order, the record write, S3 records, and `serve`),
`architecture/login.md` (a directory remembers its space, now on both
backends), `architecture/running.md` (a directory remembers its space, and the
effective values), `architecture/cli.md` (which commands remember a
directory, including `serve`), `architecture/workspace.md` (the association
note), `architecture/overview.md` (the `settings` package map),
`architecture/testing.md`, and `architecture/decisions.md` (append a decision;
decision 49's `serve`-never-reads rule and its resolution order are
superseded, not rewritten).

## Local facts

### Invariants

| ID  | Invariant | Members bound |
| --- | --------- | ------------- |
| I3  | Only a successful pull or commit writes a record, and only when the directory remembered nothing and the name did not come from the default. | `pull`, `commit`, `serve` |
| I4  | A flag or slivingdoc variable wins over the record, the record wins over the global default, and the global default wins over ambient AWS. | `pull`, `commit`, `serve` |
| I6  | A target whose name came from the global default is never recorded. | `pull`, `commit`, `serve` |
| I7  | An explicit flag or variable records a target only where the directory remembers nothing. | `pull`, `commit`, `serve` |
| I8  | A scratch server neither reads nor writes a directory record. | `serve` |
| I9  | The profile is configuration, never part of the notebook identity or the probe proof. | `pull`, `commit`, `serve` |

### Parameters

| Parameter | Default | Decided by |
| --------- | ------- | ---------- |
| Probe proof version | 1, unchanged | existing `internal/app` |
| Notebook identity | unchanged, with no profile component | maintainer |
| Effective values | taken from `Store.Resolved` after the store is built | maintainer |

## Human required

None.

## Implementation notes

Not started.

## Review findings

None.
