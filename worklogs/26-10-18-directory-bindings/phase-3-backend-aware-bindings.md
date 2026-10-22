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
2. The backend resolves in this order: `--storage s3` or `SLIVINGDOC_STORAGE=s3` selects S3 and ignores `SLIVINGDOC_TOKEN`; otherwise `SLIVINGDOC_TOKEN` selects hosted; otherwise `--storage hosted` or `SLIVINGDOC_STORAGE=hosted` selects hosted; otherwise, in `auto`, a `--bucket` or `SLIVINGDOC_BUCKET` name selects S3 only beside an S3 signal; otherwise the directory record's kind; otherwise the S3 default selects S3; otherwise a usable stored login selects hosted; otherwise S3. A selected stored login that is expired or ambiguous refuses startup rather than falling through to S3. The existing `--space` or `SLIVINGDOC_SPACE` ambiguity refusal beside an S3 record, S3 default, or S3 setting is unchanged, and the shipped `--space`/login rules fill the remaining cases. (I4, I5)
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
| `TestScenarioStorageSelection`                            | integration scenario | `pull.2`                                                        | token, storage mode, bucket spelling, and S3-signal combinations                                                          | `--storage s3` beats a token; otherwise the token beats S3 signals; `--storage hosted` beats a record; bucket spelling selects S3 beside a signal | select a different backend                               |
| `TestScenarioExpiredLoginIsRefused`, `TestScenarioTwoLoginsNeedAnEndpoint` | integration scenario | `pull.2` | no record or S3 default, with an expired selected login or multiple logins and no endpoint | startup refuses rather than falling through to S3 | contact storage or record a target |
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
| `SLIVINGDOC_TOKEN` is paired with a different named space         | the startup refusal; nothing is recorded                                        | `TestScenarioHostedStartupRefusals`                 |
| The selected stored login is expired or ambiguous                  | startup refuses rather than falling through to S3                              | `TestScenarioExpiredLoginIsRefused`, `TestScenarioTwoLoginsNeedAnEndpoint` |
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

Partial. Worker session: 2026-10-21, session 4 (handover before the context
limit). The tree already held an unfinished phase-3 attempt inside
`internal/app/storage.go` when this session started; it did not compile. The
production path now compiles and the app package tests pass.

### Production complete

- `internal/app/association.go`:
  - `settingsView` (`record`, `recorded`, `s3Default`, `defaultFound`) and
    `settingsAssociation.read`, which returns the directory's record and the
    one active S3 default.
  - `Runtime.recordEntry` and `config.target` write a complete target after a
    successful pull or commit. It writes only where the directory remembered
    nothing (`cfg.recorded` false) and a flag or slivingdoc variable named
    the target (`bucketFrom.kind() != kindOther`). A `SLIVINGDOC_TOKEN`
    process never records, a scratch server (`sessionDir` non-empty) never
    records, and a record-sourced or default-sourced target is never
    rewritten.
  - `rememberingService` wraps the notebook view for the MCP server and calls
    `rememberSpace` after a successful tool call; a failed record write logs
    on the request-scoped notebook logger and changes no result.
  - `ProcessOptions.WithAssociation` documentation now includes `serve`.
- `internal/app/config.go`: `Flags.resolve` skips the association when the
  process owns a session directory, passes the resolved `settingsView`, and
  takes every target field from `resolveStorage` (`prefix`, `region`,
  `profile`, `endpoint`, `pathStyle`, `remembered`, `recorded`). The old
  `resolveS3Store` and `applyRememberedPrefix` paths are removed.
- `internal/app/storage.go`: `selectKind` now returns the selection through a
  pointer (the old by-value call discarded the chosen kind), and a variable's
  token works under `--storage hosted` as well as `auto`. `resolveS3Target`
  and `resolveTargetPrefix` implement the record/default field order; the
  `prefixMismatch` call is fixed.
- `internal/app/app.go`: `resolvedStore` (`Resolved() s3store.Resolved`) and
  `config.withStoreResolution` apply the built S3 store's effective endpoint,
  region, profile and path style before the probe proof, identity and service
  configuration; `Runtime.Serve` passes `rememberingService{r}` to the MCP
  server.
