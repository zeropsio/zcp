# Analysis: Infrastructure-to-Repo for buildFromGit — Iteration 1
**Date**: 2026-04-03
**Scope**: Knowledge system, workflows, platform API, docs, zerops-docs reference
**Agents**: kb (zerops-knowledge), verifier (platform-verifier), primary-analyst (Explore), adversarial (Explore), verifier-pass-2
**Complexity**: Deep (ultrathink)
**Task**: Does ZCP have a guide/workflow to create a repo from existing infrastructure for buildFromGit? What exists, what's missing, how could it work?

## Summary

**No such guide or workflow exists today.** ZCP's workflows (bootstrap, recipe, deploy, cicd) are all forward-only: they create infrastructure from intent or deploy code to existing services. The reverse flow — extracting configuration from running infrastructure to generate a deployable git repository — is not implemented.

However, **~85% of the pieces already exist** — more than initially estimated. Late-stage verification revealed that the Zerops API **HAS export endpoints** (`GET /api/rest/public/project/{id}/export` and `GET /api/rest/public/service-stack/{id}/export`) already present in the `zerops-go` SDK. ZCP simply hasn't implemented them in its `Client` interface. Combined with `zerops_discover` (scaling, ports, containers, mode) and recipe knowledge (30+ zerops.yml templates), the data layer is nearly complete.

**Key insights**:
1. **Export API exists** — `sdk/GetProjectExport.go` and `sdk/GetServiceStackExport.go` in zerops-go SDK return re-importable YAML. ZCP just needs to wrap them.
2. **Export + Discover = comprehensive**: Export gives project config + service skeleton; Discover fills in scaling ranges, ports, containers, mode. Together they cover ~90% of import.yaml fields.
3. **Recipe knowledge as inference source**: 30+ framework-specific zerops.yml templates in `internal/knowledge/recipes/` enable framework detection (service type + env patterns → recipe → zerops.yml template) covering 80-90% of build/deploy config.
4. **Inline `zeropsYaml`** in import.yaml means a single-file IaC export is possible — no separate repo needed for re-import.

---

## Findings by Severity

### Critical

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F1 | ~~No platform API export endpoint~~ **CORRECTED**: Zerops API HAS export endpoints (`GET /api/rest/public/project/{id}/export`, `GET /api/rest/public/service-stack/{id}/export`). They're in the zerops-go SDK (`sdk/GetProjectExport.go`, `sdk/GetServiceStackExport.go`). **ZCP simply hasn't implemented them** in `platform.Client`. This is a ZCP implementation gap, not a platform limitation. | Live API test returned valid YAML. SDK has `ProjectExport{Yaml types.Text}` response type. | VERIFIED: verifier (live API test), adversarial (challenged original claim) |
| F2 | No workflow exists for "infra → repo" reverse flow. All 4 workflows (bootstrap, recipe, deploy, cicd) are forward-only. | Searched all workflow files, knowledge themes, guides — zero mention of reverse/export flow. | VERIFIED: kb, primary-analyst |

### Major

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F3 | zerops.yml (build/deploy pipeline config) is NOT retrievable from running services. Platform stores it during build but doesn't expose it via API afterward. | `ops/discover.go` returns scaling/containers/ports/envs but no build config. No `GetBuildConfig()` in Client interface. | VERIFIED: primary-analyst, verifier |
| F4 | `buildFromGit` is a one-time deploy trigger, not a persistent connection. After import, the repo URL is not stored as ongoing config. For persistent CI/CD, users must set up GitHub/GitLab integration separately. | Docs: "one-time build" (import.mdx:399-401). `core.md:29`: "one-time build from repo." | VERIFIED: verifier (docs), kb |
| F5 | Recipe workflow (`workflow/recipe.go`) is structurally forward-only. `RecipePlan` and `ResearchData` are INPUT structs for generation, not OUTPUT from discovery. Cannot be reversed without new code. | `recipe.go:38-92` — fields populated from research, not extraction. | VERIFIED: primary-analyst |
| F6 | Source code availability in containers is framework-dependent. Interpreted languages (Node.js, Python, PHP) deploy source to `/var/www`. Compiled languages (Go, Rust, Java) deploy only binaries. SSH extraction works for ~60% of runtimes. | `deployment-lifecycle.md:14-21`: source lands in `/var/www` via `deployFiles`. | LOGICAL: kb (framework-dependent reasoning) |

