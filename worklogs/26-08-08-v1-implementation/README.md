# slivingdoc v1 implementation worklog

**Status:** Not Started

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)

## Objective

Implement the standalone slivingdoc v1 MCP server described by the accepted
architecture. The result is a self-contained native executable and an npm
launcher that requires no Git installation.

## Status board

| Phase                                                                     | Status      | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ------------------------------------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| [1. Repository and native foundation](phase-1-native-foundation.md)       | Complete    | Create the Go repository and prove the narrow CGo/libgit2 build.                                                                                                                                                                                                                                                                                                                                                                           |
| [2. Git state engine](phase-2-git-engine.md)                              | Complete    | Native trees, commits, three-tree merge, incremental and checkpoint packs, shallow boundary.                                                                                                                                                                                                                                                                                                                                               |
| [3. S3 storage protocol](phase-3-s3-storage.md)                           | Complete    | Manifest, immutable objects, CAS, fake store, and MinIO contract; contract suite passes on the fake and MinIO.                                                                                                                                                                                                                                                                                                                             |
| [4. Managed workspaces](phase-4-workspaces.md)                            | Complete    | Path safety, os.Root scans, derived private keys, strict state.json, baseline authority, recovery mode, failpoints, and local operation locks.                                                                                                                                                                                                                                                                                             |
| [5. Notebook operations](phase-5-notebook-operations.md)                  | Complete    | Pull/commit orchestration: pack cache, three-tree merge, CAS retry and publication lookup, marker rejection, generic recovery, fake/native/MinIO suites.                                                                                                                                                                                                                                                                                   |
| [6. Checkpoints and scale](phase-6-checkpoints-scale.md)                  | Complete    | Stable-prefix checkpoints, retention, generation-fenced cleanup, stale-reader restart, MinIO checkpoint suite, and the sustained-load and cold/warm-reader benchmarks.                                                                                                                                                                                                                                                                     |
| [7. MCP application](phase-7-mcp-application.md)                          | Complete    | Two strict tools over stdio; config flags/env; startup probe; redacted diagnostics; in-memory, stdio-process, and shutdown tests.                                                                                                                                                                                                                                                                                                          |
| [8. Distribution and native releases](phase-8-distribution.md)            | Blocked     | All in-repository deliverables complete and validated (npm launcher, version injection, dependency matrix, release grammar and ref guards, caller workflow). The one remaining step is external: the reusable-workflow change sits complete in the separately owned `simple-go-pipeline` repository (branch `slivingdoc`, uncommitted) and needs a human review and commit there. No later phase depends on Phase 8; Phases 10-11 proceed. |
| [9. Documentation and quality gate](phase-9-quality-gate.md)              | Complete    | User README, isolated local MinIO example, architecture audit, and the full validation matrix; re-run after Phases 10 and 11, where it caught three pre-existing gate failures and the missing CI workflows.                                                                                                                                                            |
| [10. Integration test harness](phase-10-integration-test-harness.md)      | Complete    | Black-box MCP scenario suite validating the server contract of Phases 1–7; review 2 fixed three pre-existing gate failures and 16 harness and scenario defects.                                                                                                                                                                                                                                                                             |
| [11. Developer documentation and agent guide](phase-11-developer-docs.md) | Complete    | Verified slivingdoc AGENTS.md, the developer documentation set, and the README Development section; also recreated the CI workflows, which earlier phases recorded but never committed.                                                                                                                                                                                                                                                     |

## Strategy

### Execution order

Execute phases in numeric order. Phase 3 can begin after Phase 1 while Phase 2
continues, but Phase 5 requires Phases 2 through 4. All later dependencies are
strict. Phase 10 requires Phases 1 through 7 and validates their server
contract as a black box. Phase 11 requires Phases 1 through 10 and writes AGENTS.md and the developer documentation set from the implemented repository. The Phase 9 gate re-runs after Phase 10 and after Phase 11.

Phase 8 is marked **Blocked** on the status board: its only remaining
deliverable is a human commit in the separately owned `simple-go-pipeline`
repository, which worker sessions cannot perform (git is read-only). No phase
depends on Phase 8, so Phases 10 and 11 proceed without it; the blocked status
exists only to keep that one human step visible.

An executing agent reads this README, its active phase file, and the architecture
sections and line numbers in the table below. It does not infer protocol details
or read another phase file. Shared design rules stay here. Phase files contain
phase-specific contracts.

| Phase | Required architecture sections (line)                                              |
| ----- | ---------------------------------------------------------------------------------- |
| 1     | 5 (L131), 8.1 (L307), 19 (L1144), 21 (L1215)                                       |
| 2     | 7.1 (L188), 8 (L305), 9.3 (L537), 12 (L763), 13.2 (L835)                           |
| 3     | 9 (L386), 14 (L893), 15 (L958)                                                     |
| 4     | 7 (L186), 18.2 (L1131)                                                             |
| 5     | 2 (L26), 4 (L116), 8.2 (L333), 10–15 (L603–998)                                    |
| 6     | 9.2 (L423), 13–16 (L813–998)                                                       |
| 7     | 2 (L26), 17 (L1040), 18 (L1115)                                                    |
| 8     | 21 (L1215)                                                                         |
| 9     | All sections (L9–1366)                                                             |
| 10    | 2 (L26), 7 (L186), 10–18 (L603–1115), 20 (L1169)                                   |
| 11    | 1 (L9), 4 (L116), 10–14 (L603–998), 17 (L1040), 19 (L1144), 20 (L1169), 25 (L1351) |

Line numbers refer to [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md).
Re-verify them after any architecture edit.

### Shared invariants

Every phase must preserve these architecture rules:

1. MCP is the only public API.
2. The process never invokes Git and never imports `git2go`.
3. All CGo and libgit2 types stay inside `internal/git2`.
4. S3 access stays behind the semantic object-store boundary.
5. `current` is the only accepted-state authority.
6. Pack objects are immutable and precede any manifest reference to them.
7. Publication uses conditional ETag replacement without a writer lock.
8. A failed CAS causes a bounded merge-and-retry cycle.
9. The notebook accepts valid UTF-8 text files without U+0000 only.
10. Pull, conflict, and anomaly recovery can rewrite the visible directory.
    Unexpected partial mutation returns `RECOVERY_FAILURE`, attempts an
    authoritative resync, and never returns `OK` for that call.
11. Checkpoint or cleanup failure cannot change commit success.
12. Git history is internal and can become shallow at a checkpoint.
13. External services are replaceable by deterministic test doubles.
14. Required MinIO tests use Go testcontainers and run in CI.
15. Checkpoint packs contain complete state and use no external pack-delta base objects.
16. Increment packs depend only on the checkpoint and ordered tail in their observed manifest.
17. ETags coordinate `current`. SHA-256 and size validate pack content.
18. Active and retained manifest data are the only cleanup roots; a successful
    checkpoint cutoff bounds every cleanup candidate.

### Planned package ownership

```text
main.go
internal/app
internal/mcp
internal/notebook
internal/workspace
internal/git
internal/git2
internal/storage
internal/s3store
internal/integrationtest  (test-only black-box scenario suite)
npm/slivingdoc
```

No package exists only to forward a function. Interfaces belong to the package
that consumes them, unless a shared protocol type requires a neutral package.

### Pinned implementation baseline

Use these reviewed versions until an explicit dependency update changes them:

| Dependency             | Version                                                            |
| ---------------------- | ------------------------------------------------------------------ |
| Go                     | `1.26.5`                                                           |
| Node.js                | `24.19.0`                                                          |
| libgit2                | `v1.9.6`                                                           |
| libgit2 source SHA-256 | `a88a42a4ea9bdab7aa8686eead3bf7d9c6dd74529caca16ab22eaa92433d31d9` |
| Go MCP SDK             | `v1.7.0`                                                           |
| AWS SDK core           | `v1.43.4`                                                          |
| AWS SDK S3             | `v1.107.0`                                                         |
| testcontainers-go      | `v0.44.0`                                                          |
| MinIO image            | `minio/minio:RELEASE.2025-09-07T16-13-09Z`                         |
| gofumpt                | `v0.11.0`                                                          |
| staticcheck            | `v0.7.0`                                                           |
| Unicode text support   | `golang.org/x/text v0.40.0`                                        |
| Local file locks       | `github.com/gofrs/flock v0.13.0`                                   |

Pin all Go modules in `go.mod` and `go.sum`. Pin CI actions by full commit SHA.
Dependency updates require the same component, integration, native, and license
gates as the original phase.

### Test doctrine

Use test-driven implementation. Run the narrow relevant tests before and after
each change. Record actual commands and results in the active phase.

The complete test layers are:

| Layer       | Required evidence                                                                               |
| ----------- | ----------------------------------------------------------------------------------------------- |
| Unit        | Deterministic edge cases and error mapping without Docker or network access.                    |
| Component   | Real libgit2 behavior in temporary directories.                                                 |
| Contract    | One suite against the fake object store and the MinIO adapter.                                  |
| Integration | Real notebook publication against MinIO through testcontainers.                                 |
| Scenario    | Black-box MCP request/response evidence for every server usecase; the behavioral contract.      |
| Protocol    | In-memory MCP tests and real stdio process tests.                                               |
| Release     | Native binaries start on their target operating systems and have no libgit2 runtime dependency. |

Docker-backed tests can skip locally only when they name the unavailable Docker
dependency. The CI integration job must treat a skipped MinIO suite as failure.

Tests must not use live AWS accounts. Tests must not modify cloud resources.

### Standard commands

The repository does not contain code yet. Phase 1 establishes exact tool
versions and Make targets. The expected final commands are:

```text
go test -race -timeout=30s -count=3 ./...
go vet ./...
go fix -diff ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go run mvdan.cc/gofumpt@v0.11.0 -w -l .
npm test --prefix npm/slivingdoc
make native-smoke
```

### Storage protocol summary

The active manifest contains one checkpoint and an ordered increment tail.
Every descriptor carries a Git head, pack key, byte size, and SHA-256 checksum.

Writers upload a proposal pack and then conditionally replace `current`. A
precondition failure is normal contention. The operation reads the new state,
merges, and retries with randomized backoff.

At the configured count threshold, a worker compacts a stable accepted prefix.
It retains later increments and updates the manifest through the same CAS.

### Error taxonomy

Stable internal categories must map to stable MCP errors:

| Category             | Retryable | Meaning                                                             |
| -------------------- | --------- | ------------------------------------------------------------------- |
| `INVALID_REQUEST`    | No        | Invalid path, message, file, or malformed input.                    |
| `CONTENT_CONFLICT`   | No        | A merge conflicts or L contains a complete conflict-marker block.   |
| `REMOTE_BUSY`        | Yes       | Bounded CAS retries were exhausted.                                 |
| `STORAGE_FAILURE`    | Yes       | An S3 operation failed without a known accepted result.             |
| `STORAGE_INTEGRITY`  | No        | A manifest or pack failed validation.                               |
| `RECOVERY_FAILURE`   | Yes       | The service cannot restore L or P from authoritative R immediately. |
| `INCOMPATIBLE_STORE` | No        | The startup S3 capability probe failed.                             |

Error text can change. Error category and structured conflict paths are stable.

### Review severity

| Severity | Meaning                                                                         | Phase effect                                |
| -------- | ------------------------------------------------------------------------------- | ------------------------------------------- |
| Critical | Data loss, silent lost update, credential exposure, or unsafe release artifact. | Reopen and block dependent phases.          |
| Major    | Acceptance contract or architecture invariant is not met.                       | Reopen the phase.                           |
| Minor    | Local maintainability or documentation defect with no contract failure.         | Record and fix without mandatory reopening. |

Every review appends findings to the phase file. Critical and major findings
change that phase to `Reopened (review N)` and update this status board.

## Decisions

