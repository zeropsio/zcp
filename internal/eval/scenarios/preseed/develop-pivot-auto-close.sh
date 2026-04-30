#!/bin/bash
# Preseed for develop-pivot-auto-close scenario.
#
# Fixture has already deployed `app` (php-nginx) + `db` (postgres) via
# buildFromGit. This preseed plants a COMPLETE ServiceMeta for `app` —
# adopted, push-dev confirmed, FirstDeployedAt set — so develop can fire
# immediately without a re-adopt detour.
#
# The scenario tests cb63bf3's "1 task = 1 develop session" invariant
# end-to-end: the agent must start TWO develop sessions with different
# `intent` values, driven only by the natural auto-close (deploy+verify
# scope coverage) between them — no explicit `action="close"` calls, no
# WORKFLOW_ACTIVE errors.
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work"
cat > "$STATE/registry.json" <<'JSON'
{"version":"1","sessions":[]}
JSON
rm -f "$STATE/session-registry.json"

cat > "$STATE/services/app.json" <<JSON
{
  "hostname": "app",
  "mode": "dev",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "bootstrapSession": "sess-completed-prev",
  "bootstrappedAt": "2026-04-20T08:00:00Z",
  "firstDeployedAt": "2026-04-20T08:30:00Z"
}
JSON

echo "preseed: planted adopted+deployed meta for app (ready for two sequential develop sessions)"
