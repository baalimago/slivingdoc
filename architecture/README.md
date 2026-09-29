# Architecture docs index

One doc per command and subsystem of **slivingdoc**, the Go CLI and MCP
server. Each doc names the files and symbols that implement its concern,
the main flow, the rules the code enforces, and the traps. Read this
index, open the one doc that matches your task, and go straight to the
files it lists instead of scanning the repository.

These docs replace the former single `docs/slivingdoc-v1.md` contract.
The code is the source of truth: if a doc and the code disagree, trust
the code and fix the doc in the same change (see
[AGENTS.md](../AGENTS.md#architecture-docs)).

## Start here

- **[overview.md](./overview.md)**: purpose, scope, terms (L, P, R,
  baseline, pack, increment, checkpoint, manifest, generation, CAS), why
  Git plus S3, a one-line map of every `internal/` package, and the
  dependency direction between them.
- **[product-contract.md](./product-contract.md)**: the public surface:
  the two operations and their CLI mirror, strict inputs, the success
  envelope (`SuccessInfo`, diffstat) and error envelope (`ToolError`,
  code/reason/action tokens), the CLI report (`app.Report`), and
  read-only and writable paths (`PathPolicy`).

## Commands and operations

- **[pull.md](./pull.md)**: `Notebook.Pull` step by step: recovery,
  scan, `readRemote`, protected-path pinning, merge, clean versus conflict
  materialization, `MarkPulled`, first pull.
- **[commit.md](./commit.md)**: `Notebook.Commit`: validation order,
  first versus incremental proposals, `UploadUnique`, the `current` CAS
  (`publish`), bounded retry and `REMOTE_BUSY`, lost-response proof
  (`lookupPublication`, `PUBLICATION_UNPROVEN`), the checkpoint trigger.
- **[conflicts.md](./conflicts.md)**: the three-tree merge
  (`git.Merge`), marker format and labels, `FindConflictBlocks`,
  `MaterializeTree`, unresolved-marker refusal, and how
  `CONTENT_CONFLICT` reaches MCP and the CLI.
- **[checkpoints.md](./checkpoints.md)**: checkpoint trigger and
  compaction (`planCheckpoint`, `compactManifest`), shallow history,
  retention (`--retained-checkpoints`), and generation-fenced cleanup.
- **[cli.md](./cli.md)**: `main.go`, `cli.Run` and the `cmd/serve`,
  `pull`, `commit`, `status`, `log`, `login`, `logout`, `space`, `version` commands, `app.Setup` to `Runtime`, the
  shutdown path, the CLI report and colour, `DEBUG_PERF`.
- **[tui.md](./tui.md)**: the terminal presentation shared by every
  command (`internal/tui`): palette, marks, columns, the progress spinner,
  the table picker, the home screen, and what stays plain for scripts.
- **[login.md](./login.md)**: `slivingdoc login`, `space` and `logout`
  (the browser device flow and the account key, `internal/sitelogin`),
  the credentials file and default spaces (`internal/credentials`),
  minted space tokens, and `--storage auto|hosted|s3`: which store and
  credential a process picks, the hosts a key or token may reach, and
  the login's threat model.
- **[mcp-server.md](./mcp-server.md)**: the two MCP tools, schemas and
  descriptions, `decodePull`/`decodeCommit`, success and error shaping,
  instructions, `mcpReqID` logging, SDK log demotion.

## Subsystems

- **[notebook.md](./notebook.md)**: `internal/notebook`: the `Notebook`
  type, `Config` defaults and ranges, remote reading and the pack cache,
  `Result`, `Metrics`, failpoints, backoff, error constructors.
- **[git-engine.md](./git-engine.md)**: `internal/git` (the `Engine`
  and `Repository` seam, trees, commits, packs, OIDs, path and content
  rules, policy types) and `internal/git2` (the only CGo package,
  pinned libgit2).
- **[workspace.md](./workspace.md)**: `internal/workspace`: path
  confinement, the scan and content rules, the P layout, `state.json`,
  the operation lock, rewriting L in place, recovery-required mode.
- **[storage.md](./storage.md)**: `internal/storage`: object layout and
  key grammar, the `current` manifest, pack integrity and metadata, the
  semantic errors and `Refusal`, the probe, `UploadUnique`, the
  `ObjectStore` interface, contract suite and fake. Start here to
  implement a new backend.
- **[s3store.md](./s3store.md)**: `internal/s3store` over the AWS SDK
  (addressing, conditional writes, multipart, error mapping, metadata
  headers, IAM permissions) and the `internal/tests3` container.
- **[hosted-mode.md](./hosted-mode.md)**: `internal/httpstore`, the
  hosted storage API adapter selected by `SLIVINGDOC_TOKEN` or a stored
  login ([login.md](./login.md)): settings,
  the `CheckAccess` startup check, requests and redirects, the
  status-to-error mapping, retries, compaction of a full space, and the
  `gatewaytest` reference server.
- **[config.md](./config.md)**: every flag and environment variable,
  defaults, bounds, precedence, endpoint normalization, hosted-mode
  selection, the session directory, the shared pack cache, path sets,
  credentials.
- **[errors.md](./errors.md)**: the error taxonomy from storage and Git
  up to the tool result and the CLI exit code: codes, retryability,
  reason and action tokens, redaction, strict JSON rejections.
- **[logging.md](./logging.md)**: `slog` setup, per-module levels,
  `LOG_LEVEL`, request correlation through `notebook.WithLogger`, the
  SDK logger.
- **[security.md](./security.md)**: the trust model: stdio only, path
  canonicalization, symlink and special-file rejection, private state,
  credentials, redaction.
- **[guarantees.md](./guarantees.md)**: failure behavior and publication
  order, generic recovery (`recoverState`), recovery-required mode,
  concurrency and scaling, what `Metrics` measures.

## Build, test, release, operate

- **[testing.md](./testing.md)**: the test layers, `make test`, effect
  seams, the integration scenario harness, fault injection, the S3 test
  container and lease, coverage.
- **[build.md](./build.md)**: the native build with pinned static
  libgit2, the Makefile, targets and dependency baselines, `SHA256SUMS`,
  CI, and the npm launcher.
- **[releasing.md](./releasing.md)**: cutting a release: `make release`,
  `release.yml`, the GitHub release, npm and MCP Registry publication
  order, and the card's wait for the npm version.
- **[running.md](./running.md)**: the operator reference: commands,
  flags, credentials, MCP host setup, logging, notebook rules, conflict
  recovery, checkpoints, operational ownership.
- **[compatibility.md](./compatibility.md)**: the promise from 1.0.0 on:
  what stays stable (stored formats, tools, error tokens, command line),
  what may change, and what an older slivingdoc does with a newer
  notebook (`UPGRADE_REQUIRED`).
- **[decisions.md](./decisions.md)**: recorded architecture decisions,
  deferred work, and the invariants a change must not break.

## Reading order suggestions

- New to the repo: **overview.md → product-contract.md → notebook.md**.
- Changing pull or commit: **notebook.md → pull.md or commit.md →
  conflicts.md → guarantees.md**.
- Adding a storage backend: **storage.md → s3store.md or hosted-mode.md
  → testing.md**.
- Working on hosted storage: **hosted-mode.md → login.md → storage.md →
  errors.md → commit.md → checkpoints.md**.
- Adding a flag or command: **config.md → cli.md → running.md**.
- Changing what an agent sees: **product-contract.md → mcp-server.md →
  errors.md**.
- Changing a stored format, a flag or an error token: **compatibility.md →
  storage.md or config.md → errors.md**.
- Debugging a failed build or release: **build.md → releasing.md**.

## The hosted service

slivingdoc.dev (the storage gateway, sign-in and billing) lives in
[baalimago/slivingdoc-cloud](https://github.com/baalimago/slivingdoc-cloud),
which has its own `architecture/README.md` index.
