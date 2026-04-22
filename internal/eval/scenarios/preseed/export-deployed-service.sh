#!/bin/bash
# Preseed for export-deployed-service scenario.
#
# Fixture already deployed `app` (php-nginx) + `db` (postgres) via
# buildFromGit. This preseed plants a COMPLETE ServiceMeta for `app` —
# adopted, push-dev confirmed, FirstDeployedAt set — so the router
# offers the export workflow (offered whenever any service has
# FirstDeployedAt per 09ae4df "export(workflow): rewrite as single-atom
# task checklist + widen router offer").
#
# Without FirstDeployedAt the export offering is suppressed and the
# agent has no envelope cue pointing it at workflow="export".
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
# CleanupProject preserves .zcp — wipe leftover state from prior runs.
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
  "deployStrategy": "push-dev",
  "strategyConfirmed": true,
  "environment": "container",
  "bootstrapSession": "sess-completed-prev",
  "bootstrappedAt": "2026-04-20T08:00:00Z",
  "firstDeployedAt": "2026-04-20T08:30:00Z"
}
JSON

echo "preseed: planted adopted+deployed meta for app (export router now offers workflow=export)"
