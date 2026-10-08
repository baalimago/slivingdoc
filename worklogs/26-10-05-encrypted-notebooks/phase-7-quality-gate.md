# Phase Seven: quality gate

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Run the complete repository gate and close every worklog readiness item against the implemented tree.

## Specification

Run `make qa` with the repository's required native and Docker dependencies. Do not add a test skip, build tag, environment switch, target, or shortened timeout. Run the README readiness checklist and the separate duplication signal. Review the full change for architecture-document updates, unsafe secret output, plaintext active objects, unexpected gateway changes, and generated artifacts.

Resolve failures in the owning phase. Do not weaken an assertion or budget to obtain a green result. Keep phase statuses truthful and record the exact gate results in the README session journal.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| Run the repository QA gate | Full Go, race, coverage, and npm suites | Every required gate passes | Record command outcome | Test skips, reduced coverage, or narrowed suite |
| Run the readiness checklist and duplication signal | Worklog tree and repository tools | Checklist passes; duplication findings are classified by repository policy | Record the result | Suppressing actionable duplication without rationale |
| Review the complete diff | Maintainer and architecture docs | Behavior and docs agree; no gateway code changed | Record remaining findings | Cloud mutation or unreviewed generated files |

## Acceptance criteria

| Criterion | Test or command |
| --- | --- |
| Repository-required formatter, linter, static analysis, Go, and npm gates pass | `make qa` |
| Every line in the README readiness checklist passes | Commands in the README checklist |
| The separate duplication signal has no actionable unresolved clone | Run the command named in the README validation policy and classify its findings |
| The final diff contains only approved source, test, architecture, and documentation files | `git status --short` and full diff review |
| Worklog statuses and journal match actual results | README review |

## Error coverage

| Failure | Expected outcome | Test or command |
| --- | --- | --- |
| Any QA gate fails | Phase remains incomplete; correct the owning defect | `make qa` |
| A readiness item fails | Phase remains incomplete; correct the worklog or tree | Commands in the README checklist |
| Duplication signal reports an actionable clone | Refactor or record why it is not actionable under repository policy | The command named in the README validation policy |
| Diff contains an unapproved gateway or generated change | Remove it and rerun the complete review | `git status --short` and full diff review |

## Implementation notes

Not started.

## Review findings

None.