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

Check the gathered data against these known patterns FIRST — they cover ~80% of Zerops issues:

**Connection refused between services**
- Symptom: `ECONNREFUSED`, `connection refused`, timeout to another service
- Cause: Using `https://` for internal connections, or wrong hostname/port
- Fix: Internal services use `http://hostname:port`. SSL terminates at L7 balancer.

**Service not starting**
- Symptom: stuck in non-RUNNING state, container restarts in events
- Cause: bad `start` command, missing dependencies, port outside range
- Fix: check `start` in zerops.yml, ports must be 10-65435, app must bind `0.0.0.0` not `127.0.0.1`

**Environment variables not resolving**
- Symptom: env vars show literal `${...}` instead of values
- Cause: dashes in cross-references instead of underscores
- Fix: `${service_hostname}` (underscores), not `${service-hostname}`

**Build failures**
- Symptom: deploy events show FAILED
- Cause: missing build deps, wrong buildCommands, incompatible runtime version
- Fix: check build logs, verify prepareCommands install all system deps

**Database connection issues**
- Symptom: `could not connect`, timeout to DB service
- Cause: wrong connection string format, DB not RUNNING, using localhost
- Fix: use hostname `db:5432` (not localhost), verify DB status, check connection string format for the framework

**Port binding errors**
- Symptom: `EADDRINUSE`, `address already in use`
- Cause: reserved port (0-9, 65436+), or zerops.yml port doesn't match app config
- Fix: ensure zerops.yml `ports` matches what app listens on

**Deploy succeeds but app broken**
- Symptom: RUNNING status but errors in logs, HTTP 500s
- Cause: missing env vars, wrong env var format, missing runtime dependencies
- Fix: check `zerops_discover includeEnvs=true` for missing/wrong vars

### Step 7 — Load knowledge for uncommon issues

If the issue doesn't match common patterns above, search for Zerops-specific guidance:

```
zerops_knowledge query="{error category or message}"
```

Examples:
- `zerops_knowledge query="connection refused internal networking"`
- `zerops_knowledge query="build failure deploy"`
- `zerops_knowledge query="environment variables cross-reference"`
- `zerops_knowledge query="service not starting port"`
- `zerops_knowledge query="common gotchas"` — full list of 40+ known pitfalls

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
