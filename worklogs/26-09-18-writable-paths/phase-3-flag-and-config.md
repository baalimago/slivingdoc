# Phase 3 — Flag and configuration

**Status:** Complete

[← README](README.md)

## Goal

Give the writable set an operator surface that resolves exactly like every other
setting, shared by all three commands, and make a misconfiguration refuse startup
before the native engine or the object store is touched.

## Specification

### Resolution

The flag and its environment variable are rows in the README parameters table.
They join the shared flag set, so `serve`, `pull`, and `commit` all accept them
with no per-command wiring, and they resolve through the existing precedence:
flags beat environment variables, which beat defaults.

An explicitly empty flag value does not fall back to the environment. That rule
already exists and this phase only inherits it, but it carries weight here: a
caller that must defeat an inherited environment value passes the flag with an
empty value, and a downstream consumer depends on exactly that idiom to keep its
own publication path unconstrained. A change that made an empty flag fall through
to the environment would silently confine a process that asked not to be.

The value splits on the separator named in the README parameters table, with
surrounding whitespace trimmed from each piece and empty pieces dropped, exactly
as the read-only value splits today.

| Input | Resolved set | Test |
| --- | --- | --- |
| Flag set, environment set | The flag's entries | `TestFlagsWritablePathsResolution` |
| Flag unset, environment set | The environment's entries | `TestFlagsWritablePathsResolution` |
| Flag unset, environment unset | Empty | `TestFlagsWritablePathsResolution` |
| Flag explicitly empty, environment set | Empty; the environment is not consulted | `TestFlagsWritablePathsExplicitEmptyIgnoresEnvironment` |
| A value with surrounding whitespace and an empty piece | The trimmed, non-empty entries | `TestFlagsWritablePathsSplitting` |

### Startup refusal

Configuration normalizes both sets and constructs the policy at the point where
the read-only set is normalized today, which is before the native engine opens
and before the store probe runs. That ordering is an existing documented property
of an invalid read-only entry, and this phase extends it to the writable set and
to the overlap error rather than weakening it.

The overlap message names the offending path and both settings, because an
operator reading it needs to know which of the two to change. When a value came
from the environment rather than the flag, the message still names the settings;
the entry text is what identifies the source in practice.

The overlap tested is the one the operator wrote (README invariant 5): the
comparison runs over both sets' written entries, before either set drops the
entries another entry of the same set covers, so the refusal never depends on
which unrelated ancestors happen to be listed beside the overlapping entry. When
more than one path is named by both sets the refusal names one pair — the first
read-only entry, in the order the operator wrote it, that the writable set also
names — because one named pair is enough to send the operator to the value to
edit and a full list would be a second message format to keep true.

| Failure | Refusal point | Test |
| --- | --- | --- |
| An entry in the writable set fails path validation | Before the engine and the probe | `TestSetupRejectsInvalidWritableEntry` |
| A path is named by both sets | Before the engine and the probe | `TestSetupRejectsOverlapNamingBothSettings` |
| A path is named by both sets differing only in letter case | Before the engine and the probe | `TestSetupRejectsCaseFoldedOverlap` |
| A path is named by both sets while an ancestor in its own set also names it, in either nesting direction; with several overlaps the named pair is the first in the operator's written order | Before the engine and the probe | `TestSetupRefusesBeforeEngineAndProbe` |
| Neither failure applies | Startup proceeds unchanged | `TestSetupValidPolicyProceeds` |

The refusal ordering is asserted directly rather than inferred: the test supplies
a store factory and an engine that fail if they are called at all, so a refusal
that happened too late fails the test rather than passing quietly.

### Documentation carried by this phase

The help text is the authoritative copy of the flag table, so it changes here
along with the operator documentation and the accepted contract. Each is an
acceptance row rather than a reminder.

- The help text gains the flag line, in the existing column layout.
- The operator running guide gains the setting beside the read-only one.
- The accepted contract's configuration table gains the row.
- The repository agent guide's flag notes gain the startup-refusal property, and
  its invariant list gains the composed rule: a process configured with either
  set never publishes a change to a path the policy protects.

