# Phase 1 — Path policy resolution

**Status:** Not Started

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
not travel under a read-only label, and changes nothing about its behavior.

| Existing name | Generalized name |
| --- | --- |
| `ReadOnlySet` | `EntrySet` |
| `NormalizeReadOnly` | `NormalizeEntries` |
| `isReadOnlyAncestor` | `isEntryAncestor` |
| `readOnlyFold` | `entryFold` |

The rename is mechanical and behavior-preserving. This phase owns it and updates
every in-repository reference, so Phases 2 and 3 find one name. The references
are in `internal/app/config.go`, `internal/app/service.go`, and
`internal/notebook/notebook.go`. `EntrySet` keeps its existing methods; the
methods that exist only to serve the read-only inversion (`Covers`,
`CoveringEntry`, `ChangedUnder`, `Pin`, `ReadCovered`) stay on it unchanged and
become the policy's building blocks rather than the notebook's direct
dependency.

### Construction

`NewPolicy(readOnly, writable []string) (PathPolicy, error)` normalizes each set
independently and then rejects an overlap. Coverage **across** the sets is not
collapsed: a writable entry below a read-only entry, or a read-only entry below a
writable entry, is the composition the operator is expressing. Only exact
equality under case folding is ambiguous, and it is refused.

The zero policy — both sets empty — protects nothing and reports `Configured()`
false. Every caller short-circuits on it, so an unconfigured process does no
policy work at all.

### Resolution

`Protects(path)` finds the longest entry across both sets that is `path` itself
or an ancestor of it on a segment boundary under case folding, and answers from
the set that entry came from. With no such entry it answers from the unmatched
default named in the README parameters table.

The longest match is unique. Within one set, normalization has already dropped
every entry covered by another, so no two entries of one set can both match.
Across the sets, two matching entries of equal folded length would be the same
folded string, which construction has already refused. This argument is the
reason resolution needs no tie-break rule, and a test pins it.

| Input | Resolution | Test |
| --- | --- | --- |
| Matched only by a read-only entry | Protected | `TestPolicyProtects_ResolutionTable` |
| Matched only by a writable entry | Writable | `TestPolicyProtects_ResolutionTable` |
| Matched by both sets, the read-only entry longer | Protected | `TestPolicyProtects_ResolutionTable` |
| Matched by both sets, the writable entry longer | Writable | `TestPolicyProtects_ResolutionTable` |
| Matched by no entry, writable set empty | Writable | `TestPolicyProtects_UnmatchedDefault` |
| Matched by no entry, writable set non-empty | Protected | `TestPolicyProtects_UnmatchedDefault` |
| Both sets empty | Not configured; protects nothing | `TestPolicy_Configured` |
| An entry that is a prefix of a path but not on a segment boundary | Unmatched; falls to the default | `TestPolicyProtects_SegmentBoundary` |
| A path differing from an entry only by letter case | Matched | `TestPolicyProtects_CaseFolding` |

### Detection traversal

`ChangedProtected(repo, local, base OID)` walks the two trees in parallel and
returns, sorted, every protected path that differs between them. It reads tree
objects only. It never calls `ReadBlob`, because a differing blob is proved by a
differing object ID and the identity of the content is not needed to report the
path.

| Property | Mechanism | Test |
| --- | --- | --- |
| A subtree whose ID is equal on both sides is skipped whole | Compare the tree entry ID before descending | `TestChangedProtected_SkipsEqualSubtrees` |
| No blob is read | The traversal calls `ReadTree` only | `TestChangedProtected_ReadsNoBlob` |
| A path added on the local side is reported when protected | An entry present on one side only is a difference | `TestChangedProtected_ReportsAdditions` |
| A path removed on the local side is reported when protected | Same, in the other direction | `TestChangedProtected_ReportsRemovals` |
| A path whose content differs is reported when protected | Differing blob IDs at the same path | `TestChangedProtected_ReportsModifications` |
| A differing path that is not protected is not reported | `Protects` filters every difference | `TestChangedProtected_IgnoresWritableDifferences` |
| A subtree containing only writable paths is still descended when it also contains protected ones | Descent is decided by ID equality, never by the policy | `TestChangedProtected_DescendsMixedSubtree` |
| The result is sorted and free of duplicates | Sort before returning | `TestChangedProtected_SortedResult` |
| An unconfigured policy reports nothing and walks nothing | Short-circuit on `Configured` | `TestChangedProtected_UnconfiguredWalksNothing` |

The counted-call assertions use a `Repository` fake that counts `ReadTree` and
`ReadBlob` calls. `internal/notebook` and `internal/workspace` already carry fake
repositories against the engine seam, and the duplication policy in `AGENTS.md`
names that mirroring as acceptable, so a third fake local to this package is in
keeping rather than a clone to merge.

### Restore

