# Phase 2: space resolution and precedence

**Status:** Complete

README: [README.md](./README.md)

## Goal

Make the remembered space select the hosted store for a CLI operation, below
the flags and the environment and above the account default, and refuse every
way it can fail.

## Specification

### Where the association enters

The read enters `internal/app`'s configuration, where the flags and the
environment are already resolved, and the process options carry the pieces the
resolution needs. `ProcessOptions` gains the key and the reader, injected like
the ones beside them:

```text
ProcessOptions.NotebookPath string
ProcessOptions.Load         func() (settings.Set, error)
```

`ProcessOptions.WithAssociation(path)` sets both, so `serve` and every in-process
test that sets neither consults no association at all and keeps today's behavior.
The production `Load` reads the file once per process through `settings.Locate`,
and `settingsAssociation.read` runs the lookup in memory once the workspace root
is known.

### The key

The key is the resolved notebook path. `app.OperationPath` already returns the
cleaned absolute argument, and an omitted argument resolves to the workspace
root, which `Flags.resolve` computes before `resolveStorage` when the root
resolution moves ahead of it. This phase moves root resolution (and the
workspace root's absoluteness) ahead of `resolveStorage` in `loadConfig`, since
the association needs the root and `resolveStorage` needs nothing from roots.

Symlinks are never resolved and the key is byte-bound, so two spellings of one
directory are two entries (F21). A key outside the workspace root is never
looked up: `workspace.Open` still refuses it, and Phase 2 does not look up a
path the workspace would refuse.

### Where the space resolves

`resolveStorage` gains one input and one source. Below the mode check and
beside `resolveBucket`, when no flag and no environment named a space, it asks
the association. The new `bucketSource` value is `bucketFromRemembered`, worded
`"remembered space"`, which `architecture/login.md` lists in the same commit.

Two properties make the association host-only and therefore usable:

| Property | Mechanism | Why |
| --- | --- | --- |
| It counts like the login's default for the `s3Signals` refusal | the `auto` ambiguity check accepts a remembered source beside its login's default source | otherwise the feature refuses on any machine with an AWS file present |
| It reaches the "no login" refusal instead of falling back to S3 | the lookup happens before the `ErrNoLogin` + `auto` branch returns S3 | otherwise a logged-out machine silently pulls an S3 notebook into the directory |

The prefix follows the same rule as the space: with nothing naming it by flag
or environment, a remembered prefix supplies it. A prefix named by a flag or
the environment that differs from the remembered one is F20, a refusal naming
both.

`SLIVINGDOC_TOKEN` returns before the lookup in today's code and keeps doing so
(D11): the token names its own space, and F14 records that the association is
ignored rather than contradicted.

### Accessors

`Runtime` gains the one exported pair Phase 4 consumes, reading the resolved
space and the source that named it:

```text
func (r *Runtime) Space() string
func (r *Runtime) SpaceSource() string
```

Both are empty for an S3 process, and `SpaceSource` returns the `bucketSource`
wording, so `status` can name `remembered space` without exposing the internal
enum. The startup log record that names the source keeps its existing fields.

### Invariants this phase must hold

| Invariant | Mechanism | Test |
| --- | --- | --- |
| The settings file holds no credential | the record has no credential field, so nothing is written and nothing is read | `TestDecodeRejectsCredentialField`, the scenario's file inspection |
| The association never overrides an explicit choice | the lookup runs only after flags and the environment resolved nothing | `TestResolveSpacePrecedenceTable` |
| A remembered space selects hosted mode | the lookup precedes the `ErrNoLogin` + `auto` S3 fallback and the remembered source is host-only for `s3Signals` | `TestScenarioRememberedSpaceBeatsS3Settings`, `TestScenarioRememberedSpaceRefusals` |
| `serve` never consults the association | the reader is nil unless a path-taking command set it | `TestServeNeverLooksUpTheAssociation`, `TestScenarioServeIgnoresTheRememberedSpace` |
| A remembered space the credentials cannot reach is a refusal | every unreachable case returns before any store is built | the refusal rows of the scenario table |

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `pull` in a directory with an entry, no flags | settings store, reference gateway | the remembered space | one store read | writing the file |
| `pull` in a directory with an entry and `--space` | settings store, reference gateway | the flag's space | one store read | writing the file |
| `pull` with `SLIVINGDOC_TOKEN` and an entry | settings store | the token's space | one store read | reading the entry |
| `pull` with an entry and no credential | settings store | a refusal naming the space | none | reaching S3 |
| `pull` beside an existing `~/.aws/credentials` | settings store, injected `stat` | the remembered space | none | the ambiguity refusal |
| `serve` with an entry for its root | settings store | the launch configuration | none | reading the file |

## Acceptance criteria

| Criterion | Unit | Scenario |
| --- | --- | --- |
| A remembered space beats the stored default space and loses to a flag and to `SLIVINGDOC_SPACE` | `TestResolveSpacePrecedenceTable` | `TestScenarioRememberedSpaceReachesTheHostedStore`, `TestScenarioExplicitChoiceBeatsTheRememberedSpace` |
| The resolved source is `remembered space` for the remembered case and today's wording for the others | `TestSpaceSourceWording` | `TestScenarioRememberedSpaceReachesTheHostedStore` |
| A remembered space reaches the hosted store beside AWS signals | `TestResolveSpacePrecedenceTable` | `TestScenarioRememberedSpaceBeatsS3Settings` |
| A remembered space with no credential refuses and names the space and `slivingdoc login` | `TestRememberedSpaceRefusals` | `TestScenarioRememberedSpaceRefusals` |
| A remembered space with a login for another endpoint refuses naming both | `TestRememberedSpaceRefusals` | `TestScenarioRememberedSpaceRefusals` |
| A remembered space with several logins refuses asking for `--endpoint` | `TestRememberedSpaceRefusals` | `TestScenarioSeveralStoredLoginsRefuseTheRememberedSpace` |
| An expired login beside a remembered space refuses telling the user to log in | `TestRememberedSpaceRefusals` | `TestScenarioRememberedSpaceRefusals` |
| A remembered space fails `ValidateSpace` and refuses naming it | `TestRememberedSpaceRefusals` | `TestScenarioRememberedSpaceRefusals` |
| A remembered space the account does not hold refuses naming it | the mint refusal of `mintRefusal`, which is today's | `TestScenarioRememberedSpaceRefusals` |
| `SLIVINGDOC_TOKEN` ignores the entry and the entry is byte-identical after | `TestResolveSpacePrecedenceTable` | `TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace` |
| `--storage s3` ignores the entry and the entry is byte-identical after | — | `TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace` |
| `--storage hosted` with no space takes the remembered one | `TestResolveSpacePrecedenceTable` | `TestScenarioRememberedSpaceBeatsS3Settings` |
| A remembered prefix differs from a flag or variable and refuses naming both | `TestRememberedSpaceRefusals` | `TestScenarioRememberedSpaceRefusals` |
| A remembered prefix supplies the prefix when none is named | `TestResolveSpacePrecedenceTable` | `TestScenarioRememberedSpaceReachesTheHostedStore` |
| `serve` performs no lookup and writes nothing | `TestServeNeverLooksUpTheAssociation` | `TestScenarioServeIgnoresTheRememberedSpace` |
| A malformed, exposed, oversized or other-version file refuses startup naming the file | `TestUnusableSettingsFileRefusesStartup` | `TestScenarioUnusableSettingsFileRefusesStartup`, `TestScenarioSettingsFileMustBeTheUsers`, `TestScenarioSettingsDirectoryMustBePrivate` |
| `Runtime.Space` and `Runtime.SpaceSource` report the resolved pair | `TestRuntimeSpaceAccessors` | `TestScenarioRememberedSpaceReachesTheHostedStore` |
| The settings file is read once per process | `TestAssociationReadsTheSettingsOnce` | — |

## Error coverage

Every row is the README's failure surface. The scenario is the proof at the
command line; the unit test covers the same wording beside the resolution it
belongs to.

| Row | Failure | Expected outcome | Scenario | Unit |
| --- | --- | --- | --- | --- |
| F3 | malformed file | refuse naming the file | `TestScenarioUnusableSettingsFileRefusesStartup` | `TestUnusableSettingsFileRefusesStartup` |
| F4 | other version | refuse naming the file and the two fixes | `TestScenarioUnusableSettingsFileRefusesStartup` | `TestUnusableSettingsFileRefusesStartup` |
| F5 | exposed file or directory | refuse wording the `chmod` | `TestScenarioSettingsFileMustBeTheUsers`, `TestScenarioSettingsDirectoryMustBePrivate` | `internal/settings`: `TestLoadRefusesExposedFileAndDir` |
| F6 | a link or a non-regular file | refuse | `TestScenarioSettingsFileMustBeTheUsers` | `internal/settings`: `TestLoadRefusesWhatIsNotTheFile` |
| F7 | invalid space | refuse naming the space | `TestScenarioRememberedSpaceRefusals` | `TestRememberedSpaceRefusals` |
| F9 | no credential | refuse naming the space and `slivingdoc login` | `TestScenarioRememberedSpaceRefusals` | `TestRememberedSpaceRefusals` |
| F10 | a login for another endpoint | refuse naming both endpoints | `TestScenarioRememberedSpaceRefusals` | `TestRememberedSpaceRefusals` |
| F11 | several matching logins | refuse asking for `--endpoint` | `TestScenarioSeveralStoredLoginsRefuseTheRememberedSpace` | `TestRememberedSpaceRefusals` |
| F12 | an expired login | refuse telling the user to log in | `TestScenarioRememberedSpaceRefusals` | `TestRememberedSpaceRefusals` |
| F13 | a space the account cannot reach | refuse naming it as unreachable | `TestScenarioRememberedSpaceRefusals` | the mint refusal, which is today's |
| F14 | a token beside an entry | the token's space, the entry untouched | `TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace` | `TestResolveSpacePrecedenceTable` |
| F15 | `SLIVINGDOC_SPACE` or `SLIVINGDOC_BUCKET` | the environment's space, the entry untouched | `TestScenarioExplicitChoiceBeatsTheRememberedSpace` | `TestResolveSpacePrecedenceTable` |
| F16 | `--space`, `--bucket`, `--endpoint` | the flag's space, the entry untouched | `TestScenarioExplicitChoiceBeatsTheRememberedSpace` | `TestResolveSpacePrecedenceTable` |
| F17 | `--storage s3` | S3 mode, the entry untouched | `TestScenarioTokenAndS3ModeIgnoreTheRememberedSpace` | — |
| F18 | `--storage hosted` with no space | the remembered space | `TestScenarioRememberedSpaceBeatsS3Settings` | `TestResolveSpacePrecedenceTable` |
| F19 | ambient S3 signals | the remembered space | `TestScenarioRememberedSpaceBeatsS3Settings` | `TestResolveSpacePrecedenceTable` |
| F20 | a prefix mismatch | refuse naming both prefixes | `TestScenarioRememberedSpaceRefusals` | `TestRememberedSpaceRefusals` |

## Implementation notes

### 2026-10-03: phase 2, worker session (agent, clai)

**Deviation 1: the association is one reader on `ProcessOptions`, not a
`Lookup` and a `Load` pair.** The phase named both fields. The load alone is
enough, because the resolved workspace root is not known inside
`resolveStorage`, so the lookup needs the set it just read. The two
`ProcessOptions` fields are therefore `NotebookPath` (the key) and `Load` (the
reader), and `settingsAssociation.read(workspaceRoot)` does the lookup once the
root is known.

**Deviation 2: root resolution moved ahead of `resolveStorage` as one
extracted function.** The phase asked for a move inside `resolve`. Doing it
inside a switch left `resolve` with two copies of the root assignment, so the
move became `resolveRoots`, which returns the session directory beside the two
roots. The session directory is still created before the store resolves, and a
refusal still removes it.

**Deviation 3: the prefix is applied in `finish`, not where it resolves.** The
prefix may come from a record, but a refusal about it is a validation like every
other, and `finish` is where validation lives. `config.applyRememberedPrefix`
fills the prefix from the record where nothing named one and refuses a named
prefix that differs; it runs only when the record named the space, so an
explicit `--space` with the default prefix behaves exactly as before.

**Deviation 4: F13 needs no new branch.** A space the account cannot reach is
already refused by the site when it mints (`mintRefusal`), because the mint
names the space and the site answers `no_space`. The unit test covers the
grammar row (F7) and the endpoint, ambiguity and expiry rows; the reachability
row is the site's own refusal, reached at the first mint.

### 2026-10-03: phase 2 scenarios and gate, worker session (agent, clai)

**Deviation 5: the named unit tests became tables and scenarios.** The phase
named one test per refusal row. Each row is one subtest of a table where the
unit test belongs (`TestRememberedSpaceRefusals`,
`TestUnusableSettingsFileRefusesStartup`), and one row of a scenario table at
the command line (`TestScenarioRememberedSpaceRefusals`,
`TestScenarioUnusableSettingsFileRefusesStartup`). The acceptance tables above
now name both, so the mapping from row to test stays exact.

**Deviation 6: the POSIX rows of F5 and F6 are their own file.** A symbolic
link, a FIFO and the owner and permission checks need build-tagged helpers, so
`scenario_remembered_posix_test.go` carries them, exactly as the credentials
file's own scenarios do. The unprivileged rows (malformed, credential field,
version, bound) need no POSIX facility and stay in the portable file.

