# Decisions, deferred work, and acceptance invariants

The recorded v1 architecture decisions, the work deliberately left out of v1, and the invariants a change must preserve. It answers "was this a deliberate choice, is this feature intentionally missing, and what must never break?"

Read this when: proposing a change that reverses or extends a design choice, picking up deferred work, or reviewing a change against the core invariants.

## Key files

| File | Purpose |
|------|---------|
| `AGENTS.md` | "Invariants that a change must not break" and the error taxonomy rules ([Conventions](../AGENTS.md#conventions)) |
| `internal/storage/manifest.go` | `Manifest` version 1, the only accepted-state authority ([storage.md](./storage.md)) |
| `internal/notebook/notebook.go` | The configurable defaults named below (`DefaultCheckpointPacks`, `DefaultRetainedCheckpoints`, `DefaultRetryLimit`) |
| `internal/git2/engine.go` | `PinnedVersion` and the runtime version check in `Open`; the CGo itself is in `native.go` |
| `internal/mcp/server.go` | The two tools |
| `internal/integrationtest/` | Scenarios that encode these decisions as executable contract |

## Flow

```text
proposal → check "Decisions recorded" (reversal?) and "Deferred work" (already scoped out?)
        → check "Acceptance invariants" and AGENTS.md invariants
        → write or update the black-box scenario first (testing.md)
        → update the affected architecture doc in the same change
```

## Behavior

### Decisions recorded

Where the code has since diverged, the code wins and the note says so.

1. The MCP server is the product and the only programmatic protocol.
2. The server exposes `notes_pull(path)` and `notes_commit(path, message)`. (Code: `path` is optional in both; an omitted or empty path is the server's notebook directory.)
3. Users do not install Git or libgit2.
4. A narrow internal CGo package (`internal/git2`) calls a pinned libgit2 release.
5. The AWS Go SDK performs all S3 operations (only inside `internal/s3store` in production code; the test-only `internal/tests3` uses it to create the test bucket).
6. S3 stores immutable incremental Git packs and one `current` manifest.
7. `current` indexes the active checkpoint and the ordered incremental tail.
8. Publication uses ETag CAS and randomized bounded retries.
9. There is no S3 writer lock.
10. The notebook supports UTF-8 text files without U+0000 only.
11. libgit2 performs three-tree text merges.
12. Binary files are rejected before merge or publication.
13. Checkpointing starts automatically at a configurable pack count.
14. The default checkpoint threshold is 256 active increments (`notebook.DefaultCheckpointPacks`). The original spec said 1,024; the code wins.
15. A checkpoint can compact a stable prefix while newer commits continue.
16. Checkpoints preserve current state, not permanent Git history.
17. The current and previous checkpoint generations are retained by default (`--retained-checkpoints` 1).
18. A later checkpoint cleans older physical storage on a best-effort basis.
19. S3 versioning and backups are deployment policies.
20. V1 targets high agent concurrency and avoids a full-history upload per commit.
21. External resources have mockable semantic boundaries.
22. SeaweedFS testcontainers prove the real S3 integration contract.
23. The npm launcher downloads and verifies native GitHub release artifacts.
24. L is caller-controlled visible files, P is private local state, and R is accepted remote state.
25. Synchronization occurs at MCP operation boundaries, without a file watcher.
26. Pull merges local changes onto R and can rewrite L with conflict markers.
27. The first pull uses the canonical empty tree as its baseline.
28. V1 accepts valid UTF-8 text without U+0000 and rejects binary files.
29. Commit rejects complete slivingdoc conflict-marker blocks.
30. An unprovable ambiguous publication returns failure and is not replayed.
31. Manifest version 1 is a strict normative JSON protocol.
32. Publication and checkpoint identifiers are UUIDv7.
33. Active and retained descriptors are the publication receipts.
34. Cleanup uses the successful checkpoint cutoff as its candidate fence.
35. Retained generations contain complete reconstructable descriptor chains.
36. Unexpected partial local mutation uses one generic recovery path.
37. The `pull` and `commit` subcommands expose the same two operations to humans; they reuse the serve startup surface and print the envelope as a candid text report.
38. Read-only paths are per-process configuration (`--read-only-paths` / `SLIVINGDOC_READ_ONLY_PATHS`), not notebook or manifest state; they are enforced at commit (refuse and reset) and restored at pull. Writable paths (`--writable-paths` / `SLIVINGDOC_WRITABLE_PATHS`) are the same kind of configuration: the two sets resolve by longest match, a non-empty writable set protects every unmatched path, and both are advertised on every surface that lists a set.

### Deferred work

Outside v1:

- byte-based checkpoint thresholds
- multi-level pack compaction
- multiple independently ordered notebook partitions
- permanent or configurable logical history
- a public backup and restore API
- an administrative status API
- remote file-editing tools
- Windows arm64 artifacts
- a public Go SDK

The versioned manifest permits storage-format evolution; MCP callers do not depend on the internal representation. A hosted storage backend is in progress in a separate PR.

### Acceptance invariants

A change preserves all of these:

- `current` is the only accepted-state authority.
- Packs are immutable and uploaded before the manifest references them.
- Conditional manifest replacement prevents lost updates.
- CAS contention causes merge and retry, not silent overwrite.
- Conflicts rewrite L with merge results and markers; unexpected partial local mutation enters the explicit recovery path and never returns `OK`.
- Checkpoints bound cold-start pack count without blocking other writers (the effort runs inside the triggering commit call, after its acceptance).
- Cleanup never determines commit success.
- The executable has no runtime Git or libgit2 installation requirement.
- Unit tests can replace every network service with a deterministic fake.
- Required integration tests run against a real S3-compatible store (SeaweedFS) through testcontainers.

## Gotchas

- The original spec's 1,024 checkpoint default is obsolete; the code uses 256 (`DefaultCheckpointPacks`).

## Related

- [storage.md](./storage.md), [commit.md](./commit.md), [checkpoints.md](./checkpoints.md): where the storage decisions are implemented.
- [build.md](./build.md): distribution decisions.
- [testing.md](./testing.md): how decisions become scenarios.
- [overview.md](./overview.md): the system summary.
