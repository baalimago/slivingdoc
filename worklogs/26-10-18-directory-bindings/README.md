# slivingdoc directory bindings worklog

**Status:** README signed off. Every phase is Not Started.

**Architecture:**
[`../../architecture/mcp-server.md`](../../architecture/mcp-server.md),
[`../../architecture/product-contract.md`](../../architecture/product-contract.md),
[`../../architecture/overview.md`](../../architecture/overview.md),
[`../../architecture/config.md`](../../architecture/config.md),
[`../../architecture/running.md`](../../architecture/running.md),
[`../../architecture/workspace.md`](../../architecture/workspace.md),
[`../../architecture/storage.md`](../../architecture/storage.md),
[`../../architecture/s3store.md`](../../architecture/s3store.md),
[`../../architecture/hosted-mode.md`](../../architecture/hosted-mode.md),
[`../../architecture/cli.md`](../../architecture/cli.md),
[`../../architecture/errors.md`](../../architecture/errors.md),
[`../../architecture/testing.md`](../../architecture/testing.md)

**Provenance:** split at design sign-off from the single worklog
`26-10-18-space-backend-parity`. The sibling
[`26-10-18-space-admin-surface`](../26-10-18-space-admin-surface/README.md)
owns creating, listing, and choosing a target, and it ships first. This
worklog owns how every command reaches the chosen target: one notebook
directory per server, the resolution layers, the directory record, the
notebook identity, and the compatibility proof.

**Prerequisite:** this worklog consumes shipped code from the sibling and
changes none of its types: `settings.Target`, `settings.StoreKey`,
`settings.Set.Default`, `settings.defaults`, `s3store.Config.Profile`,
`s3store.Resolve`, `s3store.Store.Resolved`, and the `--profile` flag. The
settings file version 2 and the `space` command exist before Phase 2 starts.
Phase 1 needs none of them and may run first.

## Goal

Make one target serve every command of one directory. `serve` binds to
exactly one notebook directory, reads that directory's record, and resolves
one store at startup, so `notes_pull` and `notes_commit` take no path and
cannot switch backend. `pull`, `commit`, `status`, and `log` resolve a target
through the same layers: a flag, a slivingdoc environment variable, the
directory record, then the backend default, with the ambient AWS configuration
below the record because other tools share it. A successful `pull` or `commit`
on a directory that remembered nothing records the target it used, so the next
bare invocation and the server reach the same remote. A profile selector is
part of the notebook identity and of the compatibility proof, so two credentials
into one endpoint never share private state.

What must stay true: `current` remains the only accepted-state authority; a
pack's bytes exist before any manifest references it; publication is a
conditional ETag replacement; a failure after local mutation began returns
`RECOVERY_FAILURE`; no Git vocabulary, key, token, private path, or secret
reaches caller-facing text; CGo stays inside `internal/git2`; the AWS SDK
stays inside `internal/s3store`; no credential is stored in the settings file.

## Glossary

| Term                   | Meaning                                                                                                                                              |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| **L**                  | The visible notebook directory that callers edit, `--workspace-root`.                                                                                |
| **P**                  | The server-owned private state root, `--private-root`: `state.json`, `operation.lock`, `repo/`, `staging/`, `pulled/`.                               |
| **R**                  | The accepted remote state, indexed by the stored object `current`.                                                                                   |
| **Target**             | A named notebook on a backend: `{kind, endpoint, region, profile, name, prefix, pathStyle}`, shipped by the sibling as `settings.Target`.            |
| **Kind**               | `hosted` (the slivingdoc hosted storage API) or `s3` (an S3-compatible store).                                                                       |
| **name**               | The hosted space name or the S3 bucket name.                                                                                                         |
| **profile**            | An AWS shared-config profile name. A selector, never a secret. Empty means the ambient AWS credential chain.                                         |
| **Record**             | A target stored for one absolute directory path in `workspaces.json`, also called a directory binding. Keyed by path alone.                          |
| **Default**            | A target stored once per store key, used when no flag, environment, or record names a target. A hosted default is the login's default space.         |
| **Store key**          | `{kind, endpoint, region, profile}`: the identity of a store before its notebook name is known.                                                      |
| **Effective endpoint** | The normalized endpoint after flags, environment, profile, and AWS configuration resolve. Empty means the provider's default.                        |
| **Effective region**   | The region after flags, environment, profile, and AWS configuration resolve.                                                                         |
| **Notebook identity**  | `workspace.Identity`: effective endpoint, region, name, prefix, manifest version, hosted space id, and profile. It keys P and the shared pack cache. |
| **Probe proof**        | The 24-hour cached proof of conditional-write compatibility, `probe-ok.json` in the store identity's pack-cache directory.                           |
| **Session directory**  | The per-process temporary notebook directory that `serve` uses when the operator sets no `--workspace-root`.                                         |

