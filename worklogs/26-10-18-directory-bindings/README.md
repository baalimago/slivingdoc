# slivingdoc directory bindings

## Goal

One target serves every command of one notebook directory. `serve` binds exactly one notebook directory, resolves one store before the transport starts, and `notes_pull` and `notes_commit` take no path. `pull`, `commit`, `status`, and `log` resolve a target through the same layers: a flag, an environment variable, the directory record, then the global default. A successful `pull` or `commit` on a directory that remembered nothing records the target it proved, unless the name came from the global default or a token, so the next bare command and a durable server reach the same remote.

`space` selects one global default. The last selection wins: an S3 selection stores one complete S3 target, and a hosted selection clears it. A bare command uses that default with no repeated profile, bucket, or endpoint.

Hosted directory records already ship. This worklog adds the S3 record, consumes the global default from `space`, and makes `serve` a directory-bound command. It amends the S3 default behavior shipped by `26-10-18-space-admin-surface`: one global S3 default replaces the one-per-store-key default.

What must stay true: `current` remains the only accepted-state authority; a pack's bytes exist before any manifest references it; publication is a conditional ETag replacement; a failure after local mutation began returns `RECOVERY_FAILURE`; no Git vocabulary, key, token, private path, or secret reaches caller-facing text; CGo stays inside `internal/git2`; the AWS SDK stays inside `internal/s3store`; no credential is stored in the settings file.

This change is primarily aimed at doing two things:

1. Simplify usage with cli so that directories can 'remember' their associated slivingdoc bucket/space
2. Simplify API for mcp server so that agents doesnt need to concern themselves with what 'path' does

## Glossary

| Term                   | Meaning                                                                                                                                                       |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **L**                  | The visible notebook directory callers edit: `--workspace-root`, the working directory, or a per-process scratch directory when neither root is configured.   |
| **P**                  | The server-owned private state root, `--private-root`.                                                                                                        |
| **R**                  | The accepted remote state, indexed by the stored object `current`.                                                                                            |
| **Target**             | One named notebook on one backend: `{kind, endpoint, region, profile, name, prefix, pathStyle}`. Shipped as `settings.Target`.                                |
| **Kind**               | `hosted` (the slivingdoc hosted storage API) or `s3` (an S3-compatible store).                                                                                |
| **name**               | The hosted space name or the S3 bucket name.                                                                                                                  |
| **profile**            | An AWS shared-config profile name. Configuration, never a secret, and never part of the notebook identity. Empty means the ambient AWS credential chain.      |
| **Record**             | The target one absolute directory path remembers in `workspaces.json`, also called a directory binding. Keyed by path alone.                                  |
| **Global default**     | The target a bare command uses when no flag, variable, or record names one. It is the S3 default when one is stored, else the selected login's default space. |
| **S3 default**         | One stored S3 target carrying endpoint, region, profile, name, prefix, and path style. The last S3 `space` selection replaces it.                             |
| **Hosted default**     | The selected login's default space in `credentials.json`. A hosted `space` selection clears the S3 default.                                                   |
| **Effective endpoint** | The endpoint after the flag, the environment, the record, the default, and the AWS configuration resolve. Empty means the provider's default.                 |
| **Effective region**   | The region after the flag, the environment, the record, the default, and the AWS configuration resolve.                                                       |
| **Notebook identity**  | `workspace.Identity`: effective endpoint, region, name, prefix, manifest version, and hosted space id. It keys P and the shared pack cache.                   |
| **Probe proof**        | The 24-hour cached proof of conditional-write compatibility, `probe-ok.json` in the identity's pack-cache directory.                                          |
| **Scratch directory**  | The per-process notebook directory `serve` creates when neither `--workspace-root` nor `--private-root` is configured. It is removed when the process exits.  |
| **Durable directory**  | A notebook directory that survives the process: `--workspace-root`, `SLIVINGDOC_WORKSPACE_ROOT`, or the working directory when any root is configured.        |

## Flows

### Serve startup: one directory, one store

```text
slivingdoc serve [--workspace-root <dir>] [--private-root <dir>] [<target flags>]
  |
  v
roots                                                     (phase 1)
  neither root configured -> scratch directory
  otherwise               -> --workspace-root, its variable, or the working directory
  |
  v
target resolution                                         (phase 3)
  the record of <dir> when the directory is durable
  flag > slivingdoc variable > record > global default > ambient AWS
  |
  v
one store, built and checked: storage.Probe, or the hosted access check
  |
  v
MCP over stdio                                            (phase 1)
  notes_pull {}             -> that one store, <dir>
  notes_commit {message}    -> that one store, <dir>
```

### Pull and commit: resolve, operate, record

