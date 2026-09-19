# Phase 1 — Path policy resolution

**Status:** Complete

[← README](README.md)

## Goal

Build the complete `PathPolicy` in `internal/git`: normalization of both entry
sets, longest-match resolution with the unmatched default, exact overlap as a
typed construction error, and the tree traversal that detects and restores
protected changes without reading content it does not need.

## Specification

### Entry set normalization

The existing read-only normalization already does everything both sets need:
trim one trailing slash, validate with `ValidatePath`, drop an entry covered by
another entry of the same set, sort, and match on segment boundaries under
Unicode case folding. This phase generalizes its names so the writable set does
not travel under a read-only label, and changes nothing about the behavior of a
set normalized on its own.

| Existing name | Generalized name |
| --- | --- |
| `ReadOnlySet` | `EntrySet` |
| `NormalizeReadOnly` | `NormalizeEntries` |
| `isReadOnlyAncestor` | `isEntryAncestor` |
| `readOnlyFold` | `entryFold` |

The collapse is the one step that is not purely per-set. It drops an entry
covered by another entry of the same set only where that ancestor decides every
path the covered entry decides, so it takes the other set as a parameter: an
entry the other set splits from its own-set ancestor is kept (review 2, R2-01).
A set normalized on its own — `NormalizeEntries`, which is the read-only path
every existing caller takes — passes no other set and is byte-for-byte what it
was.

The rename is mechanical and behavior-preserving. This phase owns it and updates
every in-repository reference, so Phases 2 and 3 find one name. The references
are in `internal/app/config.go`, `internal/app/service.go`,
`internal/notebook/notebook.go`, and `internal/notebook/commit.go` — the last
holds `readOnlyRefusal` and `violatedEntries`, which take the set by type, and
omitting it breaks the build. This phase changes the type name there and nothing
else; the refusal itself is Phase 2's.

`EntrySet` keeps its existing methods, but not all of them keep a production
caller once Phase 2 replaces the two enforcement sites, and a later reader should
not have to work out which.

| Method | Disposition after Phase 2 |
| --- | --- |
| `Entries` | Used: the entry accessors and every advertised surface |
| `Covers`, `CoveringEntry` | Used: `Protects` resolves through them |
| `ChangedUnder`, `Pin` | Retained, no production caller: both take a base `Snapshot` the tree-hash design never materializes |
| `ReadCovered` | Retained, no production caller: reading every covered path is precisely what D4 removes |

The three retained methods stay exported and stay covered by
`internal/git/readonly_test.go`, so the rename costs no coverage and the
duplication signal sees no new clone. Deleting them is a separate decision and is
not this worklog's to take.

### Construction

`NewPolicy(readOnly, writable []string) (PathPolicy, error)` validates each set,
rejects an overlap, and only then normalizes. Coverage **across** the sets is not
collapsed: a writable entry below a read-only entry, or a read-only entry below a
writable entry, is the composition the operator is expressing. Only exact
equality under case folding is ambiguous, and it is refused.

The overlap test runs over the entries **as the operator wrote them** —
validated, trimmed and folded — before either set drops its own covered
entries. An entry named by both settings is refused whatever else its own set
contains, so the intra-set collapse can never destroy an entry the other set
also names (README invariant 5).

Each set is then collapsed **against the other**. An entry is redundant only
when its own-set ancestor decides every path it decides, which fails exactly
when an entry of the other set lies strictly between the two: there the
ancestor loses the longest match and the covered entry is the one that decides.
Keeping it makes the stored sets resolve exactly as the written entries do, so
the collapse normalizes what is advertised and decides neither the refusal nor
the resolution (README invariant 5, Strategy). It also makes the stored sets a
lossless input to `NewPolicy`: the production path rebuilds the policy from the
previous one's accessors twice more before enforcement reads it (review 2,
R2-03), and a rebuild is now a fixed point.

| Configuration | Normalized sets | Test |
| --- | --- | --- |
| A read-only entry with a read-only ancestor and a writable entry between them | Both read-only entries kept | `TestNewPolicyKeepsCrossSetCoverage` |
| A writable entry with a writable ancestor and a read-only entry between them | Both writable entries kept | `TestNewPolicyKeepsCrossSetCoverage` |
| A covered entry with no entry of the other set between it and its ancestor | Dropped, as today | `TestNewPolicyKeepsCrossSetCoverage`, `TestNormalizeEntriesMatchesPreviousBehaviour` |
| The policy rebuilt from its own entry accessors | Same entries and same resolution | `TestPolicyRebuiltFromAccessorsResolvesAlike` |

| Configuration | Construction | Test |
| --- | --- | --- |
| The same path in both sets | Refused, naming both written entries | `TestNewPolicyRejectsExactOverlap` |
| The same path, differing only in letter case | Refused | `TestNewPolicyRejectsCaseFoldedOverlap` |
| The overlapping read-only entry also covered by a read-only ancestor | Refused | `TestNewPolicyRejectsCollapsedOverlap` |
| The overlapping writable entry also covered by a writable ancestor | Refused | `TestNewPolicyRejectsCollapsedOverlap` |
| The collapsed overlap differing only in letter case, or written with a trailing slash | Refused | `TestNewPolicyRejectsCollapsedOverlap` |

The zero policy — both sets empty — protects nothing and reports `Configured()`
false. Every caller short-circuits on it, so an unconfigured process does no
policy work at all.

### Resolution

`Protects(path)` finds the longest entry across both sets that is `path` itself
or an ancestor of it on a segment boundary under case folding, and answers from
the set that entry came from. With no such entry it answers from the unmatched
default named in the README parameters table.

The longest match is unique. Within one set, two entries can both match — a
covered entry the other set splits from its ancestor survives the collapse — and
the longer of them is the one the set answers with, since matching entries of
one set lie on one ancestor chain. Across the sets, two matching entries of
equal folded length would be the same folded string, which construction has
already refused. This argument is the reason resolution needs no tie-break rule,
and a test pins it over the entries the operator wrote rather than over the
collapsed accessors, which cannot show an entry the collapse removed (review 2,
R2-01).

