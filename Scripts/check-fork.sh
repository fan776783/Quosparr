#!/usr/bin/env bash
# Fork-only checks. Upstream CI runs the shared Go and release-helper tests;
# Fork Sync also invokes this from its complete pre-publication gate.
set -euo pipefail
cd "$(dirname "$0")/.."

for target in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64 linux/amd64 linux/arm64; do
  GOOS="${target%/*}" GOARCH="${target#*/}" CGO_ENABLED=0 go build ./...
done
for test in Scripts/Tests/test-fork-*.py; do
  python3 "$test"
done
