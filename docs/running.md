# Configure and run

This document is the operator reference for running slivingdoc: every
flag, S3 credentials and requirements, logging, the notebook rules, and
the conflict and checkpoint behavior. The [README](../README.md) has
the short version.

## Commands

slivingdoc is a subcommand CLI. The subcommand comes first, before any
flag.

| Command         | Effect                                                        |
| --------------- | ------------------------------------------------------------- |
| `serve` (`s`)   | Serve the notebook over MCP stdio. This is the server.        |
| `pull` (`p`)    | Write the current notebook into a directory and exit.         |
| `commit` (`c`)  | Publish the changes at a directory (`-m <message>`) and exit. |
| `version` (`v`) | Print `slivingdoc <semver>` and exit, touching nothing else.  |

## Direct use: pull and commit

`pull` and `commit` are the human mirror of the two MCP tools. They run
the same startup sequence as `serve` — the pinned engine check and the
S3 compatibility probe — perform one operation, print the candid
result, and exit:

```text
slivingdoc pull notes
# edit UTF-8 text files under notes/
slivingdoc commit notes -m "meeting summary"
```

Each subcommand takes at most one notebook path, which may precede or
follow the flags. Omitting it uses the workspace root, which is the working
directory unless `--workspace-root` says otherwise. A path beginning with
`~/` resolves against the current user's home directory. Other relative
paths resolve against the working directory. The resolved path must stay at
or below the workspace root. `commit` requires `-m`/`--message`.

On success a subcommand writes the unified result report to stdout and
exits zero: the `OK` status token, the accepted remote generation, one
line per changed file with its insertion and deletion counts (a
zero-count side is omitted), and the totals trailer:

```text
OK  generation 18
  archive/old.md  -3
  notes/a.md  +1 -1
  notes/c.md  +2
3 files changed, 3 insertions(+), 4 deletions(-)
```

The diffstat answers "what is new to check out": `pull` reports the
delta between the visible directory before the pull and the materialized
result, and `commit` reports the increment the publication added over the
remote state it observed. A no-op synchronization reports an empty
stat.

A domain error prints the same status/detail/trailer skeleton to stdout
and exits nonzero: the status line (the code, a middle dot, and the
reason token), the message, one line per file with its reason rendered as
lower-case words and its one-based inclusive line ranges when present, a
`next:` line naming the caller's next step, whether a retry can help, the
recovery report when present, and a `writable:` trailer naming the
configured writable set followed by a `read-only:` trailer naming the
configured read-only set, each whenever that set is configured, on every
success and error report alike. With both sets configured a third
`path-rule: longest match decides` trailer follows them, because the two
sets can name the same region at different depths. A commit that conflicts with the remote reports each
file's reason and the line ranges to resolve:

```text
CONTENT_CONFLICT · MERGE_CONFLICT
Resolve the conflict blocks before notes_commit.
  notes/today.md  conflict  lines 12-18, 40-42
next: edit the files, then commit
retryable: false
```

Colour is presentation-only. The status tokens, the generation summary,
the per-file counts, and the conflict paths are coloured only when stdout
is a real terminal; piped or redirected output stays plain text. Any
non-empty `NO_COLOR` disables the colour even on a terminal.

A missing message, or more than one path, exits nonzero before any native
or network dependency is touched.

## Configuration

`serve`, `pull`, and `commit` read the same flags and environment
variables. Flags override environment variables, and the environment
overrides defaults.
`--bucket` is required. `-h` on any of the three prints the same
reference.

