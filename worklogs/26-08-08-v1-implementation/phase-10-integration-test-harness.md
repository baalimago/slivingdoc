# Phase 10 — Integration test harness

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 2 (L26), 7 (L186), 10–18 (L603–1093), 20 (L1147)

## Goal

Build `internal/integrationtest` as the black-box behavioral contract for the
entire server. One scenario per architecture usecase drives the implementation
of Phases 1 through 7 exclusively through the public MCP API. The scenario
catalog is the spec: where prose and a passing scenario disagree, the scenario
wins.

## Specification

### Contract role

The scenario suite is the validation evidence for the earlier phases. Every
architecture usecase in the catalog below has a named scenario file. A feature
that changes behavior amends or adds scenarios in the same commit (AGENTS.md).
Earlier phases keep their component and contract boundary suites (the storage
contract suite in Phase 3, the notebook boundary in Phase 5, the checkpoint
MinIO test in Phase 6) because those suites reach internal seams the black box
must not. Where an earlier phase's acceptance criterion is exactly a black-box
usecase, this phase's scenario is its evidence; the validation mapping below
records the correspondence.

### Package layout

Create `internal/integrationtest` with these files:

```text
main_test.go     TestMain: compose fast-path or testcontainers MinIO, AWS env
                 sanitization, shared bucket, per-iteration reset, one binary
                 build for process scenarios
harness.go       Harness struct, NewHarness(t): per-test isolation, in-process
                 app wiring, store seam, failpoint hooks, MCP sessions,
                 recorder, log capture, cleanup
scenario.go      Scenario DSL, RunScenario, barrier and waiter helpers
mcpclient.go     In-memory MCP JSON-RPC client over the SDK handler; process
                 client for the transport subset
recorder.go      Tool-call and storage-operation recorder
logcapture.go    slog capture scoped to the harness's own MCP request IDs
assertions.go    Polling filesystem, S3, manifest, and log assertions
faults.go        Fault-injecting store wrappers and failpoint install helpers
scenario_*.go    The catalog, one file per usecase group (see below)
```

### The black box

The harness wires the server exactly as production startup does, using the
`internal/app` constructors, with per-harness bucket prefix, workspace root,
private root, logger, and failpoint hooks. The only entry into the black box is
MCP JSON-RPC: initialize, tool listing, and the two tool calls. Scenarios never
call notebook, git, workspace, or storage functions directly.

The store seam is a `StoreFactory` that builds the semantic object-store
interface (architecture section 20, L1147). The default builds the real
`s3store` adapter against MinIO. Scenarios may substitute the deterministic
fake or fault-injecting wrappers from `faults.go`. The wrappers record
operation counters (CAS attempts, pack uploads, gets, deletes) that scenarios
assert on; they also induce the failures MinIO cannot reliably produce:
precondition-failure injection, accept-then-error writes, ambiguous responses,
delete failures, and corrupt or missing objects.

The notebook exposes the architecture section 15 (L958) injectable failpoints
as a hook the harness can install and remove per scenario. Recovery scenarios
activate one failpoint at a named operation boundary and drive the next call
through the public API.

The app includes the MCP request ID in every tool-call-related slog record
under attr key `mcpReqID`. The harness's log capture routes records into the
harness whose request IDs it owns, so parallel scenarios cannot see each
other's log lines. This mirrors the correlation-ID scoping used by the sakfraga
harness; `mcpReqID` is this phase's log-attr convention.

Scenarios may run with `t.Parallel()`. Isolation comes from the per-test S3
prefix and the per-test temp workspace and private roots. `go test ./...` from
the README QA command includes this package; scenarios that need MinIO skip
with a named reason when Docker is unavailable, and the CI integration job
treats a skip as failure.

### Scenario DSL

