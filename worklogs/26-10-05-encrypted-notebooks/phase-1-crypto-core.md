# Phase One: crypto core

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Implement the bounded envelope format and prove its wire, authentication, and performance contracts.

## Specification

Implement `internal/crypt` from the format and parameters below. Keep all
cryptographic primitives and key types in this package. Reject malformed
lengths and unsupported costs before allocation or KDF work. Authenticate the
object key, immutable core, door list, and payload records. Preserve payload
bytes when a caller changes doors.

Add `scripts/encryption-perf` as a repeatable measurement tool. It measures
the completed keyring and envelope, including the full candidate-door search,
serialized concurrent unlocks, and both framing directions at the hosted
gateway's maximum plaintext pack size. It records dependency versions, trial
times, median elapsed time and throughput, and maximum RSS. It does not contact
a store.

### Envelope format

All integers are unsigned big-endian. The envelope has this exact field order:

```text
magic[8] | formatVersion[u16] | objectKind[u8] | headerLength[u32]
core: chunkSize[u32] | plaintextSize[u64] | noncePrefix[16]
doorCount[u8] | doorRecord* | extensionCount[u8] | extensionRecord*
headerCommitment[32] | payloadRecords

doorRecord: doorType[u8] | kdfID[u8] | bodyLength[u16]
body: salt[16] | memoryKiB[u32] | iterations[u32] | parallelism[u8]
      | wrapNonce[24] | wrappedNotebookKey[48]

extensionRecord: extensionType[u16] | valueLength[u16] | value[valueLength]
```

`headerLength` covers the immutable core through the commitment, not the
preamble or payload. A supported door body is fixed at 97 bytes. Unknown
bounded door types and optional extension types are treated as opaque, skipped
and preserved by header rewrites. Unknown required semantics require a new
format version.
Reject malformed lengths, unsupported core values, duplicate extension types,
trailing bytes and unsupported versions before allocation or KDF work.

Payload records contain one ciphertext chunk and its tag. Every full chunk uses
the configured chunk size; the last data chunk may be shorter. Record lengths
follow from the authenticated plaintext size and chunk size; there is no length
prefix. Data counters start at zero. Nonces combine the random prefix with the
big-endian record counter. The final record authenticates empty plaintext with
the next counter and zero plaintext length. Require all declared chunks and
exactly one final record; reject truncation and trailing bytes. An empty
payload contains only the final record.

HKDF-SHA-256 uses the notebook key and an all-zero salt. Its info contains the
domain `slivingdoc/envelope/v1/`, a purpose label, a NUL byte and the
length-prefixed ASCII protocol object key. Derive separate `header-mac` and
`payload-aead` keys. The HMAC-SHA-256 header commitment covers the domain
`slivingdoc/envelope/header/v1` plus NUL, format version, object kind, encoded
object key, immutable core, exact door-count and door-record bytes, and exact
extension-count and extension-record bytes. Exclude the commitment field. Each
door wrap uses XChaCha20-Poly1305 with a random nonce and the Argon2id-derived
KEK. Wrap associated data binds `slivingdoc/envelope/wrap/v1` plus NUL, version,
encoded object key, door type, KDF id, salt and encoded KDF parameters. Payload
associated data binds `slivingdoc/envelope/payload/v1` plus NUL, version, object
kind, encoded object key, core, record type, counter and plaintext length. It
excludes doors, extensions and the header commitment so a door rewrite leaves
payload ciphertext unchanged.

### Parameters

| Parameter                   | Value                                                                                                                                             |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Format version and magic    | Version 1; `SLIVENC\0`; 8-byte magic, 16-bit version, 8-bit kind, 32-bit header length.                                                           |
| Immutable core              | 32-bit chunk size, 64-bit plaintext size, 16-byte random nonce prefix.                                                                            |
| Object kinds and door types | Codes follow the shared interface; counts use 8 bits.                                                                                             |
| Door record                 | Type, KDF id, 16-bit body length, 16-byte salt, Argon2id parameters, 24-byte nonce and 48-byte wrapped key.                                       |
| Supported door body         | 97 bytes.                                                                                                                                         |
| KDF id and encoding         | 1 is Argon2id version 19; memory KiB and iterations are 32-bit, parallelism is 8-bit.                                                             |
| Extensions                  | 8-bit count; each record has a 16-bit type and 16-bit length.                                                                                     |
| Header commitment           | 32-byte HMAC-SHA-256 over the authenticated header transcript.                                                                                    |
| Payload records             | 64-KiB chunks; 16-byte XChaCha20-Poly1305 tags; data type 1, final type 2; transcript has 8-bit type, 64-bit counter and 32-bit plaintext length. |
| Object-key encoding         | ASCII protocol key prefixed by its 16-bit byte length.                                                                                            |
| Notebook key                | Random key defined by the shared interface.                                                                                                       |
| Crypto dependency           | `golang.org/x/crypto` v0.57.0 for Argon2id and XChaCha20-Poly1305.                                                                                |
| Passphrase bytes            | Non-empty, exact valid UTF-8 without normalization; maximum 1 KiB.                                                                                |
| Written Argon2id cost       | 256 MiB memory, 3 iterations, 4 lanes.                                                                                                            |
| Accepted Argon2id window    | 64–256 MiB memory, 1–3 iterations, 1–4 lanes.                                                                                                     |
| KDF concurrency             | One Argon2id job per process through the injected gate.                                                                                           |
| Salt and nonce sizes        | 16-byte random salt per door; 24-byte wrap nonce; 24-byte payload nonce from 16-byte prefix plus 64-bit counter.                                  |
| Key derivation              | HKDF-SHA-256, separated by purpose and length-prefixed object key.                                                                                |
| Current envelope bound      | 1 MiB, including wrapper, Base64, doors, manifest payload and framing; also maximum parsed header length.                                         |

