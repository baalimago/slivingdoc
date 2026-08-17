# Phase 3 — Contract and docs

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§17 flags/environment, §20 test architecture

## Goal

Update the current-state documentation so it names SeaweedFS as the pinned
S3-compatible backend while stating the requirement is the S3 contract.
Historical worklogs are not touched.

## Specification

### `docs/slivingdoc-v1.md`

- §20 (testcontainers paragraph): "testcontainers-go to start MinIO" →
  "testcontainers-go to start the pinned S3-compatible backend (SeaweedFS)".
- §20 (the target paragraph): "MinIO is the required S3-compatible
  integration target. Deterministic adapter tests emulate ... failures that
  MinIO cannot ..." → "A real S3-compatible store is the required integration
  target; the pinned implementation is SeaweedFS (`chrislusf/seaweedfs:4.42`).
  Deterministic adapter tests emulate ... failures that a single store cannot
  produce on demand."
- §20 storage-boundary table row: "fake contract suite and MinIO contract
  suite" → "fake contract suite and real S3 contract suite".
- §24 bullet 22 and the §25 summary bullet: "MinIO testcontainers" →
  "SeaweedFS testcontainers"; "run against MinIO through testcontainers" →
  "run against a real S3-compatible store (SeaweedFS) through testcontainers".

### `docs/testing.md`

- Line 3: "MinIO tests use the pinned testcontainer image" → "Real-S3 tests
  use the pinned testcontainer image (currently SeaweedFS)".
- Line 44–46: "MinIO suites run against real HTTP conditional writes" →
  "the real-S3 suites"; "internal/testminio owns that policy" →
  "internal/tests3 owns that policy".
- Line 59 contract table: "against fake storage and MinIO" → "against fake
  storage and the real S3 backend".
- Line 94: "own MinIO container" → "own S3 test container".

### `docs/running.md`

- Line 132: "to point at a local MinIO" → "to point at a local
  S3-compatible store (such as SeaweedFS)".
- Line 215: "store such as MinIO" → "store such as SeaweedFS".

### `README.md`

- Lines 48–49: point the "No S3 account yet?" link at
  `examples/seaweedfs/` and say "runs a local SeaweedFS container".

### `AGENTS.md`

- Package-map line `examples/minio/ isolated local MinIO walkthrough` →
  `examples/seaweedfs/ isolated local SeaweedFS walkthrough`.
- Package-map line `testminio/ testcontainers MinIO helper` →
  `tests3/ testcontainers S3 backend helper (currently SeaweedFS)`.
