# Systemic fix — git-push lifecycle truthful contract

**Date:** 2026-05-23
**Scope:** `git-push-setup`, `build-integration`, `zerops_deploy strategy=git-push`, `launch_source_control_gate`, `export-publish` source-control gate
**Status:** Plan after 6 explore agents + 2 Codex review rounds
**Effort estimate:** ~1.5-2 weeks of focused work (smaller patch path)

---

## TL;DR

**Root cause:** Po `2026-04-28 096207ff` decomposition splitu (`action=strategy` → 3 actions) handlery zůstaly **passive state-stampers**. Flagy `GitPushState=configured` + `BuildIntegration=actions/webhook` jsou self-attested intent ze strany uživatele, ne ověřená operační schopnost. **Spec, atomy, 7+ recovery messages, launch source-control gate, export RemoteURL refresh** s těmihle flagy zachází jako s **ověřenou capability**. Drift způsobuje agentí friction v live evalech — agent narazí na credential/setup pasti až při použití (deploy, launch), ne při setupu.

**Princip fixu:** Capability flag jen po ověření. Mutační action smí stampnout `configured` až po ověřitelné mutaci/probe, ne po předání instrukcí agentovi.

**Architektura zůstává:** 3 orthogonální actions (close-mode / git-push-setup / build-integration) jsou správné. Návrat k monolithu NE. Jen každá action musí být **honest** o tom, co dělá.

**Asymmetry:** `git-push-setup` = **hard verifier** (token + URL + origin + auth probe). `build-integration` = **honest handoff** (ZCP nemá API na probe GitHub Actions / dashboard webhook fairly).

---

## 1. Diagnóza

### Co flagy fyzicky znamenají DNES vs co tvrdí

| Flag | Atom/spec claim | Handler reality |
|---|---|---|
| `meta.GitPushState=configured` | "GIT_TOKEN + .netrc + remote URL are stamped/provisioned/walked-through" *(7 sites)* | `meta.RemoteURL` set + flag set; nic víc. Žádný token write, žádný .netrc, žádný origin sync, žádná auth probe. |
| `meta.BuildIntegration=actions` | "Workflow + secrets wired; pushes trigger Actions build" | Flag set; agent dostal handoff (workflow YAML + `gh secret set` commands), ale ZCP neověřuje, že agent reálně commitnul/pushnul YAML nebo set-nul secrets. |
| `meta.BuildIntegration=webhook` | "Dashboard OAuth done; pushes trigger webhook build" | Flag set; agent dostal dashboard URL. ZCP neověřuje, že OAuth opravdu proběhl (žádný platform readback). |

### Konkrétní symptomy v live evalu

1. **`.netrc` "neexistuje" po deploy** — agent SSH-uje post-deploy, vidí absence. Realita: `BuildGitPushCommand` (`internal/ops/deploy_git_push.go:31-65`) vytváří `.netrc` ephemerally inside SSH chain s `trap 'rm -f ~/.netrc' EXIT`. Atom říká "wired by deploy" — technicky pravda jen during the SSH session.
2. **`category=credential` overloaded** — missing GIT_TOKEN (env stale po skipRestart) vs invalid PAT → both → same diagnostic signal. Agent retried `.netrc` plumbing místo diagnostikování tokenu.
3. **`skipRestart=true` + immediate git-push trap** — env-var lands v platform DB; container shell session nevidí. `gitTokenCheckCmd` shell test returns 0 → recovery message `"Run git-push-setup"` loops na špatnou věc (handler GIT_TOKEN nezapisuje).
4. **Setup-name silent fallback** — `setupName := input.Setup; if "" then setupName = hostname` (`deploy_git_push.go:286-288`). Standard pair: hostname `appdev`, yaml block `dev` → `INVALID_ZEROPS_YML`.
5. **`build-integration actions` precondition spider** — atom assumes `gh auth login` done. No precondition check. PAT pro git push vs PAT pro gh-CLI: 2 různé credentials, nikde explicit roadmap.

### Sekundární problémy (Codex round 1 blind spots)

