# Phase 3 — Flag and configuration

**Status:** Not Started

[← README](README.md)

## Goal

Give the writable set an operator surface that resolves exactly like every other
setting, shared by all three commands, and make a misconfiguration refuse startup
before the native engine or the object store is touched.

## Specification

### Resolution

The flag and its environment variable are rows in the README parameters table.
They join the shared flag set, so `serve`, `pull`, and `commit` all accept them
with no per-command wiring, and they resolve through the existing precedence:
flags beat environment variables, which beat defaults.

An explicitly empty flag value does not fall back to the environment. That rule
already exists and this phase only inherits it, but it carries weight here: a
caller that must defeat an inherited environment value passes the flag with an
empty value, and a downstream consumer depends on exactly that idiom to keep its
own publication path unconstrained. A change that made an empty flag fall through
to the environment would silently confine a process that asked not to be.

The value splits on the separator named in the README parameters table, with
surrounding whitespace trimmed from each piece and empty pieces dropped, exactly
as the read-only value splits today.

| Input | Resolved set | Test |
| --- | --- | --- |
| Flag set, environment set | The flag's entries | `TestFlags_WritablePathsResolution` |
| Flag unset, environment set | The environment's entries | `TestFlags_WritablePathsResolution` |
| Flag unset, environment unset | Empty | `TestFlags_WritablePathsResolution` |
| Flag explicitly empty, environment set | Empty; the environment is not consulted | `TestFlags_WritablePathsExplicitEmptyIgnoresEnvironment` |
| A value with surrounding whitespace and an empty piece | The trimmed, non-empty entries | `TestFlags_WritablePathsSplitting` |

### Startup refusal

Configuration normalizes both sets and constructs the policy at the point where
the read-only set is normalized today, which is before the native engine opens
and before the store probe runs. That ordering is an existing documented property
of an invalid read-only entry, and this phase extends it to the writable set and
to the overlap error rather than weakening it.

The overlap message names the offending path and both settings, because an
operator reading it needs to know which of the two to change. When a value came
from the environment rather than the flag, the message still names the settings;
the entry text is what identifies the source in practice.

| Failure | Refusal point | Test |
| --- | --- | --- |
| An entry in the writable set fails path validation | Before the engine and the probe | `TestSetup_RejectsInvalidWritableEntry` |
| A path is named by both sets | Before the engine and the probe | `TestSetup_RejectsOverlapNamingBothSettings` |
| A path is named by both sets differing only in letter case | Before the engine and the probe | `TestSetup_RejectsCaseFoldedOverlap` |
| Neither failure applies | Startup proceeds unchanged | `TestSetup_ValidPolicyProceeds` |

The refusal ordering is asserted directly rather than inferred: the test supplies
a store factory and an engine that fail if they are called at all, so a refusal
that happened too late fails the test rather than passing quietly.

### Documentation carried by this phase

The help text is the authoritative copy of the flag table, so it changes here
along with the operator documentation and the accepted contract. Each is an
acceptance row rather than a reminder.

- The help text gains the flag line, in the existing column layout.
- The operator running guide gains the setting beside the read-only one.
- The accepted contract's configuration table gains the row.
- The repository agent guide's flag notes gain the startup-refusal property, and
  its invariant list gains the composed rule: a process configured with either
  set never publishes a change to a path the policy protects.

### Files

- `internal/app/config.go` — the flag, the environment variable, the split, the
  normalization, the overlap check, and the help text line.
- `internal/app/config_test.go` — the resolution and refusal tables.
- `internal/app/service.go` — pass the resolved entries through to the notebook.
- `docs/running.md` — the operator entry.
- `docs/slivingdoc-v1.md` — the configuration table row.
- `AGENTS.md` — the flag note and the invariant.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `commit` with a writable set, editing inside it | Real engine, fake store | Success report with the generation and the resolved notebook path | The edit is published | None |
| `commit` with a writable set, editing outside it | Real engine, fake store | The structured error report with a nonzero exit, naming the writable entries | The touched file is reset | Remote generation unchanged |
| `pull` with a writable set and a locally edited protected path | Real engine, fake store | Success report; the protected path holds the remote content | The restore appears in the report | Writable edits untouched |
| Any command with a path named by both sets | Engine and store factory that fail when called | A nonzero exit naming the path and both settings | None | The engine is not opened; the store is not probed |
| Any command with the flag passed explicitly empty and the environment variable set | Real engine, fake store | The process behaves as if no writable set were configured | None | The environment value is not consulted |
| `serve`, `pull`, and `commit` each with the flag | — | All three accept it | None | No command rejects it as unknown |

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| The setting resolves by flag, then environment, then default | `TestFlags_WritablePathsResolution` |
| An explicitly empty flag value does not consult the environment | `TestFlags_WritablePathsExplicitEmptyIgnoresEnvironment` |
| The value splits and trims exactly as the read-only value does | `TestFlags_WritablePathsSplitting` |
| All three commands accept the flag | `TestCommandsShareWritablePathsFlag` |
| An invalid entry or an overlap refuses startup before the engine opens and before the store is probed | `TestSetup_RefusesBeforeEngineAndProbe` |
| The overlap message names the path and both settings | `TestSetup_RejectsOverlapNamingBothSettings` |
| The help text carries the flag line in the existing layout | `TestHelpText_WritablePathsLine` |
| The operator guide, the accepted contract's configuration table, and the agent guide all carry the setting | Reviewer check against the three files, recorded in Implementation notes |
| A valid configuration reaches the notebook with both sets intact | `TestSetup_PassesBothSetsToNotebook` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| An entry fails path validation | Startup refuses, naming the offending entry and which setting it came from | `TestSetup_RejectsInvalidWritableEntry` |
| A path is named by both sets | Startup refuses, naming the path and both settings | `TestSetup_RejectsOverlapNamingBothSettings` |
| The same path is named by both sets differing only in letter case | Startup refuses identically, since matching folds case | `TestSetup_RejectsCaseFoldedOverlap` |
| The environment variable holds an invalid entry and no flag is given | Startup refuses with the same message as the flag path | `TestSetup_RejectsInvalidEntryFromEnvironment` |
| The value is entirely separators or whitespace | Resolves to an empty set rather than to an invalid entry | `TestFlags_WritablePathsSplitting` |
| A refusal would otherwise happen after the engine opened | The test's engine and store fail when called, so the ordering regression fails the test | `TestSetup_RefusesBeforeEngineAndProbe` |

## Implementation notes

Not started.

## Review findings

None.
