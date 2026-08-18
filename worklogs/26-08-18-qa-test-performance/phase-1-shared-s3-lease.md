# Phase 1 — Shared S3 lease

**Status:** Complete

**Worklog:** [README](README.md)

## Goal

Start one SeaweedFS testcontainer for `make test` and inject it safely into every real-S3 test package.

## Specification

Add a test-only `tests3` lease executable that starts the existing pinned
container through `tests3.Start`, writes one ready endpoint, and remains
alive until Make ends it. `make test` starts that executable before the
unchanged Go test command, waits for readiness, injects the endpoint only for
that command, and reaps the lease process on every exit path.

`tests3.Start` detects a non-empty injected endpoint. It must validate that it
is an HTTP loopback endpoint, construct the same raw client and fixed test
credentials, and produce an attach-only Suite. Attach-only `Terminate` is a
no-op, because only the broker owns container lifecycle. Without injection,
the present per-test-process container behavior remains unchanged.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- |
| `make test` | broker, three real-S3 package binaries | All existing tests run and pass against one endpoint | Exactly one SeaweedFS container is started for the invocation | Test skips, live endpoint, duplicate test containers |
| Broker cannot start Docker backend | Make, broker | Nonzero actionable diagnostic | Go test command does not run against a fake or absent store | Passing/skipping QA |
| Direct `go test` without injection | `tests3.Start` | Existing real-S3 tests start normally | One container per package process as before | Dependence on Make-only state |
| Injected endpoint is malformed/non-loopback | `tests3.Start` | Test startup fails | No request is made to the supplied endpoint | Live network contact |

## Acceptance criteria

- [x] The one unedited Go test command sees a broker-injected loopback endpoint. — `make test` passes through `SLIVINGDOC_TESTS3_ENDPOINT_FILE` without changing its Go command.
- [x] The full real-S3 contract, notebook, and black-box suites execute with their existing tests and fresh prefixes. — `make test` passed at 82.6% coverage.
- [x] The broker is the only process that creates or terminates an injected container. — attached Suites carry no container owner; the successful multi-package gate proves their `TestMain` cleanup did not stop the broker.
- [x] Direct `go test ./...` continues to use the legacy mandatory startup path. — direct full control passed in 42.58s without the ready-file environment.
- [x] Invalid injected endpoints are refused before any network request. — `TestAttachRejectsNonLoopbackEndpoint` and `TestEndpointFromFile` pass.

## Error coverage

| Failure | Expected result | Evidence |
| --- | --- | --- |
| Docker unavailable to broker | Nonzero, actionable `s3 integration unavailable` diagnostic | Broker-focused test / Make failure path |
| Broker exits before writing readiness | Make exits nonzero, no test runs | Make shell lifecycle check |
| Endpoint fails URL or loopback validation | `tests3.Start` returns an error | `internal/tests3` unit test |
| Endpoint is reachable but non-S3 | Existing contract/probe test fails | Full real-S3 gate |
| Test process calls `Terminate` while attached | Endpoint remains available to later package tests | Attach-only lifecycle test |

## Implementation notes

2026-08-18 — Added `internal/tests3/lease`, which owns `tests3.Start`,
atomically publishes a 0600 endpoint file, and terminates only on its own
interrupt. `make test` creates a unique absolute ready-file directory, starts
the broker, injects the file path into the unchanged Go test command, and
reaps the broker and directory on every shell exit. `tests3` attaches only to
an HTTP loopback URL and leaves lifecycle ownership nil; its direct-Go path is
unchanged. Focused `go test ./internal/tests3 ./internal/tests3/lease`, full
`make test`, and direct full `go test` passed. The source-level `EndpointEnv`
is retained as a direct injection seam; Make uses the asynchronous file form
to overlap service readiness with the rest of the gate.
