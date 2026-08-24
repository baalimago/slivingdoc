# Pack import `STORAGE_INTEGRITY` worklog

**Status:** Fixed — root cause was the writepack progress struct, not a thin pack

**Scope:** the `baalimago/slivingdoc` repository. Code paths are named relative
to that repo's root (for example `internal/git2/native.go`).

## TL;DR

Every `notes_pull` against the production notebook failed with

```text
STORAGE_INTEGRITY  import increment pack [redacted]
retryable: false
```

The stored data is not corrupt, and the exported pack is **not thin**. The
importer failed on any pack that contained at least one delta object. The
cgo wrapper passed a fresh, zeroed `git_indexer_progress` struct to the
writepack `commit` callback, so `commit` saw `total_objects == 0` while
`resolve_deltas` had incremented `indexed_objects` for the pack's delta.
The resulting `indexed_objects != total_objects` mismatch surfaced as the
misleading libgit2 error `early EOF`.

The first pack to contain a delta is the first multi-file commit, which is
why the bug only appeared at pack 53.

## Background: how a notebook is stored

A slivingdoc notebook is a sequence of commits. Rather than store the full
history, the remote keeps two kinds of pack:

| Kind       | Key pattern                                           | What it contains                                                                                   |
| ---------- | ----------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Checkpoint | `packs/checkpoints/<generation>.pack`                 | A commit, its complete tree, and every referenced blob. Ancestors are intentionally omitted.       |
| Increment  | `packs/increments/<generation>-<publication-id>.pack` | Exactly the objects reachable from the new head that are not already reachable from the base head. |

A fresh `pull` imports the latest checkpoint, then replays the increments in
generation order. Each increment is a _git pack_: a concatenation of
compressed objects, optionally followed by a trailing checksum trailer.

A pack may delta-compress one blob against another similar blob. libgit2's
pack builder only selects delta bases from the objects inserted into the
builder, so an exported increment is always self-contained — a delta base is
never an object outside the pack.

## The relevant code

`internal/git2/native.go` does the actual pack import through the libgit2
writepack vtable:

```go
func libgit2ImportPack(odb *odbHandle, data []byte) error {
    // git_odb_write_pack, sl_odb_writepack_append, sl_odb_writepack_commit
}
```

The two cgo helpers forward to the vtable:

```c
static int sl_odb_writepack_append(git_odb_writepack *wp, const void *data, size_t size) {
    git_indexer_progress stats = { 0 };
    return wp->append(wp, data, size, &stats);
}

static int sl_odb_writepack_commit(git_odb_writepack *wp) {
    git_indexer_progress stats = { 0 };
    return wp->commit(wp, &stats);
}
```

`internal/notebook/remote.go` wires them together and wraps a failed import as
`STORAGE_INTEGRITY`:

```go
if err := git.ImportPack(n.ws.Repo(), data); err != nil {
    return storageIntegrity(err, "import increment pack %s", inc.Key)
}
```

## The bug: two `git_indexer_progress` structs

The libgit2 writepack contract requires the **same** `git_indexer_progress`
struct to flow through both `append` and `commit`. `append` populates
`total_objects`; `commit` compares it against `indexed_objects` after
resolving deltas.

The original helpers each declared their own local `stats = { 0 }`. The
`append` call populated a struct that was then thrown away. `commit`
therefore started from `total_objects == 0`. In `git_indexer_commit`,
`resolve_deltas` increments `indexed_objects` for every delta it resolves,
after which the check runs:

```c
if (stats->indexed_objects != stats->total_objects) {
    git_error_set(GIT_ERROR_INDEXER, "early EOF");
    return -1;
}
```

With one resolved delta, `indexed_objects` becomes `1` while `total_objects`
stays `0`, so the check fires and reports `early EOF`. Packs without deltas
resolved nothing, leaving both counters at `0`, so single-file commits
imported cleanly. The first delta-bearing (multi-file) commit failed.

The pack itself is valid. `git index-pack` and `git verify-pack` accept the
exported increment and confirm the delta base is inside the pack, so the
SHA-256 verified by the storage layer is intact. The failure is entirely in
the importer's progress accounting, not in transport or corruption.

## Evidence

A deterministic reproduction: two mutually-similar ~20 KiB blobs appear only
in the head commit, so `ExportIncrement` emits a pack with one `REF_DELTA`.
`git.ImportPack` into an empty repository failed with `early EOF` before the
fix. `git verify-pack` on the same bytes showed one delta whose base is the
other blob in the pack, proving the pack is self-contained.

## Fix

Share one `git_indexer_progress` between the `append` and `commit` calls:

```c
static int sl_odb_writepack_append(git_odb_writepack *wp, const void *data, size_t size, git_indexer_progress *stats) {
    return wp->append(wp, data, size, stats);
}

static int sl_odb_writepack_commit(git_odb_writepack *wp, git_indexer_progress *stats) {
    return wp->commit(wp, stats);
}
```

The Go caller declares the struct once and passes its address to both calls.
`TestIncrementPackWithDeltaImportsSelfContained` guards the contract.

## Impact

Downstream, the kinoview media server's "troupe" agent relies on
`notes_pull` to materialise the shared notebook before it can author new
content. Because every `pull` failed at the first delta-bearing pack, the
troupe could not start a new generation. Its play API continues to serve the
last successfully written play from the on-disk worktree, so the failure is
not immediately visible — only new generations stop.
