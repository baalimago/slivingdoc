# slivingdoc encrypted notebooks worklog

**Status:** Phases 1–7 complete.

## Goal

The mode encrypts active notebook state and all later writes on the client.
Storage operators and anyone who copies active objects cannot read note
content, but can observe object keys, sizes, request timing, publication count
and public envelope headers. Enabling encryption publishes a whole-state
encrypted checkpoint; it does not erase old versions, backups or other copies.
Rekey publishes a new root checkpoint and resets active publication history.
Supported slivingdoc builds unlock the notebook with the holder's passphrase or
recovery code. There is no key exchange or service recovery; sharing is out of
band.

Keep `current` as the only accepted-state authority. Preserve the two MCP tool
names and parameters. Do not change the gateway or either storage adapter. The
visible workspace, private state and pack cache stay plaintext.

## Glossary

| Term                 | Meaning                                                                                          |
| -------------------- | ------------------------------------------------------------------------------------------------ |
| Notebook             | Notes and accepted state stored under one object-store prefix.                                   |
| Workspace            | The visible notes directory and its private state.                                               |
| `current`            | The manifest object and sole accepted-state authority.                                           |
| Manifest             | The state description that names the active checkpoint and publication tail.                     |
| Pack                 | An immutable object carrying a checkpoint or one publication.                                    |
| `workspace.Identity` | The normalized endpoint, region, bucket, prefix, manifest version and hosted-space ID.           |
| `ObjectStore`        | The semantic object-store interface implemented by existing backends.                            |
| Encrypting store     | A client-side decorator that encrypts object bodies before writes and decrypts them after reads. |
| Door                 | A passphrase or recovery code that unwraps the notebook key.                                     |
| Notebook key         | The random symmetric key that encrypts one notebook's stored objects.                            |
| Passphrase           | A human-chosen secret for a passphrase door.                                                     |
| Recovery code        | A generated secret shown once when its door is created.                                          |
| Baseline             | The last accepted generation and head recorded by the workspace.                                 |
| Conversion           | Encrypting a notebook that already has accepted history.                                         |
| Plaintext            | Note content in the visible directory and private state.                                         |

## System diagrams

These diagrams show the shared trust boundary, lifecycle and storage path. The
phase files specify command and implementation details.

### Trust and storage boundary

```text
  Secrets on the holder's machine
  prompt | environment | envfile | notebooks.json
                     |
                     v
                 +--------+       KDFGate
                 |Keyring |<---------+
                 +---+----+
                     | NotebookKey (process memory only)
                     v
+--------------------+-----------------------------------------+
| slivingdoc: CLI/MCP -> Notebook -> encrypted ObjectStore     |
|                                                              |
| Plaintext remains in visible workspace, private state, cache |
+-------------------------------+------------------------------+
                              | ObjectStore calls
                              v
                 +-----------------------+
                 | S3-compatible / hosted|
                 | encrypted payload     |
                 | public headers visible|
                 +-----------------------+
```

The store can still observe keys, object sizes, request timing, publication
count and public envelope headers. It does not receive the notebook key.

### Notebook lifecycle

```text
  Plaintext notebook -- encrypt init / convert --> Encrypted, locked
                                                   | unlock with a door
                                                   v
                                          Encrypted, unlocked
                                          key exists in process memory
                                            | exit
                                            +----> Encrypted, locked
                                            |
                                            | rekey
                                            v
                                Encrypted, unlocked under new key
                                new root checkpoint; history reset
                                            | exit
                                            +----> Encrypted, locked
```

There is no transition back to plaintext. A new unencrypted notebook is a
separate notebook.

### Read and publish path

```text
Caller -> read `current` -> inspect public header -> resolve candidates
                                                    |
                                      no door opens -+-> refuse; no pack access
                                                    |
                                              unlock key
                                                    |
                        +---------------------------+-------------------+
                        |                                               |
                   decrypt reads                                 encrypt writes
                        |                                               |
                   notebook operation <- merge -> immutable pack upload
                                                                      |
                                                   conditional update `current`
```

Only a write reaches publication. The encrypted decorator keeps stored pack
metadata about ciphertext and presents plaintext pack data to the notebook.
`current` is published after pack upload; an ETag loss follows the existing
merge-and-retry behavior.

