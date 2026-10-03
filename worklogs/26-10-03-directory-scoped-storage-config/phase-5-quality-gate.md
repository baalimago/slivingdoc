# Phase 5: the quality gate

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Prove the whole effort against the repository's own gate, with no skipped test
and no narrowed run.

## Specification

This phase changes no behavior. It runs the gate, reads its output against the
criteria below, and records the result in this file's implementation notes.

### The sweep

| Step | Command | Passes when |
| --- | --- | --- |
| Format | `go run mvdan.cc/gofumpt@v0.11.0 -l .` | nothing is listed |
| Vet | `go vet ./...` | no output |
| Fix | `go fix -diff ./...` | nothing is printed |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` | no diagnostics |
| Test | `make test` | every package passes at the coverage floor |
| npm | `npm test --prefix npm/slivingdoc` | every check passes |
| Duplication signal | `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | every reported clone carries one of its acceptance clauses, or is fixed |

Tool versions are the repository's pinned ones and are not substituted. There
is no build tag, environment variable or flag that runs a subset.

### What the sweep must also confirm

| Check | Evidence |
| --- | --- |
| Every test name in Phases 1 to 4 exists and passes | the test output |
| Every test name is declared in exactly one phase | this worklog's tables |
| No phase file carries a numeral with a unit outside an oracle row | the checklist's grep over the phase files |
| Every architecture doc row in Phase 4 is present in the tree | the docs' own diff |
| Coverage is at or above the floor | the `make test` output |
| The new package appears in the package map | `AGENTS.md` |

## Integration contract

unit-test-only: the sweep proves the earlier phases at their own boundaries and
adds no event chain of its own.

## Acceptance criteria

| Criterion | Evidence |
| --- | --- |
| Every step above passes | the recorded output |
| Coverage is at or above the floor | `make test` |
| The duplication signal is read, and each clone is either fixed or justified by a clause of the duplication policy | this phase's notes |
| The readiness checklist passes against the tree as it stands | this phase's notes |

## Error coverage

unit-test-only: a failing gate is the failure path, and it is fixed in the
phase that owns the code, never worked around here.

## Implementation notes

Not started.

## Review findings

None.