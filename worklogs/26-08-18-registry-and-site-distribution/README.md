# MCP Registry and site-distribution worklog

**Status:** In Progress

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§22 release and distribution; [`../../docs/releasing.md`](../../docs/releasing.md)

## Objective

Make slivingdoc ready for automatic publication in the official MCP Registry,
then repair the live-site distribution surface: social media cards and the
SeaweedFS evaluation reference.

## Status board

| Phase | Status | Summary |
| --- | --- | --- |
| [1. MCP Registry manifest](phase-1-mcp-registry.md) | Complete | Added verified npm metadata, a Registry card, release-version synchronization, and automatic OIDC publication. |
| [2. Website distribution surface](phase-2-website.md) | Not Started | Website source/deployment configuration is unavailable; update social-card metadata and the SeaweedFS reference when it is supplied. |
| [3. Validation](phase-3-validation.md) | In Progress | Go and direct Node tests pass; the released Registry entry is active, while the website source remains unavailable. |

## Strategy

### Execution order

Phase 1 is independent and comes first. Phase 2 cannot start until the source
repository or deployment path for `slivingdoc.dev` is available; the live site
is not in this repository or the public GitHub repositories inspected on
2026-08-18. Phase 3 follows every changed source tree. The existing release
workflow publishes npm and then its matching Registry card.

### Shared invariants

1. The official Registry’s npm ownership verification requires the published
   package’s `mcpName` to match `server.json`; a release containing the new
   metadata must exist before registry publication.
2. `server.json` describes the real launcher invocation: `npx -y slivingdoc
   serve`, stdio transport, and `SLIVINGDOC_BUCKET` as required configuration.
3. No AWS key is stored in registry metadata, documentation, site source, or
   test output. The existing AWS credential chain remains the only credential
   path.
4. Registry publishing is an OIDC-authenticated release-workflow action that
   follows npm publication. Website deployment remains an external state change
   owned by the maintainer.
5. The live site must refer to the current `examples/seaweedfs/` walkthrough,
   never the removed MinIO path.

### Review severity

| Severity | Meaning | Phase effect |
| --- | --- | --- |
| Critical | Credential exposure or an installation command that cannot start the server. | Reopen and block publication. |
| Major | Registry metadata cannot validate or a live onboarding link is stale. | Reopen the affected phase. |
| Minor | Copy or visual quality issue without a broken install path. | Record and repair where practical. |

## Decisions

| Date | Decision | Reason |
| --- | --- | --- |
| 2026-08-18 | Publish as `io.github.baalimago/slivingdoc`. | GitHub authentication owns the `io.github.baalimago/*` namespace, and it matches the public repository. |
| 2026-08-18 | Treat `server.json` as release-versioned source and update it from `make release`. | The Registry requires its package version to be specific and to match npm ownership metadata. |
| 2026-08-18 | Require only `SLIVINGDOC_BUCKET` in Registry configuration. | Credentials may arrive through an AWS profile, ambient role, SSO, or optional static variables; making a key mandatory would reject valid secure setups. |
| 2026-08-18 | Publish the Registry card from the existing release workflow after npm publication. | GitHub OIDC authenticates the same release workflow without a stored token, and the `needs` edge preserves npm-first verification. |

## Session journal

| Date | Entry |
| --- | --- |
| 2026-08-18 | Verified the official Registry’s npm requirements: `mcpName` must match `server.json`; the card needs a concrete npm version and stdio transport. Confirmed the launcher needs the `serve` package argument and can take the required bucket through `SLIVINGDOC_BUCKET`. |
| 2026-08-18 | Inspected `slivingdoc.dev`: it is a static site serving `main.js` and `styles.css`; the live page contains a stale `examples/minio` link and no `og:image`. Its source is absent from this workspace and from the owner’s public GitHub repository list, so no safe website edit can occur yet. |
| 2026-08-18 | The mandatory QA sweep exposed an environment-isolation defect unrelated to registry metadata: the desktop process exports `NO_COLOR=1`, and the pseudo-terminal scenario inherited it despite asserting the default coloured branch. `sanitizedEnv` now removes `NO_COLOR`; scenarios that need it set it explicitly through `overrideEnv`. |
| 2026-08-18 | Added the Registry card, npm `mcpName`, release-version synchronization, and a release-layer consistency test. The complete Go suite passed with 83.6% coverage. The desktop environment has no `npm` executable, so literal `make qa` stops at that target; the identical Node test command passed 35 of 35 tests. |
| 2026-08-18 | Published `io.github.baalimago/slivingdoc` version `0.1.5` to the official Registry. Added a `publish-mcp` job to the existing release workflow; it validates and publishes the card only after the corresponding npm publish succeeds. |
| 2026-08-18 | Moved mandatory SeaweedFS fixture preparation into the integration package's `TestMain`, before Go starts the fixed per-package scenario timer. The full unmodified Go gate now passes without changing its timeout, count, race detector, or coverage floor. |

## Feedback index

No reviews recorded.
