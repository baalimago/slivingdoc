# Transport and security

The trust model: a local stdio child process, a filesystem boundary the caller cannot escape, private state the caller cannot address, credentials that never leave the process, and caller-facing text that never carries protected values. It answers "what can an MCP caller reach, and what keeps secrets and private state out of its view?"

Read this when: changing the transport, request path handling, symlink or special-file handling, the private directory layout, credential loading, the hosted API token, endpoint validation, redaction, or anything that puts new text into a tool result, the CLI report, or a startup diagnostic.

## Key files

| File | Purpose |
|------|---------|
| `internal/app/app.go` | `serve` over `sdk.StdioTransport` (no network listener); `closeTransport` |
| `internal/mcp/decode.go` | `validatePath` (absolute, UTF-8, no U+0000, at most 4,096 bytes, `~/` expansion) |
| `internal/workspace/path.go` | `canonicalize` (clean, `filepath.Rel` to the root, reject escape), `PathEscapeError`, `ErrInvalidPath`, `RootsOverlap` |
| `internal/workspace/workspace.go` | `Open` (canonical path, private dir `0o700`, `os.OpenRoot`), `rejectSymlinkComponents` |
| `internal/workspace/scan.go` | `scanWalk`/`readVisibleFile` (Lstat semantics; `ErrSymlink`, `ErrUnsupportedFile`, `ErrInvalidContent`) |
| `internal/workspace/identity.go` | `DerivedKey` (SHA-256 of length-prefixed canonical path + storage identity), `SharedCacheDirName` |
| `internal/workspace/platform_unix.go`, `platform_windows.go` | No-follow open flags |
| `internal/git/path.go` | `ValidatePath` (the notebook path rules), `ValidateContent`, `ValidateSnapshot` |
| `internal/git/readonly.go`, `policy.go` | `NormalizeEntries`, `NewPolicy` (path-set entries obey `ValidatePath`) |
| `internal/app/config.go` | `normalizeEndpoint` (refuses user information), `validateHosted` (token grammar, https unless loopback), `resolvePolicy`, `RootsOverlap` checks |
| `internal/app/storage.go` | `resolveStorage`: a stored login only for its own endpoint, `--storage s3` never reads a token or the file, `auto` refuses a login for an explicit space or `SLIVINGDOC_TOKEN` beside S3 signals (`s3Signals`) ([login.md](./login.md)) |
| `internal/app/minted.go` | `mintedTokens`: the account key goes only to its site's `space-token` route; the minted token, held in memory only, goes only to the login's endpoint; a token for another space or endpoint, or with more access than asked, is revoked |
| `internal/credentials/credentials.go`, `platform_unix.go` | The credentials file: `File.Save` (directory 0700, file 0600, temp file and rename), `File.Load` (strict, 1 MiB, `O_NOFOLLOW`, regular files only; an endpoint or site with user information, a query or a fragment is `ErrMalformed` (`plainURL`); a file of another version is `ErrUnsupportedVersion`, parsed no further; errors never echo a key or such a URL), `checkFile`, `checkOwner` and `CheckDir` (owner and mode, `ErrExposed`), `File.Lock` (`credentials.lock`) |
| `internal/sitelogin/client.go` | The login site client: `parseSite` (https unless loopback), `Start` (builds the approval pages from the site origin, code in the fragment, `userCodePattern`, the timing bounds), `issued` (validates the key, endpoint and account), `Spaces`, `Mint` (validates the space, access, endpoint and owner; `RejectedError` carries a sendable credential for revocation), `TokenHint`, `Revoke` (only 401 `invalid_token` is done), no redirects |
| `internal/app/login.go` | `platformBrowser` (starts the opener with `os.StartProcess`, never a shell; on Windows only from an absolute `SystemRoot`), `approvedBy` and `Login.confirm` (who approved, and the prompt), `Login.accept` and `Login.store` (access, site, endpoint and account guards; `--space` must be listed), `Login.discard` (revokes an issued key that is not stored), `Login.report` (never revokes another account's key) |
| `internal/s3store/store.go` | `New`: AWS default credential chain; static keys only in tests |
| `internal/httpstore/store.go` | `New` and `newClient` (no-redirect client), `checkServer` (the tokenless `GET /v1` that startup runs before any use of the token), `DescribeToken`, `ValidateEndpoint`, `IsLoopback`, `ValidateToken`, `request` (the token only in `Authorization`, from the `TokenSource`), `send` (one resend after a 401, only with a new token), `Sanitize` (server and site text) |
| `internal/mcp/errors.go` | `Redact`, `redactValues`, `invalidPathMessage`, `safeEngineDetail` |

## Flow

```text
MCP host spawns: npx -y slivingdoc serve ...      (or the native binary directly)
  stdin/stdout = MCP JSON-RPC; stderr = logs
tools/call {path}
  decodePull/decodeCommit → path omitted or "" → workspace root
                          else validatePath        # absolute, bounded, UTF-8, no NUL
  Service.notebookFor → workspace.Open
    canonicalize(root, path)                       # PathEscapeError when outside the root
    DerivedKey(canonical, identity) → <privateRoot>/<64-hex>   (0o700)
    os.OpenRoot(workspaceRoot) → rejectSymlinkComponents(rel)
  scan: Lstat every entry; symlink → ErrSymlink; non-regular → ErrUnsupportedFile;
        bad name → git.ValidatePath; bad bytes → git.ValidateContent
  result/error → MapSuccess / MapError → Redact
```

## Behavior

**Stdio only.** An MCP host starts the server as a local child process; the host and server share the visible directory. `app.serve` uses `sdk.StdioTransport`; there is no listening socket. Stdout carries protocol messages only. The npm package is the primary installation path:

```json
{ "command": "npx", "args": ["-y", "slivingdoc", "serve"] }
```

**Request paths.** An omitted or empty path means the workspace root (`decodeOptionalPath` skips `validatePath`). Otherwise `validatePath` requires an absolute UTF-8 host path of 1 through 4,096 bytes without U+0000 after `~/` expansion. `workspace.canonicalize` cleans it and requires it at or below the workspace root; an escape is `PathEscapeError`, reported as `INVALID_REQUEST`/`PATH_OUTSIDE_ROOT` with a message that names the root but never echoes the rejected path (it may be a guess at private state). A symlink in the requested path (`ErrSymlink`) and any other `ErrInvalidPath` from `workspace.Open` get the same `PATH_OUTSIDE_ROOT` code with a generic message (`invalidPathMessage`).

**Lexical checks.** The escape check (`canonicalize`) and the root overlap checks (`RootsOverlap`) are lexical (`filepath.Clean`/`filepath.Rel`); they do not resolve symlinks. Operators must not alias roots through symlinks. The workspace root itself may be a symlink, since `os.OpenRoot` follows it.

**Symlinks and special files.** `rejectSymlinkComponents` refuses a symlink in any existing component of the requested path. Scans use Lstat semantics, so a symlink is never followed and is rejected, as are devices, sockets and named pipes. Visible files are read and replaced through an `os.Root`, so replacement and cleanup do not follow links out of the root. Details are in [workspace.md](./workspace.md).

**Notebook path rules.** Every notebook-relative path (files, and `--read-only-paths`/`--writable-paths` entries) passes `git.ValidatePath`: UTF-8 in NFC, relative slash segments, bounded length, no control or reserved characters, no trailing space or dot, no `.`, `..` or `.git` segment, no Windows device names. A malformed path-set entry refuses startup instead of silently protecting nothing.

**Private state.** The internal repository, state record and locks live under `<privateRoot>/<DerivedKey>`, created `0o700`. The key is a SHA-256 of the length-prefixed canonical path and the storage identity (endpoint, region, bucket, prefix, manifest version), so it does not expose the path and callers cannot select another workspace's state. The private root and the shared pack-cache root must not be at or below the workspace root (`RootsOverlap`, checked in `config.finish` and again in `workspace.Open`). Only `serve` uses an ephemeral session directory (random per process, removed at shutdown), and only when neither root is configured by flag or environment (`SLIVINGDOC_WORKSPACE_ROOT`, `SLIVINGDOC_PRIVATE_ROOT`); `pull` and `commit` default to the cwd and `<user-cache-dir>/slivingdoc` ([config.md](./config.md)).

**Credentials.** slivingdoc has no authentication layer and no credential flag. The S3 client uses the AWS SDK default credential chain inside `internal/s3store`. In hosted mode the credential is an API token: `SLIVINGDOC_TOKEN`, or a short-lived token of one space minted from the account key that `slivingdoc login` writes to the credentials file; neither comes from a flag, so it never appears in a process listing ([hosted-mode.md](./hosted-mode.md), [login.md](./login.md)). The file is `<user-config-dir>/slivingdoc/credentials.json` (or under `SLIVINGDOC_CONFIG_DIR`), mode 0600 in a 0700 directory, replaced atomically under a lock, and refused (like ssh) when it is a symbolic link or not a regular file, when another user owns it or its directory, or when other users can read or write the file or write its directory (on Windows only the link and regular-file checks apply; the rest is left to the profile's ACLs); it holds one account key per endpoint and no space token. A key is only ever sent to the site that issued it (the `site` recorded beside it, over https or loopback http): to list spaces, to mint a token, and to be revoked; it never reaches a storage endpoint. A process trades it for a token of one space that lasts at most an hour, keeps that token in memory only, sends it only to the login's endpoint (the mint answer must name it), and mints another before it expires or once after a 401; revoking the key (`logout`, or the Tokens page) revokes every token minted from it. An expired key is refused before any request. The login itself travels only over https to the site (http to loopback only); the client builds the approval pages it prints and opens from the site origin, with the code in the URL fragment, and the browser opener is started directly with the URL as one argument; every value the site returns is validated before it is printed or stored; the approver and every space the key reaches, with its owner, are shown and, on a terminal, confirmed before the key is stored; and a key or minted token the client does not keep is revoked. The full list of threats and residual risks is login.md's [Threat model](./login.md#threat-model). It travels only as `Authorization: Bearer` from `internal/httpstore` (never to `GET /v1`, and never before that tokenless description has identified the endpoint as the storage API: `CheckAccess` and `DescribeToken` both run `checkServer` first, and startup runs one of them before any other request), and only over `https` unless the endpoint host is loopback (`validateHosted`, and `httpstore.ValidateEndpoint` again in `New`). The adapter's client never follows a redirect, so the token cannot be forwarded to another host and a redirected `PUT` cannot pass for a stored write. `AWS_ENDPOINT_URL_S3` and the other AWS variables cannot redirect the token; `--endpoint` redirects `SLIVINGDOC_TOKEN` only under `--storage hosted` (hosted mode never reads `AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3` or a profile's `endpoint_url`; under `auto` a token beside the `--endpoint` flag or either variable refuses startup as an intent check, while a region, AWS credentials or the `~/.aws` files do not), never a stored login's minted tokens (they go to the login's own endpoint), and `--storage s3` never reads either. Credentials and private repository data stay in the process: MCP callers see only the two tools and their envelopes. An endpoint URL with user information is refused at configuration time, so a secret cannot be echoed into a diagnostic. See [running.md, S3 credentials](./running.md#s3-credentials).

**Redaction.** Caller-facing text (tool results, the CLI report, startup diagnostics) never contains a credential, an S3 key, a private path, a Git object ID, or Git vocabulary. Notebook messages are written without them, and `mcp.Redact` removes `sld_` hosted API tokens, pack keys, probe keys, 40-hex and 64-hex IDs, `AKIA...` key IDs and URL user information as defense in depth. In a tool result or CLI report the only remote text is a hosted store refusal's `message`, cut to one printable ASCII line by `httpstore.Sanitize`, redacted, and appended after `The storage says:`; it is untrusted server output. Startup diagnostics carry more remote text, all through `mcp.Redact`: the S3 probe diagnostic names the S3 error, and the hosted check names the server's error code, reason and message (sanitized) and, for an unknown server, its `api` field (quoted, not sanitized). Engine causes reach callers only through the `safeEngineDetail` allowlist; everything else is logged against the `diagnosticId`. Log `cause` fields additionally strip absolute paths (`redactValues`). See [errors.md](./errors.md).

**Read-only paths are a guardrail, not a boundary.** The serve process holds the S3 credentials or the hosted token. An agent that can read that environment, or launch its own slivingdoc process, bypasses `--read-only-paths`/`--writable-paths`. The policy lives in the server configuration, never in the data ([running.md, Read-only paths](./running.md#read-only-paths)).

## Gotchas

- Any new caller-visible string must go through `Redact` or be built from values known to be safe (notebook-relative paths, fixed text).
- Do not add a network transport without revisiting this model: today every caller is the local host that spawned the process.
- `redactValues` output is for logs only; it keeps vocabulary that must not reach callers.
- A new file type accepted by the scan (for example a hard-linked file) must be checked against the no-follow and no-special-file rules.

## Related

- [mcp-server.md](./mcp-server.md): decoding and envelopes.
- [workspace.md](./workspace.md): scan, materialize and private state.
- [config.md](./config.md): roots, endpoint and path-set validation.
- [errors.md](./errors.md): redaction and the taxonomy.
- [running.md](./running.md): operator guidance on credentials and sharing.
- [hosted-mode.md](./hosted-mode.md): the hosted adapter's token, endpoint and redirect rules.
- [login.md](./login.md): the credentials file, the login site client, minted tokens, and storage selection.
