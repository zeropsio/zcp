# Workflow Gaps: Post-v1 Expansion Items

**Status**: Reference document. All items are **NOT for v1 implementation**.
**Source**: Gap analysis between main branch bash workflow (~15k lines) and v2 PRD (12 MCP tools).
**When to revisit**: After v1 is complete and stable.

---

## Architecture Context

The ZCP MCP server runs ON a Zerops service within the project (or connected via VPN during development). It has SSH access to all sibling services on the VXLAN private network. The agent (Claude Code) has native bash for SSH/SSHFS operations.

```
Workflow Layer (CLAUDE.md + MCP instructions)
  Phases, gates, evidence, iterations, subagent spawning
MCP Tool Layer (12 tools)
  API ops: discover, manage, env, logs, deploy, import,
  validate, knowledge, process, delete, subdomain, events
Agent Bash Layer (native SSH/SSHFS)
  Container exec, mount filesystems, runtime env vars,
  connectivity testing, live log tailing
Platform (Zerops API + VXLAN network)
```

This separation is correct. Main branch collapses everything into bash. v2 separates:
- **API operations** into MCP tools (structured, typed, error-coded)
- **Container interaction** into agent bash SSH (flexible, direct)
- **Workflow logic** into LLM instructions (adaptive, context-aware)

---

## GAP 1: Deploy Tool SSH Mode

**Severity**: HIGH
**PRD section affected**: `design/zcp-prd.md` section 8

### Problem

The v1 PRD designs `zerops_deploy` as a local `zcli push` wrapper. It assumes code is on the local filesystem with macOS-specific paths.

In the actual workflow, code lives ON a dev container (via SSHFS mount). `zcli push` must run FROM INSIDE that container.

### Main Branch Deploy Chain

```
1. deploy.sh reads discovery.json (dev-to-stage service pairs)
2. For each service:
   ssh dev-svc "cd /var/www && zcli login $token && zcli push $stage_id --setup=prod --deploy-git-folder"
3. status.sh --wait $stage_name
   Polls every 5s (300s timeout):
   - checks zcli project processes (BUILDING/PENDING for this service)
   - checks zcli project notifications (SUCCESS/ERROR after start timestamp)
4. On success: writes deploy_evidence.json
5. Gate 3 reads deploy_evidence.json for DEPLOY-to-VERIFY transition
```

### Proposed Expansion: Dual-Mode Deploy

The tool should support SSH-based push mode as primary, with local push as fallback.

**New parameters:**
```
zerops_deploy:
  # SSH mode (primary):
  sourceService: string     # hostname of dev container (SSH target)
  targetServiceId: string   # service ID to push to (stage)
  setup: string             # "dev" or "prod" (zerops.yml setup name)
  workingDir: string        # path inside container (default: /var/www)

  # Local mode (fallback, current v1 behavior):
  workingDir: string        # local directory with zerops.yml
  serviceId: string         # target service ID
```

**SSH mode implementation:**
1. SSH into `sourceService`
2. Authenticate zcli (using the token ZCP already has)
3. Run `zcli push $targetServiceId --setup=$setup --deploy-git-folder`
4. `zcli push` initiates upload + triggers build pipeline, then returns
5. Use `zerops_events` (serviceHostname filter) to poll for build completion
6. Return: `{status, buildDuration, serviceId, processId}`

**Build monitoring**: After `zcli push` returns, the agent uses `zerops_events` to track the build result. The existing progress notification system (PRD section 6) can be adapted. Although `zcli push` does not return a process ID, the events API shows build status per service.

---

## GAP 2: Recipe/Template API

**Severity**: LOW-MEDIUM
**PRD section affected**: None (new tool candidate)

### Problem

Main branch's `recipe-search.sh` (1690 lines) fetches from external Zerops APIs:
- `stage-vega.zerops.dev` for recipe list
- `api-d89-1337.prg1.zerops.app` for full recipe data

### Current Mitigation

The knowledge base BM25 search (65+ docs) substitutes for recipe discovery in v1.

### Future Candidate

A `zerops_recipes` tool could provide live recipe search and import for bootstrap workflows. Evaluate after v1 based on actual usage patterns.

---

## GAP 3: Service Status Granularity

**Severity**: LOW
**PRD section affected**: `design/zcp-prd.md` section 4.2 (types)

### Problem

The main branch workflow distinguishes between service states: `CREATING`, `READY_TO_DEPLOY`, `BUILDING`, `RUNNING`, `STOPPED`. The v1 PRD ports `ServiceStack.Status` from zerops-go but does not enumerate the full status set.

### Action

