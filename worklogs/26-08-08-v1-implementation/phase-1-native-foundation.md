# Phase 1 — Repository and native foundation

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 5 (L131), 8.1 (L307), 19 (L1122), 21 (L1193)

## Goal

Create the repository shell and prove a self-contained CGo/libgit2 boundary on
the first supported development platform.

## Specification

Create the Go module, `main.go`, internal package roots, Make targets, and local
validation configuration. Use the versions and libgit2 checksum in the README
pinned implementation baseline.

Create `internal/git2` as the only package that imports C or libgit2 headers.
The first boundary is deliberately small. It must initialize libgit2, report
its runtime version and features, create a temporary repository, write one
blob, and read that blob back.

The package must convert native errors into Go errors and release every native
resource deterministically. No C pointer or libgit2 type can appear in another
package API.

Build libgit2 with unneeded network transports disabled. The application uses
the AWS Go SDK, so this phase must not add libssh2 or libgit2 HTTP transport as
runtime dependencies.

Download `https://github.com/libgit2/libgit2/archive/refs/tags/v1.9.6.tar.gz`
and verify its pinned SHA-256 before extraction. Build a static libgit2 archive
with SSH, HTTPS, CLI, examples, and
libgit2 tests disabled. Keep thread support, SHA-1 repositories, zlib pack
support, merge, index, and packbuilder support. Link every non-system libgit2
dependency into the application. Release binaries can use only operating-system
runtime libraries listed by the target dependency-inspection script.

Add a checked-in build procedure that produces an executable containing
libgit2. Add dependency-inspection commands for Linux first. Phase 8 expands
this proof to the complete platform matrix.

Create a no-native test seam for higher packages. This seam can be a Go
interface and test fake. It must not become a second Git implementation.

Keep native implementation files behind the standard `cgo` build constraint.
Provide a non-CGo constructor stub that returns a clear unavailable error, so
`CGO_ENABLED=0 go test ./...` compiles and runs non-native tests. Do not emulate
Git behavior in that stub.

## Integration contract

| Trigger                    | Collaborator         | Observable result                    | Required side effect      | Prohibited side effect                      |
| -------------------------- | -------------------- | ------------------------------------ | ------------------------- | ------------------------------------------- |
| Open native engine         | Pinned libgit2       | Version and feature check succeeds   | Libgit2 initializes once  | No process exit from package initialization |
| Blob round trip            | Temporary repository | Read bytes equal written bytes       | Native objects are closed | No leaked repository handle                 |
| Build release-style binary | Native toolchain     | Binary starts and prints its version | libgit2 code is included  | No runtime `libgit2.so` dependency          |
| Build higher package tests | Fake engine          | Tests compile without native calls   | Fake records operations   | No C type outside `internal/git2`           |

## Acceptance criteria

- [x] `go.mod`, repository package roots, and standard validation commands exist.
- [x] One libgit2 release is pinned by version and source checksum.
- [x] `internal/git2` is the only CGo package.
- [x] A real blob round-trip test passes in a temporary repository.
- [x] A repeated open and close test passes under the race detector.
- [x] The native error wrapper preserves operation and libgit2 error detail.
- [x] A release-style Linux binary contains libgit2 and starts without Git.
- [x] Dependency inspection proves that Linux does not require `libgit2.so`.
- [x] CI has separate pure-Go validation and native-boundary jobs.
- [x] `CGO_ENABLED=0 go test ./...` passes without a second Git implementation.
- [x] Architecture and license notices name the pinned libgit2 release.

## Error coverage

All rows are covered by tests in `internal/git2/engine_test.go` and by the
dependency-inspection self-test.

| Failure                                         | Expected outcome                             | Required test                                                   |
| ----------------------------------------------- | -------------------------------------------- | --------------------------------------------------------------- |
| Libgit2 initialization fails                    | Typed native initialization error            | `TestOpenReportsInitFailure` (injected `initFn` seam)           |
| Repository path is invalid                      | Operation-specific Go error                  | `TestInvalidRepositoryPath` (file-component and non-repo paths) |
| Blob object cannot be read                      | Native error is copied before handle release | `TestMissingObjectRead` (error detail survives `Close`)         |
| Native allocation fails or returns null         | No dereference and no leak                   | `TestInjectedAllocationFailuresDoNotDereference`                |
| Runtime libgit2 version differs from pinned ABI | Startup refuses the engine                   | `TestOpenRefusesVersionMismatch` (injected `versionFn` seam)    |
| Binary links dynamic libgit2 unexpectedly       | Native smoke job fails                       | `scripts/test-check-deps-linux.sh` self-test cases              |

