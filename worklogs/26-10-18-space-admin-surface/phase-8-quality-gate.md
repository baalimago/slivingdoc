# Phase 8: quality gate

**Status:** Complete (2026-10-20, session 8)

README: [README.md](./README.md)

## Goal

Prove the public and cloud repositories green, every architecture doc this
worklog touches true, and the README readiness checklist re-verified after the
seven implementation phases.

## Specification

### Public repository gate

Run the public repository gate:

```bash
make qa
```

`make qa` runs lint (`gofumpt`, `go vet`, staticcheck, `go fix -diff`), the race
test with `-count=3` and the 70 % coverage floor, and the npm tests. Do not
substitute a narrower command, a short mode, a build tag, or a second gate.

After `make qa` passes, run `go run github.com/mibk/dupl@v1.0.0 -t 80 .` as a
separate signal. Triage its findings; dupl is not part of the gate.

### Cloud repository gate

Export `CLOUD_REPO` to the checkout
(`/home/imago/Projects/private/slivingdoc-cloud`) and run:

```bash
make -C "$CLOUD_REPO" qa
```

The cloud `Makefile`'s `qa` target depends on `cloud` and `dupl`, so this one
command runs the storage gateway's typecheck and tests as well as the site's
lint, build, fixtures, tests, links and dupl. Run `make -C "$CLOUD_REPO" cloud`
alone only to reproduce a gateway failure; running it before `qa` runs that gate
twice.

The cloud end-to-end suite runs a stack and is not part of `make qa`. Phase 2
changed a CLI login flow, so run its spec and record the local result here.

### Architecture docs

Every doc is updated by the phase that changes the behavior it describes. This
phase verifies that each edit is present and that the doc matches the code:

| Doc | Owner | What must be true |
| --- | --- | --- |
| `architecture/config.md` | Phases 1 and 5 | settings version 2, the target fields, the store-keyed defaults list, reading version 1, the empty hosted endpoint; and the empty S3 region default, the `--region`, `AWS_REGION`, `AWS_DEFAULT_REGION`, profile-region, `us-east-1` order, and the `--profile` signal |
| `architecture/hosted-mode.md` | Phase 1 | the hosted record with an empty endpoint, the login endpoint |
| `cloud:architecture/cli-login.md` | Phase 2 | `POST /cli/v1/spaces`, the `cliSpaces` method branch, the refusals |
| `cloud:cloud/API.md` | Phase 2 | the request and response shape and every refusal code |
| `architecture/login.md` | Phases 3, 4, 5 and 6 | the wire contract and `CreateSpace`; the hosted plain listing: the storage endpoint in the group header, directory annotations, access word, owner, the literal `! ` hint prefix, and `*`/`!` marker rules; the no-login group and orphan, no-spaces, and no-default hint lines; the default line; the `--storage hosted` no-login refusal; the hosted refusal lines for read-only access, `name_taken`, `space_limit`, `invalid_name`, `access_denied`, `busy`, and `pool_empty`; the rule for a target named `new`; the S3 group header, rows, `No buckets; …`, the four create refusals, the S3 default line, the degraded hint and the auto rule; and `--profile` in the signal diagram and the `resolveStorage` signal list |
| `architecture/storage.md` | Phase 3 | `ValidateNewSpaceName` only; the notebook storage contract stays the sibling's |
| `architecture/cli.md` | Phases 4, 6 and 7 | the `cmd/login/login.go` file-map claim that `space` takes at most one positional argument; the `Space.Run` list/set/create flow for both backends; the picker path; verify all of it matches `space`, `space <name>`, and `space new <name>`, with flags before positional words, and `--storage`, `--profile`, and the S3 flags |
| `architecture/s3store.md` | Phases 5 and 6 | `Profile`, `Resolve`, `Resolved`, `Admin`, create configuration, error mapping |
| `architecture/running.md` | Phases 4 and 5 | the `space` transcript under “The default space”, which must show the group header with the storage endpoint, the directory annotations, the `write`/`read` rows and `The default space is now <name> (<access>, <owner>).`, while the `login` transcript above it stays byte-identical; and `--profile` on the shared flags and its environment default, the `--region` empty default and the flag, `AWS_REGION`, `AWS_DEFAULT_REGION`, profile, `us-east-1` precedence |
| `architecture/security.md` | Phases 5 and 6 | the key id is display only, no credential in the settings file or the log |
| `architecture/errors.md` | Phase 6 | no new category; any new reason or action is named |
| `architecture/tui.md` | Phase 7 | the `space` table, the picker columns, the create row, the prompt |
| `architecture/testing.md` | Phase 7 | the picker test areas and the extended terminal helper |
| `architecture/decisions.md` | Phase 8 | any decision this worklog made, with its date |
| `architecture/README.md` | Phase 8 | the index and reading order still name every doc |