| Input | Resolution | Test |
| --- | --- | --- |
| Matched only by a read-only entry | Protected | `TestPolicyProtectsResolutionTable` |
| Matched only by a writable entry | Writable | `TestPolicyProtectsResolutionTable` |
| Matched by both sets, the read-only entry longer | Protected | `TestPolicyProtectsResolutionTable` |
| Matched by both sets, the writable entry longer | Writable | `TestPolicyProtectsResolutionTable` |
| Matched by no entry, writable set empty | Writable | `TestPolicyProtectsUnmatchedDefault` |
| Matched by no entry, writable set non-empty | Protected | `TestPolicyProtectsUnmatchedDefault` |
| Both sets empty | Not configured; protects nothing | `TestPolicyConfigured` |
| An entry that is a prefix of a path but not on a segment boundary | Unmatched; falls to the default | `TestPolicyProtectsSegmentBoundary` |
| A path differing from an entry only by letter case | Matched | `TestPolicyProtectsCaseFolding` |
| Under a read-only entry the operator wrote below a writable entry, itself below a read-only ancestor | Protected | `TestPolicyProtectsResolutionTable`, `TestPolicyProtectsLongestMatchIsUnique` |
| Under the writable entry between them, in the same configuration | Writable | `TestPolicyProtectsResolutionTable`, `TestPolicyProtectsLongestMatchIsUnique` |
| Under a writable entry the operator wrote below a read-only entry, itself below a writable ancestor | Writable | `TestPolicyProtectsResolutionTable`, `TestPolicyProtectsLongestMatchIsUnique` |
| Under the read-only entry between them, in the same configuration | Protected | `TestPolicyProtectsResolutionTable`, `TestPolicyProtectsLongestMatchIsUnique` |
| The same three-level composition written in another letter case | Resolved the same way | `TestPolicyProtectsResolutionTable` |
| Any of the above, resolved through a policy rebuilt from the entry accessors | Unchanged | `TestPolicyRebuiltFromAccessorsResolvesAlike` |
| Named by both sets, one side collapsed by an ancestor of its own set | Never resolved: construction refused the configuration | `TestNewPolicyRejectsCollapsedOverlap` |

### Detection traversal

`ChangedProtected(repo, local, base OID)` walks the two trees in parallel and
returns, sorted, every protected path that differs between them. It reads tree
objects only. It never calls `ReadBlob`, because a differing blob is proved by a
differing object ID and the identity of the content is not needed to report the
path.

| Property | Mechanism | Test |
| --- | --- | --- |
| A subtree whose ID is equal on both sides is skipped whole | Compare the tree entry ID before descending | `TestChangedProtectedSkipsEqualSubtrees` |
| No blob is read | The traversal calls `ReadTree` only | `TestChangedProtectedReadsNoBlob` |
| A path added on the local side is reported when protected | An entry present on one side only is a difference | `TestChangedProtectedReportsAdditions` |
| A path removed on the local side is reported when protected | Same, in the other direction | `TestChangedProtectedReportsRemovals` |
| A path whose content differs is reported when protected | Differing blob IDs at the same path | `TestChangedProtectedReportsModifications` |
| A differing path that is not protected is not reported | `Protects` filters every difference | `TestChangedProtectedIgnoresWritableDifferences` |
| A subtree containing only writable paths is still descended when it also contains protected ones | Descent is decided by ID equality, never by the policy | `TestChangedProtectedDescendsMixedSubtree` |
| A protected path that is a blob on one side and a tree on the other is reported on both sides | The entry differs in mode and ID, and the descent reports the paths beneath the tree side | `TestChangedProtectedReportsTypeChange` |
| The result is sorted and free of duplicates | Sort before returning | `TestChangedProtectedSortedResult` |
| An unconfigured policy reports nothing and walks nothing | Short-circuit on `Configured` | `TestChangedProtectedUnconfiguredWalksNothing` |
| A changed file under a three-level composition is reported on the protected side and only there | `Protects` resolves the written entries, in both nesting directions | `TestChangedProtectedReportsThreeLevelProtection` |

The counted-call assertions use the fake this package already has:
`fakeRepository` in `internal/git/fake_test.go` is in package `git`, backs blobs,
trees, and commits in memory, and already counts calls (`treeReads`,
`presenceChecks`). This phase adds one field, `blobReads`, incremented in its
existing `ReadBlob`, and reuses it. It does **not** declare a second fake: a new
`fakeRepository` in `policy_test.go` would redeclare the name in the same package
and fail to compile, and a renamed copy would be a same-package clone that no
clause of the duplication policy in `AGENTS.md` excuses — that policy's fake
mirroring clause is about keeping *independent packages* independent, which two
fakes in one package are not.

### Restore

`RestoreProtected(repo, base OID, local Snapshot, changed []string)` returns
`local` with each named path taken from the base tree. It reads exactly the named
paths and nothing else, so its cost is proportional to the violation rather than
to the notebook. Callers pass the result of `ChangedProtected`, so an operation
with no protected change never calls it at all.

| Case | Result | Test |
| --- | --- | --- |
| A named path present in both | Local content replaced by base content | `TestRestoreProtectedReplacesContent` |
| A named path present in base only | Added back to the snapshot | `TestRestoreProtectedRestoresDeletion` |
| A named path present in local only | Dropped from the snapshot | `TestRestoreProtectedDropsAddition` |
| A named path that is a blob in base and a directory prefix in local | The local files beneath it are dropped and the base blob is added, so the snapshot holds neither shape twice | `TestRestoreProtectedRestoresTypeChange` |
| A path not named | Untouched, whatever the policy says about it | `TestRestoreProtectedTouchesOnlyNamedPaths` |
| An empty name list | The snapshot is returned unchanged and no object is read | `TestRestoreProtectedEmptyListReadsNothing` |
| The result | Sorted by path, valid as a snapshot | `TestRestoreProtectedResultIsValidSnapshot` |

### Files

- `internal/git/policy.go` — new: `PathPolicy`, `NewPolicy`, resolution, the
  traversal, and the restore.
- `internal/git/policy_test.go` — new: the tables above and the counting fake.
- `internal/git/readonly.go` — renamed symbols; behavior unchanged for a set
  normalized on its own. The review-2 fix pass gave `collapseEntries` the other
  set and made `covering` resolve the longest entry.
- `internal/git/readonly_test.go` — renamed symbols; assertions unchanged.
- `internal/git/fake_test.go` — one added `blobReads` counter on the existing
  `fakeRepository`; no new fake.
