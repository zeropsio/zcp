#!/usr/bin/env python3
"""Aggregate multi-runtime weather-dashboard eval results into an audit report.

Reads the 4 scenario result dirs produced by run-multi-runtime-weather.sh and
emits a markdown audit report with:

- Per-runtime outcome (PASS/FAIL, duration, final URL status)
- Cross-runtime atom bucket matrix (which atoms are C in ≥2 runtimes — prune list)
- Friction frequency (issues cited by ≥2 runtimes — structural fixes)
- Timing variance (which runtimes take longest, where)
- Full per-runtime detail sections

Usage:
    ./eval/scripts/aggregate-weather-audit.py <suite-tag>
    # or
    ./eval/scripts/aggregate-weather-audit.py    # uses latest scenario-* dir

Input: eval/results/scenario-<tag>/<scenarioId>/{result.json,log.jsonl}
Output: eval/results/audit-multi-weather-<tag>.md
"""

from __future__ import annotations

import json
import re
import sys
from collections import Counter, defaultdict
from datetime import datetime
from pathlib import Path
from textwrap import dedent

RUNTIME_ORDER = [
    "php-laravel",
    "nodejs",
    "python",
    "go",
    "nextjs-ssr",
    "dotnet",
    "ruby",
    "rust",
    "bun",
    "java",
    "deno",
]
RESULTS_DIR = Path(__file__).resolve().parents[2] / "eval" / "results"


def find_suite_dirs(tag: str | None) -> list[Path]:
    """Find the scenario-* result dirs for the target suite.

    A run-multi-runtime-weather.sh invocation produces ONE scenario-<tag>
    dir per scenario (run-scenario.sh makes its own timestamp). We pick
    the 4 most-recent dirs whose scenario id starts with weather-dashboard-.
    """
    if tag:
        candidates = sorted(RESULTS_DIR.glob(f"scenario-{tag}*"))
    else:
        candidates = sorted(RESULTS_DIR.glob("scenario-*"))
    if not candidates:
        return []

    # Actual on-disk layout:
    #   eval/results/scenario-<tag>/<suiteID>/<scenarioID>/result.json
    # Multiple result.json instances often exist for the same scenarioID
    # because run-scenario.sh SCPs the WHOLE remote /var/www/.zcp/eval/
    # results tree back every invocation (so old runs accumulate). And the
    # driver retry logic re-runs a scenario, producing yet more files.
    #
    # Pick the BEST result per scenarioID: passed=True wins over False,
    # then most-recent mtime as tiebreaker. This gives the scenario its
    # fairest shot — if any attempt succeeded, count it as a pass.
    by_scenario: dict[str, tuple[int, float, Path]] = {}
    for d in reversed(candidates):
        for result_json in d.rglob("result.json"):
            scenario_dir = result_json.parent
            if not scenario_dir.name.startswith("weather-dashboard-"):
                continue
            runtime = scenario_dir.name.removeprefix("weather-dashboard-")
            try:
                payload = json.loads(result_json.read_text())
            except (json.JSONDecodeError, OSError):
                continue
            passed_rank = 1 if payload.get("grade", {}).get("passed") else 0
            mtime = result_json.stat().st_mtime
            prior = by_scenario.get(runtime)
            if prior is None or (passed_rank, mtime) > (prior[0], prior[1]):
                by_scenario[runtime] = (passed_rank, mtime, scenario_dir)

    out = []
    for r in RUNTIME_ORDER:
        if r in by_scenario:
            out.append(by_scenario[r][2])
    for r in sorted(by_scenario):
        if r not in RUNTIME_ORDER:
            out.append(by_scenario[r][2])
    return out


def extract_assessment(log_path: Path) -> str:
    """Pull the last `## EVAL REPORT` block from a stream-json log."""
    if not log_path.exists():
        return ""
    last_report = ""
    with log_path.open() as fh:
        for raw in fh:
            raw = raw.strip()
            if not raw:
                continue
            try:
                msg = json.loads(raw)
            except json.JSONDecodeError:
                continue
            if msg.get("type") != "assistant":
                continue
            content = msg.get("message", {}).get("content") or []
            for block in content:
                if not isinstance(block, dict) or block.get("type") != "text":
                    continue
                text = block.get("text", "")
                idx = text.find("## EVAL REPORT")
                if idx >= 0:
                    last_report = text[idx:]
    return last_report


BUCKET_RE = re.compile(r"-\s*id:\s*([A-Za-z0-9_-]+).*?\n\s*bucket:\s*([ABC])", re.DOTALL)


def parse_bucket_block(report: str) -> list[tuple[str, str, str]]:
    """Extract (atom_id, bucket, note) triples from the atom classification block."""
    out: list[tuple[str, str, str]] = []
    if not report:
        return out
    marker = report.find("Atom bucket classification")
    if marker < 0:
        marker = 0
    section = report[marker:]
    for match in re.finditer(
        r"-\s*id:\s*([A-Za-z0-9_-]+)\s*\n\s*bucket:\s*([ABC])(?:\s*\n\s*note:\s*\"?([^\"\n]*)\"?)?",
        section,
    ):
        atom_id = match.group(1).strip()
        bucket = match.group(2).strip()
        note = (match.group(3) or "").strip()
        out.append((atom_id, bucket, note))
    return out


