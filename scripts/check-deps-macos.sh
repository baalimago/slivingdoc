#!/usr/bin/env bash
# check-deps-macos.sh — verify a macOS binary depends only on the baseline.
#
# The slivingdoc release executable must contain libgit2 and every non-system
# dependency. The architecture section 21 baseline allows macOS libraries in
# /usr/lib and /System/Library only. Anything else — most importantly
# libgit2.dylib — fails the check.
#
# Usage:
#   check-deps-macos.sh <binary>          inspect a binary with otool
#   check-deps-macos.sh --check <dep...>  check an explicit dependency list
set -euo pipefail

# The baseline matches the architecture section 21 list. libgit2.dylib,
# @rpath entries, and Homebrew paths are deliberately absent: the pinned
# build links libgit2 statically.
allowed='^(/usr/lib/|/System/Library/)'

needed() {
	otool -L "$1" 2>/dev/null | tail -n +2 | sed -n 's/^[[:space:]]*\(.*\) (compatibility version.*/\1/p'
}

check() {
	local -a bad=()
	while IFS= read -r dep; do
		[[ -z "$dep" ]] && continue
		if [[ ! "$dep" =~ $allowed ]]; then
			bad+=("$dep")
		fi
	done
	if [[ ${#bad[@]} -gt 0 ]]; then
		echo "check-deps-macos: unexpected dynamic dependencies:" >&2
		printf '  %s\n' "${bad[@]}" >&2
		exit 1
	fi
}

if [[ "${1:-}" == "--check" ]]; then
	shift
	printf '%s\n' "$@" | check
elif ! command -v otool >/dev/null 2>&1; then
	echo "check-deps-macos: otool not found (run on macOS)" >&2
	exit 1
else
	binary="${1:?usage: check-deps-macos.sh <binary>}"
	check < <(needed "$binary")
fi
echo "check-deps-macos: ok"
