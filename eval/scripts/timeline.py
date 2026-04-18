#!/usr/bin/env python3
"""Generate an event-ordered timeline of a recipe run.

Reads main-session.jsonl + all subagent *.jsonl files under SESSIONS_LOGS/
and emits a chronological, cross-stream timeline of tool calls, text
reasoning, and errors. Each tool call is annotated with its source stream
(MAIN / subagent ID or description), latency (for MCP/Bash), and error flag.

Intended for running the same shape of analysis across every version —
eliminates the ad-hoc python-on-stdin pattern used during the v27
post-mortem.

USAGE

    python3 eval/scripts/timeline.py <run-dir>

        Where <run-dir> is /path/to/nestjs-showcase-v{N}/ — the script finds
        SESSIONS_LOGS/ and its contents automatically.

OUTPUT OPTIONS

    --phase         Print inferred workflow phases as section headers
                    (Research / Provision / Generate / Deploy / Finalize / Close).
    --after TS      Only events at or after TS (ISO, e.g. 2026-04-18T09:29:40).
    --before TS     Only events at or before TS.
    --source S      Filter to one source (MAIN | sub-NNN). Repeatable.
    --tool T        Filter to one tool name. Repeatable. Supports mcp__zerops__*
                    shorthand (e.g. --tool deploy matches mcp__zerops__zerops_deploy).
    --no-text       Suppress assistant text blocks (tool calls only).
    --json          Emit JSON instead of human-readable lines.
    --stats         Append a summary block (durations per phase, per tool,
                    error counts, subagent walls).

Designed for piping into grep / less / cutting a slice for inclusion in
post-mortems. The default output is grep-friendly: one event per line,
fixed-width timestamp + source + detail.

Example: emit the post-feature slice for a v-entry post-mortem:

    python3 eval/scripts/timeline.py /path/to/nestjs-showcase-v27 \\
        --after 2026-04-18T09:29:30 --no-text --phase > v27-post-feature.log
"""

import argparse
import json
import os
import sys
from pathlib import Path
from collections import defaultdict


PHASE_MARKERS = [
    # (phase_name, detector_fn)
    ("START",    lambda d: 'action=start' in d and 'workflow=recipe' in d),
    ("RESEARCH", lambda d: 'step=research' in d),
    ("PROVISION",lambda d: 'step=provision' in d),
    ("GENERATE", lambda d: 'step=generate' in d),
    ("DEPLOY",   lambda d: 'step=deploy' in d),
    ("FINALIZE", lambda d: 'step=finalize' in d or 'generate-finalize' in d),
    ("CLOSE",    lambda d: 'step=close' in d),
]


def short_detail(name, inp):
    """Compact, one-line rendering of a tool_use call."""
    if not isinstance(inp, dict):
        inp = {}
    if name == 'Bash':
        cmd = (inp.get('command', '') or '')[:100]
        return f"BASH  $ {cmd}"
    if name == 'Write':
        p = (inp.get('file_path', '') or '').replace('/var/www/', '')
        return f"WRITE {p}"
    if name == 'Edit':
        p = (inp.get('file_path', '') or '').replace('/var/www/', '')
        return f"EDIT  {p}"
    if name == 'Read':
        p = (inp.get('file_path', '') or '').replace('/var/www/', '')
        return f"READ  {p}"
    if name == 'Grep':
        return f"GREP  {(inp.get('pattern', '') or '')[:60]}"
    if name == 'TodoWrite':
        return f"TODO  ({len(inp.get('todos', []))} items)"
    if name == 'Agent':
        return f"AGENT dispatch: {(inp.get('description', '') or '')[:60]}"
    if name.startswith('mcp__zerops__'):
        tool = name.replace('mcp__zerops__', '')
        args = []
        for k in ('action', 'step', 'substep', 'workflow', 'hostname', 'serviceHostname',
                 'targetService', 'featureId', 'setup', 'topic', 'query', 'recipe'):
            if k in inp:
                args.append(f"{k}={inp[k]}")
        return f"MCP   {tool}  {' '.join(args[:4])}"
    return name


