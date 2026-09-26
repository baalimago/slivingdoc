# Build and distribution

How the native executable is built with a statically linked, pinned libgit2, what the Makefile and CI workflow do, which targets and dependency baselines a release artifact must meet, and how the npm launcher selects, verifies and runs that artifact. It answers "how do I build it, why does it need CGo, what does CI run, and how does `npx slivingdoc` reach a native binary?"

Read this when: changing the libgit2 pin or build flags, the Makefile, `ci.yml`, a release target or its toolchain, the dependency checkers, the version injection, or the npm launcher. Cutting a release is in [releasing.md](./releasing.md).

## Key files

| File | Purpose |
|------|---------|
| `Makefile` | `libgit2` (stamp on `scripts/build-libgit2.sh`), `build`/`$(BIN)` (`CGO_ENABLED=1 go build -trimpath -ldflags '$(LDFLAGS)'`), `VERSION` + `VERSION_LDFLAG`, `STATIC=1`, `PKG_CONFIG_PATH`, `test`, `cover`, `npm-test`, `lint`, `fmt`, `release`, `qa`, `clean` |
| `scripts/build-libgit2.sh` | Download, SHA-256 verify, extract and CMake-build the pinned static libgit2 into `.build/libgit2` |
| `internal/git2/native.go` | CGo directives: Linux points at `.build/libgit2` directly; darwin/windows use `pkg-config --static libgit2`; windows adds `-static-libgcc` |
| `internal/git2/engine.go` | `PinnedVersion` (`"1.9.6"`); `Open` refuses any other linked libgit2 |
| `internal/app/app.go` | `Version` (`0.1.0-dev`, overridden by `-X .../internal/app.Version`) |
| `scripts/check-deps-linux.sh`, `check-deps-macos.sh`, `check-deps-windows.sh` | Runtime dependency baselines; each has a `--check` mode for explicit lists |
| `scripts/make-sha256sums.sh` | Strict `SHA256SUMS` writer |
| `scripts/check-release-ref.sh` | Release workflow must reference the reusable pipeline by 40-hex SHA |
| `.github/workflows/ci.yml` | `qa` job (`make libgit2`, pre-pull SeaweedFS, `make lint`, `make test`), `npm` job (`make npm-test`), `readme-coverage` job on master |
| `.github/workflows/release.yml` | Tag-triggered (also `workflow_dispatch`) build matrix via `baalimago/simple-go-pipeline`, then `publish-npm`, then `publish-mcp`, both only when `github.ref_type == 'tag'` ([releasing.md](./releasing.md)) |
| `release_test.go` | `TestReleaseDependencyBaselines`, `TestReleaseChecksumGrammar`, `TestReleaseWorkflowReference`, `TestReleaseBinary`, `TestReleaseBinaryCommandSurface`, `TestMCPRegistryManifest`, `TestMCPRegistryPublishWorkflow` |
| `npm/slivingdoc/package.json` | Launcher package: `bin`, `engines.node >= 22`, `mcpName`, `test`, `prepublishOnly` |
| `npm/slivingdoc/bin/slivingdoc.mjs` | Entry point; never writes to stdout |
| `npm/slivingdoc/lib/launcher.mjs` | `run`, `version`, `releaseBaseUrl` (`SLIVINGDOC_RELEASE_BASE`), `cacheRoot` (`SLIVINGDOC_CACHE`, `npm_config_cache`, OS cache dir), `RELEASE_REPO` |
| `npm/slivingdoc/lib/platform.mjs` | `SUPPORTED_TARGETS`, `artifactFor`, `assetName`, `UnsupportedPlatformError` |
| `npm/slivingdoc/lib/install.mjs` | `ensureBinary` (verified, atomic, concurrency-safe cache install), `ChecksumError` |
| `npm/slivingdoc/lib/download.mjs` | `downloadToFile` (streamed, hashing), `downloadText`, `headStatus`, `HttpStatusError` |
| `npm/slivingdoc/lib/sums.mjs` | `parseSums` (strict `SHA256SUMS` grammar) |
| `npm/slivingdoc/lib/spawn.mjs` | `runChild` (inherited stdio, signal forwarding, exit status reproduction) |
| `npm/slivingdoc/scripts/check-release.mjs` | Publication gate: the GitHub release must hold every asset, `SHA256SUMS` and `NOTICE` |
| `NOTICE`, `LICENSE` | MIT grant and libgit2 notices; `NOTICE` ships with every release |

## Flow

