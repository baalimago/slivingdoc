# Phase 3 — Flag, environment, operator docs

**Status:** Complete

**README:** [README.md](README.md)

## Goal

The read-only set is configured through one shared flag and its environment
variable with the documented precedence, refuses startup on an invalid
entry, and is described everywhere an operator reads.

## Specification

### Configuration (`internal/app/config.go`)

- `Flags` gains `readOnlyPaths stringFlag`, bound as `read-only-paths` with
  the help text `notebook paths agents may read but never change`.
- `config` gains `readOnlyPaths []string`. `resolve` reads the flag over the
  environment variable named in the README parameters over the empty
  default, splits on the README's entry separator, trims surrounding white
  space from each piece, and drops empty pieces, so a trailing separator or
  an explicitly empty flag yields the empty set.
- `finish` normalizes the entries with `git.NormalizeReadOnly` and returns
  its error prefixed `read-only paths:`; the entry text may appear because
  entries are notebook-relative, never private.
- `serviceConfig` copies the normalized entries into
  `ServiceConfig.ReadOnlyPaths`.
- `FlagReference` gains one row in the same column layout as the others,
  naming the flag, the environment variable, the separator, the default,
  and one clause on the effect. `HelpText` is unchanged apart from the
  reference.

### Runtime

- `app.Runtime.ReadOnlyPaths() []string` exposes the service's normalized
  entries for the subcommands; Phase 4 consumes it.

### Documents

- `docs/running.md`: the flag table row; a new section "Read-only paths"
  with the fleet walkthrough: agents served with the flag, a human `pull`,
  edit under `docs/`, `commit` without the flag, the refusal an agent sees,
  the trust-boundary paragraph from the README, and the note that an
  inherited environment value is cleared by an explicitly empty flag.
- `docs/slivingdoc-v1.md`: the configuration section's flag table and
  precedence text; a new numbered decision in the decisions section stating
  that read-only paths are per-process configuration enforced at commit
  and restored at pull.
- `AGENTS.md`: the key-flags paragraph mentions the flag and its refusal;
  the invariants list gains the read-only invariant from the README.
- `README.md` (repository root): one short paragraph in the usage section.

### Invariant: every command shares the flag

| Bound actor | Mechanism                               | Test                              |
| ----------- | --------------------------------------- | --------------------------------- |
| `serve`     | shared `Flags.Bind`                     | `TestScenarioReadOnlyFlagProcess` |
| `pull`      | shared `Flags.Bind` through `app.Setup` | `TestPullAcceptsReadOnlyFlag`     |
| `commit`    | same                                    | `TestCommitAcceptsReadOnlyFlag`   |

### Limit: entry validation

| Limit            | Injectable field      | README parameter           | How the test triggers it                            |
| ---------------- | --------------------- | -------------------------- | --------------------------------------------------- |
| entry path rules | `Flags.readOnlyPaths` | read-only entry path rules | `--read-only-paths=..` and `--read-only-paths=/abs` |

## Integration contract

| Trigger                                                                        | Collaborators or fakes        | Observable result                                                | Required side effects | Prohibited side effects                                             |
| ------------------------------------------------------------------------------ | ----------------------------- | ---------------------------------------------------------------- | --------------------- | ------------------------------------------------------------------- |
| `serve --read-only-paths docs,faq.md`; client pulls                            | spawned process, fake backend | pull `OK` with readOnly `[docs, faq.md]`; instructions name both | none                  | nonzero exit                                                        |
| `SLIVINGDOC_READ_ONLY_PATHS=docs` in the environment; `serve` without the flag | spawned process               | pull readOnly `[docs]`                                           | none                  | nonzero exit                                                        |
| same environment plus `--read-only-paths=` (explicitly empty)                  | spawned process               | pull readOnly `[]`                                               | none                  | any `(read-only:` text                                              |
| `--read-only-paths=notes,docs/,docs/faq.md, ,`                                 | spawned process               | pull readOnly `[docs, notes]`                                    | none                  | nonzero exit                                                        |
| `serve --read-only-paths=..`                                                   | spawned process               | exit nonzero; stderr contains `read-only paths:` and the entry   | none                  | any request to the backend; a created session directory left behind |
| `serve --read-only-paths=/abs`                                                 | spawned process               | exit nonzero; stderr contains `read-only paths:`                 | none                  | any request to the backend                                          |
| `serve -h`                                                                     | spawned process               | exit zero; output contains the flag row                          | none                  | any dependency load                                                 |

## Acceptance criteria

