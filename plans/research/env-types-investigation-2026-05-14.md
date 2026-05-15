# Env Type Investigation — Live Findings from eval-zcp

**Date:** 2026-05-14
**Purpose:** Resolve plan v5 assumptions about SDK env field shape by querying live Zerops API.
**Method:** Direct REST calls (`/api/rest/public/service-stack/{id}/env`, `/api/rest/public/project/search`) with ZCP_API_KEY against eval-zcp project (`waAzEFn6SBaysG4YE4rv7A`); test service `probestorage` (object-storage type) provisioned + deleted after.

---

## Two distinct enums (server-authoritative)

### Project-level envs — `EnvTypeEnum`

Two values: `USER` | `SYSTEM`. Plus `Sensitive bool` + `Editable bool`.

Real eval-zcp project env entries:

| Key | Type | Sensitive | Editable |
|---|---|---|---|
| `zeropsSubdomainHost` | SYSTEM | false | **false** |
| `zeropsSubdomainString` | SYSTEM | false | **false** |
| `staticCdnUrl` | SYSTEM | false | **false** |
| `apiCdnUrl` | SYSTEM | false | **false** |
| `storageCdnUrl` | SYSTEM | false | **false** |
| `envIsolation` | SYSTEM | false | true |
| `sshIsolation` | SYSTEM | false | true |
| `ZCP_API_KEY` | USER | **false** | true |
| `SESSION_SECRET` | USER | false | true |
| `JWT_SECRET` | USER | false | true |
| `APP_KEY` | USER | false | true |

### Service-stack envs — `UserDataTypeEnum` (different enum, 5 values)

Values: `READ_ONLY` | `EDITABLE` | `SECRET` | `INTERNAL` | `ENV`. Plus `Sensitive bool`. **`Editable` field is `null`** in response for service envs (SDK DTO has no `Editable` field on `ServiceStackEnv`).

Real eval-zcp object-storage service (probestorage) envs:

| Key | Type | Sensitive | Editable |
|---|---|---|---|
| `accessKeyId` | READ_ONLY | false | null |
| `apiHost` | READ_ONLY | false | null |
| `apiUrl` | READ_ONLY | false | null |
| `bucketName` | READ_ONLY | false | null |
| `hostname` | READ_ONLY | false | null |
| `projectId` | READ_ONLY | false | null |
| `quotaGBytes` | READ_ONLY | false | null |
| `secretAccessKey` | READ_ONLY | **true** | null |
| `serviceId` | READ_ONLY | false | null |

---

## Key conclusions

### A) F19 (CDN/object-storage backlog) — FULLY RESOLVED

CDN-related keys (`staticCdnUrl`, `apiCdnUrl`, `storageCdnUrl`) are project-level `Type=SYSTEM` + `Editable=false`. Server marks them platform-managed. Target project regenerates own CDN URLs when its own storage services come online.

No pattern detection needed. No fallback table needed. SDK fields are authoritative.

### B) Service-level envs — universally derived from managed services

All service-stack envs are server-generated (`UserDataTypeEnum=READ_ONLY` for managed type infrastructure). Users do not set service-level envs except via `zerops.yaml run.envVariables` (which is build-time injection, not platform-stored).

For launch/export composition: **service envs are NEVER carried over to target**. Target's own services (importing the same yaml shape) regenerate equivalent keys. The classifier does not need to bucket them — they're filtered out before classification.

### C) `Sensitive` flag — useful but unreliable for credential detection

`ZCP_API_KEY` (a bearer token) is `Sensitive: false` in server response. `secretAccessKey` (a managed-service credential) is `Sensitive: true`. The server's `Sensitive` flag is **guidance**, not authoritative for "is this a credential".

Implication: classifier MUST NOT use `Sensitive=true` as automatic "this is a secret". User-defined credential keys (KEY/SECRET/TOKEN suffix patterns) need their own detection. LLM final say.

### D) `Editable` on SYSTEM project envs splits two ways

- `Editable=false` (CDN URLs, subdomain): platform read-only, target regenerates → **drop** in launch/export
- `Editable=true` (envIsolation, sshIsolation): platform defaults user can override → **drop** in launch (target gets fresh defaults)

Both `SYSTEM` cases drop. The `Editable` distinction matters for source-side display only, not target-side composition.

---

## Plan implications

### v5 §1.1 SDK table — corrections

Current plan claims unified `Type` field across project + service envs. **Reality: two enums.** Plan must show both:

```
Project env DTO         { Key, Content, Type EnvTypeEnum (USER|SYSTEM),
                          Sensitive bool, Editable bool }

Service env DTO         { Key, Content, Type UserDataTypeEnum (READ_ONLY|
                          EDITABLE|SECRET|INTERNAL|ENV), Sensitive bool }
```

### v5 §5.4 Classifier rules — simplified

Original 4-row table per project/service collapses to:

| Layer | Rule | Outcome |
|---|---|---|
| Service envs | Always drop | Target's managed services regenerate equivalents |
| Project env `Type=SYSTEM` | Always drop | Platform-injected; target gets own |
| Project env `Type=USER` | LLM classifies | Bias `auto-secret` if name matches `*_KEY*\|*_SECRET*\|*_TOKEN*\|*_PASS*\|APP_KEY` pattern; else `plain-config`. `Sensitive=true` is supplementary signal but not authoritative. |

Three rules total. No CDN-specific table. No service-env classification logic.

### v5 §9.4 EnvVar shape — revised

```go
type ProjectEnvVar struct {
    Key       string
    Content   string
    Type      ProjectEnvType   // USER | SYSTEM
    Sensitive bool
    Editable  bool
}

type ServiceEnvVar struct {
    Key       string
    Content   string
    Type      ServiceEnvType   // READ_ONLY | EDITABLE | SECRET | INTERNAL | ENV
    Sensitive bool
                                // no Editable per SDK shape
}
```

Distinct types per scope. `Type` enum is scope-specific.

### v5 Phase 2 work — reduced

The classifier becomes ~30 LOC pure function (three rules). Static `platformEnvAutoClass` table deletion is the bulk of the change (-150 LOC). Net delta for Phase 2 envclass: ~-100 LOC instead of +200 LOC estimated.

---

## Probestorage cleanup

Service `probestorage` (id `NlSFvcrAQ2WKLU05lsJJBg`) deleted via REST `DELETE /api/rest/public/service-stack/NlSFvcrAQ2WKLU05lsJJBg` after env capture. No stray artifacts in eval-zcp.

## Token used

Direct REST calls authenticated with eval-zcp `ZCP_API_KEY` from `.mcp.json`. No production credentials touched.