def ingest_stream(path, source, after=None, before=None, want_text=True):
    """Parse one *.jsonl session file into (ts, source, kind, detail, duration_ms, is_error) tuples."""
    events = []
    tool_uses = {}  # id → (ts, detail)
    with open(path) as fp:
        for line in fp:
            try:
                e = json.loads(line)
            except Exception:
                continue
            ts = e.get('timestamp', '')[:19]
            if after and ts < after:
                continue
            if before and ts > before:
                continue
            typ = e.get('type', '')
            if typ == 'assistant':
                msg = e.get('message', {}) or {}
                contents = msg.get('content', []) or []
                for c in contents:
                    if not isinstance(c, dict):
                        continue
                    if c.get('type') == 'tool_use':
                        n = c.get('name', '')
                        inp = c.get('input', {}) or {}
                        tid = c.get('id', '')
                        d = short_detail(n, inp)
                        tool_uses[tid] = (ts, d, n)
                        events.append({'ts': ts, 'source': source, 'kind': 'TU',
                                       'detail': d, 'tool_id': tid, 'tool_name': n})
                    elif c.get('type') == 'text' and want_text:
                        txt = (c.get('text', '') or '').strip()
                        if txt and len(txt) > 15:
                            events.append({'ts': ts, 'source': source, 'kind': 'TX',
                                           'detail': 'TEXT  > ' + txt[:150].replace('\n', ' ')})
            elif typ == 'user':
                msg = e.get('message', {}) or {}
                contents = msg.get('content', []) or []
                if not isinstance(contents, list):
                    continue
                for c in contents:
                    if not isinstance(c, dict):
                        continue
                    if c.get('type') == 'tool_result':
                        tid = c.get('tool_use_id', '')
                        if tid not in tool_uses:
                            continue
                        call_ts, call_detail, call_name = tool_uses[tid]
                        # Compute latency (seconds).
                        try:
                            from datetime import datetime
                            t0 = datetime.fromisoformat(call_ts)
                            t1 = datetime.fromisoformat(ts)
                            duration_ms = int((t1 - t0).total_seconds() * 1000)
                        except Exception:
                            duration_ms = 0
                        is_err = bool(c.get('is_error', False))
                        cont = c.get('content', '')
                        if isinstance(cont, list):
                            txt = '\n'.join(x.get('text', '') for x in cont if isinstance(x, dict))
                        else:
                            txt = str(cont)
                        size = len(txt)
                        if is_err:
                            events.append({'ts': ts, 'source': source, 'kind': 'TE',
                                           'detail': '  ⚠ERR (' + str(size) + 'B, ' +
                                           f"{duration_ms}ms) " + txt[:180].replace('\n', ' '),
                                           'tool_id': tid, 'tool_name': call_name,
                                           'duration_ms': duration_ms})
                        else:
                            # Only surface tool_result metadata; detailed content too noisy.
                            events.append({'ts': ts, 'source': source, 'kind': 'TR',
                                           'detail': '  ↳ (' + str(size) + 'B, ' +
                                           f"{duration_ms}ms)",
                                           'tool_id': tid, 'tool_name': call_name,
                                           'response_size': size, 'duration_ms': duration_ms})
    return events


def discover_streams(run_dir):
    """Find all *.jsonl session files under SESSIONS_LOGS/."""
    logs_dir = Path(run_dir) / 'SESSIONS_LOGS'
    if not logs_dir.exists():
        sys.exit(f"No SESSIONS_LOGS/ under {run_dir}")
    streams = []
    # Main session — canonical name is main-session.jsonl, some v-entries
    # use {slug}-session.jsonl (e.g. v8). Accept either.
    for p in sorted(logs_dir.glob('*.jsonl')):
        streams.append((p, 'MAIN'))
        break  # first match wins — main is the top-level .jsonl
    # Subagents — shorten the agent ID to 3 chars for readability.
    sub_dir = logs_dir / 'subagents'
    if sub_dir.exists():
        for p in sorted(sub_dir.glob('agent-*.jsonl')):
            agent_id = p.stem.replace('agent-', '')[:3]
            streams.append((p, f'SUB-{agent_id}'))
    return streams