| Outcome                                                      | Evidence                                                                                                          |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| Flag over environment over default, with the empty-flag rule | `TestLoadConfigReadOnlyPaths`, `TestLoadConfigEmptyFlagDoesNotFallBackToEnv` (extended)                           |
| Splitting, trimming, and dropping empty pieces               | `TestLoadConfigReadOnlyPaths`                                                                                     |
| Invalid entries refuse startup before any dependency         | `TestLoadConfigReadOnlyPathsInvalid`, `TestScenarioReadOnlyInvalidFlagRefusesStartup`                             |
| A refusal leaves no session directory behind                 | `TestLoadConfigRefusalRemovesSessionDir` (extended with an invalid read-only entry)                               |
| The normalized set reaches the service                       | `TestScenarioReadOnlyFlagProcess`, `TestScenarioReadOnlyEnvPrecedence`                                            |
| `pull` and `commit` accept the flag                          | `TestPullAcceptsReadOnlyFlag`, `TestCommitAcceptsReadOnlyFlag`                                                    |
| `Runtime.ReadOnlyPaths` returns the normalized entries       | `TestRuntimeReadOnlyPaths`                                                                                        |
| Help output lists the flag                                   | `TestScenarioConfigInvalidAndEarlyExit` (extended to assert the row)                                              |
| Documents describe the flag                                  | reviewer reads `docs/running.md`, the contract configuration and decisions sections, `AGENTS.md`, and `README.md` |

## Error coverage

| Failure                                              | Expected outcome                    | Test                                     |
| ---------------------------------------------------- | ----------------------------------- | ---------------------------------------- |
| entry `..`                                           | startup refusal naming the entry    | `TestLoadConfigReadOnlyPathsInvalid`     |
| absolute entry                                       | startup refusal                     | `TestLoadConfigReadOnlyPathsInvalid`     |
| entry with a `.git` segment                          | startup refusal                     | `TestLoadConfigReadOnlyPathsInvalid`     |
| entry over the path byte bound                       | startup refusal                     | `TestLoadConfigReadOnlyPathsInvalid`     |
| malformed environment value with a valid flag        | flag wins; environment never parsed | `TestLoadConfigReadOnlyPaths`            |
| refusal after the ephemeral session directory exists | directory removed                   | `TestLoadConfigRefusalRemovesSessionDir` |

## Implementation notes

2026-09-15, phase-3 execution session.

Implemented exactly the specification: `Flags.readOnlyPaths` bound as
`read-only-paths` with the exact help text, `config.readOnlyPaths` resolved
flag-over-environment-over-default with `splitReadOnlyPaths` (split on `,`,
trim, drop empty pieces), normalized in `finish` through
`git.NormalizeReadOnly` with the error wrapped `"read-only paths: %w"`,
copied into `ServiceConfig.ReadOnlyPaths` by `serviceConfig`, one
`FlagReference` row in the same column layout (verified by printing
`HelpText` in a throwaway test), and `app.Runtime.ReadOnlyPaths()` exposing
`Service.ReadOnlyPaths()` for the subcommands.

Deviation (implementation-only, no README gap): `config` was a comparable
struct compared with `==` in `TestLoadConfigDefaults`; adding the
`readOnlyPaths []string` field made it uncomparable, so that pre-existing
test now uses `reflect.DeepEqual` with an explicit `readOnlyPaths: []string{}`
in the expected value (the normalized zero value from
`git.NormalizeReadOnly(nil)` is a non-nil empty slice, not nil). No other
production behavior changed.

Test evidence, one per acceptance-criteria and error-coverage row:
- `TestLoadConfigReadOnlyPaths` (`internal/app/config_test.go`): default
  empty set, environment alone, flag over environment, explicitly empty
  flag beats an inherited environment value, splitting/trimming/dropping
  empty pieces with nested-entry collapse, and a malformed environment
  value never parsed when a valid flag wins outright.
- `TestLoadConfigEmptyFlagDoesNotFallBackToEnv` (extended): added the
  read-only-paths empty-flag case alongside the pre-existing bucket case.
- `TestLoadConfigReadOnlyPathsInvalid`: `..`, `/abs`, a `.git` segment, and
  an entry over the 4,096-byte path bound (built from 2,050 one-byte
  segments so no single segment trips the 255-byte segment bound instead),
  each asserting the `"read-only paths:"` prefix and the echoed entry.
- `TestLoadConfigRefusalRemovesSessionDir` (extended): added the
  `SLIVINGDOC_READ_ONLY_PATHS=..` row to the existing session-directory
  table.
- `TestRuntimeReadOnlyPaths` (`internal/app/readonly_test.go`): `setup()`
  end to end with `--read-only-paths=notes,docs/,docs/faq.md` resolves to
  `["docs","notes"]` through `Runtime.ReadOnlyPaths()`.
