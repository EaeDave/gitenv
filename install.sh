#!/usr/bin/env bash
# gitenv installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/EaeDave/gitenv/main/install.sh | bash
#
# Environment overrides:
#   GITENV_VERSION      release tag to install (default: latest)
#   GITENV_INSTALL_DIR  install directory (default: $HOME/.local/bin)
#   GITENV_BASE_URL     release base URL (default: GitHub releases)
set -euo pipefail

REPO="EaeDave/gitenv"
BASE_URL="${GITENV_BASE_URL:-https://github.com/$REPO/releases}"
VERSION="${GITENV_VERSION:-latest}"
INSTALL_DIR="${GITENV_INSTALL_DIR:-$HOME/.local/bin}"
BINARY="gitenv"
GITENV_TMP=""

cleanup() { [ -n "${GITENV_TMP:-}" ] && rm -rf "$GITENV_TMP"; return 0; }
trap cleanup EXIT

info() { printf '\033[1;36m==>\033[0m %s\n' "$1"; }
err() {
	printf '\033[1;31merror:\033[0m %s\n' "$1" >&2
	exit 1
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) err "unsupported OS '$(uname -s)'; use the Windows installer or build from source" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) err "unsupported architecture '$(uname -m)'" ;;
	esac
}

download() {
	# download <url> <dest>
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		err "need curl or wget to download releases"
	fi
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		err "need sha256sum or shasum to verify the download"
	fi
}

main() {
	local os arch asset url_dir tmp
	os="$(detect_os)"
	arch="$(detect_arch)"
	asset="gitenv_${os}_${arch}"

	if [ "$VERSION" = "latest" ]; then
		url_dir="$BASE_URL/latest/download"
	else
		url_dir="$BASE_URL/download/$VERSION"
	fi

	tmp="$(mktemp -d)"
	GITENV_TMP="$tmp"

	info "downloading $asset ($VERSION)"
	download "$url_dir/$asset" "$tmp/$BINARY" || err "download failed for $url_dir/$asset"
	download "$url_dir/checksums.txt" "$tmp/checksums.txt" || err "could not fetch checksums.txt"

	info "verifying checksum"
	local expected actual
	expected="$(awk -v f="$asset" '$2 == f {print $1}' "$tmp/checksums.txt")"
	[ -n "$expected" ] || err "no checksum listed for $asset"
	actual="$(sha256_of "$tmp/$BINARY")"
	[ "$expected" = "$actual" ] || err "checksum mismatch for $asset (expected $expected, got $actual)"

	info "installing to $INSTALL_DIR/$BINARY"
	mkdir -p "$INSTALL_DIR"
	chmod +x "$tmp/$BINARY"
	mv "$tmp/$BINARY" "$INSTALL_DIR/$BINARY"

	info "installed $("$INSTALL_DIR/$BINARY" version 2>/dev/null || echo "$BINARY")"
	case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		printf '\n\033[1;33mnote:\033[0m %s is not on your PATH.\n' "$INSTALL_DIR"
		printf 'Add this line to your shell profile (~/.bashrc, ~/.zshrc, ...):\n'
		printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
		;;
	esac
}

main "$@"
