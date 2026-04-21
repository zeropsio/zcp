#!/usr/bin/env python3
"""extract_flow.py — Step-1 flow reconstruction extractor.

Emits per-source trace markdown + dispatch captures for a recipe run.

Inputs:
  - run_dir         Path to /…/nestjs-showcase-v{N}/ (SESSIONS_LOGS/ must exist)
  - role_map (arg)  JSON file mapping subagent-id-prefix → role slug
                    (e.g. {"a22": "scaffold-apidev", "a83": "code-review"})

Outputs (relative to --out-dir):
  - flow-<tier>-<ref>-main.md                          — main agent trace
  - flow-<tier>-<ref>-sub-<role>.md                    — per-subagent trace
  - flow-<tier>-<ref>-dispatches/<role>.md             — transmitted Agent prompts (verbatim)

Per-trace markdown contains:
  - header (source, wall, totals)
  - flagged-events section (errors, late guidance, scope=downstream facts,
    TodoWrite full-rewrites, Agent dispatches)
  - per-event table: timestamp | phase/substep | tool | input_summary |
                     result_size | result_summary | guidance_landed | next_tool

`guidance_landed` is non-empty only when the tool_result is from a
`zerops_workflow` call — the extractor parses the result JSON's
`current.detailedGuide` (+ topics) and emits "N KB / topic1, topic2".

`next_tool` is the mechanical derivation of `decision_next`: the next
tool_use by the same source.

Run from repo root:
  python3 docs/zcprecipator2/scripts/extract_flow.py \\
      /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v34 \\
      --tier showcase --ref v34 \\
      --role-map docs/zcprecipator2/scripts/role_map_v34.json \\
      --out-dir docs/zcprecipator2/01-flow
"""

import argparse
import json
import re
import sys
from pathlib import Path
from datetime import datetime


# ------------------------ event extraction ------------------------

def _short_input(name, inp):
    """Return a <=200 char rendering of tool input, preserving paths + action params."""
    if not isinstance(inp, dict):
        return ""
    if name == "Bash":
        cmd = (inp.get("command") or "").replace("\n", " ")
        return cmd[:200]
    if name in ("Write", "Edit", "Read"):
        p = inp.get("file_path") or ""
        extra = ""
        if name == "Edit":
            old = (inp.get("old_string") or "")[:40].replace("\n", "⏎")
            new = (inp.get("new_string") or "")[:40].replace("\n", "⏎")
            extra = f"  (old='{old}' new='{new}')"
        return (p + extra)[:200]
    if name == "Grep":
        pat = inp.get("pattern") or ""
        path = inp.get("path") or ""
        glob = inp.get("glob") or ""
        return f"pattern={pat!r}  path={path}  glob={glob}"[:200]
    if name == "Glob":
        return f"pattern={inp.get('pattern')!r}  path={inp.get('path','')}"[:200]
    if name == "TodoWrite":
        todos = inp.get("todos") or []
        statuses = [t.get("status", "?") for t in todos if isinstance(t, dict)]
        return f"{len(todos)} items  statuses={statuses}"[:200]
    if name == "Agent":
        return (f"description={inp.get('description','')!r}  "
                f"subagent_type={inp.get('subagent_type','')!r}  "
                f"prompt_len={len(inp.get('prompt') or '')}")[:200]
    if name.startswith("mcp__zerops__"):
        tool = name.replace("mcp__zerops__", "")
        # Prioritise identifying params.
        parts = [tool]
        for k in ("action", "step", "substep", "workflow", "hostname", "serviceHostname",
                 "targetService", "featureId", "setup", "topic", "query", "recipe",
                 "scope", "type", "title"):
            if k in inp and inp[k] not in (None, ""):
                v = inp[k]
                if isinstance(v, (dict, list)):
                    v = json.dumps(v)[:60]
                parts.append(f"{k}={v}")
        return "  ".join(parts)[:200]
    if name == "ToolSearch":
        return f"query={inp.get('query','')!r}  max_results={inp.get('max_results','')}"[:200]
    if name == "ScheduleWakeup":
        return f"delaySeconds={inp.get('delaySeconds','')}  reason={inp.get('reason','')!r}"[:200]
    # generic fallback
    try:
        return json.dumps(inp)[:200]
    except Exception:
        return str(inp)[:200]


