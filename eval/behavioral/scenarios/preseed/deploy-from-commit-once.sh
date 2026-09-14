#!/bin/bash
# Preseed for dev-self-deploy-keeps-repo (G4).
#
# The fixture (nodejs-standard-deployed.yaml) deploys appdev + appstage via
# buildFromGit and this preseed runs AFTER that first build has settled
# ACTIVE (seed.mode: deployed). It then performs ONE deploy-from-commit push
# to appstage from appdev's mounted repo, replicating by hand what
# `zerops_deploy sha=` does (docs/spec-workflows.md §4.9) so the agent
# arrives at a pair where appdev already carries a real zcp/deploy/* tag
# ledger entry BEFORE it is asked to touch anything — the test point is
# that a plain dev self-deploy (buildSSHCommand's GLC-2 safety-net) never
# disturbs that tag namespace or the .git/info/exclude seed while doing its
# own HEAD-ensure/exclude-seed work on the SAME repo.
#
# NOT LIVE-VERIFIED (brief S6 stop condition, docs/spec-eval-farm.md §4.5):
# this script cannot be run in this session. The archive|tar-extraction push
# shape was verified live for the feature itself (plans/
# git-foundation-2026-09-14.md); this single-deploy preseed is a
# simplification of the live-verified deploy-from-commit-twice.sh and needs
# one live pass in ASSEMBLE before this cell is trusted.
set -eu

: "${ZCP_API_KEY:?ZCP_API_KEY not set — required to resolve service ids}"
: "${ZCP_PROJECT_ID:?ZCP_PROJECT_ID not set — required to resolve service ids}"

API_HOST="${ZCP_API_HOST:-api.app-prg1.zerops.io}"
API_BASE="https://${API_HOST}/api/rest/public"

# Resolve service ids via the DIRECT (non-ES) project service-stack list —
# authoritative immediately after the fixture's import (CLAUDE.md's ES-lag
# invariant).
resolve_id() {
  curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
    "${API_BASE}/project/${ZCP_PROJECT_ID}/service-stack" \
    | jq -r --arg name "$1" '.list[] | select(.name==$name) | .id'
}
APPDEV_ID=$(resolve_id appdev)
APPSTAGE_ID=$(resolve_id appstage)
[ -n "$APPDEV_ID" ] || { echo "preseed: could not resolve appdev id" >&2; exit 1; }
[ -n "$APPSTAGE_ID" ] || { echo "preseed: could not resolve appstage id" >&2; exit 1; }

# wait_active <serviceId> — polls the direct project process list for the
# newest stack.build/stack.deploy process against serviceId to reach
# FINISHED, then confirms the appVersion is ACTIVE. 5-minute cap.
wait_active() {
  local id="$1" deadline status
  deadline=$((SECONDS + 300))
  while [ "$SECONDS" -lt "$deadline" ]; do
    status=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
      "${API_BASE}/project/${ZCP_PROJECT_ID}/process?limit=50" \
      | jq -r --arg id "$id" '
        [.list[] | select(.serviceStacks? and ([.serviceStacks[]?.id] | index($id) != null))]
        | sort_by(.created) | last | .status // empty
      ')
    case "$status" in
      FINISHED) echo "preseed: build against ${id} FINISHED"; return 0 ;;
      FAILED|CANCELED) echo "preseed: build against ${id} ended ${status}" >&2; return 1 ;;
    esac
    sleep 5
  done
  echo "preseed: build against ${id} did not finish within 5 minutes (last: ${status:-unknown})" >&2
  return 1
}

SHA=$(ssh appdev "cd /var/www && git rev-parse HEAD")

ssh appdev "zcli login -- '${ZCP_API_KEY}' >/dev/null"
resolved=$(ssh appdev "cd /var/www && git rev-parse --verify '${SHA}^{commit}'")
tmp=$(ssh appdev "mktemp -d")
ssh appdev "cd /var/www && git archive --format=tar '${resolved}' | tar -x -C '${tmp}'"
ssh appdev "cd '${tmp}' && zcli push --service-id ${APPSTAGE_ID} --no-git --version-name '${resolved}'"
ssh appdev "rm -rf '${tmp}'"
wait_active "$APPSTAGE_ID"

app_version_id=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
  "${API_BASE}/project/${ZCP_PROJECT_ID}/process?limit=50" \
  | jq -r --arg id "$APPSTAGE_ID" '
    [.list[] | select(.serviceStacks? and ([.serviceStacks[]?.id] | index($id) != null))]
    | sort_by(.created) | last | .appVersion.id // empty
  ')

at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
msg=$(printf '{"sha":"%s","appVersionId":"%s","target":"appstage","project":"%s","at":"%s"}' \
  "$resolved" "$app_version_id" "$ZCP_PROJECT_ID" "$at")
tag_name="zcp/deploy/${ZCP_PROJECT_ID}/appstage/${app_version_id}"
ssh appdev "cd /var/www && git -c user.name='Zerops Agent' -c user.email='agent@zerops.io' \
  tag -a -f -m '$(printf '%s' "$msg" | sed "s/'/'\\\\''/g")' '${tag_name}' '${resolved}'"

echo "preseed: deployed ${resolved} to appstage (appVersion ${app_version_id:-unknown}), ledger tag ${tag_name}"