## Flows

### Serve startup: one directory, one store

```text
slivingdoc serve --workspace-root ~/notes [--storage ...] [--profile ...]
  |
  v
Flags.resolve: flags > environment > defaults
  |
  v
resolveRoots: L = --workspace-root (default: per-process temporary dir)
              P = --private-root  (default: <user-cache-dir>/slivingdoc)
  |
  v
ProcessOptions.WithReader(L): read the record for L, if any
  |
  v
resolveStorage(flags, env, record)
  kind, effective endpoint, region, profile, name, prefix, pathStyle
  |
  v
build the store (s3store.New or httpstore.New)
  |
  v
storage.Probe, or the hosted CheckAccess
  a fresh proof of this identity stands in for the S3 probe
  |
  v
mcp.NewServer over stdio
  notes_pull()            -> the one resolved store, L
  notes_commit(message)   -> the one resolved store, L
```

A `path` member in either tool's arguments is refused. The server never
switches backend per call, because it resolved one store before the transport
started.

### `pull` and `commit`: resolve, operate, record

```text
slivingdoc pull [path]      (an omitted path is L)
  |
  v
record = settings.Lookup(path)
  |
  v
resolveStorage(flags, env, record)
  flags and the environment first, then the record, then the store default
  |
  v
store + notebook; pull or commit
  |
  v
success:
  if the directory remembered nothing, write the resolved target
  a default-sourced target is not written, exactly like a login's default space
failure:
  nothing is written
```

`status` and `log` read the same record and never write. `serve` reads the
record for L and never writes, so one record governs every command that
addresses that directory.

## API additions

### MCP: one directory, no path argument

| Tool           | Before             | After       |
| -------------- | ------------------ | ----------- |
| `notes_pull`   | `{path?}`          | `{}`        |
| `notes_commit` | `{message, path?}` | `{message}` |

The notebook directory is `--workspace-root`, or the per-process temporary
directory when the operator sets none. A `path` member is refused with a
self-contained message that names `--workspace-root`; it is never ignored,
because a client that thinks it addressed another directory must not be told
otherwise. The result continues to report the notebook directory.

No other route, command, flag, or tool is added. `--profile` and the `space`
flags ship with the sibling.

## Data model additions

### The record stores effective values

