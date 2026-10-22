# Notebook encryption

Client-side encryption of a notebook's stored objects. A converted notebook
keeps its visible directory, private state and pack cache in plaintext and
stores only ciphertext under the object-store prefix; a store operator or
anyone who copies active objects can see keys, sizes, request timing,
publication count and public envelope headers, and nothing else. This doc
answers "what is encrypted, how is a stored body framed, how does a process
unlock a notebook, and which command manages the key material?"

Read this when: changing the envelope format, a KDF parameter, a door type,
the key material record, the `encrypt` commands, the version-two `current`
wrapper, the `Encrypted` object-store decorator, or the encryption error and
compatibility contract.

## Key files

| File | Purpose |
|------|---------|
| `internal/crypt/envelope.go` | The `current-v2`/pack envelope: `ObjectKind` (`KindCurrent`, `KindPack`), `Extension`, `Header`, `DefaultChunkSize`, `MaxHeaderLength`, `CurrentEnvelopeLimit`, `WrappedCurrentSize`; the errors `ErrMalformedEnvelope`, `ErrUnsupportedFormat`, `ErrAuthentication`, `ErrEnvelopeTooLarge`; `decodeEnvelope`, `encodeHeader`, `sealPayload`, `openPayload` |
| `internal/crypt/header.go` | `DoorType` (`DoorPassphrase`, `DoorRecovery`), `Door`, the HKDF domain separation (`hkdfDomain`, `headerDomain`, `wrapDomain`), `WrapKey`, `UnwrapKey`, `headerCommitment`, `encodeObjectKey` |
| `internal/crypt/kdf.go` | `KDFParams`, `DefaultKDFParams`, `KDFParams.Validate`, `KDFGate`, `NewKDFGate`, `NewKDFGateForCost`, `DeriveKEK`; the accepted Argon2id window (`minMemoryKiB` … `maxParallelism`); `ErrKDFParams`, `ErrSecretBounds`, `ErrKDFGate` |
| `internal/crypt/keyring.go` | `NotebookKey`, `GenerateNotebookKey`, `CandidateSource` (`SourcePrompt`, `SourceEnvironment`, `SourceEnvironmentFile`, `SourceKeyRecord`), `Candidate`, `Keyring`, `NewKeyring`, `NewUnlockedKeyring`, `Open`, `Seal`, `Rewrap`, `WrapDoor`, `AddCandidate`, `ResolvedSource`, `ResolvedSecret`, `Unlocked`, `HasCandidates`; `ErrKeyRejected`, `ErrKeyringLocked`, `ErrCandidateLimit` |
| `internal/storage/manifest.go` | `ManifestWrapperVersion` (2), `CurrentEncoding` (`CurrentPlaintext`, `CurrentEncrypted`), `currentWrapper`, `WrapCurrentEnvelope`, `ClassifyCurrent`, `CheckManifestRollback` |
| `internal/storage/encrypted.go` | `Encrypted`, `encryptedStore`, `storeMode` (`modeUnresolved`, `modePlaintext`, `modeEncrypted`), `readCurrent`, `writeCurrent`, `ensureMode`, `ciphertextMeta`, `envelopeError`, `ErrKeyRequired` |
| `internal/keyrecord/codec.go` | `FileName` (`notebooks.json`), `FormatVersion` (1), `PassphraseEnv` (`SLIVINGDOC_PASSPHRASE`), `PassphraseFileEnv` (`SLIVINGDOC_PASSPHRASE_FILE`), `Kind` (`KindPassphrase`, `KindPassphraseFile`), `Entry`, `Passphrase`, `PassphraseFile`, `Set`; `ErrNoMaterial`, `ErrUnsupportedVersion` |
| `internal/keyrecord/file.go` | `Locate`, `File.Path`, `Load`, `Save`, `Update`, `Forget`, `LockName` (`notebooks.lock`), the hardened `PrivateFile` handling |
| `internal/keyrecord/resolve.go` | `Resolve` (candidate order), `readPassphraseFile`, `envfilePassphrase` (the envfile grammar), `ErrEnvfile`, `ErrExposed`, `ErrTooLarge` |
| `internal/recovery/code.go` | `Code`, `Generate`, `Parse`, `Code.Secret`, the BIP-39 wordlist (`internal/recovery/wordlist.go`) |
| `internal/notebook/encryption.go` | `Cipher`, `EncryptionStatus`, `EncryptStatus`, `EncryptInit`, `EncryptRekey`, `EncryptDoors`, `publishRoot`, `rootRemote`, `buildRootProposal`, `uploadEncrypted`, `publishEncrypted`, `cleanupCurrent`, `keyMaterialError` |
| `internal/notebook/remote.go` | The decorator's failures become the taxonomy: a wrapped `current` reaches `manifestError`, and a pack that fails envelope validation is `ENVELOPE_INVALID` |
| `internal/app/encryption.go` | `EncryptSubcommand`, `KeyRecordOptions.Validate`, `EncryptCommand`, `EncryptReport`, `EncryptionReport`, `SecretPrompt`, `Runtime.AskForSecret`, `Runtime.Encrypt` and its per-command helpers, `candidatesOf`, `keyring`, `DoorNames`, `SourceName`, the process-wide gate |
| `internal/app/app.go`, `internal/app/service.go` | The process keyring: `buildService` resolves the candidates after the store check, `NewService` holds the keyring, `Service.notebookStore` decorates every opened notebook, `Service.AskForSecret` and `Service.storedCurrent` answer the terminal ask, and `Service.rawNotebookFor` serves the `encrypt` commands the undecorated store |
| `cmd/encrypt/encrypt.go` | The `encrypt` command: `Setup` (subcommand, path, record flags, `app.Setup`), `Run` (report, recovery code, warnings) |
| `internal/cli/cli.go`, `internal/cli/home.go` | The command map entry and its home-screen group |
| `internal/integrationtest/sync/encrypted_integrationtest_test.go`, `internal/integrationtest/sync/upgrade_integrationtest_test.go`, `internal/integrationtest/cli/encrypt_integrationtest_test.go`, `internal/integrationtest/cli/encrypt_tty_linux_integrationtest_test.go`, `internal/storage/encrypted_store_test.go` | The encrypted pull/commit round trip through the process keyring, ciphertext at rest, upload-before-manifest, rollback, the key-material refusals, the plaintext passthrough, the decorated store contract on every backend, and the conversion and terminal-ask scenarios |

