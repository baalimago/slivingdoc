# Phase 1 — `internal/tests3` bootstrap

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§9 storage boundary, §20 test architecture

## Goal

Turn `internal/testminio` into `internal/tests3`: a backend-agnostic
testcontainers seam whose only concrete detail is the pinned S3-compatible
image it starts. The package starts one container per test process and hands
out per-test prefixes below a shared bucket, exactly as before.

## Specification

Rename the directory `internal/testminio` to `internal/tests3`, the package
declaration, and both file names (`testminio.go` → `s3.go`,
`testminio_test.go` → `s3_test.go`). Keep the exported surface shape:
`Image`, `User`, `Pass`, `Bucket`, `Region`, `Suite`, `StoreConfig`,
`Ensure`, `Terminate`, `FreshPrefix`, and the `require` policy.

### Package identity

Package doc:

```go
// Package tests3 starts one pinned S3-compatible container per test process
// and hands out per-test prefixes below a shared bucket. The concrete image
// is the current pinned implementation (SeaweedFS); importers depend on the
// S3 contract, not on the vendor.
package tests3
```

The only SeaweedFS-specific facts live in the image constant and the
`start` function.

### Constants

```go
const (
    Image  = "chrislusf/seaweedfs:4.42" // the pinned S3-compatible backend
    User   = "slivingdoc"
    Pass   = "slivingdoc-secret"
    Bucket = "slivingdoc"
    Region = "us-east-1"
)
```

### Startup

`start` runs one container with the inline `s3.config` and waits for the S3
gateway log line:

```go
req := testcontainers.ContainerRequest{
    Image:        Image,
    ExposedPorts: []string{"8333/tcp"},
    Entrypoint:   "/bin/sh",
    Cmd: []string{"-c", `echo '{
      "identities": [
        {
          "name": "slivingdoc",
          "credentials": [
            { "accessKey": "slivingdoc", "secretKey": "slivingdoc-secret" }
          ],
          "actions": ["Admin", "Read", "Write", "List", "Tagging"]
        }
      ]
    }' > /etc/seaweedfs/s3.json && weed server -s3 -s3.config /etc/seaweedfs/s3.json -dir /data`},
    WaitingFor: wait.ForLog("Start Seaweed S3 API Server").
        WithStartupTimeout(30 * time.Second),
}
```

After start, map the `8333/tcp` port, build the endpoint, construct the raw
`*s3.Client` with path style, and `CreateBucket` the shared bucket — exactly
as the MinIO bootstrap did, with the port and the readiness signal the only
structural changes.

### Policy messages

Update the `require` fatal text and the `start` error wraps so no
`minio`/`MinIO` string remains:

```text
s3 integration unavailable: <cause>
Docker is required to run this suite; start the daemon and re-run.
```

and `start s3 container`, `s3 host`, `s3 port` for the three wrapped errors.

### Unit test

`TestRequireFailsWhenDockerIsUnavailable` and
`TestRequireReturnsTheAvailableSuite` keep their assertions; the second
fixture endpoint becomes `http://127.0.0.1:8333`.

## Integration contract

| Trigger                       | Collaborators     | Observable result                  | Required side effect           | Prohibited side effect                |
| ----------------------------- | ----------------- | ---------------------------------- | ------------------------------ | ------------------------------------- |
| `tests3.Ensure(t)` first call | Docker, SeaweedFS | Non-nil `*Suite`                   | Container started, bucket made | No skip when Docker is absent         |
| `suite.StoreConfig()`         | `Suite`           | Endpoint on the mapped `8333` port | Credentials as plain strings   | No AWS SDK type crossing the boundary |
| `suite.FreshPrefix("x")`      | `Suite`           | A unique `x/<uuidv7>` prefix       | Fresh per call                 | No shared mutable state               |
| `tests3.Terminate()`          | `Suite`           | Container stopped                  | Idempotent                     | No error when never started           |

## Acceptance criteria

