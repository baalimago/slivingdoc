# Phase 2 — Scenario chains

**Status:** Complete

**Worklog:** [README](README.md)

## Goal

Reduce avoidable helper startup only where a longer scenario retains the complete external contract.

## Specification

Audit every `spawnHelper` call in `internal/integrationtest`. Retain one-shot
CLI process boundaries and distinct startup configurations. A longer chain
may share a running stdio process only for cases with identical configuration
and lifecycle guarantees; it must retain each original protocol, output, log,
and error assertion. Keep `t.Parallel` unless measured evidence proves that a
specific sequential chain is faster on the target runner.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- |
| Consolidated serve scenario | Same configured helper, MCP client | Every former assertion remains true | One normal process lifecycle is observed | Losing startup/config/output coverage |
| One-shot CLI chain | Separate pull/commit helpers | Same reports and remote state | Each command remains a new process | Reusing a command process |

## Acceptance criteria

- [x] Every retained or consolidated scenario maps to the assertions it preserves. — audit of `spawnHelper` call sites found existing chains in `TestScenarioTransportStdioProcess`, `TestScenarioPathSecurityProcess`, and the CLI state scenarios.
- [x] No different startup configuration shares a helper process. — every remaining process test varies startup flags/environment, terminal shape, or one-shot command lifecycle.
- [x] Timing evidence shows no regression. — the shared-service change lowers the real full-gate control from 42.58s to 33.08s; no helper chain was changed.

## Error coverage

| Failure | Expected result | Evidence |
| --- | --- | --- |
| Shared serve process exits early | Scenario fails with process diagnostics | Existing helper wait/assertions |
| Configuration leaks between logical cases | Scenario detects wrong log/output/config behavior | Consolidated scenario assertions |

## Implementation notes

2026-08-18 — The initial audit rejected a broad sequential rewrite:
configuration rows require a new process because startup configuration is
immutable; logging rows require distinct `LOG_LEVEL` or `NO_COLOR`; terminal
coverage needs a PTY; and `pull`/`commit` chains must remain separate one-shot
processes. Phase 4 later identified three narrower compatible consolidations,
including the identical transport and Unix path-security server lifecycle.
