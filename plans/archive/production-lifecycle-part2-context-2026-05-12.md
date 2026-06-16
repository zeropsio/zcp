# Production Lifecycle — Part 2 Context (Code → Prod Pipeline)

**Date:** 2026-05-12
**Status:** Pre-design — Part 1 (project preparation) shipped, Part 2 (code-into-prod pipeline) to be designed + implemented
**Audience:** Future Claude Code session continuing after compaction, OR human collaborator

Tento dokument je **persistent anchor** pro Part 2 design + implementation. Lives in git, survives compaction. Read this first when picking Part 2 back up.

---

## 1. Mental model — rozdělení na dvě fáze

User explicitně přerámcoval scope na dvě úzce-související části:

| | **Part 1 — Project preparation** (shipped v9.86.1) | **Part 2 — Pipeline extension** (TO BUILD) |
|---|---|---|
| **Cíl** | Vytvořit production Zerops projekt s import yaml, managed services, runtime configurací, first deploy | Zajistit že kód se DOSTÁVÁ do prod projektu po launchi — ongoing CD lifecycle |
| **Mutates** | Source repo (`setup: prod` block via agent commit), new prod project (CreateAndImportProject) | Build integration on prod project's runtime service, possibly source repo (GH Actions workflow file), possibly GitHub Actions secret |
| **One-shot key** | YES — `WorkflowInput.LaunchKey` | LIKELY YES — same trust model |
| **State** | `.zcp/state/launch-production/{launchID}.json` | TBD — possibly extend same state file, possibly separate |
| **Atomicity** | Stateless multi-call narrowing, 4 calls, 6 statuses | TBD — likely similar |

Klíčové: **stejný trust boundary**. ZCP nikdy nemá standing access do prod. Part 2 také musí jet přes one-shot tokens.

---

## 2. Trust boundary invariants (NON-NEGOTIABLE for Part 2)

Tyto invarianty platí pro Part 1, MUSÍ platit pro Part 2.

| ID | Invariant | Pin |
|---|---|---|
| **P-LP-1** | LaunchKey/admin tokens nikdy v state/log/transcript/response | Sentinel-leak tests across 4 surfaces |
| **P-LP-2** | `ProjectAdminClient` constructor reachable jen z `workflow_launch_production.go` | Structural grep lint |
| **P-LP-3** | Source-immutability hashes na bundle | Drift detection per field |
| **P-LP-4** | Mandatory delete-key atom v terminal-success response | `TestLaunchedResponse_AlwaysContainsKeyDeletionStep` |
| **P-LP-5** | `EnvKey` nemá Value field — env values nikdy nepročítáme | Compile-time + structural |
| **P-LP-6** | Audit log append-only (`O_APPEND`, `0o600`) | 3-write line count test |

Part 2 design MUSÍ:
- Tokeny pro pipeline-setup-on-prod přijímat per call přes WorkflowInput, ne ze standing config
- `defer admin.Close()` zerovat handler na exit
- State files mode `0o600`, žádné token fields
- Audit log entries pro každý mutation
- Mandatory cleanup-key atom v terminal response

Pokud Part 2 zavádí nový interface (např. `BuildIntegrationClient`), musí mít interface segregation analogous to `ProjectAdminClient`.

---

## 3. Part 1 — co je shipped (v9.86.1)

### Co se dnes děje při launchi

User volá `zerops_workflow workflow="launch-production"` v sekvenci:

