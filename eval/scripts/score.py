#!/usr/bin/env python3
"""Score an eval run from extracted tool calls.

Reads tools_N.json (output of extract-tool-calls.py), produces structured
score with verdict, metrics, waste breakdown, and flags.

Usage:
    python3 score.py tools.json                    # Print to stdout
    python3 score.py tools.json -o score.json      # Write to file
    python3 score.py tools.json --type functional   # Force scenario type
"""

import argparse
import json
import sys


# Tools that are pure overhead / waste when called
WASTE_TOOLS = {"TodoWrite", "TodoRead", "Task", "TaskCreate", "TaskUpdate", "TaskList"}

# Expected maximum event polls for a clean run
MAX_CLEAN_EVENT_POLLS = 5


def detect_scenario_type(data):
    """Detect whether this is a functional or import-only scenario."""
    tools_used = {tc["tool"] for tc in data.get("tool_calls", [])}
    if "zerops_deploy" in tools_used:
        return "functional"
    return "import"


def count_event_polls(tool_calls):
    """Count zerops_events calls."""
    return sum(1 for tc in tool_calls if tc["tool"] == "zerops_events")


def count_deploy_attempts(tool_calls):
    """Count zerops_deploy calls."""
    return sum(1 for tc in tool_calls if tc["tool"] == "zerops_deploy")


def count_knowledge_calls(data):
    """Count knowledge calls by mode (query, briefing, recipe)."""
    queries = len(data.get("knowledge_queries", []))
    briefings = len(data.get("knowledge_briefings", []))
    return queries, briefings


def count_workflow_actions(data):
    """Count workflow engine actions used."""
    return len(data.get("workflow_actions", []))


def count_waste(tool_calls):
    """Count and categorize wasted tool calls."""
    breakdown = {}
    total = 0

    for tc in tool_calls:
        tool = tc["tool"]
        if tool in WASTE_TOOLS:
            breakdown[tool] = breakdown.get(tool, 0) + 1
            total += 1

    # Excess event polls (beyond MAX_CLEAN_EVENT_POLLS)
    event_polls = count_event_polls(tool_calls)
    excess_polls = max(0, event_polls - MAX_CLEAN_EVENT_POLLS)
    if excess_polls > 0:
        breakdown["excess_event_polls"] = excess_polls
        total += excess_polls

    # Redundant subdomain calls (more than 1)
    subdomain_calls = sum(1 for tc in tool_calls if tc["tool"] == "zerops_subdomain")
    if subdomain_calls > 1:
        redundant = subdomain_calls - 1
        breakdown["redundant_subdomain"] = redundant
        total += redundant

    return total, breakdown


def score(data, scenario_type=None):
    """Score an eval run."""
    tool_calls = data.get("tool_calls", [])
    real_errors = data.get("errors", [])
    retries = data.get("retries", [])

    if scenario_type is None:
        scenario_type = detect_scenario_type(data)

    total_calls = len(tool_calls)
    event_polls = count_event_polls(tool_calls)
    deploy_attempts = count_deploy_attempts(tool_calls)
    waste_calls, waste_breakdown = count_waste(tool_calls)
    useful_calls = total_calls - waste_calls
    knowledge_queries, knowledge_briefings = count_knowledge_calls(data)
    workflow_action_count = count_workflow_actions(data)

    efficiency = useful_calls / total_calls if total_calls > 0 else 1.0

    # Determine verdict
    flags = []

    if event_polls > MAX_CLEAN_EVENT_POLLS:
        flags.append("EXCESSIVE_POLLING")

    if deploy_attempts > 1:
        flags.append("BUILD_RETRY")

    if any(tc["tool"] in WASTE_TOOLS for tc in tool_calls):
        flags.append("TASK_MANAGEMENT_OVERHEAD")

    if len(real_errors) > 0:
        flags.append("API_ERRORS")

    if len(retries) > 2:
        flags.append("EXCESSIVE_RETRIES")

    # Verdict based on scenario type
    if scenario_type == "import":
        if len(real_errors) == 0 and len(retries) == 0 and total_calls <= 12:
            verdict = "PASS"
        elif total_calls <= 20 and len(real_errors) <= 1:
            verdict = "WARN"
        else:
            verdict = "FAIL"
    else:  # functional
        # Check for build failure
        build_failed = any(
            "FAILED" in str(tc.get("result", ""))
            for tc in tool_calls
            if tc["tool"] == "zerops_events"
        )

        if build_failed and deploy_attempts <= 1:
            verdict = "FAIL"
            flags.append("BUILD_FAILED")
        elif total_calls <= 40 and len(real_errors) == 0:
            verdict = "PASS"
        elif total_calls <= 60 or len(real_errors) <= 1:
            verdict = "WARN"
        else:
            verdict = "FAIL"

        if total_calls > 75:
            verdict = "FAIL"
            if "EXCESSIVE_CALLS" not in flags:
                flags.append("EXCESSIVE_CALLS")

    return {
        "verdict": verdict,
        "scenario_type": scenario_type,
        "metrics": {
            "total_calls": total_calls,
            "useful_calls": useful_calls,
            "waste_calls": waste_calls,
            "real_errors": len(real_errors),
            "event_polls": event_polls,
            "deploy_attempts": deploy_attempts,
            "retries": len(retries),
            "efficiency_ratio": round(efficiency, 2),
            "knowledge_queries": knowledge_queries,
            "knowledge_briefings": knowledge_briefings,
            "workflow_actions": workflow_action_count,
        },
        "waste_breakdown": waste_breakdown,
        "flags": flags,
    }


def print_summary(result):
    """Print human-readable score summary to stderr."""
    v = result["verdict"]
    m = result["metrics"]
    st = result["scenario_type"]

    icon = {"PASS": "OK", "WARN": "!!", "FAIL": "XX"}[v]
    print(f"[{icon}] {v} ({st})", file=sys.stderr)
    print(f"  Calls: {m['total_calls']} total, {m['useful_calls']} useful, {m['waste_calls']} waste", file=sys.stderr)
    print(f"  Efficiency: {m['efficiency_ratio']:.0%}", file=sys.stderr)
    print(f"  Errors: {m['real_errors']}, Retries: {m['retries']}, Event polls: {m['event_polls']}", file=sys.stderr)
    print(f"  Knowledge: {m['knowledge_queries']} queries, {m['knowledge_briefings']} briefings, {m['workflow_actions']} workflow actions", file=sys.stderr)

    if result["flags"]:
        print(f"  Flags: {', '.join(result['flags'])}", file=sys.stderr)

    if result["waste_breakdown"]:
        parts = [f"{k}={v}" for k, v in result["waste_breakdown"].items()]
        print(f"  Waste: {', '.join(parts)}", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(description="Score an eval run from extracted tool calls")
    parser.add_argument("input", help="Path to tools.json (output of extract-tool-calls.py)")
    parser.add_argument("-o", "--output", help="Write score JSON to file (default: stdout)")
    parser.add_argument("--type", choices=["functional", "import"],
                        help="Force scenario type (default: auto-detect)")
    parser.add_argument("-q", "--quiet", action="store_true",
                        help="Suppress human-readable summary on stderr")
    args = parser.parse_args()

    with open(args.input) as f:
        data = json.load(f)

    result = score(data, scenario_type=args.type)

    if not args.quiet:
        print_summary(result)

    if args.output:
        with open(args.output, "w") as f:
            json.dump(result, f, indent=2)
            f.write("\n")
    else:
        json.dump(result, sys.stdout, indent=2)
        print()


if __name__ == "__main__":
    main()
