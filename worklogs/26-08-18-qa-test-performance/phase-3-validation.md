# Phase 3 — Validation and docs

**Status:** Complete

**Worklog:** [README](README.md)

## Goal

Prove the optimization keeps QA complete and materially improves timing stability.

## Specification

Run the repository's unchanged format, static analysis, Go test, duplicate,
and npm gates. Record cold/warm timing evidence for the original and shared
S3 paths, along with coverage. Update `docs/testing.md` to describe the one
broker-owned container used by `make test` and the direct-`go test` fallback.

## Integration contract

unit-test-only for the documentation surface; the full gate proves behavior.

## Acceptance criteria

- [x] `make lint`, `make test`, duplicate check, and npm tests pass. — lint clean; `make test` 33.08s, 82.6% coverage; `dupl` found 0 groups; bundled Node ran all 35 launcher tests. Literal `make qa` reaches npm but the desktop has no `npm` executable.
- [x] `make test` retains race, count, timeout, full package scope, and the coverage floor. — the Makefile Go command remains byte-for-byte the required flags and package pattern; coverage is 82.6%.
- [x] Timing evidence demonstrates an improvement or documents that Phase 2 is not justified. — legacy direct control 42.58s; shared broker 33.08s; Phase 2's contract audit found no safe merge.
- [x] Testing documentation matches the lifecycle implementation. — `docs/testing.md`, `Makefile`, and real-S3 package comments describe the broker and direct-Go fallback.

## Error coverage

| Failure | Expected result | Evidence |
| --- | --- | --- |
| Docker unavailable | Nonzero, actionable gate failure | `tests3` unavailable test / manual check |
| Node unavailable in this desktop environment | Reported separately; no Go result is claimed as full QA | Exact npm command result |

## Implementation notes

2026-08-18 — `make lint` passed. `go run github.com/mibk/dupl@v1.0.0 -t 80 .`
reported zero clone groups. `make test` passed twice after the ready-file
change; the final run was 33.08s, with all packages passing and 82.6%
coverage. The exact npm script (`node --test "test/*.test.mjs"`) passed 35/35
with the bundled Node v24.19.0 under normal local-fixture permissions. Literal
`make qa` passed lint and the full Go gate, then failed only because `npm` is
not installed in this desktop environment.
