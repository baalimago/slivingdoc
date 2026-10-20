# Phase 3: quality gate

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Prove the public repository green, every architecture doc this worklog touches
true, the README readiness checklist re-verified, and the whole product claim
true on top of the sibling's shipped admin surface.

## Specification

### Public repository gate

Run the repository's one Go test command and its full gate:

```bash
make qa
```

`make qa` runs the formatter, `go vet`, staticcheck, `go fix -diff`, the race
test with `-count=3`, coverage with the 70 % floor, the npm tests, and the
dupl signal. Do not substitute a narrower command, a short mode, a build tag,
or a second gate.

The cloud repository is the sibling's gate. This worklog changes no cloud code
and runs no cloud command; it records the sibling's result when the README
journal carries one.

### Product check

This worklog completes the addressing half of the product. With the sibling
`26-10-18-space-admin-surface` shipped, prove the joined behavior:

| Claim | Evidence |
| --- | --- |
| `notes_pull` and `notes_commit` take no `path` and work at the server directory | `TestScenarioPullWithoutPath`, `TestScenarioCommitWithoutPath` |
| A hosted directory created and chosen through `space` is reached by `serve` and by a bare `pull` | `TestScenarioServeUsesTheRememberedSpace` |
| An S3 directory that pulled remembers its bucket, and a later bare `pull` and `serve` use it | `TestScenarioS3RecordReachesItsBucket`, `TestScenarioServeUsesTheS3Record` |
| A target from `slivingdoc space <name>` is used when no flag, environment, or record names one | `TestScenarioDefaultSpaceUsed` |
| No command rewrites an existing record | `TestScenarioRecordIsNeverRewritten` |

### Architecture docs

Every doc this worklog's phases touch is re-read against the code and
corrected in the same commit if it drifted:

| Doc | What must be true |
| --- | --- |
| `architecture/mcp-server.md` | schemas without `path`, the refusal, one directory |
| `architecture/product-contract.md` | tool inputs, the refusal, the success and error envelopes |
| `architecture/overview.md` | one directory per server, both backends |
| `architecture/config.md` | the resolution order, the record rules, the completed store key before a default is read |
| `architecture/running.md` | the `serve` record, `--profile` and the record layers, the full flag table |
| `architecture/workspace.md` | `Identity.Profile`, the conditional digest append |
| `architecture/storage.md` | the probe proof version 2 and the per-store proof |
| `architecture/s3store.md` | the effective values the store reports and how `app` uses them |
| `architecture/hosted-mode.md` | the hosted record with an empty endpoint, the login endpoint |
| `architecture/errors.md` | no new category; any new reason or action is named |
| `architecture/testing.md` | the harness with one directory, the S3 record scenarios |
| `architecture/cli.md` | `pull`, `commit`, `status`, and `log` resolve the record the same way |
| `architecture/README.md` | the index and reading order still name every doc |

### README readiness checklist

Run every line of the README checklist and record the outcome:

1. the shared diagrams explain serve startup and pull/commit resolution;
   phase-local sequences stay in their phase;
2. every test has one owning phase and a file list;
3. every parameter, flag, injectable field, invariant, and limit has one owner
   and a test;
4. every phase that needs a person declares `Human required`;
5. no phase depends on another phase's local detail;
6. no phase creates human-owned configuration;
7. the cited conventions match the code.

### Invariants and limits

| Rule | Mechanism | Proof |
| --- | --- | --- |
| No test skips a host capability it can run | the suite has no new skip | `make qa` |
| The coverage floor is met | `make test` | `make qa` |
| Dupl findings are triaged | `dupl -t 80` | `make qa` output |
| Every architecture doc this worklog touches is true | review table above | review notes in this phase |
| The accepted-state, pack, CAS, and error-category invariants are unchanged | the existing suite | `make qa` |
| No credential, key, token, private path, or Git vocabulary reaches caller text | existing suite | `make qa` |

### Human required

None.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `make qa` | the public toolchain | exit 0, every gate printed | none | a skipped package, a short mode |
| the product check | the test suite and the fake store | every claim above proven | none | a claim resting on a live endpoint |
| the architecture review | the docs and the code | every named doc true | doc corrections in the same change | a plan recorded as behavior |
| the readiness checklist | the README | every line recorded | a journal note | an unrecorded exception |

## Acceptance criteria

| Criterion | Command or test |
| --- | --- |
| The public gate is green | `make qa` |
| The race suite passes at count 3 | `make test` |
| Coverage is at or above 70 % | `make test` output |
| npm tests pass | `make npm-test` |
| Dupl is triaged | `go run github.com/mibk/dupl@v1.0.0 -t 80 .` |
| Every product claim above is proven | the named tests |
| Every architecture doc this worklog touches is true | review table in this phase |
| The readiness checklist is recorded | this phase's implementation notes |
| No test was skipped or shortened | `git diff` review and `make qa` output |
| No live cloud or AWS endpoint was reached | test review and `make qa` output |

## Error coverage

| Failure | Expected outcome |
| --- | --- |
| formatter reports a file | run the formatter and re-run the gate |
| `go vet` or staticcheck reports a finding | fix the code, re-run the gate |
| `go fix -diff` prints a change | apply it and re-run the gate |
| a test fails | fix the code or the scenario; never skip it |
| coverage is below 70 % | add the missing boundary test |
| a dupl group is actionable | extract the shared code |
| a dupl group is the documented interface/fake pattern | record why it stays |
| a product claim fails | reopen the owning phase; do not weaken the claim |
| an architecture doc contradicts the code | fix the doc in the same commit |
| a readiness line fails | fix the defect class, not just the cited line |

## Implementation notes

Not started.

## Review findings

None.