### Minor (Corrections)

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F7 | Mode (HA/NON_HA) IS available in platform API (`ServiceStack.Mode` at `types.go:30`) but NOT exposed in `ServiceInfo` struct in `discover.go`. Trivial fix — add field + copy in mapper. | `types.go:30`: `Mode string // HA, NON_HA`. `zerops_mappers.go:98-101` maps it from API. `discover.go:29-44` omits it from `ServiceInfo`. | VERIFIED: verifier-pass-2 |
| F8 | `SubdomainEnabled` IS in `ServiceInfo` (`discover.go:37`) and populated from API (`discover.go:122`). Not missing as initially claimed. | `discover.go:37`: `SubdomainEnabled bool` field exists. | VERIFIED: verifier-pass-2 |

---

## Existing Capabilities Inventory

| Capability | Source | What It Provides | Coverage for Reverse Flow |
|-----------|--------|-----------------|--------------------------|
| `zerops_discover` | `ops/discover.go` | hostname, type, status, env vars (with isSecret), scaling, containers, ports, subdomain | ~75% of import.yaml fields |
| GUI Export | Zerops dashboard (three-dot menu) | Full import.yaml-compatible YAML | ~95% of import.yaml (but GUI-only) |
| `.env` generation | `ops/env_export.go` | Formatted env file from discovered vars | Env var bridge ✓ |
| Bootstrap adoption | `workflows/bootstrap.md:34-67` | Registers existing services in ZCP | Service registration ✓ (but no config extraction) |
| Recipe knowledge | `knowledge/recipes/*.md` (30+ files) | Complete zerops.yml templates per framework | Framework inference source ✓ |
| Recipe workflow | `workflow/recipe.go` | Generates repos with 6 env tiers | Forward-only, but templates reusable |
| CI/CD workflow | `workflows/cicd.md` | Git repo → Zerops service pipeline | Forward-only setup |

### What's Missing (Gaps)

| Gap | Severity | Workaround | Effort to Fix |
|-----|----------|-----------|---------------|
| Programmatic export API | ~~CRITICAL~~ **RESOLVED** | API exists in zerops-go SDK (`GetProjectExport`, `GetServiceStackExport`). ZCP needs to add 2 methods to `platform.Client`. | Trivial (2 methods + response types) |
| zerops.yml reverse-generation | MAJOR | Match service type → recipe template → scaffold zerops.yml with discovered env vars/ports | Medium (leverage existing recipe knowledge) |
| Mode field in discover output | MINOR | Already in API response (`ServiceStack.Mode`), just not surfaced | Trivial (add field to `ServiceInfo`) |
| Framework detection | MAJOR | Heuristic: service type + env var patterns → recipe name | Medium (pattern library) |
| Source code extraction | MAJOR | SSH to container + download `/var/www` (interpreted langs only) | High (SSH management in stateless MCP) |

---

## How It Could Work — Architecture Recommendation

### Recommended: "Export + Discover + Infer + Scaffold" Flow

