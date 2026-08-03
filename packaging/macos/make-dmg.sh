#!/bin/bash
# Wraps a signed .app into a .dmg with a symlink to /Applications, so users
# get the familiar "drag to Applications" install flow. No custom background
# image or Finder icon layout — that requires driving Finder via AppleScript,
# which triggers a macOS Automation permission prompt.
#
# Usage: make-dmg.sh <app-path> <output-dmg> <volume-name>
set -euo pipefail

APP="$1"
OUTPUT_DMG="$2"
VOLNAME="$3"

STAGING="$(mktemp -d)/dmg-root"
mkdir -p "$STAGING"
cp -R "$APP" "$STAGING/"
ln -s /Applications "$STAGING/Applications"

rm -f "$OUTPUT_DMG"
hdiutil create -volname "$VOLNAME" -srcfolder "$STAGING" -ov -format UDZO "$OUTPUT_DMG"

echo "$OUTPUT_DMG"
