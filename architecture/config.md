# Configuration

How one process turns flags, environment variables and defaults into a validated `config`, and how that becomes the `ServiceConfig`, the storage identity and the S3 or hosted store settings. It answers "where does `--bucket` (or `--space`, or any other setting) come from, what is its default, and what makes startup refuse it?"

Read this when: adding or changing a flag or environment variable, changing a default or bound, touching the ephemeral session directory, the shared pack cache root, endpoint normalization, the read-only/writable path sets, S3 credentials, the hosted-mode token and space ([hosted-mode.md](./hosted-mode.md)), or `--storage` and stored logins ([login.md](./login.md)).

## Key files

| File | Purpose |
|------|---------|
| `internal/app/storage.go` | `resolveStorage` (`--storage`, the token or a stored login, the bucket default, the endpoint a stored token may reach), `loadLogins`; see [login.md](./login.md) |
| `internal/credentials/credentials.go` | `Locate` (`SLIVINGDOC_CONFIG_DIR` or the user configuration directory, from the injected environment), `File.Load`, `Set` lookups |
| `internal/app/config.go` | `Flags` + `NewFlags` + `Bind` (the flag set), `loadConfig`, `Flags.resolve` (precedence and session dir), `config.hosted`, `config.tokenOrigin`, `DefaultHostedEndpoint`, `config.finish` (validation), `validateHosted`, `resolvePolicy`, `resolveString`/`resolveBool`/`resolveInt`/`resolveRoot`, `normalizeEndpoint`, `absolute`, `splitPathEntries`, `parseUnsigned`, `stringFlag`/`boolFlag`/`intFlag`, `FlagReference`, `HelpText`, `defaultSessionDir`, `removeSessionDir` |
| `internal/app/service.go` | `ServiceConfig` (exported copy), `config.serviceConfig`, `Service.identity` (storage identity) |
| `internal/app/app.go` | `realStoreFactory` (config to `httpstore.Config` when hosted, else `s3store.Config` + `s3store.Options`), `ProcessOptions` (`Env`, `Cwd`, `CacheDir`, `Ephemeral`, `NewSessionDir`) |
| `internal/notebook/notebook.go` | The numeric defaults and bounds: `DefaultRetryLimit`, `MaxRetryLimit`, `DefaultCheckpointPacks`, `MinCheckpointPacks`, `DefaultRetainedCheckpoints`, `MaxRetainedCheckpoints` |
| `internal/storage/key.go` | `ValidatePrefix` (prefix grammar) |
| `internal/git/policy.go`, `internal/git/readonly.go` | `NewPolicy`, `OverlapError`, `NormalizeEntries` (path-set validation) |
| `internal/workspace/path.go` | `RootsOverlap` |
| `internal/workspace/identity.go` | `Identity`, `DerivedKey`, `SharedCacheDirName` (consume the normalized endpoint, region, bucket, prefix) |
| `internal/s3store/store.go` | `Config`, `Options`, `New` (credentials and addressing) |
| `internal/httpstore/store.go` | `ValidateSpace`, `ValidateToken`, `IsLoopback` (hosted-mode checks), `Config`, `New` |
| `internal/pathutil/home.go` | `ExpandHome` (a leading `~/` in roots and paths) |

## Flow

```text
cmd/<serve|pull|commit>.Command: flags := app.NewFlags(); flags.Bind(fs)
router parses fs
app.Setup(engine, flags, opts) → setup(process)
  → loadConfig(p)                                   # parses p.args only when p.flags == nil
    → Flags.resolve(env, cwd, cacheDir, ephemeral, newSessionDir)
        environ(env) → resolveString/Bool/Int/Root per setting
        resolveStorage(flags, env, {runtime.GOOS, time.Now()})             # login.md
          --storage | SLIVINGDOC_STORAGE | auto; bucket: --space | --bucket | SLIVINGDOC_SPACE | SLIVINGDOC_BUCKET (resolveBucket: two spellings of one layer
            that differ are a refusal naming both); without SLIVINGDOC_TOKEN and outside s3, the default login's space
          auto: the token beside --endpoint, AWS_ENDPOINT_URL or AWS_ENDPOINT_URL_S3 is a refusal (tokenDestinations);
                a login for an explicit bucket beside s3Signals (AWS variables, ~/.aws files, --region, --path-style) is one
          token: SLIVINGDOC_TOKEN, else a usable stored login for (endpoint, space), else none (S3)
        hosted → endpoint: the variable's --endpoint | SLIVINGDOC_ENDPOINT | DefaultHostedEndpoint,
                 or the stored login's own
        otherwise → region: --region | AWS_REGION | us-east-1; endpoint: --endpoint | AWS_ENDPOINT_URL_S3
        ephemeral && no root set → newSessionDir() → <session>/notebook, <session>/private
        sharedPackCache → <cacheDir>/slivingdoc/pack-cache
        --log-level set → slogcolor.ParseLevels (fail fast)
      → config.finish(cwd)
          bucket required (optional with a token), ValidatePrefix, normalizeEndpoint,
          hosted ? validateHosted (space, token, https unless loopback) : region required,
          absolute(roots), RootsOverlap checks, numeric bounds, resolvePolicy
  → config.serviceConfig() → NewService / StoreFactory
  → realStoreFactory → hosted ? httpstore.New(Config{Endpoint, Space: Bucket, Prefix, Token, UserAgent})
                              : s3store.New(Config{Bucket, Prefix, Region, Endpoint}, Options{ForcePathStyle})
```

