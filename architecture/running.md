# Configure and run

The operator reference for running slivingdoc: the commands, the result report, every flag, the session directory and shared pack cache, read-only and writable paths, S3 credentials and requirements, hosted storage and logging in, MCP host configuration, logging, profiling, the notebook rules, conflict recovery, checkpoints, and operational ownership. It answers "how do I run it, configure it, and read what it tells me?" The [README](../README.md) has the short version.

Read this when: operating or deploying slivingdoc, writing an MCP host configuration, changing any operator-visible behavior (then update this doc in the same change), or looking for the user-facing contract behind a code path.

## Key files

| File | Purpose |
|------|---------|
| `internal/cli/cli.go` | Command map and router `Usage` ([cli.md](./cli.md)) |
| `cmd/serve/serve.go`, `cmd/pull/pull.go`, `cmd/commit/commit.go`, `cmd/login/login.go`, `cmd/version/version.go` | The commands and their help text |
| `internal/app/login.go`, `internal/app/space.go`, `internal/app/minted.go`, `internal/app/storage.go`, `internal/credentials/credentials.go` | `login`/`logout`/`space`, minted tokens, `--storage` selection, the credentials file ([login.md](./login.md)) |
| `internal/app/config.go` | `FlagReference`, `HelpText`, precedence and validation ([config.md](./config.md)) |
| `internal/app/command.go` | `Report`: the CLI result report |
| `internal/app/logging.go`, `internal/app/perf.go` | Logging and `DEBUG_PERF` ([logging.md](./logging.md)) |
| `internal/mcp/server.go` | Tool descriptions, instructions, envelopes ([mcp-server.md](./mcp-server.md)) |
| `internal/storage/probe.go` | The startup compatibility probe ([storage.md](./storage.md)) |
| `internal/httpstore/store.go` | The hosted storage adapter and its startup `CheckAccess` ([hosted-mode.md](./hosted-mode.md)) |
| `terraform/` | Module that provisions the bucket and a least-privilege IAM user with access keys, granting exactly the permissions in S3 requirements |
| `examples/seaweedfs/`, `examples/terraform/` | Runnable local S3 and deployment examples |

## Flow

```text
MCP host → npx -y slivingdoc serve --bucket B    # launcher → native binary (build.md)
  startup: config → pinned libgit2 → S3 probe (hosted: access check) → serve two tools over stdio
  agent: notes_pull → edit UTF-8 files in the notebook directory → notes_commit
human: slivingdoc pull [path] → edit → slivingdoc commit [path] -m "msg"
hosted: slivingdoc login → approve in the browser → [slivingdoc space <name>] → serve, pull and commit mint space tokens
```

## Commands

slivingdoc is a subcommand CLI. The subcommand comes first, before any
flag.

| Command         | Effect                                                        |
| --------------- | ------------------------------------------------------------- |
| `serve` (`s`)   | Serve the notebook over MCP stdio. This is the server.        |
| `pull` (`p`)    | Write the current notebook into a directory and exit.         |
| `commit` (`c`)  | Publish the changes at a directory (`-m <message>`) and exit. |
| `login`         | Log in to a hosted account through the browser.               |
| `space`         | List the login's spaces, or set the default one (`space <n>`).|
| `logout`        | Revoke the stored login key and its tokens, and remove it.    |
| `version` (`v`) | Print `slivingdoc <semver>` and exit, touching nothing else.  |

## Direct use: pull and commit

`pull` and `commit` are the human mirror of the two MCP tools. They run
the same startup sequence as `serve` (the pinned engine check and the
S3 compatibility probe), perform one operation, print the candid
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
exits zero: the `OK` status token, the accepted remote generation, the
resolved notebook directory, one line per changed file with its insertion and deletion counts (a
zero-count side is omitted), and the totals trailer:

```text
OK  generation 18  /home/me/notes
  archive/old.md  -3
  notes/a.md  +1 -1
  notes/c.md  +2
3 files changed, 3 insertions(+), 4 deletions(-)
```

On a terminal a spinner on stderr shows the operation while it runs
(`Pulling /home/me/notes · space notes · 1.2s`) and the report gains
colour and marks ([tui.md](./tui.md)); a pipe gets exactly the text
above.

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
is a real terminal, where the report also gains marks (✓ before `OK`, ✗
before the code, `→` in place of `next:`) and the totals, `retryable:`
and `recovery:` lines are dimmed ([tui.md](./tui.md)); piped or
redirected output stays the plain text above. Any non-empty `NO_COLOR`
disables the colour and the marks even on a terminal.

