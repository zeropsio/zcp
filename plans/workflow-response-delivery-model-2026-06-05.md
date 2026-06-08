# Workflow Response Delivery — Reconciled Evaluation + Target Model (2026-06-05)

**Status:** synthesis / proposal. Analysis complete; this is the consolidated target model + decision
set. **Analysis only — no implementation. Karel decides scope.**

**What this is.** Two independent ground-up audits of *what and how ZCP delivers to the agent in every
workflow response*, run in parallel, plus two independent Codex architectural passes — all four converge
on one root and one target model. This doc reconciles them, bakes in Karel's two steering decisions
(clean swap, `zerops_knowledge uri=` stays), and lays out the target delivery model + migration for
Karel's go/no-go.

**Companion docs (peers, not superseded):**
- `plans/workflow-response-delivery-eval-2026-06-05.md` — the parallel-session audit (116 transcripts /
  1433 responses; has the causal close-mode measurement + its own new-bug list). Kept as evidence.
- `plans/response-audit-corpus/` — this session's corpus (428 transcripts / 5298 responses):
  `phase1-digest.md` (30 verified findings), `by-type.md`, `samples.md`, `redundancy-quant.md`, `index.jsonl`.

---

## 1. Verdict

ZCP **computes** the agent's next move as a typed object (`Plan.Primary{label,tool,args,rationale}`,
`blockers[].recovery`, `failureClassification`, `checks[].recovery`) and then **discards the structure at
the delivery boundary** — flattening it into markdown prose on the heaviest, most action-critical paths
(`develop:start` 265×/~24KB, `status` recovery up to 26KB), and shipping the *same concept* ("what to call
next") in **six incompatible field shapes** of which the fully-structured form appears in **2 of 428
responses**. The deeper root (Codex): **there is no canonical agent-decision contract at the delivery
boundary** — control flow is owned inconsistently by renderers, atoms, and per-handler response structs.
Prose-flattening is the most visible symptom.

This is not cosmetic: prose responses are 8.5% of responses but **38% of all delivered bytes**, and the
parallel audit measured a **causal** effect — in 49 runs where the close-mode tell lived only inside the
guidance wall, agents complied **4%**; in 20 runs where it rendered as a positioned DECISION line,
**80%**. Same content, 20× behavior difference by position. **Position is the contract.**

The evals mostly pass *despite* this, not *because* of it (model robustness + duplicated early hints).

---

## 2. Cross-validation — four independent sources, one conclusion

| | This audit (428/5298) | Parallel audit (116/1433) | My Codex | Their Codex |
|---|---|---|---|---|
| Root = decision-head + structured `nextCall` envelope; wall stops being delivery unit | ✓ | ✓ | ✓ (sharpened) | ✓ |
| `ComposeUnderBudget` absent on `renderDevelopBriefing`, present on status | ✓ | ✓ | ✓ | ✓ |
| `availableStacks` on adopt/recipe (needsStacks, no route axis) | ✓ | ✓ | ✓ | — |
| Reachability: next-action below the fold | ✓ | ✓ (causal) | ✓ | ✓ |
| validation-set presented as choice-set (stacks/blockers/buckets) | ✓ | ✓ | ✓ | ✓ |
| Leaf-fixes become secondary under the model; F11→derive-not-stamp; F54→structured terminal | ✓ | ✓ | ✓ | ✓ |

**The one divergence — timing weight:** the parallel audit ranks timing #4 (F11 = 16 mis-branched
develop-starts via stale `deployed`; F60 ordering; T4 render-masking). My audit + my Codex say *demote —
guardrail, not headline.* **Reconciled:** timing is **not** a systemic root (live fields are provably
fresh — availableStacks/workSessionState/verify/launch-active all confirmed live), **but F11 and F60 are
two real isolated bugs.** Fix them as bugs; keep live-truth as an invariant; don't make timing an axis of
the redesign.

---

## 3. Target model — one canonical decision envelope

Every `zerops_workflow` response becomes one envelope; the same `nextCall` object is added to mutation
tools (deploy/import/env/subdomain/scale/manage). Derived from **one owner** (`Plan` + `Recovery` →
shared `AgentCall`/`DecisionHead` vocabulary) **before any guidance renders**.

