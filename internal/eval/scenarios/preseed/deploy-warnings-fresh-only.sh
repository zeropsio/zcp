#!/bin/bash
# Preseed for deploy-warnings-fresh-only scenario.
#
# Mirrors the bootstrapped-never-deployed pattern (first-deploy-branch.sh) —
# ServiceMeta is COMPLETE and strategy confirmed, so the agent proceeds to
# deploy directly. The task itself instructs the agent to do two deploys with
# a warning condition in between; the test harness verifies both happen and
# that the second deploy's buildLogs do not carry the first's stale warning.
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work"
cat > "$STATE/registry.json" <<'JSON'
{"version":"1","sessions":[]}
JSON
rm -f "$STATE/session-registry.json"

cat > "$STATE/services/appdev.json" <<JSON
{
  "hostname": "appdev",
  "mode": "dev",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "bootstrapSession": "sess-completed-01",
  "bootstrappedAt": "2026-04-20T08:00:00Z"
}
JSON

# Seed the working dir with a PHP hello-world and a broken zerops.yaml.
# The zerops.yaml has deployFiles pointing to a non-existent 'dist' path,
# which triggers a zbuilder warning on the first deploy. The agent fixes it
# and deploys again.
WORK="${ZCP_WORK_DIR:-.}"
cat > "$WORK/index.php" <<'PHP'
<?php echo "hello world @ " . date('c'); ?>
PHP
cat > "$WORK/zerops.yaml" <<'YAML'
zerops:
  - setup: appdev
    build:
      base: php@8.4
      deployFiles: ./dist
    run:
      base: php-nginx@8.4
      ports:
        - port: 80
          httpSupport: true
      documentRoot: public
YAML

echo "preseed: planted bootstrapped-never-deployed meta for appdev + broken zerops.yaml (deployFiles: ./dist)"
