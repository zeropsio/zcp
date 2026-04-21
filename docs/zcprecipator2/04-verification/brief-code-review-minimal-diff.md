# brief-code-review-minimal-diff.md

**Purpose**: diff against the current minimal code-review template at [../01-flow/flow-minimal-spec-dispatches/code-review-subagent.md](../01-flow/flow-minimal-spec-dispatches/code-review-subagent.md). Current minimal uses the SAME block text as showcase (`code-review-subagent` block, recipe.md L3050-L3158); the tier delta is in interpolation (1 codebase vs 3).

Current minimal dispatch template: ~12.7 KB block.

## 1. Removed from current-minimal → disposition

| Current (code-review-subagent block) segment | Disposition | New home |
|---|---|---|
| L21 block opening "### 1a. Static Code Review Sub-Agent (ALWAYS — mandatory)" | dispatcher framing (the "1a" numbering is showcase-close-substep structure) | DISPATCH.md: main knows the substep from workflow state; sub-agent doesn't need the "1a" identifier. |
| L25 "⚠ TOOL-USE POLICY — include verbatim at the top of the sub-agent's dispatch prompt." | dispatcher instruction | Cut. P2. |
| L27 "You are a sub-agent spawned by the main agent inside a Zerops recipe session." | load-bearing → atom (generic framing) | `mandatory-core.md`. |
| L29 "Permitted tools" list | load-bearing → atom | `mandatory-core.md`. |
| L36 "Forbidden tools" list | load-bearing → atom | `mandatory-core.md`. |
| L46 "File-op sequencing — Read before Edit (Claude Code constraint, NOT a Zerops rule)" | load-bearing → atom | `principles/file-op-sequencing.md`. The "Claude Code constraint, NOT a Zerops rule" parenthetical is noise — cut per P2. |
| L48 "If the server rejects a call with `SUBAGENT_MISUSE`, you are the cause." | load-bearing → atom | `mandatory-core.md` — positive-form rewording: "Workflow state is main-agent-only; calling a forbidden tool returns SUBAGENT_MISUSE." |
| L50-57 `<<<MANDATORY>>>` wrapper + body | dispatcher → DISPATCH.md; load-bearing → atom | Principles pointer-includes. |
| L60 "Spawn a sub-agent as a **{framework} code expert**..." | dispatcher (instructing main how to spawn) | DISPATCH.md. Sub-agent's atom says "you are a {framework} code expert" directly. |
| L62 "The sub-agent does NOT open a browser. Browser verification (1b below) is the main agent's job." + "v5 proved that fork exhaustion during browser walk kills the sub-agent" | load-bearing (positive form rewrite) + **version-log leakage** (v5 reference) | `task.md` "Do NOT call zerops_browser". Positive form: "Browser verification is a separate phase run by the main agent." Cut v5 reference per P6. |
| L64 "The brief below is split into three explicit halves: direct-fix scope, symptom-only scope, out-of-scope." | dispatcher framing | Implicit in atom structure (task.md has In-scope / Out-of-scope / SYMPTOM reporting sections). |
| L66 "Sub-agent prompt template:" + blockquote | dispatcher framing | Atoms are the prompt; no "template" wrapper needed. |
| L70 "**CRITICAL — where commands run:**" + long prose with "fork failed: resource temporarily unavailable" warning | load-bearing (positive form) → atom + scar tissue (the fork-failed phrasing is a historical trigger message) | `principles/where-commands-run.md` positive form; cut the error-message specific phrasing (P8). |
| L86 "**Silent-swallow antipattern scan (MANDATORY — introduced after v18's Meilisearch-silent-fail class bug):**" | load-bearing → atom + **version-log leakage** (v18 anchor) | `task.md` Silent-swallow section. Cut "introduced after v18" per P6. |
| L91 "**Feature coverage scan (MANDATORY):**" | load-bearing → atom | `task.md` Feature coverage — tier-branched by plan.Features. |
| L97-98 "**Do NOT call `zerops_browser` or `agent-browser`.**" + reasoning | load-bearing → atom | `task.md`. |
| L100-101 "**Symptom reporting (NO fixes):**" + shape example | load-bearing → atom | `task.md` Symptom reporting. |
| L103-111 "**Out of scope (do NOT review, do NOT propose fixes for):**" enumerated list | load-bearing → atom | `task.md` Out-of-scope section. |
| L113-115 Report format (CRITICAL / WRONG / STYLE / SYMPTOM) | load-bearing → atom | `reporting-taxonomy.md`. |
| L116-119 "Apply any CRITICAL or WRONG fixes..." + "redeploy to verify" | load-bearing → atom (trimmed) | `task.md` + main-agent workflow concern. The redeploy instruction is main-agent territory; sub-agent just applies inline fixes. |
| L121-125 "**Close the 1a sub-step (showcase only).**" + example attestation | dispatcher instruction (main closes the substep) | DISPATCH.md. Sub-agent doesn't close substeps. For minimal tier: no substep to close; `(showcase only)` qualifier matches the tier-branching we're applying. |