### Files

- `internal/app/config.go` — the flag, the environment variable, the resolved
  `writablePaths` field on the unexported config struct (its owner row in the
  README parameters table), the split, the normalization, the overlap check, and
  the help text line. The exported `ServiceConfig` field it feeds is Phase 2's.
- `internal/app/config_test.go` — the resolution and refusal tables.
- `internal/app/service.go` — pass the resolved entries through to the notebook.
- `internal/integrationtest/scenario_writable_test.go` — the flag-process
  scenarios, appended to the file Phase 2 created.
- `cmd/commit/commit_test.go`, `cmd/pull/pull_test.go` — the per-command flag
  acceptance, beside the existing read-only ones.
- `docs/running.md` — the operator entry.
- `docs/slivingdoc-v1.md` — the configuration table row.
- `AGENTS.md` — the flag note and the invariant.

## Integration contract

The scenarios live where the read-only ones live: the flag-process scenarios in
`internal/integrationtest/scenario_writable_test.go` beside
`TestScenarioReadOnlyFlagProcess` and `TestScenarioReadOnlyInvalidFlagRefusesStartup`,
and the per-command flag acceptance in the command packages beside
`TestCommitAcceptsReadOnlyFlag` and `TestPullAcceptsReadOnlyFlag`.

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects | Test |
| --- | --- | --- | --- | --- | --- |
| `commit` with a writable set, editing inside it | Real engine, fake store | Success report with the generation and the resolved notebook path | The edit is published | None | `TestScenarioWritableFlagCommitInside` |
| `commit` with a writable set, editing outside it | Real engine, real store; an in-process writer seeds the accepted baseline | The structured error report with a nonzero exit, naming the writable entries | The touched file is reset | Remote generation unchanged | `TestScenarioWritableFlagCommitOutsideRefused` |
| `pull` with a writable set and a locally edited protected path | Real engine, real store; an in-process writer seeds the accepted baseline | Success report; the protected path holds the remote content | The restore appears in the report | Writable edits untouched | `TestScenarioWritableFlagPullRestores` |
| Any command with a path named by both sets | Engine and store factory that fail when called | A nonzero exit naming the path and both settings | None | No engine method is called; the store is not constructed or probed | `TestScenarioWritableOverlapRefusesStartup` |
| Any command with the flag passed explicitly empty and the environment variable set | Real engine, fake store | The process behaves as if no writable set were configured | None | The environment value is not consulted | `TestScenarioWritableFlagExplicitEmptyIgnoresEnvironment` |
| `serve`, `pull`, and `commit` each with the flag | — | All three accept it | None | No command rejects it as unknown | `TestCommandsShareWritablePathsFlag`, `TestCommitAcceptsWritablePathsFlag`, `TestPullAcceptsWritablePathsFlag` |

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| The setting resolves by flag, then environment, then default | `TestFlagsWritablePathsResolution` |
| An explicitly empty flag value does not consult the environment | `TestFlagsWritablePathsExplicitEmptyIgnoresEnvironment` |
| The value splits and trims exactly as the read-only value does | `TestFlagsWritablePathsSplitting` |
| All three commands accept the flag | `TestCommandsShareWritablePathsFlag`, `TestCommitAcceptsWritablePathsFlag`, `TestPullAcceptsWritablePathsFlag` |
| An invalid entry or an overlap refuses startup before the engine opens and before the store is probed | `TestSetupRefusesBeforeEngineAndProbe` |
| The overlap message names the path and both settings | `TestSetupRejectsOverlapNamingBothSettings` |
| The help text carries the flag line in the existing layout | `TestHelpTextWritablePathsLine` |
| The operator guide, the accepted contract's configuration table, and the agent guide all carry the setting | `grep -l 'writable-paths' docs/running.md docs/slivingdoc-v1.md AGENTS.md` naming all three, recorded in Implementation notes |
| A valid configuration reaches the notebook with both sets intact | `TestSetupPassesBothSetsToNotebook` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| An entry fails path validation | Startup refuses, naming the offending entry and the set it belongs to, through the `writable paths:` / `read-only paths:` prefix | `TestSetupRejectsInvalidWritableEntry` |
| A path is named by both sets | Startup refuses, naming the path and both settings | `TestSetupRejectsOverlapNamingBothSettings` |
| The same path is named by both sets differing only in letter case | Startup refuses identically, since matching folds case | `TestSetupRejectsCaseFoldedOverlap` |
| A path is named by both sets and an ancestor in its own set names it too | Startup refuses identically: the entries compared are the written ones, in either nesting direction | `TestSetupRefusesBeforeEngineAndProbe` |
| The environment variable holds an invalid entry and no flag is given | Startup refuses with the same message as the flag path | `TestSetupRejectsInvalidEntryFromEnvironment` |
| The value is entirely separators or whitespace | Resolves to an empty set rather than to an invalid entry | `TestFlagsWritablePathsSplitting` |
| A refusal would otherwise happen after the engine opened | The test's engine and store fail when called, so the ordering regression fails the test | `TestSetupRefusesBeforeEngineAndProbe` |

