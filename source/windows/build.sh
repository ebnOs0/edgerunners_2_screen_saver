#!/bin/bash
# Cross-compiles EdgeRunners2.scr (Windows x64, also runs on Windows on ARM) into ../../windows/
set -euo pipefail
cd "$(dirname "$0")"

cp ../logo.png logo.png
mkdir -p ../../windows
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w" -o ../../windows/EdgeRunners2.scr .
echo "built ../../windows/EdgeRunners2.scr"
