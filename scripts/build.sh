#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
output=${DOTTY_OUTPUT_DIR:-build/client}
mkdir -p "$output"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o "$output/dottyd" ./cmd/dottyd
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o "$output/dotty-calibrate" ./cmd/dotty_calibrate
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o "$output/dotty-probe" ./cmd/dotty_probe
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$output/dottyd" "$output/dotty-calibrate" "$output/dotty-probe"
else
    shasum -a 256 "$output/dottyd" "$output/dotty-calibrate" "$output/dotty-probe"
fi
