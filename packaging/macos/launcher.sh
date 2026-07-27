#!/bin/bash
# Contents/MacOS/launcher — the .app bundle's CFBundleExecutable.
#
# The backend has no GUI of its own, so double-clicking needs a bridge: this
# opens a visible Terminal window running the real binary (so logs are
# visible and closing the window is an obvious way to quit). The binary
# itself opens the browser once it's ready to serve (see backend/browser.go
# — shouldOpenBrowser is on by default for any non-"dev" build, which this
# one is). Uses a .command file + `open -a Terminal` rather than
# `osascript`/AppleScript, since telling Terminal what to do via Apple
# Events triggers a macOS Automation permission prompt on first launch —
# `open` on a .command file does not.
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="$DIR/../Resources/netsk8-navigator-bin"
RUNNER="$(mktemp -t netsk8-navigator-run).command"

cat >"$RUNNER" <<EOF
#!/bin/bash
exec "$BIN"
EOF
chmod +x "$RUNNER"
open -a Terminal "$RUNNER"
