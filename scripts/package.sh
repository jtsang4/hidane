#!/usr/bin/env bash
# Builds the desktop app for one platform and packs it for a release, into
# bin/release/. The release workflow (.github/workflows/release.yml) runs this
# on each platform's runner; the hidane-release skill runs it locally as a dry
# run. Expects the frontend to be built already (make frontend).
#
#   scripts/package.sh macos            universal .app → .dmg + .zip (run on macOS)
#   scripts/package.sh windows <arch>   amd64 | arm64 → .zip (cross-compiles; no cgo)
#   scripts/package.sh linux            host arch → .tar.gz + .deb (needs GTK4/WebKitGTK 6.0 dev packages)
#
# VERSION (e.g. v1.2.3) names the artifacts and is stamped into the binary.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"
version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
# Bundle and package metadata want a bare number: v1.2.3-rc.1 → 1.2.3-rc.1.
number="${version#v}"
out="$repo/bin/release"
work="$repo/bin/package"
ldflags="-s -w -X github.com/jtsang4/hidane/internal/app.Version=$version"
mkdir -p "$out"

if [[ ! -f frontend/dist/index.html ]]; then
  echo "frontend/dist is missing: run 'make frontend' first" >&2
  exit 1
fi

macos() {
  export CGO_CFLAGS="-mmacosx-version-min=12.0" CGO_LDFLAGS="-mmacosx-version-min=12.0"
  rm -rf "$work/macos" && mkdir -p "$work/macos"
  for arch in arm64 amd64; do
    CGO_ENABLED=1 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags="$ldflags" -o "$work/macos/hidane-$arch" .
  done
  app="$work/macos/Hidane.app"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  lipo -create -output "$app/Contents/MacOS/hidane" "$work/macos/hidane-arm64" "$work/macos/hidane-amd64"
  # Bundle versions must be numeric (1.2.3); the full version is in the binary.
  local bundle
  bundle="$(echo "$number" | sed -E 's/^([0-9]+\.[0-9]+\.[0-9]+).*/\1/')"
  [[ "$bundle" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || bundle="0.0.0"
  sed "s/@VERSION@/$bundle/g" build/darwin/Info.plist > "$app/Contents/Info.plist"
  cp build/darwin/icons.icns "$app/Contents/Resources/icons.icns"

  if [[ -n "${MACOS_SIGN_IDENTITY:-}" ]]; then
    # Developer ID + hardened runtime: what notarization requires.
    codesign --force --options runtime --timestamp --sign "$MACOS_SIGN_IDENTITY" "$app"
  else
    # Apple Silicon refuses to run unsigned code; an ad-hoc signature is the
    # minimum. Gatekeeper still warns on first open (see the release notes).
    codesign --force --sign - "$app"
  fi
  codesign --verify --strict "$app"

  local zip="$out/hidane-$version-macos-universal.zip"
  rm -f "$zip"
  ditto -c -k --keepParent "$app" "$zip"
  if [[ -n "${MACOS_SIGN_IDENTITY:-}" && -n "${APPLE_ID:-}" ]]; then
    xcrun notarytool submit "$zip" --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_PASSWORD" --wait
    xcrun stapler staple "$app"
    rm -f "$zip" && ditto -c -k --keepParent "$app" "$zip"
  fi

  local stage="$work/macos/dmg"
  mkdir -p "$stage" && cp -R "$app" "$stage/" && ln -sf /Applications "$stage/Applications"
  local dmg="$out/hidane-$version-macos-universal.dmg"
  rm -f "$dmg"
  hdiutil create -volname "Hidane" -srcfolder "$stage" -ov -format UDZO "$dmg" >/dev/null
  if [[ -n "${MACOS_SIGN_IDENTITY:-}" ]]; then
    codesign --force --timestamp --sign "$MACOS_SIGN_IDENTITY" "$dmg"
  fi
  echo "$zip"
  echo "$dmg"
}

windows() {
  local arch="${1:-amd64}"
  local dir="$work/windows-$arch"
  rm -rf "$dir" && mkdir -p "$dir/hidane"
  # Icon, version info and the manifest (per-monitor DPI, common controls)
  # go into the exe as a .syso the Go linker picks up from the package dir.
  local quad
  quad="$(echo "$number" | sed -E 's/^([0-9]+)\.([0-9]+)\.([0-9]+).*/\1.\2.\3.0/')"
  [[ "$quad" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.0$ ]] || quad="0.0.0.0"
  sed "s/@VERSION@/$quad/g" build/windows/wails.exe.manifest > "$dir/app.manifest"
  sed -e "s/@VERSION@/$quad/g" -e "s/@PRODUCT_VERSION@/$number/g" build/windows/info.json > "$dir/info.json"
  local syso="$repo/rsrc_windows_$arch.syso"
  trap 'rm -f "$syso"' RETURN
  "${WAILS3:-wails3}" generate syso -arch "$arch" -icon build/windows/icon.ico \
    -manifest "$dir/app.manifest" -info "$dir/info.json" -out "$syso"
  # The GUI exe has no console window; a console build of the same commands
  # (chat, events, guard, …) is shipped next to it for terminals.
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags="$ldflags -H windowsgui" -o "$dir/hidane/hidane.exe" .
  rm -f "$syso"
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -tags nogui -trimpath -ldflags="$ldflags" -o "$dir/hidane/hidane-cli.exe" .
  local zip="$out/hidane-$version-windows-$arch.zip"
  rm -f "$zip"
  (cd "$dir" && zip -qr "$zip" hidane)
  echo "$zip"
}

linux() {
  local arch
  arch="$(go env GOARCH)"
  local dir="$work/linux-$arch"
  rm -rf "$dir" && mkdir -p "$dir/hidane"
  CGO_ENABLED=1 go build -trimpath -ldflags="$ldflags" -o "$dir/hidane/hidane" .
  cp build/linux/hidane.desktop "$dir/hidane/"
  cp build/appicon.png "$dir/hidane/hidane.png"
  local tgz="$out/hidane-$version-linux-$arch.tar.gz"
  tar -C "$dir" -czf "$tgz" hidane
  echo "$tgz"

  # Debian semver: a prerelease sorts before its release with "~".
  local deb_version="${number/-/\~}"
  mkdir -p "$work/linux-stage" && cp "$dir/hidane/hidane" "$work/linux-stage/hidane"
  VERSION_DEB="$deb_version" ARCH="$arch" \
    "${NFPM:-nfpm}" package --config build/linux/nfpm.yaml --packager deb --target "$out/hidane-$version-linux-$arch.deb" >/dev/null
  echo "$out/hidane-$version-linux-$arch.deb"
}

case "${1:-}" in
  macos) macos ;;
  windows) windows "${2:-amd64}" ;;
  linux) linux ;;
  *) echo "usage: scripts/package.sh macos | windows <amd64|arm64> | linux" >&2; exit 2 ;;
esac
