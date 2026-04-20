#!/usr/bin/env python3
"""extract_calibration_evidence.py — session-log parser for v35 calibration bars.

Provides targeted extractors for bars that need structured evidence from
the JSONL session log (zerops_workflow call/response trace): deploy-round
counts, substep-completion order, TodoWrite rewrite events, editorial-
review payload parsing.

Status: **SCAFFOLD**. C-14 lands the module structure + function signatures
so measure_calibration_bars.sh has stable integration points. Bar-specific
parsing logic is deferred to post-v35 when the first real session log is
captured — several heuristics (e.g. "full-rewrite" detection) require the
actual log format to calibrate against.

Usage (from measure_calibration_bars.sh):
    python3 extract_calibration_evidence.py <command> <session-log>

Commands (scaffolded — stubs return SKIP markers):
    deploy_rounds       Count deploy.readmes retry rounds
    finalize_rounds     Count finalize retry rounds
    substep_order       List out-of-order substep attestations
    todowrite_rewrites  Count TodoWrite full-rewrite events
    editorial_payload   Parse last close.editorial-review payload
"""
import json
import sys
from pathlib import Path


def load_session(path: str) -> list[dict]:
    """Read the session-log JSONL into a list of event dicts. Ignores
    malformed lines (best-effort parse — first v35 run may have a
    transcription format that doesn't pass strict JSON)."""
    events = []
    p = Path(path)
    if not p.exists():
        print(f"session log not found: {path}", file=sys.stderr)
        sys.exit(1)
    for raw in p.read_text(encoding="utf-8").splitlines():
        raw = raw.strip()
        if not raw:
            continue
        try:
            events.append(json.loads(raw))
        except json.JSONDecodeError:
            continue  # skip non-JSON lines (comments, prose)
    return events


def extract_deploy_rounds(events: list[dict]) -> dict:
    """C-1 evidence: count deploy.readmes substep-complete retry rounds.

    Post-v35 TODO: walk events for `step=deploy substep=readmes action=complete`
    with `Passed=false` — each such event is a retry round. Target: ≤2.
    """
    # TODO(post-v35): implement against real session-log format.
    return {"status": "SKIP", "reason": "post-v35 parser calibration required"}


def extract_finalize_rounds(events: list[dict]) -> dict:
    """C-2 evidence: count finalize retry rounds. Post-v35 TODO."""
    return {"status": "SKIP", "reason": "post-v35 parser calibration required"}


def extract_substep_order(events: list[dict]) -> dict:
    """C-6 evidence: list any out-of-order substep-complete attestations.

    Target: 0. The engine's Fix C + Fix D guards reject out-of-order
    attestations with SUBAGENT_MISUSE; this extractor cross-checks
    session-log observations against the unit-test guard coverage.
    """
    return {"status": "SKIP", "reason": "engine guards cover this via unit tests; cross-check deferred"}


def extract_todowrite_rewrites(events: list[dict]) -> dict:
    """C-11 evidence: count TodoWrite full-rewrite events.

    Post-v35 TODO: detect TodoWrite tool calls whose `todos` argument
    wholly replaces the prior list (vs. incremental updates). Target:
    0 full-rewrites per session.
    """
    return {"status": "SKIP", "reason": "post-v35 parser calibration required"}


def extract_editorial_payload(events: list[dict]) -> dict:
    """E-1..E-3 evidence: parse the last close.editorial-review payload.

    Post-v35 TODO: find the last `step=close substep=editorial-review
    action=complete` event, extract its attestation string, json.loads
    it, and return the parsed EditorialReviewReturn shape for downstream
    evaluators.
    """
    return {"status": "SKIP", "reason": "post-v35 parser calibration required"}


COMMANDS = {
    "deploy_rounds": extract_deploy_rounds,
    "finalize_rounds": extract_finalize_rounds,
    "substep_order": extract_substep_order,
    "todowrite_rewrites": extract_todowrite_rewrites,
    "editorial_payload": extract_editorial_payload,
}


def main() -> int:
    if len(sys.argv) < 3:
        print("Usage: extract_calibration_evidence.py <command> <session-log>", file=sys.stderr)
        print("", file=sys.stderr)
        print("Commands:", file=sys.stderr)
        for name in sorted(COMMANDS):
            print(f"  {name}", file=sys.stderr)
        return 2
    cmd, log = sys.argv[1], sys.argv[2]
    if cmd not in COMMANDS:
        print(f"unknown command: {cmd}", file=sys.stderr)
        return 2
    events = load_session(log)
    result = COMMANDS[cmd](events)
    print(json.dumps(result, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