1. **scope-prompt** — collect productionProjectName, region, customDomain, keepNonHA
2. **classify-prompt** — bucket project envs (infrastructure / auto-secret / external-secret / plain-config)
3. **ready-to-launch** — bundle preview, prompt for one-shot launchKey
4. **launching → launched** (publish with launchKey):
   - Construct ProjectAdminClient + validate via GetUserInfo (captures clientID + clientUserID)
   - readSourceState env-aware (SSH in container / FS+exec in local) — reads zerops.yaml + git remote + git SHA + Discover→service type + managed deps
   - Validate `setup: prod` block exists (else: derive proposed block via `deriveProdSetupBlock`, return source-control blocker)
   - `BuildLaunchBundle` composes import yaml with prod-tier transforms (HA managed, NON_HA runtime with minContainers≥2, cpuMode DEDICATED, enableSubdomainAccess stripped, env classifications, tags including `env:prod` + `source-project:<id>` + `managed-by:zcp-launch`)
   - Write pre-mutation state with sourceSnapshot
   - `admin.CreateAndImportProject(ctx, importYAML, opts)` — synchronous, returns projectID + per-service stack IDs + per-service async processes
   - `admin.GrantSelfRole(ctx, projectID, "ADMIN")` — A.10 fix; reads existing roles + merges + writes back (full-replace PUT)
   - `pollImportedServices(ctx, admin, state)` — polls every imported process via `ops.PollProcess` (sdílí narrow `ProcessGetter` interface)
   - Audit log entry
   - Return `launched` response with composite atom (delete-key + post-checklist)

### Co prod projekt po launchi má

- `buildFromGit: <source-repo-url>` na runtime service entry
- `zeropsSetup: prod` references the `setup: prod` block agent appended to source zerops.yaml
- Managed services HA (postgres, valkey, atd. — empty/fresh)
- minContainers=2, cpuMode=DEDICATED, healthCheck (if user wrote it into prod block)
- Tags: env:prod, source-project:<sourceID>, managed-by:zcp-launch
- First deploy ran (built from source HEAD, started container)

### Co prod projekt po launchi NEMÁ (= Part 2 territory)

- **Žádná configured build-integration** — Zerops won't auto-rebuild on subsequent source pushes
- **Žádný webhook na git providera** registrovaný na prod's service
- **Žádný GitHub Actions workflow file** v repu pro prod-specific CD
- **Žádná tag-trigger semantics** — recommended pro prod per zerops-docs (`^v\d+\.\d+\.\d+$`)
- **Žádné secrety v GitHub repo settings** pro CI-driven `zcli push` to prod

`buildFromGit:` field VYTVOŘÍ first build on import — ale ongoing trigger (when user pushes to repo, when user tags release) needs explicit build-integration config on platform.

---

## 4. Part 2 — what needs to be designed

### 4.1 Otázka jádra: jak se kód dostává do prod po launchi?

Scénáře:

| # | Scénář | Probable shape |
|---|---|---|
| A | First deploy only (one-shot launch, no ongoing) | Already covered by Part 1's `buildFromGit:` |
| B | Every push to source `main` → prod rebuild | Webhook integration (Zerops dashboard OAuth) |
| C | Tag-based prod release (`v1.2.3` tag → prod rebuild) | Webhook + tag-trigger regex (recommended for prod) |
| D | CI/CD-driven (GitHub Actions runs `zcli push` to prod) | GitHub Actions workflow + repo secret + `ZEROPS_TOKEN` per env |
| E | Manual `zcli push` from local | User-driven, no automation |

**Recommended for prod (per Zerops docs):** Scenario C — tag-trigger.

User specifically said "rozsireni pipeline" (extension of the pipeline), which implies B/C/D — automated CD, not just first-deploy.

### 4.2 Kde tato config dnes v ZCP existuje (pro reuse / parallel)

Source-project má již dnes:
- **`zerops_workflow action="git-push-setup" service=<hostname> remoteUrl=<URL>`** — provisions GIT_TOKEN/.netrc/remote URL credentials on the source runtime container. Per-service-meta state.
- **`zerops_workflow action="build-integration" service=<hostname> integration=webhook|actions|none`** — wires the ZCP-managed CI integration. `webhook` = Zerops dashboard OAuth (Zerops pulls + builds), `actions` = GitHub Actions workflow runs `zcli push` from CI, `none` = no ZCP-managed.

Existing helpers (`workflow_phase5_test.go` covers 17 funcs). Per-service-meta state lives in `internal/workflow/service_meta.go::ServiceMeta`.

**For prod, ZCP nemá ServiceMeta** — prod project je out-of-MCP-session. Takže direct reuse of `action="build-integration"` nefunguje (vyžaduje meta).

