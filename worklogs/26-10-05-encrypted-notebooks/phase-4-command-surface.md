# Phase Four: command surface

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Add the `encrypt` commands, key prompts, reporting, and the documented key-record options.

## Specification

Add `encrypt init`, `status`, `passwd`, `recovery`, and `rekey` through the
existing CLI router. Keep MCP tools and existing command parameters unchanged.
Add only the options listed below. The command uses one injected KDF gate and
the shared keyring; it does not duplicate cryptographic operations.

`init` prints the recovery code only after accepted publication. `status` never
writes. `passwd` and `recovery` replace only the encrypted `current` header.
`rekey` prompts whether to keep the unlocking secret or enter a new passphrase.
Either choice makes a new notebook key, replaces the door set with one
passphrase door and one fresh recovery door, and publishes a new root
checkpoint. No earlier publication history remains in the active manifest.
Warn that environment values and envfiles must be updated by their owner.
If the chosen secret conflicts with the existing record, require an explicit
store or forget option before publication. Apply the requested record change
only after remote acceptance. If that local update fails, report the accepted
rekey and recovery code with a warning.
Before publishing the rekey checkpoint, reject unresolved conflict markers and
apply the configured read-only and writable path policy.

### Commands and outcomes

An omitted path means the current working directory.

| Command | Purpose and result |
| --- | --- |
| `slivingdoc encrypt init [path]` | Establish encryption and publish a checkpoint; show the recovery code once after acceptance. Refuse if already encrypted or if storage rejects publication. |
| `slivingdoc encrypt status [path]` | Report mode, doors, KDF settings, and key-material source. Never write. |
| `slivingdoc encrypt passwd [path]` | Add, change, or remove a passphrase door; do not rewrite pack objects. |
| `slivingdoc encrypt recovery [path]` | Replace the recovery door and show the new code once. |
| `slivingdoc encrypt rekey [path]` | Replace the notebook key and retained history; show a new recovery code after acceptance. |

### Options and command parameters

| Parameter | Rule |
| --- | --- |
| Maximum candidate sources | Four: prompted secret, environment value, its envfile value, and key-record value. Try each against each door. |
| Maximum doors | Four per notebook. A door-add request at the cap is refused. |
| Recovery code | 128 random entropy bits encoded as twelve canonical lowercase English BIP-39 words, single-space separated, with checksum. Do not use BIP-39 seed derivation. |
| `--store-passphrase` | Available to `init`, `passwd`, and `rekey`; stores the plaintext secret only when used with `--confirm-store-passphrase`. |
| `--confirm-store-passphrase` | Required with `--store-passphrase`; confirms the plaintext-at-rest warning. |
| `--store-passphrase-file <path>` | Available to `init`, `passwd`, and `rekey`; stores only the envfile path. Mutually exclusive with `--store-passphrase`. |
| `--forget-key-material` | Available to `passwd` and `rekey`; removes this notebook identity's record. Mutually exclusive with store options. |

`SLIVINGDOC_PASSPHRASE` and `SLIVINGDOC_PASSPHRASE_FILE` are resolved by
Phase 3. Do not add a flag that supplies a secret. Never log or echo secret
values or envfile paths.

### Stable command errors

The encryption commands add reason tokens to existing error codes, not new
top-level codes. Each failure carries the listed action and retryable value.

| Condition | Code / reason | Action | Retryable | Caller guidance |
| --- | --- | --- | --- | --- |
| No candidate is available | `STORAGE_FAILURE` / `KEY_REQUIRED` | `OPERATOR` | No | Configure a passphrase or recovery code; never ask for or echo a secret. |
| No candidate opens a door | `STORAGE_FAILURE` / `KEY_REJECTED` | `OPERATOR` | No | Supply a passphrase or recovery code that opens this notebook. |
| Envelope authentication, framing, structure or KDF-window validation fails | `STORAGE_INTEGRITY` / `ENVELOPE_INVALID` | `OPERATOR` | No | Treat the stored notebook as corrupt or modified. |
| Stored format, object kind, KDF or immutable core is unsupported | `STORAGE_FAILURE` / `UPGRADE_REQUIRED` | `OPERATOR` | No | Update slivingdoc. |
| Key record or envfile is missing, malformed, exposed or unsafe; rekey credential conflicts with the record | `STORAGE_FAILURE` / `KEY_MATERIAL_INVALID` | `OPERATOR` | No | Correct key material without sharing its value; use explicit store or forget options for rekey. |
| Door cap is exceeded | `INVALID_REQUEST` / `DOOR_LIMIT` | `FIX_INPUT` | No | Remove a door before adding another. |
| `init` targets an encrypted notebook | `INVALID_REQUEST` / `ENCRYPTION_ALREADY_ENABLED` | `FIX_INPUT` | No | Use the existing encrypted notebook or choose a plaintext notebook. |
| Non-empty remote has no workspace baseline | `INVALID_REQUEST` / `PULL_REQUIRED` | `PULL` | No | Pull accepted state, then retry conversion. |
| Manifest is older than the workspace baseline | `STORAGE_INTEGRITY` / `HISTORY_INVALID` | `OPERATOR` | No | Treat as rollback; do not replace the local baseline. |
| Store account is full | `STORAGE_FAILURE` / `STORAGE_FULL` | `OPERATOR` | No | Free space or use a store with more capacity. |
| Ciphertext exceeds the store's object limit | `STORAGE_FAILURE` / `OBJECT_TOO_LARGE` | `OPERATOR` | No | Reduce the notebook or use a store with a higher object limit; no manifest is published. |

