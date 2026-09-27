Test coverage: 85.9% 😍👌

[![slivingdoc banner](img/banner.svg)](https://slivingdoc.dev)

<div align="center">
  <p><strong>Shared notes for your agents.</strong></p>
  <p>
    Plain text files that many agents and people pull and commit at the
    same time, with Git-style merges instead of overwrites. Store them
    durably in your own S3-compatible bucket, or let
    <a href="https://slivingdoc.dev">slivingdoc.dev</a> host them, free to
    start.
  </p>
  <p>
    <a href="https://slivingdoc.dev">Website</a> ·
    <a href="https://slivingdoc.dev/docs/">Docs</a> ·
    <a href="https://slivingdoc.dev/pricing/">Pricing</a> ·
    <a href="architecture/README.md">Architecture</a>
  </p>
</div>

<p align="center">
  <img src="img/demo.gif" width="800" alt="Two agents pull the same notes and edit different sections of plan.md. The second commits without pulling the first one's change, and both commits succeed; a pull shows both edits. Then both add a different line in the same place, and the second commit returns CONTENT_CONFLICT with conflict markers in the file.">
</p>

## Why

You run several coding agents at once, such as a few Claude Code sessions
next to Codex, and you want them to share what they learn. A `NOTES.md`
committed next to the code ends in merge conflicts or lost edits, and a
memory service often keeps the notes in a store you can't open in an
editor. slivingdoc keeps
the notes as ordinary files that agents edit with the tools they already
have, and makes writing to them at the same time safe.

## Features

- **Merge-safe concurrent writes:** non-conflicting changes merge;
  overlapping edits return a conflict instead of being overwritten
- **Plain files:** UTF-8 text in a directory, readable and editable with
  any editor; no database, no embeddings
- **Two operations:** `notes_pull` and `notes_commit` over MCP, and the same
  `pull` and `commit` from the command line
- **Your bucket or ours:** any S3-compatible bucket that supports
  conditional writes, or a hosted space at
  [slivingdoc.dev](https://slivingdoc.dev)
- **Read-only and writable paths:** keep shared instructions out of reach
  of an agent's commits, or confine each agent to its own directory
- **One binary:** Git merge semantics through a statically linked libgit2;
  no Git executable, no daemon or database to run

[`architecture/`](architecture/README.md) documents the contract behind
these guarantees, one concern per file.

## Get started

### Hosted (quickest)

Sign in at [slivingdoc.dev](https://slivingdoc.dev) with GitHub or Google.
The welcome steps name your space, create a token and show the snippet for
your client (Claude Code, Claude Desktop, Cursor, Codex, or the CLI); later
tokens come from the Tokens page. For Claude Code:

```sh
claude mcp add slivingdoc \
  --env SLIVINGDOC_TOKEN=<your-api-token> \
  -- npx -y slivingdoc serve
```

The free tier holds one space, 10 MB of notes and 250,000 requests a
month; each extra 100 MB, 250,000 requests or space costs $1 a month
([pricing](https://slivingdoc.dev/pricing/)). The endpoint defaults to the
hosted service, so the site's `--endpoint` flag is optional.
[Hosted storage](#hosted-storage) below has the details.

### Self-hosted, on your own bucket

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
then give slivingdoc an API token. Each token reaches exactly one space, so
the token is all it needs:

```json
{
  "mcpServers": {
    "slivingdoc": {
      "command": "npx",
      "args": ["-y", "slivingdoc", "serve"],
      "env": { "SLIVINGDOC_TOKEN": "<your-api-token>" }
    }
  }
}
```

The token replaces the AWS settings and names the space; `--region` and
`--path-style` are ignored. Everything else works the same way.

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
variables. `--bucket` is required, except with a hosted token, which names
its own space. The most common flags:

| Flag               | Environment                 | Default             |
| ------------------ | --------------------------- | ------------------- |
| `--bucket`         | `SLIVINGDOC_BUCKET`         | — (required)[^3]    |
| `--workspace-root` | `SLIVINGDOC_WORKSPACE_ROOT` | temporary dir[^1]   |
| `--endpoint`       | `AWS_ENDPOINT_URL_S3`[^2]   | AWS resolution      |
| `--region`         | `AWS_REGION`                | `us-east-1`         |
| (environment only) | `SLIVINGDOC_TOKEN`          | empty (S3 mode)     |

[^1]: `serve` with no configured root takes a per-process temporary
    notebook directory and removes it at shutdown; the notes themselves live
    in the bucket. `pull` and `commit` default to the working directory.

[^2]: With `SLIVINGDOC_TOKEN` set, `--endpoint` names the hosted API instead:
    `SLIVINGDOC_ENDPOINT`, default `https://api.slivingdoc.dev`.
[^3]: With `SLIVINGDOC_TOKEN` set, `--bucket` is optional and defaults to the
    one space the token reaches; a `--bucket` naming another space is refused.

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
