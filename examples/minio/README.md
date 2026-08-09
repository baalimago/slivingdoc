# Local evaluation with MinIO

This example runs one pinned MinIO container so you can evaluate slivingdoc
without an S3 account. The container is for manual evaluation only: the
automated test suites create their own MinIO containers through
testcontainers and never depend on this environment.

## 1. Start MinIO

```text
docker compose up -d
```

The S3 API listens on `http://localhost:9000` and the web console on
`http://localhost:9001`. The root credentials are fixed in `compose.yaml`
(`slivingdoc` / `slivingdoc-local`); they exist only inside this local
container.

## 2. Create a bucket

Open the console at <http://localhost:9001>, sign in with the credentials
above, and create a bucket named `my-notes`. (If you have the `mc` client
installed, `mc alias set local http://localhost:9000 slivingdoc
slivingdoc-local && mc mb local/my-notes` does the same.)

## 3. Run the server

Export the credentials and start the server with the local endpoint. The
custom endpoint makes the server use path-style addressing, which is what
MinIO expects:

```text
export AWS_ACCESS_KEY_ID=slivingdoc
export AWS_SECRET_ACCESS_KEY=slivingdoc-local

slivingdoc \
  --bucket my-notes \
  --endpoint http://localhost:9000 \
  --path-style \
  --workspace-root /tmp/notes \
  --private-root /tmp/slivingdoc-private
```

Replace `slivingdoc` with the full path to the native binary, or run it
through the launcher (`npx -y slivingdoc ...`). On startup the server runs
the S3 compatibility probe below the configured prefix; any failure is
refused before the first MCP call. The server then waits on stdio for an
MCP client — that is the expected state.

## 4. Connect an MCP host

Register the server in your MCP client configuration. The `--workspace-root`
is the only path the tools may touch; `/tmp/notes` is created on first use.

```json
{
  "mcpServers": {
    "slivingdoc": {
      "command": "npx",
      "args": [
        "-y",
        "slivingdoc",
        "serve",
        "--bucket", "my-notes",
        "--endpoint", "http://localhost:9000",
        "--path-style",
        "--workspace-root", "/tmp/notes",
        "--private-root", "/tmp/slivingdoc-private"
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

Call `notes_pull` with the path `/tmp/notes`, create a text file there, and
call `notes_commit` with a message. Pull again from a second agent (or a
second configured server) and edit concurrently: non-conflicting changes
merge automatically, and conflicting changes appear with `<<<<<<< local`
/ `=======` / `>>>>>>> remote` markers that you resolve with a text editor
before committing again.

Every accepted publication is durably referenced by the `current` manifest
object below the `slivingdoc/` prefix in the bucket — you can inspect it
in the console. The local private state under `/tmp/slivingdoc-private` is
a cache; delete it and pull again to see it rebuilt from the bucket.

## Stop

```text
docker compose down        # stop the container, keep the notes volume
docker compose down -v     # also delete the notes volume
```
