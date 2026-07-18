# Plan — env-shadow / http-port-probe / utility-recipe retrieval (2026-05-27)

Source incident: live Claude Code session in a Zerops container (`.zcp/manual/mailpit.txt`)
— "add mailpit as a service + route emails to it." Three frictions surfaced.

**Status: verified + re-verified (adversarial).** Root causes confirmed by code reads + two
blast-radius sweeps + Codex (xhigh) + live eval-zcp proof. A red-team re-verify pass then found
each phase NEEDS-FIX before implementation; **those fixes are folded in below.** F2's fix changed
fundamentally and now carries one design decision for Karel.

## Unifying root cause
**ZCP stores multi-layer platform concepts but reports/acts from a flattened single-layer view.**
- Env: platform resolves env across scopes (service-scope bare key WINS over project); `zerops_env set`
  reports mutation success as runtime effectiveness and never consults the layering.
- Ports: HTTP-capability is dynamic per-port routing state; verify/subdomain treat `Ports[0]`/"all ports" as HTTP.
- Knowledge: the corpus HAS the fact (`recipe-mailpit` URL) but retrieval lacks intent disambiguation.

Targeted fix (reuse existing models), not a rewrite. Phase order **F1 → F2 → F3**. Each RED→GREEN,
all four test layers green before the next.

---

## Phase 1 — Cross-layer env shadow  *(REFRAMED / CONFIRMED · re-verify: NEEDS-FIX, folded in)*

### Root cause (evidence)
Precedence model exists but wired only to local-`.env`:
- `internal/ops/env_plan.go:271` precedence `project < yaml-setup < local-overlay`; `:447` marks
  `StatusShadowed` on key presence — **never compares values**; callable only from `BuildEnvPlan`
  (`env_generate.go:307` generate-dotenv, `workflow_checks_local_env.go:91`). `tools/env.go` +
  `deploy_preflight.go` have zero `EnvPlan`/`Conflict`/`Shadow` refs.
- `internal/ops/env_shadow.go:39` `DetectSelfShadows` only catches same-map `KEY: ${KEY}`.
- `internal/tools/deploy_preflight.go:201` `preflightEnvRefs` fetches every service's env, **discards
  values** (keys only).
- `internal/tools/env.go:224` reports `"env values are live"` unconditionally; `:250` already enumerates targets.

**Live proof (eval-zcp):** service-scope `KEY=A` + project-scope `KEY=B` → container bare `KEY=A`
(service wins); project value only as `PROJECT_KEY`/`<svc>_KEY`. Both `set` calls said "live", no warning.
`envIsolation` irrelevant (governs cross-service `${}` resolution, not bare-key precedence).

