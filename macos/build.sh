#!/usr/bin/env bash
# Build Nomad.app: compiles the Go CLI and the SwiftUI app, embeds the
# CLI as Contents/Resources/nomad and ad-hoc signs the bundle.
#
#   macos/build.sh            # -> macos/build/Nomad.app
#   macos/build.sh --open     # build, then launch
set -euo pipefail
cd "$(dirname "$0")"

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
APP="build/Nomad.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

(cd .. && go build -trimpath -ldflags "-s -w -X github.com/chyroc/nomad/internal/cli.Version=${VERSION}" \
  -o "macos/$APP/Contents/Resources/nomad" ./cmd/nomad)

swift build -c release --product Nomad
cp "$(swift build -c release --show-bin-path)/Nomad" "$APP/Contents/MacOS/Nomad"
strip -x "$APP/Contents/MacOS/Nomad"

ICONSET="build/Nomad.iconset"
rm -rf "$ICONSET"
mkdir -p "$ICONSET"
"$APP/Contents/MacOS/Nomad" --render-icon "$ICONSET/icon_512x512@2x.png" >/dev/null
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$ICONSET/icon_512x512@2x.png" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" "$ICONSET/icon_512x512@2x.png" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"
rm -rf "$ICONSET"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key><string>Nomad</string>
  <key>CFBundleIdentifier</key><string>com.github.chyroc.nomad</string>
  <key>CFBundleName</key><string>Nomad</string>
  <key>CFBundleDisplayName</key><string>Nomad</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleShortVersionString</key><string>${VERSION#v}</string>
  <key>CFBundleVersion</key><string>${VERSION#v}</string>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleLocalizations</key><array><string>en</string><string>zh-Hans</string></array>
  <key>LSMinimumSystemVersion</key><string>14.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

codesign --force --deep --sign - "$APP" >/dev/null
echo "built $(pwd)/$APP"
if [[ "${1:-}" == "--open" ]]; then
  open "$APP"
fi
