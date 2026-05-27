#!/bin/bash
# Preseed for discover-adoption-state-resumable-uses-sessionid.
#
# Fixture deploys appdev/appstage/db. Preseed plants an INCOMPLETE
# pair-keyed ServiceMeta for appdev — no BootstrappedAt, but with a
# non-empty BootstrapSession tag — simulating a crashed mid-bootstrap
# whose session-registry entry is gone but the per-service partial
# meta survived. Per workflow semantics (route.go::resumeOption,
# route.go::adoptableServices), this state is RESUMABLE (not
# adoptable).
#
# Also seeds a corresponding session-registry entry so route="resume"
# sessionId="<the seeded id>" has something real to look at; the
# session itself is incomplete (provision step pending) so resume
# routing surfaces the existing pre-state instead of erroring on
# "session not found".
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work"

SESSION_ID="sess-stale-mid-bootstrap-2026-05-27"

cat > "$STATE/registry.json" <<JSON
{"version":"1","sessions":[{"sessionId":"$SESSION_ID","workflow":"bootstrap","pid":1,"startedAt":"2026-05-26T15:00:00Z"}]}
JSON
rm -f "$STATE/session-registry.json"

# Incomplete pair-keyed meta: BootstrappedAt empty → IsComplete()=false,
# BootstrapSession set → classified as AdoptionResumable, NOT
# AdoptionAdoptable. Discover warning prose must name this sessionId
# so the agent can pass it to route=resume.
cat > "$STATE/services/appdev.json" <<JSON
{
  "hostname": "appdev",
  "mode": "standard",
  "stageHostname": "appstage",
  "closeDeployMode": "unset",
  "gitPushState": "unconfigured",
  "buildIntegration": "none",
  "environment": "container",
  "bootstrapSession": "$SESSION_ID"
}
JSON

# Stub session state file so registry lookup matches a real file even
# though step progress is empty.
cat > "$STATE/sessions/$SESSION_ID.json" <<JSON
{
  "sessionId": "$SESSION_ID",
  "workflow": "bootstrap",
  "bootstrap": {
    "active": true,
    "currentStep": 1,
    "route": "adopt",
    "steps": [
      {"name": "discover", "status": "complete"},
      {"name": "provision", "status": "pending"},
      {"name": "close", "status": "pending"}
    ]
  }
}
JSON

echo "preseed: planted incomplete pair-keyed ServiceMeta + matching session-registry entry"
echo "  Session: $SESSION_ID (step 2/3 pending)"
echo "  Expected adoptionState: resumable on appdev + appstage"
echo "  Expected warning: includes sessionId=\"$SESSION_ID\""