def matches_tool_filter(event, filters):
    if not filters:
        return True
    name = event.get('tool_name', '')
    for f in filters:
        if f in name:
            return True
    return False


def human_render(events, show_phase=True):
    current_phase = None
    for e in events:
        if show_phase and e['kind'] == 'TU':
            for phase, detector in PHASE_MARKERS:
                if detector(e['detail']):
                    if phase != current_phase:
                        print()
                        print(f"═══ {phase} ═══")
                        current_phase = phase
                    break
        print(f"{e['ts']}  [{e['source']:8s}] {e['detail']}")


def emit_stats(events):
    print()
    print("═══ STATS ═══")
    # Per-tool histogram
    tool_counts = defaultdict(int)
    tool_duration_ms = defaultdict(int)
    err_counts = defaultdict(int)
    for e in events:
        if e['kind'] == 'TU':
            tool_counts[e.get('tool_name', '?')] += 1
        elif e['kind'] == 'TR':
            tool_duration_ms[e.get('tool_name', '?')] += e.get('duration_ms', 0)
        elif e['kind'] == 'TE':
            err_counts[e.get('tool_id', '?')] += 1
    print("Tool call histogram:")
    for name, count in sorted(tool_counts.items(), key=lambda x: -x[1]):
        dur_s = tool_duration_ms.get(name, 0) / 1000
        print(f"  {count:4d}  {name:40s}  ({dur_s:.1f}s total latency)")
    total_err = sum(err_counts.values())
    if total_err:
        print(f"\nErrored tool calls: {total_err}")

    # Wall per source
    wall_by_source = defaultdict(lambda: ('', ''))
    for e in events:
        src = e['source']
        existing_first, existing_last = wall_by_source[src]
        if not existing_first or e['ts'] < existing_first:
            existing_first = e['ts']
        if not existing_last or e['ts'] > existing_last:
            existing_last = e['ts']
        wall_by_source[src] = (existing_first, existing_last)
    print("\nPer-source wall clock:")
    for src, (first, last) in sorted(wall_by_source.items()):
        try:
            from datetime import datetime
            t0 = datetime.fromisoformat(first)
            t1 = datetime.fromisoformat(last)
            dur = t1 - t0
            print(f"  {src:10s}  {first} → {last}  ({dur})")
        except Exception:
            print(f"  {src:10s}  {first} → {last}")


def main():
    ap = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    ap.add_argument('run_dir', help='Path to nestjs-showcase-v{N}/ (containing SESSIONS_LOGS/)')
    ap.add_argument('--phase', action='store_true', help='Print inferred phase headers')
    ap.add_argument('--after', help='Only events at/after this ISO timestamp')
    ap.add_argument('--before', help='Only events at/before this ISO timestamp')
    ap.add_argument('--source', action='append', default=[], help='Filter source (repeatable)')
    ap.add_argument('--tool', action='append', default=[], help='Filter tool name substring (repeatable)')
    ap.add_argument('--no-text', action='store_true', help='Suppress assistant text blocks')
    ap.add_argument('--json', action='store_true', help='Emit JSON instead of human-readable')
    ap.add_argument('--stats', action='store_true', help='Append summary stats block')
    args = ap.parse_args()

    streams = discover_streams(args.run_dir)
    all_events = []
    for path, source in streams:
        if args.source and source not in args.source:
            continue
        all_events.extend(ingest_stream(
            path, source,
            after=args.after, before=args.before,
            want_text=not args.no_text,
        ))

    # Sort: timestamp primary, then TX before TU before TR/TE (text reasoning precedes
    # its tool call; results come after).
    kind_order = {'TX': 0, 'TU': 1, 'TR': 2, 'TE': 2}
    all_events.sort(key=lambda e: (e['ts'], kind_order.get(e['kind'], 3)))

    # Tool filter
    if args.tool:
        all_events = [e for e in all_events if e['kind'] != 'TU' or matches_tool_filter(e, args.tool)]

    if args.json:
        json.dump(all_events, sys.stdout, indent=2)
        sys.stdout.write('\n')
    else:
        human_render(all_events, show_phase=args.phase)
        if args.stats:
            emit_stats(all_events)


if __name__ == '__main__':
    main()