```go
type Scenario struct {
    Name   string
    Entry  Entry
    Expect Expectations
}

type Entry struct {
    Calls []ToolCall // ordered; Client selects the session, default "default"
}

type ToolCall struct {
    Client  string // concurrent scenarios use named sessions
    Tool    string // "notes_pull" | "notes_commit"
    Path    string // absolute request path
    Message string // notes_commit only
    Barrier string // optional named barrier
    Expect  CallExpectation
}

type CallExpectation struct {
    OK        bool                  // one text item exactly "OK", no structured content
    ErrorCode string                // stable category, e.g. "CONTENT_CONFLICT"
    Retryable *bool
    Files     []FileExpectation     // path plus one-based inclusive ranges
    Recovery  *RecoveryExpectation  // stage, remoteAccepted, resynchronized
    NoText    []string              // forbidden substrings anywhere in the result
}

type Expectations struct {
    FS   FSAssertions    // visible-directory state after the run
    S3   S3Assertions    // manifest, object, and counter state, polled
    Logs *LogExpectations
}
```

A named `Barrier` on a call blocks that call until every session has completed
its preceding calls and reached the same barrier name. Because pulls are
read-only and no commit can start before the barrier, every session observes
the same manifest; the subsequent commits deterministically produce one CAS
winner and one retry. The loser's retry is asserted through the wrapper's CAS
counter.

`RunScenario(t, h, s)` sets up the harness wiring, runs the entry scripts with
barrier release, waits for asynchronous effects (checkpoint, cleanup) with
polling, and then asserts. Persistence is asynchronous, so every S3 and
filesystem assertion polls until it settles; polling removes the checked-too-
early race, it does not mask a wrong final value.

### Relationship to earlier phases

| Earlier phase | Black-box evidence supplied by Phase 10 |
| --- | --- |
| 2 Git state engine | Every pull and commit scenario runs the real libgit2 engine: snapshots, merges, root commit, packs. |
| 3 S3 storage protocol | Manifest, pack, CAS, checkpoint, cleanup, orphan, stale-reader, and integrity scenarios against real MinIO plus fault wrappers. |
| 4 Managed workspaces | Path security, first-pull baseline, L rewriting, special-file, and root-overlap scenarios. |
| 5 Notebook operations | Pull, commit, conflict, retry, ambiguity, and recovery scenarios. |
| 6 Checkpoints and scale | Checkpoint trigger, stable-prefix compaction, competing workers, retention, cleanup, and cleanup-failure scenarios. |
| 7 MCP application | Schema, result shape, error envelope, transport and configuration scenarios. |

### Deferred work

Load tests via `go benchmark` are deliberately excluded. A load number is only
meaningful when the bottleneck is known to be slivingdoc and not the local
MinIO, the race detector, or CI noise; that attribution is not guaranteed.
Phase 6's sustained-load harness for warm and cold readers remains the interim
measurement. The npm launcher and release artifacts stay in Phase 8.

## Integration contract

The catalog is the contract. Column values reference architecture sections;
each row's file is authoritative for its usecase.

