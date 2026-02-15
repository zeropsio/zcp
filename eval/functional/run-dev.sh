#!/bin/bash
# Run a functional eval: deploy Bun+PostgreSQL app on Zerops and verify.
#
# This runs on zcpx via SSH — Claude creates services, writes code,
# deploys, and runs the 7-point verification protocol.
#
# Usage:
#   ./eval/functional/run-dev.sh                           # Default
#   ./eval/functional/run-dev.sh --build                   # Rebuild & deploy ZCP first
#   EVAL_REMOTE_HOST=myhost ./eval/functional/run-dev.sh   # Custom host

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
REMOTE_HOST="${EVAL_REMOTE_HOST:-zcpx}"
TAG="func_$(date +%Y%m%d_%H%M%S)"
RESULTS_DIR="$PROJECT_DIR/eval/results/$TAG"
REMOTE_PROMPT="/tmp/eval_prompt_${TAG}.md"
REMOTE_LOG="/tmp/eval_${TAG}.jsonl"

# --- Optional: rebuild & deploy ZCP binary ---
if [[ "${1:-}" == "--build" ]]; then
    echo "==> Building and deploying ZCP binary..."
    "$PROJECT_DIR/eval/scripts/build-deploy.sh"
    shift
fi

# --- Prepare results directory ---
mkdir -p "$RESULTS_DIR"

# --- Copy prompt to remote (avoids SSH quoting hell) ---
PROMPT_FILE="$SCRIPT_DIR/prompt-dev.md"
cp "$PROMPT_FILE" "$RESULTS_DIR/prompt.md"
scp -q "$PROMPT_FILE" "$REMOTE_HOST:$REMOTE_PROMPT"

echo "==> Tag: $TAG"
echo "==> Results: $RESULTS_DIR/"
echo "==> Remote: $REMOTE_HOST"

# --- Execute on remote ---
echo "==> Running functional eval on $REMOTE_HOST..."

ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=60 "$REMOTE_HOST" \
  "claude --dangerously-skip-permissions \
    -p \"\$(cat $REMOTE_PROMPT)\" \
    --model opus \
    --max-turns 60 \
    --output-format stream-json \
    --verbose \
    --no-session-persistence 2>&1 | tee $REMOTE_LOG"  \
  > "$RESULTS_DIR/run.jsonl" 2>&1

echo "==> Execution complete. Parsing results..."

# --- Extract tool calls ---
if command -v python3 &>/dev/null; then
    python3 "$PROJECT_DIR/eval/scripts/extract-tool-calls.py" \
        "$RESULTS_DIR/run.jsonl" \
        "$RESULTS_DIR/tools.json" 2>/dev/null || echo "==> Warning: tool extraction failed"
fi

# --- Extract EVAL RESULT block from stream-json ---
python3 -c "
import json, re, sys

results = []
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    try:
        obj = json.loads(line)
        # stream-json: assistant text in content blocks
        if obj.get('type') == 'assistant':
            msg = obj.get('message', '')
            if isinstance(msg, dict):
                for block in msg.get('content', []):
                    if isinstance(block, dict) and block.get('type') == 'text':
                        results.append(block['text'])
            elif isinstance(msg, str):
                results.append(msg)
        # Also check raw content_block_delta for streaming
        if obj.get('type') == 'content_block_delta':
            delta = obj.get('delta', {})
            if delta.get('type') == 'text_delta':
                results.append(delta['text'])
    except (json.JSONDecodeError, KeyError):
        pass

full = '\n'.join(results)
match = re.search(r'=== EVAL RESULT ===(.+?)=== END RESULT ===', full, re.DOTALL)
if match:
    with open(sys.argv[2], 'w') as f:
        f.write(match.group(0) + '\n')
    print('==> Result block extracted')
else:
    # Fallback: search raw lines
    found = []
    for line in open(sys.argv[1]):
        if 'EVAL RESULT' in line or 'verdict' in line:
            found.append(line.strip())
    if found:
        with open(sys.argv[2], 'w') as f:
            f.write('\n'.join(found) + '\n')
        print('==> Result block extracted (fallback)')
    else:
        print('==> Warning: no EVAL RESULT block found in output')
" "$RESULTS_DIR/run.jsonl" "$RESULTS_DIR/result.txt" 2>/dev/null \
  || echo "==> Warning: result extraction failed"

# --- Cleanup remote temp files ---
ssh "$REMOTE_HOST" "rm -f $REMOTE_PROMPT" 2>/dev/null || true

# --- Summary ---
echo ""
echo "=== Functional Eval: $TAG ==="
if [ -f "$RESULTS_DIR/result.txt" ] && [ -s "$RESULTS_DIR/result.txt" ]; then
    cat "$RESULTS_DIR/result.txt"
else
    echo "(No structured result found — check run.jsonl manually)"
fi
echo ""
echo "Files:"
echo "  $RESULTS_DIR/prompt.md    — prompt sent"
echo "  $RESULTS_DIR/run.jsonl    — raw output"
echo "  $RESULTS_DIR/tools.json   — parsed tool calls"
echo "  $RESULTS_DIR/result.txt   — extracted result"
