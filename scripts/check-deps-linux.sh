#!/usr/bin/env bash
# check-deps-linux.sh — verify a Linux binary depends only on the baseline.
#
# The slivingdoc release executable must contain libgit2 and every non-system
# dependency. Linux may use only the target baseline C runtime, loader,
# pthread, dl, rt, and math libraries. Anything else — most importantly
# libgit2.so — fails the check.
#
# Usage:
#   check-deps-linux.sh <binary>          inspect a binary
#   check-deps-linux.sh --check <dep...>  check an explicit dependency list
set -euo pipefail

# The baseline matches the architecture section 21 list. libgit2.so,
# libz.so, and libpcre2 are deliberately absent: the pinned build bundles
# zlib and pcre2 and links libgit2 statically.
allowed='^(linux-vdso\.so|libc\.so|ld-linux|libpthread\.so|libdl\.so|librt\.so|libm\.so)'

needed() {
	readelf -d "$1" 2>/dev/null | sed -n 's/.*(NEEDED).*\[\(.*\)\].*/\1/p' | sort -u
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
		echo "check-deps-linux: unexpected dynamic dependencies:" >&2
		printf '  %s\n' "${bad[@]}" >&2
		exit 1
	fi
}

if [[ "${1:-}" == "--check" ]]; then
	shift
	printf '%s\n' "$@" | check
else
	binary="${1:?usage: check-deps-linux.sh <binary>}"
	check < <(needed "$binary")
fi
echo "check-deps-linux: ok"
