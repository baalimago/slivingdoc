# slivingdoc writable paths worklog

**Status:** Every phase `Complete` — all review-1 and review-2 findings are closed, R2-04 with the review-2 gate sweep. The holistic review and **review 3** (below) both read **ship**: **no phase is reopened**. Open for the maintainer: V1-13 (unpinned downstream-consumer claim), H-01 (a writable file over a protected directory loses the file — inherited from `8047406`, not introduced), H-07 (`make qa` root-package timeout flake — inherited), H-09 (an eighth invariant binding the completeness of the local restore), and review 3's R3-01 (detection and restore are quadratic in the number of reported protected paths, which the inverted default makes reachable) with R3-03 (nothing in the contract bounds that cost). H-02 through H-06, H-08 and R3-02 are cleanups. None blocks.

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
| [1. Path policy resolution](phase-1-path-policy.md)               | Complete    | The complete `PathPolicy`: longest-match resolution, the unmatched default, exact overlap as a typed error, and the tree traversal that detects and restores protected changes. Review 1 reopened it for R1-01 and R1-09; both are closed. Review 2 reopened it for the Phase 1 half of R2-01; closed. The collapse now takes the other set and keeps an entry that set splits from its own-set ancestor, and `covering` resolves the longest entry rather than the first, so resolution answers from the entries the operator wrote and rebuilding the policy from its accessors is a fixed point. Pinned by three-level rows in `TestPolicyProtectsResolutionTable`, `TestNewPolicyKeepsCrossSetCoverage`, `TestChangedProtectedReportsThreeLevelProtection`, `TestPolicyRebuiltFromAccessorsResolvesAlike`, and by `TestPolicyProtectsLongestMatchIsUnique` re-pointed at the written entries. Review 3 re-read the resolution core and found no third instance of that class; it files R3-01 against this phase's cost, which reopens nothing. |
| [2. Enforcement over the policy](phase-2-enforcement.md)          | Complete    | Both operations hold the policy: commit refuses and resets, pull restores, and neither reads a baseline blob unless a protected path changed. Review 1 traced invariants 3, 6 and 7 through every branch and found them held. Review 2 reopened it for the Phase 2 half of R2-01; closed by four three-level black-box scenarios, two per nesting direction and one per operation, each proved non-vacuous against both halves of the pre-fix resolution. R1-05, R1-08 and R2-03 closed with it: the restored-blob exception is stated on the invariant row and pinned, the tree-build reachability argument has an oracle on both its branches, and the three rebuilds are kept and documented as a fixed point rather than collapsed. Review 3 traced invariant 3 through the generation-0 and conflicting-pull branches and found it held; it files R3-02, a stale diagnostic sentence, which reopens nothing. |
| [3. Flag and configuration](phase-3-flag-and-config.md)           | Complete    | `--writable-paths` resolves like every other flag; an overlap refuses startup before the engine and the store load, including one an ancestor of its own set also covers. Review 1 reopened it for the Phase 3 half of R1-01; closed by a startup-refusal row and three `TestSetupRefusesBeforeEngineAndProbe` rows, both nesting directions and the pair a several-overlap refusal names. Notes R1-04, P3-N1 and P3-N2 closed with it. Review 2 re-verified the closure at the operator boundary and raised nothing against this phase. |
| [4. Advertisement and CLI report](phase-4-advertisement.md)       | Complete    | Every surface an agent reads names where it may write; the CLI report renders the set. Review 1 confirmed invariant 4 byte-for-byte and the always-present arrays on every construction site. Review 2 reopened it for R2-02; closed. A writable set now replaces the read-only sentence's "write elsewhere" — false under a non-empty writable set — with the rule that decides between the two, and every surface that names both sets carries it: instructions, both tool descriptions, both text items and the CLI report. Pinned at three levels by `TestAdvertisementNestedSetsStateRule`, `TestReportNestedSetsStateRule` and `TestScenarioWritableThreeLevelAdvertisementStatesRule`. R1-06 and R1-07 closed with it: `errorText` carries both sets and `TestErrorTextCarriesBothSets` pins it, and `runCLIOK` takes the trailers a scenario expects, so the absent case is asserted. |
| [5. Quality gate](phase-5-quality-gate.md)                        | Complete    | `make qa` green at 84.6 % coverage, every duplication verdict carrying its clause, maintenance contracts and readiness checklist re-verified. Review 1 reopened it for R1-03 and R1-02; both are closed. The evidence check reads a phase's specification and its record separately, and all four checks are empty against the tree as it stands. The review-2 gate sweep re-ran the whole gate from scratch over the thirteen tests the review-2 fix passes landed, re-derived every count and every document line number, and closed R2-04 by prescribing the single-package re-run that tells a scheduling artefact from a root-package regression. |

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
  else *enforces* against the set; the MCP server, the CLI report, and the
  harness read its entries only to advertise them
  ([`internal/mcp/server.go`](../../internal/mcp/server.go),
  [`internal/app/command.go`](../../internal/app/command.go)), which is the
  surface Phase 4 extends. Inverting the policy therefore changes the type those
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
- Two pre-existing costs are unchanged by this work and are **not** what the
  no-blob property claims. The visible directory is scanned in full on every
  operation, with each file's content read into the snapshot
  ([`internal/workspace/scan.go`](../../internal/workspace/scan.go)). And the
  three-way merge reads baseline content of its own accord: `Notebook.Pull`
  merges over `n.ws.Baseline().Tree`
  ([`internal/notebook/pull.go`](../../internal/notebook/pull.go)), so even a
  clean pull opens baseline blobs for reasons that have nothing to do with this
  feature. The property this worklog claims is narrower and exact, and it is a
  property of the **policy** rather than of the operation as a whole: the policy
  reads no blob to detect a protected change, and reads baseline content only for
  the paths a violation actually touched. Stated as an operation-level absolute
  it would be false for pull, and the fake-backed test that appeared to prove it
  would be proving only that the fake's merge is scripted.
- `NormalizeReadOnly` drops an entry covered by another entry in the same set
  ([`internal/git/readonly.go`](../../internal/git/readonly.go)). That collapse is
  correct within one set and must **not** be applied across the two sets, where
  coverage is the feature: a writable entry below a read-only entry is the case
  the operator is expressing.

- **The entries the operator wrote are the policy's input of record; the
  intra-set collapse is a normalization of the *output*.** It may decide what
  `ReadOnly()` and `Writable()` advertise. It may not decide the overlap
  refusal, and it may not decide resolution. Both R1-01 and R2-01 are the same
  root cause seen twice: the collapse ran above a decision that needed the
  written entries, and which way the guardrail fell then depended on which
  unrelated ancestors the operator happened to list beside them. Promoted here
  in review 2 so a fix to one decision does not leave the other behind, as the
  R1-01 fix did. The corollary the Phase 1 fix rests on: the entry accessors are
  the only form of the policy that survives the production path, which rebuilds
  it from them twice more (review 2, R2-03), so whatever normalization keeps
  must be enough to resolve with. A collapse that is lossless for resolution
  makes a rebuild a fixed point; one that is not makes the first construction
  silently load-bearing.
- `ValidatePath` rejects the empty string and a `.` segment
  ([`internal/git/path.go`](../../internal/git/path.go)), so no entry can denote
  the notebook root. "Everything except this directory" is therefore not
  expressible by listing entries, which is why a non-empty writable set flips the
  unmatched default rather than relying on a root entry.
- The black-box harness configures the set directly through `HarnessConfig`
  ([`internal/integrationtest/harness.go`](../../internal/integrationtest/harness.go)),
  as the read-only scenarios already do, so Phase 2 carries real scenarios before
  the CLI flag exists.
- A phase's Files list names the files a change **forces**, not only the ones it
  starts in. Widening a shape — a function signature, an interface, an envelope —
  breaks every double and every oracle of that shape, and those live in test
  files a production-file list never mentions. V1-03 caught this at the interface
  and P4-N1 caught it again one layer out at `app.Report` and the black-box
  envelope. Promoted here in review 1 so a later phase inherits the rule rather
  than rediscovering it.

- The known downstream consumer tolerates the additive surfaces. Its report
  decoder ignores unrecognized trailer lines and requires only the `retryable:`
  trailer, and its success parser matches the status line by pattern, so a new
  trailer and a new envelope field are invisible to a pinned client. This is the
  only bullet in this section that rests on a repository outside this one, and it
  is not pinned: no repository, path, or revision is recorded here, so a later
  reader cannot re-check it. Nothing in the design depends on it — the phases add
  fields and a trailer rather than changing an existing shape, which is safe
  whether or not the claim still holds. **Maintainer input:** record the
  consumer's repository and revision on this bullet, or strike it.

- **The inverted default unbinds the size of the protected change set from the
  operator's list.** With `--read-only-paths` alone, the number of paths
  detection reports and restore rewrites is bounded by the region the operator
  named. A non-empty writable set flips that: the bound becomes *everything
  outside* the writable region, so the number is chosen by what an agent wrote,
  not by what the operator listed. Any per-path work in `ChangedProtected`,
  `RestoreProtected`, the refusal, or a surface that renders the reported paths
  must therefore be linear in that number — a scan of the reported list per
  reported path, or per local file, is quadratic in a quantity no configuration
  bounds. Promoted here in review 3, which found three such scans (R3-01); the
  contract has no criterion that would have caught them, which is R3-03.

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
2. The existing error fields keep their meaning and presence, and the six codes
   of `internal/notebook/errors.go` stay the complete code set. The `readOnly` array keeps meaning exactly what it
   means today — the normalized read-only set — and never becomes a stand-in for
   the protected region. New fields are additive.
3. A configured process never publishes a change to a protected path: no commit
   from it adds, modifies, or deletes a file there, and a pull restores such a
   path from the accepted remote state.
4. With an empty writable set, resolution and every advertised surface are
   byte-for-byte what they are today, except for the always-present empty
   `writable` array.
5. A path named exactly by both sets refuses startup, before the native engine
   and the object store are touched. "Named exactly by both" means the entries
   the **operator wrote**: the cross-set overlap test runs over each set's
   validated, trimmed, folded raw entries, *before* the intra-set coverage
   collapse. Collapsing within a set must never destroy an entry the other set
   also names, or whether the guardrail holds comes to depend on which unrelated
   ancestors happen to be listed beside it (review 1, R1-01). The same rule binds
   **resolution**: `Protects` answers from the longest entry the operator wrote
   across both sets, not from the longest survivor of each set's own collapse,
   so adding a broader entry to one set can never make a narrower entry of that
   same set stop applying (review 2, R2-01).
6. The policy reads no blob to detect a protected change: detection compares
   tree objects only, and baseline content is read only for the paths a violation
   actually touched. This binds the policy, not the operation as a whole — the
   three-way merge in `Notebook.Pull` reads baseline content of its own accord,
   and this work neither adds to nor removes that. The observable form is a
   difference: a clean operation under a configured policy reads exactly as many
   blobs as the identical operation with no policy configured.
7. Recovery semantics are unchanged: the restore runs through `applyLocal`, and a
   failure during it is `RECOVERY_FAILURE` with the existing recovery stage.

### Shared interface between phases

Phase 1 builds this type in `internal/git`; Phase 2 is its only consumer; Phases
3 and 4 read its entry accessors. No phase may widen it without a README change.

