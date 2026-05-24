# launch-production friction fixes — F1-F8 from flow-eval 2026-05-21

**Date:** 2026-05-21
**Status:** Draft (Codex review integrated — ready for Karel decision)
**Triggered by:** flow-eval run of 9 launch-* scenarios after P1-P5 ship.
**Source data:** `eval/behavioral/runs/20260521-{102710,103202,103808,104459,104943,105441,110109,110448,110942}/`

---

## TL;DR

P1-P5 (`launch-production` source-of-truth + multi-runtime reshape) is **verified end-to-end** by 9 successful flow-eval scenarios. The recipe-template loophole from `.zcp/manual/l1.txt` is structurally closed.

The eval surfaced **8 friction points** none of which are regressions. Six are atom-wording or messaging gaps; two are handler-default issues that warrant code change. **None require architectural redesign** — all P1-P5 invariants hold.

| # | Severity | Root cause | Fix shape |
|---|---|---|---|
| F1 | HIGH | Pre-launch state opacity — agent doesn't know bootstrap-adopt is prereq when meta missing | atom + CLAUDE.md routing note |
| F2 | MED  | `git-push-setup` walkthrough mixes reference + checklist | atom rewrite + structured `steps[]` field |
| F3 | HIGH | `zerops_deploy strategy=git-push` defaults setup-name to hostname; fails for recipe shapes | handler change — auto-detect from yaml |
| F4 | MED  | `gh` CLI auth assumed; not authenticated in container | handler detection + atom warning + fallback |
| F5 | MED  | `skipRestart=true` env-set + GIT_TOKEN consumption race | handler change — credential-key auto-reload OR warn |
| F6 | MED  | Post-launch state opacity (P-LP-7 Path B by design) | atom + CLAUDE.md additions |
| F7 | LOW  | `sourceContext.suggestedRuntime` field naming pre-P1/P2 | field rename + atom update |
| F8 | LOW  | launchKey reuse expectation | atom wording explicit |

Total: ~2 handler changes + ~6 atom rewrites + CLAUDE.md additions.

---

## Per-finding analysis

### F1 — `service-not-bootstrapped` surprise (HIGH)

**Symptom (from retros, 3 scenarios):**
> "When you start `launch-production`, if the services were never adopted via bootstrap, the workflow returns a `service-not-bootstrapped` blocker... nothing in the launch-production tool description warns you that adopt is a prerequisite."

**Surface:**
- `internal/content/atoms/idle-launch-entry.md` — fires only when `idleScenarios: [bootstrapped] envelopeDeployStates: [deployed]`
- `internal/tools/launch_source_control_gate.go:139` — the gate emits `service-not-bootstrapped` blocker correctly
- `internal/content/atoms/idle-bootstrap-entry.md` — fires when `idleScenarios: [empty]`
- `internal/content/atoms/idle-adopt-entry.md` — fires when `idleScenarios: [adopt]` (unmanaged services on platform)

**Root cause:** the launch gate's `service-not-bootstrapped` blocker IS the structural defense — and it works. The friction is purely "agent didn't know to expect this prereq detour." When user says "spus produkci" on unbootstrapped services:

1. No `idle-launch-entry` (gated on bootstrapped+deployed).
2. `idle-adopt-entry` MAY fire (when there are unmanaged services on platform) — but its title is about adopt, not launch.
3. Agent reads CLAUDE.md / workflow listing → calls `launch-production` → gets `service-not-bootstrapped` blocker.
4. Agent navigates to bootstrap adopt. Costs ~3 round-trips (discover → provision → close).

**Proposed fix:** atom wording is the right layer; do NOT add state-aware routing (the gate already handles it).

1. `idle-adopt-entry.md` — add explicit "After adopt completes, launch-production becomes available for prod promotion" trailing note.
2. CLAUDE.md "convention" bullet for launch-production — add one sentence: "Launch refuses on unbootstrapped services; the source-control gate returns `service-not-bootstrapped` with Recovery hint to bootstrap adopt. Expect a 3-step detour."
3. Atom `launch-source-control-required.md` — already has the `service-not-bootstrapped` row in the per-blocker table. Adequate.

**Anti-patch check:** could we pre-check ServiceMeta in scope-prompt resolution and synthesize a different status (e.g. `bootstrap-required`)? That's a duplicate of the gate's existing blocker. Architecturally, the gate is the single source of truth for "is this service launchable" — adding a pre-emptive synth would split the predicate across layers. The friction is messaging, not architecture.

