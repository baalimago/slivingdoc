# Phase Three: key material

**Status:** Complete

README: [README.md](./README.md)

## Goal

Resolve passphrase candidates and store explicitly requested key material in a hardened identity-keyed record.

## Specification

Add `internal/keyrecord` for the strict `notebooks.json` format and candidate
resolver specified here. Use `credentials.PrivateFile` for file safety,
locking, bounded reads, and atomic writes. Keep this record separate from
`workspaces.json`. Key entries use the notebook identity digest and contain
either a passphrase or an expanded envfile path, never both.

Resolve candidates from the environment value, its envfile, and the record in
the order below. The application may prepend the interactive candidate. Do not
log, return, or include secret values or paths in errors. Do not create a
passphrase for the user.

### Record schema and resolution

`notebooks.json` is a strict version-one file in the configuration directory,
beside `workspaces.json`. Its top-level fields are exactly `version` and
`notebooks`. `notebooks` is keyed by the lowercase identity digest produced by
`workspace.Identity` and `SharedCacheDirName`. Each non-empty entry has exactly
one field: `passphrase` (plaintext at rest) or `passphraseFile` (the expanded
envfile path). The key record contains no notebook key or wrapped key.

Encode entries in digest order. Reject duplicate keys, unknown fields, explicit
`null`, malformed entries, unsafe files, and a record with both secret and
path. Omit empty records. `workspaces.json` remains unchanged and contains no
credential field.

Candidate settings are `SLIVINGDOC_PASSPHRASE` and
`SLIVINGDOC_PASSPHRASE_FILE`. Resolve the environment value, the value read from
its envfile, then the matching key-record candidate. When supplied, the
interactive secret is tried first by the application. Try every candidate
against every supported door; stop at the first successful unlock. The complete
candidate set is bounded by Phase 4's candidate-source limit.

The resolver returns secrets with their source labels, never their source
paths. The application prepends an interactive candidate when the command
collects one, then builds the Phase 1 keyring.

The envfile accepts `KEY=VALUE` lines. Ignore blank lines and `#` comments;
accept an optional `export ` prefix, matching quotes, and `~` expansion. Use
the private-file owner, symlink and permission checks provided by
`credentials.PrivateFile`.

The Phase 4 command layer calls record update or removal only after explicit
caller intent and, for rekey, after remote acceptance. A local record-update
failure must not hide the accepted rekey or recovery code.

Files owned by this phase:

```text
internal/keyrecord/codec.go
internal/keyrecord/file.go
internal/keyrecord/resolve.go
internal/keyrecord/*_test.go
```

| Invariant | Mechanism | Proof |
| --- | --- | --- |
| Workspace records never contain credentials | Separate file and strict target codec | `TestWorkspaceSettingsRejectCredentialFields` |
| Key material is identity-scoped and explicitly stored | Digest key and opt-in update methods | `TestKeyRecordIdentityIsolation` |
| Unsafe files never supply candidates | `credentials.PrivateFile` checks | `TestKeyRecordRejectsUnsafeFile` |

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| Unit-test-only key-record operations | Injected environment, identity, and private-file location | Strict record and ordered candidate values | Atomic write only when requested | Reading the developer's real configuration |
| Resolve an envfile candidate | Injected environment and private-file checks | Candidate source and secret bytes, without path disclosure | Read-only envfile access | Logging or echoing the secret or path |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| Valid key records round-trip with deterministic identity order | `TestKeyRecordRoundTrip` |
| Unknown, duplicate, missing, null, or conflicting fields are rejected | `TestKeyRecordStrictCodec` |
| Resolver returns candidates in this phase's order and isolates identities | `TestKeyRecordCandidateOrder` and `TestKeyRecordIdentityIsolation` |
| Envfile parsing accepts only the documented grammar and hardening rules | `TestPassphraseEnvfile` |
| Saving requires an explicit request and uses the private-file contract | `TestKeyRecordPrivateFile` |
| `workspaces.json` rejects credential-shaped fields | `TestWorkspaceSettingsRejectCredentialFields` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| Malformed, unknown-version, or oversized record | Typed refusal with no rewrite | `TestKeyRecordStrictCodec` |
| Symlink, wrong owner, or exposed file | Credentials private-file error | `TestKeyRecordRejectsUnsafeFile` |
| Missing, unreadable, or invalid envfile | Refusal that names the setting but not its value | `TestPassphraseEnvfile` |
| Record contains both secret and path | Malformed-record refusal | `TestKeyRecordStrictCodec` |
| Save fails during replacement | Error returned and prior record preserved | `TestKeyRecordPrivateFile` |

## Implementation notes

Implemented in `internal/keyrecord`, with one small addition to
`internal/workspace`.

- `codec.go` owns the strict `notebooks.json` schema: `FileName`,
  `FormatVersion`, an `Entry` variant (`KindPassphrase` or
  `KindPassphraseFile`) built by `Passphrase`/`PassphraseFile`, `Set`
  (`Lookup`, `Put`, `Remove`, `Entries`) and `encode`/`decode`. Entries encode
  in digest order; an unknown, duplicate, missing, null or conflicting field,
  a non-canonical digest, and an empty value are refused, and no refusal
  echoes a stored secret.
- `file.go` owns `File`: `Locate` (the credentials file's own rule), `Load`,
  `Save`, `CheckDir`, `Lock`, and the locked `Update`/`Forget`. Every read and
  write goes through `credentials.PrivateFile`, so a symbolic link, a
  non-regular file, another user's file, or an exposed file or directory is a
  typed `ErrExposed`, and the file is written atomically 0600 in a 0700
  directory.
- `resolve.go` owns `Resolve(id, getenv)`: it returns `crypt.Candidate` values
  in the order environment value, envfile value, key-record value, each with
  its `crypt.CandidateSource`. The envfile grammar (a `SLIVINGDOC_PASSPHRASE`
  assignment, blank lines and full-line `#` comments, an optional `export `
  prefix, matching quotes, `~` expansion) is parsed here; the file is read
  through `credentials.PrivateFile`, and any failure is re-worded to name the
  setting (`SLIVINGDOC_PASSPHRASE_FILE` or `passphraseFile`) and never the
  path.
- `internal/workspace/identity.go` gains `IdentityDigest`, the full lowercase
  SHA-256 digest of a storage identity; `SharedCacheDirName` now truncates it.
  The key record is keyed by that digest (`workspace.IdentityDigest(id)`).

`workspaces.json` is untouched, and the record is a separate file in the same
configuration directory. Decisions are D12–D14 of the
[README](README.md#decisions-log).

## Review findings

None.

## Commands and results

```text
make qa
== gofumpt == / == go vet == / == staticcheck == / == go fix ==  (clean)
== coverage: 87.6% (floor 70%) ==
npm test: tests 35, pass 35, fail 0

go test -race -count=3 -timeout=30s ./internal/keyrecord/ ./internal/workspace/
ok  github.com/baalimago/slivingdoc/internal/keyrecord  1.081s
ok  github.com/baalimago/slivingdoc/internal/workspace  3.471s

go test -cover ./internal/keyrecord/
ok  github.com/baalimago/slivingdoc/internal/keyrecord  0.014s  coverage: 91.5% of statements

go run github.com/mibk/dupl@v1.0.0 -t 80 .
Found total 5 clone groups: three pre-existing, the versioned-record error
boilerplate shared with internal/settings, and the Load shape of a strict
versioned configuration file shared with internal/settings. Both are the
established per-package configuration-file idiom, whose wording and version
sentinels each package owns; extracting them would touch internal/credentials
and internal/settings and is out of this phase's scope.
```
