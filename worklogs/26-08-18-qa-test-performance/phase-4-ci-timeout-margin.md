# Phase 4 — CI timeout margin

**Status:** Complete

**Worklog:** [README](README.md)

## Goal

Eliminate the CI-only `internal/integrationtest` timeout without relaxing the
required 30-second package timeout or reducing any behavioral assertion.

## Specification

The GitHub Actions run 32156521209 timed out at 30.343s while four scenarios
were still waiting on helper processes. Consolidate only behavior that uses
the same helper configuration and does not require a new public process
boundary: run the Unix path-security fixtures after the existing stdio
transport flow; reuse the `NO_COLOR=1` record-shape helper for the no-colour
assertion; and rely on the stronger, existing probe-failure scenario rather
than start a duplicate `bad-store serve` helper.

## Integration contract

| Trigger | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- |
| Shared fake stdio chain | Both transport and path-security assertions pass | One server handles both compatible sequences | State or logs leaking across test runs |
| `NO_COLOR=1` record helper | Structured logs contain no ANSI escapes | Record-shape and colour contracts both remain observed | Starting a duplicate same-config server |
| Probe-failing serve | Startup failure remains redacted and categorized | One stronger scenario covers the failure | Removing the negative control |

## Acceptance criteria

- [x] The three consolidations retain every former public assertion. — the
  shared stdio chain still lists both tools, runs pull/commit, rejects all
  three native path attacks, and proves protocol-only output; logging still
  proves both colour states; the retained probe scenario has the stronger
  category and redaction assertions.
- [x] `make test` passes with the exact race, count, timeout, package scope,
  and coverage floor. — the full gate passed at 82.6% coverage.
- [x] The integration package completes with useful CI margin. — its strict
  `-race -count=3 -timeout=30s` package command passed after eliminating
  three helper boots per pass (nine in the required command).

## Error coverage

| Failure | Expected result | Evidence |
| --- | --- | --- |
| Path fixture request is accepted | Scenario fails with its existing INVALID_REQUEST assertion | shared stdio chain |
| NO_COLOR emits ANSI | Record-shape scenario fails | combined logging assertion |
| Probe-failing serve accepts | Integrity startup scenario fails | existing stronger scenario |

## Implementation notes

2026-08-18 — CI exposed a 30.343s package timeout with four helper-backed
scenarios still running. `TestScenarioTransportStdioProcess` now performs the
Unix path-security flow before one shared shutdown. The `NO_COLOR=1`
record-shape helper also asserts the absence of ANSI escapes, so the colour
scenario starts only the default-colour helper. The startup-probe scenario
already makes the config test's duplicate `bad-store serve` negative control
strictly stronger, so the duplicate process was removed. `make lint`, the
full `make test` gate (82.6% coverage), the 35-test Node launcher script, and
`dupl -t 80` (zero groups) passed.
