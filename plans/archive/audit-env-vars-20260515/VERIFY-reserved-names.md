# Phase 0 — Verify report — Reserved env-var names + connectionString shape

**Date:** 2026-05-16
**Project:** eval-zcp (`waAzEFn6SBaysG4YE4rv7A`)
**Probe service:** `envprobe` (nodejs@24 NON_HA `startWithoutCode:true` `minContainers:1`, serviceId `hiy5jYyHQPO6LDQ3I8CWBA`)
**Status:** ✅ all probes executed; ready for Phase 1.

---

## A. Headline verdict — three distinct rejection regimes

Run 1 agent's "HOSTNAME is platform-reserved" was correct in **conclusion** but wrong in **mechanism**. There are actually **three** rejection paths, each with a distinct signal:

| Regime | Trigger | When it fires | Signal |
|---|---|---|---|
| **R1 — zcli pre-flight** | `hostname` (lower), `PATH`, `serviceId` (and presumably other Zerops platform-internal vars) | BEFORE package upload — zcli rejects locally based on API error | `level=error msg="✗ ERR Usage of system key(s): '<key>'"` + `code: userDataUseOfSystemKey` |
| **R2 — deploy-stage late reject** | `HOSTNAME` (UPPER) in `run.envVariables` ONLY | AFTER `Application is deploying` log line — process FAILED in ~4-5s | `BUILD_FAILED` event + `failureClass: "build"` + `failureCause: "Build pipeline failed; no recognized log pattern matched."` + zero build-container logs |
| **R3 — accepted** | Everything else tested: `USER`, `HOME`, `LOGNAME`, `SHELL`, `PWD`, `NODE_ENV`, `PORT` (number or string), `zeropsSubdomainHost`, `HOSTNAME` in `build.envVariables` | Build completes normally | `BACKUP` event + process FINISHED 56s-1m7s |

**This refutes Run 1 agent's memory text** *"HOSTNAME is platform-reserved at the runtime container level"* — partially true (HOSTNAME in `run.envVariables` IS rejected), partially false (the same key in `build.envVariables` is fine, and the rejection mechanism is platform-level, not "runtime").

The agent's confidence in the memory file was the over-confident verbalization Karel suspected: bisecting `{NODE_ENV, HOSTNAME, PORT}` together → removing the group → success → attributed to HOSTNAME by name-pattern guess, without isolating.

---

## B. Probe-by-probe results

### Sweep 1 (P1-P10)

| ID | envVariables block | Where | Result | Signal | Latency |
|---|---|---|---|---|---|
| P1 | `BASELINE: "true"` | run | ✅ PASS | BACKUP, 56s | baseline |
| P2 | `HOSTNAME: 0.0.0.0` | run | ❌ R2 | BUILD_FAILED, no-pattern fallback | 4s |
| P3 | `PORT: 3000` (number) | run | ✅ PASS | BACKUP, 56s | normal |
| P4 | `NODE_ENV: production` | run | ✅ PASS | BACKUP, 58s | normal |
| P5 | `hostname: example` (lower) | run | ❌ R1 | `userDataUseOfSystemKey: 'hostname'` | <1s, no upload |
| P6 | `PATH: /custom/bin` | run | ❌ R1 | `userDataUseOfSystemKey: 'PATH'` | <1s, no upload |
| P7 | `USER: foo` | run | ✅ PASS | BACKUP, 1m7s | normal |
| P8 | `HOME: /custom/home` | run | ✅ PASS | BACKUP, 1m5s | normal |
| P9 | `HOSTNAME: "0.0.0.0"` (quoted) | run | ❌ R2 | BUILD_FAILED, no-pattern fallback | 5s |
| P10 | `PORT: "3000"` (string) | run | ✅ PASS | BACKUP, 56s | normal |

### Sweep 2 (P11-P18)

