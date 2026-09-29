# CLI and MCP v1 work (2026-09-29)

State at the checkpoint, and how to resume. Branch `claude/project-thread-6ljtie`.

## Done

- PR #17 (merged): default and `--ignore` / `SLIVINGDOC_IGNORE` ignore list;
  S3 AccessDenied / NoSuchBucket / expired credentials as non-retryable
  `ACCESS_DENIED` with the storage's words; `UPGRADE_REQUIRED` for a newer
  manifest; `IGNORED_CONFLICT`; `architecture/compatibility.md` (the 1.x
  promise: LK still to confirm the wording).
- PR #18 (open, CI green when last checked): `status`, `log`, root
  `--version` (sole argument only). Also carries the test-budget fix below.
- Test budget: `notebook` took 21 s of the 30 s gate on a disk because its
  fsyncs dominate; `internal/scratch` (`scratch.Use`) puts `TMPDIR` on
  `/dev/shm` for `integrationtest`, `notebook`, `workspace`, `app` and
  `s3store` (notebook 7.6 s now). `integrationtest` alone is 22-24 s of 30 s;
  its cost is about 230 re-executed race+cover helper processes per count.

## Left of the v1 CLI list (nothing started)

1. Timeouts. Plan: `--timeout` / `SLIVINGDOC_TIMEOUT` as a per-store-request
   deadline via a decorator over `storage.ObjectStore` (wrap `ReadObject`'s
   reader so the deadline lives until `Close`), plus dial, TLS and
   response-header timeouts on the `httpstore` client (`New` builds
   `http.Client` in `internal/httpstore/store.go`) and the S3 SDK client.
   Never apply a deadline to local mutation (recovery paths).
2. `--json` for all CLI output. Plan: reuse `mcp.SuccessInfo` / `mcp.ToolError`
   for pull and commit; errors also as JSON on stdout; `status` and `log`
   need JSON forms (`notebook.Status`, `notebook.History`); login uses NDJSON
   events; `space`, `logout`, `version` get JSON forms; bypass `internal/tui`.
3. Richer MCP results: annotations, output schema, changed files in the text.
4. One-shot pull/commit workspace-root confusion (path defaults and the
   at-or-below-root rule; see `app.OperationPath`, `Runtime.resolve`).
5. Pull must not rewrite unchanged files (inodes): `applyInPlace` in
   `internal/workspace/materialize.go` rewrites every target file.
6. Dead code and skipped tests: `Workspace.Replace`, `Diff`, `BaselineSnapshot`
   have only test callers; find `t.Skip` uses.
7. Tell the coordinator when the CLI surface is settled so the docs thread can
   document it. Docs must say v1.0.0+.

## Known and left alone

- Replacing a directory by a file during a pull fails with `RECOVERY_FAILURE`
  in `applyInPlace` (pinned by
  `TestScenarioWritableRefusalDropsWritableUnderRestoredBlob`).
- Remote files that match the new ignore defaults stay in the notebook.
- `status` lists changes under protected paths, and creates a missing directory
  like pull does.

## How to resume

- `make test` needs Docker (`dockerd` started by hand in this box:
  `rm -f /var/run/docker.pid /var/run/docker.sock; nohup dockerd &`) and the
  libgit2 build stamp. It is at the 30 s edge when other work runs beside it.
- One PR per chunk, ready not draft, attribution block in the body, reviewers
  (code and docs) before sign-off, architecture docs in the same commit.