## Flow

```text
caller (CLI/MCP) → notebook operation → ObjectStore
  plaintext notebook  → storage.Encrypted passes bodies through unchanged
  encrypted, locked   → no candidate opens a door → refuse before pack access
  encrypted, unlocked → Open decrypts reads, Seal encrypts writes

unlock:
  keyrecord.Resolve(identity, getenv) → [environment, envfile, key record]
    prepend the prompted secret when the command collects one
    pull and commit ask the terminal when none of the three exists
  crypt.NewKeyring(candidates, gate) → first candidate that opens a door
    → notebook key in process memory; ResolvedSource names the source

convert (encrypt init):
  mint notebook key + recovery code → wrap a passphrase door and a recovery door
  → publish the merged visible/accepted state as one encrypted root checkpoint
  → accept locally → change the key record only after acceptance

process (serve | pull | commit | status | log):
  buildService → keyrecord.Resolve(identity, env) → crypt.NewKeyring
  → Service.notebookStore() → storage.Encrypted(raw store, keyring)

wrapped read:
  ReadObject("current") → ClassifyCurrent
    version == 1 or not the wrapper → plaintext manifest passes through
    version == ManifestWrapperVersion → Keyring.Open → version-one manifest
    version  > ManifestWrapperVersion → *UpgradeRequiredError (before any pack)
```

## Behavior

**Trust boundary.** Encryption is client-side and covers the bodies of the
stored objects under one prefix: the `current` manifest and every pack. It
does not cover the visible directory, the private state (repository,
`state.json`, locks), the shared pack cache, or the key record. The store,
its operator, and anyone who copies active objects still observe the object
keys (which name the prefix, kind and generation), the object sizes (which
grow by framing), the request timing, the publication count, and the public
envelope headers (magic, format version, object kind, and the header length).
There is no padding, no cover traffic, and no command that returns a notebook
to plaintext: a plaintext copy is a separate notebook. Old object versions,
backups and external copies are not erased by conversion or rekey. There is
no key exchange, keychain, unlock cache, service-side key recovery or
recipient wrapping; a person supplies passphrases and shares secrets out of
band, and the tool shows a generated recovery code once.

