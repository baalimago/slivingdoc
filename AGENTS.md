# AGENTS.md — slivingdoc

## Architecture

slivingdoc is a standalone MCP server that gives many agents one shared
directory of UTF-8 text notes, stored durably in S3-compatible object storage.
It uses Git data structures and merge behavior internally but never invokes a
Git executable and never exposes a Git repository. The full contract is
[`docs/slivingdoc-v1.md`](docs/slivingdoc-v1.md). Three states
shape every operation. **L** is the caller-controlled visible directory.
**P** is the server-owned private state (repository, baseline, locks).
**R** is the accepted remote state indexed by the S3 object `current`.

```text
     MCP client (agent)              human shell
 notes_pull / notes_commit      pull <path> / commit <path> -m <msg>
                       |         |
                       |  stdio JSON-RPC / one-shot argv
                       v         v
+----------------------------------------------+
|  main.go -> internal/cli                     |
|  command router: serve | pull | commit |     |
|  version                                     |
+----------------------+-----------------------+
                       |
                       v
+----------------------------------------------+
|  cmd/serve -> internal/app                   |
|  process body, engine lifecycle, version     |
+----------------------+-----------------------+
                       |
                       v
+----------------------------------------------+
|  internal/notebook.Notebook                  |
|  Pull . Commit . recovery . checkpoint .     |
|  cleanup  -- orchestration policy            |
+----------+----------------------------+------+
           |                            |
           | workspace ops              | storage protocol
           v                            v
+--------------------------+   +----------------------------+
|  internal/workspace      |   |  internal/storage          |
|  L: visible path         |   |  manifest v1, pack keys,   |
|  P: private dir          |   |  ETag CAS, probe           |
|  os.Root scan, state.json|   +-------------+--------------+
|  op lock (sem + flock)   |                 |
+------------+-------------+                 |
             |          ObjectStore adapter  |
             |                               v
             |                 +----------------------------+
             |                 |  internal/s3store (AWS SDK)|
             |                 |  --> S3-compatible bucket  |
             |                 +----------------------------+
             v
+--------------------------+
|  internal/git            |
|  Go seam + policy:       |
|  trees, snapshots, merge,|
|  packs, shallow history  |
+------------+-------------+
             |
             v
+--------------------------+
|  internal/git2 (CGo)     |
|  libgit2 v1.9.6 static;  |
|  never git2go, never a   |
|  Git binary              |
+--------------------------+
```

Runtime layout (implemented):

```text
L  visible directory       caller path; UTF-8 text files only
P  <private-root>/<key>/   state.json, operation.lock, repo/, staging/,
                           backup/, pulled, pack-cache/
R  <bucket>/<prefix>/      current                manifest v1, strict; the only
                                                  accepted-state authority
                           packs/checkpoints/<gen>-<id>.pack   complete state
                           packs/increments/<gen>-<id>.pack    one publication
```

Supporting packages sit beside the main path. `internal/strictjson`
supplies the strict JSON value tree shared by the manifest and
`state.json`. `internal/storage/fake` and `internal/storage/contract`
provide the deterministic object store and the one contract suite run
against both the fake and the real S3 backend. `internal/tests3` starts
the pinned S3-compatible testcontainers backend.

### Package Map