- `TestPullAcceptsReadOnlyFlag` / `TestCommitAcceptsReadOnlyFlag`
  (`cmd/pull/pull_test.go`, `cmd/commit/commit_test.go`): the real
  `git2.New()` engine, `Setup()` over the shared flag set with
  `--read-only-paths=docs`, then the unexported `runtime.ReadOnlyPaths()`.
- `TestScenarioReadOnlyFlagProcess`, `TestScenarioReadOnlyEnvPrecedence`,
  `TestScenarioReadOnlyInvalidFlagRefusesStartup`
  (`internal/integrationtest/scenario_readonly_test.go`): a spawned `serve`
  process proves the flag and the environment variable each reach the
  service (`readOnly` array and instructions), the explicit-empty-flag
  rule over a spawned process, and that `..`/`/abs` refuse startup with
  `"read-only paths:"` and the entry on stderr, before any backend call
  (the refusal is inside `loadConfig`, before `buildService`).
- `TestScenarioConfigInvalidAndEarlyExit` (extended): `serve -h` stdout now
  also asserts `--read-only-paths`.

Extended `assertProcessArgsOK` (`internal/integrationtest/scenario_transport_test.go`)
to expect the `"<path> (read-only: <entries>)"` text suffix (README
"Advertising the read-only set") whenever a call's `SuccessInfo.ReadOnly`
is non-empty, instead of writing a parallel helper; every pre-existing
caller has an empty `ReadOnly` and is unaffected.

Documentation: `docs/slivingdoc-v1.md` §17 gained the flag-table row and a
paragraph on the entry rules and startup refusal; §24 gained decision 38
(read-only paths are per-process configuration, enforced at commit,
restored at pull). `docs/running.md` gained the flag-table row and a new
"Read-only paths" section (fleet walkthrough, the refusal an agent sees,
the trust-boundary paragraph, and the explicit-empty-flag note); its
refusal example intentionally shows today's actual CLI report bytes
(`internal/app/command.go` `writeError`: code, message, one file line,
`retryable:`) rather than Phase 4's future `reason`/`next:`/`read-only:`
trailer, which that phase has not built yet, with a note that the MCP
envelope already carries `reason`, `action`, the file's `READ_ONLY`
reason, and `readOnly`. `AGENTS.md`'s key-flags paragraph and invariants
list gained the flag and the read-only invariant. Root `README.md` gained
one paragraph after the CLI report paragraph in "How it works".

Verification commands run, in order, with results:
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .` — no files listed.
- `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` — clean.
- `go fix -diff ./...` — printed two modernizations (`strings.Split` →
  `strings.SplitSeq`, a counting `for` → `for range`) against the new
  code; applied both, then reran to confirm empty output.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — "Found total 0 clone
  groups."
- `make qa` (gofumpt, go vet, staticcheck, go fix -diff, `go test -race
  -count=3 -timeout=30s -coverpkg=./... -coverprofile=...` over every
  package, `npm test --prefix npm/slivingdoc`) — every package `ok`,
  coverage 83.8% against the 70% floor, npm suite 35/35 passed.

No README gap found. Phase 3 is Complete. `app.Flags.readOnlyPaths` /
`config.readOnlyPaths` and `Runtime.ReadOnlyPaths()` are ready for Phase 4
(the CLI report renders the read-only trailer and its fixtures run with
the flag).

### 2026-09-15, phase-3 fix session (review 1)

Fixed R1-02 (minor): `internal/git/readonly.go`'s `NormalizeReadOnly` built
its per-entry error as `fmt.Errorf("git: invalid read-only path %q: %w",
raw, err)`. The leading `git:` is Git vocabulary in operator-facing text
(README invariant 9), and it was the only configuration refusal anywhere
in `internal/app/config.go` that carried a package prefix — `finish`'s own
`"read-only paths: %w"` wrapper already names the source, so the inner
prefix was redundant. Removed `git: ` from the format string; the entry
text and the wrapped `ValidatePath` message are unchanged.

`TestLoadConfigReadOnlyPathsInvalid` (`internal/app/config_test.go`) now
pins the exact diagnostic text after the `"read-only paths: "` wrapper for
the `..` and `/abs` rows (`invalid read-only path "..": invalid path
"..": ".." segment is not allowed` and `invalid read-only path "/abs":
invalid path "/abs": must not start or end with a slash`), and every row
now also asserts the text contains no `git:` substring; the `.git`-segment
and over-the-byte-bound rows keep the existing prefix-only check since
their exact text is not the point of this fix. Checked
`internal/git/readonly_test.go`'s `TestNormalizeReadOnly` (subtest
"invalid entries") and `internal/notebook/readonly_test.go`'s
`TestNewRejectsInvalidReadOnlyPaths`: neither asserts on the error text,
only on non-nil, so neither needed updating.
`internal/integrationtest/scenario_readonly_test.go`'s
`TestScenarioReadOnlyInvalidFlagRefusesStartup` asserts only the
`"read-only paths:"` substring and the echoed entry, so it also needed no
change.

Verification commands run, in order, with results:
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go run mvdan.cc/gofumpt@v0.11.0 -l -w internal/app/config_test.go
  internal/git/readonly.go` — reformatted the new test's struct literal;
  a follow-up `-l` listed no files.
