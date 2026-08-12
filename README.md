# slivingdoc

slivingdoc is a standalone notebook synchronization server for agents. It
exposes two MCP tools over stdio — `notes_pull(path)` and
`notes_commit(path, message)` — and keeps one caller-controlled visible
directory synchronized with an S3-compatible bucket. Many agents can edit
the same notes; slivingdoc merges concurrent changes and resolves conflicts
with visible text markers.

slivingdoc uses Git data structures and Git merge behavior internally, but
it never invokes a Git executable and never exposes a Git repository. The
native binary statically links the pinned libgit2 release, so users do not
install Git, libgit2, or a C toolchain.

See [`architecture/slivingdoc-v1.md`](architecture/slivingdoc-v1.md) for the
accepted design and [`worklogs/26-08-08-v1-implementation/README.md`](worklogs/26-08-08-v1-implementation/README.md)
for the phase plan.

## How it works

The server exposes exactly two tools.

| Tool           | Inputs            | Success result |
| -------------- | ----------------- | -------------- |
| `notes_pull`   | `path`            | `OK`           |
| `notes_commit` | `path`, `message` | `OK`           |

The normal workflow is: pull the notebook into `path`, edit UTF-8 text files
there with ordinary file tools, then commit the changes.

```text
notes_pull(path)
        |
        v
edit UTF-8 text files at path
        |
        v
notes_commit(path, message)
```

`notes_pull` writes the current notebook into the path and records the
accepted state. `notes_commit` publishes the caller's changes and
incorporates concurrent non-conflicting changes. Both return exactly `OK`
on success. The caller never sees Git object IDs, pack names, S3 keys, or
the private state directory; those are internal implementation details.

Accepted state lives in one S3 object, the manifest `current`, below a
configured prefix. Packs of Git objects are immutable and uploaded before
the manifest references them; concurrent writers merge and retry instead of
overwriting each other. The S3 bucket is the durability boundary: private
local state (`P`) is only a cache that can be rebuilt from `current`.

## Installation

The primary installation path is the npm launcher. It requires Node.js 24
or newer and one of the supported platforms:

| OS      | Architectures |
| ------- | ------------- |
| Linux   | amd64, arm64  |
| macOS   | amd64, arm64  |
| Windows | amd64         |

The launcher is a small Node program with no dependencies. On first run it
downloads the native binary for its exact version and platform from the
matching GitHub release, verifies the published SHA-256, caches the verified
bytes under the npm cache, and then executes it with your arguments and
standard streams. Unsupported platforms fail before any download with an
actionable error. The launcher never writes to stdout: stdout belongs to the
MCP protocol and to the child process.

```text
npx -y slivingdoc version
```

A native binary can also be downloaded directly from the GitHub release
(`slivingdoc-v<semver>-<os>-<arch>`, `.exe` on Windows) and run in place.
Neither path requires Git, libgit2, or a C toolchain.

## Configure and run

slivingdoc is a subcommand CLI. The subcommand comes first, before any
flag.

| Command         | Effect                                                       |
| --------------- | ------------------------------------------------------------ |
| `serve` (`s`)   | Serve the notebook over MCP stdio. This is the server.       |
| `version` (`v`) | Print `slivingdoc <semver>` and exit, touching nothing else. |

```text
slivingdoc serve --bucket my-notes --workspace-root /srv/notes
```

`serve` reads configuration from flags and environment variables. Flags
take precedence; the environment overrides defaults. `--bucket` is
required.

