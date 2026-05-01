#!/bin/bash
# Preseed for close-mode-git-push-setup scenario.
#
# Fixture has already deployed `app` (php-nginx) + `db` (postgres) to real
# Zerops via buildFromGit. This preseed plants a COMPLETE ServiceMeta for
# `app` — adopted, closeDeployMode=auto confirmed, FirstDeployedAt set —
# so the agent starts from "everything adopted, close-mode=auto, first
# deploy landed" state and only needs to perform the per-axis transition
# to closeDeployMode=git-push + buildIntegration=actions.
#
# This keeps the scenario focused: the point is the per-axis setup flow
# (close-mode → git-push-setup → build-integration), not re-testing
# adopt+first-deploy (covered by other scenarios). Without the preseed
# the agent would spend the first 6+ tool calls re-adopting the service
# before the close-mode transition ever fires.
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

# `app` meta — adopted, simple mode, closeDeployMode=auto confirmed, first
# deploy landed. git-push is only valid for push-source modes (standard/simple/
# local-stage/local-only); a standalone dev-mode service is rejected by the
# close-mode handler before setup.
cat > "$STATE/services/app.json" <<JSON
{
  "hostname": "app",
  "mode": "simple",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "gitPushState": "unconfigured",
  "buildIntegration": "none",
  "environment": "container",
  "bootstrapSession": "sess-completed-prev",
  "bootstrappedAt": "2026-04-20T08:00:00Z",
  "firstDeployedAt": "2026-04-20T08:30:00Z"
}
JSON

echo "preseed: planted adopted+deployed simple-mode meta for app (closeDeployMode=auto confirmed, FirstDeployedAt set)"
