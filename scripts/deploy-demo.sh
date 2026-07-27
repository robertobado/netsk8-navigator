#!/usr/bin/env bash
# Forced-command entrypoint for the CI deploy SSH key (see
# /root/.ssh/authorized_keys on the demo VPS and .github/workflows/release.yml's
# deploy-demo job) — the ONLY thing that key can run. Deliberately kept tiny
# and stable: it pulls the latest commit and re-execs the real deploy logic
# from the freshly-checked-out file, so a script that rewrites itself mid-run
# (via git reset --hard) never has bash reading stale/partial bytes of its
# own body.
set -euo pipefail

REPO_DIR=/opt/netsk8-demo/repo
cd "$REPO_DIR"
git fetch --tags origin main
git checkout main
git reset --hard origin/main

exec bash "$REPO_DIR/scripts/deploy-demo-run.sh" "$@"