```go
// PathPolicy answers, for one process, which notebook paths may be written and
// which changes to protected paths an operation must refuse or restore. It is
// an exported struct in internal/git, not a Go interface: the unconfigured
// policy is the zero value, and a nil interface would panic on Protects
// (review 1, R1-09).
type PathPolicy struct{ /* both entry sets, unexported */ }

// NewPolicy normalizes each set and refuses a path named by both. The overlap
// test runs over the entries the operator wrote, before either set drops its
// own covered entries. Each set is then collapsed against the other: an entry
// covered by an ancestor of its own set is dropped only where that ancestor
// decides every path it decides, so an entry the other set splits from its
// ancestor survives (review 2, R2-01).
func NewPolicy(readOnly, writable []string) (PathPolicy, error)

// Configured reports whether either set is non-empty. An unconfigured policy
// protects nothing and is the zero value.
func (p PathPolicy) Configured() bool

// Protects reports whether path may not be written, by longest-match
// resolution over both sets and the unmatched default.
func (p PathPolicy) Protects(path string) bool

// ReadOnly and Writable return copies of the normalized, sorted entries of
// each set; never nil. With the other set empty they are what they always
// were. With both sets configured the normalized set can hold an entry below
// another entry of the same set, because the collapse keeps what resolution
// needs, so a reader of these entries takes the longest match rather than
// assuming at most one matches (review 2, R2-01). They are also a lossless
// input to NewPolicy, which is what makes the three rebuilds on the
// production path safe (review 2, R2-03).
func (p PathPolicy) ReadOnly() []string
func (p PathPolicy) Writable() []string

// ChangedProtected returns, sorted, every protected path that differs between
// the local and base trees. It descends only where subtree IDs differ and
// reads no blob content.
func (p PathPolicy) ChangedProtected(repo Repository, local, base OID) ([]string, error)

// RestoreProtected returns local with each named path replaced by its base
// content, adding back a path absent from local and dropping one absent from
// base. It reads only the named paths. Callers pass the result of
// ChangedProtected, so a clean operation never calls it.
func (p PathPolicy) RestoreProtected(repo Repository, base OID, local Snapshot, changed []string) (Snapshot, error)
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
| `app.ServiceConfig.WritablePaths` service config field | nil | Phase 2 |
| `app.config.writablePaths` resolved flag field | nil | Phase 3 |
| `notebook.Config.WritablePaths` | nil | Phase 2 |
| `integrationtest.HarnessConfig.WritablePaths` | nil | Phase 2 |
| Unmatched-path resolution | writable when the writable set is empty, protected when it is non-empty | Phase 1 |
| Blob reads the policy adds when no protected path changed | none | Phase 2 |
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
   taxonomy and the additive-field rule. Test names are part of this: every one
   of the repository's test functions is underscore-free, so
   `grep -ohE '\bTest[A-Za-z0-9_]+' phase-*.md | grep '_'` must print nothing,
   and `grep -rhoE '^func Test[A-Za-z0-9_]+' ../../internal ../../cmd --include='*_test.go' | grep -c '_'`
   must print `0` for the convention still to be what it was.

## Decisions log

| ID | Date | Decision | Rationale | Replaces |
| --- | --- | --- | --- | --- |
| D1 | 2026-09-18 | **maintainer** The two sets compose by longest match, and a path named exactly by both is a configuration error. Sub-paths are the feature: `--read-only-paths notes --writable-paths notes/A` permits writing under `notes/A`. | Mutual exclusion would force an operator to enumerate every sibling directory to protect them; silent precedence would make a security-shaped flag ignorable. Exact overlap is the only genuinely ambiguous case, so it is the only error. | — |
| D2 | 2026-09-18 | **maintainer** A non-empty writable set flips the unmatched default to protected. An empty writable set leaves the default writable, exactly as today. | `ValidatePath` cannot express the notebook root, so "everything except this directory" is otherwise inexpressible: root files and new top-level directories would stay writable and the confinement would leak. Failing closed is the correct bias for a guardrail. | — |
| D3 | 2026-09-18 | **maintainer** The refusal names where the agent *may* write, and follows the existing error management rather than inventing a parallel one. | Under default-protected the protected region is nearly the whole notebook; listing it would be useless to an agent and enormous on every surface. The writable set is short and actionable. | — |
| D4 | 2026-09-18 | **author** Detection compares tree IDs and reads no blob content; baseline content is read only for the paths a violation actually touched. | `ReadTree` yields child IDs without content and a tree ID covers everything beneath it, so an unchanged subtree is provably unchanged without being opened. The alternative — inverting `ReadCovered` — would read the whole notebook on every pull and commit under the normal configuration. | — |
| D5 | 2026-09-18 | **author** The property in D4 is asserted as a counted-call invariant against a fake repository, not as a benchmark with a threshold. | A counting fake proves "the policy read no blob" exactly and deterministically under `-race -count=3`, and the configured-against-unconfigured difference keeps the assertion honest about reads the operation makes for its own reasons; a wall-clock threshold would be machine-dependent and would need a parameter no test could reliably trigger. | — |
| D6 | 2026-09-18 | **maintainer, author** A stat-cache index over the visible directory is considered and deferred to its own effort. | It would cut the pre-existing full scan of the visible directory, which compounds with this feature, but it is a different risk class: it fails open and silently where this feature fails closed and loudly, it belongs in the private root rather than the shared pack cache because it is mutable per-workspace state rather than immutable content-addressed data, it inherits the racy-index problem under many agents sharing one directory, and it would migrate the strict versioned `state.json`. No measurement yet shows the local scan is material against the object-store round trips. | — |
| D7 | 2026-09-18 | **author** The `readOnly` envelope array keeps its present meaning and a parallel `writable` array is added, always present and empty when unconfigured. | `AGENTS.md` pins `readOnly` as a stable, additive, always-present field carrying the read-only set. Overloading it with a protected region that is mostly the whole notebook would change a documented field's meaning and break the one known consumer's expectations. | — |

### Review 1 — 2026-09-19 — Claude Opus 5 (1M context), [[worklog-review]]

**Verdict: not ready.** The effort ships clean through the gates and the
enforcement is correct on every branch traced, but one invariant is broken in the
construction path and one phase's evidence cannot be reproduced from its own
record. Green gates are not the verdict: R1-01 is a configuration a green suite
accepts and an operator would not.

**Gates re-run independently, not taken from the notes**

| Command | Result |
| --- | --- |
| `make qa` (run 1) | exit 2 — `FAIL github.com/baalimago/slivingdoc 30.242s`, `panic: test timed out after 30s` in `TestReleaseBinary`'s in-suite `go build`; lint clean beforehand |
| `make qa` (run 2) | exit 2 — same package, `30.475s`, same cause |
| `go test . -race -count=3 -timeout=30s` alone | `ok … 3.165s` — the root package is not slow, it is starved |
| `make test` (run 3, idle machine) | exit 0 — 18 `ok` packages, 0 `FAIL`, `== coverage: 84.5% (floor 70%) ==`, `internal/integrationtest` 14.9 s |
| `make lint` (twice, inside `make qa`) | clean: gofumpt listed no file, `go vet` clean, staticcheck v0.7.0 clean, `go fix -diff` silent |
| `make npm-test` | `tests 35, pass 35, fail 0, skipped 0` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 2 clone groups` — exactly the two Phase 5 records as accepted; `internal/git` reports none |
| `git diff --stat Makefile` | empty |
| `go tool cover -func=.build/cover.out` | total 84.5 %; every exported function of `internal/git/policy.go` at 100 % |
| Phase 5 evidence check (backtick-delimited form) | **8 `MISSING` lines, 132 distinct names** — see R1-03 |
| Phase 5 acceptance sweep | Phase 1's header row only, as recorded |
| Phase 5 skip scan | `internal/git/policy_test.go:763,765` only, as recorded |
| Readiness checklist line 7, both halves | clean on both sides |

**Cross-cutting observations**

- The three no-blob oracles are the strongest thing in this effort. The
  difference form — same fixture, same operation, entry sets the only variable —
  is true of the real engine and not just of the fake, and `countingRepo` counts
  across every handle the engine hands out, so a policy blob read could not
  escape. Invariant 6 is held, and it is held for a reason a later reader can
  re-derive.
- Invariant 3 was traced through the commit conflict branch, the no-op branch,
  every CAS retry, the pull conflict branch, the recovery entry and the
  generation-0 empty-baseline case. It holds on all of them, and it holds for the
  same structural reason each time: at a protected path the local side equals the
  merge base, so the merge takes the remote side. That argument, not the tests,
  is what makes the invariant safe under future change, and it deserves to be
  written down where the next contributor will find it.
- R1-01 is the shape of defect this review exists to find: correct lines on a
  path nobody enumerated. Every overlap test in the suite uses a bare two-entry
  configuration, so the interaction between the intra-set collapse and the
  cross-set test was never expressed. It was found by asking what
  `NormalizeEntries` destroys before `NewPolicy` looks, which the README's own
  Strategy section had already warned about in the abstract.
- The nine author notes are all real; none is noise. P4-N1 is the most valuable
  and has been promoted as a planning rule rather than a Phase 4 fact. P1-N1,
  P2-N1, P2-N2, P3-N1, P4-N2, P5-N2 and P5-N3 are each a small, closable
  correction, and each is carried into the phase's Review findings. P3-N2 is the
  weakest: the row reads stricter than the prose, and tightening the row is the
  right fix rather than threading provenance through resolution.
- P5-N1 is real but understates its own case. It names three over-matching
  patterns; it does not notice that the refined command it recommends is now
  defeated by the very table that recommends it. That is R1-03.
- V1-13 remains open and correctly scoped: the downstream-consumer bullet is
  unpinned, nothing in the design depends on it, and Phase 4's shape argument
  stands without it.

### Review 2 — 2026-09-19 — Claude Opus 5 (1M context), [[worklog-review]]

**Verdict: not ready.** Every gate is green, every round-1 blocker and defect
is closed as filed, and the effort is still one silent guardrail hole away from
doing what it promises. R2-01 is R1-01's other half: the fix moved the overlap
test above the intra-set collapse and left resolution below it, so the class of
defect survived the fix that named it.

**Gates re-run independently, not taken from the notes**

| Command | Result |
| --- | --- |
| `make qa` | exit 0 on the first attempt |
| `make lint` (inside `make qa`) | gofumpt listed no file, `go vet` clean, staticcheck v0.7.0 clean, `go fix -diff` silent |
| `make test` (inside `make qa`) | 18 `ok` packages, 0 `FAIL`, `== coverage: 84.5% (floor 70%) ==`; `internal/integrationtest` 18.2 s |
| `make npm-test` (inside `make qa`) | `tests 35, pass 35, fail 0, skipped 0` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 2 clone groups` — exactly the two Phase 5 records, neither in `internal/git` |
| `git diff --stat Makefile` | empty |
| Phase 5 acceptance gate (specification halves) | empty; 109 distinct names |
| Phase 5 completeness sweep (record halves) | empty; 130 distinct names |
| Phase 5 acceptance sweep | empty |
| Phase 5 skip scan, effort files | empty |
| Phase 5 skip scan, widened | 20 hits, all in files this effort does not touch |
| Readiness checklist lines 1 and 7 | clean; the repository-side underscore count prints `0` |
| Throwaway probe in package `git` (`Protects`, `ChangedProtected` under a three-level composition), removed afterwards | reproduced R2-01 |

**Cross-cutting observations**

- **A fix that closes a finding is not the same as a fix that closes its class.**
  R1-01 was filed as "the overlap test runs after the collapse" and was fixed
  exactly there. The sentence that actually described the fault — the collapse
  runs above a decision that needs the written entries — applies verbatim to
  `Protects`, which nobody re-read after the split landed. The rule is now in
  Strategy so the next fix inherits it rather than rediscovering it.
- **The uniqueness oracle cannot fail on this class by construction.**
  `TestPolicyProtectsLongestMatchIsUnique` brute-forces over `p.ReadOnly()` and
  `p.Writable()` — the collapsed accessors — so it compares the policy's lookup
  against a second view of the same already-lossy data. An oracle that reads its
  subject's own output is a tautology wherever the loss happens before the
  output. This is worth remembering beyond this effort.
- **Every composition test in the suite is two-level.** Phase 1's resolution
  table, Phase 2's three `TestScenarioWritableComposed*` scenarios, Phase 3's
  flag rows and Phase 4's both-sets advertisement test all configure exactly one
  entry per set. Three-level nesting is not exotic — it is the second thing an
  operator tries after the two-level example in `docs/running.md` — and it is the
  shape both new findings live in.
- **The advertisement was never read under the configuration the feature is
  for.** R2-02 is not a subtle bug; it is two adjacent sentences that say
  opposite things, visible the moment a nested pair is configured. It survived
  because the only both-sets test uses disjoint entries, which is the one
  arrangement where the wording happens to be coherent.
- **Phase 5's rebuilt evidence check is the strongest thing added since review
  1.** It reproduces exactly, its counts match its record, and it survived this
  review's own findings being appended to four Review-findings sections — which
  is the specific failure mode R1-03 described.
- **R1-02's record is adequate.** The precondition sits where the gate is
  specified, cites the `Makefile`'s own comment rather than restating a number,
  forbids the wrong fix by name, and moved no gate setting. R2-04 asks only for
  the discriminator that turns the excuse into a test.
- **V1-13 remains open and still touches nothing.** Phase 4's shape argument
  stands without it.

**Consolidation.** Both findings are local to the phase that owns them — the
code fix for R2-01 is one function in `internal/git`, its boundary scenario is
one file in `internal/integrationtest`, and R2-02 is `internal/mcp/server.go`
with one test — so the three phases are reopened rather than gathered into an
addendum. R2-03 constrains the R2-01 fix and should be read with it.

### Review 3 — 2026-09-19 — Claude Opus 5 (1M context), [[worklog-review]]

**Verdict: ship.** Run after the holistic pass, against the same tree — nothing
in `internal`, `cmd` or `docs` has moved since it, and `git status --porcelain`
matches what that review recorded. Every gate is green on the first attempt this
time, all four of Phase 5's evidence checks reproduce **exactly**, and the
resolution core is still correct. Three findings, none a blocker, none reopening
a phase: one is a real and measured cost cliff that the feature's own default
makes reachable (R3-01), one is a caller-facing sentence left over from the
read-only implementation (R3-02), and one is the contract gap that let the first
through (R3-03).

**Gates re-run independently, not taken from the notes**

| Command | Result |
| --- | --- |
| `make qa` (single attempt) | **exit 0** — no root-package timeout this run |
| `make lint` (inside `make qa`) | gofumpt listed no file, `go vet` clean, staticcheck v0.7.0 clean, `go fix -diff` silent |
| `make test` (inside `make qa`) | 18 `ok` packages, 0 `FAIL`, `== coverage: 84.6% (floor 70%) ==`; root package 4.630 s, `internal/integrationtest` 23.140 s |
| `make npm-test` (inside `make qa`) | `tests 35, pass 35, fail 0, skipped 0, duration_ms 7276` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 1 clone groups` — `internal/app/command_test.go:424,444 ↔ :447,467`, the pre-existing table-driven pair, matching Phase 5's final sweep |
| `git diff --stat Makefile` | empty — no gate setting moved |
| Phase 5 acceptance gate (specification halves) | empty; **124** distinct names, exactly the figure of record |
| Phase 5 completeness sweep (record halves) | empty; **147** distinct names, exactly the figure of record — **148** after this review's own findings were appended, the one added name being `TestScenarioReadOnlyAddAndDeleteRefused`, which resolves |
| Phase 5 acceptance sweep | empty |
| Phase 5 skip scan, effort files | empty |
| Phase 5 skip scan, widened | 20 hits across the same six files, none in the modified or untracked set |
| Phase 5 maintenance contracts, all five greps re-run | Every line number of record reproduces: `346:### Writable paths`; `writable-paths` at 1501, 1574, 1877 in the contract; 110, 239, 248, 255, 277, 302 in the operator guide; 263, 365 in the agent guide |
| Readiness checklist line 7, both halves | clean; the repository-side underscore count prints `0` |
| Readiness checklist line 1, re-run after this review's edits | 26 hits, up from 15: the five this review's cost table adds sit at `phase-1-path-policy.md:721-725`, below that file's `## Implementation notes` line, so no specification prose carries a numeral with a unit |
| Throwaway scaling probe in package `git` (`ChangedProtected` and `RestoreProtected` at N = 500…8 000, both nesting directions), removed afterwards | reproduced R3-01 |
| Throwaway trailing-slash probe (`a//` through `validateEntries`) | no hole: `ValidatePath` refuses a trailing slash, so the single trim cannot produce a never-matching entry |

