Test coverage: 88.0% 😍👌

[![slivingdoc banner](img/banner.svg)](https://slivingdoc.dev)

<div align="center">
  <p><strong>Shared notes for your agents.</strong></p>
  <p>
    Durable context that outlives every session: plain text files that
    fleets of agents (and their humans) pull and commit at the same time,
    with Git-style merges instead of overwrites.
    Store them durably in your own S3-compatible bucket, or let
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
have, and makes writing to them at the same time safe. What one session
learns is there for the next, whichever agent or machine runs it: agentic
durable context, kept in storage instead of a context window.

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

On your own machine, `npx -y slivingdoc login` can stand in for the token:
it stores an account login that reaches the spaces your account can use,
with a default space for commands that name none (see
[Hosted storage](#hosted-storage)).

To bring in colleagues, make an invite link on your space's Members page
at slivingdoc.dev, with Read or Read and write access. Each colleague opens
the link, signs in, presses Join, makes their own token for the space (or
runs `slivingdoc login`), and pulls into a new or empty folder. Everyone
pulls the same notes, and those with Read and write access commit to them. Their requests and storage count
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
then log in to your account from your terminal:

```sh
slivingdoc login      # or: npx -y slivingdoc login
```

It prints a code and opens an approval page in your browser (`--no-browser`
just prints the page, for SSH and headless machines). Sign in, check that
the code matches, pick the access, and approve. Whoever enters a code
first decides it, so the terminal then shows who approved it, the access,
the storage endpoint and every space the login reaches with its owner,
and asks `Store this login? [y/N]`; answer `y` only if that was you. An
account key is stored in your user configuration directory
(`~/.config/slivingdoc/credentials.json` on Linux; `SLIVINGDOC_CONFIG_DIR`
moves it). It never reaches the storage service: each process trades it
for a token of one space that lasts an hour and is kept in memory only.

When your account reaches one space, it becomes the default space.
Otherwise choose one (or pass `--space` to `login`):

```sh
slivingdoc space              # list the spaces; * marks the default
slivingdoc space my-space     # make my-space the default
```

With a default space the MCP host needs neither a token nor a bucket:

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

For another space your login reaches, name it and the store:
`"serve", "--storage", "hosted", "--space", "other-space"` (`--space`
alone also works when nothing on the machine configures S3). Logging in
again replaces the stored key and revokes the old one when you approved
both. `slivingdoc logout` revokes the key and every token made from it.
Restart the MCP host after logging in again or changing the default
space.

For CI, or instead of logging in, give slivingdoc an API token. Each token
reaches exactly one space, so the token is all it needs:

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
`SLIVINGDOC_TOKEN` wins over a stored login and alone is enough: with no
bucket it uses the token's own space, and a stored login plays no part.
A bucket you name must be the token's space, or startup is refused. slivingdoc refuses to guess
between S3 and hosted storage: a token next to a setting that names an S3
host (an `--endpoint` flag, `AWS_ENDPOINT_URL` or `AWS_ENDPOINT_URL_S3`;
a region, AWS credentials and `~/.aws` files are fine), and a login for a
space you named next to S3 settings (an AWS variable,
`~/.aws/credentials` or `~/.aws/config`, `--region`, `--path-style`),
refuse to start until you pass
`--storage hosted` or `--storage s3` (`--storage s3` never sends a token
anywhere). Each process logs which store and token source it chose.

**Upgrading an S3 setup:** a login changes only a command line or MCP entry
that runs in the default `--storage auto`, sets no `SLIVINGDOC_TOKEN`, and
either names no bucket (the login's default space is then used) or names
a bucket (`--bucket`, `--space`, `SLIVINGDOC_BUCKET` or `SLIVINGDOC_SPACE`)
on a
machine with no S3 settings at all, and either sets no endpoint or sets the
endpoint the login was issued for. Such an entry now uses hosted storage;
an entry that names the space as its bucket next to S3 settings refuses to
start instead, as does one whose login has expired. An entry that sets its
own S3 endpoint stays on S3. Add `--storage s3` (or `SLIVINGDOC_STORAGE=s3`)
to keep an entry on S3 whatever is stored. A hosted entry that sets
`SLIVINGDOC_TOKEN` next to `AWS_ENDPOINT_URL` or `AWS_ENDPOINT_URL_S3`, or
passes `--endpoint` as a flag, now needs `--storage hosted`.

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
variables. `--bucket` is required, unless you are logged in (it then
defaults to your login's default space) or give a hosted token (which names its
own space). `--space` and `SLIVINGDOC_SPACE` are the same setting under
its hosted name; both spellings with different values are refused. The
most common flags:

| Flag               | Environment                 | Default             |
| ------------------ | --------------------------- | ------------------- |
| `--bucket`         | `SLIVINGDOC_BUCKET`         | — (required)[^3]    |
| `--space`          | `SLIVINGDOC_SPACE`          | same as `--bucket`  |
| `--workspace-root` | `SLIVINGDOC_WORKSPACE_ROOT` | temporary dir[^1]   |
| `--endpoint`       | `AWS_ENDPOINT_URL_S3`[^2]   | AWS resolution      |
| `--region`         | `AWS_REGION`                | `us-east-1`         |
| `--storage`        | `SLIVINGDOC_STORAGE`        | `auto`[^3]          |
| (environment only) | `SLIVINGDOC_TOKEN`          | empty (S3 mode)     |

[^1]: `serve` with no configured root takes a per-process temporary
    notebook directory and removes it at shutdown; the notes themselves live
    in the bucket. `pull` and `commit` default to the working directory.

[^2]: In hosted mode `--endpoint` names the hosted API instead:
    `SLIVINGDOC_ENDPOINT`, default `https://api.slivingdoc.dev`. Under the
    default `--storage auto`, an `--endpoint` flag beside `SLIVINGDOC_TOKEN`
    refuses to start; pass `--storage hosted` to mean the hosted API. A
    stored login always uses the endpoint it was issued for.

[^3]: `auto` uses `SLIVINGDOC_TOKEN`, else a `slivingdoc login`, else
    S3, and refuses when S3 settings make that a guess (see
    [`architecture/login.md`](architecture/login.md)); `hosted` and `s3`
    force the choice. With a hosted token `--space` is optional and
    defaults to the one space the token reaches; a `--space` naming
    another space is refused. With a login, `--space` defaults to the default space set by
    `slivingdoc space <name>`.

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
