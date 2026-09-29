#!/bin/sh
set -eu

repo=baalimago/slivingdoc

fail() {
	printf 'slivingdoc setup: %s\n' "$1" >&2
	exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"

case "$(uname -s)" in
	Linux*) os=linux ;;
	Darwin*) os=darwin ;;
	*) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64|amd64) arch=amd64 ;;
	armv7l|armv7*) arch=arm ;;
	aarch64|arm64) arch=arm64 ;;
	*) fail "unsupported architecture: $(uname -m)" ;;
esac

case "$os/$arch" in
	linux/amd64|linux/arm|linux/arm64|darwin/amd64|darwin/arm64) ;;
	*) fail "no release binary is available for $os/$arch" ;;
esac

printf "Detected platform: %s/%s\n" "$os" "$arch"

release_data=$(curl --fail --location --silent --show-error \
	"https://api.github.com/repos/$repo/releases/latest") || fail "could not fetch the latest release metadata"

asset_url=$(printf '%s\n' "$release_data" |
	grep '"browser_download_url"' |
	grep "/slivingdoc-v[^\" ]*-${os}-${arch}\"" |
	sed 's/.*"browser_download_url":[[:space:]]*"\([^"]*\)".*/\1/' |
	head -n 1)
[ -n "$asset_url" ] || fail "the latest release has no binary for $os/$arch"

case "$asset_url" in
	https://github.com/baalimago/slivingdoc/releases/download/*) ;;
	*) fail "the latest release returned an unexpected download URL" ;;
esac

asset_name=${asset_url##*/}
sums_url=${asset_url%/*}/SHA256SUMS

if [ "$(id -u)" -eq 0 ]; then
	default_install_dir=/usr/local/bin
else
	[ -n "${HOME:-}" ] || fail "HOME must be set for a user installation"
	default_install_dir=$HOME/.local/bin
fi
install_dir=${INSTALL_DIR:-$default_install_dir}
[ -n "$install_dir" ] || fail "INSTALL_DIR must not be empty"
mkdir -p "$install_dir" || fail "could not create install directory: $install_dir"

tmp_dir=$(mktemp -d "$install_dir/.slivingdoc.XXXXXX") || fail "could not create a temporary directory in $install_dir"
trap 'rm -rf "$tmp_dir"' 0
trap 'exit 1' HUP INT TERM

printf 'Downloading release checksum... '
curl --fail --location --silent --show-error "$sums_url" -o "$tmp_dir/SHA256SUMS" || fail "could not download release checksums"

expected=$(awk -v name="$asset_name" '$2 == name { count++; checksum = $1 } END { if (count != 1) exit 1; print checksum }' "$tmp_dir/SHA256SUMS") || fail "release checksums do not contain exactly one entry for $asset_name"
case "$expected" in
	*[!0123456789abcdef]*|'') fail "release checksum for $asset_name is invalid" ;;
esac
[ "${#expected}" -eq 64 ] || fail "release checksum for $asset_name is invalid"

printf 'verified manifest found\nDownloading binary... '
curl --fail --location --silent --show-error "$asset_url" -o "$tmp_dir/$asset_name" || fail "could not download $asset_name"

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp_dir/$asset_name" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$tmp_dir/$asset_name" | awk '{print $1}')
else
	fail "sha256sum or shasum is required to verify the download"
fi
[ "$actual" = "$expected" ] || fail "checksum verification failed for $asset_name"
printf 'checksum verified\n'

chmod 0755 "$tmp_dir/$asset_name" || fail "could not make the binary executable"
mv "$tmp_dir/$asset_name" "$install_dir/slivingdoc" || fail "could not install to $install_dir/slivingdoc"

printf 'slivingdoc installed successfully in %s\n' "$install_dir"