# ZCP Goal Contracts — Concept + Spec Sketch

**Date:** 2026-06-09 · **Status:** concept for decision + handover. NO implementation shipped.
**Self-contained:** readable without any prior conversation. Every claim cites an on-disk artifact
(appendix) or a code owner.

---

## 0. TL;DR

ZCP today guides the agent through **prescriptive step machines** (bootstrap discover→provision→close,
develop sessions with a briefing wall, step-complete ceremonies) and teaches by **pushing walls of
prose** at every transition. An empirical audit of **5,298 real response payloads from 428 live agent
runs** — cross-validated by a second independent audit, two independent Codex (gpt-5.5) critiques, and
an adversarially-verified ledger of all 293 errors the construct produced — converges on one verdict:

> **The step cursor and the push-teaching walls are a tax, not a protection. What actually protects
> (gates, preflights, locks, validation) and what actually helps (knowledge, live state, structured
> recovery) are independent of the cursor — and the best-behaving parts of ZCP already work without it.**

**The concept:** replace user-facing step machines with **declarative goal contracts**. The agent
declares a goal; every call answers `status = f(goal, inputs, LIVE state)` → the set of **unmet
requirements** (each with a live check, evidence, dependency edges, and a structured fix), plus ONE
computed `nextCall` recommendation (advice, not a gate), plus pull-on-demand knowledge refs. "Done" is
a **provable state**, not an announcement. Sequencing belongs to the model; ZCP owns truth, safety, and
knowledge.

In one image: today ZCP is a **tour guide with a script**; the concept makes it a **checklist + map +
guard at the dangerous doors**.

Codex verdict on the concept: **"Adopt, amended"** — keep durable state, requirement dependencies,
locks, and hard point-of-action gates; the protections worth keeping "are not the cursor — they are
the checks, ownership, evidence, and destructive-operation fences."

---

## 1. Why — ZCP's purpose, and what the evidence says is wrong

### 1.1 The purpose (owner intent, the design north star)

ZCP exists to (a) **deliver information the agent does not have** — Zerops platform specifics, live
project state — and (b) **set things up the way we want** — topology, conventions, safety. It should be
a **knowledge + harness layer that cooperates with capable models**: maximally efficient, maximally
clear, holding firm ONLY where something is absolutely fixed (platform invariants, destructive-op
safety, project layout/metas). Models today are highly capable at planning and acting but have large
knowledge gaps about specifics — ZCP fills the gaps and guards the rails; it should not serialize the
model's thinking.

### 1.2 Three measured failures of the current construct

**(A) Push-teaching buries the contract.** The single strongest causal measurement in the corpus
(close-mode, 69 runs): when the "set close-mode" tell existed only inside the ~18-atom develop wall,
agents complied in **2/49 runs (4%)**. When the same content rendered as a priority-1 DECISION block,
**16/20 (80%)**. Same content, different position → 20× behavior difference. **Position and structure
are the contract; prose volume is not.**

Supporting size facts (full-corpus, verified): `develop:start` is one undifferentiated markdown wall —
265 instances, p50 ~24 KB, max 32,252 B (at the 32 KB transport ceiling), **zero machine-readable
fields** (no kind/status/nextCall; the next action is a prose sentence at byte ~567 and a `Next:` bullet
at byte ~15,392). 451 of 5,298 responses (8.5%) are unstructured prose yet carry **38% of all delivered
bytes**. The agent's most frequent decision — *what to call next* — ships in **six incompatible
shapes** (`nextCalls[]` structured ×2, `nextCall` string ×44, `nextActions` prose ×1472, `nextSteps[]`
×38, `nextStep` ×194, `Next:` markdown-in-text); the fully structured form exists in 2 of 428 runs'
responses. Export's `classify-prompt` ships 23.7 KB to ask a question whose actionable core is ~713 B.
The status envelope — the documented compaction-recovery primitive — computes a fully-typed
`Plan{NextAction{Tool,Args}}` and then flattens it into 6.5–26 KB of markdown.

**(B) The step ceremony manufactures round-trips.** The construct ledger classified every error in the
corpus (293 errors + 88 failed deploys), each verdict adversarially re-verified against the transcript
AND the handler code:

| Error class (n) | Verified verdict |
|---|---|
| step-complete rejections (64) | **62/64 CONSTRUCT_INDUCED.** Exemplar: the adopt pairing gate refused a call whose own attestation declared "Adopting as a standard dev/stage pair" — the bounce elicits *format*, not *information*. Worst cascade: 11 turns of pure construct navigation after a tell≠check drift (the construct accepted a planless discover, then deadlocked provision on the missing plan). |
| `develop:start` rejections (32) | **32/32 INDUCED.** The gate guards ZCP's own evidence files, not platform state (services were ACTIVE in every instance); after 6 calls of adopt ceremony the **byte-identical** develop start succeeds. |
| route+plan-in-one-call bounce (8/8 runs) | INDUCED (core holds). Agent submits route + a **valid plan** in one call → "plan is not accepted in action=start" → resubmits the byte-identical plan two calls later. |
| deploy errors (67) | Dominant **PLATFORM_REALITY** — preflights and failure classification work. The induced minority (29/67) is the already-deleted DIAGNOSIS_REQUIRED gate + its override-ack tail. |
| git-push/build-integration (72) | **INDUCED REFUTED** — mostly genuine auth/platform failures; the real defect is swallowed diagnostics (delivery), not the construct. |
| import gates (19) | 12 induced hold (the deleted deploy-gate's ceremony tail); the kept import-override gate's *protective surfacing* was confirmed working. |

Methodological validation: the ledger independently re-derived, from transcripts alone, defects ZCP had
already fixed or deleted (the deploy-gate category error, CanonicalBareForm composite/bare matching, the
RecipeShape worker slot) — and it cleared the construct where it genuinely protects. It is not biased
against the construct.

**(C) Knowledge is in the wrong place at the wrong time.** Of the 63 frictions from the 12-round live
battery, tagged by root layer: **17 construct, 13 delivery, 25 knowledge, 1 platform, 6 already-fixed**.
The 25 knowledge items (missing/wrong Zerops facts in atoms/recipes) are an orthogonal content track —
the concept does not magically fix them, but it changes *how* knowledge reaches the agent (pull-first,
positioned, budgeted) so correct facts stop being buried.

What the audit did NOT find: a systemic timing/staleness problem. Live-truth derivation was repeatedly
CONFIRMED (availableStacks from the live schema cache; workSessionState derived per call; verify ordered
after the attempt; launch state read fresh). The few real timing bugs are isolated (F11 `deployed`
proxy, F60 verify-after-deploy ordering — §6). **Timing is a guardrail to keep, not a redesign axis.**

### 1.3 The convergence observation (the strongest argument)

ZCP's own recent architecture has been migrating toward this concept piecemeal, without naming it:

| Already shipped | What it actually is |
|---|---|
| Auto-close is **DERIVED, never stamped** — `EvaluateAutoClose`/`DeriveCloseState`, a 3-input predicate over declared scope recomputed on every read | a goal contract for develop-close — **already live** |
| **launch-production + export are stateless narrowings** — status recomputed each call from inputs + live state, no step pointer, structured `blockers[].recovery` | goal contracts for promote/export — **already live** |
| **adopt auto-derives the plan** from live discovery | contract instead of authoring ceremony |
| Hard-gate doctrine: "gates protect only genuinely-destructive or impossible operations" (the corrective-redeploy gate was deleted as a category error) | point-of-action guards, not sequence guards |

And the empirical ranking matches: **launch — the family closest to the concept — is the best-behaved
family in the corpus** (structured blockers + recovery + nextCall); the worst frictions live in the most
script-shaped families (bootstrap and develop sessions). The redesign **finishes a migration the
codebase already started.**

---

## 2. The concept

### 2.1 One sentence

> **ZCP stops telling the agent WHAT TO DO STEP BY STEP and starts telling it WHAT MUST BE TRUE —
> while recommending (not gating) the best next move.**

### 2.2 Two halves: control and delivery

- **Control half — the goal contract.** The agent declares a goal (`provision`, `first-deploy`,
  `promote-to-prod`, `export`). Every call recomputes `status = f(goal, inputs, LIVE platform state +
  on-disk evidence)` and returns the requirement set with met/unmet states. No stored step cursor; no
  `complete step=…` ceremony. "Done" = all requirements provably met (live checks), not an announcement.
- **Delivery half — the decision envelope.** Every response is ONE canonical structured shape:
  decision head first, ONE executable `nextCall {tool,args}` object (never a prose sentence, never six
  dialects), active blockers with structured recovery, and knowledge as budgeted inline minimum +
  pull-able refs. (This envelope was independently derived by both delivery audits and both Codex
  passes; it survives unchanged as the contract's wire format.)

### 2.3 What stays hard, what dissolves

| **Stays — unchanged or strengthened** | **Dissolves** |
|---|---|
| Destructive-op gates (import-override diagnose-before-destruct, DM-2 self-deploy destruction) | The user-facing step cursor (discover→provision→close as gated sequence) |
| Platform preflights + schema validation **at the point of action** (import, deploy) | `complete step=…` ceremonies and their rejection taxonomy |
| Locks & ownership: hostname locks, registry/session locking, mutation windows (two agents must not half-own one project) | Push-teaching walls at transitions (the 24 KB develop briefing, route re-explanations after the route is committed) |
| Durable memory: `ServiceMeta`, work sessions, deploy/verify attempt history, launch state — **as evidence and memory, not as control cursor** | Route/step taxonomy as an entry interrogation (`availableStacks` on routes that never choose a type) |
| Knowledge atoms as the authoring unit + the live schema catalog | Sequencing gates whose only content is "you are not on the right step" |
| Engine-version stamping of persisted plans (in-flight state safety) | The action-verb collisions inherited from one flat dispatcher (e.g. `classify`) |

### 2.4 Knowledge becomes pull-first

The contract response carries a **hard-budgeted inline slice** (only decision-critical, phase-relevant
content — the measured decision-critical share of the develop wall is ~5 blocks of ~18) plus
`guidanceRefs[]` (`zerops://` URIs with one-line "why you might need this"). The agent pulls depth when
it needs it. Validation sets (full stack catalogs, all-bucket taxonomies, all-blocker tables) are never
presented as choice menus — the response carries the one resolved recommendation and the active
blockers only.

### 2.5 Guided mode (weak-model rail)

Some runtimes (grok, antigravity) may plan worse than Claude. Guided mode is a **rendering/config flag
over the same contract** — it renders one recommended executable `nextCall`, templates, and decision
prompts more insistently. It is NOT a second engine and NOT a return of the cursor. (Codex: "they need
guided rendering, not a separate control system.")

---

## 3. Spec sketch

### 3.1 `GoalContractResponse` — the wire shape (every `zerops_workflow` response)

```jsonc
{
  "goal": { "id": "g_4f2a", "type": "first-deploy",        // provision | first-deploy | develop-iterate
            "intent": "<user words>", "scope": ["app"],     //   | promote-to-prod | export
            "mode": "dev", "owner": "<sessionId/pid>", "createdAt": "…" },
  "status": "needs-input" | "blocked" | "ready" | "running" | "done" | "failed",
  "decisionHead": {                                          // ALWAYS first, ALWAYS small
    "summary": "app has no successful deploy yet",
    "why": "auto-close needs deploy + verify evidence for every in-scope service"
  },
  "nextCall": {                                              // ONE executable object — advice, not gate
    "label": "Deploy app",
    "tool": "zerops_deploy", "args": { "targetService": "app" },
    "rationale": "un-deployed edits are not durable", "confidence": "high"
  },
  "requirements": [
    { "id": "zerops-yaml-valid", "state": "met" | "unmet" | "blocked" | "warn",
      "severity": "required" | "recommended",
      "check": "live-validate against host-derived schema",   // every requirement names its CHECK OWNER
      "evidence": "validated 2026-06-09T10:21Z, sha …",        // why ZCP believes the state
      "dependsOn": [],                                         // edges, not ceremony
      "blockers": [ { "id": "…", "message": "…",
                      "recovery": { "tool": "…", "args": { } } } ],
      "fix": { "tool": "zerops_knowledge", "args": { "uri": "zerops://atoms/scaffold-yaml" } },
      "refs": [ { "uri": "zerops://atoms/…", "why": "…" } ] }
  ],
  "blockers": [],                                            // goal-level (locks, missing inputs)
  "liveState": { "services": [ /* live-derived one-liners */ ], "freshAt": "…" },
  "guidance": {                                              // hard-budgeted inline knowledge
    "inline": [ /* ≤ budget, decision-critical only */ ],
    "refs":   [ { "uri": "zerops://atoms/develop-env-vars", "why": "env refs resolve in-container" } ],
    "budget": { "inlineBytes": 4096, "omitted": ["develop-http-diagnostics", "…"] }
  },
  "session": { "workSessionId": "…", "mutationLock": null },  // memory + safety, not cursor
  "choices": [ /* only when a genuine decision is ZCP-unresolvable: structured options, each with a
                  ready nextCall — replaces prompt-walls and pairing-rejection round-trips */ ]
}
```

Design rules (each kills a measured defect):
- `nextCall` is **always object form** `{tool,args}` — collapses the six-dialect split. The existing
  `Plan{NextAction{Label,Tool,Args,Rationale}}` (`internal/workflow/plan.go`) is the single owner;
  `topology.Recovery` aligns to the same call shape.
- `decisionHead` + `nextCall` + `blockers` serialize **before** guidance — reachability by construction
  (fixes the byte-5262 blocker, the byte-15392 `Next:`, the failureClassification-below-logs ordering).
- One shared response builder → `ComposeUnderBudget` cannot exist on one path and be absent on the
  parallel one (today: wired on status, missing on `renderDevelopBriefing`; 133/265 develop:starts
  exceed the 24 KB budget the composer was built to enforce).
- Unresolvable decisions surface as `choices[]` with ready-to-execute calls — the adopt pairing question
  becomes a structured choice in the SAME response, not an `ErrAdoptPairingChoice` bounce (62/64 of the
  largest error class disappears by construction).
- Errors use the same envelope discipline (decisionHead + structured recovery). **This replaces the P4
  "errors are leaf payloads" invariant — explicit owner decision required (§8).**

### 3.2 Per-goal requirement sets

Semantic requirements with live check owners — **never** `step1/step2/step3` (see §4).

**`provision` (today: bootstrap).** Steps map mechanically to requirements:
| Requirement | Check (live) | Notes |
|---|---|---|
| `route-resolved` | recipe match / classic intent / adopt detection | recommendation precomputed; genuine ambiguity → `choices[]` |
| `plan-committed` | plan validated against live catalog (composite/bare aware); adopt derives it; recipe derives from import-YAML shape | the "reasoning space" survives as *requirement*, not gated step |
| `hostnames-free` | live collision + lock check | lock stays |
| `services-live` | live discover: exists + ACTIVE/RUNNING + types match plan | replaces provision-complete ceremony |
| `env-evidence-recorded` | env keys discovered + written | |
| `metas-written` | ServiceMeta on disk for every target | evidence file, also feeds develop |
| infra-only boundary | **goal invariant**: provision ships no app code / zerops.yaml / first deploy | kept from spec, enforced at action (deploy refuses under an open provision goal), not by a step wall |

Dependency edges: `services-live` dependsOn `plan-committed`; import preflight enforces it at the point
of action (you literally cannot import without a plan — no separate gate needed).

**`first-deploy` / `develop-iterate`.**
| Requirement | Check |
|---|---|
| `scope-declared` | work session scope present (kept as today — auto-close derives from it) |
| `zerops-yaml-valid` | live schema validation + DM-2 self-deploy preflight at deploy time |
| `deploy-succeeded` | platform appVersion ACTIVE (live) |
| `verify-passed` | live verify AT-OR-AFTER the latest successful deploy (fixes F60 by construction — the requirement is temporal by definition) |
| `close-mode-decided` | meta.CloseDeployMode set — surfaced as a `choices[]` decision on the FIRST contract response (the 4%→80% lever, now structural) |
| `session-closed` | derived (EvaluateAutoClose) — unchanged |

What remains of "develop as a workflow" once the wall is pull-based and nextCall is computed: **the
work session** (scope, attempt history, close state) — i.e. memory + the auto-close contract that
already exists. Nothing else. The 32/32-induced `develop:start` rejection disappears: a develop goal on
an un-adopted project returns `status=blocked` + `requirements:[{id:"services-adopted", state:"unmet",
fix:{…adopt call…}}]` — same protection, zero dead-end, and the agent sees WHY.

**`promote-to-prod` (launch).** Already a narrowing; re-expressed on the shared envelope:
`promotables-selected`, `source-control-ready` (the P-LP-10/11 gate — stays, it is genuine evidence),
`env-classified`, `bundle-valid`, `launch-key-present`, `mutation-resumable` (launch state file —
stays), `post-launch-checklist`. Delivery fix: active blockers only; the six-row reference table
becomes a ref.

**`export`.** `target-selected`, `variant-chosen`, `env-classified` (suggestedBucket precomputed;
inline only per-row suggestion + override instruction — the 18 KB taxonomy becomes a ref),
`bundle-valid` (structure-only schema, as today), `publish-configured-or-skipped`.

### 3.3 Sessions, state files, compaction

Sessions/state files **do not disappear — they change role**: from control cursor to **memory +
evidence + safety** (scope, attempts, attestations, locks, launch resume state, engine-version stamp).
Compaction recovery becomes trivially better: `action=status` (or any goal call) returns the same small
contract — goal + met/unmet + nextCall — instead of today's 6.5–26 KB prose re-dump. Byte-determinism
for recovery is preserved (derived fields come from the same deterministic computations as today's
envelope).

### 3.4 Gates that remain (the complete list)

1. `zerops_import override=true` diagnose-before-destruct (+ structured retryCall corrective — as today).
2. DM-2 self-deploy source-destruction preflight.
3. Launch source-control evidence gate (P-LP-10/11) and existing-project conflict ack (P-LP-12).
4. Schema/preflight validation at every mutating call (import, deploy, env).
5. Locks: hostname ownership, mutation windows, one-goal-per-scope.

Everything else that today refuses with "wrong step / not bootstrapped / plan not accepted here"
becomes an unmet requirement with a structured fix.

### 3.5 Tool surface & compatibility

ZCP is a **local binary speaking MCP — the protocol updates atomically with the binary.** Agents read
each response fresh each session; no response shape persists anywhere. Therefore response shapes are
free to change wholesale; **no additive/legacy-field migration is needed.** The real compat surface:
- **Tool names** — keep `zerops_workflow` (users' permission allowlists pin `mcp__zerops__*`). Goal
  semantics live inside: `workflow=bootstrap` aliases `goal=provision`, etc. No rename.
- **ZCP-written files on user disk** (CLAUDE.md sections, .mcp.json) — untouched by this concept.
- **In-flight state files** — already protected by engine-version stamping (mismatched plans refuse).

---

## 4. Anti-patterns — how NOT to rebuild the script through the back door

This is the concept's main failure mode (Codex's sharpest objection). Binding rules:

1. **Requirements must be semantic, never ordinal.** `plan-committed`, not `step-2-done`. If a
   requirement's name describes position instead of truth, it is the cursor in disguise.
2. **Every requirement names its check owner and carries live evidence.** A requirement without a
   live check is a stamp — the exact drift class the auto-close redesign killed.
3. **Dependencies are edges enforced at the point of action**, not pre-emptive refusal walls.
4. **`nextCall` is advice.** The agent may call any tool in any order; only §3.4 gates refuse.
   If "recommended" hardens into "rejected unless", the script is back.
5. **`done` requires evidence, not announcement** — verification cannot be skipped just because
   nothing gates it; it is an unmet requirement blocking `done`.
6. **Stored files are evidence, not truth** — live platform wins on conflict; reconcile + report
   (never silently mutate, never reject healthy reality).
7. **Guided mode is rendering, not control** — one engine, one contract, two verbosities.

---

## 5. Build order + verification

Order (Codex-proposed, evidence-weighted; each phase independently shippable and eval-gated):

| Phase | Content | Why this order |
|---|---|---|
| 0 | **Router bugs** (`workflow.go` status-fallthrough returning bootstrap content for `workflow=launch-production`; unguarded `action=classify` verb collision) + the real-bug batch (§6) | independent of redesign; removes known agent misdirection before measuring anything |
| 1 | Shared types: `GoalContractResponse`, `AgentCall` (one owner: `Plan.NextAction` ⇄ `topology.Recovery` unified), one shared response builder with `ComposeUnderBudget` inside | the envelope is the keystone; everything else renders through it |
| 2 | **Convert develop/status** (develop:start + lifecycle status onto the envelope) | smallest control-model change, biggest measured win (25% of all delivered bytes; the 4%→80% close-mode lever; the compaction-recovery surface) |
| 3 | **Bootstrap → `provision` goal**: requirement evaluation replacing step gates; pairing/route decisions as `choices[]`; `availableStacks` only where a type is actually chosen (classic plan authoring) | kills the largest induced-error class (62/64) and the bootstrap walls |
| 4 | **Launch + export onto the shared envelope** (control model already conforms) | mostly delivery work: active-blockers-only, refs for tables |
| 5 | **Knowledge pull-first defaults** (inline budget enforcement, refs everywhere, section-addressable knowledge for the 36 KB monolith) | after the envelope exists, demotion has a place to point |

Out of order = out of scope: per-session surface-once ledger (real but second-order: 4–6% avg
re-delivery; revisit after the envelope stabilizes).

**Verification per phase:**
- **Deterministic golden tests** parse every workflow response and assert: envelope fields present,
  `decisionHead`/`nextCall`/`blockers` serialized before guidance, `nextCall` is object-form, inline
  guidance under budget, requirement checks name owners. (Mechanism correctness — cheap, exact.)
- **Flow-eval gates** per phase on eval-zcp (ergonomics on live runs): scenario set must include
  compaction recovery, launch no-active status, launch classify step, export classify,
  failed deploy, multi-service develop, adopt standard-pair (the ex-pairing-bounce), and the
  close-mode-decision scenario (asserting the 80%-style compliance holds structurally).
- Acceptance signal for the concept overall: the **construct-ledger metric** — re-run the error
  classification on fresh transcripts; CONSTRUCT_INDUCED share of step/ceremony classes should
  collapse toward zero while protective gates stay intact.

---

## 6. Independent real-bug batch (ship regardless of any redesign decision)

Found by the audits; all verified against code; none depends on the concept:

| Bug | Owner | One-line fix shape |
|---|---|---|
| **BI-1** production `build-integration` ships the eval-harness env var `ZCP_E2E_GITHUB_PAT` in `ghAuthPrecondition.setupCommand` to every real user | `internal/tools/workflow_build_integration.go:312` | user-facing auth precondition must not reference harness env; instruct asking the USER for a PAT |
| Router: `action=status workflow=launch-production` with no active launch falls through to the bootstrap route-menu (7 KB wrong-workflow prose) | `internal/tools/workflow.go` status dispatch | launch-scoped `no-active-launch` + nextCall |
| Router: `action=classify` routed unconditionally to the recipe-fact handler (wrong-domain error at launch classify step) | `internal/tools/workflow.go` classify dispatch | guard by active workflow |
| **F60/T2** auto-close accepts a verify that PREDATES the last deploy (no temporal ordering) | `work_session.go::serviceAutoCloseReady` | require verify-at-or-after latest successful deploy |
| **F11/T1** recipe-buildFromGit target never stamps `FirstDeployedAt` → stale `deployed=false` mis-branches 16 corpus develop-starts into first-deploy scaffolding | bootstrap outputs / envelope derivation | derive `deployed` from live state (derivation > another stamp) |
| **DEV-2** the close-mode DECISION atom carries axis `deployStates:[deployed]` → structurally cannot render at first develop-start (the 4%-compliance moment) | `develop-strategy-review.md` axes | axis fix (the cheapest big lever pre-redesign) |
| **LX-3** ZCP's own recipe corpus teaches schema-invalid yaml (`verticalAutoscaling` under `run:` in `nodejs-hello-world` recipe + its app repo) | recipe corpus (sync push) + recipe-app repo PR | move to import-yaml; re-validate corpus against live schema |
| **BOOT-3** bootstrap-verify atom names status `NOT_YET_DEPLOYED` — occurs in 0/1,433 payloads (real: `READY_TO_DEPLOY`) | `bootstrap-verify.md` | tell==check sweep |
| failed-deploy: structured `failureClassification` serializes LAST, below ~2 KB of logs (byte 2594 vs 643) while the contract says read it FIRST | `internal/ops/deploy_common.go` struct order | reorder wire fields (struct order = wire order) |
| **PT-3** import-gate recovery points at `zerops_logs` for failures that produce no build container → 30 B empty response, blind-groping loops | `tools/import.go` + logs empty-case | empty-result responses carry why + nextCall |
| **DS-1** dev_server reports container-internal `http://localhost:<port>` (47/49) | `dev_server_start.go` | report the reachable URL |

---

## 7. Out of scope

- **Aleš's recipe-authoring engine** (`internal/recipe/`, v3) — a phase machine for AUTHORING corpus
  artifacts with per-phase sub-agent contracts and quality gates. Both Codex passes: do not dissolve;
  it may later adopt the delivery envelope only. Any touch follows the flag-and-discuss protocol.
- **The 25 knowledge-content frictions** (wrong/missing Zerops facts) — parallel content track
  (atoms/recipes/sync push), independent of the control redesign.
- Surface-once session ledger — deferred (see §5).

## 8. Open decisions (for the owner / implementer)

1. **Adopt the concept?** Full goal-contract redesign (§5 order) vs envelope-only (phases 1–2 + 4–5,
   keeping bootstrap's step machine) vs status quo + bug batch (§6 only). The evidence supports full;
   phases are independently valuable if stopped early.
2. **P4 invariant change:** errors join the envelope (structured decisionHead + recovery) — replaces
   "error responses MUST remain leaf payloads". Recommended: yes, in phase 1.
3. **Size budget for the inline guidance slice** — Codex target 2–5 KB session-start vs the
   conservatively measured 9.4 KB intermediate. Pick the golden-test gate per phase 2.
4. **Guided-mode default** per client runtime (off for Claude; on for grok/antigravity?) — decide at
   phase 2 with eval data from at least two runtimes.

---

## Appendix — evidence artifacts (all on disk)

| Artifact | Content |
|---|---|
| `plans/response-audit-corpus/by-type.md`, `samples.md`, `samples/*.json`, `index.jsonl`, `stats.json` | the corpus: 5,298 real payloads / 428 transcripts / 93 response-kinds, sizes + component breakdowns + full representative payloads |
| `plans/response-audit-corpus/redundancy-quant.md` | prose-vs-structured byte split (38%/8.5%), within-session re-delivery (4–6% avg, 15–19% recipe/launch), most-re-sent blocks |
| `plans/response-audit-corpus/phase1-digest.md` + `phase1-findings.json` | audit #1: 9 lenses × 7 dimensions, 75 findings, 30 adversarially verified-hold (1 critical, 14 high), 8 refuted |
| `plans/workflow-response-delivery-eval-2026-06-05.md` | audit #2 (independent parallel run, own corpus 1,433/116, own Codex pass): converges on the same root + target model; source of the 4%→80% close-mode measurement and the §6 bug list (BI-1, LX-3, …) |
| `plans/response-audit-corpus/construct-ledger.md` + `construct-ledger.json` | the help-vs-hurt ledger: all 293 errors + 88 failed deploys classified protective/induced/platform/knowledge-gap, every induced class adversarially re-verified; 63-friction layer tagging (17/13/25/1/6) |
| Codex pass 1 (delivery model) | verdict "no canonical agent-decision contract at the boundary"; canonical envelope; counter-argument rulings; preserved in audit #2 §5 + this doc §2.2/§3.1 |
| Codex pass 2 (concept) | verdict "Adopt, amended"; steelman of the script; per-goal requirement mapping; anti-pattern list; build order — folded into §2–§5 |
| `plans/flow-eval-friction-report-2026-06-05.md`, `plans/flow-eval-battery-2026-06-04.md`, `plans/flow-eval-fix-master-plan-2026-06-05.md` | the 63-friction battery report + raw log + the (parked) leaf-fix plan this audit subsumes |
| `plans/workflow-response-delivery-audit-2026-06-05.md` | the original audit brief (7 dimensions, method) |
| `docs/spec-workflows.md`, `CLAUDE.md` Information-Contract section | the current construct spec + the philosophy the concept operationalizes |