def parse_friction_block(report: str) -> list[dict]:
    """Extract friction entries from agent's YAML-ish block."""
    out: list[dict] = []
    if not report:
        return out
    marker = report.find("friction:")
    if marker < 0:
        return out
    section = report[marker:]
    end = re.search(r"\n\s*(timing_minutes:|###|\Z)", section)
    if end:
        section = section[: end.start()]
    for match in re.finditer(
        r"-\s*area:\s*\"?([^\"\n]+)\"?\s*\n\s*cost_minutes:\s*(\d+)\s*\n\s*suggestion:\s*\"?([^\"\n]+)\"?",
        section,
    ):
        out.append(
            {
                "area": match.group(1).strip(),
                "cost_minutes": int(match.group(2)),
                "suggestion": match.group(3).strip(),
            }
        )
    return out


def parse_timing_block(report: str) -> dict[str, int]:
    """Extract {phase: minutes} from the agent's timing_minutes block."""
    out: dict[str, int] = {}
    if not report:
        return out
    marker = report.find("timing_minutes:")
    if marker < 0:
        return out
    section = report[marker : marker + 500]
    for match in re.finditer(r"(\w+):\s*(\d+)", section):
        key = match.group(1)
        if key == "timing_minutes":
            continue
        out[key] = int(match.group(2))
    return out


def load_result(result_json: Path) -> dict:
    if not result_json.exists():
        return {}
    try:
        return json.loads(result_json.read_text())
    except json.JSONDecodeError:
        return {}


def render_audit(runs: list[dict]) -> str:
    """Produce markdown audit report from per-runtime parsed data."""
    now = datetime.utcnow().strftime("%Y-%m-%d %H:%M UTC")

    # Cross-runtime atom bucket matrix.
    all_atoms: set[str] = set()
    per_runtime_buckets: dict[str, dict[str, str]] = {}
    per_runtime_notes: dict[str, dict[str, str]] = {}
    for r in runs:
        runtime = r["runtime"]
        per_runtime_buckets[runtime] = {}
        per_runtime_notes[runtime] = {}
        for atom_id, bucket, note in r["buckets"]:
            all_atoms.add(atom_id)
            per_runtime_buckets[runtime][atom_id] = bucket
            per_runtime_notes[runtime][atom_id] = note

    def atom_key(atom: str) -> tuple[int, str]:
        c_count = sum(1 for rt in per_runtime_buckets if per_runtime_buckets[rt].get(atom) == "C")
        return (-c_count, atom)

    ordered_atoms = sorted(all_atoms, key=atom_key)

    # Friction aggregation.
    friction_by_area: Counter = Counter()
    friction_suggestions: dict[str, list[str]] = defaultdict(list)
    for r in runs:
        for f in r["friction"]:
            key = f["area"].lower().strip().strip(".")
            friction_by_area[key] += 1
            friction_suggestions[key].append(f"[{r['runtime']}] {f['suggestion']}")

    # Build report.
    lines: list[str] = []
    lines.append(f"# Multi-runtime weather-dashboard audit — {now}")
    lines.append("")
    lines.append(f"Evals: {len(runs)} runtimes  •  binary: post-v9.9.0  •  task: weather dashboard (favorite cities + DB + Open-Meteo)")
    lines.append("")

    # Executive summary.
    passes = sum(1 for r in runs if r["pass"])
    lines.append("## Executive summary")
    lines.append("")
    lines.append(f"- Pass rate: {passes}/{len(runs)}")
    if ordered_atoms:
        c_ge_2 = [a for a in ordered_atoms
                  if sum(1 for rt in per_runtime_buckets if per_runtime_buckets[rt].get(a) == "C") >= 2]
        lines.append(f"- Atoms flagged C in ≥2 runtimes (prune candidates): {len(c_ge_2)}")
        if c_ge_2:
            lines.append(f"  - {', '.join(c_ge_2)}")
    if friction_by_area:
        cross_friction = [a for a, c in friction_by_area.items() if c >= 2]
        lines.append(f"- Friction cited by ≥2 runtimes: {len(cross_friction)}")
    lines.append("")

    # Outcome table.
    lines.append("## Per-runtime outcome")
    lines.append("")
    lines.append("| Runtime | Grade | Duration (min) | Final URL | Atoms seen | A | B | C |")
    lines.append("|---|---|---|---|---|---|---|---|")
    for r in runs:
        buckets = [b for _, b, _ in r["buckets"]]
        lines.append(
            f"| {r['runtime']} | {'PASS' if r['pass'] else 'FAIL'} | "
            f"{r.get('duration_min', '—')} | {r.get('final_url_status', '—')} | "
            f"{len(buckets)} | {buckets.count('A')} | {buckets.count('B')} | {buckets.count('C')} |"
        )
    lines.append("")

    # Atom matrix.
    if ordered_atoms:
        lines.append("## Atom bucket matrix")
        lines.append("")
        lines.append(
            "Atoms sorted by C-count across runtimes (most-likely-noise first). "
            "Empty cell = atom didn't fire for that runtime (or agent didn't report it)."
        )
        lines.append("")
        header = "| Atom ID | " + " | ".join(RUNTIME_ORDER) + " | C-count |"
        lines.append(header)
        lines.append("|" + "---|" * (len(RUNTIME_ORDER) + 2))
        for atom in ordered_atoms:
            c_count = sum(1 for rt in per_runtime_buckets if per_runtime_buckets[rt].get(atom) == "C")
            cells = []
            for rt in RUNTIME_ORDER:
                b = per_runtime_buckets.get(rt, {}).get(atom, "")
                cells.append(b)
            lines.append(f"| `{atom}` | " + " | ".join(cells) + f" | {c_count} |")
        lines.append("")

    # Friction.
    if friction_by_area:
        lines.append("## Friction frequency")
        lines.append("")
        lines.append("| Count | Area | Suggestions |")
        lines.append("|---|---|---|")
        for area, count in friction_by_area.most_common():
            sugs = "; ".join(friction_suggestions[area][:3])
            lines.append(f"| {count} | {area} | {sugs} |")
        lines.append("")

    # Timing.
    lines.append("## Timing breakdown (agent self-report)")
    lines.append("")
    timing_phases = set()
    for r in runs:
        timing_phases.update(r["timing"].keys())
    phases = sorted(timing_phases, key=lambda p: ("total" in p, p))
    if phases:
        lines.append("| Runtime | " + " | ".join(phases) + " |")
        lines.append("|" + "---|" * (len(phases) + 1))
        for r in runs:
            cells = [r["timing"].get(p, "—") for p in phases]
            cells_str = [str(c) for c in cells]
            lines.append(f"| {r['runtime']} | " + " | ".join(cells_str) + " |")
        lines.append("")

    # Per-runtime detail.
    lines.append("## Per-runtime detail")
    for r in runs:
        lines.append("")
        lines.append(f"### {r['runtime']}")
        lines.append("")
        lines.append(f"- Grade: {'PASS' if r['pass'] else 'FAIL'}")
        if r.get("grade_reasons"):
            for reason in r["grade_reasons"]:
                lines.append(f"  - {reason}")
        if r["friction"]:
            lines.append("- Friction cited:")
            for f in r["friction"]:
                lines.append(f"  - **{f['area']}** ({f['cost_minutes']} min) — {f['suggestion']}")

    return "\n".join(lines)


