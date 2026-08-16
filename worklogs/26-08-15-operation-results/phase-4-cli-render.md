# Phase 4 — CLI render and colour

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§2 CLI report, §17 flags and environment

## Goal

Render the unified result envelope as a coloured, human-readable CLI report
for both success and conflict, gated on a real terminal and `NO_COLOR`.

## Specification

Move the CLI text rendering of the result into `internal/app` so success and
error share one skeleton. `mcp.ToolError` remains the structured error value;
`app` renders it for the CLI.

`app.Report` becomes:

```go
func Report(out io.Writer, res notebook.Result, err error, env []string) error
```

Rendering rules (normative, from the README):

- Success status token `OK` (green), summary `generation <g>` (cyan), then one
  indented line per changed file `path  +I -D` (insertions green, deletions
  red, zero-count side omitted), then the trailer
  `N files changed, I insertions(+), D deletions(-)`.
- Error status token `<CODE>` (red), summary the candid message, then one
  indented line per conflicted file `path: lines a-b` (path yellow) or a bare
  `path`, then the trailer `retryable: <bool>` and `recovery: ...` when
  present.
- `OK` stays the first token of the success output for script compatibility.
- The returned error remains `nil` on success and the terse category on a
  domain error, exactly as today.

Colour:

- Add a small ANSI helper in `internal/app` (not `ancli`, which writes to
  hardcoded streams) with green, red, yellow, and cyan codes.
- Colour is enabled only when `out` is a `*os.File` whose mode is a character
  device (a real terminal) and `NO_COLOR` is unset or empty, using the
  existing `environ` helper to read the environment.
- A non-terminal or any non-empty `NO_COLOR` value emits plain text.

`cmd/pull` and `cmd/commit` pass `c.opts.Env` to `app.Report`. Help text in
both commands and `docs/running.md` are updated to describe the richer output
and the `NO_COLOR` behaviour.

## Integration contract

| Trigger                       | Collaborators                       | Observable result                           | Required side effect | Prohibited side effect |
| ----------------------------- | ----------------------------------- | ------------------------------------------- | -------------------- | ---------------------- |
| Successful pull to a terminal | Real stdout, env without `NO_COLOR` | Coloured status, generation, and file lines | Exit zero            | No ANSI when piped     |
| Successful commit to a pipe   | Captured stdout                     | Plain `OK`-prefixed diffstat, no ANSI       | Exit zero            | No colour escapes      |
| Conflict to a terminal        | Real stdout                         | Red status, yellow file lines               | Exit nonzero         | No success trailer     |
| `NO_COLOR=1`                  | Any writer                          | Plain text even on a terminal               | Exit per result      | No ANSI escapes        |

## Acceptance criteria

- [x] Success output is prefixed with `OK` and carries the generation, per-file
      counts, and the summary trailer.
      Tests: `TestReport` "success writes the OK-prefixed report" asserts the
      exact plain report of the documented three-file example (generation 18,
      `+1 -1` / `+2` / `-3` lines, totals trailer); `TestWriteSuccessColoured`
      asserts the same report with colour; `TestWriteSuccessEmptyStat` asserts
      the no-op form (status line plus zero totals).
- [x] Conflict output uses the same status/detail/trailer skeleton.
      Tests: `TestReport` "domain error writes the category report" asserts
      status + message, one indented line per conflicted file (ranged and
      rangeless), and the `retryable` trailer; `TestWriteErrorColoured` adds
      the recovery trailer.
- [x] Insertions are green, deletions red, status tokens coloured, and conflict
      paths yellow on a terminal.
      Tests: `TestWriteSuccessColoured` (green `OK` and `+` counts, red `-`
      counts, cyan generation, zero side omitted), `TestWriteErrorColoured`
      (red category, yellow paths), and `TestPainter` pins each SGR wrapper
      on and off.
- [x] Piped or non-`*os.File` output contains no ANSI escapes.
      Tests: `TestReport` "piped output stays plain" runs a real `os.Pipe` and
      asserts no `\x1b[`; `TestColourEnabled` proves a pipe and a buffer are
      never colour-capable; the Phase 5 CLI scenario `runCLIOK` scans the
      captured stdout of a spawned process for escapes.
- [x] Any non-empty `NO_COLOR` disables colour even on a terminal.
      Tests: `TestColourEnabled` (character device + `NO_COLOR=1` is plain,
      empty `NO_COLOR=` keeps colour) and `TestReport` "NO_COLOR keeps the
      report plain".
