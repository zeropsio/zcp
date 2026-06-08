# Production Pipeline Review — git-push → build-integration → export → launch (2026-06-05)

**Commissioned by Karel:** "I want us to get a project to production as well as possible — review
the whole workflow chain around build-integration: git-push, export, launch, everything related."
**Method:** 5 pipeline agents (per-surface end-to-end: code + atoms + full transcripts) + 1
cross-cutting journey agent + independent Codex (gpt-5.5 xhigh) review + adversarial verification
of all new HIGH findings (GPS-1/2, BI-*, EX-1, LP-1, J1/J2/J3 — CONFIRMED except 3 WEAKENED).
Anchor bug B1 and the other per-bug fix plans live in `plans/real-bugs-response-audit-2026-06-05.md` (v2).
Analysis only — Karel decides.

---

## 1. The journey as it ACTUALLY runs (measured from transcripts)

| stage | what happens | measured cost |
|---|---|---|
| **A — adopt ceremony** | every chain workflow (launch/export/BI/gps) bounces `not-bootstrapped` on un-adopted services → bootstrap route=adopt → `ErrAdoptPairingChoice` bounce in **32/43 adopt sessions (74%)** → re-complete → provision | **5–6 calls, ~45–60 s, paid by 16/16 chain runs** |
| **B — source-control wiring** | launch → `source-control-required` (**21/29 non-error launch starts** land here — the dominant launch state) → gps walkthrough → gps confirm (23 GIT_TOKEN_INVALID corpus-wide, each "exit status 128" + 2–6 manual probes) → build-integration (+1 ordering bounce when called first) → **5–10 MANUAL Bash calls** (workflow file, gh auth, 2× gh secret, commit, push) outside any ZCP verification | worst case: **25 Bash calls of git surgery, 3.4 min** (history reconciliation — see J1) |
| **C — launch narrowing** | classify-prompt (8.5 KB; agent echoes server-computed suggestions verbatim) → ready-to-launch (asks launchKey; user: 7 dashboard steps) → launched | 3 calls, ~50 s agent + ~10.5 min platform |
| **D — post-launch return** | `action=status` → "Phase: idle … not bootstrapped" generic bootstrap guidance; `action=list` → `[]`; agents re-pay stage A (5–9 calls), get steered into configuring the DEV project's pipeline when asked about PROD; one agent told the user it was "flying blind" | full re-pay + misdirection |
| **alt — export** | 2 start bounces → stage A → 23.6 KB classify-prompt → echo turn → validation-failed (B2 defect dev loop never surfaced) → develop detour costing **3 calls/2.4 min to 3× that** depending on close-mode path | |

### Handoff gaps (what carries vs what's missing)

| handoff | gap |
|---|---|
| adopt → chain workflows | redirect blocker carries route+workflow only — no scope/pairing, so adopt re-asks what the redirecting workflow already knew (J4: universal entry tax) |
| gps → launch gate | probe proves AUTH only; **nobody owns reconciling a recipe-bootstrapped repo (shallow, template history) with the user's real remote** — agents freestyle 25 Bash calls incl. `rm -rf .git`; the surgery re-trips the gate (J1, CONFIRMED) |
| gps → build-integration | ordering bounce (BI-first → needsGitPushSetup, 5 runs); service-half asymmetry (BI accepts stage hostname, gps rejects it); the actual wiring is manual + unverified |
| develop → export | validator drift: deploy-time live validation never rejects what export's structure schema rejects — defects ship through the whole dev loop and surface at export (EX-3) |
| launch → next session | terminal states filtered out of status recovery (except FocusIdle); list returns []; no pipelineSummary (F43); launched guidance simultaneously orders revoke-key-now AND re-call-with-key (J5) |
| bundle → fresh prod project | best-engineered handoff (R6 retryCall makes failures 1-re-call recoverable) — but failures land with no provenance back-link; recovery re-pays full adopt+develop ceremony (15–26 calls) |

---

## 2. Findings ranked (new, beyond the B1–B10 fix batch)

### P0 — production correctness
1. **`BuildIntegration=configured` is an unearned state (BI-VERIFY-1, CONFIRMED; Codex P0).**
   `meta.BuildIntegration` is written BEFORE the agent performs any of the 4 manual steps (workflow
   file commit, gh auth, secrets, push) — no verification surface exists anywhere. Launch can only
   treat it as advisory; users get "configured" for pipelines that don't exist. Fix direction:
   verified-or-pending state model (e.g. `BuildIntegrationPending` until a ZCP-checkable signal —
   workflow file on remote HEAD via the existing HEAD-SHA read — flips it), or rename the truth
   ("declared", with the launched/pipeline check as the verifier).
2. **Launch may create a non-production project (Codex P0).** The platform spike requires prod
   defaults `SERIOUS` core (platform default LIGHT); the launch YAML emits only name/tags/envs
   (`ops/bundle/launch.go:149`) and `CreateAndImportProject` IGNORES `CreateOpts`
   (`platform/project_admin.go:266`) — Location/tags passed and discarded. Likely LIGHT prod
   projects today. Fix: thread CreateOpts into the create call + emit mode in the bundle; pin with
   a live e2e read-back.
