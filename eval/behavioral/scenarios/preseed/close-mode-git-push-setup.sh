#!/bin/bash
# Preseed for delivery-git-push-actions-setup.
#
# The fixture has already deployed app + db via buildFromGit. Plant complete
# ServiceMeta for app so the behavioral run starts from a real "working app,
# direct deploy confirmed" state and focuses only on delivery setup.
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
  "mode": "simple",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "gitPushState": "unconfigured",
  "buildIntegration": "none",
  "environment": "container",
  "bootstrapSession": "sess-completed-delivery",
  "bootstrappedAt": "2026-04-20T08:00:00Z",
  "firstDeployedAt": "2026-04-20T08:30:00Z"
}
JSON

echo "preseed: planted deployed simple-mode app meta with closeMode=auto and gitPushState=unconfigured"
