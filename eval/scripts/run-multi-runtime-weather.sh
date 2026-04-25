#!/bin/bash
# Multi-runtime weather-dashboard audit sweep.
#
# Runs 4 weather-dashboard scenarios sequentially against eval-zcp to
# measure dispatch-brief bloat + friction across runtimes. Every scenario
# shares the same task skeleton + ATOM BUCKET CLASSIFICATION section at
# the end of its prompt so aggregated audit can cross-reference bucket
# assignments across runtimes.
#
# Produces:
#   eval/results/scenario-<TAG>/<scenarioID>/result.json
#   eval/results/scenario-<TAG>/<scenarioID>/run.log (agent transcript)
#
# Usage:
#   ./eval/scripts/run-multi-runtime-weather.sh
#   EVAL_SKIP_BUILD=1 ./eval/scripts/run-multi-runtime-weather.sh  # (default — binary already shipped)
#
# Runtime: ~12 min per scenario × 4 = ~50 min total.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

SCENARIOS=(
  "internal/eval/scenarios/weather-dashboard-php-laravel.md"
  "internal/eval/scenarios/weather-dashboard-nodejs.md"
  "internal/eval/scenarios/weather-dashboard-python.md"
  "internal/eval/scenarios/weather-dashboard-go.md"
)

SUITE_TAG="multi-weather-$(date +%Y%m%d_%H%M%S)"
SUITE_LOG="$PROJECT_DIR/eval/results/$SUITE_TAG.log"
mkdir -p "$PROJECT_DIR/eval/results"

# Skip build by default — binary is assumed already shipped. Override with
# EVAL_SKIP_BUILD=0 to force rebuild on first scenario.
export EVAL_SKIP_BUILD="${EVAL_SKIP_BUILD:-1}"

echo "==> Suite: $SUITE_TAG" | tee -a "$SUITE_LOG"
echo "==> EVAL_SKIP_BUILD=$EVAL_SKIP_BUILD" | tee -a "$SUITE_LOG"
echo "==> Scenarios: ${#SCENARIOS[@]}" | tee -a "$SUITE_LOG"
echo "" | tee -a "$SUITE_LOG"

cd "$PROJECT_DIR"

for i in "${!SCENARIOS[@]}"; do
  sc="${SCENARIOS[i]}"
  idx=$((i+1))
  sc_id="$(basename "$sc" .md)"
  echo "=== [$idx/${#SCENARIOS[@]}] $sc_id ($(date +%H:%M:%S)) ===" | tee -a "$SUITE_LOG"

  if ./eval/scripts/run-scenario.sh "$sc" 2>&1 | tee -a "$SUITE_LOG"; then
    echo "==> [$idx/${#SCENARIOS[@]}] $sc_id DONE" | tee -a "$SUITE_LOG"
  else
    echo "==> [$idx/${#SCENARIOS[@]}] $sc_id FAILED (continuing)" | tee -a "$SUITE_LOG"
  fi
  echo "" | tee -a "$SUITE_LOG"
done

echo "==> Suite $SUITE_TAG complete at $(date +%H:%M:%S)" | tee -a "$SUITE_LOG"
echo "==> Results under eval/results/scenario-*/" | tee -a "$SUITE_LOG"
echo "==> Suite log: $SUITE_LOG" | tee -a "$SUITE_LOG"
