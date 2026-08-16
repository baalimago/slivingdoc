# Phase 1 — Diff engine

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§2 result envelope, §7.1 UTF-8 text files

## Goal

Add a deterministic, pure-Go line diff that turns two notebook snapshots into
per-file insertion and deletion counts.

## Specification

Add `internal/git/diffstat.go` with no dependency on the native engine:

```go
type FileStat struct {
    Path       string
    Insertions int
    Deletions  int
}

type DiffStat struct {
    Files      []FileStat
    Insertions int
    Deletions  int
}

func DiffSnapshots(base, cur Snapshot) DiffStat
```

Behavior:

- Compare `base` and `cur` by normalized relative path. Both snapshots are
  already path-sorted by the snapshot contract; `DiffSnapshots` does not rely on
  that and may build a map.
- A path present only in `cur` contributes every line as an insertion.
- A path present only in `base` contributes every line as a deletion.
- A path present in both with byte-identical content is omitted.
- A path present in both with different content is diffed line-by-line with a
  deterministic Myers shortest-edit-script algorithm; the script's insertions
  and deletions are the per-file counts.
- `Files` is sorted by path. `Insertions` and `Deletions` are the sums across
  `Files`.
- Line counting follows the README norm: split on LF, strip a single trailing
  CR per line for comparison and counting, count a final unterminated run as
  one line, produce no phantom empty final line for trailing LF, and treat
  empty content as zero lines.
- The function is pure: it never validates content or paths (snapshots are
  validated upstream) and never touches the repository or the filesystem.

The Myers implementation is private to the package. Tie-breaking at equal edit
cost is fixed (deletions before insertions) so the same two inputs always
produce the same counts; the exact tie-break is an implementation note, not a
contract.

## Integration contract

`unit-test-only`

## Acceptance criteria

- [x] `DiffSnapshots` reports an empty result for two identical snapshots.
      (`TestDiffStatIdenticalSnapshots`)
- [x] A file added between base and cur counts all of its lines as insertions.
      (`TestDiffStatAddedFiles`)
- [x] A file deleted between base and cur counts all of its lines as deletions.
      (`TestDiffStatDeletedFiles`)
- [x] A modified file with a common prefix, changed middle, and common suffix
      counts the exact changed lines. (`TestDiffStatModifiedFile`)
- [x] CRLF content and LF content compare as equal line text.
      (`TestDiffStatCRLFAndLFCompareEqual`)
- [x] A file with no trailing newline counts its last line.
      (`TestDiffStatNoTrailingNewline`, `TestDiffStatAddedFiles`)
- [x] An empty file has zero insertions and zero deletions.
      (`TestDiffStatEmptyFiles`)
- [x] `Files` is sorted by path and the totals match the per-file sums.
      (`TestDiffStatSortedAndTotals`)
- [x] The result is deterministic across repeated calls.
      (`TestDiffStatDeterministic`, `TestDiffLinesMatchesLCS`)

## Error coverage

Pure function: no external failure modes. Edge cases are covered by the
acceptance criteria above (empty snapshots, empty files, CRLF, missing trailing
newline, identical content).

## Implementation notes

- **Function name resolved from the spec.** Go shares one package-block
  namespace for types and functions, so the spec's `type DiffStat` and
  `func DiffStat(...)` cannot coexist. The types keep the spec names
  (`FileStat`, `DiffStat`) because every later phase carries them as data
  (`notebook.Result.Stat git.DiffStat`, the MCP `files` array); the function is
  `DiffSnapshots(base, cur Snapshot) DiffStat`. Phase 2 must call
  `git.DiffSnapshots(local, materialized)` and
  `git.DiffSnapshots(remoteSnapshot, mergedSnapshot)`.
- The diff is Myers' linear-space algorithm (middle-snake refinement): time
  O((n+m)d), memory O(n+m), so pathological inputs cannot blow up the trace
  table of the classic O(ND) version.
- At equal edit cost the search prefers the deletion move (a right move in the
  edit graph), matching the spec's fixed tie-break. The insertion/deletion
  counts themselves are uniquely determined by the edit distance — every
  shortest edit script has exactly `len(b)-LCS` insertions and
  `len(a)-LCS` deletions — so the tie-break fixes the script shape, not the
  counts.
- `Files` contains one entry per path with a nonzero line change. An added or
  deleted empty file and a byte-different file whose comparison lines are
  unchanged (a CRLF-to-LF rewrite) contribute no entry; all nine acceptance
  criteria hold under this rule and the CLI report stays free of
  `path +0 -0` noise.
- Validation is upstream: `DiffSnapshots` never checks content or paths.
  Duplicate paths in a snapshot collapse through the path map, matching the
  snapshot contract's uniqueness guarantee.
- Verified by property tests against an independent LCS dynamic program:
  exhaustive over all `{a,b}` line sequences up to length 6 (16,129 pairs),
  2,000 seeded random cases, 50 shuffled large cases, all-different and
  reversed 300-line inputs, and line-ending edge cases.

## Review findings

No reviews recorded.