## Implementation notes

### Session 2026-08-09 (imago, worker session 1)

Created the repository shell: `main.go`, `internal/app`, `internal/git`,
`internal/git2`, Make targets, scripts, CI, and documentation. Only package
roots with real content were created; the remaining planned roots
(`internal/mcp`, `internal/notebook`, `internal/workspace`,
`internal/storage`, `internal/s3store`) belong to their phases.

`internal/git` is the Go-facing engine seam: `Engine` and `Repository`
interfaces, `OID`, `Features`, and the typed errors (`ErrUnavailable`,
`VersionMismatchError`, `NativeError`). `internal/git2` is the only package
with CGo; it implements the seam against the pinned libgit2 and exposes no C
type. `internal/app` drives the phase-1 process body (open, report version and
features, close) and consumes only the seam.

The native implementation files carry the standard `cgo` build constraint;
`CGO_ENABLED=0` builds use `internal/git2/engine_stub.go`, which reports
`git.ErrUnavailable` on every operation without emulating Git behavior.

`scripts/build-libgit2.sh` downloads the pinned tarball, verifies its SHA-256,
and builds a static archive with SSH, HTTPS, CLI, examples, fuzzers, and
libgit2 tests disabled. zlib and the regex backend are bundled
(`USE_BUNDLED_ZLIB=ON`, `REGEX_BACKEND=builtin`) so the runtime dependency
list stays within the Linux baseline. The `#cgo pkg-config: --static libgit2`
directive links `libgit2.a` through the generated pkg-config file.

`scripts/check-deps-linux.sh` enforces the Linux baseline (C runtime, loader,
pthread, dl, rt, math) and fails on anything else, including `libgit2.so`,
`libz.so`, and `libpcre2`. Its self-test is `scripts/test-check-deps-linux.sh`.

CI (`.github/workflows/ci.yml`) runs two jobs: pure-Go validation
(`make validate`, `CGO_ENABLED=0`) and the native boundary (`make libgit2`,
`make native-test`, `make native-smoke`). Actions are pinned by full commit
SHA.

`docs/build-libgit2.md` is the checked-in build procedure; `NOTICE` names the
pinned libgit2 v1.9.6 release, its source checksum, and its GPL v2 linking
exception. The architecture section 8.1 names v1.9.6 on the same line, so all
README line references remain valid.

### Verification (all passed)

Before changes: no implementation existed; the module contained only
`go.mod`. The pinned tarball download and SHA-256 verification
(`a88a42a4ea9bdab7aa8686eead3bf7d9c6dd74529caca16ab22eaa92433d31d9`) were
validated in a scratch prototype, together with the static cmake build and a
minimal cgo blob round trip.

After changes:

```text
make validate        gofumpt, go vet, staticcheck, go fix, pure-Go tests x3: PASS
make native-test     go test -race -timeout=30s -count=3 -cover ./...: PASS
make native-smoke    dep-check self-test, binary start, dependency inspection: PASS
make qa              validate + native-test + native-smoke: PASS
go run github.com/mibk/dupl@latest -t 80 .  0 clone groups: PASS
```

Release-style binary evidence:

```text
$ ./.build/slivingdoc
slivingdoc 0.1.0-dev (libgit2 1.9.6; threads, nsec, http-parser, regex, compression, sha1)
$ readelf -d .build/slivingdoc | grep NEEDED
  Shared library: [libc.so.6]
```

`TestFeaturesReflectPinnedBuild` locks the exact feature set (threads, nsec,
http-parser, regex, compression, sha1; no transport feature), so a future
libgit2 configuration drift fails the suite.

## Review findings

No reviews recorded.
