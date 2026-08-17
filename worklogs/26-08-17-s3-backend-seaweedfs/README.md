# S3 backend → SeaweedFS worklog

**Status:** Not Started

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§9 storage boundary, §17 flags/environment, §20 test architecture;
[`../../docs/testing.md`](../../docs/testing.md) test layers

## Objective

Replace every MinIO reference in the living project with SeaweedFS, and
abstract the test backend so the S3 contract — not the vendor — is what the
code depends on. The testcontainers bootstrap becomes `internal/tests3`, a
backend-agnostic seam whose only concrete detail is the pinned S3-compatible
image it starts. The public slivingdoc surface and the `ObjectStore` protocol
do not change: SeaweedFS satisfies the same S3 contract the probe and suites
exercise.

Historical worklogs are not edited. They are the dated record of the change
that introduced MinIO; this worklog is the delta that swaps it.

## Status board

| Phase                                        | Status      | Summary                                                                                       |
| -------------------------------------------- | ----------- | --------------------------------------------------------------------------------------------- |
| [1. `internal/tests3`](phase-1-tests3-bootstrap.md)     | Complete   | Rename `internal/testminio` to `internal/tests3` and rewrite the bootstrap to start the pinned SeaweedFS container. |
| [2. Test importers](phase-2-test-importers.md)          | Complete   | Rename `minio_test.go` files, update the `tests3` import and identifiers, and reword Go comments and fixture strings. |
| [3. Contract and docs](phase-3-contract-docs.md)        | Complete   | Reword every current-state doc to name SeaweedFS as the pinned S3-compatible backend and the S3 contract as the requirement; corrected stale § references. |
| [4. CI and example](phase-4-ci-example.md)              | Complete   | Pin and pre-pull the SeaweedFS image in `.github/workflows/ci.yml`, update `Makefile`, and rename `examples/minio` to `examples/seaweedfs`. |
| [5. Validation](phase-5-validation.md)                  | Complete | Full `make qa` sweep plus a manual pull/commit round trip against the compose stack. |

## Strategy

### Execution order

Phase 1 establishes the backend seam. Phases 2 and 3 depend on it only for
naming; they rewire importers and current-state prose. Phase 4 switches CI
and the example. Phase 5 proves the whole thing. Phase 5 depends on all
prior phases.

An executing agent reads this README and only the phase file it works on.
Shared rules live here so they are not duplicated per phase.

### Required architecture sections

| Phase | Required contract sections                  |
| ----- | ------------------------------------------- |
| 1     | §9 storage boundary, §20 test architecture |
| 2     | §9 storage boundary, §20 test architecture |
| 3     | §17 flags/environment, §20 test architecture |
| 4     | §17 flags/environment, `.github/workflows/ci.yml` |
| 5     | §9 storage boundary, testing layers         |

Re-verify section references after any `docs/` edit.

### Shared invariants

Every phase preserves:

1. The `ObjectStore` protocol and the startup probe are unchanged. SeaweedFS
   `4.42` has been validated to pass the probe and a full pull/commit round
   trip, so no behavior changes, only backend identity.
2. `internal/tests3` is the only place that names the concrete backend image
   and its startup command. All importers and docs describe the S3 contract,
   with SeaweedFS named only as the current pinned implementation.
3. The image pin `chrislusf/seaweedfs:4.42` appears in exactly two spots that
   must stay in sync: the `tests3.Image` constant and the CI pre-pull
   (`S3_IMAGE`). CI cannot read a Go constant, so this duplication is
   deliberate and documented.
4. No historical worklog under `worklogs/26-08-08-v1-implementation` or
   `worklogs/26-08-15-*` changes. `.build/` artifacts are gitignored and
   ignored.
5. The two tools, the one-shot `pull`/`commit` commands, the flags, and the
   error taxonomy stay unchanged. Credentials stay `slivingdoc` /
   `slivingdoc-local`; the S3 port becomes `8333`.
6. `make qa` (lint, test, npm-test) passes unchanged after the edits. The
   Docker-backed suites still require a reachable daemon and still fail, not
   skip, when it is absent.

### Review severity

| Severity | Meaning                                                                 | Phase effect                                |
| -------- | ----------------------------------------------------------------------- | ------------------------------------------- |
| Critical | Credential leak, real-AWS contact, or a broken contract reference.      | Reopen and block dependent phases.          |
| Major    | The backend cannot start, or the probe/contract suite fails against it. | Reopen the phase.                           |
| Minor    | Local maintainability or documentation defect with no contract failure. | Record and fix without mandatory reopening. |

Every review appends findings to the phase file. Critical and major findings
change that phase to `Reopened (review N)` and update this status board.

## Decisions

