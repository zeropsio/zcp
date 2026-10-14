#!/bin/bash
# Shared preseed library: plant a committed fixture repository
# (fixtures/repos/<name>/) into a dev service as a git repository with one
# commit — the monorepo fixtures F1–F3 (mono-e0 TEST-PLAN §3). No GitHub, PAT
# or network: the tree travels with the scenario tree.
#
# Usage: source "$(dirname "$0")/lib-fixture-repo.sh"
#        plant_fixture_repo <name> <ssh-host> [<dir>]   # dir default /var/www
# Variant B (mono-bun-go-worker) plants into the shared volume: <dir> /var/www/ws.

FIXTURE_REPOS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../fixtures/repos" && pwd)"
FIXTURE_IDENTITY_EMAIL="preseed@example.com"
FIXTURE_IDENTITY_NAME="Preseed User"

plant_fixture_repo() {
  local name="$1" host="$2" dir="${3:-/var/www}"
  local src="${FIXTURE_REPOS_DIR}/${name}"
  [ -f "${src}/zerops.yml" ] || { echo "preseed: no fixture repo ${name} at ${src}" >&2; return 1; }
  tar -C "${src}" -cf - . | ssh "${host}" "mkdir -p '${dir}' && tar -C '${dir}' -xf - && cd '${dir}' && \
    git init -q -b main && \
    git config user.email '${FIXTURE_IDENTITY_EMAIL}' && \
    git config user.name '${FIXTURE_IDENTITY_NAME}' && \
    git add -A && git commit -q -m 'fixture: ${name}'"
  echo "preseed: planted fixture repo ${name} on ${host}:${dir}"
}
