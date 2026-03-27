# Analysis: Local Workflow First Real Test — Complete Failure

**Date**: 2026-03-27
**Task type**: codebase-analysis + flow-tracing
**Complexity**: Deep (ultrathink)
**Scope**: Local dev workflow end-to-end — bootstrap.md guidance, workflow engine, agent behavior
**Evidence basis**: Chat log + spec + code + bootstrap.md content

## Summary

The first real test of the local workflow was a **complete failure** — the agent behaved identically to container mode because **the local-mode guidance sections don't exist in bootstrap.md**. The code in `bootstrap_guidance.go` tries to extract `<section name="generate-local">` and `<section name="deploy-local">`, but these sections were never written. The agent received container-centric guidance (SSHFS, SSH deploy, dev+stage pairs) and predictably created a dev service on Zerops, deleted .env, and deployed code to a Zerops container instead of guiding local development.

---

## Root Cause: Missing Guidance Sections (CRITICAL)

### Evidence Chain

1. **`bootstrap_guidance.go:53-54`** — code attempts to extract `generate-local` section:
   ```go
   if env == EnvLocal {
       sections = append(sections, ExtractSection(md, "generate-local"))
   }
   ```

2. **`bootstrap_guidance.go:59-60`** — code attempts to extract `deploy-local` section:
   ```go
   if env == EnvLocal {
       sections = append(sections, ExtractSection(md, "deploy-local"))
   }
   ```

3. **`bootstrap.md`** — grep for `generate-local` and `deploy-local`: **NO MATCHES**. These sections don't exist.

4. **Effect on generate step**: `ExtractSection(md, "generate-local")` returns `""`. The agent gets ONLY the generic `<section name="generate">` which says:
   - "Prerequisites: Services mounted, env vars discovered" (SSHFS mount — irrelevant locally)
   - "Write ALL files to SSHFS mount path `/var/www/{hostname}/`" (wrong — files are local)
   - "Do NOT create `.env` files" (WRONG for local — .env IS the credential bridge)

5. **Effect on deploy step**: `ExtractSection(md, "deploy-local")` returns `""`. Since `env == EnvLocal`, the code at line 59 enters the local branch and appends the empty string. The `parts` array ends up empty (or contains only empty strings). Fallback at line 81-83: `ResolveGuidance("deploy")` returns the **container SSH deploy section** — "Write zerops.yml to SSHFS mount", "zerops_deploy to dev", "Start server via SSH", etc.

6. **Effect on discover step**: No environment branching at all (line 25-27 returns early for non-generate/non-deploy). Discover guidance says "Default pattern: dev+stage service pairs" with zero mention of local mode.

7. **Effect on provision step**: Same — no environment branching. Guidance says to create dev+stage pairs, startWithoutCode, SSHFS mounts.

### Confidence: VERIFIED — read `bootstrap_guidance.go` lines 24-84, grep'd `bootstrap.md` for both section names, read full generate and deploy sections.

---

## Chat Log Trace — What Went Wrong at Each Step

### Step 1: Discover — Agent creates a Zerops dev service (WRONG)

**What happened**: Agent starts bootstrap with `plan: [{runtime: {devHostname: "app", type: "php-nginx@8.4"}}]`. After discovering existing services, submits plan with `bootstrapMode: "dev"` and `devHostname: "appdev"`.

**What should happen (per spec-local-dev.md section 4)**: In local mode, the user's machine IS dev. Bootstrap should create ONLY managed services (appdb, cache) and optionally a stage service. No `appdev` on Zerops.

**Root cause**: Discover guidance has no local-mode awareness. It says "default pattern: dev+stage pairs". The agent follows the only guidance it has.

**Evidence**: `spec-local-dev.md:87` — "Engine: creates only appstage + managed (skips appdev creation)". Decision D1: "No dev service on Zerops in local mode".

### Step 2: Provision — Agent creates appdev runtime service (WRONG)

**What happened**: Agent imports `appdev` (php-nginx@8.4) + `appdb` (mariadb) + `cache` (valkey). Creates a runtime service on Zerops that shouldn't exist.

