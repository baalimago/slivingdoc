#!/usr/bin/env bash
# build-libgit2.sh — download, verify, and build the pinned static libgit2.
#
# Produces .build/libgit2 (headers, static archive, pkg-config file) from the
# libgit2 release pinned in the worklog. The release tarball is verified
# against its SHA-256 before extraction. Network transports are disabled: the
# application performs all S3 access through the AWS Go SDK and must not gain
# SSH or HTTPS as runtime dependencies.
#
# Requirements: curl, tar, cmake, a C toolchain, and sha256sum (or the
# macOS shasum equivalent). On Windows the C toolchain is mingw-w64 gcc —
# the Go cgo build drives the same compiler, so a mingw-built archive links
# without an ABI mismatch — plus a working pkg-config. GitHub Windows
# runners provide mingw-w64 gcc at C:\mingw64\bin but no usable pkg-config
# (Strawberry Perl's .bat wrapper fails), so on Windows the script downloads
# the pinned pkg-config-lite binary (SHA-256 verified, the same artifact the
# chocolatey pkgconfiglite package installs) and pins PKG_CONFIG to it for
# the later Go build step.
set -euo pipefail

version="1.9.6"
sha256="a88a42a4ea9bdab7aa8686eead3bf7d9c6dd74529caca16ab22eaa92433d31d9"
url="https://github.com/libgit2/libgit2/archive/refs/tags/v${version}.tar.gz"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
build_dir="${BUILD_DIR:-$root/.build}"
prefix="$build_dir/libgit2"
archive="$build_dir/libgit2-${version}.tar.gz"
src="$build_dir/src"

uname_s="$(uname -s)"
is_windows=0
if [[ "$uname_s" == MINGW* || "$uname_s" == MSYS* ]]; then
	is_windows=1
fi

for tool in curl tar cmake; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "build-libgit2: missing required tool: $tool" >&2
		exit 1
	fi
done
if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
	echo "build-libgit2: missing sha256sum or shasum" >&2
	exit 1
fi
if [[ "$is_windows" -eq 1 ]] && ! command -v gcc >/dev/null 2>&1; then
	echo "build-libgit2: Windows build needs mingw-w64 gcc on PATH (the GitHub windows runner provides C:\\mingw64\\bin)" >&2
	exit 1
fi

mkdir -p "$build_dir"

if [[ ! -f "$archive" ]]; then
	echo "build-libgit2: downloading $url"
	curl -fsSL --retry 3 -o "$archive" "$url"
fi
if command -v sha256sum >/dev/null 2>&1; then
	echo "$sha256  $archive" | sha256sum -c - >/dev/null
else
	got="$(shasum -a 256 "$archive" | awk '{print $1}')"
	if [[ "$got" != "$sha256" ]]; then
		echo "build-libgit2: sha256 mismatch for $archive" >&2
		exit 1
	fi
fi

rm -rf "$src"
mkdir -p "$src"
# The tarball's tests resources contain a relative symlink
# (tests/resources/testrepo-worktree/link_to_new.txt) that Windows' system
# bsdtar cannot create, so it aborts the whole extraction (exit 2). The
# tests are never compiled (BUILD_TESTS=OFF), so the subtree is skipped;
# --exclude works on GNU tar and on bsdtar alike.
tar -xzf "$archive" -C "$src" --exclude='*/tests' --exclude='*/tests/*'

# Windows toolchain bootstrap. The later Go build resolves the #cgo
# pkg-config directive by running $PKG_CONFIG (default "pkg-config"). GitHub
# Windows runners have no working pkg-config: Strawberry Perl's
# pkg-config.bat is the only match on PATH and it fails, which aborts the
# cgo compile. Download the pinned pkg-config-lite binary (SHA-256 verified)
# into the build tree and pin PKG_CONFIG to it. The variable is written to
# GITHUB_ENV so the pipeline's Build step (a fresh shell) uses the same
# binary regardless of PATH order.
pkg_config_url="https://sourceforge.net/projects/pkgconfiglite/files/0.28-1/pkg-config-lite-0.28-1_bin-win32.zip/download"
pkg_config_sha256="2038c49d23b5ca19e2218ca89f06df18fe6d870b4c6b54c0498548ef88771f6f"
pkg_config_zip="$build_dir/pkg-config-lite-0.28-1_bin-win32.zip"
pkg_config_dir="$build_dir/tools/pkg-config-lite"