## Implementation notes

### 2026-09-18 — Phase 3 executed — Claude Opus 5 (1M context), [[worklog-work]]

Deltas only; the specification is not restated.

**Deviations and decisions**

| # | Where | What and why |
| --- | --- | --- |
| 1 | `internal/app/config.go` | `finish` validates the read-only set with `git.NormalizeEntries` **before** composing the policy with `git.NewPolicy`. The composed constructor names the offending side in its own text (`invalid writable path ...`), but the config layer prefixes each side with the setting it came from, and the existing `TestLoadConfigReadOnlyPathsInvalid` asserts the read-only diagnostic byte-for-byte (`read-only paths: invalid read-only path ...`, with no `git:` prefix). The pre-check keeps that string unchanged and leaves `resolvePolicy` with three outcomes it can name: read-only entry, writable entry, overlap. |
| 2 | `internal/app/config.go` | The overlap diagnostic is built in the app layer, not wrapped from `git.OverlapError`: that type's text starts with the `git:` package prefix, which the configuration diagnostics never carry. The message is `--read-only-paths "X" and --writable-paths "Y" name the same path`, one form for both the identical and the case-folded case, so both written entries always reach the operator. |
| 3 | `internal/app/config.go` | `splitReadOnlyPaths`/`readOnlySeparator` renamed `splitPathEntries`/`pathEntrySeparator`. Both settings split identically; a second copy would be the duplication the conventions forbid, and the old names would have lied about the second caller. |
| 4 | `internal/app/config_test.go` | `TestLoadConfigDefaults` compares the whole `config` with `reflect.DeepEqual`, so it gained `writablePaths: []string{}` — the normalized empty set is non-nil, like the read-only one. |
| 5 | `internal/integrationtest/scenario_writable_test.go` | Phase 2's `writableRefusalMessage` constant became `writableRefusal(writable string)`. The CLI scenario configures a different writable entry, and the refusal text embeds the entries; parameterising the one literal beats a second copy of the same sentence. |
| 6 | `internal/integrationtest/scenario_writable_test.go` | Two of the three CLI rows of the integration contract run against the **real** backend rather than the fake, and seed R through an in-process writer harness. The stated side effects are not observable with the fake: each spawned process builds its own in-memory store, so there is no accepted baseline for "the touched file is reset" to reset *to*, and no remote generation for "remote generation unchanged" to be unchanged *from*. This is the repository's existing answer to the same problem — see the comment on `TestScenarioCLIReadOnlyCommit`. `TestScenarioWritableFlagCommitInside` and `TestScenarioWritableFlagExplicitEmptyIgnoresEnvironment` need no baseline and keep the fake. |
| 7 | `docs/slivingdoc-v1.md`, `docs/running.md` | Scoped to configuration: the section 17 table row and paragraph, the running-guide table row and a `## Writable paths` section. The advertised surfaces and the CLI report trailer are Phase 4's, so nothing here describes them. |

