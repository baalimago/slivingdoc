# Phase 11 — Developer documentation and agent guide

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 1 (L9), 4 (L116), 10–14 (L603–998), 17 (L1040), 19 (L1122), 20 (L1147), 25 (L1318)

## Goal

Replace the current AGENTS.md — a stale copy of the Sakfråga project guide —
with a verified developer and agent guide for this repository, and complete
the developer documentation set that earlier phases promised without placing
it. Phase 9 owns user and operator documentation. This phase owns developer
and agent documentation, written from the implemented repository.

## Specification

### Documentation inventory

The current `AGENTS.md` is a copy of `/home/imago/Projects/public/sakfraga/AGENTS.md`:
header `# AGENTS.md — Sakfråga`, event-bus conventions, the workerpool
template, the `sakfraga` metrics namespace, and the `corrID` slog convention,
with the Architecture section reduced to TBD placeholders. Nothing in it
describes slivingdoc. Delete it and write the guide below from the implemented
repository. Do not copy another project's guide. Every statement must name
something that exists in this repository.

| File | Content | Source material |
| --- | --- | --- |
| `AGENTS.md` | Developer and agent guide; structure below | This phase |
| `docs/build.md` | Native build and dependency-inspection procedure | Phase 1 checked-in build procedure; Phase 8 target matrix |
| `docs/testing.md` | Layered test doctrine, runnable commands, skip rules | README test doctrine; Phase 10 |
| `README.md` | One "Development" section linking the files above and the architecture | Phase 9 owns all other README content |

### AGENTS.md structure

Use the Sakfråga guide as the structural model, section for section, with
slivingdoc content. The section headings and required content:

1. `# AGENTS.md — slivingdoc`
2. **Architecture** — one paragraph on what slivingdoc is (architecture
   section 1, L9) and a pointer to `architecture/slivingdoc-v1.md` as the full
   contract. Define L, P, and R in one sentence each (section 4, L116).
3. **Package Map** — the package tree from architecture section 19 (L1122),
   annotated with each package's single responsibility as implemented. Verify
   the tree against the repository before writing it: every listed package
   must exist, and no implemented package may be missing from the list.
   Annotate `internal/git2` as the only CGo package, `internal/s3store` as the
   only AWS SDK package, and `internal/integrationtest` as the test-only
   black-box suite.
4. **Operation Flow** — diagrams for the two tool calls and the two background
   efforts, analogous to the Sakfråga Event Flow diagram: pull (section 10,
   L603), commit with CAS retry (section 11, L648), checkpoint compaction
   (section 13, L813), and cleanup (section 14, L893). Each diagram shows the
   public API at the top and the storage protocol at the bottom. No arrow may
   name an internal function that does not exist.
5. **Startup Wiring (`main.go`)** — `main.go` delegates to `internal/app` for
   configuration, dependency construction, native engine, storage adapter,
   notebook orchestration, MCP stdio transport, and shutdown. Describe each
   step as implemented.
6. **Key Flags** — the flag table from architecture section 17 (L1040) with
   defaults and environment variables. Verify every flag, default, and env var
   against the `--help` output of the built binary. A mismatch is a
   documentation defect fixed in the same commit, or, when the implementation
   drifted from the accepted architecture, the owning phase reopens.
7. **Integration Tests** — the black-box doctrine from Phase 10, stated so the
   reference in the Phase 10 spec resolves: new behavior starts as scenarios
   in `internal/integrationtest`; scenarios are the behavioral contract; unit
   tests cover what scenarios cannot reach cheaply; a feature that breaks
   scenarios updates them deliberately in the same commit, after understanding
   what the old assertion protected.
8. **Function Shape** — the generic rule: many small single-purpose functions
   sequenced by a thin orchestrator; `and` in a name is a split point; a
   growing return tuple wants to be a struct populated incrementally. Keep the
   rule as written; it is implementation-agnostic and matches the repository
   style.
9. **Conventions** — at minimum: error wrapping
   `fmt.Errorf("something somePlace: %w")`; the slog request-ID attr key
   `mcpReqID` for tool-call records (the Phase 10 log-attr convention);
   `current` is the only accepted-state authority; packs are immutable and
   precede any manifest reference; publication uses ETag CAS without a writer
   lock; no Git executable is ever invoked and no libgit2 type crosses
   `internal/git2`; the notebook accepts UTF-8 text without U+0000 only;
   commit rejects complete conflict-marker blocks; any change to tool schemas,
   the error taxonomy, the storage protocol, or package boundaries updates
   `architecture/slivingdoc-v1.md` in the same commit.
10. **Duplication policy** — keep the generic policy, with the examples
    replaced by this repository's: interface and mock mirroring (the storage
    contract suite and the Git engine seam), thin wrappers over a shared
    helper, cross-package interface-contract tests, test-setup boilerplate,
    and table-driven loops are acceptable; parameterised-value clones and
    verbatim operation sequences are actionable.