```text
slivingdoc pull [dir]      (an omitted dir is the workspace root)
  |
  v
target resolution                                         (phase 3)
  the same layers as serve startup
  |
  v
one store and notebook; pull or commit
  |
  +-- success, the directory remembered nothing, and a
  |   flag or variable named the target
  |     |
  |     v
  |   the record is written                               (phase 3)
  |
  +-- failure
        nothing is written
```

`status` and `log` read the same record and never write it. A durable `serve`
reads the record of its directory and writes one after a successful pull or
commit, exactly as the CLI does. A scratch `serve` reads and writes none.

### Space: select the global default

```text
slivingdoc space <space>                                  (phase 2)
  |
  +-- set the selected login's default space
  +-- clear every S3 default
  |
  v
hosted default is active

slivingdoc space --bucket s3 ... (if current config allows access) (phase 2)
  |
  +-- write one S3 default target with its profile,
  |   endpoint, region, prefix, and path style
  +-- replace every previous S3 default
  |
  v
S3 default is active

a bare pull, commit, status, log, or serve                 (phase 3)
  |
  v
flag > slivingdoc variable > record > active global default
```

## Status board

**Line:** done

| #   | Node                              | Parent | Class       | Interface                                                                                  | Status      |
| --- | --------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------ | ----------- |
| 1   | One notebook directory per server | root   | contract    | Tool schema `notes_pull` and `notes_commit`, and the one-directory `serve` binding         | Complete    |
| 2   | Global default                    | root   | contract    | `space` (CLI): one active default, last selection wins, S3 default carries its full target | Complete    |
| 3   | Backend-aware bindings            | root   | composition | `bind` (CLI): `serve`, `pull`, `commit`, `status`, `log` — one target per directory        | Complete    |

Phases run in numeric order. Phase 1 needs no shipped member. Phase 2 consumes
the shipped `space` command and the settings, credentials, and S3 members
below. Phase 3 consumes Phase 1's tools, Phase 2's active default, and the same
shipped members.

## Scope

This worklog builds none of the admin surface, the settings schema shape, the
creation grammar, or the cloud repository. `space-admin-surface` owns them.

- No new command. `pull`, `commit`, and `space` are the surfaces.
- No per-call backend switch in the MCP server. One server, one directory, one
  store, resolved before the transport starts.
- No multi-notebook MCP server. Two notebooks mean two servers, one per
  directory, as a language server runs per project.
- One amendment of a completed worklog. Phase 2 replaces the store-key S3
  default and its version-2 `defaults` rule owned by
  `26-10-18-space-admin-surface`, whose phases 1 and 6 are Complete. The
  maintainer's sign-off of this README covers that amendment; no other
  interface of that worklog changes.
- No change to the hosted space creation grammar or the S3 create surface.
  Phase 2 defines the default selection and the default mark.
- No change to the accepted-state protocol, merge, checkpoints, cleanup, or the
  error taxonomy's categories.
- No S3 credential storage, no key prompt, and no secret display.
- All tests need to remain 100% functional without internet access.

## Root facts

### Invariants

These bind members of more than one phase. `I1` is declared in phase 1's
Local facts and `I3`–`I9` in phase 3's, each at the node whose clauses bind
it.

| ID  | Invariant                                                                                                                                              | Members bound                                           |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------- |
| I2  | No credential, key, token, private path, or Git vocabulary reaches the record, a log line, or caller-facing text.                                      | `notes_pull`, `notes_commit`, `pull`, `commit`, `space` |
| I5  | One global default exists. The last `space` selection wins, and an S3 default carries its own profile, endpoint, region, name, prefix, and path style. | `space`, `pull`, `commit`, `serve`                      |

### Parameters

None.

## Validation policy

### Gates

```sh
make qa
```

`make qa` runs the formatter, `go vet`, staticcheck, `go fix -diff`, the race
suite at `-count=3` with the coverage floor, and the npm tests. Dupl is a
separate review signal, not part of `make qa`:

```sh
go run github.com/mibk/dupl@v1.0.0 -t 80 .
```

The integration scenarios in `internal/integrationtest` are the behavioral
contract; no phase adds a skip, a short mode, or a second gate. The
S3 rows use the pinned container through `internal/tests3`, and no row reaches
a live endpoint. `go vet ./...` type-checks every assembled Go interface
block; a tool schema has no type checker, so its clauses are checked by
inspection and by calls through the schema.

### Human checks

Phase 3's re-planned `pull.2` interface was signed off by the maintainer on
2026-10-21 (`interfaces 3 signed`). A02 is closed.

## Findings