```text
slivingdoc/
|-- main.go                  entry point: cli.Run over os.Args
|-- cmd/                     the CLI commands (go_away_boilerplate/pkg/cmd)
|   |-- serve/               serve|s: the MCP stdio server over internal/app
|   |-- pull/                pull|p: one-shot notes_pull for humans
|   |-- commit/              commit|c: one-shot notes_commit for humans
|   `-- version/             version|v: the exact "slivingdoc <semver>" line
|-- release_test.go          release layer: dependency baselines, checksum
|                            grammar, release reference, built binary
|-- Makefile                 libgit2 build stamp; test / npm-test / lint /
|                            fmt / cover / build / qa / release / clean
|-- scripts/                 build-libgit2.sh; per-platform check-deps-*.sh;
|                            make-sha256sums.sh; check-release-ref.sh;
|                            release.go (the `make release` prompt flow)
|-- npm/slivingdoc/          zero-dependency launcher: platform mapping,
|                            verified download, cache, exec forwarding
|-- .github/workflows/       ci.yml (qa, npm, readme-coverage caller);
|                            release.yml (caller for the reusable pipeline)
|-- docs/                    slivingdoc-v1.md (the accepted contract),
|                            build.md, testing.md
|-- terraform/               reusable AWS module: bucket, IAM user, keys
|-- examples/seaweedfs/      isolated local SeaweedFS walkthrough
|-- examples/terraform/      debug configuration calling the module
|-- worklogs/                phased worklog: the implementation record
`-- internal/
    |-- app/                 process body: Flags, Setup -> Runtime, Serve;
    |                        engine open / version / features / close
    |-- cli/                 the command map, usage text, and router entry;
    |                        one definition shared by main and the scenarios
    |-- git/                 Go-facing engine seam and policy: BuildTree,
    |                        ReadSnapshot, Merge, MaterializeTree,
    |                        ExportIncrement / ExportCheckpoint, ImportPack,
    |                        MarkShallow, ValidateHistory, path and content
    |                        validation, commit messages
    |-- git2/                the ONLY CGo package: pinned libgit2 v1.9.6
    |                        boundary, version-verified at Open. There is no
    |                        pure-Go build: CGO_ENABLED=0 fails to compile
    |-- notebook/            orchestration: Pull, Commit (bounded CAS retry),
    |                        generic recovery, checkpoint compaction,
    |                        generation-fenced cleanup, metrics, failpoints,
    |                        sustained-load benchmarks
    |-- workspace/           managed visible directories: L/P layout,
    |                        os.Root-relative scans, strict state.json v1,
    |                        baseline authority, staged replacement, op locks
    |                        (semaphore + flock), recovery-required mode
    |-- storage/             semantic object-store boundary, strict manifest
    |                        v1, pack key grammar, ETag CAS, startup probe,
    |                        UploadUnique
    |   |-- contract/        one contract suite for every ObjectStore
    |   `-- fake/            deterministic in-memory ObjectStore
    |-- s3store/             the ONLY AWS SDK package: S3 adapter, prefix
    |                        join, multipart upload, semantic error mapping
    |-- strictjson/          neutral strict JSON value tree (manifest and
    |                        state.json)
    |-- tests3/              testcontainers S3 backend helper (currently
    |                        SeaweedFS) (one container per `go test`
    |                        invocation)
    |-- mcp/                 stdio MCP server: the two strict tool schemas,
    |                        strict decoding, the stable error envelope, and
    |                        the mcpReqID request logging
    `-- integrationtest/     test-only black-box MCP scenario suite: the
                             behavioral contract of the whole server
```

Interfaces belong to the package that consumes them. `internal/git` owns
the engine seam. `internal/workspace` owns its narrow `Engine` view.
`internal/notebook` owns its `Workspace` view. `internal/storage` owns the
`ObjectStore` boundary. No package exists only to forward a function.

### Event Flow

```text
Public API:   notes_pull(path) / notes_commit(path, message)
              CLI mirror: slivingdoc pull <path> / commit <path> -m <message>
                                         |
                                         |  MCP stdio JSON-RPC or one-shot argv
                                         v
Implemented orchestration:  Notebook.Pull / Notebook.Commit  (internal/notebook)

Pull(ctx)
  recovery required? --> entryRecovery
  snapshot L --> BuildTree
  readRemote: read current (ETag) --> DecodeManifest --> import missing packs
              (pack cache + ImportPack + ValidateHistory)
  Merge(base = L baseline, local = L, remote = R)
    clean    --> Materialize(R) + MarkPulled --> OK
    conflict --> Materialize(R + markers) + MarkPulled --> CONTENT_CONFLICT

Commit(ctx, message)
  validate message (UTF-8, <=16 KiB, non-blank); require the pulled marker
  snapshot L --> rejectMarkers --> BuildTree
  repeat up to retryLimit+1 attempts:
    readRemote (as above)
    Merge(base = L baseline, local = L, remote = R)
      conflict --> Materialize(R + markers) --> CONTENT_CONFLICT
      no change --> Accept(R) --> OK
      changed  --> buildProposal:
                    CreateCommit(parent = R head)
                    --> ExportIncrement / ExportCheckpoint
                    UploadUnique(pack)      pack bytes precede any manifest
                                            reference
                    publish: CAS current    CreateObject (gen 0) or
                                            ReplaceObject (by ETag)
                      lost      --> backoff --> retry attempt
                      ambiguous --> lookupPublication (read-back proof)
                      accepted  --> Accept(R) --> OK
      accepted tail >= checkpointPacks
        --> runCheckpoint (best-effort; never fails the commit)

Background efforts (same call path, best-effort)
  runCheckpoint: compact the stable accepted prefix --> CAS the new manifest
  cleanup: LIST --> delete only unreferenced objects at or before the cutoff
```