**What should happen**: Import only managed services. If Local Standard, also create `appstage` (not `appdev`).

**Root cause**: Provision guidance says "Standard mode creates `{name}dev` + `{name}stage` pairs". No local override.

### Step 3: Generate — Agent writes container config, deletes .env (WRONG)

**What happened**:
- Writes zerops.yml with `setup: appdev` and REAL start config (not `zsc noop`)
- Actually correctly writes files locally (since it IS local, despite guidance saying SSHFS)
- **Deletes .env and .env.example** — following container guidance "Do NOT create `.env` files"
- Writes env vars in zerops.yml `envVariables` using `${appdb_connectionString}` etc.

**What should happen (per spec-local-dev.md section 7)**:
- GENERATE .env from `zerops_discover` using `ops.FormatEnvFile()`
- Write zerops.yml targeting `appstage` (not `appdev`)
- Keep .env for local development, add to .gitignore
- Guide user to run `zcli vpn up` for managed service access

**Root cause**: `<section name="generate-local">` doesn't exist. Agent gets container guidance that explicitly says "Do NOT create `.env` files".

### Step 4: Deploy — Agent deploys to Zerops dev service (WRONG)

**What happened**: Agent calls `zerops_deploy targetService="appdev"` — pushes code to the Zerops dev service. First deploy fails (wrong `build.base: php-nginx@8.4` and `os: ubuntu`). Second deploy succeeds after fix.

**What should happen**: In local mode, the user develops locally. Deploy should either:
- Push to `appstage` for validation (Local Standard)
- Or not deploy at all if this is managed-only setup (user just wanted DB+Redis access)

**Root cause**: `<section name="deploy-local">` doesn't exist. Falls back to container deploy guidance.

### Step 5: Close — Everything "works" on Zerops, local dev broken

**What happened**: App deployed to Zerops, subdomain active, /status returns 200. Agent declares success.

**What's actually broken**:
- Local dev server crashed (no .env, no VPN)
- User wanted "run it in background" = local dev, "connect to db and redis" = managed services
- Got: full remote deployment instead of local dev setup

---

## All Agent Failures Mapped to Missing Guidance

| # | Agent Action | Expected (spec) | Root Cause |
|---|-------------|-----------------|------------|
| F1 | Creates `appdev` runtime on Zerops | No dev service in local mode | No local discover/provision guidance |
| F2 | Writes files "locally" but follows SSHFS instructions | Write locally (correct behavior, wrong reason) | No `generate-local` section |
| F3 | Deletes `.env` | Generate `.env` from discover | Container guidance says "no .env files" |
| F4 | No VPN guidance | Guide user to `zcli vpn up` | No local guidance at all |
| F5 | Deploys to `appdev` on Zerops | Deploy to `appstage` or skip | No `deploy-local` section |
| F6 | zerops.yml `setup: appdev` | `setup: appstage` | No local generate guidance |
| F7 | No `.env` generation | `ops.FormatEnvFile()` for local credentials | No local guidance |
| F8 | `build.base: php-nginx@8.4` fails | `build.base: php@8.4` | Recipe knowledge not applied (minor, unrelated to local mode) |
| F9 | `os: ubuntu` in zerops.yml | No `os` field needed | Same — recipe gap |

---

## What Was Implemented vs What Was Specified

### Implemented (code exists, tests pass)

| Component | File | Status |
|-----------|------|--------|
| Environment detection | `workflow/environment.go` | Working |
| Local config persistence | `workflow/local_config.go` | Working |
| Conditional deploy registration | `server/server.go:99-104` | Working |
| DeployLocal handler | `tools/deploy_local.go` | Working |
| ops.DeployLocal (zcli push) | `ops/deploy_local.go` | Working |
| Build log polling | `tools/deploy_poll.go` | Working |
| .env generation | `ops/env_export.go` | Working |
| ServiceMeta env field | `workflow/service_meta.go:27` | Working |
| Bootstrap outputs (hostname swap) | `workflow/bootstrap_outputs.go:32-34` | Working |
| Guidance branching code | `workflow/bootstrap_guidance.go:53-60` | Working (extracts empty) |
| E2E tests | `e2e/deploy_local_test.go` | Passing |
| Knowledge guide | `knowledge/guides/local-development.md` | Written |
| VPN guide | `knowledge/guides/vpn.md` | Written |
| Spec | `docs/spec-local-dev.md` | Written |