| ID    | Severity | Node | Owner | Status      | Summary                                                                                                                                                      |
| ----- | -------- | ---- | ----- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| V1-01 | major    | 2    | plan  | Closed +wip | check 3 · phase 2 amends the completed `space-admin-surface` interface without an import · cite the amended owner and the version-2 default rule it replaces |
| V1-02 | major    | 3    | plan  | Closed +wip | check 4 · phase 3 `pull.9` requires a name from a record the directory does not have · drop "or the record"                                                  |
| V1-03 | major    | 3    | plan  | Closed +wip | check 9 · phase 3 `pull.3` lets a record of another kind supply every field · state that only a record of the resolved kind contributes                      |
| V5-01 | major    | root | plan  | Closed +wip | check 4 · root facts invariants table omits invariants I1, I3, I4, I6, I7, I8, I9 that phase clauses cite · add them or stop citing them                     |
| R1-01 | minor    | root | fix   | Closed +wip | Validation policy said `make qa` runs dupl, but the Makefile runs it separately · list the signal command separately                                  |
| R1-02 | minor    | root | fix   | Closed +wip | `architecture/login.md` and `architecture/security.md` gave the wrong backend precedence · align them with `resolveStorage`                           |
| R2-01 | minor | root | fix | Closed +wip | `architecture/running.md:791` said a token with an endpoint/S3 endpoint setting is refused, contrary to `pull.2` and `selectKind` · [x] state the actual token and explicit-S3 precedence · a token plus `AWS_ENDPOINT_URL_S3` succeeds through hosted storage (`TestScenarioStorageSelection`) |
| R2-02 | minor | root | fix | Closed +wip | `architecture/cli.md:20` said successful pull/commit records only a hosted notebook, contrary to `pull.9` · [x] describe the complete backend-neutral target · a first S3 pull must record its target (`TestScenarioRecordFirstTarget`) |
| A01   | quality andon | root | maintainer | Closed +wip | integration package exceeded the strict timeout under external host contention · `make qa` passed in round 2; see phase 3 for the 28.802 s / 30 s margin note |
| A02   | design andon  | 3    | maintainer | Closed +wip | `pull.2` now matches `selectKind` and `architecture/login.md`, with black-box coverage; the maintainer signed off interfaces 3 on 2026-10-21 |

## Session journal

