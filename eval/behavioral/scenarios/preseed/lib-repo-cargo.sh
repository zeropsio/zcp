#!/bin/bash
# Shared preseed library: plant "cargo" a user's repository can legitimately
# carry, so a scenario can prove zcp's automatic git actions (GLC-1 self-heal,
# GLC-2 deploy safety-net, GLC-7 adopt baseline, deploy-from-commit) never
# destroy any of it (docs/spec-workflows.md §12.6 GF-2/GF-5/GF-9).
#
# Every value below is a constant the scenario's containerCheck entries match
# against by name — a check never needs preseed-time state, which matters for
# self-deploy (the container is replaced, /tmp is gone, only /var/www travels).
#
# Usage: source "$(dirname "$0")/lib-repo-cargo.sh"; plant_repo_cargo <ssh-host>
# Requires an existing repository with a content HEAD at /var/www on the host.

CARGO_BRANCH="feature/preseed-cargo"
CARGO_TAG="v0.1-user-release"
CARGO_REF="refs/t3/checkpoints/preseed"
CARGO_ANCHOR_REF="refs/preseed/head"
CARGO_TRACKED="cargo-tracked.txt"
CARGO_UNTRACKED="cargo-untracked.txt"
CARGO_EXCLUDE_LINE="user-private-dir/"
CARGO_IDENTITY_EMAIL="preseed@example.com"
CARGO_IDENTITY_NAME="Preseed User"
CARGO_ORIGIN="https://example.invalid/preseed/repo.git"

plant_repo_cargo() {
  local host="$1"
  ssh "$host" "cd /var/www && \
    git config user.email '${CARGO_IDENTITY_EMAIL}' && \
    git config user.name '${CARGO_IDENTITY_NAME}' && \
    git checkout -q -b '${CARGO_BRANCH}' && \
    printf 'v1\n' > '${CARGO_TRACKED}' && \
    git add '${CARGO_TRACKED}' && \
    git commit -q -m 'preseed: user commit carrying cargo' && \
    git tag '${CARGO_TAG}' && \
    git update-ref '${CARGO_REF}' HEAD && \
    git update-ref '${CARGO_ANCHOR_REF}' HEAD && \
    printf 'v2 uncommitted\n' >> '${CARGO_TRACKED}' && \
    printf 'untracked cargo\n' > '${CARGO_UNTRACKED}' && \
    mkdir -p .git/info && printf '%s\n' '${CARGO_EXCLUDE_LINE}' >> .git/info/exclude && \
    mkdir -p user-private-dir && printf 'keep\n' > user-private-dir/keep && \
    (git remote get-url origin >/dev/null 2>&1 && git remote set-url origin '${CARGO_ORIGIN}' || git remote add origin '${CARGO_ORIGIN}')"
  echo "preseed: planted repo cargo on ${host} (branch ${CARGO_BRANCH}, tag ${CARGO_TAG}, ref ${CARGO_REF}, dirty ${CARGO_TRACKED}, untracked ${CARGO_UNTRACKED})"
}
