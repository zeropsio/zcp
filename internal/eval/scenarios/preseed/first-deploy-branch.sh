#!/bin/bash
# Preseed for develop-first-deploy-branch scenario.
#
# Simulates bootstrap-complete state where infra is provisioned but no code
# has been deployed. ServiceMeta is COMPLETE (BootstrappedAt set, strategy
# confirmed) but FirstDeployedAt is empty — this is the signal that fires
# the develop first-deploy branch atoms (deployStates: [never-deployed]).
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
# CleanupProject preserves .zcp by design — wipe leftover state from the
# previous scenario so we don't inherit phantom sessions / metas.
rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work"
# Reset registry too — leftover dead-PID entries would be auto-claimed by
# NewEngine and poison the discovery envelope.
cat > "$STATE/registry.json" <<'JSON'
{"version":"1","sessions":[]}
JSON
rm -f "$STATE/session-registry.json"

# Complete meta, close-mode confirmed, never deployed.
cat > "$STATE/services/appdev.json" <<JSON
{
  "hostname": "appdev",
  "mode": "dev",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "bootstrapSession": "sess-completed-01",
  "bootstrappedAt": "2026-04-20T08:00:00Z"
}
JSON

echo "preseed: planted bootstrapped-never-deployed meta for appdev (FirstDeployedAt empty)"