| Scenario (file) | Trigger | Collaborators | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- | --- |
| `scenario_validation_test.go` · tool listing | initialize + tools/list | MCP server | Exactly `notes_pull` and `notes_commit` | — | No third tool |
| · strict schema | pull/commit with unknown field, null, relative path, oversized path or message, blank message, U+0000, invalid UTF-8 | MCP server | `INVALID_REQUEST` envelope (§2, L56) | — | No S3 or filesystem mutation |
| · result shape | successful pull and commit | MCP server | One text item exactly `OK`, no structured content (§2, L63) | — | No commit ID, pack key, or internal value in result |
| `scenario_pull_test.go` · first pull | pull into nonempty L, empty remote | Real engine + MinIO | `OK`; L retains valid local additions (§10, L644) | P records empty-tree baseline | No remote state created |
| · warm pull | pull, no changes | Real engine + MinIO | `OK` | P baseline advances to R | Full checkpoint download when cached |
| · pull after remote advance | second client commits, first client pulls | Real engine + MinIO | `OK`; local add/mod/delete rebased on R (§10, L637) | Baseline advances | Any mergeable local change discarded |
| · cold pull | empty cache, populated remote | Real engine + MinIO | `OK` | Only missing descriptors downloaded (§10, L617) | State reconstructed by LIST |
| · pull conflict | overlapping local edit and remote change | Real engine + MinIO | `CONTENT_CONFLICT` with exact paths and ranges; markers in L (§10, L637) | R recorded as new baseline | L reverted to pre-call bytes |
| · stale reader | pack deleted during read | MinIO + deletion barrier | `OK`; final L equals current head (§10, L632) | Reader rereads `current` and restarts | State guessed from object names |
| · cache corruption | cached pack bytes corrupted | Fault wrapper | Fresh download or `STORAGE_INTEGRITY` (§8.3, L369) | No false cache hit | Corrupt bytes reach L |
| `scenario_commit_test.go` · first commit | commit on empty notebook after pull | Real engine + MinIO | `OK`; independent pull sees files (§11, L703) | Root commit, state-complete checkpoint pack, `If-None-Match: *` (§11, L706) | Increment pack for first publication |
| · normal commit | one file edited | Real engine + MinIO | `OK`; independent pull sees change (§11, L652) | One increment pack precedes manifest CAS | Full checkpoint for a normal increment |
| · no-change commit | commit with L equal to R | Real engine + MinIO | `OK` (§11, L699) | L and P synchronized to R | Publication ID, pack, commit, or CAS (counters zero) |
| · commit without pull | commit on unmanaged path | Real engine + MinIO | `INVALID_REQUEST` (§11, L658) | — | Any S3 mutation |
| · L rewrite after commit | conflicting-free commit with stale L | Real engine + MinIO | `OK`; L equals accepted merged tree (§11, L697) | P baseline advances | Stale bytes kept in L |
| · two disjoint commits race | two sessions, barrier, separate files | MinIO + barrier | Both `OK`; one linear accepted state with both changes (§11, L712) | Loser merges and retries (CAS counter) | Lost update |
| · two overlapping commits race | two sessions, barrier, same file | MinIO + barrier | One `CONTENT_CONFLICT` with exact path (§11, L721) | Accepted state remains valid | False success |
| · CAS loss retry | precondition failure injected once | Fault wrapper | `OK` (§11, L726) | New attempt with new publication ID, generation, key, commit, and pack | Losing pack published at a later generation |
| · retry exhaustion | precondition failure injected always | Fault wrapper | `REMOTE_BUSY` (§11, L735) | Caller files preserved | `OK`; unbounded retry |
| · ambiguous pack upload | upload accepted, response lost | Fault wrapper | `OK` (§11, L759) | Unique key read; SHA-256 and size validated | Pack alone treated as publication |
| · ambiguous CAS, ID found | manifest accepted, response lost | Fault wrapper | `OK` (§11, L738) | `current` read; publication ID found in a descriptor | Duplicate logical change |
| · unprovable CAS | manifest accepted, follow-up read fails | Fault wrapper | `STORAGE_FAILURE` (§11, L741) | Visible workspace preserved | `OK`; automatic republication |
| `scenario_conflict_test.go` · marker grammar | commit with LF, CRLF, multiple, near-match, and literal marker blocks | Real engine | `CONTENT_CONFLICT` naming every path and range (§12, L801) | Rejection before S3 mutation | Marker bytes in accepted state |
| · resolution and republish | resolve markers, commit again | Real engine + MinIO | `OK`; accepted state includes resolution (§12, L795) | — | Marker bytes in R |
| · conflict after remote movement | R moves between conflict and resolution | Real engine + MinIO | Second merge on retry; `OK` after resolution (§12, L798) | — | Acceptance against a stale baseline |
| `scenario_checkpoint_test.go` · threshold trigger | tail reaches configured low threshold | MinIO + real engine | Checkpoint eventually indexed (§13, L829) | Complete pack uploaded before manifest CAS | Writer lock |
| · writers advance during build | commits continue while checkpoint builds | MinIO + contention wrapper | Later increments remain after checkpoint (§13, L864) | Stable prefix alone replaced | Accepted increment loss |
| · competing checkpoint workers | two workers, barrier | MinIO + barrier | One physical index wins (§13, L888) | Loser object unreferenced | Duplicate logical commit |
| · retention | second checkpoint replaces first | MinIO | Active plus one previous generation readable (§14, L898) | Retained chain reconstructs replaced state | Older descriptor removed before cutoff |
| · cleanup after CAS | successful checkpoint, old generations present | MinIO | Old unretained generation deleted best effort (§14, L909) | Roots reread before each delete batch | Active or retained descriptor deletion |
| · cleanup fence | proposals at and after cutoff | MinIO + seeded proposals | Only candidates at or before cutoff considered (§14, L923) | — | Proposal after cutoff touched |
| · malformed keys | junk keys in pack namespaces | MinIO + seeded junk | Cleanup ignores them (§14, L939) | Junk keys untouched | Malformed key parsed as candidate |
| · cleanup failure | delete fails | Fault wrapper | Commit still `OK` (§14, L917) | Warning observable in logs | Commit failure |
| `scenario_recovery_test.go` · boundary failpoints | failpoint at each operation boundary | Failpoint hooks | `RECOVERY_FAILURE` with `stage`, `remoteAccepted`, `resynchronized` (§15, L989) | Authoritative resync attempted | `OK` for that call |
| · repair impossible | failpoint on resync | Failpoint hooks | `RECOVERY_FAILURE`; P marked recovery-required (§15, L993) | Next MCP call retries resync first | New work before resync |
| `scenario_integrity_test.go` · corrupt manifest | manifest bytes corrupted | Fault wrapper | `STORAGE_INTEGRITY` (§9.2, L423) | Old index retained | State guessed from object names |
| · corrupt pack | pack checksum mismatch | Fault wrapper | `STORAGE_INTEGRITY`; no import | — | Corrupt data in L |
| · missing object, unchanged ETag | pack GET 404, ETag unchanged | Fault wrapper | `STORAGE_INTEGRITY` (§10, L634) | — | Same manifest retried forever |
| · startup probe failure | endpoint without S3 semantics | Process | `INCOMPATIBLE_STORE`; exit nonzero before transport (§9.4, L577) | Redacted stderr diagnostic | Serving tool calls |
| `scenario_path_security_test.go` · traversal | symlink in an existing path component | Stdio | `INVALID_REQUEST` (§18.2, L1109) | — | Link followed |
| · special files | FIFO or socket inside L | Stdio | `INVALID_REQUEST` (§18.2, L1116) | — | Special file read |
| · root escape | path outside workspace root | Stdio | `INVALID_REQUEST` (§18.2, L1113) | — | Write outside root |
| · overlapping roots | private root below workspace root | Config | Startup fails (§17, L1070) | — | Serving calls |
| `scenario_transport_test.go` · stdio process | real binary over stdio | Process | initialize, listing, and both calls succeed (§18.1, L1097) | Logs on stderr only | Diagnostic bytes on stdout |
| `scenario_config_test.go` · precedence | flag, env, and default for one setting | Process | Flag wins over env, env over default (§17, L1079) | — | Empty flag value falling back to env |
| · invalid configuration | missing bucket, bad bounds | Process | Nonzero exit, one redacted diagnostic (§17, L1091) | — | Secret leakage |
| · version and help | `--version`, `--help` | Process | Output on stdout, exit zero (§17, L1088) | No native init or S3 access | Configuration loaded |
| `scenario_error_taxonomy_test.go` · category mapping | one scenario per category | Harness | `code`, `retryable`, `message`, `files` always present; `recovery` only for `RECOVERY_FAILURE` (§2, L81) | Paths relative and normalized | Git terminology in recovery instructions |
| · redaction | error text and logs from failure scenarios | Harness | No credential, S3 key, private path, or Git ID (§2, L86) | — | Secret in any output |

