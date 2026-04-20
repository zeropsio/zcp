---
id: bootstrap-deploy-agents
priority: 6
phases: [bootstrap-active]
routes: [classic]
steps: [deploy]
title: "Bootstrap — agent orchestration for 2+ service pairs"
---

### Multi-service orchestration (2+ runtime pairs)

For 2 or more runtime service pairs, delegate per-service work to
specialist sub-agents with fresh context to prevent context rot.
For a single pair, follow the inline dev/standard/simple atoms.

**Parent agent responsibilities:**

1. `zerops_import content="<import.yaml>"` — create every service.
2. `zerops_discover` — verify dev services reached RUNNING.
3. Confirm auto-mount completed (check `autoMounts` in the
   provision response).
4. `zerops_discover includeEnvs=true` — single call returns env
   var keys across every service.
5. For each runtime pair, spawn a Service Bootstrap Agent in
   parallel:
   ```
   Task(subagent_type="general-purpose", model="sonnet", prompt=<Service Bootstrap Agent prompt>)
   ```
6. For each managed service, spawn a verify agent in parallel:
   ```
   Task(subagent_type="general-purpose", model="haiku", prompt=<Verify-Managed Agent prompt>)
   ```
7. After all agents return, run your own `zerops_discover` + any
   whole-project `zerops_verify` — never trust agent self-reports
   alone.

**Preconditions before spawning** (missing any → agents will guess
and fail):

- All services imported and dev services RUNNING.
- All dev services auto-mounted.
- All managed env var keys discovered.
- Runtime knowledge loaded.

Embed this context in the sub-agent prompts — they cannot see parent state.
