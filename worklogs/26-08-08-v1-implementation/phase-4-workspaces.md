# Phase 4 — Managed workspaces

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 7 (L186), 18.2 (L1109)

## Goal

Implement safe caller directories, deterministic private state, file snapshots,
and local operation serialization.

## Specification

Create `internal/workspace`. A managed workspace binds one canonical visible
path to one notebook storage identity and one private repository directory.

Derive the private directory with a stable digest. The digest input includes
the canonical visible path, normalized S3 endpoint, region, bucket, prefix, and
manifest version. Use SHA-256 over a length-prefixed encoding. Do not include
raw caller paths in the private directory name.

Implement safe recursive scans for valid UTF-8 text files without U+0000. Empty
files are valid, and scans preserve bytes and line endings. Scans must not
follow symlinks. Normalize internal relative paths and reject traversal,
unsupported names, binary content, special files, and file-versus-directory
ambiguity.

Apply the portable path grammar in architecture section 7.1 (L188). Ignore empty
directories as notebook state. Treat hard-linked files as independent paths.
All valid files below L are notebook state. V1 has no ignore mechanism.

Require an absolute MCP path at or below the absolute workspace root. Use Go
`os.Root` relative operations for scan, stage, replacement, and cleanup. Do not
use a check-then-open pathname sequence.

Implement replacement through a staging directory and controlled rename or
copy fallback. A semantic conflict intentionally rewrites L. An unexpected
failure before replacement leaves L unchanged. A partial replacement enters
recovery. Do not expose `.git` or private metadata.
Use the P and L permissions from architecture section 7.2 (L227).

Provide deterministic failpoints around scan, staging, replacement, baseline
write, and recovery. If local mutation becomes partial, mark P as requiring
recovery. `internal/notebook` owns recovery orchestration: it rereads
authoritative `current`, imports packs, and supplies the reconstructed tree.
Workspace applies that tree to L, rebuilds the baseline, and reports whether
repair completed. Workspace never reads remote state directly.

Persist strict `state.json` version 1 as specified in architecture section 7.2 (L227).
The baseline Git tree is authoritative. A file snapshot is an optional cache.
If no baseline exists, use the canonical empty tree and treat valid files in L
as local additions. Reopening P must retain the baseline across restart.

Persist `recoveryRequired=true` before replacement starts. Clear it only in the
durable state record that contains the new baseline after replacement. Restart
recovery discards local intent and reconstructs P and L from `current`.

Serialize one visible path in-process and across server processes. Locks are
local operation locks, not S3 writer locks. Different visible paths must not
share one local lock. Use an OS advisory lock file in P. Wait until the request
context ends. The file is `<P>/operation.lock`. Use `gofrs/flock` context
locking with a 50 ms retry interval. Do not store a PID or implement stale-lock
recovery. The OS releases the advisory lock when a process exits.

## Integration contract

| Trigger                      | Collaborator       | Observable result                       | Required side effect                        | Prohibited side effect            |
| ---------------------------- | ------------------ | --------------------------------------- | ------------------------------------------- | --------------------------------- |
| Open empty path              | Local filesystem   | Managed workspace opens                 | Private state is created under private root | No `.git` in visible path         |
| Open nonempty first-use path | Local filesystem   | Valid text files become local additions | Empty baseline is recorded                  | No file is silently discarded     |
| Snapshot regular files       | Local filesystem   | Normalized map of bytes                 | Open handles close                          | No symlink target read            |
| Materialize tree             | Staging filesystem | Visible files equal source tree         | Obsolete files and empty dirs are removed   | No path outside workspace changes |
| Two operations use same path | Local lock         | Second waits until lock or cancellation | Operations do not overlap                   | No deadlock after cancellation    |

## Acceptance criteria

- [ ] Workspace-root checks reject absolute and relative escapes.
- [ ] Existing symlink components and newly encountered symlinks are rejected.
- [ ] `os.Root` race tests cannot redirect an operation through a substituted symlink.
- [ ] Empty, nested, Unicode, and mixed-line-ending text fixtures preserve bytes.
- [ ] Invalid UTF-8 and U+0000 files fail with every rejected normalized path.
- [ ] Devices, sockets, FIFOs, and unsupported platform names are rejected where testable.
- [ ] Private names reveal no visible path content.
- [ ] Storage identities cannot reuse one private repository accidentally.
- [ ] Baselines survive process-level close and reopen.
- [ ] Baseline comparison reports local additions, modifications, and deletions.
- [ ] A first pull treats a nonempty valid path as additions from the empty baseline.
- [ ] Materialization makes L equal the full target tree and removes obsolete files.
- [ ] Empty directories disappear and hard-link identity is not preserved.
- [ ] `state.json` strict decoding, atomic replacement, and corruption tests pass.
- [ ] Failure injection proves staging and recovery behavior.
- [ ] Every local mutation boundary has a deterministic failure-injection test.
- [ ] A recovery-required workspace refuses normal work until resync completes.
- [ ] Same-path operations serialize under the race detector.
- [ ] Different paths can scan and stage concurrently.