def _result_summary(text, is_error):
    """Short human-readable result summary (first 200 chars)."""
    if is_error:
        # Try to extract an error class/code.
        m = re.search(r'"code"\s*:\s*"([A-Z_]+)"', text)
        code = m.group(1) if m else "ERROR"
        return f"⚠ {code}: " + text.replace("\n", " ")[:180]
    return text.replace("\n", " ")[:200]


def _extract_guidance(tool_name, result_text):
    """For zerops_workflow results, extract size + phase name of current.detailedGuide.
       Also picks up tools list + verification blurb. Empty string if not applicable."""
    if not tool_name.endswith("zerops_workflow"):
        return ""
    try:
        obj = json.loads(result_text)
    except Exception:
        return ""
    cur = obj.get("current") or {}
    nxt = obj.get("next") or {}
    # detailedGuide lives on either current (for status/start/complete-returns-current)
    # or next (after a complete call that advances state).
    dg = cur.get("detailedGuide") or nxt.get("detailedGuide") or ""
    if not dg:
        return ""
    size = len(dg) if isinstance(dg, str) else len(json.dumps(dg))
    phase = cur.get("name") or nxt.get("name") or ""
    idx = cur.get("index", nxt.get("index", ""))
    tools = cur.get("tools") or nxt.get("tools") or []
    return f"{size}B  phase={phase}(idx={idx})  tools={len(tools)}"


def _detect_fact_scope(name, inp):
    """For zerops_record_fact, return the Scope field (if any)."""
    if name != "mcp__zerops__zerops_record_fact":
        return None
    if not isinstance(inp, dict):
        return None
    return inp.get("scope") or inp.get("Scope") or ""


def _detect_todowrite_shape(name, inp):
    """Classify TodoWrite as full-rewrite vs check-off.
       Heuristic: full-rewrite = every todo is 'pending' OR 'in_progress' (fresh list);
                  check-off   = at least one todo is 'completed' AND list size unchanged from prior."""
    if name != "TodoWrite":
        return None
    if not isinstance(inp, dict):
        return None
    todos = inp.get("todos") or []
    statuses = [t.get("status", "") for t in todos if isinstance(t, dict)]
    n_completed = sum(1 for s in statuses if s == "completed")
    n_pending = sum(1 for s in statuses if s == "pending")
    n_progress = sum(1 for s in statuses if s == "in_progress")
    if not statuses:
        return "empty"
    # We compare against prior later; for now report the shape.
    return f"n={len(statuses)} completed={n_completed} pending={n_pending} in_progress={n_progress}"


PHASE_CANON = ["START", "RESEARCH", "PROVISION", "GENERATE", "DEPLOY", "FINALIZE", "CLOSE"]


def _infer_phase_from_workflow_call(inp):
    """Given a zerops_workflow tool input, return the (phase, substep) this call is associated with,
       or ('', '') if none."""
    if not isinstance(inp, dict):
        return ("", "")
    action = inp.get("action", "")
    step = inp.get("step", "")
    substep = inp.get("substep", "")
    workflow = inp.get("workflow", "")
    if action == "start" and "recipe" in str(workflow):
        return ("START", "")
    if step:
        # map step string to canonical phase
        p = str(step).upper()
        return (p, substep)
    return ("", "")


