# Phase 4 — Advertisement and CLI report

**Status:** Complete

[← README](README.md)

## Goal

Make every surface an agent or an operator reads name where writing is allowed,
so the rule is learned before the first edit and not only in the refusal, and so
a host that drops one surface still leaves the others.

## Specification

### What each surface says

The read-only set is advertised on four agent-facing surfaces and one operator
surface. Each gains the writable set beside it. The governing rule is that the
text names the **actionable** list: under a default-protected policy the
protected region is nearly the whole notebook, so naming it would be both useless
and enormous, while the writable entries are short by construction.

| Set state | Text names | Test |
| --- | --- | --- |
| Writable empty, read-only non-empty | The read-only entries, in today's wording, byte-for-byte | `TestAdvertisementReadOnlyWordingUnchanged` |
| Writable non-empty, read-only empty | The writable entries, as where writing is allowed | `TestAdvertisementNamesWritableEntries` |
| Both non-empty, disjoint | Both, writable first, since the writable region is the frame and the read-only entries are the exceptions inside it | `TestAdvertisementBothSetsWritableFirst` |
| Both non-empty, nested | The same order, plus the rule that decides between them on every surface that names both. A sentence naming one set alone contradicts the other under nesting: the read-only sentence therefore ends in the longest-match rule instead of "write elsewhere", which a non-empty writable set makes false. Pinned over a three-level configuration, because a two-level pair is the shape that hid R2-01 | `TestAdvertisementNestedSetsStateRule`, `TestReportNestedSetsStateRule`, `TestScenarioWritableThreeLevelAdvertisementStatesRule` |
| Both empty | Neither; the text is the bare form it is today | `TestAdvertisementUnconfiguredTextUnchanged` |

Entries are joined with the separator named in the README parameters table, which
is the one every surface already uses.

### The surfaces

| Surface | Change |
| --- | --- |
| Server instructions | One sentence naming the set and the consequence of writing elsewhere |
| Both tool descriptions | One sentence naming the set |
| Success result | A writable array beside the existing read-only array |
| Error result | The same array, on every domain error |
| Success text item | The parenthesized set form beside the resolved path |
| Error text item | The writable trailer above the existing read-only one, and the rule line below both when both sets are configured, so a text-only host learns where writing is allowed from every domain error rather than from the refusal alone (review 1, R1-06) |
| Operator report | A trailer line, rendered dim on a terminal like the existing one, on both the success and the error report |

The service interface the server reads widens by one accessor, which Phase 2
already put on the service and the notebook.

### Structured fields

The existing read-only array keeps meaning exactly what it means today: the
normalized read-only entries, never the protected region. The new array carries
the normalized writable entries. Both are additive, always present, and empty
when their set is unconfigured, matching how the existing array is specified in
the repository agent guide.

| Property | Test |
| --- | --- |
| The writable array is present on every success result | `TestSuccessResultCarriesWritableArray` |
| The writable array is present on every domain error result | `TestErrorResultCarriesWritableArray` |
| Both arrays are empty rather than absent when unconfigured | `TestResultsArraysEmptyWhenUnconfigured` |
| The read-only array never carries a writable entry | `TestResultsReadOnlyArrayKeepsMeaning` |

### Consumer compatibility

The known downstream consumer decodes the operator report leniently: it ignores a
trailer line it does not recognize and requires only the retryable trailer to
treat a report as a domain error, and it matches the success status line by
pattern rather than by position. A new trailer line and a new structured field
are therefore invisible to a client pinned to the current release, which is why
this phase adds fields and a line rather than changing either existing shape.

This is recorded as evidence, not as a test: the consumer is another repository
and its behavior is pinned by its own suite. It is also unpinned here — the
README bullet it comes from records no repository or revision — so it justifies
nothing on its own. What makes this phase safe is the shape of the change: a new
field and a new line, with no existing field or line altered, which the tables
above assert directly.

### Documentation carried by this phase

The accepted contract's read-only section gains a mirrored writable subsection
covering resolution, the unmatched default, the composition rule, the exact
overlap refusal, and the advertisement surfaces. The sentence promising that an
unconfigured process is byte-for-byte unchanged except for the always-present
empty array is extended to cover both arrays.