- `internal/app/config.go`, `internal/app/service.go`,
  `internal/notebook/notebook.go`, `internal/notebook/commit.go` — mechanical
  rename of the referenced symbols only. No field, flag, or behavior changes
  here; those belong to later phases.
- `docs/slivingdoc-v1.md` — added in the review-1 fix pass: the overlap refusal
  is stated over the entries the operator wrote, since the fix changes which
  configurations start. The review-2 fix pass adds the same rule for resolution
  and states that the advertised set can hold an entry below another.
- `docs/running.md` — added in the review-2 fix pass: the three-level example,
  since the operator guide is where the composition is likely to be tried.

## Integration contract

`unit-test-only`. The policy has no observable boundary until Phase 2 wires it
into `Notebook.Pull` and `Notebook.Commit`. Every guarantee in this phase is a
property of pure functions and of a traversal over an injected `Repository`, so
the unit boundary is the narrowest one that exercises the real behavior.

## Acceptance criteria

Every row is proven. Each named test is declared in `internal/git/policy_test.go`
and ran green under `make test` (`-race -count=3`); the run is recorded in the
implementation notes below.

| Done | Outcome | Proven by |
| --- | --- | --- |
| [x] Both sets normalize exactly as the read-only set does today: trailing slash trimmed, invalid entry refused, covered entry dropped, result sorted | `TestNormalizeEntriesMatchesPreviousBehaviour` |
| [x] Coverage across the sets is preserved, not collapsed | `TestNewPolicyKeepsCrossSetCoverage` |
| [x] A path named by both sets refuses construction | `TestNewPolicyRejectsExactOverlap` |
| [x] A path named by both sets differing only in letter case refuses construction | `TestNewPolicyRejectsCaseFoldedOverlap` |
| [x] A path named by both sets refuses construction even when its own set also names an ancestor covering it, in both nesting directions and under case folding | `TestNewPolicyRejectsCollapsedOverlap` |
| [x] Resolution follows the resolution table in every row | `TestPolicyProtectsResolutionTable` |
| [x] The unmatched default follows the writable set's emptiness | `TestPolicyProtectsUnmatchedDefault` |
| [x] Matching is on segment boundaries under case folding | `TestPolicyProtectsSegmentBoundary`, `TestPolicyProtectsCaseFolding` |
| [x] The longest match is unique, so resolution needs no tie-break, and `Protects` answers from the longest entry the operator wrote | `TestPolicyProtectsLongestMatchIsUnique` |
| [x] A three-level composition resolves from the written entries in both nesting directions, so a broader entry never makes a narrower entry of the same set stop applying | `TestPolicyProtectsResolutionTable`, `TestPolicyProtectsLongestMatchIsUnique` |
| [x] An entry the other set splits from its own-set ancestor survives normalization, and one with nothing between them is still dropped | `TestNewPolicyKeepsCrossSetCoverage` |
| [x] The entry accessors are a lossless input to `NewPolicy`, so the rebuilds on the production path resolve alike | `TestPolicyRebuiltFromAccessorsResolvesAlike` |
| [x] Detection reports a changed file under a three-level composition on the protected side and only there | `TestChangedProtectedReportsThreeLevelProtection` |
| [x] `ReadOnly` and `Writable` return copies a caller cannot use to mutate the policy | `TestPolicyEntriesAreCopies` |
| [x] Detection reports every protected difference and no writable one | `TestChangedProtectedReportsAdditions`, `TestChangedProtectedReportsRemovals`, `TestChangedProtectedReportsModifications`, `TestChangedProtectedReportsTypeChange`, `TestChangedProtectedIgnoresWritableDifferences` |
| [x] Detection reads no blob | `TestChangedProtectedReadsNoBlob` |
| [x] Restore reads only the named paths | `TestRestoreProtectedEmptyListReadsNothing`, `TestRestoreProtectedTouchesOnlyNamedPaths` |
| [x] The rename leaves every existing read-only assertion passing unedited | `make test` over the renamed `internal/git/readonly_test.go` |

## Error coverage

| Done | Failure | Expected outcome | Test |
| --- | --- | --- | --- |
| [x] An entry in either set fails `ValidatePath` | Construction fails, naming the offending entry and which set it came from | `TestNewPolicyRejectsInvalidEntry` |
| [x] An entry names the notebook root, empty or dot | Refused by `ValidatePath` as it is today, with the same message | `TestNewPolicyRejectsRootEntry` |
| [x] A path is named by both sets | Construction fails, naming the path and both sets, so the caller can render a message naming both flags | `TestNewPolicyRejectsExactOverlap` |
| [x] A path is named by both sets and its own set also names an ancestor of it | Construction fails the same way, over the written entries, and returns the zero policy | `TestNewPolicyRejectsCollapsedOverlap` |
| [x] `ReadTree` fails during detection | The error is wrapped with the package prefix and the tree path, and no partial result is returned | `TestChangedProtectedRepositoryFailure` |
| [x] A tree entry carries an unsupported mode | Detection fails naming the path, as the existing covered walk does | `TestChangedProtectedUnsupportedMode` |
| [x] `ReadBlob` fails during restore | The error is wrapped with the package prefix and the path, and no partial snapshot is returned | `TestRestoreProtectedRepositoryFailure` |
| [x] Restore is asked for a path absent from both sides | The path is skipped rather than failing, since detection cannot produce it and a caller that does is not corrupting state | `TestRestoreProtectedUnknownPathSkipped` |
| [x] The restored snapshot would violate the content rules | The existing snapshot validation rejects it, with the failure surfacing to the caller unchanged | `TestRestoreProtectedInvalidSnapshotRejected` |

## Implementation notes

### 2026-09-18 — Claude Opus 5 (1M context), executing agent

Deltas from the specification only.

**`PathPolicy` is a struct, not a Go `interface`.** The README's shared-surface
block writes `type PathPolicy interface`, but the same block says an
unconfigured policy "is the zero value", and the zero value of an interface is
nil — `Protects` on it panics. `AGENTS.md` also places an interface in the
package that *consumes* it, and `internal/git` is the producer here. `PathPolicy`
is therefore an exported struct in `internal/git/policy.go` carrying exactly the
method set, signatures, and semantics the README block declares, so every
statement Phases 2–4 rely on — including `NewPolicy(readOnly, writable []string)
(PathPolicy, error)` verbatim — holds unchanged, and `var p git.PathPolicy` is an
unconfigured policy that protects nothing. Recorded as a **note** in the README
session journal for the maintainer; nothing in Phases 2–5 changes because of it.