During v1 implementation, verify that `ServiceStack.Status` from the zerops-go SDK returns the full enum. If the SDK normalizes or omits intermediate states (like `READY_TO_DEPLOY`), document the gap and consider adding raw status passthrough.

Key status for workflow gating: `READY_TO_DEPLOY` indicates a runtime service that has been imported but never deployed. Without `buildFromGit` or `startWithoutCode: true` in the import YAML, services get stuck in this state.

---

## GAP 4: Validate Tool Semantic Depth

**Severity**: LOW
**PRD section affected**: `design/zcp-prd.md` section 5.1 (zerops_validate)

### Problem

The v1 `zerops_validate` tool does offline YAML syntax validation. The main branch's Gate 0.5 performs deeper semantic checks that prevent common deployment failures.

### Semantic Checks to Add Post-v1

For runtime services in import YAML:
1. **MUST have `buildFromGit` or `startWithoutCode: true`** — without these, services get stuck in `READY_TO_DEPLOY` and never transition to `RUNNING`
2. **MUST have `zeropsSetup`** — required for deploy configuration; omitting it causes build failures
3. **Database/cache services MUST have `mode: NON_HA` or `mode: HA`** — omitting passes dry-run validation but fails real import

### Implementation Notes

These checks require knowledge of service type categories (runtime vs database vs cache). The knowledge base already has this information. The validate tool could cross-reference service types against required fields.

---

## GAP 5: Build Monitoring via Events

**Severity**: LOW
**PRD section affected**: `design/zcp-prd.md` section 5.1 (zerops_events)

### Problem

After `zcli push` returns, the build/deploy result comes via the events API. The agent needs to poll `zerops_events` to confirm build success before the workflow proceeds. The v1 `zerops_events` tool merges process + appVersion timelines but does not specifically optimize for build-result tracking.

### Requirements for Build Monitoring

The `zerops_events` response should include sufficient build detail:
- **Build status**: success, failed, canceled
- **Build errors**: error message when build fails
- **Build duration**: time from push to completion
- **Service filter**: must work reliably to track a specific service's build

### Main Branch Polling Pattern

```
status.sh --wait:
  Interval: 5s
  Timeout: 300s (5 minutes)
  Checks:
    1. zcli project processes — looks for BUILDING/PENDING for this service
    2. zcli project notifications — looks for SUCCESS/ERROR after start timestamp
  Exit conditions:
    - SUCCESS notification found -> return 0
    - ERROR notification found -> return 1
    - Timeout exceeded -> return 2
```

### Action

During v1 implementation, verify that `zerops_events` returns enough build detail from `SearchAppVersions`. If build error messages are not available through this API, investigate alternative endpoints. Document any gaps for v2 enhancement.

---

## Capability Mapping Reference

Complete mapping between main branch capabilities and v2 primitives:

| Capability | Main Branch | v2 Primitive | Status |
|---|---|---|---|
| Service discovery | `zcli service list` | `zerops_discover` | OK |
| Service lifecycle | `zcli start/stop` | `zerops_manage` | OK |
| Env vars (configured) | `zcli env` | `zerops_env` | OK |
| Env vars (runtime-injected) | `ssh svc "echo $var"` | Agent bash SSH | OK |
| Log access (API) | `zcli service log` | `zerops_logs` | OK |
| Log access (live) | `ssh svc "tail /tmp/app.log"` | Agent bash SSH | OK |
| Deploy (push code) | SSH + `zcli push` from container | `zerops_deploy` | Needs SSH mode (GAP 1) |
| Deploy (monitor build) | `status.sh --wait` polling | `zerops_events` / `zerops_process` | Verify depth (GAP 5) |
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
| Recipe search | External Zerops APIs | N/A | v2+ candidate (GAP 2) |
| Phase/state/evidence | File-based JSON | Workflow layer (CLAUDE.md) | By design |

---

## Summary

| Gap | Severity | Action | Timeline |
|---|---|---|---|
| Deploy tool SSH mode | HIGH | Expand PRD section 8 with dual-mode | Post-v1, before workflow layer |
| Recipe API | LOW-MEDIUM | Evaluate `zerops_recipes` tool | v2+ based on usage |
| Service status granularity | LOW | Verify SDK enum during implementation | v1 implementation note |
| Validate semantic depth | LOW | Add Gate 0.5 checks to validate tool | Post-v1 |
| Build monitoring via events | LOW | Verify events API build detail | v1 implementation note |

**Bottom line**: The 12 MCP tools are the correct set of primitives. The architecture is well-aligned. GAP 1 (deploy SSH mode) is the only high-severity item and should be addressed before building the workflow layer on top.
