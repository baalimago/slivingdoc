# Testing

How slivingdoc is tested: one Go command and one npm command, the test layers from unit tests to black-box process scenarios, the seams that replace every external effect, the storage contract suite, fault injection and failpoints, and the pinned S3-compatible container. It answers "where does a test for this behavior go, what does it run against, and why is the gate so strict?"

Read this when: adding or changing a test, a scenario, a fault or failpoint, the S3 test backend, the hosted reference gateway, the Makefile `test` target, coverage, or anything that could make the gate slow or flaky.

No test uses live AWS resources, the live hosted service, or the live site. Real-S3 tests use the pinned S3-compatible container image (currently SeaweedFS), run through the Docker Engine API by `internal/tests3`; hosted-mode tests use the in-process reference gateway `internal/httpstore/gatewaytest`; login tests use the reference site `internal/sitelogin/sitetest`.

## Key files

| File | Purpose |
|------|---------|
| `Makefile` | `test` (lease + the one `go test` command + coverage floor), `cover`, `npm-test`, `lint`, `fmt`, `qa`; `$(BIN)` and `$(TEST_S3_LEASE)` prerequisites |
| `internal/integrationtest/doc.go` | Package contract: black-box scenarios, store selection rules |
| `internal/integrationtest/harness.go` | `HarnessConfig`, `NewHarness` (real `app.NewService` + `mcp.NewServer` over a `Recorder(faultStore(base))` stack), `Harness.Client`, `CallTool`, `Pull`, `Commit`, `WriteFile`, `ReadFile`, `FSSnapshot` (and `fsSnapshot` for process scenarios without a harness), `Manifest`, `ManifestAbsent`, `ObjectExists`, `ListObjects`, `WorkspaceFailpoints`, `NotebookFailpoints`, `Faults`, `Recorder`, `Logs` |
| `internal/integrationtest/faults.go` | `faultStore` via `NewFaultStore`: `fake.Injector` failures/ambiguity/barriers plus `UnprovableNext`, `CorruptRead`, `CorruptReadPrefix`, `RestoreRead`, `FailDeletes` |
| `internal/integrationtest/recorder.go` | `Recorder` (`Count`, `CountKey`, `CountKeyPrefix`, `KeysWithPrefix`, `Snapshot`), `Op` alias of `fake.Op`, `AllOps` |
| `internal/integrationtest/scenario.go` | Scenario DSL types: `ToolCall`, `CallExpectation`, `Expectations`, `FSAssertions`, `S3Assertions`, `LogExpectations`, ... |
| `internal/integrationtest/assertions.go` | `StateRecord`, `PackCacheDir`, `SharedPackCacheDir` |
| `internal/integrationtest/logcapture.go` | `LogCapture` handler for log assertions |
| `internal/integrationtest/main_test.go` | `TestMain` (helper-mode dispatch, `tests3.Start`, the memory scratch directory of `scratch_linux_test.go` / `scratch_other_test.go`), `helperMain` (runs `cli.Run` in a re-executed test binary; its `OpenBrowser` always fails and its `Sleep` waits a hundredth of the asked time; `helperSignalsEnv` gives it the operating system's signals and `helperSiteRouteEnv` routes every site request to a reference site through `routedSite`), `spawnHelper`, `spawnHelperIn` (a fresh `SLIVINGDOC_CONFIG_DIR` and an empty `HOME` per helper), `sanitizedEnv` (drops the AWS variables, `HOME` and the storage, token, site and credentials choices) |
| `internal/integrationtest/scenario_*_test.go` | One file per use case: pull, commit, conflict, checkpoint, recovery, integrity, error taxonomy, readonly, writable, path security, validation, transport, config, cli, ephemeral, shared cache, logging, log flags, colour, result, hosted, login |
| `internal/integrationtest/scenario_hosted_test.go` | CLI and `serve` processes (helper mode `real`, `SLIVINGDOC_TOKEN` set) against `gatewaytest`: round trip, storage full with compaction and `REQUEST_LIMIT`, a read-only token, startup refusals; a space deleted or its grant moved (`hostedCuts`) mid-session under `serve` and between a one-shot pull's access check and its read of `current` (`gatewaytest.BeforeNextObject`), and a moved grant (`moveHostedGrant`) during entry recovery (the in-process harness over the real hosted adapter, for the workspace failpoints): `ACCESS_DENIED` with the notebook directory byte-identical |
| `internal/integrationtest/scenario_login_test.go` | CLI and `serve` processes against `sitetest` and `gatewaytest` with a shared `SLIVINGDOC_CONFIG_DIR` and no `SLIVINGDOC_TOKEN`: login then pull, commit and `serve` with minted tokens alone (`gatewaytest.Used`, the key never at the gateway) and the file modes; the account lifecycle (login, `space`, `space <name>`, `serve` renewing a short-lived token (`sitetest.SetMintLifetime`) and re-minting after `gatewaytest.Revoke`, logout revoking the key and its tokens); denied/expired/refused polls, broken issuing answers and an interrupt mid-poll (`TokenHint`); `--space` rules, unrequested access and an untrusted code or page; the default site's endpoint rule (`helperSiteRouteEnv`); `--storage` selection (an S3 choice is proven by a probe failure against a closed loopback port with `AWS_MAX_ATTEMPTS=1` and no gateway request); an expired login; logout and a 401 that keeps the login; another account's and another site's approval; two logins needing `--endpoint`; credentials files that are linked, FIFOs, exposed or oversize; a file of another version; concurrent logins (`sitetest.HoldIssues`); `SLIVINGDOC_TOKEN` beside a broken file, on a server without the lookup beside a stored default space, and against another space; the S3-signal ambiguity refusals and the startup record ([login.md](./login.md)) |
| `internal/sitelogin/sitetest/site.go` | Test-only reference site of the CLI login routes with scripted approvals ([login.md](./login.md)) |
| `internal/integrationtest/pure_test.go` | Unit tests of the recorder and fault wrapper |
| `internal/storage/contract/suite.go` | `contract.Run(t, Factory)`: one `ObjectStore` suite for every backend |
| `internal/httpstore/gatewaytest/gateway.go` | Test-only reference server of the hosted storage API over the fake store: grants, quotas, injected refusals ([hosted-mode.md](./hosted-mode.md)) |
| `internal/httpstore/store_test.go` | `contract.Run` against the gateway, plus status mapping, retries, redirects, paging |
| `internal/storage/fake/fake.go`, `inject.go` | In-memory store with real conditional writes; `Injector` shared with `faultStore` |
| `internal/tests3/s3.go`, `internal/tests3/docker.go`, `internal/tests3/lease/main.go` | Pinned SeaweedFS suite, its minimal Docker Engine API client (`dockerClient`, `container`), and the `make test` lease (see [s3store.md](./s3store.md)) |
| `internal/notebook/failpoints.go` | `notebook.Failpoints{CAS}` (after CAS acceptance, before local acceptance) |
| `internal/workspace/materialize.go` | `workspace.Failpoints{Scan, Stage, Replace, Baseline}` |
| `internal/app/service.go` | `ServiceHooks{Workspace, Notebook}`: how failpoints reach a running service |
| `internal/app/process_test.go` | Process-body tests: re-executed helper over stdio, startup refusals, shutdown |
| `internal/git2/*_test.go` | Component tests against real libgit2 in temporary directories |
| `release_test.go` | Release layer: dependency baselines, checksum grammar, workflow reference, MCP Registry card, the built binary and its command surface |
| `npm/slivingdoc/test/*.test.mjs`, `helpers.mjs` | Launcher suite against a local `node:http` server and fixture artifacts |

## Flow

```text
make test
  $(BIN)            → CGO_ENABLED=1 go build ... -o .build/slivingdoc   # warms the release compile cache
  $(TEST_S3_LEASE)  → go build ./internal/tests3/lease
  tests3-lease --ready-file <tmp>/endpoint &                            # one SeaweedFS for the run
  SLIVINGDOC_TESTS3_ENDPOINT_FILE=<file> \
    go test -race -count=3 -timeout=30s -coverpkg=./... -coverprofile=.build/cover.out ./...
  go tool cover -func → fail below 70 %
  trap: SIGINT the lease, remove the ready file

scenario test
  NewHarness(t, HarnessConfig{Store: fake.New(p) | nil (real S3), Hooks, ...})
    raw store → NewFaultStore(raw) → NewRecorder(faults) → app.NewService → mcp.NewServer(LogCapture)
  h.Pull/Commit(name, path, msg) → in-memory MCP client session → tools/call
  assert: envelope, files on disk, manifest, Recorder counts, LogCapture records

process scenario
  spawnHelper(t, mode, env, args...) → os.StartProcess(os.Args[0]) with SLIVINGDOC_INTEGRATION_HELPER=<mode>
    → TestMain → helperMain → cli.Run(..., git2.New(), ProcessOptions{StoreFactory: fake | bad-store | nil (real S3)})
  parent speaks JSON-RPC (serve) or reads the report (pull/commit) and checks exit code and streams
```

## Behavior

**Two commands.** `make test` for Go and `make npm-test` for the launcher. Nothing else runs tests. No build tag, environment variable, or flag hides part of the suite (the only build constraints are platform ones: `release_test.go` is `!windows`, and `_linux`/`_unix` files): there is no short mode, no separate integration target, and no race-only target. `make qa` runs `lint`, `test` and `npm-test`; `make release` also runs the npm tests before tagging.

**The gate.** `go test -race -count=3 -timeout=30s -coverpkg=./... ./...`. The timeout covers the whole three-count run of one package, so a slow suite, a sleep instead of a poll, or state leaked between counts fails the gate. Scenarios run with `t.Parallel()` and take isolation from per-test S3 prefixes and per-test workspace, private and cache roots. Packages run concurrently with no `-p` bound; serializing them cost about 3x wall clock on a four-core runner for no headroom. Do not weaken `-race`, `-count`, or `-timeout`, and do not add a subset mode; a test too slow for the budget is a design problem.

**Pre-build.** `make test` builds the release-style binary first. The release layer (`release_test.go`, `releaseBinary`) builds a release-style binary (same flags, a test version string) once per test process; on a cold runner that alone takes about 35 s, over the 30 s budget, so without the pre-build the gate passes only on a warm cache.

**Prerequisites, not options.** The pinned static libgit2 (`.build/libgit2`, built by `make libgit2`) and Docker are required. There is no pure-Go build, so `CGO_ENABLED=0` fails at compile time. An unreachable Docker daemon fails the real-S3 suites with an actionable diagnostic and never skips (`tests3.require`, pinned by `TestRequireFailsWhenDockerIsUnavailable`). The only legitimate skips are platform capabilities (symlinks, FIFOs, hard links, Unicode normalization forms, case-folding hosts, a pseudo-terminal or character device for colour tests), and they name the capability.

**Shared S3 lease.** The lease starts one SeaweedFS container and writes its loopback endpoint to an ephemeral ready file. Container startup overlaps compilation and non-S3 packages; real-S3 test binaries wait only until the endpoint is ready. Running the displayed `go test` command directly still works: without the ready-file variable each package process starts its own container once.

**Test layers.**

| Layer | Owner | Evidence |
|---|---|---|
| Unit | all packages | pure validation and error mapping |
| Component | `internal/git2` | real libgit2 trees, merges, packs, shallow history |
| Contract | `internal/storage/contract` | one `ObjectStore` suite against the fake, real S3, and the hosted adapter over `gatewaytest` |
| Integration | `internal/notebook`, `internal/s3store`, `internal/httpstore` | publication, CAS, checkpoint, cleanup, compaction of a full space, S3 and hosted API requests |
| Scenario | `internal/integrationtest` | black-box MCP use cases over in-memory and process transports |
| Protocol | `internal/mcp`, `internal/app` | schemas, envelopes, stdio, configuration, shutdown |
| Release | root package | dependency baselines, checksum grammar, release reference, the built binary |
| npm | `npm/slivingdoc/test` | platform selection, checksum, streams, exit status |

**Seams for external effects.** Every effect has a narrow seam, so unit tests need no network:

| Effect | Seam |
|---|---|
| S3 | `storage.ObjectStore` with `storage/fake`; `app.ProcessOptions.StoreFactory` |
| Hosted storage API | `gatewaytest` (a local `httptest` server); `httpstore.Config.Client`, `Retries`, `Backoff` |
| Login site, spaces and minting, browser, poll timing, host name, the confirmation prompt, interrupts | `sitetest` (a local `httptest` server); `sitelogin.Config.Sleep`, `Now`, `Client`; `app.ProcessOptions.SiteClient`, `OpenBrowser`, `Sleep`, `Hostname`, `Terminal`, `Stdin`, `Signals` |
| Stored logins | `SLIVINGDOC_CONFIG_DIR` in the injected environment (`credentials.Locate` never reads the real process environment) |
| Git behavior | `git.Engine` / `git.Repository` interfaces with fake repositories in `notebook` and `workspace` tests |
| libgit2 | real component tests in temporary directories |
| Filesystem | per-test temporary roots |
| Time and backoff | `notebook.Config.Waiter` (and `Now`) |
| Publication IDs | `notebook.Config.NewID` |
| MCP | in-memory SDK transport (`Harness.Client`) and real process tests |
| Signals, streams, env, cwd, cache dir | `app.ProcessOptions` |
| npm download | local `node:http` server with fixture artifacts |

**Scenarios are the spec.** Public behavior changes start in `internal/integrationtest`. MCP JSON-RPC is the only entry; scenarios never call notebook, git, workspace or storage functions directly. Where prose and a passing scenario disagree, the scenario wins. Do not change an assertion until you understand the contract it protects.

**Store selection.** `NewHarness` with a nil `Store` builds the real `s3store` against the shared SeaweedFS on a fresh prefix; with an injected store, `Prefix` is required and `Bucket` defaults to `test-bucket`. Scenarios whose evidence must be real HTTP conditional writes use real S3: CAS races, competing checkpoint workers, the stale-reader restart, and cleanup after a checkpoint, plus CLI process scenarios (helper mode `real`) that need state across one-shot processes. Helper mode `real` leaves the store factory nil, so a scenario that sets `SLIVINGDOC_TOKEN` and `SLIVINGDOC_ENDPOINT` (the hosted scenarios) runs the real hosted adapter against a `gatewaytest` server instead; `sanitizedEnv` drops both variables from every other helper, together with `SLIVINGDOC_STORAGE`, `SLIVINGDOC_SITE` and `SLIVINGDOC_CONFIG_DIR`, and `spawnHelperIn` gives every helper an empty credentials directory, so a developer's login never makes a scenario hosted; the login scenarios pass one directory to every process they chain. `TestScenarioHostedSpaceGoneEntryRecovery` instead injects an `httpstore` store over a `gatewaytest` server into `NewHarness`, because the workspace failpoints reach only the in-process harness. Most other scenarios use `newFakeHarness` (`fake.New("scenario")`), which is contract-equivalent because `contract.Run` proves the fake and the adapter agree. The startup probe does not run in the harness (it lives in the process body), so recorder counts start at zero.

**Faults.** `faultStore` wraps any base store, including real S3, and injects what a real store cannot produce on demand: one-shot or permanent failures by key or prefix, accept-then-error ambiguity (`AmbiguousNext`, `AmbiguousNextOp`), unprovable CAS read-back (`UnprovableNext`), corrupted reads, failing delete batches, and op+key barriers (`BlockNext`, `BlockPrefix`, `Release`, `Waiting`) with a 5 s bound so a failed assertion cannot strand a blocked operation. Barriers make CAS winners and losers deterministic. The fake store has the same injector with a 10 s bound.

**Failpoints.** Deterministic local failures go through `app.ServiceHooks`: `notebook.Failpoints.CAS` fires after the manifest CAS accepted and before local acceptance (the window the generic recovery path repairs); `workspace.Failpoints` `Scan`, `Stage`, `Replace`, `Baseline` fire at each materialization boundary. The harness exposes them through `WorkspaceFailpoints()`/`NotebookFailpoints()` and they can be changed between calls. Production leaves them nil.

**Process tests and the race sleep.** Process scenarios re-execute the race-built test binary as the process body. A race-built program sleeps `atexit_sleep_ms` (1000 ms by default) on every clean exit; with dozens of helpers per `-count=3` run that is about 45 s. `spawnHelperIn` sets `GORACE=atexit_sleep_ms=0` for helpers only. Race detection of the helper's work is unchanged; the `internal/app` package binary keeps the default and still observes the shutdown path.

**Parallel slots.** `go test` runs at most `GOMAXPROCS` parallel tests at once (`-parallel` defaults to it), and a test that waits (on a child process, a barrier, a sleep) holds its slot while the CPU idles; the integration package is bound by its slots and, on a 4-core runner that runs other packages beside it, by CPU. So a scenario never sleeps for a real lifetime or interval (the login helper's `Sleep` waits a hundredth of the site's interval; the lifecycle scenario gives minted tokens a 100 ms lifetime), and one that needs two processes at once starts both with `spawnHelper` and then waits for each (`TestScenarioConcurrentLoginsKeepBoth`), rather than running them as parallel subtests that take extra slots and wait for each other. A step whose rule a unit test already pins is not repeated as another process in a scenario.

**What a helper costs.** Every helper process runs the whole test binary's package initialization before `TestMain` dispatches it, so every package the integration binary links pays its `init` once per helper, hundreds of times per run. That is why `internal/tests3` talks to Docker through its own `net/http` client (`docker.go`) instead of testcontainers-go: the latter's initializer lists every process on the host (to derive a session ID), which cost about 10 ms per helper on a quiet machine and more on a loaded runner, and with the Docker client libraries it linked it was nearly half of a helper's CPU. Keep heavy initializers out of the packages `internal/integrationtest` links; `GODEBUG=inittrace=1` on a helper shows them.

**Scratch on a memory filesystem.** On Linux, when `TMPDIR` is unset, `TestMain` points `TMPDIR` at a fresh directory under `/dev/shm` (`memoryScratch`, `scratch_linux_test.go`), so every `t.TempDir` of the scenarios and every helper's temporary directories live in memory, and removes it after the run. On a disk the workspace's and the credentials file's fsyncs and the creation and removal of each private repository's files were about half of the suite's system CPU and a third of its wall time; on the memory filesystem the same code runs and its fsyncs succeed, and nothing a scenario asserts depends on the disk. It applies only to a tmpfs `/dev/shm` with at least 1 GiB free (a container's 64 MiB one would fail scenarios with ENOSPC); otherwise, and on other systems (`scratch_other_test.go`), the suite says why on stderr and keeps the default. The directory is named after the test process's PID, and a later run removes the directory of a run that no longer exists (`removeAbandonedScratch`), since a timeout's panic skips the cleanup.

