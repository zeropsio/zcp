#!/bin/bash
# Preseed for custom-domain-present (PA-3, docs/spec-workflows.md §8 O3).
#
# The fixture (nodejs-simple-deployed.yaml) deploys a single simple-mode
# `api` + managed `db` with the subdomain ON at import
# (enableSubdomainAccess: true) — reused as-is (its buildFromGit is already
# grandfathered by legacyUnpinned in eval_scenario_fixture_pin_test.go;
# FM-62 forbids adding a new unpinned entry to that allowlist, and creating
# a real @pin/<label> branch on zerops-recipe-apps/nodejs-hello-world-app is
# an owner action this preseed cannot perform). This preseed then:
#   1. disables the subdomain via the platform API directly (never through
#      zerops_subdomain) so the starting state is "no subdomain";
#   2. creates a project-level public-http-routing entry routing a fake
#      custom domain (farm-<runId>.example.com) at path "/" to api:3000 —
#      the platform accepts and stores the routing without the domain
#      actually being DNS-delegated to Zerops (live-verified 2026-09-13,
#      project eval-x): the routing record exists, and PA-3 says domains !=
#      ∅ on its own is enough for zcp to derive intent=domain and never
#      attempt a subdomain auto-enable — no live DNS check is required for
#      that half of the contract. The domain's own reachability (DNS/TLS/
#      HTTP) is a live fact the agent's own zerops_verify call reports
#      honestly (likely DNS-not-pointing, since this domain is never
#      delegated) — the scenario's oracles assert that the check RAN and
#      named the domain, never that DNS itself resolved.
#
# seed.expect (custom-domain-present.md frontmatter) asserts, before the
# agent is spawned: api and db are ACTIVE.
set -eu

: "${ZCP_API_KEY:?ZCP_API_KEY not set — required to resolve the api service id, disable its subdomain, and create the routing}"
: "${ZCP_PROJECT_ID:?ZCP_PROJECT_ID not set — required to resolve the api service id and create the routing}"
: "${ZCP_FARM_RUN:?ZCP_FARM_RUN not set — required to build a unique domain label}"

API_HOST="${ZCP_API_HOST:-api.app-prg1.zerops.io}"
API_BASE="https://${API_HOST}/api/rest/public"
DOMAIN="farm-${ZCP_FARM_RUN}.example.com"

# Resolve api's service id via the DIRECT (non-ES) project service-stack
# list — authoritative immediately after the fixture's import, unlike the
# Elasticsearch-backed search (CLAUDE.md's ES-lag invariant).
API_SERVICE_ID=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
  "${API_BASE}/project/${ZCP_PROJECT_ID}/service-stack" \
  | jq -r '.list[] | select(.name=="api") | .id')

if [ -z "$API_SERVICE_ID" ]; then
  echo "preseed: could not resolve the api service id from project ${ZCP_PROJECT_ID}" >&2
  exit 1
fi

# 1. Disable the fixture's own subdomain (on by default) — never through
# zerops_subdomain. Fail loudly on a non-2xx response.
DISABLE_STATUS=$(curl -sS -o /tmp/disable-subdomain-response.json -w '%{http_code}' \
  -X PUT -H "Authorization: Bearer ${ZCP_API_KEY}" \
  "${API_BASE}/service-stack/${API_SERVICE_ID}/disable-subdomain-access")

if [ "$DISABLE_STATUS" -lt 200 ] || [ "$DISABLE_STATUS" -ge 300 ]; then
  echo "preseed: disable-subdomain-access on api (${API_SERVICE_ID}) returned HTTP ${DISABLE_STATUS}: $(cat /tmp/disable-subdomain-response.json)" >&2
  exit 1
fi

echo "preseed: api subdomain disabled via platform API (HTTP ${DISABLE_STATUS})"

# 2. Create the custom-domain routing.
ROUTING_BODY=$(jq -n --arg domain "$DOMAIN" --arg serviceStackId "$API_SERVICE_ID" '{
  sslEnabled: false,
  cdnEnabled: null,
  domains: [$domain],
  locations: [{path: "/", port: 3000, serviceStackId: $serviceStackId, config: null}]
}')

ROUTING_STATUS=$(curl -sS -o /tmp/public-http-routing-response.json -w '%{http_code}' \
  -X POST -H "Authorization: Bearer ${ZCP_API_KEY}" -H "Content-Type: application/json" \
  -d "$ROUTING_BODY" \
  "${API_BASE}/project/${ZCP_PROJECT_ID}/public-http-routing")

if [ "$ROUTING_STATUS" -lt 200 ] || [ "$ROUTING_STATUS" -ge 300 ]; then
  echo "preseed: create public-http-routing for api (${API_SERVICE_ID}) at domain ${DOMAIN} returned HTTP ${ROUTING_STATUS}: $(cat /tmp/public-http-routing-response.json)" >&2
  exit 1
fi

echo "preseed: public-http-routing created for api at domain ${DOMAIN} (HTTP ${ROUTING_STATUS})"
