# Debug: Troubleshooting Zerops Services

## Overview

Two phases: gather ALL data systematically (do not skip steps or jump to conclusions), then diagnose with Zerops domain knowledge.

---

## Phase 1: Data Gathering

Collect everything before diagnosing. The most common mistake is jumping to conclusions after Step 1.

### Step 1 — Service status

```
zerops_discover service="{hostname}" includeEnvs=true
```

Note: status, container count, resource usage, all env vars. If the service doesn't exist, stop here.

### Step 2 — Recent events

```
zerops_events serviceHostname="{hostname}" limit=10
```

Events give the timeline — what happened and when. Look for: failed deploys, unexpected restarts, scaling events, env var changes.

### Step 3 — Error logs

```
zerops_logs serviceHostname="{hostname}" severity="error" since="1h"
```

Look for: stack traces, connection errors, missing module errors, port binding failures.

### Step 4 — Warning logs

```
zerops_logs serviceHostname="{hostname}" severity="warning" since="1h"
```

Look for: connection retries, deprecated usage, memory pressure, slow queries. Warnings often reveal the root cause that errors only show the symptom of.

### Step 5 — Pattern search

If Steps 3-4 revealed specific error messages, search for recurring patterns:

```
zerops_logs serviceHostname="{hostname}" search="{error pattern}" since="24h"
```

This shows whether the issue is new or recurring. A recurring issue means the previous fix didn't address the root cause.

---

## Phase 2: Diagnosis

### Step 6 — Match against common Zerops issues

Check gathered data against known patterns (covers ~80% of issues):

**Note**: For detailed platform rules on any of these patterns, call `zerops_knowledge scope="infrastructure"` for the full reference, or `zerops_knowledge runtime="{type}" services=[...]` for stack-specific context.

| Symptom | Likely cause | Verify / Fix |
|---------|-------------|--------------|
| ECONNREFUSED between services | Using `https://` internally | Use `http://` for all internal connections |
| 502 Bad Gateway | App binds to localhost | Bind `0.0.0.0` (check runtime exceptions) |
| Env vars show literal `${...}` | Dashes in cross-references | Use underscores: `${service_hostname}` |
| Build FAILED | Wrong buildCommands or missing deps | Check build logs: `zcli service log {hostname} --showBuildLogs` |
| Service not starting | Port outside range or bad start cmd | Ports 10-65435, verify `start` in zerops.yml |
| DB connection timeout | Wrong connection string or DB not running | Use `http://hostname:port`, verify DB status |
| Deploy OK but app broken | Missing env vars or wrong format | `zerops_discover includeEnvs=true` |

If the issue doesn't match, continue to Step 7 for knowledge search.

### Step 7 — Load knowledge for uncommon issues

If the issue doesn't match common patterns above, use BM25 search for Zerops-specific guidance:

```
zerops_knowledge query="{error category or message}"
```

Examples:
- `zerops_knowledge query="connection refused internal networking"`
- `zerops_knowledge query="build failure deploy"`
- `zerops_knowledge query="environment variables cross-reference"`
- `zerops_knowledge query="service not starting port"`
- `zerops_knowledge query="common gotchas"` — full list of 40+ known pitfalls

**Alternatively**, if you know the runtime/services involved, get comprehensive context:
```
zerops_knowledge runtime="{runtime-type}" services=["{service1}", ...]
```
This returns core-principles + runtime exceptions + service cards, which covers most troubleshooting scenarios.

If knowledge returns no relevant results, report the raw evidence (logs, events) and ask the user for application-specific context.

### Step 8 — Report findings

Structure the diagnosis as:
- **Problem**: What is happening? (one sentence)
- **Evidence**: Which specific logs/events confirm this? (quote the relevant lines)
- **Root cause**: Why is it happening? (the actual underlying issue)
- **Recommended fix**: What specific action resolves it? (tool call or config change)

---

## Multi-Service Debug

### For 3+ services — agent orchestration

Spawn debug agents to prevent context rot when investigating multiple services:

1. `zerops_discover` — identify services with issues (non-RUNNING, recent errors)
2. For each problematic service, spawn in parallel:
   ```
   Task(subagent_type="general-purpose", model="sonnet", prompt=<debug agent prompt>)
   ```
3. Collect findings from all agents, produce a summary

### Debug-Service Agent Prompt

Replace `{hostname}` with actual value.

```
You diagnose issues with Zerops service "{hostname}".

Gather ALL data before diagnosing. Do not jump to conclusions.

| # | Action | Tool | What to look for |
|---|--------|------|-----------------|
| 1 | Check status | zerops_discover service="{hostname}" includeEnvs=true | Status, containers, resources, env vars |
| 2 | Recent events | zerops_events serviceHostname="{hostname}" limit=10 | Failed deploys, restarts, scaling |
| 3 | Error logs | zerops_logs serviceHostname="{hostname}" severity="error" since="1h" | Error messages, stack traces |
| 4 | Warning logs | zerops_logs serviceHostname="{hostname}" severity="warning" since="1h" | Connection issues, retries |
| 5 | Pattern search (if step 3 found errors) | zerops_logs serviceHostname="{hostname}" search="{error from step 3}" since="24h" | Recurring vs new issue |

Report as: Problem, Evidence, Root Cause, Recommended Fix.
Common Zerops issues: https:// for internal (use http://), dashes in env refs (use underscores), ports outside 10-65435, app binding localhost instead of 0.0.0.0.
Use zerops_knowledge for Zerops-specific troubleshooting guidance if needed.
```

---

## Restart as Last Resort

Only after full investigation — if root cause is unclear and service needs immediate recovery:

```
zerops_manage action="restart" serviceHostname="{hostname}"
```

Then monitor immediately:

```
zerops_logs serviceHostname="{hostname}" severity="error" since="5m"
```

A restart without understanding the root cause means the problem will likely recur.