6. **Launch gate `dev-tree-dirty` recovery je špatně** — atom `launch-source-control-required.md` row 4 říká `zerops_deploy strategy=git-push` "commits the live working tree" → handler REFUSES dirty trees (`committedCodeCheckCmd`). Recovery sends agent do dead-end.
7. **Launch `remote-mismatch` recovery points at no-op** — atom říká rerun `git-push-setup` rewrites `.git/config`. Handler `git-push-setup` `.git/config` nedotýká (dnes); jen deploy at push time.
8. **Local-mode `inputsRequired` inconsistent** — handler vrací `gitToken` jako required (`workflow_git_push_setup.go:153`). Atom `setup-git-push-local.md:15` říká "Token entry is NOT collected — local git already holds the user's credentials".
9. **Launch gate `git ls-remote` unauthenticated** — `launch_source_control_gate.go:319` runs `git ls-remote $url` bez auth → private repos false-fail jako `head-not-pushed`.
10. **Secret echo risk** — `zerops_env` `set` returns `Stored.Value` field. Pokud nový `git-push-setup` delegát zapisuje GIT_TOKEN přes `ops.EnvSet` bez sensitive flag, token leak do response/state.
11. **Container `GIT_TOKEN` + SSH remote URL kolizí** (Codex round 2 missed cost) — `.netrc` + PAT funguje jen pro HTTPS. Pokud agent pass-uje `git@github.com:org/repo.git` (scp-form SSH), verifier bude failovat correct legit URL, agent to diagnose jako token bug.

### Mezi-handler dependency

```
                ┌─────────────────────────────────────────┐
                │  zerops_env set GIT_TOKEN skipRestart=? │
                │  (writes platform env-var, NOT shell)   │
                └──────────────────┬──────────────────────┘
                                   │ AGENT MUST coordinate timing
                                   ▼
            ┌──────────────────────────────────────────────┐
            │  zerops_workflow action=git-push-setup       │
            │  (writes meta.RemoteURL + GitPushState flag) │
            │  IGNORES gitToken field declared in          │
            │  inputsRequired. No SSH, no probe, nothing.  │
            └──────────────────────┬───────────────────────┘
                                   │ AGENT MUST ensure restart happened
                                   ▼
            ┌──────────────────────────────────────────────┐
            │  zerops_deploy strategy=git-push             │
            │  (SSH commit pre-flight refuses dirty;       │
            │   ephemeral .netrc, trap-cleanup;            │
            │   git remote add origin idempotent;          │
            │   git push)                                  │
            │  This is where .netrc + remote sync ACTUALLY │
            │  happen — ephemerally.                       │
            └──────────────────────┬───────────────────────┘
                                   │ AGENT MUST observe events
                                   ▼
            ┌──────────────────────────────────────────────┐
            │  zerops_workflow action=record-deploy        │
            │  (stamps FirstDeployedAt + auto-enable       │
            │   subdomain on async builds)                 │
            └──────────────────────────────────────────────┘

Orthogonally:
            ┌──────────────────────────────────────────────┐
            │  zerops_workflow action=build-integration    │
            │  (writes BuildIntegration flag, emits        │
            │   workflow YAML + gh secret set commands.    │
            │   AGENT MUST: write file, set secrets,       │
            │   commit + push.)                            │
            └──────────────────────────────────────────────┘
```

**The problem:** Each handler stamps state assuming its job is done. The agent must orchestrate timing across them. Atoms describe an idealized "everything wired" flow that the handler implementations don't deliver. Recovery messages point at handlers that don't fix what the message claims.

---

## 2. Architektonický fix

### Princip

**Capability flag = post-verification.** `meta.GitPushState=configured` set jen když handler:
1. Sám provedl mutaci, kterou flag implikuje (token write + restart)
2. Sám ověřil reálnou capability (auth probe proti remote URL)

Pro flagy, kde ZCP nemá ověřovací cestu (`BuildIntegration` = externí GitHub Actions / Zerops dashboard webhook), zachovat **honest handoff semantics** — flag explicitly intermediate (`actions-handoff`, `webhook-handoff`) místo aspirational verified value.

### Architecture shift

```
BEFORE: passive state-stampers + atom guidance fragments
              ↓
AFTER: hard verifier (git-push) + honest handoff (build-integration)
       + shared ops/git_auth_probe.go primitive
       + symmetric source-control gates (launch + export)
```

### Layering

`internal/ops/git_auth_probe.go` (NEW) — shared transient-auth probe primitive used by:
- `internal/tools/workflow_git_push_setup.go` (setup verifier — container path)
- `internal/tools/launch_source_control_gate.go` (Phase 3 — replaces unauthenticated `git ls-remote`)
- `internal/tools/workflow_export.go` (Phase 3 — export source-control gate)

`internal/ops/env_set_sensitive.go` (NEW) or extend existing `ops.EnvSet` with `SensitiveKeys` option (Codex round 2 critical) — separates GIT_TOKEN write from value-echo path.