Files owned by this phase:

```text
cmd/encrypt/encrypt.go
internal/app/encryption.go
internal/cli/cli.go
internal/integrationtest/scenario_encrypt_command_test.go
```

| Invariant | Mechanism | Proof |
| --- | --- | --- |
| Commands never expose secret material in logs or errors | Redacted typed errors and output capture | `TestEncryptCommandDoesNotLeakSecrets` |
| Door changes do not rewrite packs | Header-only replacement | `TestEncryptPasswdChangesHeaderOnly` |
| `status` never mutates accepted state | Read-only command path | `TestEncryptStatusIsReadOnly` |
| Key-record changes are explicit and identity-scoped | Flag validation and key-record API | `TestEncryptKeyRecordOptions` |
| Rekey rejects unresolved markers and observes the configured path policy | Validate the rekey snapshot before publication | `TestEncryptRekeyPolicy` |

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| Black-box `encrypt status` | CLI process and encrypted reference store | Mode, doors, KDF settings, and source report | Reads only | Store mutation or secret output |
| Black-box `encrypt passwd` or `recovery` | Scripted prompt, key record, and reference store | Updated header and one-time recovery output where applicable | Replace `current` conditionally | Pack upload or plaintext secret logging |
| Black-box `encrypt rekey` | Scripted prompt and reference store | New key and root checkpoint become accepted | Best-effort cleanup after acceptance | Earlier history retained in the active manifest |
| Black-box rekey with key-record options | Key record and reference store | Record update or removal follows accepted publication | Update only after acceptance | Secret stored without opt-in, or a record changed before a refused publication |
| Unlock with prompted, environment, envfile, and key-record candidates | Scripted prompt, injected environment, key record, and encrypted store | The first successful candidate source is reported without exposing its value or path | Try candidates in the specified order | Secret or envfile-path output |
| Black-box invalid option combination | CLI router | Configuration refusal before store mutation | None | Partial key-record or remote write |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| Every documented subcommand is routed and reports its documented result | `TestEncryptCommandSurface` |
| CLI composes the allowed candidate sources in the documented order | `TestEncryptCandidateSources` |
| Rekey offers both credential choices, replaces the old door set, and always mints a new recovery code | `TestEncryptRekeyCredentialChoice` |
| Door changes leave pack count and keys unchanged | `TestEncryptPasswdChangesHeaderOnly` |
| `status` leaves local and remote state unchanged | `TestEncryptStatusIsReadOnly` |
| Store, confirm, envfile, and forget options obey this phase's rules | `TestEncryptKeyRecordOptions` |
| Errors use existing codes with the documented stable reason and action | `TestEncryptErrorEnvelope` |
| No command output or captured log contains a secret or envfile path | `TestEncryptCommandDoesNotLeakSecrets` |
| Rekey refuses unresolved markers and protected-path changes under the configured policy | `TestEncryptRekeyPolicy` |
| A key-record update failure does not hide the accepted rekey or recovery code | `TestEncryptRekeyRecordFailureAfterAcceptance` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| Missing or incorrect passphrase | Refusal with the documented next step | `TestEncryptUnlockRefusals` |
| More than the supported door count | Refusal with no remote mutation | `TestEncryptDoorLimit` |
| Store-passphrase without confirmation | Invalid request before key-record or store mutation | `TestEncryptKeyRecordOptions` |
| Mutually exclusive key-record options | Invalid request before mutation | `TestEncryptKeyRecordOptions` |
| A rekey choice is not supplied or the replacement passphrase is invalid | Refusal before publishing a new key | `TestEncryptRekeyCredentialChoice` |
| A new passphrase is requested without warning confirmation, or an old key record conflicts with the selected credential | Refusal before publishing a new key | `TestEncryptRekeyKeyRecordOptions` |
| Conditional header replacement loses a race | Existing retry or busy result, without pack rewrite | `TestEncryptPasswdContention` |
| Stored key record is unsafe or malformed | Typed local-file refusal without remote mutation | `TestEncryptUnsafeKeyRecord` |

## Implementation notes

Not started.

## Review findings

None.