# Phase 5 — Validation

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§9 storage boundary; [`../../docs/testing.md`](../../docs/testing.md)
test layers

## Goal

Prove the backend swap changed no behavior: the quality gate passes and the
compose example performs a full pull/commit round trip against SeaweedFS.

## Specification

### Quality gate

Run the one sanctioned gate:

```bash
make qa
```

It runs `lint`, `test`, and `npm-test`. The `test` target still requires a
running Docker daemon and the pinned libgit2; the real-S3 contract and
integration suites must pass against SeaweedFS exactly as they did against
MinIO. Also run the duplicate check:

```bash
go run github.com/mibk/dupl@v1.0.0 -t 80 .
```

No new Go code exists, so `dupl` must report no new clones.

### Manual smoke test

```bash
cd examples/seaweedfs
docker compose up -d
```

Then:

```bash
export AWS_ACCESS_KEY_ID=slivingdoc
export AWS_SECRET_ACCESS_KEY=slivingdoc-local
export AWS_REGION=us-east-1

slivingdoc pull /tmp/notes \
  --bucket my-notes \
  --endpoint http://localhost:8333 \
  --path-style \
  --workspace-root /tmp/notes \
  --private-root /tmp/slivingdoc-private

printf 'seaweed notes\n' > /tmp/notes/a.md

slivingdoc commit /tmp/notes -m first \
  --bucket my-notes \
  --endpoint http://localhost:8333 \
  --path-style \
  --workspace-root /tmp/notes \
  --private-root /tmp/slivingdoc-private

slivingdoc pull /tmp/notes-b \
  --bucket my-notes \
  --endpoint http://localhost:8333 \
  --path-style \
  --workspace-root /tmp/notes-b \
  --private-root /tmp/slivingdoc-private
```

Expect `OK generation 0`, then `OK generation 1` with `a.md +1`, then
`OK generation 1` with `/tmp/notes-b/a.md` reading `seaweed notes`. The
first pull is the startup probe, proving `If-None-Match: *`, `If-Match`
without mutation, and read-after-write against SeaweedFS. Stop and remove
the data:

```bash
docker compose down -v
```

## Integration contract

| Trigger                      | Collaborators       | Observable result                                   | Required side effect      | Prohibited side effect          |
| ---------------------------- | ------------------- | --------------------------------------------------- | ------------------------- | ------------------------------- |
| `make qa`                    | Go toolchain        | Exit 0                                              | Real-S3 suites pass       | No skip of any suite            |
| First `slivingdoc pull`      | SeaweedFS, binary   | `OK generation 0`                                   | Bucket auto-created       | Nonzero exit                    |
| `slivingdoc commit -m first` | SeaweedFS, binary   | `OK generation 1`, `a.md +1`                        | Increment published       | Secret or key in output         |
| Second `slivingdoc pull`     | SeaweedFS, binary   | `OK generation 1`, `seaweed notes` materialized     | P initialized from R      | Nonzero exit                    |

## Acceptance criteria

- [x] `make qa` exits 0. — run on 2026-08-17: lint clean (gofumpt, vet, staticcheck, go fix), `make test` all packages `ok` with coverage 83.7 % (floor 70 %), npm 35/35
- [x] `dupl -t 80` reports no new clone. — `Found total 0 clone groups.`
- [x] The smoke-test first pull prints `OK generation 0`. — `OK  generation 0` with the startup probe passing
- [x] The smoke-test commit prints `OK generation 1`. — `OK  generation 1`, `a.md +1`
- [x] The smoke-test second pull materializes `seaweed notes`. — `OK  generation 1`; `/tmp/notes-b/a.md` reads `seaweed notes`
- [x] No smoke-test output contains the secret, a private path, a pack key, or a Git ID. — every tool line is `OK generation N` plus the diffstat

## Error coverage

| Failure                              | Expected outcome                                    | Required check                          |
| ------------------------------------ | --------------------------------------------------- | ---------------------------------------- |
| A rename breaks the gate             | `make qa` fails with the specific tool and output   | `make qa`                                |
| SeaweedFS fails the startup probe    | First pull exits nonzero with the probe diagnostic  | Manual smoke test                        |
| The commit loses the CAS race        | Bounded retry, then `REMOTE_BUSY`                   | Manual smoke test (single writer, not expected) |
| The stack does not auto-create bucket | First pull exits nonzero naming the bucket         | Manual smoke test                        |

## Implementation notes

- The smoke test ran against a freshly rebuilt binary: `.build/slivingdoc` was stale (Aug 15) relative to the Phase 1-2 source renames, and the `$(BIN)` Makefile target only rebuilds on a libgit2 stamp change. The binary was removed and rebuilt with `make build` before the first pull.
- The compose stack auto-created `my-notes` on the first pull (the startup probe write), so no `CreateBucket` step was needed for the human example, matching the example README.

## Session journal

| Date       | Entry                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-17 | Completed Phase 5: ran the one sanctioned gate and the manual smoke test. `make qa` exits 0: lint clean (gofumpt, vet, staticcheck, go fix), `make test -race -count=3 -timeout=30s -coverpkg=./...` all packages `ok` including the SeaweedFS-backed suites (integrationtest 37.1 s, notebook 25.6 s, s3store 16.9 s) with coverage 83.7 % (floor 70 %), npm 35/35. `go run github.com/mibk/dupl@v1.0.0 -t 80 .` reports 0 clone groups. Manual smoke test against the compose stack: rebuilt `.build/slivingdoc` (stale pre-rename binary), `docker compose up -d` on `examples/seaweedfs`, then `slivingdoc pull /tmp/notes` → `OK generation 0`, `printf 'seaweed notes\n' > /tmp/notes/a.md` + `slivingdoc commit /tmp/notes -m first` → `OK generation 1` with `a.md +1`, and `slivingdoc pull /tmp/notes-b` → `OK generation 1` with `/tmp/notes-b/a.md` = `seaweed notes`. No smoke-test line leaks the secret, a private path, a pack key, or a Git ID. `docker compose down -v` removed the stack and volume. |

## Review findings

Holistic review (2026-08-17, worker session that ran Phase 5): no critical, major, or minor findings across the five phases. The complete working-tree delta is renames, import rewiring, comment and fixture-string rewording, doc prose, CI pin, and the example rename; no production behavior changed. Verified during the review: `rg -i 'minio|9000|9001'` matches nothing outside `worklogs/` and `.build/`; the sync-critical image pins `chrislusf/seaweedfs:4.42` (`internal/tests3/s3.go` `Image` and `.github/workflows/ci.yml` `S3_IMAGE`, with the documented `readme-coverage` literal repeat) name the same image, and the human-facing references (`examples/seaweedfs/compose.yaml`, `docs/slivingdoc-v1.md` §20) match them; the historical worklogs under `worklogs/26-08-08-v1-implementation` and `worklogs/26-08-15-*` are untouched; the `ObjectStore` protocol, probe, flags, tools, and error taxonomy diffs are limited to comments and fixture strings; and all five phases' acceptance criteria hold, ending with `make qa` exit 0 (coverage 83.7 %), `dupl -t 80` at 0 clone groups, and the compose smoke round trip at generations 0 → 1 → 1.