| Function              | Flag                     | Environment variable              | Default                   |
| --------------------- | ------------------------ | --------------------------------- | ------------------------- |
| S3 bucket (required)  | `--bucket`               | `SLIVINGDOC_BUCKET`               | —                         |
| S3 object prefix      | `--prefix`               | `SLIVINGDOC_PREFIX`               | `slivingdoc`              |
| S3 region             | `--region`               | `AWS_REGION`                      | `us-east-1`               |
| S3 endpoint           | `--endpoint`             | `AWS_ENDPOINT_URL_S3`             | empty (AWS resolution)    |
| S3 path-style access  | `--path-style`           | `SLIVINGDOC_PATH_STYLE`           | `false`                   |
| Workspace root        | `--workspace-root`       | `SLIVINGDOC_WORKSPACE_ROOT`       | startup working dir       |
| Private state root    | `--private-root`         | `SLIVINGDOC_PRIVATE_ROOT`         | `<user-cache>/slivingdoc` |
| CAS retry limit       | `--commit-retries`       | `SLIVINGDOC_COMMIT_RETRIES`       | `8` (0..100)              |
| Checkpoint pack count | `--checkpoint-packs`     | `SLIVINGDOC_CHECKPOINT_PACKS`     | `1024` (minimum 1)        |
| Retained checkpoints  | `--retained-checkpoints` | `SLIVINGDOC_RETAINED_CHECKPOINTS` | `1` (0..64)               |

`--workspace-root` is the root below which request paths may live. The
private root holds the internal Git repository, the state record, and the
operation locks; it must not be at or below the workspace root. Both roots
become absolute before startup. `slivingdoc serve -h` prints the full
reference.

### S3 configuration

The bucket must exist; slivingdoc does not create or configure it. The
server needs the following permissions below the configured prefix:

- `s3:GetObject`, `s3:PutObject`, and `s3:DeleteObject`
- multipart upload and multipart abort (`s3:AbortMultipartUpload`,
  `s3:ListBucketMultipartUploads`, `s3:ListMultipartUploadParts`)
- `s3:ListBucket` restricted to that prefix for checkpoint cleanup

Credentials come from the AWS SDK default credential chain (environment
variables, the shared credentials file, IAM roles, and so on). A custom
S3-compatible service is configured with an absolute `http` or `https`
`--endpoint`; the server always uses path-style addressing for a custom
endpoint, and `--path-style` extends that to the default AWS endpoint.

Before serving any MCP call, the server runs a disposable compatibility
probe below the configured prefix. It proves that the store enforces
`If-None-Match: *` creation, `If-Match` replacement, and read-after-write
behavior — the three conditional-write guarantees the publication protocol
requires. A store that fails the probe is refused at startup. Bucket
versioning is not required.

### stdio configuration

An MCP host starts the server as a child process and speaks MCP JSON-RPC
over stdio. A typical client configuration is:

```json
{
  "mcpServers": {
    "slivingdoc": {
      "command": "npx",
      "args": [
        "-y",
        "slivingdoc",
        "serve",
        "--bucket",
        "my-notes",
        "--workspace-root",
        "/srv/notes"
      ]
    }
  }
}
```

Stdout carries only protocol messages; logs go to stderr. The host and the
server share the visible directory, so agents edit files and the server
scans them at each call.

### Logging

Logging is configured by the environment, not by flags, so it applies to
every command and works before flags are parsed. Records are structured
`key=value` text on stderr, each carrying a timestamp, a level, and the
module that emitted it.

| Variable    | Effect                                              |
| ----------- | --------------------------------------------------- |
| `LOG_LEVEL` | Per-module levels. A bare level is the default.     |
| `NO_COLOR`  | Any non-empty value disables the ANSI level colour. |

`LOG_LEVEL` takes a comma-separated list where `module=level` sets one
module and a bare `level` sets the default for the rest:

```text
LOG_LEVEL="cli=warn,mcp=debug,info"
```

The modules are `cli` (command routing), `app` (startup and shutdown),
`mcp` (one record per tool call, carrying `mcpReqID`), and `notebook`
(best-effort checkpoint and cleanup records). Levels are `debug`, `info`,
`warn`, and `error`. A malformed `LOG_LEVEL` is reported and falls back to
`info`; it never refuses startup.

## Notebook rules

- Files must be valid UTF-8 text without the NUL character (U+0000).
  Empty files are valid; bytes and line endings are preserved.
- Symbolic links, devices, sockets, and named pipes are rejected.
- A request `path` is an absolute host path of 1 through 4,096 bytes, below
  the configured workspace root.
- A commit `message` must be non-blank UTF-8 without U+0000, at most 16,384
  bytes. Messages are retained in recent internal history only.
- A complete conflict-marker block (`<<<<<<< local`, `=======`,
  `>>>>>>> remote` at column zero) is never accepted into the notebook, even
  if it was written by hand.