Možnosti:
1. **Extend `build-integration` action with optional `targetProjectID` + `launchKey`** — uses ProjectAdminClient when targetProjectID set
2. **New action `pipeline-setup` (or workflow)** — specifically for prod-pipeline config
3. **Extend `launch-production` workflow** — add 7th status `pipeline-setup-prompt` after `launched`, configures pipeline as continuation

User decides direction.

### 4.3 SDK surface already verified (Phase A spike)

Custom domain area discovered:
- `PostProjectPublicHttpRouting` — first-class resource, safe to mutate (RESTful)
- L7 config is nginx tuning, NOT route-level (separate concern)

Build-integration / git-push area in SDK — need to verify against `zerops-go@v1.0.17/sdk/`:
- Likely something like `PostServiceStackGithubIntegration`, `PostServiceStackGitlabIntegration`, or similar
- Tag-trigger config probably field within those bodies
- `PostProjectServiceStackImport`-style for adding services + integrations as a unit (already known to work)

**Action item before Part 2 implementation:** spike against `zerops-go@v1.0.17/sdk/` for what methods exist around:
- `PostServiceStack*Integration` (github/gitlab/etc)
- `PutServiceStack*Integration`
- Webhook URL generation endpoints
- Tag-trigger / branch-trigger body fields

### 4.4 Edge cases Part 2 musí handlovat

- **Source repo má více branches** — která je prod's source? User input nebo convention?
- **Source repo má tagged releases** — `^v\d+\.\d+\.\d+$` default or user-configurable?
- **User používá GitHub Actions už pro něco jiného** — Part 2 nesmí klobnout existing workflow file
- **User nepoužívá ani GitHub ani GitLab** — pure manual `zcli push`, žádná integrace
- **Source repo je private** — webhook setup vyžaduje GH/GL OAuth, který user musí v Zerops dashboardu jednou pro celou org
- **Multi-runtime prod** — multiple runtime services in prod (frontend + api). Pipeline-setup per service.

---

## 5. Existing backlog items relevant for Part 2

- `plans/backlog/auto-wire-github-actions-secret.md` — auto-wire `ZEROPS_TOKEN` to GitHub Actions repo secret after `build-integration=actions`. Já jsem to nečetl podrobně; Part 2 design should consult.
- `plans/backlog/tool-discovery-cicd-actions-on-workflow.md` — visibility improvement for git-push-setup / build-integration discoverability.
- `plans/backlog/rename-closedeploymode-to-deliverymode.md` — vocabulary refactor; touches dev→stage delivery story.

Plus newly added:
- `plans/backlog/rejected/launch-userroles-in-import-yaml.md` — Part 1 rejected reason; Part 2 not affected.

---

## 6. Reference files for full context

Read these on Part 2 resume:

| File | What it has |
|---|---|
| `plans/archive/production-lifecycle-2026-05-11.md` | Full v2 plan, 5 phases, 14 atoms, all invariants P-LP-1..6, scope cuts list |
| `docs/spec-launch-production-platform-spike.md` | Phase A platform findings, A.10 finding + fix, SDK shapes |
| `internal/tools/workflow_launch_production.go` | Handler — read this for the 6-state machine + executeLaunchMutation + readAndValidateSourceState |
| `internal/tools/launch_source_read.go` | Env-aware source-state read pattern |
| `internal/tools/launch_prod_setup_derive.go` | Item #6 — concrete setup:prod block derivation pattern (useful for Part 2 if pipeline config needs derived suggestion) |
| `internal/tools/launch_state.go` | State file + audit log shape (Part 2 likely extends or mirrors) |
| `internal/platform/project_admin.go` | ProjectAdminClient — Part 2 likely needs analogous interface for build-integration on prod |
| `internal/content/atoms/launch-*.md` | 6 atoms — read for tone + Item #6 derivation pattern |
| `CLAUDE.md` + `CLAUDE.local.md` | Engineering discipline — strict TDD, no half-finished, clean code |
| `docs/spec-workflows.md §9` | Export workflow — analogous stateless multi-call narrowing pattern |

---