**Refusal ordering, asserted rather than inferred.** `refusingProcess` in
`internal/app/config_test.go` supplies an engine whose every method records the
call and fails, and a store factory that records and fails. Both oracles were
proved live with throwaway tests before being relied on: with a *valid* policy
the same process reaches `app: open native engine: ...` with `engine.touched =
true`, and with a fake engine it reaches the store factory. The process-level
scenario uses the suite's `bad-store` helper as its oracle; a throwaway run
confirmed that mode does print `INCOMPATIBLE_STORE` for `serve`, `pull`, and
`commit` when the configuration is valid, so its absence is evidence.

**Tests beyond the named ones.** None. Every test written for this phase is a
name the phase declares.

**Resolution table, executed**

| Input | Test | Result |
| --- | --- | --- |
| Flag set, environment set | `TestFlagsWritablePathsResolution/flag_set,_environment_set` | PASS ×3 |
| Flag unset, environment set | `TestFlagsWritablePathsResolution/flag_unset,_environment_set` | PASS ×3 |
| Flag unset, environment unset | `TestFlagsWritablePathsResolution/flag_unset,_environment_unset` | PASS ×3 |
| Flag explicitly empty, environment set | `TestFlagsWritablePathsExplicitEmptyIgnoresEnvironment` | PASS ×3 |
| Whitespace and an empty piece | `TestFlagsWritablePathsSplitting` | PASS ×3 |

**Startup-refusal table, executed**

| Failure | Test | Result |
| --- | --- | --- |
| Writable entry fails path validation | `TestSetupRejectsInvalidWritableEntry` (`..`, `/abs`, `docs/.git`) | PASS ×3, engine untouched, store not constructed |
| A path is named by both sets | `TestSetupRejectsOverlapNamingBothSettings` | PASS ×3, engine untouched, store not constructed |
| Named by both, differing only in letter case | `TestSetupRejectsCaseFoldedOverlap` | PASS ×3, engine untouched, store not constructed |
| Neither failure applies | `TestSetupValidPolicyProceeds` | PASS ×3, startup reaches the engine |

**Integration contract, executed**

| Trigger | Test | Result |
| --- | --- | --- |
| Commit inside the writable set | `TestScenarioWritableFlagCommitInside` | PASS ×3, exact `OK  generation 1  <path>` report with the `work/a.md` diffstat line |
| Commit outside the writable set | `TestScenarioWritableFlagCommitOutsideRefused` | PASS ×3, exit 1, exact structured report naming `work`, `shared/b.md` reset to the accepted bytes, `work/a.md` edit kept, manifest still generation 1 |
| Pull with a locally edited protected path | `TestScenarioWritableFlagPullRestores` | PASS ×3, exact report carrying the restore in the diffstat, protected path at the accepted bytes, writable edit untouched |
| A path named by both sets, any command | `TestScenarioWritableOverlapRefusesStartup` (serve, pull, commit) | PASS ×3, exit 1, empty stdout, stderr names the path and both settings, no `INCOMPATIBLE_STORE` |
| Explicitly empty flag with the environment set | `TestScenarioWritableFlagExplicitEmptyIgnoresEnvironment` | PASS ×3, a path the inherited value would protect publishes |
| All three commands accept the flag | `TestCommandsShareWritablePathsFlag`, `TestCommitAcceptsWritablePathsFlag`, `TestPullAcceptsWritablePathsFlag` | PASS ×3 |

**Acceptance criteria, checked**