| Date       | Decision                                                                                                                            | Reason                                                                                                                          |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-17 | Rename `internal/testminio` to `internal/tests3`, not to a vendor name.                                                             | The S3 contract, not the implementation, is what matters internally. A future S3-compatible backend then changes only `tests3`. |
| 2026-08-17 | Keep the backend-agnostic identifiers (`tests3.Ensure`, `tests3.Bucket`, `tests3.StoreConfig`) and name the image constant generically (`Image`). | Importers reference the seam, not a vendor, so the next swap does not touch them again. |
| 2026-08-17 | Pin `chrislusf/seaweedfs:4.42` as the current image; start it with `weed server -s3 -s3.config /etc/seaweedfs/s3.json -dir /data` and an inline `s3.config`. | Validated empirically against the real binary; the official single-process Docker pattern. |
| 2026-08-17 | Wait for readiness with `wait.ForLog("Start Seaweed S3 API Server")`, not an HTTP health probe.                                      | SeaweedFS exposes no `/minio/health/live`-style endpoint; the startup log line is the reliable ready signal. |
| 2026-08-17 | Keep the explicit `CreateBucket` in `tests3.start` even though SeaweedFS auto-creates buckets on upload.                            | Determinism: the shared bucket exists before any test, and the `raw` client contract is unchanged. |
| 2026-08-17 | Rename the real-HTTP test files `minio_test.go` to `integration_test.go` and strip the `Minio` prefix from test names.               | The tests prove the S3 contract against the real backend; the vendor name carries no signal. |
| 2026-08-17 | Reword the contract §20 to name SeaweedFS as the pinned target while stating the requirement is the S3 contract.                     | Docs are a current snapshot; the implementation is named, but the contract is the invariant. |
| 2026-08-17 | Replace cosmetic fixture strings (`minio.example.com`, `MINIO.local`, bare `:9000`) with neutral S3 names.                           | A clean `rg` and consistency with the abstraction; they are URL-parsing fixtures, not backend facts. |
| 2026-08-17 | Do not edit historical worklogs.                                                                                                     | They are the dated record of the prior delta; rewriting them would falsify history. |
| 2026-08-17 | Correct the stale "skip only when Docker is unavailable" comment prose to the implemented fail policy while rewording. | The `tests3.require` path fails the test with `t.Fatalf`; the comments described the pre-Phase-1 skip behavior and were wrong at the time of the reword. |
| 2026-08-17 | Leave the complete working-tree delta (Phases 1 and 2) for one coherent commit by the maintainer. | The agent environment bans `git add`/`git commit`, and the Phase 1 session left only a staged rename with its SeaweedFS rewrite unstaged. A two-commit split would require a non-compiling intermediate (renamed package with old MinIO content), so one commit keeps history honest. |
| 2026-08-17 | Correct the worklog's § references: test integration is §20 (Test architecture), not §22; Phase 4's CI reference is `.github/workflows/ci.yml`, not §21. | The phase-3 acceptance criterion re-verifies section references after the `docs/` edits; the planning citations did not match the document's actual numbering. |
| 2026-08-17 | Keep the example compose credentials `slivingdoc` / `slivingdoc-local` while `tests3` uses `slivingdoc-secret`. | The human example documents the local-only credentials the README invariant fixes; the automated suite owns its own identity. Two separate environments need not share a secret. |
| 2026-08-17 | Rebuild `.build/slivingdoc` explicitly (`rm` + `make build`) before the Phase 5 smoke test. | The `$(BIN)` Makefile target only rebuilds when the libgit2 stamp changes; the Aug 15 binary predated the Phase 1-2 source renames, so the smoke test had to run the current code. |
| 2026-08-17 | Detach the SeaweedFS testcontainer termination from the test-binary exit path: `tests3.Terminate()` now launches `ctr.Terminate` in a detached goroutine and returns immediately. | CI's `go test -timeout=30s` budgets the whole binary lifetime including `TestMain`; the moby `ContainerStop` can block in `getConn` on a busy runner and trip the alarm after every scenario already passed. Ryuk guarantees eventual cleanup, so the binary may exit first. |

## Session journal