## Public contract

The effort adds a CLI-only `encrypt` command family. It adds no MCP tool,
storage route, or adapter method. Existing MCP tools, commands and storage
interfaces remain unchanged. Phase 4 owns the command and error contract.

## Status board

| Phase                                                          | Status      | Outcome                                                        |
| -------------------------------------------------------------- | ----------- | -------------------------------------------------------------- |
| [1. Crypto core](phase-1-crypto-core.md)                       | Complete    | Define and validate the envelope and KDF contract.             |
| [2. Manifest and store](phase-2-manifest-and-store.md)         | Complete    | Add the encrypted manifest wrapper and object-store decorator. |
| [3. Key material](phase-3-key-material.md)                     | Complete    | Resolve secrets and manage the identity-scoped key record.     |
| [4. Command surface](phase-4-command-surface.md)               | Complete    | Add the `encrypt` commands and user-facing errors.             |
| [5. Conversion](phase-5-conversion.md)                         | Complete    | Convert accepted plaintext state to encrypted state.           |
| [6. Compatibility and docs](phase-6-compatibility-and-docs.md) | Complete    | Document compatibility, security limits and hosted privacy.    |
| [7. Quality gate](phase-7-quality-gate.md)                     | Complete    | Run the complete gate and close readiness findings.            |

Complete phases in order. Phase 2 and Phase 3 depend on Phase 1; Phase 4
depends on Phases 1–3; Phase 5 depends on Phases 2 and 4; Phase 6 depends on
Phases 1–5; Phase 7 is last.

## Scope

Build client-side encryption for stored notebook objects, key material, CLI
commands, conversion, and documentation. Do not change gateway routes or
storage adapter methods. Keep `current` as the only accepted-state authority.
The visible workspace, private state, and pack cache remain plaintext. Storage
operators can still observe object keys, sizes, timing, publication count and
public envelope headers; old object versions, backups and external copies are
not erased.

This version has no key exchange, keychain, unlock cache, padding, encryption-off
command, recipient wrapping, or service-side key recovery. A person supplies
passphrases and shares secrets out of band; the tool generates and displays a
recovery code once. Plaintext remains available in a new, unencrypted notebook.

## Cross-phase handoffs

| Owner                                    | Contract used by later phases                                    |
| ---------------------------------------- | ---------------------------------------------------------------- |
| [Phase 1](phase-1-crypto-core.md)        | `internal/crypt` wire format and keyring implementation.         |
| [Phase 2](phase-2-manifest-and-store.md) | `internal/storage` manifest wrapper and encrypted decorator.     |
| [Phase 3](phase-3-key-material.md)       | `notebooks.json` codec and candidate resolver.                   |
| [Phase 4](phase-4-command-surface.md)    | CLI and reporting behavior used by conversion and documentation. |

The interfaces passed across those boundaries are:

```go
type NotebookKey [32]byte
type KDFParams struct { MemoryKiB uint32; Iterations uint32; Parallelism uint8 }
type DoorType uint8
type ObjectKind uint8
type Door struct {
    Type DoorType; KDFID uint8; Salt [16]byte; Params KDFParams
    Nonce [24]byte; WrappedKey [48]byte; OpaqueBody []byte
}
type Extension struct { Type uint16; Value []byte }
type Header struct {
    FormatVersion uint16; Kind ObjectKind; ChunkSize uint32; PlaintextSize uint64
    NoncePrefix [16]byte; Doors []Door; Extensions []Extension; Commitment [32]byte
}
type CandidateSource uint8
const (
    SourceUnknown CandidateSource = iota
    SourcePrompt
    SourceEnvironment
    SourceEnvironmentFile
    SourceKeyRecord
)
type Candidate struct { Source CandidateSource; Secret []byte }
type KDFGate struct { ... }
type Keyring struct { ... }

func GenerateNotebookKey() (NotebookKey, error)
func NewKDFGate(maxJobs int) (*KDFGate, error)
func NewKeyring(candidates []Candidate, gate *KDFGate) (*Keyring, error)
func NewUnlockedKeyring(key NotebookKey, candidates []Candidate, gate *KDFGate) (*Keyring, error)
func DeriveKEK(ctx context.Context, gate *KDFGate, secret []byte, salt [16]byte, p KDFParams) ([32]byte, error)
func WrapKey(ctx context.Context, gate *KDFGate, key NotebookKey, secret []byte, objectKey string, kind DoorType, p KDFParams) (Door, error)
func UnwrapKey(ctx context.Context, gate *KDFGate, door Door, secret []byte, objectKey string) (NotebookKey, error)
func (k *Keyring) Open(ctx context.Context, objectKey string, envelope []byte) ([]byte, Header, error)
func (k *Keyring) ResolvedSource() CandidateSource
func (k *Keyring) Seal(objectKey string, kind ObjectKind, plaintext []byte, doors []Door) ([]byte, error)
func (k *Keyring) Rewrap(objectKey string, envelope []byte, doors []Door) ([]byte, error)
func Encrypted(store ObjectStore, keyring *Keyring) ObjectStore
func Resolve(id workspace.Identity, getenv func(string) string) ([]Candidate, error)
```