| Function              | Flag                     | Environment variable              | Default                   |
| --------------------- | ------------------------ | --------------------------------- | ------------------------- |
| S3 bucket (required)  | `--bucket`               | `SLIVINGDOC_BUCKET`               | —                         |
| S3 object prefix      | `--prefix`               | `SLIVINGDOC_PREFIX`               | `slivingdoc`              |
| S3 region             | `--region`               | `AWS_REGION`                      | `us-east-1`               |
| S3 endpoint           | `--endpoint`             | `AWS_ENDPOINT_URL_S3`             | empty (AWS resolution)    |
| S3 path-style access  | `--path-style`           | `SLIVINGDOC_PATH_STYLE`           | `false`                   |
| Workspace root        | `--workspace-root`       | `SLIVINGDOC_WORKSPACE_ROOT`       | session dir / working dir |
| Private state root    | `--private-root`         | `SLIVINGDOC_PRIVATE_ROOT`         | session dir / user cache  |
| Shared pack cache     | `--shared-pack-cache`    | `SLIVINGDOC_SHARED_PACK_CACHE`    | `false`                   |
| CAS retry limit       | `--commit-retries`       | `SLIVINGDOC_COMMIT_RETRIES`       | `8` (0..100)              |
| Checkpoint pack count | `--checkpoint-packs`     | `SLIVINGDOC_CHECKPOINT_PACKS`     | `256` (minimum 1)         |
| Retained checkpoints  | `--retained-checkpoints` | `SLIVINGDOC_RETAINED_CHECKPOINTS` | `1` (0..64)               |
| Read-only paths       | `--read-only-paths`      | `SLIVINGDOC_READ_ONLY_PATHS`      | empty (no read-only path) |
| Writable paths        | `--writable-paths`       | `SLIVINGDOC_WRITABLE_PATHS`       | empty (no confinement)    |
| Log levels            | `--log-level`            | `LOG_LEVEL`                       | `info`                    |
| Log timestamps        | `--log-timestamp`        | `SLIVINGDOC_LOG_TIMESTAMP`        | `true`                    |

`--workspace-root` is the root below which request paths may live, and is
also the notebook directory an omitted path resolves to. The private root
holds the internal Git repository, the state record, and the operation
locks. It must not be at or below the workspace root. Both roots become
absolute before startup.

### The session directory

`serve` with neither root configured takes a per-process session directory
and puts both roots inside it:

```text
<tmp>/slivingdoc-<random>/notebook    the workspace root
<tmp>/slivingdoc-<random>/private     the private root
```

This is the default because it needs no configuration and no coordination:
every server gets its own notebook directory and its own private state, so
concurrent agents never contend for one operation lock. The tools then need
no `path`, and both the server instructions and every tool result name the
directory. The whole session directory is removed at shutdown — the durable
notebook is the bucket, so nothing of value is in it. A process killed
outright leaves the directory for the operating system to reap; no later
process reuses it.

Configuring either root turns the default off, and neither root is removed
at shutdown. Use that when humans and agents share one directory, or when
you want the notebook to survive a server restart on disk. `pull` and
`commit` never take a session directory: they default to the working
directory, which you can still open after the process exits.

### The shared pack cache

By default every workspace keeps its own cache of downloaded pack bytes
inside its private state, so several agents on one machine each download the
same packs — and an ephemeral session throws its cache away at shutdown.
`--shared-pack-cache` moves that cache to one durable directory per
notebook:

```text
<user-cache-dir>/slivingdoc/pack-cache/<bucket>-<prefix>-<digest>/
```

Every server addressing the same endpoint, bucket, and prefix computes the
same directory from its own configuration, so agents share downloads with no
coordination: the first cold pull populates the directory and later pulls by
any agent read from it. Entries are keyed by SHA-256 and re-verified against
the authoritative manifest on every read, so a corrupt or foreign entry is
discarded and re-downloaded, never trusted. Only pack bytes are shared —
each workspace keeps its own private repository, baseline, and locks.

Writing into the cache is best-effort: a read-only or full cache directory
logs a warning and the operation continues. That makes a pre-populated
read-only cache (for example baked into a container image) work as-is.

The directory names make manual cleanup easy: remove a notebook's directory
when you are done with it, and the next pull simply re-downloads.