Layer rule respected: `ops/` is layer 1, consumed by `tools/` (layer 4) and `workflow/` (layer 3). `ops` ↛ `workflow`. `workflow` ↛ `ops`. Peer.

---

## 3. Phasing (vertical slices, test → code → atom per capability)

Codex round 2 principle: atoms can't lead handlers. Each phase delivers one shippable vertical: handler change + tests + atom updates simultaneously.

### Phase 1 — `git-push-setup` becomes hard verifier (container path)

**Goal:** Confirm mode performs probe FIRST; only on success performs side effects + stamps configured. On probe failure, project is left unchanged — no env write, no restart, no state stamp. Agent fixes inputs and re-calls.

**Sequence (probe-first, mutation-second):**

1. **Pre-checks** (no side effects): meta exists + `IsComplete()`, role is push-source-OK, URL is HTTPS form (reject scp-form `git@github.com:...` with structured error pointing at separate deploy-key flow).
2. **Probe** (no side effects): SSH to push-source container → ephemeral `.netrc` with agent's `gitToken` (trap-cleanup, same security pattern as `BuildGitPushCommand`) → `git ls-remote $remoteUrl HEAD` → cleanup `.netrc`.
   - **On probe failure:** return structured error categorized as `credential` / `url-unreachable` / `network` with recovery message. **NO STATE WRITE.** Project env untouched, no restart, no meta stamp. Agent fixes token/URL and re-calls.
3. **Probe passed → side effects start:**
   - Sensitive env write: `GIT_TOKEN` into project env (scrubbed from response/state).
   - Force-restart push-source runtime so `$GIT_TOKEN` lands in container shell.
   - SSH set/sync `origin` in `/var/www/.git/config`.
   - Stamp `meta.GitPushState=configured` + `meta.RemoteURL`.
4. Return success without secret echo.

**Why probe-first matters:**
- **Atomic semantics:** either everything works or project state is untouched.
- **No stale GIT_TOKEN in project env** if user mistyped or PAT expired.
- **No wasted 14s restart** on failing setup.
- **Probe uses identical auth pattern as real push** — if probe passes, push will pass. The probe IS the proof.

**Scope (one vertical):**
- New `internal/ops/git_auth_probe.go` primitive — supports container + local; timeouts; `GIT_TERMINAL_PROMPT=0`; SSH probe uses transient `.netrc` (same pattern as `BuildGitPushCommand`). Test pin: `TestGitAuthProbe_Container_HTTPSWithToken_Succeeds` etc.
- New `internal/ops/env_set_sensitive.go` OR `EnvSetOptions{SensitiveKeys []string, RedactValues bool}` extension — sensitive env write that does NOT return value in `Stored.Value`. Test pin: `TestEnvSetSensitive_DoesNotEchoValue`.
- `handleGitPushSetup` confirm mode (container) — probe-first sequence above.
- Note: `meta.GitPushState=broken` enum value is reserved for the deploy-time credential degradation path (Phase 5), NOT for setup-time probe failures. Setup-time failure = no stamp at all + structured error.
- Tests:
  - `TestHandleGitPushSetup_Container_HTTPSOnly_RejectsSCPForm`
  - `TestHandleGitPushSetup_Container_RequiresGitToken`
  - `TestHandleGitPushSetup_Container_StampsConfigured_OnProbeSuccess`
  - `TestHandleGitPushSetup_Container_StampsBroken_OnProbeFail`
  - `TestHandleGitPushSetup_Container_TokenNeverEchoedInResponseNorState` (sentinel scan)
  - `TestHandleGitPushSetup_Container_RestartsPushSourceRuntime`
  - Update: `TestHandleGitPushSetup_Confirm` (existing test pinning passive state — replace)
- Atom updates (in same commit):
  - `setup-git-push-container.md` — replace ".netrc wired by deploy" with truthful "git-push-setup verifies token + writes origin; deploy uses transient .netrc per push".
  - `setup-git-push-local.md` — drop "wired by deploy" claim where it exists.
  - `develop-strategy-awareness.md:22` — `configured` means "last setup probe succeeded: token works + URL valid + origin synced".
  - Tool description `workflow.go:38,277` — match new contract.
  - `develop-close-mode-git-push-needs-setup.md` — fix `PUSH_NOT_CONFIGURED` ghost code → `PREREQUISITE_MISSING`.

**Est. effort:** 4-6 days.

