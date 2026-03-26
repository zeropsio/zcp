# Review Report: plan-local-dev-flow.md — Review 1
**Date**: 2026-03-26
**Reviewed version**: plans/plan-local-dev-flow.md
**Agents**: kb (zerops-knowledge), verifier (platform-verifier), primary (correctness+completeness), adversarial
**Complexity**: Deep (ultrathink, 4 agents)
**Focus**: Verify every claim against codebase and platform, find fundamental + surface problems, root causes, design-correct fixes

## Evidence Summary
| Agent | Findings | Verified | Unverified | Downgrades |
|-------|----------|----------|------------|------------|
| KB | 11 claims checked | 9 confirmed | 2 (S3, sudo) | 0 |
| Verifier | 8 claims tested | 6 confirmed | 1 partial (S3) | 1 (--serviceId refuted) |
| Primary | 8 findings | 7 verified | 1 (S3) | 0 |
| Adversarial | 5 challenges | 3 confirmed primary | 1 missed finding added | 1 partial (C2 nuanced) |

**Overall**: CONCERNS — architecturally sound but 3 critical implementation gaps block local mode from working

---

## Knowledge Base

### Zerops Platform Facts (verified)
- VPN provides network + DNS only, NOT env vars
- Both `hostname` and `hostname.zerops` work over VPN (DNS search domain)
- `${hostname_varName}` resolved at container level, not API
- Deploy = new container, local files lost
- zcli push: positional arg `zcli push <hostname-or-id>`, flag `--service-id` (kebab-case), `--working-dir`, `--setup` (monorepo)
- Integration tokens: three scope levels (full, read, custom per-project)
- VPN sudo: "may be required" for first-time daemon install only

### Partially Verified
- S3 apiUrl "accessible remotely over internet" per docs, but TLS handshake failed at `storage.app-prg1.zerops.io:443` from local machine. Per-service apiUrl may differ. Needs E2E verification with real object-storage service.

---

## Agent Reports

### Primary Analyst — Correctness + Completeness

**Assessment**: CONCERNS
**Evidence basis**: 7 of 8 findings VERIFIED

#### Critical Findings

**[C1] Deploy tool never registers in local mode** — CRITICAL
- Plan line 349: "Remove `if s.sshDeployer != nil` guard from RegisterDeploy"
- Actual code `server.go:108-110`: Guard still exists, sshDeployer=nil in local mode
- `ops/deploy.go:71-77`: Deploy() returns error for nil sshDeployer
- `tools/deploy.go:26-34`: RegisterDeploy signature requires sshDeployer parameter
- **Impact**: In local mode, agent never sees `zerops_deploy` tool. Deploy is impossible.
- **Root cause**: Plan correctly identifies the change but describes it as Phase 0. The change is a hard prerequisite — without it, nothing else works.

**[C2] ValidateZeropsYml path coupling** — CRITICAL (nuanced by adversarial)
- Plan line 326: "Run ValidateZeropsYml on workingDir"
- Actual `ops/deploy.go:140-142`: Called with SSHFS mount path `/var/www/{sourceService}`
- Function signature (`deploy_validate.go:15`) already accepts workingDir parameter — API is correct
- But in local mode, Deploy() errors before reaching validation (blocked by C1)
- **Impact**: Even after C1 is fixed, deployLocal() must explicitly call ValidateZeropsYml with correct local path
- **Root cause**: Not a function API problem but a call-site coupling problem

**[C3] ServiceMeta writes wrong hostname in local mode** — CRITICAL
- Plan line 119: "Local: ServiceMeta hostname=appstage"
- Actual `bootstrap_outputs.go:23`: `hostname := target.Runtime.DevHostname` — unconditional
- `engine.go:20`: Engine has `environment` field but writeBootstrapOutputs never uses it
- **Impact**: Local bootstrap writes `{hostname: "appdev"}` even when no dev service exists on Zerops
- **Root cause**: writeBootstrapOutputs has no environment awareness. It always writes DevHostname regardless of whether the dev service was created.
- **Design fix**: Either (a) engine.Provision() removes dev targets from plan in local mode, OR (b) writeBootstrapOutputs branches on environment.

