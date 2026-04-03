# Content Specification: Knowledge System Redesign

**Date**: 2026-04-02
**Purpose**: Exact content requirements per file — what must be there, what must change, why.

---

## universals.md — Current Problems & Target State

### Current content (55 lines)

```
# Zerops Platform Universals
## Keywords, ## TL;DR
## Networking          — bind 0.0.0.0, SSL termination, proxy headers, per-framework proxy examples
## Filesystem          — deploy = new container, persistent data external, deployFiles mandatory
## Environment Variables — OS env vars, ${hostname_varname}, envSecrets, no .env, preprocessor
## Build/Deploy Lifecycle — build ≠ run, deployFiles = only bridge, base can differ
## Multi-Container Safety — zsc execOnce, sessions external, cron idempotency
## Recipe Conventions   — deployFiles for stage/prod, healthCheck for stage/prod only
```

### Problems

1. **"Recipe Conventions" is misplaced** — tells you how to READ recipes (dev uses [.], stage uses build output). This is useful context but the name "Recipe Conventions" is confusing. It's actually "Deploy Mode Patterns" — universal knowledge about how dev and prod differ in zerops.yml. Rename and slightly expand.

2. **Networking section has per-framework proxy examples** (Laravel, Django, .NET, Rails, Express, Spring Boot, Go). These are runtime-specific — they belong in recipes ## Gotchas, not universals. Universals should state "Apps MUST trust proxy headers" and stop there. The HOW differs per framework.

3. **Missing: Stack Composition** — no concept of incremental stack building. Agent doesn't know WHY to add services in layers or what each layer provides.

4. **Missing: Immutable Decisions** — currently only in core.md:106-112. Should be highlighted in universals as a "decide once" callout.

5. **Missing: Container Lifecycle** — universals says "deploy = new container" but doesn't cover restart/reload/scale behavior. Agent doesn't know what survives what.

6. **Missing: Project-Level Secrets** — the pattern "secrets shared across dev+stage MUST be project-level" is framework-universal (APP_KEY, SECRET_KEY_BASE, APP_SECRET all follow this). Currently only in old framework-specific recipes.

### Target content (~75 lines)

```
# Zerops Platform Universals

## Keywords, ## TL;DR (updated)

## Networking
- Apps MUST bind 0.0.0.0. L7 LB routes to container VXLAN IP.
- SSL terminates at L7. Internal = plain HTTP. service-to-service: http://hostname:port.
- Apps MUST trust proxy headers (X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host).
  → Framework-specific configuration: see runtime recipe ## Gotchas.
  [REMOVE per-framework examples — move to recipes]

## Filesystem
- [keep as-is — correctly states deploy=new container, persistent data external, deployFiles mandatory]

## Environment Variables
- [keep as-is — correctly covers OS env vars, ${hostname_varname}, envSecrets, no .env]

## Build/Deploy Lifecycle
- [keep as-is — correctly covers build ≠ run, deployFiles bridge]

## Multi-Container Safety
- [keep as-is — correctly covers zsc execOnce, sessions, cron]

## Container Lifecycle [NEW]
- Survives: restart, reload, stop/start, vertical scaling (same container persists)
- Lost on: deploy (new container replaces old), horizontal scale-up (new container starts fresh),
  scale-down (oldest container removed with its local data)
- All persistent data: database, object storage, or shared storage. Never local filesystem.

## Stack Composition [NEW]
- Build stacks incrementally: runtime → +database → +cache → +storage → +search/queue
- Each managed service adds to import.yml with priority:10 (starts before runtime services)
- Wire services via ${hostname_varName} in zerops.yml run.envVariables
- For complete import.yml examples and schemas: zerops_knowledge scope="infrastructure"

## Immutable Decisions [NEW]
- These CANNOT change after creation — choose correctly or delete+recreate:
  hostname, mode (HA/NON_HA), object storage bucket name, service type category

## Project-Level Secrets [NEW]
- Application secrets shared across dev+stage (APP_KEY, SECRET_KEY_BASE, APP_SECRET)
  MUST be project-level env vars, not per-service envSecrets.
- Per-service envSecrets generate different values per service → breaks sessions/CSRF
  across environments.
- Use: zerops_env project=true variables=["SECRET_KEY=<value>"]

## Deploy Mode Patterns [RENAMED from "Recipe Conventions"]
- Recipes show production zerops.yml (optimized deployFiles, real start command).
  For dev/self-deploy: use deployFiles: [.] so source + zerops.yml survive.
- healthCheck and readinessCheck: stage/production only.
  Dev services use start: zsc noop --silent — agent controls lifecycle via SSH.
  [keep existing content, just rename section]
```

