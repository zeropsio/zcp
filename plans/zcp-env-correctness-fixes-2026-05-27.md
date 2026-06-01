# Plan — ZCP env-correctness fixes vs Zerops env ground-truth (2026-05-27)

Audit of ZCP against `docs/spec-zerops-env-lifecycle.md` (live-verified ground truth)
+ `.claude/agent-memory/platform-verifier/verified-facts.md`. Every defect carries
`file:line`, the ground-truth line it violates, and an evidence tag
(`[code]` = confirmed by reading the code; `[live]` = live-verified in the ground
truth; `[derived]` = logical consequence of two verified facts, not independently
reproduced).

Method: 5 parallel agents (atoms/guides/themes sweep, env-code deep read,
verify/subdomain confirm, retrieval+guide audit, other-wrongness sweep) + manual
code reads of every load-bearing site + this author's cross-check against the
ground truth. F1/F2/F3 folded from `plans/env-shadow-httpport-utility-recipe-2026-05-27.md`
(already Codex-re-verified — referenced, not re-derived).

---

## 0. The mission's #1 prior is NOT borne out — correction up front

The brief flagged the **env-isolation model error** ("ZCP teaches cross-service
vars auto-inject in every mode") as *likely the biggest blast radius*. **Verified
against the actual current corpus: this falsehood is not present.** Every suspect
file was read:

| Surface | What it actually says | Verdict |
|---|---|---|
| `guides/environment-variables.md:71-74` | `service` (default) → "Must use explicit `${hostname_varname}` references"; `none` (legacy) → "auto-shared" | **mode-aware + correct** `[code]` |
| `themes/core.md:147-148` | project = "Auto-inherited by every service"; cross-service refs = "the ONLY place cross-service refs work" (`run.envVariables`) | **mode-correct** `[code]` |
| atoms `develop-env-var-model.md:18-25`, `develop-platform-rules-common.md:20-23` | teach the **explicit `${host_var}` rename** mechanism | **correct in all modes** (explicit refs resolve regardless of isolation, ground-truth §3) `[code]` |
| `ops/env_shadow.go:15-22` (comment) | "CROSS-SERVICE vars do not auto-inject under the porter-default isolation" | **isolation-aware + correct** `[code]` |

- **The primary develop env surfaces are mode-correct.** The guide isolation table,
  `themes/core.md:147-148`, and the develop-* atoms teach explicit `${host_var}` refs
  (correct in all modes). **Decisive counter-evidence the falsehood isn't the default
  teaching:** `themes/services.md:16` — "Managed services auto-generate credentials
  but they are **NOT** automatically available to runtime services. Wire them via
  `run.envVariables`." `[code]`
- **But "NO file teaches it" was too absolute (Codex red-team caught my grep hole —
  I excluded lines containing "project", which dropped "project-wide").** The
  falsehood survives in two narrow edges:
  - `themes/refinement-references/ig_one_mechanism.md:162,178` — "Zerops injects
    cross-service refs **project-wide** as `${db_hostname}`…" and "A porter can read
    `${db_hostname}` directly in code and the app works fine" (false under default
    `service`). **This is recipe-AUTHORING guidance (the refinement sub-agent) →
    Aleš's recipe-generation scope → FLAG, do not edit.** `[code]`
  - `guides/networking.md:39` — "Cross-service env vars: prefix with hostname — e.g.
    `app_API_TOKEN`." Presents the bare `<host>_KEY` form without an isolation
    caveat (mild; user-facing). → narrow one-line caveat in Phase F. `[code]`
- **Zero env code branches on `envIsolation`** — every Go occurrence treats it as a
  SYSTEM key to *filter out* (`env_generate.go:85`, `classify_envs.go:42`,
  `types.go:177`). **Benign**: explicit `${host_var}` refs resolve regardless of
  isolation (ground-truth §3 `[live]`), so ZCP never needs to predict auto-injection.

**Net:** the mission's framing ("biggest blast radius across atoms + guidance + code,
its own phase") is **not** warranted — the falsehood is absent from the primary
develop atoms/guide/code; it survives only in a recipe-authoring doc (Aleš) and one
mild networking-guide line (a one-line caveat, folded into Phase F). The real
high-leverage defects are the **slim-API flattening** family below.

---

## 1. Unifying root cause (carried from F1/F2/F3, re-confirmed)

**ZCP reasons about a multi-layer platform env from a flattened single-layer view
derived from the slim service-env API.** `GET service-stack/{id}/env` returns ~11
of ~511 container keys (~2%) — intrinsic READ_ONLY + user-set userData only; it
**omits yaml-baked `run.envVariables`, project vars, cross-service `<host>_`
aliases, and the ZEROPS_* catalog** (ground-truth §6 `[live]`; `FetchServiceEnv` →
`GetServiceEnv` → `GetServiceStackEnv`, the slim endpoint — `platform/zerops_env.go:16-18`
`[code]`). Three families of bug fall out:

