#!/usr/bin/env bash
# Builds every release artifact locally, the way .github/workflows/release.yml
# does, and smoke-tests the ones this machine can open. Nothing is published.
#
#   dry-run.sh [version]          default: v0.0.0-dryrun
#
# macOS packaging needs macOS; Linux packaging runs in Docker (skipped, with a
# note, when Docker is unavailable). Artifacts land in bin/release/.
set -euo pipefail

version="${1:-v0.0.0-dryrun}"
repo="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
cd "$repo"
export VERSION="$version"
rm -rf bin/release bin/package

echo "== frontend"
make frontend >/dev/null

if [[ "$(uname -s)" == Darwin ]]; then
  echo "== macOS (universal)"
  scripts/package.sh macos
  dir="$(mktemp -d)"
  ditto -x -k "bin/release/hidane-$version-macos-universal.zip" "$dir"
  HIDANE_HOME="$(mktemp -d)" HIDANE_GUI_SMOKE=1 HIDANE_LOGIN_SHELL=0 "$dir/Hidane.app/Contents/MacOS/hidane" 2>&1 | grep "ui ready"
else
  echo "== macOS skipped (needs a Mac)"
fi

echo "== Windows (amd64, arm64)"
wails3_version="$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)"
gobin="$(go env GOBIN)"
wails3="${gobin:-$(go env GOPATH)/bin}/wails3"
if ! "$wails3" version 2>/dev/null | grep -q "$wails3_version"; then
  CGO_ENABLED=0 go install "github.com/wailsapp/wails/v3/cmd/wails3@$wails3_version"
fi
for arch in amd64 arm64; do WAILS3="$wails3" scripts/package.sh windows "$arch"; done

echo "== Linux (Docker, host architecture)"
if docker info >/dev/null 2>&1; then
  nfpm_version="$(sed -n 's/^  NFPM_VERSION: //p' .github/workflows/release.yml)"
  docker run --rm -v "$repo":/src:ro -v "$repo/bin/release":/out -v hidane-gomod:/go/pkg/mod \
    -e VERSION="$version" -e NFPM_VERSION="$nfpm_version" golang:1.26-trixie bash -c '
      set -e
      apt-get update -qq >/dev/null
      apt-get install -y -qq libgtk-4-dev libwebkitgtk-6.0-dev xvfb dbus-x11 >/dev/null 2>&1
      go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@$NFPM_VERSION" >/dev/null 2>&1
      cp -r /src /tmp/src && cd /tmp/src && rm -rf bin
      scripts/package.sh linux 2>/dev/null
      cp bin/release/* /out/
      dir="$(mktemp -d)" && tar -xzf bin/release/*.tar.gz -C "$dir"
      # A container forbids the user namespaces WebKit sandboxes itself with.
      HIDANE_HOME="$(mktemp -d)" HIDANE_GUI_SMOKE=1 HIDANE_LOGIN_SHELL=0 WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 \
        timeout 180 xvfb-run -a dbus-run-session -- "$dir/hidane/hidane" 2>&1 | grep "ui ready"'
else
  echo "Docker is not available: Linux packaging was not checked"
fi

echo "== artifacts"
ls -la bin/release