`architecture/mcp-server.md`, `architecture/product-contract.md`,
`architecture/workspace.md`, and `architecture/overview.md` belong to the
sibling `26-10-18-directory-bindings`; this worklog changes nothing they
describe.

### README readiness checklist

Run every line of the README checklist and record the outcome:

1. the shared diagram explains the `space` surface; phase-local sequences stay
   in their phase;
2. every test has one owning phase and a file list (in its phase under Files);
3. every parameter, flag, injectable field, invariant, and limit has one owner
   and a test;
4. no phase needs a person: the cloud checkout is present and writable;
5. no phase depends on another phase's local detail;
6. no phase creates human-owned configuration;
7. the cited conventions match the code;
8. every doc sentence a phase falsifies is listed in its `Documentation` table and proved gone by a check.

This phase adds no test file and owns no test; every test belongs to the phase
that wrote it, and this phase runs the gates those phases prove.

### Invariants and limits

| Rule | Mechanism | Proof |
| --- | --- | --- |
| No test skips a host capability it can run | the suite has no new skip | `make qa` |
| The coverage floor is met | `make test` | `make qa` |
| Dupl findings are triaged | Run `go run github.com/mibk/dupl@v1.0.0 -t 80 .` after `make qa` | dupl triage recorded in this phase |
| The cloud repository is green | `make qa` (it runs `cloud`) | the recorded command |
| Every architecture doc this worklog touches is true | review table above | review notes in this phase |
| No credential, key, token, private path, or Git vocabulary reaches caller text | existing suite | `make qa` |

### Environment

The setup is present, so this phase runs unattended: the cloud checkout is
writable at `/home/imago/Projects/private/slivingdoc-cloud`, its Node
dependencies and the Playwright browsers are installed, the Docker daemon
serves the pinned SeaweedFS container, and the pinned libgit2 is built.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `make qa` | the public toolchain | exit 0, every gate printed | none | a skipped package, a short mode |
| `make -C "$CLOUD_REPO" qa` | the cloud checkout | exit 0 | none | a second cloud gate (it runs `cloud`), a cloud deployment |
| `make -C "$CLOUD_REPO" cloud`, only to reproduce a gateway failure | the cloud checkout | exit 0 | none | a second full `qa` |
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
| The cloud gate is green and includes the storage gateway | `make -C "$CLOUD_REPO" qa` |
| The cloud e2e CLI login spec ran locally and its result is recorded | the Phase 2 note in this phase |
| Every architecture doc this worklog touches is true | review table in this phase |
| The readiness checklist is recorded | this phase's implementation notes |
| No test was skipped or shortened | `git diff` review and `make qa` output |
| No live cloud or AWS endpoint was reached | test review and `make qa` output |

## Error coverage

| Failure | Expected outcome |
| --- | --- |
| formatter reports a file | run `make lint`, then re-run `make qa` |
| `go vet` or staticcheck reports a finding | fix the code, run `make lint`, then re-run `make qa` |
| `go fix -diff` prints a change | apply it, run `make lint`, then re-run `make qa` |
| a Go test fails | fix the code or the scenario, run `make test`, then re-run `make qa`; never skip it |
| an npm test fails | fix the code or the scenario, run `npm test --prefix npm/slivingdoc`, then re-run `make qa`; never skip it |
| coverage is below 70 % | add the missing boundary test and run `make test` |
| a dupl group is actionable | extract the shared code and run `go run github.com/mibk/dupl@v1.0.0 -t 80 .` |
| a dupl group is the documented interface/fake pattern | record why it stays with `go run github.com/mibk/dupl@v1.0.0 -t 80 .` |
| the cloud gate fails | fix the cloud change, then run `make -C "$CLOUD_REPO" qa`; do not deploy |
| an architecture doc contradicts the code | fix the doc in the same change, check its named `rg` criterion, then run `make qa` |
| a readiness line fails | fix the defect class, re-run the readiness checklist, then run `make qa` |

## Implementation notes

Worker: phase-8 agent, 2026-10-20, session 8.

### Public repository gate

| Command | Result |
| --- | --- |
| `make qa` before the phase-8 corrections | exit 0; gofumpt, `go vet`, staticcheck and `go fix -diff` clean; the race suite at `-count=3` passes; coverage 89.3 % against the 70 % floor; npm 35 tests pass |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` before the corrections | 3 clone groups, triaged below |
| `go test -race -count=1 ./internal/cli ./internal/app ./cmd/login` after the help-text correction | pass |
| `make qa` after the phase-8 corrections | exit 0; the same lint and race gates pass; coverage 89.3 %; npm 35 tests pass, 0 skipped |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` after the corrections | the same 3 clone groups |