## Conflict recovery

When your changes and the accepted remote state change the same lines, the
operation returns `CONTENT_CONFLICT`, rewrites the visible directory with
the merged result, and leaves conflict markers in the affected files:

```text
<<<<<<< local
the caller's text
=======
the accepted remote text
>>>>>>> remote
```

The structured error names every affected path and marker line range.
Resolve the files with ordinary file tools — edit them, delete the marker
lines, keep the text you want — and call `notes_commit` again. The resolved
directory becomes your local intent, and the server merges it against any
newer remote state. No Git command is involved.

## Checkpoints and retention

Each normal commit uploads one small incremental pack. To bound cold-start
downloads, the server periodically compacts a stable prefix of accepted
increments into one complete-state checkpoint pack. After `--checkpoint-packs`
(1,024 by default) active increments, one checkpoint is scheduled; it never
blocks writers and its failure never changes an accepted commit.

`--retained-checkpoints` (1 by default) keeps the previous checkpoint
generation's descriptors so a stale reader can restart. A later checkpoint
deletes older physical storage on a best-effort basis. Checkpoints preserve
current file state, not permanent history.

## Operational ownership

slivingdoc guarantees that accepted state is durably indexed by one
authoritative manifest, that failed concurrent publications cannot silently
overwrite accepted state, and that checkpoint and cleanup failures do not
corrupt current state.

Bucket versioning, replication, object lock, lifecycle rules, and external
backups are deployment recovery policies. They complement slivingdoc but are
not hidden prerequisites of the synchronization algorithm; you choose them
according to your own recovery requirements. Metrics for the planning
workload (100 agents, one commit per minute) are recorded in the worklog.

## Evaluate locally with MinIO

An example environment for manual evaluation — one pinned MinIO container
and a step-by-step walkthrough — lives in
[`examples/minio/`](examples/minio/). Automated tests create their own
containers through testcontainers and never depend on the example.

## Debug against AWS S3

The reusable Terraform module in [`terraform/`](terraform/) provisions the
private AWS S3 bucket, the least-privilege IAM user, and the access keys
for one notebook. The debug configuration for the `slivingdoc` bucket in
`eu-north-1` lives in [`examples/terraform/`](examples/terraform/) and
calls that module. The automated suites never touch AWS.

## Building

The repository builds the pinned static libgit2 into `.build` and then
compiles the server against it:

```text
make libgit2   download, verify, and build the pinned static libgit2
make test      every Go test: race, real libgit2, real MinIO containers
make npm-test  the zero-dependency npm launcher suite
make lint      gofumpt, vet, staticcheck, and go fix
make build     release-style native binary in .build/slivingdoc
make qa        lint plus both test suites
```

The full build procedure lives in [`docs/build-libgit2.md`](docs/build-libgit2.md).

## Development

Contributing to slivingdoc itself:

- [`AGENTS.md`](AGENTS.md) — the developer and agent guide: package map,
  operation flows, startup wiring, conventions, and the QA gates.
- [`docs/build.md`](docs/build.md) — the native build procedure, from the
  pinned libgit2 source to dependency inspection on each target platform.
- [`docs/testing.md`](docs/testing.md) — the two test commands, the seven
  test layers behind them, and the no-live-AWS rule.
- [`architecture/slivingdoc-v1.md`](architecture/slivingdoc-v1.md) — the
  accepted contract. A change to the tool schemas, the error taxonomy, the
  storage protocol, or the package boundaries updates it in the same commit.

## Distribution

Release tags `v<semver>` publish native binaries for linux/amd64,
linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64 (architecture
section 21) together with a strict `SHA256SUMS` file and the license
`NOTICE`. The npm package `slivingdoc` is a launcher: it selects the asset
for its exact version and platform, verifies the published SHA-256, caches
the verified bytes under the npm cache, and forwards stdio to the child:

```text
npx -y slivingdoc serve --bucket my-notes --workspace-root /srv/notes ...
```

Unsupported platforms fail before any download with an actionable error.
`npm run check-release` (wired into `prepublishOnly`) fails until the
GitHub release for the package version contains every required artifact, so
npm publication can never precede the complete GitHub release.