## Startup Wiring (`main.go`)

`main.go` is one call: `os.Exit(cli.Run(ctx, os.Args, git2.New(), opts))`.
`internal/cli` holds the command map (`serve|s`, `pull|p`, `commit|c`,
`version|v`) and routes through `go_away_boilerplate/pkg/cmd`. Each `cmd/`
package implements `cmd.Command`. The router parses the selected command's
flag set, then calls `Setup` and `Run`.

`cmd/pull` and `cmd/commit` are one-shot human invocations of the same two
notebook operations. They share the serve flag set (`app.Flags`) and take
one positional notebook path. A relative path resolves against the working
directory, and flags can follow the path. Both commands run the same
startup refusal surface through `app.Setup` and call `Runtime.Pull` /
`Runtime.Commit`. `app.Report` prints the candid result: exactly `OK` on
stdout, or the structured `mcp.ToolError.Report` text (category, retryable,
files with line ranges) with a nonzero exit. `commit` requires
`-m`/`--message`.

`cmd/serve` maps straight onto `internal/app`: `Setup` is `app.Setup`
(returning a `*app.Runtime`) and `Run` is `Runtime.Serve`. In order:

1. **Configure.** `Flags.resolve` applies flags, then the environment, then
   the defaults. An explicitly empty flag value does not fall back to the
   environment. It normalizes the endpoint without echoing credentials, makes
   both roots absolute, and refuses a private root at or below the workspace
   root.
2. **Early exit.** `version` and `serve -h` return zero from the router
   before `Setup` runs, so no native or network dependency is touched.
3. **Open the native engine.** `git2.New().Open()` verifies that the linked
   libgit2 is exactly the pinned v1.9.6 and refuses any other ABI.
4. **Build the object store.** The `StoreFactory` seam builds the
   `internal/s3store` adapter; tests substitute the deterministic fake.
5. **Probe the store.** `storage.Probe` proves the endpoint honors
   `If-None-Match`, `If-Match`, and read-after-write. A failure exits nonzero
   with a redacted `INCOMPATIBLE_STORE` diagnostic **before** any transport
   serves a request.
6. **Serve.** `mcp.NewServer` registers exactly `notes_pull` and
   `notes_commit` and runs over stdio. Stdout carries protocol messages only;
   logs go to stderr.
7. **Shut down.** A signal closes the live transport connection, which
   unwinds in-flight handlers. A bounded deadline (30 s) forces a nonzero
   exit for a handler that ignores cancellation. Client EOF is a clean exit.

### Key Flags

Flags beat environment variables, which beat defaults. The full table
lives in [`docs/running.md`](docs/running.md) and in the `helpText` of
`internal/app/config.go`, which `slivingdoc serve -h` prints — that code
copy is the authoritative one. Behavior worth remembering: `--bucket` is
required, `--private-root` must not be at or below the workspace root,
and `--commit-retries` exhaustion is `REMOTE_BUSY`.

The `serve`, `pull`, and `commit` commands share every flag. The
subcommand comes first. `slivingdoc version` and `-h` on any command exit
zero before loading dependencies.

### Logging

`internal/app.NewLogger` builds the process logger from the environment over
`go_away_boilerplate/pkg/slogcolor`, and `app.Module(logger, name)` binds the
module that selects its level. Bind a module once per component rather than
per call: slog consults `Enabled` before it builds a record, so the level can
only be resolved from a bound attribute.

| Variable | Effect |
| -------- | ------ |
| `LOG_LEVEL` | Per-module levels, for example `cli=warn,mcp=debug,info`. A bare level is the default. A malformed value falls back to info and is reported, never fatal. |
| `NO_COLOR` | Any non-empty value disables the ANSI level color. |

Modules are `app.ModuleCLI`, `ModuleApp`, `ModuleMCP`, and
`ModuleNotebook`.

---

## Integration Tests

Any new feature or behavior change starts by wrapping it in black-box
integration scenarios in `internal/integrationtest/`. The scenarios ARE
the feature's behavioral contract. Write them before or alongside the
implementation, never after.

- **Black-box means events in → events out.**
- **Scenarios carry the spec so prose does not have to.** The worklog
  phase can describe intent and touchpoints. The guaranteed behavior lives
  in the scenario assertions. Where prose and a passing scenario disagree,
  the scenario is the spec.
- **Unit tests cover what scenarios cannot reach cheaply** — pure
  functions, defensive branches, handler routing edge cases. Do not
  duplicate full event chains at both levels.
