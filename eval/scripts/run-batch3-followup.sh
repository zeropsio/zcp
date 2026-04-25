#!/bin/bash
# Follow-up batch — 3 runtimes (bun/java/deno) that didn't fit in autonomous-driver's 3h budget.
set -euo pipefail
PROJECT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
SCENARIOS=(
  "internal/eval/scenarios/weather-dashboard-bun.md"
  "internal/eval/scenarios/weather-dashboard-java.md"
  "internal/eval/scenarios/weather-dashboard-deno.md"
)
TAG="batch3-$(date +%Y%m%d_%H%M%S)"
LOG="$PROJECT_DIR/eval/results/$TAG.log"
mkdir -p "$PROJECT_DIR/eval/results"
export EVAL_SKIP_BUILD=1
cd "$PROJECT_DIR"
echo "==> Batch 3 follow-up: $TAG" | tee -a "$LOG"
for sc in "${SCENARIOS[@]}"; do
  echo "" | tee -a "$LOG"
  echo "=== $(basename $sc .md) $(date +%H:%M:%S) ===" | tee -a "$LOG"
  ./eval/scripts/run-scenario.sh "$sc" 2>&1 | tee -a "$LOG" || echo "FAILED $sc" | tee -a "$LOG"
  python3 eval/scripts/aggregate-weather-audit.py 2>&1 | tee -a "$LOG"
done
echo "==> Batch 3 complete $(date +%H:%M:%S)" | tee -a "$LOG"
