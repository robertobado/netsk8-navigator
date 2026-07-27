#!/usr/bin/env bash
# The actual demo-redeploy steps, invoked by deploy-demo.sh after it has
# pulled this same commit — safe to edit freely, unlike that entrypoint.
set -euo pipefail

VERSION="${1:?usage: deploy-demo-run.sh <version, e.g. 0.0.3>}"
REPO_DIR=/opt/netsk8-demo/repo
BIN_DIR=/opt/netsk8-demo/bin
GOTMPDIR=/opt/netsk8-demo/gotmp

sudo -u netsk8 -H env GOTMPDIR="$GOTMPDIR" TMPDIR="$GOTMPDIR" PATH="/usr/local/go/bin:$PATH" \
  bash -c "cd '$REPO_DIR/demo/seed' && go build -o '$BIN_DIR/demo-seed' ."

asset="netsk8-navigator_${VERSION}_linux_amd64.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/release.tar.gz" \
  "https://github.com/robertobado/netsk8-navigator/releases/download/v${VERSION}/${asset}"
tar -xzf "$tmp/release.tar.gz" -C "$tmp" netsk8-navigator
install -o netsk8 -g netsk8 -m 0755 "$tmp/netsk8-navigator" "$BIN_DIR/netsk8-navigator"
setcap cap_net_bind_service=+ep "$BIN_DIR/netsk8-navigator"

systemctl restart netsk8-demo-seed
systemctl restart netsk8-navigator

sleep 2
systemctl is-active netsk8-demo-cluster netsk8-demo-seed netsk8-navigator
curl -fsS http://localhost/api/health
