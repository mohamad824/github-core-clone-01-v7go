#!/usr/bin/env bash
# Rebuild the v7-go forge binary from source (reproducible, static).
set -euo pipefail
cd "$(dirname "$0")/go"
export GOCACHE="${GOCACHE:-$HOME/workspace/.cache/go-build}"
export GOPATH="${GOPATH:-$HOME/workspace/.go}"
export GOPROXY=off GOFLAGS=-mod=mod CGO_ENABLED=0
GO="${GO:-$HOME/workspace/.tools/go/bin/go}"
[ -x "$GO" ] || GO=go
"$GO" build -trimpath -ldflags="-s -w" -o ../forge .
