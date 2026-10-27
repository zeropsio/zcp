#!/bin/bash
# Preseed for adopt-app-with-existing-pool (A13).
#
# The fixture (code-runner-with-pool-imported.yaml) imports `app` (nodejs@22)
# and `sandbox` (swarm@1), both empty. This preseed makes them the code runner
# a person built by hand:
#   1. plants fixtures/repos/code-runner into app's /var/www;
#   2. deploys the pool's image (setup `sandbox`) from app with `zcli push`,
#      and waits for its build to finish;
#   3. deploys the app (setup `app`) the same way, and waits — last, because
#      its deploy replaces the container that holds the planted source.
# Both pushes run from INSIDE app over SSH (a real `zcli push --no-git`, never
# a zerops_deploy call): the deploys the agent finds are not its own, and the
# deployed app carries no repository, as a hand-pushed service does.
#
# seed.expect (adopt-app-with-existing-pool.md) then asserts, before the agent
# is spawned: both services ACTIVE, a FINISHED stack.build on each, and app
# answering GET / on port 3000. The pool's deploy is what gives
# `unchanged: [sandbox]` its baseline appVersion.
#
# NOT LIVE-VERIFIED: written offline. The pool push follows a live-verified
# python@3.12 pool push; the direct-list wire shape follows the other
# preseeds (deploy-from-commit-once.sh). Needs one live farm pass before the
# cell is trusted.
set -eu

: "${ZCP_API_KEY:?ZCP_API_KEY not set — required to resolve service ids}"
: "${ZCP_PROJECT_ID:?ZCP_PROJECT_ID not set — required to resolve service ids}"

API_HOST="${ZCP_API_HOST:-api.app-prg1.zerops.io}"
API_BASE="https://${API_HOST}/api/rest/public"

# shellcheck source=lib-fixture-repo.sh
. "$(dirname "$0")/lib-fixture-repo.sh"

# Resolve service ids via the DIRECT (non-ES) project service-stack list —
# authoritative immediately after the fixture's import (CLAUDE.md's ES-lag
# invariant).
resolve_id() {
  curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
    "${API_BASE}/project/${ZCP_PROJECT_ID}/service-stack" \
    | jq -r --arg name "$1" '.list[] | select(.name==$name) | .id'
}
APP_ID=$(resolve_id app)
SANDBOX_ID=$(resolve_id sandbox)
[ -n "$APP_ID" ] || { echo "preseed: could not resolve app id" >&2; exit 1; }
[ -n "$SANDBOX_ID" ] || { echo "preseed: could not resolve sandbox id" >&2; exit 1; }

# wait_build <serviceId> <hostname> — polls the direct project process list
# for the newest stack.build process against serviceId to reach FINISHED.
# 10-minute cap.
wait_build() {
  local id="$1" host="$2" deadline status
  deadline=$((SECONDS + 600))
  while [ "$SECONDS" -lt "$deadline" ]; do
    status=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
      "${API_BASE}/project/${ZCP_PROJECT_ID}/process?limit=50" \
      | jq -r --arg id "$id" '
        [.list[] | select(.actionName == "stack.build")
                 | select([.serviceStacks[]?.id] | index($id) != null)]
        | sort_by(.created) | last | .status // empty
      ')
    case "$status" in
      FINISHED) echo "preseed: ${host} stack.build FINISHED"; return 0 ;;
      FAILED|CANCELED) echo "preseed: ${host} stack.build ended ${status}" >&2; return 1 ;;
    esac
    sleep 5
  done
  echo "preseed: ${host} stack.build did not finish within 10 minutes (last: ${status:-unknown})" >&2
  return 1
}

plant_fixture_repo code-runner app /var/www

# zcli inside the service container is not logged in; ops.DeploySSH logs in
# the same way before every push.
ssh app "zcli login -- '${ZCP_API_KEY}' >/dev/null"

ssh app "cd /var/www && zcli push --service-id ${SANDBOX_ID} --setup sandbox --no-git"
wait_build "$SANDBOX_ID" sandbox

ssh app "cd /var/www && zcli push --service-id ${APP_ID} --setup app --no-git"
wait_build "$APP_ID" app

echo "preseed: code runner deployed — app + sandbox pool"
