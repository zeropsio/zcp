# Workflow Response Delivery — Ground-Up Evaluation (2026-06-05)

**What this is:** the audit commissioned by `plans/workflow-response-delivery-audit-2026-06-05.md`.
Object of study: the ACTUAL bytes ZCP sends the agent — 1,433 real response payloads extracted from
116 live flow-eval transcripts (battery 2026-06-03..05), evaluated against the Information-Contract
principles plus the Codex lens (*"a correct fact buried below the fold is not a correct tell, and a
boolean that was true before the last mutation is not live truth"*).

**Method:** Phase 0 deterministic extraction (`/tmp/zcp-response-audit/{corpus.jsonl,stats.md,samples/}` —
every payload with size + component breakdown, per-type p50/p90/max samples) → Phase 1 Workflow
fan-out (5 family agents + 4 dimension agents, 35 agents total, every HIGH claim adversarially
verified: **12 CONFIRMED, 14 WEAKENED-but-core-holds, 0 REFUTED**) → Phase 2 independent Codex
(gpt-5.5 xhigh) architectural critique → this synthesis. Full structured findings:
`/tmp/zcp-response-audit/workflow-result.json` (transient; load-bearing numbers are inlined below).

**Analysis only. No implementation shipped. Karel decides.**

---

## 1. Headline empirics — what ZCP actually sends

| metric | value |
|---|---|
| total payload volume | 5.16 MB / 1,433 responses / 116 runs (~44.5 KB per run avg) |
| heaviest single type | `workflow:start:develop`: 77×, **p50 17.9 KB**, p90 22.7 KB, max 28.4 KB — **1.27 MB = 25% of ALL bytes** |
| develop-start anatomy | decision head p50 **852 B (4.5%)**; guidance wall 17.3 KB (95.5%), p50 18 atoms / 23 blocks |
| bootstrap family | 528 responses, **2.14 MB = 41% of corpus**, family p50 4.4 KB |
| format census | bare JSON 672 resp (46%, 16% of bytes) · JSON-with-embedded-markdown 644 (44%, **50% of bytes**) · raw markdown (develop) — the remaining ~34% of bytes |
| error census | 103 errors; **84/103 (81.5%)** carry the static `recovery={status}` pointer while the REAL corrective call sits in the escaped error prose |
| deterministic error round-trips | adopt pairing rejection: **32/32 adopt-plan runs**; git-push-setup: 26 errors of which **14 wasted retries** (one run got the identical 456 B error 5×) |
| within-run re-dump | 330,888 B = **6.4% of all bytes**; worst runs 23–33% (launch 33%, export 23–24%) |
| knowledge monolith | `scope=infrastructure` = **36–38 KB single response** (28.7 KB of it YAML reference) |
| next-action form | 3,581 tool-commands inline in prose (405 with unfilled `<placeholders>`) vs a structured executable call in a handful of sites |

**The single strongest causal measurement in the corpus (close-mode, F2/F15):** in the 49 runs
where the close-mode tell existed only inside the 18-atom wall, agents set close-mode before first
deploy in **2/49 (4%)**. In the 20 runs where the priority-1 DECISION atom actually rendered (
already-deployed scenarios), **16/20 (80%)** complied. Same content, different position/salience →
20× behavior difference. Position is not cosmetic; it is the contract.

And the mechanism bug behind it (DEV-2, verified): `develop-strategy-review.md` (the DECISION atom)
carries axis `deployStates:[deployed]` — **it structurally cannot render on a fresh first
develop-start** (all services `deployed=false`), which is exactly the moment its own sibling atom
(`develop-first-deploy-intro.md:31-39`) tells the agent to set close-mode. The tell that works is
axis-locked out of the moment it's needed.

---

## 2. Delivery anatomy per family (what's in the bytes)

### 2.1 Bootstrap (41% of corpus) — JSON envelope, actionable info at the END
- **Envelope inversion (REACH-4):** in bootstrap JSON the actionable `message`/instruction sits at
  byte-fraction **0.72–0.99** — behind routeOptions/detailedGuide. Text envelopes (develop) put the
  head first; JSON envelopes put it last. Same product, inverted reading order.
- **`availableStacks` on routes with no type decision (BOOT-4, verified):** `needsStacks()`
  (`workflow_bootstrap.go:31-36`) keys on step name only — the 1.2 KB type catalog ships on all 71
  adopt+recipe responses where the agent authors no types. Validation set as presentation set,
  87.5 KB wire across the battery.
- **Recipe import YAML up to 3× per run (BOOT-5/RD-3, verified):** menu preview → route-start
  "for reference" copy (27/28 redundant re-delivery) → discover-complete rewritten copy (the only
  copy whose edits matter). Two thirds of the historical duplication already fixed on main
  (`keepBestFitImportYAMLOnly` 1205dc4f, R3-P4.4 6b6cbc24); the start-time "for reference" copy remains.
- **Adopt = a deterministic ZCP-authored error round-trip (BOOT-1):** the adopt guide's own
  recommended scope-call hits `ErrAdoptPairingChoice` in 32/32 same-base-pair runs. Agents recover
  in ONE call (the error embeds both paste-ready plans — good), but the round-trip itself is by
  construction: the guide already knows the two templates and defers them to an error. The static
  `suggestion` field re-describes the failing call shape (`workflow_bootstrap.go:76-79`) — noise,
  though no agent looped on it.
- **Preemptive failure-mode content (BOOT-6):** ~830 B of READY_TO_DEPLOYED stuck-build diagnosis
  ships in all 110 non-error provision guides for a state that doesn't exist yet; the env-discovery
  atom body enumerates BOTH route branches despite having a routes axis (axis filters presence, not body).
- **Post-commit route re-explanation (BOOT-7):** every route-start guide opens with an 824 B
  preamble re-explaining all three routes AFTER the route is committed.

### 2.2 Develop (25% of bytes in ONE type) — raw markdown, 4.5% head + 95.5% wall
- **Decision-critical share (DEV-1, verified):** at first-develop delivery only ~5 blocks
  (~6.2 KB of 17.3 KB guidance) are decision-critical for the next 1-3 turns. Measured next-3-call
  distribution across 69 runs: deploy/dev_server/verify/close-mode = 87% of actual next calls.
  Failure-path atoms (HTTP diagnostics 1.3 KB, "If unhealthy", DM-2 destruction risk) deliver
  pre-failure; their tells re-surface anyway at trigger time (failureClassification, DM-2 preflight error).
- **Head is 2-input, gate is 3-input (REACH-1, verified):** the head's only action line
  (`blockerNextAction`) emits deploy|verify only — close-mode requirement invisible in the head in
  73/73 payloads (the Services line does show `closeMode=unset`, but as state, not as required action).
  Head-follow rate: 57.5% exact-next, 83.6% within 3 calls — and the dominant mismatch is agents
  doing `close-mode` (14×) — the action the head never names.
- **ComposeUnderBudget never guards the heaviest path (DEV-3):** wired on status
  (`tools/workflow.go:1079`) + bootstrap assembly, **NOT on `renderDevelopBriefing`**
  (`workflow_develop.go:292`) — the one path producing 17–28 KB payloads. (Corrects the friction
  report's "wired but never fires": it's not wired where it matters.)
- **Triple-told rules in one payload (DEV-4/DEV-5):** deployFiles-must-be-`[.]` told on 3 surfaces;
  verify told by 3 overlapping blocks (~2.5 KB combined).
- **Second develop session re-renders full-fat (RD-4):** 43% line-identical with the first wall.

### 2.3 Launch + export — prompt-chains that re-dump themselves
- **Export "stateless narrowing" re-delivers 83% (RD-1/LX-2, confirmed):** `export-intro` composes
  into EVERY non-error export response; `export-validate` fires on both `classify-prompt` AND
  `validation-failed`, so the validation-failed response is a structural verbatim subset
  (8.1/13.8 KB) of the response one call earlier. One scenario received the identical 23.6 KB
  scope-prompt 3×.
- **Launch blocker table is static (LX-1/RD-2, confirmed):** the full 6-row reference table
  (4,791 B) renders byte-identically on all 19 `source-control-required` responses regardless of
  which blockers are live — and the protocol mandates re-calls between fixes, so it re-attaches each time.
- **Classify-prompt asks what topology already knows (LX-4):** GIT_TOKEN/ZCP_API_KEY are hard-wired
  in `topology.classifyInfrastructureKeys`, yet the prompt asks the agent to classify them (2 of 3
  rows in corpus prompts).

### 2.4 Deploy-config actions — the cost is turns, not bytes
- **git-push-setup 72% error rate; stderr swallowed (GPS-1, confirmed = F36):** `SSHExecError.Output`
  exists (`platform/errors.go:127`) and is discarded; 6 retry chains up to 5 calls, 4 identical-input
  retries. **The fix is turn-elimination, not byte-trimming.**
- **Suggestions address a human, not the agent (GPS-2):** "Verify: PAT is correct and unexpired" —
  no payload says "ask the user; NEVER fabricate a token". Zero of 23 GIT_TOKEN_INVALID payloads
  carry user-direction.
- **dev_server reports container-internal URL** (`http://localhost:3000/` in 47/49 — DS-1) —
  unactionable from the agent's seat without translation.

### 2.5 Per-tool — small medians, structural gaps
- **Import success forces a reflexive discover (PT-6):** 69/77 successful imports immediately
  followed by `zerops_discover` (p50 follow-up 1.3–4.3 KB). The import response could carry the
  service summary.
- **Empty logs = 30 B with no why (PT-3, verified):** `{"entries":[],"hasMore":false}` — and the
  import-gate recovery pointer sends agents to this structurally-empty surface (2 corpus runs,
  2–3-call blind-groping loops).
- **The auto-close trailer is the #1 repeated sentence (PT-2, confirmed):** the same gating sentence
  appears in **173 responses** (89 deploy, 82 verify), 27.8 KB total, often twice in one payload
  (note + reason).

---

## 3. Live-truth field table (TIMING dimension — the stored-vs-live census)

| field (agent-facing) | source | verdict |
|---|---|---|
| `Hostname`/`TypeVersion`/`RuntimeClass` | LIVE (`ListServices` per ComputeEnvelope) | correct |
| `Status` | LIVE in develop/status envelope; **STORED snapshot in bootstrap envelope** (`DiscoveredStatuses`) | T7 — bootstrap atom gating runs on a stale snapshot |
| `Deployed` | DERIVED-FROM-STORED (`FirstDeployedAt` stamp ∨ session deploy ∨ adopted-live) | **T1/F11 confirmed: 16 corpus develop-starts mis-branched** — recipe-buildFromGit never stamps; the stale bool selects the first-deploy branch (p50 20.1 KB) over the edit-loop branch (~10 KB lighter) AND wrong instructions |
| `Mode`/`CloseDeployMode`/`GitPushState`/`BuildIntegration` | STORED — but ZCP **owns** these concepts (no platform counterpart) | acceptable as stored intent; `RemoteURL` self-documents as cache w/ drift warnings |
| auto-close readiness | STORED event log used as currentness proxy | **T2/F60 CONFIRMED: no timestamp comparison — a verify that PREDATES the last deploy satisfies the gate** (4 corpus instances, e.g. 20260605-040507 seq11→12) |
| Durability line | asserts present-tense "live now" from stored mode, zero liveness read | T3 |
| render masking | `renderServiceLine` prints Status **only when ≠ ACTIVE** | T4 — suppresses exactly the live field that would let the agent catch a stale `deployed=false` |

The line Codex draws (and the auto-close DERIVED-never-stamped precedent already proves): **store
user intent and ZCP-owned declarations; DERIVE mutable platform facts at render time.** ComputeEnvelope
already pays the live-read cost — the stamps should be evidence, not rendered truth.

---

## 4. Systemic problems, ranked

1. **REACHABILITY is causal and measured** — close-mode 4% vs 80% compliance by tell position;
   head names a 2-input action for a 3-input gate; bootstrap JSON puts the instruction at frac
   0.72–0.99; F51 warning exists on 1 path while 3 sibling paths (confirm, develop-start
   gitPush=configured, deploy strategy=git-push error) carry nothing.
2. **RIGHT-INFO violations create both bloat and round-trips** — only ~36% of the develop wall is
   decision-critical; availableStacks on no-type-decision routes; 36 KB knowledge monolith;
   route-menu enumerates full importYaml per option (49% of menu bytes); classify-prompt asks
   pre-resolved questions; adopt pairing defers known templates to a deterministic error.
3. **REDUNDANCY compounds per turn** — 6.4% corpus-wide, 23–33% in chain-heavy runs; export 83%
   re-render; launch 4.8 KB static table per re-call; the auto-close sentence 173×.
4. **TIMING bugs flip real branches** — F11 (16 mis-branched develop-starts) + F60 (temporal
   ordering, confirmed) + the render masking (T4) that hides the contradiction.
5. **STRUCTURE: 3 parsers, decorative recovery** — status is format-bimodal (markdown in
   develop/idle, JSON in bootstrap — SP-2 confirmed); 81.5% of errors carry a generic recovery
   pointer while the real corrective is prose; close-mode call emitted from 5 hand-authored sites
   in 2 placeholder syntaxes.
6. **CORRECTNESS one-owner drifts keep shipping** — see §6 new real bugs.

---

## 5. Target delivery model (synthesis: agents + Codex agree)

**Decision-head + sparse just-in-time guidance + pullable reference.** Atoms stay the authoring
unit; **the wall stops being the delivery unit.**

1. **One canonical `AgentResponse` envelope** for every workflow/prompt/error response:
   structured head — `kind`, `phase`, `services` (live-derived), `blockers`, `stateBools`,
   **`nextCall`** (executable tool+args, or `state:"blocked"` + `missingInputs`/`choices` — never
   fake placeholders), `guidance[]` (≤1-2 phase-relevant atoms), `guidanceRefs[]` (pullable
   `zerops_knowledge uri=` pointers), `liveStateFresh` — then a markdown tail. JSON head as control
   protocol, markdown for guidance. **Errors get the same envelope discipline** (this REPLACES the
   P4 "errors are leaf payloads" invariant — explicit Karel decision required).
2. **Live-truth at render:** derive `Deployed`/status-bearing facts from the live read ComputeEnvelope
   already does; stamps become evidence. Fix T2 temporal ordering. Stop masking ACTIVE status (T4).
3. **Axis fix as the cheapest big lever:** make the close-mode DECISION atom fire on never-deployed
   first develop-start (the 4%→80% lever), and split route/status-dependent atom BODIES per axis,
   not just presence.
4. **Guidance gated on live state:** launch blocker table renders only live-blocker rows; export
   intro/validate get exportStatus axes; failure-path content moves to trigger-time surfaces
   (failureClassification, preflight errors) where it already partially exists.
5. **Knowledge becomes section-addressable** — the 36 KB `scope=infrastructure` monolith survives
   only as explicit reference/debug; briefings get stack-specific chunks (≤8–12 KB), never auto-attached.
6. **Size targets** (Codex; agent-measured conservative path in parens):
   session-start **2–5 KB** (evidence-based intermediate: 9.4 KB), mutation ack 0.5–2 KB,
   error 0.8–2.5 KB, knowledge pull ≤8–12 KB per chunk.
7. **Ship-first cut-over:** `workflow:start:develop` (25% of all bytes, clearest recurrence surface).
   **Explicitly NOT:** tuning ComposeUnderBudget as the fix — it's a transport backstop, absent from
   the develop path, and solves neither timing nor reachability nor executable recovery.

### Measured reduction potential (no decision-critical tell lost)

| family | p50 today | p50 target | family bytes saved | dominant mechanism |
|---|---|---|---|---|
| bootstrap | 4,396 | ~3,100 | ~468 KB (22%) | gate stacks to classic; kill YAML re-copies + menu enumeration + post-commit preamble; failure content → failure path |
| develop | 17,850 | ~9,400 (target-model: 2–5 K) | ~600 KB (47%) | decision-head + refs; keep 5 critical blocks; DECISION axis fix |
| launch+export | 7,800 | ~2,900 | large share of family | live-gated blocker rows; exportStatus axes; auto-classify known keys |
| per-tool | 1,026 | ~800 | ~340 KB | curate includeEnvs; import carries summary (kills 69/77 reflexive discovers); sectioned knowledge |
| deploy-config | 456 | ~400 | ~27 KB + **14 turns/battery** | surface stderr; LLM-addressed suggestions; check-before-mutate on confirm |

Total: ≈ **1.4–1.5 MB (~30%) of bytes + the turn-eliminations** (which both Codex and the
deploy-config agent rank above byte savings).

---

## 6. New REAL bugs this audit found (not in the 63-finding report)

| id | bug | owner |
|---|---|---|
| **BI-1** (confirmed) | production build-integration actions-confirm hardcodes the **eval-harness env var `ZCP_E2E_GITHUB_PAT`** into `ghAuthPrecondition.setupCommand` — shipped to every real user | `workflow_build_integration.go` |
| **LX-3** (verified) | ZCP's own recipe corpus teaches **schema-invalid yaml**: `nodejs-hello-world.md` + the recipe app repo's own `zerops.yaml` put `verticalAutoscaling` under `run:` (live zerops-yml schema: `additionalProperties:false`, 0 occurrences; import-yaml-only field). Caused the export validation-failed leg in all 3 runs of that scenario | recipe corpus + `zerops-recipe-apps/nodejs-hello-world-app` (sync push + repo PR) |
| **T2** (confirmed = Codex's F60) | auto-close gate has **no verify-after-deploy temporal ordering** — pre-deploy verify satisfies post-deploy gate; 4 corpus instances | `work_session.go::serviceAutoCloseReady` |
| **DEV-2** (verified) | close-mode DECISION atom axis `deployStates:[deployed]` locks it out of first develop-start — the 4%-vs-80% compliance lever | `develop-strategy-review.md` axes |
| **PT-3** (verified) | import-gate recovery points at `zerops_logs` for failures that produce **no build container → structurally empty logs**, with a 30 B unexplained empty response | `tools/import.go:226` + `ops/logs.go` |
| **BOOT-3** (confirmed) | bootstrap-verify atom names status `NOT_YET_DEPLOYED` — a string that occurs in **0/1,433** payloads (real: `READY_TO_DEPLOY`) + 2 more tell≠check drifts | `bootstrap-verify.md` et al. |
| **DS-1** | dev_server health URL is container-internal `localhost:<port>` in 47/49 responses | `dev_server_start.go` |

## 7. Leaf-fix master plan disposition (Codex + audit agree)

- **Survives as-is:** P3 (git-push stderr = GPS-1), P4 (planless-discover), F27 call-shape, F43
  pipelineSummary, F35, and the tell==check content fixes whose content stays a pulled/dripped atom.
- **Survives reframed:** P2/F11 — solve **live deployed truth by derivation**, not another stamp
  (Codex; T1 verification shows the proxy is a branch-selector, making derivation more valuable);
  P5/F54 — `compose-ready` as structured terminal state + nextCall, not mainly a new wall atom.
- **Becomes secondary/moot under the target model:** atom wording whose goal is "make buried
  guidance louder", route-menu prose nudges, broad knowledge-bundle guidance — anything whose
  success condition is "the atom exists somewhere in the response". The new gate: **correct live
  head, executable next call, bounded size, pullable depth.**

## 8. Open decisions for Karel

1. **Adopt the target delivery model?** (full redesign vs evidence-based intermediate per family vs
   leaf-fixes only). Recommended sequencing if yes: (i) quick REAL-bug batch (§6: BI-1, LX-3, T2,
   DEV-2 axis, PT-3, BOOT-3 — all S/M, independent of redesign), (ii) `AgentResponse` envelope +
   develop-start cut-over, (iii) family-by-family migration with flow-eval gates per phase.
2. **Invariant change:** errors leave leaf-only and join the envelope (replaces P4). Yes/no.
3. **Size target ambition:** Codex 2–5 KB vs agent-measured 9.4 KB for develop-start — pick the
   gate for the cut-over eval.
4. **The `workflow:status` bimodality** (markdown vs JSON by phase) — converge on the envelope as
   part of (ii), or earlier as a standalone fix.
