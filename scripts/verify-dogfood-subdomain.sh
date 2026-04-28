#!/usr/bin/env bash
# verify-dogfood-subdomain.sh — run-16 §9.5 step 3 (R-15-1 closure operational gate).
#
# Asserts the latest dogfood produced ZERO manual `zerops_subdomain action=enable`
# tool calls. Recipe-authoring auto-enable now works via dual-signal eligibility
# (detail.SubdomainAccess OR Ports[].HTTPSupport — see deploy_subdomain.go +
# tranche-0 commit 1); any manual enable in the dogfood is a regression.
#
# Run after every dogfood; refuses run-N readiness sign-off if any manual enable
# appears in the latest run's session jsonls.
#
# Exit codes mirror the verify-claudemd-zerops-free pattern: 0 = PASS, 1 = FAIL,
# 2 = setup error (no dogfood found, no jsonls).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUNS_DIR="$REPO_ROOT/docs/zcprecipator3/runs"

if [ ! -d "$RUNS_DIR" ]; then
    echo "ERROR: runs dir not found: $RUNS_DIR" >&2
    exit 2
fi

LATEST_RUN="$(ls -1 "$RUNS_DIR" | grep -E '^[0-9]+$' | sort -n | tail -1)"
if [ -z "$LATEST_RUN" ]; then
    echo "ERROR: no numeric run dirs in $RUNS_DIR" >&2
    exit 2
fi

RUN_DIR="$RUNS_DIR/$LATEST_RUN"

# Older runs use SESSSION_LOGS (3 S's, run-15 typo); newer runs use SESSION_LOGS.
JSONL_DIR=""
for candidate in "$RUN_DIR/SESSION_LOGS" "$RUN_DIR/SESSSION_LOGS"; do
    if [ -d "$candidate" ]; then
        JSONL_DIR="$candidate"
        break
    fi
done
if [ -z "$JSONL_DIR" ]; then
    echo "ERROR: no SESSION_LOGS dir under $RUN_DIR" >&2
    exit 2
fi

PATTERN='"name":"mcp__zerops__zerops_subdomain"[^}]*"action":"enable"'

# Collect every jsonl under main + subagents.
mapfile -t JSONLS < <(find "$JSONL_DIR" -type f -name '*.jsonl')
if [ ${#JSONLS[@]} -eq 0 ]; then
    echo "ERROR: no jsonl files under $JSONL_DIR" >&2
    exit 2
fi

OFFENDING=()
for jsonl in "${JSONLS[@]}"; do
    if grep -qE "$PATTERN" "$jsonl"; then
        OFFENDING+=("$jsonl")
    fi
done

if [ ${#OFFENDING[@]} -gt 0 ]; then
    echo "FAIL: ${#OFFENDING[@]} jsonl(s) contain manual zerops_subdomain action=enable in run $LATEST_RUN"
    for f in "${OFFENDING[@]}"; do
        echo "  $f"
        grep -nE "$PATTERN" "$f" | head -3
    done
    echo
    echo "Recipe-authoring auto-enable should fire from deploy_subdomain.go's dual-signal predicate"
    echo "(detail.SubdomainAccess OR Ports[].HTTPSupport). Manual enable indicates a regression."
    exit 1
fi

echo "PASS: ${#JSONLS[@]} jsonl(s) inspected in run $LATEST_RUN; no manual zerops_subdomain action=enable"
exit 0
