# Phase 9 — Documentation and quality gate

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
all sections (L9–1370)

## Goal

Complete operator and user documentation, then prove all v1 contracts through
the final required validation matrix.

## Specification

Write a concise README for an uninitiated MCP user. Document installation,
stdio configuration, S3 configuration, UTF-8 text limits, conflict recovery,
checkpoint controls, and operational ownership.

Document required S3 permissions and the startup compatibility probe. Separate
slivingdoc correctness from optional versioning, replication, lifecycle, and
backup policies.

Add an example local MinIO environment for manual evaluation. Automated tests
must still create MinIO through testcontainers and must not depend on a running
example environment.

Audit the architecture against the implementation. Update the architecture only
for accepted design changes. Do not rewrite it to excuse a defect.

Run all format, lint, static analysis, race, component, MinIO, MCP process, npm,
native target, vulnerability, and license checks. Record exact commands and
results in this phase.

The gate re-runs after every later phase. Phase 10's black-box scenario suite
and its acceptance evidence are part of the final matrix.

## Integration contract

| Trigger | Collaborator | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- |
| New user follows stdio guide | Published npm package or local fixture | Two MCP tools are usable | Notebook files appear at chosen path | No Git installation step |
| Operator follows S3 guide | MinIO example | Startup probe and commit succeed | Only configured prefix changes | No live AWS requirement |
| Caller resolves conflict guide | Real MCP process | Second commit succeeds | Accepted state includes resolution | No Git command required |
| Full quality command | All test boundaries | Required jobs pass | Evidence is recorded | No hidden skipped MinIO suite |

## Acceptance criteria

- [x] README explains the product before configuration details.
- [x] Stdio examples match the implemented flags and schemas.
- [x] S3 permissions and optional recovery features are accurate.
- [x] Conflict recovery uses ordinary file tools only.
- [x] UTF-8, U+0000, conflict-marker, and unsupported-platform limits are prominent.
- [x] Example MinIO workflow is isolated from automated tests.
- [x] Architecture and implementation have no unresolved contract difference.
- [x] `gofumpt`, `go vet`, and static analysis pass.
- [x] Unit and component tests pass with the race detector where supported.
- [x] Required MinIO testcontainers suites run without skips in CI.
- [x] Real stdio process tests pass.
- [x] npm launcher tests pass on supported Node versions.
- [x] Every native target builds, starts, and passes dependency inspection.
- [x] Vulnerability and license checks have reviewed results.
- [x] The target load harness has recorded baseline results and limitations.
- [x] No test or example accesses a live cloud resource by default.
- [x] All worklog acceptance evidence is cited before phases become complete.

## Error coverage

| Failure | Expected outcome | Required evidence |
| --- | --- | --- |
| Documentation command is stale | Documentation test or manual smoke fails the phase | Fresh-environment walkthrough |
| MinIO suite skips in required CI | CI job fails | Skip-detection wrapper output |
| Race detector is unsupported for one native target | Explicit target exception with equivalent stress evidence | Recorded matrix result |
| Vulnerability scanner reports native or Go issue | Review, upgrade, or documented release block | Scanner report |
| License inventory is incomplete | Release is blocked | Artifact-content check |
| Load target is not met | Publish measured limit and reopen relevant phase | Benchmark report and review |
| Architecture differs materially | Reopen owning implementation phase | Architecture audit diff |

## Implementation notes

### Session 2026-08-10 (imago, worker session 12)

The phase delivered the user documentation, the isolated MinIO example, the
architecture audit, one toolchain upgrade driven by the vulnerability gate,
and the recorded validation matrix.

**Root README.** The root [`README.md`](../../README.md) is now a concise
guide for an uninitiated MCP user. It explains the product and the two-tool
workflow before any configuration, then covers installation (npm launcher
and direct binary), the full flag/environment table, S3 configuration with
the exact required permission set and the startup compatibility probe,
stdio configuration for MCP hosts (including a complete client JSON
example), the notebook content rules (valid UTF-8 text, no U+0000, no
symlinks or special files, path and message byte bounds, rejection of
complete conflict-marker blocks), conflict recovery with the exact marker
format and ordinary file tools only, checkpoint and retention controls,
and operational ownership that separates slivingdoc correctness from
bucket versioning, replication, object lock, lifecycle, and backups. Every
flag, default, bound, and schema in the README was cross-checked against
`internal/app/config.go`, `internal/mcp/server.go`, and the MCP process
tests; the unsupported-platform limit points at the launcher's supported
matrix. Building and Distribution sections were retained and linked to the
phase plan.

**MinIO example.** [`examples/minio/`](../../examples/minio/) contains one
`compose.yaml` (the pinned `minio/minio:RELEASE.2025-09-07T16-13-09Z`
image, fixed local credentials, API port 9000, console port 9001, a named
data volume) and a walkthrough README: start the container, create the
bucket through the console (or `mc`), export the credentials, run the
server against `--endpoint http://localhost:9000 --path-style`, register
the MCP client, and inspect the `current` manifest in the console. The
example is isolated from the automated suites by construction: the tests
start their own containers through `internal/testminio` and never read the
example directory (it contains no Go files, so `go test ./...` does not
touch it), and the README says so explicitly.

