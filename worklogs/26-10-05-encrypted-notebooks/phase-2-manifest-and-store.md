# Phase Two: manifest and encrypted store

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Add manifest version two and one storage decorator that encrypts every object body without changing adapter interfaces.

## Specification

Extend `internal/storage` so `current` uses the strict version-two wrapper
below and retains the existing strict version-one manifest as its decrypted
payload. Make older readers return `UPGRADE_REQUIRED` from the wrapper version
before pack access.

Implement the `Encrypted` decorator in `internal/storage`. It encrypts all
object-write methods before they call the raw store, decrypts reads, and reports
plaintext size and digest to notebook callers. It keeps listing and deletion
transparent. It passes through plaintext version-one notebooks and an absent
`current`. It unlocks encrypted `current` before any pack read or write. It
does not retry a conditional write or add an adapter method.

### Manifest wrapper

Encode encrypted `current` as strict compact JSON with exactly the ordered
fields `version` and `envelope`. Set `version` to 2; encode `envelope` as
standard padded Base64 of the binary `current-v2` envelope. Its decrypted
payload is the unchanged strict version-one manifest. An older reader detects
the wrapper version and refuses before reading packs or writing objects.
Malformed or invalid wrappers return `STORAGE_INTEGRITY` / `MANIFEST_INVALID`.

The `current` envelope size limit is owned by Phase 1. Refuse a manifest below
the workspace's recorded baseline as `STORAGE_INTEGRITY` / `HISTORY_INVALID`;
a cold workspace has no rollback baseline.

### Decorator behavior

Run the raw store's existing startup check before constructing a notebook's
decorator. The decorator lazily reads `current`, resolves and opens a supported
door before returning plaintext or permitting a write. It encrypts
`PutObject`, `CreateObject`, and `ReplaceObject` bodies; it decrypts
`ReadObject` bodies. Stored object metadata describes ciphertext, while the
notebook receives plaintext size and digest. Descriptors continue to bind the
plaintext pack. Listing and deletion pass through unchanged. When `current` is
absent or plaintext version one, reads and writes pass through unchanged; only
accepted `encrypt init` changes the notebook to encrypted mode.

Replace upload metadata with the values for the stored ciphertext so existing
protocol headers describe the stored body. Do not re-key or rewrite a pack
descriptor. Upload encrypted pack bytes before publishing the manifest.

Files owned by this phase:

```text
internal/storage/manifest.go
internal/storage/encrypted.go
internal/storage/*encrypted*_test.go
internal/integrationtest/scenario_encrypted_store_test.go
```

| Invariant | Mechanism | Proof |
| --- | --- | --- |
| Pack bytes exist before any manifest can reference them | Encrypt at the decorator boundary; keep notebook upload order | `TestEncryptedStoreContract` |
| Conditional-write loss remains normal contention | Return the raw store result unchanged | `TestEncryptedConditionalWrite` |
| Locked notebooks do not read packs or write objects | Unlock `current` before access | `TestEncryptedStoreRefusesLockedNotebook` |
| Raw metadata describes stored ciphertext and descriptors bind plaintext packs | Translate metadata at the decorator boundary | `TestEncryptedStoreContract` |

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| Decorated object-store contract against the fake | Fake store and test keyring | Every method preserves the semantic `ObjectStore` contract | Store only ciphertext for encrypted state | New adapter methods or plaintext object bodies |
| Decorated object-store contract against S3 | Real S3-compatible test store and test keyring | Opaque ciphertext round-trips within existing object semantics | Existing S3 behavior remains | Adapter method changes |
| Decorated object-store contract against the hosted adapter | Gateway reference server and test keyring | Opaque ciphertext round-trips within existing body limits | Existing status and size behavior remains | Gateway route or storage adapter changes |
| Read an encrypted manifest with an older format reader | Strict manifest decoder | `UPGRADE_REQUIRED` before pack access | None | Object writes or pack reads |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| The version-two wrapper is strict and the decrypted version-one manifest remains unchanged | `TestEncryptedManifestCodec` |
| A manifest below the recorded workspace baseline is rejected as rollback | `TestEncryptedManifestRollback` |
| Every object-store method round-trips through encryption and decryption | `TestEncryptedStoreContract` |
| The shared store contract passes through the decorator for fake, S3, and hosted stores | `TestEncryptedObjectStoreContracts` |
| A conditional-write loss is returned without retry | `TestEncryptedConditionalWrite` |
| An unopened notebook refuses pack access and object writes | `TestEncryptedStoreRefusesLockedNotebook` |
| Ciphertext that exceeds the existing hosted limit does not publish `current` | `TestEncryptedPackLimitPreservesCurrent` |
| Encrypted pull and commit preserve the notebook operation contract and ciphertext at rest | `TestEncryptedPullCommit` and `TestEncryptedCommitContainsCiphertext` |
| Pack upload still precedes manifest publication | `TestEncryptedCommitUploadBeforeManifest` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| Unknown newer manifest version | `STORAGE_FAILURE` / `UPGRADE_REQUIRED` without pack access | `TestEncryptedManifestCodec` |
| Malformed wrapper or invalid encoded envelope | `STORAGE_INTEGRITY` / `MANIFEST_INVALID` | `TestEncryptedManifestCodec` |
| Manifest generation is below the workspace baseline | `STORAGE_INTEGRITY` / `HISTORY_INVALID`; preserve local baseline | `TestEncryptedManifestRollback` |
| Missing or invalid key material | Refusal before pack access or writes | `TestEncryptedStoreRefusesLockedNotebook` |
| Envelope authentication failure | `STORAGE_INTEGRITY` / `ENVELOPE_INVALID` | `TestEncryptedStoreRejectsTampering` |
| Ciphertext exceeds the hosted object limit | Existing too-large outcome and unchanged accepted state | `TestEncryptedPackLimitPreservesCurrent` |
| Conditional replacement loses a race | Existing contention result, with no decorator retry | `TestEncryptedConditionalWrite` |

## Implementation notes

Not started.

## Review findings

None.