**The set label is a normalization parameter.** The rename table asks for a
behavior-preserving `NormalizeEntries`, while the error-coverage table asks
construction to name "which set it came from". The existing message is
`invalid read-only path %q`. Both are satisfied by an unexported
`normalizeEntries(entries, kind)` carrying an `entryKind`; the exported
`NormalizeEntries` passes `kindReadOnly` and is byte-for-byte what it was, which
keeps the `internal/app` startup-refusal assertions
(`config_test.go` pins `invalid read-only path "..": …`) green, and `NewPolicy`
passes `kindWritable` for the second set, giving `invalid writable path %q`.

**`EntrySet.covering` added, unexported.** Longest match needs the *folded*
entry, not the raw one, so `covering` returns both and `CoveringEntry` now
delegates to it. This is the one addition to the renamed file; it removes the
loop rather than cloning it, and `TestReadOnlyCoveringEntry` passes unedited.

**Detection has one short-circuit the phase tables do not name**: equal root
tree IDs return before any read. It is the root instance of the "subtree whose
ID is equal on both sides is skipped whole" row, and
`TestChangedProtectedSkipsEqualSubtrees` covers it in a subtest so the branch is
not untested.

**A modified blob reaches `collectSide` from both sides**, so detection
de-duplicates before sorting rather than only sorting.
`TestChangedProtectedSortedResult` asserts both.

`internal/git/policy.go` is new (`PathPolicy`, `OverlapError`, `NewPolicy`,
`Configured`, `ReadOnly`, `Writable`, `Protects`, `ChangedProtected`,
`RestoreProtected`); `internal/git/policy_test.go` is new (35 tests);
`internal/git/readonly.go` and `readonly_test.go` carry the rename;
`internal/git/fake_test.go` gained the `blobReads` counter; `internal/app/config.go`,
`internal/app/service.go`, `internal/notebook/notebook.go`, and
`internal/notebook/commit.go` carry the type-name rename and nothing else.
`docs/slivingdoc-v1.md` is untouched: this phase adds no observable behavior, and
the renamed symbols appear nowhere in `docs/`.

### Verification

| Command | Result |
| --- | --- |
| `go build ./...` | clean |
| `go run mvdan.cc/gofumpt@v0.11.0 -l .` | clean, no file listed |
| `go vet ./...` | clean |
| `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` | clean |
| `go fix -diff ./...` | clean after adopting its `slices.Contains` rewrite in `appendUnique` |
| `make test` (`-race -count=3 -timeout=30s -coverpkg=./...`) | pass, coverage 84.3 % against the 70 % floor |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | 1 clone group, pre-existing (`internal/app/command_test.go:447,467`); no policy file reported |
| `go tool cover -func=.build/cover.out \| grep internal/git/policy.go` | every exported function 100 %; only nested error-propagation branches uncovered (`diffTrees` 88.2 %, `collectTree` 85.7 %, `readBlobAt` 89.5 %) |

All 35 new tests and all 8 pre-existing `readonly_test.go` tests ran green under
`go test ./internal/git/ -race -v`:

- Construction and normalization: `TestNormalizeEntriesMatchesPreviousBehaviour`,
  `TestNewPolicyKeepsCrossSetCoverage`, `TestNewPolicyRejectsExactOverlap`,
  `TestNewPolicyRejectsCaseFoldedOverlap`, `TestNewPolicyRejectsInvalidEntry`,
  `TestNewPolicyRejectsRootEntry`.
- Resolution: `TestPolicyProtectsResolutionTable`,
  `TestPolicyProtectsUnmatchedDefault`, `TestPolicyConfigured`,
  `TestPolicyProtectsSegmentBoundary`, `TestPolicyProtectsCaseFolding`,
  `TestPolicyProtectsLongestMatchIsUnique`, `TestPolicyEntriesAreCopies`.
- Detection: `TestChangedProtectedSkipsEqualSubtrees`,
  `TestChangedProtectedReadsNoBlob`, `TestChangedProtectedReportsAdditions`,
  `TestChangedProtectedReportsRemovals`,
  `TestChangedProtectedReportsModifications`,
  `TestChangedProtectedIgnoresWritableDifferences`,
  `TestChangedProtectedDescendsMixedSubtree`,
  `TestChangedProtectedReportsTypeChange`,
  `TestChangedProtectedSortedResult`,
  `TestChangedProtectedUnconfiguredWalksNothing`,
  `TestChangedProtectedRepositoryFailure`,
  `TestChangedProtectedUnsupportedMode`.
- Restore: `TestRestoreProtectedReplacesContent`,
  `TestRestoreProtectedRestoresDeletion`, `TestRestoreProtectedDropsAddition`,
  `TestRestoreProtectedRestoresTypeChange`,
  `TestRestoreProtectedTouchesOnlyNamedPaths`,
  `TestRestoreProtectedEmptyListReadsNothing`,
  `TestRestoreProtectedResultIsValidSnapshot`,
  `TestRestoreProtectedRepositoryFailure`,
  `TestRestoreProtectedUnknownPathSkipped`,
  `TestRestoreProtectedInvalidSnapshotRejected`.
- Rename unedited: `TestNormalizeReadOnly`,
  `TestNormalizeReadOnlyEmptyEntriesNonNil`, `TestReadOnlyCovers`,
  `TestReadOnlyCoveringEntry`, `TestReadOnlyChangedUnder`,
  `TestReadOnlyChangedUnderEmptySet`, `TestReadOnlyPin`,
  `TestReadOnlyReadCovered`.

The counted-call oracles: `TestChangedProtectedSkipsEqualSubtrees` pins four
`ReadTree` calls (both roots and both sides of the one differing subtree, never
the equal one) and zero on identical trees; `TestChangedProtectedReadsNoBlob`
pins zero `ReadBlob` calls across additions, removals, modifications and a type
change; `TestRestoreProtectedTouchesOnlyNamedPaths` pins one `ReadBlob` call for
one named path out of three changed files; `TestRestoreProtectedEmptyListReadsNothing`
pins zero of both.

### 2026-09-19 — Claude Opus 5 (1M context), fixing review 1

