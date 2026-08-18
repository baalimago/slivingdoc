# Phase 1 — MCP Registry manifest

**Status:** Complete

**Worklog:** [README](README.md)

## Goal

Prepare a registry-valid, release-synchronized npm/stdio manifest that the
existing release workflow can publish after the matching npm release completes.

## Specification

- Add `mcpName: io.github.baalimago/slivingdoc` to
  `npm/slivingdoc/package.json`.
- Add root `server.json` using the official 2025-12-11 schema, with the GitHub
  namespace, the public repository and website URLs, one npm package, `npx -y`
  runtime, a `serve` package argument, stdio transport, and non-secret
  configuration fields. `SLIVINGDOC_BUCKET` is required.
- Make `scripts/release.go` update and stage the package and registry version
  fields together. The package has one version field; the card has the server
  and npm-package version fields.
- Extend the existing `release.yml` workflow with a GitHub OIDC Registry job
  that depends on `publish-npm`, then document that automatic flow.
- Add a release-level test proving version/name/install-command parity.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- |
| `make release` | release script, npm package, server card, release workflow | Both manifests receive the selected version and are committed together; the later Registry job publishes it after npm succeeds. | Stage both metadata files and submit the release card once. | Publish before npm can verify it. |
| `mcp-publisher validate server.json` | Official Registry schema | The card is schema-valid. | Read local metadata only. | No public registry mutation. |
| Registry install | npm/npx, slivingdoc CLI | Client invokes `npx -y slivingdoc serve` with a bucket configuration. | Start stdio server. | Expose or require static AWS keys. |

## Acceptance criteria

- [x] The package has the matching `mcpName`.
- [x] `server.json` represents the npm stdio invocation and only requires the bucket.
- [x] A release bump preserves package/card name and version parity.
- [x] The existing release workflow validates and publishes the card after npm.
- [x] `docs/releasing.md` gives the maintainer the automatic publication flow.
- [x] A repeatable repository test proves the metadata contract.

## Error coverage

| Failure | Expected outcome | Required check |
| --- | --- | --- |
| Published npm package lacks matching `mcpName` | Registry rejects ownership verification before publication. | Post-release `mcp-publisher validate`/publish workflow. |
| Release script updates only one manifest | Repository parity test fails on the next quality gate. | `TestMCPRegistryManifest`. |
| Client lacks an AWS profile or ambient identity | Startup returns its existing redacted configuration/probe error. | Existing application/integration tests; no new credential path. |

## Implementation notes

- Added `server.json` for `io.github.baalimago/slivingdoc`, configured for stdio
  through `npx -y slivingdoc serve`.
- Added `mcpName` to the npm package and made `make release` bump the npm and
  Registry manifest versions in one release commit.
- Added `TestMCPRegistryManifest`; it verifies name, version, package invocation,
  transport, and the required bucket setting agree across the two manifests.
- The Registry metadata will be available in the next npm release, not
  retroactively in npm `0.1.4`.
- The `publish-mcp` job uses GitHub OIDC with no stored Registry token or
  personal access token. It first validates `server.json`, then publishes it
  after `publish-npm` completes.

## Review findings

No reviews recorded.
