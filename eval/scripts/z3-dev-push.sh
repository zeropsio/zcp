#!/bin/bash
# z3 dev push — deliver dev builds to a zcp container by hand, no release.
#
# The z3 work (plans/z3-brief-2026-08-28.md §4a) ships nothing while it is built:
# the modified zcp binary and the forked z3 server go straight into the target
# container over VPN + ssh. A container restart re-runs the platform's
# install.sh and replaces the zcp binary with the latest RELEASE — push zcp
# again after a restart. The z3 prefix survives a plain restart; a redeploy
# replaces the container and loses the hand-pushed bundle.
#
# Usage:
#   ./eval/scripts/z3-dev-push.sh          # zcp (default)
#   ./eval/scripts/z3-dev-push.sh zcp      # build zcp → install → zcp init → nginx → restart z3 unit
#   ./eval/scripts/z3-dev-push.sh z3       # build fork → pack → install into the container prefix → restart z3 unit
#   ./eval/scripts/z3-dev-push.sh all      # both
#
# Environment:
#   EVAL_REMOTE_HOST  ssh host (default: zcp — resolves inside whichever project VPN is up)
#   Z3_REPO           fork checkout (default: ../z3 next to this repo)
#   Z3_PREFIX         npm prefix on the container that `zcp init z3` runs z3 from
#                     (default: /home/zerops/.zcp/z3 → $Z3_PREFIX/node_modules/.bin/z3)
#   Z3_UNIT           systemd unit `zcp init z3` creates (default: zerops@z3)
#   Z3_SKIP_WEB=1     reuse apps/web/dist instead of rebuilding the web client
#   Z3_BASE_PATH      public path prefix baked into the web bundle (e.g. /z3);
#                     empty = root-served (upstream default)
#   Z3_HOSTED_APP_CHANNEL  REMOVED — the container bundle no longer claims to be the hosted app

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
REMOTE_HOST="${EVAL_REMOTE_HOST:-zcp}"
Z3_REPO="${Z3_REPO:-$(cd "$PROJECT_DIR/.." && pwd)/z3}"
Z3_PREFIX="${Z3_PREFIX:-/home/zerops/.zcp/z3}"
Z3_UNIT="${Z3_UNIT:-zerops@z3}"
WHAT="${1:-zcp}"

# Container host keys rotate on every recreate — same policy as build-deploy.sh.
SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)
remote() { ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" "$@"; }

restart_z3_unit() {
  # The unit exists only once `zcp init z3` (stream S2) has created it.
  if remote "systemctl list-units --all --no-legend '$Z3_UNIT.service' 2>/dev/null | grep -q '$Z3_UNIT'"; then
    echo "==> Restarting $Z3_UNIT..."
    remote "sudo systemctl restart '$Z3_UNIT'"
  else
    echo "==> No $Z3_UNIT unit on the container yet (zcp init z3 not landed) — nothing to restart"
  fi
}

push_zcp() {
  echo "### zcp binary → $REMOTE_HOST"
  EVAL_REMOTE_HOST="$REMOTE_HOST" "$SCRIPT_DIR/build-deploy.sh"

  echo "==> Re-running zcp init (agents, mounts, z3 unit) and the nginx render..."
  remote "cd /var/www && zcp init"
  remote "sudo -E zcp init nginx && nginx -t >/dev/null 2>&1 && nginx -s reload"
  echo "==> nginx reloaded"

  restart_z3_unit

  # /healthz exists once S2 lands; until then this reads 404 and that is fine.
  local code
  code="$(remote "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/healthz" || true)"
  echo "==> /healthz on the container: HTTP $code"
}

push_z3() {
  echo "### z3 server (fork) → $REMOTE_HOST:$Z3_PREFIX"
  [ -d "$Z3_REPO/apps/server" ] || { echo "fork not found at $Z3_REPO (set Z3_REPO)"; exit 1; }

  local sha version tarball
  sha="$(git -C "$Z3_REPO" rev-parse --short HEAD)"
  # A unique prerelease version per build: npm reinstalls only when the version
  # moves, and `z3 --version` on the container then names the commit it runs.
  version="$(node -p "require('$Z3_REPO/apps/server/package.json').version")-dev.$sha"

  (
    cd "$Z3_REPO"
    # Upstream moves daily and brings new dependencies; a rebase without a
    # reinstall fails the web build on an unresolved import. Fast when current.
    echo "==> Syncing dependencies (vp install --frozen-lockfile)..."
    vp install --frozen-lockfile
    if [ "${Z3_SKIP_WEB:-0}" != "1" ]; then
      echo "==> Building web client (base path: ${Z3_BASE_PATH:-/})..."
      # No VITE_HOSTED_APP_CHANNEL: the container bundle is NOT the hosted app. It used to claim it
      # was, because otherwise the client booted in local-server mode and /z3/ redirected to /pair
      # (S7-3, 2026-08-29) — the lie also mislabelled the stage "Latest" and emptied the connection
      # source. W4-F6-DOOR removed the need: a requires-auth gate advertising zerops-identity is a
      # Zerops door, so /z3/ renders the Zerops sign-in, and the honest hosted-static answer (false)
      # is what lets the container register its own primary environment.
      VITE_BASE_PATH="${Z3_BASE_PATH:-}" vp run --filter @t3tools/web build
    fi
    echo "==> Building server bundle + client copy..."
    node apps/server/scripts/cli.ts build
    echo "==> Packing zerops-code@$version..."
    rm -rf builds/z3 && mkdir -p builds/z3
    node apps/server/scripts/cli.ts pack --out builds/z3 --app-version "$version"
  )
  tarball="$(ls "$Z3_REPO"/builds/z3/*.tgz | head -n 1)"
  echo "==> Tarball: $tarball ($(du -h "$tarball" | cut -f1))"

  echo "==> Uploading + installing into $Z3_PREFIX..."
  # The tarball stays next to the install: package.json records it as a
  # `file:` dependency, so a later plain `npm install` in the prefix still resolves.
  remote "mkdir -p '$Z3_PREFIX'"
  scp "${SSH_OPTS[@]}" "$tarball" "$REMOTE_HOST:$Z3_PREFIX/zerops-code-dev.tgz"
  remote "cd '$Z3_PREFIX' && { [ -f package.json ] || npm init -y >/dev/null; } \
    && npm install --no-audit --no-fund --loglevel=error ./zerops-code-dev.tgz"

  # `z3 --version` prints the version baked into the bundle, not the dev tag;
  # npm's view of the installed package is what names the commit.
  echo "==> Installed: $(remote "cd '$Z3_PREFIX' && npm ls zerops-code --depth=0 2>/dev/null | grep -o 'zerops-code@[^ ]*'") — $(remote "'$Z3_PREFIX/node_modules/.bin/z3' --version 2>&1 | tail -n 1")"

  restart_z3_unit
}

case "$WHAT" in
  zcp) push_zcp ;;
  z3) push_z3 ;;
  all) push_zcp; push_z3 ;;
  *) echo "usage: $0 [zcp|z3|all]"; exit 2 ;;
esac

echo "### done"