`RestoreProtected(repo, base OID, local Snapshot, changed []string)` returns
`local` with each named path taken from the base tree. It reads exactly the named
paths and nothing else, so its cost is proportional to the violation rather than
to the notebook. Callers pass the result of `ChangedProtected`, so an operation
with no protected change never calls it at all.

| Case | Result | Test |
| --- | --- | --- |
| A named path present in both | Local content replaced by base content | `TestRestoreProtected_ReplacesContent` |
| A named path present in base only | Added back to the snapshot | `TestRestoreProtected_RestoresDeletion` |
| A named path present in local only | Dropped from the snapshot | `TestRestoreProtected_DropsAddition` |
| A path not named | Untouched, whatever the policy says about it | `TestRestoreProtected_TouchesOnlyNamedPaths` |
| An empty name list | The snapshot is returned unchanged and no object is read | `TestRestoreProtected_EmptyListReadsNothing` |
| The result | Sorted by path, valid as a snapshot | `TestRestoreProtected_ResultIsValidSnapshot` |

### Files

- `internal/git/policy.go` — new: `PathPolicy`, `NewPolicy`, resolution, the
  traversal, and the restore.
- `internal/git/policy_test.go` — new: the tables above and the counting fake.
- `internal/git/readonly.go` — renamed symbols; behavior unchanged.
- `internal/git/readonly_test.go` — renamed symbols; assertions unchanged.
- `internal/app/config.go`, `internal/app/service.go`,
  `internal/notebook/notebook.go` — mechanical rename of the referenced symbols
  only. No field, flag, or behavior changes here; those belong to later phases.

## Integration contract

`unit-test-only`. The policy has no observable boundary until Phase 2 wires it
into `Notebook.Pull` and `Notebook.Commit`. Every guarantee in this phase is a
property of pure functions and of a traversal over an injected `Repository`, so
the unit boundary is the narrowest one that exercises the real behavior.

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| Both sets normalize exactly as the read-only set does today: trailing slash trimmed, invalid entry refused, covered entry dropped, result sorted | `TestNormalizeEntries_MatchesPreviousBehaviour` |
| Coverage across the sets is preserved, not collapsed | `TestNewPolicy_KeepsCrossSetCoverage` |
| A path named by both sets refuses construction | `TestNewPolicy_RejectsExactOverlap` |
| A path named by both sets differing only in letter case refuses construction | `TestNewPolicy_RejectsCaseFoldedOverlap` |
| Resolution follows the resolution table in every row | `TestPolicyProtects_ResolutionTable` |
| The unmatched default follows the writable set's emptiness | `TestPolicyProtects_UnmatchedDefault` |
| Matching is on segment boundaries under case folding | `TestPolicyProtects_SegmentBoundary`, `TestPolicyProtects_CaseFolding` |
| The longest match is unique, so resolution needs no tie-break | `TestPolicyProtects_LongestMatchIsUnique` |
| `ReadOnly` and `Writable` return copies a caller cannot use to mutate the policy | `TestPolicy_EntriesAreCopies` |
| Detection reports every protected difference and no writable one | the detection traversal table |
| Detection reads no blob | `TestChangedProtected_ReadsNoBlob` |
| Restore reads only the named paths | `TestRestoreProtected_EmptyListReadsNothing`, `TestRestoreProtected_TouchesOnlyNamedPaths` |
| The rename leaves every existing read-only assertion passing unedited | `make test` over the renamed `internal/git/readonly_test.go` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| An entry in either set fails `ValidatePath` | Construction fails, naming the offending entry and which set it came from | `TestNewPolicy_RejectsInvalidEntry` |
| An entry names the notebook root, empty or dot | Refused by `ValidatePath` as it is today, with the same message | `TestNewPolicy_RejectsRootEntry` |
| A path is named by both sets | Construction fails, naming the path and both sets, so the caller can render a message naming both flags | `TestNewPolicy_RejectsExactOverlap` |
| `ReadTree` fails during detection | The error is wrapped with the package prefix and the tree path, and no partial result is returned | `TestChangedProtected_RepositoryFailure` |
| A tree entry carries an unsupported mode | Detection fails naming the path, as the existing covered walk does | `TestChangedProtected_UnsupportedMode` |
| `ReadBlob` fails during restore | The error is wrapped with the package prefix and the path, and no partial snapshot is returned | `TestRestoreProtected_RepositoryFailure` |
| Restore is asked for a path absent from both sides | The path is skipped rather than failing, since detection cannot produce it and a caller that does is not corrupting state | `TestRestoreProtected_UnknownPathSkipped` |
| The restored snapshot would violate the content rules | The existing snapshot validation rejects it, with the failure surfacing to the caller unchanged | `TestRestoreProtected_InvalidSnapshotRejected` |

## Implementation notes

Not started.

## Review findings

None.
