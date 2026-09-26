# Configuration

How one process turns flags, environment variables and defaults into a validated `config`, and how that becomes the `ServiceConfig`, the storage identity and the S3 client settings. It answers "where does `--bucket` (or any other setting) come from, what is its default, and what makes startup refuse it?"

Read this when: adding or changing a flag or environment variable, changing a default or bound, touching the ephemeral session directory, the shared pack cache root, endpoint normalization, the read-only/writable path sets, or S3 credentials.

## Key files

| File | Purpose |
|------|---------|
| `internal/app/config.go` | `Flags` + `NewFlags` + `Bind` (the flag set), `loadConfig`, `Flags.resolve` (precedence and session dir), `config.finish` (validation), `resolvePolicy`, `resolveString`/`resolveBool`/`resolveInt`/`resolveRoot`, `normalizeEndpoint`, `absolute`, `splitPathEntries`, `parseUnsigned`, `stringFlag`/`boolFlag`/`intFlag`, `FlagReference`, `HelpText`, `defaultSessionDir`, `removeSessionDir` |
| `internal/app/service.go` | `ServiceConfig` (exported copy), `config.serviceConfig`, `Service.identity` (storage identity) |
| `internal/app/app.go` | `realStoreFactory` (config to `s3store.Config` + `s3store.Options`), `ProcessOptions` (`Env`, `Cwd`, `CacheDir`, `Ephemeral`, `NewSessionDir`) |
| `internal/notebook/notebook.go` | The numeric defaults and bounds: `DefaultRetryLimit`, `MaxRetryLimit`, `DefaultCheckpointPacks`, `MinCheckpointPacks`, `DefaultRetainedCheckpoints`, `MaxRetainedCheckpoints` |
| `internal/storage/key.go` | `ValidatePrefix` (prefix grammar) |
| `internal/git/policy.go`, `internal/git/readonly.go` | `NewPolicy`, `OverlapError`, `NormalizeEntries` (path-set validation) |
| `internal/workspace/path.go` | `RootsOverlap` |
| `internal/workspace/identity.go` | `Identity`, `DerivedKey`, `SharedCacheDirName` (consume the normalized endpoint, region, bucket, prefix) |
| `internal/s3store/store.go` | `Config`, `Options`, `New` (credentials and addressing) |
| `internal/pathutil/home.go` | `ExpandHome` (a leading `~/` in roots and paths) |

## Flow

```text
cmd/<serve|pull|commit>.Command: flags := app.NewFlags(); flags.Bind(fs)
router parses fs
app.Setup(engine, flags, opts) → setup(process)
  → loadConfig(p)                                   # parses p.args only when p.flags == nil
    → Flags.resolve(env, cwd, cacheDir, ephemeral, newSessionDir)
        environ(env) → resolveString/Bool/Int/Root per setting
        ephemeral && no root set → newSessionDir() → <session>/notebook, <session>/private
        sharedPackCache → <cacheDir>/slivingdoc/pack-cache
        --log-level set → slogcolor.ParseLevels (fail fast)
      → config.finish(cwd)
          bucket required, ValidatePrefix, region required, normalizeEndpoint,
          absolute(roots), RootsOverlap checks, numeric bounds, resolvePolicy
  → config.serviceConfig() → NewService / StoreFactory
  → realStoreFactory → s3store.New(Config{Bucket, Prefix, Region, Endpoint}, Options{ForcePathStyle})
```

## Behavior

**Settings table.** `serve`, `pull` and `commit` share every setting through `app.Flags`; `commit` adds `-m`/`--message`. `FlagReference` in `config.go` is the authoritative help text printed by `-h`.

| Setting | Flag | Environment | Default | Validation (`finish` unless noted) |
|---|---|---|---|---|
| Bucket | `--bucket` | `SLIVINGDOC_BUCKET` | none | required |
| Prefix | `--prefix` | `SLIVINGDOC_PREFIX` | `slivingdoc` | `storage.ValidatePrefix` |
| Region | `--region` | `AWS_REGION` | `us-east-1` | non-empty |
| Endpoint | `--endpoint` | `AWS_ENDPOINT_URL_S3` | empty (AWS resolution) | `normalizeEndpoint` |
| Path style | `--path-style` | `SLIVINGDOC_PATH_STYLE` | `false` | `strconv.ParseBool` |
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

**Bucket, prefix, endpoint.** `--bucket` names the S3 bucket; slivingdoc never creates or configures it. `--prefix` is empty or a slash-separated relative key prefix with no leading or trailing slash, empty segment, backslash, `.` or `..` segment (`ValidatePrefix`); one prefix holds one notebook, and the adapter joins it to protocol keys ([storage.md](./storage.md)). `--endpoint` must be an absolute `http`/`https` URL without user information, query or fragment; `normalizeEndpoint` lowercases scheme and host, removes a trailing slash and keeps a non-root path. A custom endpoint always uses path-style addressing; `--path-style` extends that to the default AWS endpoint (`s3store.New`).

**Storage identity.** `Service.identity` builds `workspace.Identity{Endpoint, Region, Bucket, Prefix, ManifestVersion}` from the normalized values. `DerivedKey` hashes it with the canonical visible path to name the private directory, and `SharedCacheDirName` hashes it without the path to name the shared cache directory. Changing the endpoint spelling, region, bucket or prefix therefore selects different private state.