### Scope of THIS phase (explicit — uncovered surfaces deferred, not silently cut)
Covered: **project `zerops_env set` + deploy-preflight**. The `DetectLiteralShadows` primitive is
reusable; the following write surfaces are **deferred to a follow-up** (listed so the cut is explicit):
service-level set (`ops/env.go:90`), sensitive project env (`ops/env.go:161`, git-push token),
launch-production env (`launch_existing.go:406`), export/launch bundle composers
(`bundle/helpers.go:58`, `workflow_export.go:183`, `launch_bundle_compose.go:30`), recipe finalize
env (`recipe_templates_import.go:80` — Aleš's scope, mention only).

### Implementation
1. New ops primitive in `internal/ops/env_shadow.go`:
   ```go
   type LiteralShadow struct{ Key, LowerScope, LowerValue, HigherScope, HigherOwner, HigherValue string }
   // keys present in BOTH with DIFFERENT values (higher wins). Same-value collisions are NOT shadows.
   func DetectLiteralShadows(lower, higher map[string]string) []LiteralShadow
   ```
   Skip secret-typed keys when the value is masked/unreadable (avoid false/garbled compare) — note the
   limitation in the warning ("secret values not compared").
2. **env-set** (`tools/env.go::applyAutoRestart`, project sets): after store, fetch each restart-target's
   live service-scope env, `DetectLiteralShadows`, populate new optional `shadowWarnings []string`.
   Conditional success text: `Restarted X. Project envs live where not shadowed. Shadowed: KEY in appdev (service value wins).`
   Per-shadow text names both values + the two fixes (remove from yaml / set at service scope).
   **DROP the restart-skip optimization** — the live proof shows the project value stays reachable via
   `PROJECT_*`/`<svc>_*` aliases, so an app reading a prefixed alias still needs the restart. Skipping
   would starve it. (Re-verify caught this.) Restart all eligible targets as today; just add the warning.
3. **deploy-preflight** (`deploy_preflight.go::preflightEnvRefs`): retain project env values, compare
   yaml `run.envVariables` literals vs project envs, emit a `pass`-status StepCheck with warning detail
   (StepCheck = pass/fail/skip; warning rides pass). WARN never FAIL — per-setup mode flags
   (`NODE_ENV: production`) legitimately differ; accept the noise, phrase as "keep if intentional".

### Tests (RED first) — incl. pinned tests that MUST be updated
- New: `env_shadow_test.go` (`DetectLiteralShadows` table), `env_test.go` (shadowWarnings + conditional
  text), `deploy_preflight_test.go` (literal-shadow pass-row warning), `integration/` collision, **e2e
  `e2e/env_shadow_test.go` — runnable NOW (no build; verifier already exercised this path)**.
- **Update (pinned, plan-fidelity):** `annotations_test.go:152` (env tool description word-limit — keep
  new text within), `:195` (env keywords stay); `env_test.go:213` (process-poll pin). Atom edit (below)
  forces golden refresh: `internal/workflow/testdata/atom-goldens/develop/{first-deploy-dev-dynamic-container,
  failure-tier-3,first-deploy-recipe-implicit-standard,git-push-configured-webhook,git-push-unconfigured,
  mode-expansion-source,multi-service-scope-narrow,post-adopt-standard-unset,standard-auto-pair,
  steady-dev-auto-container}.md` + bootstrap/export goldens touched by env atoms.

### Checklist (P1)
- [ ] `DetectLiteralShadows` + secret-skip + unit tests
- [ ] env-set `shadowWarnings` + conditional success text (NO restart-skip)
- [ ] deploy-preflight `_env_literal_shadow` pass-row warning
- [ ] atom: teach LITERAL-value shadow (`MAIL_MAILER: log` beats project `smtp`), distinct from
      `key:${key}` self-shadow — extend `develop-platform-rules-common.md` / `develop-env-var-model.md`
- [ ] refresh listed atom goldens; update annotations/env pinned tests
- [ ] four test layers green

---

## Phase 2 — HTTP-port-blind verify / subdomain probe  *(CONFIRMED · re-verify: fix REDESIGNED — see decision)*

### Root cause (evidence)
`HTTPSupport` consulted in zero probe paths; sites use `Ports[0]` or all ports:
`verify_checks.go:193/203/206` (`ResolveSubdomainURL`→`Ports[0]`), `subdomain.go:232/244` (all ports),
`tools/subdomain.go:92` + `deploy_subdomain.go:133` (`WaitHTTPReady` all URLs), `deploy_subdomain.go:94`
(`DeployResult.SubdomainURL = SubdomainUrls[0]`), `eval/probe.go:102` (candidate = subdomain+ports>0).

**Proof:** deterministic test on the real `ResolveSubdomainURL`: `[1025,8025]` → 1025 URL (bug).

### ⚠ Re-verify correction (load-bearing)
`Port.HTTPSupport` is **NOT** zerops.yaml intent. `zerops_mappers.go:208` maps it from SDK
`HttpRouting`; `deploy_subdomain.go:50-61` documents that it **flips true only AFTER a successful
EnableSubdomainAccess** (the SDK `ServicePort` has no `httpSupport` field at all). The
`types.go:90` comment ("from zerops.yaml httpSupport") is **STALE — fix it as part of this phase.**
Consequences: (1) auto-enable eligibility MUST stay mode-allowlist + `IsSystem` only
(`deploy_subdomain.go:165-207`) — **this phase does NOT touch it**; (2) a naive "filter ports by
`HTTPSupport==true`" would yield EMPTY URLs pre-propagation (first deploy) → empty
`DeployResult.SubdomainURL` + skipped readiness → regression. The mock doesn't set `HTTPSupport`.

### Implementation (redesigned to be timing-robust — never empty, never skip)
1. Fix the stale `types.go:90` comment (HTTPSupport = post-enable L7 routing flag).
2. New ops helper `HTTPServingPort(ports) (Port, bool)` — returns the `HTTPSupport==true` port if any.
3. **Probe selection with cross-port fallback** (the robust fix, independent of HTTPSupport timing):
   - `ResolveSubdomainURL` / verify `http_root`: probe the preferred port = HTTPSupport port if present,
     else `Ports[0]`. **If that probe fails AND other ports exist, probe the rest; pass if ANY answers
     HTTP**, report the responding port. This fixes mailpit REGARDLESS of whether HTTPSupport propagated
     (even if it picks 1025 first, it then tries 8025 → 200). **Do NOT add a "skip when no HTTPSupport
     port" branch** — the signal is unreliable; rely on the probe result.
   - `DeployResult.SubdomainURL` / readiness wait / `SubdomainUrls`: keep `subdomainUrls` (backcompat);
     prefer the HTTPSupport URL else first; **never empty**. Readiness-wait warns only if NO url becomes
     ready (a single non-HTTP port's 502 must not warn).
4. Leave `env_generate.go:436` AS-IS (it's a managed-service **TCP** probe — an HTTP-port helper is
   conceptually wrong there; re-verify flagged this). Drop it from F2 scope.

### DECISION for Karel (F2 fix shape)
- **(A) recommended:** preferred-port + cross-port probe-fallback (above). Robust to HTTPSupport timing,
  no regression, fixes mailpit deterministically. Cost: up to N probes for multi-port services (N small).
- **(B)** HTTPSupport-preference only, fallback `Ports[0]`, no extra probing. Simpler, but only fixes
  mailpit IF HTTPSupport is propagated at verify time — **unconfirmable until eval-zcp build recovers**.
Both never touch auto-enable eligibility. I recommend (A).

### Tests — incl. pinned updates
- New: `verify_checks_test.go` (mailpit shape → probes/answers 8025, cross-port fallback),
  `verify_test.go`, `subdomain_test.go`/`subdomain_contract_test.go` (+`httpSubdomainUrls` if added),
  `deploy_subdomain_test.go`. E2E **DEFERRED** until eval-zcp build dispatch recovers.
- **Update (pinned, plan-fidelity):** `verify_checks_test.go:13/36/60`, `verify_test.go:153/266/308/387/412`,
  `verify_recovery_test.go:110`, `deploy_subdomain_test.go:38-42/217/244/549/569/601/627`,
  `subdomain_test.go:32/76/401/454`, `ops/subdomain_test.go:254/276/360` (these pin Ports[0]/HTTPSupport=false
  behavior). `deploy_ssh_test.go:100` already sets `HTTPSupport:true` → model fixture. Also
  `eval/probe.go:102` candidate selection.

### Checklist (P2)
- [ ] Karel picks (A)/(B)
- [ ] fix stale `types.go:90` comment
- [ ] `HTTPServingPort` + cross-port probe-fallback in verify/`ResolveSubdomainURL`
- [ ] `DeployResult.SubdomainURL` + readiness never-empty / no spurious non-HTTP warning
- [ ] auto-enable eligibility UNTOUCHED (assert via existing predicate tests)
- [ ] update all listed pinned tests; e2e deferred (note in commit)

---

## Phase 3 — utility-recipe retrieval gap  *(CONFIRMED / REFRAMED · re-verify: NEEDS-FIX, folded in)*

### Root cause (evidence)
`engine.go:60-87` `queryAliases` has no mailpit/mailhog; `:102-184` substring+title scoring → outbound
`SMTP on Zerops` wins. Recipe URL only in `production-checklist.md:31` body + `core.md:29`. **Live: 3
query variants never surface the recipe.** Guides are gitignored/upstream-synced (`.gitignore:45`, `CLAUDE.md:70`).

### Scope (explicit)
Fixes **`query=` retrieval only.** Does NOT make `recipe="mailpit"` work (`briefing.go:114`) nor add a
bootstrap recipe route — those are deferred (and bootstrap recipe matching is Aleš's scope; mention only).

### URL resolution (re-verify caught a conflict — RESOLVED)
Two real public repos exist: knowledge corpus uses **`github.com/zeropsio/recipe-mailpit`** (has
`zerops-service-import.yml`, alpine@3.20, buildFromGit); recipe-generation code uses
`github.com/zerops-recipe-apps/mailpit-app` (`recipe_service_types.go:127` — **Aleš's scope, do not
touch**). F3 uses the **corpus-consistent `zeropsio/recipe-mailpit`**. Flag the duplication to Karel/Aleš;
do not unify here.

### Implementation
1. New guide `internal/knowledge/guides/utility-recipes.md` (keywords Mailpit / dev mail catcher / SMTP
   capture / utility recipe + the `zeropsio/recipe-mailpit` buildFromGit URL). No invented Adminer URL.
2. SMTP cross-link in `guides/smtp.md`: "outbound only; for dev capture use the Mailpit utility recipe …".
3. `engine.go` aliases: `mailpit` / `mailhog` → expand to "dev mail catcher smtp capture utility recipe".
   (1+2 ship via `zcp sync push guides` → upstream PR; 3 is an in-repo commit.)

### Tests
- `engine_search_test.go`: `mailpit`/`mailhog`/`mail catcher` → utility guide outranks `guides/smtp`,
  result includes `recipe-mailpit`. Doc-inventory tests if they pin URIs. **Do NOT** alter recipe-gen
  URL behavior (`recipe_templates_test.go:616-629`).

### Checklist (P3)
- [ ] `utility-recipes.md` (upstream sync) + smtp cross-link (upstream sync)
- [ ] engine.go mailpit/mailhog aliases (in-repo) + search test
- [ ] flag recipe= / bootstrap-route gaps + URL duplication to Karel/Aleš

---

## Effort
F1 ~250-450 LOC / 1.5-2.5d · F2 ~180-320 LOC / 1-2d · F3 ~80-180 LOC-content / 0.5-1d. + pinned-test/golden updates.

## Open questions / caveats
1. **eval-zcp build pipeline is down for ALL builds** (`buildFromGit` + inline fail at `stack.build`, 0s,
   pipeline never starts). Blocks F2 live e2e + any build/deploy on eval-zcp until it recovers. Flag to operator.
2. **F2 efficacy on the exact mailpit timing** can't be live-confirmed until the build recovers; fix (A)
   is designed to be robust to that uncertainty (cross-port probe), so it's safe to build now.
3. F2 trigger frequency depends on API `svc.Ports` ordering (preserve-yaml vs sort-ascending) — confirm live later.
4. Two mailpit recipe repos exist (duplication) — Karel/Aleš to decide canonical.

## Engineering-priority notes
Backward compat: `subdomainUrls` retained; `shadowWarnings` additive; all WARN-level (no new hard FAILs).
Architecture: helpers in `internal/ops/` (layer 3); `Port.HTTPSupport` in `platform` (layer 1); no depguard
violation; no global state. Plan-fidelity: every behavior change enumerates the pinned tests it breaks (above).
