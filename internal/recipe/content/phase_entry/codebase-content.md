# Codebase-content phase — parallel sub-agent dispatch per codebase

After scaffold + feature complete, every codebase gets two sub-agents
dispatched in parallel:

1. **`codebase-content`** — Zerops-aware. Authors `codebase/<h>/intro`,
   `codebase/<h>/integration-guide/<n>` (slotted; engine pre-stamps
   n=1, agent authors n=2 through 5),
   `codebase/<h>/knowledge-base`, and the whole commented zerops.yaml
   as one fragment `codebase/<h>/zerops-yaml`. Reads the recorded fact
   stream (porter_change + field_rationale + tier_decision) plus on-
   disk source / zerops.yaml / spec.

2. **`claudemd-author`** — Zerops-free. Authors only
   `codebase/<h>/claude-md` (single slot). Brief is strictly platform-
   free; agent reads package.json / src/* directly and produces
   `/init`-style output. Does NOT read facts; does NOT see Zerops
   integration content; sibling sub-agent owns IG/KB/yaml comments.

## Dispatch shape — main agent's responsibility

For each codebase, the main agent calls `build-subagent-prompt` TWICE
(once for `briefKind=codebase-content`, once for
`briefKind=claudemd-author`), then issues all 2N briefs in a single
message with parallel `Agent` tool calls:

```
[message]
  Agent(description: "codebase-content-api", prompt: <codebase-content brief for api>)
  Agent(description: "claudemd-author-api",  prompt: <claudemd-author brief for api>)
  Agent(description: "codebase-content-app", prompt: <codebase-content brief for app>)
  Agent(description: "claudemd-author-app",  prompt: <claudemd-author brief for app>)
  ...
```

Net savings vs serial: 5-15 minutes for 3-codebase dispatches.

## Why two sub-agents

Mixing CLAUDE.md authoring into the codebase-content brief leaks Zerops
context into CLAUDE.md (run-15 R-15-4: `## Zerops service facts` /
`## Zerops dev (hybrid)` headings appeared because the brief was
Zerops-aware). The sibling Zerops-free brief makes bleed-through
structurally impossible — there is no platform principles atom, no
`zerops.yaml` pointer, no managed-service hints in the
`claudemd-author` brief.

## Engine-emitted facts the codebase-content sub-agent fills

The brief includes engine-emitted shells (§7.1-§7.2):
- Class B universal-for-role: `<host>-bind-and-trust-proxy`,
  `<host>-sigterm-drain`, `<host>-no-http-surface` (worker)
- Class C umbrella: `<host>-own-key-aliases`
- Per-managed-service shells: `<host>-connect-<svc>`

For every shell with empty Why (per-managed-service shells, worker no-
HTTP heading), the agent calls `zerops_knowledge runtime=<svc-type>`
and fills via `fill-fact-slot factTopic=<topic> why=... heading=...`.

## Complete-phase gate

Every codebase declared in `plan.codebases` must have all five
fragment ids recorded (intro + ≥1 integration-guide slot + knowledge-
base + zerops-yaml whole-yaml + claude-md). Codebase-scoped
validators run.
