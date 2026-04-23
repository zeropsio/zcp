#!/bin/bash
# Preseed for strategy-push-git-setup scenario.
#
# Fixture has already deployed `app` (php-nginx) + `db` (postgres) to real
# Zerops via buildFromGit. This preseed plants a COMPLETE ServiceMeta for
# `app` — adopted, push-dev confirmed, FirstDeployedAt set — so the agent
# starts from "everything adopted, push-dev strategy, first deploy landed"
# state and only needs to perform the strategy transition to push-git.
#
# This keeps the scenario focused: the point is the action="strategy"
# setup flow, not re-testing adopt+first-deploy (covered by other
# scenarios). Without the preseed the agent would spend the first 6+
# tool calls re-adopting the service before the strategy switch ever
# fires.
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
# CleanupProject preserves .zcp — wipe leftover from prior scenarios so
# we don't inherit phantom sessions or stale metas with different state.
rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work"
cat > "$STATE/registry.json" <<'JSON'
{"version":"1","sessions":[]}
JSON
rm -f "$STATE/session-registry.json"

# `app` meta — adopted, push-dev confirmed, first deploy landed.
cat > "$STATE/services/app.json" <<JSON
{
  "hostname": "app",
  "mode": "dev",
  "deployStrategy": "push-dev",
  "strategyConfirmed": true,
  "environment": "container",
  "bootstrapSession": "sess-completed-prev",
  "bootstrappedAt": "2026-04-20T08:00:00Z",
  "firstDeployedAt": "2026-04-20T08:30:00Z"
}
JSON

echo "preseed: planted adopted+deployed meta for app (push-dev confirmed, FirstDeployedAt set)"