**Envelope format.** One binary envelope per stored body, format version 1:

```text
preamble  magic[8] "SLIVENC\0" | formatVersion[2] | objectKind[1] | headerLength[4]
header    core[28] | doorSection | extensionSection | commitment[32]
core      chunkSize[4] | plaintextSize[8] | noncePrefix[16]
door      doorType[1] | kdfID[1] | bodyLength[2] | body[bodyLength]
door body salt[16] | memoryKiB[4] | iterations[4] | parallelism[1]
          | wrapNonce[24] | wrappedNotebookKey[48]
extension extensionType[2] | valueLength[2] | value[valueLength]
payload   payloadRecord*  (one XChaCha20-Poly1305 record per 64 KiB chunk,
                           then one empty final record)
```

`headerLength` covers the core through the commitment, not the preamble or
the payload. The 28-byte core is immutable and the payload is chunked at
`DefaultChunkSize` (64 KiB) with one 16-byte tag per chunk plus a final empty
record, so a stored body is larger than its plaintext. A header above
`MaxHeaderLength` (1 MiB) is refused. The header and payload keys are derived
with HKDF-SHA-256 from the notebook key under separate purposes
(`header-mac`, `payload-aead`), and every derivation binds the exact ASCII
protocol object key, so an envelope sealed for one key never opens as
another. `Commitment` is an HMAC-SHA-256 over the header transcript, so a
tampered or mis-bound header is refused before the payload is opened. A
format version, object kind or KDF this build does not know is
`ErrUnsupportedFormat`, which the decorator maps to `UPGRADE_REQUIRED`; a
structural fault is `ErrMalformedEnvelope` and a failed authentication is
`ErrAuthentication`, both stored-state integrity failures.

**Doors and the KDF.** A door is one wrapped copy of the notebook key: an
Argon2id cost, a random 16-byte salt, a 24-byte XChaCha20-Poly1305 nonce, and
the 32-byte key with its 16-byte tag (48 bytes). `kdfID 1` names Argon2id
version 19, the only KDF this build writes. Door types are `1` (passphrase)
and `2` (recovery); a door of an unknown type is preserved verbatim by
`Rewrap` and skipped by `Open`, so a build that does not know a newer door
type never drops it. A new door is written with `DefaultKDFParams`
(256 MiB, 3 iterations, 4 lanes). The accepted window is
64 MiB … 256 MiB, 1 … 3 iterations and 1 … 4 lanes, so a notebook written by
a build that used a cheaper cost still opens. `KDFGate` bounds how many
Argon2id derivations run at once so one process cannot multiply a door's
memory cost by its concurrency; the application creates one gate per process
(`NewKDFGate(1)`, the one-job rule) and injects it into every keyring.
`NewKDFGateForCost` lowers the accepted floor only to an injected cheaper
cost, which a test uses to stay inside its time budget.

**Keyring.** `NewKeyring(candidates, gate)` holds the candidate secrets and
resolves the notebook key from the first candidate that opens a supported
door; `NewUnlockedKeyring(key, nil, gate)` holds the key and never searches.
`Open` verifies the door and the header commitment before returning
plaintext; `Seal` wraps plaintext with the given doors; `Rewrap` changes only
the authenticated header, not the payload bytes, which is how a passphrase or
recovery change avoids rewriting packs; `WrapDoor` derives a new door from
the resolved key. Candidates hold at most one secret per source and at most
four in all; `SourceUnknown` and empty secrets are refused
(`ErrCandidateLimit`). Every passphrase and recovery secret is valid UTF-8,
non-empty and at most 1 KiB (`ErrSecretBounds`). A candidate set that opens
no door is `ErrKeyRejected`, reported as `KEY_REJECTED`, never as corruption.