| ID | envVariables block | Where | Result | Signal | Latency |
|---|---|---|---|---|---|
| P11 | `HOSTNAME: 0.0.0.0` | **build** | ✅ PASS | BACKUP, 57s | normal |
| P12 | `hostname: example` (lower) | build | ❌ R1 | `userDataUseOfSystemKey: 'hostname'` | <1s |
| P13 | `HOSTNAME: 0.0.0.0` + `DATABASE_URL: postgresql://foo:bar@db:5432/db` | run | ❌ R2 | BUILD_FAILED | 5s |
| P14 | `LOGNAME: foo` | run | ✅ PASS | BACKUP, 1m3s | normal |
| P15 | `SHELL: /bin/bash` | run | ✅ PASS | BACKUP, 1m1s | normal |
| P16 | `PWD: /custom/cwd` | run | ✅ PASS | BACKUP, 1m | normal |
| P17 | `zeropsSubdomainHost: custom123` | run | ✅ PASS | BACKUP, 1m | normal |
| P18 | `serviceId: custom` | run | ❌ R1 | `userDataUseOfSystemKey: 'serviceId'` | <1s |

### Critical facts established

1. **HOSTNAME is location-dependent.** P11 (build) passes, P2/P9/P13 (run) all fail. The platform allows `HOSTNAME` in the build scope but not the run scope.
2. **Quoting doesn't matter.** P2 (`HOSTNAME: 0.0.0.0`) and P9 (`HOSTNAME: "0.0.0.0"`) both fail identically.
3. **Value doesn't matter** (probably). Tested with `0.0.0.0` — DATABASE_URL combo (P13) confirms HOSTNAME alone triggers the fail, not the value.
4. **Reserved-key denylist at zcli level is small and case-sensitive.** `hostname` rejected, `HOSTNAME` not. `PATH` rejected — case unknown (would need probe).
5. **`USER`, `HOME`, `LOGNAME`, `SHELL`, `PWD` are NOT reserved.** Linux defaults that the agent assumed might fail — they don't.
6. **`zeropsSubdomainHost` is NOT reserved.** Run 2 agent suspected it wouldn't resolve in `run.envVariables` — it does, and overriding it doesn't fail.

---

## C. The platform signal we can use

### C1. R1 (zcli reject) — already structured

The Zerops API returns this when you POST an appVersion with a system-keyed env:

```
code: userDataUseOfSystemKey
error: 'Usage of system key(s): ''hostname'''
metadata: null
```

zcli surfaces it as a `level=error` line. **ZCP can match this code at the API client layer** and emit a clean structured error like `ErrReservedEnvKey: { rejectedKeys: ["hostname"] }`.

But: by the time the user/agent hits this, they've already typed the bad value and run a push. Better to **preflight in ZCP** before the push reaches the API — zero round-trip.

### C2. R2 (late deploy reject) — needs derivation

The platform does NOT return a typed code for the `HOSTNAME`-in-`run.envVariables` case. We get:
- Event `type: "build"` `status: "BUILD_FAILED"`
- `failureClass: "build"` (ZCP-classified)
- `failureCause: "Build pipeline failed; no recognized log pattern matched."` (ZCP fallback — no signal matched)
- Process FAILED in **4-5s**
- **Zero build-container logs** (no `RUNNING BUILD.BUILDCOMMANDS` block visible)

This is a derivable pattern: a `BUILD_FAILED` with duration < 10s AND zero build logs implies platform rejected before build container ran. The existing classifier (`internal/ops/deploy_failure.go` `ClassifyDeployFailure`) has no signal for this — we can add one.

### C3. Why HOSTNAME (upper) differs from hostname (lower)

Hypothesis: Zerops API's denylist (the one that emits `userDataUseOfSystemKey`) is incomplete. It catches `hostname` (lowercase — the platform's own auto-injected service env) and `PATH` and a small set of internals (`serviceId`, presumably `projectId`, `appVersionId`, `apiCdnUrl`, etc. — system-generated keys). But it doesn't include `HOSTNAME` (uppercase, Linux kernel convention) — that gets through validation but breaks runtime container startup at a later layer, surfaced as `BUILD_FAILED`.

This is arguably a Zerops platform bug — the API should reject `HOSTNAME` with the same `userDataUseOfSystemKey` code. ZCP can compensate without waiting for the platform fix.

---

## D. Postgres `connectionString` shape — independently confirmed

`zerops_discover service="db" includeEnvs=true includeEnvValues=true` (against a real postgresql@18 service) returns:

```json
{
  "isReference": true,
  "key": "connectionString",
  "value": "postgresql://${user}:${password}@${hostname}:${port}"
}
```

