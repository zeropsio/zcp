#!/usr/bin/env python3
"""Extract all Bash tool invocations from a Claude session jsonl and
compute per-call latency (assistant tool_use timestamp → user tool_result
timestamp), plus flag common pain patterns (SSH, dev server start,
parallel failures)."""
import json
import sys
from datetime import datetime

def parse_ts(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00"))

def main(path):
    tool_uses = {}   # tool_use_id -> (assistant_ts, command, description, timeout)
    tool_results = {}  # tool_use_id -> (user_ts, stdout, stderr, interrupted, is_error)
    with open(path) as f:
        for line in f:
            try:
                ev = json.loads(line)
            except Exception:
                continue
            etype = ev.get("type")
            ts = ev.get("timestamp")
            if not ts:
                continue
            if etype == "assistant":
                msg = ev.get("message", {})
                content = msg.get("content", [])
                if not isinstance(content, list):
                    continue
                for c in content:
                    if c.get("type") == "tool_use" and c.get("name") == "Bash":
                        inp = c.get("input", {}) or {}
                        tool_uses[c.get("id")] = (
                            parse_ts(ts),
                            inp.get("command", ""),
                            inp.get("description", ""),
                            inp.get("timeout", ""),
                            inp.get("run_in_background", False),
                        )
            elif etype == "user":
                msg = ev.get("message", {})
                content = msg.get("content", [])
                if not isinstance(content, list):
                    continue
                for c in content:
                    if c.get("type") == "tool_result":
                        tid = c.get("tool_use_id")
                        if not tid:
                            continue
                        res = c.get("content", "")
                        if isinstance(res, list):
                            res_text = " ".join(
                                (p.get("text", "") if isinstance(p, dict) else str(p))
                                for p in res
                            )
                        else:
                            res_text = str(res)
                        tur = ev.get("toolUseResult", {})
                        if not isinstance(tur, dict):
                            tur = {}
                        stdout = tur.get("stdout", "") if isinstance(tur, dict) else ""
                        stderr = tur.get("stderr", "") if isinstance(tur, dict) else ""
                        interrupted = tur.get("interrupted", False) if isinstance(tur, dict) else False
                        tool_results[tid] = (
                            parse_ts(ts),
                            (stdout or "")[:2000],
                            (stderr or "")[:2000],
                            interrupted,
                            c.get("is_error", False),
                            res_text[:2000],
                        )
    pairs = []
    for tid, (ats, cmd, desc, timeout, bg) in tool_uses.items():
        if tid in tool_results:
            uts, stdout, stderr, interrupted, is_error, rtext = tool_results[tid]
            dur = (uts - ats).total_seconds()
            pairs.append((ats, dur, cmd, desc, timeout, bg, interrupted, is_error, stdout, stderr, rtext))
    pairs.sort(key=lambda p: p[0])
    # Summary stats
    total = len(pairs)
    total_dur = sum(p[1] for p in pairs)
    print(f"BASH CALLS: {total}  TOTAL_DUR: {total_dur:.1f}s ({total_dur/60:.1f}min)")
    long = [p for p in pairs if p[1] > 10]
    print(f"LONG (>10s): {len(long)}  SUM: {sum(p[1] for p in long):.1f}s")
    very_long = [p for p in pairs if p[1] > 60]
    print(f"VERY_LONG (>60s): {len(very_long)}")
    interrupted = [p for p in pairs if p[6]]
    print(f"INTERRUPTED: {len(interrupted)}")
    errored = [p for p in pairs if p[7]]
    print(f"ERRORED: {len(errored)}")
    # Pattern buckets
    def has(cmd, *needles):
        return any(n in cmd for n in needles)
    ssh_cmds = [p for p in pairs if has(p[2], "ssh ", "ssh\t")]
    dev_cmds = [p for p in pairs if has(p[2], "start:dev", "npm run dev", "nest start", "vite dev", "vite ")]
    lsof_cmds = [p for p in pairs if has(p[2], "lsof", "fuser", "pkill", "kill -9")]
    sleep_cmds = [p for p in pairs if " sleep " in p[2] or p[2].startswith("sleep ")]
    curl_cmds = [p for p in pairs if "curl " in p[2]]
    print(f"\nSSH calls: {len(ssh_cmds)}  sum_dur: {sum(p[1] for p in ssh_cmds):.1f}s")
    print(f"Dev-server starts: {len(dev_cmds)}  sum_dur: {sum(p[1] for p in dev_cmds):.1f}s")
    print(f"Port/process kill: {len(lsof_cmds)}  sum_dur: {sum(p[1] for p in lsof_cmds):.1f}s")
    print(f"Sleeps: {len(sleep_cmds)}  sum_dur: {sum(p[1] for p in sleep_cmds):.1f}s")
    print(f"Curls: {len(curl_cmds)}  sum_dur: {sum(p[1] for p in curl_cmds):.1f}s")

    # Failure signatures
    FAIL_PATTERNS = [
        "fork failed",
        "Resource temporarily unavailable",
        "pthread_create",
        "EADDRINUSE",
        "address already in use",
        "port already",
        "ECONNREFUSED",
        "Connection refused",
        "Could not connect",
        "timed out",
        "timeout",
        "Command timed out",
        "killed",
    ]
    print("\n--- failure-signature hits in stdout+stderr ---")
    sig_counts = {}
    sig_examples = {}
    for p in pairs:
        blob = (p[8] + "\n" + p[9] + "\n" + p[10]).lower()
        for sig in FAIL_PATTERNS:
            if sig.lower() in blob:
                sig_counts[sig] = sig_counts.get(sig, 0) + 1
                sig_examples.setdefault(sig, []).append((p[0], p[2][:120], p[1]))
    for sig, cnt in sorted(sig_counts.items(), key=lambda x: -x[1]):
        print(f"  {sig!r}: {cnt}")
        for ex in sig_examples[sig][:2]:
            print(f"    @ {ex[0].isoformat(timespec='seconds')}  dur={ex[2]:.1f}s  cmd={ex[1]!r}")

    # Long-running dev server starts or interruptions
    print("\n--- 20 longest bash calls ---")
    pairs_by_dur = sorted(pairs, key=lambda p: -p[1])[:20]
    for p in pairs_by_dur:
        flag = "BG" if p[5] else ""
        flag += " INT" if p[6] else ""
        flag += " ERR" if p[7] else ""
        cmd = p[2].replace("\n", " ")[:140]
        print(f"  {p[1]:7.1f}s [{flag:>6s}] {cmd}")

    # Parallel multi-host bash (single command containing multiple ssh to diff hosts)
    print("\n--- commands with multi-host SSH patterns ---")
    for p in pairs:
        hosts = set()
        cmd = p[2]
        import re
        for m in re.finditer(r"ssh\s+([a-z][a-z0-9-]+)", cmd):
            hosts.add(m.group(1))
        if len(hosts) >= 2:
            print(f"  {p[0].isoformat(timespec='seconds')}  dur={p[1]:7.1f}s  hosts={sorted(hosts)}  err={p[7]}")
            print(f"    cmd: {cmd[:200]!r}")

if __name__ == "__main__":
    main(sys.argv[1])