| Date       | Decision                                                                                                                                                                                                                                                                                                                                                                                                                        | Reason                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-08 | Use a narrow CGo wrapper around pinned libgit2.                                                                                                                                                                                                                                                                                                                                                                                 | Users receive mature merge behavior without installing Git.                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| 2026-08-08 | Do not use `git2go`.                                                                                                                                                                                                                                                                                                                                                                                                            | Its released binding tracks an old libgit2 and exposes more surface than needed.                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| 2026-08-08 | Use immutable incremental packs and one indexed manifest.                                                                                                                                                                                                                                                                                                                                                                       | This avoids full-history upload for every small commit.                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| 2026-08-08 | Use ETag CAS without an S3 writer lock.                                                                                                                                                                                                                                                                                                                                                                                         | Merge and upload work can proceed concurrently.                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| 2026-08-08 | Checkpoint after a configurable pack count, default 1,024.                                                                                                                                                                                                                                                                                                                                                                      | This bounds cold-start requests while measurements remain possible.                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| 2026-08-08 | Retain one previous checkpoint generation by default.                                                                                                                                                                                                                                                                                                                                                                           | Stale readers can restart while old physical state has bounded retention.                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 2026-08-08 | Support UTF-8 text files without U+0000 only.                                                                                                                                                                                                                                                                                                                                                                                   | Agent notes need predictable text merge and conflict-marker behavior.                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 2026-08-09 | Rebase local changes during pull.                                                                                                                                                                                                                                                                                                                                                                                               | The visible directory is a rewriteable working area, not authoritative state.                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| 2026-08-09 | Use operation-boundary L/P/R synchronization.                                                                                                                                                                                                                                                                                                                                                                                   | File watchers add no correctness value for the two-tool workflow.                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| 2026-08-09 | Use the empty tree for first pull.                                                                                                                                                                                                                                                                                                                                                                                              | Existing valid files become local additions instead of being discarded.                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| 2026-08-09 | Reject complete conflict-marker blocks on commit.                                                                                                                                                                                                                                                                                                                                                                               | No accepted state can contain an unresolved slivingdoc conflict block.                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| 2026-08-09 | Return failure when ambiguous acceptance cannot be proved.                                                                                                                                                                                                                                                                                                                                                                      | Automatic replay can repeat or reverse a change after later remote edits.                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 2026-08-09 | Use UUIDv7 for publication and checkpoint IDs.                                                                                                                                                                                                                                                                                                                                                                                  | One standard format gives unique, key-safe identifiers.                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| 2026-08-09 | Use JSON `uint64` manifest generations.                                                                                                                                                                                                                                                                                                                                                                                         | The range exceeds the expected workload without string encoding.                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| 2026-08-09 | Make manifest version 1 strict and normative.                                                                                                                                                                                                                                                                                                                                                                                   | All processes must interpret the authoritative state in the same way.                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 2026-08-09 | Store receipts in active and retained descriptors.                                                                                                                                                                                                                                                                                                                                                                              | One retention control replaces a separate receipt-retention mechanism.                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| 2026-08-09 | Fence garbage collection by checkpoint cutoff.                                                                                                                                                                                                                                                                                                                                                                                  | Old proposals are collected without leases or a writer registry.                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| 2026-08-09 | Retain complete prior descriptor chains.                                                                                                                                                                                                                                                                                                                                                                                        | Each retained generation reconstructs the exact replaced state.                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| 2026-08-09 | Use one generic anomaly-recovery path with failpoints.                                                                                                                                                                                                                                                                                                                                                                          | The design exposes rare failures without separate logic for every interruption.                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| 2026-08-08 | Use testcontainers MinIO for required S3 integration.                                                                                                                                                                                                                                                                                                                                                                           | The protocol needs real HTTP conditional-write evidence.                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| 2026-08-09 | Drive the Phase 10 scenarios through in-process MCP JSON-RPC.                                                                                                                                                                                                                                                                                                                                                                   | The public API is the black box; failpoints and fault injection live in harness wiring; a curated subset re-runs over real stdio processes.                                                                                                                                                                                                                                                                                                                                                                                   |
| 2026-08-09 | Defer MCP-level load tests pending bottleneck attribution.                                                                                                                                                                                                                                                                                                                                                                      | Local MinIO, the race detector, and CI noise obscure whether a bad number is slivingdoc; Phase 6 keeps the interim sustained-load harness.                                                                                                                                                                                                                                                                                                                                                                                    |
| 2026-08-09 | Review 1 resolved three contract seams in the phase specs.                                                                                                                                                                                                                                                                                                                                                                      | Findings R1-01..R1-03 (all Major) were fixed before any implementation existed.                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| 2026-08-09 | libgit2 merge-file drops the label of an empty side; the policy formats deleted-side conflicts with exact markers.                                                                                                                                                                                                                                                                                                              | A modify/delete conflict would otherwise end in a bare `>>>>>>>` that violates the exact-marker contract; the policy output matches Git's own merge-file behavior.                                                                                                                                                                                                                                                                                                                                                            |
| 2026-08-09 | Detect file-versus-directory conflicts from resolved entries below the conflicted path.                                                                                                                                                                                                                                                                                                                                         | libgit2 represents a D/F replacement as a lone blob stage plus resolved children; the policy reports it without marker content.                                                                                                                                                                                                                                                                                                                                                                                               |
| 2026-08-09 | `MarkShallow` writes the gitdir `shallow` file and refreshes grafts via `git_repository_is_shallow`.                                                                                                                                                                                                                                                                                                                            | Git's own file format survives process restarts; the refresh keeps the open session consistent with a reopened repository.                                                                                                                                                                                                                                                                                                                                                                                                    |
| 2026-08-09 | The writepack vtable requires a non-NULL progress struct on append and commit.                                                                                                                                                                                                                                                                                                                                                  | libgit2's indexer asserts `stats` non-NULL; the C forwarders pass a local `git_indexer_progress`.                                                                                                                                                                                                                                                                                                                                                                                                                             |
| 2026-08-09 | Remove the streamable HTTP transport from v1.                                                                                                                                                                                                                                                                                                                                                                                   | stdio is the only transport; HTTP would require remote file editing, which recreates the synchronization problem slivingdoc solves.                                                                                                                                                                                                                                                                                                                                                                                           |
| 2026-08-09 | Add AWS SDK v2 (core v1.43.4, s3 v1.107.0) and testcontainers-go v0.44.0 as pinned direct dependencies.                                                                                                                                                                                                                                                                                                                         | The object-store adapter and the MinIO contract suite need the pinned AWS and container tooling; versions match the implementation baseline.                                                                                                                                                                                                                                                                                                                                                                                  |
| 2026-08-09 | Use google/uuid v1.6.0 for UUIDv7 generation; storage owns strict canonical-form validation.                                                                                                                                                                                                                                                                                                                                    | The library is already in the module graph via testcontainers; generation is battle-tested while the manifest validator still enforces canonical lowercase RFC 9562 version-7 form.                                                                                                                                                                                                                                                                                                                                           |
| 2026-08-09 | The object-store interface takes protocol keys; each adapter owns the configured prefix join.                                                                                                                                                                                                                                                                                                                                   | Architecture 9.1 says the adapter joins prefix and key; cleanup keys returned by LIST round-trip back into DELETE without prefix bookkeeping.                                                                                                                                                                                                                                                                                                                                                                                 |
| 2026-08-09 | UploadUnique resolves ambiguous uploads by read-back verification, never by error classification.                                                                                                                                                                                                                                                                                                                               | A transport failure may or may not have landed: matching bytes mean success, absence means never-landed storage failure, mismatch means integrity error and no overwrite.                                                                                                                                                                                                                                                                                                                                                     |
| 2026-08-09 | The fake store and the shared contract suite live in `internal/storage/fake` and `internal/storage/contract`.                                                                                                                                                                                                                                                                                                                   | Phase 10's in-process harness reuses the deterministic fake; one suite runs against both the fake and MinIO.                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 2026-08-09 | The startup probe is a storage-policy function over interface primitives.                                                                                                                                                                                                                                                                                                                                                       | Deliberately broken wrapper stores prove the probe detects ignored If-None-Match, If-Match, and read-after-write conditions.                                                                                                                                                                                                                                                                                                                                                                                                  |
| 2026-08-09 | MinIO tests start one container per `go test` invocation and isolate subtests by prefix.                                                                                                                                                                                                                                                                                                                                        | Three counts of a multi-container suite would exceed the strict 30-second gate; one container keeps the gate fast and deterministic.                                                                                                                                                                                                                                                                                                                                                                                          |
| 2026-08-09 | Add Phase 11 for AGENTS.md and developer documentation.                                                                                                                                                                                                                                                                                                                                                                         | The existing AGENTS.md is a stale copy of the Sakfråga guide; Phase 9 owns user documentation and Phase 11 owns developer and agent documentation written from the implemented repository.                                                                                                                                                                                                                                                                                                                                    |
| 2026-08-09 | Build libgit2 with bundled zlib and regex backends.                                                                                                                                                                                                                                                                                                                                                                             | `USE_BUNDLED_ZLIB=ON` and `REGEX_BACKEND=builtin` keep the Linux runtime dependency list inside the baseline (no libz, no libpcre2).                                                                                                                                                                                                                                                                                                                                                                                          |
| 2026-08-09 | Phase 1 creates only package roots with real content.                                                                                                                                                                                                                                                                                                                                                                           | Empty `mcp`, `notebook`, `workspace`, `storage`, and `s3store` directories would be dead code; their phases create them.                                                                                                                                                                                                                                                                                                                                                                                                      |
| 2026-08-09 | The non-CGo stub reports `ErrUnavailable` on every operation, including Close.                                                                                                                                                                                                                                                                                                                                                  | A stub that silently succeeds would hide a missing native boundary.                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| 2026-08-09 | The engine Open refuses a runtime version that differs from pinned v1.9.6.                                                                                                                                                                                                                                                                                                                                                      | A binary linked against a different ABI must fail at startup, not later.                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| 2026-08-09 | The exact pinned feature set is locked by a test.                                                                                                                                                                                                                                                                                                                                                                               | `TestFeaturesReflectPinnedBuild` fails on configuration drift such as an accidental transport enablement.                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 2026-08-09 | Re-pin the MinIO image to `minio/minio:RELEASE.2025-09-07T16-13-09Z`.                                                                                                                                                                                                                                                                                                                                                           | The originally pinned `RELEASE.2025-10-15T17-29-55Z` tag does not exist on Docker Hub (`docker pull` and the registry API both return not-found); the pin must name a pullable image so CI can run the contract suite. `2025-09-07` is the newest published release and shares the digest of `minio/minio:latest`.                                                                                                                                                                                                            |
| 2026-08-09 | Extract the strict JSON value tree into the neutral `internal/strictjson` package.                                                                                                                                                                                                                                                                                                                                              | The workspace `state.json` needs the same strict grammar as the manifest plus booleans; one shared parser replaces two verbatim copies.                                                                                                                                                                                                                                                                                                                                                                                       |
| 2026-08-09 | Replace L by moving it aside into P, renaming the staged tree into place, then removing the backup; a cross-device rename falls back to per-file temp-rename copies.                                                                                                                                                                                                                                                            | The rename path is atomic and breaks hard links; the copy fallback keeps the contract on heterogenous filesystems.                                                                                                                                                                                                                                                                                                                                                                                                            |
| 2026-08-09 | Open degrades to recovery-required mode on corrupt, mismatched, or interrupted state and on a missing or corrupt repository, instead of failing.                                                                                                                                                                                                                                                                                | Recovery must be runnable from any broken local state; P is a server-owned cache rebuildable from `current`.                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 2026-08-09 | A leftover `state.json.tmp` at Open forces recovery.                                                                                                                                                                                                                                                                                                                                                                            | The record write is file-sync plus atomic rename; a crash between them can lose the durable `recoveryRequired` flag, and the temp file is the only evidence.                                                                                                                                                                                                                                                                                                                                                                  |
| 2026-08-09 | The in-process per-path serialization is a context-aware semaphore; the cross-process serialization is the flock file.                                                                                                                                                                                                                                                                                                          | A mutex cannot be canceled; the acceptance criterion requires prompt cancellation of a waiting caller.                                                                                                                                                                                                                                                                                                                                                                                                                        |
| 2026-08-09 | Workspace syscall needs use `golang.org/x/sys/unix` and `golang.org/x/sys/windows` behind build tags; the subprocess lock test spawns the test binary via `os.StartProcess`.                                                                                                                                                                                                                                                    | The phase-2 seam scan forbids `os/exec` and `syscall` imports anywhere in the module as the no-Git-invocation proof; test-only process spawning must not weaken it.                                                                                                                                                                                                                                                                                                                                                           |
| 2026-08-10 | Phase 5 notebook tests use the REAL workspace over a fake engine; only the Git engine and the object store are faked.                                                                                                                                                                                                                                                                                                           | The workspace is phase-4 production code; faking it would test the notebook against its own mirror instead of the real mutation and recovery contract.                                                                                                                                                                                                                                                                                                                                                                        |
| 2026-08-10 | A conflicting pull also writes the `<P>/pulled` marker.                                                                                                                                                                                                                                                                                                                                                                         | The phase spec requires a successful OR conflicting pull to initialize P for the first commit; the conflict branch originally skipped the marker and the acceptance test caught it.                                                                                                                                                                                                                                                                                                                                           |
| 2026-08-10 | The notebook fake repository implements a real file-level three-tree merge and a self-contained binary pack format.                                                                                                                                                                                                                                                                                                             | Two fake repositories must transfer objects exactly like real packs so the orchestration tests prove the protocol without CGo.                                                                                                                                                                                                                                                                                                                                                                                                |
| 2026-08-10 | An increment's target generation continues the active chain from the checkpoint cutoff (`cutoff + tail length + 1`), never from the manifest generation counter.                                                                                                                                                                                                                                                                | The counter also advances on checkpoint replacements, so after a compaction it diverges from the chain position; the manifest validator rejected the counter-based proposals.                                                                                                                                                                                                                                                                                                                                                 |
| 2026-08-10 | The fake repository grafts declared shallow roots to zero parents at `ReadCommit`, mirroring libgit2's shallow-file behavior.                                                                                                                                                                                                                                                                                                   | The native boundary already grafts; without the mirror the deterministic suite could not prove extension after a shallow checkpoint.                                                                                                                                                                                                                                                                                                                                                                                          |
| 2026-08-10 | The Phase 6 load harness lives in `internal/notebook` test files over the fake store and fake engine; the benchmarks record throughput, latency percentiles, CAS attempts per accepted commit, conflicts, and failures for the distributed and burst schedules.                                                                                                                                                                 | The decision-log deferral keeps MCP-level load tests out of Phase 6; the interim sustained-load harness measures the planning workload deterministically without Docker or CI noise.                                                                                                                                                                                                                                                                                                                                          |
| 2026-08-10 | The MinIO stale-reader restart test gates the reader's pack download with a store wrapper so a checkpoint and its cleanup interleave deterministically over real HTTP.                                                                                                                                                                                                                                                          | Real HTTP offers no barrier primitive; the wrapper reproduces the mid-download deletion race that the fake barrier test proves, on the real store.                                                                                                                                                                                                                                                                                                                                                                            |
| 2026-08-10 | `internal/mcp` owns the diagnostic redactor (`Redact`), which scrubs pack keys, the probe key, Git IDs, derived keys, AWS access keys, and URL user information; the app reuses it for startup diagnostics.                                                                                                                                                                                                                     | One redaction policy serves the tool-error envelope and the process stderr; the app imports mcp anyway for the server.                                                                                                                                                                                                                                                                                                                                                                                                        |
| 2026-08-10 | `s3store.Options.ForcePathStyle` selects path-style addressing without a custom base endpoint (`--path-style`).                                                                                                                                                                                                                                                                                                                 | Custom endpoints always use path style; the flag extends the same addressing to the default AWS endpoint.                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 2026-08-10 | The app process carries a `storeFactory` seam so tests substitute the deterministic fake store and the startup-refusal helpers substitute a failing store.                                                                                                                                                                                                                                                                      | The process body must prove the compatibility probe without network or Docker access.                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 2026-08-10 | The stdio process tests intercept the child via `TestMain` on the `SLIVINGDOC_PROCESS_HELPER` variable and spawn the test binary with `os.StartProcess`.                                                                                                                                                                                                                                                                        | The child must run the real process body over real pipes; `os/exec` and `syscall` are forbidden by the phase-2 seam scan.                                                                                                                                                                                                                                                                                                                                                                                                     |
| 2026-08-10 | The shutdown deadline is a `process` field (default 30s) so tests can force deadline expiry.                                                                                                                                                                                                                                                                                                                                    | A fixed constant would make the bounded-shutdown test wait 30 seconds per run.                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| 2026-08-10 | `mcp.mapError` maps an unrecognized service error to a retryable `STORAGE_FAILURE` instead of leaking it.                                                                                                                                                                                                                                                                                                                       | No unknown failure can escape as a non-domain error; the category is stable and the message is generic.                                                                                                                                                                                                                                                                                                                                                                                                                       |
| 2026-08-10 | The app's signal path cancels in-flight MCP requests by closing the live transport connection.                                                                                                                                                                                                                                                                                                                                  | The SDK cancels request-handler contexts only on transport read/write failure (not on connection-context cancellation for persistent transports); closing the connection makes handlers unwind, and the bounded shutdown deadline still forces a nonzero exit for handlers that ignore cancellation.                                                                                                                                                                                                                          |
| 2026-08-10 | The app owns a `closeTransport` wrapper around the SDK transport that records the live connection for the signal path.                                                                                                                                                                                                                                                                                                          | The process body must terminate the session from outside the SDK's `Server.Run`; the wrapper exposes the connection without changing the server code.                                                                                                                                                                                                                                                                                                                                                                         |
| 2026-08-10 | The stdio process test spawns the test binary with `os.StartProcess` and passes the pipe ends in the correct order: the child's stdin is the pipe read end and the child's stdout is the pipe write end.                                                                                                                                                                                                                        | `os.Pipe` returns the read end first; a swapped stdout end made the child's fd 1 a read-only pipe (EBADF on every protocol write).                                                                                                                                                                                                                                                                                                                                                                                            |
| 2026-08-10 | `make integration-test` writes `go test -json` output to `.build/integration-test.json` and then runs `scripts/check-integration-skips.sh` on it.                                                                                                                                                                                                                                                                               | A pipeline that tee'd the JSON masked the test exit code; the two-step recipe fails on a broken build and on any skipped Docker-backed test.                                                                                                                                                                                                                                                                                                                                                                                  |
| 2026-08-10 | An increment pack and the checkpoint pack of the same head are byte-identical (same sorted object set); corruption fixtures must pull through a cold reader with an empty cache.                                                                                                                                                                                                                                                | The pack cache is keyed by content SHA-256, so a warm reader legitimately satisfies a checkpoint descriptor from the byte-identical increment cached earlier.                                                                                                                                                                                                                                                                                                                                                                 |
| 2026-08-10 | CAS race tests force the loss with a one-shot injected precondition failure instead of a true time race; the MinIO suite runs the true concurrent race.                                                                                                                                                                                                                                                                         | A deterministic barrier proves one winner and one retry; real HTTP CAS convergence is proven separately against MinIO.                                                                                                                                                                                                                                                                                                                                                                                                        |
| 2026-08-10 | `make integration-test` now also runs `./internal/notebook/` and depends on the libgit2 build stamp; the CI integration job builds libgit2 first.                                                                                                                                                                                                                                                                               | The notebook MinIO suite needs real git2 plus real HTTP conditional writes; the gate keeps failing on any skip.                                                                                                                                                                                                                                                                                                                                                                                                               |
| 2026-08-10 | The npm launcher is a zero-dependency Node package (built-in modules only); `npm test --prefix npm/slivingdoc` needs no `npm install`.                                                                                                                                                                                                                                                                                          | The launcher is small enough to avoid a dependency tree; deterministic tests need only `node:test`, `node:http`, and friends, so the package is easier to audit and the CI job is one step.                                                                                                                                                                                                                                                                                                                                   |
| 2026-08-10 | The launcher cache root prefers `SLIVINGDOC_CACHE`, then `npm_config_cache` (set by npm for lifecycle scripts), then the OS user cache directory; entries live under `_slivingdoc/<version>/<os>/<arch>/<asset>` with a `<asset>.sha256` sidecar.                                                                                                                                                                               | Architecture 21 says the cache is npm-managed and version/OS/architecture/asset-specific; the explicit override and the npm-config fallback keep the tests and mirrors deterministic without a second cache layout.                                                                                                                                                                                                                                                                                                           |
| 2026-08-10 | The launcher trusts a cache entry only when its recorded sidecar checksum still matches the bytes; a corrupt entry is deleted and re-fetched, and a fresh install verifies the binary against the release `SHA256SUMS` before the atomic rename.                                                                                                                                                                                | A sidecar verified against the authoritative release at install time avoids re-downloading `SHA256SUMS` on every run (the no-unnecessary-download contract) while still refusing any corrupt or partial cache entry.                                                                                                                                                                                                                                                                                                          |
| 2026-08-10 | The launcher parser accepts exactly the architecture 21 `SHA256SUMS` grammar (LF-terminated, lowercase 64-hex, two spaces, unique names) and rejects everything else.                                                                                                                                                                                                                                                           | The launcher must never verify against a checksum list it cannot parse exactly; strictness on both producer and consumer sides locks the grammar.                                                                                                                                                                                                                                                                                                                                                                             |
| 2026-08-10 | `internal/app.Version` changed from a constant to a variable so release builds can inject the tag-derived version with `-ldflags -X`; `make build` passes it and the smoke gate greps the injected value.                                                                                                                                                                                                                       | The release pipeline must derive version values from the release tag (architecture 21); the linker cannot rewrite a constant.                                                                                                                                                                                                                                                                                                                                                                                                 |
| 2026-08-10 | The external `simple-go-pipeline` change cannot be merged by a worker session (git is read-only); the complete proposal lives in `worklogs/26-08-08-v1-implementation/simple-go-pipeline-release-proposal.md` and the caller workflow keeps a placeholder SHA that GitHub refuses at dispatch.                                                                                                                                  | The phase must not be marked complete on an unmerged proposal; the unresolvable ref plus `scripts/check-release-ref.sh` prevent a broken release from running while the phase is in progress.                                                                                                                                                                                                                                                                                                                                 |
| 2026-08-10 | The npm package license field is `SEE LICENSE IN NOTICE` and the package ships the libgit2 notice; the GitHub release attaches the repository `NOTICE` as an asset covered by `SHA256SUMS`.                                                                                                                                                                                                                                     | The repository has no own license file, and the required libgit2/binding notices must ship with both artifacts; the release check requires `NOTICE` so publication cannot skip it.                                                                                                                                                                                                                                                                                                                                            |
| 2026-08-10 | The publication gate (`prepublishOnly`) verifies every required artifact, `SHA256SUMS`, and `NOTICE` by HEAD against the release tag before `npm publish`; `npm pack` is deliberately not gated.                                                                                                                                                                                                                                | npm runs `prepublishOnly` only on publish, so local tarball builds stay offline while publication cannot precede the complete GitHub release.                                                                                                                                                                                                                                                                                                                                                                                 |
| 2026-08-10 | The Windows dependency allowlist is a documented system-DLL set enforced case-insensitively; the macOS check allows only `/usr/lib` and `/System/Library`.                                                                                                                                                                                                                                                                      | Architecture 21 names the baselines; the allowlist is locked by the first real Windows release run, and the `--check` self-tests prove the rejection paths without the target toolchain.                                                                                                                                                                                                                                                                                                                                      |
| 2026-08-10 | `scripts/build-libgit2.sh` falls back from `sha256sum` to macOS `shasum`, uses `getconf _NPROCESSORS_ONLN`, and passes `--config Release`.                                                                                                                                                                                                                                                                                      | One script must build the pinned libgit2 on the Linux, macOS, and Windows target runners; the Linux path was re-proven by a full rebuild from the cached tarball.                                                                                                                                                                                                                                                                                                                                                             |
| 2026-08-10 | The pinned Go toolchain is upgraded from 1.26.3 to 1.26.5 in `go.mod` and in the CI and release workflows.                                                                                                                                                                                                                                                                                                                      | The Phase 9 vulnerability gate (`govulncheck v1.1.4`) reported four reachable standard-library vulnerabilities in 1.26.3, including GO-2026-4970 (os.Root symlink escape via a trailing slash), which touches the workspace path-safety boundary; 1.26.5 is the patch release that fixes them, and the full gate re-ran on the new toolchain.                                                                                                                                                                                 |
| 2026-08-10 | The indirect `github.com/klauspost/compress` dependency is upgraded from v1.18.6 to v1.18.7.                                                                                                                                                                                                                                                                                                                                    | The vulnerability gate reported GO-2026-5841 (s2 OOB read, fixed in v1.18.7); the module is reachable through testcontainers/moby archive compression (zstd), so the patch upgrade closes the finding.                                                                                                                                                                                                                                                                                                                        |
| 2026-08-10 | GO-2026-5932 (`golang.org/x/crypto/openpgp` unmaintained, no fix) is recorded as a reviewed, unreachable module advisory.                                                                                                                                                                                                                                                                                                       | `go mod why golang.org/x/crypto` proves the main module imports no package of that module (the requirement comes from testcontainers), so no code path can reach the vulnerable package; the result is recorded rather than blocking.                                                                                                                                                                                                                                                                                         |
| 2026-08-10 | The integration harness reaches the app through exported seams: `app.ServiceConfig`, `app.ServiceHooks` (workspace and notebook failpoints), `app.NewService(engine, store, cfg, hooks)`, `app.StoreFactory`, and `app.RunProcess(engine, ProcessOptions)`; `Run` delegates to `RunProcess` with the production defaults, and the harness skips the startup probe (it lives in `buildServer`), so store counters start at zero. | The harness must wire the server exactly as production does without importing cgo internals or the AWS adapter; the exported seam is the same configuration the process flags resolve, and the probe is a process-body concern the in-memory scenarios deliberately do not re-run.                                                                                                                                                                                                                                            |
| 2026-08-10 | The MCP handler generates its own 16-hex-char correlation ID per tool call and logs a started/completed pair with `mcpReqID`, `tool`, `duration`, and `outcome`; the same request-scoped logger is attached to the request context through `notebook.WithLogger` so checkpoint and cleanup warnings share the correlation ID.                                                                                                   | The SDK v1.7.0 hides the wire JSON-RPC ID from handlers (unexported `idContextKey`); the correlation ID is the harness's log-scoping key, and the notebook warnings are the observability the cleanup-failure catalog row requires.                                                                                                                                                                                                                                                                                           |
| 2026-08-10 | The notebook owns the request-context logger key (`notebook.WithLogger`/`LoggerFrom`, discard default); `runCheckpoint` and `cleanup` failure branches emit Warn records through it.                                                                                                                                                                                                                                            | Checkpoint and cleanup are best-effort background efforts; their failures must be observable in the scoped harness logs without touching the commit result or the metrics counters.                                                                                                                                                                                                                                                                                                                                           |
| 2026-08-10 | The harness store stack is raw (MinIO s3store or fake) under a fault-injecting wrapper under a recorder; the app serves the recorder, assertions read the raw store, and per-test prefixes plus per-test workspace/private roots give parallel isolation.                                                                                                                                                                       | Injected faults (corrupt reads, missing objects) must not poison the harness's own manifest assertions; the recorder proves zero-mutation rules and bounded retry cycles; the fake and MinIO share one fault code path.                                                                                                                                                                                                                                                                                                       |
| 2026-08-10 | Two-writer and checkpoint-worker races use named sessions on DISTINCT request paths under one service (distinct derived keys, no op-lock contention); the DSL barrier synchronizes sessions before the racing calls.                                                                                                                                                                                                            | The app's per-path workspace op lock serializes same-path calls, which would make a CAS race impossible; distinct paths share the bucket prefix and race on the real conditional write.                                                                                                                                                                                                                                                                                                                                       |
| 2026-08-10 | The fault wrapper adds op-level and key-prefix injections (`FailNextOp`, `AmbiguousNextOp`, `BlockPrefix`/`ReleasePrefix`/`WaitingPrefix`) beside the exact-key ones.                                                                                                                                                                                                                                                           | Pack keys embed random publication UUIDs generated inside the notebook, so scenarios cannot name them in advance; the prefix barrier lets a checkpoint build interleave with independent writers without blocking the writers' own increment puts.                                                                                                                                                                                                                                                                            |
| 2026-08-10 | `refreshRoot` reopens the workspace root handle at the CONFIGURED workspace root (`os.OpenRoot(wsRoot)`), not at the visible path; the workspace stores the canonical workspace root for this purpose.                                                                                                                                                                                                                          | Reopening at the visible path re-rooted the handle for any request path below the root, so every later scan resolved `w.rel` against the wrong root and returned an empty snapshot (the visible files vanished after the first materialize; the Phase 5 first-pull test and the workspace Diff/Accept round-trip tests exposed it). Reopening at the workspace root is correct for both cases: for rel=="." the visible path IS the root (fresh inode after the replacement), and below the root the root inode is unchanged. |
| 2026-08-11 | Scenarios run with `t.Parallel()`; per-test S3 prefixes, workspace roots, private roots, recorders, and log captures are the isolation. | The strict race gate (`-race -count=3 -timeout=30s`) timed out at 26.5 s per count while the suite ran sequentially; the isolation the phase specifies already existed and only needed to be used. The same gate now takes 13.6 s. |
| 2026-08-11 | A scenario runs against real MinIO when its contract is about real HTTP conditional writes (both writer races, competing checkpoint workers, stale-reader restart, cleanup after a successful checkpoint); every other scenario runs against the deterministic fake. | `internal/storage/contract` is one suite executed against both the fake and the adapter, so the fake's conditional writes, ETags, and error mapping are proven equivalent. Running the whole catalog against MinIO would not add evidence and would not fit the strict gate. |
| 2026-08-11 | Every fault-store barrier wait is bounded and context-aware. | An unreleased barrier previously stranded an operation inside the store, so one failed assertion deadlocked the harness cleanup and became a package-wide timeout instead of a local failure. |
| 2026-08-11 | The accept-then-error injections return no ETag, matching the real adapter. | A publication path that read the returned ETag despite the error would resolve its own ambiguity under the harness and take the recovery path only in production. |
| 2026-08-11 | Harness numeric overrides (`RetryLimit`, `CheckpointPacks`, `RetainedCheckpts`) are pointers. | Zero is a documented value for two of them; a plain int could not distinguish "no retries" from "unset", so the zero boundary was unreachable and a row that named it silently ran eight retries. |
| 2026-08-11 | One Go test command and one npm test command. No build tag, environment variable, flag, or Makefile target runs a subset of the suite. | Every gate that could be run separately eventually was not run. `make integration-test` re-ran a subset only so a shell script could grep the JSON output for skips, and `testing.Short()` guarded four tests against a `-short` mode nothing ever passed. The suite already fit one command: all 11 packages, MinIO included, pass `-race -count=3 -timeout=30s -p 1` in under a minute. |
| 2026-08-11 | Docker is a prerequisite of `make test`, not a detected capability: an unreachable daemon fails the run. | A skip let a run report success while the entire storage protocol went unexercised. The policy lives in `internal/testminio.require` and is pinned by `TestRequireFailsWhenDockerIsUnavailable`, replacing `scripts/check-integration-skips.sh`. |
| 2026-08-11 | Delete the pure-Go `internal/git2` stub; there is no `CGO_ENABLED=0` build or lint mode. | The stub let `CGO_ENABLED=0 go build` produce a binary that starts and then fails every operation; a compile error is the earlier and clearer failure. It was also the only `!cgo` island, so removing it let one CGo test run cover the whole tree — and the CGo lint mode, being a strict superset, immediately found 12 files of `go fix` modernizations that the pure mode had never been able to see. |
| 2026-08-11 | The release smoke checks are Go tests (`release_test.go`), not Makefile steps. | Five bash self-tests became one table-driven Go test that also verifies the digests it used to only pattern-match. The release binary builds once per test process, so `-count=3` links libgit2 once. Processes start through `os.StartProcess`: the module-wide `os/exec` ban is what keeps the no-Git-executable seam absolute, and it caught this file on the first run. |
| 2026-08-11 | The CLI is a subcommand router built on `go_away_boilerplate/pkg/cmd`: `serve\|s` and `version\|v`. | Shared tooling across the owner's projects. The package selects a command from the first non-flag argument, so a flag-only command line is no longer valid; this is a breaking change to the documented invocation, the MCP host configuration, and the npm launcher examples. |
| 2026-08-11 | slivingdoc keeps its own `version` command rather than `pkg/cmd/version`. | The npm launcher and the release smoke test read exactly `slivingdoc <semver>` and one LF from stdout. The shared command prints `version: X, go version: Y, checksum: Z` through `ancli`, in colour, which is a different contract. |
| 2026-08-11 | `internal/app` splits into `Flags.Bind` / `Flags.resolve` and `Setup` returning a `*Runtime` with `Serve` and `Close`. | `cmd.Command` parses the flag set before `Setup` runs, so flag definition had to separate from flag resolution. The split maps one-to-one onto `Setup`/`Run` and leaves one definition of the command line for both the router and the in-process tests. |
| 2026-08-11 | The command map lives in `internal/cli` and the scenario helper routes through it. | Two copies of the map would drift. Routing the black-box process helper through the same entry point as `main.go` is what makes subcommand selection, flag parsing, and the router's exit code part of the scenario contract instead of untested glue. |
| 2026-08-10 | Mark Phase 8 as **Blocked** on the status board so Phases 10 and 11 can proceed. The blocker is the single missing human step — the reviewed commit in the separately owned `simple-go-pipeline` repository — not any defect in the phase work, which is complete and validated.                                                                                                                                                | No later phase depends on Phase 8; the blocked status records the one human step (review, commit, record SHA, replace the placeholder) and keeps it visible instead of silently skipping the phase.                                                                                                                                                                                                                                                                                                                           |