## Read-only paths

`--read-only-paths` (environment `SLIVINGDOC_READ_ONLY_PATHS`) marks a
comma-separated set of notebook-relative paths that one process's commits
may never change, while a process without the flag keeps full write
access to the same notebook. An entry protects itself and everything
below it — `docs` covers a file named `docs` and every path under
`docs/`. Use it to let a fleet of agents read injected material (FAQ
answers, reference documentation) without risking that one of them
overwrites it:

```text
slivingdoc serve --bucket my-notes --read-only-paths docs,faq.md
```

Every agent talking to that server sees `docs` and `faq.md` named in the
server instructions, in both tool descriptions, and in the `readOnly`
array of every pull and commit result, so an agent learns the rule before
it edits and again if it forgets. A human without the flag keeps
publishing changes normally:

```text
slivingdoc pull notes
# edit notes/docs/faq.md
slivingdoc commit notes -m "update the FAQ"
```

The next pull by any agent picks up that change with no conflict — the
read-only set is enforced only against the process configured with it,
not against the notebook itself. If an agent commits a change under
`docs/` anyway, the commit is refused, the touched files are reset to the
last accepted content, and the result names the violated entries:

```text
INVALID_REQUEST · READ_ONLY_PATH
docs is read-only in this server. Your changes there were discarded and the files reset. Write outside the read-only paths, then commit again.
  docs/faq.md  read-only
next: edit the files, then commit
retryable: false
read-only: docs
```

(the MCP structured result an agent decodes carries the same message plus
the stable `reason: "READ_ONLY_PATH"`, `action: "EDIT_FILES"`, a
`READ_ONLY` reason on the `docs/faq.md` file entry, and the `readOnly`
array naming every configured entry.)

The restore on pull applies to a workspace that passes the content rules.
An invalid file under a read-only path (a binary, a symlink, an invalid
name) is refused as `INVALID_CONTENT` naming that file, on pull and commit
alike, and the restore does not run until the file is deleted.

The read-only set is a guardrail at the MCP tool boundary, not a security
boundary against the agent: the serve process holds the S3 credentials,
and an agent that can read that environment or launch its own slivingdoc
process bypasses the setting — exactly like an operator's existing sftp
model, where the policy lives in the server configuration, never in the
data.

Like every other shared flag, an explicitly empty `--read-only-paths=`
clears an inherited `SLIVINGDOC_READ_ONLY_PATHS` environment value
instead of falling back to it. An invalid entry (an absolute path, a
`..` or `.git` segment, or a path over the length bound) refuses startup
before any native or network dependency loads.

## Writable paths

`--writable-paths` (environment `SLIVINGDOC_WRITABLE_PATHS`) is the other
half of the same setting. It marks the comma-separated notebook-relative
paths one process's commits *may* change, and every path it does not cover
becomes read-only for that process. Use it to confine a fleet of agents to
a directory each, where listing what they must not touch is not possible:
a directory that did not exist at startup, and a file at the notebook
root, would otherwise stay writable.

```text
slivingdoc serve --bucket my-notes --writable-paths agents/scout
```

The two settings compose, and the longest matching entry wins, so a
protected region can hold a writable subdirectory:

```text
slivingdoc serve --bucket my-notes --read-only-paths docs --writable-paths docs/drafts
```

That process may write under `docs/drafts` and nowhere else. The writable
set is non-empty, so the unmatched default is protected: `notes/`, files at
the notebook root, and directories that do not exist yet are read-only for
it too, not only the `docs` named by `--read-only-paths`.

Entries follow the read-only rules unchanged: an entry covers itself and
everything below it, matched on segment boundaries. A path named by both
settings is a configuration error rather than a silent precedence rule.
Startup refuses, naming the path and both settings, before any native or
network dependency loads — the same point at which an invalid entry in
either set refuses. The entries compared are the ones you wrote, so an
entry that also sits below another entry of its own setting is refused just
the same.

