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
22. A pinned SeaweedFS container proves the real S3 integration contract.
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
39. A first pull proceeds only into an empty directory, into one whose every file the notebook already has with identical bytes (a protected file only needs to exist in the notebook, which restores it), or against an empty notebook (seeding it with unprotected files); otherwise it is refused as `DIRECTORY_NOT_EMPTY`, naming the files, before L, the pulled marker or `state.json` changes ([pull.md](./pull.md)).
40. The slivingdoc hosted storage API is a second `ObjectStore` adapter (`internal/httpstore`) below the unchanged protocol, selected by a non-empty `SLIVINGDOC_TOKEN` read from the environment only (decision 46 adds a stored login); `--bucket` names the space (optional since decision 43; `--space` and `SLIVINGDOC_SPACE` are its hosted names, decision 47). Its server promises conditional writes, so startup runs `CheckAccess` instead of the write probe, and a read-only token can pull ([hosted-mode.md](./hosted-mode.md)).
41. Store account refusals (full space, request allowance, throttling, denied credentials, oversized object) are `STORAGE_FAILURE` reasons; all but throttling are not retryable, and the store's own one-line message is appended to the caller's message.
42. A commit refused because the space is full is published as a whole-state checkpoint when that shrinks the space, keeping no retained generation, so deleting notes frees room (an exception to decision 17).
43. A hosted token reaches exactly one space, so the process takes the space from the token (`GET /v1/token`) and `--bucket` is optional in hosted mode. A `--bucket` naming another space refuses startup instead of either side winning; against a server without the lookup, a given `--bucket` keeps working ([hosted-mode.md](./hosted-mode.md)).
44. The S3 compatibility probe proves a property of the endpoint, so its success is recorded (`probe-ok.json`) in the identity's shared directory and reused by the same slivingdoc version and store configuration for 24 hours instead of re-proved by every process; credentials are not bound, so a credential failure moves from a startup refusal to the first request. A one-shot `pull` otherwise spends most of its time on the probe's nine dependent round trips against a distant bucket. For hosted stores, and on a host without a shared pack cache, startup is unchanged. The trade is that the endpoint's conditional-write behavior is trusted for up to 24 hours, so an endpoint that silently loses `If-Match` inside the window is not refused and a publication can be lost; a long-lived `serve` process already carried that exposure without a bound ([storage.md](./storage.md), [running.md](./running.md#the-shared-pack-cache)).
45. The shared pack cache is always on; `--shared-pack-cache` and `SLIVINGDOC_SHARED_PACK_CACHE` are retired (an unknown flag now). Pack entries are verified on read, so sharing them has no correctness cost, and the recorded proof and the reused imports only pay off when the cache survives the process. A host without a user cache directory, or a workspace root above it, keeps the private per-workspace cache instead of refusing ([config.md](./config.md), [running.md](./running.md#the-shared-pack-cache)).
46. `slivingdoc login` is an account login: a browser device approval issues an account CLI key, stored in a strict, versioned credentials file (0600, version 2) with one login per (site, endpoint) and one default space per endpoint; a file of any other version is refused by every command that reads it, `login` included, and never rewritten: an older one with `remove <file> and run 'slivingdoc login'`, a newer build's with `update slivingdoc`, keeping its live keys. The key never reaches storage: it is sent only to its site, which lists the account's spaces (`slivingdoc space` lists them and `slivingdoc space <name>` sets the default) and mints tokens of one space that last at most an hour; a process mints one for its space (the flag, then the variable, then the stored default; none is a refusal naming `slivingdoc space <name>`), keeps it in memory only, sends it only to the login's endpoint, and mints again at 80 % of its lifetime and once after a 401; `logout` revokes the key and so every minted token. `--storage auto|hosted|s3` chooses the store: `SLIVINGDOC_TOKEN` selects hosted under `auto` regardless of S3 settings; `--endpoint` then names the hosted API, AWS endpoint variables are ignored, and `--storage s3` explicitly selects S3 without reading the token or credentials file. A stored login used with an explicitly named space plus any S3 setting is refused as ambiguous under `auto`; the variable's token alone is enough: with no space it uses its own (decision 43), and a token process never reads the credentials file; a named space is refused when the token lookup names another, and on a server without the lookup only the access check proves the token reaches it ([login.md](./login.md)).
47. `--space` and `SLIVINGDOC_SPACE` are the hosted names of `--bucket` and `SLIVINGDOC_BUCKET`, one setting on `serve`, `pull`, `commit` and `login` (where it sets the default space), because a hosted notebook lives in a space, not a bucket. A flag still beats the environment and an explicitly empty flag does not fall back; the two spellings of one layer with different values are refused rather than one preferred; refusals and hints name the spelling the operator used, and hosted hints that name no source say `--space` ([login.md](./login.md), [hosted-mode.md](./hosted-mode.md)).
48. The storage identity carries the hosted server's space id (`GET /v1/token` and the mint answer name it as `spaceId`), because a space name is unique within one account only: two accounts' spaces called "notes" at one endpoint shared a directory's private state, and a commit there published one account's edits into the other's space. The id is hashed last and only when set, so S3 identities and servers without ids keep their keys; existing hosted directories get new private state on upgrade, whose first pull refuses a directory that differs from the space ([hosted-mode.md](./hosted-mode.md), Gotchas). A stored login's process stays on the first space id its mints name.

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

The versioned manifest permits storage-format evolution; MCP callers do not depend on the internal representation.

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
- Required integration tests run against a real S3-compatible store (SeaweedFS) in a Docker container that `internal/tests3` starts through the Docker Engine API; the hosted adapter runs the same contract suite against the in-process reference gateway.

## Gotchas

- The original spec's 1,024 checkpoint default is obsolete; the code uses 256 (`DefaultCheckpointPacks`).

## Related

- [storage.md](./storage.md), [commit.md](./commit.md), [checkpoints.md](./checkpoints.md), [hosted-mode.md](./hosted-mode.md): where the storage decisions are implemented.
- [build.md](./build.md): distribution decisions.
- [testing.md](./testing.md): how decisions become scenarios.
- [overview.md](./overview.md): the system summary.