## Behavior

**Settings table.** `serve`, `pull` and `commit` share every setting through `app.Flags`; `commit` adds `-m`/`--message`. `FlagReference` in `config.go` is the authoritative help text printed by `-h`.

| Setting | Flag | Environment | Default | Validation (`finish` unless noted) |
|---|---|---|---|---|
| Storage backend | `--storage` | `SLIVINGDOC_STORAGE` | `auto` | `auto`, `hosted` or `s3` (`parseStorageMode`); the selection rules are in [login.md](./login.md) |
| Hosted API token | none | `SLIVINGDOC_TOKEN` | empty (a stored login, else S3 mode) | hosted: `httpstore.ValidateToken`; ignored with `--storage s3` |
| Credentials directory | none | `SLIVINGDOC_CONFIG_DIR` | `<user-config-dir>/slivingdoc` | absolute (`credentials.Locate`); the file is read strictly, up to 1 MiB, unless `--storage s3`; a symbolic link, a non-regular file, and (not on Windows) a file or directory another user owns, a file other users can read or write, or a directory they can write, are refused (`credentials.ErrExposed`); `login` refuses such a directory even without the file; `login` and `logout` write it under `credentials.lock` |
| Bucket (hosted: space) | `--bucket`, or its hosted name `--space` | `SLIVINGDOC_BUCKET`, or its hosted name `SLIVINGDOC_SPACE` | with `SLIVINGDOC_TOKEN`: the token's space; else the default login's space unless `--storage s3` | required in S3 mode; both spellings of one layer with different values are refused; hosted: optional, `httpstore.ValidateSpace` when given, must equal the token's space |
| Prefix | `--prefix` | `SLIVINGDOC_PREFIX` | `slivingdoc` | `storage.ValidatePrefix` |
| Region | `--region` | `AWS_REGION` | `us-east-1` | non-empty; not resolved when hosted |
| Endpoint | `--endpoint` | `AWS_ENDPOINT_URL_S3`; hosted: `SLIVINGDOC_ENDPOINT` | empty (AWS resolution); hosted: `DefaultHostedEndpoint` (`https://api.slivingdoc.dev`) | `normalizeEndpoint`; hosted: `https` unless loopback |
| Path style | `--path-style` | `SLIVINGDOC_PATH_STYLE` | `false` | `strconv.ParseBool`; unused when hosted |
| Workspace root | `--workspace-root` | `SLIVINGDOC_WORKSPACE_ROOT` | serve with neither root configured: `<session>/notebook`; otherwise cwd | non-empty; made absolute |
| Private root | `--private-root` | `SLIVINGDOC_PRIVATE_ROOT` | serve with neither root configured: `<session>/private`; otherwise `<user-cache-dir>/slivingdoc` | not at or below workspace root |
| Shared pack cache | `--shared-pack-cache` | `SLIVINGDOC_SHARED_PACK_CACHE` | `false` | needs a user cache dir (checked in `Flags.resolve`); not at or below workspace root |
| Commit retries | `--commit-retries` | `SLIVINGDOC_COMMIT_RETRIES` | 8 | 0..100 |
| Checkpoint packs | `--checkpoint-packs` | `SLIVINGDOC_CHECKPOINT_PACKS` | 256 | at least 1 |
| Retained checkpoints | `--retained-checkpoints` | `SLIVINGDOC_RETAINED_CHECKPOINTS` | 1 | 0..64 |
| Read-only paths | `--read-only-paths` | `SLIVINGDOC_READ_ONLY_PATHS` | none | `resolvePolicy` |
| Writable paths | `--writable-paths` | `SLIVINGDOC_WRITABLE_PATHS` | none | `resolvePolicy` |
| Log level | `--log-level` | `LOG_LEVEL` | info | flag: `slogcolor.ParseLevels`, fatal; env: lenient |
| Log timestamp | `--log-timestamp` | `SLIVINGDOC_LOG_TIMESTAMP` | `true` | `ParseBool` |