### Files

- `internal/mcp/server.go` — the instructions sentence, both description
  suffixes, the success text form, the error text trailers, and the widened
  service interface.
- `internal/notebook/notebook.go` — the terse form of the composition rule,
  beside the list separator the same surfaces already share, so the server and
  the report cannot drift apart on it.
- `internal/mcp/success.go` — the success structured field.
- `internal/mcp/errors.go` — the error structured field. There is no `error.go`;
  the field belongs in the file that already declares the read-only array, and it
  must be set at each of that file's construction sites, all of which assign an
  empty non-nil array today. Splitting it into a second file is how one site gets
  missed and ships `null`.
- `internal/app/command.go` — the report trailer on both renderers.
- `internal/mcp/server_test.go`, `internal/app/command_test.go` — the tables
  above.
- `internal/app/app_test.go` — the three fakes that satisfy the widened service
  interface (`fakeService`, `blockingService`, `cancelingService`) each gain the
  accessor. Widening the interface without them fails to compile package `app`;
  the accessor itself is Phase 2's, and these are its test doubles.
- `internal/integrationtest/scenario_writable_test.go` — the boundary assertions
  below, appended to the file Phase 2 created.
- `internal/integrationtest/harness.go` — `pathSetText`, the black-box oracle
  for the success text item, which mirrors the composed form independently.
- `internal/integrationtest/scenario_cli_test.go` — `runCLIOK`, whose success
  oracle takes the trailers each scenario expects (review 1, R1-07).
- `cmd/pull/pull.go`, `cmd/commit/commit.go` — the help text of both one-shot
  commands describes the report they print.
- `docs/slivingdoc-v1.md` — the mirrored subsection.
- `docs/running.md` — the operator guide describes the same report and the
  composed advertisement.

## Integration contract