**Cross-cutting observations**

- **The worklog's record is reproducible to the digit, and that is unusual.**
  Phase 5's four checks, its distinct-name counts and every one of its cited
  document line numbers were re-derived from scratch and all matched. The
  specification/record split R1-03 forced is now carrying three fix passes plus
  a holistic review without drifting. This is the part of the effort I would
  hold up as the model.
- **The correctness question is closed.** Three rounds plus a holistic
  enumeration have pushed on the R1-01/R2-01 class from every angle, and this
  review found no fourth instance by re-reading `collapseEntries`, `covering`
  and `Protects` against all four construction sites. `appendUnique` turned out
  to be load-bearing rather than defensive — the modify branch collects a path
  from both sides — which is worth knowing before anyone "cleans it up".
- **The cost question is not closed, and nobody asked it.** Every review so far
  has asked what the policy *reads* (invariant 6, and it holds, non-vacuously).
  None asked what the policy *does with what it read*. R3-01 is three quadratic
  scans over the reported-path list, measured at roughly four times the cost per
  doubling, and the reason it matters is structural rather than incidental: D2's
  inversion is exactly the decision that unbinds the size of that list from the
  operator's configuration. That rule is now in Strategy.
- **The read-only vocabulary is still leaking into a writable-only process.**
  R2-02 caught it in the advertisement and Phase 2 caught it in the refusal;
  R3-02 is the same leak in the three engine-failure diagnostics, which no test
  reads and no surface table lists. The pattern across R2-02, R1-06 and R3-02 is
  that the surfaces someone enumerated were fixed and the ones nobody enumerated
  were not — which argues for a surfaces list that includes error text, not only
  advertisement text.
- **H-01 through H-09 are all still open and all still accurate.** Re-checked by
  reading rather than assumed: the six `EntrySet` methods still have no
  production caller (H-02), `n.readOnly`'s only reader is still
  `internal/notebook/commit.go:444` (H-03), `AGENTS.md` still never mentions
  `policy.go` (H-05), and `// Colour is` still sits alone mid-sentence in
  `Report`'s doc comment (H-06). H-07 did not reproduce this run, which moves
  the tally to three failures against four passes and changes nothing about the
  judgement — a margin that depends on machine load is still not a margin.
- **V1-13 remains open and still touches nothing.** No phase depends on it.

**Consolidation.** R3-01 is one file in `internal/git` and R3-02 is two files in
`internal/notebook`; both are local to the phase that owns them, so if the
maintainer takes them they reopen nothing and need no addendum phase. R3-03 is a
README change that should be made with R3-01, not after it.

## Holistic review

### 2026-09-19 — Claude Opus 5 (1M context), whole-feature pass

**Verdict: ship.** The resolution semantics are correct — not correct-by-argument,
correct under exhaustive enumeration. The feature holds together across seven
packages and three documents, the honesty of the worklog's load-bearing claims
checks out at the real boundary, and the design is one a maintainer of this
repository would recognize. Nine findings follow. None is a blocker, none
reopens a phase, and the one defect of substance (H-01) reproduces identically
with `--read-only-paths` alone on commit `8047406`, so it is inherited rather
than introduced.

The third instance of the R1-01/R2-01 class was the thing to look for. It is not
there. That is the review's main result, and it was established by enumeration
rather than by reading.

#### What was verified independently

Correctness probes were throwaway tests written into the packages, run, and
deleted; the working tree is as it was found.

| Probe | Method | Outcome |
| --- | --- | --- |
| Collapse preserves written resolution | Every assignment of a 9-path universe (`a`, `a/b`, `a/b/c`, `a/b/c/d`, `a/b/c/d/e`, `a/x`, `a/b/x`, `a/b/c/x`, `z`) to {unset, read-only, writable} — **2 187 policies × 20 probe paths** — against a reference that resolves longest match over the entries *as written* | **No mismatch.** Four- and five-level nestings, both directions, included |
| Fixed point of the accessors | The same 2 187 policies, each rebuilt from `ReadOnly()`/`Writable()` **three times** (the production rebuild count), compared against the original on every probe path | **No mismatch on any round.** The claim in `PathPolicy.ReadOnly`'s doc comment and in Strategy holds |
| Overlap detection is complete | 3⁸ = 6 561 assignments over spellings that differ by case and by a trailing slash (`a`, `A`, `a/`, `a/b`, `A/B`, `a/b/`, `a/b/c`, `z`), asserting `NewPolicy` refuses **exactly** the ambiguous compositions and resolves the rest as written | **Exact.** No false refusal, no missed ambiguity |
| `ChangedProtected` against a full-read reference | 3 000 random policies × random local/base snapshots over a 10-file pool, `ChangedProtected` vs. reading both snapshots whole and filtering by `Protects` | **Identical on every iteration** |
| The restore is a fixed point | 3 000 random policies: restore the reported paths, rebuild the tree, re-detect; also assert no *unprotected* path changed | **One failure class**, H-01 below; every other iteration clean |
| Case folding | `ẞ`→`ss`, `İ`→`i̇`, `ſ`→`s`, `ﬁ`→`fi`: fold distributes over `/` for all 49 pairs; a fold-expanding entry resolves by *folded* length on both sides of a nesting; non-boundary prefixes (`ab` vs `abc`, `ab.md`) never match | **Sound.** The folded-length comparison in `Protects` is the right comparison |
| Invariant 6 is non-vacuous | Instrumented `policyAccess` to print the counts it compares | Clean commit: **8 blob reads configured, 8 unconfigured**. Clean pull: **8 and 8**. A commit that *does* change a protected path reads **3**. The oracle is live, not two zeros |
| Invariant 4 at the real boundary | The pre-existing read-only expectations are untouched by the diff and still pass: `internal/integrationtest/scenario_cli_test.go:380,395,410`, `scenario_colour_linux_test.go:60,85`, `scenario_transport_test.go:141` | **Held**, and held by oracles written before this feature — stronger than `TestAdvertisementReadOnlyWordingUnchanged`, which compares against constants typed in the same commit |

Gates, re-run rather than taken from the record:

| Command | Result |
| --- | --- |
| `make qa` (run 1) | **exit 2** — `FAIL github.com/baalimago/slivingdoc 30.385s`, `panic: test timed out after 30s`, `running tests: TestReleaseBinary (30s)`. Lint clean beforehand. See H-07 |
| `go test . -race -count=3 -timeout=30s` (the R2-04 discriminator) | `ok … 3.164s` — scheduling artefact, not a regression, exactly as Phase 5 prescribes |
| `make qa` (run 2, idle machine) | **exit 0**; gofumpt listed no file, `go vet` clean, staticcheck v0.7.0 clean, `go fix -diff` silent; `== coverage: 84.6% (floor 70%) ==`; npm `tests 35, pass 35, fail 0`; root package 9.943 s |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` (twice) | `Found total 1 clone groups`, stable — `internal/app/command_test.go:424,444 ↔ :447,467`, the pre-existing table-driven pair. Matches Phase 5's final sweep, not the older two-group tables |
| Real-engine probe (`internal/git2`, libgit2 v1.9.6) | Used to establish H-01's real behaviour rather than the fake's |

#### Findings

| ID | Severity | Where | Finding |
| --- | --- | --- | --- |
| H-01 | defect (inherited, not introduced) | `internal/git/policy.go:212` `RestoreProtected`, `:294` `namedOrUnder`; `internal/git/path.go:133` `ValidateSnapshot`; `internal/git/tree.go:28` `buildTree` | A **writable file standing where the baseline holds a protected directory** silently loses the writable file. `RestoreProtected` returns `local` minus `namedOrUnder(changed)` plus the restored blobs; when the local blob at `X` is *writable* and the baseline holds protected content under `X/…`, neither is dropped, so the snapshot carries a blob at `X` **and** a blob under `X/…`. `ValidateSnapshot` accepts it — it checks fold collisions, not blob/tree clashes — and `buildTree` emits two tree entries named `X`, one blob and one tree, which libgit2's treebuilder resolves by last-insert-wins. Reproduced end to end: `--read-only-paths notes/agent-a/locked/secret.md --writable-paths notes/agent-a`, agent replaces the directory `notes/agent-a/locked` with a file. Outcome: commit correctly refused `READ_ONLY_PATH` naming `secret.md`, protected content correctly restored, generation unchanged — **invariant 3 holds** — but the agent's writable file is gone and nothing in the refusal says so. `docs/slivingdoc-v1.md:318` documents only the *mirror* direction (local directory over accepted file), which is a loud `RECOVERY_FAILURE`; `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob` pins only that mirror. **The same script with no writable set and `--read-only-paths notes/agent-a/locked/secret.md` behaves identically**, so this predates the effort. **This is R1-05's second direction.** R1-05 found the drop, Phase 2 closed it by stating and pinning the direction it found, and the mirror went with it unexamined — the same "a fix that closes a finding is not the same as a fix that closes its class" that review 2 wrote down for R2-01. Fix belongs with the maintainer: state the second direction beside the first in `docs/slivingdoc-v1.md` section 11.1, add the scenario row beside `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob`, and consider having `ValidateSnapshot` reject a path that is a segment-boundary prefix of another, which would turn a silent loss into a loud `INVALID_CONTENT` |
| H-02 | defect | `internal/git/readonly.go:139`, `:169`, `:202`, `:224`, `:242`, `:275` | **Dead production code.** `EntrySet.Covers`, `ChangedUnder`, `Pin`, `ReadCovered`, `walkCovered` and `hasEntryBelow` have no production caller once Phase 2 moved detection and restore onto `PathPolicy`. Roughly 110 of `readonly.go`'s 280 lines, kept alive by `internal/git/readonly_test.go:62-265` (`TestReadOnlyCovers`, `TestReadOnlyChangedUnder`, `TestReadOnlyChangedUnderEmptySet`, `TestReadOnlyPin`, `TestReadOnlyReadCovered`) and a `countingRepo` double that exists only for them — and they count toward the 84.6 % figure. Staticcheck cannot see it: the methods are exported. No phase records a decision to keep them |
| H-03 | defect | `internal/notebook/notebook.go:139` and `:168`; read at `internal/notebook/commit.go:440` | **Dead divergence.** `Notebook` carries a second, independently normalized `readOnly git.EntrySet` beside its `policy`. Its only reader is `refusalMessage`, which reaches it **only when `policy.Writable()` is empty** — and in that branch `NormalizeEntries(cfg.ReadOnlyPaths)` is `collapseEntries(ro, nil)`, provably identical to `policy.ReadOnly()`. The field's doc comment carefully explains a divergence ("it can hold a broader entry than the policy resolves over") that no caller can observe. This is the abstraction growing wrong under repair: a correct explanation attached to unreachable state |
| H-04 | note | `internal/app/config.go:286` `resolvePolicy`; `internal/git/readonly.go:35` `NormalizeEntries` | `resolvePolicy` validates the read-only entries twice — once through `NormalizeEntries` purely to obtain a `"read-only paths: "` prefix, then again inside `NewPolicy`, which already names the kind. A bad writable entry therefore reads `writable paths: invalid writable path "…"`. `NormalizeEntries` is an exported name that hard-codes `kindReadOnly` in its error text; a caller who passes writable entries to it gets a wrong diagnostic, and nothing in the signature warns them |
| H-05 | note | `internal/git/readonly.go` (whole file); `internal/git/readonly_test.go:63,117,165,207`; `AGENTS.md` package map, `internal/git` entry | **Naming drift.** The file named `readonly.go` now holds the kind-neutral `EntrySet`, the `entryKind` enum and the cross-set collapse; its tests still carry `TestReadOnly…` names for a type that is no longer read-only-specific. `AGENTS.md`'s package map for `internal/git` lists "path and content validation" but never mentions `policy.go` or the path policy, although the same file's invariant list was updated for the feature |
| H-06 | note | `internal/app/command.go:131`; `cmd/pull/pull.go:96`; `cmd/commit/commit.go:115`; `docs/running.md:69` | **Rewrap seams from the R2-02 pass.** `// Colour is` sits alone on a line mid-sentence in `Report`'s doc comment. Both `helpText` blocks — text a user reads from `-h` — leave `and exits zero. A domain` and `retryable verdict, and the same trailers when configured, and` as ragged short lines. `docs/running.md:69` is 103 columns in an 80-column document. Cosmetic, but two of the four are user-facing output |
| H-07 | defect (pre-existing, CI) | `release_test.go:442` `TestReleaseBinary`; `Makefile:86` | **`make qa` is not reliably green.** It failed on my first attempt with the root package at 30.385 s and `TestReleaseBinary` holding the `sync.OnceValues` build. Across three reviews the tally is now three failures (review 1 ×2, this review ×1) against three passes (review 2, Phase 5's sweep at 29.172 s of 30 s, this review's run 2). The discriminator confirms the cause every time — 3.164 s for the package alone. This is not the feature's defect and the feature's thirteen new tests only lengthen the window, but "29.172 s of a 30 s budget" is not a margin, it is a coin flip on a loaded CI runner. It deserves a maintainer item of its own: move the in-suite `go build` behind a shared `sync.Once` primed before the parallel packages start, give the root package its own `go test` invocation, or raise the budget for that one package. Note that `AGENTS.md` forbids changing the timeout, so this is a maintainer decision, not an executing agent's |
| H-08 | note (design) | `internal/app/config.go:266` `finish`; `internal/app/service.go:104`; `internal/notebook/notebook.go:164` | **`PathPolicy` earns its place, but the process never carries it.** `config.finish` builds a policy, takes its two entry slices, and discards it; `Service` rebuilds; each `Notebook` rebuilds. Four constructions at startup. It is provably safe — the fixed-point probe above covers all 2 187 configurations at three rebuild depths — but the property had to be *proved* only because the shape throws the value away. Carrying the `PathPolicy` in `config` and reading the entry arrays off it would have made the proof unnecessary. R2-03 chose to keep the rebuilds and document them, which is a defensible call for a review pass; it is the wrong steady state. Not a ship blocker, and worth a follow-up |
| H-09 | note (invariant set) | README, "Non-negotiable invariants" | **The seven invariants have one gap, and H-01 is standing in it.** Invariant 3 binds *publication* — nothing protected reaches the remote. Nothing binds the **completeness of the local restore**: that after a refusal or a pull, no protected path differs from the baseline, and no *unprotected* local file is destroyed except where one path cannot hold two shapes. That property is asserted only as prose in `docs/slivingdoc-v1.md` and as individual scenario assertions, which is exactly why the second type-clash direction went unnoticed while the first was found, documented and pinned. An eighth invariant, stated as the pair "every reported path matches the baseline afterwards, and every unreported path is untouched", would have forced both directions into the acceptance table |

