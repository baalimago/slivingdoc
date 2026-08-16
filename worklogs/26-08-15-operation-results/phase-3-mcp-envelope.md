# Phase 3 — MCP result envelope

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§2 result envelope

## Goal

Return a structured JSON success result over MCP that parallels the stable
error envelope, and leave the error envelope unchanged.

## Specification

Add to `internal/mcp` a success shape:

```go
type SuccessInfo struct {
    Code         string       `json:"code"`         // always "OK"
    Generation   uint64       `json:"generation"`
    FilesChanged int          `json:"filesChanged"`
    Insertions   int          `json:"insertions"`
    Deletions    int          `json:"deletions"`
    Files        []ChangeFile `json:"files"`
}

type ChangeFile struct {
    Path       string `json:"path"`
    Insertions int    `json:"insertions"`
    Deletions  int    `json:"deletions"`
}
```

Map `notebook.Result` into `SuccessInfo`: `FilesChanged` is `len(Stat.Files)`,
totals come from `Stat.Insertions` / `Stat.Deletions`, and `Files` is always
present (empty for a no-op). All paths are the same normalized internal slash
form used by error `files`.

Change the success envelope in `mcp/server.go`:

- The success tool result keeps `IsError=false`, keeps one candid text item
  `OK`, and sets `StructuredContent` to the `SuccessInfo`.
- The error tool result is unchanged: `IsError=true`, one candid text item
  equal to the message, `StructuredContent` equal to the existing `ToolError`.

`mcp.Service` already carries the result from Phase 2; the handler now consumes
it through a `MapSuccess(notebook.Result) *SuccessInfo` helper instead of
discarding it. No redaction is needed for these fields (generation and change
counts are not secrets), but paths must pass through the same normalized form
as error files.

## Integration contract

| Trigger                | Collaborators        | Observable result                                        | Required side effect                | Prohibited side effect        |
| ---------------------- | -------------------- | -------------------------------------------------------- | ----------------------------------- | ----------------------------- |
| `notes_pull` success   | Fake notebook result | One `OK` text item plus `SuccessInfo` structured content | Service result reaches the envelope | No `isError`, no secret field |
| `notes_commit` success | Fake notebook result | Same envelope as pull                                    | Result reaches the envelope         | No commit ID or pack key      |
| No-op commit           | Fake notebook result | `filesChanged: 0`, empty `files`, remote generation      | None                                | No error envelope             |
| Conflict               | Fake notebook error  | Existing `ToolError` envelope unchanged                  | Paths and ranges preserved          | No success shape on error     |

## Acceptance criteria

- [x] Successful pull and commit return `code: "OK"` with `generation`,
      `filesChanged`, `insertions`, `deletions`, and a `files` array.
      Tests: `TestPullSuccessEnvelope` / `TestCommitSuccessEnvelope` in
      `internal/mcp/server_test.go` assert the exact `SuccessInfo` of the
      documented example after its round trip through the SDK envelope;
      `TestPullSuccessReturnsOK` / `TestCommitSuccessReturnsOK` keep the
      request-path and message assertions on the same envelope.
- [x] `files` entries carry `path`, `insertions`, and `deletions` in normalized
      relative form.
      Test: `TestPullSuccessEnvelope` asserts `notes/a.md`,
      `notes/c.md`, `archive/old.md` (the slash-form internal paths also used
      by error files) with their exact counts.
- [x] A no-op commit returns `filesChanged: 0` and an empty `files` array.
      Test: `TestNoOpCommitReturnsEmptyStatEnvelope` asserts the zero-stat
      `SuccessInfo` with the remote generation, and `assertOKResult` proves
      `files` is non-nil even when empty.
- [x] The success text item remains exactly `OK`.
      Test: the text-item assertion inside `assertOKResult`, used by every
      success test above, and the black-box `Harness.assertOK` /
      `assertProcessCallOK` keep proving the literal `OK` text item.