- **When a feature breaks existing integration tests, update the tests to
  match the feature.** Scenarios encode current intended behavior, not
  frozen history. A feature that legitimately changes retry semantics,
  event fields, or terminal states rewrites the affected assertions in the
  same commit. Before you rewrite an assertion, understand what it
  protected. If the breakage is a side effect rather than the feature's
  purpose, it is a regression finding, not a test to silence.

## Function Shape

Prefer many small single-purpose functions sequenced by a thin orchestrator over
one function that does several things. Two smells drive most refactors here:

- **`and` in a name is a split point.** `fooAndBar` is two functions
  wearing one name. Name each helper for the single verb it performs and
  let a caller sequence them. The orchestrator then reads as the outline of
  the operation.
- **A growing return tuple wants to be a struct — or wants to not exist at
  all.** When a function returns three or more values, or when you are
  tempted to add one more value to carry new data, that is a code smell.
  Normalize the signature, or remove the extra values.

The example below populates that struct incrementally and captures each
value at its source. A value set early (timing, telemetry, the raw
upstream result) survives a later step's failure. It is available on both
the success and error paths, so the caller reads one field regardless of
outcome — no per-branch plumbing.

```go
// Smell: one function, two jobs, a four-value return that only ever grows.
func (p *P) resolveAndStore(ctx context.Context, req Req) (*Info, Proof, Config, error) { /* ... */ }

// Preferred: a thin orchestrator over single-purpose steps, returning one struct.
type outcome struct {
    Info   *Info
    Proof  Proof
    Config Config
    Usage  *Telemetry // set the instant it is known; survives a later step failing
}

func (p *P) Resolve(ctx context.Context, req Req) (*outcome, error) {
    out := &outcome{}

    raw, usage, err := p.fetch(ctx, req)
    out.Usage = usage // captured up front — valid on every return below, success or error
    if err != nil {
        return out, err
    }
    cand, err := p.assess(raw) // pure: gates + shaping, no I/O
    if err != nil {
        return out, err
    }
    out.Proof, err = p.verify(ctx, cand)
    if err != nil {
        return out, err
    }
    out.Info, err = p.persist(ctx, cand)
    if err != nil {
        return out, err
    }
    out.Config = snapshot(raw)
    return out, nil
}
```

Returning a non-nil `out` alongside a non-nil error is deliberate here:
the failure path still carries what was gathered before it (the `Usage`
telemetry). Reserve that shape for structs whose job is to carry
diagnostics across the outcome boundary. Keep the usual "nil result on
error" everywhere else.

## Conventions

**Error wrapping.** `fmt.Errorf("package: what failed: %w", err)`. The
package name leads, and the operation follows. Wrap the cause with `%w`,
not `%v`, so callers can use `errors.Is`.

**Logging.** Every tool-call record carries `mcpReqID` and `tool`.
`mcpReqID` is a 16-hex correlation ID that the MCP handler generates per
call. Completion records add `duration` and `outcome`. `internal/notebook` takes the same request-scoped
logger from the context (`notebook.WithLogger` / `LoggerFrom`), so a
checkpoint or cleanup warning shares the request's ID. Logs go to stderr;
stdout is protocol-only.

**Invariants that a change must not break.** These come from the accepted
architecture, and a change that touches one of them updates
`docs/slivingdoc-v1.md` in the same commit:

- MCP and the one-shot `pull`/`commit` subcommands are the only public
  APIs, and both expose exactly the same two operations. The process never
  invokes Git and never imports `git2go`.
- All CGo and libgit2 types stay inside `internal/git2`. All AWS SDK use
  stays inside `internal/s3store`.
- `current` is the only accepted-state authority. State is never inferred
  from object names, and `LIST` is a cleanup tool, not a read path.
- Pack objects are immutable, and a pack's bytes exist before any manifest
  references it.
- Publication is conditional ETag replacement with no writer lock. A
  precondition failure is normal contention and drives a bounded
  merge-and-retry cycle.
- The notebook accepts valid UTF-8 text without U+0000 only: no symlinks
  and no special files.
- Commit rejects a complete conflict-marker block, so no accepted state can
  contain an unresolved conflict.
- Checkpoint and cleanup are best-effort background efforts. They must not
  change the result of the commit that scheduled them.
- A failure after local mutation began returns `RECOVERY_FAILURE`, attempts
  an authoritative resync, and never returns `OK` for that call.

