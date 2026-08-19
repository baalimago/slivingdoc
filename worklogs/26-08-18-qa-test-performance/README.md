# QA test-performance worklog

**Status:** Complete

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§9 storage boundary, §20 test architecture; [`../../docs/testing.md`](../../docs/testing.md)
test layers and mandatory test command

## Objective

Make the existing full `make qa` gate faster and less variable without
weakening its race detector, three-count execution, timeout, coverage,
Docker requirement, scenario assertions, or real-S3 evidence. The change
starts one SeaweedFS test-service lease for the whole `make test` invocation,
then injects its loopback endpoint into every Go test package. Scenario
consolidation is permitted only when the same externally observable process
boundaries and assertions remain.

## Status board

| Phase | Status | Summary |
| --- | --- | --- |
| [1. Shared S3 lease](phase-1-shared-s3-lease.md) | Complete | Added one broker-owned SeaweedFS lease, ready-file injection, loopback validation, and direct-Go fallback. |
| [2. Scenario chains](phase-2-scenario-chains.md) | Complete | Audited every helper boundary; existing chains already cover compatible cases, so no unsafe consolidation was made. |
| [3. Validation and docs](phase-3-validation.md) | Complete | Measured the improvement, passed lint/Go/duplicate/Node checks, and documented the lifecycle. |
| [4. CI timeout margin](phase-4-ci-timeout-margin.md) | Complete | Removed three compatible helper boots per test pass while preserving every assertion. |

## Strategy

`go test ./...` executes package test binaries in separate processes, so the
existing `tests3` singleton is shared only within one package. Its endpoint
is already shared by all helpers in `internal/integrationtest`; helper
processes do **not** start containers. They do need fresh process startup to
prove the CLI and stdio surfaces, and that evidence must remain.

Phase 1 therefore adds a test-only broker process started by `make test`. It
owns one testcontainers SeaweedFS instance, publishes only its loopback
endpoint, and stays alive until the one unchanged Go test command exits. A
test binary that sees the injected endpoint attaches rather than starting or
terminating a container. Direct `go test` remains supported: without the
endpoint it starts one mandatory local container per test process exactly as
before. Fresh object-prefix allocation continues to isolate every test.

Phase 2 treats each helper process as a contract boundary, not incidental
setup. A sequential chain may share a server only when its configuration,
visible state, stderr/stdout guarantees, and expected lifecycle are exactly
the same as the original cases. One-shot `pull` and `commit` checks continue
to use distinct processes. Different startup configurations continue to use
distinct processes.

## Shared invariants

1. `make test` still executes exactly `go test -race -count=3 -timeout=30s
   -coverpkg=./... -coverprofile=.build/cover.out ./...`; no target, tag, or
   flag omits tests.
2. Docker remains mandatory. A broker startup failure or a direct test-suite
   startup failure is actionable and nonzero; no path skips a real-S3 suite.
3. The startup S3 compatibility probe, production configuration, and public
   tools are unchanged. The injected value is test infrastructure only.
4. The broker supplies a local testcontainers endpoint and fixed test
   credentials only. It never reads developer AWS credentials or contacts a
   live S3 service.
5. Every test retains its unique S3 prefix and existing process-boundary
   assertions. Tests can become sequential only where that retains all
   asserted behavior.
6. Test-container cleanup stays best effort and cannot consume the Go test
   binary's 30-second timeout; testcontainers' reaper remains the eventual
   cleanup authority.

## Review severity

| Severity | Meaning | Phase effect |
| --- | --- | --- |
| Critical | A live or credential-bearing endpoint can be injected, or test scope is skipped. | Reopen and block dependent phases. |
| Major | Broker lifecycle fails, suites lose isolation, or a process contract weakens. | Reopen the phase. |
| Minor | Non-contract documentation or maintainability issue. | Record and fix normally. |

## Session journal

| Date | Entry |
| --- | --- |
| 2026-08-18 | Baseline: default Go gate was dominated by `internal/integrationtest` at 30.432s; `s3store` 13.695s; `notebook` 9.862s. Higher test parallelism and concurrent lint made the gate slower. The integration package shares one container already, but the three real-S3 package binaries start independent containers. |
| 2026-08-18 | Replaced per-package SeaweedFS startup in `make test` with one `tests3-lease` broker. The asynchronous ready-file allows the unchanged Go command to compile and run non-S3 packages while the service starts. A same-source direct control took 42.58s; shared-service runs took 35.55s and 33.08s, with the slow integration package reduced to 24.650s. |
| 2026-08-18 | Audited helper scenarios. Transport, path-security, and CLI state flows each carried compatible internal chains. Configuration, logging, terminal, and one-shot CLI cases mostly need distinct process startup to prove their independent public contracts. |
| 2026-08-18 | GitHub Actions run 32156521209 timed out in `internal/integrationtest` at 30.343s. The follow-up found three compatible cross-scenario lifecycles: transport and Unix path-security use an unconfigured fake stdio server; the no-colour logging case uses the record-shape server configuration; and the config negative control duplicates the stronger startup-probe scenario. Phase 4 consolidated them, removing three helper boots per pass (nine across `-count=3`), then passed `make lint`, `make test` at 82.6% coverage, the 35-test launcher script, and `dupl` with zero clone groups. |
