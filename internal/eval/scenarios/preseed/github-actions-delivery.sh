#!/bin/bash
# Preseed for delivery-git-push-actions-e2e.
#
# The scenario needs a real reusable GitHub repository without leaking the PAT
# into versioned scenario files or the agent transcript. The runner must pass
# EVAL_GITHUB_PAT (and usually GH_TOKEN for gh CLI) in the process env. This
# script:
#   1. plants complete simple-mode ServiceMeta for the already-deployed app,
#   2. stores GIT_TOKEN on the app service via the Zerops API,
#   3. restarts app if needed so GIT_TOKEN is live in the runtime container,
#   4. initializes /var/www in the app container as a clean git repo, and
#   5. force-pushes that exact baseline to github.com/krls2020/eval2 main.
#
# The PAT is never written to the repo, never echoed, and only appears as an
# environment variable consumed by curl/git/gh.
set -eu

PROJECT_ID="i6HLVWoiQeeLv8tV0ZZ0EQ"
REMOTE_URL="https://github.com/krls2020/eval2.git"
STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

if [ -z "${EVAL_GITHUB_PAT:-}" ]; then
  echo "preseed: EVAL_GITHUB_PAT is required for github-actions delivery scenario" >&2
  exit 1
fi

rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work"
cat > "$STATE/registry.json" <<'JSON'
{"version":"1","sessions":[]}
JSON
rm -f "$STATE/session-registry.json"

cat > "$STATE/services/app.json" <<JSON
{
  "hostname": "app",
  "mode": "simple",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "gitPushState": "unconfigured",
  "buildIntegration": "none",
  "environment": "container",
  "bootstrapSession": "sess-completed-github-actions",
  "bootstrappedAt": "2026-04-20T08:00:00Z",
  "firstDeployedAt": "2026-04-20T08:30:00Z"
}
JSON

svc_id="$(zcli service list -P "$PROJECT_ID" 2>/dev/null | awk '$0 ~ / app / {print $2; exit}')"
if [ -z "$svc_id" ]; then
  echo "preseed: could not resolve app service id" >&2
  exit 1
fi

api_token="${ZCP_API_KEY:-}"
if [ -z "$api_token" ] && [ -r "$HOME/.config/zerops/cli.data" ]; then
  api_token="$(jq -r '.Token // empty' "$HOME/.config/zerops/cli.data")"
fi
if [ -z "$api_token" ]; then
  echo "preseed: could not resolve Zerops API token from env or zcli cli.data" >&2
  exit 1
fi

env_payload="$(jq -n --arg token "$EVAL_GITHUB_PAT" '{envFile: ("GIT_TOKEN=" + $token + "\n")}')"
curl -fsS \
  -X PUT "https://api.app-prg1.zerops.io/api/rest/public/service-stack/${svc_id}/user-data/env-file" \
  -H "Authorization: Bearer ${api_token}" \
  -H "Content-Type: application/json" \
  --data-binary "$env_payload" >/dev/null

if ! ssh $SSH_OPTS app 'test -n "$GIT_TOKEN"' 2>/dev/null; then
  zcli service stop app -P "$PROJECT_ID" >/dev/null 2>&1 || true
  zcli service start app -P "$PROJECT_ID" >/dev/null 2>&1 || true
fi

for _ in $(seq 1 60); do
  if ssh $SSH_OPTS app 'test -n "$GIT_TOKEN"' 2>/dev/null; then
    break
  fi
  sleep 5
done

if ! ssh $SSH_OPTS app 'test -n "$GIT_TOKEN"' 2>/dev/null; then
  echo "preseed: GIT_TOKEN did not become visible inside app runtime" >&2
  exit 1
fi

ssh $SSH_OPTS app "REMOTE_URL='$REMOTE_URL' bash -s" <<'REMOTE'
set -eu
cd /var/www
if [ "$(pwd)" != "/var/www" ]; then
  echo "refusing to reset unexpected working directory: $(pwd)" >&2
  exit 1
fi
find . -mindepth 1 -maxdepth 1 -exec rm -rf {} +
mkdir -p public
cat > public/index.php <<'PHP'
<?php
echo "<!doctype html><html><head><title>ZCP delivery baseline</title></head><body>";
echo "<h1>ZCP delivery baseline</h1>";
echo "<p>Ready for GitHub Actions delivery.</p>";
echo "</body></html>";
PHP
cat > zerops.yaml <<'YAML'
zerops:
  - setup: app
    build:
      base: php@8.4
      deployFiles: ./
    run:
      base: php-nginx@8.4
      ports:
        - port: 80
          httpSupport: true
      documentRoot: public
YAML
git init -b main >/dev/null 2>&1 || { git init >/dev/null 2>&1; git checkout -B main >/dev/null 2>&1; }
git config user.email agent@zerops.io
git config user.name "Zerops Agent"
git add -A
git commit -m "baseline for zcp eval" >/dev/null 2>&1 || true
git remote add origin "$REMOTE_URL" 2>/dev/null || git remote set-url origin "$REMOTE_URL"
trap 'rm -f ~/.netrc' EXIT
umask 077
printf 'machine github.com login oauth2 password %s\n' "$GIT_TOKEN" > ~/.netrc
chmod 600 ~/.netrc
git push --force -u origin main >/dev/null
REMOTE

echo "preseed: app ServiceMeta planted, GIT_TOKEN set, eval2 main reset to runtime baseline"
