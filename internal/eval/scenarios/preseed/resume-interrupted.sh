#!/bin/bash
# Preseed for bootstrap-resume-interrupted scenario.
#
# Plants incomplete ServiceMeta for `appdev` tagged with a session ID that
# belongs to a dead PID — the envelope must report IdleIncomplete and
# BootstrapDiscover must surface `route="resume"` as the top option.
#
# env:
#   ZCP_SUITE_ID   — unused
#   ZCP_WORK_DIR   — work directory (defaults to CWD)
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
mkdir -p "$STATE/services" "$STATE/sessions"

SESSION_ID="sess-abandoned-01"
DEAD_PID=9999999

# Incomplete ServiceMeta — BootstrappedAt empty, BootstrapSession tagged to
# the abandoned session. IsResumable() fires only when both conditions hold.
cat > "$STATE/services/appdev.json" <<JSON
{
  "hostname": "appdev",
  "mode": "dev",
  "bootstrapSession": "${SESSION_ID}",
  "bootstrappedAt": "",
  "environment": "container"
}
JSON

# Orphan session file with the dead PID. Engine.Resume accepts this because
# isProcessAlive(DEAD_PID) returns false.
cat > "$STATE/sessions/${SESSION_ID}.json" <<JSON
{
  "version": "2",
  "sessionId": "${SESSION_ID}",
  "pid": ${DEAD_PID},
  "projectId": "proj-1",
  "workflow": "bootstrap",
  "intent": "laravel dashboard (first attempt)",
  "iteration": 0,
  "createdAt": "2026-04-19T10:00:00Z",
  "updatedAt": "2026-04-19T10:05:00Z",
  "bootstrap": {
    "active": true,
    "steps": [
      {"name": "discover", "status": "complete", "attestation": "plan submitted"},
      {"name": "provision", "status": "in_progress"},
      {"name": "close", "status": "pending"}
    ],
    "plan": {
      "targets": [{
        "runtime": {"devHostname": "appdev", "type": "php-nginx@8.4", "bootstrapMode": "dev"},
        "dependencies": []
      }]
    }
  }
}
JSON

# Registry entry so ListSessions picks it up.
cat > "$STATE/session-registry.json" <<JSON
{
  "entries": [
    {
      "sessionId": "${SESSION_ID}",
      "pid": ${DEAD_PID},
      "workflow": "bootstrap",
      "projectId": "proj-1",
      "intent": "laravel dashboard (first attempt)",
      "createdAt": "2026-04-19T10:00:00Z",
      "updatedAt": "2026-04-19T10:05:00Z"
    }
  ]
}
JSON

echo "preseed: planted resumable session ${SESSION_ID} (dead PID ${DEAD_PID}) + incomplete meta for appdev"