```
User: "Create a repo from my running infrastructure"
    ↓
Step 1: EXPORT — Call Zerops API export endpoints (project + per-service)
    Returns: re-importable YAML skeleton with project config, service types, scaling thresholds
    ↓
Step 2: DISCOVER — zerops_discover all services + env vars
    Returns: scaling RANGES (min/max), ports, containers, mode (HA/NON_HA), full env vars
    ↓
Step 3: MERGE — Combine export (skeleton + project config) with discover (detailed config)
    Export provides: project envVariables, corePackage, isolation settings
    Discover provides: mode, scaling ranges, ports, containers, env var secrets distinction
    ↓
Step 4: CLASSIFY — Separate managed (db, cache, storage) from runtime services
    ↓
Step 5: INFER FRAMEWORK — Match runtime type + env patterns → recipe template
    (e.g., nodejs@22 + NEXT_PUBLIC_* → nextjs recipe → zerops.yml template)
    ↓
Step 6: GENERATE IMPORT.YAML — From merged data:
    - Base: export YAML skeleton (already import-compatible)
    - Enrich: mode, scaling ranges, ports, containers from discover
    - Env vars: envSecrets (isSecret=true) vs project envVariables
    - Option: inline zeropsYaml for single-file IaC (no separate zerops.yml needed)
    ↓
Step 7: GENERATE ZEROPS.YML — From recipe template + discovered config:
    - setup: hostname
    - build.base: from service type
    - run.ports: from discovered ports
    - run.envVariables: from discovered env vars (cross-service refs)
    - build/deploy sections: from recipe template defaults
    - Comments: "VERIFY: these are defaults from {recipe} template"
    ↓
Step 8: SCAFFOLD REPO — Create directory structure:
    - import.yaml (generated, comprehensive)
    - zerops.yml (generated from template)
    - .env.example (from discovered vars)
    - README.md (instructions for buildFromGit usage)
    ↓
Step 9: USER REVIEW — Present generated files for review
    - Highlight fields marked "VERIFY"
    - Ask: "Is this your framework? Are build commands correct?"
    ↓
Step 10: OPTIONAL — Add buildFromGit to import.yaml pointing to new repo
```

### Late-Stage Discovery: Export API Endpoints

The verifier confirmed that Zerops API HAS export endpoints in the zerops-go SDK:
- `sdk/GetProjectExport.go` → `GET /api/rest/public/project/{id}/export`
- `sdk/GetServiceStackExport.go` → `GET /api/rest/public/service-stack/{id}/export`
- Response: `ProjectExport { Yaml types.Text }` — single YAML string field

**Live test results** (project export for zcp@1):
- Includes: project name ("copy of" prefix), corePackage, ALL project envVariables (including secrets in plaintext), envIsolation, sshIsolation, service hostname + type, verticalAutoscaling (cpuMode, free thresholds)
- Missing: mode (HA/NON_HA), min/max scaling ranges, horizontal scaling, ports, enableSubdomainAccess, service-level env vars, buildFromGit/zeropsSetup

**Conclusion**: Export + Discover together cover ~90% of import.yaml fields. Export gives project-level config; Discover fills service-level details.

### Implementation Options

**Option A: New "export" workflow** (recommended)
- `zerops_workflow action="start" workflow="export"`
- Steps: discover → classify → infer → generate → review
- Reuses existing discover tool, recipe knowledge, and workflow engine
- Effort: 5-7 days

**Option B: Extend bootstrap "adopt" flow**
- When adopting existing services, offer "also generate repo files?"
- Builds on existing adoption logic
- Effort: 3-4 days (but conceptually muddled — bootstrap is about creating infra, not exporting it)

**Option C: Extend recipe workflow in reverse**
- `zerops_workflow action="start" workflow="recipe" mode="reverse"`
- Reuses RecipePlan + ResearchData structs but populates from discover instead of user research
- Effort: 4-5 days (cleanest abstraction but requires recipe workflow changes)

### Key Design Decision: `zeropsYaml` Inline vs Separate File

The import.yaml `zeropsYaml` field allows embedding zerops.yml config INLINE in the import file. This means:
- **With `zeropsYaml` inline**: The generated import.yaml is self-contained. No separate repo needed for `buildFromGit` — the YAML defines both infrastructure AND pipeline. But: `buildFromGit` still needs a repo with source code.
- **With separate zerops.yml**: Standard pattern. Repo has zerops.yml + source code. `buildFromGit` points to repo.
- **Recommendation**: Generate BOTH — import.yaml (infra) + zerops.yml (pipeline) as separate files. The user decides whether to use `zeropsYaml` inline or separate repo.

