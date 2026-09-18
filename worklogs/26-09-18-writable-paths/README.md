# slivingdoc writable paths worklog

**Status:** Not Started

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)

## Goal

Let an operator declare the notebook paths one slivingdoc process *may* write,
instead of only the paths it may not. A fleet of agents sharing one notebook can
then be confined to a directory each: everything outside the declared region is
protected by default, and the process advertises where writing is allowed, so an
agent learns the rule before it edits and again in the refusal. The two settings
compose — the longest matching entry wins, so a protected region can hold a
writable subdirectory — and a path named by both sets is a configuration error
rather than a silent precedence rule. The enforcement point does not move: this
is the existing commit refusal and pull restore evaluated against a policy that
can now default to protected. What does move is the detection that feeds them.
Today the protected side is proved by reading every protected file out of the
baseline; inverted, that would read the whole notebook on every pull and commit.
Detection therefore becomes a comparison of tree hashes, and baseline file
content is read only when a protected path actually changed.

## Status board

| Phase                                                             | Status      | Outcome |
| ----------------------------------------------------------------- | ----------- | ------- |
| [1. Path policy resolution](phase-1-path-policy.md)               | Not Started | The complete `PathPolicy`: longest-match resolution, the unmatched default, exact overlap as a typed error, and the tree traversal that detects and restores protected changes. |
| [2. Enforcement over the policy](phase-2-enforcement.md)          | Not Started | Both operations hold the policy: commit refuses and resets, pull restores, and neither reads a baseline blob unless a protected path changed. |
| [3. Flag and configuration](phase-3-flag-and-config.md)           | Not Started | `--writable-paths` resolves like every other flag; exact overlap refuses startup before the engine and the store load. |
| [4. Advertisement and CLI report](phase-4-advertisement.md)       | Not Started | Every surface an agent reads names where it may write; the CLI report renders the set. |
| [5. Quality gate](phase-5-quality-gate.md)                        | Not Started | `make qa` green, evidence check, duplication verdicts recorded. |

Phases complete in numeric order. Phase 2 depends on Phase 1 (it holds the
policy the first phase builds). Phase 3 depends on Phase 2 (the flag resolves
into the service config field Phase 2 introduces). Phase 4 depends on Phase 3
(its CLI fixtures run with the flag). Phase 5 depends on all of them.

There is no gating phase 0. The design rests on code already read, not on an
unverified claim: the cost question that would have justified a gate dissolved
when hash comparison replaced the baseline content read, and the resulting
property is asserted directly by Phase 2 rather than measured.

An executing agent reads this README and only its phase file. Anything two
phases share is written here.

## Strategy

### Evidence the design rests on

- Enforcement lives at exactly two call sites. `Notebook.Pull` pins protected
  paths to the baseline before the merge
  ([`internal/notebook/pull.go`](../../internal/notebook/pull.go)), and
  `Notebook.enforceReadOnly` refuses a commit that changed one and resets the
  touched files through `applyLocal`
  ([`internal/notebook/commit.go`](../../internal/notebook/commit.go)). Nothing
  else consults the set. Inverting the policy therefore changes the type those
  two sites hold, not where enforcement happens.
- `Repository.ReadTree` returns each entry's name, mode and object ID **without
  reading content** ([`internal/git/engine.go`](../../internal/git/engine.go)),
  and a tree's ID is computed over entries that carry their children's IDs. Two
  trees with the same ID have identical content beneath them, so a walk can skip
  an entire subtree without opening a file. The interface already documents this
  principle on `HasObject`: "a full `ReadBlob` would inflate every blob only to
  discard the bytes."
- `BuildTree` is deterministic — "the same normalized snapshot always produces
  the same tree OID" ([`internal/git/tree.go`](../../internal/git/tree.go)) — so
  comparing a tree built from the visible directory against the stored baseline
  tree is a sound test for "did anything change here".
- The commit path already builds the local tree, and `state.json` already stores
  `baselineTree`, so both sides of that comparison are in hand before any
  protected-path work begins. No new read is needed to obtain them.
- Today's pull pins on **every** pull, whether or not a protected path changed,
  because `ReadCovered` is the only way it can learn the baseline content. With
  hash detection an unchanged protected region needs no pin at all: the pinned
  tree would equal the local tree.
- The visible directory is scanned in full on every operation, with each file's
  content read into the snapshot
  ([`internal/workspace/scan.go`](../../internal/workspace/scan.go)). That cost
  is pre-existing and unchanged by this work. The property this worklog claims is
  narrower and exact: no **baseline** blob is read unless a protected path
  changed.
