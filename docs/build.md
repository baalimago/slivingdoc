# Native build

This procedure builds the native executable with static libgit2. Run the commands from the repository root.

## Requirements

Install Go `1.26.5`, Node.js `24.19.0`, `curl`, `tar`, `cmake`, `pkg-config`, and a C compiler. Linux also needs `sha256sum`. macOS can use `shasum -a 256`.

## Build libgit2

The pinned source is libgit2 `v1.9.6`. Its SHA-256 is `a88a42a4ea9bdab7aa8686eead3bf7d9c6dd74529caca16ab22eaa92433d31d9`.

Run this command:

```text
make libgit2
```

The command runs `scripts/build-libgit2.sh`. The script downloads `https://github.com/libgit2/libgit2/archive/refs/tags/v1.9.6.tar.gz`. It checks the SHA-256 before extraction. It then configures this static build:

```text
-DBUILD_SHARED_LIBS=OFF
-DBUILD_TESTS=OFF
-DBUILD_CLI=OFF
-DBUILD_EXAMPLES=OFF
-DBUILD_FUZZERS=OFF
-DUSE_SSH=OFF
-DUSE_HTTPS=OFF
-DUSE_BUNDLED_ZLIB=ON
-DREGEX_BACKEND=builtin
```

The script installs headers, `libgit2.a`, and `libgit2.pc` in `.build/libgit2/`. Network transports stay disabled. The AWS SDK owns all S3 network access.

## Build the executable

Run this command:

```text
make build
```

The executable is `.build/slivingdoc`. The Makefile sets `PKG_CONFIG_PATH` to `.build/libgit2/lib/pkgconfig`. `internal/git2/native.go` requests static linking with `#cgo pkg-config: --static libgit2`.

Release pipelines build directly into their artifact name instead of `.build/slivingdoc`. The reusable release workflow exports `TARGET_BINARY` (the architecture-21 asset name, for example `slivingdoc-v0.1.0-rc0-linux-amd64`), and the caller passes it as the Makefile's `BIN` variable: `make build BIN="${TARGET_BINARY}" VERSION="${RELEASE_VERSION#v}"`. The artifact then exists at the exact path the pipeline's dependency inspection, smoke test, and upload steps expect. The pipeline smoke runs the `version` subcommand (`./"${TARGET_BINARY}" version`): the router has no `--version` flag — a dash-prefixed argument is a skipped flag and a bare invocation prints usage — and bash does not search the working directory, so the `./` prefix is required.

There is no pure-Go build. `internal/git2` requires CGo, so `CGO_ENABLED=0 go build ./...` fails to compile rather than producing a binary that starts and then fails every operation.

## Inspect dependencies

Run this command on Linux:

```text
./scripts/check-deps-linux.sh ./.build/slivingdoc
```

Linux allows only the loader and the C runtime, pthread, dl, rt, and math libraries. The result must not name `libgit2.so`, `libz.so`, or `libpcre2`.

Run this command on macOS:

```text
./scripts/check-deps-macos.sh ./.build/slivingdoc
```

macOS allows libraries in `/usr/lib/` and `/System/Library/` only.

Run this command on Windows in a Visual Studio developer environment:

```text
./scripts/check-deps-windows.sh .build/slivingdoc.exe
```

Windows allows the documented system DLL list only. It rejects `git2.dll`, `libgit2.dll`, and third-party DLLs.

These checks are not manual steps. `release_test.go` runs them as part of `make test`: it builds the release-style binary, proves `slivingdoc version` reports the version injected through the linker, and on Linux runs the dependency baseline against the real executable. See [build-libgit2.md](build-libgit2.md) for the detailed rationale.
