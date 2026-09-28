# Git engine: `internal/git` and `internal/git2`

slivingdoc uses Git objects, packs, and three-way merge internally, through libgit2 called in-process. It never runs a Git binary, never uses `git2go`, and never asks libgit2 for a network transport (S3 access belongs to the Go process). The code is split in two: `internal/git` is the pure-Go seam (the `Engine` and `Repository` interfaces) plus all policy built on top of narrow native operations; `internal/git2` is the only CGo package and implements the seam against a pinned, statically linked libgit2 v1.9.6. This doc answers "which Git operation lives where, what rules do trees, paths, packs, and OIDs follow, and how is the CGo boundary kept narrow".

Read this when: adding or changing a native operation, touching tree building, snapshot reading, pack export/import, history validation, path or content rules, the path policy, OID handling, or the libgit2 pin.

## Key files

| File | Purpose |
|------|---------|
| `internal/git/engine.go` | Package doc; `Engine` (`Open`, `Close`, `Version`, `Features`, `CreateRepo`, `OpenRepo`), `Repository` (12 narrow ops + `Close`), `Features`, `FeaturesFromMask`, `VersionMismatchError`, `NativeError`, `OID` (`[20]byte`) and `OID.String` |
| `internal/git/types.go` | `FileMode` (`ModeBlob` 100644, `ModeTree` 040000), `TreeEntry`, `Commit`, `CommitSpec`, `AuthorName`/`AuthorEmail`, `IndexEntry`, `MergeIndex`, `MergeFileResult`, `MarkerRange`, `Conflict`, `MergeResult`, `Snapshot`, `File`, `Pack` |
| `internal/git/oid.go` | `ParseOID` (40 lowercase hex only), `IsZero`, `MarshalJSON`, `ErrInvalidOID` |
| `internal/git/tree.go` | `BuildTree`, `ReadSnapshot`, `walkTree`, `EmptyTree`, `SortTreeEntries`, `treeEntryLess` |
| `internal/git/pack.go` | `ExportIncrement`, `ExportCheckpoint`, `ImportPack`, `MarkShallow`, `ValidateHistory`, `ValidateTree`, `writePack`, `reachableFromCommit`, `treeClosure`, `treeClosureValidate` |
| `internal/git/commit.go` | `CreateCommit`, `ValidateCommitMessage` |
| `internal/git/merge.go` | `Merge`, `MaterializeTree`, `FindConflictBlocks`. See [conflicts.md](./conflicts.md) |
| `internal/git/path.go` | `ValidatePath`, `validateSegment`, `isWindowsDeviceName`, `ValidateContent`, `ValidateSnapshot`, `ValidateFoldedDirectories`, `FoldedDirectoryPairs`, `PathCollisionError` |
| `internal/git/policy.go` | `PathPolicy`, `NewPolicy`, `Configured`, `Protects`, `ChangedProtected`, `RestoreProtected`, `OverlapError` |
| `internal/git/readonly.go` | `EntrySet`, `NormalizeEntries`, `validateEntries`, `collapseEntries`, `Covers`, `CoveringEntry`, `ChangedUnder`, `Pin`, `ReadCovered` |
| `internal/git/diffstat.go` | `DiffSnapshots`, `DiffStat`, `FileStat` (Myers middle-snake line diff) |
| `internal/git/errors.go` | Named causes `ErrNoNewObjects`, `ErrObjectMissing`, `ErrEmptyPack`, `ErrHeadRequired`; `UnsupportedModeError` |
| `internal/git/gittest/gittest.go` | `ObjectID`: fake object hashing for fakes in `notebook` and `workspace` tests |
| `internal/git2/doc.go` | Package doc: the only CGo/libgit2 boundary; no pure-Go build |
| `internal/git2/engine.go` | `PinnedVersion` ("1.9.6"), `New`, `engine` (`Open` version check), `repository` (mutex-guarded ops), `reloadShallowGrafts` |
| `internal/git2/native.go` | cgo directives and C helpers (`sl_merge_file`, `sl_odb_writepack_*`, OID copy); seam variables `initFn` … `markShallowFn`; `libgit2*` functions |

