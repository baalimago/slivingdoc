# Phase 4 — CI and example

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§17 flags/environment, `.github/workflows/ci.yml`

## Goal

Point the CI pre-pull at the SeaweedFS image, update the Makefile comment,
and replace the local MinIO example with the SeaweedFS example.

## Specification

### `.github/workflows/ci.yml`

Rename the image variable and its uses:

```yaml
env:
  S3_IMAGE: "chrislusf/seaweedfs:4.42"
```

- The `qa` job pre-pull step: "pre-pull MinIO image" → "pre-pull S3 backend
  image", `docker pull "$S3_IMAGE"`.
- The `readme-coverage` job `prerun-step-cmd`:
  `make libgit2 && docker pull chrislusf/seaweedfs:4.42`.
- The `qa` job comment "Docker-backed MinIO suites" → "Docker-backed S3
  suites".

The `S3_IMAGE` pin and the `tests3.Image` constant must name the same image;
add a one-line comment in `ci.yml` noting that the two stay in sync.

### `Makefile`

Line 59 comment: "real MinIO containers" → "real S3-compatible containers
(SeaweedFS)". No target changes.

### `examples/minio` → `examples/seaweedfs`

Rename the directory and rewrite both files.

#### `examples/seaweedfs/compose.yaml`

```yaml
# Local SeaweedFS for manual slivingdoc evaluation
#
# This environment is for humans only. Automated tests start their own
# S3-compatible containers through testcontainers (internal/tests3) and never
# read this file; no test depends on this environment running.
#
#   docker compose up -d
#   docker compose down          # stop
#   docker compose down -v       # stop and remove the note data volume
services:
  seaweedfs:
    image: chrislusf/seaweedfs:4.42
    entrypoint: /bin/sh
    command:
      - -c
      - |
        echo '{
          "identities": [
            {
              "name": "slivingdoc",
              "credentials": [
                { "accessKey": "slivingdoc", "secretKey": "slivingdoc-local" }
              ],
              "actions": ["Admin", "Read", "Write", "List", "Tagging"]
            }
          ]
        }' > /etc/seaweedfs/s3.json &&
        weed server -s3 -s3.config /etc/seaweedfs/s3.json -dir /data
    ports:
      - "8333:8333" # S3 API
    volumes:
      - seaweedfs-data:/data

volumes:
  seaweedfs-data:
```

#### `examples/seaweedfs/README.md`

Keep the MinIO README's shape with the SeaweedFS facts:

1. Start: `docker compose up -d`; S3 gateway on `http://localhost:8333`;
   credentials `slivingdoc` / `slivingdoc-local`, local-only.
2. No bucket step: `my-notes` is created on first write; the startup probe
   performs that write.
3. Run the server with `--endpoint http://localhost:8333 --path-style`.
4. Connect an MCP host with the same JSON block and `:8333`.
5. Work with the notebook as before (pull, edit, commit, merge, markers).
6. Note the non-fatal `no signing key found for STS service` startup line is
   safe to ignore for basic credentials.
7. Stop: `docker compose down` / `down -v`.

## Integration contract

| Trigger                    | Collaborators     | Observable result                     | Required side effect          | Prohibited side effect            |
| -------------------------- | ----------------- | ------------------------------------- | ----------------------------- | --------------------------------- |
| CI `qa` job                | Docker, SeaweedFS | Pre-pull succeeds                     | Image cached before tests     | No MinIO image pulled             |
| `docker compose up -d`     | SeaweedFS         | S3 gateway on `:8333`                 | Container healthy            | No host-path config mount required |
| `rg -i minio` (repo)       | Repo              | Matches only in historical worklogs   | —                             | —                                 |

## Acceptance criteria

- [x] `ci.yml` references only `S3_IMAGE` and `chrislusf/seaweedfs:4.42`. — `.github/workflows/ci.yml`
- [x] `ci.yml` and `tests3.Image` name the same image, with a sync note. — `.github/workflows/ci.yml` env block comment; `internal/tests3/s3.go` `Image` constant
- [x] `Makefile` comment names the S3-compatible backend. — `Makefile` test target comment
- [x] `examples/seaweedfs/compose.yaml` and `README.md` exist; `examples/minio` does not. — `examples/seaweedfs/`
- [x] The example README documents `:8333`, auto-created buckets, and the non-fatal STS error. — `examples/seaweedfs/README.md` §1, §2, §6
- [x] `rg -i minio` returns matches only under historical worklogs. — `rg -i -l minio` outside `worklogs/` and `.build/` returns nothing

Each checked criterion cites the file it edits.

## Error coverage

| Failure                                   | Expected outcome                                    | Required check                          |
| ----------------------------------------- | --------------------------------------------------- | ---------------------------------------- |
| CI pre-pull and `tests3.Image` diverge    | Pull of an unexpected image, or a pull inside a timed test | Manual review of both pins              |
| The compose stack fails the startup probe | Phase 5 manual smoke test exits nonzero             | Phase 5                                  |

## Implementation notes

- The `qa` job pre-pull uses `docker pull "$S3_IMAGE"`; the `readme-coverage`
  job repeats the image literally because the `env` context is unavailable in
  a `workflow_call` job's `with` block (its `GO_VERSION` repeat already says
  the same).
- The example compose keeps the documented human credentials `slivingdoc` /
  `slivingdoc-local` (README shared invariant 5); `tests3` uses
  `slivingdoc-secret`. The two environments are deliberately separate, so
  their secrets do not need to match.
- The sync comment in `ci.yml` names `internal/tests3/s3.go` explicitly so a
  future pin bump finds both spots.

## Session journal

| Date       | Entry                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 2026-08-17 | Completed Phase 4: switched `.github/workflows/ci.yml` to `S3_IMAGE: chrislusf/seaweedfs:4.42` with a keep-in-sync note naming `tests3.Image`, renamed the pre-pull step to "pre-pull S3 backend image", reworded the Docker-backed suites comment, and updated the `readme-coverage` pre-pull; reworded the `Makefile` test comment to "real S3-compatible containers (SeaweedFS)"; renamed `examples/minio` to `examples/seaweedfs` and rewrote `compose.yaml` (pinned image, inline `s3.config` identity, port 8333) and `README.md` (8333 gateway, no bucket step, `--path-style`, MCP block, STS startup noise). Validation: YAML parses for both edited YAML files; `rg -i 'minio|9000|9001'` outside `worklogs/` and `.build/` returns nothing; `make qa` passes — lint clean (gofumpt, vet, staticcheck, go fix), `make test` all packages `ok` with coverage 83.7 % (floor 70 %) including the SeaweedFS-backed suites (integrationtest 36.7 s, notebook 25.6 s, s3store 23.4 s), npm 35/35. |

## Review findings

No reviews recorded.