## Acceptance criteria

- [x] The package runs under the README QA command without live AWS access.
- [x] MinIO scenarios skip with a named reason without Docker; CI fails on skip.
- [x] Every catalog row above has a passing scenario before this phase completes.
- [x] The only entry to the server is MCP JSON-RPC; scenarios never call internal functions directly.
- [x] The store seam defaults to the real `s3store` adapter and supports the deterministic fake and every fault wrapper.
- [x] Store wrappers record CAS, upload, get, and delete counters asserted by scenarios.
- [x] Failpoint hooks install and remove cleanly around recovery scenarios.
- [x] Named barriers make two-writer and competing-worker races deterministic.
- [x] Every S3 and filesystem assertion polls until settled.
- [x] Parallel scenarios cannot observe each other's log records; `mcpReqID` scoping is proven.
- [x] The app logs the MCP request ID under `mcpReqID` for tool-call records.
- [x] Successful tools are proven to return one text item exactly `OK` and no structured content.
- [x] Every error category is proven through the MCP envelope with `code`, `retryable`, `message`, and `files`.
- [x] `RECOVERY_FAILURE` is the only category that carries `recovery`.
- [x] Error file paths are relative normalized paths; request paths are absolute.
- [x] No scenario result or log contains a credential, S3 key, private path, or Git ID.
- [x] No-change commit, marker rejection, and invalid requests are proven to cause zero S3 mutation.
- [x] First commit and first pull prove root-commit, empty-tree baseline, and `If-None-Match: *` behavior.
- [x] Retry, ambiguity, and exhaustion scenarios prove publication-ID lookup and never-`OK`-on-uncertainty.
- [x] Checkpoint, retention, cleanup, and cleanup-failure scenarios prove the §13–14 contract.
- [x] Recovery scenarios prove the generic path at every operation boundary.
- [x] Stale-reader restart and corrupt-object scenarios prove the §10 and §9.3 integrity behavior.
- [x] Path-security scenarios pass over stdio.
- [x] Process scenarios prove stdout protocol-only.
- [x] Config scenarios prove precedence, invalid-config exit, and version/help startup rule.
- [x] Each scenario file cites its architecture sections and line numbers.
- [x] The validation mapping above is complete: each earlier phase's black-box acceptance area has evidence.
- [x] The full QA matrix from the README passes with this suite included.

