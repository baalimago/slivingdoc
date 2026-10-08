# slivingdoc encrypted notebooks worklog

**Status:** Worklog finalized for validation. Implementation phases are not started.

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
| [1. Crypto core](phase-1-crypto-core.md)                       | Not Started | Define and validate the envelope and KDF contract.             |
| [2. Manifest and store](phase-2-manifest-and-store.md)         | Not Started | Add the encrypted manifest wrapper and object-store decorator. |
| [3. Key material](phase-3-key-material.md)                     | Not Started | Resolve secrets and manage the identity-scoped key record.     |
| [4. Command surface](phase-4-command-surface.md)               | Not Started | Add the `encrypt` commands and user-facing errors.             |
| [5. Conversion](phase-5-conversion.md)                         | Not Started | Convert accepted plaintext state to encrypted state.           |
| [6. Compatibility and docs](phase-6-compatibility-and-docs.md) | Not Started | Document compatibility, security limits and hosted privacy.    |
| [7. Quality gate](phase-7-quality-gate.md)                     | Not Started | Run the complete gate and close readiness findings.            |

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
| The visible workspace, private state and pack cache are not encrypted.    | Commands, conversion and documentation. | Phase 4 `TestEncryptStatusWording`; Phase 6 documentation review.                             |
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

## Session journal

### 2026-10-08 — Worklog scope review

Moved implementation-specific contracts into their owning phases and restored
the shared trust-boundary, lifecycle, and read/publish diagrams. Updated the
public worklog-plan skill to keep these cross-phase diagrams in the README and
local procedures in phases. Readiness checks and `make qa` passed; all phases
remain Not Started.