```json
{
  "status": "active", "phase": "develop-active", "workflow": "develop",
  "decisionHead": { "summary": "app has no successful deploy yet",
                    "why": "auto-close is blocked until deploy and verify pass" },
  "nextCall": { "label": "Deploy app", "tool": "zerops_deploy",
                "args": { "targetService": "app" }, "rationale": "un-deployed edits are not durable" },
  "blockers": [ { "id": "...", "severity": "...", "recovery": { "tool": "...", "args": {...} } } ],
  "liveState": { ... },
  "guidance": {
    "inline": [ "...≤budget, only phase-relevant atoms..." ],
    "omitted": [ { "atomId": "develop-http-diagnostics",
                   "pull": "zerops_knowledge uri=\"zerops://atoms/develop-http-diagnostics\"",
                   "reason": "budget" } ],
    "budget": { "inlineBytes": 4096, "truncated": false }
  }
}
```

**Design rules (Karel's two decisions baked in):**
- **`nextCall` is always an object** (`{label,tool,args,rationale}`), never a copy-paste string. If the
  tool has an `action`, it goes in `args.action` — one call dialect, kill the second `Recovery.Action`.
- **Field order = wire order:** `decisionHead`/`nextCall`/`blockers` serialize BEFORE `guidance`
  (struct-field order in Go). The decision head is physically first; guidance is subordinate and last.
- **Clean swap — NO backward compat for response shapes** (Karel). Response payloads are ephemeral wire
  content read by a stateless LLM each call; ZCP is one auto-updated binary (protocol + agent surface
  ship together). So: convert `develop:start` + `status` from markdown to the envelope, **delete** the
  old prose `nextActions`/`nextStep`/`nextSteps` fields, **no `legacyText`, no dual-field period**, rewrite
  our own tests (RED→GREEN). Compat still holds ONLY for ZCP→disk surfaces (`.zcp/state`, `.claude.json`,
  CLAUDE.md/AGENTS.md, `mcp__zerops__*` tool names + input schemas) — the response shape is not one of them.
- **Guidance demotion uses the live `zerops_knowledge uri=` tool pull** (Karel: it stays — pinned by
  `knowledge_atom_uri_test.go`), expressed as the tool-call stub. Never a bare `zerops://`, never the
  removed resources protocol. Atoms stay the authoring unit; they stop being the carrier of executable
  control flow.

---

## 4. Systemic problems, ranked (post-Codex)

1. **No canonical decision/next-call contract** (the root). CRITICAL.
2. **Validation-set presented as guidance** — availableStacks on adopt/recipe; export 18KB bucket
   taxonomy for a resolved 3-row table; launch 6-row blocker table when 1–2 active; status all-routes menu.
3. **Parallel render paths, inconsistent budgeting** — `ComposeUnderBudget` on status, absent on
   develop:start (50% / 133 of 265 exceed the 24KB budget, max 32.3KB at the transport ceiling).
4. **Router bugs (2 real)** — see §5.
5. **Surface-once duplication** — second-order amplifier (exact re-delivery avg 4%, pockets 15–19% in
   recipe/launch). Real but not first; the ledger lands LAST.
6. **Timing/live-truth** — guardrail. Two isolated bugs (F11, F60), not a systemic axis.

---

## 5. Real bugs — fixable independently of the redesign (union of both audits)

| id | bug | owner | source |
|---|---|---|---|
| **BI-1** | prod build-integration ships `echo "$ZCP_E2E_GITHUB_PAT" \| gh auth login` to every real user (eval-harness env var leaked into production guidance) | `workflow_build_integration.go:312` | parallel; **confirmed here** |
| **Router-1** | `action=status workflow=launch-production` (no active launch) → returns the 7KB BOOTSTRAP route-menu, drops the workflow param | `workflow.go` status fallback (~L443/465) | this audit; Codex confirmed |
| **Router-2** | `action=classify` at launch classify-prompt → recipe-fact handler error referencing concepts that don't exist in launch (overloaded verb, no per-workflow guard) | `workflow.go:499` → handleRecipeClassify | this audit; Codex confirmed |
| **F11** | recipe-buildFromGit never stamps `FirstDeployedAt` → stale `deployed=false` mis-branches 16 develop-starts to the heavy first-deploy branch + wrong instructions. Fix = **derive live, not stamp** | `bootstrap_outputs.go` / develop branch select | both audits |
| **F60** | auto-close gate has no verify-after-deploy temporal ordering — a pre-deploy verify satisfies a post-deploy gate | `work_session.go::serviceAutoCloseReady` | both audits |
| **LX-3** | ZCP's own recipe corpus teaches schema-invalid yaml (`verticalAutoscaling` under `run:`) — caused export validation-failed in 3 runs | recipe corpus + app repo | parallel |
| **deploy-order** | failed-deploy `failureClassification` serializes LAST below ~2KB logs (struct order) | `ops/deploy_common.go` | this audit |
| **DS-1** | dev_server health URL is container-internal `localhost:<port>` (unactionable from agent's seat) | `dev_server_start.go` | parallel |

BI-1 + the two router bugs + deploy-order are S-effort and independent of the model decision.

---

## 6. Migration (clean-swap, phased, eval-gated)

1. **Router bugs first** (Router-1/2) — small, independent, unblock launch flows.
2. **Shared `AgentCall`/`DecisionHead` vocabulary** in `topology` (peer to `Recovery`) + one builder that
   derives the decision head from `Plan`/`Recovery`. No per-handler next-step authoring after this.
3. **Convert all workflow responses to the envelope**, including `develop:start` + `status`, via the ONE
   shared builder (so `ComposeUnderBudget` cannot drift between start/status). **Delete** the old prose
   next-step fields; rewrite pinned tests. (Clean swap — no legacy fields.)
4. **Demote guidance** — cap inline to the phase-relevant atoms + active blockers + the one recommended
   call; move validation tables/taxonomies to the `zerops_knowledge uri=` pull (omitted-stub form);
   gate `availableStacks` on the classic route only.
5. **Per-session surface-once ledger** — LAST, after the envelope is stable.

Bug batch (BI-1, F11 derive, F60 ordering, deploy-order, LX-3, DS-1) can land independently, before or
parallel to phase 1.

**Verification:** deterministic golden tests parse every workflow response and assert presence +
field-order of `status/phase`, `decisionHead`, `nextCall`, `blockers` BEFORE `guidance`, plus the
`nextCall` matches the typed `Plan.Primary`. Live eval gates (4 runtimes) prove ergonomics on: compaction
recovery, launch no-active status, launch classify-prompt, export classify, failed deploy, multi-service
develop. (Per Codex point 5: golden assertions prove mechanism; live eval proves fresh-transcript
ergonomics — both, not either.)

---

## 7. Leaf-fix master plan disposition (both audits + both Codexes agree)

- **Survives as-is:** Router stderr surfacing (F36), planless-discover (F35), F27 call-shape, F43
  pipelineSummary, and tell==check content fixes whose content becomes a pulled atom.
- **Survives reframed:** F11 → derive live `deployed`, not another stamp; F54 `compose-ready` → a
  structured terminal status + nextCall, not mainly a new wall atom.
- **Subsumed by the model:** every fix whose success condition is "make the buried guidance louder" /
  "the atom exists somewhere in the response" — route-menu prose nudges, broad knowledge-bundle guidance,
  atom-wording reachability patches. New gate: **correct live head, executable nextCall, bounded size,
  pullable depth.**

---

## 8. Open decisions for Karel

1. **Adopt the target delivery model?** All four sources recommend it. Scope: full envelope redesign vs
   the independent bug batch only vs both.
2. **Invariant change — errors join the envelope.** Codex + the parallel audit both want error responses
   to carry the same `decisionHead`/`nextCall` discipline. This **replaces** the current P4 invariant
   ("error responses MUST remain leaf payloads"). Explicit Karel call.
3. **BI-1 now** as a standalone shipped-bug fix (independent of the model)?
4. **Guidance inline budget target** — Codex's ~4KB inline cap for develop vs a more conservative
   intermediate; sets the cut-over eval gate.