- The two prose mentions in the architecture section ("against both the fake
  and MinIO", "starts the pinned testcontainers MinIO") → "against both the
  fake and the real S3 backend" and "starts the pinned S3-compatible
  testcontainers backend".

### Terraform READMEs

- `terraform/README.md` line 93: "MinIO containers" → "S3 test containers".
- `examples/terraform/README.md` line 11: "their own MinIO containers" →
  "their own S3-compatible test containers".

## Integration contract

| Trigger                    | Collaborators | Observable result                    | Required side effect | Prohibited side effect                 |
| -------------------------- | ------------- | ------------------------------------ | -------------------- | -------------------------------------- |
| `rg -i minio` (docs, root) | Repo          | No match outside historical worklogs | —                    | No stale vendor naming in the contract |

## Acceptance criteria

- [x] `docs/slivingdoc-v1.md` names the S3 contract as the requirement and SeaweedFS as the pinned target.
- [x] `docs/testing.md` references `internal/tests3` and the real S3 backend.
- [x] `README.md`, `docs/running.md`, `AGENTS.md`, and both Terraform READMEs carry no MinIO string.
- [x] `rg -i minio` returns matches only under `worklogs/26-08-08-v1-implementation` and `worklogs/26-08-15-*`.
- [x] Section references in `AGENTS.md` and the worklog are re-verified after the `docs/` edits.

Each checked criterion cites the file and section it edits.

## Error coverage

| Failure                                | Expected outcome                     | Required check             |
| -------------------------------------- | ------------------------------------ | -------------------------- |
| A stale MinIO string survives in a doc | `rg -i minio` match outside worklogs | The acceptance `rg` sweep  |
| A section reference drifts             | A broken or mis-numbered link        | Manual review of § numbers |

## Implementation notes

Executed 2026-08-17 (worker session 3). All edits follow the phase table verbatim.

- Reworded `docs/slivingdoc-v1.md` §20 (Test architecture): the testcontainers
  paragraph now starts the pinned S3-compatible backend (SeaweedFS), the
  target paragraph names the S3 contract as the requirement with
  `chrislusf/seaweedfs:4.42` as the pinned implementation, and the boundary
  table row reads "fake contract suite and real S3 contract suite". §24 bullet
  22 and the §25 architecture-acceptance bullet name SeaweedFS testcontainers
  and a real S3-compatible store.
- Reworded `docs/testing.md` (lines 3, 44–47, 59, 94) to reference
  `internal/tests3` and the real S3 backend.
- Reworded `docs/running.md` (lines 132, 215), `README.md` (lines 48–49),
  `AGENTS.md` (architecture prose, package map: `examples/seaweedfs/` and
  `tests3/`), `terraform/README.md` (line 93), and
  `examples/terraform/README.md` (line 11) to carry no MinIO string.
- The acceptance `rg` sweep is clean in `docs/` and every edited file. The
  only remaining non-worklog matches are exactly Phase 4 scope: `Makefile:59`,
  `.github/workflows/ci.yml`, and `examples/minio/`. The current worklog
  itself names MinIO as the record of the swap, as every phase file does.

## Acceptance criteria evidence

| Criterion | Proof |
| --------- | ----- |
| `docs/slivingdoc-v1.md` names the S3 contract as the requirement and SeaweedFS as the pinned target | §20 testcontainers and target paragraphs, §20 boundary table, §24 bullet 22, §25 acceptance bullet |
| `docs/testing.md` references `internal/tests3` and the real S3 backend | lines 3, 44–47, 59, 94 after the reword |
| `README.md`, `docs/running.md`, `AGENTS.md`, and both Terraform READMEs carry no MinIO string | `rg -i minio` on the seven edited files: `(no matches)` |
| `rg -i minio` returns matches only under `worklogs/26-08-08-v1-implementation` and `worklogs/26-08-15-*` | `rg -i -l minio` outside `worklogs/` and `.build/` returns only `Makefile`, `.github/workflows/ci.yml`, and `examples/minio/*` — all Phase 4 scope |
| Section references in `AGENTS.md` and the worklog are re-verified after the `docs/` edits | `AGENTS.md` carries no numbered § references; the worklog's stale "§22 test integration" citations were corrected to §20 (Test architecture) and Phase 4's "§21 CI" to `.github/workflows/ci.yml` |

## Review findings

### Review 1 (2026-08-17, worker session 3)

No critical or major findings. The phase matched its integration contract: no
MinIO string remains in `docs/` or any edited file, and the corrected §
references resolve against the current `docs/slivingdoc-v1.md` numbering.

Minor, recorded, no action required:

- The acceptance sweep `rg -i minio` still matches `Makefile:59`,
  `.github/workflows/ci.yml`, and `examples/minio/`. Those are exactly the
  Phase 4 items (Makefile comment, CI pre-pull, example rename), so the
  phase's observable result cannot fully clear until Phase 4 executes.
- The root `README.md` link now points at `examples/seaweedfs/`, which does
  not exist until Phase 4 renames the example directory; the link resolves
  once Phase 4 lands.
- The section-reference re-verification corrected the worklog's planning
  citations: "§22 test integration" (six places) was stale — test integration
  lives in §20 (Test architecture) — and Phase 4's "§21 CI" pointed at
  Distribution and native builds, which has no CI content; the Phase 4 row
  now cites `.github/workflows/ci.yml`. The phase-1 and phase-2 headers got
  the same §20 correction; their records are otherwise untouched.
