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
# macOS shasum equivalent).
set -euo pipefail

version="1.9.6"
sha256="a88a42a4ea9bdab7aa8686eead3bf7d9c6dd74529caca16ab22eaa92433d31d9"
url="https://github.com/libgit2/libgit2/archive/refs/tags/v${version}.tar.gz"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
build_dir="${BUILD_DIR:-$root/.build}"
prefix="$build_dir/libgit2"
archive="$build_dir/libgit2-${version}.tar.gz"
src="$build_dir/src"

for tool in curl tar cmake; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "build-libgit2: missing required tool: $tool" >&2
		exit 1
	fi
	if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
		echo "build-libgit2: missing sha256sum or shasum" >&2
		exit 1
	fi
done

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

echo "build-libgit2: configuring static build of libgit2 ${version}"
cmake -S "$src/libgit2-$version" -B "$src/build" \
	-DCMAKE_BUILD_TYPE=Release \
	-DCMAKE_INSTALL_PREFIX="$prefix" \
	-DBUILD_SHARED_LIBS=OFF \
	-DBUILD_TESTS=OFF \
	-DBUILD_CLI=OFF \
	-DBUILD_EXAMPLES=OFF \
	-DBUILD_FUZZERS=OFF \
	-DUSE_SSH=OFF \
	-DUSE_HTTPS=OFF \
	-DUSE_BUNDLED_ZLIB=ON \
	-DREGEX_BACKEND=builtin

cmake --build "$src/build" --config Release -j"$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)"
cmake --install "$src/build"

echo "build-libgit2: installed to $prefix"
