# Workspace: visible and private directories

`internal/workspace` manages one visible directory L and its private state P. It canonicalizes and confines the request path, scans L into a validated `git.Snapshot` without following links, owns the private libgit2 repository and the strict `state.json` baseline record, serializes operations with an in-process semaphore plus a cross-process file lock, and rewrites L in place through a staged, failure-marked procedure. It never reads remote state; the notebook supplies trees and baselines. This doc answers "how does L map to P, what is on disk, and what rules protect L and P".

Read this when: changing path handling, the scan, the P layout, `state.json`, the operation lock, how L is rewritten, recovery-required mode, the derived key, or the shared pack cache directory.

## Key files

| File | Purpose |
|------|---------|
| `internal/workspace/workspace.go` | Package doc; `Engine` (consumer-owned: `CreateRepo`, `OpenRepo`), `Config`, `Workspace`, `Open`, `openPrivateState`, `rejectSymlinkComponents`, `Close`, `Baseline`, `BaselineSnapshot`, `Diff`, `Replace`, `Accept`, `Materialize`, `CacheDir`, `Pulled`, `MarkPulled`, `Recover`, `withOpLock` |
| `internal/workspace/scan.go` | `Snapshot`, `scanLocked`, `scanWalk`, `readVisibleFile`, `collectVisible`, `readDir`, `ScanError`, `ErrSymlink`, `ErrUnsupportedFile`, `ErrInvalidContent` |
| `internal/workspace/materialize.go` | `applyLocked`, `markRecoveryRequired`, `writeStage`, `applyInPlace`, `targetDirSet`, `tempSuffix`, `Failpoints`, `Diff` |
| `internal/workspace/state.go` | `state` (the `state.json` record), layout constants, `Baseline`, `EmptyTreeID`, `newWorkspaceState`, `encodeState`, `decodeState`, `validateState`, `persistState`, `readStateFile`, `ErrRecoveryRequired`, `ErrPartial` |
| `internal/workspace/identity.go` | `Identity`, `ManifestVersion`, `DerivedKey`, `SharedCacheDirName`, `sanitizeCacheComponent` |
| `internal/workspace/path.go` | `canonicalize`, `RootsOverlap`, `ErrInvalidPath`, `PathEscapeError` |
| `internal/workspace/platform_unix.go` | `noFollowFlag = O_NOFOLLOW \| O_NONBLOCK` |
| `internal/workspace/platform_windows.go` | `noFollowFlag = 0` (os.Root and Lstat checks carry the protection) |
| `internal/app/service.go` | `Service.notebookFor` (calls `Open`), `Service.identity` |
| `internal/app/config.go` | Uses `RootsOverlap` to refuse a private root or pack cache at or below the workspace root |

## Flow

```text
Service.notebookFor(path)
  → workspace.Open(ctx, Config{WorkspaceRoot, Path, PrivateRoot, PackCacheRoot, Identity, Engine, Failpoints})
      canonicalize(root, path) → RootsOverlap checks → DerivedKey(canonical, identity)
      MkdirAll(workspace root), MkdirAll(<private-root>/<key>, 0700) → os.OpenRoot(workspace root)
      rejectSymlinkComponents → root.MkdirAll(rel)
      flock(<P>/operation.lock) → openPrivateState (create or open repo, EmptyTree, load/create state.json)

Snapshot   → withOpLock → scanLocked → scanWalk → readVisibleFile → git.ValidateSnapshot
Materialize(baseline, tree) / Accept(baseline) / Recover(baseline)
           → withOpLock → [Recover: markRecoveryRequired] → applyLocked
               git.ReadSnapshot(tree) → clean staging/backup → writeStage(<P>/staging)
               → markRecoveryRequired (durable) → applyInPlace(L)
               → persistState(new baseline, recoveryRequired=false)
MarkPulled → withOpLock → write <P>/pulled via temp + rename
```

## Behavior

### Notebook content rules

