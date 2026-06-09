# Open-work compact — continuation context (2026-06-09)

Self-contained handoff so a fresh session can continue WITHOUT re-deriving. Three open streams:
**A) production pipeline (PRIMARY)**, **B) delivery-model redesign (groundwork)**, **C) cache-clear
slug fix**. Plus one immediate operational item. Written as if you've never seen the prior session.

---

## 0. What already shipped (closed — do not redo)

Branch **`fix/response-audit-real-bugs`** (pushed; PR not yet created — token lacks `createPullRequest`;
make it from https://github.com/zeropsio/zcp/compare/main...fix/response-audit-real-bugs). 13 commits:

- **9 real-bug fixes** (B1 gh-auth-from-owner; B10 credential redaction; B3 auto-close verify-ordering;
  B5 close-mode DECISION on never-deployed; B8 phantom-status + status-token lint; B6 git-push stderr +
  credential contract + idempotent confirm; B7 empty-logs + events-first; B4 derive deployed for
  recipe-buildFromGit; B9 dev_server consumer-vantage URL).
- **sync-push fix** (zerops.yaml length-guard → schema-validity gate; dry-run == real-push).
- **B2 class-prevention** (recipe-lint + content-lint validate yaml snippets vs the live schema).
- **B2 content published**: nodejs + gleam app-repo PRs merged; Strapi re-extracted (cache-clear with
  the Strapi slug); local corpus pulled clean; all three layers in sync.

Several pipeline findings below were ALREADY fixed by that batch and are excluded here: GPS-1 (=B6),
GPS-4 (=B10), GPS-6 (=B6c), BI-1/BI-NEW-1 (=B1), LP-1 (=B10), J2 (=B6).

Inputs for everything below: `plans/production-pipeline-review-2026-06-05.md`,
`plans/workflow-response-delivery-eval-2026-06-05.md`, `plans/real-bugs-response-audit-2026-06-05.md`.

---

## ⚠️ 1. Immediate operational — ROTATE the leaked PAT

`github_pat_11ARR63TQ0AM3jIR8NtfTk_…` (the gh-auth fine-grained PAT) is on disk in
`eval/behavioral/runs/**/transcript.jsonl` and `/tmp/zcp-response-audit/corpus.jsonl` — a real GitHub
credential captured in eval transcripts (GPS-2). **Rotate it.** B10 stopped ZCP from echoing it in
NEW responses, but the already-captured copies are live. (Also: this PAT lacks `Pull requests: write`
on zeropsio/zcp — granting it fixes the PR-create failure above.)

---

## STREAM A — Production pipeline (PRIMARY: "get a project to production as well as possible")

The dev→prod chain is git-push-setup → build-integration → export → launch-production. Full journey
map + handoff table: `production-pipeline-review-2026-06-05.md §1`. Open work, P0 first.

### A-P0 (production correctness — fix these to not ship broken prod)

1. **`BuildIntegration=configured` is an unearned state (BI-VERIFY-1, Codex P0).**
   `meta.BuildIntegration` is stamped (`workflow_build_integration.go:~199`) BEFORE the agent does any
   of the 4 manual steps (workflow-file commit, gh auth, 2× repo secrets, push) — nothing verifies it.
   Launch can only treat it as a weak advisory. **Fix:** model it as `pending` vs `verified` — flip to
   verified only on a ZCP-checkable signal (workflow file on remote HEAD via the existing HEAD-SHA read,
   or repo-secrets-present via the GitHub API), OR rename the field to `declared` and make the
   launched/pipeline check the verifier. Meta-schema change → one-way idempotent migration + pin.

2. **Launch may create a LIGHT (not SERIOUS) production project (Codex P0).** The spike requires prod
   default `SERIOUS` core (`docs/spec-launch-production-platform-spike.md:255`); the launch YAML emits
   only name/tags/envs (`ops/bundle/launch.go:~149`) and `CreateAndImportProject` **ignores `CreateOpts`**
   (`platform/project_admin.go:~266`) — Location/tags passed and discarded. Likely LIGHT prod today.
   **Fix:** thread CreateOpts into the create call + emit mode in the bundle. **Needs a live spike on
   eval-zcp first** to confirm the platform contract (how core mode is set at project-create).

3. **`customDomain` is a phantom feature (LP-3).** Accepted as input, echoed in `launchInputsEcho`,
   promised on FOUR agent-facing surfaces, implemented nowhere. **Recommended fix (question-the-artefact):
   DELETE it** — P-PROD-2 already decided domain attachment is operator-owned dashboard work and ZCP has
   no prod access post-key-revoke. Remove the input + the 4 tells. (Or implement via import-yaml
   `domains:` — but the spike says operator-owned, so delete is the honest fix.)

### A-P1 (credential flow + chain contract)

4. **Credential flow is N ad-hoc asks, not one (Codex P1).** gps asks a PAT → BI asks gh-auth + 2 repo
   secrets → Actions needs them → launch asks a launchKey. Only `ZEROPS_TOKEN`=`ZCP_API_KEY` is coherent.
   **Target:** collect ONE GitHub credential at source-control setup, scopes by chosen integration
   (Contents rw; +Secrets rw +Workflows rw for Actions); while it's in-request: verify git, set repo
   secrets via API, write/commit workflow; store only non-readable GIT_TOKEN; launchKey only at the
   irreversible project-create boundary. This dissolves B1's gh-auth precondition for the API-capable path.

5. **First-class production state (Codex chain-contract verdict).** The three axes (close-mode/git-push/
   build-integration) are right for source-side; production needs its own readable state:
   `SourceControlReady` / `ProdProjectCreated` / `ProdPipelineConfigured-or-Observed`. Today it's smeared
   across meta bools (one unearned, A-P0-1) + a launch state file status-recovery filters out + nothing
   post-launch (J3).

6. **Open credential/UX gaps (sharp, S/M each):**
   - **GPS-3** container confirm dead-ends when the user won't paste a raw PAT inline → add an env-token
     path (probe with the container's GIT_TOKEN env when gitToken omitted + remote set).
   - **GPS-5** GIT_TOKEN is a project-singleton but the atom mandates single-repo-scoped PATs — decide the
     owner (move to service-level env on the push source, or document the multi-repo limitation).
   - **LP-2** the git-push recovery chain lacks the wait-for-user credential discipline the launchKey path
     has → agents FABRICATE PATs/URLs. Add the explicit "repoUrl+gitToken come FROM THE USER; ask and wait,
     never invent" STOP to the blocker + atom (parity with launch-mutation-key-required). (B6 added the
     credential contract on ERRORS; this is the proactive blocker side.)
   - **J1** nobody owns git-history reconciliation between the recipe-bootstrapped dev repo (shallow,
     template history) and the user's real remote → 25-Bash-call surgery + re-trips the gate. Make the
     git-push-setup probe ALSO read divergence (ls-remote HEAD vs local HEAD, shallow, ahead/behind) and
     return a structured reconcile choice.

### A-P2 (ergonomics / terminal surfaces — after P0/P1)

- **EX-1 / F54** `compose-ready` (export success terminal) exists only in the handler — no atom, intro
  still promises publish-ready|validation-failed. Author the `export-compose-ready` atom
  (`topology.ExportStatusComposeReady` already in the enum) + axis.
- **J3** post-launch state invisible: ship F43 `pipelineSummary` on the terminal/recovery envelope +
  attach `state.ImportedServices` (prod service handles) to `launchLaunchedResponse` (LP-6).
- **LP-4** ready-to-launch is empty consent — wire the already-composed baseline bundle preview +
  `readinessCheck[]` into it before asking for the launchKey.
- **LP-5** push-unsupported modes dead-end — consult `ClassifyPushSource` in `validateLaunchSourceControl`,
  emit a `mode-unsupported-<host>` blocker.
- **LP-7** reset atom drift: `wouldDelete`→`wouldDestroy`, `confirmDestructive:true`→structured object (I
  fixed the launch-status-recovery atom's wouldDestroy in B8; re-verify the full reset section matches).
- **LP-8 / J4** adopt entry-tax: launch/export `service-not-bootstrapped` recovery should carry
  `scope=[dev,stage]` so the redirected adopt skips its pairing question (the redirecting workflow already
  knows the pair).
- **BI-2 / F34-F61 / BI-3 / BI-NOOP-1** build-integration: no PushSourceCheckFor (normalize to
  meta.Hostname); source/target scope-truth in every response; omit `alternateWorkflowFiles` when
  setupMandatory; make the noop re-call return the full enriched handoff (stateless recompute).
- **EX-2..6** export: per-file error provenance; the strict-vs-lenient zerops.yaml owner seam (B2-related);
  soften the `meta.IsComplete()` adopt gate; fix the export→git-push-setup handoff call shape; trim the
  23.6 KB classify-prompt (auto-classify `IsClassifyInfrastructure` keys).
- **J5 / J6** launched payload self-contradiction on the launchKey (make it conditional on pipeline
  blockers); export validation-failed should name the cheap config-only exit (edit → `action=close` → re-call).

**Suggested A sequencing:** rotate PAT → A-P0-1 (BuildIntegration state) → A-P0-3 (customDomain delete,
cheap) → A-P0-2 (SERIOUS, needs spike) → A-P1 credential flow + first-class prod state → A-P2 ergonomics.

---

## STREAM B — Delivery-model redesign (groundwork; secondary but Karel wants it fully primed)

**The problem the whole audit was about** (`workflow-response-delivery-eval-2026-06-05.md`): ZCP front-loads
the agent. Measured over 1,433 real payloads / 116 runs: `workflow:start:develop` p50 **17.9 KB**, **96%**
guidance wall (18 atoms), decision head only **4.5%** (852 B); it's **25% of all bytes ZCP sends**. The
9-bug batch fixed *correctness*; the redesign fixes the *delivery model*.

**Target model (agents + Codex converged):** decision-head + sparse just-in-time guidance + pullable
reference. One canonical `AgentResponse` envelope for every workflow/prompt/error: structured head
(`kind`, `phase`, `services` (live-derived), `blockers`, `stateBools`, **`nextCall`** = executable
tool+args or `state:"blocked"`+missingInputs/choices, `guidance[]` ≤1–2 phase-relevant atoms,
`guidanceRefs[]` = pullable `zerops_knowledge uri=` pointers, `liveStateFresh`) + a markdown tail.
Atoms stay the authoring unit; the wall stops being the delivery unit.

**The 5 levers (with measured reduction, `eval §5`):**
1. **Decision-head + on-demand pull** — develop-start 17.9 KB → ~9.4 KB (measured) / 2–5 KB (Codex target).
   Keep ~5 decision-critical blocks inline; everything situational/failure-path behind `zerops_knowledge uri=`.
2. **Structured `nextCall`** on every prompt-type response (route-menu, classify, close-mode decision,
   pairing-choice, export/launch prompts). Precedents exist (import `retryCall`). Failure mode to avoid:
   placeholder cargo-culting → use `state:"blocked"` + `missingInputs`/`argsTemplate`, never fake-ready args.
3. **Live-truth at render** — derive `Deployed`/status from the live read `ComputeEnvelope` already does;
   stamps become evidence, not the rendered truth. (B3/B4 were instances; the model generalizes it.)
4. **Guidance gated on live state** — launch blocker table renders only live-blocker rows; export
   intro/validate get exportStatus axes; failure-path content moves to trigger-time.
5. **Knowledge section-addressable** — the 36 KB `scope=infrastructure` monolith becomes ≤8–12 KB chunks,
   never auto-attached.

**Code owners:** `internal/workflow/render.go` (RenderStatus/renderGuidance), `synthesize.go` (atom
selection), `compose.go` (ComposeUnderBudget — NOTE: NOT wired on develop-start, only status/bootstrap;
do NOT tune it as the fix), `envelope.go`/`compute_envelope.go`, `tools/convert.go` (ErrorWire),
atoms `internal/content/atoms/`.

**Ship-first:** the `AgentResponse` envelope + cut `workflow:start:develop` over first (25% of bytes,
clearest recurrence surface — close-mode/two-phase guidance existed in the wall and agents still violated
it: 4% vs 75% compliance by position).

**§8 open DECISIONS (Karel owns):** (1) full redesign vs evidence-based intermediate per family vs
leaf-only; (2) **invariant change** — errors leave leaf-only and join the envelope (replaces the current
P4 "errors are leaf payloads" invariant in CLAUDE.md) — yes/no; (3) size target — Codex 2–5 KB vs measured
9.4 KB for develop-start; (4) `workflow:status` is format-bimodal (markdown in develop/idle, JSON in
bootstrap) — converge on the envelope. Recommended phasing: (i) ✅ real-bug batch (done), (ii) envelope +
develop cut-over, (iii) family-by-family migration with a flow-eval gate per phase.

---

## STREAM C — cache-clear slug remap fix (small, Karel wants it)

**Bug:** `zcp sync cache-clear <local-slug>` hits `POST {api}/recipes/{slug}/cache/clear` with the LOCAL
slug, but Strapi knows recipes by their STRAPI slug. `cache-clear nodejs-hello-world` → **404** because
Strapi's slug is `node-js-hello-world` (`.sync.yaml slug_remap: node-js-hello-world → nodejs-hello-world`;
also `recipe → nodejs-hello-world`). `RemapSlug` (config.go:109) maps Strapi→local; cache-clear needs the
REVERSE. Silent failure: the operator thinks the cache cleared but it 404'd, so the corpus never updates.

**Fix:** in `CacheClear` (`internal/sync/cache.go`), resolve each given local slug to its Strapi slug(s)
before hitting the endpoint. Build a reverse index from `cfg.SlugRemap` (local→[strapi slugs]; it's 1:N —
`recipe` AND `node-js-hello-world` both map to `nodejs-hello-world`), and clear ALL matching Strapi slugs;
fall back to the slug as-is when no remap exists. Pin with a unit test over a remap fixture. (cache-clear
with NO args already uses `fetchAllSlugs` = Strapi slugs and works — this only fixes the explicit-slug path.)
**Verified-working manual path meanwhile:** `cache-clear node-js-hello-world` (the Strapi slug) succeeds.

---

## Pointers

- Empirical corpus (1,433 payloads): `/tmp/zcp-response-audit/` (corpus.jsonl, samples/, stats.md) — may
  be gone after /tmp cleanup; regenerate via `/tmp/zcp-response-audit/extract.py` over
  `eval/behavioral/runs/2026060*/`.
- Per-bug full-breadth analyses + the production-pipeline workflow result: `bugs-pipeline-result.json`
  (transient) — the fix designs above are extracted from it.
- eval-zcp (org Muad) is the live playground for the SERIOUS spike (A-P0-2); `ssh <host>` + `zcli` on the
  zcp container are wired.
