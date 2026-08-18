# Phase 1 — MCP Registry manifest

**Status:** Complete

**Worklog:** [README](README.md)

## Goal

Prepare a registry-valid, release-synchronized npm/stdio manifest that the
maintainer can publish after the next npm release completes.

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
- Document the post-release `mcp-publisher validate`, login, and publish flow
  in `docs/releasing.md`.
- Add a release-level test proving version/name/install-command parity.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- |
| `make release` | release script, npm package, server card | Both manifests receive the selected version and are committed together. | Stage both metadata files. | No Registry publication. |
| `mcp-publisher validate server.json` | Official Registry schema | The card is schema-valid. | Read local metadata only. | No public registry mutation. |
| Registry install | npm/npx, slivingdoc CLI | Client invokes `npx -y slivingdoc serve` with a bucket configuration. | Start stdio server. | Expose or require static AWS keys. |

## Acceptance criteria

- [x] The package has the matching `mcpName`.
- [x] `server.json` represents the npm stdio invocation and only requires the bucket.
- [x] A release bump preserves package/card name and version parity.
- [x] `docs/releasing.md` gives the maintainer the correct publication order.
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

## Review findings

No reviews recorded.
