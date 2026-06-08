#!/usr/bin/env bash
# run-batch.sh — run a list of flow-eval scenarios sequentially, continuing
# past per-scenario failures. One background invocation → one notification.
# Usage: run-batch.sh <id> [<id> ...]
# Env vars (ZCP_E2E_*) exported by the caller propagate via flow-eval.sh.
set -uo pipefail
cd "$(dirname "$(readlink -f "$0" 2>/dev/null || echo "$0")")/../.."  # repo root
FE="./eval/behavioral/flow-eval.sh"
declare -a OK=() FAIL=()
for id in "$@"; do
  printf '\n######## BATCH: %s ########\n' "$id" >&2
  if "$FE" "$id"; then
    OK+=("$id")
  else
    FAIL+=("$id")
    printf '!!!!!!!! BATCH FAILED: %s (continuing) !!!!!!!!\n' "$id" >&2
  fi
done
printf '\n======== BATCH SUMMARY ========\n' >&2
printf 'OK   (%d): %s\n' "${#OK[@]}" "${OK[*]:-}" >&2
printf 'FAIL (%d): %s\n' "${#FAIL[@]}" "${FAIL[*]:-}" >&2