Door codes are 1 for passphrase and 2 for recovery; object-kind codes are 1 for
encrypted `current` and 2 for packs. Candidate sources are prompt, environment,
envfile and key record. Phase 1 generates each `NotebookKey` with secure
randomness.

Phase 1 owns the crypto types and their semantics; Phase 2 owns `Encrypted`;
Phase 3 owns `Resolve`. Each phase defines its own parameters, limits and local
invariants. This README keeps only the interfaces needed across phase
boundaries. `internal/app` creates one `KDFGate` per process and injects it into
every keyring; the crypto package has no process-global gate. `Open` verifies
the door and header commitment before returning plaintext. `Rewrap` changes
only the authenticated header, not payload bytes. `NewKeyring` enforces Phase
4's bound on candidate sources.

## Shared invariants

| Invariant                                                                 | Applies across phases                   | Proof                                                                                         |
| ------------------------------------------------------------------------- | --------------------------------------- | --------------------------------------------------------------------------------------------- |
| `current` remains the sole accepted-state authority.                      | All storage and notebook work.          | Phase 2 `TestEncryptedManifestCodec`; Phase 5 conversion scenarios.                           |
| Packs are immutable, and bytes exist before `current` references them.    | Store, commands, conversion and rekey.  | Phase 2 `TestEncryptedCommitUploadBeforeManifest`.                                            |
| Existing MCP tools, storage routes and adapter method sets do not change. | Every phase.                            | Phase 7 diff review; Phase 2 store-contract scenarios.                                        |
| The visible workspace, private state and pack cache are not encrypted.    | Commands, conversion and documentation. | Phase 4 `TestEncryptCommandSurface`; Phase 6 documentation review.                           |
| Unresolved conflict markers are never published.                          | Operations that publish notebook state. | Phase 5 `TestEncryptInitRejectsMarkersAndProtectedChanges`; Phase 4 `TestEncryptRekeyPolicy`. |

Phase-owned invariants, limits and their tests are listed in the owning phase.

## Definition of success

| Criterion                                                                   | Evidence                                        |
| --------------------------------------------------------------------------- | ----------------------------------------------- |
| Two machines can use the same encrypted notebook with only the passphrase.  | Phase 2 encrypted pull/commit scenario.         |
| Active stored objects contain ciphertext, not note text or paths.           | Phase 2 and Phase 5 store-inspection scenarios. |
| Recovery and passphrase changes work without rewriting pack objects.        | Phase 4 command scenarios.                      |
| Older builds refuse encrypted state without pack access or writes.          | Phase 6 compatibility scenario.                 |
| Conversion preserves accepted state and does not claim to erase old copies. | Phase 5 scenarios and Phase 6 docs.             |
| No gateway route or storage adapter method changes.                         | Final source diff review.                       |
| All repository quality gates pass.                                          | Phase 7 `make qa` result.                       |

## Validation policy

The repository gate is `make qa`; do not skip tests or narrow its suite. Black-box
scenarios in `internal/integrationtest` define behavior, and unit tests cover
internal parsing and failure branches. Run the duplication command separately
as a signal, not a gate:

```text
go run github.com/mibk/dupl@v1.0.0 -t 80 .
```

Phase-specific measurements and external repository gates belong to their
owning phase.

