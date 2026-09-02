#!/bin/bash
# mate dev push — deliver dev builds to a zcp container by hand, no release.
#
# Unreleased mate work ships nothing while it is built:
# the modified zcp binary and the forked mate server go straight into the target
# container over VPN + ssh. A container restart re-runs the platform's
# install.sh and replaces the zcp binary with the latest RELEASE — push zcp
# again after a restart. The mate prefix survives a plain restart; a redeploy
# replaces the container and loses the hand-pushed bundle.
#
# Usage:
#   ./eval/scripts/mate-dev-push.sh          # zcp (default)
#   ./eval/scripts/mate-dev-push.sh zcp      # build zcp → install → zcp init → nginx → restart mate unit
#   ./eval/scripts/mate-dev-push.sh mate     # build fork → pack → install into the container prefix → restart mate unit
#   ./eval/scripts/mate-dev-push.sh all      # both
#
# Environment:
#   EVAL_REMOTE_HOST  ssh host (default: zcp — resolves inside whichever project VPN is up)
#   MATE_REPO           fork checkout (default: ../z3 next to this repo)
#   MATE_PREFIX         npm prefix on the container mate lives under. Each version
#                     gets its own dir under $MATE_PREFIX/versions/; a dev push
#                     lands at versions/dev and repoints $MATE_PREFIX/current at
#                     it — the same versioned layout mate.EnsureInstalled uses
#                     (default: /home/zerops/.zcp/mate →
#                     $MATE_PREFIX/current/node_modules/.bin/mate)
#   MATE_UNIT           systemd unit `zcp init mate` creates (default: zerops@mate)
#   MATE_SKIP_WEB=1     reuse apps/web/dist instead of rebuilding the web client
#   MATE_BASE_PATH      public path prefix baked into the web bundle (e.g. /mate);
#                     empty = root-served (upstream default)
#   MATE_HOSTED_APP_CHANNEL  REMOVED — the container bundle no longer claims to be the hosted app

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
REMOTE_HOST="${EVAL_REMOTE_HOST:-zcp}"
MATE_REPO="${MATE_REPO:-$(cd "$PROJECT_DIR/.." && pwd)/z3}"
MATE_PREFIX="${MATE_PREFIX:-/home/zerops/.zcp/mate}"
MATE_UNIT="${MATE_UNIT:-zerops@mate}"
WHAT="${1:-zcp}"

# Container host keys rotate on every recreate — same policy as build-deploy.sh.
SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)
remote() { ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" "$@"; }

restart_mate_unit() {
  # The unit exists only once `zcp init mate` (stream S2) has created it.
  if remote "systemctl list-units --all --no-legend '$MATE_UNIT.service' 2>/dev/null | grep -q '$MATE_UNIT'"; then
    echo "==> Restarting $MATE_UNIT..."
    remote "sudo systemctl restart '$MATE_UNIT'"
  else
    echo "==> No $MATE_UNIT unit on the container yet (zcp init mate not landed) — nothing to restart"
  fi
}

push_zcp() {
  echo "### zcp binary → $REMOTE_HOST"
  EVAL_REMOTE_HOST="$REMOTE_HOST" "$SCRIPT_DIR/build-deploy.sh"

  echo "==> Re-running zcp init (agents, mounts, mate unit) and the nginx render..."
  remote "cd /var/www && zcp init"
  remote "sudo -E zcp init nginx && nginx -t >/dev/null 2>&1 && nginx -s reload"
  echo "==> nginx reloaded"

  restart_mate_unit

  # /mate/healthz answers as soon as nginx is up (readiness marker, no proxy).
  local code
  code="$(remote "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/mate/healthz" || true)"
  echo "==> /mate/healthz on the container: HTTP $code"
}

push_mate() {
  echo "### mate server (fork) → $REMOTE_HOST:$MATE_PREFIX"
  [ -d "$MATE_REPO/apps/server" ] || { echo "fork not found at $MATE_REPO (set MATE_REPO)"; exit 1; }

  local sha version tarball
  sha="$(git -C "$MATE_REPO" rev-parse --short HEAD)"
  # A unique prerelease version per build: npm reinstalls only when the version
  # moves, and `mate --version` on the container then names the commit it runs.
  version="$(node -p "require('$MATE_REPO/apps/server/package.json').version")-dev.$sha"

  (
    cd "$MATE_REPO"
    # Upstream moves daily and brings new dependencies; a rebase without a
    # reinstall fails the web build on an unresolved import. Fast when current.
    echo "==> Syncing dependencies (vp install --frozen-lockfile)..."
    vp install --frozen-lockfile
    if [ "${MATE_SKIP_WEB:-0}" != "1" ]; then
      echo "==> Building web client (base path: ${MATE_BASE_PATH:-/})..."
      # No VITE_HOSTED_APP_CHANNEL: the container bundle is NOT the hosted app. It used to claim it
      # was, because otherwise the client booted in local-server mode and /mate/ redirected to /pair
      # (S7-3, 2026-08-29) — the lie also mislabelled the stage "Latest" and emptied the connection
      # source. W4-F6-DOOR removed the need: a requires-auth gate advertising zerops-identity is a
      # Zerops door, so /mate/ renders the Zerops sign-in, and the honest hosted-static answer (false)
      # is what lets the container register its own primary environment.
      VITE_BASE_PATH="${MATE_BASE_PATH:-}" vp run --filter @t3tools/web build
    fi
    echo "==> Building server bundle + client copy..."
    node apps/server/scripts/cli.ts build
    echo "==> Packing zerops-mate@$version..."
    rm -rf builds/mate && mkdir -p builds/mate
    node apps/server/scripts/cli.ts pack --out builds/mate --app-version "$version"
  )
  tarball="$(ls "$MATE_REPO"/builds/mate/*.tgz | head -n 1)"
  echo "==> Tarball: $tarball ($(du -h "$tarball" | cut -f1))"

  local version_dir="$MATE_PREFIX/versions/dev"
  echo "==> Uploading + installing into $version_dir..."
  # The tarball stays next to the install: package.json records it as a
  # `file:` dependency, so a later plain `npm install` in that version dir
  # still resolves.
  remote "mkdir -p '$version_dir'"
  scp "${SSH_OPTS[@]}" "$tarball" "$REMOTE_HOST:$version_dir/zerops-mate-dev.tgz"
  remote "cd '$version_dir' && { [ -f package.json ] || npm init -y >/dev/null; } \
    && npm install --no-audit --no-fund --loglevel=error ./zerops-mate-dev.tgz"

  # Activate: repoint $MATE_PREFIX/current at versions/dev, the same relative
  # symlink mate.EnsureInstalled's own activation produces. `zcp mate update` (or
  # the next `zcp init`) sees this dev build through the normal path and, with
  # no --force, keeps it — the "-dev." tag is what mate.IsDevVersion reads.
  remote "cd '$MATE_PREFIX' && ln -sfn versions/dev current"

  # `mate --version` prints the version baked into the bundle, not the dev tag;
  # npm's view of the installed package is what names the commit.
  echo "==> Installed: $(remote "cd '$version_dir' && npm ls zerops-mate --depth=0 2>/dev/null | grep -o 'zerops-mate@[^ ]*'") — $(remote "'$MATE_PREFIX/current/node_modules/.bin/mate' --version 2>&1 | tail -n 1")"

  restart_mate_unit
}

case "$WHAT" in
  zcp) push_zcp ;;
  mate) push_mate ;;
  all) push_zcp; push_mate ;;
  *) echo "usage: $0 [zcp|mate|all]"; exit 2 ;;
esac

echo "### done"
