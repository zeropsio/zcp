#!/bin/bash
# Preseed for develop-first-deploy-branch scenario.
#
# Simulates bootstrap-complete state where infra is provisioned but no code
# has been deployed. ServiceMeta is COMPLETE (BootstrappedAt set, strategy
# confirmed) but FirstDeployedAt is empty — this is the signal that fires
# the develop first-deploy branch atoms (deployStates: [never-deployed]).
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
mkdir -p "$STATE/services"

# Complete meta, strategy confirmed, never deployed.
cat > "$STATE/services/appdev.json" <<JSON
{
  "hostname": "appdev",
  "mode": "dev",
  "deployStrategy": "push-dev",
  "strategyConfirmed": true,
  "environment": "container",
  "bootstrapSession": "sess-completed-01",
  "bootstrappedAt": "2026-04-20T08:00:00Z"
}
JSON

echo "preseed: planted bootstrapped-never-deployed meta for appdev (FirstDeployedAt empty)"
