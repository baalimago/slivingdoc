#!/usr/bin/env bash
# make-sha256sums.sh — emit the architecture section 21 SHA256SUMS file.
#
# The release grammar is strict: one LF-terminated line per asset, lowercase
# 64-hex SHA-256, two spaces, and the asset name; lines sorted by asset name.
# Names are the basenames of the given paths, matching the names under which
# the release uploads the assets. The assembly job of the release pipeline
# runs this over the exact bytes that will be uploaded, so the checksums
# cover the published artifacts.
#
# Usage: make-sha256sums.sh <asset...>
# Writes the checksum file to stdout.
set -euo pipefail

if [[ $# -eq 0 ]]; then
	echo "make-sha256sums: no assets given" >&2
	exit 1
fi

sum() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$@"
	else
		shasum -a 256 "$@"
	fi
}

while [[ $# -gt 0 ]]; do
	file="$1"
	shift
	hex="$(sum "$file" | awk '{print $1}')"
	printf '%s  %s\n' "$hex" "$(basename "$file")"
done | sort -k2