**Verdict (pre-Codex):** ship atom edit + CLAUDE.md note.

**Tests:** no new pin (atom corpus lint catches malformed atoms).

---

### F2 — `git-push-setup` walkthrough density (MED, recurring 5×)

**Symptom:**
> "The `git-push-setup` walkthrough is long and mostly helpful, but it buries the actual command sequence inside prose."

**Surface:**
- `internal/content/atoms/setup-git-push-container.md` + `setup-git-push-local.md`
- `internal/tools/workflow_git_push_setup.go:126-179` — emits `inputsRequired[]` + dense `prompt` + `nextStep` prose

**Root cause:** the walkthrough atom mixes two functions:
1. Reference doc — WHY each step exists, error categories, token-scope tables.
2. Actionable checklist — DO A then B then C.

Agents need (2) immediately; (1) is for the user if they ask "why are we doing this."

**Proposed fix (two-part):**

1. **Structured `steps[]` field on the response:** `workflow_git_push_setup.go` walkthrough response adds:
   ```go
   "steps": []map[string]any{
     {"n": 1, "title": "Set GIT_TOKEN as project env",
      "call": "zerops_env action=\"set\" project=true variables=[\"GIT_TOKEN=<value>\"]"},
     {"n": 2, "title": "Confirm remote URL on meta",
      "call": "zerops_workflow action=\"git-push-setup\" service=\"<hostname>\" remoteUrl=\"<url>\""},
     {"n": 3, "title": "Commit + push (if container has uncommitted code)",
      "call": "zerops_deploy targetService=\"<hostname>\" strategy=\"git-push\""},
   }
   ```
   Agents iterate `steps[]` programmatically; prose remains the human-readable layer.

2. **Atom rewrite:** lead with the structured sequence as a numbered list; move "Why" + token-scope reference to bottom paragraphs.

**Anti-patch check:** is this just "less wordy doc"? No — adding `steps[]` is structural. It lets the agent build a checklist UI / iterate calls without prose parsing. Atom rewrite is supplemental.

**Verdict (pre-Codex):** ship both (`steps[]` field + atom rewrite).

**Tests:** new test `TestGitPushSetupWalkthrough_EmitsStepsField` pinning the `steps[]` shape.

---

### F3 — `zerops_deploy strategy=git-push` setup-name default (HIGH, recurring 3×)

**Symptom:**
> "tool defaults to using the target service's hostname as the setup name, but this zerops.yaml uses `prod` and `dev` as setup names, not `appdev`/`appstage`."

**Surface:**
- `internal/tools/deploy_local_git.go:88-92` — `setupName := input.Setup; if setupName == "" { setupName = hostname }`
- `internal/tools/deploy_local.go:43` — schema description says "Omit only when zerops.yaml has a single setup AND its name matches the target hostname"
- Helper `listSetupNames(body string) ([]string, error)` ALREADY exists at `internal/tools/workflow_export_probe.go:108`

**Root cause:** hostname-as-default is correct for single-runtime adoption (simple-mode, local-stage) but wrong for recipe shapes (multiple setups named `dev`/`prod`). The handler doesn't auto-detect from the yaml even though the parsing helper exists.

**Proposed fix:** handler change in `deploy_local_git.go`:

```go
setupName := input.Setup
if setupName == "" {
    setupName = resolveSetupNameFromYaml(ctx, sshDeployer, rt, hostname)
}
```

`resolveSetupNameFromYaml`:
1. Read `/var/www/zerops.yaml` (or local working dir) via existing helpers.
2. `names := listSetupNames(body)`.
3. If exactly 1 → return that name.
4. If hostname matches a setup name → return hostname (existing convention).
5. If hostname doesn't match BUT canonical `dev` exists for git-push self-deploy → return `dev`.
6. Otherwise → return error with `availableSetups: [...]` (let the deploy fail-fast with helpful message).

**Anti-patch check:** is auto-detect papering over a deeper convention issue? Possibly. The deeper question: should setup names always be canonical (`dev`/`prod`), and hostname-matched setups deprecated? That's a bigger change touching every recipe + every adopted single-runtime project. Auto-detect at the handler boundary handles both shapes without breaking anyone.

**Verdict (pre-Codex):** ship auto-detect at handler boundary. Document the resolution order in atom guidance.