These are the boundary scenarios the acceptance table names. Each row carries its
test, for the same reason Phase 2's do: Phase 5 resolves test names out of these
files, and an unnamed row is a row nothing proves.

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects | Test |
| --- | --- | --- | --- | --- | --- |
| An agent initializes against a process with a writable set | Real engine, fake store | The server instructions name the writable entries and the consequence of writing elsewhere | None | The instructions do not enumerate the protected region | `TestScenarioWritableInstructionsNameSet` |
| An agent lists tools against the same process | Real engine, fake store | Both tool descriptions name the writable entries | None | None | `TestScenarioWritableToolDescriptionsNameSet` |
| An agent pulls successfully | Real engine, fake store | The result carries both arrays and the text item names the set | None | The read-only array is unchanged in meaning | `TestScenarioWritableSuccessEnvelopeCarriesBothArrays` |
| An agent commits and is refused | Real engine, fake store | The error carries both arrays alongside the existing category, reason, action, and file entries | None | No existing field changes shape | `TestScenarioWritableErrorEnvelopeCarriesBothArrays` |
| An agent works against a process with neither set | Real engine, fake store | Instructions, descriptions, and text items are today's bare forms; both arrays are present and empty | None | No text mentions either set | `TestScenarioWritableUnconfiguredSurfacesUnchanged` |
| An operator runs the one-shot commit and is refused | Real engine, fake store | The report renders the existing status line, message, file entries, and next step, plus the trailer naming the set | None | Existing lines keep their order and wording | `TestScenarioWritableReportTrailerOnRefusal` |
| An agent initializes, lists tools and pulls against a process configured with a three-level composition | Real engine, fake store | Every text surface names both sets in the order above and ends in the rule that decides between them; the advertised entries are the ones the operator wrote, third level included | None | No surface tells the agent to write elsewhere while the writable set protects elsewhere | `TestScenarioWritableThreeLevelAdvertisementStatesRule` |

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| Each of the four agent-facing surfaces names the writable set when one is configured | `TestAdvertisementNamesWritableEntries`, `TestScenarioWritableInstructionsNameSet`, `TestScenarioWritableToolDescriptionsNameSet`, `TestScenarioWritableSuccessEnvelopeCarriesBothArrays`, `TestScenarioWritableErrorEnvelopeCarriesBothArrays` |
| Each surface keeps today's wording when only a read-only set is configured | `TestAdvertisementReadOnlyWordingUnchanged` |
| Both sets are named, writable first, when both are configured | `TestAdvertisementBothSetsWritableFirst` |
| A nested composition advertises both sets without contradicting itself, on every agent-facing surface and on the operator report | `TestAdvertisementNestedSetsStateRule`, `TestReportNestedSetsStateRule`, `TestScenarioWritableThreeLevelAdvertisementStatesRule` |
| The error text item names both sets, and names neither more than it did before when no writable set is configured | `TestErrorTextCarriesBothSets` |
| A process with neither set is byte-for-byte unchanged except for the always-present empty arrays | `TestAdvertisementUnconfiguredTextUnchanged`, `TestResultsArraysEmptyWhenUnconfigured` |
| The writable array appears on every success and every domain error | `TestSuccessResultCarriesWritableArray`, `TestErrorResultCarriesWritableArray` |
| The read-only array still carries only read-only entries | `TestResultsReadOnlyArrayKeepsMeaning` |
| The operator report renders the trailer on both the success and the error path, dim only on a terminal | `TestReportWritableTrailer` |
| The existing report lines keep their order and wording | `TestReportReadOnlyTrailerUnchanged` |
| The accepted contract carries the mirrored subsection | `grep -nE '^#+ .*[Ww]ritable' docs/slivingdoc-v1.md` naming the subsection heading, recorded in Implementation notes |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| A domain error is produced before the policy is consulted | The arrays are still attached, since they come from the service rather than from the failing operation | `TestErrorResultArraysOnEarlyFailure` |
| A recovery failure is returned | The arrays are attached alongside the recovery stage, with no existing field displaced | `TestErrorResultArraysOnRecoveryFailure` |
| The report is written to a pipe rather than a terminal | The trailer renders without styling, like the existing one | `TestReportWritableTrailerUnstyledOnPipe` |
| A set is empty while the other is not | Only the non-empty set appears in text; both arrays remain present | `TestAdvertisementOneSetEmpty` |
| A host forwards only the success text item and drops the structured content | The text item alone still names where writing is allowed | `TestSuccessTextWritableForm` |
| A host forwards only the text item of a domain error that is not the refusal | The error text names the writable set and the rule beside the read-only trailer; with no writable set the trailer is unchanged | `TestErrorTextCarriesBothSets` |

## Implementation notes

Session: worklog-work, Claude Opus 5, 2026-09-18.

### Wording the phase left to the implementation

The specification fixed which surface says what, not the sentences. What
shipped, with the writable form mirroring the read-only one it sits beside:

| Surface | Writable form |
| --- | --- |
| Instructions | ` Writable paths: <entries>. notes_commit refuses any change elsewhere, resets those files, and reports READ_ONLY_PATH; write only under them.` |
| Tool descriptions | ` Writable paths in this server: <entries>; changes elsewhere are refused and reset.` |
| Success text item | `<path> (writable: <entries>)`, and `<path> (writable: <entries>; read-only: <entries>)` when both are configured |
| CLI report | `writable: <entries>`, dim label, immediately above the read-only trailer |

The read-only sentences are untouched, so a read-only-only process is
byte-identical (`TestAdvertisementReadOnlyWordingUnchanged`). With both sets
the writable sentence precedes the read-only one on every surface, which is
what `TestAdvertisementBothSetsWritableFirst` asserts by index rather than by
suffix, since only one of the two can be the suffix.

### Deviations and decisions

- **The MCP error *text* item keeps only its read-only trailer.** The
  surfaces table puts the writable array on the error *result*, and names the
  text item only for the success result; Phase 2's refusal message already
  names the writable set in text ("Only notes is writable in this server…"),
  so a host that drops structured content still learns the rule on the
  refusal that matters. Adding a second trailer there would have been
  unspecified scope. Recorded as a note rather than implemented.