### What moves OUT of universals.md

| Content | Destination | Why |
|---------|-------------|-----|
| Per-framework proxy examples (Laravel TRUSTED_PROXIES, Django CSRF_TRUSTED_ORIGINS, .NET ForwardedHeaders, Rails config.hosts, Express trust proxy, Spring forward-headers, Go read headers) | Per-runtime hello-world recipes ## Gotchas | Framework-specific configuration doesn't belong in universals |

---

## core.md — Current Problems & Target State

### Problems in Rules & Pitfalls (lines 231-299)

The H2 "Rules & Pitfalls" section is already organized into H3 subsections. That's good. But:

1. **"### Build & Deploy" (lines 238-248)** mixes universal and runtime-specific:
   - UNIVERSAL: Never /var/www in prepareCommands, addToRunPrepare pattern, never initCommands for packages, never manual git init
   - RUNTIME-SPECIFIC: node_modules in deployFiles (Node.js/Bun), fat JAR (Java), Maven/Gradle wrapper (Java), pip --no-cache-dir (Python), Composer --ignore-platform-reqs (PHP)

2. **"### Base Image & OS" (lines 250-255)** mixes universal and runtime-specific:
   - UNIVERSAL: Never apt-get on Alpine, never apk on Ubuntu, sudo apk/apt-get syntax
   - RUNTIME-SPECIFIC: Never run.base alpine for Go, os: ubuntu for Deno/Gleam

3. **"### Import Generation" (lines 277-290)** — entirely flow-specific. Already in bootstrap.md.

4. **"### Runtime-Specific" (lines 292-299)** — entirely runtime-specific. Belongs in recipes.

### Target state for Rules & Pitfalls

```
## Rules & Pitfalls

### Networking
- [keep as-is — all universal]

### Build & Deploy
- NEVER reference /var/www in run.prepareCommands [keep]
- ALWAYS use addToRunPrepare for files needed in prepareCommands [keep]
- NEVER use initCommands for package installation [keep]
- NEVER run manual git init before zerops_deploy [keep]
[REMOVE: node_modules, fat JAR, Maven wrapper, pip no-cache, Composer ignore-platform-reqs]

### Base Image & OS
- NEVER use apt-get on Alpine [keep]
- NEVER use apk on Ubuntu [keep]
- ALWAYS use sudo apk add --no-cache on Alpine [keep]
- ALWAYS use sudo apt-get update && install -y on Ubuntu [keep]
[REMOVE: Go alpine mismatch, Deno/Gleam ubuntu]

### Environment Variables
- [keep as-is — all universal]

### Import & Service Creation
- [keep as-is — all universal platform rules (valkey@7.2, mode, containers, scaling, priority, etc.)]
[NOTE: this section stays because it's about platform constraints, not bootstrap flow]

### [REMOVE: Import Generation] → already in bootstrap.md
### [REMOVE: Runtime-Specific] → moved to recipes

### Scaling & Platform
- [keep as-is — all universal]

### Event Monitoring
- [keep as-is — all universal]
```

### Target state for Causal Chains (lines 360-375)

Keep universal rows:
```
| Bind localhost | 502 Bad Gateway | LB routes to VXLAN IP |
| Reference /var/www in prepareCommands | File not found | Deploy files arrive AFTER prepareCommands |
| No mode for managed service | Defaults to NON_HA | Set HA explicitly for production |
| Set minContainers for PostgreSQL | Import fails | Managed services have fixed containers |
| build.base: php-nginx@8.3 | unknown base | Webserver variants are run bases only |
| valkey@8 in import | Import fails | Only valkey@7.2 is valid |
```

Move to recipes:
```
| Deploy thin JAR | ClassNotFoundException | → java-hello-world.md ## Gotchas
| npm install only in build | Missing modules | → nodejs-hello-world.md ## Gotchas (already in YAML comments)
| Bare mvn in buildCommands | command not found | → java-hello-world.md ## Gotchas
| deployFiles: dist/~ + start: bun dist/index.js | File not found | → relevant SSR recipes (tilde semantics)
```

