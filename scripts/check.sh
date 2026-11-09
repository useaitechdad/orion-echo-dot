#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
go test -race ./internal/... ./cmd/dottyd ./cmd/dotty_probe ./cmd/dotty_calibrate
go vet ./internal/... ./cmd/dottyd ./cmd/dotty_probe ./cmd/dotty_calibrate
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build ./internal/audio
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o /dev/null ./cmd/dottyd
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o /dev/null ./cmd/dotty_calibrate
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o /dev/null ./cmd/dotty_probe
for script in scripts/*.sh native/microwake/dotty-wake; do
    sh -n "$script"
done