### Dupl triage

The three groups are all acceptable under the repository's duplication
policy, and none is new in this phase:

| Group | Verdict |
| --- | --- |
| `internal/app/login.go`, `internal/app/space.go` | The `New*Flags` and `Bind` bodies are declarative flag-registry tables over disjoint flag sets and different typed setters. A shared registrar would add a flag-spec type and obscure the fields; `app.Flags.Bind` uses the same idiom. |
| `internal/integrationtest/scenario_conflict_test.go`, `internal/notebook/pull_test.go` | Table-driven scenario and unit rows with the same shape in two independent test layers: test-setup boilerplate, deliberately not shared across the black-box boundary. |
| `internal/git/merge_test.go`, two table entries | Two rows of one table-driven test loop: the idiom, not a clone. |

### Cloud repository gate

```bash
make -C "$CLOUD_REPO" qa
```

exits 0. It runs `cloud` first: the gateway typechecks and 26 test files
pass 670 tests. The site then passes Prettier, `astro check`, ESLint,
Stylelint, Knip, the build, the fixture build, 33 test files with 593
tests, `scripts/check-links.mjs`, and jscpd with the 2 known clones in
`tests/unit/shell.test.ts`.

```bash
cd "$CLOUD_REPO/e2e" && E2E_DOMAIN=slivingdoc.localhost:8080 npx playwright test tests/cli-login.spec.ts tests/space-create.spec.ts --project flows
```

starts the local stack and passes 4 tests in 40.2 s: the three
`cli-login.spec.ts` flows (approved login and logout revocation, denied
login, signed-out approval page) and the `space-create.spec.ts` flow that
creates a space through `POST /cli/v1/spaces` and lists it. No deployed
site or stack was used.

### Architecture docs review

| Doc | Verdict |
| --- | --- |
| `architecture/config.md` | True: settings version 2 and its target fields, the store-keyed defaults, the version-1 read path, the empty hosted endpoint, and the region and `--profile` order. One plan clause was corrected. |
| `architecture/hosted-mode.md` | True: the version-2 hosted record and the login's endpoint. The same plan clause was corrected. |
| `cloud:architecture/cli-login.md` | True: `cliSpaces` owns GET and POST, and `cliCreate` runs key, limiter `create:<key id>`, content type, capped body, duplicate-member check, shape, `Accounts.createSpace`. |
| `cloud:cloud/API.md` | True: the POST request and 201 response shape and the 400 `invalid_name`, 401, 403 `space_limit`, 409 `name_taken`, 429 `busy` and 503 `pool_empty` refusals. |
| `architecture/login.md` | True: the wire contract and `CreateSpace`, the hosted listing (storage endpoint, directories, access, owner, literal `! ` prefix, `*`/`!` rules), the no-login group and orphan, the no-spaces and no-default lines, the default line, the `--storage hosted` no-login refusal, the hosted and S3 create refusals, the `new` rule, the S3 header and rows, and `--profile` in the signal list. Two duplicate file rows were merged. |
| `architecture/storage.md` | True: `ValidateNewSpaceName` only, with the notebook protocol untouched. |
| `architecture/cli.md` | True: the positional forms, flag order, `Space.Run` list/create/set flow for both backends, and the picker path. |
| `architecture/s3store.md` | True: `Profile`, `Resolve`, `Resolved`, `Admin`, the create configuration rule, and the error mapping. |
| `architecture/running.md` | True after correction: the `space` transcript shows the storage endpoint, directory annotations, `write`/`read` rows and the default line, and the `login` transcript is unchanged; `--profile` and the region order are current. The stale single-login picker sentence was replaced. |
| `architecture/security.md` | True: the key id is display only and the settings file holds no credential. |
| `architecture/errors.md` | True: no new category; `space` refusals are plain CLI text outside the MCP taxonomy. |
| `architecture/tui.md` | True: one headerless `space` table, its columns, the create rows, the name prompt, and the signal rule. |
| `architecture/testing.md` | True: the picker test areas and `runCLIOnTypedTTY`, including its named PTY capability skip. |
| `architecture/decisions.md` | True: decision 52 (S3 identity and admin, phase 5, dated) and decision 53 (picker, phase 7, dated); decisions 46 and 49 carry the hosted create and settings-version-2 changes made by phases 1 and 4, and this phase added their dated attribution. This phase made no new architecture decision. |
| `architecture/README.md` | True: every doc in `architecture/` is linked from the index; this worklog adds no doc. |

The docs this worklog does not own (`mcp-server.md`, `product-contract.md`,
`workspace.md`, `overview.md`) still describe the one-directory MCP
surface and were not changed beyond the package-map lines the settings and
`sitelogin` changes require.

