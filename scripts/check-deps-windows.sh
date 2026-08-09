#!/usr/bin/env bash
# check-deps-windows.sh — verify a Windows PE binary depends only on the
# documented Windows system DLL baseline (architecture section 21).
#
# The slivingdoc release executable must contain libgit2 and every non-system
# dependency. Windows may use only documented Windows system DLLs; anything
# else — most importantly git2.dll or libgit2.dll — fails the check.
#
# Real inspection needs dumpbin from a Visual Studio installation; vswhere
# locates the latest installation on GitHub Windows runners. The --check mode
# validates an explicit dependency list and runs anywhere (used by the
# self-test and by CI fixture evidence).
#
# Usage:
#   check-deps-windows.sh <binary>          inspect a binary with dumpbin
#   check-deps-windows.sh --check <dep...>  check an explicit dependency list
set -euo pipefail

# The baseline is the documented Windows system DLL allowlist. Dependency
# names are compared case-insensitively. git2.dll, libgit2.dll, and any
# third-party DLL are deliberately absent.
allowed='^(kernel32\.dll|msvcrt\.dll|user32\.dll|advapi32\.dll|shell32\.dll|ws2_32\.dll|ole32\.dll|oleaut32\.dll|bcrypt\.dll|crypt32\.dll|iphlpapi\.dll|ntdll\.dll|gdi32\.dll|version\.dll|winmm\.dll|wininet\.dll|dnsapi\.dll|secur32\.dll|netapi32\.dll|shlwapi\.dll|wtsapi32\.dll|rpcrt4\.dll|setupapi\.dll|dbghelp\.dll|psapi\.dll|wintrust\.dll|wldap32\.dll|normaliz\.dll|userenv\.dll|sspicli\.dll|comdlg32\.dll|comctl32\.dll|imm32\.dll|msimg32\.dll|uxtheme\.dll|dwmapi\.dll|powrprof\.dll|shcore\.dll)$'

find_dumpbin() {
	local vswhere install_dir
	vswhere="${ProgramFiles(x86)}/Microsoft Visual Studio/Installer/vswhere.exe"
	[[ -f "$vswhere" ]] || return 1
	install_dir="$("$vswhere" -latest -products '*' \
		-requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 \
		-property installationPath 2>/dev/null)" || return 1
	find "$install_dir/VC/Tools/MSVC" -name dumpbin.exe -path '*/Hostx64/x64/*' 2>/dev/null \
		| sort -V | tail -n 1
}

check() {
	local -a bad=() dep
	while IFS= read -r dep; do
		[[ -z "$dep" ]] && continue
		dep="${dep,,}"
		if [[ ! "$dep" =~ $allowed ]]; then
			bad+=("$dep")
		fi
	done
	if [[ ${#bad[@]} -gt 0 ]]; then
		echo "check-deps-windows: unexpected dynamic dependencies:" >&2
		printf '  %s\n' "${bad[@]}" >&2
		exit 1
	fi
}

if [[ "${1:-}" == "--check" ]]; then
	shift
	printf '%s\n' "$@" | check
else
	binary="${1:?usage: check-deps-windows.sh <binary>}"
	dumpbin="$(command -v dumpbin 2>/dev/null || find_dumpbin || true)"
	if [[ -z "$dumpbin" ]]; then
		echo "check-deps-windows: dumpbin not found; run inside a Visual Studio developer environment" >&2
		exit 1
	fi
	"$dumpbin" /dependents "$binary" 2>/dev/null \
		| sed -n 's/^    \([A-Za-z0-9_.-]*\.dll\)$/\1/Ip' \
		| check
fi
echo "check-deps-windows: ok"
