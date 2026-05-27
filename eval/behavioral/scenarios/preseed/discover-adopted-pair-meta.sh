#!/bin/bash
# Preseed for discover-adoption-state-adopted-no-rebootstrap.
#
# Fixture deploys appdev/appstage/db (standard pair + managed dep).
# This preseed plants COMPLETE pair-keyed ServiceMeta for appdev (with
# stageHostname=appstage) so the behavioral run starts from a fully-
# adopted state. Agent should read this via zerops_discover and see
# adoptionState="adopted" for both pair halves + adoptionState=
# "managed-dep" for db, never re-bootstrapping.
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work"
cat > "$STATE/registry.json" <<'JSON'
{"version":"1","sessions":[]}
JSON
rm -f "$STATE/session-registry.json"

cat > "$STATE/services/appdev.json" <<JSON
{
  "hostname": "appdev",
  "mode": "standard",
  "stageHostname": "appstage",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "gitPushState": "unconfigured",
  "buildIntegration": "none",
  "environment": "container",
  "bootstrapSession": "sess-completed-adopt-pair",
  "bootstrappedAt": "2026-05-20T08:00:00Z",
  "firstDeployedAt": "2026-05-20T08:30:00Z",
  "primarySetupName": "dev",
  "stageSetupName": "prod"
}
JSON

echo "preseed: planted complete pair-keyed ServiceMeta appdev/appstage (mode=standard, bootstrappedAt set → adoptionState=adopted on both halves)"