## Error coverage

| Failure                              | Expected outcome                                          | Required test                        |
| ------------------------------------ | --------------------------------------------------------- | ------------------------------------ |
| Requested path escapes root          | Invalid-request error                                     | Traversal table test                 |
| Symlink appears during scan          | Invalid-request error and no target access                | Symlink race fixture where supported |
| Unsupported special file exists      | Unsupported-file error with relative path                 | FIFO or socket fixture               |
| Baseline metadata is corrupt         | Recovery failure, no visible overwrite                    | Corrupt metadata fixture             |
| Staging write fails                  | Prior visible state remains or recovery error is explicit | Injected filesystem failure          |
| Rename is unavailable across devices | Safe staged copy fallback                                 | Filesystem seam test                 |
| Local lock owner exits               | Later process can recover local operation ownership       | Subprocess lock test                 |
| Context cancels while waiting        | Prompt cancellation without leaked lock                   | Concurrency test                     |

## Implementation notes

### 2026-08-09 — Implementation (imago, worker session 5)

Implemented `internal/workspace` and the shared strict-JSON tree. The
strict value-tree parser formerly private to `internal/storage` now lives in
the neutral package `internal/strictjson` (gaining Bool support for the
state record); `storage` consumes it with unchanged semantics and its whole
contract suite still passes.

Workspace scope delivered:

- `CanonicalPath`/`canonicalize`: lexical containment of the absolute MCP
  path below the absolute workspace root; the private root is rejected at
  or below the workspace root.
- `DerivedKey`: lowercase SHA-256 of a length-prefixed encoding of the
  canonical visible path, normalized endpoint, region, bucket, prefix, and
  manifest version; no raw path content appears in the private directory
  name.
- `Workspace.Open`: creates L and P (0700), opens the workspace root with
  `os.OpenRoot`, rejects symlink components, creates or opens the private
  Git repository, and loads or creates the strict `state.json` version 1
  record. A corrupt, mismatched, or interrupted record, or a missing or
  corrupt repository, opens the workspace in the recovery-required mode
  instead of failing; normal operations refuse with
  `ErrRecoveryRequired` until `Recover` applies the reconstructed remote
  baseline. A leftover `state.json.tmp` at Open also forces recovery,
  closing the crash window of the file-sync-plus-rename record write.
- Scan: recursive `os.Root`-relative walk, NFC name normalization with
  raw-name on-disk access, Lstat-based symlink rejection, fstat
  re-check after an `O_NOFOLLOW|O_NONBLOCK` open (so a substituted FIFO,
  device, or symlink cannot be read or block the scan), special-file and
  name-grammar rejection, file-versus-directory ambiguity detection, and
  `git.ValidateSnapshot` case-folding rejection.
- Replacement: staging inside P, a durable `recoveryRequired=true` write
  before any L mutation, then either move-aside + rename + backup removal
  or, on cross-device rename failure (`EXDEV`), a copy fallback that writes
  each file through a temporary rename (breaking hard links), clears
  file/directory swaps, and removes obsolete files and empty directories.
  `Replace` (conflict materialization, baseline unchanged) and `Accept`
  (baseline recorded) share one locked mutation path; `Recover` is the
  only operation allowed in the recovery-required mode.
- Serialization: per-path in-process semaphore (context-aware, so a
  waiting caller cancels promptly) plus the `gofrs/flock` advisory lock on
  `<P>/operation.lock` with a 50 ms retry interval; different paths never
  share a lock. A subprocess lock test proves the OS releases the lock when
  the owner exits.
- Failpoints at scan, staging, replacement, baseline write, and recovery,
  each with a deterministic boundary test asserting the exact observable
  state (L unchanged or replaced, durable recovery flag set or clear).

The cross-platform syscall needs (`O_NOFOLLOW`, `O_NONBLOCK`, `EXDEV`)
use `golang.org/x/sys/unix` and `golang.org/x/sys/windows` behind build
tags, so the phase-2 seam scan that forbids `os/exec` and `syscall`
imports keeps passing; the subprocess lock test spawns the test binary
itself via `os.StartProcess`.

Gates run and passed:

```text
go test -race -timeout=30s -count=3 -p 1 ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go run mvdan.cc/gofumpt@v0.11.0 -w -l .
go fix -diff ./...
go run github.com/mibk/dupl@latest -t 80 .
```

`dupl` reports 0 clone groups. The MinIO contract suite and the native
smoke were not re-run in this phase (unchanged code paths).

## Review findings

### Review 1 (2026-08-09)

- [x] R1-03 (Major): The recovery wording implied workspace reads remote state,
  which would cross the semantic object-store boundary. Clarified that
  `internal/notebook` owns the remote read and workspace applies the supplied
  reconstructed tree.
- Verified good: path grammar, os.Root usage, state.json version 1 fields,
  baseline authority, recoveryRequired durability, and local lock semantics
  match architecture sections 7 and 18.2.