## Feedback index

### Review 1 (2026-08-09)

Pre-implementation review of all phase contracts against the architecture. No
code or tests existed, so no gates were re-run. The findings amended the phase
specifications in the same review. All phases remain `Not Started`.

| ID    | Severity | Phase                            | Summary                                                                            |
| ----- | -------- | -------------------------------- | ---------------------------------------------------------------------------------- |
| R1-01 | Major    | [Phase 2](phase-2-git-engine.md) | Root commit (zero-parent) creation missing from the engine contract.               |
| R1-02 | Major    | [Phase 3](phase-3-s3-storage.md) | Blanket duplicate-ID rejection contradicted the allowed publication-ID repetition. |
| R1-03 | Major    | [Phase 4](phase-4-workspaces.md) | Recovery wording implied workspace reads remote state, crossing the S3 boundary.   |

## Session journal

### 2026-08-08 — Planning

Replaced the obsolete extraction plan with a greenfield architecture and this
implementation worklog. No implementation or tests existed to run.

### 2026-08-09 — Contract hardening

Resolved the review discussion across uncertain publication, L/P/R workspace
semantics, text-only files, first pull, conflict markers, strict manifest
version 1, checkpoint retention, generation-fenced cleanup, generic recovery,
MCP schemas, configuration, and release artifacts. Pinned the implementation
baseline. No implementation existed. JSON-fence parsing and local Markdown-link
validation passed.

