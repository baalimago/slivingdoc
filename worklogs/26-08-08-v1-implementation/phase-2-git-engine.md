# Phase 2 — Git state engine

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 7.1 (L188), 8 (L305), 9.3 (L537), 12 (L763), 13.2 (L835)

## Goal

Implement the internal Git operations for regular-file trees, three-tree merge,
incremental packs, and shallow checkpoint packs.

## Specification

Add the remaining narrow functions to `internal/git2`. Place Go-facing policy
and orchestration in `internal/git`.

The Git engine must:

1. Build a deterministic tree from a validated UTF-8 text-file snapshot.
2. Read a complete UTF-8 text-file snapshot from a tree.
3. Create a commit with a supplied message and zero, one, or an intentionally omitted parent.
4. Merge base, local, and remote trees with libgit2.
5. Return structured conflicts from the merge index.
6. Materialize text conflict markers only for conflicted paths.
7. Export one incremental pack for new objects.
8. Import and validate packs into a local repository.
9. Export a state-complete checkpoint pack with one complete tree and a shallow history boundary.

The engine rejects binary content, invalid UTF-8, U+0000, symlinks, submodules,
special modes, and unsafe path names.

V1 uses Git SHA-1 repositories. All Go-facing object IDs use exactly 40
lowercase hexadecimal characters. SHA-256 is reserved for complete pack-byte
integrity checks.
Rename detection is not a product requirement. A rename is a deletion plus an
addition.

Use fixture repositories and golden pack hashes only where output is stable
for the pinned libgit2 release. Test semantic object and tree results otherwise.

Use mode `100644` for blobs and `040000` for trees. Sort entries by Git tree
order. Use the fixed author and committer identity from architecture section
8.2 (L333). Inject the attempt clock and use UTC second precision. Merge the three
explicit trees without history-based merge-base discovery. Use exact `local`
and `remote` marker labels. Do not load external merge drivers or user Git
configuration.

## Integration contract

| Trigger                | Collaborator                | Observable result                               | Required side effect                          | Prohibited side effect               |
| ---------------------- | --------------------------- | ----------------------------------------------- | --------------------------------------------- | ------------------------------------ |
| Build tree             | Real libgit2                | Returned tree reads as identical files          | Blobs and trees enter private ODB             | No special file mode enters the tree |
| Merge disjoint edits   | Real libgit2                | Result contains both edits                      | Merge index is consumed and freed             | No conflict result                   |
| Merge overlapping text | Real libgit2                | Structured conflict and marker content          | All three conflict variants remain available  | No commit is created                 |
| Pack round trip        | Second temporary repository | Imported head reconstructs identical tree       | Pack checksum can be computed while streaming | No Git executable invocation         |
| Checkpoint round trip  | Empty repository            | Checkpoint alone reconstructs its complete tree | Head is marked as a shallow boundary          | No older pack is required            |

## Acceptance criteria

- [x] Tree creation is deterministic for the same normalized file snapshot.
- [x] Repository creation and object-ID validation enforce the Git SHA-1 format.
- [x] Empty, nested, Unicode-content, Unicode-name, and mixed-line-ending fixtures round trip byte-for-byte.
- [x] Invalid UTF-8 and U+0000 fixtures are rejected before tree creation.
- [x] Unsupported file modes and invalid paths fail before object publication.
- [x] Disjoint file and compatible line changes merge automatically.
- [x] Add/add, modify/delete, and file/directory conflicts are tested.
- [x] Conflict results identify exact normalized paths and marker ranges for text.
- [x] A normal commit has the latest remote head as its single parent.
- [x] A root commit with no parent is created for the first publication.
- [x] Increment packs omit objects already available from the indexed base.
- [x] Increment descriptors identify the exact indexed base needed for import.
- [x] Increment packs import correctly after their checkpoint and prior tail.
- [x] Checkpoint packs contain no unresolved or external pack-delta base.
- [x] Checkpoint packs reconstruct the complete tree without pre-checkpoint packs.
- [x] Validation permits only the declared missing commit-parent history at the shallow boundary.
- [x] A checkpoint head supports later increment commits.
- [x] Every native handle has explicit lifetime tests or wrapper ownership rules.
- [x] Tests prove that no code invokes `git` or imports `git2go`.

## Error coverage

