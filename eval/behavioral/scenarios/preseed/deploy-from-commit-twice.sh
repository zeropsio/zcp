#!/bin/bash
# Preseed for rollback-stage-from-ledger (G5).
#
# The buildFromGit fixture leaves appdev with source history. This preseed
# deploys archived commits to stage with --version-name carrying the SHA.
# It saves resulting appVersion IDs in /tmp/zcp-preseed-appversions on
# appdev solely as seed-completion evidence, not as a product deploy ledger.
# Rollback discovers the prior version through platform events.
# The revised tag-free preseed requires a new live farm pass.
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
# FINISHED. The seed service-status check verifies ACTIVE. 5-minute cap per call.
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

# deploy_from_commit <sha> — replicates zerops_deploy sha=<sha> by hand,
# inside appdev, over its own SSH session: resolve, extract outside the
# working tree, push with --no-git --version-name, save seed evidence.
deploy_from_commit() {
  local sha="$1" tmp resolved
  ssh appdev "zcli login -- '${ZCP_API_KEY}' >/dev/null"
  resolved=$(ssh appdev "cd /var/www && git rev-parse --verify '${sha}^{commit}'")
  tmp=$(ssh appdev "mktemp -d")
  ssh appdev "cd /var/www && git archive --format=tar '${resolved}' | tar -x -C '${tmp}'"
  ssh appdev "cd '${tmp}' && zcli push --service-id ${APPSTAGE_ID} --setup prod --no-git --version-name '${resolved}'"
  ssh appdev "rm -rf '${tmp}'"
  wait_active "$APPSTAGE_ID"

  # Resolve the appVersion id this push produced (newest event against
  # appstage) for the fixture completion probe.
  local app_version_id
  app_version_id=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
    "${API_BASE}/project/${ZCP_PROJECT_ID}/process?limit=50" \
    | jq -r --arg id "$APPSTAGE_ID" '
      [.list[] | select(.serviceStacks? and ([.serviceStacks[]?.id] | index($id) != null))]
      | sort_by(.created) | last | .appVersion.id // empty
    ')

  [ -n "$app_version_id" ] || { echo "preseed: missing appVersion id" >&2; exit 1; }
  printf '%s\n' "$app_version_id" | ssh appdev 'cat >> /tmp/zcp-preseed-appversions'
  echo "preseed: deployed ${resolved} to appstage (appVersion ${app_version_id})"
}

ssh appdev ': > /tmp/zcp-preseed-appversions'

SHA_OLD=$(ssh appdev "cd /var/www && git rev-parse HEAD")
deploy_from_commit "$SHA_OLD"

# One trivial commit so the two deployed commits are genuinely distinct
# (same tree would make the second push a no-op build). A NEW tracked file,
# not an append to a guessed entry point: the recipe's tree has no tracked
# index.js, and `commit -am` on an untracked file commits nothing (exit 1 —
# the first live pass died exactly there).
ssh appdev "cd /var/www && echo 'preseed marker: second commit for the rollback fixture' > zcp-preseed-marker.txt && \
  git add zcp-preseed-marker.txt && \
  git -c user.email='preseed@zcp.local' -c user.name='ZCP Preseed' commit -q -m 'preseed: second commit for rollback fixture'"
SHA_NEW=$(ssh appdev "cd /var/www && git rev-parse HEAD")
deploy_from_commit "$SHA_NEW"

echo "preseed: rollback fixture ready — SHA_OLD=${SHA_OLD} SHA_NEW=${SHA_NEW}"