**Coverage.** 70 % statement coverage is the floor (the Makefile fails below it); 90 % is preferred. `-coverpkg=./...` is required because the black-box suite exercises other packages; per-package coverage understates it badly. `make cover` opens the last profile.

**Other gates.** `make lint` runs gofumpt (list mode), `go vet`, staticcheck, and `go fix -diff`; `make fmt` applies gofumpt. These are not tests. AGENTS.md also lists `dupl -t 80` as a signal.

## Gotchas

- A `testing.Short()` guard, a Docker-conditional skip, or a second "full gate" command hides coverage; none is allowed.
- A scenario with an injected store must set `HarnessConfig.Prefix` to the prefix the store was built with (`NewHarness` fails otherwise); `Bucket` defaults to `test-bucket` and `Endpoint` to empty.
- `HarnessConfig.RetryLimit`, `CheckpointPacks`, `RetainedCheckpts` are pointers because zero is a meaningful value.
- `helperMain` needs a per-helper cache dir (`helperCacheEnv`); a shared path leaks private state across tests and counts.
- The SeaweedFS pin lives in `tests3.Image` and in `.github/workflows/ci.yml`; keep them in sync.

## Related

- [storage.md](./storage.md): contract suite and fake.
- [s3store.md](./s3store.md): `tests3` and the lease.
- [hosted-mode.md](./hosted-mode.md): the reference gateway and the hosted scenarios.
- [build.md](./build.md): libgit2 prerequisite, CI workflow.
- [cli.md](./cli.md): the process body the process scenarios run.
- [AGENTS.md, Integration Tests](../AGENTS.md#integration-tests) and [QA validation](../AGENTS.md#qa-validation).