The recovery door's generation and mnemonic encoding are owned by Phase 4. Use
the exact recovery secret bytes as the Argon2id input; do not apply BIP-39 seed
derivation.

### Feasibility and acceptance measurement

Preliminary measurements on the Intel Core i5-8350U reference host with eight
logical CPUs support feasibility only: sixteen KDF attempts took 4.249 s
median and 784,348 KiB peak RSS; two serialized workers took 7.528 s and
783,840 KiB peak RSS; a 95-MiB sample framed in 332 ms encrypt / 198 ms decrypt
with 295,880 KiB peak RSS. The unchanged fake, S3 and hosted stores accept
opaque object bodies; the hosted gateway still enforces its ciphertext limit.

The final harness records Go and crypto-library versions and runs five
independent trials. Use median time and throughput and maximum peak RSS. Meet
all these budgets:

| Measurement                   | Budget                                                      |
| ----------------------------- | ----------------------------------------------------------- |
| One isolated full unlock      | At most 15 s.                                               |
| Two simultaneous full unlocks | At most 30 s total.                                         |
| Paired full-unlock peak RSS   | At most 1 GiB.                                              |
| Framing throughput            | At least 100 MiB/s in each direction on the reference host. |
| Framing payload               | `gatewaytest.MaxPackBytes` plaintext bytes.                 |
| Framing peak RSS              | At most 350 MiB for encryption or decryption.               |

Run `go run ./scripts/encryption-perf` on the reference host. This is an
acceptance gate. If dependency, time, throughput or memory results fail, stop
and revise the design with the maintainer; do not weaken a test or budget.

### Human required

The reference-host measurement is the required human-provided artifact. Finish
the implementation and unit tests before measuring. If the named host is
unavailable, stop before accepting performance criteria; do not substitute a
different host or change a budget.

Files owned by this phase:

```text
internal/crypt/envelope.go
internal/crypt/header.go
internal/crypt/keyring.go
internal/crypt/kdf.go
internal/crypt/*_test.go
scripts/encryption-perf/main.go
```

| Invariant                                                                | Mechanism                                        | Proof                                                            |
| ------------------------------------------------------------------------ | ------------------------------------------------ | ---------------------------------------------------------------- |
| A decoder rejects malformed or out-of-window input before expensive work | Bounded parser and KDF validation                | `TestEnvelopeRejectsMalformedHeaders` and `TestKDFLimitsAndGate` |
| Door changes do not alter payload ciphertext                             | Separate header and payload keys and transcripts | `TestDoorRewritePreservesPayload`                                |
| The same object key is required to open an envelope                      | Object-key-bound derivation and associated data  | `TestEnvelopeAuthenticatesPayload`                               |

## Integration contract

| Trigger                                    | Collaborators                           | Observable result                                                              | Required side effects | Prohibited side effects                                  |
| ------------------------------------------ | --------------------------------------- | ------------------------------------------------------------------------------ | --------------------- | -------------------------------------------------------- |
| Unit-test-only envelope and KDF operations | Fixed vectors and injected KDF gate     | The specified bytes round-trip or return the named error                       | None                  | Network, persistent key storage, or unbounded allocation |
| Performance harness invocation             | `internal/crypt` and the reference host | A report with all required statistics and a pass or fail for each phase budget | A local report only   | Store access or secret output                            |

## Acceptance criteria

| Criterion                                                                                     | Test or command                                                                         |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Fixed inputs produce the specified binary vectors                                             | `TestEnvelopeVectors`                                                                   |
| Tampering, truncation, trailing bytes, wrong object keys, and invalid commitments fail closed | `TestEnvelopeAuthenticatesPayload` and `TestEnvelopeRejectsMalformedHeaders`            |
| Candidate-door search respects input bounds and the injected KDF gate                         | `TestKeyringCandidateSearch` and `TestKDFLimitsAndGate`                                 |
| Door rewrite preserves the encrypted payload and rejects a changed door list                  | `TestDoorRewritePreservesPayload`                                                       |
| Secret size, KDF parameters, and current-envelope size obey this phase's parameters           | `TestSecretInputBounds`, `TestKDFParametersWritten`, and `TestCurrentEnvelopeSizeLimit` |
| Final implementation meets every performance budget in this phase                             | `go run ./scripts/encryption-perf` on the named reference host                          |

## Error coverage

| Failure                                                          | Expected outcome                                    | Test                                  |
| ---------------------------------------------------------------- | --------------------------------------------------- | ------------------------------------- |
| Unknown envelope version, kind, or KDF identifier                | Typed unsupported-format error before KDF work      | `TestEnvelopeRejectsMalformedHeaders` |
| Oversized, truncated, trailing, or internally inconsistent input | Typed malformed-envelope error                      | `TestEnvelopeRejectsMalformedHeaders` |
| KDF cost outside the accepted window                             | Refusal before derivation                           | `TestKDFLimitsAndGate`                |
| Wrong secret or altered wrapped key                              | No key is returned                                  | `TestKeyringCandidateSearch`          |
| Payload, header, or object-key tampering                         | Authentication error                                | `TestEnvelopeAuthenticatesPayload`    |
| Performance result exceeds any phase budget                      | Harness exits nonzero and reports the failed budget | `go run ./scripts/encryption-perf`    |

## Implementation notes

Not started.

## Review findings

None.