| Outcome | Evidence |
| --- | --- |
| Resolves by flag, then environment, then default | `TestFlagsWritablePathsResolution` — PASS ×3 |
| An explicitly empty flag does not consult the environment | `TestFlagsWritablePathsExplicitEmptyIgnoresEnvironment` — PASS ×3; at the process boundary `TestScenarioWritableFlagExplicitEmptyIgnoresEnvironment` — PASS ×3 |
| Splits and trims exactly as the read-only value does | `TestFlagsWritablePathsSplitting` — PASS ×3 |
| All three commands accept the flag | `TestCommandsShareWritablePathsFlag`, `TestCommitAcceptsWritablePathsFlag`, `TestPullAcceptsWritablePathsFlag` — PASS ×3 |
| Invalid entry or overlap refuses before the engine and the probe | `TestSetupRefusesBeforeEngineAndProbe` (4 rows) — PASS ×3, both oracles proved live |
| The overlap message names the path and both settings | `TestSetupRejectsOverlapNamingBothSettings`, `TestSetupRejectsCaseFoldedOverlap` — PASS ×3; at the process boundary `TestScenarioWritableOverlapRefusesStartup` — PASS ×3 |
| The help text carries the flag line in the existing layout | `TestHelpTextWritablePathsLine` — PASS ×3; it asserts the exact three-line block and that the description and environment columns equal the read-only line's |
| Operator guide, accepted contract, and agent guide carry the setting | `grep -l 'writable-paths' docs/running.md docs/slivingdoc-v1.md AGENTS.md` prints `AGENTS.md`, `docs/slivingdoc-v1.md`, `docs/running.md` |
| A valid configuration reaches the notebook with both sets intact | `TestSetupPassesBothSetsToNotebook` — PASS ×3 (the resolved entries on the `ServiceConfig` the notebook is built from, and on both `Runtime` accessors, with `docs/open` **not** collapsed into `docs`); end to end, the flag-configured `TestScenarioWritableFlagCommitOutsideRefused` is refused by exactly that policy |

**Error coverage, executed**

| Failure | Test | Result |
| --- | --- | --- |
| An entry fails path validation | `TestSetupRejectsInvalidWritableEntry` | PASS ×3, text is `writable paths: invalid writable path "<entry>": ...` with no `git:` prefix |
| A path is named by both sets | `TestSetupRejectsOverlapNamingBothSettings` | PASS ×3 |
| Named by both, case-folded | `TestSetupRejectsCaseFoldedOverlap` | PASS ×3, both written forms present |
| Invalid entry from the environment, no flag | `TestSetupRejectsInvalidEntryFromEnvironment` | PASS ×3, string-equal to the flag refusal |
| Value is entirely separators or whitespace | `TestFlagsWritablePathsSplitting` | PASS ×3, resolves to the empty set |
| A refusal that happened after the engine opened | `TestSetupRefusesBeforeEngineAndProbe` | PASS ×3 |

**Verification commands**

| Command | Result |
| --- | --- |
| `make lint` | clean: gofumpt, `go vet`, staticcheck v0.7.0, `go fix -diff` (one `go fix` finding on a new test, fixed: `strings.Split` to `strings.SplitSeq`) |
| `make test` | all packages ok, coverage 84.4 % (floor 70 %); `internal/integrationtest` 14.9 s of the 30 s budget, up from 13.1 s |
| `go test ./internal/app/ -race -count=3 -run 'TestFlagsWritablePaths\|TestSetup\|TestHelpText'` | ok. Each alternative is a prefix — Go's `-run` pattern is unanchored — and the three select eleven tests in this package. The row was first recorded with ellipses, which made the selection unrecoverable; written out and re-run 2026-09-19 it is `ok … 1.076s`, 81 PASS lines counting subtests over the three counts. Closes P5-N3 |
| `go test ./cmd/commit/ ./cmd/pull/ -race -count=3 -run AcceptsWritablePathsFlag` | ok |
| `go test ./internal/integrationtest/ -race -count=3 -run 'TestCommandsShareWritablePathsFlag\|TestScenarioWritableFlag\|TestScenarioWritableOverlap'` | ok |
| `grep -l 'writable-paths' docs/running.md docs/slivingdoc-v1.md AGENTS.md` | all three named |
| `go run . serve -h` | the flag block prints in the existing column layout |

