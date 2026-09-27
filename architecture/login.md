# Login, stored credentials, and storage selection

`slivingdoc login` gets a hosted API token through a browser approval (the device flow, like `gh auth login`) and stores it, so `serve`, `pull` and `commit` reach a hosted space without `SLIVINGDOC_TOKEN`. `slivingdoc logout` revokes it. `--storage auto|hosted|s3` decides, at every startup, whether a process uses S3 or the hosted API and with which token. The site side (the `/cli/v1/*` routes and the approval page) lives in [baalimago/slivingdoc-cloud](https://github.com/baalimago/slivingdoc-cloud) (`architecture/cli-login.md` there). This doc answers "how does a login become a token on disk, and which store and token does a process pick?"

Read this when: changing `login` or `logout`, the site client or its wire contract, the credentials file, `--storage` or the rules that pick S3 or hosted mode, the endpoint a stored token may reach, the browser opener, or the login scenarios.

## Key files

| File | Purpose |
|------|---------|
| `cmd/login/login.go` | `Command` (`login`) and `LogoutCommand` (`logout`) over one `command` type: `Setup` refuses a positional argument and calls `app.PrepareLogin` / `app.PrepareLogout`; `Run` runs the prepared operation; `loginHelp`, `logoutHelp` |
| `internal/app/login.go` | `SiteEnv`, `LoginFlags`, `LogoutFlags`, `PrepareLogin` → `Login.Run`, `Login.discard`, `PrepareLogout` → `Logout.Run`, `loggedIn` (the result line), `revoke`, `credentialsFile`, `siteClient`, `ProcessOptions.errOut`, `ProcessOptions.openBrowser`, `platformBrowser`, `lookupFold`, `findOnPath` |
| `internal/app/storage.go` | `storageMode` (`auto`, `hosted`, `s3`), `parseStorageMode`, `tokenOrigin` (`originEnv`, `originLogin`), `awsSignals`, `storageSelection`, `storageInputs`, `resolveStorage`, `loadLogins`, `findLogin`, `noLoginRefusal`, `awsConfigured` |
| `internal/app/config.go` | `Flags.storage` (`--storage`), `Flags.resolve` calls `resolveStorage`; `config.tokenOrigin`; `FlagReference` |
| `internal/app/app.go` | `ProcessOptions.OpenBrowser`, `Sleep`, `Hostname`; `checkStore` (the refusal for a stored login says to log in again) |
| `internal/credentials/credentials.go` | `FileName`, `DirEnv`, `FormatVersion`, `Locate`, `userConfigDir`, `File` (`Path`, `Load`, `Save`, `checkPrivate`), `Set` (`Logins`, `Default`, `Lookup`, `ForSpace`, `Space`, `Put`, `Remove`), `Login`, `Key`, `Access`, `ParseAccess`, `Expiry`, `ExpiresAt`; errors `ErrNoConfigDir`, `ErrMalformed`, `ErrNoLogin`, `ErrNoDefault`, `ErrAmbiguous`, `ErrExpired`, `ErrExposed` |
| `internal/sitelogin/client.go` | `DefaultSite`, `Config`, `Client` (`New`, `Site`, `Start`, `Wait`, `Revoke`), `StartRequest`, `Approval`, `Issued`, `Refusal`, `RejectedTokenError`; errors `ErrDenied`, `ErrCodeExpired`, `ErrProtocol`, `ErrRefused`, `ErrUnreachable`; `parseSite`, `sameOrigin`, `issued`, `transient`, `retryWait`, `label` |
| `internal/sitelogin/sitetest/site.go` | Test-only reference site: `Start`, `URL`, `Next` (a `Script` of pending codes, then a final code or an `Issue`), `SetTiming`, `Starts`, `Polls`, `Revoked`, `Revoke`, `Issued`, `Close` |
| `internal/credentials/credentials_test.go`, `internal/sitelogin/client_test.go`, `internal/app/storage_test.go`, `internal/app/login_test.go` | Unit tests: the file format and its refusals, the poll timing and outcomes, the resolution table, login and logout over `sitetest` |
| `internal/integrationtest/scenario_login_test.go` | CLI processes against `sitetest` and `gatewaytest`: login then pull and commit, login again, poll outcomes, `--storage` selection, an expired login, logout |

## Flow

```text
slivingdoc login [--bucket s] [--read-only] [--site url] [--no-browser]
  Setup: PrepareLogin
    credentialsFile(env) → credentials.Locate(SLIVINGDOC_CONFIG_DIR | <user config dir>/slivingdoc)
    File.Load (refuse a malformed file now), httpstore.ValidateSpace(--bucket)
    siteClient(--site | SLIVINGDOC_SITE | https://www.slivingdoc.dev)   https unless loopback, origin only
  Run: Login.Run
    POST <site>/cli/v1/start {space, access, client: hostname}  → Approval (code, page on the site's origin)
    stderr: code + page + "only approve if you started this"; openBrowser unless --no-browser (failure: a hint)
    Client.Wait: sleep(interval) → POST /cli/v1/token {deviceCode}
        authorization_pending → again; slow_down → interval += 5 s; access_denied → ErrDenied;
        expired_token or the deadline passed → ErrCodeExpired
        no answer or a broken body (ErrUnreachable) or a 5xx → retry, the pause doubling up to a minute, capped at the deadline
        any other error answer → ErrRefused
        200 → Issued (token, space, access, expiresAt, endpoint, account, owner), validated;
              an undecodable body → ErrProtocol; a sendable token in an answer outside the contract
              → *RejectedTokenError → discard (revoke)
    refuse a write token for --read-only, or an endpoint that does not normalize → discard (revoke)
    File.Load → Set.Put (new default) → File.Save (0600 temp file + rename, dir 0700);
      a Load or Save failure → discard (revoke)
    replaced a different token for the same (endpoint, space) → revoke it at its site (best effort)
    stdout: Logged in as <account> to space "<space>" (<access>) until <date>[, owned by <owner>]

slivingdoc logout [--bucket s] [--site url]
  Setup: PrepareLogout: logins of --bucket (else the default's space), narrowed to --site | SLIVINGDOC_SITE
  Run: for each: POST <its site>/cli/v1/revoke (Bearer token); 204 or 401 → remove; else keep, report
       File.Save; stdout, per removed login: Logged out of space "<space>" at <endpoint>; the token was revoked

serve | pull | commit: Flags.resolve → resolveStorage(flags, env, {GOOS, now})
  mode = --storage | SLIVINGDOC_STORAGE | auto
  s3     → bucket from --bucket | SLIVINGDOC_BUCKET only; token and logins never read
  bucket = --bucket | SLIVINGDOC_BUCKET | the default login's space (kept only if the outcome is hosted)
  SLIVINGDOC_TOKEN set → hosted, origin env, endpoint --endpoint | SLIVINGDOC_ENDPOINT | DefaultHostedEndpoint
  login = explicit endpoint ? Set.Lookup(endpoint, space) : Set.ForSpace(space)
    none      → auto: S3; hosted: refusal (names the endpoint the login was issued for)
    ambiguous → refusal: pass --endpoint
    found     → auto and an awsSignals variable set → refusal
              → expired → refusal: run 'slivingdoc login --bucket <space>', or pass --storage s3
              → hosted, origin login, endpoint = the login's
```

## Behavior

**Wire contract.** The site routes take and answer JSON, set no cookies, and answer errors as `{"error", "message"}`. `start` takes `space` (optional), `access` (`write` or `read`) and `client` (the host name, cut by `label` to 64 printable ASCII bytes) and answers `deviceCode`, `userCode`, `verificationUri`, `verificationUriComplete`, `interval` and `expiresIn`; a `503 busy` or any other error answer ends the login with the site's message. `token` answers the token, `space`, `access`, `expiresAt`, `endpoint` (the hosted API that belongs to this site, so a dev site's login talks to the dev API), `account` (who approved the code) and `owner` (who owns the space). `revoke` authenticates with the token itself and answers 204, or 401 for a token the site does not know, which counts as revoked.