## Readiness checklist

1. Each phase defines its own parameters, limits and invariants; other phases
   refer to the owner instead of copying values.
2. Every test name belongs to one phase and appears in that phase's file list;
   cross-phase error rows may cite the owning test.
3. Each route, file schema, config field, flag and injectable field has one
   owning phase.
4. Every invariant and limit has an observable test in its owning phase.
5. Every phase requiring a person declares `Human required` and names its
   artifact and stop point.
6. No phase depends on text an earlier phase deletes; deletion belongs to the
   phase that performs it.
7. No phase creates human-owned configuration or content reserved by Scope.
8. New conventions agree with repository code and architecture docs.

Phase 7 checks this list against the implemented tree.

## Decisions log

Decisions this effort records, newest last. `author` is the worker that
implemented the phase named in the entry.

| ID | Date | Decision | Rationale | Replaces |
| --- | --- | --- | --- | --- |
| D1 | 2026-10-08 | **author (phase-1 worker)** HKDF-SHA-256 comes from the standard library `crypto/hkdf`; the pinned `golang.org/x/crypto` v0.57.0 covers Argon2id and XChaCha20-Poly1305 only. | Phase 1 names the dependency for those two primitives. `crypto/hkdf.Key` returns the key with an error instead of an `io.Reader` that cannot fail for SHA-256, so no derived key can be silently short. | — |
| D2 | 2026-10-08 | **author (phase-1 worker)** The canonical Argon2id cost is a function, `DefaultKDFParams()`, not a package-level value. | A Go composite literal is not a constant expression, and package-level mutable state is avoided in this repository. | — |
| D3 | 2026-10-08 | **author (phase-1 worker)** Phase 1 owns the stored `current` bound (`CurrentEnvelopeLimit`) and exposes `WrappedCurrentSize`, which adds the exact 27-byte empty version-two wrapper and the Base64 length. `Seal` and `Rewrap` refuse an oversized `KindCurrent` envelope; Phase 2 owns the wrapper encoding itself. | The parameter table makes the 1 MiB bound cover the wrapper and the Base64 encoding, so the phase that owns the number must account for both; the wrapper field order and spelling stay Phase 2's. | — |
| D4 | 2026-10-08 | **author (phase-1 worker)** `Rewrap` replaces the supported doors with the caller's list, re-appends the exact records of door types this build does not know, and keeps every extension. | An older build that silently dropped a newer build's door would lock the newer build out of the notebook; the signature carries no extensions, so the parsed ones are the only source. | — |
| D5 | 2026-10-08 | **author (phase-1 worker)** A candidate set holds at most one candidate per source and at most four in all; `SourceUnknown` and empty secrets are refused. `NewUnlockedKeyring` validates the same bound but never searches. | Matches Phase 4's four named sources, so a duplicated source is a caller bug rather than a longer search; validating in both constructors keeps one bound for both. | — |
| D6 | 2026-10-08 | **author (phase-1 worker)** `KDFGate` is a buffered semaphore; the application passes 1 to hold the one-Argon2id-job-per-process rule, and the gate itself accepts any positive width. | The parameter names one job per process, but the width is a process policy the application injects, and a test can prove the bound without a second policy constant. | — |
| D7 | 2026-10-08 | **author (phase-1 worker)** `openPayload` decrypts each record into the preallocated plaintext buffer instead of appending per-record results. | The preallocated destination removes one 64 KiB allocation per record and about 90 MiB of transient churn at the 95 MiB framing payload, which brought the measured decryption peak inside the 350 MiB budget without changing the format. | — |
| D8 | 2026-10-08 | **author (phase-1 worker)** The harness measures framing before the Argon2id phases and releases the heap at every phase and trial boundary. | RSS is process-wide and Argon2id leaves 256 MiB resident; without the release the framing peak would report the unlock phase's footprint. Every budget and trial count is unchanged. | — |
| D9 | 2026-10-08 | **author (phase-1 worker)** `golang.org/x/crypto` v0.57.0 is a direct dependency; its module graph raises `golang.org/x/sys` to v0.48.0, `golang.org/x/term` to v0.46.0, `golang.org/x/text` to v0.42.0 and `golang.org/x/sync` to v0.23.0. | Minimum version selection from the pinned crypto version; the four are transitive and carry no source change of this effort. | — |
| D10 | 2026-10-08 | **author (phase-2 worker)** `Encrypted` keeps the two-argument signature the cross-phase interface declares. The phase-2 draft's `sealDoors` variadic is removed: `encrypt init`, `rekey` and `passwd` seal their new `current` envelope themselves and the decorator writes an already-wrapped body through unchanged. | The extra parameter was never in the declared interface, and the passthrough the decorator already needs for `init` makes it redundant; one construction path is simpler for Phase 4. | — |
| D11 | 2026-10-08 | **author (phase-2 worker)** The version-two wrapper lives in `internal/storage/manifest.go` beside the version-one manifest it wraps, and the decorator in `internal/storage/encrypted.go`. The one notebook edit is `internal/notebook/remote.go`, which maps the decorator's failures: rollback is `HISTORY_INVALID`, an unreadable `current` is refused before pack access, and a pack that fails envelope validation is `PACK_INVALID`. | The phase owns the manifest and the store boundary, so their codec stays together; the notebook is the only place those errors become the taxonomy, and Phase 4 repoints the pack-envelope reason to `ENVELOPE_INVALID`. | — |
| D12 | 2026-10-08 | **author (phase-3 worker)** The record is keyed by the full lowercase SHA-256 identity digest, `workspace.IdentityDigest(id)`; `SharedCacheDirName` now truncates that same digest. | Phase 3 keys key material by "the lowercase identity digest produced by `workspace.Identity` and `SharedCacheDirName`", and one function keeps the two in step. The record carries the full digest rather than the cache name's truncated one because key material needs collision resistance the cache name explicitly disclaims. | — |
| D13 | 2026-10-08 | **author (phase-3 worker)** The envfile grammar lives in `internal/keyrecord/resolve.go`, and the file is read through `credentials.PrivateFile`. A passphrase-file failure is re-worded to name the setting (`SLIVINGDOC_PASSPHRASE_FILE` or `passphraseFile`) and never the path. | `PrivateFile`'s own messages name the file, and no caller-facing text may carry an envfile path; the grammar is small and has one reader, so it stays beside it. | — |
| D14 | 2026-10-08 | **author (phase-3 worker)** `File.Update` and `File.Forget` own the locked read-modify-write; the resolver reads through `File.Load` without a lock. `Entry` is a variant (`digest`, `Kind`, `value`) built only through `Passphrase`/`PassphraseFile`. | Phase 4 changes the record only after an explicit caller request, so the lock belongs to the change, while a read is safe against the atomic rename. The variant makes "both secret and path" unrepresentable in Go instead of a validated pair of strings. | — |