**Deviation 7: one test for the once-per-process read.** The README's limit
row asks for a test that counts `Load` calls. Counting a reader the test
injects proves the caller's own reader, not `scopedSettings`, so
`TestAssociationReadsTheSettingsOnce` changes the file between the two reads
and asserts the second read answers from the set of the first. That proves the
bound the row exists for: one read of the file per process.

**Commands run, and their results.**

```bash
go build ./...                                  # ok
go vet ./...                                    # ok
go test -count=1 ./internal/integrationtest/... # ok, every package
go test -count=1 -run TestScenario -v ./internal/integrationtest/...   # ok
go test -count=1 ./internal/app/... ./internal/settings/... ./internal/credentials/... ./cmd/...
                                                # ok, before and after the change
make qa                                         # ok: lint, test, npm-test
go run github.com/mibk/dupl@v1.0.0 -t 80 .    # 2 pre-existing clone groups, none in this phase
```

`make qa` reported coverage 88.9 % against the floor of 70 %. The two clone
groups are the pre-existing ones (a conflict scenario against a pull unit
test, and two merge rows), both outside the code of this phase.

## Review findings

### Review 2 — 2026-10-04 — status `Complete` (one minor finding, doc only)

**R2-02 — minor — README failure surface F14, F15 and F16.** The rows
paraphrase the resolution code and drifted from it. F14 says a token beside a
record extends the `spaceMismatch` wording; the token branch of `resolveStorage`
returns before any record is read (`internal/app/storage.go`), so no
`spaceMismatch` is involved and the row's token column is wrong. F15 and F16 say
the entry is "neither used nor rewritten"; `applyRememberedPrefix`
(`internal/app/config.go`) applies the record's prefix whenever the resolved
space equals the remembered one, whatever named that space, so an environment
space or a flag that matches the record still uses its prefix — required by F20
and by this phase's acceptance criteria — and `--endpoint` names no space, so
the record still selects the space under it. The entry is never rewritten, which
those rows keep.

- [x] State the rule once in `runtime.recordsSpace` and `applyRememberedPrefix`,
      and make F14, F15 and F16 quote it rather than paraphrase it. Closed by
      Phase 6.

### Verified good (review 2)

`bucketFromRemembered` is the only remembered source; `s3Signals` excludes it,
so a remembered space beside the shared AWS files reaches the hosted store and
never the ambiguity refusal. The lookup runs before the `ErrNoLogin` + `auto`
branch, so a remembered space with no credential refuses and never falls back to
S3. `applyRememberedPrefix` runs only when the resolved space equals the
remembered one, and a remembered record with no prefix leaves the default
standing. `serve` sets no association, so it reads nothing; a malformed,
exposed, oversized or other-version file refuses startup naming the file. The
scenario table proves F14 to F20 at the command line against the reference
gateway.