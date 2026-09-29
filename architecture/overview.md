# System overview

slivingdoc gives many agents one shared directory of UTF-8 text notes that they read and edit with ordinary file tools. It stores the current notebook durably in S3-compatible object storage, or in a space of the slivingdoc hosted storage API when an API token is set, and resolves concurrent edits with Git's three-way merge, run in-process through a statically linked libgit2. It is not a source-control product: it uses Git data structures internally but never exposes a repository, a ref, a commit ID, or a Git executable. This doc answers "what are the moving parts, which package owns what, and which way do dependencies point".

Read this when: you are new to the repo, you need to find which package owns a behavior, or you are about to add an import between internal packages.

## Purpose and priorities

1. Resolve concurrent file changes quickly.
2. Store the current notebook durably in S3 (or the hosted storage API, which offers the same conditional-write semantics).

The public API is exactly two operations, `notes_pull` and `notes_commit`, served over MCP stdio and mirrored as the one-shot `slivingdoc pull [path]` and `slivingdoc commit [path] -m <msg>` subcommands. There is no public Go package, SDK, or HTTP API: all Go packages are internal, and the supported interfaces are the MCP tools, the `pull` and `commit` subcommands, process flags, release artifacts, and the npm launcher. See [product-contract.md](./product-contract.md).

## Scope

In v1: stdio MCP transport; direct `pull`/`commit` subcommands; one shared notebook per configured bucket+prefix; UTF-8 text files and directories; text merges with visible conflict markers; S3-compatible storage, or the hosted storage API ([hosted-mode.md](./hosted-mode.md)); optimistic concurrent publication; count-based automatic checkpoints; a self-contained native executable; an npm launcher.

Out of scope: a Git executable, `git-remote-s3`, `git2go`, a public Git remote, a writer lock or lease object in S3, symlinks/devices/sockets/pipes/hard-link semantics, branch/tag/ref/checkout/rollback APIs, permanent Git history, an application-level backup service.

## Terms

| Term | Meaning |
|------|---------|
| L (local state) | The caller-controlled visible directory. Agents and humans edit it; slivingdoc rewrites it during calls. |
| P (private state) | `<private-root>/<derived-key>/`: `repo/`, `state.json`, `operation.lock`, `pulled`, `staging/`, and, only on a host without a user cache directory, `pack-cache/` (otherwise pack bytes live in the shared per-identity directory under the user cache). Server-owned. |
| Accepted baseline | The remote state recorded in P (`workspace.Baseline`: generation, head, tree) that L's unpublished changes are relative to. |
| R (remote state) | The accepted state indexed by the S3 object `current`. Unreferenced S3 objects are not part of R. |
| Pack | An immutable Git pack file. |
| Increment | A pack holding the objects of one accepted publication; depends only on earlier indexed packs. |
| Checkpoint | A closed pack that reconstructs one complete notebook state without older packs. |
| Manifest | The strict JSON document stored at `current` (`storage.Manifest`). |
| Generation | The monotonic manifest counter; it advances on every accepted publication and every checkpoint replacement. |
| CAS | A conditional `current` write that succeeds only if the observed ETag still matches (`If-Match`), or the object is absent (`If-None-Match: *`). |

## Why Git and S3 together

| Component | Responsibility |
|-----------|----------------|
| libgit2 (`internal/git2`) | Build trees and commits, create and import packs, merge three trees. |
| S3 (`internal/s3store`), or the hosted storage API (`internal/httpstore`) | Store immutable bytes, hold the accepted manifest, enforce conditional publication. |
| slivingdoc (`internal/notebook`, `internal/workspace`, `internal/git`) | Product policy: paths, content rules, retries under contention, conflict presentation, checkpoints. |

This reuses mature Git merge behavior without operating a Git server, a mounted object-store filesystem, or a coordination database. Most work (scan, merge, pack build, upload, download) runs concurrently across writers; only the final CAS on the small `current` object orders publications.

## Key files

| File | Purpose |
|------|---------|
| `main.go` | `main()`: `cli.Run(ctx, os.Args, git2.New(), app.ProcessOptions{...})` |
| `cmd/serve`, `cmd/pull`, `cmd/commit`, `cmd/status`, `cmd/log`, `cmd/login`, `cmd/version` | The subcommands over `internal/app` (`cmd/login` holds `login`, `logout` and `space`) |
| `internal/app/service.go` | `Service`: maps a request path to one `workspace.Workspace` + `notebook.Notebook` pair (`notebookFor`) |
| `internal/notebook/notebook.go` | `Notebook`, `Config`, `New`, recovery helpers; `Pull` is in `pull.go`, `Commit` in `commit.go` |
| `internal/workspace/workspace.go` | `Workspace`, `Open`: L and P, the operation lock |
| `internal/git/engine.go` | `Engine` / `Repository`: the seam over libgit2 |
| `internal/git2/engine.go` | `New`, `PinnedVersion`: the only CGo implementation of the seam |
| `internal/storage/store.go` | `ObjectStore`: the semantic object-store boundary |

