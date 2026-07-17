#!/usr/bin/env bash
# dc-live-remote.sh runs the Data Console live conformance suite ON the zcp
# eval container over SSH: builds the e2e-tagged conformance test binary +
# the dclive config generator from the CURRENT revision, ships both to a
# private remote temp dir, generates DC_LIVE_CONFIG in place there (REST env
# API, in-project DNS — no VPN needed), runs the suite remotely, and always
# retrieves the JSON ledger artifact (pass or fail). Invoked via
# `make dc-live-remote` (docs/spec-dataconsole-testing.md §6).
#
# Deliberately does NOT disable SSH host key checking (unlike the e2e-zcp*
# Makefile targets) — it relies on the operator's own known_hosts.
set -euo pipefail

DC_REMOTE_HOST="${DC_REMOTE_HOST:-zcp}"
DC_LIVE_PROFILE="${DC_LIVE_PROFILE:-full}"
DC_LIVE_MANIFEST="${DC_LIVE_MANIFEST:?DC_LIVE_MANIFEST required, e.g. db=postgresql,cache=valkey,storage=object-storage}"
DC_LIVE_REVISION="${DC_LIVE_REVISION:-$(git rev-parse HEAD)}"

LOCAL_TMPDIR="$(mktemp -d)"
REMOTE_DIR=""

cleanup() {
  if [ -n "$REMOTE_DIR" ]; then
    ssh "$DC_REMOTE_HOST" "rm -rf -- '$REMOTE_DIR'" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$LOCAL_TMPDIR"
}
trap cleanup EXIT

echo "==> Building conformance test binary + dclive (linux/amd64) at $DC_LIVE_REVISION..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -tags e2e -o "$LOCAL_TMPDIR/conformance.test" ./internal/dataconsole/console/provider/conformance/
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$LOCAL_TMPDIR/dclive" ./cmd/dclive

echo "==> Preflighting $DC_REMOTE_HOST..."
remote_uname="$(ssh "$DC_REMOTE_HOST" 'uname -sm')"
if [ "$remote_uname" != "Linux x86_64" ]; then
  echo "ERROR: $DC_REMOTE_HOST is not Linux x86_64 (got: $remote_uname)" >&2
  exit 1
fi

echo "==> Creating private remote workdir..."
REMOTE_DIR="$(ssh "$DC_REMOTE_HOST" 'umask 077 && mktemp -d /tmp/dclive.XXXXXX')"

echo "==> Shipping binaries to $DC_REMOTE_HOST:$REMOTE_DIR..."
scp -q "$LOCAL_TMPDIR/conformance.test" "$LOCAL_TMPDIR/dclive" "$DC_REMOTE_HOST:$REMOTE_DIR/"
ssh "$DC_REMOTE_HOST" "chmod 0700 '$REMOTE_DIR' && chmod +x '$REMOTE_DIR/conformance.test' '$REMOTE_DIR/dclive'"

echo "==> Generating DC_LIVE_CONFIG remotely..."
ssh "$DC_REMOTE_HOST" "cd '$REMOTE_DIR' && ./dclive gen-config --out cfg.json"

echo "==> Running live conformance remotely (profile=$DC_LIVE_PROFILE, manifest=$DC_LIVE_MANIFEST)..."
set +e
ssh "$DC_REMOTE_HOST" "cd '$REMOTE_DIR' && DC_LIVE_CONFIG=cfg.json DC_LIVE_PROFILE='$DC_LIVE_PROFILE' DC_LIVE_MANIFEST='$DC_LIVE_MANIFEST' DC_LIVE_REVISION='$DC_LIVE_REVISION' DC_LIVE_SUMMARY=summary.json ./conformance.test -test.v -test.count=1"
TEST_EXIT=$?
set -e

echo "==> Retrieving summary (run outcome incl. below)..."
LOCAL_SUMMARY="dc-live-summary-remote-$(date -u +%Y%m%dT%H%M%SZ).json"
if scp -q "$DC_REMOTE_HOST:$REMOTE_DIR/summary.json" "$LOCAL_SUMMARY"; then
  echo "Summary saved to: $LOCAL_SUMMARY"
else
  echo "WARNING: could not retrieve summary.json from $DC_REMOTE_HOST:$REMOTE_DIR" >&2
fi

exit "$TEST_EXIT"