**Error taxonomy.** The categories are stable API: `INVALID_REQUEST`,
`CONTENT_CONFLICT`, `REMOTE_BUSY`, `STORAGE_FAILURE`, `STORAGE_INTEGRITY`,
`RECOVERY_FAILURE`, `INCOMPATIBLE_STORE`. Error text can change. The
category, the retryable flag, and the structured conflict paths must not
change.
An unrecognized internal error maps to retryable `STORAGE_FAILURE` rather
than leaking. Caller-facing text must never contain a credential, an S3
key, a private path, a Git object ID, or Git vocabulary.

## Duplication policy

Duplication is not always a defect. `dupl -t 80` is a signal, not a verdict.
Use these principles to decide whether a reported clone needs fixing.

### Acceptable duplication (do not refactor)

- **Interface + fake mirroring.** A fake that mirrors an interface's method set is the proof that the contract is satisfiable. Examples: `internal/storage/fake` against the `ObjectStore` boundary, and the fake repositories in `internal/notebook` and `internal/workspace` against the `internal/git` engine seam. `dupl` reports the two fakes as clones; they belong to independent packages and must stay independent.
- **Thin wrappers over a shared helper.** Several small functions that differ only in a key kind or namespace and all delegate to one implementation are the abstraction, not a clone.
- **Cross-package contract assertions.** `assertProcessOK` in `internal/app` and `Harness.assertOK` in `internal/integrationtest` assert the same success envelope. The duplication is deliberate: merging them makes the black-box suite depend on a test helper of the package that it observes from outside.
- **Test-setup boilerplate.** The per-test harness preamble (temp roots, store, engine, cleanup) is structural, not logical, duplication.
- **Table-driven test loops.** `for _, tt := range tests { t.Run(tt.name, …` is the idiom, not a clone.

### Actionable duplication (fix these)

- **Two or more functions or tests whose bodies differ only in parameterised values** (URLs, error sentinels, input payloads, expected strings). Merge them into a table-driven test or extract a parameterised helper.
- **Production code where the same sequence of operations appears verbatim** with different call-site constants. Extract a function.
- **Identical setup + teardown across >3 tests in the same file.** Extract a test helper (`newTestXxx`) local to that file.

## QA validation

Run `make qa`. It runs `lint`, `test`, and `npm-test`. Before signing off on
ANY change, every gate below must pass. Tool versions are pinned to the
implementation baseline; do not substitute `@latest`.

There is **one** Go test command and **one** npm test command. No target,
build tag, environment variable, or flag runs a subset of the suite. The
pinned libgit2 and a running Docker daemon are prerequisites of `make test`,
not optional extras: an unreachable daemon fails the run with an actionable
diagnostic rather than skipping the storage protocol.

| Tool        | Command                                                       |
| ----------- | ------------------------------------------------------------- |
| Format      | `go run mvdan.cc/gofumpt@v0.11.0 -w -l .`                     |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...`      |
| Lint        | `go vet ./...`                                                |
| Fix         | `go fix -diff ./...` (must print nothing)                     |
| Test        | `make test` (race, 3 counts, 30 s, coverage with a 70 % floor) |
| npm         | `npm test --prefix npm/slivingdoc`                            |
| Dupl        | `go run github.com/mibk/dupl@v1.0.0 -t 80 .`                  |

Run every Go tool in the default CGo mode. There is no `CGO_ENABLED=0` gate:
`internal/git2` requires CGo, so a pure build fails to compile, and the CGo
build is a strict superset that sees every file.

The dupl check is a signal, not a verdict — see the Duplication policy
above for deciding which clones are acceptable and which need fixing.

**Important:** `go test ./... -race -count=3 -timeout=30s -coverpkg=./...` MUST pass unedited. The strictness
is intentional to produce a highly testable, efficient system which follows strict inversion of control.
Do not modify the timeout, count, or race. Do not add test skips, false-positive tests or any other cheat.
Instead, start testing early and ensure that test passes for each new modification.

A `testing.Short()` guard, a Docker-conditional skip, or a second "full gate"
command is the same cheat wearing a different hat: it advertises a fast path
that hides coverage. The only legitimate skip is a genuine platform
capability the host cannot provide, and it must name that capability.

**Important:** 70+% test coverage is a must. 90+% test coverage is preferred.
`make test` fails below the 70 % floor. `make cover` opens the profile from
the last run.

[`docs/testing.md`](docs/testing.md) records the test layers, the
concurrency and timeout rationale, and the coverage measurement details.