**Maintenance contracts honored.** `docs/slivingdoc-v1.md` section 17 (table row
and paragraph) and `docs/running.md` (table row and the `## Writable paths`
section) in the same change as the flag; `AGENTS.md` gained the startup-refusal
property in its flag notes and the composed rule in its invariant list. No
generated artifact, migration record, or diagram is affected.

### 2026-09-19 — Phase 3 review-1 fixes — Claude Opus 5 (1M context), [[worklog-work]]

Deltas only; the specification is not restated.

**R1-01, the Phase 3 half.** Nothing in this phase's code changed. Phase 1 fixed
the construction — `NewPolicy` compares the written entries and collapses each
set only afterwards — and it changed no signature, error type, or message, so
`resolvePolicy` consumes exactly what it did: `git.NewPolicy(readOnly, writable)`
and `*git.OverlapError`. What this phase owed was the contract row and the
flag-level proof that the configuration the review named refuses startup at the
documented point. `TestSetupRefusesBeforeEngineAndProbe` gained three rows, all
going through `loadConfig` from real argument strings and the refusing engine and
store factory:

| Row | Configuration | Refusal |
| --- | --- | --- |
| Overlap covered by an ancestor of the read-only set | `--read-only-paths=docs,docs/open --writable-paths=docs/open` | `--read-only-paths "docs/open" and --writable-paths "docs/open" name the same path`, engine untouched, store unconstructed |
| Overlap covered by an ancestor of the writable set | `--read-only-paths=docs/open --writable-paths=docs,docs/open` | the same message, engine untouched, store unconstructed |
| Several overlaps | `--read-only-paths=notes,docs --writable-paths=docs,notes` | names `notes`, the first read-only entry the operator wrote |

The third row is not decoration: it pins which pair the message names, which is
the only observable the written-order comparison adds beyond "it refuses". The
existing rows carry no expected message and are unchanged.

**Non-vacuity, proved live.** `internal/git/policy.go` was reverted in a throwaway
edit to compare the two sets *after* `collapseEntries` — the pre-fix shape. All
three new rows failed and the four existing ones passed, which is the review's
finding reproduced exactly. The several-overlaps row failed on the named pair
rather than on the refusal, because the collapsed set is sorted and named `docs`.
The file was restored from a byte copy and the rows pass again.

**R1-04.** `docs/running.md` gained one paragraph under the composed example
saying what else that configuration protects: the writable set is non-empty, so
`notes/`, files at the notebook root, and directories that do not exist yet are
read-only for that process too, not only the `docs` that `--read-only-paths`
names. The same section gained the written-entry clause on the overlap refusal,
matching what `docs/slivingdoc-v1.md` already states, since that is now
operator-visible startup behavior.

**P3-N1.** The collaborator column on the two CLI rows now reads "Real engine,
real store; an in-process writer seeds the accepted baseline", which is what
`TestScenarioWritableFlagCommitOutsideRefused` and
`TestScenarioWritableFlagPullRestores` do through `realCLIEnv` and
`seedWritableCLIBaseline`. No test changed.

**P3-N2.** The error-coverage row now asks the refusal to name the entry and the
*set* it belongs to, through the `writable paths:` / `read-only paths:` prefix,
which is what the phase's own prose already accepted and what
`TestSetupRejectsInvalidWritableEntry` already asserts. No test changed.

**Verification commands**

| Command | Result |
| --- | --- |
| `go test ./internal/app/ -race -count=3 -run TestSetupRefusesBeforeEngineAndProbe -v` | ok; 7 subtests × 3, all PASS |
| `go test ./internal/app/ -race -count=3` | `ok … 5.426s` |
| `go test ./internal/app/ -count=1 -run TestSetupRefusesBeforeEngineAndProbe` with the throwaway collapse-then-compare revert | FAIL on exactly the three new rows; PASS again after restore |
| `make lint` | clean: gofumpt listed no file, `go vet` clean, staticcheck clean, `go fix -diff` silent |
| `make test` | 18 `ok` packages, `== coverage: 84.5% (floor 70%) ==` |
| `make test`, two earlier runs | exit 2 on the root package alone: `panic: test timed out after 30s` in `TestReleaseBinary`'s in-suite `go build`, while `internal/git` and `internal/integrationtest` ran beside it. `go test . -race -count=3 -timeout=30s` alone is `ok … 3.255s`. This is R1-02, Phase 5's load-sensitivity note; nothing in this pass touches that package |
| `grep -l 'writable-paths' docs/running.md docs/slivingdoc-v1.md AGENTS.md` | all three named |

