# slivingdoc release-binary e2e worklog

**Status:** Not Started

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md),
[`../../docs/testing.md`](../../docs/testing.md)

## Objective

Add one top-level `main_e2e_test.go` that builds the release binary and runs
a full `pull` → edit → `commit` → `pull` round trip plus a conflict scenario
against real MinIO. The test validates the exit code, the stdout report, the
materialized visible files, and the redaction invariants of the actual
shipped executable — the one runtime seam the current release layer does not
exercise.

## Status board

| Phase                                                  | Status      | Summary                                                                                       |
| ------------------------------------------------------ | ----------- | --------------------------------------------------------------------------------------------- |
| [1. Release binary e2e](phase-1-release-binary-e2e.md) | Not Started | `main_e2e_test.go` over `releaseBinary()` and `testminio`: round trip and conflict scenarios. |
| [2. Quality gate](phase-2-quality-gate.md)             | Not Started | Full `make qa` sweep and the `docs/testing.md` release-layer update.                          |

## Strategy

### Execution order

Phase 1 implements the test and its helpers. Phase 2 runs the full quality
gate and updates the testing documentation. Phase 2 depends on Phase 1.

An executing agent reads this README and only the phase file it works on.
Shared rules live here so they are not duplicated per phase.

### Required architecture sections

| Phase | Required contract sections                       |
| ----- | ------------------------------------------------ |
| 1     | §17 flags and environment, testing release layer |
| 2     | §17 flags and environment, testing release layer |

Re-verify section references after any `docs/` edit.

### Shared invariants

Every phase preserves:

1. `os/exec` and `syscall` stay absent from the module. The e2e starts the
   binary with `os.StartProcess`, as `release_test.go` already does, so the
   no-Git-executable seam scan in `internal/git2` keeps passing.
2. The inherited developer AWS and slivingdoc environment never reaches the
   binary. The e2e strips those variables and sets only the local MinIO
   credentials, endpoint, path style, bucket, prefix, and roots.
3. No test output asserts anything weaker than the real artifact: exit code,
   exact `OK` stdout on success, the structured conflict report on a domain
   error, the materialized visible bytes, and the redaction of the MinIO
   secret, private root, pack keys, and Git IDs.
4. The release binary is built at most once per test process via the
   existing `releaseBinary()` cache, so `-count=3` links libgit2 once. The
   root package gains exactly one MinIO container per test process through
   `testminio.Ensure`, matching the other MinIO suites.
5. The existing `internal/integrationtest` black-box suite remains the
   behavioural contract. This e2e adds wiring coverage only; it must not
   duplicate the scenario matrix.

### Review severity

| Severity | Meaning                                                                 | Phase effect                                |
| -------- | ----------------------------------------------------------------------- | ------------------------------------------- |
| Critical | Credential leak, real-AWS contact, or an unsafe release artifact.       | Reopen and block dependent phases.          |
| Major    | Acceptance contract or an architecture invariant is not met.            | Reopen the phase.                           |
| Minor    | Local maintainability or documentation defect with no contract failure. | Record and fix without mandatory reopening. |

Every review appends findings to the phase file. Critical and major findings
change that phase to `Reopened (review N)` and update this status board.

## Decisions

| Date       | Decision                                                                                                                           | Reason                                                                                                                          |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-15 | Put the e2e in a new top-level `main_e2e_test.go` (`package main`, `!windows`), not inside `internal/integrationtest`.             | The unique seam is the built release binary; the black-box suite already covers the in-process and helper-binary paths.         |
| 2026-08-15 | Reuse `releaseBinary()`, the existing `TestMain` cleanup, and a new env-aware process runner rather than reimplementing the build. | The build cache and `os.StartProcess` runner already exist in `release_test.go`; only env passing is missing.                   |
| 2026-08-15 | Add `startAndWaitEnv(name, env, args...)` and keep `startAndWait` as a thin wrapper over it.                                       | A wrapper keeps every existing release test call site unchanged while letting the e2e pass a sanitized env.                     |
| 2026-08-15 | Cover two scenarios: one success round trip and one conflict report.                                                               | Two scenarios prove both the success and error halves of the CLI report on the real binary without duplicating the full matrix. |
| 2026-08-15 | Strip inherited AWS and slivingdoc variables with a dedicated sanitize helper in the root test package.                            | The release binary must never observe the developer's cloud configuration during a local e2e.                                   |

## Session journal

| Date       | Entry                                                                                                                                                                                                                                                                                                                                                           |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-15 | Investigated the existing release layer (`release_test.go`: `releaseBinary`, `startAndWait`, `TestMain`) and the black-box process harness (`internal/integrationtest`: helper-mode spawn, real-MinIO CLI scenarios). Confirmed the only untested seam is the built release binary performing a real notebook operation against S3; everything else is covered. |

## Feedback index

No findings recorded.
