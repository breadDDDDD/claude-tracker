#!/bin/sh
# Maintainers only: rebuild the prebuilt binaries in dist/ (needs Go).
set -e
cd "$(dirname "$0")"
mkdir -p dist
for t in windows/amd64 windows/arm64 darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
  os=${t%/*}; arch=${t#*/}; ext=""; [ "$os" = windows ] && ext=".exe"
  echo "building $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "dist/honjoji-$os-$arch$ext" .
done