### NOT Implemented (missing content)

| Component | Where It Should Be | Impact |
|-----------|-------------------|--------|
| `<section name="generate-local">` | `bootstrap.md` | Agent gets container SSHFS guidance |
| `<section name="deploy-local">` | `bootstrap.md` | Agent gets container SSH deploy guidance |
| Local-aware discover guidance | `bootstrap.md` discover section | Agent creates dev services |
| Local-aware provision guidance | `bootstrap.md` provision section | Agent provisions dev services |
| Deploy workflow local guidance | `deploy.md` | Deploy workflow uses container guidance |

### Assessment

The **plumbing is complete** — all Go code, tests, conditional registration, env detection, local config, .env generation. The **content is missing** — bootstrap.md has zero local-mode guidance sections. The code correctly tries to extract sections that don't exist, gets empty strings, and falls back to container guidance.

This is like building a car with a working engine, transmission, and steering, but no road signs. The infrastructure works perfectly; the agent just has no idea how to use it in local mode.

---

## Implementation Gap: What bootstrap.md Needs

### 1. `<section name="generate-local">` (CRITICAL — ~50 lines)

Must cover:
- Write files **locally** (CWD), not to SSHFS mount
- zerops.yml `setup:` targets `appstage` (not `appdev`)
- **Generate .env** from discovered env vars (the credential bridge)
- Add `.env` to `.gitignore`
- Guide VPN setup: `zcli vpn up <projectId>`
- Local dev server: user manages it themselves
- zerops.yml envVariables use `${hostname_varName}` (same as container — resolved at deploy time)
- Do NOT delete existing `.env` — merge or regenerate

### 2. `<section name="deploy-local">` (CRITICAL — ~40 lines)

Must cover:
- `zerops_deploy targetService="appstage"` (from local CWD)
- No SSH, no SSHFS, no source service
- Build pipeline runs on Zerops, code pushed from local
- After deploy: `zerops_verify serviceHostname="appstage"`
- Local dev server continues running independently
- VPN survives deploys (verified)

### 3. Local-aware discover section (MAJOR — modify existing)

Add conditional text to discover section:
- In local mode: user's machine IS dev, do NOT create `{name}dev` on Zerops
- Create only managed services + optionally `appstage`
- If user only wants managed services: empty plan (`plan=[]`)

### 4. Local-aware provision section (MAJOR — modify existing)

- Skip dev service creation
- After managed services provision: guide VPN setup
- Generate .env from discovered env vars

---

## Comparison with Spec (spec-local-dev.md)

| Spec Section | Implementation Status | Gap |
|-------------|----------------------|-----|
| 1. Philosophy | Correct in code | Missing in guidance |
| 2. Environment Detection | COMPLETE | - |
| 3. Authentication | COMPLETE | - |
| 4. Topology | Code handles it | Guidance doesn't communicate it |
| 5. Deploy Architecture | COMPLETE (code) | Guidance missing |
| 6. ServiceMeta | COMPLETE | - |
| 7. Env Var Bridge | Code exists (`FormatEnvFile`) | Guidance never tells agent to use it |
| 8. VPN | Knowledge guide exists | Bootstrap guidance never mentions VPN |
| 9. Guidance System | Code branches correctly | **Content doesn't exist** |
| 10. Local Config | COMPLETE | - |
| 11. Health Verification | Spec says "agent uses Bash" | Guidance doesn't tell agent this |
| 12. Deploy Strategies | Code works | - |

---

## Recommendations