Not flags: `NO_COLOR` and `DEBUG_PERF` are read from the environment only ([logging.md](./logging.md), [cli.md](./cli.md)).

**Precedence.** An explicitly set flag wins over the environment, which wins over the default (`resolveString`, `resolveBool`, `resolveInt`); exception: the S3 transport endpoint, see Gotchas. Each flag type records `set`, so an explicitly empty flag (`--read-only-paths=`) does not fall back to the environment; that is how a process clears an inherited value. An empty environment value counts as unset. For roots, an explicitly empty flag counts as configured and then fails the empty-root check (`resolveRoot`, `absolute`). A duplicated environment variable takes its last value (`environ`).

**Value grammar.** Booleans use `strconv.ParseBool` (`boolFlag` also accepts the bare form: `--path-style`, `--shared-pack-cache`, `--log-timestamp`). Integers are unsigned decimal only: `parseUnsigned` rejects any sign or non-digit and bounds the value to 31 bits. Path-set values are comma-separated, trimmed, and empty pieces dropped (`splitPathEntries`).

**Bucket, prefix, endpoint.** `--bucket` names the S3 bucket; slivingdoc never creates or configures it. `--space` and `SLIVINGDOC_SPACE` are the same setting under its hosted name ([login.md](./login.md)): a flag beats the variables, an explicitly empty flag does not fall back, and `--bucket` and `--space` (or the two variables) with different values refuse startup naming both. `--prefix` is empty or a slash-separated relative key prefix with no leading or trailing slash, empty segment, backslash, `.` or `..` segment (`ValidatePrefix`); one prefix holds one notebook, and the adapter joins it to protocol keys ([storage.md](./storage.md)). `--endpoint` must be an absolute `http`/`https` URL without user information, query or fragment; `normalizeEndpoint` lowercases scheme and host, removes trailing slashes and keeps a non-root path, and refuses a value whose normalized form does not parse again (a non-ASCII IPv6 zone), so a stored endpoint always loads. A custom endpoint always uses path-style addressing; `--path-style` extends that to the default AWS endpoint (`s3store.New`).

**Hosted mode.** `resolveStorage` picks the token ([login.md](./login.md)): a non-empty `SLIVINGDOC_TOKEN` unless `--storage s3`, else a usable stored login for the space unless `--storage s3`. Under `auto`, the token beside the `--endpoint` flag, `AWS_ENDPOINT_URL` or `AWS_ENDPOINT_URL_S3` (an intent check: an S3 host was configured on purpose, though hosted mode never reads the two variables; a region, AWS credentials or the `~/.aws` files are no refusal), and a login for an explicit bucket beside an S3 signal, are refusals that ask for `--storage hosted` or `--storage s3`; a login whose space was the default wins over S3 signals, since S3 would have no bucket. The signals are the AWS variables, `AWS_CONTAINER_CREDENTIALS_*`, `SLIVINGDOC_PATH_STYLE`, the `--region` and `--path-style` flags, and an existing `~/.aws/credentials` or `~/.aws/config` (`s3Signals`, the exact table in login.md). `setup` logs the outcome at Info (`logStorage`). A stored token is sent to its endpoint and, when `login` or `logout` revokes it, to `/cli/v1/revoke` of the site that issued it; nothing else receives it. A token makes `config.hosted` true, `config.tokenOrigin` records where it came from and `config.bucketFrom` which setting named the bucket, in the spelling used: `--space`, `--bucket`, `SLIVINGDOC_SPACE`, `SLIVINGDOC_BUCKET`, the default login or the token. The bucket is then optional and, when present, names the hosted space; `buildService` fills an empty one with the token's own space and refuses one the token does not reach, worded for its source (`resolveHostedSpace`, `spaceMismatch`, [hosted-mode.md](./hosted-mode.md)). The endpoint resolves from `--endpoint`, `SLIVINGDOC_ENDPOINT`, then `DefaultHostedEndpoint` for the variable's token, and is the login's own endpoint for a stored one (an explicit endpoint that differs means the login does not apply), and `AWS_REGION`/`AWS_ENDPOINT_URL_S3` are not read (the region stays empty and is not required). After `normalizeEndpoint`, `validateHosted` refuses a given space outside the `httpstore.ValidateSpace` grammar, a token that is not printable ASCII without white space (the message names `SLIVINGDOC_TOKEN`, never its value), and a non-`https` endpoint unless its host is loopback (`httpstore.IsLoopback`). See [hosted-mode.md](./hosted-mode.md).