**Tests:**
- `TestDeployGitPush_AutoDetectSetup_SingleSetup` — single setup → uses it.
- `TestDeployGitPush_AutoDetectSetup_HostnameMatch` — hostname matches → uses hostname.
- `TestDeployGitPush_AutoDetectSetup_DevFallback` — `dev` exists, no hostname match → uses `dev`.
- `TestDeployGitPush_AutoDetectSetup_AmbiguousFailsClean` — multiple non-matching → structured error.

---

### F4 — `gh` CLI auth dead-end (MED)

**Symptom:**
> "The `build-integration` response gives you shell commands like `gh secret set ZEROPS_TOKEN -b "$ZCP_API_KEY"`... But ZCP's container has no GitHub auth configured."

**Surface:**
- `internal/content/atoms/setup-build-integration-actions.md:64-80` — emits `gh secret set` commands
- `internal/tools/workflow_build_integration.go:225-300` — response composes nextStep prose

**Root cause:** atom + handler assume gh is authenticated. Local-mode operators usually have it; container-mode ZCP doesn't. Agent runs the suggested command, hits HTTP 401, has no Recovery surface.

**Proposed fix (3 layers, increasing effort):**

1. **Detection at handler boundary** (`workflow_build_integration.go` for `integration=actions`):
   - Pre-flight: `gh auth status` (via SSH if container, exec if local).
   - If unauthenticated → response carries `ghAuthRequired: true` + `manualFallback: { url: "<github.com/repo>/settings/secrets/actions", instructions: "Set ZEROPS_TOKEN to ZCP_API_KEY and ZEROPS_SERVICE_ID to <id> manually via the UI." }`.
   - Atom guidance branches on the field.

2. **Atom edit** (`setup-build-integration-actions.md`):
   - Open with "First: `gh auth status` — if unauthenticated, jump to the manual-UI fallback below."
   - Manual-UI fallback section with explicit URL template + step-by-step.

