# Workflow Gaps: Post-v1 Backlog

**Status**: Reference for post-v1 work. Architecture and deploy SSH mode are now in `zcp-prd.md`.
**When to revisit**: After v1 is complete and stable.

---

## GAP 2: Recipe/Template API

**Severity**: LOW-MEDIUM

Main branch's `recipe-search.sh` (1690 lines) fetches from external Zerops APIs:
- `stage-vega.zerops.dev` for recipe list
- `api-d89-1337.prg1.zerops.app` for full recipe data

**v1 mitigation**: Knowledge base BM25 search (65+ docs) substitutes for recipe discovery.

**v2+ candidate**: A `zerops_recipes` tool could provide live recipe search and import for bootstrap workflows. Evaluate based on actual usage patterns.

---

## GAP 3: Service Status Granularity

**Severity**: LOW — implementation note for v1

The main branch workflow distinguishes: `CREATING`, `READY_TO_DEPLOY`, `BUILDING`, `RUNNING`, `STOPPED`.

Key detail: `READY_TO_DEPLOY` indicates a runtime service imported but never deployed. Without `buildFromGit` or `startWithoutCode: true` in the import YAML, services get stuck in this state.

**Action**: During v1 implementation, verify `ServiceStack.Status` enum from zerops-go SDK. PRD section 4.2 has the requirement.

---

## GAP 4: Validate Tool Semantic Depth

**Severity**: LOW — post-v1

v1 `zerops_validate` does offline YAML syntax validation. The main branch's Gate 0.5 performs deeper checks:

1. **Runtime services MUST have `buildFromGit` or `startWithoutCode: true`** — otherwise stuck in `READY_TO_DEPLOY`
2. **Runtime services MUST have `zeropsSetup`** — omitting causes build failures
3. **Database/cache services MUST have `mode: NON_HA` or `mode: HA`** — omitting passes dry-run but fails real import

These checks require knowledge of service type categories. The knowledge base has this info — the validate tool could cross-reference.

---

## GAP 5: Build Monitoring via Events

**Severity**: LOW — implementation note for v1

After `zcli push`, the build result comes via the events API. Requirements:
- Build status: success, failed, canceled
- Build errors: error message on failure
- Build duration: push to completion
- Service filter: track a specific service's build

Main branch polling pattern:
```
status.sh --wait:
  Interval: 5s, Timeout: 300s
  Checks: zcli project processes (BUILDING/PENDING) + notifications (SUCCESS/ERROR)
  Exit: SUCCESS → 0, ERROR → 1, Timeout → 2
```

**Action**: During v1, verify `zerops_events` / `SearchAppVersions` returns enough build detail. Document gaps for v2.

---

## Capability Mapping: Main Branch → v2

| Capability | Main Branch | v2 Primitive | Status |
|---|---|---|---|
| Service discovery | `zcli service list` | `zerops_discover` | OK |
| Service lifecycle | `zcli start/stop` | `zerops_manage` | OK |
| Env vars (configured) | `zcli env` | `zerops_env` | OK |
| Env vars (runtime) | `ssh svc "echo $var"` | Agent bash SSH | OK |
| Log access (API) | `zcli service log` | `zerops_logs` | OK |
| Log access (live) | `ssh svc "tail /tmp/app.log"` | Agent bash SSH | OK |
| Deploy (push code) | SSH + `zcli push` | `zerops_deploy` (SSH mode) | OK |
| Deploy (monitor build) | `status.sh --wait` | `zerops_events` / `zerops_process` | Verify depth (GAP 5) |
| Import services | `zcli service-import` | `zerops_import` | OK |
| YAML validation | Gate scripts | `zerops_validate` | Needs depth (GAP 4) |
| Knowledge/docs | N/A | `zerops_knowledge` | OK |
| Process tracking | Polling | `zerops_process` + progress | OK |
| Activity timeline | N/A | `zerops_events` | OK |
| Subdomain | N/A | `zerops_subdomain` | OK |
| Delete | N/A | `zerops_delete` | OK |
| Scale | `zsc scale` | `zerops_manage` (scale) | OK |
| SSHFS mounts | `sshfs svc:/var/www` | Agent bash | OK |
| SSH exec | `ssh svc "cmd"` | Agent bash | OK |
| Connectivity testing | SSH + DNS/TCP/HTTP | Agent bash | OK |
| Recipe search | External Zerops APIs | N/A | v2+ (GAP 2) |
| Phase/state/evidence | File-based JSON | Workflow layer (CLAUDE.md) | By design |