def ingest_stream(path, source):
    """Read one JSONL file, pair tool_use with tool_result, emit events list.
       Each event has keys: ts, source, kind, tool_name, input, input_summary,
       result_size, result_summary, result_text, is_error, duration_ms, raw_input."""
    events = []
    pending = {}  # tool_use_id -> event dict (awaiting result)
    with open(path) as fp:
        for line in fp:
            try:
                e = json.loads(line)
            except Exception:
                continue
            typ = e.get("type", "")
            ts = (e.get("timestamp") or "")[:19]
            msg = e.get("message") or {}
            contents = msg.get("content") or []
            if not isinstance(contents, list):
                continue
            if typ == "assistant":
                for c in contents:
                    if not isinstance(c, dict):
                        continue
                    if c.get("type") != "tool_use":
                        continue
                    name = c.get("name", "")
                    inp = c.get("input") or {}
                    tid = c.get("id", "")
                    ev = {
                        "ts": ts,
                        "source": source,
                        "tool_name": name,
                        "input": inp,
                        "input_summary": _short_input(name, inp),
                        "result_size": 0,
                        "result_summary": "",
                        "result_text": "",
                        "is_error": False,
                        "duration_ms": 0,
                        "tool_use_id": tid,
                    }
                    pending[tid] = ev
                    events.append(ev)
            elif typ == "user":
                for c in contents:
                    if not isinstance(c, dict):
                        continue
                    if c.get("type") != "tool_result":
                        continue
                    tid = c.get("tool_use_id", "")
                    ev = pending.pop(tid, None)
                    if ev is None:
                        continue
                    cont = c.get("content", "")
                    if isinstance(cont, list):
                        txt = "\n".join(
                            (x.get("text") or "") for x in cont if isinstance(x, dict)
                        )
                    else:
                        txt = str(cont or "")
                    is_err = bool(c.get("is_error", False))
                    ev["result_text"] = txt
                    ev["result_size"] = len(txt)
                    ev["result_summary"] = _result_summary(txt, is_err)
                    ev["is_error"] = is_err
                    # duration
                    try:
                        t0 = datetime.fromisoformat(ev["ts"])
                        t1 = datetime.fromisoformat(ts)
                        ev["duration_ms"] = int((t1 - t0).total_seconds() * 1000)
                    except Exception:
                        pass
    return events


# ------------------------ trace assembly ------------------------

def annotate_phase(events):
    """Walk events in order, assigning (phase, substep) to each event based on
       the most recent zerops_workflow call that moved state forward.

       Policy: the phase *of the event* is the phase *the workflow is currently in*
       at that moment — i.e. after the most recent `complete` advanced it, OR the
       phase of the currently in-progress step before complete lands.

       For subagents, the main-agent workflow state is not directly observable;
       we attach whatever the subagent has said (typically none — subagents don't
       call zerops_workflow per v8.90).
    """
    phase = ""
    substep = ""
    for ev in events:
        name = ev["tool_name"]
        inp = ev["input"]
        if name == "mcp__zerops__zerops_workflow":
            p, ss = _infer_phase_from_workflow_call(inp)
            # `action=complete` attests completion of (p, ss); subsequent events
            # are "still in p, between ss and next substep" — but the server's
            # result will usually return next substep's guide. For column simplicity
            # we label the event's own phase as p+ss (what's being attested).
            action = inp.get("action", "")
            if action in ("start", "next") and p:
                phase, substep = p, ss
            elif action == "complete" and p:
                phase, substep = p, ss  # attesting p.ss
            elif action == "status":
                pass  # read-only
            else:
                if p:
                    phase = p
                if ss:
                    substep = ss
        ev["phase"] = phase
        ev["substep"] = substep
    return events


def derive_next_tool(events):
    """For each event, attach next_tool_name and next_input_summary from the
       next event in the same source stream. This is the mechanical
       decision-next inference."""
    # events are already per-source (we process one stream at a time before merging).
    for i, ev in enumerate(events):
        if i + 1 < len(events):
            nxt = events[i + 1]
            ev["next_tool"] = f"{nxt['tool_name']} — {nxt['input_summary'][:80]}"
        else:
            ev["next_tool"] = "(end of stream)"
    return events


# ------------------------ markdown rendering ------------------------

MD_HEADER_TEMPLATE = """# flow-{tier}-{ref}-{label}.md

**Source**: `{source}` — {role}
**Log file**: `{log_path}`
**Wall clock**: {first_ts} → {last_ts}  ({wall})
**Tool calls**: {tool_count}  ({errored_count} errored)

---

## Flagged events

{flags}

---

## Per-tool trace

| # | timestamp | phase/substep | tool | input_summary | result_size | result_summary | guidance_landed | next_tool |
|---|---|---|---|---|---|---|---|---|
"""


def _md_escape(s):
    if s is None:
        return ""
    return str(s).replace("|", "\\|").replace("\n", " ")


def _wall(events):
    if not events:
        return "", "", ""
    first = events[0]["ts"]
    last = events[-1]["ts"]
    try:
        t0 = datetime.fromisoformat(first)
        t1 = datetime.fromisoformat(last)
        dur = t1 - t0
    except Exception:
        dur = ""
    return first, last, str(dur)


