# Phase 1 — Release binary e2e

**Status:** Not Started

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§17 flags and environment; [`../../docs/testing.md`](../../docs/testing.md)
test layers (release)

## Goal

Add `main_e2e_test.go` so the built release binary performs a full pull and
commit round trip and a conflict report against real MinIO, with the exit
code, stdout, visible files, and redaction invariants validated.

## Specification

Add one file at the repository root: `main_e2e_test.go`, `package main`, with
the `//go:build !windows` tag matching `release_test.go`. It reuses the
existing `releaseBinary()` cache and the existing `TestMain` cleanup; it must
not define a second `TestMain`.

### Process runner

Add `startAndWaitEnv` beside `startAndWait` in `release_test.go`:

```go
func startAndWaitEnv(name string, env []string, args ...string) (stdout, stderr string, code int, err error)
```

It is the real implementation: `os.StartProcess` with the given `env`, two
pipes drained concurrently, and `proc.Wait`. `startAndWait` becomes a thin
wrapper that calls `startAndWaitEnv(name, os.Environ(), args...)`, so every
existing release test call site stays unchanged.

### Environment sanitizing

Add a root-package helper that strips every inherited AWS and slivingdoc
variable, then sets only the local MinIO and slivingdoc values:

```go
func e2eEnv(suite *testminio.Suite, prefix, workspaceRoot, privateRoot string) []string
```

The stripped names are the same surface as
`internal/integrationtest.sanitizedEnv`: `AWS_ACCESS_KEY_ID`,
`AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_PROFILE`,
`AWS_DEFAULT_REGION`, `AWS_REGION`, `AWS_ENDPOINT_URL_S3`,
`AWS_ENDPOINT_URL`, `AWS_CA_BUNDLE`, `AWS_SHARED_CREDENTIALS_FILE`,
`AWS_CONFIG_FILE`, `SLIVINGDOC_BUCKET`, `SLIVINGDOC_PREFIX`,
`SLIVINGDOC_WORKSPACE_ROOT`, `SLIVINGDOC_PRIVATE_ROOT`.

The helper then appends exactly:

```text
AWS_ACCESS_KEY_ID=testminio.User
AWS_SECRET_ACCESS_KEY=testminio.Pass
AWS_ENDPOINT_URL_S3=suite.Endpoint
AWS_REGION=testminio.Region
SLIVINGDOC_PATH_STYLE=true
SLIVINGDOC_BUCKET=testminio.Bucket
SLIVINGDOC_PREFIX=<prefix>
SLIVINGDOC_WORKSPACE_ROOT=<workspaceRoot>
SLIVINGDOC_PRIVATE_ROOT=<privateRoot>
```

### Success round trip

`TestE2EReleaseBinaryPullCommitRoundTrip`:

1. `bin, err := releaseBinary()`; fail on error.
2. `suite := testminio.Ensure(t)`; `prefix := suite.FreshPrefix("release-e2e")`.
3. `workspaceRoot := t.TempDir()`, `privateRoot := t.TempDir()`;
   `pathA := filepath.Join(workspaceRoot, "a")`,
   `pathB := filepath.Join(workspaceRoot, "b")`.
4. `env := e2eEnv(suite, prefix, workspaceRoot, privateRoot)`.
5. Run `bin pull pathA`; assert exit code 0 and stdout exactly `OK\n`.
6. Write `pathA/a.md` with `"e2e notes\n"`.
7. Run `bin commit pathA -m first`; assert exit code 0 and stdout exactly
   `OK\n`.
8. Run `bin pull pathB`; assert exit code 0 and stdout exactly `OK\n`, and
   `pathB/a.md` reads exactly `"e2e notes\n"`.

Every run asserts the collected stderr and stdout do not contain the MinIO
secret (`testminio.Pass`), the private root, `packs/`, or a 40-hex Git ID.

### Conflict report

`TestE2EReleaseBinaryConflictReport`:

1. Build and set up the same environment with `pathA` and `pathB`.
2. `pull pathA`; write `pathA/shared.md` `"base\n"`; `commit pathA -m first`.
3. `pull pathB`; assert `pathB/shared.md` reads `"base\n"`; write
   `pathB/shared.md` `"B-v2\n"`; `commit pathB -m second`.
4. Write `pathA/shared.md` `"A-v2\n"`; run `commit pathA -m third`.
5. Assert a nonzero exit code and a stdout containing `CONTENT_CONFLICT`,
   `retryable: false`, and `shared.md: lines 1-5`.
6. Assert the stdout does not contain `workspaceRoot` (paths are relative).
7. Assert `pathA/shared.md` contains `<<<<<<<`.

Apply the same no-secret assertions as the success scenario.

## Integration contract

| Trigger                           | Collaborators         | Observable result                                     | Required side effect              | Prohibited side effect               |
| --------------------------------- | --------------------- | ----------------------------------------------------- | --------------------------------- | ------------------------------------ |
| `pull pathA` on a fresh prefix    | Release binary, MinIO | Exit 0, stdout `OK\n`                                 | Empty notebook accepted           | No remote object created             |
| `commit pathA -m first`           | Release binary, MinIO | Exit 0, stdout `OK\n`                                 | Increment published, L/P accepted | No secret in stdout/stderr           |
| `pull pathB`                      | Release binary, MinIO | Exit 0, stdout `OK\n`, `a.md` materialized            | P initialized from R              | No pack or S3 key in output          |
| Divergent `commit pathA -m third` | Release binary, MinIO | Nonzero exit, `CONTENT_CONFLICT` report, markers in L | R unchanged, markers materialized | No absolute root or secret in stdout |

## Acceptance criteria

- [ ] `main_e2e_test.go` exists at the repository root with `package main` and `!windows`.
- [ ] The release binary performs a full `pull` → edit → `commit` → `pull` round trip against real MinIO.
- [ ] Every success step returns exit 0 and stdout exactly `OK\n`.
- [ ] The published bytes reach the second workspace on its pull.
- [ ] A divergent commit returns a nonzero exit with the `CONTENT_CONFLICT` report, the relative path, and `retryable: false`, and materializes markers.
- [ ] No stdout or stderr leaks the MinIO secret, the private root, a pack key, or a Git ID.
- [ ] The test passes under `-count=3` with the binary built once.
- [ ] The existing release tests still pass unchanged.

Each checked criterion cites its test in `main_e2e_test.go` (or the unchanged
`release_test.go` tests for the last criterion).

## Error coverage

| Failure                                                  | Expected outcome                                 | Required test                               |
| -------------------------------------------------------- | ------------------------------------------------ | ------------------------------------------- |
| Binary build fails                                       | Test fails with the build error                  | `main_e2e_test.go` (shared `releaseBinary`) |
| MinIO unavailable                                        | Test fails with the actionable Docker diagnostic | `testminio.Ensure`                          |
| Process start or wait fails                              | Test fails with the `os.StartProcess` error      | `main_e2e_test.go`                          |
| A success step exits nonzero                             | Assertion failure naming the step and stderr     | `main_e2e_test.go`                          |
| A conflict step exits zero                               | Assertion failure                                | `main_e2e_test.go`                          |
| Output leaks a secret, private path, pack key, or Git ID | Assertion failure naming the leaked substring    | `main_e2e_test.go`                          |

## Implementation notes

## Review findings

No reviews recorded.