**Storage identity.** `Service.identity` builds `workspace.Identity{Endpoint, Region, Bucket, Prefix, ManifestVersion}` from the normalized values (in hosted mode: the hosted endpoint, an empty region, and the space; the token is not part of it). `DerivedKey` hashes it with the canonical visible path to name the private directory, and `SharedCacheDirName` hashes it without the path to name the shared cache directory. Changing the endpoint spelling, region, bucket or prefix therefore selects different private state.

**Credentials.** No flag carries a credential. The only credential slivingdoc reads itself is the hosted API token, from `SLIVINGDOC_TOKEN` or from the credentials file that `slivingdoc login` writes ([login.md](./login.md)), which travels only in the hosted adapter's `Authorization` header ([hosted-mode.md](./hosted-mode.md)). In S3 mode `realStoreFactory` leaves `s3store.Config.AccessKey`/`SecretKey` empty, so `awsconfig.LoadDefaultConfig` uses the AWS SDK default chain: `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_SESSION_TOKEN`, the shared files with `AWS_PROFILE`, then ambient identity (SSO, ECS/EKS task roles, EC2 metadata). `AccessKey`/`SecretKey` are set only by tests (`tests3.Suite.StoreConfig`). An endpoint with user information is refused, so a secret cannot echo into a diagnostic. The operator view is in [running.md, S3 credentials](./running.md#s3-credentials).

**Ephemeral session directory.** When `ProcessOptions.Ephemeral` is set (only `serve`) and neither root is configured, `resolve` calls `NewSessionDir` (default `os.MkdirTemp("", "slivingdoc-")`) and uses `<session>/notebook` and `<session>/private`. The random component keeps two servers apart; the durable notebook is the bucket. `Runtime.Close` removes the whole directory, and a refusal after creation removes it too. Configuring either root disables this: the workspace root then defaults to the startup cwd and the private root to `<user-cache-dir>/slivingdoc`, and neither is removed. `pull` and `commit` never take a session directory.

**Roots.** Both roots and the pack-cache root become absolute and clean (`absolute`: `~` or `~/` expansion, then join to cwd). Without a resolvable user cache directory the private-root default degrades to `<cwd>/slivingdoc`, which overlaps a cwd workspace root and refuses startup; set `--private-root`. The private root and the shared pack-cache root must not be at or below the workspace root (`workspace.RootsOverlap`), because private state inside a visible directory would become notebook content. `workspace.Open` re-checks both.

**Shared pack cache.** When enabled, `packCacheRoot` is `<cacheDir>/slivingdoc/pack-cache`; each notebook identity gets `<sanitized bucket>-<sanitized prefix>-<16 hex of the identity digest>` below it (`SharedCacheDirName`; `sanitizeCacheComponent` lowercases, maps every non-`[a-z0-9]` to `-`, and truncates each part to 32). Enabling it without a resolvable user cache directory refuses startup. See [workspace.md](./workspace.md).

**Numeric bounds.** `--commit-retries` counts retries after the first CAS attempt; exhaustion is `REMOTE_BUSY`. Retry delay uses full jitter from an exponential ceiling starting at 25 ms, capped at 2 s (`notebook.defaultBackoffMin`/`defaultBackoffMax`). `--checkpoint-packs` is the active tail length that schedules a checkpoint; `--retained-checkpoints` is how many previous checkpoint generations the manifest keeps ([checkpoints.md](./checkpoints.md)). The bounds are the notebook's constants, so flag and notebook validation cannot drift.

