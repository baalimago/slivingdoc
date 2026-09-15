# Phase 3 — Flag, environment, operator docs

**Status:** Not Started

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

Not started.

## Review findings

None.