**Candidate sources.** `keyrecord.Resolve(id, getenv)` returns the candidates
in the order a keyring must try them: the `SLIVINGDOC_PASSPHRASE` value, the
value of the envfile named by `SLIVINGDOC_PASSPHRASE_FILE`, then the value
the key record holds for this identity. The `encrypt` commands prepend the
interactive secret, which they ask for only when none of the three supplies
one, and `pull` and `commit` add it when none of the three exists at all
(`Service.AskForSecret`). The envfile grammar accepts `KEY=VALUE` lines, ignores blank lines and full-line
`#` comments, strips an optional leading `export `, removes one matching quote
pair, and expands a leading `~`. A passphrase file is read through
`credentials.PrivateFile`, and a refusal names the setting
(`SLIVINGDOC_PASSPHRASE_FILE` or `passphraseFile`) and never the path.

**Key material record.** `notebooks.json` in the configuration directory
(`SLIVINGDOC_CONFIG_DIR` or the user configuration directory) is a strict,
versioned file (`FormatVersion` 1) that maps one lowercase SHA-256 identity
digest to one entry (`workspace.IdentityDigest`). An entry is a variant: a
stored plaintext passphrase (`KindPassphrase`) or the path of an envfile that
holds one (`KindPassphraseFile`). It is read and written through the
hardened `credentials.PrivateFile` (regular file, owner and mode checks,
temporary file and atomic rename), and a change is a locked
read-modify-write under `notebooks.lock` (`File.Update`, `File.Forget`). The
record never names a credential, and a lock file is separate from
`workspaces.json`, so the two configuration locks never wait on each other. A
stored plaintext passphrase is a deliberate convenience, protected only by
the file mode; `--store-passphrase` requires `--confirm-store-passphrase` for
that reason.

**Recovery code.** `recovery.Generate` draws 128 entropy bits and renders
twelve canonical lowercase BIP-39 words with the 4-bit checksum;
`recovery.Parse` proves the checksum before any key work. The recovery
secret is the phrase bytes themselves, never a BIP-39 seed, so no unused
PBKDF2 step is added. The code is shown once, after the publication that
created its door is accepted; it is never stored by slivingdoc and never
printed by a later command.

**The version-two `current` wrapper.** An encrypted `current` is strict
compact JSON with exactly the ordered fields `version` and `envelope`:
`{"version":2,"envelope":"<standard padded Base64>"}`. Its decrypted payload
is the unchanged version-one manifest, so every notebook rule above the store
works on the same manifest. `WrapCurrentEnvelope` refuses an envelope whose
stored wrapper would exceed `CurrentEnvelopeLimit` (1 MiB), which
`WrappedCurrentSize` computes as the wrapper overhead plus the Base64 length.
`ClassifyCurrent` reads the version first: a version above 2 is
`*UpgradeRequiredError` before any other field is read, a malformed
version-two wrapper or invalid Base64 is `ErrIntegrity`, and bytes that are
not the wrapper are `CurrentPlaintext`, so `DecodeManifest` keeps deciding
the version-one manifest. The wrapper is what an older reader refuses before
it reads a pack or writes an object ([compatibility.md](./compatibility.md)).