- **Files the phase did not list.** Widening `app.Report` by one parameter
  forces its two call sites (`cmd/pull/pull.go`, `cmd/commit/commit.go`), and
  widening the envelope forces the black-box harness that decodes it
  (`internal/integrationtest/harness.go`, `scenario.go`,
  `scenario_helpers_test.go`) and the CLI success oracle whose regexp pinned
  the totals line as the last line (`scenario_cli_test.go`). See P4-N1 in the
  README feedback index.
- **Three earlier scenarios were updated to the feature**, per the AGENTS.md
  rule on integration tests a feature legitimately changes:
  `TestScenarioWritableFlagCommitInside`,
  `TestScenarioWritableFlagCommitOutsideRefused`, and
  `TestScenarioWritableFlagPullRestores` now expect the trailer their
  configured processes emit, and `TestScenarioWritableCleanCommitUnchanged`
  now compares the operation's result with the advertised sets factored out,
  asserting the writable array separately — the configured and unconfigured
  envelopes can no longer be identical, by D7's design.
- **`MESSAGE_BLANK`, not `MALFORMED_INPUT`**, is the reason token of a blank
  commit message in the unconfigured scenario; the notebook classifies it
  before the strict decode's generic token.
- **The help text of both one-shot commands and `docs/running.md`** describe
  the report they print, so both gained the writable trailer in the same
  change.

### Oracles probed

Every oracle that could have passed vacuously was proved live by breaking the
implementation in a throwaway edit and watching the named test fail, then
restoring it:

| Broken on purpose | Test that caught it |
| --- | --- |
| Read-only description suffix placed before the writable one | `TestAdvertisementBothSetsWritableFirst` |
| `te.Writable` never assigned in `errorResult` | `TestErrorResultCarriesWritableArray`, `TestErrorResultArraysOnEarlyFailure`, `TestErrorResultArraysOnRecoveryFailure` |
| `writePathSets` dropped from `writeError` | `TestReportWritableTrailer` (error and coloured rows), `TestReportReadOnlyTrailerUnchanged` |

The CLI trailer proved itself the same way without an edit: the three Phase 3
flag scenarios failed on the exact report text until their expectations were
updated, which is the trailer being observed across the process boundary.

### Row-by-row evidence

Set-state table: `TestAdvertisementReadOnlyWordingUnchanged`,
`TestAdvertisementNamesWritableEntries`,
`TestAdvertisementBothSetsWritableFirst`,
`TestAdvertisementUnconfiguredTextUnchanged` — all pass.

Structured-field table: `TestSuccessResultCarriesWritableArray`,
`TestErrorResultCarriesWritableArray`,
`TestResultsArraysEmptyWhenUnconfigured` (which also asserts `"writable":[]`
in the raw structured content, so "empty rather than absent" is checked on the
wire), `TestResultsReadOnlyArrayKeepsMeaning` — all pass.

Integration contract: `TestScenarioWritableInstructionsNameSet`,
`TestScenarioWritableToolDescriptionsNameSet`,
`TestScenarioWritableSuccessEnvelopeCarriesBothArrays`,
`TestScenarioWritableErrorEnvelopeCarriesBothArrays`,
`TestScenarioWritableUnconfiguredSurfacesUnchanged`,
`TestScenarioWritableReportTrailerOnRefusal` — all pass, each against the real
engine over the fake store, the last one over spawned one-shot processes.

Error coverage: `TestErrorResultArraysOnEarlyFailure` (a decode failure that
never reaches the service, asserted by the fake recording no call),
`TestErrorResultArraysOnRecoveryFailure` (arrays beside an unchanged
`commit.cas` recovery report), `TestReportWritableTrailerUnstyledOnPipe`,
`TestAdvertisementOneSetEmpty`, `TestSuccessTextWritableForm` — all pass.

Documentation row: `grep -nE '^#+ .*[Ww]ritable' docs/slivingdoc-v1.md` prints
`336:### Writable paths`, the mirrored subsection covering resolution, the
unmatched default, composition, the exact-overlap refusal, and the
advertisement surfaces. The byte-for-byte sentence now reads "except the
always-present empty `readOnly` and `writable` arrays", and both envelope
examples and their always-present field lists carry `writable`.

### Verification commands

