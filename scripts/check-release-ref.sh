#!/usr/bin/env bash
# check-release-ref.sh — the release workflow must reference the reusable
# pipeline by immutable commit SHA, never by a branch or a moving tag.
#
# The reference in .github/workflows/release.yml must be a real 40-hex
# commit SHA of the reusable simple-go-pipeline workflow. This check turns
# a placeholder (and any branch or tag reference) into a clear diagnostic
# for humans; the workflow itself fails at dispatch on an unresolvable SHA.
#
# Usage: check-release-ref.sh <workflow-file>
set -euo pipefail

file="${1:?usage: check-release-ref.sh <workflow-file>}"

if [[ ! -f "$file" ]]; then
	echo "check-release-ref: $file does not exist; the release workflow is not in place" >&2
	exit 1
fi

ref="$(sed -n 's#.*baalimago/simple-go-pipeline/.github/workflows/release.yml@\([0-9a-f]\{40\}\).*#\1#p' "$file" | head -n 1)"

if [[ -z "$ref" ]]; then
	echo "check-release-ref: $file does not reference the reusable release workflow by a 40-hex commit SHA" >&2
	exit 1
fi

placeholder="0000000000000000000000000000000000000000"
if [[ "$ref" == "$placeholder" ]]; then
	echo "check-release-ref: $file uses the placeholder SHA; the simple-go-pipeline release-workflow change is not merged yet" >&2
	exit 1
fi

echo "check-release-ref: ok ($ref)"