3. **Optional auto-provision** (gated on GIT_TOKEN scope detection):
   - If GIT_TOKEN has `Repository Secrets: write` + `Repository Workflows: write`, `echo $GIT_TOKEN | gh auth login --with-token` before `gh secret set`.
   - Risk: scope check is heuristic (PAT scopes aren't queryable without API call). Recommend deferring.

**Anti-patch check:** could ZCP provision gh authentication once at init time? Yes, but that's an init-flow concern, not a build-integration concern. For now, detection + fallback at the right layer.

**Verdict (pre-Codex):** ship layers 1 + 2. Defer layer 3.

**Tests:**
- `TestWorkflowBuildIntegration_GhAuthDetectionFailure_SurfacesFallback` — when gh auth fails, response carries `manualFallback` block.

---

### F5 — `skipRestart=true` env-set + GIT_TOKEN race (MED)

**Symptom:**
> "skipRestart=true on `zerops_env action=set` will silently leave the new GIT_TOKEN value out of the container's environment... when you then call `zerops_deploy strategy=git-push`, the failure classification says `category=credential`."

**Surface:**
- `internal/tools/env.go:31` — `SkipRestart FlexBool`
- `internal/tools/env.go:195` — current `NextActions` warns about non-live values
- `internal/ops/deploy_git_push.go:42-44` — `BuildGitPushCommand` SSH-execs `umask ... > ~/.netrc ... password $GIT_TOKEN`

**Root cause:** `skipRestart=true` was designed for batch env-mutations. The `git-push-setup` walkthrough passes `skipRestart=true` for GIT_TOKEN because "next call is deploy which redeploys." But `zerops_deploy strategy=git-push` does NOT restart the source container — it SSH-execs a .netrc-write that reads `$GIT_TOKEN` from the container's env. Without the restart, $GIT_TOKEN isn't in the container env. Race window.

**Proposed fix (two-part):**

1. **Detection on env-set with credential pattern keys:**
   `env.go` set action — when `skipRestart=true` AND any key matches credential pattern (`*_TOKEN`, `*_KEY`, `*_SECRET`, exact `GIT_TOKEN`/`GH_TOKEN`/`GITLAB_TOKEN`), response carries `credentialNotLive: true` + `recoverAction: "zerops_manage action=reload service=<hostname>"`.

2. **Walkthrough emits `skipRestart=false` (default) for GIT_TOKEN:** `setup-git-push-container.md` atom — drop the `skipRestart=true` flag from the GIT_TOKEN-set command. Auto-restart is correct here; trying to batch was wrong.

**Anti-patch check:** option (1) is a warning; option (2) is the root fix (the walkthrough atom guidance was wrong). Both are right — (1) catches the edge case if someone hand-rolls the env-set; (2) eliminates the friction for the canonical path.

**Verdict (pre-Codex):** ship both.

**Tests:**
- `TestEnvSet_SkipRestartCredentialKey_SurfacesCredentialNotLive`
- Walkthrough atom update verified via golden regen.

---

### F6 — Post-launch state opacity (MED, design-acknowledged)

**Symptom:**
> "You cannot verify anything about the production project from here... CLAUDE.md routing table has a gap [for post-launch ops]."

**Surface:**
- P-LP-7 invariant (`docs/spec-workflows.md`): ZCP never PUTs to prod's integration endpoint
- `internal/content/atoms/launch-pipeline-configure-dashboard.md` — dashboard guidance
- `internal/content/atoms/launch-post-checklist.md` — post-launch checklist

**Root cause:** architecturally correct. Source-side ZCP session has no token to reach prod project (launchKey is one-shot, deleted post-launch). The gap: atom + CLAUDE.md don't explicitly tell agents/users that "post-launch ops happen in a DIFFERENT ZCP session bound to the prod project."

**Proposed fix (atom + CLAUDE.md):**

1. **`launch-post-checklist.md`** — add a "Subsequent prod ops" section:
   > To deploy a new release to your launched production project:
   > (a) Generate a project-scoped token from Settings → Access Tokens on the **prod** project (not this dev session).
   > (b) Open a new ZCP session bound to that project: set `ZCP_API_KEY` to the prod token and `ZCP_PROJECT_ID` to the prod project ID.
   > (c) Use that prod-side ZCP session for ongoing ops — develop/deploy/build-integration.
   > Alternatively: use the Zerops dashboard's deploy panel directly (no ZCP).

2. **CLAUDE.md** — add a "Post-launch ops" entry:
   > Launch-production creates a SEPARATE prod project. Source-side ZCP session has no token to reach it post-launch (launchKey is one-shot). Post-launch deploy questions go to a prod-side ZCP session OR the Zerops dashboard.

**Anti-patch check:** could ZCP track the prod project ID + suggest "switch context" UX? Possible v2; not warranted for v1. Pure messaging fix.

**Verdict (pre-Codex):** ship messaging.

---

### F7 — `sourceContext.suggestedRuntime` confusion (LOW, atom lag)

**Symptom:**
> "the `sourceContext.suggestedRuntime` in the launch response says `appstage`, but `targetService` accepts the dev-half hostname (`appdev`)."

**Surface:**
- `internal/tools/launch_source_context.go` — `SuggestedRuntime` is the STAGE hostname for pairs
- Post-P1+P2: `normalizeTargetServiceForLaunch` accepts either half
- `internal/content/atoms/launch-scope-prompt.md:27` — already says "Either half is accepted as input — the handler normalizes internally"

**Root cause:** atom wording IS post-P1/P2-correct. Friction is the field NAME (`suggestedRuntime`) reads as a copy-paste input value when it's actually a "validated headline" hint.

**Proposed fix:** field rename + companion field:
- Rename `SuggestedRuntime` → `PromotionHeadline` (or `SuggestedTargetService` with clarifying schema description).
- Add `Promotables: [...]` companion field that surfaces the dev-half + stage-half explicitly per runtime.

**Anti-patch check:** is this just renaming? No — `Promotables[]` aligns with the P2 input shape (`WorkflowInput.Promotables`), so the response and the input speak the same language. Small but symmetric.

**Verdict (pre-Codex):** field rename + Promotables companion + atom update.

**Tests:** atom-golden regen + `TestLaunchSourceContext_PromotablesShape`.

---

### F8 — launchKey reuse expectation (LOW)

**Symptom:**
> "the user casually says 'same launchKey' as if they expect to reuse it."

**Surface:**
- `internal/content/atoms/launch-mutation-key-required.md`
- `internal/content/atoms/launch-delete-key.md`

**Root cause:** trust model correct; agents/users default to "key = persistent credential." Atom doesn't lead with "one-shot, account-wide, must-delete" prominence.

**Proposed fix:** atom edit.

- `launch-mutation-key-required.md` lede:
  > **This key is one-shot.** Generate, use for THIS launch, then delete. Future ops on the launched project use a **separate** project-scoped token from THAT project's dashboard — not this key.

- `launch-delete-key.md` reinforces it.

**Verdict (pre-Codex):** ship atom edit.

---

---

## Codex review — corrections + amplifications

Codex's deep-dive pass corrected 4 of my 8 verdicts and confirmed the rest. Summary:

### F1 (atom axis correction)

**My miss:** I targeted `idle-launch-entry.md`. **Codex catch:** unbootstrapped is `idleScenario=adopt`, not `bootstrapped` (`internal/workflow/envelope.go:45-47`). `idle-adopt-entry.md` is the atom that fires; edit it instead. Also: don't build a CLAUDE.md-side ServiceMeta evaluator — that duplicates `handleRoute`'s logic (`internal/tools/workflow_route.go:25-43`).

**Revised fix:** edit `idle-adopt-entry.md` to mention "if intent is launch-production and runtimes are unmanaged, run bootstrap adopt first; launch-production becomes available post-adopt." Plus workflow/tool description note. Skip CLAUDE.md routing-table addition.

### F3 (use the existing resolver, don't write a new one)

**My miss:** I proposed a generic "auto-detect from yaml" helper. **Codex catch:** non-git-push deploy ALREADY has setup resolution (`internal/tools/deploy_preflight.go:74-90`, `:136-153`) that does `explicit setup → role (dev/prod) → hostname`. Git-push bypasses it (`deploy_git_push.go:283-290`, `deploy_local_git.go:87-92`). **Right fix: extract & reuse the existing resolver in git-push preflight.** Don't deprecate hostname fallback (it's the legitimate legacy adoption pattern).

