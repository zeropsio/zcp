# brief-code-review-showcase-simulation.md

**Purpose**: cold-read simulation of composed code-review-showcase brief.

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | `task.md` — "README framework sections only. Do NOT review fragment content" | What is a "framework section" in a README? A README with intro/IG/knowledge-base fragments is almost entirely fragment content with no non-fragment framework-specific section. Does this mean the code-review role does not review README.md at all? | **medium** — clarify: "README framework-level review = does the README exist, are fragments present, are code blocks well-formed. Fragment CONTENT (what's inside the knowledge-base bullets, the IG phrasing) is writer/platform-owned." |
| A2 | `manifest-consumption.md` — "grep the fact's title-tokens in at least one codebase's knowledge-base fragment" | "title-tokens" is not defined. Is it the full title, substrings, or a tokenized-and-normalized set? Cold reader would default to substring match with leading/trailing words. | medium — define: "title-tokens = the fact_title split on whitespace, lowercased, words of length ≥4 (stop-word filter informally). At least one such token must appear in the target fragment." |
| A3 | `task.md` silent-swallow "array-consuming store without `[]` default" | Ambiguous signal. `let items = $state<ItemDTO[]>([])` has `[]` default. `let items = $state<ItemDTO[]>()` does not. Which pattern is WRONG? | low — the positive form is in the feature brief's ux-quality: "Svelte 5 runes with `$state<Type[]>([])` default." Code-review's silent-swallow is to catch the absence. Maybe add a one-line example: "WRONG: `let x = $state<Item[]>()` — default undefined."|
| A4 | `manifest-consumption.md` route verification for `content_env_comment` — "advisory only, writer does NOT author import.yaml directly" | So code-review cannot actually grep env comments (files don't exist at close time — env comments are written at finalize). Is there a delta? Should code-review read the env-comment-set PAYLOAD from the writer's return message? That payload isn't preserved past the main-agent turn. | medium — either (a) defer `content_env_comment` routing check to a finalize-phase code-review-like check (doesn't exist today), or (b) treat `content_env_comment` routing as "trust writer's manifest claim" (less rigorous). |
| A5 | `reporting-taxonomy.md` — CRIT criteria list 5 items; the list feels exhaustive but real code review often surfaces novel CRIT-worthy classes. Is the list closed or illustrative? | low — add "…or any framework-level code defect that breaks runtime functionality" as a capstone. |
| A6 | `task.md` feature-coverage: "grep appdev for `data-feature="{uiTestId}"`" — but `data-feature` values are declared at Svelte-component level. Grep for `data-feature="items-crud"` matches the attribute but not its hostname (all three codebases are greppable). | low — the grep `grep -rn 'data-feature="items-crud"' /var/www/appdev/` scopes to appdev. Maybe add a scope reminder. |

## 2. Contradictions

| # | A | B | Resolution |
|---|---|---|---|
| C1 | `task.md` permits Read/Edit/Write/Grep/Glob; `mandatory-core.md` lists same + Bash via ssh | No contradiction; same list. | — |
| C2 | `task.md` "apply fixes for CRITICAL and WRONG directly (Read before Edit)"; `manifest-consumption.md` "Report inline or apply the inline fix + re-run this scan" | Both allow inline fix. Code-review is fix-enabled at this phase. | — |
| C3 | "Do NOT call zerops_browser" (task.md) vs permitted list in mandatory-core.md | Permitted list correctly EXCLUDES zerops_browser. No contradiction. | — |

No actual contradictions.

## 3. Impossible-to-act-on

| # | Instruction | Why | Fix |
|---|---|---|---|
| I1 | `manifest-consumption.md` route verification for `content_env_comment` | Env comments are not yet written at close.code-review time. File paths `env*/import.yaml` don't exist. | See A4: either defer to finalize-phase check, or skip the content_env_comment branch during close.code-review (mark verified=unknown in reporting). |
| I2 | `completion-shape.md` pre-attest aggregate runs `zcp check manifest-honesty` + `zcp check symbol-contract-env-consistency` + `zcp check cross-readme-dedup` | Requires shim CLI; same concern as writer brief I2. Deferred to rewrite runtime. | Noted but not a P7 blocker. |

