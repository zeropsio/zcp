# brief-writer-minimal-diff.md

**Purpose**: diff against the current minimal writer flow. Per RESUME decision #1 there is no live v34 minimal session log; reference is the spec-derived flow + the `readme-with-fragments` block in recipe.md L2205-L2388 (the old v8-shape minimal writer brief).

Current minimal writer artifact: [../01-flow/flow-minimal-spec-dispatches/readme-with-fragments.md](../01-flow/flow-minimal-spec-dispatches/readme-with-fragments.md) — ~21 KB block, v8 pre-fresh-context shape, dispatched or consumed-inline at main's discretion per recipe.md.

## 1. Removed from current-minimal → disposition

| Current (readme-with-fragments block) segment | Disposition | New home |
|---|---|---|
| Block opening "### Per-codebase README with extract fragments (post-deploy `readmes` sub-step)" + MANDATORY TOOL-USE POLICY preamble | load-bearing → atom (body) + dispatcher → DISPATCH.md (meta) | `mandatory-core.md` + principles pointer-includes. Recipe-slug stripped. |
| "Permitted tools" + "Forbidden tools" lists | load-bearing → atom | `mandatory-core.md`. |
| `<<<MANDATORY>>>` wrapper body | load-bearing → atom | Pointer-included principles (file-op-sequencing, tool-use-policy, where-commands-run). |
| "This is the `readmes` sub-step of deploy." framing + "A speculative gotchas section written during generate is the root cause of the authenticity failures in v11/v12." | **version-log leakage** + load-bearing rationale | "v11/v12" anchor cut per P6. Fresh-context-premise atom absorbs the rationale in positive form. |
| "README.md vs CLAUDE.md" distinction paragraph | load-bearing → atom | `content-surface-contracts.md` rows 1-4. |
| "For a dual-runtime showcase, that is 6 files..." prose | tier-mixed → atom (tier-branched) | `canonical-output-tree.md` with hostname interpolation per tier. |
| `prettyName` from workflow response for titles | load-bearing (interpolation) | Stitcher interpolates plan.Research.PrettyName. |
| Critical formatting: "Invented variants like `<!-- FRAGMENT:intro:start -->` or `<!-- BEGIN:intro -->` are rejected" | **enumerated prohibition** (P8 violation) | Rewritten positively: "Fragment marker shape is `<!-- #ZEROPS_EXTRACT_START:name# -->` / `<!-- #ZEROPS_EXTRACT_END:name# -->` — exact bytes." |
| Full README + CLAUDE.md skeleton code blocks | load-bearing → atom | `content-surface-contracts.md` embeds the canonical skeletons. |
| "**Worker production-correctness gotchas** (MANDATORY for every `isWorker: true` target...)" section | tier-conditional → atom | Retained in showcase writer composition; filtered OUT for minimal (minimal has no worker). Closes v30 class at showcase side; minimal doesn't need it. |
| "Framework × platform gotcha candidates to consider" — SDK module-system boundary, bundler major shift, reconnect-forever, etc. | load-bearing → atom (but trimmed) | Absorbed into classification-taxonomy examples. The pattern "here are candidate classes to consider when writing gotchas" is guidance, not rules; kept as informational in taxonomy. |
| "Completion" example attestation paragraph at block tail | load-bearing → atom | `completion-shape.md` (attestation text is main-agent's responsibility; writer completion is bulleted deliverables). |
| v18-anchor references inside current block (v18 appdev regression / v17 compliance / v7-v14 gold-standard runs) | **version-log leakage** | Cut per P6. Positive rules preserved. |

## 2. Added to new composition

| Content | Closes defect class |
|---|---|
| `fresh-context-premise.md` atom (even in main-inline path) | **v28-debug-agent-writes-content** — current minimal has no fresh-context premise; it absorbed the showcase-writer's v8.94 shape only in showcase. Minimal writer's old `readme-with-fragments` block has no structural protection against debug-memory bias. New composition closes the gap. |
| `routing-matrix.md` atom with `routed_to` enum and all-pairs honesty | **v34-manifest-content-inconsistency** — current minimal has NO content manifest requirement; no routing discipline. New composition brings manifest contract to minimal. |
| `manifest-contract.md` requiring ZCP_CONTENT_MANIFEST.json | **v29-ZCP_CONTENT_MANIFEST-missing** at minimal tier |
| Positive-form `canonical-output-tree.md` | **v33-phantom-output-tree** closure at minimal (current minimal block has no canonical-output-tree rule) |
| `self-review-per-surface.md` author-runnable aggregate | **v34-convergence-architecture** — minimal's current flow had no runnable pre-attest; adds one. |
| `FactRecord.RouteTo` field in manifest schema | Routes minimal's facts the same way showcase's are routed |

## 3. Boundary changes (structural)

| Axis | Current (readme-with-fragments) | New |
|---|---|---|
| Audience | Mixed dispatcher + sub-agent (same as showcase v8 shape) | Pure sub-agent (atoms); audience-mode-aware stitch for main-inline |
| Tier shape | Shared block for minimal + showcase (but showcase migrated to content-authoring-brief v8.94) | Same atom tree for both tiers; tier-conditional sections within atoms |
| Manifest contract | Absent (v29 retrofit never shipped to minimal block) | Required (`manifest-contract.md`) |
| Fresh-context premise | Implied but not explicit | Explicit atom with main-inline note |
| Positive-form output tree | Absent | Canonical allow-list (P8) |
| Worker-specific rules | Mandatory for every `isWorker:true` target (even minimal — which has no worker — so the rule was vestigial) | Tier-conditional in atom — applies in showcase, not in minimal |
| Version anchors | "v7-v14 gold-standard", "v11/v12", "v17 floors", "v18 regression" | None (P6) |

## 4. Byte-budget reconciliation

| Segment | Current minimal (~21 KB block) | New minimal (~9 KB) | delta |
|---|---:|---:|---:|
| TOOL-USE + MANDATORY (duplicated, role-mixed) | ~3 KB | ~0.7 KB | -2.3 KB |
| Dispatcher framing + recipe-slug | ~500 | 0 | -500 |
| Framework × platform gotcha candidates prose | ~5 KB | ~1 KB (in classification-taxonomy examples) | -4 KB |
| README skeleton | ~2 KB | ~1.5 KB | -500 |
| CLAUDE.md skeleton + examples | ~2.5 KB | ~2 KB | -500 |
| Worker production-correctness section (applied to minimal vestigially) | ~2 KB | 0 (tier-filtered out) | -2 KB |
| Fact-routing + manifest (NEW for minimal) | 0 | ~2 KB | +2 KB |
| Self-review aggregate (NEW for minimal) | 0 | ~1 KB | +1 KB |
| Version anchors + v-references prose | ~1 KB | 0 | -1 KB |
| Fragment-rules + positive canonical tree | ~1.5 KB | ~1 KB | -500 |
| **Total** | **~21 KB** | **~9 KB** | **-12 KB (~57% reduction)** |

Largest tier-level reduction of any role (57%). Current minimal consumed the v8-era monolithic block whole; new composition tier-filters showcase-only concerns and drops version-anchored storytelling.

## 5. Silent-drops audit

Every current minimal block segment covered. Version anchors cut per P6 with rationale preserved in positive rules. Worker production-correctness section removed for minimal tier (no worker); preserved for showcase writer. Framework × platform gotcha candidates prose (the long list of SDK module-system / bundler / reconnect / search-index / static-mount patterns) trimmed to compact examples inside classification-taxonomy — the patterns live as CLASSIFICATION EXAMPLES, not as a standalone guidance list.