## Package map

One line per `internal/` package.

| Package | Owns |
|---------|------|
| `app` | Process body: flag/env resolution (`config.go`), storage selection (`storage.go`), login, logout and space (`login.go`, `space.go`), minted space tokens (`minted.go`), startup order (`app.go`), `Service` path-to-notebook map (`service.go`), CLI report rendering (`command.go`), logging, `DEBUG_PERF` profiling |
| `cli` | The command map (`serve|s`, `pull|p`, `commit|c`, `status`, `log`, `login`, `logout`, `space`, `version|v`), usage text, `Run` and its `router` (help, the one-line error, the home screen) |
| `tui` | The terminal presentation every command renders through: palette, status marks, columns, the progress spinner, the table picker; plain off a terminal. See [tui.md](./tui.md) |
| `credentials` | The stored account logins of `slivingdoc login` and their default spaces: the strict, versioned `credentials.json` under the user configuration directory, written 0600 by temp file and rename. See [login.md](./login.md) |
| `sitelogin` | Client of the site's CLI login routes (start, key polling, spaces, space-token minting, revoke) that validates every answer and never follows a redirect. See [login.md](./login.md) |
| `sitelogin/sitetest` | Test-only reference server of the site's CLI login routes with scripted approvals |
| `git` | Go-facing engine seam (`Engine`, `Repository`) and all Git policy: trees, snapshots, merge structuring, packs, history validation, path/content rules, read-only/writable `PathPolicy`, diffstat. See [git-engine.md](./git-engine.md) |
| `git/gittest` | Deterministic fake object hashing shared by fakes in higher packages (test-only) |
| `git2` | The only CGo package: pinned libgit2 v1.9.6 behind the `git` seam. See [git-engine.md](./git-engine.md) |
| `notebook` | Pull, Commit, bounded CAS retry, publication proof, generic recovery, checkpoints, cleanup, metrics, failpoints, domain error taxonomy. See [notebook.md](./notebook.md) |
| `workspace` | L/P layout, `os.Root` scans, strict `state.json`, baseline authority, staged in-place materialization, op lock, recovery-required mode. See [workspace.md](./workspace.md) |
| `storage` | `ObjectStore` boundary, semantic errors (including the store refusals and `Refusal`), pack metadata fields, strict manifest v1, pack key grammar, `UploadUnique`, startup `Probe`, UUIDv7, SHA-256 type |
| `storage/fake` | Deterministic in-memory `ObjectStore` with fault injection (tests) |
| `storage/contract` | One contract suite run against every `ObjectStore` (tests) |
| `s3store` | The only production AWS SDK package (test-only `tests3/s3.go` also uses the SDK to create its bucket): S3 adapter, prefix join, multipart upload, semantic error mapping |
| `httpstore` | The second production `ObjectStore`, selected by `SLIVINGDOC_TOKEN` or a stored login: the hosted storage API over HTTPS with a bearer token (fixed, or from a `TokenSource` of minted tokens) and one space, status-to-semantic error mapping, `CheckAccess` instead of the probe. See [hosted-mode.md](./hosted-mode.md) |
| `httpstore/gatewaytest` | Test-only reference server of the hosted API over `storage/fake` |
| `mcp` | stdio MCP server: two strict tool schemas, strict decoding, error/success envelopes, redaction, `mcpReqID` logging |
| `strictjson` | Strict JSON value tree shared by the manifest and `state.json` (rejects unknown, duplicate, missing, null) |
| `pathutil` | `ExpandHome` for `~/` request paths |
| `tests3` | S3 test backend (a SeaweedFS container, started through the Docker Engine API) for tests; `tests3/lease` owns the shared container |
| `integrationtest` | Test-only black-box scenario suite: the behavioral contract of the whole server |

## Dependency direction

```text
main.go ──> cli ──> cmd/* ──> app ──┬──> mcp ──────┬──> notebook ──┬──> workspace ──┬──> git <── git2 (CGo)
   │                                │              │               │                └──> strictjson
   │                                ├──> notebook  │               ├──> storage ────┬──> git
   │                                ├──> workspace │               │                └──> strictjson
   │                                ├──> storage <─┼── s3store     └──> git
   │                                ├──> s3store   ├──> workspace, git, strictjson, pathutil
   │                                ├──> httpstore     (s3store and httpstore each import only storage)
   │                                ├──> git, pathutil
   │                                └──> tui    (cli imports tui too; tui imports no internal package)
   └──> git2 (engine constructed once in main, passed down as git.Engine)
```

Rules the import graph follows (verified by `grep` over non-test imports):