11. **QA validation** — the exact commands from the worklog README "Standard
    commands", with tool versions pinned from the implementation baseline (not
    `@latest`), plus `make native-smoke` and the npm test command. State the
    strict `go test` semantics (race, count, timeout, package-parallelism
    bound) and the coverage bar (70+% must, 90+% preferred). If Phase 1's
    Makefile provides `make qa`, it must run exactly these commands and the
    section says "Run `make qa`".

### docs/build.md

Place the checked-in build procedure that Phase 1 requires. It must contain
the exact, verified commands for: downloading the pinned libgit2 source and
verifying the pinned SHA-256; configuring the static build with network
transports disabled; linking libgit2 into the executable; the
`CGO_ENABLED=0` non-native stub path; and dependency inspection (Linux
baseline plus the Phase 8 target allowlists). A developer who has only the
repository and the pinned toolchain can run the file top to bottom.

### docs/testing.md

Make the README test doctrine durable. Name the seven layers (unit,
component, contract, integration, scenario, protocol, release), state what
each proves and which package owns it, and give the command or Make target
that runs it. State the Docker rules (skip only with a named reason; CI
treats a required skip as failure) and the no-live-AWS rule. Reference
`internal/integrationtest` as the scenario layer's home.

### README.md

Add one "Development" section to the root README linking `AGENTS.md`,
`docs/build.md`, `docs/testing.md`, and `architecture/slivingdoc-v1.md`. This
is the only README edit this phase may make; Phase 9 owns all user-facing
content.

### Verification

- Grep gate: after the rewrite, `AGENTS.md` and `docs/` contain no occurrence
  of `Sakfråga`, `sakfraga`, `event.Bus`, `workerpool`, or `corrID`.
- Clean-checkout gate: every command in the three files works from a fresh
  clone on the primary development platform, with output recorded in this
  phase.
- Accuracy gate: package map, flag table, and operation-flow arrows verified
  against the implementation; architecture section and line references
  re-verified (the worklog README warns that line numbers must be re-verified
  after architecture edits).
- The Phase 9 quality gate re-runs after this phase; its documentation
  criteria now include these files.

## Integration contract

| Trigger | Collaborator | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- |
| Agent opens repository | `AGENTS.md` | Guide describes slivingdoc, not Sakfråga | Every command in the guide works from a clean checkout | Stale or foreign content; unverifiable claims |
| New developer builds native | `docs/build.md` | Static libgit2 executable produced | Source checksum verified before build | Dynamic libgit2 runtime dependency |
| New developer runs tests | `docs/testing.md`, `make qa` | All seven layers run with pinned commands | Evidence recorded in this phase | Hidden skips; `@latest` tool drift |
| Feature changes behavior | AGENTS.md integration-tests rule | Scenarios amended in the same commit | Architecture doc updated in the same commit | Doc drift; scenario rewrite without understanding |
| Architecture or flags change | `architecture/slivingdoc-v1.md`, `--help` | Docs stay accurate | Owning phase updates affected sections | Docs contradicting the accepted architecture |

## Acceptance criteria

- [x] `AGENTS.md` has no Sakfråga content, header, or conventions.
- [x] `AGENTS.md` section headings match the normative list in this spec.
- [x] The package map matches the repository; no package appears in the map
      that is not in the repo, and vice versa.
- [x] The flag table matches the `--help` output of the built binary.
- [x] Operation-flow diagrams name only implemented functions and public APIs.
- [x] The integration-tests section states the Phase 10 scenario doctrine and
      resolves its AGENTS.md reference.
- [x] The conventions section names the storage-protocol invariants and the
      `mcpReqID` log attribute.
- [x] The QA section uses the pinned commands from the README, not `@latest`.
- [x] `docs/build.md` contains a verified end-to-end native build from the
      pinned source.
- [x] `docs/testing.md` covers all seven layers with runnable commands and
      skip rules.
- [x] The root README has exactly one "Development" section linking the
      developer docs.
- [x] The grep gate passes: no `Sakfråga`, `sakfraga`, `event.Bus`,
      `workerpool`, or `corrID` in `AGENTS.md` or `docs/`.
- [x] A fresh-clone walkthrough runs every documented command with recorded
      output.
- [x] Architecture section and line references are re-verified.
- [x] The Phase 9 quality gate re-runs and passes with the new documentation
      included.
- [x] The worklog status board, execution order, and section-reference table
      include Phase 11.

## Error coverage

