#!/usr/bin/env bash
# break-prod-setup.sh — preseed for scenario launch-failure-build-stuck.
#
# After the fixture has imported the source pair (appdev + appstage + db)
# and the runtimes have reached ACTIVE with their buildFromGit-mounted
# source code at /var/www, this script SSHes into appdev and rewrites
# the `setup: prod` block's `build.base` to a nonexistent runtime tag.
#
# The subsequent agent run executes launch-production, which:
#   1. readSourceState reads the (now-broken) /var/www/zerops.yaml from appdev
#   2. Source-control gate passes (hasSetupProd matches "prod" — block is
#      still THERE, just broken)
#   3. Bundle composes, ProjectAdminClient.CreateAndImportProject succeeds
#   4. First-deploy poll catches FAILED build (bogus base)
#   5. launchFirstDeployFailedResponse fires with the S2.2.2 retry-via-push
#      guidance
#
# This script is operator-side staging; the breakage is reverted on the
# NEXT clean seed (the buildFromGit re-clone pulls the upstream yaml).
#
# Env vars provided by the runner:
#   ZCP_SCENARIO_ID   — scenario name (informational)
#   ZCP_SUITE_ID      — suite name (informational)
#   ZCP_WORK_DIR      — /var/www on the zcp container
set -euo pipefail

SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)

log() { printf '[preseed:%s] %s\n' "${ZCP_SCENARIO_ID:-?}" "$*" >&2; }

# Wait for appdev's /var/www/zerops.yaml to exist (buildFromGit clone +
# initial deploy land asynchronously). Cap at 5 min — fixture wait_active
# already gates this, so the file should be present immediately, but the
# loop is cheap insurance against race.
log "waiting for /var/www/zerops.yaml on appdev"
for i in $(seq 1 60); do
  if ssh "${SSH_OPTS[@]}" appdev "test -f /var/www/zerops.yaml" 2>/dev/null; then
    break
  fi
  if [[ "$i" -eq 60 ]]; then
    echo "FATAL: /var/www/zerops.yaml never materialized on appdev after 5min" >&2
    exit 1
  fi
  sleep 5
done

# Snapshot the current setup: prod block for diagnostic.
log "current setup: prod block on appdev (pre-break):"
ssh "${SSH_OPTS[@]}" appdev "grep -A 10 'setup: prod' /var/www/zerops.yaml || true" >&2

# Rewrite build.base inside the setup: prod block. Anchor on the prod
# block to avoid touching dev's base. Strategy:
#   1. Find the line range of `- setup: prod` block (from its header to
#      either the next `- setup:` or EOF).
#   2. Within that range, replace `base: nodejs@<anything>` with bogus.
#
# nodejs-hello-world-app's prod block uses `nodejs@22` as build.base.
log "rewriting setup: prod build.base → nodejs@99-deliberately-bogus"
ssh "${SSH_OPTS[@]}" appdev '
  python3 -c "
import re, sys
p = \"/var/www/zerops.yaml\"
with open(p) as f:
    body = f.read()
# Match the setup: prod block (greedy until next top-level - setup: or EOF).
m = re.search(r\"(^\\s*- setup:\\s*prod\\b.*?)(?=^\\s*- setup:|\\Z)\", body, re.S | re.M)
if not m:
    print(\"FATAL: setup: prod block not found in zerops.yaml\", file=sys.stderr)
    sys.exit(2)
block = m.group(1)
new_block = re.sub(r\"base:\\s*nodejs@22\", \"base: nodejs@99-deliberately-bogus\", block, count=1)
if block == new_block:
    print(\"FATAL: build.base substitution did not match in setup: prod block\", file=sys.stderr)
    sys.exit(3)
body = body[:m.start(1)] + new_block + body[m.end(1):]
with open(p, \"w\") as f:
    f.write(body)
print(\"OK: rewrote build.base in setup: prod\", file=sys.stderr)
"
'

# Verify the edit landed.
log "post-break setup: prod block:"
ssh "${SSH_OPTS[@]}" appdev "grep -A 10 'setup: prod' /var/www/zerops.yaml" >&2

if ! ssh "${SSH_OPTS[@]}" appdev "grep -q 'nodejs@99-deliberately-bogus' /var/www/zerops.yaml"; then
  echo "FATAL: post-break verification failed — sentinel string not in /var/www/zerops.yaml" >&2
  exit 4
fi

log "DONE: source pair zerops.yaml staged with broken setup: prod"