### Readiness checklist

1. The shared diagrams explain the `space` surface. The flow now says the
   picker needs all three streams and lists the real rows and create row;
   phase-local sequences stay in their phase files.
2. Every test has an owning phase and a file list. Each phase has a `Files`
   table, and every proof test named in the README and the phase files was
   found with `rg`.
3. Every parameter, flag, injectable field, invariant, and limit has one
   owner and a test. Every test named in the README parameter and invariant
   tables exists.
4. No phase needs a person. The cloud checkout is present and writable, its
   Node dependencies and Playwright browsers are installed, Docker serves
   the pinned SeaweedFS container, and libgit2 is built. No phase declares
   `Human required`.
5. No phase depends on another phase's local detail. The prerequisite order
   is stated, and the stale-sentence checks below are clean.
6. No phase creates human-owned configuration. No AWS profile, cloud
   deployment, bucket pool entry, or site setting was created.
7. The cited conventions match the code: the strict domain-plus-wire structs
   of `settings`, the flag-then-environment-then-default order of `app`, and
   the SDK containment. `rg -l 'aws-sdk-go-v2' --glob '*.go' --glob '!*_test.go' --glob '!internal/s3store/**' --glob '!internal/tests3/**' .`
   prints nothing; the integration scenario's raw client is test-only.
8. Every falsified doc sentence is listed and proved gone. The phase 1, 4,
   5 and 6 checks were rerun: `rg 'a space and a prefix only'`,
   `rg -e 'always explicit' -e 'setting are ignored' -e 'region is required'`
   and `rg 'owned by' architecture/running.md` (one hit, the login
   transcript) all match their expected result.

### No skip, no live endpoint

`git diff` was reviewed: no test skip, short mode, build tag, or second gate
was added. The picker scenarios reuse `openPTY`'s pseudo-terminal capability
skip, which names the capability and predates this worklog. The Go suite used
`sitetest`, `gatewaytest` and the pinned SeaweedFS container; the cloud gate
and the e2e suite used the local stack
at `slivingdoc.localhost`. No deployed site, AWS endpoint, R2 endpoint, or
cloud environment was reached.

## Review findings

Phase 8 found six documentation and operator-text defects. All are fixed in
this change; none changes behavior:

1. `cmd/login/login.go` `spaceHelp` still described phase 4's picker: spaces
   of one login, no S3 rows, no create rows. It now describes the phase-7
   table and its create row per backend.
2. `architecture/running.md` repeated the same stale single-login picker
   sentence. It now states the multi-backend picker and the create row.
3. `architecture/login.md` carried two `Key files` rows each for
   `internal/app/present.go` and `internal/app/space.go`, added by phases 4
   and 6. Each file has one row with the current symbol set.
4. `architecture/config.md` and `architecture/hosted-mode.md` recorded the
   sibling worklog as the future writer of a hosted endpoint, which is a plan
   in behavior prose. Both now state the current rule: a hosted target keeps
   an empty endpoint because the selected login supplies the site.
5. `architecture/running.md` said the empty endpoint holds "in this build", a
   time-bound qualifier. It states the rule without it.
6. The worklog README's `space` flow and picker sketch said "stdout is a
   terminal" and showed group headers and one create row. `pickable` requires
   all three streams, and the picker is one headerless table with a create row
   per backend; both now match the code.

### Review 2 (2026-10-20, session 11)

Round 2 re-ran the gate and the result stands: `make qa` exits zero with
gofumpt, `go vet`, staticcheck and `go fix -diff` silent, the race suite at
`-count=3` green, coverage 89.4 % against the 70 % floor, and 35 npm tests
passing with none skipped. The phase itself has no open defect. One process
note is recorded here because this phase owns the review's placement:

- **R2-09 (note).** Round 1 put its findings in the standalone
  [review-2026-10-20.md](./review-2026-10-20.md) and most of them in the Phase 6
  file rather than the phases they concerned. Round 2 records every finding in
  the affected phase files, the Feedback index and this journal, and leaves the
  round-1 file as that round's history. Closed by this review.

The gate conclusion of session 8 is unchanged: green gates are not a
correctness verdict, and round 2's two majors pass them.

### V6-06 (note, evidence ownership)

The Error coverage rows name gate failures and remedies but do not name a test
or repeatable command for each failure. Add the relevant gate or check to each
row.

### Resolution (2026-10-20, session 20)

V6-06 is resolved. Each Error coverage row now names its gate or repeatable
command, including the public lint, Go test and npm commands, dupl check, cloud
gate, and doc/readiness checks. Phase 8 remains Complete.