## Flow

```text
main.go: git2.New() → cli.Run → cmd/<command>.Setup → app.Setup → engine.Open()
         (libgit2 init + exact version check)
workspace.Open → Engine.CreateRepo/OpenRepo(<P>/repo) → git.EmptyTree
Notebook uses package functions over ws.Repo():
  BuildTree(snapshot)         → Repository.WriteBlob / WriteTree
  ReadSnapshot(tree)          → Repository.ReadTree / ReadBlob (+ ValidateSnapshot)
  Merge(base, local, remote)  → Repository.MergeTrees / MergeFile
  CreateCommit(spec)          → Repository.CreateCommit
  ExportIncrement / ExportCheckpoint → reachable set → writePack → Repository.WritePack
  ImportPack(bytes)           → Repository.ImportPack
  MarkShallow(head)           → Repository.MarkShallow → reloadShallowGrafts
  ValidateHistory(head, shallow) → ReadCommit / ReadTree / HasObject
git2.repository.X → r.mu lock → usable() → xxxFn (seam var) → libgit2Xxx (cgo) → libgit2
```

## Behavior

### Narrow CGo boundary

- All CGo and libgit2 types stay inside `internal/git2`; the exported API uses only `internal/git` types. OIDs cross as 20-byte copies (`sl_oid_to_bytes`/`sl_oid_from_bytes`); native errors are copied into `git.NativeError{Op, Class, Message}` at failure time so they outlive the handles.
- The native surface is only: init/shutdown/version/features; create or open a repository; write/read/exist blob; write/read tree; create/read commit; merge trees; merge file; write pack; import pack; mark shallow. Everything else (validation, ordering, conflict structuring, pack planning, history checks) is Go policy in `internal/git`.
- `Open` fails with `VersionMismatchError` unless the runtime libgit2 is exactly `PinnedVersion` 1.9.6, and keeps the init/shutdown count balanced. Every repository call checks that the engine is still open and the repository not closed (`usable`).
- Each `repository` serializes its native calls with one mutex; different repositories run in parallel.
- libgit2 is linked statically: on Linux from `.build/libgit2` (built by `scripts/build-libgit2.sh` via the Makefile), on macOS and Windows through `pkg-config --static libgit2`. There is no pure-Go fallback: `CGO_ENABLED=0` fails to compile rather than shipping a binary that fails every call. Users need no Git, libgit2, C compiler, or CGo.
- Native seam functions are package variables (`initFn`, `mergeTreesFn`, …) so white-box tests can inject failures; a test must restore the original.

### Trees, modes, and commits