## Error coverage

| Failure | Expected outcome | Required test |
| --- | --- | --- |
| Tool JSON is malformed | SDK invalid-params response | `scenario_validation_test.go` |
| Blank or whitespace-only message | `INVALID_REQUEST` before scan or S3 access | `scenario_validation_test.go` |
| Commit without managed pull | `INVALID_REQUEST` before S3 access | `scenario_commit_test.go` |
| Pull merge conflict | `CONTENT_CONFLICT` with markers in L | `scenario_pull_test.go` |
| Commit contains a marker block | `CONTENT_CONFLICT` before S3 access | `scenario_conflict_test.go` |
| CAS precondition failure | Merge and retry with a fresh proposal | `scenario_commit_test.go` |
| Retry exhaustion | `REMOTE_BUSY`, caller files preserved | `scenario_commit_test.go` |
| Pack upload response lost | Unique key read; SHA-256 and size validated | `scenario_commit_test.go` |
| CAS response lost | Publication lookup decides; `OK` only when found | `scenario_commit_test.go` |
| CAS status unprovable | `STORAGE_FAILURE`, no automatic republication | `scenario_commit_test.go` |
| Pack GET fails | `STORAGE_FAILURE`, baseline unchanged | `scenario_integrity_test.go` |
| Pack checksum is corrupt | `STORAGE_INTEGRITY`, no import | `scenario_integrity_test.go` |
| Missing object with unchanged ETag | `STORAGE_INTEGRITY`, no infinite retry | `scenario_integrity_test.go` |
| Startup probe fails | `INCOMPATIBLE_STORE`, exit nonzero before transport | `scenario_integrity_test.go` |
| Unexpected failure after local mutation | `RECOVERY_FAILURE` with stage and resync fields | `scenario_recovery_test.go` |
| Resync impossible | Recovery-required marker; next call resyncs first | `scenario_recovery_test.go` |
| Checkpoint build or upload fails | Accepted current unchanged | `scenario_checkpoint_test.go` |
| Checkpoint CAS repeatedly loses | Bounded effort, retry on later trigger | `scenario_checkpoint_test.go` |
| Cleanup delete fails | Warning and retry opportunity, no commit failure | `scenario_checkpoint_test.go` |
| Symlink traversal or special file | `INVALID_REQUEST` | `scenario_path_security_test.go` |
| Invalid configuration | Nonzero exit, one redacted diagnostic | `scenario_config_test.go` |
| Secret in error text or log | Redaction failure | `scenario_error_taxonomy_test.go` |

## Implementation notes

### 2026-08-11 — Completion and hardening (worker session 17)

An earlier session left the harness core and a first scenario catalog in
place but never ran the strict gate over the package. This session reviewed
the whole package against the catalog, fixed what the review found, and
completed the missing rows.

**Two blocking defects were found before any review work.**

`go test -race -count=3 -timeout=30s ./internal/integrationtest/` **timed
out**: one count took 26.5 s, so the required three could not finish. No
scenario used `t.Parallel()`, although the phase specification requires
parallel-capable scenarios and the harness already gives every test its own
S3 prefix, workspace root, private root, recorder, and log capture. Making
the scenarios parallel took the same gate to **13.6 s**. This was the
package's real design defect: the isolation existed, nothing used it.