| D15 | 2026-10-09 | **author (phase-4 worker)** The encrypt commands live in `cmd/encrypt/encrypt.go` and `internal/app/encryption.go`; their notebook operations (`EncryptStatus`, `EncryptDoors`, `EncryptInit`, `EncryptRekey`) live in `internal/notebook/encryption.go`, and `internal/recovery` owns the recovery-code codec. | The notebook layer is the only place that can publish a root checkpoint through the existing merge, bounded retry, marker and path-policy rules; the command layer stays a thin report and prompt surface. Phase 5 owns extending the notebook file. | — |
| D16 | 2026-10-09 | **author (phase-4 worker)** The black-box command scenarios are `cli.Run` calls over one shared fake store in `internal/cli/encrypt_command_test.go`, not `internal/integrationtest` spawned helpers. | A spawned helper gets its own fake store, so consecutive encrypt commands could not observe one notebook, and it cannot inject the test KDF cost. The in-process harness is the same process boundary without those limits. | The phase draft's `internal/integrationtest/scenario_encrypt_command_test.go`. |
| D17 | 2026-10-09 | **author (phase-4 worker)** `ProcessOptions.KDFParams` plus `crypt.NewKDFGateForCost` let a test lower the accepted Argon2id floor; a production process keeps `DefaultKDFParams` and the canonical window. | The canonical 256 MiB/3-lane cost is deliberately expensive and would blow the 30 s per-package budget under `-race -count=3`; the gate lowers the floor only to the injected cost. | — |
| D18 | 2026-10-09 | **author (phase-4 worker)** A recovery code is 128 entropy bits as twelve canonical lowercase BIP-39 words with the 4-bit checksum; its secret is the phrase bytes, never a BIP-39 seed. | The phase parameter names exactly that encoding and forbids seed derivation, which would add an unused PBKDF2 step. | — |
| D19 | 2026-10-09 | **author (phase-4 worker)** `ReasonEncryptionNotEnabled` (`INVALID_REQUEST` / `FIX_INPUT`) is added beyond the phase's error table for a door command against a plaintext notebook. | The table assigns no token to that refusal, and the refusal still needs a stable reason and action. | — |
| D20 | 2026-10-09 | **author (phase-4 worker)** `TestParseRejects/checksum_fails` uses the canonical all-zero BIP-39 vector and its checksum-broken twin instead of swapping a random word. | A four-bit checksum accepts one random single-word mutation in sixteen, so the old row made `make test` fail intermittently. | — |
| D21 | 2026-10-09 | **author (phase-5 worker)** The black-box conversion scenarios spawn the real one-shot CLI over the hosted reference gateway, and the integration helper gains the test-only `helperKDFEnv` knob (`SLIVINGDOC_INTEGRATION_CHEAP_KDF`) to write and accept the cheap Argon2id cost. | The two limits that made Phase 4 keep its command scenarios in process (D16) do not apply: the hosted store is external, so consecutive helpers observe one notebook, and the knob lets the strict 30 s package budget afford the door derivations. Every other helper keeps `crypt.DefaultKDFParams`, and the canonical cost stays pinned by the `internal/crypt` unit tests and the performance harness. | — |
| D22 | 2026-10-09 | **author (phase-5 worker)** `sanitizedEnv` drops `SLIVINGDOC_PASSPHRASE` and `SLIVINGDOC_PASSPHRASE_FILE`. | Phase 3 and Phase 4 introduced the two settings; without the drop a developer's own secret would reach every spawned helper that does not override it. | — |
| D23 | 2026-10-09 | **author (phase-5 worker)** `TestEncryptInitCapacityBoundary` seeds two generations that rewrite one note, so the accepted state is smaller than the packs the space already holds. | A checkpoint pack exported from the head state must be no larger than the stored packs for a space at its quota to fit it under the doubled limit; growth-only history makes the whole-state checkpoint as large as the stored total and leaves no headroom to observe. | — |
| D24 | 2026-10-09 | **author (phase-6 worker)** `scripts/check-architecture-links.py` checks every local Markdown link of `architecture/*.md`: the target file must exist, and a `#fragment` must match a GitHub-style heading slug (duplicate headings get `-1`, `-2` suffixes). External scheme links and bare fragments are skipped. | The phase asks one script that proves local links resolve. A heading check catches a renamed section that a file-existence check alone misses, and a repo script makes the check repeatable instead of a one-off. | — |
| D25 | 2026-10-09 | **author (phase-6 worker)** The compatibility scenario proves the older-reader contract with this build's raw (undecorated) store path, not a separately built older binary: a hand-written version-two manifest and an at-rest encrypted `current` are read through a plain harness, which refuses both as `STORAGE_FAILURE`/`UPGRADE_REQUIRED` with zero pack reads and zero writes. | This build's plaintext path is exactly a version-one-only reader, because `DecodeManifest` rejects the version-two wrapper before any other field. An in-process scenario shares the deterministic fake store and can assert the zero-access guarantee; a downloaded binary could do neither. | — |
| D26 | 2026-10-09 | **author (phase-6 worker)** The phase's Human required artifact, the hosted privacy text in `baalimago/slivingdoc-cloud` (`src/content/site/privacy.md`), was not changed: the repository's required review and its `make qa` result cannot be obtained in this session, so the work stops before that repository and reports the blocked artifact. | The phase's own human-required handoff says to stop when the approval is unavailable; the slivingdoc architecture and CLI documentation was completed first. | — |
| D27 | 2026-10-09 | **author (phase-7 worker)** The readiness review corrected the phantom test name `TestEncryptStatusWording` to `TestEncryptCommandSurface` in the README shared-invariants table and the phase-6 invariant table, and added `internal/integrationtest/main_test.go` to phase 5's owned files. | Readiness item 2 requires every cited test name to exist and item 3 requires every injectable field to have one owning phase; no test defines `TestEncryptStatusWording` and the status wording is asserted by `TestEncryptCommandSurface`, while the `helperKDFEnv` knob (D21) lives in `main_test.go`. | — |
| D28 | 2026-10-09 | **author (phase-7 worker)** The carried finding that `serve`, `pull` and `commit` never construct `storage.Encrypted` is carried forward explicitly rather than opened as a new phase. | `architecture/encryption.md` declares the process-level decorator wiring a separate change, and the effort's Definition of Success is proven at the decorator boundary by the Phase 2 `TestEncryptedPullCommit` scenario; the worklog's phase set stays closed. | — |
| D29 | 2026-10-22 | **author (follow-up session)** The wiring D28 carried forward landed right after the effort closed, because a user met the D28 gap in real use: `app.buildService` now resolves the process keyring after the store check, `app.NewService` holds it, and `Service.notebookStore` decorates every opened notebook while `Service.rawNotebookFor` keeps the raw view for the `encrypt` commands. `storage.ErrKeyRequired` tells "no secret at all" from `crypt.ErrKeyRejected`, and `keyMaterialError` maps both to KEY_REQUIRED and KEY_REJECTED before any pack access. | The carried finding was a product defect, not a deferral: `serve`, `pull` and `commit` refused an encrypted notebook as `UPGRADE_REQUIRED`, a false and unactionable answer. The scenarios that encoded the old behavior were rewritten in the same change (`upgrade_integrationtest_test.go`), the encrypted sync scenarios now run the service's own decorator, and `RefusalLine` gives a refused startup the same stable tokens an operation report carries. | D28's "separate change" gotcha in `architecture/encryption.md`. |
| D30 | 2026-10-22 | **author (follow-up session)** `pull` and `commit` ask `Passphrase or recovery code: ` on a terminal when no configured candidate exists and the stored `current` is an envelope (`app.Service.AskForSecret`), and the ask is masked: `internal/tui.ReadMasked` puts stdin in raw mode, draws one `*` per typed character, drops escape sequences, and the answer becomes the `SourcePrompt` candidate of that run only. Ctrl-C and a bare Ctrl-D are `KEY_REQUIRED`, so no new error token appeared. | The KEY_REQUIRED text told a person to export an environment variable, which is unsafe on a shared machine and unusable for the one-shot commands. Asking is what a person expects, and the mask keeps the answer off the screen and out of a scrollback; the `encrypt` commands use the same read for every passphrase question. A pipe, a script and the MCP server keep the refusal, because the ask needs a terminal on both stdin and stderr. | The KEY_REQUIRED wording that named only the environment. |

