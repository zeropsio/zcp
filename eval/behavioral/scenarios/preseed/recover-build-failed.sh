#!/bin/bash
# Preseed for recover-build-failed (D2).
#
# The fixture (nodejs-dev-deployed-mounted.yaml) deploys appdev via
# buildFromGit and this preseed runs AFTER that first build has settled
# ACTIVE (seed.mode: deployed). It then:
#   1. breaks the MOUNTED source at /var/www on appdev by adding a
#      dependency that cannot resolve (package.json), so the next build is
#      the deliberately broken one — not the fixture's own import;
#   2. triggers a self-deploy of that broken source from INSIDE appdev over
#      SSH (a real `zcli push`, never a `zerops_deploy` tool call — the
#      FAILED build the agent discovers must not be something the agent
#      itself triggered);
#   3. polls the project's process list until that build's process reaches
#      FAILED (10-minute cap).
#
# seed.expect (recover-build-failed.md frontmatter) then asserts, before the
# agent is spawned: appdev is still ACTIVE (the earlier appVersion keeps
# serving a FAILED build never tears down what's live) and the latest
# stack.build process against appdev is FAILED.
#
# NOT LIVE-VERIFIED (brief S6 stop condition, docs/spec-eval-farm.md §4.5):
# this script cannot be run in this session. The DIRECT project-scoped list
# endpoints (GET /project/{id}/service-stack, GET /project/{id}/process) are
# only exercised today through the Go SDK (internal/platform/zerops_direct.go);
# the exact wire shape assumed below (top-level "list" array; service-stack
# entries carry "id"/"name"; process entries carry "actionName"/"status"/
# "serviceStacks[].id") is inferred from internal/platform/types.go's mapped
# Go structs, not observed on the wire directly via curl. Needs one live pass
# in ASSEMBLE before this cell is trusted.
set -eu

: "${ZCP_API_KEY:?ZCP_API_KEY not set — required to resolve appdev's service id}"
: "${ZCP_PROJECT_ID:?ZCP_PROJECT_ID not set — required to resolve appdev's service id}"

API_HOST="${ZCP_API_HOST:-api.app-prg1.zerops.io}"
API_BASE="https://${API_HOST}/api/rest/public"

# Resolve appdev's service id via the DIRECT (non-ES) project service-stack
# list — authoritative immediately after the fixture's import, unlike the
# Elasticsearch-backed search (CLAUDE.md's ES-lag invariant: a just-imported
# service can be briefly absent from the search).
APPDEV_ID=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
  "${API_BASE}/project/${ZCP_PROJECT_ID}/service-stack" \
  | jq -r '.list[] | select(.name=="appdev") | .id')

if [ -z "$APPDEV_ID" ]; then
  echo "preseed: could not resolve appdev's service id from project ${ZCP_PROJECT_ID}" >&2
  exit 1
fi

# Break the mounted source: add a dependency that cannot resolve. Node
# rewrites package.json in place rather than shelling out to a JSON tool
# that may not be on the container.
ssh appdev 'cd /var/www && node -e '"'"'
const fs = require("fs");
const pkg = JSON.parse(fs.readFileSync("package.json", "utf8"));
pkg.dependencies = pkg.dependencies || {};
pkg.dependencies["left-pad-does-not-exist"] = "^99.0.0";
fs.writeFileSync("package.json", JSON.stringify(pkg, null, 2) + "\n");
'"'"''

# Trigger the self-deploy of the now-broken source from INSIDE appdev.
# zcli inside the service container is not logged in (matrix-2: "unauthenticated
# user"); ops.DeploySSH logs in the same way before every push.
ssh appdev "zcli login -- '${ZCP_API_KEY}' >/dev/null"
ssh appdev "cd /var/www && zcli push --service-id ${APPDEV_ID} --setup dev --no-git"

# Poll the project's DIRECT process list (lag-free) for the newest
# stack.build process against appdev to land in FAILED. 10-minute cap.
DEADLINE=$((SECONDS + 600))
STATUS=""
while [ "$SECONDS" -lt "$DEADLINE" ]; do
  STATUS=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
    "${API_BASE}/project/${ZCP_PROJECT_ID}/process?limit=50" \
    | jq -r --arg id "$APPDEV_ID" '
      [.list[] | select(.actionName == "stack.build")
                | select([.serviceStacks[]?.id] | index($id) != null)]
      | sort_by(.created) | last | .status // empty
    ')
  if [ "$STATUS" = "FAILED" ]; then
    echo "preseed: appdev stack.build reached FAILED"
    exit 0
  fi
  sleep 5
done

echo "preseed: appdev stack.build did not reach FAILED within 10 minutes (last status: ${STATUS:-unknown})" >&2
exit 1
