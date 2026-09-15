# Phase 5 — Quality gate

**Status:** Not Started

**README:** [README.md](README.md)

## Goal

Every repository gate passes on the finished effort and every document that
describes the shipped behavior agrees with the code.

## Specification

- Run the README's validation policy in full: `make qa`, the formatter,
  staticcheck, `go vet`, `go fix -diff`, and the dupl signal. Fix every
  finding at its source; never add a skip, a build tag, or a subset
  command. Review each dupl report under the AGENTS.md duplication policy
  and record the verdict per clone in the implementation notes.
- Coverage: `make test` holds the floor named in the README parameters. If
  the new packages lower it, add unit tests to the phase that owns the
  uncovered code, not to this phase.
- Document coherence sweep, recorded as a checklist in the implementation
  notes:
  - every token in the README's reason, file-reason, and action tables
    appears in the contract's product-contract section and in the code
    constants, with no extra or missing token in either direction;
  - the flag reference in `internal/app/config.go`, `docs/running.md`, the
    contract configuration section, and `AGENTS.md` describe the same flag,
    variable, separator, and default;
  - the contract's decisions section carries the new decision;
  - `AGENTS.md`'s invariants and error-taxonomy paragraphs mention the
    read-only invariant and the additive fields;
  - the operation-results worklog is untouched (older worklogs are never
    rewritten).
- Run the README readiness checklist commands once more against the
  finished phase files and record the outcome.
- Flip every phase status and the README status board to `Complete` with a
  one-line outcome each, and write the session-journal entry.

## Integration contract

unit-test-only

## Acceptance criteria

| Outcome                       | Evidence                                                               |
| ----------------------------- | ---------------------------------------------------------------------- |
| All gates green               | `make qa` output pasted into the implementation notes                  |
| Formatter and analyzers clean | the formatter, staticcheck, `go vet`, and `go fix -diff` print nothing |
| Dupl reviewed                 | per-clone verdicts in the implementation notes                         |
| Coverage floor held           | the coverage line of `make test`                                       |
| Documents coherent            | the checklist above, each item ticked with the file checked            |
| README and phases closed      | status board rows `Complete`; journal entry dated                      |

## Error coverage

| Failure                                    | Expected outcome                                             | Test                    |
| ------------------------------------------ | ------------------------------------------------------------ | ----------------------- |
| a gate fails                               | fixed at the source in the owning phase; the run is repeated | `make qa`               |
| a token exists in code but not in the docs | docs updated in the owning phase's sections; sweep repeated  | the coherence checklist |
| coverage below the floor                   | tests added to the owning phase; run repeated                | `make test`             |

## Implementation notes

Not started.

## Review findings

None.
