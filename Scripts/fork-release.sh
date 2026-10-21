#!/usr/bin/env bash
# Build, sign and publish a macOS release of this fork to its own GitHub
# Releases, which the fork's update feed (mygo.json updates.github) reads.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO="${FORK_REPO:-fan776783/Quosparr}"
REMOTE="${FORK_REMOTE:-fork}"
KEY="${FORK_UPDATE_KEY:-$HOME/.config/quosparr-fork/mygo-update.key}"
ARCH="${ARCH:-$(uname -m)}"
case "$ARCH" in
  arm64) GOARCH=arm64 ;;
  x86_64) GOARCH=amd64 ;;
  *) printf 'Unsupported architecture: %s\n' "$ARCH" >&2; exit 2 ;;
esac
die() { printf '%s\n' "$*" >&2; exit 1; }

cd "$ROOT"
[[ -z "$(git status --porcelain)" ]] || die "Working tree is not clean"
[[ -r "$KEY" ]] || die "Missing update signing key: $KEY"
VERSION="$(python3 -c 'import json; print(json.load(open("mygo.json"))["version"])')"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+-cursor\.[0-9]+$ ]] || die "Version must look like 1.7.3-cursor.1, got $VERSION"
TAG="v$VERSION"
HEAD="$(git rev-parse HEAD)"
[[ "$(git ls-remote "$REMOTE" refs/heads/main | cut -f1)" == "$HEAD" ]] || die "Push HEAD to $REMOTE/main first"
! gh release view "$TAG" -R "$REPO" >/dev/null 2>&1 || die "Release $TAG already exists"

WORK="$ROOT/build/fork-release-$ARCH"
rm -rf "$WORK"
MYGO_UPDATE_PRIVATE_KEY="$(cat "$KEY")" V1_DEVELOPMENT_PACKAGE=0 V1_BUILD_DIR="$WORK" ARCH="$ARCH" \
  bash "$ROOT/Scripts/package-v1-app.sh"
go run ./Scripts/release-assets verify -config "$WORK/config/mygo.json" -dir "$WORK" -targets "darwin-$GOARCH"

gh release create "$TAG" -R "$REPO" --target "$HEAD" --latest --title "Quosparr $VERSION" \
  --notes "Fork build of upstream v${VERSION%%-cursor.*} with Cursor quota monitoring." \
  "$WORK/Quosparr-macos-$ARCH.zip" "$WORK/Quosparr-macos-$ARCH.dmg" \
  "$WORK/quosparr-$VERSION-darwin-$GOARCH.tar.gz" "$WORK/update-darwin-$GOARCH.json"
printf 'Published %s: https://github.com/%s/releases/tag/%s\n' "$TAG" "$REPO" "$TAG"