## 4. Defect-class cold-read scan

| Class | Ships? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No | where-commands-run pointer |
| v21 framework-token-purge | No | task.md scoping — framework specifics per codebase |
| v21 claude_readme_consistency dead regex | N/A — check retired (knowledge_base_authenticity rewritten to runnable shim, which code-review reviews) |
| v22 NATS creds | No (code-review catches URL-embedded creds regression via silent-swallow adjacent) | |
| v22 S3 endpoint | No (code-review catches wrong endpoint env var) | |
| v23 env-self-shadow | No | scaffold phase already caught; but code-review can double-check |
| v25 substep-bypass | No | dispatched via Agent |
| v25 subagent at spawn | No | forbidden list |
| v28 debug writes content | N/A | writer role |
| v28 33% genuine gotchas | No | code-review reviews authenticity via manifest-consumption + knowledge-base scan |
| v29 ZCP_CONTENT_MANIFEST | No | manifest-consumption reads it; will flag missing/invalid |
| v30 worker SIGTERM | No | code-review catches `drain()` absence in onModuleDestroy |
| v31 apidev enableShutdownHooks | No | code-review catches absence via framework-expert checklist |
| v32 Read-before-Edit | No | file-op-sequencing pointer |
| v32 six principles | Partial — code-review does NOT explicitly pointer-include platform-principles atoms. Framework-expert review covers framework patterns; platform principles are expected to hold from earlier phases. | medium gap — should code-review pointer-include platform-principles so it can flag regressions? |
| v33 Unicode box-drawing | N/A to code-review (platform config out of scope — handled by `visual_style_ascii_only` check elsewhere) |
| v33 phantom output tree | N/A to code-review (writer role + close-entry `canonical_output_tree_only` check) |
| v34 manifest ↔ content | **No** — `manifest-consumption.md` is the secondary enforcement point for routing-vs-content. Reads manifest + greps every fragment. Catches v34 DB_PASS class. Writer brief is the PRIMARY closure; code-review is defense-in-depth. |
| v34 cross-scaffold env-var | **No** — framework-expert checklist: "Cross-codebase env-var naming: every codebase reads from the canonical SymbolContract names. Any variant is CRITICAL." + `zcp check symbol-contract-env-consistency` in completion aggregate |
| v34 convergence architecture | No | all pre-attest commands are runnable shims |

## 5. P7 verdict

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | **PASS with caveats** — A1 (README framework sections definition), A2 (title-tokens definition), A4 (env-comment routing at close time) |
| Author-runnable pre-attest aggregate | PASS (completion-shape names 3 shim commands) |
| Every applicable v20-v34 defect class has a prevention mechanism | PASS |
| No dispatcher text (P2) | PASS |
| No version anchors (P6) | PASS — "after v18" in v34's silent-swallow paragraph was cut |
| No internal check vocabulary | PASS (shim CLI names are author-runnable, not internal) |
| No Go-source paths | PASS |

**Net**: passes conditional on clarifications A1/A2/A4 and the gap flagged for v32 six principles (should code-review pointer-include platform-principles atoms?).

## 6. Proposed edits

- A1: define "README framework-level review" explicitly — file existence, fragments present, code-block well-formedness. Fragment CONTENT reviewing is writer/platform-owned.
- A2: define "title-tokens" — whitespace split, lowercased, length ≥4.
- A3: add a one-line counter-example for silent-swallow array default.
- A4: defer `content_env_comment` routing check to a finalize-phase step; in close.code-review mark as "verified=pending finalize."
- Consider adding `principles/platform-principles/01..06.md` pointer-include to code-review so the checklist has a rule reference when catching a principle regression.