**Maintenance contracts honored.** `docs/running.md` is the only documentation
this fix pass changed; `docs/slivingdoc-v1.md` already carries the written-entry
rule from Phase 1's fix, and no flag, help line, or configuration table row moved.

## Review findings

### Review 1 — 2026-09-19 — reopened; every finding closed the same day, status `Complete`

**R1-01 — blocker — this phase's startup-refusal table and acceptance row "An
invalid entry or an overlap refuses startup before the engine opens and before
the store is probed".** The full finding is filed against Phase 1, where the
cause lives (`internal/git/policy.go:38-53`). It reaches this phase because the
contract that refuses startup is this phase's: a configuration in which the
operator named one path under *both* settings starts normally and resolves
silently whenever another entry of the same set covers the overlapping one, for
example `--read-only-paths=docs,docs/open --writable-paths=docs/open`, which
starts and makes `docs/open` writable. `TestSetupRefusesBeforeEngineAndProbe`
covers only the direct overlap, so the gap is invisible to this phase's suite.

- [x] Closed. The startup-refusal table has the row, and the error-coverage
      table a matching one; `TestSetupRefusesBeforeEngineAndProbe` gained both
      nesting directions and a several-overlaps row pinning which pair the
      message names. Proved non-vacuous against a throwaway revert of
      `NewPolicy` to collapse-then-compare: the three new rows failed, the four
      existing ones did not.

**R1-04 — note — `docs/running.md:248-254`.**
The composed example is introduced as "The two settings compose, and the longest
matching entry wins, so a protected region can hold a writable subdirectory" and
then shows `--read-only-paths docs --writable-paths docs/drafts`. It does not say
that, because the writable set is now non-empty, *every* path outside
`docs/drafts` — `notes/`, `team/`, files at the notebook root, directories that
do not exist yet — is protected as well. An operator reading only this section
would reasonably expect `docs` to be the protected region. The inversion is
stated correctly in `docs/slivingdoc-v1.md:345-350`; it is missing exactly where
it is most likely to be misread, and this is a security-shaped setting.

- [x] Closed. `docs/running.md` now says, under the composed example, that the
      non-empty writable set makes `notes/`, notebook-root files, and
      directories that do not exist yet read-only for that process as well —
      not only the `docs` that `--read-only-paths` names.

**P3-N1 is real and should be closed rather than carried.** The integration
contract's collaborator column says "fake store" for all three CLI rows, while
`TestScenarioWritableFlagCommitOutsideRefused` and
`TestScenarioWritableFlagPullRestores` run against the real backend with an
in-process writer seeding R — which is the only way their stated side effects are
observable, and is what `TestScenarioCLIReadOnlyCommit` already does.

- [x] Closed. Both rows now read "Real engine, real store; an in-process
      writer seeds the accepted baseline", which is what the two scenarios do.

**P3-N2 is real but minor.** The error-coverage row asks the refusal to name
"which setting it came from" and the implementation names the *set* through the
`writable paths:` / `read-only paths:` prefix rather than the flag versus the
environment variable. The phase's own prose already accepts this. Threading
provenance through resolution would be a new convention no other setting follows;
tightening the row's wording is the cheaper correction.

- [x] Closed. The row now asks for the entry and the set it belongs to, naming
      the prefix that carries it. No behavior changed.

### Verified good

