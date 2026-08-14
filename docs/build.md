# Native build

This procedure builds the native executable with a static libgit2. Run
every command from the repository root.

## Requirements

All platforms need Go `1.26.5`, Node.js `22.23.2`, `curl`, `tar`, and
`cmake`.

| Platform | Additional requirements                                         |
| -------- | --------------------------------------------------------------- |
| Linux    | `sha256sum`, `pkg-config`, and a C toolchain (`gcc` or `clang`) |
| macOS    | Xcode command-line tools and `shasum`                           |
| Windows  | Git Bash and mingw-w64 gcc                                      |

On Windows the build script downloads a pinned `pkg-config-lite` binary
(SHA-256 verified) into the build tree, because GitHub Windows runners
provide no usable `pkg-config`. The Go cgo build drives the same
mingw-w64 gcc that builds the archive, so the link has no ABI mismatch.

## Pinned libgit2

| Item    | Value                                                                |
| ------- | -------------------------------------------------------------------- |
| Version | `v1.9.6`                                                             |
| Source  | `https://github.com/libgit2/libgit2/archive/refs/tags/v1.9.6.tar.gz` |
| SHA-256 | `a88a42a4ea9bdab7aa8686eead3bf7d9c6dd74529caca16ab22eaa92433d31d9`   |
| License | GPL v2 with linking exception (see [NOTICE](../NOTICE))              |

## Build libgit2

```text
make libgit2
```

The target runs `scripts/build-libgit2.sh`, which performs every step:

1. Download the pinned tarball into `.build/` when it is not present.
2. Verify the SHA-256 against the pinned value. A mismatch fails the
   build.
3. Extract into `.build/src/libgit2-1.9.6/`. The tarball's tests subtree
   is skipped (`--exclude='*/tests'`): it contains the only symlink of
   the archive, which Windows' system bsdtar cannot create, and the
   tests are never compiled.
4. Configure with CMake, static only, transports disabled:

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

   On Windows the configure also selects the MinGW generator
   (`-G "MinGW Makefiles" -DCMAKE_C_COMPILER=gcc`) so the archive is
   built with the same compiler the cgo link uses.

5. Build and install into `.build/libgit2/` (headers, `libgit2.a`, and
   `lib/pkgconfig/libgit2.pc`).

### Why the transports are off

The application performs all S3 access through the AWS Go SDK. libgit2
needs no network transport, so SSH, HTTPS, and the CLI are compiled out.
This keeps libssh2, OpenSSL, and HTTP stacks out of the artifact. zlib
and the regex backend are bundled, so the runtime dependency list
contains no compression or regex library. Merge, index, and packbuilder
support stay enabled: the notebook protocol depends on them.

## Build the executable

```text
make build
```

The executable is `.build/slivingdoc`. The Makefile sets
`PKG_CONFIG_PATH` to `.build/libgit2/lib/pkgconfig`.
`internal/git2/native.go` declares `#cgo pkg-config: --static libgit2`,
so the CGo toolchain links `libgit2.a` statically. On Windows the same
file adds `-static-libgcc`, which keeps the mingw runtime DLLs out of
the executable's dependency list.

There is no pure-Go build. `internal/git2` requires CGo, so
`CGO_ENABLED=0 go build ./...` fails to compile instead of producing a
binary that starts and then fails every operation.

### Release pipeline wiring

Release pipelines build directly into their artifact name. The reusable
release workflow exports `TARGET_BINARY` (for example
`slivingdoc-v0.1.0-rc0-linux-amd64`), and the caller passes it as the
Makefile's `BIN` variable:

```text
# Linux targets build with musl-gcc + STATIC=1 so the binary is fully static
# and runs on both glibc and musl (alpine).
CC=musl-gcc make build BIN="${TARGET_BINARY}" VERSION="${RELEASE_VERSION#v}" STATIC=1
# macOS and Windows targets keep their native toolchains.
make build BIN="${TARGET_BINARY}" VERSION="${RELEASE_VERSION#v}"
```

The pipeline smoke test runs `./"${TARGET_BINARY}" version`. The `./`
prefix is required because bash does not search the working directory.
The dependency inspection maps the Go-style OS name to the checker's
script name (`darwin` → `check-deps-macos.sh`).

## Inspect dependencies

The release executable must not require `libgit2.so`,
`libgit2.dylib`, `git2.dll`, or a Git executable on the target machine.
One script per platform enforces the baseline from architecture
section 21:

| Script                          | Allowed dependencies                     |
| ------------------------------- | ---------------------------------------- |
| `scripts/check-deps-linux.sh`   | fully static (no dynamic dependencies)   |
| `scripts/check-deps-macos.sh`   | `/usr/lib` and `/System/Library` only    |
| `scripts/check-deps-windows.sh` | documented Windows system DLL allowlist  |

Run the matching script against the built binary:

```text
./scripts/check-deps-linux.sh ./.build/slivingdoc
```

Each script has a `--check` mode that validates an explicit dependency
list. `TestReleaseDependencyBaselines` in `release_test.go` drives that
mode for all three platforms, so `make test` proves the positive and
negative cases without the target toolchain. The release pipeline's
target jobs run the real script against the real binary, so a dynamic
libgit2 linkage fails before any release can assemble.

Windows details: the script locates `dumpbin` through `vswhere` and
invokes it with `MSYS2_ARG_CONV_EXCL='*'`. Without that variable, the
MSYS2 runtime rewrites the `/dependents` option as a POSIX path. The allowlist
admits `kernel32.dll`, `msvcrt.dll`, the Universal CRT forwarders
(`api-ms-win-crt-*`), and `ucrtbase.dll`. It rejects `git2.dll`,
`libgit2.dll`, `libgcc_s_seh-1.dll`, and `libwinpthread-1.dll`.
