Test coverage: 83.2% 😍👌

[![slivingdoc banner](img/banner.svg)](https://slivingdoc.dev)

<div align="center">
  <p>Distributed durable notebook built for high scale agents.</p>
</div>

## Features

- **Gitlike semantics:** `slivingdoc` uses terminology we (and agents) all know, designed for ease of use
- **Automatic conflict resolution:** commit without fear, trust that all conflicts must be resolved before being accepted
- **High speed processing:** the solution is quite simple conceptually, allowing for very high scale and parallelism
- **Plug-and-play:** setup the bucket, point at it, and start syncing notes!

[`docs/slivingdoc-v1.md`](docs/slivingdoc-v1.md) is the full accepted
contract behind these guarantees.

## Get started

Add it to your agentic harness:

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
      ],
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
No S3 account yet? [`examples/minio/`](examples/minio/) runs a local
MinIO container with a step-by-step walkthrough. You can also download
a native binary directly from the
[GitHub release](https://github.com/baalimago/slivingdoc/releases)
(`slivingdoc-v<semver>-<os>-<arch>`) and run it in place.

Supported platforms: Linux (amd64, arm64), macOS (amd64, arm64), and
Windows (amd64).

## How it works

The server exposes two MCP tools over stdio:

| Tool           | Inputs            | Success result |
| -------------- | ----------------- | -------------- |
| `notes_pull`   | `path`            | `OK`           |
| `notes_commit` | `path`, `message` | `OK`           |

`notes_pull` writes the current notebook into your directory.
`notes_commit` publishes your changes and incorporates concurrent
non-conflicting changes. Between calls there is no protocol at all —
agents edit the files with the tools they already have, and humans can
write in the same directory with any editor. The next commit carries
their changes too.

Humans can also drive both operations directly, without an MCP host.
A relative path resolves against the working directory:

```text
slivingdoc pull notes
# edit UTF-8 text files under notes/
slivingdoc commit notes -m "meeting summary"
```

Success prints the unified result report: the `OK` status, the accepted
remote generation, per-file insertion and deletion counts, and a totals
trailer. A domain error exits nonzero and prints a candid report: the
error category, the retryable verdict, and every conflicted file with its
line ranges. Colour appears only on a real terminal and is disabled by
any non-empty `NO_COLOR`.

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
variables. `--bucket` is required. The most common flags:

| Flag               | Environment                 | Default             |
| ------------------ | --------------------------- | ------------------- |
| `--bucket`         | `SLIVINGDOC_BUCKET`         | — (required)        |
| `--workspace-root` | `SLIVINGDOC_WORKSPACE_ROOT` | startup working dir |
| `--endpoint`       | `AWS_ENDPOINT_URL_S3`       | AWS resolution      |
| `--region`         | `AWS_REGION`                | `us-east-1`         |

`slivingdoc serve -h` prints the full reference, and
[`docs/running.md`](docs/running.md) covers everything an operator
needs: all flags, the exact S3 permissions, logging (`LOG_LEVEL` on
stderr), the notebook rules, conflict recovery, and checkpoint
retention. The Terraform module in [`terraform/`](terraform/)
provisions a bucket and a least-privilege IAM user for one notebook.

## Development

- [`AGENTS.md`](AGENTS.md) — the developer and agent guide: package
  map, operation flows, conventions, and the QA gates.
- [`docs/slivingdoc-v1.md`](docs/slivingdoc-v1.md) — the accepted
  architecture contract.
- [`docs/build.md`](docs/build.md) — the native build, from pinned
  libgit2 source to dependency inspection.
- [`docs/testing.md`](docs/testing.md) — the test commands, the test
  layers, and the no-live-AWS rule.
- [`docs/releasing.md`](docs/releasing.md) — release artifacts, npm
  trusted publishing, and `make release`.

```bash
make qa #lint plus the full Go and npm test suites
```

## License

MIT — see [LICENSE](LICENSE). Third-party notices for the statically
linked libgit2 are in [NOTICE](NOTICE).

<sub>Not affiliated with Paris Hilton.</sub>