| Command | Result |
| --- | --- |
| `make lint` | clean (gofumpt, go vet, staticcheck, go fix) |
| `make test` | pass, coverage 84.5 % (floor 70 %) |
| `go test ./internal/mcp ./internal/app ./internal/integrationtest -run '<the 21 names above>' -race -count=3 -v` | 21/21 pass, three counts each |
| `grep -nE '^#+ .*[Ww]ritable' docs/slivingdoc-v1.md` | `336:### Writable paths` |
| Every cited test name resolved against `internal` and `cmd` | each resolves to exactly one `func Test…`, none underscored |

### Review-2 fix pass

Session: worklog-work, Claude Opus 5 (1M context), 2026-09-19. R2-02, R1-06
and R1-07 closed; nothing else in the phase re-opened.

**What the composed surfaces now say.** The single-set forms are untouched,
so invariant 4 and the writable-only wording are byte-for-byte what they
were. Only the composed forms changed, and each of them gained the one rule
that reconciles the two sentences:

| Surface | Composed form |
| --- | --- |
| Instructions | the writable sentence unchanged, then ` Read-only paths: <entries>. notes_commit refuses any change under them, resets those files, and reports READ_ONLY_PATH; where the two sets nest, the longest matching entry decides.` |
| Tool descriptions | the writable suffix unchanged, then `… changes under them are refused and reset, and where the two sets nest the longest matching entry decides.` |
| Success text item | `<path> (writable: <entries>; read-only: <entries>; longest match decides)` |
| Error text item | the existing body, then `writable: <entries>`, `read-only: <entries>`, `path-rule: longest match decides` |
| CLI report | `writable:`, `read-only:`, then a `path-rule:` trailer, all three dim-labelled |

**Deviations and decisions**

- **The read-only sentence loses "write elsewhere" whenever a writable set
  is configured.** That clause was the contradiction R2-02 names, and it was
  also false on its own terms: a non-empty writable set protects everything
  it does not cover, so "elsewhere" is exactly where the agent may not
  write. Replacing it is what makes the composed text true; stating the rule
  is what makes it useful.
- **The rule is stated whenever both sets are configured, not only when
  they actually nest.** Testing for nesting would give the advertisement two
  both-sets forms and make the wording depend on set contents, which is one
  more shape to specify and to prove. The rule is true of every composed
  configuration, so it is said in all of them; the disjoint case simply
  gains a clause it does not need.
- **R1-06 closed by implementing it, not by recording a decision against
  it.** The earlier decision — the refusal message is the text surface for
  errors — held only while the read-only trailer was the whole story. Under
  a nested composition, an error text carrying `read-only: notes` alone is
  misleading by omission about the one directory the agent uses, which is
  R2-02's defect on a surface R2-02 does not name. `errorText` now mirrors
  the report's trailer block, and the surfaces table carries the row.
- **The terse form lives in `internal/notebook`**, beside
  `ReadOnlyListSeparator`, because the MCP server and the CLI report both
  render it and neither imports the other.
- **`path-rule:` is a trailer of its own** rather than a qualifier inside
  the `read-only:` value, so a reader that parses a trailer value still gets
  the bare entry list. It is absent unless both sets are configured, which
  keeps every unconfigured and read-only-only report byte-for-byte
  unchanged.
- **Files the phase did not list**, each forced by the wording change:
  `internal/integrationtest/harness.go` (`pathSetText`, the black-box oracle
  for the composed text item — written out rather than imported, so it stays
  an independent expectation), `internal/integrationtest/scenario_cli_test.go`
  (`runCLIOK`, R1-07), `cmd/pull/pull.go` and `cmd/commit/commit.go` (both
  help texts describe the report), `docs/running.md` (the operator guide
  describes the same report and the composed advertisement), and
  `internal/notebook/notebook.go` (the shared terse form). Added to Files, as
  the Strategy rule promoted from P4-N1 requires.