def collect_flags(events):
    """Return a list of (timestamp, class, description) tuples."""
    flags = []
    prior_todo_sig = None
    for ev in events:
        name = ev["tool_name"]
        inp = ev["input"]
        # 1. is_error=true
        if ev.get("is_error"):
            flags.append((ev["ts"], "ERROR", f"{name} → {ev['result_summary'][:160]}"))
        # 2. scope=downstream fact
        scope = _detect_fact_scope(name, inp)
        if scope and scope != "content":
            title = inp.get("title") or inp.get("type") or ""
            flags.append((ev["ts"], f"FACT scope={scope}", f"{title}"))
        # 3. TodoWrite shape
        tw = _detect_todowrite_shape(name, inp)
        if tw:
            # full-rewrite heuristic: list signature changes wholesale (texts differ) AND
            # all statuses fresh. For now, flag every TodoWrite with its shape; the
            # full-rewrite vs check-off split is refined at the trace-level diff.
            todos = inp.get("todos") or []
            sig = tuple((t.get("content", "")[:60] for t in todos if isinstance(t, dict)))
            if prior_todo_sig is not None and sig != prior_todo_sig:
                shape = "full-rewrite (contents changed)"
            elif prior_todo_sig is None:
                shape = "initial write"
            else:
                shape = "check-off (same contents, status updated)"
            flags.append((ev["ts"], "TODO", f"{shape} — {tw}"))
            prior_todo_sig = sig
        # 4. Agent dispatch
        if name == "Agent":
            desc = inp.get("description", "")
            flags.append((ev["ts"], "DISPATCH", f"{desc}"))
        # 5. Guidance arrives AFTER the work it governs — can only detect post-hoc
        # by checking if a `complete` attestation is followed by a subagent being
        # dispatched that requires the brief delivered at that substep. We emit
        # a simpler heuristic: any `action=complete` whose result's guidance exceeds
        # 10KB but whose source is a *backfill* (i.e. substep order diverged from
        # the server's expectation).
        if name == "mcp__zerops__zerops_workflow" and ev.get("is_error"):
            txt = ev["result_text"] or ""
            if "SUBAGENT_MISUSE" in txt or "out of order" in txt.lower() or "expected" in txt.lower():
                flags.append((ev["ts"], "ORDER", ev["result_summary"][:160]))
    return flags


def render_trace_markdown(events, tier, ref, label, source, role, log_path):
    first_ts, last_ts, wall = _wall(events)
    tool_count = len(events)
    errored_count = sum(1 for e in events if e.get("is_error"))
    flags = collect_flags(events)
    if flags:
        flag_lines = "\n".join(f"- `{ts}` **{cls}** — {desc}" for ts, cls, desc in flags)
    else:
        flag_lines = "_None._"
    out = MD_HEADER_TEMPLATE.format(
        tier=tier, ref=ref, label=label,
        source=source, role=role, log_path=log_path,
        first_ts=first_ts, last_ts=last_ts, wall=wall,
        tool_count=tool_count, errored_count=errored_count,
        flags=flag_lines,
    )
    for i, ev in enumerate(events, 1):
        tool = ev["tool_name"].replace("mcp__zerops__", "mcp:")
        guidance = _extract_guidance(ev["tool_name"], ev.get("result_text", "") or "")
        # Escape for markdown table cells
        row = "| {} | {} | {} | {} | {} | {} | {} | {} | {} |\n".format(
            i,
            ev["ts"],
            _md_escape(f"{ev.get('phase','')}/{ev.get('substep','')}".strip("/")),
            _md_escape(tool),
            _md_escape(ev["input_summary"]),
            ev["result_size"],
            _md_escape(ev["result_summary"]),
            _md_escape(guidance),
            _md_escape(ev.get("next_tool", "")),
        )
        out += row
    return out


# ------------------------ dispatch extraction ------------------------

