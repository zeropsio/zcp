#!/bin/bash
# Preseed for develop-ambiguous-state scenario.
#
# Plants two bootstrapped-but-never-deployed services (appdev + appstage,
# standard mode pair) AND an initial zerops.yaml + minimal app code in the
# SSHFS mount. The agent gets a terse "deploy my weather app" prompt and
# must dedeuce: first-deploy branch, dev first then stage cross-deploy,
# read the existing code rather than generate new.
set -eu

STATE="${ZCP_WORK_DIR:-.}/.zcp/state"
MOUNT="${ZCP_WORK_DIR:-.}/appdev"
# CleanupProject preserves .zcp by design — wipe leftover state from the
# previous scenario so we don't inherit phantom sessions / metas.
rm -rf "$STATE/sessions" "$STATE/services"
mkdir -p "$STATE/services" "$STATE/sessions" "$STATE/work" "$MOUNT/public"
cat > "$STATE/registry.json" <<'JSON'
{"version":"1","sessions":[]}
JSON
rm -f "$STATE/session-registry.json"

# Standard-mode pair: dev+stage sharing a single ServiceMeta record.
cat > "$STATE/services/appdev.json" <<JSON
{
  "hostname": "appdev",
  "stageHostname": "appstage",
  "mode": "standard",
  "closeDeployMode": "auto",
  "closeDeployModeConfirmed": true,
  "bootstrapSession": "sess-completed-02",
  "bootstrappedAt": "2026-04-20T08:00:00Z"
}
JSON

# Scaffolded zerops.yaml already on the SSHFS mount.
cat > "$MOUNT/zerops.yaml" <<'YAML'
zerops:
  - setup: appdev
    build:
      base: php@8.4
    run:
      base: php-nginx@8.4
      ports:
        - port: 80
          httpSupport: true
      documentRoot: public
  - setup: appstage
    build:
      base: php@8.4
    run:
      base: php-nginx@8.4
      ports:
        - port: 80
          httpSupport: true
      documentRoot: public
YAML

# Tiny weather app that reads the city from the query string.
cat > "$MOUNT/public/index.php" <<'PHP'
<?php
$cities = ['prague' => 50.0755, 'brno' => 49.1951, 'ostrava' => 49.8209];
$city = $_GET['city'] ?? 'prague';
$lat = $cities[strtolower($city)] ?? $cities['prague'];
$url = "https://api.open-meteo.com/v1/forecast?latitude={$lat}&longitude=14.4378&current_weather=true";
$data = @json_decode(@file_get_contents($url), true);
$temp = $data['current_weather']['temperature'] ?? 'n/a';
echo "<h1>Weather for " . htmlspecialchars($city) . ": {$temp}°C</h1>";
echo '<ul><li><a href="?city=prague">Prague</a></li><li><a href="?city=brno">Brno</a></li><li><a href="?city=ostrava">Ostrava</a></li></ul>';
PHP

echo "preseed: planted bootstrapped-never-deployed standard pair + zerops.yaml + weather app code on mount"
