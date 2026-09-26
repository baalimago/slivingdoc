# Releasing

How a maintainer cuts a release and how one `v<semver>` tag becomes native GitHub release assets, an npm launcher version, and an official MCP Registry card, in that order. It answers "how do I ship, and what guarantees the three publications cannot drift or run out of order?" Users install through npm or a GitHub release; see the [README](../README.md).

Read this when: cutting a release, changing `scripts/release.go`, `release.yml`, the npm publication gate, `server.json`, or the version fields.

## Key files

| File | Purpose |
|------|---------|
| `scripts/release.go` | `make release`: `run` (branch/dirty checks, `recentTags`, prompt, `tagExists`, `bumpPackageVersion`, `bumpRegistryVersions`, npm tests, commit, annotated tag, push) |
| `Makefile` | `release: ./scripts/release.go` |
| `.github/workflows/release.yml` | `release` job (reusable `simple-go-pipeline` at a pinned SHA: build matrix, dependency checks, smoke, `SHA256SUMS`, `NOTICE`, GitHub release), `publish-npm`, `publish-mcp` |
| `npm/slivingdoc/package.json` | Launcher version (must equal the tag), `mcpName: io.github.baalimago/slivingdoc`, `prepublishOnly` |
| `npm/slivingdoc/scripts/check-release.mjs` | Publication gate: every required asset, `SHA256SUMS` and `NOTICE` present in the GitHub release |
| `server.json` | Official MCP Registry card; its versions move with `package.json` |
| `scripts/check-release-ref.sh` | Reusable pipeline reference must be an immutable 40-hex SHA |
| `scripts/make-sha256sums.sh` | Checksum file of the uploaded bytes |
| `release_test.go` | `TestReleaseWorkflowReference`, `TestMCPRegistryManifest`, `TestMCPRegistryPublishWorkflow`, `TestReleaseChecksumGrammar`, `TestReleaseBinary` |

## Flow

```text
make release → scripts/release.go:run
  require repo root, branch master, clean tree
  print current launcher version and the 5 most recent tags
  prompt version (semver, != current, tag absent locally and on origin) and optional description
  bumpPackageVersion(npm/slivingdoc/package.json); bumpRegistryVersions(server.json)
  npm test --prefix npm/slivingdoc
  git add both; git commit -m "chore: release v<ver>" [-m desc]; git tag -a v<ver>
  git push origin master; git push origin v<ver>

push tag v* → .github/workflows/release.yml
  release      → simple-go-pipeline release.yml@<sha>
                   per target: build libgit2 → make build BIN=$TARGET_BINARY → check-deps → smoke "version"
                   assemble: make-sha256sums.sh dist/* + NOTICE → gh release create (all assets)
  publish-npm  (needs release, tag only)
                   npm >= 11.5.1 check → package.json version == tag
                   dist-tag = next if prerelease else latest
                   npm publish --tag <dist-tag>   # prepublishOnly → check-release.mjs: HEAD every asset, any missing → exit 1
  publish-mcp  (needs publish-npm, tag only)
                   install mcp-publisher → validate server.json → login github-oidc → publish server.json
```

## Behavior

**What a release contains.** Native binaries for linux/amd64, linux/arm (32-bit ARMv7, Raspberry Pi OS armhf), linux/arm64, darwin/amd64, darwin/arm64 and windows/amd64, named `slivingdoc-v<semver>-<os>-<arch>[.exe]`, plus the strict `SHA256SUMS` and the license `NOTICE`. Targets and build details are in [build.md](./build.md).

**Cutting a release.** `make release` is the only supported way; it needs Go at `/usr/local/go/bin/go` (the script's shebang-style first line). It refuses off master or with a dirty tree, rejects a non-semver version, the current version, or an existing tag, bumps `npm/slivingdoc/package.json` and `server.json` together, runs the launcher tests, commits `chore: release v<version>`, creates the annotated tag, and pushes branch and tag. Do not edit either release version by hand.

**Version injection.** The workflow passes `VERSION="${RELEASE_VERSION#v}"` to `make build`, which sets `app.Version` through the linker; `slivingdoc version` prints it and the smoke test checks it.

**Pinned pipeline.** `release.yml` references `baalimago/simple-go-pipeline/.github/workflows/release.yml` by an immutable commit SHA, never a branch or moving tag; `check-release-ref.sh` and `TestReleaseWorkflowReference` enforce it. Bump it only with a reviewed commit.

**Publication order.** One tag publishes three things strictly in sequence:

1. The GitHub release, created by the reusable job only after every target build, dependency check and smoke test passed and every asset is uploaded.
2. The npm launcher (`publish-npm`, `needs: release`). The job fails unless `package.json` equals the tag version. The ordering itself comes from `needs: release`. As a guard, `npm publish` runs `prepublishOnly` (`check-release.mjs`), which sends one HEAD request per required asset (every target binary, `SHA256SUMS`, `NOTICE`) and exits 1 if any is missing. It does not wait for the release to complete; only transient network failures are retried (up to 5 attempts with backoff). A prerelease version (a `-` in the core version) publishes to the `next` dist-tag, a stable one to `latest`.
3. The MCP Registry card (`publish-mcp`, `needs: publish-npm`). The Registry verifies the npm package's `mcpName` and version, so the card is submitted only once that exact package is public.

**Credentials.** None are stored. npm uses trusted publishing (OIDC): npmjs.com accepts publishes of `slivingdoc` only from `release.yml` of `baalimago/slivingdoc`, the job has `id-token: write`, and npm CLI 11.5.1 or later is required (the job pins Node 24 for that; the runtime floor stays Node 22). No `--provenance` flag is passed; provenance relies on npm trusted publishing's default attestation for a public repository. The Registry job exchanges the job's GitHub OIDC identity for a Registry token whose grant covers `io.github.baalimago/*`.

**Recovering a failed publish.** For a transient GitHub or Registry failure, re-run only the failed job (for example `Publish MCP Registry card`) from the release workflow. Do not publish the card or the package by hand as a normal release step.

## Gotchas

- `TestMCPRegistryManifest` checks that `server.json` matches `package.json`; `publish-npm` checks that `package.json` matches the tag.
- `release.yml` has `permissions: contents: write` at the top; the publish jobs narrow it to `contents: read` plus `id-token: write`.
- The pipeline appends `dist/*` to the checksum command; passing `dist` yourself makes `sha256sum` fail on a directory.
- `tagExists` treats an unreachable origin as "absent" with a warning; the push fails later if the tag does exist remotely.

## Related

- [build.md](./build.md): targets, toolchains, dependency baselines, the npm launcher.
- [testing.md](./testing.md): the release test layer.
- [running.md, Operational ownership](./running.md#operational-ownership).
