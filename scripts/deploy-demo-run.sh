#!/usr/bin/env bash
# The actual demo-redeploy steps, invoked by deploy-demo.sh after it has
# pulled this same commit — safe to edit freely, unlike that entrypoint.
set -euo pipefail

VERSION="${1:?usage: deploy-demo-run.sh <version, e.g. 0.0.3>}"
BIN_DIR=/opt/netsk8-demo/bin

# Deliberately does NOT rebuild demo/seed here: `go build` on kwok's
# dependency tree is heavy enough to swap-thrash this VPS's 956Mi of RAM
# into unresponsiveness (observed firsthand — it once ran long enough to
# push /api/health latency past the front proxy's timeout, taking the live
# site down with 502s). demo/seed changes rarely; rebuild it by hand
# (see docs/DEMO_CLUSTER.md) when its source actually changes, not on
# every release.
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

systemctl is-active netsk8-demo-cluster netsk8-demo-seed netsk8-navigator

# This VPS is resource-constrained enough that startup latency varies a lot
# under load (observed anywhere from ~1s to ~11s) — poll instead of a fixed
# sleep, or a deploy fails the job despite the service coming up moments
# later.
for i in $(seq 1 30); do
  curl -fsS http://localhost/api/health && exit 0
  sleep 1
done
echo "netsk8-navigator did not become healthy within 30s" >&2
exit 1