**The `Encrypted` decorator.** `Encrypted(store, keyring)` makes the notebook
read and write plaintext while the store holds ciphertext. `PutObject`,
`CreateObject` and `ReplaceObject` seal their body; `ReadObject` opens it and
translates the stored metadata back to the plaintext size and digest, so the
notebook's size and SHA-256 checks still describe the plaintext pack.
`ListObjects` and `DeleteObjects` pass through unchanged. A keyring that
already holds the key seals at once; otherwise the first operation reads
`current`, remembers the door set, and every later read and write is
encrypted until a `current` write invalidates the cached mode. A version-one
manifest or an absent `current` leaves the notebook plaintext, and a
`current` body the caller already wrapped (an `encrypt` command's, or a door
rewrite's) is written through unchanged, so no body is double-wrapped.
A conditional-write loss and every other raw store result are returned
unchanged: the decorator never retries. The notebook maps the decorator's
failures onto the existing taxonomy in two places, before any pack access:
`readCurrent` and `ensurePack` (`internal/notebook/remote.go`) turn a
key-material refusal into `KEY_REQUIRED` or `KEY_REJECTED` with
`keyMaterialError` (`internal/notebook/encryption.go`), a manifest below the
workspace baseline is `HISTORY_INVALID`, and a pack that fails envelope
validation is `ENVELOPE_INVALID`.

**The process keyring.** Every notebook operation reaches the store through
the decorator. `app.buildService` resolves the candidate secrets after the
store check (`keyrecord.Resolve` over the environment, its envfile and the
record) and builds one `crypt.Keyring` for the process; `app.NewService`
holds it, and `Service.notebookStore` decorates the store of every notebook
the service opens. A process with no candidate at all gets a locked keyring,
so a plaintext notebook keeps its passthrough and an encrypted one is refused
at its first operation. A machine with no configuration directory contributes
no record candidate and is not a refusal; a record that exists and cannot be
used (malformed, of another version, or not private) refuses startup as
`STORAGE_FAILURE`/`KEY_MATERIAL_INVALID` before any notebook operation, the
same rule the stored logins follow. The two key-material refusals of an
operation are distinct because the caller's next step differs:
`storage.ErrKeyRequired` is the decorator's answer when the process holds no
candidate, and `crypt.ErrKeyRejected` when its candidates opened no door. The
`encrypt` commands are the one exception: they classify, open and rewrite the
stored wrapper themselves and therefore take `Service.rawNotebookFor`, the
undecorated view.

**The terminal ask of `pull` and `commit`.** The one-shot commands run where a
person can answer, so they ask instead of refusing. Before the operation and
before any progress line, `Runtime.AskForSecret` (`cmd/pull`, `cmd/commit`)
asks `Service.AskForSecret` only when all three conditions hold: the process
can ask (`ProcessOptions.terminal` reports `OnTerminal`, so stdin and stderr
are terminals), the keyring holds no candidate at all, and the stored
`current` is a version-two wrapper (`Service.storedCurrent` reads it, with no
key, purely to decide). The answer becomes the `SourcePrompt` candidate of the
same keyring, so the operation that follows opens the notebook with it. The
prompt is masked: the terminal stops echoing before the label is drawn, and
one `*` per character appears instead of the typed secret
([tui.md](./tui.md), The masked read), which is the same read the `encrypt`
commands use for every passphrase question. A wrong answer is `KEY_REJECTED`
rather than a second question, an empty answer, Ctrl-C, or Ctrl-D with nothing
typed is `KEY_REQUIRED`, and the answer is never written to the key record. A
process off a terminal asks nothing, which is how a script and the MCP server
keep the `KEY_REQUIRED` refusal; a store read that fails here decides nothing
and leaves the operation to report the failure in its own taxonomy.

**The `encrypt` command family.** `slivingdoc encrypt
init|status|passwd|recovery|rekey [flags] [path]` is the CLI-only key-material
surface; there is no MCP tool and no storage route or adapter method for it.
It composes the same startup as `serve` (configuration, pinned engine, store
check) and takes at most one notebook path, which defaults to the workspace
root.

- `init` establishes encryption over a plaintext notebook: it mints the
  notebook key and recovery code, wraps one passphrase door and one recovery
  door, and publishes the merged visible and accepted state as one encrypted
  root checkpoint with an empty increment tail. It refuses an already
  encrypted notebook (`ENCRYPTION_ALREADY_ENABLED`) and prints the recovery
  code once, after acceptance.
- `status` reports `encryption: off|enabled`, whether the notebook is locked,
  the door names, the shared KDF settings, the key-material source, and the
  reminder `local: plaintext`. It reads only and never writes; a candidate
  set that opens no door reports the notebook as locked rather than failing.
- `passwd` adds, changes or removes the passphrase door. It rewrites only the
  authenticated `current` header, so no pack object moves and no later
  manifest generation is needed beyond the header rewrite. Removing the last
  door is refused (`DOOR_LIMIT` bounds the other end at four doors).
- `recovery` replaces the recovery door and shows the new code once.
- `rekey` replaces the notebook key: it publishes the visible and accepted
  state as a new root checkpoint under the new key with one passphrase door
  and one fresh recovery door, and the old history becomes unreferenced and
  is cleaned up best-effort. It requires an already encrypted notebook and an
  established workspace baseline.

The passphrase or recovery code comes from the four candidate sources above
or the terminal; no flag carries a secret and no command echoes one. The
key-record flags are `--store-passphrase` (with `--confirm-store-passphrase`),
`--store-passphrase-file <path>`, and `--forget-key-material`; `init` accepts
the first two, `passwd` and `rekey` accept all three, and `status` and
`recovery` accept none. A key-record change lands only after the store
accepts the operation; if that local change fails, the command still reports
its accepted result and prints a warning, and a passphrase the owner set in
the environment or an envfile is flagged as stale. A rekey whose chosen
secret would leave the stored key record pointing at a different secret is
refused until the caller states what to do with that record.

**Compatibility.** A build that reads `current` and finds the version-two
wrapper but does not know it returns `STORAGE_FAILURE`/`UPGRADE_REQUIRED`
(action `OPERATOR`, not retryable) before it reads a pack or writes an
object; the notebook is intact. A build that knows the wrapper but holds no
secret that opens a door refuses before pack access and changes nothing
([compatibility.md](./compatibility.md)). `encrypt init` is the one-way
transition: the next publication writes encrypted packs and an encrypted
`current`, and every older build then answers `UPGRADE_REQUIRED`.

**Errors.** The encryption reasons are `KEY_REQUIRED` (no usable secret),
`KEY_REJECTED` (no candidate opened a door), `KEY_MATERIAL_INVALID` (unusable
key material or record), `DOOR_LIMIT` (door count out of bounds, or removing
the last door), `ENCRYPTION_ALREADY_ENABLED` and `ENCRYPTION_NOT_ENABLED`.
`KEY_REQUIRED`, `KEY_REJECTED` and `KEY_MATERIAL_INVALID` are permanent
`STORAGE_FAILURE` reasons; `DOOR_LIMIT`, `ENCRYPTION_ALREADY_ENABLED` and
`ENCRYPTION_NOT_ENABLED` are `INVALID_REQUEST`/`FIX_INPUT`. An envelope that
fails validation is `STORAGE_INTEGRITY`/`ENVELOPE_INVALID`; the version-two
wrapper of a newer format is `STORAGE_FAILURE`/`UPGRADE_REQUIRED`
([errors.md](./errors.md)).

## Gotchas

- The notebook key exists in process memory only. Nothing in the design
  caches it on disk, in the key record, or in the shared pack cache; a
  process that loses the key must be given a secret again.
- Conversion and rekey do not erase old object versions, backups, or
  external copies. Storage lifecycle policy, versioning and backups remain
  the operator's decisions; the encrypted state is only the active state.
- The visible workspace, private state and pack cache stay plaintext. A
  process that prints a secret, a path or a key into a caller-facing message
  breaks the contract; key-record and envfile refusals name the setting,
  never the value or the path.
- `Rewrap` keeps the exact records of door types this build does not know and
  every extension. An older build that dropped them would lock a newer build
  out of the notebook.
- A plaintext notebook never loses the passthrough: an absent `current` and a
  version-one manifest are not touched, so encryption is opt-in per notebook.
- `NewKDFGateForCost` lowers only the accepted floor; a production process
  keeps the canonical window, and a test that injects a cheap cost must
  never leak into a production default.
- The startup probe runs before the keyring is used, so a locked notebook
  still proves the store; the unlock refusal happens at the first notebook
  operation, not at startup.
- The decorator is built once per opened notebook, so its cached mode and door
  set are per notebook, not per process. A conversion another process makes
  between two operations is picked up on the next `current` read; a
  `current` written already wrapped invalidates the cache.
- The `encrypt` commands must keep the raw view. Decorating them would decrypt
  the wrapper they classify and rewrite, and a door rewrite would then seal
  the wrapper inside a second envelope.

## Related

- [storage.md](./storage.md): the object layout, the version-one manifest and
  the `ObjectStore` seam the decorator wraps.
- [compatibility.md](./compatibility.md): the `UPGRADE_REQUIRED` promise.
- [errors.md](./errors.md): the codes, reasons and actions.
- [security.md](./security.md): the trust model, credentials and redaction.
- [config.md](./config.md): the configuration directory and the environment.
- [cli.md](./cli.md), [running.md](./running.md): the command surface and the
  operator reference.
- [hosted-mode.md](./hosted-mode.md): the hosted adapter, whose stored payload
  this mode encrypts.