- `cmd/serve/serve.go`: a durable server sets `opts.WithAssociation("")`, so
  its record is keyed by the workspace root; the scratch case is skipped in
  `Flags.resolve` by the session directory.

### Tests updated and green

- `internal/app/storage_test.go`: `TestRememberedTargetKeepsOnlyAHostedRecord`
  is replaced by `TestSettingsViewReadsTheRecordAndDefault` (record + default,
  another key, default alone, unreadable file, no association).
- `internal/app/remember_test.go`: `TestS3ModeNeverRecords` became
  `TestS3ModeRecordsTheTarget`; `TestRecordedKeyIsTheResolvedPath` now proves
  a record-sourced run rewrites nothing; warning wording is
  `remembering the notebook failed`.

```sh
go build ./...                                              # ok
go test ./internal/app -count=1 -timeout 180s               # ok
```

### Remaining before the phase is complete

1. Integration scenarios (the behavioral contract; none run in this session):
   - Replace `TestScenarioServeIgnoresTheRememberedSpace` in
     `internal/integrationtest/scenario_remembered_test.go` with
     `TestScenarioServeReadsTheRecord`: a durable server reads its record
     before the transport starts, reaches the recorded hosted space, records a
     target after a successful commit, and leaves the tool result unchanged
     when that write fails.
   - Extend `TestScenarioEphemeralNotebook` so a scratch server neither reads
     nor writes the settings file (plant an unreadable file and prove startup
     still succeeds, and prove no file appears after a pull/commit).
   - Add `TestScenarioRecordFirstTarget` (flag-named records the full target;
     global-default and token targets do not), `TestScenarioBareCommandUsesTheS3Default`
     (the stored S3 default supplies endpoint, region, prefix and path style
     with no S3 identity variable), `TestScenarioRecordWriteFailures` (failed
     pull, record-sourced pull, failed write), and
     `TestScenarioResolvedEffectiveValues` (a record profile supplies the
     region, the identity changes once, packs import again, no note is lost,
     and the profile is in the record but not the identity/proof).
   - Add the missing `TestScenarioCLIOmittedDirectory` and update
     `TestScenarioCLIUsageRefusals`: its help row currently requires
     `serve never does`, which phase 3 reverses.
   - Re-run the phase-2 remembered/space scenarios; they should hold, but the
     S3 default and record paths changed materially.
2. Documentation, in the phase commit:
   - `architecture/config.md`: resolution order per field, the record write,
     S3 records, durable/scratch `serve`, and the effective values from
     `Store.Resolved`. Remove the old `applyRememberedPrefix` description.
   - `architecture/login.md`: a directory remembers its target on both
     backends; `serve` reads a durable record and a scratch server does not.
   - `architecture/running.md` ("A directory remembers its space" and the
     flag table), `architecture/cli.md` (which commands remember a directory,
     including `serve`), `architecture/workspace.md` (the association note),
     `architecture/overview.md` (the `settings` package row),
     `architecture/testing.md`, and `architecture/decisions.md` (append a
     decision; decision 49's serve-never-reads rule and its resolution order
     are superseded, not rewritten; decision 50's hosted-only record is
     extended).
   - `internal/app/config.go` `FlagReference` and `HelpText`: the final
     paragraph still says `serve never does`; it must state the four layers
     and name `serve` among the commands that read a durable record.
   - `AGENTS.md`: the settings row and the command behaviour if it still says
     only path-taking commands record.
3. `make qa` and the dupl signal. No `make qa` run happened in this session.

### Known risks for the next worker

- `config.recorded` is the resolution-time fact. A process that removes the
  settings file after startup does not notice; a new process does. This is
  intended by the phase (the record is read once per process).
- `withStoreResolution` changes `cfg.region` for S3 after the store is built;
  the probe proof and identity use the new value, and the record does too. A
  fake store in unit tests reports no resolution, so its config keeps the
  resolved values.
- The hosted record still carries an empty endpoint, because the selected
  login supplies the site. The S3 record carries the effective endpoint,
  region, profile, prefix and path style.
- All phase-1 and phase-2 changes in this tree are still uncommitted.

### Re-plan (2026-10-21, root review 2)