### Multi-Service Examples (lines 378-464)

**KEEP as-is.** These are 4 complete import.yml examples (dev/stage + DB, API + DB, full-stack, production). They will be NEWLY INJECTED at provision step via `getCoreSection(kp, "Multi-Service Examples")`.

Review content: the examples are good. They show:
- Dev/stage naming pattern with PostgreSQL
- Alternative prefix (api vs app)
- Full stack (DB + cache + storage) with preprocessor
- Production (buildFromGit, HA)

No content changes needed. Just delivery change (inject at provision).

---

## operations.md — Target State

### What stays (~148 lines)

Lines 3-145 are all clean operational reference:
- Networking (L7, public access, Cloudflare, firewall, VPN, SSH isolation)
- CI/CD Integration
- Logging & Monitoring
- SMTP
- CDN
- Scaling (brief — detail in guides/scaling.md)
- RBAC
- Object Storage Integration
- Production Checklist

No content changes needed for these sections.

### What moves out

| Lines | Content | Destination | Content change needed? |
|-------|---------|-------------|----------------------|
| 147-185 | Service Selection Decisions (38L) | DELETE — decisions/ files are authoritative source | No — decisions/ already have better content |
| 187-210 | Tool Access Patterns (23L) | bootstrap.md (new section in deploy guidance) | Minor: reframe from "reference" to "remember these rules" |
| 212-227 | Dev vs Stage (15L) | bootstrap.md generate section | Already partially there — merge, deduplicate |
| 229-251 | Verification & Attestation (22L) | bootstrap.md deploy section | Minor: merge with existing verification guidance |
| 253-273 | Troubleshooting (20L) | bootstrap.md deploy section + deploy.md investigate | Split: common symptoms to both, workflow-specific to each |

---

## bases/ — Current Problems & Target State

### Current state (5 files, 104 lines total)

These files are VERY thin and serve as fallback runtime guides for non-language runtimes.

| File | Lines | Content quality |
|------|-------|----------------|
| alpine.md | 15 | OK but redundant with core.md Base Image Contract. TL;DR duplicates content. |
| ubuntu.md | 15 | Same issue — TL;DR duplicates core.md. |
| docker.md | 23 | Good — unique info (VM, --network=host, no autoscaling, no :latest) |
| nginx.md | 19 | Good — unique info (SPA routing, template vars, prerender) |
| static.md | 32 | Good — unique info (build != run, tilde, framework output dirs, SPA fallback) |

### Target state

**alpine.md and ubuntu.md**: Add when-to-use guidance. Currently just repeats "musl" and "glibc." Agent doesn't know WHEN to choose which.

```markdown
# Alpine on Zerops
## TL;DR
Default base OS (~5MB). Uses musl libc. Package manager: `sudo apk add --no-cache`.

### When to Use (default)
Alpine is the default for all runtimes. Use it unless you need glibc.

### When to Switch to Ubuntu
- CGO-enabled Go binaries that link C libraries
- Python packages with C extensions that require glibc (numpy, pandas with compiled backends)
- Deno and Gleam (not available on Alpine)
- Any software that explicitly requires glibc

### Package Installation
`sudo apk add --no-cache {package}` — sudo required (containers run as `zerops` user).
NEVER use apt-get on Alpine.
```

**docker.md**: Good as-is. Add one missing gotcha: "resource change triggers VM restart" should be more prominent.

**nginx.md**: Good as-is.

**static.md**: Good as-is. The framework output directories table is particularly useful.

---

## guides/ — Current Problems & Target State

### Current state (20 files, 2185 lines)

These are query-only (not in structured delivery). Content quality varies:

| File | Lines | Quality | Notes |
|------|-------|---------|-------|
| scaling.md | 210 | GOOD | Comprehensive: thresholds, applicability matrix, YAML syntax, common mistakes |
| zerops-yaml-advanced.md | 191 | GOOD | Behavioral reference: health/readiness checks, cron, routing, envReplace |
| deployment-lifecycle.md | 178 | GOOD | Full pipeline: build phases, limits, runtime prepare, deploy modes |
| environment-variables.md | 175 | GOOD | Complete: scope hierarchy, precedence, isolation, cross-service, secrets |
| production-checklist.md | 171 | GOOD | Comprehensive pre-launch checklist |
| networking.md | 156 | OVERLAPS | L7 balancer detail — overlaps operations.md + core.md. Adds firewall, DNS, WebSocket |
| object-storage-integration.md | 155 | GOOD | Code examples (PHP, Node, Python, Java), policies, CDN |
| local-development.md | 133 | GOOD | VPN, .env generation, zcli push, workflow |
| php-tuning.md | 109 | GOOD | PHP-specific: OPcache, FPM pools, extensions, xdebug |
| build-cache.md | 99 | GOOD | Two-layer cache architecture, invalidation, per-framework patterns |
| ci-cd.md | 92 | OVERLAPS | GitHub Actions + GitLab — overlaps operations.md CI/CD section |
| cloudflare.md | 73 | OVERLAPS | SSL mode, DNS — overlaps operations.md Cloudflare section |
| logging.md | 67 | OK | Log types, forwarding, syslog format |
| cdn.md | 65 | OK | CDN regions, TTL, purge |
| backup.md | 62 | OK | Backup system, schedule, retention |
| metrics.md | 57 | OK | Prometheus, APM |
| public-access.md | 56 | GOOD | IP types, DNS setup, zerops.app subdomain detail |
| vpn.md | 46 | OK | VPN setup, .zerops suffix, limitations |
| firewall.md | 46 | OVERLAPS | Port restrictions — overlaps operations.md firewall section |
| smtp.md | 34 | OK | Port 587 only, provider examples |

### Content actions

**No content changes needed for most guides.** They're good reference docs. The issue is DELIVERY, not content.

**Guides that overlap with operations.md:**
- networking.md / cloudflare.md / firewall.md / ci-cd.md — these EXTEND operations.md with more detail. operations.md has the summary, guide has the depth. This is OK — not duplication if operations.md says "for detail see guide."

**Add "See Also" cross-references** where missing:
- operations.md Networking section → link to guides/networking.md, guides/cloudflare.md, guides/firewall.md
- operations.md CI/CD section → link to guides/ci-cd.md
- operations.md Scaling section → link to guides/scaling.md

**php-tuning.md** — this is the ONLY runtime-specific guide. It's appropriate here because PHP tuning (OPcache, FPM pools) is operational, not build/deploy specific. Keep as-is.

---

## Flow content — What bootstrap.md needs to absorb

### From operations.md (moving in)

**Tool Access Patterns** (ops:187-210) → Add to bootstrap.md as a section near deploy step guidance. Content is correct, just needs relocation. Agent needs to know "psql runs from ZCP, not container" and "pipe outside SSH" during bootstrap.

**Dev vs Stage** (ops:212-227) → Merge into existing bootstrap.md generate-standard section. Much of this content already exists in bootstrap.md as the dev vs stage properties table and deploy flow. Deduplicate — keep the richer version.

**Verification & Attestation** (ops:229-251) → Merge into bootstrap.md deploy section. The attestation model ("good: specific, provable; bad: vague") is valuable agent guidance.

**Troubleshooting** (ops:253-273) → Split:
- Common symptoms (502, connection refused, SSH hangs) → bootstrap.md deploy section
- Same common symptoms → deploy.md investigate section (both need it)
- Framework-specific troubleshooting → stays with frameworks (recipes if applicable)

### From core.md (moving in)

**Import Generation** (core:277-290) → bootstrap.md already has import guidance in provision section (lines 122-196). The 14 Import Generation rules from core.md are ALREADY PRESENT in bootstrap.md in a different form (the dev vs stage properties table, hostname pattern, validation checklist). Verify no unique rules are lost, then simply DELETE from core.md. If any rule exists ONLY in core.md and not in bootstrap.md, add it.

Specific check — rules in core.md:277-290 vs bootstrap.md:

| core.md Import Generation rule | bootstrap.md equivalent |
|-------------------------------|------------------------|
| Standard mode: dev/stage pairs | provision:132 "Hostname pattern" |
| startWithoutCode on dev only | provision:136-143 dev vs stage properties table |
| maxContainers: 1 for dev | provision:139 table row |
| startWithoutCode on all that need immediate start | provision:138 table row |
| zeropsSetup only with buildFromGit | NOT IN bootstrap.md — ADD |
| minRam high enough for spikes | provision:141 table row |
| Managed hostname conventions | NOT explicitly in bootstrap.md — ADD |
| priority: 10 for managed | provision implied but not explicit — ADD |
| setup: matches hostname | NOT explicitly in provision — lives in generate. OK. |
| enableSubdomainAccess + subdomain enable | provision:partially, deploy:416-418 |
| shared-storage post-deploy connect | provision:145-149 |
| healthCheck stage only | generate-standard:285-286, generate-dev:316, generate-simple:337 |
| readinessCheck stage only | generate-standard: implied |

**3 rules to add to bootstrap.md** from core.md Import Generation:
1. "zeropsSetup: only set with buildFromGit when setup name differs from hostname"
2. "Managed hostname conventions: db, cache, queue, search, storage"
3. "priority: 10 for managed services, lower for runtime (default or 5)"

---

## Summary of ALL content changes

### Files to EDIT (content changes)

| File | What changes | Lines affected |
|------|-------------|----------------|
| universals.md | Remove per-framework proxy examples from Networking; Add Stack Composition, Immutable Decisions, Container Lifecycle, Project-Level Secrets; Rename Recipe Conventions → Deploy Mode Patterns | ~20 lines removed, ~20 lines added = net ~75L |
| core.md | Remove 5 runtime-specific rules from Build & Deploy; Remove 2 from Base Image; Remove Import Generation (277-290); Remove Runtime-Specific (292-299); Split Causal Chains | ~50 lines removed = ~410L |
| operations.md | Remove Service Selection (147-185); Remove Tool Access, Dev vs Stage, Verification, Troubleshooting (187-273) | ~125 lines removed = ~148L |
| alpine.md | Add when-to-switch guidance | +8 lines |
| ubuntu.md | Add when-to-use list | +5 lines |
| bootstrap.md | Add 3 Import Generation rules; Add Tool Access, Troubleshooting; Merge Dev vs Stage, Verification | ~60 lines added |
| deploy.md | Add troubleshooting to investigate section | ~20 lines added |

### Files to NOT EDIT

| File | Why no change |
|------|--------------|
| services.md (226L) | Content is good. Wiring templates comprehensive. URI hardcoded. |
| docker.md (23L) | Content is good and unique. |
| nginx.md (19L) | Content is good and unique. |
| static.md (32L) | Content is good. Framework output dirs table valuable. |
| All 20 guides/ | Content quality is good. Issue is delivery, not content. |
| All 5 decisions/ | Content is authoritative. Fix is in code routing, not content. |

### Recipes — content changes

Recipes gain ## Gotchas with runtime platform rules moved from core.md. This is specified in the main analysis doc (7 recipes, specific rules per recipe). The recipe section headings must use "## Gotchas" per lint validation.

Additionally, the per-framework proxy trust config currently in universals.md Networking section moves to relevant hello-world recipes:
- php-hello-world.md: TRUSTED_PROXIES
- python-hello-world.md: CSRF_TRUSTED_ORIGINS, SECURE_PROXY_SSL_HEADER
- ruby-hello-world.md: config.hosts, trusted_proxies
- dotnet-hello-world.md: ForwardedHeadersOptions
- java-hello-world.md: server.forward-headers-strategy
- nodejs-hello-world.md: app.set('trust proxy', true)
- go-hello-world.md: read X-Forwarded-For/Proto headers directly
- bun-hello-world.md: (same as Node.js if using Express-like)

---

## Validation criteria

After all content changes, each file must satisfy:

| File | Criterion |
|------|----------|
| universals.md | No framework-specific examples. No YAML blocks. Every statement applies to ALL services. ≤80 lines. |
| core.md | No flow-specific rules (dev/stage, startWithoutCode). No runtime-specific rules (Java, PHP, Django). All Rules & Pitfalls entries start with platform constraint, not framework behavior. |
| operations.md | No workflow-specific content (verification, troubleshooting, tool access patterns, dev vs stage). Clean operational reference only. |
| bootstrap.md | Contains ALL flow-specific rules from core.md and operations.md. No duplication with core.md Import & Service Creation (platform rules stay in core). |
| recipes | Each hello-world recipe for a language runtime has ## Gotchas with that runtime's platform rules + proxy/trust config. |
| bases/ | alpine.md and ubuntu.md explain WHEN to use, not just WHAT they are. |
