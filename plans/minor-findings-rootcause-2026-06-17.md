# Minor flow-eval findings — root-cause plan (2026-06-17)

## STAV (shipped 2026-06-17, branch `fix/eval-gitpush-launchprod-feedback`)

All 5 fix-now clusters shipped + verified (`go test ./... -short` PASS, `make
lint-local` 0 issues, `-race` PASS on touched packages):

| Cluster | Commit | What |
|---|---|---|
| git-token (#1+#2+R0) | `6e9bffa4` | cause-checklist in transportGitAuth, repo-select trap in recommendation, write-auth-proven nextStep, topology single-owner const |
| F9 | `3a093339` | guarded gh-secret builder for launch-prod (no silent empty `ZEROPS_TOKEN_PROD`) |
| F1 | `2bc6e926` | close-mode presents auto/manual only; git-push is a delivery dimension |
| F10 | `e6585830` | warn-blocker leads with "does NOT block launch" |
| F6 | `99fc8d60` | single-owner credential ask-mechanism hedge (AskUserQuestion OR plain text) |

Dropped/backlogged exactly as triaged below — F2/F4/F8 → `plans/backlog/rejected/`,
F3/F5 → `plans/backlog/`. eval2 stale-workflow cleanup (test-env, not ZCP) left to
the operator (outward-facing push to a user repo — not auto-done).

Remaining eval gate: run the flow-eval scenarios in §4 to observe the user-facing
improvement (observation-only; no CI gate).

---


Source: two flow-eval retrospectives (suites `20260617-171651` git-push-setup-then-actions,
`20260617-172316` launch-with-existing-cicd) surfaced ~10 friction findings after the
git-push/launch-prod fix batch (`34316914`…`11de178f` on `fix/eval-gitpush-launchprod-feedback`).

Investigated via a 21-agent root-cause workflow (each finding: read the actual code +
introducing commit → Step-1 bug-vs-symptom → single-owner → fix → tests → triage), then an
adversarial verify pass that **reversed 5 of 8** findings, then synthesis.

---

## Unifying pattern

Every fix-now finding is **one defect class: a TELL surface drifted from its single owner** —
either from the CHECK that enforces the behavior, or from a sibling path that already does the
right thing. The remedy is the same architectural move throughout: **single-owner derivation +
presentation-set discipline** — the value/clause the agent is *shown* derives from the one
authority, never re-authored per site. The reversed findings were rejected precisely because
their proposed fixes would have *broken* this discipline (dead URI pointer; content-blind
write-suppression against a write-class probe; validation-set leaking into the presentation set).

---

## Triage outcome

| ID | Finding | Verdict | Why |
|---|---|---|---|
| **F1** | close-mode "folded to auto" reads like rejection | **FIX** | symptom of: git-push retired as close-mode VALUE, but TELL (4 atoms + router + msg) still offers it |
| **R0** | #2 write-proof residue: stale "read probe / write NOT proven" strings | **FIX (new)** | self-inflicted drift from `11de178f`: probe is now write-class, 3 strings + 1 comment lie |
| **F10** | launch build-integration warn-blocker unclear if it blocks | **FIX** | TELL/CHECK parity: message doesn't lead with what `gateHasBlockingFailure` enforces (warn ≠ block) |
| **F6** | credentialsRequired prescribes AskUserQuestion, no plain-text hedge | **FIX** | parallel-paths-to-parity: atoms already hedge, handler strings don't; 1 owner const |
| **F7** | GIT_TOKEN_INVALID can't name likely cause | **FIX** | enrich classifier + consolidate PAT-scope literal into topology const (retire duplicate) |
| **F9** | launch-prod `gh secret set` silently writes empty `ZEROPS_TOKEN_PROD` | **FIX** | real correctness bug: hand-rolled `secretCmd` lacks `[ -n ]` guard that `ghSecretSetCommand` has |
| F2 | build-integration response density | **DROP** | proposed `zerops_knowledge uri=` targets an inline atom `resolveAtomURI` rejects (dead pointer); "twice in one turn" premise false (sequential surfaces); real seam subsumed by F7 |
| F4 | `rebase --continue --no-edit` unsupported | **DROP** | agent-error: grep-verified ZERO `--no-edit`/`GIT_EDITOR` hits in repo — ZCP never emits it; principle owned at `agents_container.md:3` |
| F8 | "Public repositories only" PAT trap | **DROP** | premise factually false vs HEAD: probe is ALREADY write-class (`11de178f`, my #2) → read-only PAT 403s at probe; investigator misread a stale comment → that comment IS R0 |
| F3 | workflow-file freshness (skip write if remote has it) | **BACKLOG** | proposed present→suppress is content-blind masking (drifted file wrongly skipped); `deploy_git_push` already gives non-fast-forward recovery; eval-env-amplified |
| F5 | adopt plan-type OS-prefix (`ubuntu/nodejs@22` vs `nodejs@22`) | **BACKLOG** | real drift but bare type validated+worked (CHECK form-tolerant); primary path already hands composite paste template; proposed fix leaks validation-set into presentation-set |

Backlog entries → `plans/backlog/workflowfile-freshness-diff-not-blindwrite.md`,
`plans/backlog/adopt-plantype-composite-spelling.md`.

---

## Phases (atom-only → prose → code; each independently build+test green)

### Phase 1 — close-mode menu parity (F1)
Root: `git-push` was retired as a close-mode VALUE (delivery DERIVES from `GitPushState`;
`foldLegacyCloseMode` one-ways it to `auto`) but only the CHECK side (`handleCloseModeList.options
= {auto, manual}`, spec §687) was updated. The TELL still enumerates it → agent picks a value the
handler rewrites → the "legacy git-push folded" message reads like rejection.

- **Owner:** `handleCloseModeList.options` (presentation set). `validCloseModes` (keeps git-push
  for wire-compat) is the validation set — **left untouched**; the value must keep PARSING, it just
  stops being OFFERED.
- **Files:** `internal/content/atoms/develop-strategy-review.md`,
  `develop-standard-unset-iterate.md`, `develop-change-drives-deploy.md`,
  `develop-standard-unset-promote-stage.md`, `internal/workflow/router.go`,
  `internal/tools/workflow_close_mode.go` (msg), `internal/content/git_push_atom_sentinel_test.go`.
  *(6–7 files but one cohesive change — see Open Q3.)*
- **Fix:** strip git-push from close-mode *menu/gating* contexts; replace with one orthogonality
  line ("delivery is a separate dimension — run `git-push-setup`; works under either close-mode").
  Reword fold msg from event-shaped → confirmation-shaped (no "legacy"/"folded"). **Leave
  delivery-strategy git-push refs intact** (`develop-first-deploy-intro`, `develop-git-push-delivery`
  — those are live deploy arguments, not close-mode values).
- **Tests:** sentinel `TestAtomCorpus_NoForbiddenGitPushClaims` extended to forbid the *class*
  (any close-mode menu listing git-push + the "auto and git-push" gating phrase);
  `TestHandleCloseMode_GitPushFold_MessageReadsAsConfirmationNotRejection` (contains `=auto` +
  `git-push-setup`, NOT "folded"/"legacy"; meta persists auto);
  `TestDevelopRouterHint_CloseMode_OffersAutoManualOnly`.
- **Golden-regen:** YES (`ZCP_UPDATE_ATOM_GOLDENS=1`, all affected develop-* renders).

### Phase 2 — #2 write-proof residue (R0; was F8's misread comment)
Root: `11de178f` (#2) made BOTH probes write-class (`push --dry-run`, unborn-HEAD fallback to
ls-remote) but left agent-facing strings + a comment asserting the OLD read-only behavior. They
now lie in the dominant (born-HEAD) case.

- **Files:** `internal/tools/workflow_git_push_setup.go` (lines ~382 comment, ~434 local nextStep,
  ~665 container nextStep).
- **Fix (precise — validate leaf against trunk):** `push --dry-run` to a throwaway branch proves
  **write-AUTH** (git-receive-pack accepted the credential) but NOT that `HEAD→main` is
  fast-forwardable (no divergence on a fresh branch). So narrow the hedge, don't delete it:
  "write authentication is proven; a divergent-remote (non-fast-forward) on your branch still
  surfaces at the first real push." Fix line-382 comment ("read-only auth check" → "write-auth
  probe (push --dry-run), non-mutating"). Unborn-HEAD fallback case keeps the honest "write not
  yet proven" wording.
- **Tests:** `TestGitPushSetup_NextStep_WriteAuthProvenNotReadProbe` (local + container nextStep
  contain "write" auth proven, NOT "read probe"/"NOT proven yet" in the born-HEAD path);
  unborn-HEAD path keeps the hedge. No golden (response prose).

### Phase 3 — launch warn-blocker clarity (F10)
Root: `sourceControlBlockerFor` (build-integration-recommended branch) message doesn't lead with
what `gateHasBlockingFailure` (CHECK) enforces — warn does NOT block.

- **Files:** `internal/tools/launch_source_control_gate.go` (message),
  `internal/tools/workflow.go` (`SkipBuildIntegration` schema tag), test file.
- **Fix:** both warn-message variants lead with "This does NOT block launch — recommended but
  optional"; mirror one sentence in the schema tag. **Drop the "independent of stage CI"
  WHY-clause** (pedagogical, doesn't change behavior). No behavior change.
- **Tests:** `TestHandleLaunchProduction_BuildIntegrationNone_NoSkip_AdvancesToClassifyPrompt`
  (seed `BuildIntegrationNone`, no skip → status=classify-prompt, exactly one warn blocker — pins
  an invariant no current sequence test covers; fixtures default `BuildIntegration=actions`). Keep
  `TestValidateLaunchSourceControl_SkipBuildIntegration_AckSuppressesWarn` green. **Drop** a
  brittle `MessageStatesNonBlocking` wording-assert (ossifies advisory prose).

### Phase 4 — credential-ask single owner (F6)
Root: the AskUserQuestion clause is hand-authored at 3 sites; atoms already hedge "AskUserQuestion
when your harness exposes it, else plain text", handler strings don't.

- **Files:** `internal/tools/errwire.go` (new exported const), `workflow_launch_production.go`
  (`credentialUserOwnedAskContract` derives), `workflow_build_integration.go` (2 local-mode gh
  strings derive), test updates.
- **Fix:** one exported const carries the hedged "(AskUserQuestion when harness exposes it, else
  plain text); WAIT; NEVER fabricate"; 3 sites concatenate it. Single-owner derivation IS the
  anti-drift mechanism — no new broad substring guard.
- **Framing (Open Q4):** ship as **TELL-parity dedup**, NOT a real-world dead-end fix (the denial
  dance is an eval-harness artifact; real Claude Code supports AskUserQuestion).
- **Tests:** update existing pins in same commit: `TestConvertError_CredentialContract`,
  `TestLaunchSourceControlRequired_CredentialsAskBlock` (keep "WAIT for their answer"/"NEVER invent"
  substrings), `TestBuildIntegration_Phase5_LocalGhTokenTell`.

### Phase 5 — git-token diagnostics + topology scope const (F7)
Root: the auth-rejection classifier doesn't name the likely cause; the PAT push-min scope literal
is duplicated (unguarded) across `gh_pat_scope.go` + `transportGitRepoNotFound`.

- **Files:** `internal/topology/<scope const file>` (new PAT push-min scope const + settings URL),
  `internal/ops/deploy_failure_signals.go` (`transportGitAuth` enriched, `transportGitRepoNotFound`
  derives), `internal/tools/gh_pat_scope.go` (add topology import; derive scope literal), test files.
- **Fix:** topology const (import-legal from ops + tools, stdlib-only) owns the scope string +
  settings URL; `ghPATScopeRecommendation`, `transportGitRepoNotFound`, and the new
  `transportGitAuth` cause-checklist all read it — makes `gh_pat_scope`'s "single owner"
  doc-comment true. **Cause-set trimmed to likeliest-2** (fine-grained-PAT "Public repositories
  only" default; SAML-unauthorized for org repos), typo/expired collapsed to one terse line
  (Open Q2). Do the const consolidation NOW (backlogging it = silent scope-cut per verdict).
- **Tests:** `TestClassifyDeployFailure_GitAuthRejected_EnumeratesClosedCauseSet` (2 cause markers
  + scope literal from const; keep existing HTTP-Basic case green);
  `TestGitPushSetupContainer_ProbeFailure_AuthRejected_SurfacesCauseChecklist`; re-verify
  `TestGhPATScope_AtomsAgreeWithOwner` against const-derived owner.
- **Live-platform:** GitHub fine-grained-PAT "Public Repositories" = read-only — confirmed in
  investigation; no further verification before coding.

### Phase 6 — gh-secret single builder (F9)
Root: launch-prod `prodCDActionsBlock:1896` hand-rolls `secretCmd` via `fmt.Sprintf` WITHOUT the
`[ -n "$_t" ]` empty-token guard + failure echo that `ghSecretSetCommand:720` has → an empty staged
token silently writes an empty `ZEROPS_TOKEN_PROD` repo secret (broken prod delivery).

- **Files:** `internal/tools/workflow_build_integration.go` (parametrize `ghSecretSetCommand` for
  the SSH-read launch-token value expr), `workflow_launch_production.go` (`prodCDActionsBlock` calls
  the shared builder), test files.
- **Fix:** route launch-prod's `secretCmd` through `ghSecretSetCommand` → gains the empty-token
  guard + failure echo. **NO new constants, NO happy-path success marker** (verdict: the inline
  ZCP_* markers elsewhere are literals read by ZCP, not agent-read named constants — inventing a
  convention; agent has the exit code). Close the one real TELL drift: `ghTokenFailureSymptom`
  references the builder's echo-suffix.
  - *Impl note:* the builder's `[ -n ]` guards the GH_TOKEN auth value (GIT_TOKEN). Confirm during
    coding whether the launch-token VALUE also needs an emptiness guard, or whether reusing the
    auth-guard suffices — the bug is the empty *secret value*, so the value path is what must not
    write empty. May need a small builder extension (guard the value expr too).
- **Tests:** `TestActionsTrackProdCD_SecretCmd_HasEmptyTokenGuard` (guard + failure echo present);
  TELL==CHECK test that `ghTokenFailureSymptom` contains the builder's echo suffix.

---

## Eval gate (observation-only, after all phases land)

| Scenario | Phases | Look for in retrospective |
|---|---|---|
| `greenfield-node-postgres-dev-stage` (or strategy-setup) | 1, 4, 6 | close-mode picked without git-push-as-rejected confusion; no ambiguous empty stdout after `gh secret set` |
| launch-production w/ under-scoped fine-grained PAT (operator-supplied) | 5, 2 | on probe failure agent names likely cause (Public-repos-only / missing Contents:write) + asks re-scope; write-auth honestly described |
| launch-production `BuildIntegration=none`, no skip | 3 | agent proceeds past warn without inferring "skip required to advance" |
| `flow-eval-local` launch where harness lacks structured ask | 4 | plain-text ask, no denial→prose dance |

---

## Backward-compat

All changes are LLM-facing prose or response-field *values* (not field names/shapes). `validCloseModes`
+ `foldLegacyCloseMode` untouched → legacy on-disk meta + in-flight calls keep working. `SkipBuildIntegration`
field key unchanged. New topology const internal. Signal id `transport:git-auth-failed` unchanged. No
`.mcp.json` allowlist, `.zcp/state` shape, or atom-ID change. All atom-IDs stay dereferenceable.

## Effort

~6 phases · est. 250–350 LOC (mostly atom prose + classifier text + one shared-builder reroute) ·
~1–1.5 days incl. golden-regen + eval gate.

---

## Open questions (Karel decides before/at coding)

1. **R0 scope** — fold the #2 write-proof residue cleanup as its own Phase 2 (recommended — it's a
   self-inflicted comment-vs-code lie from `11de178f`, fix regardless), or skip? *Recommend: keep as
   Phase 2.*
2. **F7 cause-set** — trim to likeliest-2 (Public-repos-only + SAML) with typo/expired as one terse
   line, or keep all 4? *Recommend: 2 (recommend-don't-dump); cheap to re-expand if eval shows
   typo/expired confusion.*
3. **F1 file count** — accept 6–7 files in one phase (cohesive "strip git-push from close-mode menu"
   change), or split fold-message+sentinel into Phase 1b? *Recommend: one phase — splitting leaves
   Phase 1 green-but-incomplete (atoms stripped, sentinel not yet forbidding regression).*
4. **F6 framing** — ship as TELL-parity dedup, NOT claiming a real-world dead-end fix (denial dance
   is eval-harness artifact). *Recommend: yes — messaging fidelity only, no code-shape question.*
