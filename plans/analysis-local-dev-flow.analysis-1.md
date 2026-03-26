# Implementation Plan: Local Development Flow for ZCP — Analysis 1

**Date**: 2026-03-26
**Agents**: kb (zerops-knowledge), verifier (platform-verifier), primary (Explore), adversarial (Explore)
**Complexity**: Deep (ultrathink, 4 agents)
**Task**: Design the local development flow where dev environment runs on user's local machine with ZCP + zcli + VPN to Zerops project

## Summary

Local dev mode is **feasible** with a clear architectural path. The codebase is already partially prepared (`EnvLocal` constant, `localEnvironment` system prompt, tests using `EnvLocal`). The main work is: (1) deploy redesign (local `zcli push` instead of SSH-based), (2) guidance branching for local paths, (3) env var bridge for local process, and (4) a fundamental design decision about whether "dev" services exist on Zerops in local mode.

**Evidence basis**: 17 VERIFIED, 5 LOGICAL, 2 UNVERIFIED

---

## Findings by Severity

### Critical

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| C1 | **Fundamental design question: should "dev" services exist on Zerops in local mode?** If the user runs the server locally, the Zerops dev service serves no purpose. This changes the entire bootstrap mode model. Options: (a) local-only mode (no dev service, just managed + optional stage), (b) local+stage mode (user's machine is dev, Zerops has stage), (c) local+dev+stage (keep current model, dev service mostly idle). | spec-bootstrap-deploy.md:4-5 explicitly defers local; bootstrap.md assumes dev service; MF1 from adversarial | Primary + Adversarial |
| C2 | **Deploy() is SSH-only and requires structural change** — not purely additive. `ops.Deploy()` takes `SSHDeployer` interface as required parameter. Local mode needs either a new `DeployLocal()` function or a refactored `Deploy()` with environment routing. The MEMORY.md reference to "deployLocal() now auto-runs zcli login" is aspirational, not implemented. | ops/deploy.go:60-95, tools/deploy.go:52-53 | Primary + Adversarial (CH1, CH2) |
| C3 | **Env vars NOT available via VPN** — the user's local process needs actual values, not `${hostname_varName}` references. Bridge exists: `zcli project env --export --service <name>` outputs `export KEY=VALUE`. ZCP must integrate this into the local workflow. | vpn.mdx:51, zcli commands.mdx:122-133 | KB + Verifier |

### Major

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| M1 | **zcli binary/auth validation missing** — local mode depends on `zcli` being installed, in PATH, authenticated, and compatible. No detection or validation exists. | No grep matches for exec.Command+zcli in codebase | Adversarial (MF3) |
| M2 | **ValidateZeropsYml silently skips in local mode** — deploy.go:140-142 checks `os.Stat(mountPath)` where mountPath is SSHFS path. In local mode, this path never exists, so validation warnings are silently lost. | ops/deploy.go:140-142 | Adversarial (MF5) |
| M3 | **workingDir semantics change** — deploy tool schema says "Container path for deploy. Default: /var/www". In local mode, workingDir is user's project directory (CWD). Deploy tool needs env-aware defaults and description. | tools/deploy.go:21, ops/deploy.go:124-135 | Adversarial (MF6) |
| M4 | **VPN lifecycle not managed** — local mode assumes VPN is active for managed service access. No VPN state detection, no guidance on when to connect, no clear error when VPN is down. | No VPN-related code in ZCP codebase | Adversarial (MF4) |
| M5 | **Hostname format for VPN** — live-tested 2026-03-26: both `hostname` and `hostname.zerops` resolve and connect. macOS DNS search domain `.zerops` makes plain hostname work. `dig`/`nslookup` don't use system resolver (false negatives), but `dscacheutil` and actual TCP connections work. Internal `db_hostname` env var contains VXLAN address (e.g., `evdt433f95`), NOT user-facing hostname — cannot be used for VPN connections. | Live VPN test on macOS, vpn.mdx:86-93 | Verifier + Live test |

### Minor

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| m1 | **Mount tool already guarded** — tools/mount.go returns error if mounter==nil. In local mode, no SSHFS mounter is configured, so mount calls fail gracefully. Adversarial confirms existing null-check is sufficient. | tools/mount.go:29-34 | Adversarial (CH3) |
| m2 | **`zcli push` uses --service-id (ID), not hostname** — ZCP resolves hostname→ID via API, so needs to pass service ID to zcli. | zcli commands.mdx:206-216 | Verifier |
| m3 | **Valkey TLS ports (6380, 7001)** are documented as "for external/VPN only" — local apps connecting via VPN may need TLS port guidance. | Verifier finding | Verifier |

---

## Adversarial Challenges — Resolution

### CH4 (env var resolution in cross-deploy): REJECTED
The adversarial analyst claimed `${hostname_varName}` references break in local→stage cross-deploy. This is **wrong**. Per arch_env_var_two_tier.md: "Zerops resolves env var references at container runtime." When `zcli push` sends zerops.yml to Zerops (from ANY source — local or container), the deployed container resolves `${db_connectionString}` correctly. The adversarial confused "resolved at container level" with "only works when pushed from a container."

The REAL env var concern is C3: the LOCAL process needs actual values to run. This is a separate problem from deploy, and is addressed by the env var bridge.

### CH1 (deployLocal doesn't exist): CONFIRMED
MEMORY.md reference is aspirational. No implementation exists.

### CH2 (not purely additive): CONFIRMED
Deploy() signature requires SSHDeployer. Adding local mode requires structural change.

### CH3 (mount guard): CONFIRMED as already handled
Existing null-check guard is sufficient. No new code needed.

---

## Recommended Architecture

### The Fundamental Decision: Local Mode Topology

**Recommended: Option B — Local + Stage**

| Topology | Description | Complexity | Use Case |
|----------|-------------|------------|----------|
| A. Local-only | No Zerops runtime services; only managed (DB, cache) | LOW | Prototyping, managed services only |
| **B. Local + Stage** | User's machine = dev, Zerops has stage for validation | MEDIUM | **Most common local dev pattern** |
| C. Local + Dev + Stage | Keep dev service on Zerops (mostly idle) | HIGH | Overkill for local mode |

**Rationale**: In local mode, the user IS the dev server. Creating a Zerops "dev" service that runs `zsc noop --silent` adds no value. The natural flow is: develop locally → push to stage (or single service) for validation.

**This maps to existing modes**:
- **Local Standard**: User develops locally, pushes to `{name}stage` on Zerops for validation. Managed services on Zerops, accessible via VPN.
- **Local Simple**: User develops locally, pushes to `{name}` on Zerops. Single service.
- **Local Dev-only**: User only uses managed services on Zerops. No runtime service needed. Pure local development.

### Bootstrap Flow Changes

| Step | Container Mode | Local Mode | Delta |
|------|---------------|-----------|-------|
| **Discover** | API-based, choose mode | API-based, choose mode | NONE — identical |
| **Provision** | Import services + SSHFS mount + env discovery | Import services + env discovery (NO mount) | MINOR — skip mount, skip dev service creation in local-standard |
| **Generate** | Write to SSHFS at `/var/www/{hostname}/` | Write to local CWD | GUIDANCE ONLY — agent uses local paths |
| **Deploy** | SSH → container → zcli push | Direct `zcli push` from local | MAJOR — new deploy function |
| **Close** | ServiceMeta write | ServiceMeta write | NONE — identical |

### Deploy Redesign

```
Current (container):
  Agent → zerops_deploy → ops.Deploy() → SSHDeployer.ExecSSH() → zcli push inside container

Proposed (local):
  Agent → zerops_deploy → ops.Deploy() → ops.deployLocal() → exec.Command("zcli", "push", ...) from CWD

Routing:
  ops.Deploy() checks: sshDeployer != nil → deploySSH()
                        sshDeployer == nil → deployLocal() [NEW]
```

**Key insight**: sshDeployer is already nil in local mode (server.go doesn't create one when !rtInfo.InContainer). The existing nil check at deploy.go:71-76 currently returns an error — change it to route to deployLocal().

### Env Var Bridge Design

Two distinct problems:
1. **For zerops.yml references** (deployed to Zerops): `${hostname_varName}` works as-is — Zerops resolves at container runtime. No change needed.
2. **For local process** (running on user's machine): Need actual values.

**Recommended approach**:
```
Step 1: zerops_discover includeEnvs=true → returns actual values
Step 2: ZCP generates env var export commands or .env content
Step 3: Guidance tells agent to either:
  a) Generate .env file in project dir (frameworks that use dotenv)
  b) Output export commands for user to source
  c) Pass env vars when starting local dev server process
```

The `zcli project env --export --service <name>` command is available but NOT YET integrated into ZCP. ZCP can replicate this via its existing API access (zerops_discover already returns the values).

### Verification in Local Mode

| Target | Container Mode | Local Mode |
|--------|---------------|-----------|
| Local dev server | N/A | `http://localhost:{port}/health` — check directly |
| Stage on Zerops | Zerops subdomain | Zerops subdomain — same as container mode |
| Managed services | API status check | API status check — same |

**Implementation**: Add localhost health check path to `ops/verify_checks.go`. Parse port from zerops.yml `run.ports` config.

---

## Implementation Phases

### Phase 1: Deploy Foundation (P0)
**Files**: ops/deploy.go, tools/deploy.go
**Scope**: ~200L new code + ~150L tests

1. Add `deployLocal()` function to ops/deploy.go
   - Validate zcli binary exists in PATH
   - Validate zerops.yml exists in workingDir
   - Run `zcli login <token>` + `zcli push --serviceId <id>` via exec.Command
   - Parse output, return same DeployResult type
   - Error handling: zcli not found, auth failed, push failed
2. Change Deploy() nil-SSHDeployer guard from error → route to deployLocal()
3. Update tools/deploy.go description for local context
4. Add workingDir default logic: container → `/var/www`, local → CWD

**Testing**: TDD — write TestDeployLocal_Success, TestDeployLocal_MissingZcli, TestDeployLocal_MissingZeropsYml, TestDeployLocal_AuthFailed first

### Phase 2: Guidance Branching (P1)
**Files**: deploy_guidance.go, bootstrap_guidance.go, bootstrap.md, deploy.md
**Scope**: ~200L content changes

1. Extend env branching in deploy_guidance.go (currently only lines 56-63)
   - Local mode: show CWD paths, zcli push commands, env var export guidance
   - Remove SSHFS/SSH references when env == EnvLocal
2. Add env-conditional sections to bootstrap.md
   - Provision: skip mount guidance for local
   - Generate: local dir paths instead of SSHFS
   - Deploy: zcli push instead of SSH deploy
3. Add local-specific sections to deploy.md
   - Local dev server lifecycle (user manages)
   - Env var export before starting

### Phase 3: Env Var Bridge + Validation (P1)
**Files**: ops/env.go or new ops/env_local.go, workflow/validate.go
**Scope**: ~150L + ~100L tests

1. Add env var export function: takes discovered env vars, outputs .env format or shell exports
2. Add zerops.yml validation for local mode (fix silent skip from MF2)
3. Add env var reference validation against discovered vars

### Phase 4: Local Verification (P2)
**Files**: ops/verify_checks.go
**Scope**: ~80L + ~60L tests

1. Add localhost health check for local dev server
2. Parse port from zerops.yml
3. Branch in verify: local dev → localhost, stage → Zerops subdomain

### Phase 5: Spec + Knowledge Update (P2)
**Files**: docs/spec-bootstrap-deploy.md, knowledge guides
**Scope**: ~200L documentation

1. Add local mode section to spec
2. Add VPN + local development knowledge guide
3. Update bootstrap.md with local topology options

---

## Risk Assessment

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|------------|--------|-----------|
| R1 | zcli push invocation via exec.Command: output parsing, error handling | MEDIUM | HIGH | Copy error handling pattern from SSH deploy; test with mock executor |
| R2 | VPN not active when user tries to access managed services | HIGH | MEDIUM | Add VPN status check in guidance; clear error message suggesting `zcli vpn up` |
| R3 | Hostname resolution over VPN (plain vs .zerops suffix) | MEDIUM | MEDIUM | Always use `hostname.zerops` in guidance and generated configs |
| R4 | Dev service purpose confusion — user creates unnecessary dev service in local mode | MEDIUM | LOW | Clear guidance: local mode = your machine is dev, push to stage |
| R5 | zerops.yml validation silently skips in local mode | HIGH | MEDIUM | Fix ValidateZeropsYml to accept explicit path parameter, not hardcoded SSHFS path |

---

## Evidence Map

| Finding | Confidence | Basis |
|---------|------------|-------|
| EnvLocal exists, unused in production code | VERIFIED | environment.go:8-10, grep confirms |
| Deploy is SSH-only | VERIFIED | ops/deploy.go:54-212 |
| Env vars not available via VPN | VERIFIED | vpn.mdx:51, verifier confirmed |
| `zcli project env --export` exists | VERIFIED | zcli commands.mdx:122-133 |
| `zcli push` works from local | VERIFIED | zcli commands.mdx:203-229 |
| VPN uses hostname.zerops format | VERIFIED | vpn.mdx:86-93, verifier confirmed |
| ${hostname_varName} resolved at container runtime | VERIFIED | arch_env_var_two_tier.md, memory |
| localEnvironment system prompt exists | VERIFIED | server/instructions.go:36-45 |
| Mount nil-guard already exists | VERIFIED | adversarial confirmed |
| deployLocal() does NOT exist | VERIFIED | adversarial CH1, grep confirms |
| sshDeployer is nil in local mode | LOGICAL | server.go doesn't create SSHDeployer when !InContainer |
| Local mode doesn't need dev service on Zerops | LOGICAL | dev service runs zsc noop, local mode runs server locally |
| zcli push uses --service-id (not hostname) | VERIFIED | verifier, zcli commands.mdx:215 |
| Internal hostname env var (VXLAN) differs from service hostname | VERIFIED | verifier live test |
| ValidateZeropsYml silently skips in local mode | LOGICAL | deploy.go:140-142 checks SSHFS path |
| Dev service purpose undefined for local mode | VERIFIED | spec-bootstrap-deploy.md:4-5 defers |
| All tests already use EnvLocal | VERIFIED | grep confirms 70+ uses |

---

## Adversarial Challenges

| Challenge | Verdict | Resolution |
|-----------|---------|-----------|
| CH1: deployLocal() doesn't exist | **CONFIRMED** | MEMORY.md line is aspirational, not implemented |
| CH2: Deploy() refactor not additive | **CONFIRMED** | SSHDeployer parameter requires routing change |
| CH3: Mount guard is 5 lines | **PARTIALLY CONFIRMED** | Guard exists via nil-check, no new code needed |
| CH4: Env var refs break in cross-deploy | **REJECTED** | Zerops resolves at container runtime regardless of push source |
| MF1: Dev service necessity | **CONFIRMED — CRITICAL** | Fundamental design decision required |
| MF2: Local env discovery flow | **PARTIALLY CONFIRMED** | Valid for local-only managed services scenario |
| MF3: zcli binary detection | **CONFIRMED** | Must validate zcli availability |
| MF4: VPN lifecycle | **CONFIRMED** | Guidance needed, not code |
| MF5: ValidateZeropsYml skip | **CONFIRMED** | Silent validation loss |
| MF6: workingDir assumptions | **CONFIRMED** | Default must be CWD in local mode |
| MF7: Service type immutability | **CONFIRMED — MINOR** | Documentation issue |

---

## Key Design Decisions Required

1. **Should local mode create dev services on Zerops?** Recommendation: NO for standard local, YES only if user explicitly wants Zerops-native dev.
2. **How should env vars reach local process?** Recommendation: ZCP generates .env file + shell exports from discovered values.
3. **Should ZCP manage the local dev server process?** Recommendation: NO for Phase 1 — guidance only. Phase 2: optional `zerops_dev` tool.
4. **VPN management?** Recommendation: Guidance only (tell user to run `zcli vpn up`), not automated.