- [x] `internal/tests3` exists and `internal/testminio` no longer exists.
- [x] `rg -i 'minio|9000' internal/tests3` returns nothing.
- [x] `Image` is the only SeaweedFS-specific constant, documented as the pinned backend.
- [x] `Ensure`/`Terminate`/`FreshPrefix`/`StoreConfig` keep their signatures.
- [x] The Docker-unavailable policy test still asserts a failure, not a skip, and still passes.
- [x] The package compiles and its own tests pass (`go test ./internal/tests3/`).

Each checked criterion cites its file:

- Directory rename and file names: `git mv` output in the session note.
- `rg -i 'minio|9000' internal/tests3` → no matches (exit 1).
- `Image` constant with the pinned-backend comment: `internal/tests3/s3.go`.
- Signatures: `Ensure`, `Terminate`, `FreshPrefix`, `StoreConfig` in `internal/tests3/s3.go`.
- Policy test: `TestRequireFailsWhenDockerIsUnavailable` in `internal/tests3/s3_test.go`.
- Package compile and tests: `go test ./internal/tests3/` → ok.

## Error coverage

| Failure                        | Expected outcome                                     | Required check                            |
| ------------------------------ | ---------------------------------------------------- | ----------------------------------------- |
| Docker unavailable             | `Ensure` fails with the actionable diagnostic        | `TestRequireFailsWhenDockerIsUnavailable` |
| Container start fails          | `start` returns a wrapped `start s3 container` error | `tests3.start`                            |
| S3 gateway never becomes ready | `Ensure` times out on the log wait                   | `wait.ForLog`                             |
| Bucket creation fails          | `start` returns a wrapped error and terminates       | `raw.CreateBucket`                        |

## Implementation notes

### Session 2026-08-17 (imago, worker session 1) — phase complete

Renamed `internal/testminio` to `internal/tests3` (`git mv` on the
directory and both files: `testminio.go` → `s3.go`, `testminio_test.go` →
`s3_test.go`). The exported surface is unchanged: `Image`, `User`, `Pass`,
`Bucket`, `Region`, `Suite`, `StoreConfig`, `Ensure`, `Terminate`,
`FreshPrefix`, and the `require` policy keep their signatures.

Rewrote the bootstrap to the pinned SeaweedFS image:

- `Image = "chrislusf/seaweedfs:4.42"`, documented as the current pinned
  implementation; the S3 contract is the invariant importers depend on.
- `start` runs one container on port `8333/tcp` with `Entrypoint`
  `["/bin/sh"]` and the inline `s3.config` followed by
  `weed server -s3 -s3.config /etc/seaweedfs/s3.json -dir /data`.
- Readiness is `wait.ForLog("Start Seaweed S3 API Server")` with a 30 s
  startup timeout — SeaweedFS exposes no MinIO-style health endpoint.
- The explicit `CreateBucket` step is retained for determinism even
  though SeaweedFS auto-creates buckets on first write.
- Error wraps are now `start s3 container`, `s3 host`, `s3 port`; the
  `require` fatal text is `s3 integration unavailable: <cause>` with the
  same actionable Docker diagnostic.
- Package doc, suite comments, and the `FreshPrefix` panic string are
  vendor-neutral; the unit-test fixture endpoint became
  `http://127.0.0.1:8333`.

Found while implementing: `testcontainers.ContainerRequest.Entrypoint`
is `[]string`, so the spec's `"/bin/sh"` literal needed the slice form;
corrected in `s3.go`.

Verification (exact commands and results):

- `go test ./internal/tests3/ -count=1 -v` → ok, both policy tests pass
  (no Docker needed for the unit tests).
- `go vet ./internal/tests3/` → ok; `gofmt -l internal/tests3` → no
  output.
- `rg -i 'minio|9000' internal/tests3` → no matches (exit 1).
- Temporary smoke test `TestSmokeEnsureStartsContainer` (removed after
  acceptance): `Ensure` started the SeaweedFS container, the log wait
  fired, and the raw client listed the shared `slivingdoc` bucket
  (`endpoint http://localhost:33149`, PASS in 3.4 s).

Deferred to later phases by design: the importers (`internal/s3store`,
`internal/notebook`, `internal/integrationtest`) still reference
`internal/testminio` and compile-fail until Phase 2 rewires them; docs
and CI references update in Phases 3 and 4.

## Review findings

No reviews recorded.