- The notebook is directories plus regular UTF-8 text files. Empty files are valid; bytes and line endings are preserved.
- Every file below L is notebook state; there is no ignore file. A stray text file (an editor backup such as `foo~`) is accepted and published as notebook state; a stray binary, symlink, special file, or invalid name refuses the whole pull or commit as `INVALID_REQUEST`/`INVALID_CONTENT` until it is deleted.
- The scan rejects, naming the path in `ScanError`: symbolic links anywhere (`ErrSymlink`), sockets, pipes, devices and other non-regular entries (`ErrUnsupportedFile`), content that is not UTF-8 or contains U+0000 (`ErrInvalidContent`), names that are not UTF-8 or fail `git.ValidatePath` (`ErrInvalidPath`), and a path that is both a file and a directory, including an NFC and an NFD on-disk name that normalize to one path. Two same-type entries whose names normalize to one NFC path are refused by the `duplicate path` / `duplicate directory` branches of `scanWalk`, and names that collide under Unicode case folding by `git.ValidateSnapshot`; neither carries a workspace sentinel, so the notebook reports them as `STORAGE_FAILURE`/`LOCAL_STATE` with no file named. Path rules: [git-engine.md](./git-engine.md#paths-and-content-pathgo).
- On-disk names are normalized to NFC for internal paths; the raw name is used for disk access.
- Hard links are read as independent files; a rewrite replaces the file, so the link is not preserved. Empty directories are not state: materialization creates needed parents and removes every directory the target does not need, including empty directories the user created. Executable bits and host modes are not state.

### Path security

- `canonicalize` requires an absolute root and path, cleans the path lexically, and refuses anything outside the root (`PathEscapeError`, whose caller text names the root but never echoes the path). The relative form is slash-separated and never contains `..`.
- All filesystem access goes through `os.Root` relative operations on the workspace root (no check-then-open on host paths). `rejectSymlinkComponents` refuses an existing symlink in any component of the requested path before creating it.
- Files are opened with `noFollowFlag` and re-checked with `Stat` on the open descriptor, so a substituted symlink, FIFO, or device can neither be read nor block the scan (`O_NONBLOCK`). Directory listings use Lstat semantics; removal during materialization never opens entries, so removing a symlink removes only the link.
- The private root and the shared pack-cache root must not be at or below the workspace root (`RootsOverlap`), otherwise P would become notebook content. Callers can never choose the P directory: it is derived.

### Private directory layout

`<private-root>/<derived-key>/`, created `0700`:

| Entry | Written by | Purpose |
|-------|-----------|---------|
| `state.json` | `persistState` (temp `state.json.tmp`, fsync, rename, `0600`) | Strict v1 baseline record |
| `operation.lock` | `flock` | Cross-process operation lock |
| `repo/` | `Engine.CreateRepo` | Private libgit2 repository (non-bare; object cache, shallow file) |
| `staging/` | `writeStage` | Full target tree written before L is touched; removed after success |
| `backup/` | none (only removed) | Leftover name from an older rename-swap design; `applyLocked` deletes it if present |
| `pulled` | `MarkPulled` (temp + rename) | Durable "a pull initialized P" marker required by commit |
| `pack-cache/` | notebook `cacheWrite` | Verified pack bytes by SHA-256, unless a shared cache root is configured |

- `DerivedKey` = lowercase hex SHA-256 over the strings canonical L path, endpoint, region, bucket, prefix, each prefixed with its 8-byte big-endian length, then the manifest version as a fixed 8-byte big-endian integer (`Identity`, `writeLengthPrefixed`, `writeIdentity`). Length prefixes rule out separator collisions; the digest does not reveal the path. The endpoint must already be normalized by configuration.
- With `--shared-pack-cache` (see [running.md](./running.md#the-shared-pack-cache)), `CacheDir` returns `<user-cache-dir>/slivingdoc/pack-cache/<SharedCacheDirName(identity)>`: sanitized bucket and prefix plus a 16-hex identity digest without the L path, so every workspace of one notebook shares verified pack bytes while repositories and baselines stay per-workspace. The name carries no correctness weight; every entry is re-verified on read.
- Only `serve` is ephemeral: with neither root configured by flag or environment (`--workspace-root`/`SLIVINGDOC_WORKSPACE_ROOT`, `--private-root`/`SLIVINGDOC_PRIVATE_ROOT`), both roots go under one session directory (`os.MkdirTemp("", "slivingdoc-")` with children `notebook/` and `private/`) removed at shutdown, so a temporary L gets a temporary P. Otherwise, and always for CLI `pull`/`commit`, the default workspace root is the working directory and the default private root is `<user-cache-dir>/slivingdoc`. See [running.md](./running.md#the-session-directory).

### `state.json` and the baseline

- Fields in order: `version` (1), `identity` (the derived key, 64 lowercase hex), `remoteGeneration`, `baselineHead` (empty exactly at generation 0), `baselineTree` (40 lowercase hex), `recoveryRequired`. Decoded with `strictjson` (unknown, duplicate, missing, null fields rejected) and cross-checked by `validateState`; encoded compact with HTML escaping off and no trailing newline.
- The baseline tree in P is authoritative. No file snapshot is persisted; `BaselineSnapshot` and `Diff` read the tree when needed.
- A new workspace starts at generation 0 with an empty head, `EmptyTreeID` (`4b825dc642cb6eb9a060e54bf8d69288fbee4904`), and `recoveryRequired=false`.
- `persistState` always restamps `identity` with the derived key, so a recovery write repairs a corrupt or mismatched record.

### Opening and recovery-required mode

- Fresh first use (no state, no repo, no temp file): create the repository, write the empty tree, persist the initial record.
- Any anomaly forces recovery-required mode instead of failing `Open`: missing state with an existing repository (interrupted first init), a leftover `state.json.tmp`, an unreadable or invalid record, an identity mismatch, or a repository that will not open (removed and recreated empty; it is only a cache).
- While recovery is required, every operation except `Recover` returns `ErrRecoveryRequired` from `withOpLock`. The notebook repairs it from `current`; see [guarantees.md](./guarantees.md).

### Rewriting L (`applyLocked`)

- L is only read and written at operation boundaries (an MCP tool call or a CLI `pull`/`commit`); there is no filesystem watcher. This avoids races with partial editor writes and atomic-rename save patterns. Callers must not edit L while an operation on it is running.
- Steps: read and validate the target tree; clear `staging/` and `backup/`; write the complete target into `staging/` (a disk-full or cancelled write fails here with L untouched and no recovery needed); durably set `recoveryRequired=true`; rewrite L in place; persist the new baseline (if any) with `recoveryRequired=false`. `staging/` is only a disk-space and cancellation pre-flight: `applyInPlace` writes from the in-memory snapshot, not from the staged copy.
- `applyInPlace` keeps L's directory identity: L is created if missing but never renamed, replaced, or removed, so shells, editors, watchers, and the workspace's own `os.Root` stay valid. It clears directories where the target has a file and files where it has a directory, writes each file to a random `.slivingdoc-tmp-<hex>` sibling and renames it over the target (atomic per file), removes obsolete files, then removes obsolete directories deepest first. Every target file is rewritten (new inode and mtime) on every materialization, changed or not. Once `applyInPlace` starts, every failure leaves `recoveryRequired=true`; most wrap `ErrPartial` but not all (context cancellation, `collectVisible`, parent-directory creation), so test with `RecoveryRequired()`/`ErrRecoveryRequired`, not `errors.Is(err, ErrPartial)`.
- L directories are created `0755` and files `0644`, subject to umask; Windows inherits the root ACL.
- `Materialize(baseline, tree)` writes `tree` to L and records `baseline` in one failure-atomic operation: this is how pull and conflicts show a merged or marker-bearing tree while recording R. `Accept(baseline)` writes `baseline.Tree`. `Recover(baseline)` marks recovery first, then does the same.

### Operation lock (`withOpLock`)

- Per workspace: a one-slot semaphore (honors the request context) serializes calls in-process, then `flock.TryLockContext` on `operation.lock` (retry every 50 ms until the context ends) serializes across processes. The OS releases the lock on process exit; no PID or stale-lock recovery exists. `Open` takes the same file lock while creating P.
- Different paths lock independently and can work concurrently.

## Gotchas

- The lock is per workspace method, not per notebook operation. `Notebook.Pull` takes it separately for `Snapshot`, `Materialize`, and `MarkPulled`, so two concurrent operations on one path can interleave.
- `app.Service` caches one `Workspace` per request path string; `Workspace.state` is an in-memory copy of `state.json`. Two strings for the same canonical path produce two `Workspace` values over one P, and each trusts its own cached state.
- `Replace`, `Diff`, and `BaselineSnapshot` have no production callers (tests only). `Replace`'s comment calls it the conflict path, but the notebook uses `Materialize`.
- `staging/` files are written `0644` inside P, not `0600`.
- `Pulled` is a plain `os.Stat` outside the lock.

## Related

- [pull.md](./pull.md), [commit.md](./commit.md), [conflicts.md](./conflicts.md), [guarantees.md](./guarantees.md), [git-engine.md](./git-engine.md), [notebook.md](./notebook.md)
- [running.md](./running.md#notebook-rules), [running.md](./running.md#configuration)
- `../AGENTS.md`: [Architecture](../AGENTS.md#architecture) (runtime layout of L, P, R)
- Other concerns: [config.md](./config.md)