- 2026-10-18 · plan · README drafted; split from the single space-backend worklog
- 2026-10-20 · plan · README amended: whole file rebased onto worklog system v2; tree reduced to phases 1–2 and the quality-gate phase dropped; old phase files deleted
- 2026-10-20 · plan · phases 1–2 written
- 2026-10-20 · plan · README amended: Status board, Flows, Root facts, Imports
- 2026-10-20 · plan · README amended: scope rescoped to the S3 record, the one global default, and the durable/scratch serve split; phases 1–3
- 2026-10-20 · plan · README amended: invariant and parameter placement, imports, and the default scope wording
- 2026-10-21 · validate · V1 Not ready · inspection: checks 1–10; `rg` member existence in `internal/settings`, `internal/credentials`, `internal/s3store`, `internal/mcp`, `internal/workspace` · notes: 5
- 2026-10-21 · plan · README amended: amendment ownership, Imports, glossary, invariant binding, Findings
- 2026-10-21 · plan · phase 1 re-planned: documentation
- 2026-10-21 · plan · phase 2 re-planned: active default, credentialed default-space members, documentation
- 2026-10-21 · plan · phase 3 re-planned: record kind, clause 9, identity recovery, invariant binding, documentation
- 2026-10-21 · validate · V2 Ready · inspection: checks 1–10 · notes: 1
- 2026-10-21 · plan · phase 1 re-planned: the maintainer decided the tool API is not yet declared stable, so `path` is removed without a deprecation release
- 2026-10-21 · validate · V3 Ready · inspection: phase 1, checks 1–10 · notes: 0
- 2026-10-21 · plan · phases 1–3 re-planned: acceptance criteria, test cases, and error management tables; phase 3 `pull.8` now states that the profile reaches neither the notebook identity nor the probe proof
- 2026-10-21 · validate · V4 Ready · inspection: checks 1–10 over phases 1–3; every clause, criterion, and cited invariant reached by a test row; every error row names a test that appears in its Test cases table · notes: 0
- 2026-10-21 · validate · V5 Not ready · inspection: checks 1–10 over phases 1–3 · notes: 0
- 2026-10-21 · validate · V6 Not ready · inspection: checks 1–10 over phases 1–3 · notes: 0
- 2026-10-21 · plan · README amended: Root facts names where I1 and I3–I9 are declared, and V5-01 is closed: every cited invariant exists in the Local facts of the phase whose clauses bind it, so no check failed
- 2026-10-21 · plan · phases 2–3 re-planned: phase 2 declares the settings members its rebuild uses; phase 3 gains `pull.13` and `serve.6` for a record write that fails after a successful publish
- 2026-10-21 · validate · V7 Ready · go test ./...: ok · notes: 0
- 2026-10-21 · worker · phase 1 code and tests complete (mcp, app, all integration scenarios green; `go vet ./...` clean); documentation partially updated (product-contract, mcp-server, overview, decisions 55, compatibility); remaining: testing/security/running/errors/AGENTS docs, `make qa`, README completion. Handover in the phase file.
- 2026-10-21 · worker · phase 1 complete: the remaining documentation (testing, security, running, errors, overview, workspace, product-contract, examples/seaweedfs, AGENTS), the `closeClient` dedup, and the null-message row; `make qa` green at 89.5 % coverage. Next: phase 2.
- 2026-10-21 · worker · phase 2 partial (session 3): the five new integration scenarios and the hosted/S3 scenario extensions are green; `TestScenarioSpaceS3` asserts the complete target; `space` no longer lets a stored default override `SLIVINGDOC_ENDPOINT`; `architecture/login.md` updated. Remaining: config, cli, running, overview and testing docs, `make qa`, and the status board. Handover in the phase file.
- 2026-10-21 · worker · phase 2 complete: config, cli, running, overview, testing and the `AGENTS.md` settings row updated; staticcheck's unused `stderr` in `TestScenarioSpaceSettingsFailures` fixed; `make qa` green at 89.5 % coverage and 35 npm tests. Next: phase 3.
- 2026-10-21 · worker · phase 3 partial (session 4): production path compiles and `internal/app` tests pass; `settingsView`, record write for both backends, durable/scratch serve association, and effective values from `Store.Resolved` implemented. Remaining: integration scenarios, documentation and `make qa`; handover in the phase file.
- 2026-10-21 · worker · phase 3 scenarios complete (session 5): `TestScenarioServeReadsTheRecord`, the scratch-record half of `TestScenarioEphemeralNotebook`, `TestScenarioCLIOmittedDirectory`, `TestScenarioRecordFirstTarget`, `TestScenarioBareCommandUsesTheS3Default`, `TestScenarioRecordWriteFailures` and `TestScenarioResolvedEffectiveValues`; helper isolation moved to a private configuration directory. Remaining: documentation and `make qa`.
- 2026-10-21 · worker · phase 3 complete (session 6): the remaining architecture docs, `AGENTS.md`, and decision 57; the three S3 binding scenarios now write below a fresh prefix of the shared bucket after a fresh bucket refused the first conditional write under `-count=3`; the diagnostic probe test was removed; `make qa` green at 89.6 % coverage and 35 npm tests. Next: none, the worklog is complete.
- 2026-10-21 · review · root round 1 not ready · `make qa` (before and after fixes): format, vet, staticcheck, and go fix passed; race suite timed out at 30 s (70.5% observed) · `make test`: race suite timed out at 30 s (70.4% observed) · `go test -race -count=3 -timeout=30s ./internal/integrationtest`: timed out at 30 s · `npm test --prefix npm/slivingdoc`: 35 passed · `go run github.com/mibk/dupl@v1.0.0 -t 80 .`: 3 acceptable clone groups · `git diff --check`: clean · findings R1-01/R1-02 closed +wip; andons A01/A02 open.
- 2026-10-21 · plan · phase 3 re-planned: `pull.2` backend selection now states `--storage s3` > token > `--storage hosted` > bucket spelling with an S3 signal > record > S3 default > usable login > S3; existing `--space` ambiguity kept.
- 2026-10-21 · validate · V8 Ready · inspection: checks 1–10 on the phase-3 `pull.2` re-plan; `make qa` and the targeted selection/expired/ambiguous-login scenarios passed; notes: 0. Fresh interface sign-off remains required.
- 2026-10-21 · review · root round 2 not ready · `make qa`: exit 0, 89.6% coverage, integration package 35.379 s wall duration, npm 35/35 · `go test -race -count=3 -timeout=30s ./internal/integrationtest`: exit 0, 34.419 s · `go run github.com/mibk/dupl@v1.0.0 -t 80 .`: 3 acceptable clone groups · `git diff --check`: clean · A01 closed +wip; R2-01/R2-02 fixed +wip; A02 remains open pending fresh interface sign-off, so the line waits on human: root.
- 2026-10-21 · maintainer · interfaces 3 signed
- 2026-10-21 · fix · A02 closed +wip · phase 3 `pull.2` re-planned and signed; no runtime change · `make qa`: exit 0, 89.6 % coverage, npm 35/35
- 2026-10-21 · review · done · `make qa`: exit 0, 89.6 % coverage, npm 35/35 · `go run github.com/mibk/dupl@v1.0.0 -t 80 .`: 3 accepted clone groups · `git diff --check`: clean
