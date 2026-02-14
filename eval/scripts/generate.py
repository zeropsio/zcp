#!/usr/bin/env python3
"""Generate a random Zerops scenario from taxonomy.yaml.

Picks a random task type, selects runtimes/services/modifiers,
and renders a natural-language prompt for Claude to execute.

Usage:
    python3 generate.py                    # Random scenario
    python3 generate.py --seed 42          # Reproducible
    python3 generate.py --task full-stack  # Force task type
"""

import argparse
import random
import sys
from pathlib import Path

import yaml


def load_taxonomy():
    taxonomy_path = Path(__file__).parent.parent / "taxonomy.yaml"
    with open(taxonomy_path) as f:
        return yaml.safe_load(f)


def pick_runtime(tax, prefer_uncommon=False):
    """Pick a random runtime with version."""
    runtimes = tax["runtimes"]
    if prefer_uncommon:
        # Prefer less common runtimes
        uncommon = [r for r in runtimes if r["name"] in (
            "elixir", "rust", "dotnet", "java", "static", "bun"
        )]
        pool = uncommon if uncommon else runtimes
    else:
        pool = runtimes
    rt = random.choice(pool)
    ver = random.choice(rt["versions"])
    return rt["name"], ver


def pick_service(tax, service_filter=None):
    """Pick a random managed service with version."""
    services = tax["services"]
    if service_filter:
        services = [s for s in services if s["name"] in service_filter]
    svc = random.choice(services)
    ver = random.choice(svc["versions"])
    return svc["name"], ver


def generate_hostname(prefix, name):
    """Generate eval-prefixed hostname (a-z, 0-9 only, no hyphens)."""
    suffix = random.randint(100, 999)
    clean_name = name.replace("-", "").replace("_", "")
    return f"eval{prefix}{clean_name}{suffix}"


def build_scenario(tax, task_type_name=None):
    """Build a complete scenario from taxonomy."""
    task_types = tax["task_types"]

    if task_type_name:
        task = next((t for t in task_types if t["name"] == task_type_name), None)
        if not task:
            print(f"Unknown task type: {task_type_name}", file=sys.stderr)
            sys.exit(1)
    else:
        task = random.choice(task_types)

    prefer_uncommon = task.get("prefer_uncommon", False)
    service_filter = task.get("service_filter")
    forced_modifiers = task.get("modifiers", [])
    focus = task.get("focus")

    # Pick runtimes
    n_runtimes = random.randint(task["min_runtimes"], task["max_runtimes"])
    runtimes = []
    seen_names = set()
    for _ in range(n_runtimes):
        for _attempt in range(10):
            name, ver = pick_runtime(tax, prefer_uncommon)
            if name not in seen_names:
                seen_names.add(name)
                runtimes.append((name, ver))
                break

    # Pick services
    n_services = random.randint(task["min_services"], task["max_services"])
    services = []
    seen_svc = set()
    for _ in range(n_services):
        for _attempt in range(10):
            name, ver = pick_service(tax, service_filter)
            if name not in seen_svc:
                seen_svc.add(name)
                services.append((name, ver))
                break

    # Pick modifiers
    if forced_modifiers:
        modifiers = forced_modifiers
    else:
        available = tax["modifiers"]
        n_mods = random.randint(0, min(2, len(available)))
        modifiers = random.sample(available, n_mods)

    return {
        "task_type": task["name"],
        "runtimes": runtimes,
        "services": services,
        "modifiers": modifiers,
        "focus": focus,
    }


def render_prompt(scenario, tax):
    """Render a natural-language prompt from a scenario."""
    parts = []

    # Describe runtimes
    for name, ver in scenario["runtimes"]:
        hostname = generate_hostname("app", name)
        if ver and ver != "latest":
            parts.append(f"{name} {ver} (hostname: {hostname})")
        else:
            parts.append(f"{name} (hostname: {hostname})")

    # Describe services
    for name, ver in scenario["services"]:
        hostname = generate_hostname("svc", name)
        if ver:
            parts.append(f"{name} {ver} (hostname: {hostname})")
        else:
            parts.append(f"{name} (hostname: {hostname})")

    # Build task description
    if len(parts) == 1:
        task_desc = f"a {parts[0]} service"
    elif len(parts) == 2:
        task_desc = f"{parts[0]} with {parts[1]}"
    else:
        task_desc = ", ".join(parts[:-1]) + f", and {parts[-1]}"

    # Add modifiers
    mod_parts = []
    for mod in scenario["modifiers"]:
        if mod == "ha-mode":
            mod_parts.append("Enable HA mode (multiple containers) for the runtime service.")
        elif mod == "subdomain":
            mod_parts.append("Enable the zerops.app subdomain for the runtime service.")
        elif mod == "priority-ordering":
            mod_parts.append("Set service start priorities so databases start before runtimes.")
        elif mod == "unsupported-request":
            mod_parts.append("Also try to add a Redis service (note: Zerops uses Valkey, not Redis).")

    # Add focus
    focus_parts = []
    if scenario["focus"] == "env-vars":
        focus_parts.append(
            "Make sure all services are properly wired with environment variables. "
            "The runtime should have connection strings for all managed services."
        )

    # Pick template
    template = random.choice(tax["prompt_templates"])
    prompt = template.format(task_description=task_desc)

    if mod_parts:
        prompt += "\n\n" + "\n".join(mod_parts)

    if focus_parts:
        prompt += "\n\n" + "\n".join(focus_parts)

    return prompt


def main():
    parser = argparse.ArgumentParser(description="Generate random Zerops eval scenario")
    parser.add_argument("--seed", type=int, help="Random seed for reproducibility")
    parser.add_argument("--task", type=str, help="Force specific task type")
    args = parser.parse_args()

    if args.seed is not None:
        random.seed(args.seed)

    tax = load_taxonomy()
    scenario = build_scenario(tax, args.task)
    prompt = render_prompt(scenario, tax)

    print(prompt)


if __name__ == "__main__":
    main()
