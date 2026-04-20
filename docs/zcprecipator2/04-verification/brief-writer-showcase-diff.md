# brief-writer-showcase-diff.md

**Purpose**: diff against [../01-flow/flow-showcase-v34-dispatches/write-per-codebase-readmes-claude-md.md](../01-flow/flow-showcase-v34-dispatches/write-per-codebase-readmes-claude-md.md). v34 length: 11346 chars / 214 lines.

## 1. Removed from v34 → disposition

| v34 segment | Disposition | New home |
|---|---|---|
| L14 "You are a content-authoring sub-agent for the `nestjs-showcase` Zerops recipe. You have NO memory..." (recipe-slug + fresh-context) | split: recipe-slug → dispatcher; fresh-context → atom | DISPATCH.md interpolates slug. `briefs/writer/fresh-context-premise.md` keeps the "no memory" framing. |
| L16 "Session ID: `4856bb30df43b2b1`. Facts log: `/tmp/zcp-facts-4856bb30df43b2b1.jsonl`." | load-bearing → atom (interpolated) | Same values, interpolated into `mandatory-core.md` or `fresh-context-premise.md`. |
| L18–35 `<<<MANDATORY>>>` wrapper + body (canonical-output + file-op-sequencing + tool-use + SSH-only) | dispatcher → DISPATCH.md (wrapper); load-bearing → atom (body) | `canonical-output-tree.md` + `mandatory-core.md` + `principles/file-op-sequencing.md` + `principles/where-commands-run.md`. Physical separation — every atom is either dispatcher-owned or transmitted. |
| L20–26 "Canonical output tree — the ONLY files you write:" list | load-bearing → atom | `canonical-output-tree.md` — **kept as positive allow-list**, per P8. Forbidden-paths enumeration cut. |
| L27 "Forbidden output locations include (non-exhaustive): `/var/www/recipe-{slug}/`, `/var/www/{slug}-output/`, any `environments/` tree you create on the SSHFS mount, any `0 — Development with agent` / `4 — Small production` / `5 — HA production` folder you invent by paraphrasing." | **scar tissue (enumerates invented forbidden paths per P8)** | Rewritten as positive: "Any path not in 'the ONLY files you write' list is out of scope. The publish CLI ignores them." — P8 compliance. Closes v33 phantom output tree class via positive form instead of negative enumeration. |
| L37 "## Recipe state (read from the mounts directly)" with codebase summaries | load-bearing → atom | `fresh-context-premise.md` + interpolated `SymbolContract` summary. |
| L45 "## Plan shape" bullet list | load-bearing → atom | Interpolated from plan.Research — not hardcoded recipe-specifics. |
| L55–63 "## Role — Three pathologies shipped across v20–v28 ..." | **version-log leakage** + load-bearing rationale | The "v20–v28" anchor is cut per P6. The three pathologies (fabricated mental models / wrong-surface placement / self-referential decoration) are reframed as positive-form authoring rules in `classification-taxonomy.md` and `self-review-per-surface.md`. |
| L65–72 "## Inputs you HAVE / Inputs you do NOT have" | load-bearing → atom | `fresh-context-premise.md`. |
| L77 "## Return contract: content manifest (MANDATORY)" + JSON skeleton + Rules | load-bearing → atom | `briefs/writer/manifest-contract.md`. **Expanded** per P5 — routed_to enum grows from {apidev-gotcha, apidev-ig, ...} hostname-qualified to role-qualified ({content_gotcha, content_intro, content_ig, content_env_comment, claude_md, zerops_yaml_comment, scaffold_preamble, feature_preamble, discarded}). Hostname-qualification was a v34 convention that accidentally made honesty-check per-hostname; new shape is per-surface (honesty check iterates hostname × surface). |
| L98 "## The six content surfaces (you author #4-7 only)" numbered table | load-bearing → atom | `content-surface-contracts.md` — **renumbered** and expanded. v34 numbered them 1-7 and then said "you author #4-7" — confusing because #1-3 were apparently not listed. New shape: 6 surfaces 1-6, all author-owned (or payload-owned for env comments). |
| L107 "## Classification taxonomy (apply BEFORE routing)" | load-bearing → atom | `classification-taxonomy.md` — table kept verbatim. |
| L120 "## Citation map (MANDATORY consultation)" | load-bearing → atom | `citation-map.md` — table kept verbatim. |
| L134 "## Per-codebase README skeleton (markers BYTE-LITERAL)" code block | load-bearing → atom | Embedded in `content-surface-contracts.md` as the canonical skeleton for surfaces 1-3. |
| L172 "## Per-codebase CLAUDE.md" brief description | load-bearing → atom | `content-surface-contracts.md` row 4 + detail. |
| L176 "## Worker-specific rules (workerdev)" | load-bearing → atom | `content-surface-contracts.md` tail section "For workerdev specifically" — preserves queue-group + SIGTERM requirements. |
| L181 "## Workflow" numbered list | load-bearing → atom | Implicit in atom ordering; made explicit in `self-review-per-surface.md`. |
| L190 "## Self-review checklist" | load-bearing → atom | `self-review-per-surface.md`. |
| L199 "## Deliverables" list | load-bearing → atom | `completion-shape.md`. |
| L208 "## Return message" | load-bearing → atom | `completion-shape.md`. |

