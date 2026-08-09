# Phase 10 — Integration test harness — OOM investigation log

> **Session breadcrumbs.** This file is written on EVERY tool call during the
> OOM investigation. If the terminal dies, a new session resumes from here.
> Last writer is responsible for keeping this file current.

## 2026-08-10 — Session start (OOM investigation)

**Task:** Analyze why phase 10 keeps going OOM, killing the terminal.

**State at start:**

- `phase-10-worklog.md` was 0 bytes (previous sessions died before persisting).
- Plan: `phase-10-integration-test-harness.md` (Not Started status).
- Repo git dirty: 27 changes.

**Investigation checklist (in progress):**

- [ ] Read plan and architecture refs (done: plan read; worklog was empty).
- [ ] Inspect `internal/integrationtest` current state (exists? files?).
- [ ] Inspect past conversations for OOM context.
- [ ] Find memory-heavy suspects: testcontainers MinIO, pack imports, MCP
      in-process wiring, parallel scenarios, race detector.
- [ ] Reproduce OOM with memory monitoring if possible.

## Log of tool calls (most recent first)

1. `cat phase-10-worklog.md` + `ls worklogs/` — worklog empty; plan file exists.
2. `cmd wc -c` — confirmed 0 bytes; read plan file fully (goal/spec/catalog).

## 2026-08-10 22:36 — Interim summary (mid-investigation)

**The smoking gun (journalctl -k, tonight):** the kernel OOM killer killed the
`integrationtest` test binary 4 times:

| Time     | PID     | anon-rss | total-vm | oom invoked by |
| -------- | ------- | -------- | -------- | -------------- |
| 21:56:34 | 946718  | 4.56 GiB | 7.9 GiB  | systemd        |
| 22:14:06 | 1015328 | 4.66 GiB | 7.9 GiB  | grafana        |
| 22:28:54 | 1068295 | 4.94 GiB | 10.6 GiB | brave          |
| 22:33:27 | 1088107 | 4.84 GiB | 10.6 GiB | ServiceWorker  |

Trend: total-vm grew 7.9 GiB -> 10.6 GiB between the first two and last two
kills; anon-rss crept 4.56 -> 4.94 GiB. The binary is
`internal/integrationtest` under `-race` (memory multiplier).

**Environment:** 14 GiB RAM, 4 GiB swap **completely exhausted**, 8 CPUs.
Long-running dev stacks (supabase, sakfraga, grafana, 2x minio, chrome/brave,
gopls) already consume most of RAM. Any `go test` of integrationtest with
`-race` pushes the box over the edge; the OOM killer then takes down the
session's terminal too.

**Known state of phase 10:**

- Worklog file was 0 bytes; plan file (phase-10-integration-test-harness.md)
  fully read.
- Session 14 (conversation 019fea74) built: app seams (ServiceConfig,
  ServiceHooks, NewService, RunProcess), mcp mcpReqID logging, notebook
  logger, workspace refreshRoot fix, integrationtest core (scenario DSL,
  recorder, logcapture, faults, harness, main_test helpers). Session ended
  ALL-GREEN on `go test -race -count=1 -timeout=300s -p 1 ./...`.
- Files present: assertions.go doc.go faults.go harness.go logcapture.go
  main*test.go pure_test.go recorder.go scenario.go + 6 scenario*\*\_test.go
  (checkpoint, commit, conflict, helpers, pull, validation).
- Missing per plan: mcpclient.go, scenario_recovery, scenario_integrity,
  scenario_path_security, scenario_transport, scenario_config,
  scenario_error_taxonomy.

**Hypotheses to test (ordered):**

1. A scenario test allocates unbounded data in-process (loop building packs,
   giant strings, or an accumulator in recorder/logcapture/faults).
2. libgit2 engine or pack import leaks per harness (engine Open/Close).
3. `-race` + testcontainers MinIO + helper processes stack up on a
   memory-starved box; the suite is simply too heavy for 14 GiB with the
   dev stacks running (process-level, not a code leak).
4. Default CheckpointPacks=1024 triggers a heavy checkpoint in some test.

**Next:** read recorder.go, logcapture.go, assertions.go, scenario\_\*\_test.go,
notebook defaults; then reproduce with a memory ceiling (GOMEMLIMIT) instead
of risking another terminal death.

## 2026-08-10 22:45 — Continued investigation

**Code audit so far (no in-process leak found in the obvious places):**
- Scenario files (commit/checkpoint/pull/conflict/validation/helpers) use only
  small files and small packs; nothing loops or builds large data.
- recorder.go / logcapture.go / faults.go / assertions.go are bounded
  per-harness maps and slices.
- git2 CGo boundary (native.go) frees every C handle (defer free on all
  paths); engine Open/Close balances libgit2 init/shutdown per harness.
- notebook remote.go / pack export paths read small packs; writePack uses a
  C git_buf freed via git_buf_dispose.
