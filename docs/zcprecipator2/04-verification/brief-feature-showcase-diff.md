# brief-feature-showcase-diff.md

**Purpose**: diff against [../01-flow/flow-showcase-v34-dispatches/implement-all-6-nestjs-showcase-features.md](../01-flow/flow-showcase-v34-dispatches/implement-all-6-nestjs-showcase-features.md). v34 length: 14816 chars / 128 lines.

## 1. Removed from v34 → disposition

| v34 segment | Disposition | New home |
|---|---|---|
| L14 library-import verification preamble (the "training-data memory for library APIs is version-frozen..." paragraph) | load-bearing → atom | `briefs/feature/mandatory-core.md` first paragraph — kept verbatim as a policy rule at the head of the core atom |
| L16–17 role framing ("You are the feature sub-agent for the `nestjs-showcase` Zerops recipe...") | dispatcher/recipe-specific mixed → split | Role framing moved to `briefs/feature/task.md` (recipe-agnostic "you own the features end-to-end across mounts"); recipe slug is interpolated or omitted |
| L18–21 "Scaffold state (already shipped, do NOT regress)" bulleted state summary per codebase | load-bearing → atom | `briefs/feature/symbol-contract-consumption.md` scaffold-state summary section — but re-framed from "what scaffold has produced" to "what the contract + mounts carry at this substep" |
| L21 tail: "DB_PASS / NATS_PASS (NOT *_PASSWORD — this was fixed earlier)" | **version-log leakage** (partial) | "this was fixed earlier" clause cut. The contract names DB_PASS/NATS_PASS; the positive form is sufficient. (P6) |
| L23–29 "Managed services, env var names (exact)" list | load-bearing → atom + contract | `SymbolContract.EnvVarsByKind` JSON supersedes the prose list. Kept as a summary reminder in the scaffold-state section. |
| L31–41 `<<<MANDATORY>>>` wrapper + body | dispatcher → DISPATCH.md (wrapper), load-bearing → atom (body) | `mandatory-core.md` + `principles/where-commands-run.md` + `diagnostic-cadence.md` hold the body. Wrapper is redundant in the atomic model. |
| L43 "Where commands run:" short prose | load-bearing → atom | `principles/where-commands-run.md`. |
| L45–81 Feature list (each feature's surfaces, healthCheck, detailed apidev / appdev / workerdev tasks) | load-bearing → atom | `briefs/feature/task.md` — same content, tightened phrasing, grouped per-feature. |
| L76 "**Pragmatic approach**: treat `preview` as an alias of `sent` in the `[data-status]` text so the plan's MustObserve passes." | scar tissue (rewrite needed — see simulation A2) | Rewritten in `task.md` to explain WHY the alias exists, not just "so MustObserve passes." |
| L83–90 "## Contract discipline (required)" numbered list | load-bearing → atom | `task.md` Contract discipline section (keep verbatim — it's the positive-form workflow per feature). |
| L92–100 "## UX quality contract" bullets | load-bearing → atom | `briefs/feature/ux-quality.md`. Content kept verbatim; atom-level reorganization is cosmetic. |
| L102–117 "## After implementing features" numbered list | load-bearing → atom | `task.md` "After implementing all features" — kept verbatim. |
| L119–127 "## Fact recording (MANDATORY — ≥5 calls)" | load-bearing → atom | `principles/fact-recording-discipline.md` (pointer-included) — expanded with FactRecord.Scope + RouteTo routing per P5. |

## 2. Added to new composition

| Content | Closes defect class |
|---|---|
| `SymbolContract` JSON interpolation | **v34-cross-scaffold-env-var** — the feature sub-agent consumes the same contract scaffold sub-agents used; env-var names are byte-identical. |
| `FactRecord.RouteTo` field introduction in fact-recording-discipline (pointer-included) | **v34-manifest-content-inconsistency** — v34 worker-dev DB_PASS gotcha was routed to claude-md in manifest but shipped as gotcha. Setting RouteTo at record-time lets the writer enforce routing by source, not by post-hoc re-classification. |
| `diagnostic-cadence.md` atom (positive form: max 3 probes per hypothesis, batches separated by non-probes) | **v33-diagnostic-probe-burst** — v34 Fix D shipped an explicit cadence rule; the atom re-frames it positively ("max 3 per hypothesis") rather than as enumerated prohibitions. P8. |
| `PriorDiscoveriesBlock(...)` slot at tail | **v34-manifest-content-inconsistency** (partial) — scaffold sub-agents' downstream-scope facts land here so the feature sub-agent sees cross-scaffold contracts already in place (e.g. env-var names, NATS queue-group decision) without re-deriving. |
| Explicit library-import verification (kept verbatim from v34 preamble) as the FIRST paragraph of `mandatory-core.md` | **v29-circular-import** + v-several library-version-drift classes — library-import verification is the positive form of the "verify before import" rule; moving it to the first paragraph hoists it above every other rule. |
| `principles/platform-principles/01..06.md` pointer-include | **v30 worker SIGTERM** (preserved across feature changes — principle 1 reminder) + **v31 apidev enableShutdownHooks** (preserved) + **v22 nats creds** (principle 5) + **v22 queue-group** (principle 4) |

## 3. Boundary changes

| Axis | v34 | New |
|---|---|---|
| Audience | Mixed (role framing + task + dispatcher-implied subordinates) | Pure feature-sub-agent; dispatcher text in DISPATCH.md |
| Env-var names | Prose list + parenthetical "fixed earlier" | Contract JSON |
| Probe cadence | "at most THREE targeted probes" in MANDATORY block | `diagnostic-cadence.md` atom with positive form (3 per hypothesis) + clearer batch-vs-hypothesis boundary |
| Fact recording | "MANDATORY — ≥5 calls" count threshold | discipline atom with scope + RouteTo routing (quality, not count) |
| Version anchors | Zero in v34 feature dispatch (was a clean one) | Zero |

## 4. Byte-budget reconciliation

| Segment | v34 | new | delta |
|---|---:|---:|---:|
| Library-import preamble | ~500 | ~500 | 0 (kept verbatim) |
| Role framing + scaffold state | ~1800 | ~1400 (contract JSON summary) | -400 |
| Managed services list | ~800 | ~100 (contract reference) | -700 |
| MANDATORY body + wrapper | ~900 | ~400 (mandatory-core + pointer-includes) | -500 |
| Feature list (6 features) | ~5500 | ~5200 (minor prose trim) | -300 |
| Contract discipline | ~700 | ~700 | 0 |
| UX quality | ~700 | ~700 | 0 |
| After implementing | ~1000 | ~950 | -50 |
| Fact recording | ~500 | ~450 (pointer-include + minor expand) | -50 |
| Platform principles (new) | — | +300 | +300 |
| PriorDiscoveries slot | — | +50 | +50 |
| **Total** | **~14.8 KB** | **~10.75 KB** | **-4 KB (~27% reduction)** |

## 5. Silent-drops audit

Every v34 line covered. Only "this was fixed earlier" clause is cut (version-log noise per P6); the positive content (use DB_PASS, NATS_PASS) is preserved via the contract.