`internal/notebook.TestRecoverFailpointReportsFailedResync` **failed
deterministically** on the repository as found. It left the workspace
`Recover` failpoint permanently installed and then asserted that the next
call self-heals, which that failpoint makes impossible. The test now asserts
the durable recovery-required state, proves a later call keeps reporting
`RECOVERY_FAILURE` while the condition persists, clears the failpoint, and
only then requires the self-heal. A third pre-existing failure,
`internal/git2.TestNoGitExecutableOrGit2goImport`, was hidden by output
truncation: `scenario_path_security_unix_test.go` imported `syscall` for
`Mkfifo`, which the module-wide seam scan forbids; it now uses
`golang.org/x/sys/unix` as the decision log requires.

**Harness fidelity.** Barrier waits were unbounded and ignored the context,
so a scenario that failed an assertion before releasing its barrier stranded
an operation inside the store and turned one local failure into a
package-wide timeout; every wait is now bounded and context-aware. The
accept-then-error injections returned the real ETag beside the injected
error, which no real adapter can do and which would have let a publication
path resolve its own ambiguity under the harness while taking the recovery
path in production. `DeleteObjects` keyed every injection on the empty
string, so keyed delete injections silently never fired, and the recorder
recorded deletes under the empty key, so `CountKey(OpDelete, k)` was
permanently zero and any "this object was never deleted" assertion passed
vacuously. The log capture inverted `slog` precedence (a bound attr beat a
call-site attr) and its group handler dropped every previously bound attr.
`Recorder` embedded the store interface, so a new `ObjectStore` method would
have been forwarded silently and counted nowhere; it now holds an explicit
field. `FSSnapshot` discarded its walk error, so an unreadable or missing
directory produced an empty map and "nothing was imported" assertions passed
on a missing directory.

**Scenario honesty.** The two writer races were rewritten from timing races
into barrier-driven ones: the loser now parks on the conditional create
until the winner's publication is accepted, so the outcome no longer depends
on goroutine scheduling, and both run against real MinIO as the catalog
specifies. The competing-checkpoint-workers scenario had an unsynchronized
window in which the loser's upload could land after the winner's inline
cleanup had already enumerated the namespace; the releases are now ordered.
The redaction assertion forbade a credential literal that never entered
scope and checked neither Git object IDs nor Git vocabulary, so a
`RECOVERY_FAILURE` leaking an object ID or telling a caller to run `git
merge` would have passed; credential redaction moved to the configuration
scenario, where a secret genuinely reaches the diagnostic. `RetryLimit: 0`
did not configure zero retries — the harness mapped zero to the default of
eight — so the documented zero boundary was unreachable; the overrides are
pointers now. Further vacuous or weakened assertions were repaired in the
CAS-loss retry (distinct publication keys, not just call counts), the
ambiguous pack upload (the read-back now validates size and digest and a
mismatch must refuse), the L-rewrite scenario (which never constructed a
stale L), the malformed-key cleanup scenario (which had no positive
control), the cleanup-failure scenario (whose warning did not discriminate
between the three steps that emit it), and the strict-schema table (whose
`notes_commit` rows were all satisfied by commit-without-pull rather than by
the message rule under test).

**Rows added.** Overlapping roots (the only catalog row with no test at
all), special files in an attributable location, a pack GET transport
failure mapping to `STORAGE_FAILURE`, a failed checkpoint pack upload, a
checkpoint CAS that loses every attempt, the notebook content rules
(invalid UTF-8 and U+0000 in a file), and the no-mutation recovery
boundaries (`Scan` and `Stage`), which prove the generic recovery path does
**not** fire where nothing was mutated.

**Two contract facts were confirmed against the implementation** rather than
assumed, after the first drafts of the new tests failed. A compaction always
selects the oldest `threshold` increments, so a checkpoint retried after a
failure still cuts at the original generation and leaves later increments in
the tail. And with retention 1, the checkpoint of the first compaction is
the retained one after the second compaction; the chain that becomes
collectable is the one the first compaction had itself retained.

