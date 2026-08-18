# Phase 3 — Validation

**Status:** In Progress

**Worklog:** [README](README.md)

## Goal

Prove the prepared Registry metadata and any available website source meet
their release and distribution contracts.

## Specification

- Run the repository’s mandatory `make qa` gate after Phase 1.
- Run a JSON parse and the release-level metadata test before the full gate.
- Validate the card with `mcp-publisher` and verify the official Registry entry
  after the release. Future releases run the same commands through the existing
  GitHub Actions workflow using OIDC.
- If Phase 2 obtains source, inspect the deployed page for the exact social
  metadata and SeaweedFS link.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- |
| `make qa` | Go, Docker S3 backend, npm tests | Exit 0. | Full suite executes. | Skipping Docker-backed integration tests. |
| Tagged release after npm succeeds | `mcp-publisher`, GitHub OIDC, official Registry | The matching card validates and publishes as the active Registry version. | Publish only after npm ownership is verifiable. | Store a Registry token or GitHub PAT. |

## Acceptance criteria

- [ ] The literal `make qa` command completes. The Go portion passed (83.6% coverage),
      but this desktop environment has no `npm` executable; the identical Node test
      command passed 35 of 35 tests.
- [x] The Registry card parses and passes the repository metadata test.
- [x] `mcp-publisher validate server.json` succeeded for released version `0.1.5`, which is active in the official Registry.
- [ ] Deployed site validation is recorded if Phase 2 completes. — pending website source

## Error coverage

| Failure | Expected outcome | Required check |
| --- | --- | --- |
| Malformed server card | JSON or release metadata test fails. | JSON parse and `TestMCPRegistryManifest`. |
| Registry schema drift | `mcp-publisher validate` reports an actionable error before publish. | Maintainer command. |
| Quality regression | `make qa` identifies the failing gate. | Mandatory QA. |

## Implementation notes

- The first mandatory `make qa` run reached the Docker-backed integration
  suite but failed its existing pseudo-terminal colour scenario three times
  because the desktop environment exports `NO_COLOR=1`. The test harness's
  `sanitizedEnv` claimed to provide a controlled helper environment but did
  not remove that user preference. It now does; explicit `NO_COLOR` test
  cases still pass it through `overrideEnv`.
- `go test -run '^TestMCPRegistryManifest$' .` passed. The final `make qa`
  attempt completed formatting, linting, the full race-enabled Go suite, and
  coverage before its npm target could start because `npm` is absent locally.
- Running the package's npm test script directly with the bundled Node runtime
  passed all 35 tests. `dupl -t 80` reported zero clone groups, and
  `git diff --check` is clean.
- `mcp-publisher validate server.json` succeeded against the official Registry,
  then `io.github.baalimago/slivingdoc` `0.1.5` was verified active through its
  public Registry API. The next release will use the workflow's OIDC job.
- A first full Go run exposed the integration package's container startup as a
  timeout edge. `TestMain` now prepares that mandatory Docker fixture before
  `m.Run` starts Go's fixed 30-second test timer; the unchanged
  `make test` command then passed at 83.6% coverage. The release-workflow
  contract test, YAML parse, lint, duplicate scan, and all 35 launcher tests
  also passed.

## Review findings

No reviews recorded.