## Session journal

### 2026-10-08 — Phase 1 complete

Implemented `internal/crypt` and `scripts/encryption-perf`. The reference-host
measurement (Intel Core i5-8350U, 8 logical CPUs, go1.26.5,
golang.org/x/crypto v0.57.0) passed every budget; see
[phase-1-crypto-core.md](phase-1-crypto-core.md) for the exact results.

### 2026-10-08 — Phase 2 complete

Implemented the version-two `current` wrapper and the `Encrypted` object-store
decorator in `internal/storage`, plus the notebook read-path mappings in
`internal/notebook/remote.go`. Unit tests cover the codec, rollback, the
decorated method set, conditional-write passthrough, locked-notebook refusal
and envelope tampering. The integration suite runs the shared store contract
through the decorator for the fake, S3 and hosted adapters, and proves the two
machine encrypted pull/commit round trip, ciphertext at rest, pack upload
before manifest, the hosted object limit, and the rollback refusal. See
[phase-2-manifest-and-store.md](phase-2-manifest-and-store.md) for the
commands and results.

### 2026-10-08 — Worklog scope review

Moved implementation-specific contracts into their owning phases and restored
the shared trust-boundary, lifecycle, and read/publish diagrams. Updated the
public worklog-plan skill to keep these cross-phase diagrams in the README and
local procedures in phases. Readiness checks and `make qa` passed; all phases
remain Not Started.