A missing message, or more than one path, exits nonzero before any native
or network dependency is touched.

## Configuration

`serve`, `pull`, and `commit` read the same flags and environment
variables. Flags override environment variables, and the environment
overrides defaults.
`--bucket` is required for S3. `--space` (and `SLIVINGDOC_SPACE`) is the
same setting under its hosted name; passing both spellings with different
values refuses startup. In hosted mode the space defaults, with
`SLIVINGDOC_TOKEN`, to the token's own space, and otherwise to the stored
login's default space when `--storage` is not `s3`; a space that is given
must be the token's space, or one the login reaches. `-h` on any of the three
prints the same reference. A non-empty `SLIVINGDOC_TOKEN`, or a stored
login, switches to [hosted storage](#hosted-storage), which
changes the meaning of `--bucket`/`--space` and `--endpoint` as noted; `--storage`
makes the choice explicit ([Choosing the storage](#choosing-the-storage)).

| Function                          | Flag                     | Environment variable              | Default                      |
| --------------------------------- | ------------------------ | --------------------------------- | ---------------------------- |
| Storage backend                   | `--storage`              | `SLIVINGDOC_STORAGE`              | `auto`                       |
| Bucket or hosted space            | `--bucket` or `--space`  | `SLIVINGDOC_BUCKET` or `SLIVINGDOC_SPACE` | S3: none (required); hosted: the token's (`SLIVINGDOC_TOKEN`), else the login's default space |
| Object prefix                     | `--prefix`               | `SLIVINGDOC_PREFIX`               | `slivingdoc`                 |
| S3 region                         | `--region`               | `AWS_REGION`                      | `us-east-1`                  |
| S3 endpoint                       | `--endpoint`             | `AWS_ENDPOINT_URL_S3`             | empty (AWS resolution)       |
| S3 path-style access              | `--path-style`           | `SLIVINGDOC_PATH_STYLE`           | `false`                      |
| Hosted API token                  | none                     | `SLIVINGDOC_TOKEN`                | empty (S3 mode)              |
| Hosted API endpoint               | `--endpoint`             | `SLIVINGDOC_ENDPOINT`             | `https://api.slivingdoc.dev` |
| Credentials directory             | none                     | `SLIVINGDOC_CONFIG_DIR`           | `<user-config-dir>/slivingdoc` |
| Workspace root                    | `--workspace-root`       | `SLIVINGDOC_WORKSPACE_ROOT`       | session dir / working dir    |
| Private state root                | `--private-root`         | `SLIVINGDOC_PRIVATE_ROOT`         | session dir / user cache     |
| CAS retry limit                   | `--commit-retries`       | `SLIVINGDOC_COMMIT_RETRIES`       | `8` (0..100)                 |
| Checkpoint pack count             | `--checkpoint-packs`     | `SLIVINGDOC_CHECKPOINT_PACKS`     | `256` (minimum 1)            |
| Retained checkpoints              | `--retained-checkpoints` | `SLIVINGDOC_RETAINED_CHECKPOINTS` | `1` (0..64)                  |
| Read-only paths                   | `--read-only-paths`      | `SLIVINGDOC_READ_ONLY_PATHS`      | empty (no read-only path)    |
| Writable paths                    | `--writable-paths`       | `SLIVINGDOC_WRITABLE_PATHS`       | empty (no confinement)       |
| Ignore patterns                   | `--ignore`               | `SLIVINGDOC_IGNORE`               | empty (built-in names only)  |
| Log levels                        | `--log-level`            | `LOG_LEVEL`                       | `info`                       |
| Log timestamps                    | `--log-timestamp`        | `SLIVINGDOC_LOG_TIMESTAMP`        | `true`                       |

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
directory. The whole session directory is removed at shutdown; the durable
notebook is the bucket, so nothing of value is in it. A process killed
outright leaves the directory for the operating system to reap; no later
process reuses it.

Configuring either root turns the default off, and neither root is removed
at shutdown. Use that when humans and agents share one directory, or when
you want the notebook to survive a server restart on disk. `pull` and
`commit` never take a session directory: they default to the working
directory, which you can still open after the process exits.

### The shared pack cache

Downloaded pack bytes are cached in one durable directory per notebook,
shared by every workspace and every process on the machine:

```text
<user-cache-dir>/slivingdoc/pack-cache/<bucket>-<prefix>-<digest>/
```

Every server addressing the same endpoint, region, bucket, and prefix (and, in
hosted mode, the same space id) computes the same directory from its own configuration, so agents share downloads with no
coordination: the first cold pull populates the directory and later pulls by
any agent read from it. Entries are keyed by SHA-256 and re-verified against
the authoritative manifest on every read, so a corrupt or foreign entry is
discarded and re-downloaded, never trusted. Only pack bytes and the store
compatibility proof (below) are shared; each workspace keeps its own
private repository, baseline, and locks.

The same directory records the outcome of the startup compatibility probe
in `probe-ok.json`. The probe is nine dependent round trips proving a
property of the endpoint, so a one-shot `pull` or `commit` against a
distant bucket pays seconds for it on every start. A process reuses a
record written by the same slivingdoc version for the same endpoint,
region, bucket, prefix, and addressing mode within the last 24 hours and
starts without probing; an absent, corrupt, foreign, or expired record
simply means the probe runs and rewrites it. Remove the file to force a
probe on the next start. The record does not bind credentials: a
credential that stopped working is refused by the first request, not at
startup.

Reusing the record means the endpoint's conditional-write behavior is taken
on trust for up to 24 hours. An endpoint that silently stops honoring
`If-Match` inside that window is no longer refused at startup, and a
publication can be lost instead. A long-lived `serve` process always had
that exposure, and without a bound, because it probes once and then runs
for as long as the host keeps it alive; the record gives the one-shot
commands the same property rather than a new one. Delete `probe-ok.json`
to force a fresh proof on the next start.

There is no switch. Two hosts fall back to a private `pack-cache/` inside
each workspace's private state, with a debug log line: one without a
resolvable user cache directory (no `HOME` and no `XDG_CACHE_HOME`), and a
workspace root that contains the user cache directory, where the shared
directory would become notebook content.

Writing into the cache is best-effort: a read-only or full cache directory
logs a warning and the operation continues. That makes a pre-populated
read-only cache (for example baked into a container image) work as-is.

Two operational notes. Nothing prunes the directory: it grows with every
publication until a checkpoint makes most entries unreferenced, and the
names make manual cleanup easy: remove a notebook's directory when you are
done with it, and the next pull simply re-downloads. And the directory is
trusted at the level of the user who owns it: pack entries are verified on
read, but the proof record is not, so on a host where several principals
share `XDG_CACHE_HOME`, point it somewhere private.

## Read-only paths

`--read-only-paths` (environment `SLIVINGDOC_READ_ONLY_PATHS`) marks a
comma-separated set of notebook-relative paths that one process's commits
may never change, while a process without the flag keeps full write
access to the same notebook. An entry protects itself and everything
below it: `docs` covers a file named `docs` and every path under
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

The next pull by any agent picks up that change with no conflict: the
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
process bypasses the setting, exactly like an operator's existing sftp
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
network dependency loads, the same point at which an invalid entry in
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
between them (the read-only sentence says "where the two sets nest, the
longest matching entry decides" rather than "write elsewhere", which a
non-empty writable set makes false), so an agent is never told to write
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

slivingdoc has no authentication layer of its own for S3 (in [hosted
storage](#hosted-storage) the only credential is `SLIVINGDOC_TOKEN` or a
stored login, and nothing in this section applies). `serve`, `pull`, and
`commit` all build the S3 client the same way, and credentials come
from the AWS SDK default credential chain, resolved by the SDK at
startup:

1. Environment variables: `AWS_ACCESS_KEY_ID`,
   `AWS_SECRET_ACCESS_KEY` (plus `AWS_SESSION_TOKEN`).
2. The shared config and credentials files (`~/.aws/credentials`,
   `~/.aws/config`), honoring `AWS_PROFILE`.
3. Ambient identity: SSO sessions, ECS/EKS task roles, and the EC2
   instance metadata service.

slivingdoc's own flags shape _where_ the client points (`--bucket`/`--space`,
`--prefix`, `--region`, `--endpoint`), never _who it is_. No flag
carries a credential, and a `--endpoint` URL with user information is
refused, so a secret can never echo into a diagnostic.

The region is always explicit. slivingdoc passes `--region` (or
`AWS_REGION`, default `us-east-1`) to the SDK on every run, so
`AWS_DEFAULT_REGION` and a profile's `region` setting are ignored; set
`--region` or `AWS_REGION` when the bucket lives elsewhere.

There are three ways to deliver credentials, and the choice is a
deployment decision:

- **Inherit.** The process inherits the environment of whatever
  launched it. A shell with an exported profile or an active SSO
  session needs nothing else; this covers `slivingdoc pull` and
  `commit` run by hand, and a `serve` whose MCP host was started from
  that shell.
- **Inject.** Most MCP hosts accept an `env` block per server (see the
  example below). Use it when the host is not launched from a
  credentialed shell (a GUI app, a service manager) or to point at a
  local S3-compatible store (such as SeaweedFS). Prefer injecting
  `AWS_PROFILE` over pasting static keys: host configuration files tend
  to be synced and backed up, while a profile keeps the secret in
  `~/.aws/credentials`.
- **Ambient.** On EC2, ECS, or EKS, an attached role satisfies the
  chain with no configuration at all. This is the cleanest server
  deployment.

Credentials stay inside the slivingdoc process. They never cross the
MCP protocol (the client sees only `notes_pull`, `notes_commit`, and
their result envelopes), and the redaction layer keeps key material out
of every error and log line as defense in depth.

One consequence of the one-shot commands: `serve` resolves the chain
once and holds the session, while every `pull` or `commit` invocation
resolves it fresh. With short-lived STS or SSO credentials each
invocation needs a currently valid session. An expired login surfaces
as a redacted startup refusal (the compatibility probe fails, reported
as a refused credential), not a mid-operation error.

## S3 requirements

The bucket must exist. slivingdoc does not create or configure it.

The server needs these permissions:

- On the objects (`arn:...:bucket/*`): `s3:GetObject`, `s3:PutObject`,
  `s3:DeleteObject`, `s3:AbortMultipartUpload`, and
  `s3:ListMultipartUploadParts`. IAM has no separate action for creating,
  uploading a part of, or completing a multipart upload: `s3:PutObject`
  authorizes all three.
- On the bucket (`arn:...:bucket`): `s3:ListBucket` and
  `s3:ListBucketMultipartUploads`.

The reusable Terraform module in [`terraform/`](../terraform/) provisions
the bucket and a least-privilege IAM user with access keys, and grants
that user exactly this policy.

A custom S3-compatible service is configured with an absolute `http` or
`https` `--endpoint`. The server always uses path-style addressing for
a custom endpoint. `--path-style` extends that to the default AWS
endpoint.

Before the first MCP call, the server runs a disposable compatibility
probe below the configured prefix. The probe proves that the store
enforces `If-None-Match: *` creation, `If-Match` replacement, and
read-after-write behavior, the three conditional-write guarantees the
publication protocol requires. A store that fails the probe is refused
at startup with the `INCOMPATIBLE_STORE` category. A probe the service
refuses for its credentials or bucket (`AccessDenied`,
`InvalidAccessKeyId`, `NoSuchBucket`, unresolved credentials) is not a
missing capability: it is reported as `S3 storage refused the credentials
or the bucket`, naming the service's reason, while the probe key and any
secret stay redacted. Bucket versioning is not required. The proof is recorded in
the shared pack cache and reused for 24 hours (see
[the shared pack cache](#the-shared-pack-cache)), so only the first
process of a store identity pays for the probe.

## Hosted storage

Instead of a bucket, the notebook can live in a space of the slivingdoc
hosted storage service ([slivingdoc.dev](https://slivingdoc.dev)). A
non-empty `SLIVINGDOC_TOKEN`, or a stored login
([Logging in](#logging-in)), selects it:

- Each token reaches exactly one space, so with `SLIVINGDOC_TOKEN`
  `--space` is optional: at
  startup the process asks the server which space the token reaches
  (`GET /v1/token`) and uses it. A `--space` that names a different
  space refuses startup, naming both; one that names the same space
  changes nothing. With a stored login, the space is `--space`, else
  `SLIVINGDOC_SPACE`, else the default space. When given, it is 1 to 63 lowercase letters, digits,
  and inner hyphens. `--prefix` still separates notebooks inside the
  space.
- The endpoint is `--endpoint`, else `SLIVINGDOC_ENDPOINT`, else
  `https://api.slivingdoc.dev` (with a stored login: the endpoint it was
  issued for). It must be `https`, except to a loopback
  address. `AWS_ENDPOINT_URL_S3`, `AWS_REGION`, and the AWS credential
  chain are not used, and `--path-style` is validated, then unused. Beside
  `SLIVINGDOC_TOKEN` a region, AWS credentials and the `~/.aws` files are
  simply ignored; `--region` counts only against a stored login used
  with a `--space` you named (see [Choosing the storage](#choosing-the-storage)),
  and the `--endpoint` flag, `AWS_ENDPOINT_URL` or `AWS_ENDPOINT_URL_S3`
  beside the token refuse startup under `--storage auto`.
- The token is read from the environment, or minted from the stored
  login, never from a flag; `SLIVINGDOC_TOKEN`
  must be printable ASCII (0x21 to 0x7E) without white space. It travels only in
  the `Authorization` header and never appears in a diagnostic, a log
  line, or a tool result.

Startup does not run the S3 probe. It asks the server to describe itself
(it must speak `slivingdoc-storage` version 1 and promise conditional
writes, else `INCOMPATIBLE_STORE`) and reads the space's usage with the
token, so a read-only token starts and can pull. A token the server does
not accept, or a space it was not granted, refuses startup with a
diagnostic that names `SLIVINGDOC_TOKEN` and the space setting you used
(`--space` when none), or, for a stored
login, says to run `slivingdoc space` or `slivingdoc login` again. A server too old to name a
token's space still works when `--space` is given.

Account limits and refusals surface as `STORAGE_FAILURE` with their own reason:
`STORAGE_FULL` (nothing was published, the edits stay, pulls keep
working; deleting notes and committing again can compact the space),
`REQUEST_LIMIT` (the monthly request allowance is used up; it resets on
the first of the month, UTC), `RATE_LIMITED` (retryable), `ACCESS_DENIED`
(a read-only token that commits, a revoked or ungranted token, a space
that no longer exists, an `--endpoint` that is not the storage API, or,
for S3, credentials or a bucket the service refuses), and `OBJECT_TOO_LARGE`.
All but `RATE_LIMITED` are `retryable: false` and ask for the operator.
The server's own explanation follows `The storage says:` in the message.
[hosted-mode.md](./hosted-mode.md) has the details.

### Logging in

A person does not need to copy a token: `slivingdoc login` logs in to
the hosted account through the browser and stores an account key.

```text
$ slivingdoc login
To log in, open this page and approve the code BCDF-GHJK:
  https://www.slivingdoc.dev/cli/login#BCDF-GHJK
(or open https://www.slivingdoc.dev/cli/login and enter the code)
Only approve it if you started this login in your own terminal.
Waiting for approval (the code expires at 12:10:00 UTC)...
Approved by ada@example.com (read and write) until 2026-12-26 12:00 UTC.
  Storage endpoint: https://api.slivingdoc.dev
  Site: https://www.slivingdoc.dev
  Spaces:
    notes (read and write), owned by ada@example.com
Store this login? [y/N] y
The default space is "notes"; 'slivingdoc space <name>' changes it.
Logged in as ada@example.com (read and write) until 2026-12-26 12:00 UTC; default space "notes"
```

That is the plain form a script sees. On a terminal the same facts come
styled ([tui.md](./tui.md)): a `◆ slivingdoc login` header, the page and
the code as labelled rows, the warning in amber, a spinner counting down
to the code's expiry instead of the waiting line, the spaces as a table
with their owners, and a ✓ result line. When the login reaches several
spaces and none becomes the default, a terminal then offers a picker:
type the number of the default space, or `q` to choose later.

The prompt goes to stderr and the result line to stdout. The page opens in
a browser unless `--no-browser` is given or no browser starts (SSH,
headless machines); then open it yourself, on any device. slivingdoc
builds the page address itself from the site; the code travels after the
`#`, so it never reaches a server log. On the page you sign in, pick the
access (`--read-only` asks for read only), compare the code with the
terminal, and approve. The login is to the account, not to one space. The
code lives ten minutes at most.

Whoever enters a code first decides it, so before anything is stored the
login shows who approved it, the access, the storage endpoint, the site,
and every space the login reaches with its owner (`a team` for a space a
team owns). On a terminal it then
asks `Store this login? [y/N]`; any answer but `y`, or Ctrl-C, revokes the
new key and stores nothing (a second Ctrl-C while that revocation runs, up
to 10 seconds, is ignored). Without a terminal (a script) nothing is
asked, so nobody confirms who approved a first login; check the `Approved
by` line. A login that would replace a stored login another account
approved is refused and its key revoked, unless `--force` is given. A
replaced key is revoked only when the same account approved it;
otherwise it stays valid and the login says to revoke it on the site's
Tokens page if it is no longer needed.

The key is an account CLI key (90 days, label `CLI login on <client>`,
the client being this host's name, or plain `CLI login` when the host
name is unknown), visible and revocable on the site's Tokens page. It never reaches the
storage service: each `serve`, `pull` or `commit` trades it at the site
for a token of one space that lasts an hour, keeps that token in memory
only, and `serve` gets a new one before it expires. It is stored in
`<user-config-dir>/slivingdoc/credentials.json`
(`~/.config/slivingdoc/` on Linux, `~/Library/Application Support/slivingdoc/`
on macOS, `%AppData%\slivingdoc\` on Windows; `SLIVINGDOC_CONFIG_DIR`
names another directory), mode 0600, with one login per storage endpoint
and that endpoint's default space. Logging in again replaces the stored
key. A credentials file of another format version is refused by every
command that reads it, `login` included, and nothing rewrites it: an
older file says to remove it and run `slivingdoc login`; a newer
slivingdoc's file says to update slivingdoc and keep the file, whose keys
are still live. The file, and its
directory, must belong to you and must not be accessible to other users:
like ssh, slivingdoc refuses a symbolic link or anything but a regular
file, a file or directory another user owns, a file group or other can
read or write, and a directory they can write, and says which `chmod`
fixes the mode. `login` checks the directory even before the file exists;
every other command checks only an existing file. On Windows only the
link and regular-file checks apply, and the file relies on the
permissions of your user profile. `login`, `logout` and `space <name>`
change the file under a lock (`credentials.lock` beside it), so two of
them never lose each other's change.

**The default space.** When the login reaches exactly one space, it
becomes the default space. Otherwise the login lists them and says to run
`slivingdoc space <name>`; a default from an earlier login is kept while
the login still reaches it. `--space` (or `--bucket`) on `login` sets the
default directly, and the login must reach it: otherwise the key is
revoked and nothing is stored.

```text
$ slivingdoc space
* notes (read and write), owned by ada@example.com
  team (read only), owned by bob@example.com
$ slivingdoc space team
The default space is now team (read only), owned by bob@example.com
```

`slivingdoc space` lists the spaces the login reaches and marks the
default with `*`; when stdin, stdout and stderr are all a terminal it
shows them as a numbered picker with the default marked instead, and the
number you type becomes the default (`q` changes nothing). `slivingdoc space <name>` makes a listed space the
default; a name the login does not reach changes nothing. With logins for
several endpoints, `--endpoint` (or `SLIVINGDOC_ENDPOINT`) picks one.

`--site` (or `SLIVINGDOC_SITE`) names another site, such as a development
deployment, whose logins then talk to that site's own storage API; the
login says `Logging in through <site> (from --site)` before it sends
anything, and the result line then names the storage endpoint. The default
site's keys are only accepted for `https://api.slivingdoc.dev`. A login
through one site never replaces or revokes a login another site issued for
the same endpoint: log out of that one first (`slivingdoc logout --site
<site>`).

`slivingdoc logout` revokes every stored key at the site that issued it
(`--site` narrows it to one site's logins; on a terminal with several
logins and no `--site`, a picker asks which), which also revokes every token
minted from it, and removes the login and its default space. A key the
site reports as unknown (`401 invalid_token`) counts as revoked, and any
other failure keeps the login so you can retry. If another `login` stored a
newer key meanwhile, logout keeps it and says so. A key is sent only to
the site that issued it; a minted token only to the login's storage
endpoint.

Once logged in, `slivingdoc pull ~/notes`, `slivingdoc commit ~/notes -m
msg` and `slivingdoc serve` need neither `SLIVINGDOC_TOKEN` nor `--space`
when a default space is set; `--space` (or `SLIVINGDOC_SPACE`) picks
another space the login reaches. With no space and no default, startup is
refused and names `slivingdoc space <name>`. A stored login that has
expired refuses startup with `run 'slivingdoc login' again` (or pass
`--storage s3`); a space the site will not mint a token for (not granted,
suspended) refuses startup and suggests `slivingdoc space`. The key and
the default space are read once when a process starts, so restart `serve`
(your MCP host) after logging in again or changing the default. Every
process logs, at Info on stderr, which store it chose: `storage selected
backend=hosted endpoint=... space=... token=login` (or `env`, or
`backend=s3 ... token=none`; an S3 endpoint that comes from
`AWS_ENDPOINT_URL_S3` or `AWS_ENDPOINT_URL` is logged as scheme and host
with the variable's name, and none at all as `aws-default (SDK: env or
profile)`).

### Choosing the storage

`--storage` (`SLIVINGDOC_STORAGE`) is `auto` by default. In `auto`, these
count as configuring S3 on purpose ("S3 settings"), which matters for a
stored login: any of
`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`,
`AWS_PROFILE`, `AWS_REGION`, `AWS_DEFAULT_REGION`, `AWS_CONFIG_FILE`,
`AWS_SHARED_CREDENTIALS_FILE`, `AWS_ROLE_ARN`,
`AWS_WEB_IDENTITY_TOKEN_FILE`, `AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`,
`AWS_CONTAINER_CREDENTIALS_*` or `SLIVINGDOC_PATH_STYLE` set; the
`--region` or `--path-style` flag given; or an existing
`~/.aws/credentials` or `~/.aws/config`. Then, in order:

1. `SLIVINGDOC_TOKEN` is set: with the `--endpoint` flag,
   `AWS_ENDPOINT_URL` or `AWS_ENDPOINT_URL_S3` (you pointed S3 at a
   host), startup is refused, naming them, rather than guess the store;
   pass `--storage hosted` to use hosted storage (the token goes only to
   the hosted endpoint; hosted mode never reads the AWS endpoint
   variables) or `--storage s3`. Otherwise
   hosted storage with that token, whatever other S3 settings exist: a
   region, AWS credentials and `~/.aws` files are not read in hosted mode. It
   wins over a stored login and never reads the credentials file, so a
   read-only token in one MCP entry and a login in another can sit side by
   side.
2. A login is stored and no space is given: hosted storage with the
   login's default space, whatever S3 settings exist (S3 would have no
   bucket); with no default space, startup is refused, naming
   `slivingdoc space <name>`.
3. A login is stored and the space is given (`--space`, `--bucket`,
   `SLIVINGDOC_SPACE` or `SLIVINGDOC_BUCKET`): with S3 settings, startup
   is refused, naming them; pass `--storage hosted` or `--storage s3`.
   Otherwise hosted storage with the login.
4. Otherwise: S3.

`--storage s3` uses the AWS credential chain only; `SLIVINGDOC_TOKEN` and
stored logins are ignored, so a token can never reach an S3 endpoint.
`--storage hosted` uses `SLIVINGDOC_TOKEN`, else the stored login, and
refuses to start without either; it never looks at S3 settings.

A stored login is only used for the storage endpoint it was issued for. An
explicit `--endpoint` or `SLIVINGDOC_ENDPOINT` that differs means the login
does not apply: S3 under `auto` (and then its default space is not used as
the bucket), a refusal under `--storage hosted`. With logins for several
endpoints, pass `--endpoint` to choose. A credentials file that cannot be read
refuses startup with its fix (fix or remove the file, remove an older
version and log in again, or update slivingdoc for a newer one), or pass
`--storage s3`.
[login.md](./login.md) has the details and its threat model.

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
store such as SeaweedFS. For [hosted storage](#hosted-storage), the
`env` block carries `SLIVINGDOC_TOKEN` instead, and `--space` can be
left out: the token names its space. After [`slivingdoc login`](#logging-in)
neither is needed: `"args": ["-y", "slivingdoc", "serve"]` with no `env`
block uses the stored login and its default space, and `"--space",
"notes"` picks another space the login reaches.

Stdout carries only protocol messages; logs go to stderr. The host and
the server share the visible directory: agents and humans edit files
there, and the server scans them at each call. A human edit made with
any editor is published by the next `notes_commit` for that path, or
directly with `slivingdoc commit [path] -m <message>`. This sharing needs
`--workspace-root`: a session directory is private to the server process.

## Logging

Logging is configured by the environment, which applies to every command
and works before flags are parsed. `serve`, `pull`, and `commit` also
take `--log-level` and `--log-timestamp`, which override the environment
once the flags resolve; the few records emitted before that point (command
routing, a level-fallback warning) follow `LOG_LEVEL` and `NO_COLOR` and
always carry `time=`. Router lines such as a startup refusal are printed
separately, as one untimestamped `error: ...` line (`✗ ...` on a
terminal). Records
are structured `key=value` text on stderr. Each record carries a
timestamp (unless `--log-timestamp=false`), a level, and the module that
emitted it.

| Variable    | Effect                                              |
| ----------- | --------------------------------------------------- |
| `LOG_LEVEL` | Per-module levels. A bare level is the default.     |
| `SLIVINGDOC_LOG_TIMESTAMP` | `false` removes the `time=` field, for hosts that stamp log lines themselves. Like `--log-timestamp`, it applies only once `serve`, `pull`, or `commit` resolves its configuration; router records (including those of `version`) always carry `time=`. Startup refusals are not log records: the router prints them as one untimestamped `error: <diagnostic>` line. |
| `NO_COLOR`  | Any non-empty value disables colour: of log levels and of the styled terminal output (colours, marks, spinners, the home screen; [tui.md](./tui.md)). Pickers still appear on a terminal. |

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

`DEBUG_PERF` captures performance profiles across one whole command
(startup, the operation, and shutdown) for finding where a slow `pull`
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
- Every file under the notebook directory is notebook state unless it is
  ignored: a stray text file (such as an editor backup `foo~`) is
  published, and a stray binary, symlink, or special file refuses the whole
  pull or commit as `INVALID_REQUEST`/`INVALID_CONTENT` until it is deleted
  or ignored.
- Ignored entries are never read, published, written or removed. The
  built-in names are `.DS_Store`, `._*`, `.AppleDouble`, `.Spotlight-V100`,
  `.Trashes`, `.fseventsd`, `.TemporaryItems`, `Thumbs.db`, `desktop.ini`,
  `*.swp`, `*.swo`, `.git` and `*.slivingdoc-tmp-*`. `--ignore` (`SLIVINGDOC_IGNORE`) adds
  comma-separated patterns: a name such as `*.log` matches at any depth, a
  path such as `private/scratch` matches from the notebook directory and
  everything below it. A file the notebook already holds under an ignored
  name stays as the notebook has it and is not written here; local edits
  to it are not published.
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
- The first pull into a directory needs that directory to be empty, or to
  hold only files the notebook already has with identical content. The
  exception is an empty notebook: the first pull into a directory of
  existing files then succeeds, and the next commit publishes them as the
  notebook's first state. Any other first pull is refused as
  `INVALID_REQUEST`/`DIRECTORY_NOT_EMPTY`, naming each file that is not
  in the notebook or differs from it, before the directory or the
  pull state changes (the notebook may already be downloaded into the
  private cache): pull into an empty directory, or move the files away
  and pull again. A file under a read-only or otherwise protected path
  only needs to exist in the notebook, whose content the pull restores;
  one the notebook lacks is refused, even by an empty notebook, because
  the pull would delete it and this process could never publish it.

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
marker lines, keep the text you want. Then call `notes_commit` (or run
`slivingdoc commit`) again.
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

slivingdoc guarantees:

- accepted state is indexed by one authoritative manifest (`current`);
- a failed concurrent publication cannot silently overwrite accepted state;
- accepted packs are immutable;
- a successful commit is durably referenced by `current`;
- merge conflicts do not advance remote state;
- checkpoint and cleanup failures do not corrupt current state.

Deployment owners decide:

- bucket versioning and noncurrent-version retention;
- replication and object lock;
- backup export and recovery procedures;
- the storage lifecycle policy.

These S3 features complement slivingdoc. They are not hidden
prerequisites of the synchronization algorithm; choose them according to
your own recovery requirements.

## Related

- [cli.md](./cli.md), [config.md](./config.md), [mcp-server.md](./mcp-server.md): the code behind each section above.
- [security.md](./security.md): the trust model behind credentials and paths.
- [storage.md](./storage.md), [s3store.md](./s3store.md): what the bucket holds and how it is addressed.
- [hosted-mode.md](./hosted-mode.md): the hosted storage adapter, its limits, and compaction of a full space.
- [conflicts.md](./conflicts.md), [checkpoints.md](./checkpoints.md): conflict and checkpoint mechanics.
- [build.md](./build.md), [releasing.md](./releasing.md): installing and shipping binaries.