- **Tests this change legitimately moved.**
  `TestAdvertisementBothSetsWritableFirst` keeps its disjoint sets and its
  index-based ordering assertion, and now expects the composed read-only
  wording. The both-sets expectations of `TestReportWritableTrailer`,
  `TestReportWritableTrailerUnstyledOnPipe`,
  `TestReportReadOnlyTrailerUnchanged` (its `withWritable` half only — the
  two read-only-only halves are untouched, which is the invariant-4 pin) and
  `TestScenarioWritableSuccessEnvelopeCarriesBothArrays` gained the new
  trailer or clause.

**Oracles probed.** Each new oracle was proved live by breaking the
implementation in a throwaway edit and watching the named test fail, then
restoring it:

| Broken on purpose | Test that caught it |
| --- | --- |
| The instructions' read-only tail put back to `; write elsewhere.` | `TestAdvertisementNestedSetsStateRule`, `TestAdvertisementBothSetsWritableFirst`, `TestScenarioWritableThreeLevelAdvertisementStatesRule` |
| The description's read-only tail put back to `.` | `TestAdvertisementNestedSetsStateRule`, `TestAdvertisementBothSetsWritableFirst` |
| The writable trailer dropped from `errorText` | `TestErrorTextCarriesBothSets` |
| The rule line dropped from `errorText` | `TestErrorTextCarriesBothSets` |
| The rule line dropped from `writePathSets` | `TestReportNestedSetsStateRule`, `TestReportWritableTrailer`, `TestReportWritableTrailerUnstyledOnPipe`, `TestReportReadOnlyTrailerUnchanged` |
| A stray `writable:` trailer emitted by `writePathSets` unconditionally | `TestScenarioCLIMarkerConflictReport`, through `runCLIOK` — the absent case the previous regexp tolerated (R1-07) |

**Verification commands**

| Command | Result |
| --- | --- |
| `make lint` | clean (gofumpt, go vet, staticcheck v0.7.0, go fix) |
| `make test` | 18 `ok` packages, 0 `FAIL`, `== coverage: 84.6% (floor 70%) ==` |
| `go test ./internal/mcp ./internal/app ./internal/integrationtest -run 'TestAdvertisementNestedSetsStateRule\|TestErrorTextCarriesBothSets\|TestReportNestedSetsStateRule\|TestScenarioWritableThreeLevelAdvertisementStatesRule' -race -count=3` | pass, three counts each |
| `grep -n 'path-rule' docs/slivingdoc-v1.md docs/running.md` | the report trailer, the error text item, the colour list, and the composed-advertisement paragraph |
| `grep -nE '^#+ .*[Ww]ritable' docs/slivingdoc-v1.md` | `346:### Writable paths` — the same subsection, moved down by this pass's additions above it |


## Review findings

### Review 1 — 2026-09-19 — status `Complete` (notes only)

**R1-06 — note — `internal/mcp/server.go:280-308`; this phase's "The surfaces"
table.** The MCP **error** text item ends with the `read-only:` trailer and never
names the writable set. A host that forwards only the text item therefore learns
where writing is allowed from the success text item and from the READ_ONLY_PATH
refusal message, but from no other domain error: a `MERGE_CONFLICT`,
`REMOTE_BUSY` or `MESSAGE_BLANK` returned by a process configured with both sets
shows `read-only: docs` and nothing about `notes`. This is P4-N2, which is real.
The phase's own error-coverage row for a text-only host names the *success* text
item only, so nothing shipped against the table — but the asymmetry is
undocumented on the surface that carries it.

- [x] Add the error text item to the surfaces table with its decision, or record
      on the table that the refusal message is the text surface for errors.
      Closed by implementing it rather than by deciding against it: the
      surfaces table carries the row, `errorText` emits the writable trailer
      above the read-only one and the rule line below both, and
      `TestErrorTextCarriesBothSets` pins the composed form and the unchanged
      read-only-only one.

**R1-07 — note — `internal/integrationtest/scenario_cli_test.go:82`.**
`runCLIOK`'s regexp changed from pinning the totals line as the final line to
`…deletions\(-\)\n(writable: [^\n]+\n)?(read-only: [^\n]+\n)?$`. The widening
is necessary, but it now also accepts a `writable:` trailer in the CLI scenarios
where no writable set is configured, which is exactly the regression the
unconfigured-surface guarantee exists to catch, and the failure message still
reads "want the totals trailer as the final line". The exposure is bounded —
`TestScenarioWritableReportTrailerOnRefusal` and the three Phase 3 flag scenarios
pin exact report text, and `TestReportReadOnlyTrailerUnchanged` pins the
read-only-only report byte-for-byte at the unit boundary — but the shared helper
is a looser guard than it was.

