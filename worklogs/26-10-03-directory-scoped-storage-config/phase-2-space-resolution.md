# Phase 2: space resolution and precedence

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Make the remembered space select the hosted store for a CLI operation, below
the flags and the environment and above the account default, and refuse every
way it can fail.

## Specification

### Where the association enters

The read enters `internal/app`'s configuration, where the flags and the
environment are already resolved, and the process options carry the pieces the
resolution needs. `ProcessOptions` gains two fields, injected like the ones
beside them:

```text
ProcessOptions.Lookup func(path string) (settings.Target, bool)
ProcessOptions.Load   func() (settings.Set, error)
```

When `Lookup` is nil the process consults no association at all, which is how
`serve` and every in-process test keep today's behavior. The production `Load`
reads the file once per process through `settings.Locate`; the lookup then runs
in memory.

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
| A remembered space selects hosted mode | the lookup precedes the `ErrNoLogin` + `auto` S3 fallback and the remembered source is host-only for `s3Signals` | `TestRememberedSpaceSelectsHostedBesideAWSSettings`, `TestRememberedSpaceWithoutCredentialRefuses` |
| `serve` never consults the association | `Lookup` is nil unless a path-taking command set it | `TestServeNeverLooksUpTheAssociation` |
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

| Criterion | Test |
| --- | --- |
| A remembered space beats the stored default space and loses to a flag and to `SLIVINGDOC_SPACE` | `TestResolveSpacePrecedenceTable` |
| The resolved source is `remembered space` for the remembered case and today's wording for the others | `TestSpaceSourceWording` |
| A remembered space reaches the hosted store beside AWS signals | `TestRememberedSpaceSelectsHostedBesideAWSSettings` |
| A remembered space with no credential refuses and names the space and `slivingdoc login` | `TestRememberedSpaceWithoutCredentialRefuses` |
| A remembered space with a login for another endpoint refuses naming both | `TestRememberedSpaceLoginForOtherEndpointRefuses` |
| A remembered space with several logins refuses asking for `--endpoint` | `TestRememberedSpaceAmbiguousLoginRefuses` |
| An expired login beside a remembered space refuses telling the user to log in | `TestRememberedSpaceExpiredLoginRefuses` |
| A remembered space fails `ValidateSpace` and refuses naming it | `TestRememberedSpaceInvalidRefuses` |
| `SLIVINGDOC_TOKEN` ignores the entry and the entry is byte-identical after | `TestTokenIgnoresRememberedSpace` |
| `--storage s3` ignores the entry and the entry is byte-identical after | `TestS3ModeIgnoresRememberedSpace` |
| `--storage hosted` with no space takes the remembered one | `TestHostedModeTakesRememberedSpace` |
| A remembered prefix differs from `--prefix` and refuses naming both | `TestRememberedPrefixMismatchRefuses` |
| A remembered prefix supplies the prefix when none is named | `TestRememberedPrefixAppliesWhenNoneNamed` |
| `serve` performs no lookup | `TestServeNeverLooksUpTheAssociation` |
| A malformed, exposed, oversized or other-version file refuses startup naming the file | `TestSetupRefusesUnusableSettingsFile` |
| `Runtime.Space` and `Runtime.SpaceSource` report the resolved pair | `TestRuntimeSpaceAccessors` |

## Error coverage

Every row is the README's failure surface; the mapping is one to one.

| Row | Failure | Expected outcome | Test |
| --- | --- | --- | --- |
| F3 | malformed file | refuse naming the file | `TestSetupRefusesUnusableSettingsFile` |
| F4 | other version | refuse naming the file and the two fixes | `TestSetupRefusesUnusableSettingsFile` |
| F5 | exposed file or directory | refuse wording the `chmod` | `TestSetupRefusesUnusableSettingsFile` |
| F6 | a link or a non-regular file | refuse | `TestSetupRefusesUnusableSettingsFile` |
| F7 | invalid space | refuse naming the space | `TestRememberedSpaceInvalidRefuses` |
| F9 | no credential | refuse naming the space and `slivingdoc login` | `TestRememberedSpaceWithoutCredentialRefuses` |
| F10 | a login for another endpoint | refuse naming both endpoints | `TestRememberedSpaceLoginForOtherEndpointRefuses` |
| F11 | several matching logins | refuse asking for `--endpoint` | `TestRememberedSpaceAmbiguousLoginRefuses` |
| F12 | an expired login | refuse telling the user to log in | `TestRememberedSpaceExpiredLoginRefuses` |
| F13 | a space the account cannot reach | refuse naming it as unreachable | `TestRememberedSpaceNotInTheAccountRefuses` |
| F14 | a token beside an entry | the token's space, the entry untouched | `TestTokenIgnoresRememberedSpace` |
| F15 | `SLIVINGDOC_SPACE` or `SLIVINGDOC_BUCKET` | the environment's space, the entry untouched | `TestResolveSpacePrecedenceTable` |
| F16 | `--space`, `--bucket`, `--endpoint` | the flag's space, the entry untouched | `TestResolveSpacePrecedenceTable` |
| F17 | `--storage s3` | S3 mode, the entry untouched | `TestS3ModeIgnoresRememberedSpace` |
| F18 | `--storage hosted` with no space | the remembered space | `TestHostedModeTakesRememberedSpace` |
| F19 | ambient S3 signals | the remembered space | `TestRememberedSpaceSelectsHostedBesideAWSSettings` |
| F20 | a prefix mismatch | refuse naming both prefixes | `TestRememberedPrefixMismatchRefuses` |

## Implementation notes

Not started.

## Review findings

None.