| # | Recommendation | Priority | Effort | Evidence |
|---|---------------|----------|--------|----------|
| R1 | Write `<section name="generate-local">` in bootstrap.md | P0 CRITICAL | ~50 lines | Root cause of F2, F3, F6, F7 |
| R2 | Write `<section name="deploy-local">` in bootstrap.md | P0 CRITICAL | ~40 lines | Root cause of F5 |
| R3 | Add local-mode conditional to discover section | P0 CRITICAL | ~15 lines | Root cause of F1 |
| R4 | Add local-mode conditional to provision section | P1 MAJOR | ~20 lines | Root cause of F1, F4 |
| R5 | Add php-nginx `build.base` rule to laravel recipe | P2 MINOR | ~3 lines | Root cause of F8 |
| R6 | Test: spawn local-mode bootstrap and verify guidance contains local sections | P1 MAJOR | ~30 lines test | Regression prevention |
| R7 | Consider: should discover/provision sections use environment branching like generate/deploy? | P1 MAJOR | Design decision | Currently hardcoded container assumptions |

---

## Implementation (completed 2026-03-27)

### Go code changes (2 files)

1. **`bootstrap_guidance.go`** — restructured `ResolveProgressiveGuidance`:
   - Non-generate/deploy steps now check for `{step}-local` addendum sections when `env == EnvLocal`
   - Generate in local mode: `generate-local` REPLACES base + mode-specific (not appended)
   - Deploy: unchanged (already correct)
   - Fallback changed from `ResolveGuidance(step)` to `ExtractSection(md, step)` to avoid double-loading

2. **`guidance.go`** — simplified `resolveStaticGuidance`:
   - All steps (except close) now route through `ResolveProgressiveGuidance` for environment awareness
   - Previously, discover/provision bypassed progressive guidance entirely

### Content changes (1 file, 4 new sections)

**`bootstrap.md`** — added 4 `<section>` blocks at the end:

| Section | Lines | Purpose |
|---------|-------|---------|
| `discover-local` | ~25 | "No dev service on Zerops", topology table, plan format unchanged |
| `provision-local` | ~35 | Import rules (no dev), .env generation, VPN guidance |
| `generate-local` | ~95 | Self-contained: local file writes, zerops.yml for stage, .env bridge, endpoints |
| `deploy-local` | ~55 | zcli push from local, iteration loop, recovery patterns |

### Test changes (2 files)

1. **`bootstrap_guidance_test.go`** — added 7 tests:
   - `TestResolveProgressiveGuidance_DiscoverLocal_ContainsAddendum`
   - `TestResolveProgressiveGuidance_ProvisionLocal_ContainsAddendum`
   - `TestResolveProgressiveGuidance_GenerateLocal_ReplacesContainer`
   - `TestResolveProgressiveGuidance_GenerateLocal_SkipsContainerModes`
   - `TestResolveProgressiveGuidance_DeployLocal_ReplacesContainer`
   - `TestResolveProgressiveGuidance_ContainerDiscover_NoLocalAddendum`

2. **`bootstrap_test.go`** — fixed 3 tests that used `EnvLocal` but tested container-specific behavior:
   - Changed to `EnvContainer` since they test mode-specific guidance (zsc noop, Simple mode)

### Results

- All 250+ tests pass (13 packages)
- Lint clean (0 issues)
- Pre-existing failure in `internal/update` unrelated

---

## Historical Context

Based on memory and git history:
- **2026-03-26**: `ad89a78 feat: implement local development mode (Phases 0-5)` — all Go code implemented
- **2026-03-26**: `85b1a37 e2e: add local deploy tests` — E2E tests passing
- **2026-03-26**: Multiple review iterations (`plan-local-dev-flow.review-1.md`, `review-2.md`) — focused on code correctness
- **2026-03-27**: `spec-local-dev.md` written — authoritative spec
- **2026-03-27**: Knowledge guides written (`local-development.md`, `vpn.md`)
- **Decision**: VPN auto-connect deferred (requires admin privileges)
- **What was NOT done**: bootstrap.md content sections for local mode

The implementation ended with "code + tests + spec + knowledge guides done, VPN auto-connect deferred to documentation only." The critical gap is that the **workflow guidance content** — the actual text that tells the agent what to do during bootstrap — was never written. The knowledge guides exist but are only surfaced via `zerops_knowledge` queries, not injected into the bootstrap workflow steps.
