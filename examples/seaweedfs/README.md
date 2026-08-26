# Local evaluation with SeaweedFS

This example runs one pinned SeaweedFS container so you can evaluate
slivingdoc without an S3 account. The container is for manual evaluation
only: the automated test suites create their own S3-compatible containers
through testcontainers (`internal/tests3`) and never depend on this
environment.

## 1. Start SeaweedFS

```text
docker compose up -d
```

The S3 gateway listens on `http://localhost:8333`. The credentials are
fixed in `compose.yaml` (`slivingdoc` / `slivingdoc-local`). They exist
only inside this local container.

## 2. No bucket step

`my-notes` is created on first write: the startup probe performs that
write, so the bucket exists by the time the server accepts MCP calls.

## 3. Run the server

Export the credentials and start the server with the local endpoint. The
custom endpoint makes the server use path-style addressing, which is what
SeaweedFS expects:

```text
export AWS_ACCESS_KEY_ID=slivingdoc
export AWS_SECRET_ACCESS_KEY=slivingdoc-local

slivingdoc serve \
  --bucket my-notes \
  --endpoint http://localhost:8333 \
  --path-style \
  --workspace-root /tmp/notes \
  --private-root /tmp/slivingdoc-private
```

Replace `slivingdoc` with the full path to the native binary, or run it
through the launcher (`npx -y slivingdoc ...`). On startup, the server
runs the S3 compatibility probe below the configured prefix. A probe
failure stops the server before the first MCP call. The server then waits
on stdio for an MCP client. That is the expected state.

## 4. Connect an MCP host

Register the server in your MCP client configuration. The
`--workspace-root` is the only path that the tools can touch. The server
creates `/tmp/notes` on first use. Both root flags are optional — without
them the server takes its own temporary notebook directory and removes it
at shutdown — but this walkthrough names them so you can open the files
yourself while following along.

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
        "--endpoint",
        "http://localhost:8333",
        "--path-style",
        "--workspace-root",
        "/tmp/notes",
        "--private-root",
        "/tmp/slivingdoc-private"
      ],
      "env": {
        "AWS_ACCESS_KEY_ID": "slivingdoc",
        "AWS_SECRET_ACCESS_KEY": "slivingdoc-local"
      }
    }
  }
}
```

## 5. Work with the notebook

Call `notes_pull` with the path `/tmp/notes` (with no `--workspace-root`
you would omit the path, and the result would tell you the directory).
Create a text file there.
Then call `notes_commit` with a message. Pull again from a second agent
(or a second configured server) and edit concurrently. Non-conflicting
changes merge automatically. Conflicting changes appear with
`<<<<<<< local` / `=======` / `>>>>>>> remote` markers. Resolve the
markers with a text editor before you commit again.

The `current` manifest object below the `slivingdoc/` prefix in the bucket
durably references every accepted publication. You can inspect it with an
S3 client. The local private state under `/tmp/slivingdoc-private` is a
cache. Delete it and pull again to see it rebuilt from the bucket.

## 6. Startup noise

The container logs the non-fatal `no signing key found for STS service`
line on startup. It is safe to ignore for basic credentials.

## Stop

```text
docker compose down        # stop the container, keep the notes volume
docker compose down -v     # also delete the notes volume
```