`pull.2` now names the shipped backend precedence and keeps its existing
`--space` ambiguity rule. `TestScenarioStorageSelection` covers the corrected
choices. The implementation already follows this order; the change corrects
the signed clause and its evidence, not the runtime behavior. The maintainer
signed off interfaces 3 on 2026-10-21, so the phase is Complete and A02 is
closed.

## Review findings

### Review 1, 2026-10-21

- **A02 · design andon · open.** `pull.2` states a backend precedence order
  in which a token selects hosted and a `--bucket` name selects S3. The code
  gives explicit `--storage s3` priority over a token, and `--bucket` selects
  S3 in `auto` only beside an S3 signal. For example,
  `TestScenarioExplicitChoiceBeatsTheRememberedSpace` passes `--bucket` with
  a hosted record and login, then expects the named hosted space. The signed
  interface does not state these exceptions. Do not change code or the
  interface without maintainer re-planning and sign-off.

Verified good: the durable serve reads its record before serving and records
after a successful tool call; scratch serve reads and writes none. CLI record
precedence, default target fields, record-write failures, and effective S3
values have black-box scenarios. No phase-3 `R` finding remains.

### Review 2, 2026-10-21

- [x] **A01 · quality andon · Closed +wip.** Round two's `make qa` passed;
  the standalone `-race -count=3 -timeout=30s` integration run also passed.
  The previous timeout reports match the supplied host-contention evidence.
  The narrow timing margin is recorded in the note below; the gate settings
  are unchanged.
- [x] **A02 · design andon · Closed +wip.** `pull.2` was re-planned to match
  `selectKind` and the table in `architecture/login.md`: explicit S3 mode,
  token, explicit hosted mode, bucket spelling plus an S3 signal, record,
  S3 default, stored login, then S3. The existing `--space` ambiguity clause
  remains. `TestScenarioStorageSelection` proves the mode/token order, the
  S3-signal bucket choice, and hosted mode over an S3 record. The maintainer
  signed off interfaces 3 on 2026-10-21.
- [x] **R2-01 · minor · Closed +wip.** `architecture/running.md:791` said a token
  beside an endpoint or S3 endpoint variable was refused, but `pull.2` and
  `selectKind` make the token hosted unless S3 mode was explicitly selected.
  For example, `TestScenarioStorageSelection/the token beats the login` uses a
  token with S3 settings and succeeds through hosted storage. The running guide
  now states the actual precedence and endpoint behavior.
- [x] **R2-02 · minor · Closed +wip.** `architecture/cli.md:20` said successful
  `Runtime.Pull` and `Runtime.Commit` record only a hosted notebook. A first
  S3-targeted pull also records its complete target, as
  `TestScenarioRecordFirstTarget` proves. The package-map text now names the
  backend-neutral target; the status trailer text also covers S3 records.

Verified good: every phase-3 acceptance criterion and error row maps to an
integration scenario. The black-box cases cover record and default precedence,
both backends, success-only recording, failed record writes, durable and
scratch `serve`, effective identity changes without note loss, and every
selection branch in `pull.2`. `make qa` exercises the race suite at three
counts; the full integration package also passed a second standalone run.
Phase-1 and phase-2 clauses and the README's durable/scratch, record, default,
and backend-aware flows were checked against their scenarios. R1-01 and R1-02
remain closed; `dupl` is separate from `make qa`, and the corrected login and
security precedence text matches the code.

Note: the independent round-2 `make qa` evidence reported the integration
package at 28.802 s against its 30 s test timeout, a 1.198 s margin; the
independent standalone three-count run was 26.7 s. This is a narrow margin
under host contention, not a reason to weaken the gate. The review's two green
`make qa` runs reported package durations of 30.391 s and 35.379 s; the
standalone flake-check run reported 34.419 s. These package durations include
`TestMain` setup before `m.Run` starts the strict test timeout. Every run
passed without a timeout, so A01 is closed as host contention.

## Session 5 handover (2026-10-21, worker)

Phase 3 is still in progress. The production path compiles and the new
integration scenarios pass. Documentation is the main remainder.

### Done in session 5

Production fixes on top of the session-4 tree:

- `internal/app/storage.go`: `resolveS3Target` refuses a `--space` or
  `SLIVINGDOC_SPACE` name beside an S3 record or the S3 default only. With
  neither, the spelling names an S3 bucket, including with `--storage s3`.
  `TestResolveStorage` gained the row `s3: --space names the bucket with no
  hosted alternative`.
- `internal/app/app.go`: `Runtime.NotebookDirectory()`.
- `cmd/pull`, `cmd/commit`, `cmd/status`, `cmd/log`: an omitted path is
  reported as the resolved workspace root. Before this, a bare command's
  success or status report carried an empty path.
- `internal/app/config.go` and the four command help texts: the closing
  paragraph now names the four layers, the one global default, the complete
  target recorded, and `serve` with a durable directory.

Integration contract:

- Replaced `TestScenarioServeIgnoresTheRememberedSpace` with
  `TestScenarioServeReadsTheRecord` (record read before the transport, a
  durable server records a flag-named target, a failed record write logs and
  changes no tool result).
- `TestScenarioEphemeralNotebook` now plants a settings file this build
  cannot read and proves a scratch server neither reads nor writes it.
- Added `TestScenarioCLIOmittedDirectory` and updated the help rows of
  `TestScenarioCLIUsageRefusals`.
- Added `internal/integrationtest/scenario_bindings_test.go` with
  `TestScenarioRecordFirstTarget`, `TestScenarioBareCommandUsesTheS3Default`,
  `TestScenarioRecordWriteFailures`, and
  `TestScenarioResolvedEffectiveValues`.
- Harness isolation: `spawnHelperIn` and `runCLIOnTTY` now use
  `privateTempDir` (0700) for `SLIVINGDOC_CONFIG_DIR` and `HOME`, and the
  TTY helper sets both. This fixed the pre-existing
  `TestScenarioCLIColourOnTerminal/read-only refusal report` failure, where
  the TTY helper inherited the host's configuration directory and therefore
  a different S3 identity.
- `internal/app/remember_test.go`: `TestServeNeverRecords` became
  `TestScratchServeNeverRecords`.

Documentation started: the Flow block of `architecture/config.md` now shows
roots before the record, the kind and per-field order, `withStoreResolution`,
and `--space` alone naming a bucket.

### Verified commands and results

```sh
go test ./internal/app -count=1 -timeout 300s                       # ok
go test ./internal/integrationtest -run '^TestScenarioServeReadsTheRecord$' -count=1 -timeout 200s   # ok
go test ./internal/integrationtest -run '^TestScenarioEphemeralNotebook$|^TestScenarioCLIOmittedDirectory$|^TestScenarioCLIUsageRefusals$' -count=1 -timeout 200s  # ok
go test ./internal/integrationtest -run '^TestScenarioRecordFirstTarget$|^TestScenarioBareCommandUsesTheS3Default$' -count=1 -timeout 300s  # ok
go test ./internal/integrationtest -run '^TestScenarioRecordWriteFailures$' -count=1 -timeout 300s   # ok
go test ./internal/integrationtest -run '^TestScenarioResolvedEffectiveValues$' -count=1 -timeout 300s # ok
go test ./internal/integrationtest -run '^TestScenarioCLIStderrOnTerminal$|^TestScenarioCLIColourOnTerminal$' -count=1 -timeout 250s # ok
go test ./internal/integrationtest -run 'TestScenarioRemembered|TestScenarioExplicitChoice|TestScenarioServe|TestScenarioEphemeral|TestScenarioTokenAndS3|TestScenarioSeveralStored|TestScenarioUnusable|TestScenarioRecord|TestScenarioBareCommand|TestScenarioResolved|TestScenarioCLI|TestScenarioPullRecords|TestScenarioEachDirectory|TestScenarioFlagNaming|TestScenarioStatusNames|TestScenarioFailedOperation|TestScenarioSpace' -count=1 -timeout 500s # ok
```

### Remaining before the phase is complete