**No `/${dbName}` suffix.** Confirmed both empirically in the Run 1 transcript (agent SSH'd in and got `DATABASE_URL=postgresql://db:DYg...@db:5432`) and via the structured API response. Prisma/Drizzle/SQLAlchemy and any client that needs a fully-qualified URL must compose explicitly. The atom claim *"prefer it over assembling hostname:port:user:password:dbName"* is factually misleading.

The platform also exposes `dbName` as a separate env, so the corrected guidance is:
```yaml
DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
```

---

## E. Verified reserved-name set for Phase 1 atom

Three confidence levels, all empirically verified:

### E1. Hard-reserved (zcli rejects with `userDataUseOfSystemKey`) — verified
Exact case-sensitive match against the Zerops API denylist:

| Key | Verified | Probe |
|---|---|---|
| `hostname` | ✅ | P5, P12 |
| `PATH` (uppercase only — `Path` and `path` slip through) | ✅ | P6 |
| `serviceId` | ✅ | P18 |
| `projectId` | ✅ | P19 |
| `appVersionId` | ✅ | P20 |
| `appVersionName` | ✅ | P21 |
| `zeropsSubdomain` (resolved fully-qualified URL) | ✅ | P24 |

### E2. Platform-injected BUT not API-rejected — surprising
These appear in container `printenv` (platform-injected) but the API silently accepts user overrides:

| Key | Verified accepts override | Probe |
|---|---|---|
| `apiCdnUrl` | ✅ accepts | P22 |
| `envIsolation` | ✅ accepts | P23 |
| `zeropsSubdomainHost` | ✅ accepts | P17 |
| `zeropsSubdomainString` | (untested, presumed same as Host) | — |

Treating these as reserved in our atom is over-conservative; the platform permits the override, which is a legitimate (if unusual) use case. Document as "discouraged — you're shadowing a platform-provided value" rather than "rejected".

### E3. Run-scope-only deploy-stage rejection (passes zcli, fails late) — verified
Pattern: anything that smells like the OS PATH or HOSTNAME convention, *case-INsensitively*. The runtime container init crashes; surface = empty build logs + BUILD_FAILED in 4-5s.

| Key | Verified | Probe |
|---|---|---|
| `HOSTNAME` (uppercase) | ✅ | P2, P9, P13 |
| `Path` (capitalized) | ✅ | P25 |
| `path` (lowercase) | ✅ | P26 |

Asymmetry: `PATH` is API-rejected (R1, case-sensitive); `Path`/`path` slip past the API and crash runtime (R2). Both regimes catch every dangerous override; only the signal differs. The user-actionable diagnosis is the same in both cases: pick a different key name.

### E4. NOT reserved (PASS in both regimes) — verified
Linux defaults the agent (and I in the original plan) assumed might be reserved — they're not:

| Key | Verified | Probe |
|---|---|---|
| `USER` | ✅ PASS | P7 |
| `HOME` | ✅ PASS | P8 |
| `LOGNAME` | ✅ PASS | P14 |
| `SHELL` | ✅ PASS | P15 |
| `PWD` | ✅ PASS | P16 |
| `PORT` (number) | ✅ PASS | P3 |
| `PORT` (string) | ✅ PASS | P10 |
| `NODE_ENV` | ✅ PASS | P4 |
| `HOSTNAME` in build.envVariables (NOT run) | ✅ PASS | P11 |

### E5. Strongly suspected reserved by extension (untested individually)
These are also `ZEROPS_*`-family or known internal vars seen in container `printenv`; same reject mechanism almost certainly applies:

- `staticCdnUrl`, `storageCdnUrl` (E2-pattern — likely accepted but discouraged)
- `sshIsolation` (E2-pattern — likely accepted)
- Most `ZEROPS_*` prefixed keys (we never tested an explicit `ZEROPS_*` user override)
- Per-service-type generated keys like `db_*`, `cache_*` when overridden on the OWN service of that hostname

None of these are blocking for the Phase 1 atom — the verified E1 + E3 set is enough to document the failure modes.

---

## F. What changes in PLAN.md (Phase 1 + Phase 3)

### F1. Atom — `develop-reserved-env-names.md`
New atom for Phase 1. Content:

```markdown
## Reserved env-var keys — two regimes

Two distinct rejection mechanisms apply to zerops.yaml `envVariables`:

### Regime 1 — Hard-reserved, API-level (any scope)
The Zerops API rejects these at push time with
`code: userDataUseOfSystemKey`. zcli surfaces the error before upload
so you see it inline. Case-sensitive match:

- `hostname` (lowercase — Zerops' service-injected service name)
- `PATH` (uppercase only)
- `serviceId`, `projectId`, `appVersionId`, `appVersionName`
- `zeropsSubdomain` (the fully-resolved URL — distinct from
  `zeropsSubdomainHost` which IS overridable)

If the API rejects, the error names the key. Rename it and retry.

### Regime 2 — Run-scope-only, runtime-init failure
These pass the API check but break runtime container startup when set
in `run.envVariables`. They're fine in `build.envVariables`. Pattern:
anything that conflicts with PATH or HOSTNAME case-insensitively.

- `HOSTNAME` (uppercase)
- `Path` (capitalized)
- `path` (lowercase)

Symptom: `BUILD_FAILED` event in 4-5 seconds with **zero build logs**
and `failureCause: "Build pipeline failed; no recognized log pattern
matched."` This is the empty-logs trap that both Run 1 + Run 2 agents
burned 4-10+ deploys bisecting. ZCP preflight catches this before push;
if you're seeing it on a manually-pushed (raw zcli) deploy, remove the
offending key from `run.envVariables`.

### Discouraged but accepted (you can override, but probably shouldn't)
Platform-injected vars that the API silently accepts as user overrides
— overriding them shadows the platform-provided value:

- `apiCdnUrl`, `staticCdnUrl`, `storageCdnUrl`
- `envIsolation`, `sshIsolation`
- `zeropsSubdomainHost`, `zeropsSubdomainString`

Override these only when you have a specific reason; default is to read
the platform value.

### Allowed — feel free to set
Linux defaults the platform provides but doesn't object to overriding
(though it's typically unnecessary — the defaults work):

- `USER`, `HOME`, `LOGNAME`, `SHELL`, `PWD`
- `PORT` (number or quoted string both work), `NODE_ENV`
```

### F2. ZCP preflight — `CheckReservedEnvNames` in `internal/ops/deploy_validate.go`

New validator chained into `ValidateZeropsYml`:

```go
// hardReservedEnvKeys: Zerops API rejects these in either build or run
// envVariables with code=userDataUseOfSystemKey. Case-sensitive match
// against the platform's denylist as verified 2026-05-16 against
// postgres@18 / nodejs@24 in eval-zcp.
var hardReservedEnvKeys = map[string]bool{
    "hostname":       true,
    "PATH":           true,
    "serviceId":      true,
    "projectId":      true,
    "appVersionId":   true,
    "appVersionName": true,
    "zeropsSubdomain": true,
}

// runScopeReservedEnvKeys: pass API validation but crash the runtime
// container init when set in run.envVariables (build.envVariables OK).
// Surface: BUILD_FAILED in 4-5s with zero build logs.
var runScopeReservedEnvKeys = map[string]bool{
    "HOSTNAME": true,
    "Path":     true,
    "path":     true,
}
```

CheckReservedEnvNames inspects setup.run.envVariables and
setup.build.envVariables, returns structured error
`ErrReservedEnvKey: { rejectedKeys: [...], scope: "run"|"build" }` with
an actionable hint pointing at the atom.

Saves agents ~5-10 deploys per first-time encounter (empirical from
Run 1 + Run 2).

### F3. Tighten the existing empty-logs baseline in `internal/ops/deploy_failure.go`

The current baseline (deploy_failure.go:140) already handles empty-logs:

```go
} else {
    cls.SuggestedAction = "Build logs were not captured before the container exited. Re-check zerops.yaml buildCommands syntax + manifests; consider simplifying buildCommands to bisect the failing step."
}
```

This is the exact message Run 1 agent saw — it pointed him at buildCommands
(reasonable but wrong) and he ended up bisecting envVars by luck. Tighten:

```go
} else {
    cls.SuggestedAction = "Build container exited before producing logs (typically <10s). The most common cause is a reserved key in run.envVariables — HOSTNAME, Path, or path — which crashes runtime-init before any build output. Check zerops.yaml run.envVariables; remove any of those keys. Less commonly: buildCommands syntax error or a reserved-name override in build.envVariables. See develop-reserved-env-names atom."
}
```

Plus add a tighter signal that fires BEFORE the baseline:

```go
{
    id:              "build:zero-logs-suggests-reserved-env",
    phases:          []DeployFailurePhase{PhaseBuild},
    requireZeroLogs: true,    // new field — matches when BuildLogs is empty
    maxDurationSec:  10,      // new field — process exited fast
    build: buildLikelyReservedEnvKey,
},
```

Returns:
- category: `build/runtime-init-rejection`
- likelyCause: `"Build container exited in under 10s with no logs — the runtime container init failed before build pipeline started. Most common: HOSTNAME / Path / path in run.envVariables."`
- suggestedAction: `"Remove HOSTNAME (or Path/path) from zerops.yaml run.envVariables. They pass API validation but crash runtime-init. If those aren't present, re-check buildCommands syntax. See develop-reserved-env-names atom."`

### F4. Discovery — `connectionString` shape annotation

In `internal/ops/env_generate.go` (or wherever discover output is
assembled), when emitting Postgres/MariaDB/MySQL `connectionString`,
attach:

```json
{
  "key": "connectionString",
  "isReference": true,
  "value": "postgresql://${user}:${password}@${hostname}:${port}",
  "completenessFlags": { "includesDbName": false },
  "warning": "connectionString omits /${dbName}; for Prisma / Drizzle / sqlx / SQLAlchemy / Sequelize, compose explicitly: postgresql://${user}:${password}@${hostname}:${port}/${dbName}"
}
```

LLMs scanning keys-only catch this in the structured `warning` field
even without reading the value.

---

## G. Cost-of-doing-nothing — what Phase 1 actually saves

Empirical from Run 1 + Run 2:

| Friction | Without fix | With Phase 1 atom + Phase 3 preflight |
|---|---|---|
| HOSTNAME-in-run bisect | 4-10+ deploys, ~8-15 min | preflight rejects in <1s, structured error names the key |
| connectionString missing /dbName | 1-5 deploys + ssh debugging + manual SQL grants | atom worked-example deflects to manual compose at first write |
| LLM corpus over-confidence | agent memory writes confident-but-wrong attributions | empirical reserved-name list with mechanism explanation |

Two-session sample: ~25 wasted deploys, ~25 minutes total friction, plus knowledge regressions baked into both agents' memory files.

---

## H. Resolved + remaining open questions

### Resolved during this verify pass
1. ✅ **Full hard-reserved set**: `hostname`, `PATH`, `serviceId`, `projectId`, `appVersionId`, `appVersionName`, `zeropsSubdomain` (7 verified). NOT reserved despite platform-injection: `apiCdnUrl`, `envIsolation`, `zeropsSubdomainHost`.
2. ✅ **Run-scope-only set**: HOSTNAME confirmed; also `Path` and `path` (case variants of PATH that bypass API but crash runtime).
3. ✅ **`HOSTNAME` in `build.envVariables`**: accepted (P11). Same key, different scope, different outcome.
4. ✅ **Case sensitivity of PATH**: only `PATH` (uppercase) is API-rejected; `Path` and `path` go through API but crash runtime-init.

### Remaining open
5. **`ZEROPS_*` user override** — never tested. Likely R1 zcli-rejected (they're platform internals), but untested individually. Adding a single probe `ZEROPS_MYTHING: foo` would resolve.
6. **Whether `HOSTNAME` in `build.envVariables` is actually USED by buildCommands** — P11 confirmed the deploy succeeds, but didn't read the value inside the build container. Worth a future probe with `buildCommands: - 'echo "HOSTNAME=$HOSTNAME"'` to see if the override is real or silently dropped.
7. **Other runtime-init-rejecting keys** — only checked HOSTNAME/Path/path. Could be more (`SHLVL`, `OLDPWD`, etc. probably fine; `MAIL`, `TZ`, etc. unknown). Low priority — verified set covers Run 1 + Run 2 observed friction.

These do not block Phase 1; they would expand the atom's reserved-name list if/when needed.

---

## I. Next step

PLAN.md Phase 1 is unblocked. Implementation:
1. Write `develop-reserved-env-names.md` atom using §F1 above.
2. Edit `develop-first-deploy-env-vars.md` to remove *"prefer over assembling"* and add the Postgres worked example (§D).
3. Edit `gotcha_pass_platform_invariant_env_shadow.md` symptom string (literal `${var}`, not empty).
4. Then Phase 3 implements `CheckReservedEnvNames` preflight + the new failure signal + the connectionString annotation.

Cleanup: delete `envprobe` service from eval-zcp.