**Revised fix:** refactor `deploy_preflight.go` setup resolution into a reusable helper, call it from `deploy_git_push.go` + `deploy_local_git.go` before the hostname fallback. Explicit `setup=` parameter remains supported.

### F4 (Codex adds: pre-stamp issue)

**Confirm:** add `ghAuthRequired` / `ghAvailable` / manual UI fallback. **Codex caveat:** do NOT auto-login `gh` with GIT_TOKEN — silently broadens git credential into CLI credential + persists auth state. Make any auto-login an explicit user opt-in after `gh auth status` fails.

**Codex adds (separate finding F4b):** `BuildIntegration=actions` is stamped on meta BEFORE the external workflow/secrets are actually installed (`workflow_build_integration.go:192-202`). If eval surfaces friction from this, split "handoff prepared" from "confirmed installed." Out of scope for this plan but tracked.

### F5 (narrow the warning, don't blanket credential-pattern keys)

**My miss:** I proposed warning on every `*_TOKEN`/`*_KEY`/`*_SECRET` skipRestart=true. **Codex catch:** too broad. **Right fix narrower:**
1. Walkthrough drops `skipRestart=true` for GIT_TOKEN (canonical path).
2. `deploy_git_push.go` preflight detects EXACT case: live `$GIT_TOKEN` empty AND project env has GIT_TOKEN → specific recovery to `zerops_manage action=reload <hostname>`.
3. Skip the freshness-heuristic N-second window.

### F7 (structural, not rename)

**My miss:** I called it "field rename + companion field." **Codex catch:** the code COMMENT at `launch_source_context.go:38-41` says `SuggestedRuntime` should be "always the dev-half hostname" but the IMPLEMENTATION populates it with the stage hostname (`:144-146`), and tests pin the stage value (`launch_source_context_test.go:258-270`). **Code and doc are internally contradictory.** Fix needs:

- New field `targetServiceCanonical` = dev-half (the literal copy-paste value for `targetService` input).
- Keep existing `SuggestedRuntime` value (stage hostname) but **rename to `PromotionHeadline`** — human-facing presentation only.
- Tests reshape; no compat shim (pre-prod, CLAUDE.md rule).

### F8 (wording alone NOT enough — internal contradiction)

**My miss:** I called it pure wording. **Codex catch:** atoms have an internal contradiction:
- `launch-intro.md:24` says "revoke the key the moment `launched` status returns."
- `launch-pipeline-configure-dashboard.md:24` + `launch-pipeline-skipped.md:18` tell agents to re-call launch with the SAME `launchKey` to re-check pipeline state.
- Code supports this when pipeline config is pending (`workflow_launch_production.go:283-288`, `launch_pipeline.go:197-204`).