#### Judgement on the three questions per the mandate

**Is it correct?** Yes, and now demonstrably so. `entryBetween` is the right
predicate, and the reason is worth writing down where the next contributor will
find it: for a path `p` whose longest own-set cover is the dropped entry `it`,
the decision can only flip if the other set's cover `t` satisfies
`|anc| < |t| < |it|`; since `t`, `anc` and `it` are all ancestors-or-equal of `p`
they form a chain, so that condition is exactly "an other-set entry lies strictly
between", and `t == anc` or `t == it` is the overlap `NewPolicy` refuses. Chained
drops compose for the same reason. `covering` returning the longest match closes
the other half. Equal folded lengths across the two sets are impossible without
being the same folded string, which is refused — so `Protects` has no tie case to
get wrong. The enumeration confirms all of it.

**Does it hold together?** Mostly. The documents are the strongest part: section
2 "Writable paths", section 17, `docs/running.md`'s four-level example and
`AGENTS.md`'s invariant all say the same thing in four registers, and none
contradicts the code. The seams are in `internal/git` and `internal/notebook`,
where two rounds of repair left behind the machinery they replaced (H-02), a
second copy of state that can no longer diverge (H-03), a file name and a test
prefix that outlived their type (H-05), and four ragged rewraps (H-06). None
affects behaviour; together they are about a half-hour of cleanup that would
leave the next reader a much clearer `internal/git`.

**Is it the right design?** Yes. `PathPolicy` belongs in `internal/git`: it needs
`Repository` to walk trees, and `internal/git` owns that seam. The tree-hash
detection is the right call and D4's reasoning survives scrutiny. D7's refusal to
overload `readOnly` is right and is the kind of restraint that keeps an API
stable. D2's fail-closed default is right. The advertisement carries the correct
amount — the actionable list, writable first, plus the one rule that reconciles
them — and the R2-02 fix is a genuine improvement, not a patch. The three
rebuilds are the one shape I would push back on (H-08), and the invariant set is
one item short (H-09).

**Is it honest?** Yes, on every claim I could reach. Invariant 6 is real,
non-vacuous, and measured as a difference so the fake cannot flatter it.
Invariant 4 is proved by oracles the effort did not write. Phase 5's final gate
sweep records `Found total 1 clone groups` and explains *why* a clone stopped
matching rather than claiming credit for it — that is the section I trusted most.
The worklog's self-criticism in review 2 ("an oracle that reads its subject's own
output is a tautology") is accurate and generalizes.

#### Recommendation

**Ship.** V1-13 stays open for the maintainer as designed. H-01, H-07 and H-09
should become maintainer items rather than blocking this merge: H-01 and H-07
both predate the effort, and H-09 is a hardening of the contract rather than a
correction to it. H-02 through H-06 and H-08 are cleanups that would be better
done now, while the reasons are still in view, than left for a later reader —
but none of them is a reason to hold the merge.

**No phase is reopened.** H-02, H-03 and H-05 touch Phase 1's and Phase 2's
files; H-04 touches Phase 3's; H-06 touches Phase 4's; H-07 touches Phase 5's
subject. All are below the bar the severity taxonomy sets for reopening: no
invariant is broken and no phase ships behaviour its acceptance table does not
cover. H-01 is behaviour the accepted contract describes in one direction and not
the other, and it is behaviour of commit `8047406`, not of this diff.

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
4. An operation in which no protected path changed reads no blob on the policy's
   account: the same operation with no policy configured reads exactly as many.
   Proven by the Phase 2 counted-call invariant, which measures the difference
   the policy makes rather than constraining the merge's own baseline reads.
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

One entry per validation or review round, mapping each finding ID to the edit or
decision that closed it.

### Validation round 1 — 2026-09-18 — verdict: not ready (5 major)

| ID | Severity | Where | Resolution |
| --- | --- | --- | --- |
| V1-01 | major | README invariant 6, D4, D5, success 4, parameters row; Phase 2 counted-call rows | Closed. The no-blob property was stated as an operation-level absolute and is false for pull, whose three-way merge takes the baseline tree as its base. Rewritten throughout as a property of the policy, with the observable form a difference: a clean operation under a configured policy reads exactly as many blobs as the identical operation with no policy. Phase 2's two tests renamed to `TestCommitCleanCommitPolicyAddsNoBlobRead` and `TestPullCleanPullPolicyAddsNoBlobRead` and their mechanism column rewritten. |
| V1-02 | major | Phase 1 rename reference list and Files | Closed. `internal/notebook/commit.go` holds `readOnlyRefusal` and `violatedEntries`, which take the set by type; omitting it broke the build at the end of Phase 1. Added to both lists, scoped to the type name only. |
| V1-03 | major | Phase 4 Files; Phase 2 accessors | Closed. Widening the MCP service interface breaks the three fakes in `internal/app/app_test.go`; that file is now in Phase 4's Files. The `Runtime` accessor wrapper and `internal/app/readonly_test.go` are now explicitly Phase 2's, so the accessor exists on every type Phase 4 reads. |
| V1-04 | major | Phase 2 integration contract; two acceptance rows | Closed. Every scenario row now carries a test name, and the acceptance rows that pointed at "the root and new-directory scenarios" name them. Two acceptance rows added for the composition rule (D1) and the unconfigured process. Phase 3's and Phase 4's integration tables were unnamed for the same reason and were given the same treatment. |
| V1-05 | major | Phase 1 counting fake; Phase 5 expected clones | Closed. `internal/git/fake_test.go` already declares `fakeRepository` in package `git` with `treeReads` and `presenceChecks` counters; a second one would redeclare the name. Phase 1 now extends it with a `blobReads` field, and Phase 5's expected-clone row for it is deleted, since the fake-mirroring clause covers independent packages and not two fakes in one. |
| V1-06 | minor | README parameters table | Closed. `app.Config.WritablePaths` renamed to `app.ServiceConfig.WritablePaths`, the type that exists; a row added for `app.config.writablePaths`, the resolved flag field, owned by Phase 3. |
| V1-07 | minor | README invariant 2 | Closed. Six codes, not seven, and the file that declares them is named. |
| V1-08 | minor | Phase 4 Files | Closed. `internal/mcp/error.go` does not exist; corrected to `errors.go`, with a note that the new field must be set at each construction site that assigns the empty array today. |
| V1-09 | minor | Phase 3, 4, 5 acceptance rows; Phase 5 maintenance contracts | Closed. Every "reviewer check" replaced by a command. Phase 5 gained an acceptance sweep that prints any acceptance row naming neither a test nor a command, as the companion to its evidence check. |
| V1-10 | minor | Phase 1 retained `EntrySet` methods | Closed. A disposition table says which methods keep a production caller after Phase 2 and which are retained without one, and records that deleting them is not this worklog's decision. |
| V1-11 | note | Phase 1 detection and restore tables | Closed. A row on each table for a protected path that is a blob on one side and a tree on the other. |
| V1-12 | note | Phase 2 refusal | Closed. The file-entry list is stated as deliberately unbounded, with the reason. |
| V1-13 | note | README consumer evidence; Phase 4 | Open — **maintainer input**. The bullet now says the claim is unpinned and that nothing depends on it. Record the consumer's repository and revision, or strike the bullet. |
| V1-14 | note | README strategy, first bullet | Closed. Scoped to enforcement; the advertisement readers are named. |
| V1-15 | major | Every cited test name | Closed. Raised during the fix pass rather than in the round-1 report: all 555 test functions in `internal` and `cmd` are underscore-free, and all 81 cited names used underscores. All renamed — `TestIntegration_Writable_*` to `TestScenarioWritable*`, matching `TestScenarioReadOnly*` in the file Phase 2 creates, and the rest underscore-stripped. Checklist line 7 now carries the command that catches this. |

### Review round 1 — 2026-09-19 — verdict: not ready (1 blocker, 1 defect, 7 notes)

Phases reopened: 1, 3 (R1-01) and 5 (R1-03). Phases 2 and 4 keep `Complete`; the
notes against them do not block. All three reopened phases are back to
`Complete`. R1-05, R1-06 and R1-08 stay open as notes against Phases 2 and 4, and
V1-13 stays open for the maintainer; none of them blocks.

