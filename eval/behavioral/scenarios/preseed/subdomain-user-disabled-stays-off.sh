#!/bin/bash
# Preseed for subdomain-user-disabled-stays-off (PA-2, docs/spec-workflows.md
# §8 O3).
#
# The fixture (nodejs-dev-deployed-mounted.yaml) deploys appdev (dev-only,
# mounted) + db with the subdomain enabled at import (enableSubdomainAccess:
# true) — a stand-in for "zcp auto-enabled this once, same as it does for
# every fresh dev-mode runtime". This preseed then:
#   1. plants ServiceMeta for appdev recording the state zcp leaves after its
#      one-time auto-enable: publicAccess.appdev = {intent: auto,
#      subdomainEnabledByZcpAt: <now>} (internal/workflow/service_meta.go's
#      on-disk shape — the field zcp reads via PublicAccessFor). Without this
#      stamp, ShouldAutoEnableSubdomain (internal/topology/public_access.go)
#      would see an unstamped "auto" record and legitimately re-enable the
#      subdomain the moment the agent's deploy/dev-server-start hook finds a
#      live listener — defeating the scenario.
#   2. disables the subdomain via the platform API directly (never through a
#      zerops_subdomain tool call) — simulating a user who turned it off
#      after zcp's auto-enable. PA-2 says this observed-off state, combined
#      with the non-empty stamp, must reconcile the persisted intent from
#      auto to none the next time zcp reads it (during the agent's own
#      deploy/verify calls) — never re-enable.
#
# seed.expect (subdomain-user-disabled-stays-off.md frontmatter) then asserts,
# before the agent is spawned: appdev and db are ACTIVE. There is no runner
# field to assert the platform's subdomainAccess boolean directly (checked
# docs/spec-eval-farm.md §4: `meta` reads one flat top-level key of the
# candidate's own .zcp/state/services/<hostname>.json, never a nested path
# such as publicAccess.appdev.intent; `expectedServices`/`containerCheck` have
# no subdomainAccess field either) — see this slice's report for the flagged
# gap. This preseed's own disable call is checked for a 2xx response instead
# (below), so a silent platform failure here fails the preseed loudly rather
# than producing a falsely green run.
set -eu

: "${ZCP_API_KEY:?ZCP_API_KEY not set — required to resolve the appdev service id and disable its subdomain}"
: "${ZCP_PROJECT_ID:?ZCP_PROJECT_ID not set — required to resolve the appdev service id}"
: "${ZCP_WORK_DIR:?ZCP_WORK_DIR not set — required to plant the appdev ServiceMeta}"

API_HOST="${ZCP_API_HOST:-api.app-prg1.zerops.io}"
API_BASE="https://${API_HOST}/api/rest/public"

# Resolve appdev's service id via the DIRECT (non-ES) project service-stack
# list — authoritative immediately after the fixture's import, unlike the
# Elasticsearch-backed search (CLAUDE.md's ES-lag invariant).
APPDEV_ID=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
  "${API_BASE}/project/${ZCP_PROJECT_ID}/service-stack" \
  | jq -r '.list[] | select(.name=="appdev") | .id')

if [ -z "$APPDEV_ID" ]; then
  echo "preseed: could not resolve appdev's service id from project ${ZCP_PROJECT_ID}" >&2
  exit 1
fi

# 1. Plant the post-auto-enable ServiceMeta stamp for appdev. Written
# directly under the run's own state dir (workflow.WriteServiceMeta's shape),
# not over SSH — the evaluator/candidate share this filesystem the same way
# eval/behavioral/scenarios/preseed/discover-adopted-pair-meta.sh's full-meta
# plant does.
STATE_SERVICES="${ZCP_WORK_DIR}/.zcp/state/services"
mkdir -p "$STATE_SERVICES"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
cat > "${STATE_SERVICES}/appdev.json" <<JSON
{
  "hostname": "appdev",
  "mode": "dev",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "gitPushState": "unconfigured",
  "buildIntegration": "none",
  "environment": "container",
  "bootstrapSession": "sess-completed-subdomain-disabled",
  "bootstrappedAt": "${NOW}",
  "firstDeployedAt": "${NOW}",
  "primarySetupName": "dev",
  "publicAccess": {
    "appdev": {"intent": "auto", "subdomainEnabledByZcpAt": "${NOW}"}
  }
}
JSON

echo "preseed: planted appdev ServiceMeta with publicAccess.appdev stamped (intent=auto, subdomainEnabledByZcpAt=${NOW})"

# 2. Disable the subdomain via the platform API directly — never through
# zerops_subdomain. Fail loudly on a non-2xx response so a preseed-side
# platform hiccup never masquerades as a passing run.
DISABLE_STATUS=$(curl -sS -o /tmp/disable-subdomain-response.json -w '%{http_code}' \
  -X PUT -H "Authorization: Bearer ${ZCP_API_KEY}" \
  "${API_BASE}/service-stack/${APPDEV_ID}/disable-subdomain-access")

if [ "$DISABLE_STATUS" -lt 200 ] || [ "$DISABLE_STATUS" -ge 300 ]; then
  echo "preseed: disable-subdomain-access on appdev (${APPDEV_ID}) returned HTTP ${DISABLE_STATUS}: $(cat /tmp/disable-subdomain-response.json)" >&2
  exit 1
fi

echo "preseed: appdev subdomain disabled via platform API (HTTP ${DISABLE_STATUS})"