#### Major Findings

**[M1] zcli push flag is `--service-id` (kebab-case), not `--serviceId`** — MAJOR
- Plan line 328: `zcli push --serviceId <id>` — wrong flag name
- Existing SSH code `ops/deploy.go:201`: Also uses `--serviceId` — same bug exists in current code
- Verifier confirmed: actual flag is `--service-id` (kebab-case) or positional arg
- **Impact**: zcli exits with "unknown flag" error. Both SSH and local deploy affected.
- **Root cause**: Original SSH code used wrong flag name. Plan copied the bug.
- **Note**: SSH deploy has been working in production. This means either (a) zcli is more lenient than docs suggest, or (b) the specific zcli version deployed on containers accepts camelCase. Needs E2E test.

**[M2] Plan omits `--setup` flag for monorepo** — MAJOR
- Plan line 207: `zerops_deploy targetService="frontstage" workingDir="./frontend"` — no --setup
- zcli push `--setup` flag selects which zerops.yml entry when file has multiple `setup:` entries
- **Impact**: Monorepo deploy may deploy wrong service or fail with "multiple entries" error
- **Design fix**: Add optional `setup` parameter to DeployInput. deployLocal() passes `--setup <name>` to zcli push.

**[M3] S3 "works without VPN" — unverified, partial evidence against** — MAJOR
- Plan line 259: "S3 object storage uses HTTPS apiUrl — works without VPN"
- Verifier: TLS handshake failed at `storage.app-prg1.zerops.io:443`
- Per-service apiUrl may use different endpoint. Needs E2E with real object-storage.
- **Impact**: If false, users get .env with broken S3 credentials. App crashes at runtime.

**[M4] Auth text oversimplifies token scoping** — MAJOR (accuracy)
- Plan D2: "one token = one project"
- Reality: Zerops has three scope levels. auth.go correctly enforces single-project via ListProjects check.
- Plan text is misleading but code logic is correct.
- Adversarial noted auth.go:171 already has reasonable multi-project error message.

**[M5] Deploy guidance has no local environment branch** — MAJOR
- `deploy_guidance.go:56`: Only `if env == EnvContainer { ... }` — no else branch
- Plan Phase 4 covers this but it's unimplemented
- **Impact**: Local mode gets generic guidance without VPN hints, .env generation, or local-specific instructions

### Adversarial Analyst — Challenges + Missed Findings

#### Challenged Findings
- **[CH1] Re: [C2]** — Partially accurate. ValidateZeropsYml function API already accepts workingDir param (`deploy_validate.go:15`). Issue is call-site in deploy.go, not function design. But moot until C1 is fixed.
- **[CH2] Re: [M4]** — auth.go:171 already provides multi-project guidance. Primary may have reviewed stale understanding.

#### Missed Findings
- **[MF1] writeBootstrapOutputs has no environment access** — CRITICAL (confirms C3)
  - `bootstrap_outputs.go:23` uses DevHostname unconditionally
  - Engine has `environment` field (engine.go:20) but writeBootstrapOutputs is a method on Engine, so it CAN access `e.environment` — it just doesn't
  - Design fix is straightforward: branch on `e.environment == EnvLocal`

#### Confirmed (survived challenge)
- **[C1]** — Confirmed: server.go guard, Deploy() guard, RegisterDeploy signature all block local mode
- **[C3]** — Confirmed: ServiceMeta hostname mismatch in local mode
- **[M1]** — Confirmed: --serviceId camelCase is wrong per docs (but may work in practice — needs testing)

---

## Evidence-Based Resolution

### Verified Concerns (must fix before implementation)

