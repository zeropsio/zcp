# Knowledge System Redesign v2 — Draft

**Date**: 2026-04-02
**Status**: Working draft — needs codebase deep-dive and team review

## 4-Layer Model (from discussion)

### Layer 1: Conceptual understanding (MCP initial instructions / first workflow step)
- How Zerops works: Container Universe, Two YAML Files, Build/Deploy Lifecycle, Networking basics
- ~50 lines of platform mental model
- Delivered ONCE at start of interaction, not in knowledge files
- Could live in `themes/model.md`, injected in discover step

### Layer 2: Universal constraints (always-present, prepended to recipes)
- ~15 hard MUST/NEVER rules that apply regardless of flow/mode/runtime
- Replaces current universals.md (77L of mixed content)
- `GetUniversals()` returns this
- File: `themes/constraints.md` or section in merged file

Draft content:
```
- Bind 0.0.0.0. L7 LB routes to VXLAN IP, not loopback.
- Internal = HTTP only. Never https:// between services.
- Trust proxy headers. Framework config: recipe ## Gotchas.
- Deploy = new container. Local files lost. Only deployFiles survives.
- Build ≠ Run. deployFiles = only bridge. prepareCommands runs BEFORE deploy files arrive.
- No .env files. Zerops injects OS env vars at container start.
- Cross-service wiring: ${hostname_varname} in zerops.yml run.envVariables.
- import.yml service level: envSecrets only (NOT envVariables).
- Migrations: zsc execOnce ${appVersionId} -- <cmd> in initCommands.
- Sessions: external store (Valkey/DB) when >1 container.
- Immutable after creation: hostname, mode (HA/NON_HA), object storage bucket, service type.
- Managed services: priority: 10 (start before runtime services).
- Shared app secrets (APP_KEY, SECRET_KEY_BASE): project-level, not per-service envSecrets.
- Dev: deployFiles: [.], start: zsc noop --silent, no healthCheck.
- Prod/stage: build output in deployFiles, real start, healthCheck required.
```

### Layer 3: Flow knowledge (workflow-specific, injected per-step)
- import.yml Schema → provision step
- zerops.yml Schema → generate step
- Rules & Pitfalls → generate step
- Schema Rules (deploy semantics) → deploy step
- Multi-Service Examples → provision step
- Causal Chains → troubleshooting
- Plus: workflow docs (bootstrap.md, deploy.md, cicd.md, recipe.md)
- Plus: recipes (per-framework zerops.yml patterns + gotchas)

Currently in core.md as "bag of H2 sections" extracted by getCoreSection().
Question: should core.md lose its Platform Model sections (Container Universe, Networking, Storage, Scaling, Base Image) since those belong to Layer 1?

### Layer 4: Dynamic/contextual knowledge (on-demand)
- services.md — managed service cards + wiring (briefing mode)
- guides/ — detailed topic guides (query mode)
- decisions/ — service selection (briefing hints)
- bases/ — infra runtime guides (briefing fallback)
- runtime_resources.go — RAM recommendations (code, not markdown)

## Open Questions

1. Where exactly does Platform Model content go? model.md? system prompt? both?
2. Should core.md be renamed to something that reflects "YAML generation reference"?
3. How does `scope=infrastructure` work after restructure? Does it concatenate model + core?
4. Does the workflow engine need code changes to inject model.md at discover step?
5. What happens to the STOP warning at top of core.md?
6. Are there other places in the codebase that assume core.md structure?

## Next Steps

1. Deep-dive codebase: understand ALL knowledge consumers, delivery paths, system prompt assembly
2. Map every place knowledge is read, injected, or referenced
3. Re-evaluate this draft with full codebase understanding
4. Present refined proposal