## 7. Open questions for the user (ask before designing Part 2)

These need user input — don't guess.

1. **Scope of Part 2:** Tag-trigger CD only? Or also webhook? Or also GitHub Actions? Or all three as integration= choices?
2. **Trigger semantics:** Tag-regex default `^v\d+\.\d+\.\d+$`? Or simpler `v*`? Or user-supplied always?
3. **Extension shape:**
   - (a) Extend `launch-production` workflow s 7th status (pipeline-setup-prompt)?
   - (b) New separate workflow `setup-production-pipeline`?
   - (c) Extend existing `action="build-integration"` s `targetProjectID` + `launchKey`?
4. **One-shot key continuity:** Re-use same launchKey from Part 1 (within state window)? Or always require fresh token for Part 2?
5. **Source repo mutations:** Part 2 will write `.github/workflows/zerops-prod.yml`? Or user does that manually?
6. **GH Actions secret auto-wire:** Bring back the backlog item (`auto-wire-github-actions-secret.md`) — yes/no?
7. **Migration guidance (out-of-band but related):** Add to `launch-write-prod-setup` + `launch-post-checklist` atoms? User asked at end of Part 1 session and answer is pending.

---

## 8. Quick reference — current state at session-handoff

- Branch: `main`
- Last release: **v9.86.1** (deferred-items cleanup, A.10 included via v9.84.1)
- All tests passing: unit + e2e (with admin token: `ZCP_E2E_PROD_LAUNCH=1 ZCP_LAUNCH_KEY=<admin>`)
- Lint: lint-fast + lint-local clean
- Atom corpus pin density: closed (knownUnpinnedAtoms empty)
- No outstanding feature branches in flight

User confirmed scope cuts:
- Data migration: **out** (app owns migrations + seeds; ZCP doesn't transfer data — confirmed in original Q2 + reaffirmed mid-Part-2 discussion)
- Custom domain L7 mutation: **backlogged** (works via PostProjectPublicHttpRouting but user-on-the-line for v1)
- Rollback tool: **backlogged**
- Cost ack gate: **deferred** ("ted bych neresil")

Open atoms-level gap (discussed at session end, not yet resolved):
- `launch-write-prod-setup` doesn't mention initCommands / migrations
- `launch-post-checklist` doesn't mention empty DB state
- User asked "do we fix this?" — **pending decision**. Trivial fix (~50 LOC + atom edits + readiness check). Probably yes.

---

## 9. Where Part 2 likely lands

Anticipated file additions for Part 2 (subject to user direction on §7):
- `internal/platform/build_integration_client.go` (new) — analogous to ProjectAdminClient, narrow interface for build-integration mutations on prod
- `internal/ops/pipeline_setup.go` (new) — bundle composer for build-integration config
- `internal/tools/workflow_pipeline_setup.go` (new) OR extension of existing files — handler
- `internal/content/atoms/pipeline-*.md` (new) — atom guidance
- `internal/tools/launch_state.go` — possibly extend with pipeline-config fields
- `docs/spec-pipeline-setup.md` (new) — design doc analogous to spec-launch-production-platform-spike
- 7th status added to `topology.LaunchProductionStatus` if extending Part 1 workflow

Total estimate: ~600-1200 LOC + atoms + tests, similar magnitude to Part 1's mutation pipeline phase.

---

## 10. Hard rules (re-emphasis)

1. **No standing prod access ever.** Part 2 one-shot key pattern same as Part 1.
2. **No half-finished implementations.** Don't ship handler with "// follow-up: source-control reads" type TODOs — they degrade to permanent technical debt (lesson from Phase D.2 history).
3. **TDD mandatory** per CLAUDE.md. Test substrate at every phase: unit + integration + e2e.
4. **Atom corpus discipline.** Every new atom needs scenarios_test.go pin OR explicit coverageExempt rationale. Strict checks already in place — don't bypass.
5. **English everywhere in code/atoms/specs.** Czech only in conversation.
6. **Existing SDK methods first.** Don't reinvent — `PostServiceStackGithubIntegration` (if exists) is probably the right tool, not handrolled webhook calls.