- `NormalizeReadOnly` drops an entry covered by another entry in the same set
  ([`internal/git/readonly.go`](../../internal/git/readonly.go)). That collapse is
  correct within one set and must **not** be applied across the two sets, where
  coverage is the feature: a writable entry below a read-only entry is the case
  the operator is expressing.
- `ValidatePath` rejects the empty string and a `.` segment
  ([`internal/git/path.go`](../../internal/git/path.go)), so no entry can denote
  the notebook root. "Everything except this directory" is therefore not
  expressible by listing entries, which is why a non-empty writable set flips the
  unmatched default rather than relying on a root entry.
- The black-box harness configures the set directly through `HarnessConfig`
  ([`internal/integrationtest/harness.go`](../../internal/integrationtest/harness.go)),
  as the read-only scenarios already do, so Phase 2 carries real scenarios before
  the CLI flag exists.
- The known downstream consumer tolerates the additive surfaces. Its report
  decoder ignores unrecognized trailer lines and requires only the `retryable:`
  trailer, and its success parser matches the status line by pattern, so a new
  trailer and a new envelope field are invisible to a pinned client.

### Trust boundary

Unchanged from the read-only set: this is a guardrail at the MCP tool boundary,
not a security boundary against the agent. The serve process holds the S3
credentials, and an agent that can read that environment or launch its own
slivingdoc process bypasses the setting. The policy lives in the server
configuration, never in the data.

### Non-negotiable invariants

Every phase preserves:

1. MCP and the one-shot `pull`/`commit` subcommands remain the only public APIs
   and expose the same two operations. Every flag stays shared by `serve`,
   `pull`, and `commit`.
2. The existing error fields keep their meaning and presence, and the seven codes
   stay the complete code set. The `readOnly` array keeps meaning exactly what it
   means today — the normalized read-only set — and never becomes a stand-in for
   the protected region. New fields are additive.
3. A configured process never publishes a change to a protected path: no commit
   from it adds, modifies, or deletes a file there, and a pull restores such a
   path from the accepted remote state.
4. With an empty writable set, resolution and every advertised surface are
   byte-for-byte what they are today, except for the always-present empty
   `writable` array.
5. A path named exactly by both sets refuses startup, before the native engine
   and the object store are touched.
6. No baseline blob is read on an operation in which no protected path changed.
7. Recovery semantics are unchanged: the restore runs through `applyLocal`, and a
   failure during it is `RECOVERY_FAILURE` with the existing recovery stage.

### Shared interface between phases

Phase 1 builds this type in `internal/git`; Phase 2 is its only consumer; Phases
3 and 4 read its entry accessors. No phase may widen it without a README change.

```go
// PathPolicy answers, for one process, which notebook paths may be written and
// which changes to protected paths an operation must refuse or restore.
type PathPolicy interface {
	// Configured reports whether either set is non-empty. An unconfigured
	// policy protects nothing and is the zero value.
	Configured() bool

	// Protects reports whether path may not be written, by longest-match
	// resolution over both sets and the unmatched default.
	Protects(path string) bool

	// ReadOnly and Writable return copies of the normalized, sorted entries
	// of each set; never nil.
	ReadOnly() []string
	Writable() []string

	// ChangedProtected returns, sorted, every protected path that differs
	// between the local and base trees. It descends only where subtree IDs
	// differ and reads no blob content.
	ChangedProtected(repo Repository, local, base OID) ([]string, error)

	// RestoreProtected returns local with each named path replaced by its
	// base content, adding back a path absent from local and dropping one
	// absent from base. It reads only the named paths. Callers pass the
	// result of ChangedProtected, so a clean operation never calls it.
	RestoreProtected(repo Repository, base OID, local Snapshot, changed []string) (Snapshot, error)
}
```

### Severity taxonomy

Review and validation findings carry one of:

- **blocker** — an invariant above is broken, or a phase ships behavior its
  acceptance table does not cover.
- **defect** — behavior is wrong or untested, but no invariant is at risk.
- **note** — clarity, naming, or documentation; never blocks a phase.

## Parameters and owners

Every default and tunable lives here once. Phase files refer to rows by name and
never restate a value.