### 2026-08-09 — Review 1 (pre-implementation)

Audited all nine phase files against the architecture. Found three contract
seams: root-commit creation (R1-01), the publication-ID repetition rule
(R1-02), and recovery ownership across the package boundary (R1-03). Amended
the phase specifications in the same review. Line-reference and link
validation passed. No implementation or tests existed to run.

### 2026-08-09 — HTTP transport removed

Removed the streamable HTTP transport, bearer authentication, and all related
configuration from the architecture and phase contracts. The server supports
only stdio. An HTTP deployment would require a remote editing path, which
recreates the synchronization problem slivingdoc solves. Re-verified all
section and line references.

### 2026-08-09 — Phase 10 planning

Added the Phase 10 integration-test-harness contract. The harness drives the
full server exclusively through MCP JSON-RPC (in-process, real MinIO, fault
wrappers and failpoints via harness wiring) with a curated stdio
process subset; the scenario catalog maps every architecture usecase and
validates the Phases 1–7 server contract as a black box. Load tests via `go
benchmark` were deferred pending bottleneck attribution. Line-reference and
link validation passed. No implementation or tests existed to run.

### 2026-08-09 — Phase 11 planning

Added the Phase 11 developer-documentation contract. The phase replaces the
Sakfråga-derived AGENTS.md with a verified slivingdoc guide, places the
checked-in build procedure and the layered test doctrine in `docs/`, and adds
one Development section to the root README. Line-reference and link
validation passed. No implementation or tests existed to run.

### 2026-08-09 — Phase 1 implementation (imago, worker session 1)

Implemented the repository shell and the native boundary: `internal/git`
engine seam, `internal/git2` CGo boundary with the pinned static libgit2
v1.9.6, `internal/app` process body, Make targets, Linux dependency
inspection, CI jobs, and the checked-in build procedure. All gates pass
(`make qa`, dupl). The pinned tarball SHA-256 verifies; the release-style
binary depends only on `libc.so.6` and starts without Git.

### 2026-08-09 — Phase 2 implementation (imago, worker session 2)

Implemented the native Git engine: trees, commits, three-tree merge with
rename detection disabled, file merge with exact markers, incremental and
checkpoint packs, pack import with truncation rejection, and the shallow
boundary. Hardened the merge policy for libgit2's D/F index shape and its
label-less empty-side markers. Fixed four phase-1 defects exposed by the new
test suite and by the `go fix`/staticcheck gates. Component tests run
against real libgit2; policy tests run against the deterministic fake; a
source scan proves no `git` invocation and no `git2go` import. All gates
pass (`make native-test`, `make native-smoke`, vet, staticcheck, gofumpt,
go fix, dupl).

### 2026-08-09 — Phase 3 implementation (imago, worker session 4)

Completed the storage protocol phase: the probe and fake suites, the
adapter validation units, and the MinIO contract suite. The shared
contract suite passes against both the deterministic fake and real MinIO
through testcontainers (one pinned container per `go test` invocation,
per-test prefixes). The MinIO suite proves conditional create/replace,
stale-ETag rejection, multipart upload (5 MiB + 1 in two parts), prefix
isolation, a real-HTTP concurrent-CAS race with exactly one winner, and
startup without bucket versioning. Fixed five defects the new tests
exposed: a probe-test import cycle, the fake's unblockable in-flight
barrier, the contract suite's LIST ordering and zero-size metadata
fixtures, the adapter's `NoSuchKey`-on-replace mapping, and a multipart
`io.ErrUnexpectedEOF` abort. Re-pinned the MinIO image to the newest
published tag after the original pin turned out not to exist on Docker
Hub. Added the `integration-test` Make target (fails on skipped tests)
and the CI `integration` job with image pre-pull in `validate` and
`native`. All gates pass (`make qa`, dupl, `make integration-test`).

### 2026-08-09 — Phase 4 implementation (imago, worker session 5)

Implemented `internal/workspace` (path policy, derived private keys,
`os.Root`-relative scans, strict `state.json` version 1, baseline
authority, staged replacement with rename and cross-device copy fallback,
recovery-required mode, failpoints, and local operation locks) and
extracted the strict JSON value tree into the neutral
`internal/strictjson` package used by both the manifest and the state
record. Pure tests run under CGO_ENABLED=0 with a deterministic fake
engine and repository; component tests run against the pinned real
libgit2 and include a subprocess lock-handover test. All gates pass
(`go test -race -timeout=30s -count=3 -p 1 ./...`, vet, staticcheck,
gofumpt, go fix, dupl with 0 clone groups).

### 2026-08-10 — Phase 5 implementation (imago, worker session 8)

Implemented `internal/notebook` tests and wiring: the full fake repository
(deterministic OIDs, real file-level merge, exact markers, binary packs),
pull/commit/recovery suites over the real workspace, native libgit2 tests,
and the MinIO notebook integration suite. Fixed a production bug the
acceptance tests caught: a conflicting pull now writes the `<P>/pulled`
marker so it also initializes P for the first commit. `make
integration-test` now covers the notebook suite and depends on the libgit2
build stamp; the CI integration job builds libgit2 first. All gates pass:
the strict race gate (`-race -timeout=30s -count=3 -p 1 ./...`), vet,
staticcheck, gofumpt, go fix, dupl (only acceptable mirror groups), and
`make integration-test` (85 pass actions, 0 skips).

### 2026-08-10 — Phase 6 implementation (imago, worker session 9)

Completed the checkpoint and scale phase. An interrupted earlier session had
written `checkpoint.go`, `checkpoint_test.go`, and the checkpoint trigger in
`commit.go` without documenting or validating them; the suite failed on five
tests. Fixed three production defects: increment target generations now
continue from the checkpoint cutoff instead of the manifest generation
counter; the fake repository grafts shallow roots to zero parents at
`ReadCommit` like libgit2; and a successful compaction re-records the tail
metrics from the compacted manifest. Fixed five test defects (missing local
changes in two competing-worker tests, a warm-cache corruption fixture,
`lastCommitFailEngine` hitting the commit flow's own read, and expectations
that encoded the counter-based numbering). Added the MinIO checkpoint suite
(`TestMinioNotebookCheckpointCleansAndReaderRestarts`: publication, cleanup,
and a gated stale-reader restart over real HTTP) and the sustained-load
harness with the distributed and burst benchmarks plus cold/warm-reader
benchmarks, recording throughput, latency percentiles, CAS attempts per
accepted commit, and conflicts. All gates pass: the strict race gate
(`-race -cover -timeout=30s -count=3 -p 1 ./...`), vet, staticcheck, gofumpt,
go fix, dupl (only the acceptable mirror groups), `make integration-test`
(0 skips), and the benchmark run.

### 2026-08-10 — Phase 7 implementation (imago, worker session 10 continued)

Completed the MCP application phase (the interrupted session 10 left
`internal/mcp` done and the app rewrite unwritten). The app package now
parses flags and environment with the documented precedence (explicitly
empty flags do not fall back to the environment), normalizes the endpoint
without echoing credentials, makes both roots absolute and disjoint,
refuses incompatible libgit2 and S3 stores before any transport runs, and
serves exactly two strict tools over stdio. In-memory tests cover config
precedence, bounds, endpoint and root normalization, help/version early
exit, engine-open and probe refusal, client-EOF shutdown, signal
cancellation of in-flight requests, and shutdown-deadline expiry. Real
libgit2 service tests cover the pull/commit round trip, path-outside-root
mapping, concurrent distinct paths, and idempotent Close. Spawned-child
stdio tests prove initialize/list/two tool calls over real pipes,
protocol-only stdout, stderr logs, startup refusal with redacted
diagnostics, and clean exit on stdin EOF. The `closeTransport` seam makes
the signal path cancel in-flight request contexts by closing the live
connection; the 30-second deadline still forces a nonzero exit for
handlers that ignore cancellation. Fixed the phase-1 smoke gate (the
version report no longer exists; `--version` proves the binary) and the
vacuous `native-test`/`native-smoke`/`integration-test` Make targets;
`integration-test` now fails on any skipped MinIO test. Decision-log rows
record the SDK cancellation mechanism, the pipe-end ordering fix in the
spawn helper, and the two-step integration-test recipe. Gates: `go test
./... -race -timeout=30s -count=3 -p 1` passes without the MinIO suite
(Docker-dependent tests skip by name); vet, staticcheck, gofumpt, go fix,
and dupl pass; `make validate`, `make smoke`, and the redacted `--version`
/`--help` / invalid-config binary checks pass.

### 2026-08-10 — Phase 8 implementation (imago, worker session 11)

Completed the in-repository deliverables of the distribution phase and
prepared the one external deliverable for review. `npm/slivingdoc` is a
zero-dependency Node launcher: strict platform mapping (the five
architecture-21 targets; unsupported platforms fail before download with an
actionable error), streamed downloads with redirect handling, a strict
`SHA256SUMS` parser, verified atomic installs under
`_slivingdoc/<version>/<os>/<arch>/<asset>` with a sidecar (cache hits are
re-verified, corrupt entries are deleted and re-fetched, concurrent
installs race safely, interrupted downloads leave nothing behind), verbatim
argument forwarding, inherited stdio, SIGINT/SIGTERM forwarding, and
exact exit-code/signal propagation. `scripts/check-release.mjs` gates
`npm publish` (prepublishOnly) on a complete GitHub release. The suite (33
tests across five files) covers every error row of the phase; a
global-install end-to-end against a fixture release proved download,
verify, execute, and cached re-execution with the release shut down.

`internal/app.Version` became a linker-injectable variable and `make build`
now injects it; the smoke gate greps the injected value. Added the macOS and
Windows dependency-inspection scripts with `--check` self-tests (Linux
already had one), the `SHA256SUMS` grammar producer
(`make-sha256sums.sh`) with a self-test, and the release-reference guard
(`check-release-ref.sh`) with a self-test. `scripts/build-libgit2.sh` is
portable to macOS and Windows runners (shasum fallback, portable CPU count,
`--config Release`); the Linux path was re-proven by a full rebuild from the
cached tarball. CI gained an `npm` job; the caller `release.yml` references
the reusable pipeline at a placeholder SHA because the reusable-workflow
change in `baalimago/simple-go-pipeline` must land as a reviewed commit by
a human (git is read-only for worker sessions); the complete proposal and
its validation plan are recorded in `simple-go-pipeline-release-proposal.md`.
The phase stays **In Progress** until that commit's SHA replaces the
placeholder and `check-release-ref.sh` passes on the real file.

Gates: `make validate` passes; `make smoke` passes (three platform
dependency self-tests, checksum-grammar self-test, release-ref self-test,
injected `--version`, Linux dependency inspection); `go test ./... -race
-timeout=30s -count=3 -p 1` passes; vet, staticcheck, gofumpt, and go fix
pass; dupl reports only the pre-existing acceptable mirror groups in
`internal/git/merge_test.go`; `npm test --prefix npm/slivingdoc` passes
(33 tests, 0 failures).

### 2026-08-10 — Phase 8 continuation (imago, worker session 16)