**Path sets.** `resolvePolicy` first normalizes the read-only entries (`git.NormalizeEntries`), then composes both with `git.NewPolicy`. An invalid entry refuses startup naming the setting; a path named by both settings is an `OverlapError` rendered as `--read-only-paths "x" and --writable-paths "x" name the same path`. `finish` stores `policy.ReadOnly()`/`Writable()` back into the config. The sets resolve by longest match, and a non-empty writable set protects every unmatched path; the behavioral contract is in [running.md, Read-only paths](./running.md#read-only-paths).

**Diagnostics.** Resolution and validation refusals (environment values, `--log-level`, everything in `finish`) are wrapped by `setup` as `app: invalid configuration: <msg>` and passed through `mcp.Redact`. A malformed flag value on the command line is rejected earlier by the flag parser, unredacted: by the router (`failed to parse flagset`, usage on stdout) when it precedes the path, or by `OperationPath` (`pull: ...`, `commit: ...`) when it follows it. Messages never echo credentials or private values; path-set entries are notebook-relative and may be named.

## Gotchas

- `ServiceConfig` mirrors the storage, root, numeric and path-set fields of `config` but not all of them: it has no token (nor the session directory or logging fields), so a `ProcessOptions.StoreFactory` never sees hosted mode, and the hosted process scenarios use helper mode `real` (no injected factory). A new setting that the service or store factory needs must be added to `config`, `ServiceConfig`, and `serviceConfig()`; `integrationtest.NewHarness` builds `ServiceConfig` directly and must be updated too.
- Adding a flag means: a field on `Flags`, a line in `Bind`, a resolve call in `resolve` (or `resolveStorage`), validation in `finish`, and a row in `FlagReference`. `TestReleaseBinaryCommandSurface` and the `serve -h` scenario assert the help text.
- `SLIVINGDOC_LOG_TIMESTAMP` alone sets `logConfigured`, which makes `setup` rebuild the logger; `LOG_LEVEL` alone does not (the environment logger already honours it).
- `--endpoint` is shared by both modes: with `SLIVINGDOC_TOKEN` set in the environment, an `--endpoint` flag (or `AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`) refuses startup under `--storage auto` and becomes the hosted endpoint only under `--storage hosted`; `--storage s3` never reads the token. `SLIVINGDOC_ENDPOINT` is not an S3 signal. A stored login is only used for the endpoint it was issued for.
- The region flag has a default (`us-east-1`), so only an explicitly set `--region` (or `AWS_REGION`) is an S3 signal; the same holds for `--path-style`. Either counts only against a stored login for an explicit bucket, never against `SLIVINGDOC_TOKEN`.
- `resolveStorage` normalizes an explicit `--endpoint`/`SLIVINGDOC_ENDPOINT` even when the process ends up in S3 mode, so a malformed `SLIVINGDOC_ENDPOINT` now refuses startup outside `--storage s3`. The credentials file is read through the injected environment only; `testProcess` passes no `HOME`, so unit tests see no logins unless they set `SLIVINGDOC_CONFIG_DIR`.
- `AWS_REGION` and `AWS_ENDPOINT_URL_S3` are read by slivingdoc (in S3 mode only) and passed explicitly (`WithRegion`, `WithBaseEndpoint`). The S3 client resolves its service endpoint again in `resolveBaseEndpoint`, over the load options first, then the environment, then the shared profile; so a non-empty `--endpoint` wins over `AWS_ENDPOINT_URL_S3` and a profile's `services` s3 `endpoint_url`. `AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=true` or a profile's `ignore_configured_endpoint_urls = true` makes the configuration load drop every configured endpoint, `WithBaseEndpoint` included, which would send the traffic to AWS; `s3store.New` therefore also sets `BaseEndpoint` on the S3 client options, after the load, whenever an endpoint is configured. `TestConfiguredEndpointBeatsServiceEndpointSettings` covers all four settings, with negative-control rows showing that without an endpoint the ambient settings do redirect. An empty endpoint passes no load option, so `--endpoint=` does not clear a process `AWS_ENDPOINT_URL_S3`, and a generic `AWS_ENDPOINT_URL` or a profile `endpoint_url` redirects traffic without entering the storage identity or forcing path style. Other `AWS_*` variables reach the SDK only through `LoadDefaultConfig`.

## Related

- [cli.md](./cli.md): where `Setup` runs and refusals exit.
- [s3store.md](./s3store.md): how `Region`, `Endpoint`, `ForcePathStyle` and credentials reach the SDK.
- [security.md](./security.md): redaction and path rules.
- [running.md, Configuration](./running.md#configuration): operator reference.
- [AGENTS.md, Key Flags](../AGENTS.md#key-flags).