Nesting can go deeper than one level, and the entries you wrote decide
there too:

```text
slivingdoc serve --bucket my-notes \
  --read-only-paths notes,notes/agent-a/locked --writable-paths notes/agent-a
```

That process may write under `notes/agent-a`, except under
`notes/agent-a/locked`, which the longer read-only entry protects again.
Listing the broader `notes` beside it changes nothing about the narrower
entry: adding an entry to a setting never makes a narrower entry of the
same setting stop applying.

Every surface the process advertises says so. With both settings
configured, the server instructions, both tool descriptions, the result
text item and the report name both sets and end with the rule that decides
between them — the read-only sentence says "where the two sets nest, the
longest matching entry decides" rather than "write elsewhere", which a
non-empty writable set makes false — so an agent is never told to write
only under an entry and, in the next sentence, that changes under it are
refused.

Enforcement is the read-only enforcement above, evaluated against the
composed policy: a commit that changes a protected path is refused and the
touched files are reset to the last accepted content, and a pull restores
those paths from the accepted remote state. The refusal names where the
process *may* write, because under a writable set the protected region is
nearly the whole notebook.

Like every other shared flag, an explicitly empty `--writable-paths=`
clears an inherited `SLIVINGDOC_WRITABLE_PATHS` environment value instead
of falling back to it. That is how a process asks not to be confined.

## S3 credentials

slivingdoc has no authentication layer of its own. `serve`, `pull`, and
`commit` all build the S3 client the same way, and credentials come
from the AWS SDK default credential chain, resolved by the SDK at
startup:

1. Environment variables — `AWS_ACCESS_KEY_ID`,
   `AWS_SECRET_ACCESS_KEY` (plus `AWS_SESSION_TOKEN`).
2. The shared config and credentials files (`~/.aws/credentials`,
   `~/.aws/config`), honoring `AWS_PROFILE`.
3. Ambient identity — SSO sessions, ECS/EKS task roles, and the EC2
   instance metadata service.

slivingdoc's own flags shape _where_ the client points (`--bucket`,
`--prefix`, `--region`, `--endpoint`), never _who it is_. No flag
carries a credential, and a `--endpoint` URL with user information is
refused, so a secret can never echo into a diagnostic.

There are three ways to deliver credentials, and the choice is a
deployment decision:

- **Inherit.** The process inherits the environment of whatever
  launched it. A shell with an exported profile or an active SSO
  session needs nothing else — this covers `slivingdoc pull` and
  `commit` run by hand, and a `serve` whose MCP host was started from
  that shell.
- **Inject.** Most MCP hosts accept an `env` block per server (see the
  example below). Use it when the host is not launched from a
  credentialed shell — a GUI app, a service manager — or to point at a
  local S3-compatible store (such as SeaweedFS). Prefer injecting
  `AWS_PROFILE` over pasting static keys: host configuration files tend
  to be synced and backed up, while a profile keeps the secret in
  `~/.aws/credentials`.
- **Ambient.** On EC2, ECS, or EKS, an attached role satisfies the
  chain with no configuration at all. This is the cleanest server
  deployment.

Credentials stay inside the slivingdoc process. They never cross the
MCP protocol — the client sees only `notes_pull`, `notes_commit`, and
their result envelopes — and the redaction layer keeps key material out
of every error and log line as defense in depth.

One consequence of the one-shot commands: `serve` resolves the chain
once and holds the session, while every `pull` or `commit` invocation
resolves it fresh. With short-lived STS or SSO credentials each
invocation needs a currently valid session. An expired login surfaces
as a redacted startup refusal (the compatibility probe fails), not a
mid-operation error.

## S3 requirements

The bucket must exist. slivingdoc does not create or configure it.

The server needs these permissions:

- On the objects (`arn:...:bucket/*`): `s3:GetObject`, `s3:PutObject`,
  `s3:DeleteObject`, `s3:CreateMultipartUpload`, `s3:UploadPart`,
  `s3:CompleteMultipartUpload`, `s3:AbortMultipartUpload`, and
  `s3:ListMultipartUploadParts`.
- On the bucket (`arn:...:bucket`): `s3:ListBucket` and
  `s3:ListBucketMultipartUploads`.

The reusable Terraform module in [`terraform/`](../terraform/) grants
exactly this policy.

A custom S3-compatible service is configured with an absolute `http` or
`https` `--endpoint`. The server always uses path-style addressing for
a custom endpoint. `--path-style` extends that to the default AWS
endpoint.

Before the first MCP call, the server runs a disposable compatibility
probe below the configured prefix. The probe proves that the store
enforces `If-None-Match: *` creation, `If-Match` replacement, and
read-after-write behavior — the three conditional-write guarantees the
publication protocol requires. A store that fails the probe is refused
at startup with the `INCOMPATIBLE_STORE` category; when the failure is
an operational error rather than a missing capability, the diagnostic
names the underlying reason (for example the S3 `AccessDenied` or
`InvalidAccessKeyId` error) while the probe key and any secret stay
redacted. Bucket versioning is not required.

## MCP host configuration

An MCP host starts the server as a child process and speaks MCP
JSON-RPC over stdio. A typical client configuration is:

```json
{
  "mcpServers": {
    "slivingdoc": {
      "command": "npx",
      "args": ["-y", "slivingdoc", "serve", "--bucket", "my-notes"],
      "env": {
        "AWS_PROFILE": "notes"
      }
    }
  }
}
```

With no `--workspace-root`, the server takes its own session directory. The
agent omits `path` or sends an empty string when it calls `notes_pull` and
`notes_commit`. Add
`"--workspace-root", "/srv/notes"` to the args when humans and agents should
share one fixed directory instead.