Deltas from the previous notes only. R1-01 and R1-09 closed; the phase returns
to `Complete`.

**The overlap test moved above the collapse, by splitting normalization in
two.** `normalizeEntries` was one function that validated and collapsed in one
pass, so `NewPolicy` had nothing but collapsed sets to compare. It is now
`validateEntries(entries, kind) ([]entryItem, error)` — trim, `ValidatePath`,
fold, keep every written entry in written order — followed by
`collapseEntries(items) EntrySet` — drop a covered entry, de-duplicate, sort.
`normalizeEntries` is the two composed and is byte-for-byte what it was, so
`NormalizeEntries` and every `internal/app` startup diagnostic are unchanged.
`NewPolicy` now validates both sets, tests the overlap over the written
entries, and collapses each side only after the test passes. No signature and
no error type changed: `NewPolicy` and `OverlapError` are what Phases 2–4
already consume, and `OverlapError` still carries the entry each side was
*written* as, which is what `internal/app/config.go` renders into the
both-flags message.

**Which pair is reported changed for a multi-overlap configuration.** The test
iterates the written entries, so the first written read-only entry with a
written writable match is the one named, rather than the first in sorted
normalized order. No caller depends on the choice; it stays deterministic in
the operator's own input order.

**The new rows were probed before being trusted.** `NewPolicy` was temporarily
reverted to collapse-then-compare and all four
`TestNewPolicyRejectsCollapsedOverlap` subtests failed with
`= <nil>, want *OverlapError` — including the two orderings review 1 filed
(`docs,docs/open` × `docs/open` and `notes/locked` × `notes,notes/locked`), the
case-folded variant (`Docs,Docs/Open` × `docs/open`) and a trailing-slash
variant. The fix was restored and they pass.

**R1-09 closed in the README, not in the code.** The shared-interface block now
reads `type PathPolicy struct` with the same method set and comments; the code
is unchanged, since the struct is what the block's own "the zero value" promise
requires.

`docs/slivingdoc-v1.md` gains one clause in "Writable paths": the entries
compared for the overlap refusal are the ones the operator wrote, so an entry
also sitting below another entry of its own set is refused just the same. The
section already stated the rule this fix restores; the clause removes the
reading under which the old behaviour was conformant.

### Verification (fix pass)

| Command | Result |
| --- | --- |
| `go build ./...` | clean |
| `go test ./internal/git/ -run TestNewPolicyRejectsCollapsedOverlap -race -count=1 -v` | 4 subtests pass |
| Same test against a collapse-then-compare `NewPolicy` (throwaway revert) | 4 subtests **fail**, so the rows are not vacuous |
| `go test ./internal/git/ ./internal/app/ ./internal/notebook/ -race -count=1` | 3 `ok` |
| `make lint` | clean: gofumpt listed no file, `go vet` clean, staticcheck clean, `go fix -diff` silent |
| `make test` (`-race -count=3 -timeout=30s -coverpkg=./...`) | pass unedited, 18 `ok` packages, 0 `FAIL`, `== coverage: 84.5% (floor 70%) ==` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 2 clone groups` — the two Phase 5 records as accepted; no new clone in `internal/git` |
| `go tool cover -func=.build/cover.out \| grep internal/git` | `NewPolicy`, `normalizeEntries`, `validateEntries`, `collapseEntries` all 100 % |

### 2026-09-19 — Claude Opus 5 (1M context), fixing review 2

Deltas from the previous notes only. R2-01 closed; the phase returns to
`Complete`.

**The collapse became a two-set operation rather than the written entries
becoming a third field.** R2-01 offered two shapes and R2-03 decides between
them. Carrying the written entries on `PathPolicy` fixes the first
construction only: `internal/app/config.go` overwrites its own fields with
`policy.ReadOnly()`/`Writable()`, `internal/app/service.go` builds
`notebook.Config` from the accessors again, and `notebook.New` calls
`NewPolicy` a third time, so whatever the struct carries is re-derived from
the collapsed entries before enforcement reads it. Threading the written
entries through those three files instead would work, but it is Phase 2's and
Phase 3's code and it leaves the same trap for the next caller. The fix taken
is the other option the review offered, and it holds wherever the policy is
rebuilt: `collapseEntries(items, other)` drops an entry only where its own-set
ancestor decides every path it decides, which fails exactly when an entry of
the other set lies strictly between them. The stored sets then resolve exactly
as the written entries do, so rebuilding from the accessors is a fixed point —
pinned by `TestPolicyRebuiltFromAccessorsResolvesAlike`, which rebuilds three
times and compares both accessors and every verdict.

Why the condition is exactly right, since the collapse is now load-bearing:
for a path P below a kept ancestor A and a dropped entry E (A above E, same
set), any entry W of the other set that also matches P lies on P's ancestor
chain with A and E. Either W is above A, and A outranks it with or without E;
or W is below E, and W outranks both; or W is strictly between A and E, which
is the case the fix keeps E for. W cannot equal A or E — that is the overlap
`NewPolicy` already refuses. So dropping E never changes an answer.

**`covering` had to become longest-match.** With a set able to hold an entry
below another, the first match in sorted order is the *shortest* one, so
`Protects` would have compared the wrong length and the fix would have been
invisible at the boundary. `EntrySet.CoveringEntry` therefore returns the most
specific entry now; its doc comment claimed disjointness, which is no longer
true. The one production caller of it is `violatedEntries` in
`internal/notebook/commit.go`, which names the read-only entry a refusal
quotes — the more specific entry is the better answer there, and that path is
only reached when the writable set is empty, where nothing nests.

**What the accessors return changed for nested configurations.** `ReadOnly()`
for `--read-only-paths notes,notes/agent-a/locked --writable-paths
notes/agent-a` is now `[notes notes/agent-a/locked]` where it was `[notes]`.
Every set with one side empty is byte-for-byte what it was, so README
invariant 4 is untouched and `TestNormalizeEntriesMatchesPreviousBehaviour`
pins it, but this is a visible change to what Phase 4 advertises and what
Phase 3 may pin. Recorded in the README "Shared interface between phases"
block rather than left in the code.

**`internal/notebook` and `internal/app` needed no edit.** No signature, error
type, or message moved: `NewPolicy`, `OverlapError`, `NormalizeEntries` and
the accessors are what Phases 2–4 already consume.

### Verification (review-2 fix pass)

| Command | Result |
| --- | --- |
| `go build ./...` | clean |
| `go test ./internal/git/ -run 'TestPolicyProtectsResolutionTable\|TestPolicyProtectsLongestMatchIsUnique\|TestPolicyRebuiltFromAccessorsResolvesAlike\|TestNewPolicyKeepsCrossSetCoverage\|TestChangedProtectedReportsThreeLevelProtection' -race -count=1 -v` | all subtests pass |
| Probe A — the same tests against `collapseEntries(ro, nil)`/`collapseEntries(wr, nil)` (throwaway revert of the cross-set collapse) | 11 subtests **fail** across all four tests; `TestNormalizeEntriesMatchesPreviousBehaviour` and the eight `readonly_test.go` tests still pass |
| Probe B — the same tests against a first-match `covering` (throwaway revert, cross-set collapse kept) | 7 subtests **fail** across three tests, so both halves of the fix are pinned |
| `go test ./internal/git/ -race -count=3` | `ok` |
| `go test ./internal/git/ ./internal/app/ ./internal/notebook/ ./internal/mcp/ -race -count=1` | 4 `ok` |
| `make lint` | clean: gofumpt listed no file, `go vet` clean, staticcheck v0.7.0 clean, `go fix -diff` silent |
| `make test` (`-race -count=3 -timeout=30s -coverpkg=./...`), idle machine | pass unedited, 18 `ok` packages, 0 `FAIL`, `== coverage: 84.5% (floor 70%) ==` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 2 clone groups` — the two Phase 5 records; none in `internal/git` |
| `go tool cover -func=.build/cover.out \| grep internal/git` | `NewPolicy`, `collapseEntries`, `entryBetween`, `covering`, `CoveringEntry`, `Protects` all 100 % |

