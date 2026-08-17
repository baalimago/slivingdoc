# Phase 2 — Test importers and Go comments

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§9 storage boundary, §20 test architecture

## Goal

Rewire every Go file that imports `internal/testminio` to
`internal/tests3`, rename the real-HTTP test files and their `Minio`-prefixed
identifiers, and clear every MinIO prose string from non-worklog Go source.

## Specification

### Rename and rewire

| From                              | To                                      |
| --------------------------------- | --------------------------------------- |
| `internal/s3store/minio_test.go`  | `internal/s3store/integration_test.go`  |
| `internal/notebook/minio_test.go` | `internal/notebook/integration_test.go` |

In both files, change the import to
`github.com/baalimago/slivingdoc/internal/tests3` and replace `testminio.`
with `tests3.`. Rename the helpers and tests to backend-agnostic names:

| From                                                 | To                                              |
| ---------------------------------------------------- | ----------------------------------------------- |
| `newMinioStore`                                      | `newTestStore`                                  |
| `newMinioNotebook`                                   | `newTestNotebook`                               |
| `TestMinioNotebookPullSeesPublishedChange`           | `TestNotebookPullSeesPublishedChange`           |
| `TestMinioNotebookTwoWriterRace`                     | `TestNotebookTwoWriterRace`                     |
| `TestMinioNotebookCheckpointCleansAndReaderRestarts` | `TestNotebookCheckpointCleansAndReaderRestarts` |
| `TestMinioContractSuite`                             | `TestContractSuite`                             |
| `TestMinioMultipartUpload`                           | `TestMultipartUpload`                           |
| `TestMinioPrefixIsolation`                           | `TestPrefixIsolation`                           |
| `TestMinioConcurrentCAS`                             | `TestConcurrentCAS`                             |

Update the package doc comments and inline comments in both files so they
describe the S3 contract against the pinned backend, not MinIO. The literal
note value `"minio v1"` in the notebook round trip becomes `"s3 v1"` (or any
backend-neutral content).

### Integration suite importers

In `internal/integrationtest`, change the import in `harness.go`,
`main_test.go`, `scenario_cli_test.go`, and `scenario_integrity_test.go` to
`internal/tests3` and replace `testminio.` with `tests3.`. No scenario logic
changes.

### Comment-only edits

Reword MinIO prose to S3-contract prose in:

- `internal/storage/contract/suite.go` — package doc: "against both the
  in-memory fake and the real S3 backend".
- `internal/storage/fake/fake_test.go` — "like the real S3 backend does".
- `internal/s3store/store.go` — the three comments: credentials injected by
  "the S3 test backend", "S3-compatible endpoints (SeaweedFS and similar)",
  and the conditional-PUT-missing-key note reworded to "the pinned S3 backend
  answers a conditional PUT against a missing key with NoSuchKey".
- `internal/notebook/load_test.go` — "the real S3 backend suite".
- `internal/integrationtest/faults.go`, `doc.go`,
  `scenario_checkpoint_test.go`, `scenario_pull_test.go`,
  `scenario_helpers_test.go`, `main_test.go` — prose only.

### Fixture strings

Replace the cosmetic literals so a repo-wide `rg -i minio` is empty:

- `internal/mcp/errors_test.go`: `minio.example.com` → `s3.example.com`.
- `internal/app/config_test.go`: `MINIO.local` → `s3.local`,
  `:9000/minio/` → `:8333/s3/`, and the remaining bare `:9000` URL fixtures
  → `:8333`.
- `internal/testminio/testminio_test.go` is covered by Phase 1.

The fixture edits change no assertion semantics; they only rename example
hosts and ports in URL-normalization and redaction tests.

## Integration contract

| Trigger                   | Collaborators    | Observable result         | Required side effect      | Prohibited side effect          |
| ------------------------- | ---------------- | ------------------------- | ------------------------- | ------------------------------- |
| `go test ./...`           | Docker, `tests3` | All suites pass           | Real S3 backend exercised | No `minio`/`MinIO` in Go source |
| `rg -i minio` (Go source) | Repo             | No match outside worklogs | —                         | —                               |

## Acceptance criteria

- [ ] `internal/s3store/integration_test.go` and `internal/notebook/integration_test.go` exist.
- [ ] No `internal/testminio` import remains in the module.
- [ ] `rg -i 'minio|9000' --glob '*.go'` returns nothing under `internal/` and the repository root (worklogs and `.build/` excepted).
- [ ] The contract suite, notebook suite, and integration suite pass unchanged under the new names.
- [ ] The fixture-string edits leave the URL-normalization and redaction tests passing.