- Only two modes exist: `100644` for every file and `040000` for every directory. Everything else (symlinks, submodules, executables) is rejected: the policy walkers (`walkTree`, used by `ReadSnapshot` and conflict-free `MaterializeTree`; `treeClosure`; `treeClosureValidate`; `PathPolicy.collectSide`) and `Merge` return `UnsupportedModeError`; `libgit2WriteTree` refuses with a plain `NativeError` (no caller detail); conflicted-path `MaterializeTree` fails with "unexpected index shape" for a non-blob entry. Executable bits and other host modes are not notebook state.
- `BuildTree` validates the snapshot, sorts by path, writes blobs and subtrees recursively, and sorts entries in Git tree order (`treeEntryLess`: byte order with directories compared as if suffixed by `/`, matching libgit2's `git_tree_entry_cmp`). The same snapshot always yields the same tree OID.
- `ReadSnapshot` walks a tree, rejecting bad modes, unsafe names, and invalid content, so a hostile pack fails before any caller sees its state. Output is sorted by path.
- `EmptyTree` writes the canonical empty tree (`4b825dc642cb6eb9a060e54bf8d69288fbee4904`); `workspace.Open` ensures it exists in every repository.
- `CreateCommit` requires a non-zero tree and a message that is valid UTF-8 without U+0000. Identity is fixed `slivingdoc <slivingdoc@localhost>`; time is the spec time as Unix seconds with offset zero. `CreateCommit` accepts any parent list; each parent must exist locally (`git_commit_lookup` failure goes through `nativeLookupError`, so a missing parent carries `ErrObjectMissing`). The root-commit and single-parent (observed R head) shapes are notebook policy (`buildFirstProposal`, `buildIncrementProposal`).
- Commit history is an internal aid for recent ancestry and pack planning, not a product contract.

### Three-tree merge

`Merge(repo, base, local, remote)` passes three explicit trees to `git_merge_trees` with no flags (no rename detection, no fail-on-conflict) and no merge-base discovery, driver, config, or attributes. Marker labels are `local`/`remote` (an ancestor label `base` is set but not shown in the default marker style). Details of conflict structuring and materialization: [conflicts.md](./conflicts.md).

### Packs

- `ExportIncrement(head, base)`: every object reachable from `head` (commits, trees, blobs, full ancestry) minus everything reachable from `base`. Importing it into a repository that holds `base` reconstructs `head`. Empty difference is `ErrNoNewObjects`.
- `ExportCheckpoint(head)`: the head commit plus its complete tree closure, no ancestors. Imports into an empty repository. See [checkpoints.md](./checkpoints.md).
- `writePack` sorts objects by OID and hashes the bytes while writing; `Pack.SHA256` is the checksum stored in manifest descriptors. The packbuilder is single-threaded, so bytes are deterministic for the pinned release, and delta bases always live inside the pack.
- `ImportPack` refuses zero bytes (`ErrEmptyPack`); libgit2's writepack indexer validates the pack and trailer and imports nothing from a truncated or corrupt pack. The notebook has already checked size and SHA-256 against the descriptor.
- `MarkShallow` appends the OID to `<gitdir>/shallow` (idempotent) and reopens the repository and object database unless this handle has already loaded that boundary (`repository.grafts`, read from the file at open and after each reload), because libgit2 loads shallow grafts only on open. A boundary another process appended to a shared private repository is therefore honored the first time this handle marks it; a boundary this handle already holds costs one file read.
- `ValidateHistory(head, shallow)` walks every commit from `head`, reading every tree (`ReadTree`) and proving every blob with `HasObject` (never a full blob read); only the declared shallow commit may name missing parents. With libgit2, every commit listed in the `shallow` file reads with zero parents, so the walk stops at the first shallow-listed commit (the notebook package's fake repository mirrors this). One seen set spans the walk, so cost is proportional to unique objects.

### Paths and content (`path.go`)

- Internal paths: valid UTF-8 in NFC, `/`-separated, no leading or trailing slash, at most 4,096 bytes. Segments: 1 to 255 bytes, no control characters, none of `\:*?"<>|`, not ending in space or dot, not `.`, `..`, `.git` (case-insensitive), not a Windows device name (`CON`, `PRN`, `AUX`, `NUL`, `COM1`-`COM9`, `LPT1`-`LPT9`, with or without extension). The rules apply on every host so one notebook is portable.
- Content: valid UTF-8 without U+0000. Bytes and line endings are preserved; nothing is normalized. Empty files are valid.
- `ValidateSnapshot` checks every path and content and rejects two paths equal under Unicode case folding (`PathCollisionError`, `Fold` set) or duplicated, and a file whose exact name is also a directory of another path (`PathCollisionError`, `Dir` set, `First` the file); `BuildTree` would otherwise write two entries of one name. It runs on local scans, `BuildTree`, `ReadSnapshot` of accepted state, and conflicted merge results alike.
- `ValidateFoldedDirectories` adds the case-folded form of that rule (`README` beside `readme/x.md`, `Fold` and `Dir` set) for new content only. The workspace scan runs it after `ValidateSnapshot`, and a commit runs `FoldedDirectoryPairs` on its clean merged state before any upload (`rejectNewFoldedPairs` in `internal/notebook/commit.go`): each writer's directory can be valid alone while the merge pairs one writer's `P` with another's `p/x.md`, so a pair R does not already hold whole is refused as `INVALID_REQUEST`/`INVALID_CONTENT` naming both paths, with L and P untouched ([commit.md](./commit.md)). Accepted remote state and merge results are otherwise held to the exact rule alone, so a notebook an older writer published with such a pair stays readable instead of failing every call as `STORAGE_INTEGRITY`, and a commit that merges a pair R already holds is not refused for it. A pull is not checked: pulling such a pair from R (or a commit's acceptance of a tolerated one) writes both names into L. On a case-sensitive host the next scan then refuses them as `INVALID_REQUEST`/`INVALID_CONTENT` naming both paths until one is renamed and the rename committed; on a case-insensitive host the two names meet on disk.
- A folded pair under a protected path cannot be repaired from a process that protects it: its scan refuses the pair, a rename there is a protected change the commit resets, and a pull restores both names. The operator renames one of the two from a process without that protection (one started without the `--read-only-paths` or `--writable-paths` setting that covers the pair), commits, and the protected processes' next pull takes the rename.
- Every `Repository.WriteTree` runs `CheckUniqueNames` (`internal/git/tree.go`) first: the native boundary (`repository.WriteTree` in `internal/git2/engine.go`) and the test fakes alike refuse a repeated entry name, because libgit2's tree builder replaces an entry on a repeated insert and a file and a directory of one name would silently lose one.

### Path policy (`policy.go`, `readonly.go`)

`PathPolicy` composes the read-only and writable `EntrySet`s by longest match; `ChangedProtected(local, base)` descends only into subtrees whose OIDs differ and reads no blobs; `RestoreProtected` takes named paths back from the base tree. Semantics: [product-contract.md](./product-contract.md#read-only-and-writable-paths).

### OIDs

`OID` is a 20-byte SHA-1. `ParseOID` accepts only the canonical 40 lowercase hex characters (uppercase is rejected). `MarshalJSON` writes that form into the manifest. OIDs never appear in caller-facing output.

## Gotchas

- `EntrySet.ChangedUnder`, `EntrySet.Pin`, and `EntrySet.ReadCovered` have no production callers; production uses `PathPolicy.ChangedProtected` and `RestoreProtected`.
- `Repository.MergeTrees` returns a zero `Tree` whenever the index holds conflicts; `Merge` also treats a conflict-free index with a zero tree as an error.
- The private repository is created non-bare (`libgit2CreateRepo(path, false)`), so its git directory is `<P>/repo/.git`; `libgit2RepoPath` resolves it for the `shallow` file.
- `internal/git`'s own tests cannot import `gittest` (import cycle) and keep a local copy of the hashing helper.
- Engine failures reach callers only as a closed set of named causes (`errors.go`); any new classifiable failure should get a sentinel there and an entry in `mcp.safeEngineDetail`.
- `ErrObjectMissing` is produced only by `nativeLookupError` (on `GIT_ENOTFOUND`) and `treeClosureValidate`. A new native lookup must go through `nativeLookupError` or a missing object loses its named cause.

## Related

- [conflicts.md](./conflicts.md), [checkpoints.md](./checkpoints.md), [workspace.md](./workspace.md), [notebook.md](./notebook.md), [overview.md](./overview.md)
- `../AGENTS.md`: [Architecture](../AGENTS.md#architecture), [Package Map](../AGENTS.md#package-map), [Conventions](../AGENTS.md#conventions) (CGo confinement), [QA validation](../AGENTS.md#qa-validation) (CGo mode)
- Other concerns: [build.md](./build.md), [errors.md](./errors.md)