## Review findings

### Review 1 — 2026-09-19 — reopened; both findings closed the same day, status `Complete`

**R1-01 — blocker — `internal/git/policy.go:38-53`, with `internal/git/readonly.go:39-83`.**
The cross-set overlap test runs *after* each set's own coverage collapse, so an
entry the operator wrote in both settings is destroyed before it can be tested.
`normalizeEntries` drops an entry covered by another entry of the same set
(`readonly.go:52-63`), and `NewPolicy` compares only what survives
(`policy.go:47-53`). The README Strategy section states the opposite rule — that
the intra-set collapse "must **not** be applied across the two sets, where
coverage is the feature" — but the collapse runs first and the cross-set test
never sees the collapsed entry.

Reproduced with a throwaway test in package `git` (removed afterwards):

| Configuration | Startup | `Protects` | Expected |
| --- | --- | --- | --- |
| `--read-only-paths=docs/open --writable-paths=docs/open` | refused: `git: path "docs/open" is named by both the read-only and the writable set` | — | refusal |
| `--read-only-paths=docs,docs/open --writable-paths=docs/open` | **accepted**; sets resolve to `readOnly=[docs] writable=[docs/open]` | `docs/open/c.md` → **writable** | refusal |
| `--read-only-paths=notes/locked --writable-paths=notes,notes/locked` | **accepted**; sets resolve to `readOnly=[notes/locked] writable=[notes]` | `notes/locked/d.md` → **protected** | refusal |

