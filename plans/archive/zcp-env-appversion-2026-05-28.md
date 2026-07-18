# Plan — env correctness via app-version adoption + production-readiness (2026-05-28)

Authority: `docs/spec-zerops-env-lifecycle.md` (live-verified 2026-05-28). Subsumes
`plans/zcp-env-correctness-fixes-2026-05-27.md` (H1 + F2/point-5 already shipped:
`726867db`, `09ca6a28`). Built from a full re-audit: 4 agents + Codex (19 findings,
convergent) + live tests on throwaway `service` projects + a PHP-FPM reload live test.

Evidence tags: `[code]` read · `[live]` observed on platform 2026-05-28 · `[derived]`.

---

## 0. Unifying root cause + the fix

**ZCP reads service env only from the slim `service-stack/{id}/env` (~2% of container
env) and is therefore blind to yaml-baked `run.envVariables`.** The correct source —
the one the GUI "Environment variables from master" reads — is the **app-version
userDataList** (`GetAppVersion(activeAppVersionId).GetAppVersionUserDataList`),
live-confirmed to return yaml-baked vars (`FOO`, `${db_hostname}` as template,
`ZEROPS_YAML`). The SDK exposes it; **ZCP just never wired it** (`zerops_appversion.go`
has only `GetAppVersionAppCode`; `mapActiveAppVersion` drops `UserDataList`;
`ListServices` drops `ActiveAppVersion.ID`).

**Fix shape:** wire the app-version env source once (Phase 0), then redesign the three
slim-blind consumers (env-ref validation, shadow detection, discover) to read it. This
replaces the "defensive WARN / local-yaml-only" approach of the 05-27 plan with the
**correct, server-side, sibling-capable** read. Plus production-readiness (always
explicit `${host_var}`) + small bugs + docs.

---

## 1. Lifecycle model — the load-bearing correctness constraint

**app-version userDataList exists ONLY for a live runtime service that has deployed at
least once.** Every consumer MUST handle three service states `[live]`:

| Service state | Has app-version? | yaml-baked source | Fallback |
|---|---|---|---|
| **Managed dep** (postgres, valkey…) | **No** (not built from yaml) | n/a — connection vars are intrinsic/managed userData, **already in slim `/env`** | use slim (complete for managed) |
| **Runtime, never-deployed** (bootstrap, `startWithoutCode`) | **No** | only the LOCAL `zerops.yaml` (not yet on platform) | local yaml (deploy-preflight) OR skip+WARN |
| **Runtime, live** (deployed ≥1×) | **Yes** | `app-version/{activeId}` userDataList | — (primary path) |

