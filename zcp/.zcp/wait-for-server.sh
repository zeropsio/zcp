#!/usr/bin/env bash
# .zcp/wait-for-server.sh - Wait for server to start listening on port
#
# Usage: .zcp/wait-for-server.sh <hostname> <port> [timeout_seconds]
#
# Polls until port is listening or timeout reached.
# Returns 0 on success, 1 on timeout.

set -euo pipefail

HOSTNAME="${1:-}"
PORT="${2:-8080}"
TIMEOUT="${3:-120}"  # Default 2 minutes for first-time startup with deps

if [ -z "$HOSTNAME" ]; then
    echo "Usage: $0 <hostname> <port> [timeout_seconds]" >&2
    exit 2
fi

# Validate inputs
if [[ ! "$PORT" =~ ^[0-9]+$ ]] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then
    echo "Invalid port: $PORT" >&2
    exit 2
fi

if [[ ! "$TIMEOUT" =~ ^[0-9]+$ ]]; then
    echo "Invalid timeout: $TIMEOUT" >&2
    exit 2
fi

echo "Waiting for $HOSTNAME:$PORT (timeout: ${TIMEOUT}s)..."

START_TIME=$(date +%s)
POLL_INTERVAL=3
LAST_STATUS=""

while true; do
    ELAPSED=$(($(date +%s) - START_TIME))

    if [ "$ELAPSED" -ge "$TIMEOUT" ]; then
        echo ""
        echo "Timeout after ${TIMEOUT}s - port $PORT not listening"
        echo ""
        echo "Check logs: ssh $HOSTNAME 'tail -50 /tmp/app.log'"
        exit 1
    fi

    # Check if port is listening
    if ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$HOSTNAME" \
        "netstat -tlnp 2>/dev/null | grep -q ':$PORT ' || ss -tlnp 2>/dev/null | grep -q ':$PORT '" 2>/dev/null; then
        echo ""
        echo "Port $PORT is listening (${ELAPSED}s)"
        exit 0
    fi

    # Show progress indicator
    printf "."

    # Check log for status (downloading vs error vs success)
    CURRENT_STATUS=$(ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$HOSTNAME" \
        'tail -5 /tmp/app.log 2>/dev/null' 2>/dev/null || echo "")

    if [ -n "$CURRENT_STATUS" ] && [ "$CURRENT_STATUS" != "$LAST_STATUS" ]; then
        # Check for success patterns (early exit even before port check succeeds)
        if echo "$CURRENT_STATUS" | grep -qiE "listening on|server started|ready on|serving at"; then
            printf " (startup detected in log)"
        # Check for download activity
        elif echo "$CURRENT_STATUS" | grep -qiE "download|installing|fetching|resolving"; then
            printf " (downloading deps)"
        # Check for fatal errors
        elif echo "$CURRENT_STATUS" | grep -qiE "error|panic|fatal|failed"; then
            echo ""
            echo "Error detected in log:"
            echo "  $(echo "$CURRENT_STATUS" | tail -2)"
            echo ""
            echo "Full log: ssh $HOSTNAME 'cat /tmp/app.log'"
            exit 1
        fi
        LAST_STATUS="$CURRENT_STATUS"
    fi

    sleep "$POLL_INTERVAL"
done