Row two is the damaging one: a path the operator listed under
`--read-only-paths` becomes writable with no diagnostic, which is the silent
precedence D1 exists to forbid and which breaks README invariant 5 ("A path
named exactly by both sets refuses startup"). Row three fails closed but still
contradicts the operator's written intent without saying so. Whether the
guardrail holds depends on whether some *other* entry of the same set happens to
cover the overlapping one — a dependency no surface discloses.

- [x] Run the cross-set overlap test over the validated, trimmed, folded entries
      of each set **before** the intra-set coverage collapse, so an entry named
      by both settings is refused whatever else its own set contains.
      Closed: `NewPolicy` compares `validateEntries` output and collapses only
      afterwards (`internal/git/policy.go`, `internal/git/readonly.go`).
- [x] Add a resolution-table row and a construction row for the collapsed
      overlap in both nesting directions, so the branch is pinned.
      Closed: the Construction table, the Resolution table, one acceptance row
      and one error-coverage row, all proven by
      `TestNewPolicyRejectsCollapsedOverlap`.

**R1-09 — note — README "Shared interface between phases".**
The block still declares `type PathPolicy interface` while `internal/git/policy.go:16`
ships an exported struct. The block is the phase-to-phase contract that Phases
2–4 read; leaving it contradicting the code makes it unusable for the next
reader. This is P1-N1, which is real and still open. The struct is the right
call for the reason the phase gives — the same block promises the unconfigured
policy "is the zero value", and a nil interface panics on `Protects`.

- [x] Change the block to `type PathPolicy struct` with its method set, or say
      why the interface is wanted and which package owns it.
      Closed: the README block now declares the struct the code ships.

### Verified good

- `NewPolicy` refuses the *direct* exact and case-folded overlap, and the
  refusal carries both written entries (`OverlapError.ReadOnly`/`.Writable`),
  which is what lets `internal/app/config.go:274-284` name both flags.
- `Protects` resolves longest match correctly in both nesting directions, and
  the uniqueness argument holds: within a set normalization leaves no two
  matching entries, and across the sets two matches of equal folded length would
  be the same folded string, which construction refuses. The equal-length branch
  is therefore unreachable, and `len(roFolded) > len(wrFolded)` is safe.
- `ChangedProtected` reads tree objects only on every branch — additions,
  removals, modifications, type changes and mixed subtrees — and
  `TestChangedProtectedReadsNoBlob` pins zero `ReadBlob` calls over a fixture
  that exercises all four. `TestChangedProtectedSkipsEqualSubtrees` pins exactly
  four `ReadTree` calls and zero on identical trees; neither is vacuous.
- `RestoreProtected` reads only the named paths, drops a local file named or
  beneath a named path, skips a path absent from both sides rather than failing,
  and validates the result. The empty-list branch returns `local` untouched with
  no read at all.
- Statement coverage of `internal/git/policy.go` re-measured from
  `.build/cover.out`: every exported function 100 %; only nested error
  propagation is uncovered (`diffTrees` 88.2 %, `collectTree` 85.7 %,
  `readBlobAt` 89.5 %), matching what the phase recorded.
- The rename is behaviour-preserving: all eight pre-existing
  `internal/git/readonly_test.go` tests pass unedited, and `NormalizeEntries`
  still emits `invalid read-only path %q` byte-for-byte, which is what keeps the
  `internal/app` startup diagnostics unchanged.

### Review 2 — 2026-09-19 — reopened; R2-01 closed the same day, status `Complete`

**R2-01 — blocker — `internal/git/policy.go:41-57` and `:76-92`, with
`internal/git/readonly.go:69-104`; this phase's Resolution table.**
R1-01's fix moved the cross-set *overlap test* above the intra-set coverage
collapse, but left **resolution** below it. `NewPolicy` still stores
`collapseEntries(ro)` and `collapseEntries(wr)`, and `Protects` resolves the
longest match over those collapsed sets (`policy.go:80-81` reads
`p.readOnly.covering` / `p.writable.covering`). An entry the operator wrote is
therefore still destroyed before the decision that matters, whenever the other
set does not name it *exactly* but does sit between it and its own ancestor.

Reproduced with a throwaway test in package `git` (removed afterwards):

| Configuration | Normalized sets | `Protects` | Expected |
| --- | --- | --- | --- |
| `--read-only-paths notes,notes/agent-a/locked --writable-paths notes/agent-a` | `readOnly=[notes] writable=[notes/agent-a]` | `notes/agent-a/locked/secret.md` → **writable** | protected |
| `--read-only-paths a/b --writable-paths a,a/b/c` | `readOnly=[a/b] writable=[a]` | `a/b/c/f.md` → **protected** | writable |
| `--read-only-paths a/b/c --writable-paths a/b` (control, no ancestor) | `readOnly=[a/b/c] writable=[a/b]` | `a/b/c/f.md` → protected | protected |

Row one is the damaging one and it is the composition the feature exists for: an
operator confines an agent to `notes/agent-a` and keeps `notes/agent-a/locked`
read-only. `notes/agent-a/locked` is covered by `notes` *within the read-only
set*, so `collapseEntries` drops it, and the only surviving read-only match for
`notes/agent-a/locked/secret.md` is `notes` (4 folded bytes) against the writable
`notes/agent-a` (13), so the writable side wins. Rows one and three differ only
in whether the unrelated ancestor `notes` is also listed as read-only: **adding a
broader read-only entry makes a narrower read-only entry writable.** That is
exactly the dependency README invariant 5 was sharpened to forbid — "whether the
guardrail holds comes to depend on which unrelated ancestors happen to be listed
beside it" — expressed through resolution instead of through the refusal.

It is not a resolution-only fault. At the enforcement input, with the same
policy, a baseline holding `notes/agent-a/locked/secret.md` = `original` and a
local tree holding `TAMPERED`:

```
p.ChangedProtected(repo, local, base) = []
```

so `Notebook.enforcePolicy` short-circuits at `commit.go:364` and the commit is
published. README invariant 3 ("A configured process never publishes a change to
a protected path") does not hold for this configuration, nor does D1's "the
longest matching entry wins", nor the clause `docs/slivingdoc-v1.md:344` states
as accepted contract.

**Why the suite does not see it.** Every row of this phase's Resolution table is
a two-level composition — one entry per set — so no test configures an entry
that its own set's ancestor covers *and* then resolves a path under it. The
uniqueness oracle that would be the natural guard,
`TestPolicyProtectsLongestMatchIsUnique`, brute-forces over `p.ReadOnly()` and
`p.Writable()` — the already-collapsed accessors — so it is structurally unable
to observe an entry the collapse removed. `TestNewPolicyRejectsCollapsedOverlap`
pins the refusal for the collapsed *exact* overlap and nothing about resolution.
Review 1's own "Verified good" entry, "`Protects` resolves longest match
correctly in both nesting directions", is true only for the two-level case it
enumerated.

- [x] Resolve over the entries the operator wrote: keep the validated written
      entries on `PathPolicy` and let `Protects` take the longest written match
      across both sets, or collapse an entry only when no entry of the *other*
      set lies between it and its covering ancestor. Whichever is chosen,
      `ReadOnly()` and `Writable()` must keep returning the collapsed, sorted
      entries, because Phase 4 advertises them and Phase 3 pins their text.
      Closed by the second option: `collapseEntries(items, other)` keeps an
      entry the other set splits from its own-set ancestor, and `covering`
      resolves the longest entry of a set rather than the first
      (`internal/git/readonly.go`, `internal/git/policy.go`). The first option
      alone would not have survived the production path, which rebuilds the
      policy from the collapsed accessors twice more (R2-03). The accessors
      still return the sorted normalized entries; what changed is which entries
      normalization keeps, which is a README shape change and is recorded
      there.
- [x] Add Resolution-table rows for the three-level composition in both nesting
      directions, and make them non-vacuous by reverting the fix and watching
      them fail.
      Closed: six Resolution rows and one normalization table, proven by
      `TestPolicyProtectsResolutionTable`, `TestNewPolicyKeepsCrossSetCoverage`
      and `TestPolicyRebuiltFromAccessorsResolvesAlike`, each probed against
      both halves of the pre-fix shape.
- [x] Re-point `TestPolicyProtectsLongestMatchIsUnique` at the written entries,
      or add a second oracle over them; as written it cannot fail on this class.
      Closed: the test now brute-forces `longestWrittenMatch` over the entries
      the configuration was written with and compares the verdict against
      `Protects`, over one two-level and both three-level compositions.
- [x] Name the enforcement consequence at Phase 2's boundary (see that phase's
      Review 2 section).
      Phase 2 owns the boundary scenario and is reopened for it. This phase
      pins the consequence at its own boundary:
      `TestChangedProtectedReportsThreeLevelProtection` reports the tampered
      file that `ChangedProtected` returned nothing for.

### Verified good (review 2)

- **R1-01 is genuinely fixed for the case it names.** `NewPolicy` validates both
  sets with `validateEntries`, compares `r.folded == w.folded` over the written
  entries, and only then calls `collapseEntries` (`policy.go:41-57`). Re-checked
  by reading the code, not the notes.
- **The `validateEntries` / `collapseEntries` split is behaviour-preserving for
  the read-only path.** `normalizeEntries` is the exact composition of the two
  and its body is line-for-line the previous one; `git diff internal/git/readonly.go`
  shows no change to the collapse rule, the sort key, the dedup, or the
  `invalid read-only path %q` message. All eight pre-existing
  `internal/git/readonly_test.go` tests and the `internal/app` startup
  diagnostics pass unedited, and `TestNormalizeEntriesMatchesPreviousBehaviour`
  pins it.
- **`OverlapError` still carries the raw written entry of each side**, which is
  what `internal/app/config.go:288-292` renders into the both-flags message, so
  the fix moved no signature Phases 2–4 consume.
- **R1-09 is closed**: the README "Shared interface between phases" block now
  declares `type PathPolicy struct` with the method set the code ships.
- **Detection and restore are unchanged by the fix pass** and still hold what
  review 1 recorded: `ChangedProtected` calls `ReadTree` only on every branch,
  `RestoreProtected` reads only the named paths, and the empty-list branch reads
  nothing.

### Review 3 — 2026-09-19 — not reopened; one defect (efficiency) filed against this phase

**R3-01 — defect (efficiency) — `internal/git/policy.go:185` (`appendUnique`,
declared `:303`), `:228` (`namedOrUnder`, declared `:294`), `:244` `readBlobAt`
with `:274` `findEntry`.** Detection and restore are each quadratic in the
number of changed protected paths, and the unmatched-default inversion is what
makes a large number reachable.

Three independent quadratic terms, all over `changed`:

| Site | Cost | Why it is there |
| --- | --- | --- |
| `collectSide` → `appendUnique` → `slices.Contains` | O(N²) in the reported paths | Load-bearing, not defensive: `diffTrees`' "same name, different shape or different content" branch collects **both** sides, so an ordinary modified protected blob is appended twice and the scan is what de-duplicates it |
| `RestoreProtected` → `namedOrUnder(f.Path, changed)` | O(M·N) over local files × reported paths | The prefix test must stay a prefix test for the type-change direction, but it is run once per local file against the whole list |
| `readBlobAt` → `findEntry` | O(N²) when the reported paths share a directory, plus one root-down re-walk and N re-reads of the same trees | Each restored path is resolved from the root independently, and `findEntry` scans the directory linearly |

Reproduced with a throwaway test in package `git` (removed afterwards), a fake
repository, `--writable-paths w`, and N protected paths changed outside `w`:

| N | detect / restore, N paths **added** locally | detect / restore, N paths **deleted** locally |
| --- | --- | --- |
| 500 | 1 ms / 4 ms | 2 ms / 2 ms |
| 1 000 | 3 ms / 18 ms | 6 ms / 7 ms |
| 2 000 | 12 ms / 81 ms | 15 ms / 17 ms |
| 4 000 | 27 ms / 198 ms | 51 ms / 59 ms |
| 8 000 | 125 ms / 941 ms | 152 ms / 168 ms |

Roughly four times the cost per doubling on both columns and both directions,
which is the signature. Extrapolated on the worst column, thirty thousand
reported paths is about a quarter minute of pure CPU inside one refusal.

**Why this is a finding now and was not one for the read-only set.** Under
`--read-only-paths` alone, N is bounded by the region the operator named, and an
operator who names a huge region has chosen it. D2's inversion makes N bounded by
*everything outside* the writable region, so the number is set by what the agent
wrote rather than by what the operator listed — and an agent dumping a build
directory, a cache, or a checkout outside its own directory is precisely the
confinement case the feature exists for. The failure scenario: a process run with
`--writable-paths agents/scout`, an agent that creates thirty thousand files
outside `agents/scout`, and one `notes_commit` — the refusal is **correct**, names
every path, resets every file, and publishes nothing, but the caller waits tens of
seconds for it, and every retry pays it again.

**What it does not break.** Invariant 6 is untouched: the quadratic work is over
tree entries already in hand, and `TestChangedProtectedReadsNoBlob` still holds —
detection opens no blob, and the restore still reads only the named paths.
Invariants 3, 5 and 7 are untouched; the answer is right, only slow. No
acceptance row of this phase makes a cost claim that this violates, which is why
this finding does not reopen the phase — see R3-03 in the README, which is the
missing claim rather than a broken one.

- [ ] De-duplicate the reported paths through a `map[string]struct{}` rather than
      `slices.Contains`, keeping the two-sided collection that makes the
      de-duplication necessary.
- [ ] Replace `namedOrUnder`'s per-file scan with a lookup of each local path and
      its ancestor prefixes against a set built once from `changed`, which is
      O(depth) per file and keeps the segment-boundary semantics exactly.
- [ ] Resolve the restore reads against one memoized walk of the base tree rather
      than once per path from the root, so N paths in one directory cost one
      `ReadTree` of that directory instead of N.

### Verified good (review 3)

- **The resolution core is unchanged since the holistic pass and still reads
  correctly.** `collapseEntries`' `entryBetween` guard, `covering`'s
  longest-match resolution and `NewPolicy`'s pre-collapse overlap test are
  byte-for-byte what the holistic review enumerated over 2 187 configurations;
  no third instance of the R1-01/R2-01 class was found by re-reading them
  against `internal/app/config.go`, `internal/app/service.go` and
  `internal/notebook/notebook.go`.
- **A trailing-slash entry cannot silently match nothing.** `validateEntries`
  trims exactly one trailing slash and then calls `ValidatePath`, which rejects a
  path that ends in a slash (`internal/git/path.go:40-42`), so `a//` refuses
  startup naming the raw entry rather than normalizing to an `a/` entry that
  `isEntryAncestor` could never match. This was probed because the trim is
  single, not repeated.
- **`appendUnique` is not dead defensive code.** The modify branch of `diffTrees`
  collects the same path from both sides, so removing the de-duplication would
  report every modified protected file twice. R3-01's fix must keep the
  de-duplication and change only its cost.
- **`Protects` has no tie case and no unreachable default.** All four branches
  were re-read: both-match resolves on folded length, a single match decides, and
  the unmatched branch is the documented inversion. Equal folded lengths across
  the sets imply the same folded string, which `NewPolicy` refuses.
- **The accessors still copy.** `TestPolicyEntriesAreCopies` covers it and
  `EntrySet.Entries` allocates, so the policy value shared by one `Service`
  across concurrent requests cannot be mutated through a returned slice.
