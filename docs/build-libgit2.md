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
requires Git Bash, `curl`, `tar`, `cmake`, and the Visual Studio C++ build
tools (the script accepts the default CMake generator; `--config Release`
keeps the configuration explicit for multi-config generators).

## Procedure

```text
make libgit2
```

The `scripts/build-libgit2.sh` script performs every step:

1. Download the pinned tarball into `.build/` when it is not present.
2. Verify `sha256sum` against the pinned value.
3. Extract into `.build/src/libgit2-1.9.6/`.
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

5. Build and install into `.build/libgit2/` (headers, `libgit2.a`, and
   `lib/pkgconfig/libgit2.pc`).

The native Go build then links through pkg-config:

```text
PKG_CONFIG_PATH="$(pwd)/.build/libgit2/lib/pkgconfig" go build ./...
```

`internal/git2/native.go` declares `#cgo pkg-config: --static libgit2`, so
the CGo toolchain links `libgit2.a` and its transitive baseline libraries.

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
Windows runners; the allowlist is locked by the first real Windows release
run.

The target jobs of the release pipeline run the matching script against the
built binary, so a dynamic libgit2 linkage fails the target job before any
release can assemble.
