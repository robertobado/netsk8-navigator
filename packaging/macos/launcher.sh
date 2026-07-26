#!/bin/bash
# Contents/MacOS/launcher — the .app bundle's CFBundleExecutable.
#
# The backend has no GUI of its own, so double-clicking needs a bridge: this
# opens a visible Terminal window running the real binary (so logs are
# visible and closing the window is an obvious way to quit), then opens the
# browser once the backend responds. Uses a .command file + `open -a
# Terminal` rather than `osascript`/AppleScript, since telling Terminal what
# to do via Apple Events triggers a macOS Automation permission prompt on
# first launch — `open` on a .command file does not.
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

URL="http://127.0.0.1:8080"
for _ in $(seq 1 50); do
  curl -s -o /dev/null "$URL/api/health" && break
  sleep 0.2
done
open "$URL"