**What the client trusts.** `sitelogin.New` accepts an https origin, or http to a loopback host (`httpstore.IsLoopback`), without a path, user information, query or fragment; `--site` passes through `normalizeEndpoint` first, so a trailing slash or upper-case host is fine. The default site is the `www` host because the bare domain answers a `POST` with a 301. The client never follows a redirect (a 3xx is a `Refusal`), reads at most 16 KiB of an answer, and sanitizes the site's `error` and `message` with `httpstore.Sanitize`. `Start` requires both approval pages on the site's own scheme and host (`sameOrigin`), so the page a browser opens can never be another host or a `file:` URL, and requires printable codes. `issued` validates the token (`httpstore.ValidateToken`), the space (`httpstore.ValidateSpace`), the access, the endpoint (`httpstore.ValidateEndpoint`: https unless loopback), `expiresAt` (required, RFC 3339), and a printable `account` and `owner` of at most 254 bytes; anything else is `ErrProtocol`, and no error carries the token. When the token itself is sendable, the error is a `*RejectedTokenError` carrying it, so `Login.Run` revokes it (`Login.discard`) instead of leaving a minted token valid and unseen.

**Polling.** `Wait` sleeps the interval (at least one second) before every poll, adds five seconds after each `slow_down`, and stops with `ErrCodeExpired` once the clock reaches the approval's deadline (`expiresIn` after `Start`) without polling again. `access_denied` is `ErrDenied` and `expired_token` is `ErrCodeExpired`, each with the site's message. A poll that got no answer or whose answer broke off while being read (`ErrUnreachable`), or a 5xx, is retried (`transient`), the pause doubling from the interval per failure in a row up to a minute (`retryWait`) and never past the deadline; if the code expires meanwhile, `ErrCodeExpired` names the last failure. Every other error answer (a 4xx outside the contract's codes) ends the wait as `ErrRefused`. The sleep and clock are `Config.Sleep` and `Config.Now`; the process passes `ProcessOptions.Sleep`.

**The result line.** Whoever enters a user code first decides it, so the line names the approving account and, when it differs, the owner: `Logged in as ada@x to space "notes" (read and write) until 2026-12-26 09:00 UTC, owned by bob@y` (`loggedIn`, `Access.Describe`, `Expiry.Describe`; the line always names a date, because `issued` refuses an answer without `expiresAt`; only a stored entry without `expiresAt` has no expiry, and `Login.Usable` never refuses it). The prompt (code, page, the warning, a browser failure, the wait) goes to stderr; only the result line goes to stdout. A login with `--read-only` refuses a write token, and a token whose endpoint does not normalize is refused too; either is revoked at once (`Login.discard`) and nothing is stored, as is a token the credentials file could not be read or written for. When the host name is unknown, stderr says so and the token is labelled without it (`CLI login`).

**The browser.** `ProcessOptions.OpenBrowser` opens the page when set; otherwise `platformBrowser` starts `/usr/bin/open` on macOS, `%SystemRoot%\System32\rundll32.exe url.dll,FileProtocolHandler` on Windows (the variable looked up without regard to case, `lookupFold`, and refused unless it is an absolute path, so no `rundll32.exe` from the working directory ever runs), and `xdg-open` from `PATH` elsewhere (`findOnPath`), through `os.StartProcess` with null streams, and does not wait. A failure prints `Could not open a browser (...); open the page yourself.` and the login goes on; `--no-browser` never tries.

**The credentials file.** `credentials.Locate` reads `SLIVINGDOC_CONFIG_DIR` (absolute) from the injected environment, else mirrors `os.UserConfigDir` over it (`XDG_CONFIG_HOME` or `HOME/.config`, `HOME/Library/Application Support`, `AppData`) plus `slivingdoc`; no directory is `ErrNoConfigDir`. The file is `credentials.json`:

```json
{
  "version": 1,
  "default": { "endpoint": "https://api.slivingdoc.dev", "space": "notes" },
  "logins": [
    { "endpoint": "https://api.slivingdoc.dev", "space": "notes", "site": "https://www.slivingdoc.dev",
      "token": "sld_...", "access": "write", "expiresAt": "2026-12-26T09:00:00Z",
      "account": "ada@example.com", "owner": "ada@example.com" }
  ]
}
```

It is read with `strictjson`: unknown or duplicate fields, `null`, another `version`, an invalid endpoint, site, space, token or access, a malformed `expiresAt`, an empty `account` or `owner`, two logins for one (endpoint, space), and a `default` without its login are `ErrMalformed`, and no message echoes a token. `default`, `expiresAt`, `account` and `owner` may be absent. A missing file is an empty `Set`. Like ssh, on every platform but Windows `File.Load` refuses (`ErrExposed`, saying which `chmod` to run) an existing directory that group or other can write, even when it holds no file yet, so `login` fails before it contacts the site, and an existing file that group or other can read or write; `File.Save` repeats the check after creating the directory (`checkPrivate`). `File.Save` creates the directory `0700`, writes a `0600` temporary file beside the target, syncs it and renames it over the target, so a reader never sees half a file; two logins at once each write a whole file and the last one wins. The endpoint is stored normalized (`normalizeEndpoint`), which is what a process compares against, and `site` records where `logout` revokes the token.

**One login per (endpoint, space).** `Set.Put` replaces the login of the same key and makes it the default; `Login.Run` then revokes the replaced token at the site that issued it, best effort (a failure is a stderr hint to revoke it on the Tokens page). The file is read again right before it is written, so a login that finished in another terminal meanwhile keeps its entry.

**Logout.** `PrepareLogout` takes every login of `--bucket` (else the default login's space; no default is a refusal), narrowed to the logins issued by `--site` or `SLIVINGDOC_SITE` when either is set; none for the space is `ErrNoLogin`, and none left after that narrowing is `ErrNoLogin` naming the site filter. `Logout.Run` revokes each at its own `site`; a revoked token (204) or one the site no longer knows (401) is removed, with one stdout line per removed login, and removing the default clears it. A failed revocation keeps that login stored, prints no "revoked" line for it, and ends the command nonzero, so the logout can be repeated.

**Which storage a process uses.** `resolveStorage` runs first in `Flags.resolve`, for `serve`, `pull` and `commit` alike:

| `--storage` | `SLIVINGDOC_TOKEN` | Stored login for the space | AWS settings | Result |
|---|---|---|---|---|
| `s3` | ignored | ignored (file never read) | used | S3 |
| `hosted` | set | any | ignored | hosted with the variable's token |
| `hosted` | unset | usable | ignored | hosted with the login's token, at its endpoint |
| `hosted` | unset | none, or not for this endpoint | ignored | refusal: run `slivingdoc login` |
| `auto` | set | any | ignored | hosted with the variable's token |
| `auto` | unset | usable | any of `awsSignals`: `AWS_ACCESS_KEY_ID`, `AWS_PROFILE`, `AWS_ENDPOINT_URL_S3`, `AWS_ENDPOINT_URL`, `AWS_SHARED_CREDENTIALS_FILE`, `AWS_WEB_IDENTITY_TOKEN_FILE` | refusal naming them and `--storage hosted` / `--storage s3` |
| `auto` | unset | usable | none | hosted with the login's token, at its endpoint |
| `auto` | unset | none, or not for this endpoint | any | S3 |

An expired login that would be used is a refusal (`Login.Usable`) naming the date, `run 'slivingdoc login --bucket <space>'` and `--storage s3`; nothing is sent. Outside `s3`, an omitted `--bucket` (and `SLIVINGDOC_BUCKET`) is the default login's space, also when the token comes from the variable, but only when the outcome is hosted: when the login does not apply (another explicit endpoint) the process falls back to S3 with no bucket, which is the usual `bucket is required` refusal, never an S3 bucket named after the space. The variable's token keeps today's endpoint rule (`--endpoint`, `SLIVINGDOC_ENDPOINT`, `DefaultHostedEndpoint`); it never borrows a login's endpoint.

**A stored token only reaches its own endpoint.** With no explicit endpoint, `Set.ForSpace` picks the default login when it names the space, else the only login for the space; several at different endpoints are a refusal asking for `--endpoint`. An explicit `--endpoint` or `SLIVINGDOC_ENDPOINT` selects exactly the login issued for it (`Set.Lookup`, after `normalizeEndpoint`), so a different endpoint means the login does not apply: S3 under `auto`, a refusal naming the endpoint the login was issued for under `hosted`. `--storage s3` never reads the token or the file, which closes the old gotcha of an S3 `--endpoint` receiving a stray `SLIVINGDOC_TOKEN` for anyone who sets it ([hosted-mode.md](./hosted-mode.md)).

**The credentials file at startup.** Outside `s3` mode, a process that has no token, or no bucket, reads the file (`loadLogins`). No configuration directory in the environment means no logins. A file that exists but cannot be read strictly refuses startup, naming the file and suggesting `--storage s3`. `version` and `-h` exit before `Setup`, so they never read it.

**Refused tokens.** When the hosted startup check refuses a token that came from a stored login (`config.tokenOrigin` is `originLogin`), `checkStore` says `app: hosted storage refused the stored login: ...; run 'slivingdoc login' again, or check --bucket` instead of naming `SLIVINGDOC_TOKEN`. A token revoked while `serve` runs is still reported by the notebook's fixed `ACCESS_DENIED` message, which names `SLIVINGDOC_TOKEN`: the notebook does not know where its token came from ([errors.md](./errors.md)).

## Gotchas

- The token is read once at startup; after a new login, an MCP host must restart `serve`.
- A space renamed on the site keeps its old name in the file; the gateway answers `no_space` and the startup check says to log in again.
- The file is read through the injected environment (`ProcessOptions.Env`), never `os.UserConfigDir`, so tests stay hermetic: `internal/integrationtest` drops `SLIVINGDOC_CONFIG_DIR`, `SLIVINGDOC_STORAGE`, `SLIVINGDOC_SITE`, `SLIVINGDOC_TOKEN` and `SLIVINGDOC_ENDPOINT` (`sanitizedEnv`) and gives every helper its own empty `SLIVINGDOC_CONFIG_DIR`; the `internal/app` process helper drops the same names and every name it sets itself (`helperEnv`) before appending its own values, because a child keeps the first entry of a duplicate name; `release_test.go` drops them and points every binary at a per-run empty directory made in `TestMain`; and `testProcess` passes no `HOME`.
- Tests that write a credentials file create its directory `0700` themselves (a `cfg` subdirectory of `t.TempDir()`): under a `0002` umask a bare `t.TempDir()` is group-writable, which `ErrExposed` refuses.
- `os/exec` is banned module-wide (`TestNoGitExecutableOrGit2goImport`), so the browser opener finds and starts its program with `os.StartProcess`.
- The integration helper's `Sleep` waits a hundredth of what the site asks for; exact poll timing is pinned by the `sitelogin` unit tests over a fake clock, not by the scenarios.
- `Set.Put` returns `ErrNoLogin` when nothing was replaced: absence is an error by the code style, so callers test `err == nil` for "a login was replaced".
- CI keeps using `SLIVINGDOC_TOKEN`; the login is for people.

## Related

- [hosted-mode.md](./hosted-mode.md): the hosted adapter the chosen token feeds, and its startup check.
- [config.md](./config.md) and [running.md](./running.md): every flag and variable, including `--storage`, `SLIVINGDOC_CONFIG_DIR` and `SLIVINGDOC_SITE`.
- [cli.md](./cli.md): the command map and the startup refusal surface.
- [security.md](./security.md): where the token is stored, which hosts it may reach, and what is redacted.
- [testing.md](./testing.md): `sitetest`, the process seams, and the login scenarios.
