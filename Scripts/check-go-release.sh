#!/usr/bin/env bash
# Shared pre-publication checks for CI and Fork Sync. No desktop interaction.
set -euo pipefail
cd "$(dirname "$0")/.."

go vet ./...
go test -race -count=1 -timeout=10m ./...
go test -tags=quosparr_dev_package -count=1 ./internal/app -run '^TestPackagedDevelopmentUpdate'
for target in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64 linux/amd64 linux/arm64; do
  GOOS="${target%/*}" GOARCH="${target#*/}" CGO_ENABLED=0 go build ./...
done
python3 Scripts/Tests/test-v1-release.py
python3 Scripts/Tests/test-v1-homebrew.py
python3 Scripts/Tests/test-v1-platform.py
python3 Scripts/Tests/test-workflow-drafts.py