if [[ "$is_windows" -eq 1 ]]; then
	if ! pkg-config --version >/dev/null 2>&1; then
		if ! command -v unzip >/dev/null 2>&1; then
			echo "build-libgit2: Windows pkg-config bootstrap needs unzip" >&2
			exit 1
		fi
		if [[ ! -f "$pkg_config_zip" ]]; then
			echo "build-libgit2: downloading pkg-config-lite"
			curl -fsSL --retry 3 -o "$pkg_config_zip" "$pkg_config_url"
		fi
		if command -v sha256sum >/dev/null 2>&1; then
			echo "$pkg_config_sha256  $pkg_config_zip" | sha256sum -c - >/dev/null
		else
			got="$(shasum -a 256 "$pkg_config_zip" | awk '{print $1}')"
			if [[ "$got" != "$pkg_config_sha256" ]]; then
				echo "build-libgit2: sha256 mismatch for $pkg_config_zip" >&2
				exit 1
			fi
		fi
		rm -rf "$pkg_config_dir"
		mkdir -p "$pkg_config_dir"
		unzip -j -q -o "$pkg_config_zip" -d "$pkg_config_dir"
		pkg_config_exe="$pkg_config_dir/pkg-config.exe"
	else
		pkg_config_exe="$(command -v pkg-config)"
	fi
	export PKG_CONFIG="$(cygpath -w "$pkg_config_exe")"
	if ! "$pkg_config_exe" --version >/dev/null 2>&1; then
		echo "build-libgit2: pinned pkg-config does not run: $PKG_CONFIG" >&2
		exit 1
	fi
	if [[ -n "${GITHUB_ENV:-}" ]]; then
		echo "PKG_CONFIG=$PKG_CONFIG" >> "$GITHUB_ENV"
	fi
fi

cmake_args=(
	-S "$src/libgit2-$version"
	-B "$src/build"
	-DCMAKE_BUILD_TYPE=Release
	-DCMAKE_INSTALL_PREFIX="$prefix"
	-DBUILD_SHARED_LIBS=OFF
	-DBUILD_TESTS=OFF
	-DBUILD_CLI=OFF
	-DBUILD_EXAMPLES=OFF
	-DBUILD_FUZZERS=OFF
	-DUSE_SSH=OFF
	-DUSE_HTTPS=OFF
	-DUSE_BUNDLED_ZLIB=ON
	-DREGEX_BACKEND=builtin
)

# The C compiler for the static archive must match the one cgo links with.
# A musl release build sets CC=musl-gcc so libgit2 is built against musl
# libc headers; the later Go link then binds the same musl runtime statically
# (STATIC=1 in the Makefile). When CC is unset, cmake picks the native
# default (clang on macOS, gcc elsewhere), and Windows pins the mingw-w64 gcc
# below.
if [[ -n "${CC:-}" ]]; then
	cmake_args+=(-DCMAKE_C_COMPILER="$CC")
fi

# On Windows, build with the same mingw-w64 gcc that Go's cgo uses. The
# default generator (Visual Studio) produces an MSVC archive whose ABI does
# not match the gcc-driven cgo link.
if [[ "$is_windows" -eq 1 ]]; then
	cmake_args+=(-G "MinGW Makefiles" -DCMAKE_C_COMPILER=gcc)
fi

echo "build-libgit2: configuring static build of libgit2 ${version}"
cmake "${cmake_args[@]}"

cmake --build "$src/build" --config Release -j"$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)"
cmake --install "$src/build"

# The Makefile's .build-stamp is the "already built" marker: touching it here
# makes `make build` skip a redundant rebuild when the release pipeline has
# already run this script in its setup step.
touch "$prefix/.build-stamp"

echo "build-libgit2: installed to $prefix"
