#!/bin/bash
# Preseed for dev-self-deploy-keeps-repo (G4).
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
# FINISHED. The seed service-status check verifies ACTIVE. 5-minute cap.
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

ssh appdev ': > /tmp/zcp-preseed-appversions'

SHA=$(ssh appdev "cd /var/www && git rev-parse HEAD")

ssh appdev "zcli login -- '${ZCP_API_KEY}' >/dev/null"
resolved=$(ssh appdev "cd /var/www && git rev-parse --verify '${SHA}^{commit}'")
tmp=$(ssh appdev "mktemp -d")
ssh appdev "cd /var/www && git archive --format=tar '${resolved}' | tar -x -C '${tmp}'"
ssh appdev "cd '${tmp}' && zcli push --service-id ${APPSTAGE_ID} --setup prod --no-git --version-name '${resolved}'"
ssh appdev "rm -rf '${tmp}'"
wait_active "$APPSTAGE_ID"

app_version_id=$(curl -sS -H "Authorization: Bearer ${ZCP_API_KEY}" \
  "${API_BASE}/project/${ZCP_PROJECT_ID}/process?limit=50" \
  | jq -r --arg id "$APPSTAGE_ID" '
    [.list[] | select(.serviceStacks? and ([.serviceStacks[]?.id] | index($id) != null))]
    | sort_by(.created) | last | .appVersion.id // empty
  ')

[ -n "$app_version_id" ] || { echo "preseed: missing appVersion id" >&2; exit 1; }
printf '%s\n' "$app_version_id" | ssh appdev 'cat >> /tmp/zcp-preseed-appversions'
echo "preseed: deployed ${resolved} to appstage (appVersion ${app_version_id})"

# G4 cargo: after the stage deploy, load appdev's repository with user-owned
# state (branch, tag, custom ref, dirty + untracked files, exclude line,
# identity, origin) — the self-deploy must carry all of it into the
# replacement container (docs/spec-workflows.md §12.6 GF-2/GF-9; P10).
# shellcheck source=lib-repo-cargo.sh
. "$(dirname "$0")/lib-repo-cargo.sh"
plant_repo_cargo appdev