| Parameter | Default | Owner |
| --- | --- | --- |
| `--writable-paths` flag | empty | Phase 3 |
| `SLIVINGDOC_WRITABLE_PATHS` environment variable | unset | Phase 3 |
| Entry separator in the flag and environment value | comma, as `--read-only-paths` uses today | Phase 3 |
| Advertised entry separator | `notebook.ReadOnlyListSeparator` | Phase 4 |
| `app.Config.WritablePaths` service config field | nil | Phase 2 |
| `notebook.Config.WritablePaths` | nil | Phase 2 |
| `integrationtest.HarnessConfig.WritablePaths` | nil | Phase 2 |
| Unmatched-path resolution | writable when the writable set is empty, protected when it is non-empty | Phase 1 |
| Baseline blob reads on an operation with no protected change | none | Phase 2 |
| Entry matching | segment boundary, Unicode case folding, as the read-only set matches today | Phase 1 |

## Readiness checklist

The author runs this before requesting validation and records the outcome in the
session journal.

1. No numerals with units in phase files outside oracle rows:
   `grep -nE '(^|[^=])\b[0-9]+([.,][0-9]+)? ?(s|ms|MB|KiB|%)\b' phase-*.md`
2. Every test name cited in a phase is declared in exactly one phase and one file
   list.
3. Every config field, flag, and injectable field has exactly one owner row in
   Parameters and owners.
4. Every invariant and limit in a phase is a table with a test per row, not
   prose.
5. Every phase mentioning listening, manual, or paid carries a `Human required`
   subsection. (Expected: none do.)
6. No phase references text scheduled for deletion.
7. New conventions do not contradict existing code conventions. Cite the file
   checked: `internal/git/readonly.go` for set normalization and matching,
   `internal/app/config.go` for flag resolution, `AGENTS.md` for the error
   taxonomy and the additive-field rule.

## Decisions log

| ID | Date | Decision | Rationale | Replaces |
| --- | --- | --- | --- | --- |
| D1 | 2026-09-18 | **maintainer** The two sets compose by longest match, and a path named exactly by both is a configuration error. Sub-paths are the feature: `--read-only-paths notes --writable-paths notes/A` permits writing under `notes/A`. | Mutual exclusion would force an operator to enumerate every sibling directory to protect them; silent precedence would make a security-shaped flag ignorable. Exact overlap is the only genuinely ambiguous case, so it is the only error. | — |
| D2 | 2026-09-18 | **maintainer** A non-empty writable set flips the unmatched default to protected. An empty writable set leaves the default writable, exactly as today. | `ValidatePath` cannot express the notebook root, so "everything except this directory" is otherwise inexpressible: root files and new top-level directories would stay writable and the confinement would leak. Failing closed is the correct bias for a guardrail. | — |
| D3 | 2026-09-18 | **maintainer** The refusal names where the agent *may* write, and follows the existing error management rather than inventing a parallel one. | Under default-protected the protected region is nearly the whole notebook; listing it would be useless to an agent and enormous on every surface. The writable set is short and actionable. | — |
| D4 | 2026-09-18 | **author** Detection compares tree IDs and reads no blob content; baseline content is read only for the paths a violation actually touched. | `ReadTree` yields child IDs without content and a tree ID covers everything beneath it, so an unchanged subtree is provably unchanged without being opened. The alternative — inverting `ReadCovered` — would read the whole notebook on every pull and commit under the normal configuration. | — |
| D5 | 2026-09-18 | **author** The property in D4 is asserted as a counted-call invariant against a fake repository, not as a benchmark with a threshold. | A counting fake proves "no baseline blob was read" exactly and deterministically under `-race -count=3`; a wall-clock threshold would be machine-dependent and would need a parameter no test could reliably trigger. | — |
| D6 | 2026-09-18 | **maintainer, author** A stat-cache index over the visible directory is considered and deferred to its own effort. | It would cut the pre-existing full scan of the visible directory, which compounds with this feature, but it is a different risk class: it fails open and silently where this feature fails closed and loudly, it belongs in the private root rather than the shared pack cache because it is mutable per-workspace state rather than immutable content-addressed data, it inherits the racy-index problem under many agents sharing one directory, and it would migrate the strict versioned `state.json`. No measurement yet shows the local scan is material against the object-store round trips. | — |
| D7 | 2026-09-18 | **author** The `readOnly` envelope array keeps its present meaning and a parallel `writable` array is added, always present and empty when unconfigured. | `AGENTS.md` pins `readOnly` as a stable, additive, always-present field carrying the read-only set. Overloading it with a protected region that is mostly the whole notebook would change a documented field's meaning and break the one known consumer's expectations. | — |

## Definition of success

1. An operator can confine a process to one notebook directory with a single
   `--writable-paths` entry, and every path outside it — including files at the
   notebook root and directories that did not exist when the process started — is
   refused on commit and restored on pull. Proven by the Phase 2 scenarios and
   the Phase 3 flag tests.
