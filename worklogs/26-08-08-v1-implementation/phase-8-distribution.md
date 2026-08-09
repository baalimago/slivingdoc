# Phase 8 — Distribution and native releases

**Status:** Blocked (external deliverable pending)

All in-repository deliverables of this phase are complete and validated (npm
launcher, version injection, dependency-inspection matrix, release grammar and
workflow-ref guards, caller workflow). The phase cannot finish because the one
remaining deliverable is external: the reusable-workflow change must land as a
reviewed commit in the separately owned `baalimago/simple-go-pipeline`
repository, and worker sessions operate with git in read-only mode, so only a
human can commit it. The change sits complete and validated in that
repository's working tree on branch `slivingdoc` (uncommitted), with the
adaptations and evidence recorded in
[`simple-go-pipeline-release-proposal.md`](simple-go-pipeline-release-proposal.md).

**Why later phases proceed without this phase.** No phase depends on Phase 8:
Phase 10 requires Phases 1-7 and Phase 11 requires Phases 1-10. The blocked
status exists only to keep the single human step visible. When a human commits
the change and records the reviewed commit SHA (steps under "Recording the
result" at the end of this file), the phase flips to Complete.

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
section 21 (L1193)

## Goal

Publish verified native binaries and an npm launcher that runs slivingdoc
without Git, libgit2, or a compiler installed.

## Specification

Extend the reusable workflow in the separately owned
`baalimago/simple-go-pipeline` repository. Do not place credentials or
repository-specific secrets in the reusable workflow.

Complete the reusable-workflow change through a reviewed commit in that
repository. Record its immutable commit SHA in this phase. The slivingdoc caller
workflow references that commit SHA, not a branch or moving tag. This phase is
not complete while the external change is only an unmerged proposal.

Use native or proven target toolchains for:

- Linux amd64 and arm64
- macOS amd64 and arm64
- Windows amd64

Each target job compiles the pinned libgit2 configuration, runs native boundary
tests, builds slivingdoc with CGo, inspects runtime dependencies, starts the
binary, and uploads a workflow artifact.

A single assembly job downloads all required artifacts, creates SHA-256
checksums, creates one GitHub release, and uploads assets. Matrix jobs must not
race to create releases or use the unrelated latest release.

Create `npm/slivingdoc` as a small Node package. It selects the exact artifact
for package version, operating system, and architecture. It downloads into an
npm-managed cache, validates SHA-256, forwards arguments and standard streams,
and returns the child exit status.

Use the tag, asset, OS, architecture, and `SHA256SUMS` grammar in architecture
section 21 (L1193). Publish raw native binaries. Store downloads under the npm cache in
a version, OS, architecture, and asset-specific path. Use a unique temporary
file and atomic rename. Concurrent verified downloads can race safely.

The launcher never logs to stdout while the MCP server uses stdio. Unsupported
platforms fail before download with an actionable error.

Publish npm only after the GitHub release contains every required artifact and
checksum. Version values derive from the same release tag.

## Integration contract

| Trigger             | Collaborator                     | Observable result                   | Required side effect                   | Prohibited side effect               |
| ------------------- | -------------------------------- | ----------------------------------- | -------------------------------------- | ------------------------------------ |
| Native target build | Target runner and pinned libgit2 | Executable starts                   | Artifact includes libgit2              | No runtime Git or libgit2 dependency |
| Release tag         | Reusable workflow                | One complete GitHub release         | All target assets and checksums upload | No per-matrix release race           |
| First launcher run  | Local fixture HTTP server        | Correct artifact downloads and runs | Checksum verified before execute       | No execution of corrupt bytes        |
| Cached launcher run | npm cache                        | Existing verified artifact runs     | No unnecessary download                | No version-crossed cache reuse       |
| Stdio execution     | Child process                    | Streams and exit status match child | stdin remains connected                | No wrapper text on stdout            |

## Acceptance criteria

- [ ] The reusable workflow accepts native-build configuration without breaking pure-Go callers. *(in `simple-go-pipeline` working tree, branch `slivingdoc`; merge pending)*
- [ ] Required target builds use pinned action and tool versions. *(working tree; merge pending)*
- [ ] Libgit2 source or artifact input has a verified checksum. *(existing `build-libgit2.sh` + portability fixes)*
- [ ] Every target runs a real libgit2 pack or blob smoke test. *(native boundary tests run per target; CI native job proves the Linux case)*
- [x] Dependency inspection rejects dynamic libgit2 linkage. *(Linux, macOS, and Windows check scripts with self-tests)*
- [x] No target binary invokes or requires a Git executable. *(phase-2 seam scan; dependency baselines)*
- [ ] One assembly job creates one release after all builds pass. *(working tree; merge pending)*
- [ ] Release assets have stable names consumed by the npm launcher. *(grammar locked by tests on both sides)*
- [x] Checksums cover exact published bytes. *(`make-sha256sums.sh` grammar self-test)*
- [x] `SHA256SUMS` has sorted LF lines with lowercase digest, two spaces, and asset name. *(grammar self-test + strict launcher parser)*
- [x] Launcher tests cover each supported and unsupported platform mapping.
- [x] Download, cache, checksum, argument, stream, signal, and exit-code behavior are tested.
- [x] A corrupt or incomplete download is never executed.
- [ ] `npx -y slivingdoc --version` works from a clean environment on required targets. *(proven locally through global-install end-to-end; requires the published package for the literal acceptance)*
- [x] npm publication cannot precede complete GitHub release publication. *(`prepublishOnly` gate + tests)*
- [x] Required libgit2 and binding license notices ship with artifacts. *(npm `NOTICE`; release `NOTICE` asset)*
- [x] Full dependency inspection enforces the architecture target allowlists.

## Error coverage

| Failure                                 | Expected outcome                            | Required test                                       |
| --------------------------------------- | ------------------------------------------- | --------------------------------------------------- |
| Native libgit2 compilation fails        | Target job fails before assembly            | Workflow fixture or deliberate test branch evidence |
| Artifact has dynamic libgit2 dependency | Target job fails                            | Dependency-inspection negative fixture (`--check` self-tests) |
| One target fails                        | No release and no npm publication           | Workflow dependency assertion (`needs` chain in the proposal) |
| Asset is missing                        | Assembly or launcher fails clearly          | Manifest fixture test (release-gate and launcher tests) |
| Checksum mismatches                     | Delete bad cache entry and refuse execution | Node integration test |
| Download is interrupted                 | Temporary file removed, no execution        | Local HTTP failure test |
| Platform is unsupported                 | Actionable stderr and nonzero exit          | Node unit test |
| Child exits nonzero or by signal        | Wrapper propagates outcome                  | Child fixture tests |
| npm version and release tag differ      | Publication fails                           | Release validation test |

## Implementation notes

### Session 2026-08-10 (imago, worker session 16) — pipeline moved into `simple-go-pipeline`

Moved and adapted the reusable-workflow change into the separately owned
repository at `/home/imago/Projects/public/simple-go-pipeline`, on branch
`slivingdoc` (kept until the pipeline is production ready). The working-tree
diff replaces `.github/workflows/release.yml` and adds a "Release
pipeline" section to the repository `README.md`. The diff is uncommitted:
git operates in read-only mode for worker sessions, so a human review and
commit is the remaining step before the SHA can be recorded.

The move adapted the proposal (`simple-go-pipeline-release-proposal.md`)
in five places, each fixing a defect that would have broken a pure-Go caller
or the Windows target: (1) the two `if`-guarded jobs became one `build` job
whose `targets` input defaults to the legacy pure-Go matrix, so the matrix
expression never sees `fromJSON('')` and the assembly job never depends on a
skipped job; (2) every run step gained `shell: bash`, which the Windows
runner needs because its default shell is PowerShell; (3) `notice-file`
defaults to empty (opt-in), so pure-Go callers without a `NOTICE` file are
not broken; (4) the default `--version` smoke applies only to native
callers, keeping pure-Go callers smoke-free as before; (5) the default
checksum path excludes the checksum file itself so the checksums cover the
exact uploaded bytes. Pinned action SHAs were verified against the GitHub
tag refs; `actionlint`, YAML parsing, `fromJSON` input parsing, `bash -n` on
every run block, and a local release-assembly simulation (both the caller
and the default checksum paths against the strict grammar) all pass. The
updated proposal document records the adaptations, the local validation
evidence, the post-merge fixture validation plan, and the remaining human
steps.

### Session 2026-08-10 (imago, worker session 11)

All in-repository deliverables of the phase are implemented, tested, and
documented. The one external deliverable — the reviewed commit in
`baalimago/simple-go-pipeline` — is prepared as a complete reviewable
proposal and recorded here, but it cannot be merged by a worker session
(git operates in read-only mode). The phase therefore stays **In Progress**
and the caller workflow keeps the placeholder SHA, which GitHub refuses at
dispatch, so a broken release can never run. `scripts/check-release-ref.sh`
turns the placeholder into a clear diagnostic.

**npm launcher.** `npm/slivingdoc` is a zero-dependency Node package
(built-in modules only; `npm test` needs no installation). `bin/slivingdoc.mjs`
maps `process.platform`/`process.arch` to the architecture section 21 grammar,
downloads the `SHA256SUMS` and the binary from the release tag `v<version>`,
streams the binary into a unique temporary file while hashing, verifies the
digest, chmods it, and atomically renames it into the cache under
`_slivingdoc/<version>/<os>/<arch>/<asset>` with a `<asset>.sha256` sidecar.
A cache hit is trusted only when the sidecar still matches the bytes; a
corrupt entry is deleted and re-fetched, never executed. Concurrent verified
installs race safely (unique temps + atomic rename; the sidecar is
idempotent). The launcher forwards arguments verbatim, inherits stdio (stdin
stays connected), forwards SIGINT/SIGTERM to the child, and reproduces the
child's exit code or terminating signal. It never writes to stdout.

`lib/sums.mjs` parses the strict `SHA256SUMS` grammar and rejects malformed
lines, duplicate entries, missing trailing LF, and empty files.
`lib/download.mjs` follows up to 10 redirects (GitHub release downloads
redirect to the object store), streams without buffering, and enforces an
idle timeout. `scripts/check-release.mjs` is the publication gate: it
verifies every required asset (all five targets, `SHA256SUMS`, `NOTICE`) by
HEAD against the release tag and is wired into `prepublishOnly`, so npm
publication cannot precede the complete GitHub release.

Launcher tests (`npm test --prefix npm/slivingdoc`, 33 assertions across
five files) cover the platform mapping and the unsupported matrix, the
strict checksum grammar, cold install and cache reuse without downloads,
checksum mismatch and interrupted download (partial file removed, nothing
executed), version-crossed cache isolation, corrupt-cache replacement,
concurrent installs, stdout purity and argument forwarding, stdin
forwarding, exit-code and signal propagation, a trailing-slash release
base, missing-asset and missing-checksum failures, and the release
publication gate including the version/tag divergence case. A global-install
end-to-end proof ran the packed tarball against a fixture release: first
run downloaded, verified, cached, and executed; a second run with the
fixture shut down executed from the cache.

**Version injection.** `internal/app.Version` changed from a constant to a
variable so release builds can inject the tag-derived version through
`-ldflags -X`; `make build` now passes `-X
github.com/baalimago/slivingdoc/internal/app.Version=$(VERSION)` and the
smoke gate greps the injected value, which proves the injection works.

**Dependency inspection matrix.** `scripts/check-deps-macos.sh` allows only
`/usr/lib` and `/System/Library` dependencies (otool); `scripts/check-deps-windows.sh`
allows the documented Windows system DLL allowlist (dumpbin located through
vswhere on GitHub Windows runners). Both share the `--check` mode with the
Linux script, and the three self-tests run in `make native-smoke`.

**Release grammar.** `scripts/make-sha256sums.sh` emits the strict
`SHA256SUMS` grammar (lowercase, two spaces, sorted by asset name,
LF-terminated) over the exact bytes the release will upload; its self-test
proves the grammar, the sorting, and the trailing LF. `scripts/check-release-ref.sh`
requires the caller workflow to reference the reusable pipeline by a 40-hex
commit SHA and rejects the placeholder, tags, branches, and short SHAs; its
self-test covers all five cases.

**Build script portability.** `scripts/build-libgit2.sh` now falls back from
`sha256sum` to macOS `shasum`, replaces `nproc` with
`getconf _NPROCESSORS_ONLN`, and passes `--config Release` for multi-config
generators, so the same script builds the pinned libgit2 on the Linux, macOS,
and Windows target runners. The Linux path was re-proven by a full rebuild
from the cached tarball.

**Workflows.** `.github/workflows/ci.yml` gained an `npm` job (setup-node
24.19.0 pinned by SHA). `.github/workflows/release.yml` is the caller
workflow: it references the reusable pipeline at the placeholder SHA
(documented above), passes the five-target matrix, the per-target libgit2
setup, the version-injecting build, the platform dependency check, the
start smoke, the checksum command, and the notice file. The complete
reusable-workflow change — native inputs, per-target runners, dependency
inspection and smoke hooks, one assembly job, checksums, and the `NOTICE`
asset — is written up for review in
[`simple-go-pipeline-release-proposal.md`](simple-go-pipeline-release-proposal.md)
with a validation plan. The proposal's YAML was parsed and the caller
workflow YAML was parsed locally.

**Status of the external deliverable.** A human with write access to
`baalimago/simple-go-pipeline` must review the working-tree diff on branch
`slivingdoc` (the move and adaptations are documented in
`simple-go-pipeline-release-proposal.md`), commit it, record its commit SHA
here, replace the placeholder in `.github/workflows/release.yml`, and
re-run `scripts/check-release-ref.sh`. Until then the phase stays
In Progress and the release workflow cannot dispatch (unresolvable ref).

### Verification (all passed)

Before changes: `make validate` passed (baseline). The npm suite did not
exist.

After changes:

```text
make libgit2          rebuild of the pinned static libgit2 with the
                      portability fixes, from the cached tarball: PASS
make smoke            dependency self-tests (linux/macos/windows),
                      checksum grammar self-test, release-ref self-test,
                      injected --version, Linux dependency inspection: PASS
npm test --prefix npm/slivingdoc   33 tests, 0 failures: PASS
npm pack + global install + fixture release end-to-end
                      download-verify-execute and cached re-execution: PASS
go test ./... -race -timeout=30s -count=3 -p 1    full gate: PASS
go vet ./... ; staticcheck ; gofumpt ; go fix -diff: PASS
```

The race gate output, the staticcheck/gofumpt runs, and the exact npm test
summary are recorded in the worklog README session journal.

Session 16 (pipeline move) verification, all run locally against the
working-tree diff in `simple-go-pipeline`:

```text
actionlint .github/workflows/release.yml            clean (exit 0)
YAML parse; default targets JSON (5 {os,arch,runner})  PASS
caller inputs all present in the pipeline            PASS
fromJSON input parsing (default + block-scalar)      PASS
bash -n on every run block                          PASS
pinned action SHAs vs GitHub tag refs               PASS (4/4)
release-assembly simulation, caller + default
  checksum paths vs the strict SHA256SUMS grammar   PASS
scripts/test-check-release-ref.sh                   PASS
check-release-ref.sh on the caller workflow         placeholder diagnostic
                                                     (expected until merge)
actionlint on the caller release.yml                clean (exit 0)
```

## Recording the result

When a human with write access to `baalimago/simple-go-pipeline` reviews and
commits the working-tree diff on branch `slivingdoc` (git is read-only for
worker sessions), record here, in order:

1. the immutable commit SHA of the reusable-workflow change (`git rev-parse
   HEAD` in that repository after the commit),
2. the validation evidence from the post-merge fixture plan in
   [`simple-go-pipeline-release-proposal.md`](simple-go-pipeline-release-proposal.md)
   (pure-Go regression, native-setup failure, dependency negative fixture,
   slivingdoc five-target release with grammar-valid `SHA256SUMS`),
3. replace the placeholder reference in
   [`.github/workflows/release.yml`](../../.github/workflows/release.yml) with
   that SHA and re-run `scripts/check-release-ref.sh`, which must print
   `check-release-ref: ok (<sha>)`.

Then flip this phase's status from Blocked to Complete on the worklog status
board.

### Correction 2026-08-11 (worker session 17)

Phase 11 found that `.github/` did not exist in the repository: neither the
`ci.yml` this phase records adding an `npm` job to, nor the `release.yml`
caller workflow it records writing, had ever been tracked by git. Both were
recreated to the specifications recorded above and in
[`simple-go-pipeline-release-proposal.md`](simple-go-pipeline-release-proposal.md):
the five architecture-21 targets, the per-target libgit2 setup, the
version-injecting build, per-target dependency inspection and start smoke,
the strict `SHA256SUMS` producer, and the `NOTICE` asset. `actionlint v1.7.7`
reports both workflows clean and the `targets` input parses as five targets.

This does not change the phase's blocked status. The caller still references
the reusable pipeline at the documented placeholder SHA, so
`scripts/check-release-ref.sh` reports the outstanding external review and
GitHub refuses the workflow at dispatch, which is the intended interlock.

## Review findings

No reviews recorded.