- **Env**: the slim list is treated as the universe of "known vars" (→ A1 deploy
  false-fail **and** A2 generate-dotenv hard-error), as runtime effectiveness (→ F1
  shadow / "live" overclaim), and as the complete env to PUT (→ H1 service-set
  data-loss). Tool/guide text presents it as complete (→ F-tool-desc).
- **Ports**: HTTP-capability is per-port post-enable routing state, not yaml intent;
  probe/URL sites use `Ports[0]`/all-ports (→ F2).
- **Knowledge**: the corpus *has* the mailpit recipe URL but retrieval can't
  disambiguate "dev mail catcher" intent (→ F3).

---

## 2. Confirmed defects (enumerated)

| ID | Location | Ground-truth violated | Evidence | Severity |
|---|---|---|---|---|
| **A1** | `ops/deploy_validate.go:411-436` (`ValidateEnvReferences`) + `tools/deploy_preflight.go:215-235` (`preflightEnvRefs`, `statusFail`) wired at `:166-169` → `checksAllPassed` `:171`; **blocks every deploy path**: `deploy_ssh.go:177-195`, `deploy_local.go:117-135`, `deploy_batch.go:99-119`, `deploy_git_push.go:333-343` | §6: slim API misses yaml-baked/project/system vars → a valid `${sibling_VAR}` ref to a sibling's **yaml-baked** var is flagged "unknown variable" and **FAILs preflight, blocking the deploy** | `[code]` wiring (Codex-confirmed all paths) + `[live]` PENDING-4 → `[derived]` false-fail | **High** (false deploy-blocker) |
| **A2** | `ops/env_generate.go:204-220` (`refExpander` resolves refs via slim `GetServiceEnv`) → unresolved count → `EnvGenerateDotenv:308-320` hard-errors "could not resolve env vars" | §6 + §3: same slim-blindness → a valid `${sibling_yamlbaked_VAR}` is unresolvable → **blocks local `.env` generation** (ground-truth says unresolved refs should stay literal, not error) | `[code]` (Codex-found; I confirmed the hard-error path) | **High** (false local-`.env` block) |
| **F1** | `tools/env.go:319` ("env values are live"); no cross-layer literal-shadow detector (`ops/env_shadow.go` only same-key self-shadow) | §2 precedence + §6: a yaml-baked literal silently shadows a project var; "live" ignores precedence | `[code]` + `[live]` (mailpit `MAIL_MAILER: log`; DUP precedence) | **High** (silent wrong value) |
| **H1** | `ops/env.go:99-100` (`EnvSet` service path: `buildEnvFileContent(pairs)` → `SetServiceEnvFile`); doc-comment `:42` "a single PUT **replaces the entire env file**" | service env-file PUT is a full **replace**: `zerops_env set scope=service KEY=v` sends only the new pair → **silently deletes all other user-set service vars**. The `:39` "upsert semantics" comment is false vs `:42`/the code | **`[LIVE-CONFIRMED 2026-05-28]`** on eval-zcp probe `envprobe`: set `ALPHA` → set `BETA` (separate call) → `ALPHA` GONE, only `BETA` remained (READ_ONLY intrinsics survived). Client-side single-pair send confirmed by code (single caller, no pre-merge). | **High** (silent data-loss — PROVEN) |
| **C-dup** | `ops/env.go:90-109` (`EnvSet` service PUTs blindly); no `userDataDuplicateKey` in `platform/errors.go` | §2 `[live]`: a service env on a key owned by yaml `run.envVariables` is rejected `userDataDuplicateKey` 400 — raw error surfaced, no actionable recovery | `[code]` (Codex-confirmed no handling) + `[live]` | **Medium** |
| **F2** | `ops/verify_checks.go:193,203,206` (`Ports[0]`); `ops/subdomain.go:231-235,244-250` (all ports); `tools/subdomain.go:93-98`; `tools/deploy_subdomain.go:93-95,135-140`; `eval/probe.go:102`; stale comment `platform/types.go:90` | §F2: `HTTPSupport` is post-enable L7 state (mapped from SDK `HttpRouting`, `zerops_mappers.go:202,208`), not yaml intent; probing `Ports[0]` mis-resolves multi-port services (mailpit SMTP 1025 + HTTP UI 8025) | `[code]` all 8 sites + `[code]` deterministic test in F2 plan | **High** (verify fail / wrong URL) |
| **F3** | `knowledge/engine.go:61-87` (`queryAliases`, no mail*); scoring `:128-135`; recipe URL only in body text (`production-checklist.md:37`, `core.md:29`) | retrieval intent gap: "mailhog" → empty result set; "mail catcher" → `smtp.md` (outbound, the antonym) wins; `recipe-mailpit` URL unfindable | `[code]` (`grep` confirms no doc contains "mailhog"; aliases verbatim) | **Medium** (discoverability) |
| **E1** | `guides/environment-variables.md:3,106,163` | §7: secret API read is **privilege-gated** (admin verbatim / read-only `REDACTED`), not absolute "write-only / cannot be read back via API" | `[code]` (verbatim) + `[live]` PENDING-3 | **Medium** (false agent belief) |
| **E2** | `guides/environment-variables.md:63-67` | §6: API-visibility gap narrowed to "cross-service refs show as templates" — conceals that the endpoint returns ~2% of env (yaml-baked + project + cross-service all invisible) | `[code]` + `[live]` | **Medium** |
| **E3** | `guides/environment-variables.md:3,11-12,150,152,161` + `themes/core.md:154` | §5: env value lands via the env daemon in-place ~5-10s **without restart**; a *running* process keeps its boot env until it restarts/respawns. Prose asserts "changes require restart" as a flat platform fact | `[code]` + `[live]` E1 | **Low** (imprecise; actionable advice — "restart to make the running app see it" — is still correct) |
| **E4** | `docs/spec-env-handling.md §4, §12.1` | §11: §4 rationale ("API can't distinguish user/system") is too narrow (real cause = incompleteness); §4 precedence omits system + service-userData layers and conflates local-`.env` order with container-runtime order; §12.1 container-review can't use service-env API alone | `[code]` + ground-truth §11 | **Low** (doc) |
| **E5** | `tools/discover.go:44-45` ("Sufficient for bootstrap, deploy, recipe validation"); `tools/env.go:48` (service get "return env var keys and values" — no slim caveat) | §6: tool descriptions present the slim service-env read as complete/authoritative; it returns ~2% of container env | `[code]` (Codex-found) | **Low** (agent-facing overclaim) |
| **E6** | `ops/manage.go:66` ("Reload… sufficient for env var changes"); `tools/manage.go:29` ("Use reload after env var changes") | §5: a running PID1 keeps its boot env; reload (~4s, lighter than restart) likely does **not** re-exec the app → it won't observe new env. "reload sufficient" steers the agent to the wrong op | `[code]` (Codex-found) → **`[needs-live/docs]`** confirm what reload does to the app process | **Medium** (wrong-action tool text) |
| **G1a** | `ops/env.go:147-184` (`EnvSetSensitiveProject`, GIT_TOKEN via `workflow_git_push_setup.go:400`) | §7: **project-level `sensitive=true` does not persist** → token is a plain project USER var: a **read-only project token reads it verbatim** (a true service-secret would be `REDACTED`), and it's returned by `zerops_discover`/`zerops_env get` under the opt-in `includeEnvValues` like any project value (`discover.go:323-330`, `env.go:187-209`) | `[code]` (Codex-sharpened) + `[live]` | **Decision** (was flag-only; Codex argues promote — see Phase G) |
| **G1b** | `ops/env_generate.go:83-92` (`platformInternalKeys`) | denylist filters `ZCP_API_KEY` (stated reason: "`git add -A` would otherwise publish the deploy token") but **omits `GIT_TOKEN`** — a repo-write PAT, deploy-only, never read by app code (the denylist's exact criterion) → it renders into local `.env` | `[code]` | **Low (do-now)** — 1-line parity fix |

**Not a defect (verified clean):** `Sensitive`-as-secret conflation (`envclass/classify.go:16-17`
explicitly documents Sensitive is "supplementary, never authoritative"); import-time
`services[].envVariables` dropped (`ops/import.go` already warns); slim-API
completeness in *discover* (`discover.go:179` reads the platform-injected
`zeropsSubdomain` env, the correct source — see F2 note).

---

## Phase A — Env-ref handling vs the slim API: don't block valid refs  *(NEW — highest leverage)*

Two tools share one root cause (slim API can't confirm a runtime sibling's yaml-baked
var) and one fix pattern (managed target → strict; runtime target → lenient): **A1**
deploy-preflight (FAIL → blocks deploy) and **A2** generate-dotenv (hard-error →
blocks local `.env`).

### A1 root cause
`preflightEnvRefs` (`deploy_preflight.go:202-240`) builds the known-var universe per
hostname from `ops.FetchServiceEnv` (slim `/env`, keys-only — `:215-223`), then
`ValidateEnvReferences` flags any cross-service `${host_var}` whose `var` isn't in
that list (`deploy_validate.go:426-432`) as **`statusFail`** (`:233`). The slim list
is ~2% of the target's env — it **cannot see** the target's yaml-baked
`run.envVariables`, project vars, or system vars (§6, §3 `[live]`). So a *valid*
ref to a sibling's yaml-baked var (e.g. service B's `UPSTREAM: ${a_API_URL}` where
A bakes `API_URL` in `run.envVariables`) is reported "unknown variable" and **blocks
the deploy** (`:166-169` → `checksAllPassed:171` → `Passed:false` "fix issues before
deploying"). Explicit refs resolve regardless of isolation (§3 `[live]`), so the ref
*works at runtime* — the validator is simply blind to the proof.

### Fix — condition FAIL/WARN on whether the target's slim env is authoritative
**Not a blanket WARN.** The slim env's completeness depends on the *target* service:
- **Managed dependency target (postgres, valkey, …):** no `zerops.yaml`, no
  yaml-baked `run.envVariables` → its slim `/env` is **complete** for the vars users
  ref (`connectionString`, `hostname`, `port`, `user`, `password`, `dbName` — all
  intrinsic/managed userData, all in slim). A missing ref here is a **true typo**
  (the corpus relies on this: `themes/services.md:76` "Do NOT reference
  `${cache_password}` — they don't exist"). → **keep `statusFail`.**
- **Runtime app target (nodejs, php, …):** has yaml-baked `run.envVariables` that the
  slim `/env` **cannot see** (§6 `[live]`) → a "missing" ref may be a valid reference
  to the sibling's yaml-baked var. → **`statusPass` + warning, never block.**

Implementation:
1. `preflightEnvRefs` (`deploy_preflight.go:202-240`): partition `discoveredEnvVars`
   targets by managed-vs-runtime (topology `RuntimeClass` / `ServiceStackTypeCategoryName`
   — confirm exact predicate at impl). For runtime targets, downgrade an unconfirmed
   ref from `statusFail` to `statusPass` + warning detail; for managed targets keep
   `statusFail`.
2. Reword the runtime-target message from "unknown variable" to "could not confirm
   `%q` on `%q` — its yaml-baked vars are invisible to the env API; verify the ref is
   intentional" (honest: invisible, not unknown). Managed-target FAIL keeps "unknown
   variable" (it genuinely is).
3. Leave the matching/skip logic (`deploy_validate.go:421-424` lone-ref skip;
   classifier) **unchanged** — correct.

*Simpler fallback (A):* blanket WARN for all targets. Rejected as default — it
discards the high-value managed-var typo detection the corpus depends on. *Gold-plated
alternative (C):* widen the runtime target's known-universe by fetching its deployed
appVersion `run.envVariables` keys → FAIL stays accurate everywhere. Rejected: extra
API calls per sibling for a less-common ref shape; the conditional (B) gets the
correctness without the cost.

### Blast radius (keep the primitive pure; partition in the caller)
- Keep `ValidateEnvReferences` (`deploy_validate.go:411-436`) **unchanged** — it stays
  a pure "which refs aren't in the supplied known-set" function. The FAIL/WARN +
  target-type decision moves to the caller. (This keeps `deploy_validate_test.go:645-700`
  green — fewer pinned tests touched.)
- `tools/deploy_preflight.go:202-240` — the only `statusFail` env-ref emitter:
  partition each `EnvRefError` by its `host`'s managed-vs-runtime class, map managed→FAIL
  (keep message), runtime→PASS+warning (reworded).
- `ops/checks/env_refs.go::CheckEnvRefs` — **second FAIL site** (`:59` `StatusFail`,
  confirmed); same conditional contract (skips when `liveHostnames` empty — shim mode).
- The FAIL blocks **every deploy path** (Codex-confirmed): `deploy_ssh.go:177-195`,
  `deploy_local.go:117-135`, `deploy_batch.go:99-119`, `deploy_git_push.go:333-343`.
- The conditional (B) **preserves the managed-typo true-positive** (`${db_pasword}`
  stays FAIL) — directly answering the "don't throw away typo detection" concern.

### A2 — generate-dotenv hard-errors on the same valid ref
`refExpander.expandRefs` (`env_generate.go:204-220`) resolves `${host_var}` by fetching
the host's slim env; an unresolved ref is kept literal + counted (`:214-219`), then
`EnvGenerateDotenv` (`:308-320`) converts a nonzero unresolved count into a hard
`ErrInvalidParameter` "could not resolve env vars" — **blocking the local `.env`**.
A `${sibling_yamlbaked_VAR}` is unresolvable (slim-blind) → false block. Ground-truth
§3 says unresolved refs should **stay literal**, not error.
**Fix (same pattern):** for a *runtime*-target ref that can't resolve, keep it literal
+ warn (don't count toward the hard error); reserve the hard error for transient
(VPN/API-down — `RefResolveTransientError`, `:209`) and managed-target misses (real
typo). Touches `env_generate.go` + `env_plan.go` unresolved-count aggregation.

### Tests (RED→GREEN, four layers)
- **No change to `deploy_validate_test.go`** (primitive unchanged — verify after).
- **New:** `deploy_preflight_test.go` — (i) ref to a *managed* target's nonexistent
  var → `statusFail` (typo still caught); (ii) ref to a *runtime* sibling whose slim
  env lacks the (yaml-baked) key → `statusPass` + warning, **not** `statusFail`;
  integration row; e2e optional (cheap on eval-zcp: a runtime svc with a yaml-baked
  var + a sibling that refs it → preflight must not block).
- `ops/checks/env_refs_test.go` — update only if `CheckEnvRefs` status mapping changes.
- **A2:** `env_generate_test.go` — runtime-sibling yaml-baked ref → `.env` written with
  the ref kept literal + warning (no hard error); transient (API-down) still errors;
  managed-target miss still errors. (Pin against `TestEnvGenerateDotenv_*`.)

### Checklist (A)
- [ ] confirm managed-vs-runtime predicate (topology `RuntimeClass` / category)
- [ ] A1: `preflightEnvRefs` + `CheckEnvRefs` per-error FAIL(managed)/WARN(runtime) + reworded msg
- [ ] A2: `EnvGenerateDotenv` keeps runtime-sibling unresolved refs literal+warn (not hard-error)
- [ ] `ValidateEnvReferences` + `deploy_validate_test.go` untouched & still green
- [ ] new preflight + generate-dotenv tests; four layers green

---

## Phase B — `zerops_env set` correctness: shadow + service-merge + duplicate-key

Three defects, all in the `zerops_env set` / `ops.EnvSet` mutation path.

### B1 — F1 cross-layer shadow + "live" overclaim  *(folds F1 §Phase 1 verbatim)*
New `DetectLiteralShadows` primitive in `env_shadow.go`; `tools/env.go`
`shadowWarnings` + conditional success text (NO restart-skip optimization — the
project value stays reachable via `PROJECT_*` aliases, so the restart is still
needed); deploy-preflight literal-shadow `pass`-row warning (WARN never FAIL); atom
edit teaching literal-value shadow distinct from `key:${key}` self-shadow; the listed
golden refreshes + pinned-test updates (`annotations_test.go:152,195`, `env_test.go:213`).

### B2 — service `EnvSet` data-loss: replace → merge  *(H1, NEW — most severe; LIVE-PROVEN)*
`EnvSet` service path (`env.go:99-100`) does `buildEnvFileContent(pairs)` →
`SetServiceEnvFile`, a **full env-file replace** (doc-comment `:42`). So
`zerops_env set scope=service KEY=v` sends only the new pair and **silently deletes
every other user-set service var**. The `:39` "upsert semantics" comment is false.
- **PROVEN live (2026-05-28, eval-zcp probe `envprobe`):** set `ALPHA` → set `BETA`
  (separate call) → `ALPHA` GONE, only `BETA` survived (intrinsic READ_ONLY vars kept).
- **Codex-confirmed** single caller (MCP tool, `tools/env.go:211`), reachable with a
  partial set (`project` defaults false), no recipe/import/launch caller; no merge at
  any layer.
- **⚠ The mock HIDES this** (`mock_methods.go:203` discards the env-file content), so
  `integration/multi_tool_test.go` *looks* like it proves merge but doesn't — the mock
  never applies the replace. Tests give false safety.

Fix:
1. `EnvSet` (service) reads current service userData env, **merges** the new pairs over
   it, PUTs the union. Fix the `:39`/`:42` contradictory comments.
2. **Make the mock faithful first** — `mock_methods.go` `SetServiceEnvFile` must model
   replace (store exactly the supplied file). Otherwise the merge test is vacuous. This
   is the real RED step (live behavior already proven).
3. Interaction with B3: the merge must skip yaml-owned keys (they'd 400) — see below.
4. ⚠ **Merge reads via the slim `/env`** — which omits yaml-baked/cross-service/project
   (§6). That's fine here: we only want to preserve *user-set service* vars (exactly
   what slim returns), and yaml-baked keys must be skipped anyway (B3). Note it so the
   merge isn't mistaken for "preserve everything".

### B3 — `userDataDuplicateKey` actionable recovery  *(C-dup, NEW)*
Ground-truth §2 `[live]`: a service env on a key owned by yaml `run.envVariables` is
rejected `userDataDuplicateKey` 400 — service-scope override is *structurally
impossible*. `ops.EnvSet` surfaces the raw error (no handling `[code]`).
1. New error code in `platform/errors.go`. **Platform preserves the raw API code**
   (`PlatformError.APICode`); **`ops.EnvSet` owns the recovery language** (Codex
   layering note — ops has the context to name the yaml fix, platform must not).
2. Translate to the single-path F1 recovery: "`KEY` is owned by `<svc>`'s
   `zerops.yaml run.envVariables` — edit the yaml + redeploy; service-scope override
   is rejected by the platform."

Tests: F1 set (`env_shadow_test.go`, `env_test.go`, `deploy_preflight_test.go`,
e2e `e2e/env_shadow_test.go`); B2 service-merge (`env_test.go` + e2e two-set survival);
B3 `errors_test.go` + `env_test.go` dup-key translation row.

### Why "live" is the overclaim, not the restart
The ZCP auto-restart (`env.go:300`) is **correct** — a running PID1 keeps its boot
env (§5 `[live]`), so restart is how the running app picks up a project-env change.
(Agent-sweep claim "restart is unnecessary overhead" is **wrong** and rejected.) The
overclaim is that "env values are live" ignores **precedence shadowing**: if a
higher layer (yaml-baked / service userData) owns the key, the restarted app still
reads the *shadowing* value, not the one just set. F1's `shadowWarnings` is the fix.

---

## Phase C — HTTP-port-blind verify / subdomain  *(folds F2; option A)*

Take F2 from the F1/F2/F3 plan **option (A): preferred-port + cross-port
probe-fallback** (recommended there; robust to first-deploy `HTTPSupport` timing):
fix the stale `types.go:90` comment; add `HTTPServingPort(ports)`; `ResolveSubdomainURL`
/ verify `http_root` probe the HTTPSupport port if present else `Ports[0]`, and **on
failure probe the remaining ports, pass if ANY answers HTTP**; `DeployResult.SubdomainURL`
/ readiness-wait prefer HTTPSupport URL else first, **never empty**, warn only if NO
url becomes ready; auto-enable eligibility **UNTOUCHED** (mode-allowlist + `IsSystem`
only — assert via existing predicate tests); leave `env_generate.go:436` (managed-svc
TCP probe) out of scope.

### Audit additions vs the F2 plan
- **Corroboration:** `discover.go:179` already resolves its single `SubdomainURL`
  from the platform-injected `zeropsSubdomain` env var (`ExtractSubdomainURL`) — the
  *correct* source, which is why discover alone avoids the `Ports[0]` trap `[code]`.
  Cross-port probe (A) is still preferred for verify/deploy because `zeropsSubdomain`
  isn't resolved pre-enable/pre-propagation (first deploy); note the asymmetry in
  the fix comment so the two paths are understood as deliberate.
- **2 latent same-family sites** (flag, fix opportunistically): `env_generate.go:436-437`
  (managed-service `Ports[0]` reachability probe — wrong port for any >1-port managed
  svc); `discover.go:255` (surfaces per-port `httpSupport`, which is post-enable state
  — an agent reading discover pre-propagation sees `httpSupport:false` on a genuine
  HTTP port; reporting-fidelity caveat, not the resolution bug).

### Tests / pinned
Per F2 plan's enumerated pinned updates (`verify_checks_test.go`, `verify_test.go`,
`verify_recovery_test.go`, `deploy_subdomain_test.go`, `subdomain_test.go`,
`ops/subdomain_test.go`, `eval/probe.go` candidate). E2E deferred per F2 plan.

---

## Phase D — Utility-recipe retrieval gap  *(folds F3)*

Take F3 from the F1/F2/F3 plan **verbatim**: new guide
`knowledge/guides/utility-recipes.md` (Mailpit / dev mail catcher / SMTP capture
keywords + `github.com/zeropsio/recipe-mailpit` buildFromGit URL); SMTP cross-link in
`guides/smtp.md`; `engine.go` aliases `mailpit`/`mailhog` → "dev mail catcher smtp
capture utility recipe". Guide edits ship via `zcp sync push guides` (upstream PR);
the alias is an in-repo commit. Flag the two-repo duplication (`zeropsio/recipe-mailpit`
vs recipe-gen's `zerops-recipe-apps/mailpit-app` — Aleš's scope) to Karel/Aleš; do
not unify. Test: `engine_search_test.go` (mail* queries surface the utility guide,
outrank `smtp.md`).

---

## Phase E — Corpus + spec prose corrections

All factual, low-risk. **Guide edits amplify via `sync push guides`** — preview the
full upstream-vs-local diff before pushing; if the diff exceeds the intended edit,
STOP (CLAUDE.local sync-push rule).

1. **E1 — secrets read-back** (`guides/environment-variables.md:3,106,163`,
   `sync push`): "write-only / cannot be read back via API" → "API read is
   **privilege-gated**: an admin/write token reads the value verbatim; a read-only
   token reads `REDACTED`. In-container the value is plaintext (the app needs it)."
2. **E2 — API visibility** (`:63-67`, `sync push`): widen from "cross-service refs
   show as templates" to "the service-env API returns only the service's own
   user-set + intrinsic vars — project vars, cross-service aliases, the ZEROPS_*
   catalog, and **yaml-baked `run.envVariables`** are not returned; read the
   container (or assemble project + active yaml) for the effective env."
3. **E3 — restart precision** (`:3,11-12,150,152,161`, `core.md:154` in-repo): "an
   env-store change propagates in ~5-10s without a container restart; a **running**
   process keeps its boot-time env until it restarts or respawns, so restart the
   service to make the running app read the new value. `zerops.yaml run.envVariables`
   changes need a redeploy (baked into the app version)." *(Optional-polish: actionable
   advice today is already correct — sequence after E1/E2.)*
4. **E4 — spec reconciliation** (`docs/spec-env-handling.md`, in-repo): §4 rationale
   → API *incompleteness* (cite ground-truth §6), not "can't distinguish user/system";
   §4 precedence → cite ground-truth §2, note this is the **local-`.env` merge order**
   (`project < yaml-setup < .env.local`), distinct from and not the container-runtime
   total order (`system > yaml-baked > service-userData > project`); §12.1 → container
   env review must assemble project + active yaml + userData or read in-container, not
   the service-env API alone.
5. **E5 — tool descriptions overclaim slim completeness** (in-repo, NEW): `discover.go:44-45`
   "Sufficient for bootstrap, deploy, recipe validation" and `env.go:48` (service `get`
   "return env var keys and values") present the slim read as complete. Add a one-clause
   caveat: "service env reads are partial — own user-set + intrinsic vars only; not the
   effective container env." Touches `annotations_test.go` word-limit pins.
6. **E6 — reload-vs-restart tool text** (in-repo, NEW, **needs verification**):
   `ops/manage.go:66` + `tools/manage.go:29` say "reload… sufficient for env var
   changes". Per §5 a running PID1 keeps boot env; if reload doesn't re-exec the app it
   won't see new env. **First confirm what Zerops `reload` does to the app process**
   (docs/`../zerops-docs/` or live); if it doesn't re-exec, change the text to recommend
   **restart** for env-change visibility. (ZCP's own `zerops_env set` already restarts,
   so this only affects manual `zerops_manage reload` guidance.)
7. **E7 — networking.md isolation caveat** (`guides/networking.md:39`, `sync push`, from
   §0): "`app_API_TOKEN`-style sibling vars are auto-present only under legacy `none`;
   under default `service` isolation, reference siblings explicitly via `${host_var}`
   in `run.envVariables`."
8. **Soft isolation gap** (optional): one line in `develop-env-var-model.md` or the
   guide — "explicit `${host_var}` refs resolve regardless of `envIsolation`; default
   `service` mode only gates *automatic* sibling injection." Closes the §0 gap.

**Also FLAG to Aleš (do not edit — recipe-authoring scope):**
`themes/refinement-references/ig_one_mechanism.md:162,178` states cross-service refs
"inject project-wide" and a "porter can read `${db_hostname}` directly in code" — false
under default `service` isolation. It's refinement-sub-agent guidance, so it's Aleš's
recipe-generation scope.

No atom AST-pinned fields change here (prose only) except the F1 atom edit (Phase B).

---

## Phase F — git-push token handling

### F.do-now — G1b: denylist `GIT_TOKEN` from local `.env` (parity fix)
Add `"GIT_TOKEN": true` to `platformInternalKeys` (`env_generate.go:83-92`) next to
`ZCP_API_KEY`. Same documented rationale (a deploy-only credential never read by app
code; `git add -A` after generate-dotenv would otherwise publish it). 1-line + a row
in the env_generate denylist test asserting GIT_TOKEN is omitted (alongside the
existing ZCP_API_KEY assertion). Low risk; ships with the env-tooling phases.

### F.decision — G1a: project `sensitive=true` is a platform no-op  *(Codex says promote)*
`EnvSetSensitiveProject` (`ops/env.go:147-184`) writes `GIT_TOKEN` as a project env
with `sensitive=true`; ground-truth §7 `[live]`: project `sensitive=true` does not
persist → stored as plain `USER` var. Exposure surfaces:
- **A read-only project token reads it verbatim** (a true service-level SECRET would be
  `REDACTED` for that token) — the GIT_TOKEN-unique leak.
- Returned by `zerops_discover` / `zerops_env get` under the opt-in `includeEnvValues`
  (`discover.go:323-330`, `env.go:187-209`) — but that's **by-design for every secret**
  (the flag means "show me values"), so not GIT_TOKEN-specific; G1b removes the `.env`
  path.
ZCP's own no-echo (value never in normal response/state/log) **holds**.

Decision for Karel — Codex argues this is a real security bug, not flag-only: **(a)**
relocate the token to a service-level SECRET surface (the only true SECRET store per
§7 — platform `REDACTED` for low-priv tokens) — larger change, touches git-push-setup
wiring + deploy `$GIT_TOKEN` reads; **(b)** document the limitation in the verifier
doc-comment + `git-push-setup` atom; **(c)** leave as-is. Recommend **(a)** if
read-only collaborators are part of the threat model, else **(b)**. *Not implemented
pending your decision — relocating the token is its own mini-project with deploy-path
blast radius (`deploy_git_push.go`, `.netrc` provisioning), so I won't fold it silently.*

---

## Backward-compat seams (published product)

| Surface | Change | Compat |
|---|---|---|
| `zerops_deploy` preflight + `generate-dotenv` (A) | env-ref FAIL/hard-error → conditional WARN for runtime targets | **looser** — fewer false blocks; managed-typo FAIL kept; no user breakage |
| `zerops_env set service` (B2) | replace → **merge** | **bug-fix** — stops silent data-loss; existing service vars now survive a partial set |
| `zerops_env set` (B1/B3) | `shadowWarnings` field; `userDataDuplicateKey` message | additive; restart behavior unchanged |
| `zerops_verify` / `zerops_subdomain` / deploy (C) | `subdomainUrls` retained; `SubdomainURL` never-empty; optional `httpSubdomainUrls` | additive |
| `zerops_knowledge` (D) | new guide + aliases | additive |
| tool descriptions / guides / themes / spec (E) | prose | no schema impact |
| `.env` render (F-G1b) | `GIT_TOKEN` now denylisted | **safer**; a user relying on GIT_TOKEN in local `.env` (unintended) loses it — acceptable (deploy-only credential) |
| user `CLAUDE.md`/`.mcp.json`/`.zcp/state`/permission allowlists | none | tool names + shapes preserved |

No migration needed. All tool names and response shapes preserved; new fields additive.
**B2 is the one behavior *change* (not just additive)** — but it changes data-loss into
correctness, and is gated on a live replace-vs-merge confirmation first.

---

## Phase order, eval gates, effort

**Order (leverage + dependency):**
1. **A** — env-ref conditional WARN (A1 deploy + A2 generate-dotenv; active false
   blockers; depends on nothing).
2. **B** — `zerops_env set` correctness: B1 shadow/"live" (F1) + **B2 service-merge
   data-loss** + B3 dup-key. B2 is the most severe (silent data-loss); live-confirm first.
3. **C** — F2 http-port (independent; option A).
4. **D** — F3 retrieval (independent; content + alias).
5. **E** — corpus/spec/tool-text corrections (prose; sync-push for guides; E6 reload
   gated on a reload-semantics check).
6. **F** — G1b denylist `GIT_TOKEN` (do-now, 1-line, ships with B/E); G1a decision-gated.

**Live confirmations:**
- **B2:** ✅ **DONE (2026-05-28)** — proven on eval-zcp: replace → data-loss. RED step
  is now "make the mock faithful + add merge assertion", code already proven against live.
- **E6:** still open — what does Zerops `reload` do to the running app process (re-exec
  or not)? Cheap to check before touching that tool text.

**Eval gates (`flow-eval`):**
- After **C**: a scenario adding a utility service with a non-HTTP-first port
  (mailpit: SMTP 1025 + HTTP 8025) **plus** a cross-service ref from another runtime
  service — exercises A + B + C together (verify resolves the HTTP port; cross-service
  ref doesn't false-block; a yaml shadow surfaces a warning). Suggested id
  `utility-service-multiport-crossref`.
- **D** is unit-gated (`engine_search_test.go`) — no flow-eval needed.
- Each phase: RED→GREEN, four layers (unit/tool/integration/e2e) green before the next.

**Effort:** A ~120-220 LOC (A1+A2) · B ~360-560 LOC (F1 + service-merge + dup-key) · C
~180-320 LOC · D ~80-180 content+alias · E ~content + 2 tool-desc · F ~10 LOC (G1b).
Total ≈ **800-1400 LOC + content, ~5-8 days** incl. pinned-test/golden updates +
2 live confirmations. (G1a relocation, if chosen, is separate.)

---

## Gate question

1. **Accept the §0 correction (as amended)?** — no isolation-rewrite phase: the
   falsehood is absent from the primary develop atoms/guide/code; it survives only in
   a recipe-authoring doc (→ Aleš) and one networking-guide line (→ E7 one-liner).
   Codex caught that my first "NO file" was too absolute — agreed and amended.
2. **B2 data-loss** — ✅ now PROVEN live (not a hypothesis). OK to ship the merge fix
   (read-merge-write + faithful mock)? This is the most severe finding.
3. **Start with Phase A** (the active false blockers), or reorder?
4. **F2 = option A** (cross-port probe-fallback) confirmed?
5. **G1a (git-token sensitive no-op)** — Codex pushes to promote to a real fix
   (relocate to service-secret, option **(a)**, deploy-path blast radius). Flag-only +
   document **(b)**, or pull **(a)** into scope now?
