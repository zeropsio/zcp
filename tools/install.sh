#!/bin/bash
cd "$(dirname "$0")"
set -e
cd ..
export GOBIN=$PWD/bin
export PATH="${GOBIN}:${PATH}"
[[ ! -d "${GOBIN}" ]] && mkdir -p "${GOBIN}"
echo "GOBIN=${GOBIN}"
rm -rf tmp
# Analyzer bootstrap follows the declared language floor, not the release compiler.
GOTOOLCHAIN=go1.25.12 GOBIN="$GOBIN" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0
