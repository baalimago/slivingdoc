# `simple-go-pipeline` native release workflow — moved and adapted

This document is the Phase 8 change to the separately owned
`baalimago/simple-go-pipeline` repository. The change has been **moved into
that repository's working tree** on branch `slivingdoc` (the branch used
until the pipeline is production ready) as an uncommitted diff:
`.github/workflows/release.yml` was replaced and the repository `README.md`
gained a "Release pipeline" section. A worker session cannot merge (git
operates in read-only mode), so the remaining steps are a human review and
commit in that repository, then the SHA recording below. The phase is not
complete while the reviewed commit does not exist; the slivingdoc caller
workflow keeps the placeholder SHA that GitHub refuses at dispatch, and
`scripts/check-release-ref.sh` turns it into a clear diagnostic.

## Why the reusable workflow must change

The previous `release.yml` had three defects for native callers:

1. It builds with `GOOS`/`GOARCH` only. A CGo caller (slivingdoc statically
   links libgit2) cannot cross-compile; each native target needs its own
   runner and a per-target toolchain step.
2. Every matrix job creates a release through the API and the upload step
   looks up `releases/latest`. Matrix jobs race to create releases and can
   attach assets to an unrelated latest release.
3. No checksum file is produced, and the release has no single assembly
   point that can fail the whole release when one target fails.

The change is additive: pure-Go callers that omit the new inputs keep the
previous matrix and default build.

## Adaptations made while moving the change (review these first)

The proposal YAML in the earlier version of this document was adapted during
the move. Each adaptation removes a defect that would have broken a pure-Go
caller or the Windows target:

1. **Single `build` job; the `targets` input defaults to the legacy pure-Go
   matrix.** The proposal used two jobs with `if: inputs.targets != ''`
   guards. That design has two failure modes: `fromJSON('')` in the skipped
   job's matrix can fail the whole workflow, and a skipped job in `needs`
   propagates the skip to the assembly job — either way a pure-Go caller
   would produce no release. With one job whose matrix expression always
   receives valid JSON (the default or the caller's), neither failure mode
   exists and `needs: [build]` is unambiguous.
2. **`shell: bash` on every run step.** GitHub-hosted Windows runners default
   to PowerShell; the `[[ ... ]]` and `$GITHUB_OUTPUT` steps would fail on
   `windows-2022`. Git Bash ships on the Windows runners, so `shell: bash`
   makes the same script run on every target.
3. **`notice-file` defaults to empty (opt-in).** A default of `NOTICE` would
   fail the next release of any pure-Go caller without a `NOTICE` file. The
   slivingdoc caller passes `notice-file: NOTICE` explicitly.
4. **The default `--version` smoke runs only for native callers.** A pure-Go
   caller whose binary does not implement `--version` would fail a default
   smoke. Pure-Go callers keep the previous behavior (no smoke); native
   callers (with `native-build-command` set) get the default smoke when they
   do not pass `smoke-command`.
5. **The default checksum path excludes the checksum file itself.**
   `sha256sum *` inside `dist` hashes the empty `SHA256SUMS` just created by
   the redirect and writes a bogus self-referential line. The default now
   filters it (`grep -v '  SHA256SUMS$'`), so the checksums cover the exact
   uploaded bytes in both the default and the caller-supplied paths.
6. **Pinned actions.** `actions/checkout` v4.2.2 and `actions/setup-go`
   v5.4.0 match the slivingdoc CI pins; `actions/upload-artifact` v4.6.2 and
   `actions/download-artifact` v4.3.0 are new pins. All four SHAs were
   verified against the tag refs through the GitHub API.

## Adapted `release.yml` (in the repository working tree)

The complete file lives at
`/home/imago/Projects/public/simple-go-pipeline/.github/workflows/release.yml`
(branch `slivingdoc`, uncommitted). Behavior notes for the review:

- The single `build` job runs `${{ fromJSON(inputs.targets) }}` as its
  matrix. The `targets` input defaults to the legacy pure-Go matrix (darwin
  and linux on amd64, arm64, and 386, excluding darwin-386, all on
  `ubuntu-latest`). Native callers pass a JSON array of
  `{os, arch, runner}` triples; each entry runs on its own runner.
- Only the single `release` job creates the release, after every `build`
  matrix instance succeeded; a failed target produces no release. The
  `releases/latest` lookup is gone, so a matrix job can never attach assets
  to an unrelated release.
- `checksum-command` receives the exact `dist/*` paths (working directory is
  the checkout root), so callers like slivingdoc can run their own grammar
  script (`make-sha256sums.sh`), and the default still emits sorted
  two-space lowercase `SHA256SUMS` lines.
- The checksum file covers the exact uploaded bytes: it is created after the
  notice copy and before `gh release create` uploads `dist/*`.
- `$GITHUB_REF_NAME` is `v<semver>`; asset names use the tag minus `v`.
  `RELEASE_VERSION` exports the tag name verbatim; callers strip the `v`
  when it belongs in the linker variable.
- Every target step runs the same command inputs with `TARGET_OS`,
  `TARGET_ARCH`, `TARGET_BINARY`, and `RELEASE_VERSION` exported, so the
  caller's dependency inspection and smoke commands are target-aware.
- All run steps use `shell: bash` (required for the Windows target).
- The `smoke-command` and `notice-file` inputs default to empty; see the
  adaptations above.

## Validation performed before the move was handed over

- `actionlint` on the new `release.yml`: clean (exit 0).
- YAML parse of the workflow; the default `targets` JSON parses as five
  valid `{os, arch, runner}` entries; every input the slivingdoc caller
  passes exists in the workflow.
- `fromJSON` inputs parse: the default and the caller's block-scalar form
  (trailing newline) both parse as JSON.
- `bash -n` on every `run` block after interpolation of the placeholder
  commands: clean.
- A local simulation of the release assembly: `make-sha256sums.sh` over the
  five target binaries plus `NOTICE` produces a grammar-valid `SHA256SUMS`
  (LF-terminated, lowercase 64-hex, two spaces, sorted, unique names); the
  default checksum path also produces grammar-valid output and excludes the
  checksum file itself.
- Pinned action SHAs verified against the GitHub tag refs:
  checkout `11bd719…` (v4.2.2), setup-go `0aaccfd…` (v5.4.0),
  upload-artifact `ea165f8…` (v4.6.2), download-artifact `d3f86a1…` (v4.3.0).

## Validation plan after the reviewed commit lands

- A fixture pure-Go caller (no new inputs) must produce the same assets as
  before with a single release (regression).
- A fixture native caller with a deliberate `native-setup-command` failure
  must fail the target job and produce no release (workflow dependency
  assertion).
- A fixture native caller with a `dependency-check-command` that lists
  `libgit2.so` must fail the target job (negative fixture).
- The slivingdoc caller must build all five targets, start each binary, and
  assemble one release whose `SHA256SUMS` matches the grammar.

## Remaining steps (human, in order)

1. Review the working-tree diff in
   `/home/imago/Projects/public/simple-go-pipeline` (branch `slivingdoc`)
   and commit it. The immutable commit SHA becomes the reviewed reference.
2. Record in [`phase-8-distribution.md`](phase-8-distribution.md):
   - the immutable commit SHA of the reusable-workflow change,
   - the validation evidence above (fixture runs on GitHub Actions).
3. Replace the placeholder reference in
   [`.github/workflows/release.yml`](../../.github/workflows/release.yml)
   with that SHA and re-run `scripts/check-release-ref.sh`.
