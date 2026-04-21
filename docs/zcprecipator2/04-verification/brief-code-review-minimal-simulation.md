# brief-code-review-minimal-simulation.md

**Purpose**: cold-read simulation of composed minimal-code-review composition.

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | "Tier note: minimal close has no gated substep. The aggregate runs as an author-discipline check; main observes output and advances close." | Main advances close based on code-review return. But minimal close is ungated — there's no `zerops_workflow complete close substep=code-review` at the server. The return is just "main reads output, applies any fixes, moves on." What if code-review reports CRITICAL? Is main forced to iterate? | **medium** — the ungated-but-dispatched pattern creates a soft gate. Clarify: "if code-review reports CRITICAL, main applies inline fixes (via the sub-agent's own inline-fix pattern) + re-dispatches OR closes the recipe with the known issue documented. No server-side gate blocks advance." |
| A2 | `task.md, tier=minimal` feature-coverage iteration `{{range .Plan.Features}}` | For a very simple minimal (e.g. "one CRUD + health"), the feature list is 1-2 items. OK. But for dual-runtime minimal it might be 5+. Scale-appropriate. | low |
| A3 | `manifest-consumption.md` tier-conditional phrasing — refers to minimal's smaller fact set without saying so | Reader-side not a concern (works with whatever volume). OK. | low |
| A4 | Cross-codebase env-var rule for dual-runtime minimal but single-codebase "not applicable" | Stitcher tier-branches OK. But what if plan.Research says dual-runtime but tier=minimal? The rule applies. Stitcher has a compound condition. | low — stitcher decision |
| A5 | `completion-shape.md, tier=minimal` aggregate — only `zcp check manifest-honesty` plus conditional dual-runtime shims | That's an incomplete aggregate compared to showcase (which also runs `symbol_contract_env_var_consistency` unconditionally). For single-codebase minimal the latter is truly N/A. OK but the reader might miss the fact that tier-conditional filtering happened. | low |
| A6 | Title-tokens defined as "whitespace-split, lowercased, words of length ≥4" | Edge case: short but meaningful tokens like "SIGTERM", "NATS", "CORS" are ≥4. "DB" is 2 — would be filtered. Problematic for facts about db-specific gotchas. | **medium** — threshold should be ≥3 or include explicit ALL-CAPS acronym carve-out. |

## 2. Contradictions

| # | A | B | Resolution |
|---|---|---|---|
| C1 | "minimal close has no gated substep" (tier note) | "Pre-attest aggregate exit code (must be 0)" | Tension: if close is ungated, what enforces "must be 0"? Answer: the sub-agent's return protocol. Main reads the return; if exit non-zero, main decides iterate vs accept. Not a server-side gate. Explicit in A1 clarification. |

## 3. Impossible-to-act-on

| # | Instruction | Why | Fix |
|---|---|---|---|
| I1 | `completion-shape.md` aggregate relies on `zcp check` CLI shims | Same concern as all briefs; deferred to rewrite runtime. | Noted, not blocking. |

## 4. Defect-class cold-read scan

| Class | Ships? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No | where-commands-run pointer |
| v21 scaffold hygiene | No | N/A to code-review; scaffold-phase closes |
| v21 claude-readme dead regex | Partial — tier-gated OUT for minimal (authenticity check doesn't fire) | Relies on author discipline + manifest-consumption |
| v22 NATS creds | N/A for most minimals |
| v22 queue-group | N/A — no worker |
| v25 substep-bypass | No — dispatched via Agent |
| v28 debug writes content | N/A — writer role |
| v28 33% genuine gotchas | Partial — manifest-consumption catches routing; authenticity tier-gated out |
| v29 ZCP_CONTENT_MANIFEST | No — manifest-consumption reads it; absence flagged CRITICAL |
| v30 worker SIGTERM | N/A |
| v31 apidev enableShutdownHooks | No — framework-expert checklist catches |
| v32 dispatch compression | No — atomic stitching |
| v32 six principles | Partial — framework-expert "does the app work?" covers behaviorally |
| v33 Unicode box-drawing | No (framework review scope; platform config out of scope) |
| v33 phantom output tree | N/A — writer role |
| v34 manifest-content-inconsistency | **No** — manifest-consumption covers all routing dimensions. Same coverage as showcase code-review, tier-scoped hostnames. |
| v34 cross-scaffold env-var | No (if dual-runtime: framework-expert rule + shim); trivially N/A for single-codebase |
| v34 convergence architecture | No — runnable aggregate |

## 5. P7 verdict

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | **PASS with 2 caveats** — A1 (ungated but must-be-0 semantics), A6 (title-tokens threshold) |
| Author-runnable pre-attest | PASS (shim-gated on CLI availability) |
| Every applicable v20-v34 class has prevention | PASS |
| No dispatcher text | PASS |
| No version anchors | PASS |
| No internal check vocabulary | PASS |
| No Go-source paths | PASS |

**Net**: passes. Clarifications land on A1 semantics (ungated + aspirational aggregate) and A6 title-token threshold.

## 6. Proposed edits

- Explicit A1 resolution: "minimal close substep is ungated; the aggregate is the author-discipline check. If CRITICAL issues surface, main applies fixes inline and re-verifies before closing — decision is main's, not server's."
- Title-tokens definition (A6): "whitespace-split, lowercased, length ≥ 3 OR length ≥ 2 if ALL-UPPERCASE acronym in original form (heuristic for matching DB / CPU / S3 / SSL)." — or pick a clean ≥3 with no carve-out.
- Consider adding `principles/platform-principles/*` pointer-includes (same caveat as showcase code-review).
