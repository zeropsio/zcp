#!/usr/bin/env bash
# verify-claudemd-zerops-free.sh — run-16 §10.4 test 12a / §0.6 mechanism gate.
#
# Asserts every codebase/<host>/claude-md fragment authored in the latest dogfood
# carries zero Zerops platform content. The brief for the claudemd-author sub-
# agent is strictly Zerops-free by construction (briefs.go::BuildClaudeMDBrief);
# this gate is the §0.6 mechanism verification — proves the contract held on the
# actual sub-agent output, not just on the brief's composer-time shape.
#
# Prohibited tokens (per readiness §8.1 + §10.4 #12a):
#   ## Zerops, zsc, zerops_, zcp, zcli, and every hostname declared in plan.json's
#   plan.Services (when a plan.json is present alongside the run).
#
# Ships in tranche 0 alongside verify-dogfood-subdomain.sh; first activates once
# tranche 4+ produces claudemd-author fragments.
#
# Exit codes:
#   0 — PASS (zero violations across all inspected claude-md fragments)
#   1 — FAIL (one or more violations; offending fragmentIds + tokens listed)
#   2 — setup error (no dogfood found, no jsonls)

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

PLAN_JSON="$RUN_DIR/plan.json"

python3 - "$JSONL_DIR" "$PLAN_JSON" <<'PY'
import json
import os
import re
import sys
from pathlib import Path

jsonl_dir = Path(sys.argv[1])
plan_path = Path(sys.argv[2])

# Static prohibited tokens. Word boundaries kept loose intentionally — the brief
# bans literal `zsc`, `zerops_*`, `zcp`, `zcli` so even substring-style hits are
# violations when authoring CLAUDE.md.
static_patterns = [
    (re.compile(r'^##\s+Zerops', re.MULTILINE), '## Zerops heading'),
    (re.compile(r'\bzsc\b'), '`zsc` tool reference'),
    (re.compile(r'\bzerops_[a-z_]+'), '`zerops_*` tool reference'),
    (re.compile(r'\bzcp\b'), '`zcp` tool reference'),
    (re.compile(r'\bzcli\b'), '`zcli` tool reference'),
]

# Optional plan.Services hostname check.
service_hostnames = []
if plan_path.is_file():
    try:
        with plan_path.open() as f:
            plan = json.load(f)
        services = plan.get('services') or plan.get('Services') or []
        for svc in services:
            host = svc.get('hostname') or svc.get('Hostname')
            if host:
                service_hostnames.append(host)
    except Exception as exc:
        print(f'WARN: failed to read plan.json: {exc}', file=sys.stderr)

# fragmentId shape we care about: codebase/<host>/claude-md (exact slot, no
# legacy sub-slot like /service-facts or /notes — those are pre-architecture).
claudemd_fragment_re = re.compile(r'^codebase/[^/]+/claude-md$')

inspected = 0
violations = []

# Each jsonl line is a single JSON document. Iterate every tool_use whose name
# matches mcp__*zerops_recipe and whose input is record-fragment for a claude-md
# fragmentId.
for jsonl in jsonl_dir.rglob('*.jsonl'):
    with jsonl.open() as f:
        for lineno, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            try:
                entry = json.loads(line)
            except json.JSONDecodeError:
                continue
            content = entry.get('message', {}).get('content')
            if not isinstance(content, list):
                continue
            for block in content:
                if not isinstance(block, dict):
                    continue
                if block.get('type') != 'tool_use':
                    continue
                name = block.get('name', '')
                if 'zerops_recipe' not in name:
                    continue
                inp = block.get('input', {}) or {}
                if inp.get('action') != 'record-fragment':
                    continue
                frag_id = inp.get('fragmentId', '')
                if not claudemd_fragment_re.match(frag_id):
                    continue
                body = inp.get('fragment', '') or ''
                inspected += 1
                hits = []
                for pat, label in static_patterns:
                    if pat.search(body):
                        hits.append(label)
                for host in service_hostnames:
                    if re.search(rf'\b{re.escape(host)}\b', body):
                        hits.append(f'managed-service hostname `{host}`')
                if hits:
                    violations.append({
                        'jsonl': str(jsonl),
                        'line': lineno,
                        'fragmentId': frag_id,
                        'hits': hits,
                    })

if violations:
    print(f'FAIL: {len(violations)} claude-md fragment(s) with Zerops content in run {jsonl_dir.parent.name}')
    for v in violations:
        print(f"  {v['fragmentId']}  ({v['jsonl']}:{v['line']})")
        for h in v['hits']:
            print(f'    - {h}')
    print()
    print('CLAUDE.md is authored by a Zerops-free sub-agent. Zerops platform content')
    print('belongs in IG / KB / zerops.yaml comments, NOT CLAUDE.md.')
    sys.exit(1)

print(f'PASS: {inspected} claude-md fragment(s) inspected in run {jsonl_dir.parent.name}; zero Zerops content')
PY