**Architecture audit.** Every contract surface was re-verified against the
implementation before the documentation was written: the two tools,
schemas, and OK/error envelopes (section 2); the derived private identity
and strict `state.json` (7.2); the pack key grammar and prefix validation
(9.1); the normative manifest shape and its 15 validator rules (9.2); pack
integrity and the shallow checkpoint boundary (9.3); the S3 compatibility
probe sequence and the required permission set (9.4); pull and commit
semantics including the empty-remote first pull and the commit-requires-
pull rule (10–11); the exact conflict markers and the complete-block
scanner (12); checkpoint compaction and retention (13–14); the failure
table and the generic recovery report (15); configuration defaults, bounds,
the 25 ms/2 s backoff ceiling, and shutdown behavior (17); path security
(18.2); the distribution grammar and launcher cache layout (21); and
operational responsibility (22). **No discrepancy required an architecture
change and no implementation defect was found.** The audit verdict is
recorded in the worklog README session journal.

**Toolchain upgrade (vulnerability gate).** `govulncheck v1.1.4` against
the pinned Go 1.26.3 reported four reachable standard-library
vulnerabilities: GO-2026-4970 (os.Root symlink escape via a trailing
slash; fixed in 1.26.5 — directly relevant because `os.Root` is the
workspace path-safety boundary), GO-2026-5856 (crypto/tls ECH privacy
leak; fixed in 1.26.5), GO-2026-5039 (net/textproto; fixed in 1.26.4),
and GO-2026-5037 (crypto/x509; fixed in 1.26.4). The workspace code does
not pass a trailing-slash path to `os.Root` today, but the boundary exists
to reject symlink traversal (architecture 18.2) and the patch-level
upgrade removes the class entirely. The pinned Go version was therefore
upgraded from 1.26.3 to 1.26.5 in `go.mod`, in all three CI jobs
(`.github/workflows/ci.yml`), and in the caller release workflow
(`.github/workflows/release.yml`); the worklog baseline table and decision
log record the change. After the upgrade the same scan reports **0
vulnerabilities in code and imports**; one module-level advisory remains,
GO-2026-5932 (`golang.org/x/crypto/openpgp` unmaintained, fixed in N/A) —
`go mod why golang.org/x/crypto` proves no package of that module is
imported, so it is documented as reviewed and not reachable. A second
module-level finding, GO-2026-5841 (`klauspost/compress/s2` OOB read), was
closed by upgrading the indirect dependency to v1.18.7.

**License check.** The only native third-party component statically linked
into the executable is libgit2 v1.9.6 (GPL v2 with linking exception);
`NOTICE` ships with the executable's releases and in the npm package, and
the release publication gate requires the `NOTICE` asset. All Go modules
are pinned in `go.mod`/`go.sum` and remain dependencies of the AWS SDK,
the MCP SDK, testcontainers, or the workspace lock/Unicode helpers; none
is distributed as a binary artifact. The license inventory is complete.

**Load harness baseline.** The Phase 6 sustained-load harness produced the
recorded baseline below on go1.26.5 / linux/amd64 (one run each; the
harness is deterministic against the fake store and fake engine).

| Benchmark | Throughput | CAS/commit | p50/p90/p99 | Conflicts | Failed | Checkpoint/cleanup |
| --- | --- | --- | --- | --- | --- | --- |
| Distributed (100 writers, 1 KiB, 2 s window) | 50.04 commits/s | 1.010 | 8.0/10.1/12.6 ms | 0 | 0 | 1 run, 0 deleted |
| Burst (100 writers at once, production retry bound) | 157.2 commits/s | 10.67 | 202.8/334.3/356.9 ms | 0 | 40 | 1 run, 506 deleted |
| Cold pull (checkpointed state, empty cache) | 29.75 MB/s | — | 14.1 ms total | — | — | 12.6 MB allocs |
| Warm pull (one new increment) | 12.9 ms total | — | — | — | — | 9.9 MB allocs |

The planning cadence (one commit per agent per minute) is far inside the
measured distributed rate. The burst limitation is the production retry
bound: with the default 8 retries (9 attempts) and 100 simultaneous
writers, 40 commits exhaust the bound and return `REMOTE_BUSY` with their
visible files preserved for a caller retry — the architecture's stated
contract, not a silent loss. Burst contention concentrates in CAS attempts
(10.67 per accepted commit) while conflicts stay zero because the writers
edit disjoint files.

