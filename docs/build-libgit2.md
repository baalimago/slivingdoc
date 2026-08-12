# Build procedure — static libgit2

slivingdoc links one pinned libgit2 release into every executable. The
release tarball is verified against its SHA-256 before extraction, the build
disables every network transport, and the resulting binary depends only on
the operating-system baseline libraries.

## Pinned release

| Item    | Value                                                                |
| ------- | -------------------------------------------------------------------- |
| Version | `v1.9.6`                                                             |
| Source  | `https://github.com/libgit2/libgit2/archive/refs/tags/v1.9.6.tar.gz` |
| SHA-256 | `a88a42a4ea9bdab7aa8686eead3bf7d9c6dd74529caca16ab22eaa92433d31d9`   |
| License | GPL v2 with linking exception (see [NOTICE](../NOTICE))              |

The checksum is the reviewed value from the worklog baseline. The build
script fails when the downloaded bytes do not match it.

## Requirements

Linux requires `curl`, `tar`, `sha256sum`, `cmake`, `pkg-config`, and a C
toolchain (`gcc` or `clang`). macOS requires Xcode command-line tools, `curl`,
`tar`, `cmake`, and `shasum` (the script falls back from `sha256sum`). Windows
requires Git Bash, `curl`, `tar`, `cmake`, mingw-w64 gcc, and a working
`pkg-config`. The Go cgo build drives the same mingw-w64 gcc, so a
mingw-built archive links without an ABI mismatch. GitHub Windows runners
provide mingw-w64 gcc at `C:\mingw64\bin` but no usable `pkg-config`
(Strawberry Perl's `.bat` wrapper fails), so on Windows the script installs
`pkgconfiglite` through Chocolatey when needed and pins `PKG_CONFIG` to the
real binary for the later Go build step.

## Procedure

```text
make libgit2
```

The `scripts/build-libgit2.sh` script performs every step:

1. Download the pinned tarball into `.build/` when it is not present.
2. Verify `sha256sum` against the pinned value.
3. Extract into `.build/src/libgit2-1.9.6/`. The tarball's tests subtree is skipped (`--exclude='*/tests'`): its resources contain the only symlink of the archive, which Windows' system bsdtar cannot create, and the tests are never compiled (`BUILD_TESTS=OFF`).
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
   (`-G "MinGW Makefiles" -DCMAKE_C_COMPILER=gcc`) so the archive is built
   with the same compiler the cgo link uses; on Linux and macOS the native
   generator and toolchain are used (`--config Release` keeps the
   configuration explicit for multi-config generators).

5. Build and install into `.build/libgit2/` (headers, `libgit2.a`, and
   `lib/pkgconfig/libgit2.pc`).

The native Go build then links through pkg-config:

```text
PKG_CONFIG_PATH="$(pwd)/.build/libgit2/lib/pkgconfig" go build ./...
```

`internal/git2/native.go` declares `#cgo pkg-config: --static libgit2`, so
the CGo toolchain links `libgit2.a` and its transitive baseline libraries.
On Windows the same file adds `#cgo windows LDFLAGS: -static-libgcc
-static-libwinpthread`, which keeps the mingw-w64 compiler runtime out of
the executable's runtime dependency list.

## Disabled feature rationale

The application performs all S3 access through the AWS Go SDK. libgit2 needs
no network transport, so SSH, HTTPS, and the CLI are compiled out. This keeps
libssh2, OpenSSL, and HTTP stacks out of the artifact and out of the runtime
dependency list.

Thread support, SHA-1 repositories, zlib pack support, merge, index, and
packbuilder support stay enabled: the merge and pack behavior of the
notebook protocol depends on them. zlib and the regular-expression backend
are bundled (`USE_BUNDLED_ZLIB=ON`, `REGEX_BACKEND=builtin`) so the runtime
dependency list contains no compression or regex library.

## Dependency inspection

The release executable must not require `libgit2.so`, `libgit2.dylib`,
`git2.dll`, or a Git executable on the target machine. One script per
platform enforces the architecture section 21 baseline:

```text
scripts/check-deps-linux.sh     C runtime, loader, pthread, dl, rt, math
scripts/check-deps-macos.sh     /usr/lib and /System/Library only
scripts/check-deps-windows.sh   documented Windows system DLL allowlist
```

Each script has a `--check` mode that validates an explicit dependency list.
`TestReleaseDependencyBaselines` in `release_test.go` drives that mode for
all three platforms, proving the positive and negative cases without the
target toolchain. The Windows script locates `dumpbin` through `vswhere` on GitHub
Windows runners. Its allowlist admits the Windows system DLLs the release
links — `kernel32.dll`, the Go runtime's `msvcrt.dll`, and the Universal CRT
`ucrtbase.dll` that the mingw-w64 UCRT toolchain links — and rejects
`git2.dll`, `libgit2.dll`, and the mingw runtime DLLs `libgcc_s_seh-1.dll`
and `libwinpthread-1.dll`.

The target jobs of the release pipeline run the matching script against the
built binary, so a dynamic libgit2 linkage fails the target job before any
release can assemble.