| ID | Severity | Where | Summary | Resolution |
| --- | --- | --- | --- | --- |
| R1-01 | blocker | [Phase 1](phase-1-path-policy.md), [Phase 3](phase-3-flag-and-config.md); `internal/git/policy.go:38-53`, `internal/git/readonly.go:39-83` | The cross-set overlap test runs after intra-set coverage collapse, so `--read-only-paths=docs,docs/open --writable-paths=docs/open` starts without a refusal and makes `docs/open` writable. Breaks invariant 5 and D1. | Fixed in Phase 1 — `NewPolicy` tests the overlap over the entries the operator wrote and collapses each set only afterwards (`validateEntries` / `collapseEntries` in `internal/git/readonly.go`), pinned by `TestNewPolicyRejectsCollapsedOverlap` in both nesting directions, case-folded and with a trailing slash; `docs/slivingdoc-v1.md` states the rule. Closed in Phase 3 too: its startup-refusal and error-coverage tables carry the row, and `TestSetupRefusesBeforeEngineAndProbe` proves the flag-level refusal precedes the engine and the store in both nesting directions, with a third row pinning that a several-overlap refusal names the first pair the operator wrote. |
| R1-02 | note | [Phase 5](phase-5-quality-gate.md) | `make qa` is load-sensitive: two runs failed on the root package's in-suite release build hitting the 30 s per-package budget; a third, idle-machine run passed clean. The record should state the precondition. | Closed. Phase 5's "The gates" section now states that the run of record is taken on an idle machine and why, citing the `Makefile`'s own `$(BIN)` comment rather than restating its numbers, and rules that a timeout there is never answered by touching the timeout, the count, or the race flag. No gate setting moved: `git diff --stat Makefile` is empty. |
| R1-03 | defect | [Phase 5](phase-5-quality-gate.md) | The evidence check is self-invalidating — Phase 5's own adjudication table backticks the eight over-matching tokens — so the recommended command now prints eight `MISSING` lines and the distinct-name count is 132, not the recorded 122. | Closed. A phase file's specification and its record are now read by separate commands: an acceptance gate over the specification halves with no exclusion at all (109 names), and a prefix-tolerant completeness sweep over the record halves with one stated exclusion, the bare word Tests (130 names, 134 in union). The acceptance sweep drops header rows by matching `Proven by`, and the skip scan escapes the `.`. All four print nothing, re-derived against the tree after Phases 1 and 3's fixes. P5-N1, P5-N2 and P5-N3 closed with it. |
| R1-04 | note | [Phase 3](phase-3-flag-and-config.md); `docs/running.md:248-254` | The composed `--read-only-paths docs --writable-paths docs/drafts` example does not say that the non-empty writable set also protects everything outside `docs/drafts`. | Closed. The operator guide now states the inversion under the example — `notes/`, notebook-root files and directories that do not exist yet are read-only for that process too — and carries the written-entry rule for the overlap refusal beside it. |
| R1-05 | note | [Phase 2](phase-2-enforcement.md); `internal/git/policy.go:206-216` | "The agent's edits to writable paths stay on disk after a refusal" has an unstated, untested exception: a baseline blob standing where the visible tree holds a directory drops every local file beneath it, writable ones included. | Closed in Phase 2's review-2 pass. The Commit invariant row now states the type change as the one structural exception and says what the reset does there, and `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob` pins the outcome at the black-box boundary in both configurations that reach it — a writable entry below the restored file, and a read-only entry above it. |
| R1-06 | note | [Phase 4](phase-4-advertisement.md); `internal/mcp/server.go:280-308` | The MCP error text item carries only the `read-only:` trailer, so a text-only host never sees the writable set on a domain error other than the refusal. This is P4-N2. | Closed in Phase 4's review-2 pass, by implementing the surface rather than recording the decision: under a nested composition a lone `read-only:` trailer misleads by omission about the one directory the agent uses, which is R2-02 on a surface R2-02 does not name. `errorText` now emits the writable trailer above the read-only one and the rule line below both, mirroring the report; the surfaces table carries the row, and `TestErrorTextCarriesBothSets` pins the composed form and the unchanged read-only-only one. |
| R1-07 | note | [Phase 4](phase-4-advertisement.md); `internal/integrationtest/scenario_cli_test.go:82` | `runCLIOK`'s success oracle was widened to allow trailers it should forbid in the unconfigured and read-only-only CLI scenarios, and its failure message no longer matches what it asserts. | Closed in Phase 4's review-2 pass. The helper takes the exact trailer lines the scenario expects and matches the totals line followed by those lines and nothing else, so the ten unconfigured call sites pass `nil` and forbid every path-set trailer, and the failure message names what it asserted. Probed: a `writable:` trailer emitted unconditionally now fails `TestScenarioCLIMarkerConflictReport` through the helper, which the previous regexp tolerated. |
| R1-08 | note | [Phase 2](phase-2-enforcement.md); `internal/notebook/commit.go:54-60` | Enforcement moved below `git.BuildTree`, changing which error a doubly-bad commit returns and writing protected-path blobs before the refusal; the reachability argument that makes this safe is unpinned. | Closed in Phase 2's review-2 pass. Both branches now have an oracle rather than an argument: `TestCommitInvalidContentRefusedBeforeTreeBuild` proves a commit carrying invalid content beside a protected edit returns the scan's refusal, discriminated by the file the scan names and the build does not, and `TestCommitTreeBuildWriteFailureIsInvalidContent` pins the second branch review 2 found — a `WriteBlob` fault maps to the same invalid content, names no file, begins no local mutation and publishes nothing. Both are error-coverage rows of the phase. |
| R1-09 | note | [Phase 1](phase-1-path-policy.md); README "Shared interface between phases" | The block still declares `type PathPolicy interface` while the code ships a struct. This is P1-N1, still open. | Closed. The "Shared interface between phases" block now declares `type PathPolicy struct` with its method set; the code is unchanged, since the struct is what the block's own zero-value promise requires. |

### Review round 2 — 2026-09-19 — verdict: not ready (1 blocker, 1 defect, 2 notes)

Phases reopened: 1, 2 (R2-01) and 4 (R2-02). Phases 3 and 5 keep `Complete`.
All three reopened phases are back to `Complete`: both halves of R2-01 are
closed, and Phase 4's R2-02 is closed with them.
R1-05, R1-08 and R2-03 are closed with the Phase 2 half, and R1-06 and R1-07
with the Phase 4 one. R2-04 is closed by Phase 5's review-2 gate sweep, which
also re-ran the whole gate over the thirteen tests those passes landed. V1-13
stays open for the maintainer and does not block.

| ID | Severity | Where | Summary | Resolution |
| --- | --- | --- | --- | --- |
| R2-01 | blocker | [Phase 1](phase-1-path-policy.md), [Phase 2](phase-2-enforcement.md); `internal/git/policy.go:41-57,76-92`, `internal/git/readonly.go:69-104` | R1-01's fix moved the overlap test above the intra-set collapse but left *resolution* below it. `--read-only-paths notes,notes/agent-a/locked --writable-paths notes/agent-a` collapses `notes/agent-a/locked` away, so `Protects` and `ChangedProtected` call it writable and a commit that tampers with it is published. Breaks invariants 3 and 5, D1, and `docs/slivingdoc-v1.md`'s "the longest matching entry wins". | Closed in Phase 1 — `collapseEntries` now takes the other set and keeps an entry that set splits from its own-set ancestor, and `EntrySet.covering` resolves the longest entry of a set rather than the first, so `Protects` and `ChangedProtected` answer from the entries the operator wrote and the collapse decides only what is advertised (`internal/git/readonly.go`, `internal/git/policy.go`). Pinned by three-level rows in `TestPolicyProtectsResolutionTable`, `TestNewPolicyKeepsCrossSetCoverage` and `TestChangedProtectedReportsThreeLevelProtection`, by `TestPolicyRebuiltFromAccessorsResolvesAlike`, and by `TestPolicyProtectsLongestMatchIsUnique` re-pointed at the written entries; all probed non-vacuous against both halves of the pre-fix shape. `docs/slivingdoc-v1.md` and `docs/running.md` state the rule. Closed in Phase 2 too: four black-box scenarios take a three-level composition through the real boundary — commit and pull, each in both nesting directions (`TestScenarioWritableThreeLevelCommitRefusesNestedProtected`, `TestScenarioWritableThreeLevelCommitAllowsNestedWritable`, `TestScenarioWritableThreeLevelPullRestoresNestedProtected`, `TestScenarioWritableThreeLevelPullKeepsNestedWritable`) — with the matching invariant, integration-contract and acceptance rows. All four were proved non-vacuous against both halves of the pre-fix resolution: with `entryBetween` forced false the tampered commit publishes, which is the finding verbatim, and with `covering` back on first match the mirror nesting is refused where it must publish. |
| R2-02 | defect | [Phase 4](phase-4-advertisement.md); `internal/mcp/server.go:223-259`, `internal/app/command.go:213-220` | Under a nested set pair the composed advertisement contradicts itself: "Writable paths: notes/agent-a … write only under them" beside "Read-only paths: notes … write elsewhere". The only both-sets test uses disjoint sets, so the headline composition is advertised by no test. | Closed in Phase 4. The contradicting clause is also false on its own terms — a non-empty writable set protects exactly the "elsewhere" it sends the agent to — so with a writable set configured the read-only sentence ends in "where the two sets nest, the longest matching entry decides" instead, both tool descriptions end in the same rule, and the terse surfaces carry it as one more part or trailer: `<path> (writable: …; read-only: …; longest match decides)` on both text items and a dim `path-rule:` line below the two report trailers. The set-state row is split into a disjoint and a nested case, and the nested one is pinned at *three* levels rather than two — `readOnly=[notes, notes/agent-a/locked]`, `writable=[notes/agent-a]` — by `TestAdvertisementNestedSetsStateRule`, `TestReportNestedSetsStateRule` and `TestScenarioWritableThreeLevelAdvertisementStatesRule`, each probed non-vacuous by restoring "write elsewhere". Invariant 4 is untouched: every single-set and unconfigured surface is byte-for-byte what it was, still pinned by `TestAdvertisementReadOnlyWordingUnchanged` and `TestReportReadOnlyTrailerUnchanged`. `docs/slivingdoc-v1.md`, `docs/running.md` and both one-shot help texts state the composed form. |
| R2-03 | note | [Phase 2](phase-2-enforcement.md); `internal/app/config.go:273-274`, `internal/app/service.go:186-187`, `internal/notebook/notebook.go:153` | The policy is rebuilt three times on the production path, each time from the previous one's *collapsed* accessors, so the operator's written entries survive only inside the first `NewPolicy`. Constrains any R2-01 fix and makes the first construction load-bearing without saying so. | Closed in Phase 2. Its constraint decided the R2-01 fix: the collapsed accessors are a lossless input to `NewPolicy` and a rebuild is a fixed point (`TestPolicyRebuiltFromAccessorsResolvesAlike`), so no construction is load-bearing. Phase 2 answers the remaining question — **keep the three rebuilds, document and pin the fixed point**. Collapsing them would carry a `git.PathPolicy` through `ServiceConfig` and `notebook.Config` in place of the two `[]string` fields Parameters owns, strand the harness that feeds raw entries, and remove each layer's validation of its own configuration. The fixed point is now stated where a caller meets it — `PathPolicy.ReadOnly`, both rebuild sites, and Phase 2's Configuration fields section — and pinned outside `internal/git` by `TestServiceNestedEntriesSurviveRebuild`. |
| R2-04 | note | [Phase 5](phase-5-quality-gate.md) | R1-02's precondition is recorded but no discriminator is: the section says a root-package timeout under load is an artefact and does not prescribe the single-package re-run that proves it. | Closed in Phase 5's review-2 gate sweep. "The gates" now prescribes `go test . -race -count=3 -timeout=30s` on the root package alone as the discriminator, and says which way each outcome falls: passing alone while every other package passed beside it is the artefact, and a failure that reproduces alone or on an idle machine is a finding against the phase that owns the code. No gate setting moved — it is the committed race, count and timeout applied to one package. Exercised rather than only written: the run of record put the root package at 29.172 s of its 30 s budget inside `make qa`, the discriminator returned `ok … 3.071s`, and the standalone `make test` put the same package at 7.020 s. |

**Disposition of every prior finding**

| ID | Round-1 state | Review-2 disposition |
| --- | --- | --- |
| R1-01 | blocker, fixed in Phases 1 and 3 | **Resolved for the case it names, incompletely resolved as a class.** The overlap refusal now runs over the written entries and is proven at both the unit and the operator boundary. Resolution was left on the collapsed sets, which is R2-01. |
| R1-02 | note, closed | Resolved. The precondition is stated where the gate is specified; `make qa` exited 0 here on the first attempt. R2-04 is the one remaining gap. |
| R1-03 | defect, closed | Resolved. All four checks re-run by this review print nothing; 109 and 130 distinct names, matching the record. |
| R1-04 | note, closed | Resolved — `docs/running.md:255-259,266-269`. |
| R1-05 | note, open | Still open, judgement unchanged. Re-traced: the drop is structurally forced, the invariant row still promises otherwise and no test covers it. |
| R1-06 | note, open | Still open, judgement unchanged. `errorText` still carries only the `read-only:` trailer. |
| R1-07 | note, open | Still open, judgement unchanged. `runCLIOK`'s regexp and its failure message still disagree. |
| R1-08 | note, open | Still open, judgement unchanged. The reachability argument holds for the content half; the `WriteBlob` failure branch is a second unmentioned case. |
| R1-09 | note, closed | Resolved — the shared-type block declares `type PathPolicy struct`. |
| V1-13 | note, open | Still open for the maintainer. Nothing in this review depends on it. |
| P1-N1 … P5-N3 | author notes | All closed in round 1 and none reopened here. P4-N1 remains promoted into Strategy. |


