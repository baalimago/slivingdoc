Test coverage: 88.7% 😍👌

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
  <img src="img/demo.gif" width="800" alt="Two panes: you on the command line, an agent calling notes_pull and notes_commit over MCP. You change one line of plan.md and the agent another; both commit without pulling first and both land, merged. Then both change the same line: the agent commits first, and your commit returns CONTENT_CONFLICT with both versions in the file."">
</p>

## Features

- **Durable context:** automatically store agent's context
- **Merge-safe concurrent writes:** move fast with no fear
- **Edits outsourced:** slivingdoc concerns itself with storing, not writing
- **Simple interface:** `pull` and `commit`, the rest is handled magically
- **Your bucket or ours:** any S3-compatible bucket, or a hosted space you
  can share with invite links
- **Powerful yet simple ACL:** you decide where agents can read, where they can write
- **One binary:** "batteries included", as they say

The [docs](https://slivingdoc.dev/docs/) cover
[how it works](https://slivingdoc.dev/docs/concepts/how-it-works/),
[the CLI](https://slivingdoc.dev/docs/guides/cli/) and
[configuration](https://slivingdoc.dev/docs/reference/configuration/);
`npx -y slivingdoc serve -h` prints every flag.

## Get started

**Hosted.** Sign in at [slivingdoc.dev](https://slivingdoc.dev) with GitHub
or Google, create a token, and add the server to your agent:

```sh
claude mcp add slivingdoc \
  --env SLIVINGDOC_TOKEN=<your-api-token> \
  -- npx -y slivingdoc serve
```

Free for one space, 10 MB and 250,000 requests a month
([pricing](https://slivingdoc.dev/pricing/)).

**Self-hosted.** Point it at an existing S3-compatible bucket that
supports conditional writes.:

```sh
AWS_REGION=eu-north-1 AWS_ACCESS_KEY_ID=<key> AWS_SECRET_ACCESS_KEY=<key> npx -y slivingdoc serve --bucket my-notes
```

No bucket yet? [`examples/seaweedfs/`](examples/seaweedfs/) runs one
locally in a container, and [`terraform/`](terraform/) provisions one on AWS.

### As stdio MCP client

See [MCP hosts](https://slivingdoc.dev/docs/guides/mcp-hosts/) for how to configure the MCP
server in your agentic coder. The same system applies in the [openai agent SDK](https://openai.github.io/openai-agents-python/mcp/) (and any other LLM engine with MCP client support).

### As CLI

"Install" it via npx:

```sh
npx -y slivingdoc version
```

`npx` fetches and verifies the native binary on first run. You can also
install the matching binary with the checksummed setup script:

```sh
curl -fsSL https://raw.githubusercontent.com/baalimago/slivingdoc/setup.sh | sh
```

It installs into `$HOME/.local/bin` (or `/usr/local/bin` when run as root).

```sh
slivingdoc login
slivingdoc pull notes
echo "hello from $(hostname)" > notes/hello.md
slivingdoc commit notes -m "First note"
```

## Development

[`AGENTS.md`](AGENTS.md) is the developer guide and
[`architecture/`](architecture/README.md) holds the contract, one concern
per file.

```bash
make qa # lint plus the full Go and npm test suites
```

## License

MIT, see [LICENSE](LICENSE). Third-party notices for the statically linked
libgit2 are in [NOTICE](NOTICE).

<sub>Not affiliated with Paris Hilton.</sub>