## 2. Added to new composition

| Content | Closes defect class |
|---|---|
| `manifest-consumption.md` atom | **v34-manifest-content-inconsistency** secondary enforcement at code-review. Current minimal block has NO manifest-consumption rule — only showcase had it implicitly via writer brief. New composition adds explicit manifest verification at code-review for minimal. |
| Cross-codebase env-var rule (dual-runtime minimal branch) | **v34-cross-scaffold-env-var** — for dual-runtime minimal |
| Pre-attest aggregate with `zcp check manifest-honesty` | **v34-convergence-architecture** — runnable pre-attest, even when close is ungated |
| Tier-branched feature coverage interpolation | **v28 / scope-discipline** — feature list from plan is source-of-truth, not narrative |
| PriorDiscoveriesBlock slot | **v34 / v8.96 Theme B** — facts-log visibility at code-review time |

## 3. Boundary changes

| Axis | Current (minimal via showcase block) | New |
|---|---|---|
| Audience | Mixed (dispatcher "spawn as {framework} expert" + sub-agent "you are a {framework} expert") | Pure sub-agent; dispatcher text in DISPATCH.md |
| Tier branching | Block is singular; interpolation handled by main at dispatch-composition | Atom tier-branching at stitch time; minimal gets reduced scope (no 1a substep identifier, no worker checks, simpler aggregate) |
| Manifest consumption | Implicit (no explicit atom) | Explicit `manifest-consumption.md` atom |
| Version anchors | "v5", "v18", "v19", etc. threaded through prose | None (P6) |
| Aggregate | Hand-rolled "look at each check" pattern in prose | Runnable `zcp check` shim commands |

## 4. Byte-budget reconciliation

| Segment | Current minimal (~12.7 KB) | New minimal (~6.5 KB) | delta |
|---|---:|---:|---:|
| Block framing + "1a" identifier + dispatcher prose | ~1.5 KB | ~300 | -1.2 KB |
| TOOL-USE POLICY duplicated | ~2 KB | ~500 | -1.5 KB |
| MANDATORY wrapper body | ~800 | ~300 | -500 |
| "Spawn as framework expert" dispatcher instruction | ~700 | 0 | -700 |
| CRITICAL-where-commands-run + fork-failed phrasing | ~500 | ~250 (positive form) | -250 |
| Silent-swallow (w/ v18 anchor) | ~800 | ~700 | -100 |
| Feature coverage scan | ~900 | ~800 | -100 |
| Symptom reporting | ~400 | ~350 | -50 |
| Out-of-scope list | ~500 | ~400 | -100 |
| Report format + "Close the 1a" | ~700 | ~300 | -400 |
| manifest-consumption (NEW) | 0 | ~1.3 KB | +1.3 KB |
| Pre-attest aggregate (NEW) | 0 | ~300 | +300 |
| PriorDiscoveries slot | 0 | ~50 | +50 |
| Version-anchor prose + historical framing | ~600 | 0 | -600 |
| **Total** | **~12.7 KB** | **~6.5 KB** | **-6.2 KB (~49% reduction)** |

Similar proportional reduction to minimal writer — the current minimal block is showcase-shaped with tier-scaffolding.

## 5. Silent-drops audit

Every segment of the current block covered. Version anchors (v5, v18, v19) cut per P6 with rationale preserved in positive rules. Dispatcher framing ("spawn as...", "close the 1a sub-step (showcase only)") moved to DISPATCH.md per P2.