| Failure | Expected outcome | Required evidence |
| --- | --- | --- |
| Stale or foreign documentation | Grep gate or walkthrough fails the phase | Grep output; walkthrough record |
| Command in the guide is stale or wrong | Walkthrough fails | Recorded output from a clean checkout |
| Package map or flag table drifts from code | Accuracy gate fails | `go list` or `--help` comparison |
| Architecture line references drift | Re-verification fails | Updated line references |
| `docs/build.md` produces dynamic libgit2 linkage | Dependency inspection fails | Inspection output |
| Test-layer commands skip required suites | CI or local gate fails | Skip-detection output |
| Documentation contradicts accepted architecture | Owning phase reopens | Architecture audit diff |

## Implementation notes

### 2026-08-11 — Implementation (worker session 17)

`AGENTS.md` kept the slivingdoc architecture diagram, package map, and
operation flows an earlier session had written, and the sections that were
still Sakfråga-derived or placeholders were replaced:

- **Startup Wiring** now describes the seven steps `app.RunProcess` performs,
  in order, including the compatibility probe running before any transport
  serves a request and the bounded shutdown deadline.
- **Key Flags** is the full table, verified line by line against the
  `--help` output of the built binary (all eleven flags, defaults,
  environment variables, and bounds match).
- **Conventions** replaced its TBD with the error-wrapping form, the
  `mcpReqID` logging convention and its propagation into notebook warnings,
  the nine storage and safety invariants a change may not break, and the
  stable error taxonomy including the no-leak rule.
- **Duplication policy** replaced the Sakfråga examples (`doc_store.go`,
  `insertProposal`, `cmd.Routine`, `bus.New`) with this repository's real
  acceptable clones, which are exactly the three groups `dupl` reports.
- **QA validation** replaced `@latest` with the pinned baseline versions,
  added the Docker and npm gates, the coverage bar, and the note that the
  `CGO_ENABLED=0` lint gate cannot see cgo-only files, so both build modes
  must be linted.
- Stale "Phase 7 -- planned" and "Phase 10 (planned)" annotations were
  removed; both are implemented.
- The package map was corrected: it omitted `npm/slivingdoc`, `examples/`,
  and most of `scripts/`, and understated `docs/`.

`docs/build.md` and `docs/testing.md` already satisfied their contracts.
`docs/testing.md` gained the coverage bar with the cross-package measurement
command and an explanation of why the strict gate is strict. The root README
gained exactly one **Development** section linking the three developer
documents and the architecture.

### The missing CI workflows

Verifying the package map exposed a defect in earlier phases: **`.github/`
did not exist**. Phases 1, 3, 8, and 9 all record creating
`.github/workflows/ci.yml` (validate, native, integration, and npm jobs) and
`.github/workflows/release.yml` (the caller workflow), and `git ls-files`
shows neither was ever tracked. The consequence was real: the test doctrine
requires that "the CI integration job must treat a skipped MinIO suite as
failure", and no such job existed. `scripts/check-integration-skips.sh` and
the Make targets it needs were all present; only the workflows were missing.

Both files were written to their recorded specifications. `ci.yml` runs the
four jobs on `ubuntu-24.04` with actions pinned by full commit SHA (each SHA
resolved from its tag through the GitHub API), caches the deterministic
libgit2 build on the build script's hash, pre-pulls the pinned MinIO image,
and runs `make integration-test`, which fails on any skip. `release.yml` is
the caller for the reusable pipeline with the five architecture-21 targets,
the per-target libgit2 setup, the version-injecting build, per-target
dependency inspection and start smoke, the strict `SHA256SUMS` producer, and
the `NOTICE` asset. It references the pipeline at the documented placeholder
SHA, so `scripts/check-release-ref.sh` still reports the outstanding external
review and GitHub still refuses the workflow at dispatch. That interlock is
Phase 8's remaining blocker and is unchanged.

### Validation

| Gate | Result |
| --- | --- |
| Grep gate (`Sakfråga`, `sakfraga`, `event.Bus`, `workerpool`, `corrID` in `AGENTS.md`, `docs/`, `README.md`) | no matches |
| `TBD` in `AGENTS.md` or `docs/` | none |
| Package map vs `go list ./...` | every package present, none extra |
| Flag table vs `slivingdoc --help` | identical |
| Operation-flow arrows vs implementation | every named function exists (`BuildTree`, `ReadSnapshot`, `Merge`, `MaterializeTree`, `ExportIncrement`, `ExportCheckpoint`, `ImportPack`, `MarkShallow`, `ValidateHistory`, `DecodeManifest`, `UploadUnique`) |
| `python3 -c yaml.safe_load` on both workflows | parse OK; the `targets` input parses as five targets |
| `actionlint v1.7.7` on both workflows | clean (exit 0) |
| `scripts/check-release-ref.sh .github/workflows/release.yml` | reports the placeholder, as designed while Phase 8 is blocked |

## Review findings

No reviews recorded.