### 2026-10-08 — Phase 3 complete

Implemented `internal/keyrecord`: the strict `notebooks.json` codec, the
hardened private file with the locked `Update`/`Forget`, and the candidate
resolver that orders the environment value, its envfile value and the
key-record value. `internal/workspace` gained `IdentityDigest`, the full
identity digest the record is keyed by, and `SharedCacheDirName` now truncates
it. Unit tests cover the round trip, the strict codec, candidate order and
identity isolation, the envfile grammar and its hardening, the private-file
contract, unsafe files, and the separation from `workspaces.json`. See
[phase-3-key-material.md](phase-3-key-material.md) for the commands and
results. `make qa` passed at 87.6% coverage.

### 2026-10-09 — Phase 4 complete

The command surface was already present in the working tree; this session
verified it, reconciled the phase documents with the tree, and closed the one
failing gate. `internal/recovery/code_test.go` held a flaky checksum row: it
swapped the last word of a fresh random code and assumed the four-bit BIP-39
checksum would reject every mutation, but four bits accept one mutation in
sixteen, so `make test` failed intermittently. The row now uses the canonical
all-zero vector and its checksum-broken twin. `make qa` passes at 86.9 %
coverage with 35 npm tests. See
[phase-4-command-surface.md](phase-4-command-surface.md) for the commands,
results, and one deferred, unowned finding: the process never constructs
`storage.Encrypted`, so `serve`, `pull` and `commit` cannot read an encrypted
notebook even though the README's read/publish diagram expects it.