---

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| F1 (export API exists, ZCP doesn't implement) | VERIFIED | Live API test + zerops-go SDK inspection |
| F2 (no reverse workflow) | VERIFIED | Full search of workflows, knowledge, guides |
| F3 (zerops.yml not retrievable) | VERIFIED | discover.go + Client interface review |
| F4 (buildFromGit one-time) | VERIFIED | Docs citation (import.mdx:399) |
| F5 (recipe forward-only) | VERIFIED | recipe.go struct analysis |
| F6 (source code framework-dependent) | LOGICAL | deployment-lifecycle.md + framework semantics |
| F7 (Mode in API, not in discover) | VERIFIED | types.go:30 + zerops_mappers.go:98 + discover.go gap |
| F8 (SubdomainEnabled exists) | VERIFIED | discover.go:37, discover.go:122 |
| Architecture recommendation | LOGICAL | Combines verified capabilities + identified gaps |

---

## Adversarial Challenges

### From adversarial agent:

| Challenge | Finding | Resolution |
|-----------|---------|------------|
| CH1: "No export API" may be ZCP gap, not platform gap | F1 originally CRITICAL platform limitation | **CONFIRMED by verifier**: API endpoints EXIST in zerops-go SDK. F1 corrected — ZCP implementation gap only. |
| CH2: Inline `zeropsYaml` simplifies the flow | Analyst didn't evaluate single-file option | **Valid**: import.yaml with inline zeropsYaml = self-contained IaC export. No separate repo needed for re-import. |
| CH3: AppVersionEvent/BuildInfo has no build config | Adversarial verified types.go:203-214 | **Confirmed**: BuildInfo has timing data only (PipelineStart/Finish/Failed), not config used. |

### From verifier (pass 2):

| Challenge | Finding | Resolution |
|-----------|---------|------------|
| CH4: Mode IS in API | F7 originally MAJOR gap | Downgraded to MINOR — trivial to expose in discover |
| CH5: SubdomainEnabled exists | Was listed as missing | Removed from gaps — already in ServiceInfo |

### From verifier (pass 3 — CRITICAL DISCOVERY):

| Challenge | Finding | Resolution |
|-----------|---------|------------|
| CH6: Export API exists and works | F1 was "no API export" | **OVERTURNED**: `sdk/GetProjectExport.go` + `sdk/GetServiceStackExport.go` confirmed via live API test. Returns re-importable YAML. |
| CH7: Export is incomplete vs discover | Assumed export would be comprehensive | Export missing: mode, scaling ranges, horizontal scaling, ports, service-level envs. Discover fills these gaps. |

KB agent added: recipe knowledge (30+ templates) makes framework inference feasible without a new pattern library. Match `service type → recipe name → extract zerops.yml template`. This raises zerops.yml generation coverage to ~80-90% for known frameworks.

---

## Recommendations (max 7, evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | **Implement export API in ZCP**: Add `GetProjectExport()` + `GetServiceStackExport()` to `platform.Client` — SDK already has them | **HIGHEST** | verifier: live API test confirmed endpoints work, SDK has types | 2-4 hours |
| R2 | Add `Mode` field to `ServiceInfo` in `discover.go` — trivial, already in `ServiceStack.Mode` | HIGH | verifier: types.go:30 has Mode, discover.go omits it | 1 hour |
| R3 | Create `ops/export.go`: merge export YAML + discover data into comprehensive import.yaml | HIGH | Export gives skeleton + project config; discover fills scaling/ports/containers/mode | 2 days |
| R4 | Build framework inference: `service type + env patterns → recipe name` mapping | HIGH | kb: 30+ recipes already encode zerops.yml patterns | 2-3 days |
| R5 | Create "export" workflow orchestrating: export → discover → merge → infer → generate | HIGH | primary-analyst + adversarial architecture recommendation | 5-7 days |
| R6 | Write knowledge guide: "Exporting and Replicating Infrastructure" with manual steps | MEDIUM | No guide exists (verified); users need this NOW even before automation | 1 day |
| R7 | Defer SSH-based source extraction to Phase 3 | LOW | Complex, framework-dependent, user can provide source manually | Future |
