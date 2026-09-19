#!/bin/sh
set -eu
test "$(go env GOOS)" = linux
test "$(go env GOARCH)" = amd64
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o dist/cpa-helper-plugin.so ./cmd/cpa-helper-plugin