**Rules every phase obeys:**
- A managed target → slim env is authoritative (don't fetch app-version; it has none).
- A runtime target with `ActiveAppVersion.ID` empty (never-deployed) → **do NOT hard-fail**;
  fall back (local yaml if candidate, else WARN "not yet deployed, can't confirm").
- Only fetch app-version when `IsManagedService==false && ActiveAppVersion.ID != ""`.
- Candidate deploy preflight still reads the **local** `zerops.yaml` for the service
  BEING deployed (its new version isn't on the platform yet); app-version is for the
  SIBLINGS it references.

This is verified end-to-end in Phase 8 (eval) across bootstrap → first-deploy → live.

---

## 2. Confirmed defects (re-audit consolidated)

| ID | Location | Issue | Sev |
|---|---|---|---|
| **P0** | `platform/zerops_appversion.go`, `client.go:98`, `zerops_mappers.go:104,171` | no app-version userData reader; `ActiveAppVersion.ID` dropped on ListServices; stale comment "DTOs don't carry yaml" | enabler |
| **A1** | `deploy_preflight.go:202-240`, `deploy_validate.go:411-436`; blocks `deploy_ssh.go:189`/`deploy_local.go:129`/`deploy_batch.go:111`/`deploy_git_push.go:335` | env-ref validation against slim → false-fail valid ref to sibling yaml-baked → blocks deploy | High |
| **A2** | `env_generate.go:181-220`, `env_plan.go:401-467` | generate-dotenv hard-errors unresolved runtime-sibling ref (slim-blind) | High |
| **F1** | `env_shadow.go`, `checks/env_self_shadow.go`, `env.go` "live" msg | only same-key self-shadow; no cross-layer; "live" ignores precedence | High |
| **disc** | `discover.go:333`, `tools/discover.go:44`, `tools/env.go:187` | discover/env-get omit yaml-baked "from master"; "includeEnvs sufficient" overclaim | Med |
| **PR-1** | `develop-env-var-channels.md:24-27` | precedence inverted (service overrides yaml) — live-disproven | High (corpus) |
| **PR-2** | `environment-variables.md:16,20,105,106-108` | precedence omits yaml layer; secret "cannot read via API"; `none` wording | Med |
| **PR-3** | `networking.md:39`, `examples/gotcha_*` (2), `ig_one_mechanism.md` (Aleš) | bare `<host>_KEY` teaching (none-only, not prod-ready) | Med |
| **PR-4** | `bundle/launch.go:176` | no gate if promoted setup has managed deps but no explicit refs | Med |
| **B3** | `env.go:108`, `errors.go` | `userDataDuplicateKey` surfaced raw (no actionable recovery) | Med |
| **sec** | `env.go:171`, `git_push_setup.go:399`, `launch_existing.go:522` | project `sensitive=true` is a no-op (doesn't persist) | Flag |
| **tok** | `env_generate.go:83` | GIT_TOKEN not denylisted from local `.env` | Low (do-now) |
| **rel** | `manage.go:29`, `ops/manage.go:66`, `env.go:282` | reload advised for env changes; reload doesn't propagate (PHP-FPM zenv-no-USR2; getenv boot-environ) — restart is correct | Med |
| **doc** | `spec-env-handling.md §4,§12.1` | stale precedence rationale + "can't review env from API" | Low |

**Already shipped:** H1 service-env replace→merge (`726867db`); F2/point-5 http-port (`09ca6a28`).
**Out of scope (flag only):** none-mode→service migration for ZCP container projects
(Codex+agent: no hard code dependency; verify live before switching — separate decision).

---

## 3. Phases (TDD RED→GREEN, four layers each)

### Phase 0 — app-version env source (enabler)
- `platform`: add `Client.GetAppVersionUserData(ctx, appVersionID) ([]ServiceEnvVar, error)`
  wrapping `handler.GetAppVersion` → map `out.UserDataList` (Key/Content; drop deprecated Type).
  Mock + e2e.
- `zerops_mappers.go:104` `mapEsServiceStack`: populate `ActiveAppVersion.ID` (ES list carries it).
- New ops helper `ops/env_effective.go`: `EffectiveServiceEnv(ctx, client, svc) (Layered, error)`
  — assembles project + slim-service + (app-version IF runtime & live) with `Source` labels;
  lifecycle-aware (§1). Returns yaml-baked keys/values (templates) for runtime-live.
- Fix stale comment `zerops_appversion.go:14-18`.
- Tests: `platform` client+mock; `ops/env_effective_test.go` (managed→no app-version,
  never-deployed→skip, live→yaml-baked included).

### Phase 1 — env-ref validation on app-version (A1+A2)
- `deploy_preflight.go::preflightEnvRefs`: build `discoveredEnvVars[host]` = slim ∪
  app-version-userData-keys (runtime-live siblings) ∪ project keys. Managed miss → FAIL
  (real typo). Runtime-live miss → FAIL (now we can see its yaml-baked). Never-deployed
  sibling → WARN (can't confirm). Reword message accordingly.
- `env_generate.go::refExpander`: resolve runtime-sibling refs via app-version values;
  unresolved managed → hard error; unresolved runtime-never-deployed → keep literal + warn.
- `checks/env_refs.go`: caller passes app-version-enhanced keys (doc + contract).
- Tests: `deploy_validate_test.go` (unchanged primitive), new `deploy_preflight_test.go`
  (managed-FAIL / live-runtime-resolves / never-deployed-WARN), `env_generate_test.go`.

### Phase 2 — cross-layer shadow + dup-key + live-overclaim (F1)
- `ops/env_shadow.go`: `DetectLayeredShadows(project, serviceUserData, yamlBaked)` →
  report yaml-owned keys as non-overridable (edit yaml + redeploy). Keep self-shadow.
- `env.go` set: `shadowWarnings` from EffectiveServiceEnv; success text conditional.
- `errors.go` + `env.go:108`: translate `userDataDuplicateKey` → "key owned by
  `<svc>` zerops.yaml run.envVariables — edit yaml + redeploy".
- Tests: `env_shadow_test.go`, `env_test.go`, `errors_test.go`, e2e.

### Phase 3 — discover/env-get yaml-baked (disc)
- `discover.go::attachEnvs`: for runtime-live svc, append yaml-baked vars
  `Source:"zerops.yaml"` (from app-version); keep managed ref-hints.
- `tools/discover.go:44`: drop "sufficient for deploy/recipe validation"; note env is
  layered (own+intrinsic from /env, yaml-baked from app-version).
- `tools/env.go` get: include yaml-baked layer for runtime-live.
- Tests: `discover_test.go`, `env_test.go`, `annotations_test.go` (word-limit pins).

### Phase 4 — production-readiness corpus (atoms in-repo; guides via sync push)
- `develop-env-var-channels.md:24-27`: rewrite precedence (yaml owns key; can't override
  at service scope; edit yaml+redeploy). [in-repo, golden refresh]
- `environment-variables.md`: precedence add yaml layer (:16,20); secret privilege-gated
  not write-only (:105); `none` wording (:106-108). [sync push — preview diff first]
- `networking.md:39`: explicit `${host_var}` primary; bare `<host>_KEY` = none-only legacy. [sync push]
- `examples/gotcha_*` (2): same bare→explicit. [in-repo]
- Add explicit-`${host_var}` production-readiness line to `develop-env-var-model.md`. [in-repo]
- FLAG `ig_one_mechanism.md:162` to Aleš (recipe-gen scope — no edit).
- Refresh atom goldens; `TestAtomAuthoringLint` etc. green.

### Phase 5 — launch-production explicit-ref gate (PR-4)
- `bundle/launch.go` / `launch_bundle_compose.go`: when promoted setup has managed deps,
  warn if its `run.envVariables` has no `${host_var}` ref to them (would break in
  `service` prod). Surface as launch warning (not hard-fail). Tests.

### Phase 6 — small bugs
- `env_generate.go:83`: add `"GIT_TOKEN": true` to `platformInternalKeys` + test.
- `manage.go:29`, `ops/manage.go:66`, `env.go:282`: reload→restart text (env changes need
  restart; reload doesn't propagate — PHP-FPM zenv-no-USR2 + getenv boot-environ). Tests/pins.
- sec (project sensitive no-op): FLAG decision (relocate GIT_TOKEN to service secret vs
  document) — do not silently rewrite git-push wiring; raise to Karel.

### Phase 7 — docs + spec
- `spec-env-handling.md §4,§12.1`: reconcile (cite lifecycle spec; API IS reconstructable
  via app-version; precedence is 4-layer).
- `environment-variables.md` secrets/precedence (sync push, overlaps Phase 4).
- `spec-zerops-env-lifecycle.md §5`: add PHP-FPM reload nuance (zenv rewrites config files
  but sends no SIGUSR2 → FPM keeps old config; getenv keeps boot environ; restart only).

---

## 4. Backward-compat seams
- `zerops_deploy` preflight (A1): looser (app-version resolves more; never-deployed→WARN). No break.
- `zerops_env set` (F1/B3): additive `shadowWarnings` + better dup-key message. No break.
- `zerops_discover`/`zerops_env get` (disc): additive yaml-baked layer (new entries, source-labeled). No break.
- corpus/docs (PR/doc): prose. No schema impact.
- New platform `Client` method: internal interface — mock updated; no user surface.
- All tool names/shapes preserved; user `CLAUDE.md`/`.mcp.json`/`.zcp/state`/permissions untouched.

---

## 5. Order, eval, effort
**Order:** 0 (enabler) → 1 → 2 → 3 → 4 → 5 → 6 → 7. Phases 4/6/7-corpus can parallelize (agents).
**Eval gate (`flow-eval`):** scenario with managed db + 2 runtime services where one
references the other's yaml-baked var + a project/yaml shadow — exercises app-version
resolution across bootstrap→first-deploy→live (the lifecycle states in §1). Also confirm
existing greenfield scenarios stay green.
**Effort:** P0 ~150-250 LOC · P1 ~150-250 · P2 ~200-300 · P3 ~120-200 · P4 ~content · P5 ~80-150 ·
P6 ~40-80 · P7 ~docs. Total ≈ 900-1500 LOC + content, multi-day. + live re-verify on eval-zcp.

**Gate question on sec/none only** (the two genuine decisions); everything else executes.