The `env` block is the injection route from [S3
credentials](#s3-credentials): the host passes these variables to the
child process, and the AWS SDK chain picks them up. Omit it when the
host already runs in a credentialed environment; replace it with
`AWS_ENDPOINT_URL_S3` and static keys only for a local S3-compatible
store such as SeaweedFS.

Stdout carries only protocol messages; logs go to stderr. The host and
the server share the visible directory: agents and humans edit files
there, and the server scans them at each call. A human edit made with
any editor is published by the next `notes_commit` for that path — or
directly with `slivingdoc commit [path] -m <message>`. This sharing needs
`--workspace-root`: a session directory is private to the server process.

## Logging

Logging is configured by the environment, which applies to every command
and works before flags are parsed. `serve`, `pull`, and `commit` also
take `--log-level` and `--log-timestamp`, which override the environment
once the flags resolve; the few records emitted before that point (the
command router, a configuration refusal) follow the environment. Records
are structured `key=value` text on stderr. Each record carries a
timestamp (unless `--log-timestamp=false`), a level, and the module that
emitted it.

| Variable    | Effect                                              |
| ----------- | --------------------------------------------------- |
| `LOG_LEVEL` | Per-module levels. A bare level is the default.     |
| `SLIVINGDOC_LOG_TIMESTAMP` | `false` removes the `time=` field, for hosts that stamp log lines themselves. |
| `NO_COLOR`  | Any non-empty value disables ANSI colour: log levels and the CLI report. |

`LOG_LEVEL` takes a comma-separated list. `module=level` sets one
module; a bare `level` sets the default for the rest:

```text
LOG_LEVEL="cli=warn,mcp=debug,info"
```

The modules are `cli` (command routing), `app` (startup and shutdown),
`mcp` (one record per tool call, carrying `mcpReqID`), and `notebook`
(best-effort checkpoint and cleanup records). Levels are `debug`,
`info`, `warn`, and `error`. A malformed `LOG_LEVEL` is reported and
falls back to `info`; it never refuses startup. An invalid `--log-level`
flag value, in contrast, refuses startup like any other flag.

## Profiling

`DEBUG_PERF` captures performance profiles across one whole command —
startup, the operation, and shutdown — for finding where a slow `pull`
or `commit` spends its time. `1` (or `true`) writes under
`slivingdoc-perf/` in the system temporary directory; `0`, `false`, and
empty disable the capture; any other value is the base directory
itself. Each invocation creates its own timestamped run directory under
the base, so repeated benchmark runs never overwrite each other and can
be compared with `go tool pprof -diff_base`.

A run directory holds three artifacts:

| File         | Shows                                                        |
| ------------ | ------------------------------------------------------------ |
| `cpu.pprof`  | where CPU time went: `go tool pprof cpu.pprof`                |
| `heap.pprof` | what the command retained at exit: `go tool pprof heap.pprof` |
| `trace.out`  | the execution timeline, including time blocked on the network and on locks: `go tool trace trace.out` |

For an operation dominated by object-store round trips, the CPU profile
stays near-empty and `trace.out` shows the waiting; that split is the
point of capturing both.

```bash
DEBUG_PERF=1 slivingdoc pull ./notes
```

The exact paths are reported on stderr when the capture starts and
finishes; stdout stays protocol-only. A capture that cannot start or
finish is a warning, never a refusal, and never changes the command's
result or exit code.

## Notebook rules

- Files must be valid UTF-8 text without the NUL character (U+0000).
  Empty files are valid. Bytes and line endings are preserved.
- Symbolic links, devices, sockets, and named pipes are rejected.
- An MCP request `path` is optional; omitting it uses the server's notebook
  directory, which every result reports. When supplied it may begin with
  `~/`, which resolves against the current user's home directory. The
  resulting absolute host path must be 1 through 4,096 bytes and below the
  configured workspace root. A subcommand path is optional too and may be
  relative; it resolves against the working directory before the same root
  rule applies.
- A commit `message` must be non-blank UTF-8 without U+0000, at most
  16,384 bytes. Messages are retained in recent internal history only.
- A complete conflict-marker block (`<<<<<<< local`, `=======`,
  `>>>>>>> remote` at column zero) is never accepted into the notebook,
  even if it was written by hand.

## Conflict recovery

When your changes and the accepted remote state change the same lines,
the operation returns `CONTENT_CONFLICT`, rewrites the visible
directory with the merged result, and leaves conflict markers in the
affected files:

```text
<<<<<<< local
the caller's text
=======
the accepted remote text
>>>>>>> remote
```

The structured error names every affected path and marker line range.
Resolve the files with ordinary file tools: edit them, delete the
marker lines, keep the text you want. Then call `notes_commit` — or run
`slivingdoc commit` — again.
The resolved directory becomes your local intent, and the server merges
it against any newer remote state. No Git command is involved.

## Checkpoints and retention

Each normal commit uploads one small incremental pack. To bound
cold-start downloads, the server periodically compacts a stable prefix
of accepted increments into one complete-state checkpoint pack. After
`--checkpoint-packs` (256 by default) active increments, one
checkpoint is scheduled. It never blocks writers, and its failure never
changes an accepted commit.

`--retained-checkpoints` (1 by default) keeps the previous checkpoint
generation's descriptors so a stale reader can restart. A later
checkpoint deletes older physical storage on a best-effort basis.
Checkpoints preserve current file state, not permanent history.

## Operational ownership

slivingdoc guarantees that accepted state is durably indexed by one
authoritative manifest, that failed concurrent publications cannot
silently overwrite accepted state, and that checkpoint and cleanup
failures do not corrupt current state.

Bucket versioning, replication, object lock, lifecycle rules, and
external backups are deployment recovery policies. They complement
slivingdoc, but they are not hidden prerequisites of the
synchronization algorithm. Choose them according to your own recovery
requirements.