### Phase 2 — `git-push-setup` local path

**Goal:** Symmetric verifier for local mode.

**Scope:**
- `handleGitPushSetup` confirm mode (local) — same probe-first principle:
  1. Pre-check: meta + role.
  2. **Probe** locally with `GIT_TERMINAL_PROMPT=0 GIT_SSH_COMMAND='ssh -o BatchMode=yes'` against `remoteUrl` using user's local credential helper. On fail → structured error, NO state write.
  3. Probe passed → set/sync `origin` in `workingDir`'s `.git/config` + stamp `configured`.
  - NO `gitToken` collected (drop from `inputsRequired` when env detected as local).
- Tests + atom updates symmetric.

**Est. effort:** 1-2 days (after Phase 1 lays groundwork).

### Phase 3 — Launch + export gates use shared probe

**Goal:** Source-control gates use the same authenticated probe; close the same recipe-template loophole in export.

**Scope:**
- `validateLaunchSourceControl` (launch_source_control_gate.go) — replace unauthenticated `git ls-remote` with `ops.GitAuthProbe`. Container mode SSH-side; local mode local exec.
- `workflow_export.go` — add source-control gate symmetric to launch P-LP-10: `RemoteURL` cache requires `GitPushState=configured` proof, not auto-seed from live remote. Otherwise export-publish může propsat recipe-template URL into exported import.yaml (Codex round 2 finding).
- Fix atom `launch-source-control-required.md` Codex blind spots #1 + #2:
  - Row 4 (dirty-tree): recovery is `ssh {host} 'cd /var/www && git add -A && git commit -m "msg"'` precedes deploy; deploy refuses dirty trees.
  - Row 2 (remote-mismatch): after Phase 1, git-push-setup truly rewrites `.git/config` — claim becomes accurate.
  - Row 3 (delete misleading "stamps deploy timestamp" — Agent F finding).
- Tests:
  - `TestLaunchSourceControl_RemoteCheck_UsesAuthenticatedLsRemote` (private repo fixture)
  - `TestExportPublish_RefusesWhenGitPushNotConfigured`
  - Update existing `TestValidateLaunchSourceControl_*` to use authenticated probe stubs.

**Est. effort:** 2-3 days.

### Phase 4 — `build-integration` honest semantics (prose-only)

**Goal:** Make atoms truthful about what `BuildIntegration` flag means. **No enum changes, no new state machine, no auto-promotion.** Pure clarification.

**Scope (lean: just atoms + tool description + response precondition note):**
- `setup-build-integration-actions.md` — add `gh auth status` precondition section before `gh secret set` block.
- `develop-strategy-awareness.md` — clarify: `BuildIntegration=actions/webhook` means "agent picked this integration shape; ZCP emitted handoff (workflow YAML + secret commands or dashboard URL); ZCP cannot verify that the agent committed/pushed YAML, set secrets, or completed OAuth — those are outside ZCP's reach."
- `develop-close-mode-git-push.md` — surface honest expectation: "After picking actions/webhook, the integration only works if the agent completed the external handoff (commit + push workflow YAML, set GitHub secrets, complete dashboard OAuth)."
- `handleBuildIntegration` confirm-mode response — add `ghAuthPrecondition` field for actions path (informational note: "Verify `gh auth status` first; PAT in `gh` needs Secrets:Read+Write scope on target repo").

No handler logic change. No enum changes. No new sub-actions. Just prose-truth.

**Est. effort:** 0.5-1 day.

### Phase 5 — Deploy ergonomics

**Goal:** Make deploy refuse with structured Recovery for non-actionable conditions; surface the 3 specific eval friction symptoms.

**Scope:**
- `gitTokenCheckCmd` enhancement — read platform-side project env API AND container shell. Diagnose:
  - "GIT_TOKEN never set in project env" → recovery: `zerops_workflow action=git-push-setup` (which now writes it)
  - "Platform env exists but container shell stale" → recovery: restart runtime (`zerops_manage action=restart`)
- Kill silent hostname fallback v `setupName` — reuse `internal/tools/deploy_preflight.go::resolveSetupEntry` (explicit → role → hostname → refuse with `availableSetups`).
- Dirty-tree handling — already refuses; add structured Recovery payload pointing at exact `ssh {host} 'cd {workingDir} && git add -A && git commit -m "msg"'` command.
- On credential failure during git push (transport error category=credential), DEGRADE `meta.GitPushState=configured` → `broken` (Codex round 2 — token rotation handling).
- Tests:
  - `TestGitPush_GIT_TOKEN_Diagnosis_PlatformExistsShellStale`
  - `TestGitPush_SetupName_NoMatch_RefusesWithAvailableSetups`
  - `TestGitPush_DirtyTree_StructuredRecovery`
  - `TestGitPush_CredentialFailure_DegradesMetaToBroken`