Moved and adapted the reusable release workflow into the separately owned
`baalimago/simple-go-pipeline` repository: an uncommitted working-tree diff
on branch `slivingdoc` replaces `.github/workflows/release.yml` and adds a
"Release pipeline" section to that repository's README. The move adapts the
proposal in five places — one build job with a default legacy matrix (no
`fromJSON('')`, no skipped-job `needs`), `shell: bash` on every run step,
opt-in `notice-file`, native-only default smoke, and a self-excluding
default checksum path — each fixing a defect that would have broken a
pure-Go caller or the Windows target. Validated with `actionlint`, YAML and
`fromJSON` parsing, `bash -n` on every run block, GitHub-API verification of
the pinned action SHAs, and a local release-assembly simulation of both
checksum paths against the strict grammar. Phase 8 is marked **Blocked** on the
status board: a human must review and commit the diff, record the commit SHA,
and replace the placeholder in the caller workflow; the updated
`simple-go-pipeline-release-proposal.md` records the adaptations, the
validation evidence, the post-merge fixture plan, and the remaining steps.

### 2026-08-10 — Phase 9 implementation (imago, worker session 12)

Completed the documentation and quality-gate phase. The root README is now
a guide for an uninitiated MCP user: product and two-tool workflow first,
then installation, the exact flag/environment table, S3 permissions and the
startup compatibility probe, stdio client configuration, notebook content
rules (UTF-8 text, no U+0000, no symlinks or special files, path and
message byte bounds, conflict-marker rejection), conflict recovery with
ordinary file tools only, checkpoint and retention controls, and
operational ownership separated from bucket versioning/replication/
lifecycle/backup policies. `examples/minio/` adds one pinned-MinIO
`compose.yaml` and a walkthrough; it is isolated from the automated suites
by construction (tests use testcontainers and never read the example
directory). The architecture audit re-verified every contract surface
(tools and envelopes, private identity and state, key grammar, manifest
shape and validator rules, probe and permissions, pull/commit semantics,
conflict markers, checkpoints and retention, failure and recovery
contracts, configuration, path security, distribution grammar and launcher
cache, operational responsibility) and found no discrepancy requiring an
architecture change and no implementation defect.

The vulnerability gate drove two dependency updates: the pinned Go
toolchain moved from 1.26.3 to 1.26.5 (four reachable standard-library
vulnerabilities, including GO-2026-4970 which touches the `os.Root`
path-safety boundary) and the indirect `klauspost/compress` moved to
v1.18.7. After the upgrades `govulncheck v1.1.4` reports 0 vulnerabilities
in code and imports; the one remaining module advisory (GO-2026-5932
openpgp, no fix) is recorded as reviewed and unreachable. The Phase 6 load
harness produced the recorded baseline: distributed 50.0 commits/s at
1.01 CAS/commit with 0 conflicts and 0 failures; burst 157.2 commits/s at
10.67 CAS/commit with 40 commits exhausting the production retry bound
(`REMOTE_BUSY`, caller files preserved — the architecture's stated
contract); cold pull 14.1 ms at 29.75 MB/s; warm pull 12.9 ms.

Gates (go1.26.5): `make validate` passes; `make test` (`-race
-timeout=30s -count=3 -p 1`, native libgit2 and MinIO suites) passes;
`make smoke` passes; `make integration-test` passes with 0 skips;
`npm test --prefix npm/slivingdoc` passes (33 tests, 0 failures);
`govulncheck v1.1.4 ./...` reports 0 vulnerabilities in code and imports;
dupl reports only the pre-existing acceptable mirror groups. The Phase 8
matrix exception (first real macOS/Windows target runs after the external
pipeline lands) and the target toolchain limitation are recorded in the
phase file; the gate re-runs after Phases 10 and 11.

### 2026-08-10 — Phase 10 implementation start (imago, worker session 14)

Selected Phase 10 (integration test harness) as the next eligible phase;
Phase 8 is In Progress but externally blocked on the reusable-pipeline
review SHA. Implemented the production seams first: `internal/app` now
exports `ServiceConfig`, `ServiceHooks`, `NewService` (the former
`notebookService`), `StoreFactory`, and `RunProcess(engine, ProcessOptions)`
with `Run` delegating to it; `internal/mcp` logs a started/completed pair
per tool call under a generated `mcpReqID` and attaches the request-scoped
logger to the context; `internal/notebook` owns the context logger key
(`WithLogger`/`LoggerFrom`) and its `runCheckpoint`/`cleanup` failure
branches now emit Warn records. The Makefile `integration-test` target
includes `./internal/integrationtest/`.

Created `internal/integrationtest` with the pure (CGO_ENABLED=0) core —
the scenario DSL (`scenario.go`), the store recorder (`recorder.go`), the
scoped slog capture (`logcapture.go`), and the fault-injecting store
wrapper (`faults.go` with one-shot/permanent failures, accept-then-error
writes, unprovable-CAS reads, corrupt reads, delete failures, exact-key
and key-prefix barriers, and op-level injections for the notebook's
internal pack keys) — each with passing unit tests. The cgo layer has the
harness (`harness.go`: MinIO-default or injected store, recorder+faults
stack, in-memory MCP sessions, `RunScenario` with barrier-managed client
goroutines and polling assertions), the process helper
(`main_test.go`: `SLIVINGDOC_INTEGRATION_HELPER` fake/bad-store modes,
spawn via `os.StartProcess`, sanitized AWS env, protocol-only stdout
check), and the state-record assertions (`assertions.go`).

Gates run so far: `go build ./...` (cgo) and `CGO_ENABLED=0 go vet ./...`
pass; `CGO_ENABLED=0 go test ./internal/integrationtest/` passes (pure
suite). Remaining: the scenario catalog files (validation, pull, commit,
conflict, checkpoint, recovery, integrity, path security, transport,
config, error taxonomy), the mcpReqID-scoping scenario, then the full QA
matrix and the Docker-backed `make integration-test`.

### 2026-08-10 — Phase 10 continuation note (imago, worker session 14, context-limit handover)

Discovered and fixed a pre-existing production defect in
`internal/workspace` that blocked every materialize-then-scan flow:
`refreshRoot` reopened the `os.Root` handle at the VISIBLE PATH
(`os.OpenRoot(w.path)`) instead of the configured workspace root. For any
request path below the root, the next scan resolved `w.rel` against the
wrong root, recreated a phantom `<path>/<path>` directory, and returned an
empty snapshot — so first-pull local additions vanished, `Accept`/`Diff`
saw nothing, and `TestCommitUploadsPackBeforeManifestCAS` hung. The fix
stores the canonical workspace root in `Workspace.wsRoot` and reopens
`os.OpenRoot(w.wsRoot)`; the rel=="." case still works because the visible
path is the root then. Decision-log row recorded. After the fix the full
suite is green (`go test -race -count=1 -timeout=300s -p 1 ./...`, 11
packages, live MinIO included).

The Phase 10 production seams, the harness core, and the pure-unit-tested
DSL/recorder/logcapture/faults layers are complete and green; the scenario
catalog files are the remaining work (see PHASE10-HANDOVER.md for the
per-file plan, ordering, and verified sequencing facts).

### 2026-08-11 — Phase 10 completion and review 2 (worker session 17)

Audited `internal/integrationtest` against the catalog and completed the
phase. The package as found did not pass the project's own gates: the strict
race gate timed out on the package because no scenario used `t.Parallel()`
(the harness already provided per-test isolation, nothing used it; the fix
takes the gate from a timeout to 13.6 s), `internal/notebook`'s
`TestRecoverFailpointReportsFailedResync` failed deterministically because
it left a failpoint installed while asserting a self-heal, and the phase-2
seam scan failed because the path-security scenario imported `syscall`.

Review 2 recorded 19 findings (7 Critical, 12 Major), all fixed in the same
session. The harness fixes closed the ways a scenario could pass while the
contract was violated: unbounded barriers that could deadlock the package,
an accept-then-error injection that returned an ETag no real adapter
returns, keyed delete injections that never fired and keyed delete counters
that were always zero, inverted `slog` attribute precedence in the log
capture, an embedded store interface that would forward a new method
uncounted, a snapshot helper that read a missing directory as empty, and
`RetryLimit: 0` silently meaning the default of 8. The scenario fixes
replaced the two timing-based writer races with barrier-driven ones on real
MinIO, ordered the competing-checkpoint-worker releases, and repaired
vacuous assertions in redaction (which checked no Git object ID and no Git
vocabulary), CAS-loss retry, ambiguous pack upload, L rewriting, malformed-
key cleanup, cleanup failure, and the strict-schema table. Seven catalog and
error-coverage rows that had no test were written, including the
overlapping-roots startup refusal and the no-mutation recovery boundaries.

Two contract facts were confirmed against the implementation after first
drafts failed: a compaction always selects the oldest `threshold`
increments, so a retried checkpoint still cuts at the original generation;
and with retention 1 the first compaction's checkpoint is the retained one
after the second compaction.

Gates: `go test -race -timeout=30s -count=3 -p 1 ./...` passes across all 11
test packages; `make integration-test` passes with no skips; `make smoke`,
`npm test` (33 tests), vet, staticcheck, gofumpt, and `go fix` all pass;
dupl reports three groups, all acceptable under the duplication policy;
coverage is 84.1 % across the module, with the black-box suite alone
covering 66.8 %.

### 2026-08-11 — Phase 11 implementation (worker session 17)

Replaced the Sakfråga-derived sections of `AGENTS.md` with verified
slivingdoc content: the seven startup-wiring steps of `app.RunProcess`, the
full flag table checked against the built binary's `--help`, the conventions
section (error wrapping, the `mcpReqID` logging convention, the nine
storage and safety invariants, the stable error taxonomy and its no-leak
rule), this repository's real duplication examples in place of another
project's, and a QA section with baseline-pinned tool versions, the Docker
and npm gates, the coverage bar, and the reason both build modes must be
linted. Removed the stale "planned" annotations for Phases 7 and 10 and
corrected the package map, which omitted `npm/slivingdoc`, `examples/`, and
most of `scripts/`. Added the coverage bar and cross-package measurement to
`docs/testing.md` and one Development section to the root README.

Verifying the package map exposed a defect in the earlier phases: **`.github/`
did not exist.** Phases 1, 3, 8, and 9 all record creating `ci.yml` and
`release.yml`, and neither was ever tracked by git. The test doctrine's rule
that "the CI integration job must treat a skipped MinIO suite as failure"
therefore had no job behind it. Both workflows were written to their recorded
specifications: `ci.yml` runs validate, native, integration, and npm jobs with
every action pinned by a full commit SHA resolved from its tag through the
GitHub API, and `release.yml` is the five-target caller for the reusable
pipeline. The caller still references the pipeline at the documented
placeholder SHA, so `scripts/check-release-ref.sh` keeps reporting Phase 8's
outstanding external review and GitHub still refuses the workflow at
dispatch; that interlock is unchanged.

Gates: the grep gate finds no foreign content and no `TBD`; the package map
matches `go list ./...`; the flag table matches `--help`; every function named
in the operation flows exists; both workflows parse and `actionlint v1.7.7`
reports them clean.

### 2026-08-11 — Phase 9 gate re-run after Phases 10 and 11 (worker session 17)

Re-ran the full validation matrix over the finished tree, as the execution
order requires. Everything passes: `make qa`, the strict race gate across all
11 test packages, the pure `CGO_ENABLED=0` suite, `make integration-test`
with no skips, the npm launcher suite, vet and staticcheck in both build
modes, gofumpt, `go fix`, and `actionlint` on both workflows. `dupl` reports
three groups, all acceptable under the duplication policy. `govulncheck
v1.1.4` reports 0 vulnerabilities in code and imports; the single remaining
module advisory (GO-2026-5932, openpgp) is the one Phase 9 already recorded
as reviewed and unreachable. Coverage is 84.1 % across the module. All 31
architecture line references in this file re-verify, every citation added in
`internal/integrationtest`, `AGENTS.md`, and `docs/` verifies against the
section headings, and no local Markdown link is broken.

The re-run earned its place: it caught three pre-existing failures that no
previous session had reported (the timed-out race gate on the scenario
package, the deterministically failing notebook recovery test, and the seam
scan failing on a `syscall` import), and verifying the documentation exposed
that `.github/` had never existed despite four phases recording CI work.

Phase 8 remains **Blocked** on its one external human step, unchanged: the
reviewed commit in `baalimago/simple-go-pipeline`. The caller workflow keeps
the placeholder reference, so `scripts/check-release-ref.sh` still reports it
and GitHub still refuses the release workflow at dispatch. Every other phase
is Complete.

### 2026-08-11 — Test-command collapse (worker session 17)

Feedback: the Makefile was too complex, and gating tests behind flags is a
sign of inefficient tests. One Go test command, one npm test command, no
exceptions.

The measurement came first, and it reframed the work: the whole tree already
passed `go test -race -count=3 -timeout=30s -p 1 ./...` in 45 s with Docker
up, all packages included. The complexity was not protecting anything. `make
integration-test` re-ran a subset of what `make test` had already run, purely
so `check-integration-skips.sh` could grep the JSON for `"Action":"skip"`;
`native-test` and `native-smoke` were aliases; and the four `testing.Short()`
guards had never fired, because nothing in the repository has ever passed
`-short`.

Removed: the `validate`, `native-test`, `native-smoke`, `smoke`, and
`integration-test` targets, `scripts/check-integration-skips.sh`, the five
`scripts/test-*.sh` self-tests, the `!cgo` engine stub and its test, all 25
`//go:build cgo` tags, and every `testing.Short()` guard. Added:
`release_test.go`, which drives the real release scripts as one table-driven
test and builds the release binary once per process, and
`internal/testminio.require`, which makes an unreachable Docker daemon a
failure rather than a skip and is itself tested.

Two findings fell out of the collapse. Deleting the stub made the CGo lint
mode the only mode, and being a strict superset it immediately reported 12
files of `go fix` modernizations that the `CGO_ENABLED=0` gate had never been
able to see — the pure gate had been lint-blind to every `//go:build cgo`
file. Applying them made the `boolPtr` and `count` helpers unused, so both
are gone. Separately, the first draft of `release_test.go` used `os/exec` and
was caught on its first run by the module-wide seam scan; it now spawns
processes through `os.StartProcess` like the rest of the repository, which is
the invariant working exactly as designed.

The subprocess lock helper in `internal/workspace` moved from a `TestXxx`
body that skipped on every normal run to a `TestMain` dispatch, and the
`t.Skipf("helper exit")` after killing it became a `Fatalf`. The Go suite now
reports **zero skips** on Linux; the only remaining skips in the tree are
platform capabilities (Windows symlinks, FIFOs, hard links, Unicode
normalization forms) that name the capability they need.

Gates: `make qa` (lint, the full Go suite, 33 npm tests) passes in 59 s;
`go test -race -count=3 -timeout=30s -p 1 ./...` exits 0 across 13 packages
with 0 failures and 0 skips, slowest package 7.3 s against the 30 s budget;
coverage is unchanged at 84.1 %; `dupl` reports the same three acceptable
groups; `govulncheck v1.1.4` reports 0 vulnerabilities in code and imports;
`actionlint` is clean on both workflows; no Markdown link is broken.