| Date       | Entry                                                                                                                                                                                                                                                                                                                                                                                         |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-17 | Read the SeaweedFS S3 API and S3 Conditional Operations wikis: `If-Match`, `If-None-Match: *`, `ListObjectsV2`, `DeleteObjects`, and multipart are all supported, and the conditional check plus write are atomic per key. |
| 2026-08-17 | Empirically validated SeaweedFS 4.42 against the real slivingdoc binary: `slivingdoc pull` (full `If-None-Match`/`If-Match`/read-after-write probe) returned `OK generation 0`, and a pull → edit → commit → pull round trip reached generation 1 with the note bytes on the second workspace. |
| 2026-08-17 | Confirmed bucket auto-creation on first write, the `-s3.config` JSON shape, and the `weed server -s3 -s3.config … -dir /data` startup form. |
| 2026-08-17 | Noted the non-fatal `no signing key found for STS service` startup error and the `SSE-S3 KeyManager` warning; neither blocks basic-credential S3 traffic. |
| 2026-08-17 | Mapped the full MinIO surface: the test backend, seven importer/comment files, two CI/build files, seven docs, two Terraform READMEs, the example, and the historical worklogs. |
| 2026-08-17 | Completed Phase 1: renamed `internal/testminio` to `internal/tests3`, rewrote the bootstrap to the pinned SeaweedFS image (`chrislusf/seaweedfs:4.42`, port 8333, log-line readiness), reworded policy messages to `s3 integration unavailable`, and re-pointed the unit-test fixture at `:8333`. Package tests, vet, gofmt, and the `rg -i minio|9000` check pass; a temporary smoke test confirmed `Ensure` starts the container and the shared bucket is reachable. |
| 2026-08-17 | Completed Phase 2: renamed `minio_test.go` → `integration_test.go` in `internal/s3store` and `internal/notebook`, rewired all `internal/testminio` imports to `internal/tests3`, renamed the helpers/tests (`newTestStore`, `newTestNotebook`, `TestContractSuite`, `TestMultipartUpload`, `TestPrefixIsolation`, `TestConcurrentCAS`, `TestNotebookPullSeesPublishedChange`, `TestNotebookTwoWriterRace`, `TestNotebookCheckpointCleansAndReaderRestarts`), reworded every MinIO Go comment to S3-contract prose, and replaced the fixture hosts/ports (`s3.example.com`, `s3.local:8333`). `rg -i 'minio|9000' --glob '*.go'` is empty outside `worklogs/`. `make qa` passes: lint clean, `make test` 83.7 % coverage (floor 70 %), npm 35/35. |
| 2026-08-17 | Reviewed Phase 2: no critical or major findings; remaining MinIO mentions are non-Go files that are Phase 3/4 scope. |
| 2026-08-17 | Completed Phase 3: rewrote `docs/slivingdoc-v1.md` §20/§24/§25, `docs/testing.md`, `docs/running.md`, `README.md`, `AGENTS.md`, and both Terraform READMEs per the phase table; `rg -i minio` is empty in `docs/` and every edited file. The section-reference re-verification corrected the stale "§22 test integration" citations (six places) to §20 and Phase 4's "§21 CI" to `.github/workflows/ci.yml`. Remaining non-worklog matches are exactly Phase 4 scope: `Makefile:59`, `.github/workflows/ci.yml`, `examples/minio/`. No Go or test-file changes, so the `make qa` gates are untouched by this phase. Validation (before-state = Phase 2's recorded `make qa` pass on the same tree minus these edits): `make lint` clean (gofumpt, vet, staticcheck, go fix); `make test` passes with coverage 83.7 % (floor 70 %), all packages `ok` including the SeaweedFS-backed suites (integrationtest 36.7 s, notebook 25.9 s, s3store 17.6 s); `npm test` 35/35. |
| 2026-08-17 | Completed Phase 4: switched `.github/workflows/ci.yml` to `S3_IMAGE: chrislusf/seaweedfs:4.42` (sync note naming `tests3.Image`, "pre-pull S3 backend image" step, Docker-backed S3 suites comment, `readme-coverage` pre-pull updated); reworded the `Makefile` test comment; renamed `examples/minio` to `examples/seaweedfs` and rewrote `compose.yaml` and `README.md` (8333 gateway, no bucket step, MCP block, STS noise). `rg -i minio` now matches only under `worklogs/`. `make qa` passes: lint clean, `make test` 83.7 % coverage (floor 70 %) with the SeaweedFS-backed suites green (integrationtest 36.7 s, notebook 25.6 s, s3store 23.4 s), npm 35/35. |
| 2026-08-17 | Completed Phase 5 (validation): `make qa` exits 0 (lint clean; `make test` all packages `ok`, coverage 83.7 %, floor 70 %; npm 35/35); `dupl -t 80` reports 0 clone groups; the manual compose smoke test round-tripped `pull /tmp/notes` → `OK generation 0`, `commit -m first` → `OK generation 1` (`a.md +1`), `pull /tmp/notes-b` → `OK generation 1` with `seaweed notes` materialized, and no output leaked the secret, a private path, a pack key, or a Git ID; `docker compose down -v` removed the stack. All five phases are now Complete. |
| 2026-08-17 | Diagnosed and fixed the PR #2 CI failure (`actions/runs/32050735315/job/95449218538`): `make test` panicked with `test timed out after 30s` in `internal/integrationtest` after all scenarios passed. The stack was `TestMain → tests3.Terminate → testcontainers DockerContainer.Terminate → moby ContainerStop → http.Transport.getConn` blocking past the binary-wide 30 s alarm. Fix: `tests3.Terminate` now stops the container in a detached goroutine and returns immediately, so `os.Exit(code)` runs before the alarm; Ryuk reaps the container. |

## Feedback index

Holistic review (2026-08-17, Phase 5 session): no findings — no critical, major, or minor issues across the five phases. Details in [`phase-5-validation.md`](phase-5-validation.md).
