#!/bin/bash
# Launch the Knowledge Eval Agent.
# Run from any terminal — handles CLAUDECODE env var automatically.
#
# Usage: ./eval/run.sh

set -euo pipefail

cd "$(dirname "$0")/.."

unset CLAUDECODE 2>/dev/null || true

mkdir -p eval/results

LOG="eval/results/agent_$(date +%Y%m%d_%H%M%S).log"

echo "==> Starting eval agent. Log: $LOG"
echo "==> Watch progress: tail -f $LOG"

claude --dangerously-skip-permissions \
  -p "$(cat eval/AGENT_PROMPT.md)" \
  --model opus \
  --max-turns 200 \
  2>&1 | stdbuf -oL tee "$LOG"
