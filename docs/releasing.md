# Releasing

This document is for maintainers. Users install slivingdoc through npm
or a GitHub release. See the [README](../README.md).

## What a release contains

A release tag `v<semver>` publishes native binaries for linux/amd64,
linux/arm (32-bit ARMv7, including Raspberry Pi OS armhf), linux/arm64,
darwin/amd64, darwin/arm64, and windows/amd64 (architecture section 21),
together with a strict `SHA256SUMS` file and the license `NOTICE`. Asset names
follow `slivingdoc-v<semver>-<os>-<arch>`, with `.exe` on Windows.

The npm package `slivingdoc` is a launcher. It selects the asset for
its exact version and platform, verifies the published SHA-256, caches
the verified bytes under the npm cache, and forwards stdio to the
child. Unsupported platforms fail before any download with an
actionable error.

## Cut a release

```text
make release
```

The command prints the recent releases, prompts for the new version and
a tag description, bumps `npm/slivingdoc/package.json`, commits the
bump, annotates the `v<version>` tag, and pushes branch and tag. The
tag push runs the release workflow.

## Publication order

`npm run check-release` (wired into `prepublishOnly`) fails until the
GitHub release for the package version contains every required
artifact. npm publication can never precede the complete GitHub
release.

The same `v<semver>` tag publishes the npm launcher automatically after
the GitHub release is complete. A prerelease tag (for example
`v1.2.3-rc1`) publishes to the `next` dist-tag. A stable tag publishes
to `latest`.

Publication uses npm trusted publishing (OIDC). The repository stores
no npm token, and each publish carries a provenance attestation.