def render_dispatch_markdown(ev, role_slug, i_of_n):
    """Render a single Agent dispatch to markdown with verbatim prompt."""
    inp = ev["input"]
    desc = inp.get("description", "")
    subagent_type = inp.get("subagent_type", "general-purpose")
    prompt = inp.get("prompt", "") or ""
    return f"""# Dispatch {i_of_n} — {role_slug}

**Dispatched at**: `{ev['ts']}`
**Description**: {desc}
**Subagent type**: `{subagent_type}`
**Prompt length**: {len(prompt)} chars
**Tool-use id**: `{ev['tool_use_id']}`

---

## Transmitted prompt (verbatim)

```
{prompt}
```
"""


# ------------------------ main ------------------------

def _role_label(source, role_map):
    if source == "MAIN":
        return "main"
    prefix = source.replace("SUB-", "")
    # role_map keys may be 3-char prefixes from timeline.py semantics.
    # Try exact, then 3-char prefix.
    if prefix in role_map:
        return role_map[prefix]
    if prefix[:3] in role_map:
        return role_map[prefix[:3]]
    return prefix


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("run_dir", help="Path to nestjs-showcase-v{N}/ (SESSIONS_LOGS/ inside)")
    ap.add_argument("--tier", required=True, help="tier label — 'showcase' or 'minimal'")
    ap.add_argument("--ref", required=True, help="ref label — e.g. 'v34'")
    ap.add_argument("--role-map", required=True,
                    help="JSON file mapping subagent-id-prefix → role slug")
    ap.add_argument("--out-dir", required=True, help="output dir (01-flow/)")
    args = ap.parse_args()

    logs_dir = Path(args.run_dir) / "SESSIONS_LOGS"
    if not logs_dir.exists():
        sys.exit(f"No SESSIONS_LOGS/ under {args.run_dir}")

    with open(args.role_map) as fp:
        role_map = json.load(fp)

    out_root = Path(args.out_dir)
    out_root.mkdir(parents=True, exist_ok=True)
    dispatch_dir = out_root / f"flow-{args.tier}-{args.ref}-dispatches"
    dispatch_dir.mkdir(exist_ok=True)

    # Enumerate streams.
    streams = []
    main_jsonls = sorted(p for p in logs_dir.glob("*.jsonl"))
    if main_jsonls:
        streams.append((main_jsonls[0], "MAIN"))
    sub_dir = logs_dir / "subagents"
    if sub_dir.exists():
        for p in sorted(sub_dir.glob("agent-*.jsonl")):
            agent_id = p.stem.replace("agent-", "")
            source = f"SUB-{agent_id[:3]}"
            streams.append((p, source))

    # Per-stream trace emission.
    dispatches_by_role = {}
    dispatch_seq = 0
    for path, source in streams:
        events = ingest_stream(path, source)
        events = annotate_phase(events)
        events = derive_next_tool(events)

        role_slug = _role_label(source, role_map)
        label = "main" if source == "MAIN" else f"sub-{role_slug}"
        md = render_trace_markdown(
            events, args.tier, args.ref, label, source, role_slug, str(path),
        )
        out_path = out_root / f"flow-{args.tier}-{args.ref}-{label}.md"
        out_path.write_text(md)
        print(f"wrote {out_path} ({len(events)} events, {sum(1 for e in events if e.get('is_error'))} errored)")

        # Capture dispatches from this stream (typically only MAIN has Agent calls).
        for ev in events:
            if ev["tool_name"] == "Agent":
                dispatch_seq += 1
                desc = ev["input"].get("description", "")
                # Derive dispatch role slug from description (best-effort).
                slug = re.sub(r"[^a-zA-Z0-9]+", "-", desc.lower()).strip("-")[:40]
                if not slug:
                    slug = f"dispatch-{dispatch_seq}"
                # Append index to disambiguate duplicates.
                dispatches_by_role.setdefault(slug, []).append(ev)

    # Emit dispatches.
    total = 0
    for slug, evs in dispatches_by_role.items():
        for idx, ev in enumerate(evs):
            total += 1
            suffix = "" if len(evs) == 1 else f"-{idx+1}"
            fn = dispatch_dir / f"{slug}{suffix}.md"
            fn.write_text(render_dispatch_markdown(ev, slug, f"{total}"))
            print(f"wrote {fn}  (prompt_len={len(ev['input'].get('prompt') or '')})")

    print(f"\ntotal streams: {len(streams)}   total dispatches captured: {total}")


if __name__ == "__main__":
    main()