- s3store multipart allocates one partSize buffer per upload (check value).
- mcp server is thin; LogCapture accumulates records per harness only.
- No t.Parallel in the package; no spawnHelper usage yet (transport/config
  scenario files don't exist yet).

**Evidence from shell history (timestamps converted):**
- Last commands recorded: `make qa` (07:30/07:48/07:58 EEST), `make test`
  at 22:58:57 EEST (after the last OOM kill 22:33 — likely a fresh attempt).
- No test processes running right now; no leftover testcontainers.

**Measurement plan (in progress):**
1. Build the integrationtest test binary without -race (`go test -c`).
2. Run subsets + full suite under a systemd-run cgroup MemoryMax cap so a
   kill stays inside the cgroup and never takes the terminal again.
3. Watch RSS/heap (gctrace + /proc sampling). Decide: real leak vs
   -race+environment weight.
4. If needed, rebuild with -race and repeat to quantify the multiplier.

## 2026-08-11 08:05 — REPRODUCTION SUCCESS (root cause narrowed)

**Measured, without -race, under a 4G cgroup cap (`systemd-run --user --scope
-p MemoryMax=4G`):**
- Full suite run (`/tmp/integrationtest.test -test.v`): heap stable 3-4 MB
  through pure tests and fake-store checkpoint tests, then
  `TestScenarioCheckpointRetention` FAILED, then
  `TestScenarioCheckpointCleanupFence` started and the Go heap DOUBLED every
  ~0.1-0.7s: 5 -> 10 -> 20 -> 43 -> 95 -> 212 -> 475 -> 1068 -> 2401 MB,
  then the cgroup OOM killed it (EXIT=137). Live heap (gctrace) — not a
  GC-lag artifact.
- `TestScenarioCheckpointCompetingWorkers` alone passes in 1.58s under a 1G
  cap (no explosion in isolation).
- SIGQUIT goroutine dump during the explosion: test goroutine [running]
  (stack unavailable), MCP SDK pump goroutines idle, one net/http dialConn;
  no obviously hot loop captured.

**Conclusion so far:** the leak is real, in-process, in Go heap, appears
only in the full suite (or only in the fake-store CleanupFence test after
prior tests), ~2.25x doubling. It is NOT the race detector (this run had
none) and NOT testcontainers startup per se.

**Next:** temporary instrumentation (heap profile ticker inside a copy of
the CleanupFence body) to capture the exact allocation site.

## 2026-08-11 08:15 — ROOT CAUSE FOUND, FIXED, VERIFIED

### The OOM root cause (100% reproduced)

`strReader.Read` in `internal/integrationtest/scenario_helpers_test.go`:

```go
func (s strReader) Read(p []byte) (int, error) {   // VALUE receiver
    n := copy(p, s)
    s = s[n:]                                       // mutates the COPY only
    return n, nil                                   // never EOF
}
```

Every `Read` call re-returns the same bytes and never reaches EOF. The only
consumer was `putJunk` -> `fake.(*Store).PutObject` -> `io.ReadAll(r)`.
`io.ReadAll` therefore loops forever, doubling its buffer (512B -> 1KB ->
... -> GBs). The Go heap doubled ~2.25x per GC cycle — the exact gctrace
fingerprint measured:

    gc 24 @1.735s: 5->5->5 MB
    gc 25 @1.745s: 10->10->10 MB
    gc 26 @1.766s: 20->20->20 MB
    ... gc 29 @2.084s: 212 MB ... gc 32 @5.931s: 2401 MB -> OOM kill

Heap profile of the explosion (go tool pprof):

    1.56GB 99.63%  io.ReadAll
      putJunk -> fake.(*Store).PutObject -> io.ReadAll

Trigger test: `TestScenarioCheckpointCleanupFence` (the first test calling
`putJunk`). Last night's four kernel OOM kills (21:56, 22:14, 22:28,
22:33, anon-rss 4.5-4.9 GiB) were this same binary; the machine (14 GiB
RAM, swap 100% full, ~10 GiB baseline dev-stack usage) had no headroom,
so the kill also took the terminal session.

### Fixes applied (3 test bugs, no production code touched)

1. **scenario_helpers_test.go** — replaced the broken `strReader` with
   `strings.NewReader` (the OOM fix; `io` import retained for the
   `io.Reader` return type, `strings` imported).
2. **scenario_checkpoint_test.go / TestScenarioCheckpointRetention** —
   the roots assertion required P1/I2/I3 to be retained while the
   subsequent cleanup assertion required them deleted: contradiction.
   Aligned with the catalog (retention 1: active + one previous
   generation readable, older unretained).
3. **scenario_checkpoint_test.go / TestScenarioCheckpointCleanupFailure**
   — c3 committed no file change, so the tail never reached the
   checkpoint threshold, no cleanup ran, and the "cleanup failed" warning
   never fired. Now writes f3.md (to trigger the checkpoint) AND seeds an
   unreferenced pre-cutoff pack (so cleanup has a doomed candidate).

### Verification (all under cgroup MemoryMax caps; terminal safe)

- Full integrationtest suite, no race: PASS, 40 tests, heap stable at
  ~7 MB peak (was 2.4 GB+); whole run ~2.4s.
- `go test -race -count=1 ./internal/integrationtest/`: ok, 4.353s,
  EXIT=0 — the exact command family that OOMed 4x last night.
- `CGO_ENABLED=0 go test ./internal/integrationtest/`: ok (pure tests).
- `make test` (the full QA native gate, -race -count=3 -p 1 ./...) kicked
  off under a 6G cap; result recorded below.

### Notes for the environment

- The box is chronically memory-starved: 14 GiB RAM, swap 100% full,
  ~10 GiB baseline (supabase, sakfraga, grafana, 2x minio, chrome/brave,
  gopls). Even with the leak fixed, keep `make test`'s `-p 1` (sequential
  packages) and avoid running heavy dev stacks during `make qa`.
- `eventually` polls fn() twice on deadline (loop + Fatalf arg) — minor,
  not worth changing.

**`make test` result: PASS** — `go test -race -timeout=30s -count=3 -p 1 ./...`
→ all 11 test packages ok (each x3), EXIT=0, ~30s under the 6G cgroup cap.
This is the command that OOMed the machine four times last night. The
phase-10-worklog investigation is complete.