### 2026-10-09 — Phase 5 complete

The conversion operations (`EncryptInit`, `EncryptRekey`, `publishRoot`,
`rootRemote`, `cleanupCurrent`) and `internal/notebook/encryption_test.go`
were already in the working tree when this session started; it verified them
against the specification, added the missing black-box scenarios in
`internal/integrationtest/cli/encrypt_integrationtest_test.go`, and closed the
phase. The spawned helper gains the test-only `helperKDFEnv` cost knob (D21),
and `sanitizedEnv` now drops the two passphrase settings (D22). `make qa`
passes. See [phase-5-conversion.md](phase-5-conversion.md) for the commands and
results.

### 2026-10-09 — Phase 6 complete

Finished the documentation and compatibility phase. Added the hosted-operator
security boundary to `architecture/hosted-mode.md`, the `golang.org/x/crypto`
v0.57.0 dependency baseline to `architecture/releasing.md`, and the `encrypt`
command, the two passphrase settings, an `## Encryption` operator section and
the encryption lines in Operational ownership to `architecture/running.md`.
These complete the doc set started earlier the same day (`encryption.md`, the
index and reading order, and the storage, compatibility, errors,
product-contract, config, cli, security and guarantees updates), plus the
`encrypt` command and the three new packages in `architecture/overview.md`
and `AGENTS.md`.
`TestScenarioUpgradeRequiredForNewerManifest` and
`TestScenarioUpgradeRequiredForEncryptedCurrent` pass, and
`python3 scripts/check-architecture-links.py` resolves every local link and
heading. The hosted privacy text in `baalimago/slivingdoc-cloud` remains the
phase's **Human required** artifact and was not changed; the blocked artifact
is reported to the maintainer (D26). See
[phase-6-compatibility-and-docs.md](phase-6-compatibility-and-docs.md) for the
commands and results.

### 2026-10-09 — Phase 7 complete

Ran the repository gate and closed the worklog. `make qa` passes: gofumpt, go
vet, staticcheck and go fix clean; the Go suite under
`-race -count=3 -timeout=30s -coverpkg=./...` passes with
`== coverage: 87.1% (floor 70%) ==`; `npm test` passes 35 of 35. The readiness
checklist closed two findings (D27): the phantom test name
`TestEncryptStatusWording` became `TestEncryptCommandSurface`, and phase 5's
owned files gained `internal/integrationtest/main_test.go`. The duplication
signal reports five clone groups, none actionable. The diff review confirms no
gateway, route or storage-adapter change, no generated artifact, and the pinned
`golang.org/x/crypto` dependency. The carried finding that `serve`, `pull` and
`commit` never build `storage.Encrypted` is carried forward explicitly (D28),
as `architecture/encryption.md` declares that wiring a separate change. See
[phase-7-quality-gate.md](phase-7-quality-gate.md) for the commands and
results.