```text
make build
  .build/libgit2/.build-stamp ← scripts/build-libgit2.sh
      download v1.9.6 tarball → verify SHA-256 → extract (skip */tests)
      → cmake (static, no transports) → install .build/libgit2/{include,lib,lib/pkgconfig}
  .build/slivingdoc ← CGO_ENABLED=1 go build -trimpath -ldflags '-s -w -X ...app.Version=$(VERSION) [-extldflags "-static"]' .

npx -y slivingdoc serve ...
  bin/slivingdoc.mjs → launcher.run(process)
    → platform.artifactFor(platform, arch)                 # UnsupportedPlatformError before any download
    → install.ensureBinary({version: package.json, spec, cacheRoot, baseUrl})
        cached + checksum still matches → reuse
        else download SHA256SUMS → parseSums → downloadToFile(<base>/v<ver>/<asset>) (hash while streaming)
             → compare → chmod → atomic rename into <cacheRoot>/_slivingdoc/<ver>/<os>/<arch>/<asset>
    → spawn.runChild(binary, argv.slice(2))                # stdio inherited, signals forwarded, exit status reproduced
```

## Behavior

**Requirements.** Go `1.26.5`, Node.js `22.23.2`, `curl`, `tar`, `cmake` everywhere. Linux: `sha256sum` and a C toolchain (no `pkg-config`: `native.go` links `.build/libgit2` directly). macOS: Xcode command-line tools and `shasum`. Windows: Git Bash and mingw-w64 gcc; the build script downloads a pinned, SHA-256-verified `pkg-config-lite` because Windows runners have no usable `pkg-config`, and the cgo build drives the same mingw-w64 gcc that built the archive.

**Pinned libgit2.** Version `v1.9.6` from `https://github.com/libgit2/libgit2/archive/refs/tags/v1.9.6.tar.gz`, SHA-256 `a88a42a4ea9bdab7aa8686eead3bf7d9c6dd74529caca16ab22eaa92433d31d9`, GPL v2 with linking exception (see `NOTICE`). `git2.PinnedVersion` must match; `Engine.Open` refuses any other ABI at startup ([git-engine.md](./git-engine.md)).

