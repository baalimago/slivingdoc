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

It works without agents too. Colleagues, or your own machines, can share
a folder of UTF-8 text files with two commands, `slivingdoc pull` and
`slivingdoc commit`: like a simpler Git, with no repository to set up, no
staging and no branches.

## Features

- **Merge-safe concurrent writes:** non-conflicting changes merge;
  overlapping edits return a conflict instead of being overwritten
- **Plain files:** UTF-8 text in a directory, readable and editable with
  any editor; no database, no embeddings
- **Two operations:** `notes_pull` and `notes_commit` over MCP, and the same
  `pull` and `commit` from the command line, so people share the files too
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

Or, from the command line, share notes with colleagues:

```sh
export SLIVINGDOC_TOKEN=<your-api-token>
npx -y slivingdoc pull notes
echo "hello from $(hostname)" > notes/hello.md
npx -y slivingdoc commit notes -m "First note"
```

To bring in colleagues, make an invite link on your space's Members page
at slivingdoc.dev, with Read or Read and write access. Each colleague opens
the link, signs in, presses Join, makes their own token for the space, and
pulls into a new or empty folder. Everyone pulls the same notes, and those
with Read and write access commit to them. Their requests and storage count
against your plan. [Hosted storage](#hosted-storage) below has the details.

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

Each agent or person pulls the notebook into a plain directory, edits files
with any tool, and commits. slivingdoc merges concurrent commits the way Git
would, through a built-in libgit2, and returns `CONTENT_CONFLICT` with
conflict markers in the file when edits overlap. The website has the
details:
[how pull and commit work](https://slivingdoc.dev/docs/concepts/how-it-works/),
[using the CLI](https://slivingdoc.dev/docs/guides/cli/),
[connecting an MCP host](https://slivingdoc.dev/docs/guides/mcp-hosts/),
[the MCP tools](https://slivingdoc.dev/docs/reference/mcp-tools/),
[resolving conflicts](https://slivingdoc.dev/docs/guides/conflicts/),
[read-only and writable paths](https://slivingdoc.dev/docs/guides/path-policies/),
and
[sharing a directory with people](https://slivingdoc.dev/docs/guides/shared-directory/).
[`architecture/`](architecture/README.md) holds the exact contract.

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