2. An operator can open a subdirectory inside a protected region with the two
   flags together, and a path named exactly by both refuses startup with a
   message naming both flags. Proven by the Phase 1 resolution table and the
   Phase 3 startup-refusal tests.
3. An agent that writes where it may not receives a refusal naming the paths it
   may write, carrying the existing `code`, `reason`, `action`, and per-file
   `reason` tokens, and finds the same set in the server instructions, both tool
   descriptions, and every success and error envelope. Proven by the Phase 2
   refusal tests and the Phase 4 advertisement tests.
4. An operation in which no protected path changed reads no baseline blob.
   Proven by the Phase 2 counted-call invariant.
5. A process with neither set configured behaves byte-for-byte as it does today
   on every surface except the always-present empty `writable` array. Proven by
   the Phase 4 unconfigured-surface tests.
6. `make qa` passes unedited, at or above the repository coverage floor, with
   duplication verdicts recorded against the duplication policy. Proven by
   Phase 5.

## Validation policy

The repository gates apply unchanged and no exception is claimed. `make qa` runs
`lint`, `test`, and `npm-test`; `go test ./... -race -count=3 -timeout=30s
-coverpkg=./...` must pass unedited, at or above the seventy percent coverage
floor; `gofumpt`, `staticcheck`, `go vet`, and `go fix -diff` must be clean; and
`dupl -t 80` is a signal read against the duplication policy in `AGENTS.md`, not
a verdict. Tool versions are pinned to the baseline recorded in `AGENTS.md`.

No phase requires a person, a paid run, or a machine-local asset. No phase
introduces a `testing.Short()` guard, a conditional skip, or a second gate
command.

Every phase that changes behavior updates `docs/slivingdoc-v1.md` in the same
commit, per the AGENTS.md invariant contract.

## Feedback index

None yet. One entry per validation or review round, mapping each finding ID to
the edit or decision that closed it.

## Session journal

### 2026-09-18 — design and authoring

Design settled over a working session against the code. Investigated whether
`--read-only-paths` could express "everything except one directory" and
established that it cannot: `NormalizeReadOnly` silently drops an entry covered
by another, so an exception entry is swallowed with no error, and `ValidatePath`
cannot name the notebook root. Established that per-agent scoping in one shared
checkout is unsafe without per-agent checkouts, because a pull pins protected
paths back to the baseline and would discard a concurrent agent's in-flight work;
that constraint belongs to the consumer's deployment, not to this feature, and is
recorded here only so it is not rediscovered.

Q1 settled D1, Q2 settled D2, and the maintainer added D3 during the same
exchange. The third question — whether the inverted covered read needed a gating
phase 0 to measure it — was withdrawn: the maintainer asked why matching could
not simply be done by pattern, which led to establishing that matching was never
the cost and that detection can compare tree IDs instead of content. That became
D4, and D5 turned the resulting property into an assertion rather than a
benchmark. D6 records the deferred index after the maintainer proposed reusing
the shared pack cache pattern for it.

README written and phases authored in the same session at the maintainer's
request, who approved the phase list before authoring and asked for both steps in
one pass rather than signing off the README separately.

Readiness checklist run against the five phase files:

| Line | Outcome |
| --- | --- |
| 1. No numerals with units outside oracle rows | Pass, clean |
| 2. Every test name declared in exactly one phase | One finding, fixed: Phase 5 restated a test name Phase 3 declares, in its maintenance-contract table. Phase 5 now cites Phase 3's acceptance row instead. Re-run clean across eighty-one distinct names. |
| 3. Every config field, flag, and injectable field has one owner | Pass. Phases 2 and 3 both edit the service file, but Phase 2 introduces the config field and the accessor while Phase 3 introduces only the flag that resolves into it; the rule binds fields, not files. |
| 4. Every invariant and limit is a table with a test per row | Pass. The surfaces list in Phase 4 has no test column by design: it enumerates touchpoints, and the guarantees over them are in the set-state and structured-field tables. |
| 5. Every phase mentioning listening, manual, or paid has `Human required` | Pass, none do |
| 6. No phase references text scheduled for deletion | Pass. Phase 1 renames symbols and deletes none. |
| 7. New conventions do not contradict existing code conventions | Pass. Checked `internal/git/readonly.go` (the policy reuses its normalization and matching unchanged), `internal/app/config.go` (the flag mirrors the existing resolution and split), and `AGENTS.md` (existing error tokens are reused rather than extended, and the new envelope field follows the additive always-present rule). |

Ready for [[worklog-validate]]. Every phase is `Not Started`.
