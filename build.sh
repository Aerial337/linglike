#!/bin/sh
# Builds the Windows executables into ./build (works on Linux, macOS, Windows).
set -e
cd "$(dirname "$0")"
mkdir -p build
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o build/linglike.exe ./cmd/linglike
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o build/lingcli.exe ./cmd/lingcli
ls -la build