`.github/workflows/ci.yml` drops to two jobs, `go` and `npm`. There is no
integration job, because there is no longer an integration command.

### 2026-08-11 — Subcommand CLI on go_away_boilerplate/pkg/cmd (worker session 17)

Feedback: use the shared `go_away_boilerplate/pkg/cmd` system for flags,
following the sakfråga example.

Reading the package first changed what the work was. `cmd.parse` selects a
command from the first non-flag argument, so `slivingdoc --bucket ...` cannot
survive: the CLI had to grow a `serve` subcommand. That was raised before any
code changed, together with two smaller collisions, and the breaking change
was accepted.

`ancli.Okf` and `ancli.Noticef` write to a hardcoded `os.Stdout` in colour, and
`cmd.Run` prints `err.Error()` through `ancli.Errf` with no redaction of its
own. Stdout already carries usage and help output by contract, so the router's
usage text is consistent with it; the redaction is preserved by returning an
already-redacted error from `Setup`, which the process scenario verifies
end-to-end (an `AKIA…` key in `SLIVINGDOC_PREFIX` still reaches stderr only as
`[redacted]`). `cli.Run` turns colour off, once, behind a `sync.Once`.

The shape is `main.go` → `internal/cli` (command map, usage, router) →
`cmd/serve` and `cmd/version`. `cmd/serve` maps onto a new two-phase
`internal/app` API — `Flags.Bind`/`Flags.resolve` and `Setup` returning a
`*Runtime` with `Serve` and `Close` — because the router parses the flag set
before `Setup` runs. `serve` takes injected `ProcessOptions`, so the black-box
process helper routes through the same command map as the released binary
instead of calling `app.RunProcess` behind it; subcommand selection, flag
parsing, and the router's exit code are now scenario-covered rather than
untested glue.

The race detector earned its place twice. It rejected the first draft of
`cli.Run`, which wrote the `ancli.UseColor` global on every call — harmless in
a process that calls it once, wrong in a parallel test. And the coverage sweep
(uninstrumented for race but slower) exposed a **latent flake that predates
this work**: `TestScenarioCheckpointCompetingWorkers` asserted that exactly one
gen-3 checkpoint object survives, but nothing forced worker A to win the
compaction. When B won, it ran cleanup while A was still blocked in its upload,
and A's proposal landed unreferenced with no later checkpoint to collect it —
an outcome the contract explicitly permits, since cleanup is best-effort and
generation fenced. The scenario now parks B at its checkpoint CAS (installed
only once both workers are in the upload barrier, so it catches the checkpoint
CAS rather than the increment CAS), which pins the ordering. It survives 10
race-enabled counts and 5 plain ones.

Line references: section 17 grew by 11 lines, so every citation of sections 18
through 25 shifted. All live references in `internal/` and `docs/` were updated
and re-verified to land in their cited section; worklog entries above this one
cite the pre-change numbering.

Gates: `make qa` passes in 61 s across 14 packages; coverage is 84.2 %, with
`internal/cli`, `cmd/serve`, and `cmd/version` at or near full statement
coverage; `dupl` reports the same three acceptable groups; `govulncheck v1.1.4`
reports 0 vulnerabilities in code and imports; no Markdown link is broken.

### 2026-08-11 — Structured logging with per-module levels (worker session 17)

Feedback: use slog the way sakfråga does; every record needs a timestamp;
`go run . serve` printed `error: ... bucket is requiredexit status 1` with no
newline, no timestamp, and no colour; honour `NO_COLOR`. Then: extract the
implementation into a new package in the local `go_away_boilerplate` checkout,
depend on it locally, and let `LOG_LEVEL` set levels per module —
`LOG_LEVEL="cli=warn,mcp=debug,info"`.

Two of the three defects were mine: `cli.Run` set `ancli.UseColor = false`, and
`ancli` only terminates a line when `ancli.Newline` is set. The missing
timestamp needed `ancli.SetupSlog()`, which routes ancli through slog —
errors to stderr, usage and help to stdout, each stamped and terminated.

The new `pkg/slogcolor` in `go_away_boilerplate` is an extraction of
sakfråga's `internal/slogcolor`, **with its two TODOs implemented**. That
handler's `WithAttrs` and `WithGroup` returned the receiver, so every bound
attribute was silently dropped. Carrying that across would have erased
`mcpReqID`, `tool`, `outcome`, and `duration` from every slivingdoc tool call
— the exact attributes the scenario suite asserts on and scans for leaked
secrets. `ancli.SetupSlog`'s own `ansiprint` has the same defect and also
writes Info to stdout, which is why it could not be used here at all.

Per-module levels are the new capability. `ParseLevels` reads the grammar and
the handler resolves a module's level when the module attribute is *bound*,
not when a record is emitted: slog consults `Enabled` before it builds a
record, so a call-site attribute would be too late. `slivingdoc` binds one of
four modules — `cli`, `app`, `mcp`, `notebook` — through `app.Module`.

`internal/app.NewLogger` reads `LOG_LEVEL` and `NO_COLOR` from the *injected*
environment rather than `os.Getenv`, keeping the process body testable. A
malformed `LOG_LEVEL` is reported and falls back to info rather than refusing
startup: diagnostic plumbing must not be able to take the server down, and a
scenario proves the process still serves with `LOG_LEVEL=mcp=verbose`.

Coverage of the behaviour is at both levels: the boilerplate package has unit
tests for the grammar, attribute retention, group qualification, LogValuer
resolution, filtering, and concurrent non-interleaving; slivingdoc has
`scenario_logging_test.go`, which drives a real process and proves every
stderr record carries `time=`, `level=`, `msg=`, and `module=`, that
`LOG_LEVEL=mcp=error,info` silences one module while the others keep logging,
and that `NO_COLOR` behaves in both directions.

**Open item:** `go.mod` carries `replace github.com/baalimago/go_away_boilerplate
=> /home/lorkin/Projects/not_wasmer/go_away_boilerplate`, as requested for
local debugging. It must be removed once `pkg/slogcolor` is published, or no
one else can build slivingdoc. The boilerplate change is uncommitted in its
own repository.

Line references: section 17 grew again, so citations of sections 18 through 25
shifted a second time. All 72 live references re-verified against their cited
section.

Gates: `make qa` passes in 60 s across 14 packages; the boilerplate's own
`go test -race -count=3 ./pkg/...` passes; `go fix` found one modernization in
the new scenario (`strings.SplitSeq`) which was applied.

### 2026-08-11 — Package parallelism removed, coverage folded into the gate (worker session 17)

Feedback: why is `go test` run with `-p 1` — can the suite not handle
concurrency? And coverage should come from the Go test run.

`-p 1` bounds how many test binaries run at once, not the concurrency of the
code; tests inside a package already run under `t.Parallel()`. The bound was
presumably inherited caution about the three packages that each start their
own MinIO container. Measurement disposed of it. On this 22-core machine the
gate went from **54 s to 10 s**. The number that actually matters is the
per-package timeout, and on a four-core runner (`taskset -c 0-3`, modelling
CI) the slowest package sat near 20 s of the 30 s budget in every
configuration:

| 4-core runner | wall | slowest package |
| --- | --- | --- |
| `-p 1` | 72 s | 22.8 s |
| `-p 2` | 36 s | 22.0 s |
| `-p 4` | 23 s | 19.9 s |
| `-p` unset | 28 s | 24.8 s |

So `-p 1` cost about 3x wall clock and bought no headroom at all on the
constraint it appeared to protect. It is gone. Six consecutive runs of the new
gate pass with the slowest package between 8.5 s and 15.8 s.

Coverage turned out to be free: 19.9 s slowest without it against 20.1 s with
`-coverpkg=./...` on four cores. `make test` now carries
`-coverpkg=./... -coverprofile=.build/cover.out`, prints the total, and fails
below the documented 70 % floor; `make cover` opens the profile. The
`-coverpkg=./...` form is required rather than plain `-cover`, because the
black-box scenario suite exercises packages other than its own and
per-package coverage understates the real figure badly (66 % against 84 %).

The floor guard was verified to fail, not just to pass: `make test
COVER_FLOOR=99` exits nonzero with `coverage 84.3% is below the 99% floor`.
An unfalsifiable gate is not a gate.

`AGENTS.md` and `docs/testing.md` carried "do not modify the
package-parallelism bound" as a standing rule. That rule is now removed, with
the measurement recorded in its place; the race detector, the count, and the
timeout remain untouchable.

Gates: `make qa` drops from 60 s to 15 s and passes, coverage 84.3 %.

### 2026-08-11 — The scripted scenario DSL deleted (worker session 17)

Question: why is `internal/integrationtest` only at 76 % coverage?

It was 602 of 792 statements. The 190 uncovered ones were not missing tests.
Attributing them by function gave three groups:

- **~78 statements — the phase-10 scenario DSL, with zero call sites.**
  `RunScenario`, `Harness.session`, `writeFiles`, `callSession`,
  `barrierManager`/`newBarrierManager`/`arrive`, and `Harness.WorkspaceRoot`
  were never invoked by any scenario, nor by anything else.
- **~40 statements — declarative expectation branches nothing sets.** The
  catalog asserts manifests, counters, and logs through direct harness methods
  (`h.Manifest()`, `h.Recorder().CountKeyPrefix()`), so `S3.Manifest` and the
  script-only `ToolCall` fields never executed.
- **~70 statements — `t.Fatalf` and error paths inside assertion helpers**,
  which by construction run only when a test fails. These are legitimately
  uncoverable and are the package's real floor.

The root cause of the first two is one thing: phase 10 specified a declarative
scenario script, and the catalog was written against the imperative harness
API instead. Both shipped; only one was ever used. Two review findings had
already been spent on the dead half — R2-14 fixed `t.Fatalf` being called from
client goroutines inside `RunScenario`, code nothing invokes.

Its `barrierManager` also duplicated the fault-store barriers
(`BlockPrefix`/`WaitingPrefix`/`Release`) every concurrency scenario actually
uses, and the fault-store version is strictly stronger: it blocks at a real
storage operation rather than at an arbitrary script point. The
competing-workers ordering fixed earlier the same day needed exactly that
precision, which a script-level barrier could not express.

Deleted: `RunScenario` and its private helpers, the barrier manager,
`Harness.WorkspaceRoot`, the types `Scenario`, `Entry`, `FileWrite`, and
`ManifestExpectation`, `Scenario.Validate` and `Scenario.clients`, the
script-only `ToolCall` fields (`Client`, `Writes`, `Barrier`), the
`S3Assertions.Manifest` field with its assertion branch, and the four
`pure_test.go` tests that existed only to exercise `Validate` and `clients`.
Kept, because the catalog uses them: `ToolCall` (38 uses), `CallExpectation`
(32), `assertExpectations`, `FileExpectation`, `RangeExpectation`,
`RecoveryExpectation`, `CountExpectation`, and `LogExpectations`.

**Deviation from the phase-10 plan**, recorded rather than hidden: that plan
lists `scenario.go` as "Scenario DSL, RunScenario, barrier and waiter helpers"
and specifies `RunScenario(t, h, s)`. The plan file is left as the historical
record of what was specified; this entry is the record of what the
implementation converged on and why.

`internal/integrationtest` goes from **76.0 % (602/792) to 85.0 % (565/665)**,
and the module total from 84.3 % to **86.0 %**. `make qa` passes in 17 s. The
suite now has one barrier mechanism instead of two.

### 2026-08-12 — CI gate timeout root cause and fix (worker session 18)

The `qa` workflow and the `readme test coverage` job both failed on master
with `panic: test timed out after 30s` in the root package: `TestReleaseBinary`
and `TestReleaseBinaryCommandSurface` were still blocked in
`release_test.go`'s in-suite `go build` of the release binary at the 30 s
mark. The gate passes locally only because a warm build cache hides the
cost; a cold `GOCACHE` reproduces the CI failure exactly (`FAIL
github.com/baalimago/slivingdoc 30.792s`, `make: *** [Makefile:51: test] Error 1`).

Measured on this machine: the release-style build alone takes about 35-40 s
from a cold cache (32.9 s on four cores, 40.0 s on eight), against a 30 s
per-package budget — the in-suite build can never fit the gate on a cold
runner, and CI starts cold every run. `go vet` does not warm the build
variant (a vet-warmed cache still builds in 27 s), so the pre-existing
`make lint` step gave no help.

Fix: `make test` now depends on the shared `$(BIN)` target, so it builds the
release-style binary first and warms the exact compile cache the in-suite
build reuses; the in-suite build then costs a link only. `make build`
delegates to the same target. Cold-cache validation: `make build` then
`make test` passes with the root package at 4.4 s and the slowest package at
22.1 s (integrationtest). The race detector, the count, and the timeout are
unchanged, and `release_test.go` still builds and proves its own binary.

### 2026-08-12 — release workflow smoke failure root cause and fix (worker session 18 continued)

The first release attempt (tag `v0.1.0-rc0`, commit `3f13669`) failed in all four started build-matrix targets (`linux-amd64`, `linux-arm64`, `darwin-arm64`, `windows-amd64`); `linux-amd64` failed at the pipeline's Smoke test step with exit 127. Three independent defects in the caller wiring caused it:

1. `native-build-command: make build VERSION=...` wrote the binary to the fixed development path `.build/slivingdoc`, while the reusable pipeline expects the artifact at `$TARGET_BINARY` — the architecture-21 asset name (`slivingdoc-v0.1.0-rc0-linux-amd64`). The dependency-inspection step tolerated the missing file (`readelf` failed silently into an empty list), so the failure surfaced only at the smoke step, and the artifact upload would have failed next (`if-no-files-found: error`).
2. The smoke command `"${TARGET_BINARY}" --version` invoked a flag the router does not implement: in `go_away_boilerplate/pkg/cmd`, any dash-prefixed argument is a skipped flag, so `--version` leaves no command candidate and the router prints usage and exits 1.
3. The smoke command omitted the `./` prefix. Bash does not search the working directory, so even a correctly placed binary would not be found (exit 127).

Fix (`.github/workflows/release.yml`): the build now writes the artifact directly at the pipeline's expected path (`make build BIN="${TARGET_BINARY}" VERSION="${RELEASE_VERSION#v}"`, where `BIN` is the Makefile's build destination), and the smoke runs `./"${TARGET_BINARY}" version` — the `version` subcommand the router implements, with the `./` prefix bash requires. The checksum grammar, the pinned pipeline SHA, and the architecture-21 asset grammar are unchanged.