3. **customDomain is a phantom feature (LP-3).** Accepted as input, echoed in launchInputsEcho,
   promised on FOUR agent-facing surfaces — implemented nowhere. Either implement (import yaml
   `domains:`? post-import process?) or remove the input + tells (tell==check).

### P1 — the credential story (Codex verdict: "N ad-hoc asks, not one flow")
Today: gps asks for a PAT → BI asks the agent to gh-auth (B1) + set 2 repo secrets → Actions needs
them at run time → launch asks for launchKey. Only coherent piece: ZEROPS_TOKEN derives from
ZCP_API_KEY. **Target (Codex, endorsed):** collect ONE GitHub credential at source-control setup,
scopes derived from the chosen integration (Contents rw; +Secrets rw +Workflows rw for Actions);
while it's in-request: verify git, set repo secrets via API, write/commit workflow; store only
non-readable GIT_TOKEN; launchKey only at the irreversible project-create boundary. This dissolves
B1's gh-auth precondition entirely for the API-capable path (gh CLI stays as fallback).
**Plus the hygiene cluster (B10):** PAT echoed verbatim in blocker payloads (CONFIRMED leak),
credential-embedded URLs accepted end-to-end, http:// accepted despite "requires HTTPS" tell,
agents FABRICATING tokens after generic errors (4 independent runs).

### P1 — chain contract
- **Source-of-truth split (Codex):** export resolves repo URL from live origin; launch from
  gate-validated meta. Same chain, different authority. Unify on the launch gate's
  meta+live-match model.
- **First-class production state (Codex verdict on the three-axis question):** the three axes
  remain right for the SOURCE side; production needs its own state the chain can read:
  SourceControlReady / ProdProjectCreated / ProdPipelineConfigured-or-Observed. Today that state is
  smeared across meta bools (one unearned), a launch state file status recovery filters out, and
  nothing (post-launch invisibility J3).
- **Adopt entry tax (J4/LP-8/EX-4):** the `meta.IsComplete()` hard gate buys little (export needs
  mode + live services) but costs every un-adopted project a 5–6-call ceremony with a deterministic
  74% pairing bounce — and the redirect drops the scope info that would let adopt skip its question.
  Candidates: auto-derive adopt from the redirecting workflow's scope; or soften the gate where the
  consumer only needs mode.

### P2 — terminal/return surfaces
- **Post-launch invisibility (J3, WEAKENED but real):** status filters terminal launches under an
  open work session; list returns []; launched response drops `state.ImportedServices` (prod service
  handles the operator needs). F43 pipelineSummary is necessary but not sufficient — the
  return-to-production conversation needs a first-class read.
- **compose-ready knowledge gap end-to-end (EX-1 CONFIRMED = F54):** the SUCCESS terminal of the
  common export path exists only in the handler — no atom axis covers it, export-intro still
  promises publish-ready|validation-failed.
- **Launched self-contradiction (J5):** revoke-the-key-now vs re-call-with-key in one payload.
- **Bundle error provenance (EX-2):** import+zerops errors merged into one slice; agent can't tell
  which FILE failed.

### Notable positives (verified, keep)
- The generated Actions workflow YAML is substantially CORRECT against live reality (zcli flags,
  secret names, trigger shape) — the wiring around it is what's broken (BI-YAML-1).
- The bundle→fresh-project handoff with R6 retryCall is the best-engineered seam in the chain.
- Adopt's pairing ERROR is self-correcting in one call (templates embedded) — the bounce itself is
  the authored defect, not the recovery.

---

## 3. Ship order (Codex + audit, merged)

1. **B1 + B10 security cluster** (fix batch v2 — already planned).
2. **B6 diagnostics + credential contract** (kills the fabrication class + 14 wasted turns/battery).
3. **Production artifact correctness:** SERIOUS project mode (P0-2), customDomain resolution (P0-3),
   BuildIntegration verified-or-pending (P0-1), launch pipelineSummary (F43) + post-launch read (J3),
   compose-ready propagation (F54/EX-1).
4. **Credential flow redesign** (one-collect model) — M/L, dissolves B1's precondition class.
5. **Chain ergonomics:** adopt entry-tax softening (J4), classify echo-turn elimination
   (LP-10/J8 — server already computes the buckets), source-of-truth unification (export↔launch).
6. **NOT now (per Codex):** response-envelope rewrites — correctness first, delivery shape rides
   the planned redesign (`workflow-response-delivery-eval-2026-06-05.md` §5).

**DO-NOT-DO (Codex, endorsed):** no new magic env vars; no more alternate-YAML menus; don't let
launch trust live SSH remote alone (keep meta+live-match); don't mark BuildIntegration configured
before GitHub-side work is verified or explicitly modeled pending.

## 4. Open decisions for Karel

1. P0-1 state model: `pending→verified` BuildIntegration vs rename-to-declared? (changes meta schema
   — migration must be one-way idempotent per backward-compat rule)
2. P0-2 SERIOUS mode: confirm the platform contract (CreateOpts → project create API) on eval-zcp
   before coding — needs a live spike.
3. P0-3 customDomain: implement or remove?
4. Credential-flow redesign (ship-order #4): green-light the design pass now, or after the bug batch?
5. J4 adopt tax: soften gate vs auto-derive-from-redirect — which direction?