**Store selection.** The catalog names MinIO as the collaborator for most
rows. Rows whose evidence must be real HTTP conditional writes run against
MinIO: both writer races, the competing checkpoint workers, the stale-reader
restart, and the cleanup after a successful checkpoint. The rest run against
the deterministic fake, which is contract-equivalent by construction because
`internal/storage/contract` is one suite executed against both the fake and
the adapter. `doc.go` records this rule. That keeps the whole catalog inside
the strict race gate while the rows that need real HTTP still get it.

### Validation

| Gate | Result |
| --- | --- |
| `go test -race -timeout=30s -count=3 -p 1 ./...` | pass, 11 test packages (integrationtest 14.4 s) |
| `go test -race -count=3 -timeout=30s ./internal/integrationtest/` | pass, 13.8 s (was a timeout) |
| `CGO_ENABLED=0 go test -count=3 -timeout=30s ./...` | pass |
| `make integration-test` | pass, `integration-test: no skips` |
| `make smoke` | pass |
| `npm test --prefix npm/slivingdoc` | pass, 33 tests, 0 failures |
| `go vet ./...` and `CGO_ENABLED=0 go vet ./...` | pass |
| `staticcheck ./...` (both build modes) | pass |
| `gofumpt -l .` | clean |
| `go fix -diff ./...` | clean |
| `dupl -t 80 .` | 3 groups, all acceptable (see below) |
| coverage | 84.1 % across the module; the black-box suite alone covers 66.8 % |

The three remaining `dupl` groups are acceptable under the AGENTS.md policy:
the `internal/notebook` and `internal/workspace` fake-engine mirrors and the
`internal/git/merge_test.go` pair are pre-existing interface mirrors, and
`internal/app/process_test.go` ↔ `internal/integrationtest/harness.go` is
the same success-envelope assertion written independently in the package
under test and in the black-box suite. Merging the last one would make the
black-box suite depend on a test helper of the code it is meant to observe
from outside.

## Review findings

### Review 2 (2026-08-11) — package audit before completion

Three independent audits (harness support code, catalog coverage, scenario
quality) ran against the package as the earlier session left it. Findings
are summarized in the implementation notes above and were all fixed in the
same session; no finding remains open.

| ID | Severity | Area | Summary |
| --- | --- | --- | --- |
| R2-01 | Critical | package | The strict race gate timed out: no scenario used `t.Parallel()`. |
| R2-02 | Critical | notebook | `TestRecoverFailpointReportsFailedResync` failed deterministically. |
| R2-03 | Critical | git2 seam | `scenario_path_security_unix_test.go` imported `syscall`, which the seam scan forbids. |
| R2-04 | Critical | taxonomy | Redaction checked no Git object ID and no Git vocabulary, and forbade a credential that never entered scope. |
| R2-05 | Critical | commit | Both writer races were timing races, not barrier-driven. |
| R2-06 | Critical | checkpoint | Competing workers had an unsynchronized window before the winner's cleanup enumeration. |
| R2-07 | Critical | path security | The overlapping-roots row had no test; the special-file row was not attributable. |
| R2-08 | Major | faults | Unbounded, context-ignoring barriers could deadlock the package. |
| R2-09 | Major | faults | Accept-then-error returned an ETag no real adapter returns. |
| R2-10 | Major | faults/recorder | Keyed delete injections never fired; keyed delete counters were always zero. |
| R2-11 | Major | logcapture | Bound attrs beat call-site attrs, and groups dropped bound attrs. |
| R2-12 | Major | harness | `RetryLimit: 0` silently meant the default of 8. |
| R2-13 | Major | harness | `FSSnapshot` swallowed walk errors, so a missing directory read as empty. |
| R2-14 | Major | harness | `RunScenario` called `t.Fatalf` from client goroutines. |
| R2-15 | Major | pure tests | The validation table shared one backing array; six of nine cases proved nothing. |
| R2-16 | Major | catalog | Four error-coverage rows had no test at all. |
| R2-17 | Major | package | 17 cited architecture subsections did not exist and no file cited line numbers. |
| R2-18 | Major | main_test | A fixed shared cache directory leaked state across runs; `stdoutText` was dead and broken. |
| R2-19 | Major | config | The redaction subtest could not fail; the diagnostic never formats the planted secret. |