### Holistic review — 2026-09-19 — verdict: ship (0 blockers, 4 defects, 5 notes)

Whole-feature pass, not a per-phase checklist. Findings `H-01`–`H-09` live in the **Holistic review** section above, with the
independent verification that produced them. Summary:

| ID | Severity | One line | Reopens a phase |
| --- | --- | --- | --- |
| H-01 | defect (inherited) | A writable file where the baseline holds a protected directory: the protected path is restored, the writable file is silently lost | No — reproduces on `8047406` with `--read-only-paths` alone |
| H-02 | defect | `EntrySet.Covers/ChangedUnder/Pin/ReadCovered/walkCovered/hasEntryBelow` have no production caller | No |
| H-03 | defect | `Notebook.readOnly` is a second normalization whose only reader runs where it is provably identical to `policy.ReadOnly()` | No |
| H-04 | note | `resolvePolicy` validates the read-only set twice; `NormalizeEntries` hard-codes the read-only error kind | No |
| H-05 | note | `readonly.go` and its `TestReadOnly…` names outlived the read-only-specific type; `AGENTS.md` never mentions `policy.go` | No |
| H-06 | note | Four ragged rewraps from the R2-02 pass, two of them in user-facing `-h` text | No |
| H-07 | defect (inherited, CI) | `make qa` failed once more on the root-package 30 s timeout; three failures against three passes across the three reviews | No |
| H-08 | note (design) | The process throws the `PathPolicy` away and rebuilds it three times; safe, but the property should not have needed proving | No |
| H-09 | note (invariant set) | No invariant binds the completeness of the local restore, which is the gap H-01 sits in | No |

Verified independently and found sound: the collapse over all 2 187 two-set configurations of a 9-path universe, the
three-deep accessor fixed point, overlap completeness over case and trailing-slash spellings, `ChangedProtected`
against a full-read reference, Unicode fold behaviour, invariant 6's non-vacuity (8 blob reads configured vs. 8
unconfigured), and invariant 4 against the untouched pre-feature scenario oracles.


### Review round 3 — 2026-09-19 — verdict: ship (0 blockers, 1 defect, 2 notes)

Phases reopened: **none**. Every phase keeps `Complete`. Run against the same
tree as the holistic review, after it. All nine H-findings and V1-13 were
re-checked and are unchanged; see the Review 3 entry in the Decisions log.

| ID | Severity | Where | Summary | Resolution |
| --- | --- | --- | --- | --- |
| R3-01 | defect (efficiency) | [Phase 1](phase-1-path-policy.md); `internal/git/policy.go:185`, `:228`, `:244` with `:274` | Detection and restore are quadratic in the number of reported protected paths: `appendUnique` scans the list per reported path, `RestoreProtected` runs `namedOrUnder` over the whole list per local file, and `readBlobAt` re-walks the base tree from the root per path with a linear `findEntry` at each segment. Measured at roughly 4× per doubling, ~1 s at 8 000 paths. D2's inversion is what makes a large count reachable, since the bound becomes everything outside the writable region rather than the region the operator named. | **Open.** Three checkboxes on the Phase 1 finding: a set for the de-duplication, an ancestor-prefix lookup for `namedOrUnder`, and one memoized walk of the base tree for the restore reads. Invariants 3, 5, 6 and 7 are untouched — the answer is right, only slow — so the phase is not reopened. |
| R3-02 | note | [Phase 2](phase-2-enforcement.md); `internal/notebook/commit.go:362`, `:395`, `internal/notebook/pull.go:104` | All three engine-failure sites still say "read the baseline snapshot for the read-only check". Detection reads no snapshot — that is invariant 6 — and a process configured with `--writable-paths` alone has no read-only setting. The text is caller-facing through `internal/mcp/errors.go:129`. | **Open.** Two checkboxes on the Phase 2 finding: name the tree comparison and the protected-path read, and say "protected" rather than "read-only". No test pins the string. The category, reason, action and recovery stage are correct and unchanged. |
| R3-03 | note (contract) | README, "Non-negotiable invariants" and "Definition of success" | Nothing in the contract bounds the **cost** of detection or restore in the number of reported paths. Invariant 6 and success 4 bound what the policy *reads* and are held non-vacuously; both are silent on the work done per reported path, which is how three quadratic scans shipped through four review passes. | **Open — maintainer.** Pairs with H-09: H-09 wants an invariant on the *completeness* of the restore, this one wants a criterion on its *cost* — for example, that the policy's work is linear in the reported paths and in the local file count. The rule itself is promoted into Strategy so the next phase inherits it. |

**Disposition of every prior finding**

| ID | Prior state | Review-3 disposition |
| --- | --- | --- |
| R1-01 … R1-09 | all closed by the review-1 and review-2 fix passes | Resolved; no regression. The class was re-probed by reading, not assumed. |
| R2-01 … R2-04 | all closed | Resolved. R2-04's discriminator was not needed this run — `make qa` passed on the first attempt. |
| H-01 | defect (inherited), open | **Still open, unchanged.** Re-derived from `RestoreProtected`: the reported paths never name the writable blob standing where the baseline holds a protected directory, so nothing drops it and the snapshot carries a blob and a tree at one name. |
| H-02, H-03, H-05, H-06 | defects and notes, open | **Still open, unchanged**, each re-checked by grep rather than assumed. |
| H-04, H-08 | notes, open | **Still open, unchanged.** H-08's four constructions are visible in `internal/app/config.go:266`, `service.go:104` and `notebook/notebook.go:164`. |
| H-07 | defect (inherited, CI), open | **Still open.** Did not reproduce this run; the tally is now three failures against four passes. |
| H-09 | note (invariant set), open | **Still open**, and R3-03 is its cost-side twin. Both belong in the same contract change. |
| V1-13 | note, open — maintainer | **Still open.** Nothing in this review depends on it. |

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

### 2026-09-18 — validation round 1 and its fixes

Validated against the code rather than against the headings, and the code
disagreed in five places that mattered. The substantive one was V1-01: invariant 6
claimed no baseline blob is read on a clean operation, but `Notebook.Pull` merges
over `n.ws.Baseline().Tree`, so a clean pull opens baseline blobs for reasons that
predate this feature. The invariant would have held only against the notebook fake,
whose merge is scripted — a green test proving that the fake is a fake. The property
is now the policy's, and its oracle is a difference against an unconfigured run,
which is true of the real engine too.

Three findings were reference errors that would each have broken a phase mid-flight:
a renamed type used in a file no phase listed (V1-02), a widened interface breaking
three test doubles in a file no phase listed (V1-03), and a counting fake that
already exists in the package where Phase 1 proposed to add one (V1-05). V1-04 was
the opposite kind of gap: the composition rule that motivates the whole effort had
six integration rows and no test name among them, so Phase 5's evidence check could
not have reached it.

V1-15 was found while fixing the others and is the one the author's own checklist
run should have caught: every cited test name used underscores, and not one of the
repository's 555 test functions does. All 89 underscored names were rewritten,
`TestIntegration_Writable_*` becoming `TestScenarioWritable*` to match the
`TestScenarioReadOnly*` family it will sit beside. Checklist line 7 now carries the
command that would have caught it.

Checklist re-run after the fixes:

| Line | Outcome |
| --- | --- |
| 1. No numerals with units outside oracle rows | Pass, clean |
| 2. Every test name declared in exactly one phase | Pass across 107 distinct names, up from 81 as the unnamed scenarios were named |
| 3. Every config field, flag, and injectable field has one owner | Pass after V1-06; the resolved flag field now has its own row |
| 4. Every invariant and limit is a table with a test per row | Pass; the three integration tables now carry a test column too |
| 5. Every phase mentioning listening, manual, or paid has `Human required` | Pass, none do |
| 6. No phase references text scheduled for deletion | Pass |
| 7. New conventions do not contradict existing code conventions | Pass after V1-15; the line now names the test-naming convention and its command |

New: an acceptance sweep in Phase 5, the companion to its evidence check. The
evidence check proves every cited name resolves to a test; the sweep proves no
acceptance row rests on a reviewer's word. Both print nothing when they pass.

One item is open and needs the maintainer: V1-13, the downstream consumer's
repository and revision. Nothing in the design depends on it.

### 2026-09-18 — Phase 1 executed

`PathPolicy` built in `internal/git/policy.go` with the rename of the entry-set
symbols in `internal/git/readonly.go` and its four call sites. Phase 1 is
`Complete`: every acceptance and error-coverage row is proven by a named test in
`internal/git/policy_test.go`, and `make test` passed unedited at 84.3 %
coverage. The phase file carries the full command list and the per-test results.

One specification note for the maintainer, recorded rather than fixed:

| ID | Severity | Where | Note |
| --- | --- | --- | --- |
| P1-N1 | note | README "Shared interface between phases" | The block declares `type PathPolicy interface` while stating in the same block that an unconfigured policy "is the zero value". An interface's zero value is nil, so the two cannot both hold, and `AGENTS.md` places an interface in the consuming package rather than in the producing one (`internal/git`). Phase 1 shipped `PathPolicy` as an exported struct carrying exactly the declared method set and signatures, so `NewPolicy(readOnly, writable []string) (PathPolicy, error)` and every consumer statement in Phases 2–4 read identically. Either change the block to `type PathPolicy struct`, or say why the interface is wanted and which package should own it. |

