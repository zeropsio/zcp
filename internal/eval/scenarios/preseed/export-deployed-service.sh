#!/bin/bash
# Preseed for export-deployed-service scenario.
#
# Fixture already deployed `app` (php-nginx) + `db` (postgres) via
# buildFromGit. This preseed plants a COMPLETE ServiceMeta for `app` —
# bootstrapped, FirstDeployedAt set, GitPushState=configured (so the
# new multi-call export workflow can reach the publish-ready branch
# without first chaining through git-push-setup), CloseDeployMode=
# git-push, and RemoteURL pre-cached to the export target so Phase 6
# cache-refresh produces no drift warning.
#
# Without FirstDeployedAt the export offering is suppressed and the
# agent has no envelope cue pointing it at workflow="export".
# Without GitPushState=configured the handler chains to
# setup-git-push-container before publish — the scenario instead pre-
# configures so the agent walks the canonical scope → classify →
# publish narrowing without a chain detour.
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
# CleanupProject preserves .zcp — wipe leftover state from prior runs.
rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work"
cat > "$STATE/registry.json" <<'JSON'
{"version":"1","sessions":[]}
JSON
rm -f "$STATE/session-registry.json"

cat > "$STATE/services/app.json" <<JSON
{
  "hostname": "app",
  "mode": "dev",
  "bootstrapSession": "sess-completed-prev",
  "bootstrappedAt": "2026-04-20T08:00:00Z",
  "firstDeployedAt": "2026-04-20T08:30:00Z",
  "closeDeployMode": "git-push",
  "closeDeployModeConfirmed": true,
  "gitPushState": "configured",
  "remoteUrl": "https://github.com/krls2020/eval1"
}
JSON

echo "preseed: planted ServiceMeta for app — bootstrapped+deployed+git-push-configured (export workflow walks scope→classify→publish without setup-git-push detour)"
