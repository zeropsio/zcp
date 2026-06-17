# Bootstrap-plan durability-demotion warning (DB HA → single)

**Why rejected:** the demote path is unreachable at the bootstrap-plan layer —
warning a healthy live HA DB nobody is changing would be clutter on a dead path.

**Surfaced:** 2026-06-17, expected-state-contract analysis
(`plans/expected-state-contract-decision-2026-06-17.md` §1 F5). Original premise
(carried from the eval-feedback plan): "warn earlier than develop-active when a
simple→standard expansion demotes a live durable DB."

**Why it's a dead path (code-proven, 3 independent agents + grounding):**
- `planTargetSnapshots` (`internal/workflow/bootstrap_guide_assembly.go:181`)
  emits ONLY runtime snapshots (dev + optional stage runtime pair). It never
  emits a managed-dep snapshot, so a mode-expansion plan carries no managed dep
  to compare planned-vs-live against.
- `ValidateBootstrapTargets` (`internal/workflow/validate.go:336`) handles
  managed deps as `Resolution=EXISTS` (leave the live service untouched, :467)
  or `CREATE` (rejected if the hostname already exists live, :463). There is no
  comparison of a planned dep's mode/variant against a live `ServiceStack.Mode`,
  and no path that re-imports a live HA DB at a lower variant.
- `mergeExistingMeta` preserves user fields; expansion adds the stage runtime,
  it does not recreate the live DB.
- Confirmed: NO demotion warning exists anywhere today (the "today only at
  develop-active" premise was also false).

**The app-HA-readiness sibling concern is NOT rejected — it is already shipped.**
The user's broader ask ("make the LLM aware the app must be HA-ready before
running on multiple containers") is covered by the `launch-ha-assessment` atom
(`internal/content/atoms/launch-ha-assessment.md`, phase
`launch-production-active`): a grep-able checklist (in-memory session state,
local-disk writes, boot migrations, in-process queues, realtime pub/sub) gated
exactly at the 2-container HA floor, framed as a user consent conversation. It
fires at the moment multi-container actually happens. Adding a second copy
earlier would be a single-owner drift, not an improvement.

**Trigger to promote:** if a real path is added that re-provisions or re-imports
a live managed dependency at a LOWER durability variant (e.g. an explicit
`zerops_scale` mode-down, or an import-override that recreates a managed dep),
THAT path — not bootstrap — should carry a `topology`-derived demotion warning
(reconcile-toward-live, never block; HA immutability = delete+recreate = data
loss). Scope it where the demote actually occurs.

**Refs:** `plans/expected-state-contract-decision-2026-06-17.md`,
`plans/zcp-goal-contracts-concept-2026-06-09.md` (the broader goal-contract
redesign this leaf belongs under).
