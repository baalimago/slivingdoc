# Phase 5 — Quality gate

**Status:** Not Started

[← README](README.md)

## Goal

Prove the whole effort against the repository's own gates, unedited, and prove
that every claim the earlier phases made is backed by a test that exists and
asserts something.

## Specification

### The gates

The gates are the repository's, listed in the QA validation table of the agent
guide, and this phase runs them as written. No target, build tag, environment
variable, or flag runs a subset. No skip is added, and the timeout, count, and
race settings of the test command are not modified. The tool versions are the
pinned baseline recorded in that table, never a floating latest.

| Gate | Source of truth |
| --- | --- |
| Format, static analysis, vet, and the fix diff | The QA validation table in `AGENTS.md` |
| The single Go test command, including race, repeat count, timeout, and the coverage floor | The same table |
| The single npm test command | The same table |
| Duplication signal | The same table, read against the duplication policy in `AGENTS.md` |
| The whole gate in one command | `make qa` |

Coverage is reported against the repository floor named in the README validation
policy. A figure below it fails the run; the achieved figure is recorded in
Implementation notes.

### Duplication verdicts

The duplication tool is a signal, not a verdict. Every clone it reports that
touches this effort's files is recorded in Implementation notes with a verdict
and the clause of the duplication policy that justifies it. Two clones are
expected and acceptable in advance:

| Expected clone | Policy clause |
| --- | --- |
| The counting repository fake in `internal/git` mirroring the fakes in `internal/notebook` and `internal/workspace` | Interface and fake mirroring: independent packages must stay independent |
| The writable advertisement helpers mirroring the read-only ones in the same file | Thin wrappers over a shared helper, where both delegate to the one joining routine |

A clone that is not one of these is assessed on its merits and either fixed or
recorded with the clause that excuses it. A clone recorded without a clause is a
defect.

### Evidence check

Every test name cited anywhere in the phase files must resolve to a declared
test, and every acceptance row must name one. The check is repeatable from the
worklog directory:

```sh
grep -ohE '\bTest[A-Za-z0-9_]+' phase-*.md | sort -u | while read -r name; do
  grep -rqE "func ${name}\(" ../../internal ../../cmd ../../*.go || echo "MISSING ${name}"
done
```

An empty result is the passing condition. The count of distinct names checked is
recorded in Implementation notes so a later reader can tell the check ran against
the whole set rather than a fragment.

### Maintenance contracts

The repository requires that a change touching an accepted invariant updates the
contract document in the same commit. Phases 3 and 4 each carry their own
document edits, so this phase verifies rather than performs them.

| Contract | Verified by |
| --- | --- |
| The accepted contract carries the writable subsection and the extended unchanged-process sentence | Reading `docs/slivingdoc-v1.md` against Phase 4's specification |
| The accepted contract's configuration table carries the setting | Reading the table against Phase 3's specification |
| The operator guide carries the setting | Reading `docs/running.md` against Phase 3's specification |
| The agent guide carries the flag note and the composed invariant | Reading `AGENTS.md` against Phase 3's specification |
| The help text carries the flag line, and it is the authoritative copy | Phase 3's acceptance row for the help text line |

### Readiness checklist

The README's readiness checklist is run here as well as by the author before
validation, and its outcome is recorded in Implementation notes. A line that
fails is a finding against the phase that owns the text, not a defect of this
phase.

## Integration contract

`unit-test-only`. This phase adds no behavior and therefore has no boundary of
its own. It runs the suites the earlier phases wrote, over the code they built.

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| The whole gate passes unedited | `make qa` output recorded in Implementation notes |
| Coverage is at or above the repository floor | The test command's coverage figure, recorded in Implementation notes |
| The test command's race, repeat count, and timeout are unmodified | The command line recorded in Implementation notes, compared against the agent guide |
| No skip, short guard, or second gate command was introduced | Reviewer check across the effort's test files, recorded in Implementation notes |
| Every test name cited in a phase file resolves to a declared test | The evidence-check command, with an empty result |
| Every acceptance row in every phase names a test or a repeatable command | Reviewer sweep of the four acceptance tables |
| Every duplication report touching this effort carries a verdict and a policy clause | The duplication verdict record in Implementation notes |
| Every maintenance contract above is satisfied | The contract table |
| The readiness checklist passes | Its recorded outcome |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| A gate fails | The phase stays open; the failure is recorded as a finding against the owning phase rather than worked around | The gate's own output |
| Coverage falls below the floor | The test command fails on its own; missing coverage is added to the owning phase's tests, never to a test written only to raise the figure | The test command |
| A cited test name does not resolve | The evidence check names it; the owning phase either declares the test or drops the citation | The evidence-check command |
| A duplication report has no justifying clause | The clone is refactored rather than recorded | The duplication tool's output |
| A document required by a maintenance contract was not updated | A finding against the phase that owns the document, which updates it in its own commit | The contract table |
| The readiness checklist fails a line | A finding against the phase that owns the offending text | The checklist |

## Implementation notes

Not started.

## Review findings

None.
