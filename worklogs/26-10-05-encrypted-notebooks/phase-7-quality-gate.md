# Phase Seven: quality gate

**Status:** Complete

README: [README.md](./README.md)

## Goal

Run the complete repository gate and close every worklog readiness item against the implemented tree.

## Specification

Run `make qa` with the repository's required native and Docker dependencies. Do not add a test skip, build tag, environment switch, target, or shortened timeout. Run the README readiness checklist and the separate duplication signal. Review the full change for architecture-document updates, unsafe secret output, plaintext active objects, unexpected gateway changes, and generated artifacts.

Resolve failures in the owning phase. Do not weaken an assertion or budget to obtain a green result. Keep phase statuses truthful and record the exact gate results in the README session journal.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| Run the repository QA gate | Full Go, race, coverage, and npm suites | Every required gate passes | Record command outcome | Test skips, reduced coverage, or narrowed suite |
| Run the readiness checklist and duplication signal | Worklog tree and repository tools | Checklist passes; duplication findings are classified by repository policy | Record the result | Suppressing actionable duplication without rationale |
| Review the complete diff | Maintainer and architecture docs | Behavior and docs agree; no gateway code changed | Record remaining findings | Cloud mutation or unreviewed generated files |

## Acceptance criteria

| Criterion | Test or command |
| --- | --- |
| Repository-required formatter, linter, static analysis, Go, and npm gates pass | `make qa` |
| Every line in the README readiness checklist passes | Commands in the README checklist |
| The separate duplication signal has no actionable unresolved clone | Run the command named in the README validation policy and classify its findings |
| The final diff contains only approved source, test, architecture, and documentation files | `git status --short` and full diff review |
| Worklog statuses and journal match actual results | README review |

## Error coverage

| Failure | Expected outcome | Test or command |
| --- | --- | --- |
| Any QA gate fails | Phase remains incomplete; correct the owning defect | `make qa` |
| A readiness item fails | Phase remains incomplete; correct the worklog or tree | Commands in the README checklist |
| Duplication signal reports an actionable clone | Refactor or record why it is not actionable under repository policy | The command named in the README validation policy |
| Diff contains an unapproved gateway or generated change | Remove it and rerun the complete review | `git status --short` and full diff review |

## Implementation notes

Complete (2026-10-09, phase-7 worker). Ran the repository gate on the
implemented tree and closed the two readiness findings the earlier phases
left open.

- `make qa` passes: gofumpt, go vet, staticcheck and go fix clean; the Go
  suite under `-race -count=3 -timeout=30s -coverpkg=./...` passes with
  `== coverage: 87.1% (floor 70%) ==`; `npm test` passes 35 of 35.
- Readiness checklist: items 1 and 3–8 pass. Item 2 failed on one name: the
  README shared-invariants table and the phase-6 invariant table cited
  `TestEncryptStatusWording`, which no test defines; the status wording
  (`local: plaintext`) is asserted by `TestEncryptCommandSurface` in
  `internal/cli/encrypt_command_test.go`, so both tables now cite that test.
  Item 3 also required `internal/integrationtest/main_test.go` (the
  `helperKDFEnv` injectable knob of D21) to be named by its owning phase, so
  phase 5's file list now includes it.
- Duplication signal `go run github.com/mibk/dupl@v1.0.0 -t 80 .`: five
  clone groups, none actionable. Two are the strict versioned
  configuration-record idiom shared by `internal/keyrecord` and
  `internal/settings` (the established per-package boilerplate recorded by
  Phase 3); one is the test-only key setup shared by
  `internal/storage/encrypted_store_test.go` and
  `internal/integrationtest/sync/encrypted_integrationtest_test.go` (acceptable
  test-setup boilerplate across two test packages that cannot share a
  helper); the remaining two are pre-existing.
- Diff review: `git status --short` lists only approved source, test,
  architecture and worklog files. No gateway, route or storage-adapter file
  changed (`internal/httpstore`, `internal/httpstore/gatewaytest`,
  `internal/s3store` and `internal/storage/contract` are untouched), no
  generated or ignored artifact is staged, and the only new dependency is
  the pinned `golang.org/x/crypto` recorded by D9.
- Secret and ciphertext review: the command-leak invariant is proven by
  `TestEncryptCommandDoesNotLeakSecrets`, and ciphertext at rest by
  `TestEncryptedCommitContainsCiphertext` and
  `TestEncryptInitConvertsHistory`. The visible workspace, private state and
  pack cache are asserted plaintext by `TestEncryptCommandSurface`.

Test commands and results:

```text
make qa
== gofumpt == / == go vet == / == staticcheck == / == go fix ==  (clean)
ok  ... every package ... (race, -count=3, -timeout=30s)
== coverage: 87.1% (floor 70%) ==
npm test: 35 passed, 0 failed

python3 scripts/check-architecture-links.py
29 file(s) checked, every local link resolves

go run github.com/mibk/dupl@v1.0.0 -t 80 .
Found total 5 clone groups.
```

## Review findings

Carried forward, explicitly, the finding Phases 4–6 recorded: the process
never constructs `storage.Encrypted` for `serve`, `pull` or `commit`, so those
commands read an encrypted `current` through the raw store and refuse it as
`UPGRADE_REQUIRED` instead of unlocking it. No phase of this worklog owns it,
and this review does not open one: `architecture/encryption.md` declares the
process-level wiring of the decorator "a separate change from this worklog
phase", and the effort's Definition of Success ("two machines can use the same
encrypted notebook with only the passphrase") is proven at the decorator
boundary by the Phase 2 scenario `TestEncryptedPullCommit`. The limitation is
a documented operating contract, not a defect against a declared phase; it
stays open for a future worklog.

The Phase 6 **Human required** artifact, the hosted privacy text in
`baalimago/slivingdoc-cloud`, remains blocked and unchanged; it was reported
to the maintainer under D26 and is not a gate of this phase.