- [x] Pass the expected trailers into `runCLIOK` so the helper asserts the
      absent case too, or correct its failure message. Closed: the helper
      takes the exact trailer lines the scenario expects and matches the
      totals line followed by those lines and nothing else, so `nil` now
      forbids every path-set trailer; the failure message names the trailers
      it asserted. Probed: with a stray `writable:` trailer emitted
      unconditionally, `TestScenarioCLIMarkerConflictReport` fails on the
      helper, which the previous regexp tolerated.

**P4-N1 is the most useful of this effort's author notes and should be
promoted.** Listing only the production files a change starts in hides the
doubles and oracles a widened shape forces — here `cmd/pull/pull.go`,
`cmd/commit/commit.go`, `internal/integrationtest/harness.go`, `scenario.go`,
`scenario_helpers_test.go` and `scenario_cli_test.go`. It is V1-03 one layer out
and it is a planning rule, not a Phase 4 fact.

### Verified good

- **Invariant 4 holds on every surface.** With an empty writable set,
  `instructions`, both `*DescriptionSuffix` helpers, `successText`,
  `writePathSets` and `errorText` all produce byte-identical output to the
  previous code; `TestAdvertisementReadOnlyWordingUnchanged` asserts the exact
  text item `"/abs/notes (read-only: docs, faq.md)"` and forbids the substring
  `"Writable paths"` on the instructions and both descriptions, and
  `TestReportReadOnlyTrailerUnchanged` pins both report shapes byte-for-byte.
- **The writable array is present on every envelope.** All four `ToolError`
  construction sites in `internal/mcp/errors.go` (`:85`, `:99`, `:124`, `:213`)
  assign `Writable: []string{}`, `MapSuccess` does the same, and
  `h.errorResult`/`h.successResult` overwrite both from the service, which never
  returns nil. The black-box harness enforces it independently:
  `assertOK` fails on a nil `writable`, and `envelopeTokenViolation` treats a
  missing `writable` key as a violation, so a `null` could not pass unnoticed.
- **`readOnly` keeps its meaning.** `TestResultsReadOnlyArrayKeepsMeaning` runs a
  process with both sets configured and asserts the success and error arrays
  carry `[docs]` alone.
- **Writable precedes read-only everywhere**, asserted by index rather than by
  suffix in `TestAdvertisementBothSetsWritableFirst`, and the three
  break-it-on-purpose probes the phase records are the right ones.
- **The CLI trailer crosses the process boundary.**
  `TestScenarioWritableReportTrailerOnRefusal` spawns real one-shot processes and
  compares the whole refusal report string, including the reset of `team/b.md`
  from disk.

### Review 2 — 2026-09-19 — reopened; status `Reopened (review 2)`

**R2-02 — defect — `internal/mcp/server.go:223-259`, `internal/app/command.go:213-220`;
this phase's "What each surface says" table, row "Both non-empty".**
Under the *nested* composition — the configuration D1 exists for and README
success criterion 2 promises — the two sentences each surface emits contradict
each other about the one directory the agent is meant to use. With
`--read-only-paths notes --writable-paths notes/agent-a` the server
instructions read:

```
… Writable paths: notes/agent-a. notes_commit refuses any change elsewhere,
resets those files, and reports READ_ONLY_PATH; write only under them.
Read-only paths: notes. notes_commit refuses any change under them, resets
those files, and reports READ_ONLY_PATH; write elsewhere.
```

`notes/agent-a` *is* under `notes`, so sentence one says write only there and
sentence two says changes there are refused and the agent should write
elsewhere. Both tool descriptions carry the same pair
(`" Writable paths in this server: notes/agent-a; changes elsewhere are refused
and reset."` followed by `" Read-only paths in this server: notes; changes under
them are refused and reset."`), and the two CLI trailers stack the same way
(`writable: notes/agent-a` above `read-only: notes`). Neither surface states the
longest-match rule that reconciles them, and neither marks the read-only entry
as the outer region rather than an exception inside the writable one.

