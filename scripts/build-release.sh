#!/usr/bin/env bash
# Cross-compile gitenv for every supported platform into dist/.
#
# Usage: scripts/build-release.sh [version]
# The version defaults to `git describe` and is embedded via -ldflags.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT_DIR="dist"
PACKAGE="./cmd/gitenv"

# GOOS/GOARCH pairs published for each release.
TARGETS=(
	"linux/amd64"
	"linux/arm64"
	"darwin/amd64"
	"darwin/arm64"
	"windows/amd64"
	"windows/arm64"
)

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

echo "building gitenv $VERSION"
for target in "${TARGETS[@]}"; do
	os="${target%/*}"
	arch="${target#*/}"
	name="gitenv_${os}_${arch}"
	if [ "$os" = "windows" ]; then
		name="${name}.exe"
	fi
	echo "  -> $name"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
		go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
		-o "$OUT_DIR/$name" "$PACKAGE"
done

# Checksums let the installers verify each download before trusting it.
( cd "$OUT_DIR" && sha256sum gitenv_* > checksums.txt )

echo "artifacts in $OUT_DIR/:"
ls -1 "$OUT_DIR"
