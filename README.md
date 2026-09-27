Test coverage: 85.7% 😍👌

[![slivingdoc banner](img/banner.svg)](https://slivingdoc.dev)

<div align="center">
  <p>Shared notes for people and agents.</p>
  <p>
    Pull and commit through MCP or the CLI. slivingdoc merges
    non-conflicting concurrent changes and stores the durable notebook in
    your S3-compatible bucket.
  </p>
</div>

## Features

- **Gitlike semantics:** `slivingdoc` uses terminology we (and agents) all know, designed for ease of use
- **Merge-safe concurrent writes:** non-conflicting changes merge; overlapping edits return a conflict instead of being overwritten
- **High speed processing:** the solution is quite simple conceptually, allowing for very high scale and parallelism
- **Plug-and-play:** setup the bucket, point at it, and start syncing notes!

[`architecture/`](architecture/README.md) documents the contract behind
these guarantees, one concern per file.

## Get started

Connect an MCP host or use the CLI directly:

```json
{
  "mcpServers": {
    "slivingdoc": {
      "command": "npx",
      "args": ["-y", "slivingdoc", "serve", "--bucket", "my-notes"],
      "env": {
        "AWS_ACCESS_KEY_ID": "<your-access-key-id>",
        "AWS_SECRET_ACCESS_KEY": "<your-secret-access-key>",
        "AWS_REGION": "us-east-1"
      }
    }
  }
}
```

The bucket must exist, and credentials come from the normal AWS chain.
No S3 account yet? [`examples/seaweedfs/`](examples/seaweedfs/) runs a local
SeaweedFS container with a step-by-step walkthrough. You can also download
a native binary directly from the
[GitHub release](https://github.com/baalimago/slivingdoc/releases)
(`slivingdoc-v<semver>-<os>-<arch>`) and run it in place.

Supported platforms: Linux (amd64, 32-bit ARMv7, arm64), macOS (amd64,
arm64), and Windows (amd64). The 32-bit Linux ARM artifact supports Raspberry
Pi OS armhf.

### Hosted storage

Rather not run a bucket? Create a space at [slivingdoc.dev](https://slivingdoc.dev),
then log in from your terminal:

```sh
slivingdoc login --bucket my-space      # or: npx -y slivingdoc login --bucket my-space
```

It prints a code and opens an approval page in your browser (`--no-browser`
just prints the page, for SSH and headless machines). Sign in, check that
the code matches, pick the space and the access, and approve. Whoever
enters a code first decides it, so the terminal then shows who approved it,
the space, its owner, the access and the storage endpoint, and asks
`Store this login? [y/N]`; answer `y` only if that was you. The token is
stored in your user configuration directory
(`~/.config/slivingdoc/credentials.json` on Linux; `SLIVINGDOC_CONFIG_DIR`
moves it), and your first login becomes the default login, so the MCP host
needs neither a token nor a bucket:

```json
{
  "mcpServers": {
    "slivingdoc": {
      "command": "npx",
      "args": ["-y", "slivingdoc", "serve"]
    }
  }
}
```

For another logged-in space, name it and the store:
`"serve", "--storage", "hosted", "--bucket", "other-space"` (`--bucket`
alone also works when nothing on the machine configures S3). A later
login keeps the default unless you pass `--default`. Logging in again for
the same space at the same storage endpoint replaces the stored token and
revokes the old one when you approved both. `slivingdoc logout` revokes
the stored token. Restart the MCP host after logging in again.

For CI, or instead of logging in, give slivingdoc an API token and the
space name:

```json
{
  "mcpServers": {
    "slivingdoc": {
      "command": "npx",
      "args": ["-y", "slivingdoc", "serve", "--bucket", "my-space"],
      "env": { "SLIVINGDOC_TOKEN": "<your-api-token>" }
    }
  }
}
```

The token replaces the AWS settings. Everything else works the same way.
`SLIVINGDOC_TOKEN` wins over a stored login. slivingdoc refuses to guess
between S3 and hosted storage: a token next to S3 settings (an AWS
variable, `~/.aws/credentials` or `~/.aws/config`, `--region`,
`--path-style`) or next to an `--endpoint` flag, and a login for a
`--bucket` you named next to S3 settings, refuse to start until you pass
`--storage hosted` or `--storage s3` (`--storage s3` never sends a token
anywhere). Each process logs which store and token source it chose.

**Upgrading an S3 setup:** a login changes only a command line or MCP entry
that runs in the default `--storage auto`, sets no `SLIVINGDOC_TOKEN`, and
either names no bucket (the login's default space is then used) or names
the logged-in space as its bucket (`--bucket` or `SLIVINGDOC_BUCKET`) on a
machine with no S3 settings at all, and either sets no endpoint or sets the
endpoint the login was issued for. Such an entry now uses hosted storage;
an entry that names the space as its bucket next to S3 settings refuses to
start instead, as does one whose login has expired. An entry that sets its
own S3 endpoint stays on S3. Add `--storage s3` (or `SLIVINGDOC_STORAGE=s3`)
to keep an entry on S3 whatever is stored. A hosted entry that sets
`SLIVINGDOC_TOKEN` next to AWS settings, or passes `--endpoint` as a flag,
now needs `--storage hosted`.

## How it works

The server exposes two MCP tools over stdio:

| Tool           | Inputs                       | Success result |
| -------------- | ---------------------------- | -------------- |
| `notes_pull`   | `path` (optional)            | `OK`           |
| `notes_commit` | `message`, `path` (optional) | `OK`           |

No path needed. Each server takes its own private notebook directory and
tells the agent where it is, in the server instructions and in every tool
result, so nothing has to be configured or coordinated between agents. Pass
`--workspace-root` instead when you want a fixed directory that humans and
agents share, and `path` then addresses any directory below it.

`notes_pull` writes the current notebook into your directory.
`notes_commit` publishes your changes and incorporates concurrent
non-conflicting changes. Between calls there is no protocol at all —
agents edit the files with the tools they already have, and humans can
write in the same directory with any editor. The next commit carries
their changes too.

Humans can also drive both operations directly, without an MCP host. The
path is optional and defaults to the working directory; a relative path
resolves against it:

```text
slivingdoc pull notes
# edit UTF-8 text files under notes/
slivingdoc commit notes -m "meeting summary"
```

Success prints the unified result report: the `OK` status, the accepted
remote generation, per-file insertion and deletion counts, a totals
trailer, and a `read-only: <entries>` trailer when the process has a
configured read-only set. A domain error exits nonzero and prints a
candid report: the status line (the error code, a middle dot, and the
`reason` token), the message, every affected file with its reason and
line ranges, a `next:` line naming the caller's next step, the retryable
verdict, and the same read-only trailer when configured. Colour appears
only on a real terminal and is disabled by any non-empty `NO_COLOR`.
MCP errors provide the same essential fields in their text item, including a
diagnostic ID for server-log correlation. An engine failure adds a plain-language
detail when the engine could name the cause; otherwise the cause stays in the
server log, which the diagnostic ID points at.

Pass `--read-only-paths docs,faq.md` (or `SLIVINGDOC_READ_ONLY_PATHS`) to
let a fleet of agents read those notebook paths but never change them: a
commit that touches one is refused and the files are reset, while a human
process started without the flag keeps full write access.

### The git part

Letting all agents write at once would work, but they would get overrun by race conditions.
So `slivingdoc` has built-in git via [libgit2](https://github.com/libgit2/libgit2) which effectively
does:

1. `git pull`
1. (potential conflict resolution locally)
1. `git add .`
1. `git commit -m "<agent message>"`
1. `git push`
1. (potential conflict resolution locally)

All of these git operations are handled locally within a private mirror of the notes directory
leaving a "streamlined" git sequence. This works due to two compromises, firstly that the local
notes directory is prone to be changed on `notes_pull`, precedence goes to the remote state, leaving
conflict markers. Secondly, the system only works for text (clean UTF-8).

## Configuration

`serve`, `pull`, and `commit` read the same flags and environment
variables. `--bucket` is required, unless you are logged in: then it
defaults to your login's space. The most common flags:

| Flag               | Environment                 | Default             |
| ------------------ | --------------------------- | ------------------- |
| `--bucket`         | `SLIVINGDOC_BUCKET`         | — (required)        |
| `--workspace-root` | `SLIVINGDOC_WORKSPACE_ROOT` | temporary dir[^1]   |
| `--endpoint`       | `AWS_ENDPOINT_URL_S3`[^2]   | AWS resolution      |
| `--region`         | `AWS_REGION`                | `us-east-1`         |
| `--storage`        | `SLIVINGDOC_STORAGE`        | `auto`[^3]          |
| (environment only) | `SLIVINGDOC_TOKEN`          | empty (S3 mode)     |

[^1]: `serve` with no configured root takes a per-process temporary
    notebook directory and removes it at shutdown; the notes themselves live
    in the bucket. `pull` and `commit` default to the working directory.

[^2]: With `SLIVINGDOC_TOKEN` set, `--endpoint` names the hosted API instead:
    `SLIVINGDOC_ENDPOINT`, default `https://api.slivingdoc.dev`. A stored
    login always uses the endpoint it was issued for.

[^3]: `auto` uses `SLIVINGDOC_TOKEN`, else a `slivingdoc login` for the
    space, else S3, and refuses when S3 settings make that a guess (see
    [`architecture/login.md`](architecture/login.md)); `hosted` and `s3`
    force the choice.

`slivingdoc serve -h` prints the full reference, and
[`architecture/running.md`](architecture/running.md) covers everything an operator
needs: all flags, the exact S3 permissions, logging (`LOG_LEVEL` on
stderr), the notebook rules, conflict recovery, and checkpoint
retention. The Terraform module in [`terraform/`](terraform/)
provisions a bucket and a least-privilege IAM user for one notebook.

## Development

- [`AGENTS.md`](AGENTS.md) — the developer and agent guide: package
  map, operation flows, conventions, and the QA gates.
- [`architecture/README.md`](architecture/README.md) — the architecture
  index: one doc per command and subsystem (pull, commit, storage, the Git
  engine, the MCP server, configuration, build, testing, releasing, and
  more).

```bash
make qa #lint plus the full Go and npm test suites
```

## License

MIT — see [LICENSE](LICENSE). Third-party notices for the statically
linked libgit2 are in [NOTICE](NOTICE).

<sub>Not affiliated with Paris Hilton.</sub>