**libgit2 build (`build-libgit2.sh`).** Downloads into `.build/` when absent, verifies the SHA-256 (mismatch fails), extracts into `.build/src/libgit2-1.9.6/` skipping the tests subtree (it holds the archive's only symlink, which Windows bsdtar cannot create), configures with `-DBUILD_SHARED_LIBS=OFF -DBUILD_TESTS=OFF -DBUILD_CLI=OFF -DBUILD_EXAMPLES=OFF -DBUILD_FUZZERS=OFF -DUSE_SSH=OFF -DUSE_HTTPS=OFF -DUSE_BUNDLED_ZLIB=ON -DREGEX_BACKEND=builtin` (Windows adds `-G "MinGW Makefiles" -DCMAKE_C_COMPILER=gcc`), and installs headers, `libgit2.a` and `lib/pkgconfig/libgit2.pc` into `.build/libgit2/`. A `CC` from the environment selects the compiler (musl-gcc, arm-linux-gnueabihf-gcc).

**Why transports are off.** All S3 access goes through the AWS Go SDK; libgit2 needs no network. SSH, HTTPS and the CLI are compiled out, keeping libssh2, OpenSSL and HTTP stacks out of the artifact. zlib and regex are bundled. Merge, index and packbuilder stay enabled; the protocol depends on them.

**Executable.** `make build` writes `.build/slivingdoc` (override with `BIN=`). The Makefile exports `PKG_CONFIG_PATH=.build/libgit2/lib/pkgconfig` for macOS and Windows; on Linux `native.go` points CGo at the archive, so a Linux checkout can also `./scripts/build-libgit2.sh && go install .` without Make (that links the verified archive in the checkout, not a system libgit2; release artifacts still use `make build` for version injection and static linking). `-s -w` strips; `VERSION` is injected into `app.Version`; `STATIC=1` adds `-extldflags "-static"` (quoted, because `-ldflags` is space-split). There is no pure-Go build: `CGO_ENABLED=0` fails to compile.

**Targets.** Release tags are `v<semver>`; assets are `slivingdoc-v<semver>-<os>-<arch>` with `.exe` on Windows. Supported: linux/amd64, linux/arm (32-bit ARMv7 hard-float, Raspberry Pi OS armhf), linux/arm64, darwin/amd64, darwin/arm64, windows/amd64. Windows arm64 is deferred until its toolchain and runner are proven.

| Target | Runner | Build |
|---|---|---|
| linux amd64, arm64 | `ubuntu-24.04`, `ubuntu-24.04-arm` | `CC=musl-gcc make build BIN=$TARGET_BINARY VERSION=... STATIC=1` (fully static, runs on glibc and musl) |
| linux arm | `ubuntu-24.04` | `GOFLAGS=-tags=netgo,osusergo GOOS=linux GOARCH=arm GOARM=7 CC=arm-linux-gnueabihf-gcc make build ... STATIC=1`; smoke via `qemu-arm` |
| darwin amd64, arm64 | `macos-15-intel`, `macos-15` | `make build BIN=... VERSION=...` |
| windows amd64 | `windows-2025` | `make build BIN=... VERSION=...` |

`netgo`/`osusergo` keep the static glibc ARMv7 build from loading NSS modules. The pipeline smoke test runs `./$TARGET_BINARY version` (the router has no `--version` flag; `./` is needed because bash does not search the working directory).

**Dependency baselines.** The artifact must not need `libgit2.so`, `libgit2.dylib`, `git2.dll`, or a Git executable. Linux: the checker allows only libc, the loader, pthread, dl, rt, m and vdso (release artifacts are built fully static, so they list none). macOS: only `/usr/lib` and `/System/Library`. Windows: an allow-list of OS system DLLs (`kernel32`, `msvcrt`, `ucrtbase`, `api-ms-win-crt-*`, `ws2_32`, `advapi32`, `bcrypt`, and others; see the `allowed` pattern in the script); `git2.dll`, `libgit2.dll`, `libgcc_s_seh-1.dll`, `libwinpthread-1.dll` are rejected. The Windows checker finds `dumpbin` through `vswhere` and runs it with `MSYS2_ARG_CONV_EXCL='*'` so MSYS2 does not rewrite `/dependents`. The pipeline maps `darwin` to `check-deps-macos.sh`. `TestReleaseDependencyBaselines` drives every checker's `--check` mode in `make test`; the release jobs run the real checker on the real binary.

**Checksums.** `SHA256SUMS`: one LF-terminated line per asset, lowercase 64-hex, two spaces, asset basename, sorted by name. `make-sha256sums.sh` writes it (the pipeline appends `dist/*`), `TestReleaseChecksumGrammar` pins it, and the launcher's `parseSums` rejects any other form.

**CI (`ci.yml`).** Runs on pushes to master, pull requests, and dispatch, with read-only default permissions (`readme-coverage` elevates to `contents: write`) and actions pinned by commit SHA. `qa`: setup Go, cache `.build/libgit2` keyed on the build script, `make libgit2`, `docker pull` the SeaweedFS image (so no timed test pays for the pull), `make lint`, `make test`. `npm`: setup Node, `make npm-test` (no install step; the launcher has zero dependencies). `readme-coverage` (master pushes only) calls the reusable `simple-go-pipeline` validate workflow to update the README coverage line. There is no separate integration job; the Docker-backed suites are part of `make test`.

**npm launcher.** Version comes from `package.json`. `artifactFor` maps Node's platform and arch to a supported target or throws `UnsupportedPlatformError` before any download. `ensureBinary` trusts a cached binary only while its recorded checksum still matches; otherwise it downloads `SHA256SUMS` and the asset from `<base>/v<version>/` into a unique temporary file, hashing while streaming, verifies, sets the executable bit, and atomically renames, so concurrent launchers race safely and a partial download is never executed. `HttpStatusError` (404, redirect loop) is not retried; network failures are. The cache key is `_slivingdoc/<version>/<os>/<arch>/<asset>` below `cacheRoot`: `SLIVINGDOC_CACHE`, else `npm_config_cache`, else the OS cache directory. `SLIVINGDOC_RELEASE_BASE` overrides the GitHub download base (mirrors, tests). `runChild` inherits stdio, forwards SIGINT/SIGTERM, returns the child's exit code, and re-raises a signal death. The launcher never writes to stdout.

## Gotchas

- Go version is pinned in three places in CI (`GO_VERSION` env, the `readme-coverage` `go-version` input, and `release.yml`); the SeaweedFS image in two (`S3_IMAGE`, `prerun-step-cmd`) plus `tests3.Image`.
- `GO_SOURCES` makes `$(BIN)` depend on every `.go` file; without it the in-suite release checks could run a stale binary.
- The libgit2 archive must be built with the same compiler as the cgo link (musl-gcc for static Linux), or the link fails on the ABI mismatch.
- `scripts/release.go` is a `go run` script, not part of the binary. Its shebang-style first line runs `/usr/local/go/bin/go`, so `make release` requires Go at that path.
- The launcher's supported-target list and `release.yml`'s matrix must stay in sync.

## Related

- [releasing.md](./releasing.md): cutting a release, publication order, MCP Registry.
- [testing.md](./testing.md): `make test`, the release test layer.
- [git-engine.md](./git-engine.md): the CGo boundary and pinned-version check.
- [decisions.md](./decisions.md): distribution decisions.
- [AGENTS.md, QA validation](../AGENTS.md#qa-validation).