**Right fix:** split the guidance into two distinct phases of the key's lifecycle:
1. **Launch-window** (until pipeline is confirmed configured): key MAY be reused within this session for pipeline rechecks.
2. **Post-window** (after pipeline confirmed OR explicit skip): delete the key; future prod ops use a separate prod-scoped token.

The atoms must surface this split explicitly — currently the "revoke immediately" instruction contradicts the recheck path.

### F2, F6 — confirmed as-stated

No corrections. F2: ship `steps[]` structured field + atom rewrite. F6: messaging-only fix (post-launch atom + CLAUDE.md note).

### Synthesis (Codex's recurring root cause)

> "The shared root cause across F2, F3, F4, and F5 is that deploy-config flows are multi-tool handoffs but only partly machine-readable. The code has strong state gates, but the handoff sequence lives in prose, and some handlers stamp meta before external reality is true."

**Priority order (Codex's leverage analysis):**
1. **F3** + **F5** — burn real failed deploys. Highest impact.
2. **F7** — contradictory input hints. Easy structural fix.
3. **F2** — repeat friction across every source-control chain.
4. **F1, F6, F8** — knowledge-surface fixes, mostly messaging. F8 must reconcile the contradiction first.

**Architectural debt vs "just messaging":**
- **Architectural:** F3 (duplicate resolver), F7 (response shape drift), F4 (actions-stamped-before-installed — flagged but deferred).
- **Messaging:** F1, F6, F8 (after reconciling internal contradiction in F8).

---

## Open questions for Karel

1. **F3 setup resolver extraction** — should the new helper live in `internal/tools/deploy_preflight.go` (single file expansion) or `internal/ops/deploy_setup_resolver.go` (new ops-layer helper consumable from non-tools callers)? Codex implies the former; I'd argue ops-layer if export workflow could also reuse it. Pick one.
2. **F4 gh auto-login opt-in** — accept Codex's recommendation (manual UI fallback only, no auto-`gh auth login --with-token`)?
3. **F4b (build-integration meta stamp before install)** — defer, or batch into this plan? Codex flagged it but deferred.
4. **F7 field naming** — `targetServiceCanonical` (descriptive) or `targetServiceSuggestion` (suggests it's just a hint)? Or different name?
5. **F8 same-launch recheck semantics** — keep the recheck-with-key feature (current code supports it) OR remove it (force agent to track pipeline via dashboard alone after launch)? Recheck adds value but creates the lifecycle complexity.
6. **Phase split granularity** — one PR with all 8 fixes, or three PRs (handler changes / atom rewrites / spec touches)?

---

## Phase plan

Ordered by Codex's leverage analysis. Each phase ships independently.

### P1 — F3 + F5: deploy-side handler fixes (HIGH leverage, real failures)

**Files:**
- `internal/tools/deploy_preflight.go` — extract setup-resolution helper (or move to `internal/ops/`).
- `internal/tools/deploy_git_push.go:283-290` — call the helper before hostname fallback.
- `internal/tools/deploy_local_git.go:87-92` — same.
- `internal/tools/deploy_git_push.go:259-275` (push preflight) — add specific recovery for "live GIT_TOKEN empty but project env has it" case.
- `internal/content/atoms/setup-git-push-container.md` — drop `skipRestart=true` flag from GIT_TOKEN-set step in the canonical walkthrough.
- `internal/content/atoms/setup-git-push-local.md` — same.

**Tests:**
- `TestDeployGitPush_SetupResolver_ExplicitWins`
- `TestDeployGitPush_SetupResolver_RoleFallback` (dev/prod role match)
- `TestDeployGitPush_SetupResolver_HostnameLegacyFallback`
- `TestDeployGitPush_GitTokenLiveEmptyButProjectHasIt_RecoverReload`
- `TestDeployLocalGit_SetupResolver_*` (mirror set)

**Risk:** setup resolver behavior change — verify both hostname-fallback and role-fallback continue to work for adopted single-runtime + recipe shapes.

### P2 — F7: source-context response shape

**Files:**
- `internal/tools/launch_source_context.go:30-72` — add `TargetServiceCanonical` field (dev-half), rename `SuggestedRuntime` → `PromotionHeadline`.
- `internal/tools/launch_source_context_test.go:258-298` — tests reshape: pin both fields.
- `internal/content/atoms/launch-scope-prompt.md:26-28` — point at `targetServiceCanonical` for input; `promotionHeadline` as disclosure.

**Tests:**
- `TestLaunchSourceContext_StandardPair_ExposesCanonicalAndHeadline`
- `TestLaunchSourceContext_SimpleMode_CanonicalEqualsHeadline`

**Risk:** field rename touches every test fixture using the old field. Pre-prod rule = no compat shim, full rename.

### P3 — F2: git-push-setup walkthrough — structured steps[]

**Files:**
- `internal/tools/workflow_git_push_setup.go:149-178` — add `steps[]` field to walkthrough response shape.
- `internal/content/atoms/setup-git-push-container.md` — rewrite as checklist-first, reference prose at bottom.
- `internal/content/atoms/setup-git-push-local.md` — same.
- (Optional) integration-aware PAT scope variants: if `integration=webhook`, walkthrough skips Actions-only PAT-scope rows.

**Tests:**
- `TestGitPushSetupWalkthrough_EmitsStepsField`
- Atom golden regen for both env modes.

**Risk:** atom rewrite touches scenario goldens.

### P4 — F1 + F6 + F8: messaging cluster

**Files:**
- `internal/content/atoms/idle-adopt-entry.md` — add "if user wants launch-production but services unmanaged, run adopt first" note.
- `internal/tools/workflow.go` — workflow description string: mention "requires bootstrapped services (run bootstrap adopt route if meta missing)."
- `internal/content/atoms/launch-post-checklist.md` — explicit "subsequent prod ops" section (separate prod-scoped token, separate ZCP session, or dashboard).
- `CLAUDE.md` — "Post-launch ops" entry in the launch-production convention bullet (clarify source-side session cannot reach prod post-launch).
- `internal/content/atoms/launch-intro.md` — reconcile F8 contradiction: split "launch-window key" (may be reused for pipeline recheck) from "post-window" (delete + use prod-scoped).
- `internal/content/atoms/launch-pipeline-configure-dashboard.md` + `launch-pipeline-skipped.md` — clarify the recheck path uses the SAME key within the launch window.
- `internal/content/atoms/launch-delete-key.md` — make the trigger explicit (after pipeline confirmed OR explicit skip).

**Tests:** atom lint + scenario goldens regen.

**Risk:** atom rewriting needs verification of axis-M/axis-N lint compliance (CLAUDE.local.md atom authoring contract).

### P5 — F4: gh auth detection + fallback

**Files:**
- `internal/tools/workflow_build_integration.go:225-300` — pre-flight `gh auth status` (env-aware: SSH if container, exec if local); response adds `ghAvailable` + `ghAuthenticated` + `manualFallback` block.
- `internal/content/atoms/setup-build-integration-actions.md:64-83` — open with "First: `gh auth status` — if unauthenticated, jump to manual-UI fallback." Add fallback section.

**Tests:**
- `TestWorkflowBuildIntegration_GhAuthUnavailable_SurfacesFallback`
- `TestWorkflowBuildIntegration_GhAuthAvailable_KeepsCurrentShape`

**Risk:** SSH-exec of `gh auth status` adds one round-trip to build-integration response time (~200ms). Acceptable.

### Post-plan: F4b (deferred)

`BuildIntegration=actions` stamp-before-install — out of scope for this plan. Track in backlog.

---

## Acceptance criteria

| Phase | Verify |
|---|---|
| P1 | `go test -race ./... -count=1`; re-run `flow-eval launch-production-existing-with-webhook` + `launch-production-dev-only` (the scenarios that surfaced F3) — retrospective should NOT mention setup-name friction. |
| P2 | Same suite; retrospective should NOT mention "suggestedRuntime says appstage but targetService expects appdev". |
| P3 | Re-run `launch-production-from-standard-pair` — retrospective should mention parsing `steps[]` structured field, not "wall of text." |
| P4 | Re-run `launch-production-pipeline-configured` + `launch-production-pipeline-not-configured` — retrospective should NOT flag post-launch CLAUDE.md routing gap; should NOT find launchKey lifecycle contradiction. |
| P5 | Re-run `launch-production-new-project-push-mode` — retrospective should NOT mention HTTP 401 dead-end on `gh secret set`. |

After all 5 phases ship + flow-eval re-verification: `make release-patch` ships the cumulative friction-fix release.