- **The refusal ordering oracle is real, not decorative.** `refusingProcess`
  supplies an engine whose every method records and fails and a store factory
  that records and fails; `assertUntouched` asserts both. All four rows —
  invalid writable entry, invalid read-only entry, exact overlap, case-folded
  overlap — refuse with the engine untouched and the store unconstructed.
  `TestSetupValidPolicyProceeds` is the live control: the same process with a
  valid policy does reach the engine.
- **Resolution and splitting reuse the existing mechanism.**
  `splitPathEntries`/`pathEntrySeparator` replace the read-only-specific names
  and both settings resolve through the same `resolveString` precedence, so the
  explicitly-empty-flag idiom is inherited rather than re-implemented. Asserted
  at the unit boundary and again across the process boundary.
- **The read-only diagnostic is preserved byte-for-byte.** `resolvePolicy`
  pre-validates the read-only set with `git.NormalizeEntries` so the existing
  `read-only paths: invalid read-only path "..": …` text is unchanged, and the
  overlap diagnostic is built in the app layer without the `git:` prefix the
  configuration layer never carries.
- **The flag is genuinely shared.** `TestCommandsShareWritablePathsFlag` looks
  the flag up on the real `serve`, `pull`, and `commit` flag sets, and both
  command packages assert the resolved entries reach `Runtime` with `docs/open`
  *not* collapsed into `docs`.
- **The help text, the operator guide, the accepted contract and the agent guide
  all carry the setting**; `grep -l 'writable-paths' docs/running.md
  docs/slivingdoc-v1.md AGENTS.md` names all three, re-run during this review.

### Review 2 — 2026-09-19 — status `Complete` (no new finding against this phase)

**R1-01's Phase 3 half is closed and the closure was verified independently.**
`TestSetupRefusesBeforeEngineAndProbe` (`internal/app/config_test.go:897-932`)
now carries seven rows, three of them the collapsed overlap: the read-only
ancestor beside it, the writable ancestor beside it, and the several-overlap
configuration whose message must name the first *written* read-only entry. Each
row goes through `setup` from real argument strings against `refusingProcess`,
whose engine and store factory fail on any call, and `assertUntouched` proves
both were untouched. `TestSetupValidPolicyProceeds` is the live control. Read
the code rather than the notes: `loadConfig` → `finish` → `resolvePolicy`
(`internal/app/config.go:269-296`) runs before `p.engine.Open()`
(`internal/app/app.go:273`) and before `buildService` constructs the store, so
README invariant 5's ordering clause holds structurally and not only by test.

**R1-04 is closed.** `docs/running.md:255-259` now states the inversion under
the composed example — `notes/`, notebook-root files and directories that do not
exist yet are read-only for that process too — and `:266-269` carries the
written-entry rule for the overlap refusal.

**P3-N1 and P3-N2 are closed** as review 1 proposed: the two CLI
integration-contract rows now name the real store with an in-process writer
seeding the baseline, and the error-coverage row asks for the set rather than
flag-versus-environment provenance.

**R2-01 touches this phase only as a consequence, not as a fault.**
`resolvePolicy` hands `git.NewPolicy` the raw split entries, which is correct and
is what makes the startup refusal see the written entries. The resolution defect
is entirely inside `internal/git`. One thing to watch when it is fixed:
`finish` overwrites `cfg.readOnlyPaths`/`cfg.writablePaths` with
`policy.ReadOnly()`/`policy.Writable()` — the **collapsed** sets — and those are
what reach `ServiceConfig` and, through it, `notebook.Config`. See R2-03.

### Verified good (review 2)

- **The refusal ordering oracle is real** — re-read `refusingProcess`,
  `setupRefusal` and `assertUntouched`; the engine records and fails on every
  method and the store factory records and fails, and both are asserted
  untouched on all seven rows.
- **The read-only diagnostic is still byte-for-byte**: `resolvePolicy`
  pre-validates the read-only set with `git.NormalizeEntries` so
  `read-only paths: invalid read-only path "..": …` is unchanged, and the
  overlap message is built in the app layer without the `git:` prefix.
- **`git diff --stat Makefile` is empty** and no flag default moved.