- `go test ./internal/app/... -run TestLoadConfigReadOnlyPathsInvalid -v`
  — all four subtests pass.
- `go test ./internal/git/... ./internal/notebook/... ./internal/app/...
  -run 'ReadOnly' -v` — every read-only test in the three packages passes.
- Fresh binary built exactly as specified: `cd
  /home/imago/Projects/public/slivingdoc && CGO_ENABLED=1
  PKG_CONFIG_PATH=/home/imago/Projects/public/slivingdoc/.build/libgit2/lib/pkgconfig
  go build -o <scratchpad>/slivingdoc .` (invoked with `go build -C
  <repo> -o <scratchpad>/slivingdoc .` to keep the module root as the
  build directory while placing the binary in the scratch directory) —
  clean build. Then, from a scratch working directory:
  - `NO_COLOR=1 <scratchpad>/slivingdoc commit --bucket b
    --read-only-paths '../x' -m x .` printed exactly:
    `error: failed to setup command: app: invalid configuration:
    read-only paths: invalid read-only path "../x": invalid path "../x":
    ".." segment is not allowed` (timestamp prefix omitted here) — no
    `git:` prefix, matching the fixed `TestLoadConfigReadOnlyPathsInvalid`
    expectation for the `..` entry.
  - `NO_COLOR=1 <scratchpad>/slivingdoc commit --bucket b
    --read-only-paths '/abs' -m x .` printed the corresponding `/abs`
    diagnostic with no `git:` prefix.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group,
  `internal/app/command_test.go:426,446` and `:451,471`
  (`TestFileReasonWords`/`TestActionWording`), the same pre-existing
  table-driven-test clone already reviewed and accepted under the
  AGENTS.md duplication policy by Phases 4 and 5 and the Phase 2 fix; no
  new clone group.
- `make qa` (gofumpt, go vet, staticcheck, go fix -diff, `go test -race
  -count=3 -timeout=30s -coverpkg=./...` over every package, `npm test
  --prefix npm/slivingdoc`) — exit 0, coverage 84.0% against the 70%
  floor, npm suite 35/35 passed.

No README gap found. Phase 3 returns to Complete.

## Review findings (review 1, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). Gates re-run: `make qa` exit
0. Built a fresh binary from the working tree and ran `commit
--read-only-paths` with `../x`, `docs//x`, `/abs`, `.git/x`, and
`docs,,notes/` to observe the real startup diagnostics.

- [x] **R1-02 (minor).** The startup refusal reads `app: invalid
  configuration: read-only paths: git: invalid read-only path "../x":
  invalid path "../x": ".." segment is not allowed`. The `git:` package
  prefix is Git vocabulary in operator-facing text (README invariant 9,
  AGENTS.md error-taxonomy rule) and no other configuration refusal
  carries a package prefix. Fix: build the `NormalizeReadOnly` error
  without the `git:` prefix (the `read-only paths:` wrapper already names
  the source) and pin the exact diagnostic in
  `TestLoadConfigReadOnlyPathsInvalid`. — resolved (fix 1)

Verified good:

- Flag over environment over default, an explicitly empty flag clearing an
  inherited environment value (D4), splitting and trimming, and the
  normalized set reaching the service are each proven at the spawned-process
  boundary (`TestScenarioReadOnlyFlagProcess`,
  `TestScenarioReadOnlyEnvPrecedence`).
- Invalid entries refuse startup inside `loadConfig`, before any native or
  S3 dependency loads, and the ephemeral session directory is removed.
- `docs/running.md`, `docs/slivingdoc-v1.md` §17 and §24, `AGENTS.md`, and
  the root README describe the shipped flag and precedence accurately.

## Review findings (review 2, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). R1-02 resolved and re-verified: `NormalizeReadOnly` no longer prefixes `git:`; the exact diagnostic is pinned in `TestLoadConfigReadOnlyPathsInvalid` and observed from a fresh binary. No new finding.