| Failure                                         | Expected outcome                               | Required test                     |
| ----------------------------------------------- | ---------------------------------------------- | --------------------------------- |
| Input contains symlink mode                     | Unsupported-file error with path               | `TestWriteTreeRejectsUnsupportedModes` (native) and `TestReadSnapshotRejectsUnsupportedMode` (policy) |
| Blob read fails                                 | Git-engine error with object and operation     | `TestReadSnapshotRejectsMissingBlob`, `TestNativeErrorsCarryDetail` |
| Merge index reports conflict                    | Structured conflict, no commit                 | `TestMergeOverlappingTextProducesMarkers` |
| Input contains invalid UTF-8 or U+0000          | Invalid-request error with path                | `TestSnapshotRejectsInvalidContentBeforeTreeCreation`, `TestValidateContent` |
| Pack import is truncated                        | Integrity or native pack error                 | `TestTruncatedPackRejected` |
| Pack references unavailable base objects        | Missing-base error                             | `TestIncrementPackRequiresBase`, `TestValidateHistoryAllowsDeclaredShallowGap` |
| Checkpoint omits required tree object           | Checkpoint validation fails                    | `TestValidateHistoryFailsOnMissingBlob`, `TestValidateHistoryFailsOnUnsupportedMode` |
| Checkpoint omits its declared historical parent | Import succeeds and records a shallow boundary | `TestCheckpointPackRoundTrip`, `TestMarkShallowWritesBoundaryFile` |
| Commit message is invalid for native call       | Validation error before mutation               | `TestCreateCommitRejectsBeforeMutation`, `TestValidateCommitMessage` |

## Implementation notes

### Session 2026-08-09 (imago, worker session 2)

Implemented the complete native boundary in `internal/git2` and hardened the
`internal/git` policy layer.

`internal/git2/native.go` now contains every libgit2 call: tree builder and
tree lookup, commit creation and lookup, three-tree merge through
`git_merge_trees` with rename detection disabled (`flags = 0`), file merge
with exact `local`/`remote` labels, pack export through the single-threaded
`git_packbuilder` (deterministic bytes for the pinned release), pack import
through `git_odb_write_pack`, and shallow marking. The writepack vtable
requires a non-NULL progress struct, so the C forwarders pass a local
`git_indexer_progress`. `internal/git2/engine.go` implements the nine
missing `Repository` methods; every native handle has explicit lifetime
rules and `TestRepositoryLifetimes` proves that a closed repository and a
closed engine invalidate every later operation.

`MarkShallow` writes the `shallow` file in the repository git directory in
Git's own format (one hex OID per line) and refreshes the in-memory graft
table through `git_repository_is_shallow`, so the current session agrees
with a freshly reopened repository. `TestMarkShallowWritesBoundaryFile`
proves the file content and the graft (a marked commit reads back with zero
parents).

The merge policy in `internal/git` now handles two libgit2 behaviors that
the phase contract pins:

- A file-versus-directory replacement appears in the merge index as a lone
  blob stage at the path plus resolved entries below it. The policy detects
  the directory side from resolved entries under the conflicted path and
  reports the conflict without marker content.
- libgit2's merge-file wrapper drops the marker label of an empty side, so a
  modify/delete conflict would end in a bare `>>>>>>>` line. The policy
  formats deleted-side conflicts with the exact `<<<<<<< local`, `=======`,
  `>>>>>>> remote` signatures, matching Git's own merge-file output.

Phase-1 defects fixed while bringing the full gate suite green: the test
fake panicked on a nil blob map instead of failing; `ValidateSnapshot` did
not reject exact duplicate paths; the deterministic-tree fixture used a
path with control characters; `ReadSnapshot` comparison failed on
nil-versus-empty slices; `go fix` modernizations (`bytes.Cut`,
`strings.SplitSeq`, `wg.Go`) were unapplied; and a dead `oidList` helper was
removed. The fake now returns non-nil empty slices so `reflect.DeepEqual`
round trips hold.

New tests: `internal/git2/operations_test.go` (component tests against real
libgit2 in temporary repositories), `internal/git2/seam_test.go` (source
scan proving no `git` invocation and no `git2go` import), and policy tests
`internal/git/commit_test.go`, `internal/git/merge_test.go`,
`internal/git/pack_test.go` against the deterministic fake.

### Verification (all passed)

Before changes: `go build ./...` failed because `*repository` did not
implement `git.Repository`; `go test ./internal/git/` panicked on the fake's
nil blob map.

After changes:

```text
go test -race -timeout=30s -count=3 -p 1 ./...   PASS (app, git, git2)
go vet ./...                                      PASS
CGO_ENABLED=0 go vet ./...                        PASS
go fix -diff ./...                                no output
staticcheck ./...                                 PASS
CGO_ENABLED=0 staticcheck ./...                   PASS
gofumpt -l .                                      no files
dupl -t 80 .                                      0 clone groups
make native-smoke                                 PASS (dep check + binary start)
```

## Review findings

### Review 1 (2026-08-09)

- [x] R1-01 (Major): The engine contract required a commit with exactly one
      parent, but the first publication needs a root commit with no parent and a
      checkpoint needs an intentionally omitted parent. Amended the commit
      contract and added a root-commit acceptance criterion.
- Verified good: deterministic tree building, SHA-1 object-ID enforcement,
  three-tree merge, marker labels, incremental pack base rules, and shallow
  checkpoint rules match architecture sections 8 and 9.3.
