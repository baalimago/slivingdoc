# Phase 5 — Scenarios, docs, quality gate

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§2, §10, §11, §12, §17

## Goal

Lock the new result contract into the black-box scenario suite, update the
contract and running docs, and pass the full quality gate.

## Specification

Update the black-box scenarios in `internal/integrationtest`:

- Replace the success-envelope assertion "one text item exactly `OK` and no
  structured content" with the new success envelope: one `OK` text item plus
  `StructuredContent` matching `SuccessInfo`.
- `assertOK` and `CallExpectation` gain a structured-content expectation for
  success (`Generation`, `FilesChanged`, `Insertions`, `Deletions`, `Files`).
- Add scenarios: pull delta stat, commit increment stat, no-op commit (empty
  stat), conflict still returns the unchanged error envelope, and CLI success
  and conflict render correctly with plain (piped) output.
- Add a colour scenario at the process level or unit level proving ANSI is
  present on a terminal and absent when piped or `NO_COLOR` is set.

Update the documentation in the same change:

- `docs/slivingdoc-v1.md` §2: success result is now a structured `SuccessInfo`
  plus `OK` text; the "no structured content" sentence is replaced.
- `docs/running.md`: describe the richer CLI output and `NO_COLOR` colour
  gating.
- `cmd/pull` and `cmd/commit` help text (already updated in Phase 4) stay in
  sync with the docs.

Run the full gate: `make qa` (format, vet, staticcheck, `go fix -diff`, `make
test` with the unmodified race/count/timeout/coverage flags, `npm test`, and
the dupl signal). Record exact commands and outcomes in the implementation
notes.

## Integration contract

| Trigger                           | Collaborators     | Observable result                                 | Required side effect     | Prohibited side effect    |
| --------------------------------- | ----------------- | ------------------------------------------------- | ------------------------ | ------------------------- |
| Pull and commit over a fake store | Black-box harness | Structured success envelope with the correct stat | L and S3 state as before | No secret in any field    |
| No-op commit                      | Black-box harness | Empty success stat                                | No remote mutation       | No error envelope         |
| Conflict                          | Black-box harness | Unchanged `CONTENT_CONFLICT` envelope             | Markers materialized     | No success shape on error |
| CLI pull/commit                   | Helper process    | Plain `OK`-prefixed diffstat, exit zero           | None                     | No ANSI on a pipe         |

## Acceptance criteria

- [x] Every scenario in the suite passes with the new success envelope. —
      `go test ./internal/integrationtest/ -race -count=3 -timeout=30s`
      passes (92 scenarios, including the MinIO-backed rows); the full
      module gate is below.
- [x] The documented MCP and CLI result shapes match the implemented
      output. — `TestScenarioPullDeltaStat` / `TestScenarioCommitIncrementStat`
      pin the exact `SuccessInfo` values; `TestScenarioCLIPullCommitRoundTrip`
      pins both CLI reports byte-for-byte; `TestScenarioCLIColourOnTerminal`
      pins the exact ANSI report on a PTY; §2 and `docs/running.md` carry
      the same shapes.
- [x] `make qa` passes unedited: format, vet, staticcheck, `go fix -diff`,
      `make test` (race, count 3, 30s, 70% coverage floor), `npm test`,
      dupl signal reviewed per the duplication policy. — exact commands and
      outcomes below.
- [x] No scenario asserts the removed "no structured content" invariant. —
      `grep` finds the old invariant only in historical worklogs; every
      `StructuredContent == nil` check in the suite is a failure branch
      asserting the new "structured content present" contract.

Diffstat-computation-failure coverage lives at the notebook unit layer
(`TestCommitDiffStatReadFailureMapsToIntegrity` in commit_test.go and its
pull twin), because the failure is a repository read after the merge that
no store-seam fault can reach from outside the black box; the scenarios
cover the observable taxonomy through every other error row.

Each checked criterion cites the scenario or command run that proves it.

## Error coverage

| Failure                               | Expected outcome                       | Required test                                                          |
| ------------------------------------- | -------------------------------------- | --------------------------------------------------------------------- |
| Diffstat computation error            | Existing error path, zero success stat | Notebook unit tests (`TestCommitDiffStatReadFailureMapsToIntegrity` and its pull twin) |
| Colour in a non-terminal destination  | No ANSI in captured output             | Scenario (`runCLIOK` / `TestScenarioCLIMarkerConflictReport`)          |
| Docs drift from the implemented shape | Failing scenario or manual cross-check | `make qa` + scenario                                                   |

## Implementation notes

- `CallExpectation` gained `Success *SuccessExpectation` pinning the exact
  structured success envelope (generation, totals, ordered per-file stat);
  nil asserts the shape only. `assertOK` was refactored onto a shared
  `successInfo` decoder, `assertEnvelope` wires the new expectation, and
  `decodeEnvelope` now scans every error envelope for the success-only
  field names so the two shapes can never be confused.
- New scenarios: `TestScenarioPullDeltaStat` (remote advance + local edit,
  exact generation 3 / 2 files / 2 insertions / 1 deletion),
  `TestScenarioCommitIncrementStat` (first publication and a mixed
  modify/add increment). `TestScenarioPullFirstPull` and
  `TestScenarioCommitNoChange` now pin the empty-stat envelope (generation
  0 and 1 respectively).
- CLI render: `runCLIOK` asserts the status token, the file-detail lines,
  the totals trailer as the final line, and the absence of ANSI on a pipe;
  `TestScenarioCLIPullCommitRoundTrip` pins both reports byte-exact via
  `runCLIExact`; `TestScenarioCLIMarkerConflictReport` also asserts no
  ANSI. The conflict tests keep asserting the unchanged error envelope,
  and `decodeEnvelope` proves every error carries no success shape.
- Colour: new Linux process-level scenario `TestScenarioCLIColourOnTerminal`
  spawns the one-shot CLI with its stdout on a real pseudo-terminal
  (x/sys ioctls, OPOST disabled for byte-exact capture) and proves the
  exact ANSI report on a terminal; `runCLITTY` reads the PTY master
  concurrently and treats the slave-close EIO as EOF. The piped scenarios
  prove the plain side of the gate, and the unit-level colour gate tests
  prove that `NO_COLOR` disables a terminal.
- Docs: `docs/slivingdoc-v1.md` §2 now carries the structured `SuccessInfo`
  envelope, the diff-semantics and line-counting rules, the unified CLI
  report, and the `NO_COLOR` gate; the "no structured content" sentence is
  replaced. `docs/running.md` gained the diff-semantics paragraph. The
  unreferenced `docs/output.md` draft was folded into §2 and removed.

Gate before: `go test ./internal/integrationtest/ -run <targeted>` OK;
`go build ./...` OK; `go vet ./internal/integrationtest/...` OK.

Gate after: `go test ./internal/integrationtest/ ...` OK; full
`go test ./... -race -count=3 -timeout=30s -coverpkg=./...` OK (aggregate
coverage 83.6%, floor 70%); `make qa` OK; `go vet`, `staticcheck`,
`go fix -diff`, `gofumpt -l` (nothing listed), and `dupl -t 80 .`
(0 clone groups) clean; `npm test --prefix npm/slivingdoc` OK (35/35).
Worker session: 2026-08-15 slivingdoc phase-5 session (worker: imago,
cwd /home/imago/Projects/public/slivingdoc).

## Review findings

No reviews recorded.