| # | Finding | Severity | Evidence | Resolution |
|---|---------|----------|----------|------------|
| C1 | Deploy tool never registers in local mode | CRITICAL | server.go:108-110, deploy.go:71-77 | Remove guard, change Deploy() signature to accept nil sshDeployer |
| C2 | ValidateZeropsYml called with wrong path | CRITICAL | deploy.go:140-142 | deployLocal() must call with local workingDir |
| C3 | ServiceMeta writes DevHostname unconditionally | CRITICAL | bootstrap_outputs.go:23 | Branch on e.environment in writeBootstrapOutputs |
| M1 | zcli flag --serviceId is wrong case | MAJOR | KB + verifier | Use --service-id or positional arg; test current SSH deploy first |
| M2 | Missing --setup flag for monorepo | MAJOR | KB: zcli docs | Add setup parameter to DeployInput |
| M5 | No local guidance branch | MAJOR | deploy_guidance.go:56 | Phase 4 implementation |

### Logical Concerns (high confidence, not directly tested)

| # | Finding | Severity | Reasoning |
|---|---------|----------|-----------|
| M3 | S3 may not work without VPN | MAJOR | TLS failed at generic endpoint; per-service URL may differ |
| M4 | Token scope text misleading | MAJOR | Code correct, plan text oversimplifies |

### Unverified Concerns (need investigation)

| # | Finding | Notes |
|---|---------|-------|
| M1-sub | Does zcli on containers accept camelCase --serviceId? | SSH deploy works in prod — either zcli is lenient or different version |

### Recommendations (max 7, evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | Fix C1: Remove sshDeployer guard, change Deploy() signature | P0 (blocking) | server.go:108, deploy.go:71 | ~30L |
| R2 | Fix C3: Add environment branch to writeBootstrapOutputs | P0 (blocking) | bootstrap_outputs.go:23, engine.go:20 | ~15L |
| R3 | Fix M1: Test zcli --serviceId vs --service-id; fix if broken | P1 | deploy.go:201 | ~5L |
| R4 | Fix M2: Add --setup flag support for monorepo | P1 | KB: zcli docs | ~20L |
| R5 | Verify M3: E2E test S3 apiUrl without VPN | P1 | Verifier partial result | ~1h |
| R6 | Fix C2: deployLocal() calls ValidateZeropsYml with correct path | P1 (after R1) | deploy.go:140-142 | ~10L |
| R7 | Clarify plan text: token scoping, zcli flags, ServiceMeta flow | P2 | Multiple sections | ~30min |

---

## Revised Version

See `plans/plan-local-dev-flow.v2.md` — incorporates VERIFIED + LOGICAL concerns only.

**Key changes from v1:**
1. Phase 0 expanded: explicit Deploy() signature change + RegisterDeploy guard removal
2. Section 3.5 ServiceMeta: added environment branch specification
3. Section 7.1 deployLocal(): fixed zcli flags to kebab-case, added --setup for monorepo
4. Section 8.4 S3: added "UNVERIFIED" caveat, needs E2E test
5. Section 5.5 Monorepo: added --setup flag documentation

---

## Change Log
| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | 3.3 Key arch change | Expanded: must also change Deploy() signature to accept nil sshDeployer | server.go:108, deploy.go:71 | C1 |
| 2 | 3.5 ServiceMeta | Added environment branch: local mode writes appstage, container writes appdev | bootstrap_outputs.go:23 | C3 |
| 3 | 7.1 deployLocal() | Fixed --serviceId → --service-id; added note about positional arg alternative | KB + verifier | M1 |
| 4 | 5.5 Monorepo | Added --setup flag for zerops.yml entry selection | KB: zcli docs | M2 |
| 5 | 8.4 S3 | Added UNVERIFIED caveat; needs E2E test before release | Verifier partial | M3 |
| 6 | Phase 0 | Expanded: 3 sub-tasks (guard removal, Deploy() signature, RegisterDeploy always) | C1 | C1 |
| 7 | Phase 5 | Added: writeBootstrapOutputs environment branch | bootstrap_outputs.go:23 | C3 |