Validation on this machine (linux-amd64 target, `VERSION=0.1.0-rc0`): `make build BIN=slivingdoc-v0.1.0-rc0-linux-amd64` produces the artifact; `./scripts/check-deps-linux.sh slivingdoc-v0.1.0-rc0-linux-amd64` prints `check-deps-linux: ok`; `./slivingdoc-v0.1.0-rc0-linux-amd64 version` prints exactly `slivingdoc 0.1.0-rc0` with exit 0. The default `make build` still produces `.build/slivingdoc`, and `go test -run TestRelease -count=1 .` passes unchanged.

### 2026-08-12 — release workflow darwin and windows failures (worker session 18 continued)

Run 2 (tag `v0.1.0-rc1`, commit `34dbcce`) passed both Linux targets; `darwin-amd64` and `darwin-arm64` failed at Inspect runtime dependencies (exit 1), and `windows-amd64` failed at Prepare native toolchain (exit 2). Three more defects surfaced:

1. The dependency check referenced `check-deps-"${TARGET_OS}".sh`, but the pipeline names platforms the way Go does (`TARGET_OS=darwin`) while the repo's checkers are named `linux`, `macos`, and `windows` — `check-deps-darwin.sh` does not exist. Fix: the caller maps the one difference with bash substitution (`os="${TARGET_OS/darwin/macos}"`) before building the script name.
2. GitHub's Windows runners enable `core.autocrlf` by default, so the LF-only POSIX scripts checked out as CRLF; bash treats the CR as part of a token, `then<CR>`/`fi<CR>` are syntax errors, and `build-libgit2.sh` exited 2 (the exit code for a bash parse error) at Prepare native toolchain. Fix: the repository now pins `.gitattributes` (`* text=auto eol=lf`) so every text file checks out LF on every runner; all 183 tracked files were already LF in the index, so no bytes changed.
3. `check-deps-windows.sh` referenced `"${ProgramFiles(x86)}"`, which is a bash bad substitution (parentheses are not a valid parameter name), so find_dumpbin could never locate vswhere and the check would fail with a false "dumpbin not found" on the Windows runner. Fix: the canonical vswhere path is built from `SYSTEMDRIVE` (defaulting to `C:`) and translated with `cygpath`.

Validation: `bash -n` passes on every script; `check-deps-windows.sh --check` still accepts the baseline and rejects `git2.dll`/`libgit2.dll`; the darwin target resolves to `check-deps-macos.sh` and the linux/windows names are unchanged; `go test -run TestRelease -count=1 .` and `make lint` pass.

### 2026-08-12 — release workflow windows extraction failure (worker session 18 continued)

Run 3 (tag `v0.1.0-rc2`, commit `89999be`) passed both Linux and both Darwin targets; only `windows-amd64` failed, again at Prepare native toolchain (exit 2). The job log (fetched with `gh`) showed the real cause: the download and checksum succeeded, then Windows' system `bsdtar` aborted the whole extraction with `tar: libgit2-1.9.6/tests/resources/testrepo-worktree/link_to_new.txt: Cannot create symlink to 'new.txt'` and `tar: Exiting with failure status` (exit 2).