**Credentials.** No flag or environment variable of slivingdoc carries a credential. `realStoreFactory` leaves `s3store.Config.AccessKey`/`SecretKey` empty, so `awsconfig.LoadDefaultConfig` uses the AWS SDK default chain: `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_SESSION_TOKEN`, the shared files with `AWS_PROFILE`, then ambient identity (SSO, ECS/EKS task roles, EC2 metadata). `AccessKey`/`SecretKey` are set only by tests (`tests3.Suite.StoreConfig`). An endpoint with user information is refused, so a secret cannot echo into a diagnostic. The operator view is in [running.md, S3 credentials](./running.md#s3-credentials).

**Ephemeral session directory.** When `ProcessOptions.Ephemeral` is set (only `serve`) and neither root is configured, `resolve` calls `NewSessionDir` (default `os.MkdirTemp("", "slivingdoc-")`) and uses `<session>/notebook` and `<session>/private`. The random component keeps two servers apart; the durable notebook is the bucket. `Runtime.Close` removes the whole directory, and a refusal after creation removes it too. Configuring either root disables this: the workspace root then defaults to the startup cwd and the private root to `<user-cache-dir>/slivingdoc`, and neither is removed. `pull` and `commit` never take a session directory.

**Roots.** Both roots and the pack-cache root become absolute and clean (`absolute`: `~` or `~/` expansion, then join to cwd). Without a resolvable user cache directory the private-root default degrades to `<cwd>/slivingdoc`, which overlaps a cwd workspace root and refuses startup; set `--private-root`. The private root and the shared pack-cache root must not be at or below the workspace root (`workspace.RootsOverlap`), because private state inside a visible directory would become notebook content. `workspace.Open` re-checks both.

**Shared pack cache.** When enabled, `packCacheRoot` is `<cacheDir>/slivingdoc/pack-cache`; each notebook identity gets `<sanitized bucket>-<sanitized prefix>-<16 hex of the identity digest>` below it (`SharedCacheDirName`; `sanitizeCacheComponent` lowercases, maps every non-`[a-z0-9]` to `-`, and truncates each part to 32). Enabling it without a resolvable user cache directory refuses startup. See [workspace.md](./workspace.md).

**Numeric bounds.** `--commit-retries` counts retries after the first CAS attempt; exhaustion is `REMOTE_BUSY`. Retry delay uses full jitter from an exponential ceiling starting at 25 ms, capped at 2 s (`notebook.defaultBackoffMin`/`defaultBackoffMax`). `--checkpoint-packs` is the active tail length that schedules a checkpoint; `--retained-checkpoints` is how many previous checkpoint generations the manifest keeps ([checkpoints.md](./checkpoints.md)). The bounds are the notebook's constants, so flag and notebook validation cannot drift.

**Path sets.** `resolvePolicy` first normalizes the read-only entries (`git.NormalizeEntries`), then composes both with `git.NewPolicy`. An invalid entry refuses startup naming the setting; a path named by both settings is an `OverlapError` rendered as `--read-only-paths "x" and --writable-paths "x" name the same path`. `finish` stores `policy.ReadOnly()`/`Writable()` back into the config. The sets resolve by longest match, and a non-empty writable set protects every unmatched path; the behavioral contract is in [running.md, Read-only paths](./running.md#read-only-paths).

**Diagnostics.** Resolution and validation refusals (environment values, `--log-level`, everything in `finish`) are wrapped by `setup` as `app: invalid configuration: <msg>` and passed through `mcp.Redact`. A malformed flag value on the command line is rejected earlier by the flag parser, unredacted: by the router (`failed to parse flagset`, usage on stdout) when it precedes the path, or by `OperationPath` (`pull: ...`, `commit: ...`) when it follows it. Messages never echo credentials or private values; path-set entries are notebook-relative and may be named.

## Gotchas

- `ServiceConfig` duplicates `config` field for field. A new setting that the service or store factory needs must be added to `config`, `ServiceConfig`, and `serviceConfig()`; `integrationtest.NewHarness` builds `ServiceConfig` directly and must be updated too.
- Adding a flag means: a field on `Flags`, a line in `Bind`, a resolve call in `resolve`, validation in `finish`, and a row in `FlagReference`. `TestReleaseBinaryCommandSurface` and the `serve -h` scenario assert the help text.
- `SLIVINGDOC_LOG_TIMESTAMP` alone sets `logConfigured`, which makes `setup` rebuild the logger; `LOG_LEVEL` alone does not (the environment logger already honours it).
- `AWS_REGION` and `AWS_ENDPOINT_URL_S3` are read by slivingdoc and passed explicitly (`WithRegion`, `WithBaseEndpoint`), but the S3 client re-reads the process `AWS_ENDPOINT_URL_S3` (and a profile's `services` s3 `endpoint_url`) in `resolveBaseEndpoint` and lets it override. So `--endpoint` does not beat it at the transport, `--endpoint=` does not clear it, and with `--endpoint` empty a generic `AWS_ENDPOINT_URL` or profile `endpoint_url` redirects traffic without entering the storage identity or forcing path style (known bug). Other `AWS_*` variables reach the SDK only through `LoadDefaultConfig`.

## Related

- [cli.md](./cli.md): where `Setup` runs and refusals exit.
- [s3store.md](./s3store.md): how `Region`, `Endpoint`, `ForcePathStyle` and credentials reach the SDK.
- [security.md](./security.md): redaction and path rules.
- [running.md, Configuration](./running.md#configuration): operator reference.
- [AGENTS.md, Key Flags](../AGENTS.md#key-flags).