The set-state table's "Both non-empty" row is satisfied only by the *disjoint*
case: `TestAdvertisementBothSetsWritableFirst`
(`internal/mcp/server_test.go:846-858`) configures `readOnly=[docs, faq.md]`
against `writable=[notes, team.md]`, which share no prefix, and asserts nothing
but ordering. No test in the suite advertises a nested composition, so the
wording an agent will actually meet under the headline configuration is
unspecified and unproven.

This is the surface half of what R2-01 breaks in resolution: an operator reading
the advertisement cannot tell whether the nesting is honoured, and — while R2-01
stands — for a three-level configuration it is not.

- [x] Extend the "Both non-empty" row to say what a nested composition
      advertises, and add a set-state test that configures one
      (`readOnly=[notes]`, `writable=[notes/agent-a]`) and pins the text.
      Closed: the row is split into a disjoint and a nested case, and the
      nested one is pinned at three levels rather than two —
      `readOnly=[notes, notes/agent-a/locked]`, `writable=[notes/agent-a]` —
      by `TestAdvertisementNestedSetsStateRule` on the agent surfaces,
      `TestReportNestedSetsStateRule` on the operator report, and
      `TestScenarioWritableThreeLevelAdvertisementStatesRule` over the real
      boundary.
- [x] Make each surface's composed form self-consistent — e.g. state the
      longest-match rule once, or qualify the read-only sentence as "except
      where a writable path is named below them" — so an agent is not told two
      opposite things about the same directory. Closed: with a writable set
      configured the read-only sentence ends in the rule instead of "write
      elsewhere", on the instructions and on both tool descriptions, and the
      terse surfaces — both text items and the CLI report — carry the same
      rule as a third part or a third trailer.

**R1-06 — still open, still non-blocking.** Re-verified: `errorText`
(`internal/mcp/server.go:281-307`) appends `read-only:` and nothing else, so a
text-only host reading a `MERGE_CONFLICT`, `REMOTE_BUSY` or `MESSAGE_BLANK` from
a process configured with both sets still never learns the writable set from the
error text item. The judgement that this is a note stands: the refusal message
itself names the writable set and the structured `writable` array is present on
every error. Under R2-02 it gains a little weight — the error text is one more
surface whose composed form is not specified — but it does not block on its own.

**R1-07 — still open, still non-blocking.** Re-verified at
`internal/integrationtest/scenario_cli_test.go:82`: the oracle is still
`…deletions\(-\)\n(writable: [^\n]+\n)?(read-only: [^\n]+\n)?$`, so a
`writable:` trailer is still accepted in the unconfigured and read-only-only CLI
scenarios, and the failure message at `:84` still reads "want the totals trailer
as the final line" while the regexp no longer asserts that. The helper's doc
comment was updated; the assertion and its message were not. Judgement holds:
the exact-text scenarios bound the exposure, so it is a note.

### Verified good (review 2)

- **Invariant 4 still holds byte-for-byte.** With an empty writable set,
  `instructions`, both `*DescriptionSuffix` helpers, `successText`, `errorText`
  and `writePathSets` all short-circuit on `len(writable) > 0` before emitting
  anything, so a read-only-only or unconfigured process prints exactly what it
  printed before; `TestAdvertisementReadOnlyWordingUnchanged`,
  `TestAdvertisementUnconfiguredTextUnchanged` and
  `TestReportReadOnlyTrailerUnchanged` pin all three states. R2-02 is confined
  to the both-sets-configured case.
- **The `writable` array is still present on every envelope**: all four
  `ToolError` sites in `internal/mcp/errors.go` and `MapSuccess` assign
  `[]string{}`, and `h.errorResult`/`h.successResult` overwrite from the
  service's never-nil accessor.
- **Writable still precedes read-only on every surface** — descriptions
  (`server.go:74,79`), instructions, success text, and both report renderers.
