#!/usr/bin/env python3
"""Autonomous driver for the batch-2 weather-dashboard sweep.

Flow:
  1. Wait for batch 1 (multi-weather-*.log "Suite complete" marker) — no-op if already done
  2. Execute batch 2 (7 runtimes) sequentially via eval/scripts/run-scenario.sh
  3. After each scenario: run aggregate-weather-audit.py → snapshot audit report
  4. Retry once if grade.passed=false + log retry reason
  5. Hard cap: MAX_BUDGET seconds total, per-scenario 30 min already enforced by zcp eval
  6. Log every decision to eval/results/autonomous-driver-<tag>.log

Produces:
  eval/results/audit-multi-weather-<timestamp>.md    (updated after each scenario)
  eval/results/autonomous-driver-<tag>.log           (decision log)

Usage:
  ./eval/scripts/autonomous-weather-sweep.py        # waits for batch 1 then runs batch 2
  ./eval/scripts/autonomous-weather-sweep.py --no-wait   # start batch 2 immediately
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path


PROJECT_DIR = Path(__file__).resolve().parents[2]
RESULTS_DIR = PROJECT_DIR / "eval" / "results"
RUN_SCENARIO = PROJECT_DIR / "eval" / "scripts" / "run-scenario.sh"
AGGREGATOR = PROJECT_DIR / "eval" / "scripts" / "aggregate-weather-audit.py"

BATCH_2_SCENARIOS = [
    "internal/eval/scenarios/weather-dashboard-nextjs-ssr.md",
    "internal/eval/scenarios/weather-dashboard-dotnet.md",
    "internal/eval/scenarios/weather-dashboard-ruby.md",
    "internal/eval/scenarios/weather-dashboard-rust.md",
    "internal/eval/scenarios/weather-dashboard-bun.md",
    "internal/eval/scenarios/weather-dashboard-java.md",
    "internal/eval/scenarios/weather-dashboard-deno.md",
]

BATCH_1_SCENARIO_IDS = [
    "weather-dashboard-php-laravel",
    "weather-dashboard-nodejs",
    "weather-dashboard-python",
    "weather-dashboard-go",
]

# Per-scenario wall-clock hard cap (already enforced by `zcp eval scenario`
# via config.Timeout=30m — we keep the same here for clarity, the subprocess
# inherits it).
MAX_BUDGET_SECONDS = int(os.environ.get("DRIVER_MAX_BUDGET", 3 * 3600))
SCENARIO_POLL_INTERVAL = 15


def log(handle, message: str) -> None:
    stamp = datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
    line = f"[{stamp}] {message}"
    print(line, flush=True)
    if handle:
        handle.write(line + "\n")
        handle.flush()


def wait_for_batch_1(handle) -> None:
    """Polling-wait until batch 1 has produced result.json for every scenario ID.

    Only logs when the completion-count CHANGES to keep the event stream quiet.
    """
    log(handle, "waiting for batch 1 completion (polling every 15s)...")
    last_logged = -1
    while True:
        done = batch_1_completion_status()
        if done == len(BATCH_1_SCENARIO_IDS):
            log(handle, f"batch 1 complete — all {done}/{len(BATCH_1_SCENARIO_IDS)} results present")
            return
        if done != last_logged:
            log(handle, f"batch 1 progress: {done}/{len(BATCH_1_SCENARIO_IDS)} scenarios have results")
            last_logged = done
        time.sleep(SCENARIO_POLL_INTERVAL)


def batch_1_completion_status() -> int:
    """Count how many batch-1 scenarios have a result.json in the latest scenario-* dirs."""
    seen: set[str] = set()
    scenario_dirs = sorted(RESULTS_DIR.glob("scenario-*"), reverse=True)
    for sd in scenario_dirs:
        for child in sd.rglob("result.json"):
            # child path: scenario-<tag>/<suiteID>/<scenarioID>/result.json
            scenario_id = child.parent.name
            if scenario_id in BATCH_1_SCENARIO_IDS:
                seen.add(scenario_id)
    return len(seen)


def run_scenario(scenario_path: str, handle) -> dict:
    """Invoke run-scenario.sh and return the parsed result.json (or empty dict on failure)."""
    log(handle, f"launching {scenario_path}...")
    started = time.time()
    env = os.environ.copy()
    env.setdefault("EVAL_SKIP_BUILD", "1")

    proc = subprocess.run(
        [str(RUN_SCENARIO), scenario_path],
        cwd=str(PROJECT_DIR),
        env=env,
        capture_output=True,
        text=True,
    )
    elapsed = time.time() - started

    if proc.returncode != 0:
        log(handle, f"run-scenario.sh non-zero exit ({proc.returncode}) after {elapsed:.0f}s")

    scenario_id = Path(scenario_path).stem
    result = find_latest_result_for(scenario_id)
    if result is None:
        log(handle, f"{scenario_id}: NO result.json produced (elapsed={elapsed:.0f}s)")
        return {}
    log(handle, f"{scenario_id}: elapsed={elapsed:.0f}s, grade.passed={result.get('grade',{}).get('passed')}")
    return result


def find_latest_result_for(scenario_id: str) -> dict | None:
    """Find the MOST RECENT result.json for a given scenario ID across all scenario-* dirs."""
    best_path: Path | None = None
    best_mtime = 0.0
    for sd in RESULTS_DIR.glob("scenario-*"):
        for candidate in sd.rglob("result.json"):
            if candidate.parent.name != scenario_id:
                continue
            mtime = candidate.stat().st_mtime
            if mtime > best_mtime:
                best_mtime = mtime
                best_path = candidate
    if best_path is None:
        return None
    try:
        return json.loads(best_path.read_text())
    except (json.JSONDecodeError, OSError):
        return None


def run_aggregator(handle) -> Path | None:
    """Run the aggregator and return the path to the produced audit report."""
    log(handle, "running aggregator...")
    proc = subprocess.run(
        [sys.executable, str(AGGREGATOR)],
        cwd=str(PROJECT_DIR),
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        log(handle, f"aggregator non-zero exit ({proc.returncode}): {proc.stderr.strip()}")
        return None
    # Last line of stdout is the report path (by aggregator contract).
    lines = [ln for ln in proc.stdout.strip().splitlines() if ln.strip()]
    if not lines:
        return None
    report_path = Path(lines[-1])
    if not report_path.exists():
        return None
    log(handle, f"audit report refreshed: {report_path}")
    return report_path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--no-wait", action="store_true", help="skip batch 1 wait")
    args = parser.parse_args()

    tag = datetime.utcnow().strftime("%Y%m%d_%H%M%S")
    log_path = RESULTS_DIR / f"autonomous-driver-{tag}.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    handle = log_path.open("w")

    overall_started = time.time()
    try:
        log(handle, f"=== autonomous weather sweep driver (tag={tag}) ===")
        log(handle, f"log: {log_path}")

        if not args.no_wait:
            wait_for_batch_1(handle)
        else:
            log(handle, "--no-wait: skipping batch 1 wait")

        log(handle, f"starting batch 2: {len(BATCH_2_SCENARIOS)} scenarios")

        summary: list[dict] = []
        for idx, scenario in enumerate(BATCH_2_SCENARIOS, start=1):
            if time.time() - overall_started > MAX_BUDGET_SECONDS:
                log(handle, "HARD BUDGET EXCEEDED — aborting remaining scenarios")
                break

            log(handle, f"--- [{idx}/{len(BATCH_2_SCENARIOS)}] {scenario} ---")
            result = run_scenario(scenario, handle)
            passed = bool(result.get("grade", {}).get("passed"))
            summary.append({"scenario": scenario, "passed": passed, "attempt": 1})

            if not passed:
                reason = result.get("grade", {}).get("reasons") or result.get("error") or "unknown"
                log(handle, f"FAIL — reason: {reason}; retrying once")
                retry = run_scenario(scenario, handle)
                retry_passed = bool(retry.get("grade", {}).get("passed"))
                summary.append({"scenario": scenario, "passed": retry_passed, "attempt": 2})
                if not retry_passed:
                    log(handle, f"RETRY FAIL — moving on to next scenario (data still captured)")

            run_aggregator(handle)

        log(handle, "=== batch 2 complete ===")
        for row in summary:
            log(handle, f"  {row['scenario']} attempt={row['attempt']} passed={row['passed']}")

        # Final aggregator pass so the last report reflects ALL scenarios.
        final_report = run_aggregator(handle)
        if final_report:
            log(handle, f"FINAL audit: {final_report}")

        overall_elapsed = time.time() - overall_started
        log(handle, f"driver done in {overall_elapsed/60:.1f} min")
        return 0
    finally:
        handle.close()


if __name__ == "__main__":
    raise SystemExit(main())