The pinned tarball contains exactly one symlink, a relative link inside the tests resources. Windows' `C:\Windows\System32\tar.exe` (bsdtar) cannot create it and exits 2; the earlier CRLF failure had the same exit code and was fixed first, which is why this one only surfaced on run 3. The tests are never compiled (`BUILD_TESTS=OFF`; the top-level CMakeLists only calls `add_subdirectory(tests)` under `if(BUILD_TESTS)`), so the extraction now skips the whole tests subtree: `tar -xzf "$archive" -C "$src" --exclude='*/tests' --exclude='*/tests/*'`. Both patterns cover the directory entry and its contents on GNU tar and on bsdtar.

Validation: a local extraction with the two excludes reproduces the full tree minus `tests/` (diff shows only that directory missing) and no symlink survives; `./scripts/build-libgit2.sh` re-ran end to end on this machine with the new extraction and rebuilt the pinned libgit2 successfully; `bash -n` and `go test -run TestRelease -count=1 .` pass.

### 2026-08-12 — release workflow windows compile failure (worker session 18 continued)

Run 4 (tag `v0.1.0-rc3`) passed Linux, Darwin, and the Windows libgit2 build (Prepare native toolchain); `windows-amd64` then failed at Build with the first real Go compile of the Windows code path: `internal/workspace/platform_windows.go:21:32: undefined: windows.EXDEV`.

The release pipeline is the first place the Windows code is ever compiled: the repository cannot cross-compile (CGo), `make test` runs on Linux only, and the Windows build-tagged files were never exercised. `isCrossDevice` in `platform_windows.go` matched `windows.EXDEV`, which `golang.org/x/sys/windows` does not export (the grep over x/sys v0.47.0 finds no EXDEV at all), and the comment's claim that Go maps `ERROR_NOT_SAME_DEVICE` to EXDEV is wrong: `os.rename` on Windows wraps the raw Win32 error in a `*LinkError`. Fix: match only `windows.ERROR_NOT_SAME_DEVICE` (Errno 17).

Validation: `GOOS=windows CGO_ENABLED=0 go build ./internal/workspace/` compiles the fixed file (the package has no CGo dependency); `go vet` for Windows on the package only fails on test files that import the CGo engine, which the release pipeline never builds; `make lint` and `go test -run TestRelease -count=1 .` pass unchanged.

Watch items for the next run (cannot be proven from a Linux host): the Windows Build step compiles the CGo engine with a C compiler and pkg-config on the runner, and the dependency check runs `dumpbin` against the linked binary — the allowlist in `check-deps-windows.sh` has no VCRUNTIME/ucrtbase/api-ms-win-crt entries, so a MSVC-runtime or mingw-w64 dependency would fail it with a real diagnostic.

### 2026-08-12 — release workflow windows pkg-config failure (worker session 19)

Run 5 (tag `v0.1.0-rc4`, commit `caae4aa`) passed Linux, Darwin, the Windows
libgit2 build, and the Windows Go compile of `platform_windows.go`; the Build
step then failed at the first real CGo compile of `internal/git2` with:

```text
# github.com/baalimago/slivingdoc/internal/git2
# [pkg-config --cflags --static -- libgit2]
Can't find C:\Strawberry\perl\bin\pkg-config.bat on PATH, '.' not in PATH.
```

Root cause: `native.go` resolves the libgit2 flags through `#cgo pkg-config:
--static libgit2`, and Go runs `$PKG_CONFIG` (default `pkg-config`). The
windows-2025 runner has no working pkg-config: the only match on PATH is
Strawberry Perl's `pkg-config.bat` wrapper, which fails when invoked, so the
cgo compile aborts. The runner image provides mingw-w64 gcc 15.2.0 (UCRT,
posix threads) at `C:\mingw64\bin` on PATH, but no pkg-config at all (cmake
reported `Could NOT find PkgConfig`).

Fix (three parts, in `scripts/build-libgit2.sh`, `internal/git2/native.go`,
and `scripts/check-deps-windows.sh`):

1. On Windows the setup script downloads the pinned `pkg-config-lite`
   binary (SHA-256 verified; the same artifact the `pkgconfiglite`
   Chocolatey package installs) into the build tree when no working
   pkg-config is on PATH, pins `PKG_CONFIG` to the extracted
   `pkg-config.exe` (absolute Windows path via `cygpath -w`), and writes
   `PKG_CONFIG` to `$GITHUB_ENV` so the pipeline's later Build step — a
   fresh shell — uses the same binary regardless of PATH order. A dev
   machine that already has a working pkg-config skips the download.
   (The first attempt installed pkgconfiglite through Chocolatey; the rc5
   run failed because the community feed answered 504 Gateway Timeout, so
   the bootstrap was changed to the pinned download with no package
   manager in the path.)
2. The Windows libgit2 build switches from the default Visual Studio
   generator to `-G "MinGW Makefiles" -DCMAKE_C_COMPILER=gcc`, so the
   archive is built with the same mingw-w64 gcc that Go's cgo drives. The
   MSVC-built archive would link through mingw only by luck of ABI
   compatibility; the mingw build removes the question.
3. `native.go` adds `#cgo windows LDFLAGS: -static-libgcc
   -static-libwinpthread` so the release executable does not import the
   mingw-w64 runtime DLLs `libgcc_s_seh-1.dll` / `libwinpthread-1.dll`.
   `check-deps-windows.sh` admits `ucrtbase.dll` (the Universal CRT, which
   the mingw-w64 UCRT toolchain links; `msvcrt.dll` stays, as the Go
   runtime links it) and the baseline test in `release_test.go` pins it and
   rejects the mingw runtime DLLs.

Validation: `make test` and `make lint` pass on Linux with the changed
script and cgo directive; the release-baseline tests cover the extended
allowlist positive and negative cases. The runner-specific outcome (exact
dumpbin DLL set, mingw libgit2 build) is proven by the next release run.

Watch item for the next run: the first real Windows link reports its actual
dumpbin dependency list; if it contains a DLL outside the allowlist (for
example `vcruntime140.dll`), the fix is extending the allowlist with that
exact system DLL rather than weakening the check.

### 2026-08-12 — release workflow windows rc5: chocolatey feed failure (worker session 19 continued)

Run 6 (tag `v0.1.0-rc5`) failed earlier than rc4: the Windows job's Prepare
native toolchain step exited 1 inside the new pkg-config bootstrap. The log:

```text
Failed to fetch results from V2 feed at
'https://community.chocolatey.org/api/v2/Packages(Id='pkgconfiglite',Version='0.28.0')'
with following message : Response status code does not indicate success: 504 (Gateway Timeout).
```

Chocolatey's community feed is a network dependency the bootstrap cannot
control, and the fallback then pinned the broken Strawberry Perl bat
(`build-libgit2: pinned pkg-config does not run: C:\Strawberry\perl\bin\pkg-config`),
which the verify guard caught. Fix: drop Chocolatey from the bootstrap
entirely. The script now downloads the pinned `pkg-config-lite`
`pkg-config-lite-0.28-1_bin-win32.zip` directly from SourceForge
(SHA-256 `2038c49d23b5ca19e2218ca89f06df18fe6d870b4c6b54c0498548ef88771f6f`,
verified against the pinned value before extraction — the same artifact the
chocolatey package installs), extracts `pkg-config.exe` into
`.build/tools/pkg-config-lite/` with `unzip -j`, and pins `PKG_CONFIG` to
it. No package manager, no feed, no PATH-order dependence: the same
deterministic fetch pattern as the libgit2 tarball itself.

Validation: the download, SHA check, and extraction were reproduced on this
machine; `bash -n`, `make lint`, and `make test` pass on Linux with the
changed script. The runner-specific outcome (mingw libgit2 build, cgo link
through pkg-config-lite, dumpbin dependency list) is proven by the next
release run.

### 2026-08-12 — release workflow windows rc6: gcc rejects -static-libwinpthread (worker session 19 continued)

Run 7 (tag `v0.1.0-rc6`, commit `e817ff1`) finally passed Prepare native
toolchain on Windows: the pinned pkg-config-lite download, the mingw-w64
libgit2 build, and the cgo compile of `internal/git2` all succeeded. The
Build step then failed at the very last stage, the external link:

```text
C:\mingw64\bin\gcc.exe ... -static-libgcc -static-libwinpthread
  -LD:/a/slivingdoc/slivingdoc/.build/libgit2/lib -lgit2 -lws2_32 -lsecur32 ...
gcc: error: unrecognized command-line option '-static-libwinpthread';
  did you mean '-static-libgfortran'?
```

The mingw-w64 gcc driver (checked against the gcc 15.2.0 source,
`config/i386/mingw-w64.h`) does not implement `-static-libwinpthread` — the
flag does not exist in this toolchain. It is also unnecessary: the same
source shows winpthread enters a link only through `LIB_SPEC`'s
`%{pthread:-lpthread}`, and Go's Windows external link line never passes
`-pthread` (visible in the failing command: no `-pthread`, no `-lpthread`,
no `-lwinpthread`). The `-mthreads` compile flag maps to `-lmingwthrd`, a
static mingw-w64 archive, not winpthread. libgit2 on Windows uses Win32
threads, so no pthread symbol exists in the link at all.

Fix: `native.go`'s Windows directive is now only
`#cgo windows LDFLAGS: -static-libgcc` (which the driver DOES honor and
which keeps `libgcc_s_seh-1.dll` out via the `static-libgcc` branch of the
driver's libgcc selection). The release binary therefore links: the static
mingw-built `libgit2.a`, static libgcc, Go's own static runtime archives,
and the system CRT — `ucrtbase.dll` (niXman UCRT default `-mcrtdll=ucrt`),
which the dependency-check allowlist already admits.

Validation: `make lint` and `make test` pass on Linux with the changed
directive; the failing link command from the rc6 log was inspected to
confirm the exact flag set. The next run is expected to reach Inspect
runtime dependencies for the first time; the dumpbin list is the last
unknown.

### 2026-08-12 — release workflow windows rc7: MSYS2 rewrites dumpbin's /dependents option (worker session 19 continued)

Run 8 (tag `v0.1.0-rc7`, commit `f90703b`) passed every earlier stage on
Windows for the first time: the toolchain, the cgo compile, and the final
link with `-static-libgcc`. The Inspect runtime dependencies step then died
silently with exit code 157 after about five seconds — no script output, no
dumpbin list. All other platforms passed.

The exit code is the fingerprint: dumpbin is a native MSVC tool (link.exe
family), and LNK1181 "cannot open input file" is error 1181; bash reports a
native child's exit code truncated through the wait status, and
1181 & 0xFF = 157 (`bash -c 'exit 1181'; echo $?` prints 157). dumpbin
therefore ran and failed to open its input. Its stderr was suppressed by
`2>/dev/null`, which is why the step showed nothing.

Why could dumpbin not open the input? The script runs under Git for
Windows' MSYS2 runtime, and the msys2-runtime source
(`winsup/cygwin/msys2_path_conv.cc`) shows the argument rewrite:
`find_path_start_and_type` classifies any `/`-prefixed single component as
ROOTED_PATH, and `rp_convert`/`posix_to_win32_path` rewrites it to
`<Git-root>/dependents` before the native program sees it. dumpbin then
treats `C:/Program Files/Git/dependents` as an input file, reports LNK1181,
and exits before ever reading the real binary — the classic MSYS leading-
slash option trap, the same family as the earlier `ProgramFiles(x86)` bash
bad-substitution defect.

Fix (`scripts/check-deps-windows.sh`): the dumpbin invocation now runs with
`MSYS2_ARG_CONV_EXCL='*'`, the runtime's documented switch to pass every
argument byte-identical (the `/dependents` option form and the relative
binary name both need no conversion). The `2>/dev/null` suppression was
dropped so a real dumpbin failure shows its LNK message instead of a bare
exit code. The comment block above the invocation records the reasoning.

Validation: `bash -n` passes; `check-deps-windows.sh --check` still accepts
the baseline and rejects `git2.dll`/`libgit2.dll`/`libgcc_s_seh-1.dll`;
`go test -run TestRelease -count=1 .` and `make lint` pass. The argument
rewrite was proven against the pinned msys2-runtime source rather than by
execution, because no Linux host can run Git for Windows' runtime. The next
run should print the first real dumpbin dependency list; the allowlist
already admits the predicted imports (kernel32, ucrtbase, ws2_32, secur32,
the crypto/net DLLs).

### 2026-08-12 — release workflow windows rc8: mingw-w64 UCRT links api-ms-win-crt-* forwarders (worker session 19 continued)

Run 8 (tag `v0.1.0-rc8`, commit `f90703b` + the rc7 MSYS2 fix) reached the
first real dumpbin dependency list. The MSYS2 fix held: dumpbin ran, read
the binary, and the step failed with a loud, actionable diagnostic instead
of a bare exit code:

```text
check-deps-windows: unexpected dynamic dependencies:
  api-ms-win-crt-convert-l1-1-0.dll
  api-ms-win-crt-environment-l1-1-0.dll
  api-ms-win-crt-filesystem-l1-1-0.dll
  api-ms-win-crt-heap-l1-1-0.dll
  api-ms-win-crt-locale-l1-1-0.dll
  api-ms-win-crt-math-l1-1-0.dll
  api-ms-win-crt-private-l1-1-0.dll
  api-ms-win-crt-runtime-l1-1-0.dll
  api-ms-win-crt-stdio-l1-1-0.dll
  api-ms-win-crt-string-l1-1-0.dll
  api-ms-win-crt-time-l1-1-0.dll
  api-ms-win-crt-utility-l1-1-0.dll
```

The mingw-w64 UCRT toolchain links the Universal CRT through its
`api-ms-win-crt-*` API-set forwarders, not through `ucrtbase.dll` directly.
Those forwarders are OS-owned components of Windows 10 and later (they live
in System32 and forward to `ucrtbase.dll`), so they are documented Windows
system DLLs under the architecture section 21 baseline — the allowlist
prediction from the rc3 watch items ("no VCRUNTIME/ucrtbase/api-ms-win-crt
entries") had it exactly right.

Fix (`scripts/check-deps-windows.sh`): the allowlist admits the whole
UCRT API-set family with the pattern `api-ms-win-crt-[a-z0-9-]+\.dll`
(the namespace is reserved to Microsoft; the family is a closed, documented
set), and the comment above the allowlist now states that the UCRT is
linked through these forwarders with `ucrtbase.dll` admitted as their
target. `release_test.go` pins the new baseline: a positive case with three
UCRT api-sets (including an uppercase name, proving case-insensitive
matching) and a negative case rejecting `api-ms-win-core-synch-l1-1-0.dll`,
so the boundary stays exact — UCRT api-sets are allowed, other api-sets are
not. `docs/build-libgit2.md` records the same.

Validation: `bash -n`, the `--check` positive/negative modes, `go test -run
TestRelease -count=1 .`, `make lint`, `make test` (85.9 % coverage vs the
70 % floor), and `npm test` all pass. The next run should pass Inspect
runtime dependencies and reach the smoke test: `./slivingdoc-...exe
version` must print `slivingdoc 0.1.0-rc9` and exit 0.

### 2026-08-12 — release workflow rc9: checksum command fed a directory (worker session 19 continued)

Run 9 (tag `v0.1.0-rc9`) passed every target job — including the first
Windows smoke test, where the built exe printed `slivingdoc 0.1.0-rc9` and
exited 0. The release then failed in the assembly job at Create checksums:

```text
Run if [[ -n "./scripts/make-sha256sums.sh dist" ]]; then
  ./scripts/make-sha256sums.sh dist dist/* > dist/SHA256SUMS
  ...
sha256sum: dist: Is a directory
Process completed with exit code 1.
```

Root cause: the reusable pipeline appends the asset paths to the caller's
`checksum-command` input itself — the executed line is
`<command> dist/* > dist/SHA256SUMS`. The caller had passed
`./scripts/make-sha256sums.sh dist`, so the script received the `dist`
directory as its first argument and `sha256sum` rejected it. The proposal
document stated the contract ("`checksum-command` receives the exact
`dist/*` paths"), but the caller wiring added a stray `dist`.

Fix (`.github/workflows/release.yml`): `checksum-command` is now just
`./scripts/make-sha256sums.sh`; the pipeline supplies `dist/*`. A comment
above the input records why the trailing argument must not be there.

Validation: a clean local simulation of the exact pipeline line — five
dummy assets plus `NOTICE` in `dist/`, then
`./scripts/make-sha256sums.sh dist/* > dist/SHA256SUMS` — produces the
strict grammar (LF-terminated, lowercase 64-hex, two spaces, basenames,
sorted): six lines, one per uploaded asset, with no self-referential
`SHA256SUMS` line (the shell globs before the redirect truncates the
target, so the checksum file is not among the hashed inputs). A first
attempt appeared to show a self-line, but that was a leftover empty
`dist/SHA256SUMS` from a failed prior invocation, not the pipeline
behavior; a controlled `ls * > out` test confirmed the glob order.
`go test -run TestRelease -count=1 .` passes the script's grammar
self-test, and `make lint` / `make test` / `npm test` pass. The next run
should reach Classify release kind and Create release and upload assets.

### 2026-08-12 — npm publication: network retry for the flaky sandbox egress (worker session 20)

The rc10 GitHub release is complete and the launcher works end-to-end
against it, but `npm publish` was blocked: `check-release.mjs` (the
`prepublishOnly` gate) died with `socket hang up` on the github.com →
release-assets.githubusercontent.com redirect hop roughly half the time.
Direct hits to both hosts were 10/10; the redirect hop was the flake. The
gate needed retries to ride through it.

Fix (`npm/slivingdoc/lib/download.mjs`): bounded network retry around the
request chains. `HttpStatusError` marks deterministic outcomes (non-2xx,
redirect loop) and is never retried, so a missing asset still fails
immediately. `withNetworkRetry` runs 5 attempts with exponential backoff
(300 ms base) and wraps both `requestStream` (GET, downloads) and
`headStatus` (HEAD, the publication gate); a reset on a redirect hop
restarts the whole chain. The request logic was refactored into
`requestOnce`/`headOnce` so retries wrap the full chain without nesting.

Tests (`npm/slivingdoc/test/helpers.mjs`, `test/install.test.mjs`): the
fixture gained a `failFirst` hook that destroys the connection before any
response is written (a transient failure); the retry test asserts an asset
with `failFirst: 2` still installs and the SHA256SUMS request log shows 3
attempts; the 404 test asserts a missing asset produces exactly one request.
`npm/slivingdoc/README.md` documents the retry contract in the cache
section.

Validation: `npm test --prefix npm/slivingdoc` passes all 35 tests, and
`node scripts/check-release.mjs` from `npm/slivingdoc` passed 10/10 runs
against the real rc10 release (previously roughly 50 % failed). The next
step is the manual publish: `cd npm/slivingdoc && npm publish --access
public --tag next`, verified with `npx -y slivingdoc@next version` — this
closes the open acceptance item `npx -y slivingdoc --version` works from a
clean environment.

### 2026-08-12 — npm publication succeeded; MCP examples run through npx (worker session 20 continued)

The manual publish succeeded: `npm publish --access public --tag next` from
`npm/slivingdoc`. The registry dist-tags are `latest` and `next`, both
`0.1.0-rc10` (npm set `latest` on the first publish), and
`npx -y slivingdoc@next version` prints `slivingdoc 0.1.0-rc10` with exit 0.
The remaining MCP example that referenced the local development binary
(`examples/terraform/README.md`, `"command":
"/home/imago/Projects/public/slivingdoc/.build/slivingdoc"`) now runs
through the npm launcher (`npx -y slivingdoc serve ...`) like the root
README, the MinIO example, and the v1 specification — all four example
configurations are now consistent. Docs-only change; no tests affected.

### 2026-08-12 — automatic npm publication on tagged releases (worker session 21)

The npm publish was manual until now. A `publish-npm` job in
`.github/workflows/release.yml` now runs after the reusable release job
(`needs: [release]`), so the GitHub release and every asset exist before the
npm gate runs; the workflow fires only on tag pushes (`if: github.ref_type
== 'tag'`). The job asserts that `npm/slivingdoc/package.json` reports the
tag version (a mismatched tag fails with a diagnostic instead of publishing
the wrong version), then publishes with the `next` dist-tag for a
prerelease version or the `latest` dist-tag for a stable version, mirroring
the reusable pipeline's own prerelease classification. The `prepublishOnly`
gate (`scripts/check-release.mjs`) still runs inside `npm publish` and
remains the guarantee that npm never precedes a complete GitHub release.

Credentials: the pipeline uses npm trusted publishing (OIDC) instead of a
token — the answer to the earlier "I am not comfortable storing npm
credentials" concern. The npm-side trust is configured once on npmjs.com
for the `slivingdoc` package: owner `baalimago`, repository `slivingdoc`,
workflow `release.yml`, no environment (decision record: no GitHub
environment; semver-driven dist-tags). The job runs on a GitHub-hosted
runner with `permissions: id-token: write` and asserts npm CLI >= 11.5.1.
No secret is stored in the repository; provenance attestations are
automatic. `npm/slivingdoc/package.json` gained the public `repository`
field that npm requires for provenance.

Validation: `actionlint` clean on the modified workflow; `npm test --prefix
npm/slivingdoc` passes all 35 tests; `node scripts/check-release.mjs` still
reports all 7 required assets for v0.1.0-rc10; `scripts/check-release-ref.sh`
passes on the unchanged pipeline SHA; the dist-tag and npm-version
classifications were exercised with bash (prerelease -> next, stable ->
latest, build metadata stripped, npm 11.4.0 rejected, 11.5.1+ accepted).
The human steps remaining: configure the npm-side Trusted Publisher and
watch the first tag-push end-to-end run.

### 2026-08-12 — first release run failed; make release added (worker session 21 continued)

The first end-to-end run (tag `v0.1.0-rc11`) built all five targets and
created the GitHub release, then the `publish-npm` job failed with the
version-sync gate: `package.json version '0.1.0-rc10' does not match tag
version '0.1.0-rc11'`. The gate did its job — the npm publish was refused
because the package version had not been bumped to the tag — but the human
release flow made that mistake easy: nothing reminded the releaser to bump
`npm/slivingdoc/package.json` before tagging.

Fix: a single release entry point. `scripts/release.sh` (wired as `make
release VERSION=<semver> MESSAGE=<description>`) validates the version
against the pipeline's exact semver pattern, refuses an existing tag, an
unchanged package version, a dirty working tree, or a non-master branch,
bumps `npm/slivingdoc/package.json` (version line only, indentation
preserved), runs the npm launcher tests, commits `release: v<version>`,
creates the annotated tag with the description, and pushes branch and tag.
The tag push then runs the release workflow with the versions guaranteed in
sync. `README.md`, the architecture section 21, and the Makefile document
the command.

Validation: `bash -n` is clean on the script; shellcheck is not installed
in the sandbox. The semver pattern accepted 8 valid and rejected 8 invalid
versions; the node bump preserved the file's two-space indentation and
produced valid JSON with the new version; the script refused an invalid
semver, an unchanged version, and a dirty tree with exit 1 and clear
diagnostics. The commit/tag/push sequence itself could not be executed in
the sandbox (git writes are banned for worker sessions); its commands are
plain git and were reviewed line by line.

Human follow-up for `v0.1.0-rc11`: either delete the release and tag
(`gh release delete v0.1.0-rc11 --cleanup-tag`, `git tag -d v0.1.0-rc11`)
and re-cut it with `make release`, or cut `v0.1.0-rc12` and leave the
orphan release. The npm-side Trusted Publisher (owner `baalimago`,
repository `slivingdoc`, workflow `release.yml`, no environment) must be
configured before the next publish can authenticate.

### 2026-08-12 — make release redesigned as an interactive Go script (worker session 21 continued)

The first real use of the pipeline exposed the workflow gap: tagging
`v0.1.0-rc11` without first bumping `npm/slivingdoc/package.json` made the
version-sync gate fail the npm publish (the gate worked; the release flow
allowed the mistake). The one-shot `make release VERSION=... MESSAGE=...`
bash wrapper fixed the sync but required remembering two arguments. The
user then asked for a fully interactive release entry point in Go,
following the shebang-driven script pattern of
`sakfraga/scripts/validate_se_municipalities.go`.

`scripts/release.go` (replaces `scripts/release.sh`): the first line
`/*usr/local/go/bin/go run "$0" "$@"; exit; */` runs the file as a Go
program from any shell (the kernel's ENOEXEC fallback re-executes it
through the shell), exactly the sakfraga pattern the user requested. The
file carries no build tag and no flags: it is a plain `main` package under
`./scripts`, which the QA gate tolerates (gofumpt, go vet, go fix, and
staticcheck are clean on it, and the last full coverage run at 85.9 %
leaves ample margin above the 70 % floor). The script verifies the working
directory is the repository root, refuses a non-master branch and a dirty
tree, prints the five most recent release tags (version, tag, date; the
current package version is marked), prompts for the new version in a
re-prompt loop (semver validity against the pipeline's exact pattern, not
the current version, no existing local or remote tag), prompts for the tag
description (default `Release v<version>`), bumps
`npm/slivingdoc/package.json` on the version line only, runs the npm
launcher tests, and then commits, annotates the tag, and pushes branch and
tag. There is no dry-run and no argument passing: `make release` just runs
it.

Validation: the shebang trick was proven in /tmp stubs and the script
executes in the real repository, refusing the dirty tree with a clear
diagnostic; gofumpt, go vet, go fix, and staticcheck are clean on the
file; a fixture repository (real history, clean worktree) exercised the
interactive flow: the recent-releases table renders with real tags, an
invalid semver, the current version, and an existing tag each re-prompt
with the correct error, EOF aborts before any mutation, and the version
bump was verified on the fixture (version line only; indentation and all
other fields untouched). The abort-after-npm-test-failure path was not
executed end to end (forcing a failure made the fixture tree dirty, so the
dirty guard correctly fired first); it is reviewed by inspection, as is
the commit/tag/push sequence, which is plain git invoked via exec — the
sandbox cannot execute git writes.

Human follow-up for `v0.1.0-rc11` (unchanged): either delete the release
and tag (`gh release delete v0.1.0-rc11 --cleanup-tag`, `git tag -d
v0.1.0-rc11`) and re-cut it with `make release`, or cut `v0.1.0-rc12` and
leave the orphan release. The npm-side Trusted Publisher (owner
`baalimago`, repository `slivingdoc`, workflow `release.yml`, no
environment) must be configured before the next publish can authenticate.