def main() -> int:
    tag = sys.argv[1] if len(sys.argv) > 1 else None

    suite_dirs = find_suite_dirs(tag)
    if not suite_dirs:
        print("No weather-dashboard result dirs found under eval/results/", file=sys.stderr)
        return 2

    runs: list[dict] = []
    for sd in suite_dirs:
        runtime = sd.name.removeprefix("weather-dashboard-")
        result = load_result(sd / "result.json")

        log_path = None
        for candidate in ("log.jsonl", "agent.log", "run.jsonl"):
            if (sd / candidate).exists():
                log_path = sd / candidate
                break

        report = extract_assessment(log_path) if log_path else ""
        buckets = parse_bucket_block(report)
        friction = parse_friction_block(report)
        timing = parse_timing_block(report)

        grade = result.get("grade") or {}
        grade_reasons = grade.get("reasons") or []
        # ScenarioResult.Grade serializes as `passed` (Go bool tag), not `pass`.
        pass_ok = bool(grade.get("passed"))

        final_url = result.get("finalUrl") or {}

        runs.append(
            {
                "runtime": runtime,
                "pass": pass_ok,
                "grade_reasons": grade_reasons,
                "duration_min": _minutes_from_duration(result.get("duration")),
                # FinalURLProbe serializes the actual HTTP status as `got`.
                "final_url_status": final_url.get("got") or final_url.get("status") or "—",
                "buckets": buckets,
                "friction": friction,
                "timing": timing,
                "report": report,
                "source_dir": str(sd),
            }
        )

    audit = render_audit(runs)
    stamp = datetime.utcnow().strftime("%Y%m%d_%H%M%S")
    out_path = RESULTS_DIR / f"audit-multi-weather-{stamp}.md"
    out_path.write_text(audit)
    print(out_path)
    return 0


def _minutes_from_duration(dur) -> str:
    if not dur:
        return "—"
    # Duration is serialized as a human string (e.g. "8m32s") by the runner.
    if isinstance(dur, str):
        m = re.match(r"(\d+)m", dur)
        if m:
            return m.group(1)
    return str(dur)


if __name__ == "__main__":
    raise SystemExit(main())