- [x] The domain-error exit contract (nonzero, terse category) is unchanged.
      Test: `TestReport` "domain error writes the category report" returns
      the terse `CONTENT_CONFLICT` category, which the command router echoes
      as the process exit; `cmd/pull` and `cmd/commit` tests keep exit-zero
      assertions on success.
- [x] `ToolError` structured fields are unchanged; only the CLI text rendering
      moves or changes.
      Test: `mcp/errors_test.go` keeps the full `TestMapError*` / envelope
      decode family on the untouched `ToolError`; `internal/mcp/errors.go`
      only loses the old `Report()` text renderer, whose behaviour moved to
      `app.writeError` with its own tests (`TestReport`, `TestWriteErrorColoured`).

Each checked criterion cites its unit test in `internal/app` (colour helper and
renderer) and the Phase 5 CLI scenarios.

## Error coverage

| Failure                         | Expected outcome                      | Required test          |
| ------------------------------- | ------------------------------------- | ---------------------- |
| Domain error (conflict)         | Unified coloured report, nonzero exit | CLI scenario           |
| Non-domain error (cancellation) | Unchanged error return, no report     | `app.Report` unit test |
| Writer is not a terminal        | Plain output                          | `app.Report` unit test |
| `NO_COLOR` set                  | Plain output on any writer            | `app.Report` unit test |

Covered:

- `TestReport` "domain error writes the category report" drives a
  `notebook.Error` through `Report` and asserts the unified category report
  plus the terse category return; the `cmd/pull` and `cmd/commit` command
  tests prove the router exits nonzero on the category. The full coloured
  conflict form is `TestWriteErrorColoured`.
- `TestReport` "non-domain error passes through unprinted" returns
  `context.Canceled` unchanged and writes nothing, so cancellation keeps its
  existing CLI behaviour (reported by the router without a report).
- `TestReport` "piped output stays plain" captures a real pipe and finds no
  ANSI escape; `TestColourEnabled` covers the pipe, buffer, and nil-writer
  cases at the gate.
- `TestReport` "NO_COLOR keeps the report plain" and `TestColourEnabled`
  (character device + `NO_COLOR=1`) cover a non-empty `NO_COLOR` on any
  writer.

The Phase 5 CLI scenario suite additionally asserts the plain `OK`-prefixed
report of a spawned process (no ANSI on a pipe) and the exit-zero success
contract.

## Implementation notes

- `internal/app/colour.go` holds the ANSI helper: the four SGR codes
  (green, red, yellow, cyan), a small `painter` that wraps one token when
  colour is on and passes it through byte-identically otherwise, and the
  `colourEnabled(out, env)` gate. The gate reuses the existing `environ`
  helper and the `logEnvNoColor` constant from `logging.go`, requires a
  real terminal (`*os.File` whose mode has `ModeCharDevice`), and follows
  the NO_COLOR convention: any non-empty value disables colour.
- `app.Report(out, result, err, env)` now renders both outcomes with the
  one status/detail/trailer skeleton. The error renderer moved out of
  `internal/mcp`: `ToolError.Report()` is gone and `app.writeError` owns
  the text form, so the structured `ToolError` value stays the only shape
  the MCP layer knows. `writeSuccess` / `writeError` build into a
  `strings.Builder` and write once, so a report never interleaves with
  another write.
- `cmd/pull` and `cmd/commit` capture the operation's `(result, err)` and
  pass `c.opts.Env` to `Report`, so the subprocess environment (where a
  caller would set `NO_COLOR`) reaches the gate. Help text in both
  commands, `docs/running.md`, and the top-level `README.md` describe the
  unified report and the colour gating.
- The error trailer order changed as part of the unification: `retryable`
  (and `recovery`) now follow the file detail lines, matching the
  status/detail/trailer skeleton, instead of following the message as in
  the removed `ToolError.Report()`. The trailer content is unchanged.
- Gate before: `go build ./...` OK; `go test ./internal/app/...
  ./cmd/...` OK. Gate after: `go test ./internal/app/...`,
  `./cmd/...` OK; full `go test ./... -race -count=3 -timeout=30s
  -coverpkg=./...` OK (aggregate coverage 83.7%, floor 70%); `make test`
  OK; `go vet ./...`, `staticcheck ./...`, `go fix -diff ./...`,
  `gofumpt -l` (nothing listed), and `dupl -t 80 .` (0 clone groups)
  clean; `npm test --prefix npm/slivingdoc` OK (35/35).

## Review findings

No reviews recorded.