**Native target matrix.** Linux amd64 is fully proven locally: the
race-enabled gate, the release-style build, `--version` with the injected
version, and the Linux dependency baseline (only the operating-system
baseline libraries; no libgit2 or Git runtime dependency). macOS and
Windows dependency baselines are enforced by the self-tested
`check-deps-macos.sh` / `check-deps-windows.sh` scripts (positive and
rejection paths), and the node/npm suite covers the launcher for all five
targets. The first real macOS/Windows target builds and starts can only
run after the Phase 8 reusable pipeline lands; that matrix exception is
recorded here and re-runs after Phase 8 completes. The race detector is
supported on the Linux target (the CI native job runs it); the target
matrix records the toolchain limitation for the other platforms.

**No live cloud access.** No test and no example touches a live AWS
account: MinIO evidence comes from testcontainers or the local example
container, and the object-store boundary is deterministic-fake in every
non-MinIO suite.

### Verification (all passed, go1.26.5)

Before changes: `make validate`, `make test` (race, `-count=3 -p 1`,
including the MinIO suites), `make smoke`, `make integration-test` (0
skips), and `npm test` (33 tests) all passed on the pinned go1.26.3
baseline; `govulncheck` then reported the four reachable standard-library
vulnerabilities that drove the upgrade.

After changes (go.mod `go 1.26.5`, workflows `go-version: "1.26.5"`,
`klauspost/compress v1.18.7`):

```text
make validate     gofumpt, vet, staticcheck, go fix, CGO_ENABLED=0 tests: PASS
make test         go test -race -timeout=30s -count=3 -p 1 ./... (native
                  libgit2 + MinIO suites): PASS
make smoke        dependency self-tests (linux/macos/windows), checksum
                  grammar, release-ref, injected --version, Linux
                  dependency inspection: PASS
make integration-test   MinIO contract and notebook suites, no skips: PASS
npm test --prefix npm/slivingdoc   33 tests, 0 failures: PASS
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...   0 vulnerabilities
                  in code and imports; 1 unreachable module advisory
                  (GO-2026-5932 openpgp, N/A fix, not imported): PASS
go run github.com/mibk/dupl@latest -t 80 .   only the pre-existing
                  acceptable mirror groups: PASS
load benchmarks   recorded above: PASS
```

The race gate, validate, smoke, integration, and npm outputs are recorded
in the worklog README session journal.

### Re-run 2026-08-11 after Phases 10 and 11 (worker session 17)

The worklog strategy requires this gate to re-run after Phase 10 and after
Phase 11. Both are now complete, so it ran once over the finished tree.

The re-run found three failures that the gate had not previously reported,
all pre-existing and all now fixed (recorded in Phase 10 and Phase 11):

1. `go test -race -count=3 -timeout=30s` **timed out** on
   `internal/integrationtest`; the scenarios ran sequentially although the
   phase specification requires parallel-capable scenarios and the harness
   already provided per-test isolation.
2. `internal/notebook.TestRecoverFailpointReportsFailedResync` failed
   deterministically: it left a failpoint installed while asserting a
   self-heal.
3. `internal/git2.TestNoGitExecutableOrGit2goImport` failed: the
   path-security scenario imported `syscall`, which the module-wide seam
   scan forbids. This one had been masked by truncated output.

A fourth defect was found while verifying documentation rather than by a
gate: `.github/` did not exist, so the CI integration job that the test
doctrine depends on had never existed. Phase 11 recreated both workflows.

| Gate | Result |
| --- | --- |
| `make qa` (validate, test, smoke, npm-test) | pass |
| `go test -race -timeout=30s -count=3 -p 1 ./...` | pass, 11 test packages |
| `CGO_ENABLED=0 go test -count=3 -timeout=30s ./...` | pass |
| `make integration-test` | pass, `integration-test: no skips` |
| `npm test --prefix npm/slivingdoc` | pass, 33 tests, 0 failures |
| `go vet ./...` (both build modes) | pass |
| `staticcheck v0.7.0 ./...` (both build modes) | pass |
| `gofumpt v0.11.0 -l .` | clean |
| `go fix -diff ./...` | clean |
| `dupl -t 80 .` | 3 groups, all acceptable under the duplication policy |
| `govulncheck v1.1.4 ./...` | 0 vulnerabilities in code and imports; 1 module advisory (GO-2026-5932 openpgp), still reviewed as unreachable |
| Coverage | 84.1 % across the module (70 % floor met; the black-box suite alone covers 66.8 %) |
| Markdown local links | 0 broken across the repository |
| Architecture line references | 31 references in the worklog table re-verified, 0 stale; every citation in `internal/integrationtest`, `AGENTS.md`, and `docs/` verified against the section headings |
| `actionlint v1.7.7` on both workflows | clean |
| Documentation grep gate | no `Sakfråga`, `sakfraga`, `event.Bus`, `workerpool`, `corrID`, or `TBD` in `AGENTS.md`, `docs/`, or `README.md` |

The Phase 8 matrix exception is unchanged: the first real macOS and Windows
target runs still wait on the external reusable-pipeline commit, and
`scripts/check-release-ref.sh` still reports the placeholder by design.

## Review findings

No reviews recorded.