1. `architecture/config.md` Behavior paragraphs, still describing the old
   hosted-only record and `applyRememberedPrefix`:
   - Key files: `rememberedTarget`, `Runtime.recordsSpace`, and
     `config.applyRememberedPrefix` no longer exist. Cite `selectKind`,
     `resolveS3Target`, `resolveTargetPrefix`, `settingsView`, `recordEntry`,
     `rememberingService`, `config.target`, `config.withStoreResolution`,
     and `Runtime.NotebookDirectory`.
   - Settings table: the scoped settings row still says a durable `serve`
     ignores the file.
   - The paragraphs `The remembered notebook of a directory`,
     `Recording the association`, and `The operator surface of the record`:
     state the per-field order, S3 records, the complete target write, the
     durable/scratch `serve` split, and the effective values from
     `Store.Resolved`.
2. Update `architecture/login.md`, `architecture/running.md`,
   `architecture/cli.md`, `architecture/workspace.md`,
   `architecture/overview.md`, `architecture/testing.md`, and
   `architecture/decisions.md` (append a decision; decision 49's
   serve-never-reads rule and its resolution order are superseded, decision
   50's hosted-only record is extended).
3. Update the `AGENTS.md` settings row and any command behaviour that still
   says only path-taking commands record.
4. Run `make qa` (not run in this session). At minimum run the full
   `go test ./internal/integrationtest -count=1` once; only subsets ran.
5. Consider whether `ReportStatus` should name an S3 record's source. It
   currently prints a trailer only for `rememberedSource` (`remembered
   bucket` is silent), and the docs must match whichever choice stands.

### Known risks

- `resolvedStore` is implemented by the real `s3store`; the fake reports
  none, so unit tests keep the resolution-time values.
- The session-4 note that a hosted record keeps an empty endpoint and an S3
  record keeps the resolved identity still holds.
- All phase 1-3 changes remain uncommitted.

## Session 6 completion (2026-10-21, worker)

Phase 3 is complete. The session-5 handover left documentation and the gate;
both are finished, and the S3 scenarios were made stable under the full
`-count=3` run.

### Documentation completed

- `architecture/config.md`: the scoped-settings row now names a durable
  `serve`; the per-field order, S3 records, the complete target write, the
  durable/scratch split, and the effective values from `Store.Resolved` are
  stated in the Behavior paragraphs. The Key files row lists
  `defaultBucketSource` too.
- `architecture/login.md`, `architecture/running.md`, `architecture/cli.md`,
  `architecture/workspace.md`, `architecture/overview.md` and
  `architecture/testing.md` state the backend-aware record, `serve`'s
  durable/scratch split, the per-field order, and the new scenarios.
- `architecture/decisions.md` decision 57 records the backend-aware binding;
  it supersedes decision 49's serve-never-reads rule without rewriting it and
  extends decision 50's hosted-only record.
- `AGENTS.md` states the four layers and names a durable `serve` among the
  readers and writers.
- `architecture/testing.md` gained the gotcha that a scenario writing objects
  against the real S3 suite uses the shared bucket with a fresh prefix.

### Test stability

`TestScenarioRecordFirstTarget`, `TestScenarioBareCommandUsesTheS3Default` and
`TestScenarioResolvedEffectiveValues` created a bucket and then wrote into it
at once. Under the full `-count=3` run that first conditional write was
refused intermittently by the shared backend with `InternalError`, so all
three now use the shared `tests3.Bucket` with their existing fresh prefix.
The untracked diagnostic `internal/integrationtest/zz_bucket_probe_test.go`
was removed; it also consumed about 15 s of the package's 30 s budget.

### Verification

Before the fix, the full gate failed in those three scenarios and the
integration package timed out:

```sh
make qa
go test ./internal/integrationtest -race -count=3 -timeout=30s   # FAIL: 3 scenarios, timeout
```

After the fix:

```sh
go test ./internal/integrationtest -race -count=3 -timeout=30s
# ok  github.com/baalimago/slivingdoc/internal/integrationtest  17.065s
make qa
# green: lint, race suite -count=3, coverage 89.6 % (floor 70 %), npm 35 tests
go run github.com/mibk/dupl@v1.0.0 -t 80 .
# 3 clone groups, all pre-existing and acceptable (test tables and flag-holder boilerplate)
```

### Remaining

None. The review above raised no findings, and the phase's acceptance
criteria all pass through `make qa`.