No other gap surfaced. V1-13 (the downstream consumer's repository and revision)
is still open and still touches nothing this phase built.

### 2026-09-18 — Phase 2 executed

Both operations now hold `git.PathPolicy`. Commit builds the local tree first and
hands it to the enforcement step, which compares it against the baseline tree,
restores only the protected paths that actually differ, and refuses; pull runs
the same detection and skips the pin entirely when nothing protected changed.
`Service` and `Runtime` gained the writable accessor beside the read-only one,
and `HarnessConfig` gained the field the sixteen new black-box scenarios
configure. Phase 2 is `Complete`: every invariant, limit, error-coverage, and
acceptance row is proven by a named passing test, the sixteen existing read-only
scenarios pass unedited, `make lint` is clean and `make test` passes at 84.4 %
coverage. The phase file carries the row-by-row evidence.

Two notes for later phases, recorded rather than fixed:

| ID | Severity | Where | Note |
| --- | --- | --- | --- |
| P2-N1 | note | Phase 2 "What the two operations hold" | `PathPolicy` answers every enforcement question except one: which read-only entry a violated path falls under, which the unchanged read-only refusal wording names. The README forbids widening the shared interface, so `Notebook` keeps a `git.EntrySet` of the read-only side beside the policy for that one purpose. Either add a covering-entry accessor to the shared-interface block, or accept the retained set. |
| P2-N2 | note | Phase 2 "Commit", error coverage rows 2 and 3 | `PathPolicy.RestoreProtected` validates the restored snapshot itself, so a failed baseline blob read and an unrepresentable snapshot return one wrapped error and the phase asks for two different categories. The notebook discriminates by passing the repository through a read-error recorder. A typed error from Phase 1 would be cleaner if the worklog ever reopens `internal/git/policy.go`. |

V1-13 (the downstream consumer's repository and revision) is still open and still
touches nothing this phase built.

### 2026-09-18 — Phase 3 executed

`--writable-paths` and `SLIVINGDOC_WRITABLE_PATHS` resolve through the shared
flag set of `serve`, `pull`, and `commit`, split like the read-only value, and
compose into `git.PathPolicy` at the point where the read-only set was
normalized before — ahead of the native engine and the store probe. The
ordering is asserted with an engine and a store factory that fail on any call,
and both oracles were proved live with throwaway runs before being trusted.
Phase 3 is `Complete`: every resolution, refusal, integration-contract,
acceptance, and error-coverage row is proven by a named passing test, `make
lint` is clean and `make test` passes unedited at 84.4 % coverage. The phase
file carries the row-by-row evidence.

Two notes for later phases, recorded rather than fixed:

| ID | Severity | Where | Note |
| --- | --- | --- | --- |
| P3-N1 | note | Phase 3 integration contract, the three CLI rows | The collaborator column says "fake store", but two of those rows state side effects the fake cannot express: each spawned process builds its own in-memory store, so there is no accepted baseline for "the touched file is reset" to reset to and no remote generation for "remote generation unchanged" to be unchanged from. Both rows run against the real backend with an in-process writer seeding R, which is the repository's own answer to the same problem (`TestScenarioCLIReadOnlyCommit`). The two rows that need no baseline keep the fake. Either correct the column, or say why a weaker oracle is wanted. |
| P3-N2 | note | Phase 3 error coverage, row 1 | "naming the offending entry and which setting it came from" is satisfied by the entry text and the `writable paths:` / `read-only paths:` prefix, not by naming the flag versus the environment variable. Naming the true source would mean threading provenance through resolution, which no other setting does. The phase's own Startup-refusal prose accepts this ("the entry text is what identifies the source in practice"); the error-coverage row reads stricter than the prose. |

V1-13 (the downstream consumer's repository and revision) is still open. It
touches Phase 3 only as the unpinned justification for the explicitly-empty
flag idiom, which this phase proves independently
(`TestFlagsWritablePathsExplicitEmptyIgnoresEnvironment`,
`TestScenarioWritableFlagExplicitEmptyIgnoresEnvironment`).

### 2026-09-18 — Phase 4 executed

Every surface an agent or an operator reads now names the writable set: the
server instructions, both tool descriptions, the success text item, a
`writable` array on every success and every domain error, and a dim
`writable:` trailer above the read-only one on both CLI reports. The writable
form always precedes the read-only form, and a read-only-only process is
byte-identical to what it printed before. Phase 4 is `Complete`: every
set-state, structured-field, integration-contract, acceptance, and
error-coverage row is proven by a named test, all twenty-one pass under
`-race -count=3`, `make lint` is clean and `make test` passes unedited at
84.5 % coverage. The phase file carries the row-by-row evidence, the exact
wording that shipped, and the throwaway edits each oracle was probed with.

Three notes for the maintainer, recorded rather than fixed:

| ID | Severity | Where | Note |
| --- | --- | --- | --- |
| P4-N1 | note | Phase 4 "Files" | The list names the files the change starts in but not the ones it forces. Widening `app.Report` by one parameter forces `cmd/pull/pull.go` and `cmd/commit/commit.go`; widening the envelope forces the black-box harness that decodes and re-derives it (`internal/integrationtest/harness.go`, `scenario.go`, `scenario_helpers_test.go`) and the CLI success oracle whose regexp pinned the totals line as the last line (`scenario_cli_test.go`). This is V1-03's finding one layer out: a widened shape breaks every double and every oracle of that shape, and listing only the production files hides them. |
| P4-N2 | note | Phase 4 "The surfaces" | The table puts the writable array on the error *result* and names a text item only for the success result, so the MCP error text item keeps its read-only trailer alone. That is what shipped, deliberately: Phase 2's refusal already names the writable set in the message a text-only host reads. Either add the error text trailer to the table, or record that the refusal message is the text surface for errors. |
| P4-N3 | note | Phase 2 `TestScenarioWritableCleanCommitUnchanged` | The scenario asserted that a configured clean commit returns the *same envelope* as an unconfigured one, which D7 makes impossible once the arrays are advertised. It now compares the operation's result with the advertised sets factored out and asserts the writable array separately. Three Phase 3 CLI scenarios were likewise updated to expect the trailer their configured processes print, per the AGENTS.md rule on tests a feature legitimately changes. |

V1-13 (the downstream consumer's repository and revision) is still open. It is
the bullet behind this phase's consumer-compatibility section; nothing here
rests on it, since the phase added a field and a trailer and altered neither
existing shape, which `TestReportReadOnlyTrailerUnchanged` and
`TestAdvertisementReadOnlyWordingUnchanged` assert directly.

### 2026-09-18 — Phase 5 executed

`make qa` passes unedited over the whole effort at 84.5 % coverage, above the
seventy percent floor: gofumpt clean, `go vet` clean, staticcheck clean,
`go fix -diff` silent, eighteen Go packages `ok` under `-race -count=3
-timeout=30s` against the pinned libgit2 and a real SeaweedFS container, and
thirty-five npm tests passing with none skipped. `git diff --stat Makefile`
prints nothing, so no gate setting moved. Phase 5 is `Complete`, and with it
every phase of the effort.

`dupl -t 80` reported three clone groups. One was actionable and was fixed
rather than excused: two adjacent writable scenarios whose bodies differed only
in the written path, its content, and the commit message now share one
parameterised helper, keeping both test names so the phase evidence still
resolves. The other two are excused by named clauses — the harness/server
success-text pair by "cross-package contract assertions", and a pre-existing
`internal/app` pair by "table-driven test loops". The clone this phase predicted
never appeared, because Phase 4 widened the existing advertisement helpers
instead of adding parallel ones.

The three self-checks all needed adjudication rather than counting, which is the
one lesson worth carrying: each was authored against specification prose and now
also reads the Implementation notes the phases appended, where a `-run`
alternation looks like a citation, an unescaped `t.Skip` matches `…PathSkipped`,
and a third `Done` column defeats a header filter. Every underlying outcome
holds — 122 backticked citations all resolve, no acceptance row is unbacked, and
no file this effort touched carries a skip — but the literal empty-output
conditions do not, and the phase file records each adjudication with its
evidence. Three notes are open for the maintainer (P5-N1 through P5-N3) and none
is a finding against Phases 1–4.

V1-13 remains the one open maintainer item from validation.

### 2026-09-19 — review round 1

Reviewed all five phases against the code rather than against the implementation
notes, with every gate re-run from the repository root. Verdict: **not ready** —
one blocker, one defect, seven notes; Phases 1, 3 and 5 reopened.

The blocker is R1-01 and it is a composition nobody expressed in a test. Every
overlap case in the suite uses a bare two-entry configuration, so the interaction
between `normalizeEntries`' intra-set coverage collapse and `NewPolicy`'s
cross-set overlap test never came up. Because the collapse runs first,
`--read-only-paths=docs,docs/open --writable-paths=docs/open` starts without a
refusal and makes `docs/open` writable: a path the operator listed as read-only,
silently ignored. The README's own Strategy section had warned in the abstract
that the collapse "must not be applied across the two sets"; the implementation
applies it before the cross-set test can see the entry. The rule is now written
into invariant 5 so a fix cannot regress to the same shape.

The gates themselves are sound. `make lint` is clean, `dupl -t 80` reports
exactly the two clone groups Phase 5 records, npm is 35/35, and an idle-machine
`make test` is 18 `ok` packages at 84.5 %. Two earlier `make qa` runs failed, both
on the root package's in-suite release build hitting the 30 s per-package budget
under whole-suite concurrency — the fragility the `Makefile` already documents,
not a defect of this effort (R1-02).

What did not survive re-running was Phase 5's evidence check (R1-03). The phase
adjudicated eight over-matching tokens by writing them, backticked, into its own
notes, so the refined command it recommends now collects them: eight `MISSING`
lines and 132 distinct names against the recorded 122. The outcome still holds —
every genuine citation resolves — but a check that its own record defeats proves
nothing to the next reader.

Everything else held under tracing. Invariant 3 was followed through the commit
conflict branch, the no-op branch, every CAS retry, the pull conflict branch, the
recovery entry and the generation-0 empty baseline, and it holds on all of them
for one structural reason: at a protected path the local side equals the merge
base, so the merge takes the remote side. Invariant 6's difference oracle is
honest and non-vacuous. Invariant 4 is byte-for-byte on every surface, and the
`writable` array is present at all four `ToolError` construction sites with the
black-box harness failing independently on a nil.

All nine author notes are real; none is noise. P4-N1 was promoted into Strategy
as a planning rule. V1-13 is still the one open maintainer item from validation.

### 2026-09-19 — Phase 1 review-1 fixes

R1-01 fixed where the review said it lives: the order of two steps, not the
comparison itself. `normalizeEntries` was one pass that validated and collapsed
together, so `NewPolicy` never saw an entry the collapse had already swallowed.
It is now `validateEntries` (trim, `ValidatePath`, fold, keep every written
entry) followed by `collapseEntries` (drop a covered entry, de-duplicate, sort),
with `normalizeEntries` the two composed and byte-for-byte what it was.
`NewPolicy` tests the overlap over the written entries and collapses each side
only after the test passes. No signature, error type, or message changed, so
Phases 2–4 consume exactly what they did; what changed is which configurations
start. `--read-only-paths docs,docs/open --writable-paths docs/open` now
refuses, as does the reverse nesting and the case-folded form.

`TestNewPolicyRejectsCollapsedOverlap` pins the four rows and was proved
non-vacuous by reverting `NewPolicy` to collapse-then-compare in a throwaway
edit: all four subtests failed, and passed again once the fix was restored.
`make lint` is clean, `make test` passes unedited with 18 `ok` packages at
84.5 %, and `dupl -t 80` still reports exactly the two clone groups Phase 5
records.

R1-09 is closed in this README rather than in the code: the shared-type block
declares `type PathPolicy struct` with its method set, which is what the block's
own "the zero value" promise requires. `docs/slivingdoc-v1.md` gained one clause
saying the overlap refusal compares the entries the operator wrote, since this
change moves observable startup behaviour.

Phase 1 is `Complete`. R1-01 stays open against Phase 3, which owns the
flag-level startup-refusal row and test for the same configuration; nothing it
needs from Phase 1 changed shape. V1-13 is still the one open maintainer item.

### 2026-09-19 — Phase 3 review-1 fixes

The Phase 3 half of R1-01 needed no code: Phase 1 fixed the construction without
moving a signature, an error type, or a message, so `resolvePolicy` composes
exactly what it composed before and the configuration the review named already
refuses. What Phase 3 owed was its own contract row and the proof at the operator
boundary. `TestSetupRefusesBeforeEngineAndProbe` gained three rows —
`--read-only-paths=docs,docs/open --writable-paths=docs/open`, the reverse
nesting, and a several-overlap configuration — each going through `loadConfig`
from real argument strings against the engine and store factory that fail when
touched. The third row pins the one observable the written-entry comparison adds
beyond refusing: the pair the message names is the first read-only entry the
operator wrote, not the first of the sorted collapsed set. All three were proved
non-vacuous by reverting `NewPolicy` to collapse-then-compare in a throwaway
edit, which failed exactly those three and left the four existing rows passing.

R1-04 is closed in `docs/running.md`: the composed example now says what else
that configuration protects, which `docs/slivingdoc-v1.md` already said and the
operator guide is the likelier place to be misread. P3-N1 and P3-N2 are closed as
the review proposed — the collaborator column on the two CLI rows now says real
store with an in-process writer seeding the baseline, and the error-coverage row
asks for the set rather than the flag-versus-environment provenance no setting
threads.

`make lint` is clean and `make test` passes at 84.5 % over 18 `ok` packages. Two
earlier `make test` runs failed exactly as review 1 described in R1-02 — the root
package's in-suite release build hitting the 30 s per-package budget while
`internal/git` and `internal/integrationtest` ran beside it, with the package
alone `ok` in 3.3 s — which is now a third observation of that fragility for
Phase 5 to record.

Phase 3 is `Complete`. Phase 5 still owns R1-03 and R1-02; its evidence check
sees no new test name from this pass, since every row added is a subtest of
`TestSetupRefusesBeforeEngineAndProbe`, a name Phase 3 already declares. V1-13
remains the one open maintainer item.

### 2026-09-19 — Phase 5 review-1 fixes

Phase 5 closed R1-03 and R1-02, and P5-N1, P5-N2 and P5-N3 with them. No
production code and no test changed; the whole gate was re-run from scratch
rather than carried over, because Phases 1 and 3 changed under it after the first
sweep.

R1-03's root cause was that the evidence check could not tell a phase's
specification from its record. A backticked `Test…` token above
`## Implementation notes` is a citation; below it the same token may be a `-run`
prefix, a glob stem, or a word being adjudicated. The first sweep's own
adjudication table therefore became input to the command that sweep recommended,
and its arithmetic described a file state that stopped existing the moment it was
written. The fix splits the two halves and gives each its own command: an
acceptance gate over the specifications with no exclusion and no tolerance (109
names), and a prefix-tolerant completeness sweep over the records with one stated
exclusion (130 names; 134 in union). The acceptance sweep drops header rows by
matching `Proven by` rather than by counting columns, and the skip scan escapes
its `.`. All four print nothing.

The cross-phase rule this exposes is worth stating once, and it is now written
into Phase 5's Specification: **a check that reads the worklog must distinguish
what a phase claims from what it records, or it will eventually fail on its own
notes.** It is not specific to this effort. The mechanism proved itself in the
same session — a first pass at the fix backticked the excluded word in
specification prose and wrote an acceptance row naming no command, and re-running
the four checks printed both before anything was recorded as passing.

R1-02 is recorded as a precondition rather than an excuse: the run of record is
taken on an idle machine, stated in Phase 5's "The gates" against the `Makefile`'s
own `$(BIN)` comment, with the rule that a timeout there is never answered by
touching the timeout, the count, or the race flag. Nothing in the gate moved.

Fresh gate results: `make qa` exit 0; `make lint`, `make test` and `make npm-test`
each green standalone; 18 `ok` packages, 0 `FAIL`, `== coverage: 84.5% (floor
70%) ==`, 35 npm tests passing; `dupl -t 80` finds the same two clone groups,
both accepted with their clauses and neither in `internal/git`.

All five phases are `Complete` and the top-level status now agrees with the
board. What stays open is not a phase: V1-13 needs the maintainer to record or
strike the downstream consumer's repository and revision, and R1-05, R1-06 and
R1-08 remain notes against Phases 2 and 4 that review 1 judged non-blocking.
Whether the effort is ready is review 2's call, not this session's.

### 2026-09-19 — review round 2

Reviewed all five phases against the code again, with every gate re-run from the
repository root and the round-1 findings re-checked one by one. Verdict: **not
ready** — one blocker, one defect, two notes; Phases 1, 2 and 4 reopened.

R1-01, R1-03 and R1-04 reproduce as closed. The overlap refusal now compares the
entries the operator wrote, at the unit boundary and again through `loadConfig`
against an engine and a store that fail on any call. All four of Phase 5's
rebuilt evidence checks print nothing and their distinct-name counts match the
record. `make qa` exited 0 on the first attempt, at 84.5 % over 18 `ok`
packages, with `dupl` reporting the same two accepted clone groups and
`git diff --stat Makefile` empty.

What the fix did not carry is the class. R1-01 said the intra-set collapse runs
above the cross-set overlap test; the fix moved the overlap test. `Protects`
still resolves over the collapsed sets, so
`--read-only-paths notes,notes/agent-a/locked --writable-paths notes/agent-a`
drops the operator's inner read-only entry and makes `notes/agent-a/locked`
writable — proved at the enforcement input, where `ChangedProtected` returns the
empty list for a tampered file there and the commit publishes. Adding a broader
read-only entry makes a narrower read-only entry stop applying, which is the
dependency invariant 5 was sharpened to forbid, expressed through resolution
instead of through the refusal. That is R2-01, and invariant 5 now binds
resolution as well.

The second finding is one layer out: the composed advertisement (R2-02). Under a
nested set pair every agent-facing surface emits "write only under
notes/agent-a" immediately followed by "changes under notes are refused; write
elsewhere". The only both-sets test configures disjoint entries, so the
composition the feature exists for is advertised by no test at all.

Two notes. R2-03 records that the policy is rebuilt three times down the
production path, each time from the previous one's collapsed accessors, so the
written entries survive only inside the first `NewPolicy` — a constraint on any
R2-01 fix. R2-04 asks Phase 5 to name the single-package re-run that
discriminates a load artefact from a real root-package regression.

R1-05, R1-06, R1-07 and R1-08 were re-read in the code and all four still hold
as filed; the judgement that none blocks stands. V1-13 is still the one open
maintainer item.

The rule behind both rounds is now in Strategy: the entries the operator wrote
are the policy's input of record, and the intra-set collapse normalizes the
output only. It may decide what is advertised; it may decide neither the
refusal nor the resolution.

### 2026-09-19 — Phase 1 review-2 fixes

R2-01's Phase 1 half is closed and Phase 1 is `Complete` again. The review
offered two shapes and R2-03 chose between them. Carrying the written entries
on `PathPolicy` would have fixed only the first of the three constructions on
the production path: `internal/app/config.go` overwrites its own fields with
the collapsed accessors, `internal/app/service.go` rebuilds `notebook.Config`
from them, and `notebook.New` calls `NewPolicy` a third time, so a field the
struct carries is re-derived from collapsed entries before enforcement ever
reads it. Threading the written entries through those three files would work
but is Phase 2's and Phase 3's code, and it leaves the same trap for the next
caller. The fix taken instead makes the collapse itself lossless for
resolution: `collapseEntries(items, other)` drops an entry covered by an
ancestor of its own set only where that ancestor decides every path the entry
decides, which fails exactly when an entry of the other set lies strictly
between them. Rebuilding the policy from its own accessors is now a fixed
point, so no construction is load-bearing and R2-03's hazard is answered
wherever the policy is built.

The second half of the fix is smaller and was invisible until the first landed:
with a set now able to hold an entry below another, `EntrySet.covering`
returned the *first* match in sorted order, which is the shortest, so
`Protects` compared the wrong length. It resolves the longest match now, and
`CoveringEntry`'s promise of disjointness is withdrawn.

What this changes for the other phases is what the entry accessors return. For
`--read-only-paths notes,notes/agent-a/locked --writable-paths notes/agent-a`,
`ReadOnly()` is `[notes notes/agent-a/locked]` where it was `[notes]`. With
either set empty nothing moves, so invariant 4 and every unconfigured and
read-only-only surface are untouched; but a nested configuration now advertises
the entry it protects, and a reader of those entries must take the longest
match rather than assume one. The "Shared interface between phases" block says
so, which is the README change review 2 anticipated.

The tests were probed against both halves of the pre-fix shape before being
trusted: reverting the cross-set collapse fails eleven subtests across four
tests, and reverting `covering` to first-match fails seven across three, while
`TestNormalizeEntriesMatchesPreviousBehaviour` and the eight pre-existing
`readonly_test.go` tests pass under both reverts. `make lint` is clean, `make
test` passes unedited at 84.5 % over 18 `ok` packages, `dupl -t 80` still finds
exactly the two accepted clone groups and none in `internal/git`, and all four
of Phase 5's checks still print nothing (111 specification names, 133 record
names, up from 109 and 130 as the new names were cited).

R2-01 stays open against Phase 2, which owns the boundary scenario; R2-02 stays
open against Phase 4. V1-13 is still the one open maintainer item.

### 2026-09-19 — Phase 2 review-2 fixes

R2-01's Phase 2 half is closed and Phase 2 is `Complete` again, with R1-05,
R1-08 and R2-03 closed beside it. No production behaviour moved: the
resolution fix was Phase 1's, and what this phase owed was the proof at the
real boundary plus three notes that had been carried since round 1.

This session resumed an interrupted one. A previous pass on this phase was
killed mid-work by an API rate limit and left uncommitted code in the tree
with nothing in the worklog to say what it was. Everything it wrote was
audited, kept, re-run and probed from scratch before being recorded; nothing
was trusted because it was already there. The lesson is the worklog's own:
an implementation-notes delta written as work lands, rather than at the end,
is what makes an interrupted pass recoverable.

The boundary proof is four scenarios, not the two the checkbox asked for.
Commit and pull are separate enforcement points and R2-01 defeats both, and
the two nesting directions travel different paths through the collapse — one
keeps a read-only entry the collapse would have dropped, the other a writable
one. Each was run against both halves of the pre-fix shape: with
`entryBetween` forced false the tampered commit publishes and the pull leaves
the tampered file, which is the finding verbatim, and with `covering` back on
first match the mirror nesting is refused where it must publish.

R2-03 is answered rather than deferred: **keep the three rebuilds, document
and pin the fixed point.** Collapsing them would put a `git.PathPolicy` in
two configuration structs in place of the `[]string` fields Parameters owns,
strand the black-box harness that feeds raw written entries, and take away
each layer's validation of what it is configured with. The hazard is gone for
a better reason than call order — Phase 1's collapse is lossless for
resolution — and what was missing was that this was written nowhere a future
caller would look and pinned nowhere outside `internal/git`. Both gaps are
closed.

The two Phase 1 hand-offs are closed with it. The notebook's retained
read-only entry set is collapsed within its own set while the policy collapses
each set against the other, so the two can disagree — but only when the
writable set is non-empty, where the refusal names the writable entries and
never reads the retained set. `TestReadOnlyRefusalSetMatchesPolicyEntries`
pins all three halves of that argument, and forcing the refusal down the
read-only branch makes it fail with an empty entry list, which is what the
retained set answers there.

`make lint` is clean and `make test` passes unedited at 84.6 % over 18 `ok`
packages, up from 84.5 % as the nine new tests landed. The read-only
regression guard is still unedited.

Phase 4 owns what remains: R2-02, the composed advertisement. Phase 5 re-runs
the gates after it, and its evidence check sees nine new names from this pass.
V1-13 is still the one open maintainer item.

### 2026-09-19 — Phase 4 fix pass (review 2)

R2-02 turned out to be two faults in one sentence. The contradiction the
review names — "write only under them" beside "write elsewhere" — is the
visible half; the other is that "write elsewhere" was already false the
moment a writable set existed, since a non-empty writable set protects
exactly the elsewhere it points at. Fixing the second fixes the first: the
clause is replaced, not qualified, and what replaces it is the one rule that
reconciles the two sets. Every surface that names both now carries that rule,
in prose where there is prose and as one more part or trailer where there is
not.

The rule is stated whenever both sets are configured rather than only when
they actually nest. Detecting nesting would have given the advertisement two
both-sets forms, each needing its own specification row and its own oracle,
to save a clause in the arrangement where it is merely redundant. The
composed form is one form.

The new pins are three-level, not two, on the review's own instruction: a
two-level pair is the arrangement that hid R2-01, and it is also the one
arrangement where the *old* wording looked coherent. The three-level
advertisement scenario runs the composition over the real
boundary, so it proves the advertised entries are the ones the operator
wrote — the collapse kept the third level — and that the wording reconciles
them, in one test.

R1-06 was closed by implementing it rather than by recording the decision the
checkbox also allowed. Under a nested composition an error text carrying
`read-only: notes` alone misleads by omission about the one directory the
agent works in; that is R2-02 on a surface R2-02 does not name, and leaving
it recorded would have left the class open again — the failure mode this
effort has now met twice.

`make lint` is clean and `make test` passes unedited at 84.6 % over 18 `ok`
packages. Four new tests land in this pass, all cited in Phase 4, and
`dupl -t 80` has not been re-run since the thirteen tests of the last two
passes; Phase 5's gate sweep owns that.

### 2026-09-19 — Phase 5 review-2 gate sweep

Phase 5 re-ran the whole gate from scratch and closed R2-04, the last finding
open against any phase. No production code and no test changed. The sweep was
not a re-reading of the last one: the review-2 fix passes to Phases 1, 2 and 4
landed thirteen tests, a resolution change in `internal/git`, a rewritten
composed advertisement in `internal/mcp` and `internal/app`, and edits to both
documents, and `dupl` had been run against none of it.

Fresh results: `make qa` exit 0 on the first attempt; `make lint`, `make test`
and `make npm-test` each green standalone; 18 `ok` packages, 0 `FAIL`,
`== coverage: 84.6% (floor 70%) ==`, 35 npm tests passing with none skipped;
`git diff --stat Makefile` empty. Coverage moved 84.5 % → 84.6 % with the
thirteen tests. All four evidence checks print nothing, at 124 specification
names and 148 record tokens of which 147 are checked — up from 111 and 133, and
recorded on both sides of the one exclusion this time, because the earlier
sweeps recorded a single number without saying which side it fell on.

Two results were not the expected ones and both are recorded rather than
smoothed over. `dupl -t 80` now reports **one** clone group where two sweeps
reported two: the accepted harness/server pair stopped matching, because Phase
4's fix appended the nest rule to both from different places — the server from
`notebook.PathSetsNestRule`, the harness from a literal kept deliberately
independent — which splits the shared token run in the middle. The duplication
is still there and still deliberate; only the tool's corroboration of the
verdict is gone, which is worth knowing before someone reads the single group as
an improvement. The clone Phase 4 predicted, `writePathSets` against the trailer
block of `errorText`, does not reach the threshold and was judged on its merits
anyway: what must not drift between those two surfaces is already one shared
constant, and merging the rest would couple the CLI renderer to the MCP text
builder.

Every line-number citation in the maintenance-contract section had moved, the
writable subsection from 336 to 346 and the accepted contract's three
`writable-paths` lines by more than thirty each, because Phase 1's and Phase 4's
review-2 passes both added prose above them. Each grep was re-run and each
subsection re-read against the phase that owns it rather than re-cited.

R2-04 is closed by prescribing the discriminator rather than by arguing the
excuse: a root-package timeout is confirmed as a scheduling artefact by
re-running that package alone under the committed race, count and timeout, and a
failure that reproduces there or on an idle machine is a finding. It was
exercised in the same session, and the run of record is the closest observation
this effort has made to the fragility R1-02 names — the root package took
29.172 s of its 30 s budget inside `make qa`, 3.071 s alone, and 7.020 s in the
standalone `make test`. The package is not slow; it is starved, and this
effort's new tests lengthen the window it is starved in.

All five phases are `Complete` and every finding from validation round 1 and
review rounds 1 and 2 is closed. V1-13 — the downstream consumer's repository
and revision — is the one open item and needs the maintainer, not a phase.