The `workspaces.json` version-2 schema, its encoder, its decoder, its bound,
and its store key are the sibling's
([schema](../26-10-18-space-admin-surface/README.md#workspacesjson-version-2)).
This worklog decides which values a command writes, and when:

- A successful `pull` or `commit` writes the target it actually used, with the
  effective endpoint, region, profile, name, prefix, and path style.
- A default-sourced target is never recorded.
- A failed operation records nothing.
- A record-sourced target is not rewritten; an explicit flag or environment
  value beside an existing record leaves the record byte for byte.
- A record is written only when the directory remembered nothing. No command
  removes or rewrites an existing record; an operator who wants another target
  edits the file, and the next successful operation records what it used.
- `serve`, `status`, and `log` never write.

### `probe-ok.json` version 2

| Field | Change |
| --- | --- |
| `version` | 2 |
| `profile` | new, required; compared with the store's profile |
| every other field | unchanged: `slivingdoc`, `probedAt`, `endpoint`, `region`, `bucket`, `prefix`, `pathStyle` |

A version-1 record reads as "no recorded proof" and costs one probe. The proof
directory is already the identity's, so the profile now separates proofs of one
endpoint reached with different credentials. The 24-hour lifetime is unchanged.

### Notebook identity gains `Profile`

`workspace.Identity` gains `Profile`, appended to the digest after `SpaceID`
only when non-empty, exactly like `SpaceID`. Every existing S3 workspace keeps
its private-directory key and shared cache directory; a directory bound to a
profile is a different remote from the same names under another profile, and
the identity must say so. `Service.identity` and the integration harness's
`Identity` set it.

## Status board

| Phase                                                                       | Status      | Outcome                                                                                                                    |
| --------------------------------------------------------------------------- | ----------- | -------------------------------------------------------------------------------------------------------------------------- |
| [1. One notebook directory per server](phase-1-one-directory-per-server.md) | Not Started | The MCP tools lose `path`, `serve` reads its directory's record, and a lingering `path` is refused                         |
| [2. Backend-aware bindings](phase-2-backend-aware-bindings.md)              | Not Started | S3 records and defaults in resolution and recording, the profile identity and proof, and `serve` resolving S3 through them |
| [3. Quality gate](phase-3-quality-gate.md)                                  | Not Started | The public repository green, every architecture doc true, and the whole product claim proven                               |

Phases complete in numeric order. Phase 1 is independent of the sibling and may
run first. Phase 2 needs Phase 1 and the sibling's shipped Phases 1, 5, and 6.
Phase 3 depends on both.

## Scope

**In scope.** One notebook directory per server; the tool schemas; the
resolution layers over flags, environment, record, and default; the completed
store key before a default is read; recording after success; the profile in
the notebook identity and the probe proof; the operator surface and the
architecture docs for all of it.

**Out of scope. No phase does these:**

- No new command, no new flag, and no change to the `space` command, its
  backends, the settings file format, or the creation grammar. The sibling owns
  them.
- No per-call backend switch in the MCP server. One server, one directory, one
  store, resolved at startup.
- No multi-notebook MCP server. Two notebooks mean two servers, one per
  directory, the way a language server runs per project.
- No command removes a directory record. A rebind is an operator edit of
  `workspaces.json`, then a successful operation.
- No S3 credential storage, no key prompt, and no secret display.
- No change to the accepted-state protocol, merge, checkpoints, cleanup, or
  the error taxonomy's categories.
- No cloud code and no cloud gate; the cloud repository belongs to the
  sibling.
- No claim that R2 works before a probe passes, and no live cloud or AWS
  endpoint in any test.

## Store resolution

Every command resolves one target through the same layers: a flag, then a
slivingdoc environment variable, then the directory record, then the default.
The ambient AWS configuration (profile, region, endpoint URLs) sits below the
record, because it is shared with other tools; it fills only what neither a
flag nor the record names. The record is never rewritten by a resolution.

| Field       | Order                                                                                                                                                                                                                                      |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `kind`      | `--storage` / `SLIVINGDOC_STORAGE`, then the record's kind, then the `auto` rules: a token selects hosted, a stored login with no S3 signal selects hosted, every other case selects S3, and the existing ambiguity refusals are unchanged |
| `name`      | `--bucket` or `--space` (one setting, two spellings), then their variables, then the record, then the default, then a refusal naming `space` and `--bucket`                                                                                |
| `profile`   | `--profile`, then the record, then `AWS_PROFILE`, then empty                                                                                                                                                                               |
| `endpoint`  | `--endpoint` / `SLIVINGDOC_ENDPOINT`, then the record, then the SDK's own order (`AWS_ENDPOINT_URL_S3`, `AWS_ENDPOINT_URL`, the profile's `endpoint_url`), then empty                                                                      |
| `region`    | `--region`, then the record, then `AWS_REGION` and `AWS_DEFAULT_REGION`, then the profile's region, then `us-east-1`                                                                                                                       |
| `pathStyle` | `--path-style` / `SLIVINGDOC_PATH_STYLE`, then the record, then true when the effective endpoint is custom or the name contains a dot on the default endpoint, then false                                                                  |
| `prefix`    | `--prefix` / `SLIVINGDOC_PREFIX`, then the record, then the default, then the built-in prefix                                                                                                                                              |

The rules that make those layers safe:

- **A token ignores the record.** `SLIVINGDOC_TOKEN` names its own space, as it
  does today; a named space that differs is still the existing startup refusal.
- **The record binds a store, so ambient AWS settings do not move it.**
  `AWS_PROFILE`, `AWS_REGION`, and `AWS_ENDPOINT_URL*` are shared with other
  tools. They fill only what neither a flag nor the record names. An explicit
  `--profile` wins for the run, and it contributes its endpoint and region
  only where the record does not name them.
- **A `--storage` that names another kind makes the record not apply.** The
  run must then name its own target, which prevents a hosted space name from
  being read as an S3 bucket or the reverse.
- **A record's kind counts in `auto`.** A remembered S3 target is an S3
  signal; a remembered hosted target selects hosted, as a remembered space
  does today.
- **A named prefix that contradicts the record is refused** with the
  `prefixMismatch` wording, because the record says which notebook of the store
  the directory holds.
- **The default is consulted last, and only when no record supplied a
  target.** Its key is the effective store key `{kind, endpoint, region,
  profile}`. A default-sourced target is not recorded, exactly like a login's
  default space.
- **The effective store key is completed before a default is read.** When the
  kind is S3 and no flag or record names the name, the run loads the AWS
  configuration through `s3store.Resolve`, without retrieving credentials, and
  fills the profile's endpoint, region, and path style. The default is then
  looked up by the completed key. The store is built after the name is known.
- **`serve` reads, never writes.** It resolves its own `--workspace-root`
  through the same layers, which is how one record governs the CLI and the
  server. A settings file this build cannot read refuses every command that
  consults it, `serve` included, with the existing `unusableSettings` wording.
- **A named target beside a record is a choice for that run.** The operation
  uses it and leaves the record byte for byte.
- **A hosted default is not in `settings.defaults`.** It is the login's
  default space, looked up by endpoint in `credentials.json`, exactly as
  today; `settings.defaults` holds S3 targets only.

## Shared interfaces

```go
// internal/app
func (o ProcessOptions) WithReader(path string) ProcessOptions

// internal/workspace
type Identity struct {
    Endpoint, Region, Bucket, Prefix string
    ManifestVersion                  int
    SpaceID                          string
    Profile                          string // appended to the digest only when non-empty
}
```

`WithReader` sets only the reader, which `serve`, `status`, and `log` use;
`WithAssociation` sets the reader and the recorder for `pull` and `commit`, as
it does today. `settingsAssociation` gains `set()` and `read(workspaceRoot)` so
one cached set feeds the record lookup and the default lookup.

The consumer side of the sibling's types is fixed here:

- `storageInputs` carries `remembered func() (settings.Target, bool, error)`
  and `defaultTarget func(settings.StoreKey) (settings.Target, bool, error)`.
- `app` calls `s3store.Resolve` before a default is read and uses
  `Store.Resolved` after the store is built, for the effective endpoint,
  region, profile, and path style.
- `settings.Target`, `settings.StoreKey`, `Set.Default`, and
  `s3store.Config.Profile` are read and never changed.

No package outside `internal/workspace` reads a notebook identity.

## Invariants

A path that is not a row does not exist; a phase that needs one adds a row
first. Each row names its proving phase; the phase names the test.

| #  | Invariant                                                                                  | Actors                         | Mechanism                                                       | Proof   |
| -- | ------------------------------------------------------------------------------------------ | ------------------------------ | --------------------------------------------------------------- | ------- |
| 1  | The MCP server has one notebook directory and no tool takes a path                          | `serve`, `internal/mcp`        | `WithReader(L)`; schemas without `path`; strict refusal          | Phase 1 |
| 2  | A record is written only after success, and only when the directory remembered nothing      | `pull`, `commit`               | `recordsSpace`, extended to both kinds                          | Phase 2 |
| 3  | The record wins over ambient AWS settings; an explicit `--profile` wins for the run         | `internal/app`                 | The resolution table above                                      | Phase 2 |
| 4  | The store key is completed before a default is read                                         | `internal/app`                 | `s3store.Resolve` before a default is read                      | Phase 2 |
| 5  | The effective endpoint and region feed path style, the default key, identity, and proof     | `s3store`, `internal/app`      | `Resolve`, `Admin.Identity`, and the resolved config            | Phase 2 |
| 6  | A profile changes the notebook identity; an empty profile changes no existing key           | `internal/workspace`           | Conditional append in `writeIdentity`                           | Phase 2 |
| 7  | `serve` never writes a record                                                               | `cmd/serve`, `internal/app`    | it carries no recorder                                          | Phases 1 and 2 |
| 8  | The accepted-state, pack, CAS, and error-category invariants are unchanged                  | all                            | the existing suite                                              | Phase 3 |
| 9  | No credential enters the record, a log line, or caller text                                  | `internal/settings`, `s3store` | the `Target` schema; the key id stays display-only in `space`   | Phase 2 |

## Parameters and owners

| Parameter              | Value                                                                    | Owner              | Test                            |
| ---------------------- | ------------------------------------------------------------------------ | ------------------ | ------------------------------- |
| Default lookup key     | `{kind, endpoint, region, profile}`, completed before the lookup         | Phase 2            | Resolution tests                |
| Probe proof lifetime   | 24 hours, unchanged; the record gains `profile` and version 2            | Phase 2            | `TestOldProofIsIgnored`         |
| New fields             | `ProcessOptions.WithReader` (Phase 1), `workspace.Identity.Profile` (Phase 2) | Phases 1 and 2 | Each phase's table tests        |

The `--profile` flag, `s3store.Config.Profile`, `s3store.Resolve`, and
`Store.Resolved` already ship with the sibling; this worklog consumes them and
introduces no flag of its own.

## Definition of success

This worklog completes the product claim that the sibling started.

- `notes_pull` and `notes_commit` work with no `path`, and a `path` member is
  refused with a message naming `--workspace-root`.
- A directory that pulled or committed on S3 remembers its target; a later
  bare `pull` uses it; `serve --workspace-root <dir>` uses it too.
- A directory bound to a profile gets its own private state and pack cache,
  and does not reuse another profile's probe proof.
- One target serves `serve`, `pull`, `commit`, `status`, and `log` for the same
  directory, and no resolution rewrites an existing record.
- Together with the sibling: `slivingdoc space` lists and creates on both
  backends, `space <name>` sets the default, and `serve` with no name uses it.
- The public repository gate is green and every architecture doc named at the
  top is true.

## Validation policy

- Public repository: `make qa` (format, vet, staticcheck, fix, the race test
  with coverage, npm, dupl).
- Integration scenarios in `internal/integrationtest` are the behavioral
  contract of the public command surface. No phase adds a skip, a short mode,
  or a second gate.
- The S3 tests use the pinned test container through `internal/tests3`. No
  test reaches AWS, R2, or any live cloud endpoint.
- The cloud repository gate belongs to the sibling; this worklog runs no cloud
  command and claims no cloud result.

## Readiness checklist

1. The shared diagrams explain serve startup and pull/commit resolution;
   phase-local sequences appear only in their phase.
2. Every test has one owning phase and a file list; no phase redeclares
   another's test.
3. Every parameter, config field, flag, and invariant has one owner and a
   test; shared values appear once above.
4. Every phase that needs a person declares `Human required`. None does.
5. No phase depends on local detail from another phase or on text scheduled
   for deletion; the sibling's shipped types are named by symbol.
6. No phase creates human-owned configuration: no AWS profile, no cloud
   deployment, no site setting.
7. The conventions cited are the ones the code uses: the flag, environment,
   and default order of `internal/app`, the strict settings codec, and the SDK
   containment of `internal/s3store`.

## Session journal

### 2026-10-18: split into two worklogs

The single worklog bundled an additive admin surface with a rewrite of how
every command resolves and records a target, plus a breaking MCP schema
change. It was split by concern at design sign-off, before any phase started.
This worklog keeps the addressing half in three phases; the sibling keeps the
admin surface and the shared store model in eight. One phase moved here
unchanged apart from its number: one notebook directory per server became
Phase 1.

Boundary decisions recorded at the split:

- The record is written by `pull` and `commit`, so its recording rules,
  including the effective values, are here. The settings schema and the store
  key are the sibling's.
- `workspace.Identity.Profile` and the probe proof version are here, because
  they follow from how a target is resolved.
- The `space` command and the directory column it prints are the sibling's;
  this worklog never touches `cmd/login` or the `space` backends.

### 2026-10-18: decisions that bind this worklog

Settled in the parent interview and kept: one notebook directory per server
with no MCP `path` argument; a record per absolute directory path; the record
wins over ambient AWS settings; a named target is a choice for one run; a
default-sourced target is never recorded; lazy recording with no credential
scanning; `serve` reads and never writes; no command removes a record.

Rejected: per-call backend switching in the MCP server; a multi-notebook
server; enumerating every `~/.aws` profile at startup.

Status: README signed off; phases are written.