## 2. Added to new composition

| Content | Closes defect class |
|---|---|
| `routing-matrix.md` atom — enumerates EVERY (routed_to × surface) pair | **v34-manifest-content-inconsistency** (DB_PASS) — v34 had single-dimension `(discarded, published_gotcha)` honesty; new matrix covers all 9×6 pairs. |
| `self-review-per-surface.md` author-runnable aggregate — greps mount paths for fragment markers + CLAUDE.md byte-floor + manifest honesty shim invocation | **v34-convergence-architecture** (Fix E refuted) — writer runs the gate-equivalent commands locally before attesting. |
| Positive-form `canonical-output-tree.md` (no forbidden-path enumeration) | **v33-phantom-output-tree** — v8.103/v8.104 added MANDATORY "don't write to `/var/www/recipe-{slug}/`" as prohibition; new atom uses positive allow-list per P8. |
| Positive-form `briefs/writer/*` atoms (no "don't write headings in intro", etc.) | **v33-Unicode-box-drawing class** (style-axis invention) — positive style statements in `comment-style.md` + `visual-style.md`. |
| `FactRecord.RouteTo` in manifest schema | **v34 routing dimensions** — RouteTo set at record time by the recording sub-agent (or at classification time by the writer) makes every fact's routing explicit. |
| Explicit "facts with scope=downstream are filtered from writer's input" | **v25 / v8.96 Theme B** — preserved; writer doesn't see downstream-only facts at all. |
| `fresh-context-premise.md` atom preserved + expanded | **v28-debug-agent-writes-content** (v8.94 shape). |
| Mandatory pre-attest `zcp check manifest-honesty` shim invocation | **v34 single-dimension check miss** — expanded coverage via shim. |

## 3. Boundary changes (structural)

| Axis | v34 | New |
|---|---|---|
| Audience | Mixed — MANDATORY wrapper, recipe-slug in framing, "if this brief is used as a sub-agent dispatch prompt, read before your first tool call" dispatcher meta | Pure sub-agent; dispatcher text in DISPATCH.md |
| routed_to enum | Hostname-qualified (`apidev-gotcha`, `apidev-ig`, ...) — 24 values | Role-qualified (`content_gotcha`, `content_intro`, ...) — 9 values. Honesty check iterates (hostname × surface) at check-time, not at manifest-schema time. |
| Forbidden paths | Explicit enumeration of invented paths | Positive allow-list only |
| Predecessor floor | "showcase tier needs at least 3 net-new gotchas beyond the predecessor" | Dropped (check flagged for deletion per check-rewrite.md §15) |
| Workflow section | Numbered list | Implicit in atom ordering + explicit self-review |
| Version anchors | "v20–v28 pathologies" | Positive-form authoring rules, no version anchors |

## 4. Byte-budget reconciliation

| Segment | v34 | new | delta |
|---|---:|---:|---:|
| Recipe-slug/framing/session-ID | ~400 | ~250 | -150 |
| MANDATORY wrapper + body | ~800 | ~450 (pointer-includes + mandatory-core) | -350 |
| Canonical output (positive only) | ~350 (+ 400 forbidden enumeration) | ~450 | -300 |
| Recipe state + plan shape | ~600 | ~550 (interpolated) | -50 |
| Role / pathologies | ~450 | ~280 (positive-form rules in classification-taxonomy) | -170 |
| Inputs HAVE / don't | ~450 | ~400 | -50 |
| Manifest contract | ~600 | ~850 (expanded routed_to enum + rules) | +250 |
| Content surfaces | ~650 | ~900 (expanded contracts + self-review hooks) | +250 |
| Classification taxonomy | ~500 | ~500 | 0 |
| Citation map | ~400 | ~400 | 0 |
| README skeleton | ~1600 | ~1500 | -100 |
| CLAUDE.md descr | ~200 | ~250 | +50 |
| Worker-specific rules | ~700 | ~600 | -100 |
| Workflow / self-review | ~650 | ~1200 (author-runnable aggregate) | +550 |
| Deliverables / return | ~550 | ~500 | -50 |
| **Total** | **~11.3 KB** | **~12 KB** | **+700 B (~6% growth)** |

Writer brief grows slightly — the expanded routing matrix + author-runnable aggregate are new content that closes v34 defect classes. Growth trade-off is justified: +700 bytes of preventive structure vs. eliminating a multi-round convergence loop.

## 5. Silent-drops audit

Every v34 segment covered. Version anchors ("v20-v28") cut per P6 with rationale preserved in positive rules. Forbidden-path enumeration cut per P8 with positive allow-list preserved.