- Atom updates:
  - `develop-close-mode-git-push.md:27` — drop "ensures fresh commit"; replace with "refuses dirty, requires commit-first".
  - `setup-git-push-container.md:51` — drop misleading "wired by deploy" prose where misleading; clarify per-push transient.

**Est. effort:** 2-3 days.

### Phase 6 — Topology + corpus cleanup

**Goal:** Delete dead enum branches; align dangling pieces from refactoring archeology.

**Scope:**
- Drop `GitPushUnknown` enum value (no real writer ever; collapses into `GitPushBroken` semantics post-Phase 1).
- Update `internal/topology/types.go:67-77` docstring (drop `.netrc` claim from `configured` definition; reference Phase 1 verifier contract).
- Spec `docs/spec-workflows.md:60` — replace `git-push-setup` description with truthful contract.
- Spec — add formal `P-PROD-1`, `P-PROD-2` invariant entries (Agent F finding — referenced 5+ places, defined nowhere).
- Rewrite `export-publish.md:42` — delete "deploy handles git init" lie; bootstrap's `InitServiceGit` already does this.
- Tool description `workflow.go:38,277`, `workflow_close_mode.go:152`, `gitPushSetupPointerInstructions` const — strip false claim "GIT_TOKEN/.netrc/remote URL setup" since handler now actually does GIT_TOKEN + remote URL but NOT .netrc.

**Est. effort:** 1 day.

### Phase 7 — Narrow sentinel tests (NOT generic atom-handler lint)

**Goal:** Specific assertion tests against the recurring lies; not a generic system (Codex round 2 said generic lint is overengineering).

**Scope:**
- `TestAtomCorpus_NoForbiddenClaims` — grep-scan for specific phrases that proved load-bearing wrong:
  - `persistent .netrc`
  - `deploy commits dirty work` / `deploy ensures fresh commit`
  - `BuildIntegration triggers every push` (if flag is non-handoff)
  - `git-push-setup wires GIT_TOKEN.*.netrc.*remote URL` (the central lie)
- Test refuses to pass if any atom contains these strings (positive-list of phrases known to mislead).
- `TestErrorMessageCorpus_NoForbiddenClaims` — same scan against handler error message constants.

**Est. effort:** 1 day.

---

## 4. Effort summary

| Phase | Scope | Effort |
|---|---|---|
| 1 | git-push-setup verifier (container) + ops/git_auth_probe + sensitive env setter | 4-6 days |
| 2 | git-push-setup verifier (local) | 1-2 days |
| 3 | Launch + export gates use shared probe + atom recovery fixes | 2-3 days |
| 4 | build-integration honest handoff + record-deploy promotion | 3-4 days |
| 5 | Deploy ergonomics (token diagnose, setup-name, dirty-tree, broken degradation) | 2-3 days |
| 6 | Topology cleanup + spec/atom dangling pieces | 1 day |
| 7 | Narrow sentinel tests | 1 day |
| **Total** | **Systemic fix, smaller-patch path** | **~14-22 days (2-3 weeks)** |

**Bigger patch path (deferred — backlog candidate):**
Codex round 2 navrhuje explicit desired/observed model:
```
GitPush: { remoteUrl, authMethod, state, verifiedAt, lastProbeError }
BuildTrigger: { provider, state, verifiedAt }
```
Cleaner than overloading enum values; ale větší migration přes atom axes, tests, gates. Hold pro budoucí iteraci pokud current cleanup nestačí.

---

## 5. Klíčové úpravy oproti pre-Codex-round-2 draftu

