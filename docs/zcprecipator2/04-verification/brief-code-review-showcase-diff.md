# brief-code-review-showcase-diff.md

**Purpose**: diff against [../01-flow/flow-showcase-v34-dispatches/nestjs-svelte-code-review.md](../01-flow/flow-showcase-v34-dispatches/nestjs-svelte-code-review.md). v34 length: 6256 chars / 102 lines.

## 1. Removed from v34 → disposition

| v34 segment | Disposition | New home |
|---|---|---|
| L14 "You are a NestJS + Svelte 5 code expert..." | load-bearing → atom (generic framing) | Kept verbatim in `briefs/code-review/task.md`. |
| L16–24 `<<<MANDATORY>>>` wrapper + body (File-op sequencing, Tool-use policy, SSH-only) | dispatcher → DISPATCH.md (wrapper); load-bearing → atom (body) | `mandatory-core.md` + `principles/file-op-sequencing.md` + `principles/tool-use-policy.md` + `principles/where-commands-run.md`. |
| L26 "CRITICAL — where commands run" | load-bearing → atom | `principles/where-commands-run.md`. |
| L28–30 "Mounts to review" list (apidev/appdev/workerdev) | load-bearing → atom | Interpolated from `plan.Hostnames` into `task.md`. |
| L34–36 "Read and review (direct fixes allowed)" bulleted list | load-bearing → atom | `task.md` "In-scope for review" section. |
| L40–46 Framework-expert checklist | load-bearing → atom | `task.md` "Framework-expert review checklist" — kept verbatim + expanded with "Cross-codebase env-var naming" rule (v34 DB_PASS addition). |
| L48–50 Silent-swallow scan | load-bearing → atom | `task.md` "Silent-swallow antipattern scan" — kept verbatim. |
| L52–68 Feature coverage scan | load-bearing → atom | `task.md` "Feature coverage scan" — kept verbatim with `{{.Plan.Features}}` interpolation. |
| L70–72 "Do NOT call zerops_browser" | load-bearing → atom | `task.md` same section. |
| L74–80 Out-of-scope list | load-bearing → atom | `task.md` "Out of scope" — kept verbatim. |
| L82–100 Report format + inline-fix policy | load-bearing → atom | `briefs/code-review/reporting-taxonomy.md`. |

## 2. Added to new composition

| Content | Closes defect class |
|---|---|
| `manifest-consumption.md` atom — reads `/var/www/ZCP_CONTENT_MANIFEST.json` and verifies every `routed_to × surface` pair | **v34-manifest-content-inconsistency** — defense-in-depth for writer brief's primary coverage. v34 had no secondary code-review enforcement of manifest routing. Adding it here catches regressions the writer's self-review might have missed. |
| "Cross-codebase env-var naming" rule in framework-expert checklist | **v34-cross-scaffold-env-var** — code-review as a secondary enforcement. Primary is P3/contract/new `symbol_contract_env_var_consistency` check; code-review catches it structurally. |
| `zcp check manifest-honesty` + `zcp check symbol-contract-env-consistency` + `zcp check cross-readme-dedup` in completion aggregate | **v34-convergence-architecture** — code-review runs the same runnable commands the gate runs. Rounds collapse. |
| `PriorDiscoveriesBlock` tail slot | **v34-manifest-content-inconsistency** + misroute-map.md §1 — code-review sees facts inventory explicitly before verifying content placement. |
| Worker subscription checklist item: "`onModuleDestroy` drains BOTH subscriptions plus the connection" | **v30 worker SIGTERM** (extended) — covers the v34 feature-brief case where adding `jobs.process` subscription without updating drain leaks the new sub. |

## 3. Boundary changes (structural)

| Axis | v34 | New |
|---|---|---|
| Audience | Mixed (MANDATORY wrapper, dispatcher meta) | Pure sub-agent (atoms) |
| Manifest consumption | v34 did NOT name manifest-consumption explicitly | `manifest-consumption.md` atom — secondary coverage for writer's v34 class |
| Cross-codebase env-var check | absent (implicit via framework review) | explicit — rule in checklist + shim command in aggregate |
| Platform-principles pointer-include | absent (code-review is framework-only) | proposed (simulation P7 caveat) — open question whether to add |

## 4. Byte-budget reconciliation

| Segment | v34 | new | delta |
|---|---:|---:|---:|
| Preamble + framing | ~400 | ~280 | -120 |
| MANDATORY wrapper + body | ~500 | ~400 (pointer-includes) | -100 |
| Mounts to review | ~150 | ~150 (interpolated) | 0 |
| In-scope read-review | ~350 | ~350 | 0 |
| Framework-expert checklist | ~750 | ~900 (+ cross-codebase env-var rule) | +150 |
| Silent-swallow scan | ~550 | ~550 | 0 |
| Feature coverage scan | ~900 | ~900 | 0 |
| Do NOT call browser | ~150 | ~150 | 0 |
| Out of scope | ~350 | ~350 | 0 |
| Symptom reporting | ~350 | ~350 | 0 |
| Manifest-consumption (NEW) | 0 | ~1600 | +1600 |
| Reporting taxonomy | ~250 | ~300 | +50 |
| Completion shape + aggregate | ~150 | ~400 (author-runnable) | +250 |
| PriorDiscoveries slot | 0 | +50 | +50 |
| **Total** | **~6.25 KB** | **~8 KB** | **+1.75 KB (~28% growth)** |

Code-review brief grows because manifest-consumption atom is new. This is the cost of v34 DB_PASS defense-in-depth. Growth is a structural investment — v34 code-review caught 3 WRONG but missed the DB_PASS manifest gotcha entirely.

## 5. Silent-drops audit

Every v34 segment covered. Zero drops.
