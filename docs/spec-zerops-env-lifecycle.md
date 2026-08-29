# Zerops Environment-Variable Lifecycle — Ground-Truth Specification

> **Status**: Authoritative for *platform reality* — how Zerops itself stores,
> resolves, and propagates env vars in containers. This is the **territory**;
> `spec-env-handling.md` (ZCP's local-`.env` rendering model) is a **map** that
> MUST conform to this document. ZCP-vs-this-spec conformance is verified
> separately (not in this doc).
> **Date**: 2026-08-21 (service userData model change — `[LIVE 08-21]`): the
> service env type enum collapsed to `USER | SYSTEM`, `sensitive` became a
> REQUIRED write-side flag (`POST/PUT user-data` 400 `invalidUserInput
> metadata.sensitive:["field is required"]` without it), pre-existing secrets
> were migrated to `sensitive:false`, and yaml-baked `run.envVariables` are now
> ALSO mirrored (read-only) on the slim `service-stack/{id}/env` — see §1, §6,
> §7. Prior revision 2026-05-28, after a fresh-`service`-project live pass that
> (a) **found the app-version endpoint that DOES expose yaml-baked vars** —
> reversing PENDING-4's "no endpoint" conclusion; (b) confirmed default `service`
> on a brand-new project + that **managed** `db_*` vars are gated too (not only
> user-defined); (c) re-confirmed precedence (`userDataDuplicateKey` 400 on a
> yaml-owned key) and admin-verbatim secret read. Prior: 2026-05-27 Codex
> red-team (overclaims downgraded; endpoint nuance added).
> **Method**: triangulated across independent sources; every claim carries an
> evidence tag. Highest authority = live empirical.

## Evidence tiers
| Tag | Source | Authority |
|---|---|---|
| `[LIVE]` | Direct REST + in-container SSH on eval-zcp, 2026-05-27/28 (+ REST 2026-05-14) | **Highest** — observed |
| `[LIVE 05-28]` | Fresh throwaway `service`-mode projects via admin REST (`zcp-isotest`/`zcp-prectest`/`zcp-avtest`, all deleted) | **Highest** — observed on default-mode projects |
| `[SDK]` | `zerops-go@v1.0.20` env DTOs/enums | High — API shape |
| `[GUI]` | `zeropsio/frontend-legacy` (platform's own GUI) | High — platform model |
| `[DOC-A]` | Zerops-authored docs | Medium — documented intent |
| `[DOC-B]` | `guides/environment-variables.mdx` = **ZCP prose round-tripped via `sync push`** | **Weak / circular** — never sole basis |
| `PENDING-n` | not yet directly observed — see §9 | — |

---

## 1. The model — two scopes, ONE type enum (`USER | SYSTEM`) + a `sensitive` flag, **three read surfaces**

Env is NOT one uniform thing. Two **scopes**, each its own entity, since 2026-08 sharing one type enum `[SDK][GUI][LIVE 08-21]`:

| Scope | Entity / store | Type enum | Extra fields | Set via |
|---|---|---|---|---|
| **Project** | `ProjectEnv` (`project.envList`) | `USER` \| `SYSTEM` | `Sensitive`, `Editable` | `POST/PUT project/{id}/env`, `PUT project/{id}/env/file` (bulk) |
| **Service** | `ServiceStackEnv` / `UserData` | `USER` \| `SYSTEM` (legacy `READ_ONLY`\|`EDITABLE`\|`SECRET`\|`INTERNAL`\|`ENV` retired 2026-08; the pinned SDK enum still lists them but the wire never returns them) | `Sensitive` (**REQUIRED on every write** — `POST service-stack/{id}/user-data` → 400 `invalidUserInput metadata.sensitive:["field is required"]` without it `[LIVE 08-21]`; `PUT user-data/{id}` requires it per OpenAPI `RequestUserDataPut` — not live-observed without the flag), `serviceStackId` (no `Editable`) | `POST service-stack/{id}/user-data` (ZCP hand-rolls it: the SDK body lacks `sensitive`), `PUT user-data/{id}` (in place), `PUT service-stack/{id}/user-data/env-file` (bulk) `[LIVE 08-21]` |

- Both scopes now share `USER | SYSTEM`; a service env is still a userData record (`UserDataId`, not `EnvId`). `[SDK][LIVE 08-21]`
- **Yaml-baked `zerops.yaml run.envVariables`** for the active app version are userData records of type `USER` with `editable:false` on the **app-version** surface `[LIVE 08-21]` (pre-2026-08: type `ENV`) — and since 2026-08 they are ALSO mirrored on the slim `service-stack/{id}/env` as `USER` records that are **read-only**: `POST` of the same key → 400 `userDataDuplicateKey`, `PUT user-data/{id}` → 400 `recordIsReadOnly`, `DELETE user-data/{id}` → 400 `userDataDeleteForbidden` ("Deleting system userData is forbidden"). `[LIVE 08-21]`
- **User-set vs yaml-baked on the slim `/env` is NOT distinguishable by type** (both `USER`; the slim item carries no `editable`). The user-set layer = slim `/env` `USER` records MINUS the keys of the active app-version `userDataList` `USER` entries — user-set vars never appear in that list (`[LIVE 08-21]`, re-read after the env-change process FINISHED). ZCP derives it that way (`ops.FetchServiceUserEnvs`).
- **THREE read surfaces — which one returns yaml-baked is load-bearing:** `[SDK][LIVE 05-28]`
  - `GET service-stack/{id}/env` → `ServiceStackEnvList` (**slim**). What ZCP's `ops.FetchServiceEnv` uses. `[LIVE 08-21]`: `SYSTEM` intrinsics (hostname, serviceId, projectId, appVersionId, appVersionName, zeropsSubdomain, PATH, ZEROPS_*) + `USER` = user-set userData **AND the read-only mirror of yaml-baked `run.envVariables`** (pre-2026-08 the yaml-baked vars were ABSENT here — `[LIVE 05-28]` saw exactly 9 intrinsic `READ_ONLY` keys + user-set).
  - embedded `userData[]` on `GET service-stack/{id}` (**rich, service-scope**) — omitted yaml-baked pre-2026-08 (PENDING-4 original finding); UNVERIFIED since the 2026-08 mirror landed on the slim `/env`.
  - **`GET app-version/{activeAppVersionId}` → `GetAppVersionUserDataList` (the yaml-baked surface). `[LIVE 05-28]` THIS returns the yaml-baked vars** (`[LIVE 08-21]`: typed `USER` with `editable:false`, listed alongside `SYSTEM` intrinsics; user-set vars NEVER appear here) (`FOO=fromyaml`, `DBREF=${db_hostname}` as a *template*, `SELFREF=${zeropsSubdomain}`, plus `ZEROPS_YAML` = the whole zerops.yaml). **This is what the GUI's "Environment variables from master" reads.** ZCP reads it via `GetAppVersionUserData` / `ops.AppVersionEnvVars` (env-ref validation, generate-dotenv, project-set shadow check, discover/env-get) — the correct source for yaml-baked vars on ANY service (incl. siblings), server-side, no SSH.
- **GUI categories map (load-bearing for ZCP-vs-platform parity)** `[GUI][LIVE 05-28]`:

  | GUI category | Scope / type | Source surface |
  |---|---|---|
  | Project "Environment variables" | project `USER` | `project/{id}/env` |
  | Project "Generated variables" | project `SYSTEM` (envIsolation, *CdnUrl, sshIsolation…) | `project/{id}/env` |
  | Service "Environment variables from master" | yaml-baked `USER` (`editable:false`; pre-2026-08 `ENV`) | **`app-version/{id}` userDataList** (+ read-only mirror on the slim `/env`) |
  | Service "Secret variables" | service user-set `USER` (+ per-var `sensitive` flag; pre-2026-08 type `SECRET`) | `service-stack/{id}/env` (slim) |
  | Service "Generated variables" | service `SYSTEM`/intrinsic (ZEROPS_*, projectId…) | `service-stack/{id}/env` (slim) |

- All env mutations are async (return a `Process`); only GETs are synchronous. Input bodies cannot set `type` (server-assigned) but MUST set `sensitive` (`[LIVE 08-21]`; the pinned SDK `body.UserDataPost`/`UserDataPut` lack the field, so ZCP hand-rolls the POST on the SDK transport and guards the drift with `TestSDKUserDataBody_StillLacksSensitive`). `[SDK]`

---

## 2. Precedence — who wins the BARE key in the container

**Verified TOTAL order** `[LIVE E3, E3-ext, PENDING-1✓]`:

```
system/platform  >  yaml-baked run.envVariables (ENV)  >  service userData (user-set / secret)  >  project
 (unoverridable)        (owns the key namespace)               (beats project)                     (lowest)
```

- Every edge directly tested: system top (E3-ext); **yaml-baked > service-secret** (PENDING-1✓, order-independent — confirms `[DOC-A]` FEAT:317, refutes the earlier userData>yaml inference); **service-userData > project** (live `DUP`; `[GUI]` UI copy "service-level variables take precedence over project-level ones"; `[DOC-A]` FEAT:313); **yaml-baked > project** (mailpit). Transitively consistent.
- **Two cross-layer key-uniqueness guards** `[LIVE]`: (a) a service env-file using a reserved **system** key → `userDataUseOfSystemKey` 400; (b) a service env on a key already in **yaml `run.envVariables`** → `userDataDuplicateKey` 400 (*"key not unique in service stack frame of reference"*). The yaml var literally **owns the key** — a colliding secret is silently dropped at import, removed on override, or rejected post-hoc; the two values never coexist.
- **Direct live proof of (b)** `[LIVE 05-28]`: deployed `app` with yaml `run.envVariables: {FOO: fromyaml}` → `PUT service-stack/{id}/user-data/env-file {envFile:"FOO=fromuserdata"}` → **`userDataDuplicateKey` 400** *"UserData key 'FOO' is not unique in service stack frame of reference."* So yaml-baked `> service userData` is not just a precedence ordering — the lower layer **cannot be written at all** for a yaml-owned key. (Refutes any "service-level shadows the yaml var, delete it to fix" model.)
- **F1 consequence (sharpened):** a yaml-baked shadow (mailpit's `MAIL_MAILER: log`) is fixable **only by editing the yaml** — overriding it via `zerops_env set service` is *structurally impossible* (platform rejects the duplicate key). "Set at service scope" is not just non-durable, it's disallowed → the F1 warning is single-path: edit the yaml + redeploy.

---

## 3. Bare-vs-aliased model + cross-service injection

Prefixed aliases observed **under `envIsolation=none`** (eval-zcp's mode) `[LIVE E3, E4]`:

| Alias | Meaning | Present when |
|---|---|---|
| `<KEY>` (bare) | §2 precedence winner | always |
| `PROJECT_<KEY>` | the project-scope value (even when shadowed) | for project vars `[LIVE]`; `[DOC-A]` does NOT document this prefix |
| `<hostname>_<KEY>` | a sibling's resolved value | for sibling vars **under `envIsolation=none`** |
| `RUNTIME_<KEY>` | this service's runtime var, readable during build | `[DOC-A][GUI]` |
| `BUILD_<KEY>` | a build var readable at runtime | ✗ REFUTED `[LIVE 2026-06-02]`: `${BUILD_x}` reaches the runtime process as a literal string — build vars are NOT carried into the runtime env |

- Under `none`, cross-service injection is extensive: each sibling's fully-resolved env (incl. secrets + yaml-baked) lands as `<host>_KEY` (observed `core_JWT_SECRET`, `zcp_ZCP_API_KEY`). Bare keys do NOT leak cross-service. `[LIVE E4]`
- **Under default `envIsolation=service` this auto-injection does NOT happen** `[LIVE PENDING-2✓]`: a service receives NO `<host>_KEY` sibling vars (confirmed in-container + zembed + env-file render). The "every sibling var" behavior above is **`none`-mode only**.
- **Gating applies to MANAGED vars too, not just user-defined** `[LIVE 05-28]`: on a fresh default-`service` project (`zcp-isotest`: managed `db` + runtime `app`), `app`'s env-file render showed **ZERO `db_*` keys** (`overrideEnvIsolation=service` → 0 `db_*`; `=none` → all 21: `db_hostname`, `db_password`, `db_connectionString`, …). So a runtime service does NOT auto-see a managed DB's connection vars under `service` — it MUST reference `${db_*}` explicitly (or set the DB to `none`, per `[DOC-A]` tip *"set a database service to `envIsolation: none` to expose its connection details without having to manually reference them"*). This closes the one sub-case PENDING-2 left open (it had tested user-defined vars only).
- **`none` is a publish/SOURCE semantic, directional:** setting service Y to `none` exposes Y's vars to siblings; it does NOT make Y *receive* others'. To make X auto-receive Y's vars without an explicit ref, the SOURCE Y (or the whole project) must be `none`. `[LIVE]`
- **Project→service inheritance is NOT gated:** `PVAR` + `PROJECT_PVAR` reach a service even under `service` mode; only service→service sharing is gated. `[LIVE]`
- **Explicit refs resolve regardless of isolation:** `BREF=${aaa_AVAR}` resolved to `aval` under `service` even though `aaa_AVAR` is not an injected key — refs resolve at deploy/interpolation, independent of the auto-injection gate. `[LIVE]`
- The parser treats `_` as the cross-service ref delimiter (`local` vs `external` ref). Service hostnames are `[a-z0-9]` only (dashes/underscores/uppercase rejected `serviceStackNameInvalid`), so no dash→underscore rewrite applies — a dashed hostname cannot exist. `[GUI][LIVE 2026-06-02]`
- **Unresolved refs stay literal**: `${db_hostname}` to an absent service reaches the process verbatim — no error, no blank (the self-shadow failure class). `[LIVE E7]`

---

## 4. envIsolation — modes + mutability

`envIsolation` is a `SYSTEM` project var, **`Editable=true`** `[LIVE 05-14][SDK]`. Set at project/service **creation** (import `project.envIsolation` / `services[].envIsolation`); the existing SYSTEM var is **likely updatable post-creation via PUT/GUI** (Editable=true) — the live `POST` only proved you can't create a *second* `envIsolation` (`projectEnvDuplicateKey`), NOT that it's immutable. Enum: `service` (default) \| `none` (legacy). `[SDK][DOC-A]`

| Mode | Behavior | Status |
|---|---|---|
| `none` (legacy) | a service in `none` PUBLISHES its vars to all siblings as `<host>_` (source-side) | **CONFIRMED** `[LIVE]` |
| `service` (default) | siblings NOT auto-injected; service sees only own + project vars + explicit `${host_var}` refs | **CONFIRMED `[LIVE PENDING-2✓]`** — DOC-A correct |

- **`envIsolation` is SOURCE-side and directional** `[LIVE PENDING-2✓]`: flipping service Y to `none` exposes Y's vars to siblings; it does NOT make Y receive others'. (Confirmed: `bbb`→`none` left bbb blind to `aaa`, but `aaa` gained all `bbb_*` keys.) Receipt is governed by the SOURCE's mode or the project mode — not the receiver's.
- **Default on a brand-new project is `service`** `[LIVE 05-28]`: a project imported with NO `envIsolation` field reads back `envIsolation="service"` (env-file render). So every fresh user project (GUI- or import-created) is isolated.
- **⚠ ZCP-managed *container* projects are `none` by ZCP's own choice** `[LIVE][GUI]`: the project where a ZCP agent container runs (`eval-zcp`; any project carrying `ZCP_API_KEY` + `sshIsolation: vpn service@zcp`) is set to `none` so the ZCP container can read sibling env for discovery/management. This is a **ZCP deployment choice, NOT the platform default**. Consequence: **eval-zcp (and every flow-eval run on it) is `none`, so it does NOT represent the `service`-isolated reality of user projects + production.** Anything ZCP/atoms/evals do that depends on auto-injection passes on eval-zcp and breaks for a real user. (The fix: ZCP corpus already teaches explicit `${host_var}` refs — see §recipe-readiness — which work in both modes.)

---

## 5. Propagation & lifecycle

**Mechanism** `[LIVE E1]`: a `zerops-zembed` daemon maintains `/etc/zerops-zembed/env.json` (flat merged map). Project/service env changes are written there **in place, ~5–10 s, NO container restart**. The **running PID1 keeps its boot-time environ** — only newly-spawned processes see the change. This reconciles docs' "restart required" (true for the running process) with reality (the value lands without a restart).

| Event | Reaches container | Restart? | Running process sees it? |
|---|---|---|---|
| set project env | zembed, ~5 s | No | new processes only |
| set service userData | zembed, ~6 s | No | new processes only |
| delete project/service env | removed (+ aliases), ~10 s | No | new processes only |
| change `zerops.yaml run.envVariables` | **only on redeploy** (baked into app version) | new container | yes |
| restart service | re-reads merged env.json at boot | yes | yes |
| sibling change | `<host>_KEY` updates, ~8 s | No | new processes only |

- Platform does NOT auto-restart on env set; ZCP's `zerops_env set` adds the restart. `[DOC-A][LIVE]`
- **PHP-FPM caveat — reload does NOT re-read env/config; restart does** `[LIVE 05-28]`: for PHP runtimes, `zerops.yaml`-configured `PHP_INI_*` / `PHP_FPM_*` vars are applied by `zerops-zenv` rewriting the FPM config files on `reload`, but zenv does **not** send the FPM master `SIGUSR2` — so the running master keeps its old config; only a `restart` (or a manual `kill -USR2 <fpm-master-pid>`) re-reads them. `getenv()`-style env likewise stays at the PID1 boot environ until restart. Consequence: for PHP, prefer **restart over reload** when an env or `PHP_*` config change must take effect — which is why `zerops_env set` restarts rather than reloads.

---

## 6. API visibility — NO single endpoint is complete, but the env IS API-observable in pieces

The **single `GET service-stack/{id}/env` (slim)** endpoint that ZCP uses returns only a sliver of the ~511 container keys — `SYSTEM` intrinsics + `USER` (user-set userData + the read-only mirror of yaml-baked `run.envVariables`, `[LIVE 08-21]`). It does NOT return project envs, cross-service `<host>_` aliases, or platform-injected runtime vars. Pre-2026-08 it also omitted the yaml-baked vars (9 intrinsic `READ_ONLY` keys + user-set, `[LIVE 05-28]`). `[SDK]`

- This is an **API-incompleteness** problem for *that one endpoint* — but **the effective env IS reconstructable from the API** by combining surfaces (no SSH required):
  - **project vars** ← `project/{id}/env` (or project-search `EnvList`). `[SDK]`
  - **yaml-baked `run.envVariables`** ← **`app-version/{activeAppVersionId}` userDataList** `[LIVE 05-28]` (the key correction — see §1; this is the GUI "from master" source, works for any service incl. siblings).
  - **service user-set + intrinsic** ← slim `service-stack/{id}/env`.
  - cross-service refs appear in app-version userData as **unresolved templates** (`${db_hostname}`); to see the *resolved* value, read in-container (`/etc/zerops-zembed/env.json` via SSH) or `project/{id}/env-file?overrideEnvIsolation=…` (env-file render resolves refs — `[LIVE 05-28]`).
- **Implication (corrected):** ZCP need NOT be slim-blind. A cross-layer shadow check, an env-ref validation, or a "container env review" can assemble project + app-version-userData + service-env from the API. Reading the local `zerops.yaml` (deploy-preflight) is only the *local-mode* alternative; the app-version endpoint is the server-side, sibling-capable source.

---

## 7. Secrets & the `sensitive` flag (2026-08 model)

- **`sensitive` is an explicit, per-variable, REQUIRED write-side flag** on service userData (`POST service-stack/{id}/user-data` `[LIVE 08-21]`; `PUT user-data/{id}` per OpenAPI `RequestUserDataPut`). The platform migrated every pre-existing service secret (old type `SECRET`) to `type:USER, sensitive:false` — e.g. `VSCODE_PASSWORD` and the `ZCP_AGENT_OAUTH_*` flags read back `sensitive:false` after the change. `envSecrets` / `dotEnvSecrets` (import) and the GUI secret editor still create user-set `USER` records; which `sensitive` value a fresh import assigns is UNVERIFIED since the change.
- **ZCP writes every service-scope SECRET var with `sensitive:true`** (`zerops_env set serviceHostname=…`, git-push-setup `GIT_TOKEN`, launch-production `ZCP_LAUNCH_TOKEN`) — exactly the pre-change behavior, when everything ZCP wrote there was a masked `SECRET`; project-scope writes stay `sensitive:false`. The `ZCP_AGENT_OAUTH_*` flags are the one deliberate exception: `zcp agent mark-oauth` writes them `sensitive:false` (matching the GUI's own writer), because the GUI's `/user-data/search` read path redacts sensitive content even for the org owner — a sensitive flag row would read back as NOT authorized in the GUI though the platform holds it `true`. `ops.MarkAgentOAuth` migrates a pre-existing sensitive row (delete + recreate non-sensitive) on the next call. Plain config belongs in `zerops.yaml run.envVariables` (§2 channel rule).
- Precedence "yaml basic/runtime overrides secret" is **VERIFIED** `[LIVE PENDING-1✓]`: the yaml-runtime var wins and the colliding secret never registers (see §2). Confirms `[DOC-A]` FEAT:317. (2026-08: the yaml-baked key is now visibly present on `/env` and rejects set/delete — §1.)
- **In-container = PLAINTEXT**: `/etc/zerops-zembed/env.json` carried `SECRET` values unmasked — the running app needs the real value `[LIVE]` (pre-2026-08). For a `sensitive:true` record under the new model this is **VERIFIED** `[LIVE 08-22]`: a `zerops_env set` write (`sensitive:true`, no restart) was visible in a FRESH SSH session on the same service ~10 s later (`printf %s "$KEY"` → the plaintext value), and git-push-setup's step-6c fresh-session auth probe (credential helper reading the just-written `GIT_TOKEN`) passed on the first attempt — the delivery path is the env store → fresh-session env, independent of the flag.
- **API read is PRIVILEGE-GATED** `[LIVE PENDING-3✓]`: on the SAME `GET /service-stack/{id}/env`, an admin/write token gets `content` **verbatim**; a **read-only token gets `content:"REDACTED"`**. Masking is keyed on `sensitive=true` (non-sensitive vars are verbatim for both), applies to `/env` AND the env-file projection, and is server-enforced — the GUI blur is an *additional* layer, not the only one. A low-privilege holder can enumerate that a secret EXISTS (key/type/sensitive/timestamps) but not its value. `GET /user-data/{id}/reveal` returns the decrypted value of a sensitive item and "requires sudo mode" (OpenAPI, `[LIVE 08-21]` spec read).
  - Admin-verbatim re-confirmed `[LIVE 08-21]`: `POST {key:ZCP_PROBE, content:probe, sensitive:true}` → `GET /env` with an owner token → `{key:"ZCP_PROBE", content:"probe", type:"USER", sensitive:true}` (earlier `[LIVE 05-28]` sample read `type:"SECRET"`). Read-only `REDACTED` half is from the earlier pass.
- **`sensitive` is the only secret marker now** — there is no `SECRET` type any more; it is still caller-chosen, not authoritative about the key's nature (pre-change `[LIVE 05-14]`: `ZCP_API_KEY` → `Sensitive=false`; managed `secretAccessKey` → `Sensitive=true`). ZCP's value masking stays key-based (`RedactCredentialValue`), never flag-based.
- **Project-level `sensitive=true` did NOT persist** pre-2026-08 (read back `sensitive:false, type:USER`) `[LIVE]`; UNVERIFIED since the change. ZCP keeps project-scope writes `sensitive:false`.

---

## 8. Platform-injected vars (observed sample, not exhaustive)

An **observed sample** (~119 bare on ONE no-config alpine — not a guaranteed universal set) `[LIVE E6]`: identity (`hostname`/`serviceId`/`projectId`/`appVersionId`), the `ZEROPS_*` networking set (NAT/VxLan/VPN ranges+gateways — **incl. `ZEROPS_VpnPrivateKey`/`VpnPublicKey` exposed in-container**), `zeropsSubdomain*`, `apiCdnUrl`/`staticCdnUrl`/`storageCdnUrl`, `envIsolation`, `sshIsolation`, `ZEROPS_YAML` (full deployed yaml). Project-scope SYSTEM vars split: `Editable=false` (CDN/subdomain, platform-managed) vs `Editable=true` (`envIsolation`/`sshIsolation`). `[LIVE 05-14]`

---

## 8b. Adjacent mechanisms (in scope, lightly covered)
- **build vs run are separate environments** — `build.envVariables` ≠ `run.envVariables`; same names allowed; not auto-shared. Cross-access via `RUNTIME_` (run→build, supported) / `BUILD_` (build→run, **REFUTED** — does not resolve, `[LIVE 2026-06-02]`). `[DOC-A]`
- **`envReplace`** — deploy-time *file* placeholder substitution (yaml spec), distinct from container env injection. Its own source/precedence are untested → PENDING. `[DOC-A]`
- **Import-time `services[].envVariables` is SILENTLY DROPPED by the API** — only `envSecrets` / `dotEnvSecrets` / `run.envVariables` create service env. ZCP surfaces a warning (`internal/ops/import.go`). `[LIVE]`

---

## 9. Open cells
| # | Question | Status | Close / closed by |
|---|---|---|---|
| **PENDING-1** | yaml-runtime vs service-secret same-key winner | ✅ **RESOLVED** — yaml wins, order-independent, owns the key (§2) | `[LIVE]` env-prec. (Minor sub-case left: `build.envVariables` vs secret in the build container.) |
| **PENDING-2** | does project `envIsolation=service` gate sibling auto-injection? | ✅ **RESOLVED — YES it gates** (DOC-A correct, DOC-B wrong). `none` is source-side; project→service not gated; explicit refs resolve — §3/§4 | `[LIVE]` throwaway project `zcp-envtest-throwaway` |
| **PENDING-3** | secret masking for non-admin token | ✅ **RESOLVED — API privilege-gated** (read-only token → `content:"REDACTED"`; admin → verbatim; keyed on `sensitive=true`; cross-surface) — §7. 2026-08: `sensitive` is now the explicit, required write-side flag (no `SECRET` type) — admin-verbatim re-confirmed `[LIVE 08-21]` | `[LIVE]` |
| **PENDING-4** | does any endpoint expose yaml-baked `ENV` vars? | ✅ **RESOLVED — YES, via the app-version endpoint (was answered too narrowly before).** *2026-08 update `[LIVE 08-21]`: the yaml-baked vars (now typed `USER`, `editable:false`) are ALSO mirrored read-only on the slim `service-stack/{id}/env`; the app-version list stays the authoritative yaml-baked surface and never carries user-set vars — §1.* Pre-2026-08 the service-stack endpoints (slim `/env` + embedded `userData[]`) did NOT return yaml-baked — only `GET app-version/{activeAppVersionId}` `GetAppVersionUserDataList` did (`[LIVE 05-28]`: `FOO`, `${db_hostname}` template, `ZEROPS_YAML`). This is the GUI "from master" source. So deploy-preflight reading local `zerops.yaml` is NOT the only detector — the app-version userDataList is a server-side, sibling-capable detector. §1/§6 corrected. | `[LIVE 05-28]` (`zcp-avtest`) |

## 10. Contradictions
- **C1 (isolation auto-inject) — RESOLVED `[LIVE 05-28]`:** default `service` mode **gates** sibling auto-injection (no `<host>_` keys, **including managed `db_*`**); `none` auto-shares (source-side). **DOC-A is correct.** The earlier "auto-inject everywhere" impression came entirely from eval-zcp being `none` (a ZCP container-project choice, §4) — a brand-new project is `service`. **Production-readiness consequence:** ZCP-generated code MUST use explicit `${host_var}` refs (which resolve in BOTH modes); most recipe corpus files already do this (`${db_hostname}` etc.), so the corpus is production-ready. Bare reliance on `<host>_KEY` injection (none-only) would break on user/production projects.
- **GUI vs API — RESOLVED:** GUI's "from master" yaml-baked vars come from the **`app-version/{id}` userDataList**, not the service-env endpoints — §1/§6, PENDING-4 corrected.
- **`PROJECT_<KEY>` prefix:** `[LIVE]` real; `[DOC-A]` undocuments it → spec records live truth.

## 11. Reconciliation with `spec-env-handling.md` (the ZCP map)
`spec-env-handling.md` has been reconciled against this spec — the items below are now CONFORMANT:
- Its §4 retracted the old "API cannot distinguish user from system service envs" attribution as **too narrow** and now states the real blocker — API *shape* (pre-2026-08 the slim `/env` omitted yaml-baked; since 2026-08 it mirrors them read-only as `USER`, so the user-set layer is derived by subtracting the app-version `USER` keys) — and cites this spec (§1, §6). The full effective service env is reconstructable from the API.
- Its §4 "`zerops.yaml > project`" holds **for bare runtime keys** and now cites this spec's 4-layer order (§2), scoping it below the system + service-userData layers.
- Its §12.1 "container env review" no longer relies on the slim service-env API alone: it assembles `project/{id}/env` + **`app-version/{id}` userDataList** (yaml-baked) + slim `service-stack/{id}/env` from the API without SSH (citing §6). For resolved cross-service ref values, read in-container (zembed/SSH) or the env-file render.

## 12. Glossary
- **userData** — per-service env store (`ServiceStackEnv`/`UserData`); `SYSTEM` intrinsics + `USER` records (user-set, each with a caller-set `sensitive` flag) + since 2026-08 a read-only `USER` mirror of the yaml-baked `run.envVariables` (authoritative yaml-baked surface = app-version userDataList; user-set = slim `USER` minus those keys — §1).
- **app-version userDataList** — `GetAppVersion(activeAppVersionId).GetAppVersionUserDataList`; the surface that returns **yaml-baked `run.envVariables`** (as templates) + `ZEROPS_YAML`. GUI "from master" source; ZCP reads it via `platform.Client.GetAppVersionUserData` / `ops.AppVersionEnvVars`.
- **zembed** — in-container daemon owning `/etc/zerops-zembed/env.json`, the flat merged effective env; updated in place on env change.
- **three read surfaces** — slim `service-stack/{id}/env` (ZCP uses; since 2026-08 it ALSO carries the read-only `USER` mirror of yaml-baked vars) · embedded `userData[]` (missed yaml-baked pre-2026-08; UNVERIFIED since) · `app-version/{id}` userDataList (HAS yaml-baked — the authoritative yaml-baked surface).
- **bare key vs alias** — `KEY` (winner) vs `PROJECT_KEY` / `<host>_KEY` (scoped copies).

---

## 13. ZCP production-readiness rule (derived)

ZCP operates across BOTH modes: container-dev projects are `none` (ZCP's choice, §4),
but **user dev (local) + every `launch-production` project are `service`** (§4 —
default; launch bundle sets no `envIsolation`). Therefore **ZCP must always emit
cross-service wiring as explicit `${host_var}` refs in `run.envVariables`** — never
rely on `<host>_KEY` auto-injection (none-only). This single rule makes ZCP output
correct on every project type. Most recipe corpus files already do this.

**`none`-dependency — RESOLVED (2026-05-28):** ZCP has NO hard code dependency on
`none` (all sibling/managed env reads go through the API, never container env —
verified across discover/auth/init/runtime). Decision: **zerops app will be updated
platform-side to create ZCP container projects with `envIsolation: service`** (Karel)
so they match what users + production run, giving full parity (and making flow-eval
on eval-zcp representative of `service` behavior). No ZCP code change needed; until
that platform change ships, eval-zcp remains `none`.
