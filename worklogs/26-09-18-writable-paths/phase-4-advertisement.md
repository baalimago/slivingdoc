# Phase 4 — Advertisement and CLI report

**Status:** Not Started

[← README](README.md)

## Goal

Make every surface an agent or an operator reads name where writing is allowed,
so the rule is learned before the first edit and not only in the refusal, and so
a host that drops one surface still leaves the others.

## Specification

### What each surface says

The read-only set is advertised on four agent-facing surfaces and one operator
surface. Each gains the writable set beside it. The governing rule is that the
text names the **actionable** list: under a default-protected policy the
protected region is nearly the whole notebook, so naming it would be both useless
and enormous, while the writable entries are short by construction.

| Set state | Text names | Test |
| --- | --- | --- |
| Writable empty, read-only non-empty | The read-only entries, in today's wording, byte-for-byte | `TestAdvertisement_ReadOnlyWordingUnchanged` |
| Writable non-empty, read-only empty | The writable entries, as where writing is allowed | `TestAdvertisement_NamesWritableEntries` |
| Both non-empty | Both, writable first, since the writable region is the frame and the read-only entries are the exceptions inside it | `TestAdvertisement_BothSetsWritableFirst` |
| Both empty | Neither; the text is the bare form it is today | `TestAdvertisement_UnconfiguredTextUnchanged` |

Entries are joined with the separator named in the README parameters table, which
is the one every surface already uses.

### The surfaces

| Surface | Change |
| --- | --- |
| Server instructions | One sentence naming the set and the consequence of writing elsewhere |
| Both tool descriptions | One sentence naming the set |
| Success result | A writable array beside the existing read-only array |
| Error result | The same array, on every domain error |
| Success text item | The parenthesized set form beside the resolved path |
| Operator report | A trailer line, rendered dim on a terminal like the existing one, on both the success and the error report |

The service interface the server reads widens by one accessor, which Phase 2
already put on the service and the notebook.

### Structured fields

The existing read-only array keeps meaning exactly what it means today: the
normalized read-only entries, never the protected region. The new array carries
the normalized writable entries. Both are additive, always present, and empty
when their set is unconfigured, matching how the existing array is specified in
the repository agent guide.

| Property | Test |
| --- | --- |
| The writable array is present on every success result | `TestSuccessResult_CarriesWritableArray` |
| The writable array is present on every domain error result | `TestErrorResult_CarriesWritableArray` |
| Both arrays are empty rather than absent when unconfigured | `TestResults_ArraysEmptyWhenUnconfigured` |
| The read-only array never carries a writable entry | `TestResults_ReadOnlyArrayKeepsMeaning` |

### Consumer compatibility

The known downstream consumer decodes the operator report leniently: it ignores a
trailer line it does not recognize and requires only the retryable trailer to
treat a report as a domain error, and it matches the success status line by
pattern rather than by position. A new trailer line and a new structured field
are therefore invisible to a client pinned to the current release, which is why
this phase adds fields and a line rather than changing either existing shape.

This is recorded as evidence, not as a test: the consumer is another repository
and its behavior is pinned by its own suite.

### Documentation carried by this phase

The accepted contract's read-only section gains a mirrored writable subsection
covering resolution, the unmatched default, the composition rule, the exact
overlap refusal, and the advertisement surfaces. The sentence promising that an
unconfigured process is byte-for-byte unchanged except for the always-present
empty array is extended to cover both arrays.

### Files

- `internal/mcp/server.go` — the instructions sentence, both description
  suffixes, the success text form, and the widened service interface.
- `internal/mcp/success.go` — the success structured field.
- `internal/mcp/error.go` — the error structured field.
- `internal/app/command.go` — the report trailer on both renderers.
- `internal/mcp/server_test.go`, `internal/app/command_test.go` — the tables
  above.
- `internal/integrationtest/scenario_writable_test.go` — the boundary assertions
  below, appended to the file Phase 2 created.
- `docs/slivingdoc-v1.md` — the mirrored subsection.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| An agent initializes against a process with a writable set | Real engine, fake store | The server instructions name the writable entries and the consequence of writing elsewhere | None | The instructions do not enumerate the protected region |
| An agent lists tools against the same process | Real engine, fake store | Both tool descriptions name the writable entries | None | None |
| An agent pulls successfully | Real engine, fake store | The result carries both arrays and the text item names the set | None | The read-only array is unchanged in meaning |
| An agent commits and is refused | Real engine, fake store | The error carries both arrays alongside the existing category, reason, action, and file entries | None | No existing field changes shape |
| An agent works against a process with neither set | Real engine, fake store | Instructions, descriptions, and text items are today's bare forms; both arrays are present and empty | None | No text mentions either set |
| An operator runs the one-shot commit and is refused | Real engine, fake store | The report renders the existing status line, message, file entries, and next step, plus the trailer naming the set | None | Existing lines keep their order and wording |

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| Each of the four agent-facing surfaces names the writable set when one is configured | `TestAdvertisement_NamesWritableEntries` and the boundary scenarios |
| Each surface keeps today's wording when only a read-only set is configured | `TestAdvertisement_ReadOnlyWordingUnchanged` |
| Both sets are named, writable first, when both are configured | `TestAdvertisement_BothSetsWritableFirst` |
| A process with neither set is byte-for-byte unchanged except for the always-present empty arrays | `TestAdvertisement_UnconfiguredTextUnchanged`, `TestResults_ArraysEmptyWhenUnconfigured` |
| The writable array appears on every success and every domain error | `TestSuccessResult_CarriesWritableArray`, `TestErrorResult_CarriesWritableArray` |
| The read-only array still carries only read-only entries | `TestResults_ReadOnlyArrayKeepsMeaning` |
| The operator report renders the trailer on both the success and the error path, dim only on a terminal | `TestReport_WritableTrailer` |
| The existing report lines keep their order and wording | `TestReport_ReadOnlyTrailerUnchanged` |
| The accepted contract carries the mirrored subsection | Reviewer check against the document, recorded in Implementation notes |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| A domain error is produced before the policy is consulted | The arrays are still attached, since they come from the service rather than from the failing operation | `TestErrorResult_ArraysOnEarlyFailure` |
| A recovery failure is returned | The arrays are attached alongside the recovery stage, with no existing field displaced | `TestErrorResult_ArraysOnRecoveryFailure` |
| The report is written to a pipe rather than a terminal | The trailer renders without styling, like the existing one | `TestReport_WritableTrailerUnstyledOnPipe` |
| A set is empty while the other is not | Only the non-empty set appears in text; both arrays remain present | `TestAdvertisement_OneSetEmpty` |
| A host forwards only the success text item and drops the structured content | The text item alone still names where writing is allowed | `TestSuccessText_WritableForm` |

## Implementation notes

Not started.

## Review findings

None.