| Aspekt | Pre-round-2 draft | Final |
|---|---|---|
| `build-integration` | Two-step (handoff → `attest=true`) | Single-step honest handoff (`actions-handoff`); `record-deploy` promotion on observed build trigger. Codex: `attest=true` je další passive stamp. |
| Container `git@github.com:...` SSH remotes | Not mentioned | **HTTPS-only enforcement** v Phase 1; SSH deploy-key flow je separate concern (potential future phase). |
| Sensitive env write | Delegate to existing `ops.EnvSet` | **New sensitive setter or `EnvSetOptions{SensitiveKeys}`** — current `ops.EnvSet` echoes value, would leak GIT_TOKEN. |
| Token rotation | No mention | Phase 5: deploy credential failure → degrade `meta.GitPushState=configured` → `broken`. |
| Probe safety | Just timeouts | + `GIT_TERMINAL_PROMPT=0` + `GIT_SSH_COMMAND='ssh -o BatchMode=yes'` to prevent MCP hang. |
| Export gate | Mentioned as P-LP-3 follow-up | Phase 3 INCLUDES export source-control gate (Codex Q10 — same loophole). |
| Atom-handler lint | Generic system | **Narrow sentinel scan** against known-bad phrases (Codex Q4 — generic lint je overengineering). |
| Phasing | Atom updates as Phase 5 (separate) | **Vertical slices** — each phase ships handler + tests + atoms together. |

---

## 6. Out of scope (not addressed by this plan)

- **SSH deploy-key flow** (`git@...` remotes) — current verifier rejects SSH form; separate phase if/when needed.
- **GitPush vs BuildTrigger struct split** (Codex bigger-patch path) — desired/observed model; defer.
- **Auto-commit dirty trees in deploy** — Codex round 2 strongly NO; explicit refuse + structured Recovery.
- **`gh auth login` automation** — out of ZCP scope; precondition note only in response.
- **OAuth dashboard webhook readback** — Zerops platform doesn't expose this today; flag stays `webhook-handoff` until first observed build.

---

## 7. Why this is structural, not patches

Codex round 2 highlighted: "Phase 1/3/4 yes; Phase 2 two-step is overengineering". The plan converged on the **minimum architectural change** that makes flags truthful:

1. **One new ops primitive** (`git_auth_probe`) used by 3 callers (setup, launch gate, export gate) — eliminates 3 different implementations of "verify git remote reachable".
2. **One new ops capability** (sensitive env write) — prevents the obvious secret-leak path Codex flagged.
3. **One semantic shift** (handler verifies before stamping) — touches `git-push-setup` deeply, `build-integration` lightly (single-step handoff naming), launch + export gates symmetrically (use the new probe).
4. **Atom corpus + spec catch-up runs per-phase**, not as a separate cleanup phase — preventing the same drift that this plan is fixing.

No new tools. No new tool actions. No new ServiceMeta fields. No backwards-compat shims (per CLAUDE.md `Engineering Priority`).

---

## 8. References

### Source documents
- Live eval retrospectives: `eval/behavioral/runs/20260523-{155636,160357}/*/self-review.md`
- Spec: `docs/spec-workflows.md` (sections 4.3-4.8, §8 invariants, §10 launch)
- Architecture: `docs/spec-architecture.md`, `CLAUDE.md`

### Codex review files
- Round 1 (diagnosis opponent): `/tmp/codex-out-1779555355-88321-29566.md`
- Round 2 (solution review): `/tmp/codex-out-1779556024-91042-653.md`

### Recent refactoring history (Agent C archeology)
- `b76aa499 2026-04-21` collapse cicd + git-push into action=strategy (monolith creation)
- `096207ff 2026-04-28` (P5 of deploy-strategy-decomposition) — split monolith into 3 actions
- `ffac8ad1 2026-04-30` git-push removes auto-stamp, requires record-deploy
- `f36ad726 2026-04-29` build-integration handoff + git-push-setup call order
- `4960c7d7 2026-05-19` DeployIntent resolver
- `8c24b326 2026-05-20` P1 launch-production source-control gate

### Drift hot spots (Agent A/B/E/F/D synthesis)
- **GitPushState=configured lie**: 7 sites (5 atoms + tool description + chained pointer + deploy error const + spec line) all claim flag means GIT_TOKEN + .netrc + remote URL provisioned
- **PUSH_NOT_CONFIGURED ghost code** in `develop-close-mode-git-push-needs-setup.md:14` (actual: PREREQUISITE_MISSING)
- **`export-publish.md:42` "deploy handles git init"** — false; bootstrap's `InitServiceGit` does it
- **P-PROD-1/P-PROD-2** referenced everywhere, defined in NO spec doc
- **dirty-tree recovery** in launch-source-control-required.md row 4 says deploy commits — doesn't
- **remote-mismatch recovery** says git-push-setup rewrites .git/config — doesn't
- **launch ls-remote unauthenticated** — private repos false-fail
- **local-mode inputsRequired** still asks gitToken though atom says no