Each checked criterion cites the file or command that proves it.

## Error coverage

| Failure                                  | Expected outcome                         | Required check                     |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------- |
| A stale `testminio` import remains       | Compile error in the affected package    | `go build ./...` / `go test ./...` |
| A renamed test asserts the wrong content | Test failure naming the expected bytes   | The round-trip test                |
| A fixture edit weakens a redaction test  | Test failure naming the leaked substring | `internal/mcp/errors_test.go`      |

## Implementation notes

Executed 2026-08-17 (worker session 2). All edits follow the phase table verbatim.

- Renamed `internal/s3store/minio_test.go` → `integration_test.go` and
  `internal/notebook/minio_test.go` → `integration_test.go` with `git mv`, so
  history records the rename.
- Rewired every `internal/testminio` import to `internal/tests3` and every
  `testminio.` reference to `tests3.` in `internal/s3store`, `internal/notebook`,
  and all four `internal/integrationtest` files. Renamed the helpers and tests
  per the table (`newTestStore`, `newTestNotebook`, `TestContractSuite`,
  `TestMultipartUpload`, `TestPrefixIsolation`, `TestConcurrentCAS`,
  `TestNotebookPullSeesPublishedChange`, `TestNotebookTwoWriterRace`,
  `TestNotebookCheckpointCleansAndReaderRestarts`). The notebook round trip
  note value became `"s3 v1"`.
- Reworded the package docs of both renamed suites and the inline comments so
  they describe the S3 contract against the pinned backend. While rewording,
  corrected the stale "skip only when Docker is unavailable" prose to the
  implemented fail policy (the `tests3.require` path calls `t.Fatalf`); the CI
  integration job treats any skip as failure either way.
- Applied the comment-only rewordings in `internal/storage/contract/suite.go`,
  `internal/storage/fake/fake_test.go`, `internal/s3store/store.go`,
  `internal/notebook/load_test.go`, and the six `internal/integrationtest`
  files. Reflowed the `faults.go` and `doc.go` comments to keep lines within
  the surrounding width.
- Replaced the fixture strings: `minio.example.com` → `s3.example.com` in
  `internal/mcp/errors_test.go`, and `MINIO.local` / `:9000` → `s3.local` /
  `:8333` in `internal/app/config_test.go` (including the `:9000/minio/` →
  `:8333/s3/` path pair). No assertion semantics changed.

## Acceptance criteria evidence

| Criterion | Proof |
| --------- | ----- |
| `internal/s3store/integration_test.go` and `internal/notebook/integration_test.go` exist | `git status` shows the renames; both compile and run |
| No `internal/testminio` import remains in the module | `rg testminio --glob '*.go'` returns nothing outside `worklogs/` |
| `rg -i 'minio\|9000' --glob '*.go'` empty under `internal/` and the root | command output: `(no matches)` |
| Contract, notebook, and integration suites pass under the new names | `go test ./internal/s3store/...` (17.7 s), `./internal/notebook/...` (14.0 s), `./internal/integrationtest/...` (22.8 s), all `ok`; full `make test` 83.7 % coverage |
| Fixture edits leave URL-normalization and redaction tests passing | `go test ./internal/app/... ./internal/mcp/...` both `ok` |
| `make qa` passes unchanged | `make lint` clean (gofumpt, vet, staticcheck, go fix); `make test` clean with coverage 83.7 % (floor 70 %); `npm test` 35/35 |

## Review findings

### Review 1 (2026-08-17, worker session 2)

No critical or major findings. The phase matched its integration contract: the
full `make qa` sweep passed with the Docker-backed suites exercising the pinned
SeaweedFS container, and `rg -i minio|9000 --glob '*.go'` is empty outside
`worklogs/`.

Minor, recorded, no action required:

- The repo still names MinIO in non-Go files (`docs/`, `Makefile`, CI,
  `examples/minio/`, both Terraform READMEs, `AGENTS.md`). That is exactly the
  Phase 3 and Phase 4 scope; no Phase 2 leak.
- The Phase 1 tree was uncommitted at the start of this session (staged rename,
  unstaged SeaweedFS rewrite). The agent environment permits only read-only git
  operations, so this session leaves the complete Phase 1 + Phase 2 delta in the
  working tree for one coherent commit by the maintainer, per the decision in
  the README decision log.