- [x] The error envelope's JSON fields are byte-for-byte unchanged.
      Test: `ToolError` is untouched; `TestConflictDataSurvivesSDKEnvelope`
      and the `TestMapError*` family in `errors_test.go` still decode the
      error into the same `code` / `retryable` / `message` / `files` /
      `recovery` shape, and the new success-only-field scan proves no
      success key leaks into an error object.
- [x] No success or error field contains a Git ID, pack key, S3 key, private
      path, or credential.
      Tests: `TestCommitSuccessEnvelope` scans the marshaled success
      envelope for `packs/`, `probe/`, `AKIA`, and 40-hex Git IDs;
      `TestScenarioResultShape` (integrationtest) extends the same scan to
      the structured content of both tools; the existing `TestRedact` and
      `TestErrorMessagesAreRedacted` cover the error side.

Each checked criterion cites its unit test in `internal/mcp`; the updated
black-box scenario assertions land in Phase 5.

## Error coverage

| Failure                                                     | Expected outcome                                                | Required test |
| ----------------------------------------------------------- | --------------------------------------------------------------- | ------------- |
| Notebook returns an error                                   | Existing `ToolError` envelope, success shape absent             | MCP unit test |
| Notebook returns a zero result with no error (contract bug) | `code: "OK"` with zero generation and empty stat, never a panic | MCP unit test |

Covered:

- `TestConflictDataSurvivesSDKEnvelope` proves an error result carries the
  unchanged `ToolError` envelope and adds a scan proving no success-only
  field (`generation`, `filesChanged`, `insertions`, `deletions`) appears in
  the marshaled error object.
- `TestZeroResultWithNoErrorNeverPanics` drives the zero `notebook.Result`
  (the fake's default) through a clean call and asserts `code: "OK"` with
  generation 0 and an empty stat instead of a panic or protocol error.

## Implementation notes

- `internal/mcp/success.go` defines `SuccessInfo`, `ChangeFile`, and
  `MapSuccess(notebook.Result) *SuccessInfo`. `MapSuccess` maps the
  `git.DiffStat` fields directly: the diffstat paths are already the
  normalized slash-form relative paths used by error files, and the stat is
  sorted by path by `git.DiffSnapshots`, so no re-sort or redaction is
  needed in the mapping. `Files` is always allocated non-nil, empty for a
  no-op.
- `mcp/server.go` replaces `okResult()` with `successResult(result
  notebook.Result)`: the success envelope keeps `IsError` false and one
  text item `OK`, and now sets `StructuredContent` to `MapSuccess(result)`.
  Both handlers capture the `(notebook.Result, error)` return and feed the
  result only on the clean path; `resultFor(err)` still maps the error.
- The existing success-envelope assertion helpers that encoded the old
  "no structured content" contract were updated to the new shape in the
  same change so the suite stays green: `assertOKResult` (mcp),
  `resultOK` / `assertProcessOK` (app), and `Harness.assertOK` /
  `assertProcessCallOK` (integrationtest) now assert one `OK` text item
  plus a `SuccessInfo` with `code: "OK"` and the always-present `files`
  key. The exact per-scenario generation and diffstat expectations remain
  Phase 5's `CallExpectation` work; `TestScenarioResultShape` gained the
  structured-content leak scan so its "no internal value" guarantee stays
  true now that success carries data.
- The `fakeService` in `internal/mcp/server_test.go` gains a canned
  `result` so the envelope tests inject the documented three-file stat
  without a notebook.
- Gate before: `go build ./...` OK; `go test ./internal/mcp/...` OK.
  Gate after: `go test ./internal/mcp/...`, `./internal/app/...`,
  `./internal/integrationtest/...` OK; `go test ./... -race -count=3
  -timeout=30s -coverpkg=./...` OK (aggregate coverage 83.6%, floor 70%);
  `go vet ./...`, `staticcheck ./...`, `go fix -diff ./...`, `gofumpt -l`
  (nothing listed), and `dupl -t 80 .` (0 clone groups) clean; `npm test
  --prefix npm/slivingdoc` OK (35/35).

## Review findings

No reviews recorded.
