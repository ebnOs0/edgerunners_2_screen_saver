#!/bin/bash
# Builds a universal (Apple Silicon + Intel) EdgeRunners2.saver into ../../macos/
set -euo pipefail
cd "$(dirname "$0")"

OUT=../../macos/EdgeRunners2.saver
rm -rf "$OUT"
mkdir -p "$OUT/Contents/MacOS" "$OUT/Contents/Resources"

clang -arch arm64 -arch x86_64 -mmacosx-version-min=10.13 -fobjc-arc -O2 -bundle \
    -framework ScreenSaver -framework Cocoa -framework QuartzCore \
    -o "$OUT/Contents/MacOS/EdgeRunners2" EdgeRunners2View.m

cp Info.plist "$OUT/Contents/Info.plist"
cp ../logo.png "$OUT/Contents/Resources/logo.png"
sips -Z 300 ../logo.png --padToHeightWidth 200 320 --padColor 000000 --out "$OUT/Contents/Resources/thumbnail@2x.png" >/dev/null 2>&1
sips -Z 150 ../logo.png --padToHeightWidth 100 160 --padColor 000000 --out "$OUT/Contents/Resources/thumbnail.png" >/dev/null 2>&1

codesign --force --deep --sign - "$OUT"
echo "built $OUT"