- `git` imports no internal package. Everything above it speaks `git.OID`, `git.Snapshot`, `git.Repository`.
- `git2` imports only `git`. In production only `main.go` imports it; tests in `integrationtest`, `cmd/pull`, `cmd/commit`, `app`, `notebook`, and `workspace` import it for native runs. Every other package receives a `git.Engine`.
- `s3store` imports only `storage`, and only `app` (`realStoreFactory`, the default when `ProcessOptions.StoreFactory` is nil) and `integrationtest` import it.
- `httpstore` imports only `storage` (and the standard library); in production `app` (`realStoreFactory`, `validateHosted`, `PrepareLogin`), `credentials` and `sitelogin` (its validators and `Sanitize`) import it.
- `credentials` imports `httpstore` and `strictjson`; `sitelogin` imports `credentials` and `httpstore`; only `app` imports either in production, and `sitelogin/sitetest` only tests (`sitelogin`, `app`, `integrationtest`). `httpstore/gatewaytest` imports `storage` and `storage/fake` and is imported only by tests (`httpstore`, `app` and `notebook` in their `hosted_test.go`, `integrationtest`).
- `tui` imports no internal package (only `go_away_boilerplate/pkg/table`); in production `app` and `cli` import it.
- `notebook` imports `workspace`, `git`, `storage`; it never imports `mcp` or `app`.
- `workspace` imports `git` and `strictjson`; it never reads remote state.
- `storage` imports `git` (for `git.OID` in the manifest) and `strictjson`.
- Interfaces belong to their consumer: `notebook.Workspace`, `workspace.Engine`, `storage.ObjectStore`, `git.Repository`. No package exists only to forward a function.

## Flow

```text
main.go:main
  → cli.Run → app.Setup (config, git2 Open + version check, s3store + storage.Probe,
                          or httpstore + CheckAccess when SLIVINGDOC_TOKEN or a stored
                          login selects hosted mode, see login.md)
  → Runtime.Serve → mcp.NewServer (notes_pull, notes_commit)      [serve]
    or Runtime.Pull / Runtime.Commit → app.Report                  [pull, commit]
      → Service.Pull/Commit(path) → Service.notebookFor(path)
          → workspace.Open + notebook.New   (first use of a path)
      → Notebook.Pull / Notebook.Commit     (see pull.md, commit.md)
```

## Behavior

- Invariants (from AGENTS.md "Conventions"; a change that breaks one must update these docs): the process never runs Git or imports `git2go`; all CGo stays in `git2`, all production AWS SDK use in `s3store`; `current` is the only accepted-state authority and `LIST` is a cleanup tool, never a read path; packs are immutable and uploaded before any manifest references them; publication is ETag CAS with no writer lock; content is UTF-8 without U+0000, no symlinks or special files; commit rejects complete marker blocks; checkpoint and cleanup never change a commit result; a failure after local mutation returns `RECOVERY_FAILURE`, never `OK`.
- Stores are `ObjectStore` adapters (`s3store`, and `httpstore` for hosted mode). The notebook stays store-neutral except where it reacts to the semantic store refusals: `storeRefusal` reasons and the compaction of a full space on `storage.ErrQuotaExceeded` ([hosted-mode.md](./hosted-mode.md)).
- Caller-facing data never contains a credential, S3 key, private path, Git object ID, or Git vocabulary: notebook messages are fixed strings (except a hosted store refusal, which appends the server's sanitized message after `The storage says:`), `mcp.Redact` is a backstop that masks `sld_` API tokens, pack and probe keys, 40- and 64-hex IDs, access keys, and URL userinfo, and `mcp.safeEngineDetail` is an allowlist for `detail`.
- One `Service` holds one `Notebook` per cleaned request path for the life of the process (`Service.opened`). See [workspace.md](./workspace.md) for the P directory each maps to.

## Gotchas

- There is no pure-Go build. `CGO_ENABLED=0` fails to compile because of `git2`; run every tool in the default CGo mode.
- `Service.opened` is keyed by `filepath.Clean` of the request path (after `~/` expansion), so `/ws/a` and `/ws/a/` share one `Workspace` (`TestServiceSharesOneNotebookPerDirectory`).

## Related

- [product-contract.md](./product-contract.md): the two tools, result and error envelopes, read-only and writable paths
- [guarantees.md](./guarantees.md): failure guarantees, recovery, concurrency and scaling
- [notebook.md](./notebook.md), [pull.md](./pull.md), [commit.md](./commit.md), [conflicts.md](./conflicts.md), [checkpoints.md](./checkpoints.md)
- [git-engine.md](./git-engine.md), [workspace.md](./workspace.md)
- [running.md](./running.md): commands, configuration, and operator guidance
- `../AGENTS.md`: [Architecture](../AGENTS.md#architecture) (box diagram and runtime layout), [Package Map](../AGENTS.md#package-map), [Event Flow](../AGENTS.md#event-flow), [Startup Wiring](../AGENTS.md#startup-wiring-maingo), [Conventions](../AGENTS.md#conventions)
- Other concerns: [storage.md](./storage.md), [s3store.md](./s3store.md), [hosted-mode.md](./hosted-mode.md), [mcp-server.md](./mcp-server.md), [cli.md](./cli.md), [config.md](./config.md), [errors.md](./errors.md), [logging.md](./logging.md), [testing.md](./testing.md), [build.md](./build.md)
