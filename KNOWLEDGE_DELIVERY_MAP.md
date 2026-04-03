# ZCP Knowledge Delivery Map — Complete Flow

**Purpose**: Map HOW knowledge reaches the LLM agent in the ZCP codebase. Not WHAT content says, but HOW it's delivered.

---

## 1. Initial Context (Before Any Tool Call)

### MCP System Prompt Assembly — `internal/server/server.go:40-54`

**Entry point**: `New()` creates MCP server with Instructions via `BuildInstructions()`.

```go
srv := mcp.NewServer(
    &mcp.Implementation{Name: "zcp", Version: Version},
    &mcp.ServerOptions{
        Instructions: BuildInstructions(ctx, client, authInfo.ProjectID, rtInfo, stateDir),
        KeepAlive:    30 * time.Second,
        ...
    },
)
```

**What the LLM sees FIRST** (via `BuildInstructions()` — `internal/server/instructions.go:40-86`):

1. **Section A: Base Instructions** (`instructions.go:17-24, 47-53`)
   - Static: "ZCP manages Zerops PaaS infrastructure"
   - Workflow types: bootstrap, deploy, recipe, cicd
   - Direct tools list
   - **Overridable** via `ZCP_INSTRUCTION_BASE` env var

2. **Section B: Workflow Hint** (`instructions.go:55-59`)
   - From `.zcp/state/` registry (if active/resumable sessions exist)
   - Format: "Active workflow: bootstrap (step 3/5: generate)"
   - Via `buildWorkflowHint()` → `workflow.ListSessions()` → `workflow.LoadSessionByID()`

3. **Section C: Environment Context** (`instructions.go:61-77`)
   - **Container environment** (`instructions.go:26-29`): "Control plane container — manages OTHER services"
   - **Local environment** (`instructions.go:31-34`): "Local machine — code in working directory"
   - Detects via `rt.InContainer` (runtime detection)
   - **Overridable** via `ZCP_INSTRUCTION_CONTAINER` or `ZCP_INSTRUCTION_LOCAL`

4. **Section D: Project Summary** (`instructions.go:79-83`)
   - Via `buildProjectSummary()` → calls `client.ListServices()` → live Zerops API
   - **Three parts**:
     1. Service listing (name, type, status, labels)
     2. Post-bootstrap orientation (per-service guides)
     3. Router offerings (workflow suggestions)

---

## 2. zerops_knowledge Tool (Agent Calls Explicitly)

**Location**: `internal/tools/knowledge.go:50-167`

**Entry**: `RegisterKnowledge()` defines the tool, handler at line 59.

**Four mutually exclusive modes**:

### Mode 1: `scope="infrastructure"`
**Exact output** (`tools/knowledge.go:94-122`):
1. **Universals prepended first**: `store.GetUniversals()` → `zerops://themes/universals`
2. **Core appended second**: `store.GetCore()` → `zerops://themes/core`
3. **Live stack list prepended (if available)**: `knowledge.FormatStackList(types)` from live API
4. **Order**: Live stacks → Universals → Core

**File sources**:
- `zerops://themes/universals` (`internal/knowledge/briefing.go:220-225`) — prepended via `prependUniversals()`
- `zerops://themes/core` (`internal/knowledge/engine.go:192-199`) — full content
- Live service types from cache (`internal/tools/knowledge.go:113-116`) — via `StackTypeCache.Get()`

### Mode 2: `query="text"`
**Exact output** (`tools/knowledge.go:125-128`):
- Calls `store.Search(query, limit)` → `internal/knowledge/engine.go:109-164`
- **Search mechanics**:
  - Query expansion via aliases (e.g., "postgres" → "postgres postgresql")
  - BM25-style scoring: title match +2.0, content match +1.0
  - Returns: `[]SearchResult` with URI, title, score, snippet
  - **Recursive scope**: Searches ALL docs (themes, recipes, guides, decisions, bases)

**Search result sources**: All 5 embedded directories (`documents.go:10-14`):
- `themes/` — platform reference, universals, services, operations
- `bases/` — alpine, docker, nginx, static, ubuntu
- `recipes/` — 29 framework guides
- `guides/` — docs-driven content
- `decisions/` — choose-database, choose-cache, etc.

### Mode 3: `runtime="..." | services=[...]` (Briefing)
**Exact output** (`tools/knowledge.go:131-148`):

Calls `store.GetBriefing(runtime, services, mode, liveTypes)` — `internal/knowledge/briefing.go:18-99`.

**Layered composition** (in order):
1. **Live service stacks** (`briefing.go:27-31`): `FormatServiceStacks(liveTypes)` — current versions running
2. **Runtime guide** (`briefing.go:33-44`): 
   - `normalizeRuntimeName()` → `getRuntimeGuide(slug)` 
   - Resolution: `recipes/{slug}-hello-world` → `recipes/{slug}` → `bases/{slug}`
3. **Matching recipes hint** (`briefing.go:47-58`): Lists recipes matching runtime base (from `runtimeRecipeHints` map)
4. **Service cards** (`briefing.go:60-74`): For each service, extract H2 section from `zerops://themes/services`
5. **Wiring syntax** (`briefing.go:77-83`): `getWiringSyntax()` from `zerops://themes/services` H2 section
6. **Decision hints** (`briefing.go:85-90`): Compact summaries from `decisions/` files
7. **Version check** (`briefing.go:92-96`): `FormatVersionCheck()` — compares local versions vs live

**Mode adaptation** (`tools/knowledge.go:136-137`):
- `resolveKnowledgeMode()`: If `input.Mode` empty, auto-detects from active session
  - Bootstrap session → uses `Plan.Mode()` (dev/standard/simple)
  - Deploy session → uses `Deploy.Mode`
  - No session → unfiltered

### Mode 4: `recipe="name"`
**Exact output** (`tools/knowledge.go:150-161`):

Calls `store.GetRecipe(name, mode)` — `internal/knowledge/briefing.go:105-139`.

**Prepended content**:
1. **Universals**: `prependRecipeContext()` → `prependUniversals()` → `GetUniversals()`
2. **Mode adaptation**: `prependModeAdaptation(mode, runtimeType)` returns mode-specific pointer (dev/standard/simple)

**Resolution chain**:
1. Exact match: `zerops://recipes/{name}`
2. Fuzzy match: `findMatchingRecipes()` — prefix, substring, content match
3. Multiple matches: Return disambiguation list with TL;DRs
4. No match: Error with available recipe list

**Recipe auto-runtime detection** (`briefing.go:144-156`):
- Reverse-lookup `runtimeRecipeHints` map
- Skip "static" to prefer richer Node.js context

---

## 3. Workflow Guidance Assembly — Automatic Per Step

**Parent controller**: `internal/tools/workflow.go` → calls `workflow.Engine.GetGuidance(step, iteration)`

### Bootstrap Workflow Guidance

**Flow**: 5 steps (discover, provision, generate, deploy, close) — `internal/workflow/bootstrap.go`

**Guidance source**: `internal/workflow/bootstrap_guide_assembly.go:13-40`

```go
func (b *BootstrapState) buildGuide(step, iteration, env, kp) string
    → assembleGuidance(GuidanceParams{...}) [guidance.go:27-44]
```

**Layer 1: Static Guidance** (`bootstrap_guidance.go:20-95`):
- Extracted from `workflows/bootstrap.md` via `ExtractSection(md, step)`
- Each step has a `<section name="{step}">` marker in markdown
- **Environment-aware routing** (`ResolveProgressiveGuidance()`):
  - Discover/Provision: base + optional local addendum
  - Generate: complex multi-mode routing (local self-contained vs container multi-section)
  - Deploy: local self-contained vs SSH base + conditionals
  - Close: fixed guidance

**Layer 2: Runtime Knowledge** (`guidance.go:64-97`):
- **At provision**: `getCoreSection(kp, "import.yml Schema")`
  - Extracts H2 section from `zerops://themes/core`
  - Via `doc.H2Sections()` cache (`documents.go:30-37`)

- **At generate**: 
  - Runtime briefing: `kp.GetBriefing(runtimeBase, nil, mode, nil)`
  - Dependency briefing: `kp.GetBriefing("", dependencyTypes, mode, nil)`
  - Env vars: `formatEnvVarsForGuide(discoveredEnvVars)` — markdown table of `${hostname_varName}` refs
  - zerops.yaml schema: `getCoreSection(kp, "zerops.yaml Schema")`

- **At deploy**: none (bootstrap.md deploy section covers all deploy semantics)

Note: Rules & Pitfalls and Schema Rules deliberately excluded from bootstrap.
Runtime briefings already provide correct patterns for each runtime; cross-runtime
rules are noise. The 400+ line bootstrap.md deploy section covers tilde syntax,
deploy modes, path invariants, and common issues.

**Layer 3: Iteration Delta** (`guidance.go:28-32`):
- If `iteration > 0`, calls `BuildIterationDelta()` which **replaces** normal guidance
- Escalating recovery tactics per iteration level

**Full assembly** (`guidance.go:27-44`):
```
assembleGuidance():
  ├─ Iteration delta (if iteration > 0) → RETURN early
  ├─ Static guidance [ResolveProgressiveGuidance()]
  └─ Knowledge layers [assembleKnowledge()]
      ├─ Platform model (discover)
      ├─ import.yaml Schema (provision)
      ├─ Runtime + dependency briefings (generate)
      ├─ Env vars markdown (generate)
      └─ zerops.yaml Schema (generate)
```

### Deploy Workflow Guidance

**Flow**: 3 steps (prepare, deploy, verify) — `internal/workflow/deploy.go`

**Guidance source**: `internal/workflow/deploy_guidance.go:24-151`

**Prepare step** (`deploy_guidance.go:24-73`):
```
buildPrepareGuide():
  ├─ Service summary (targets, mode, strategy)
  ├─ Checklist (zerops.yml entries, env var refs, runtime-specific rules)
  ├─ Platform rules (deploy = new container, ${} silent fallback, build ≠ run)
  ├─ Strategy note (push-dev/ci-cd/manual context)
  └─ Knowledge pointers [buildKnowledgeMap()]
```

**Deploy step** (`deploy_guidance.go:75-141`):
```
buildDeployGuide():
  ├─ Iteration escalation (if iteration > 0) [writeIterationEscalation()]
  ├─ Mode-specific workflow (standard/dev/simple/local) [writeStandardWorkflow(), etc.]
  ├─ Key facts (environment-specific, subdomain persistence)
  ├─ Code-only changes shortcut
  └─ Diagnostics (build failed, container didn't start, unreachable, health)
```

**Verify step** (`deploy_guidance.go:144-150`):
```
buildVerifyGuide():
  └─ Extracted from workflows/deploy.md [deploy-verify section]
```

**No knowledge injection** in deploy guidance — all context is preprepared in workflow state.

### Recipe Workflow Guidance

**Flow**: 6 steps (research, provision, generate, deploy, finalize, close) — `internal/workflow/recipe.go`

**Guidance source**: `internal/workflow/recipe_guidance.go:14-31`

```go
func (r *RecipeState) buildGuide(step, iteration, kp) string
    → resolveRecipeGuidance(step, plan) [guidance.go:44-88]
    + assembleRecipeKnowledge(step, plan, discoveredEnvVars, kp) [guidance.go:90-135]
```

**Static guidance** (`recipe_guidance.go:44-88`):
- Extracted from `workflows/recipe.md`
- Research: tier-specific (showcase vs minimal)
- Generate: base + fragments sections
- Other steps: step-name sections

**Knowledge injection** (`recipe_guidance.go:90-143`):
- **Research**: none (agent fills form using own knowledge + on-demand zerops_knowledge)
- **Provision**: `getCoreSection(kp, "import.yaml Schema")`
- **Generate**: 
  - Runtime briefing: `kp.GetBriefing(runtimeBase, nil, "", nil)`
  - Discovered env vars: `formatEnvVarsForGuide()`
  - zerops.yaml schema: `getCoreSection(kp, "zerops.yaml Schema")`
- **Deploy**: none (static guidance + iteration loop)
- **Finalize**: `getCoreSection(kp, "import.yaml Schema")`
- **Close**: none

Note: Rules & Pitfalls and Schema Rules are deliberately excluded from recipe workflow.
Recipe is about CREATING knowledge — the agent discovers framework pitfalls and documents
them rather than copying from a rule sheet. Bootstrap injects these because it CONSUMES
rules to create correct deployments.

---

## 4. Knowledge Store — Document Loading & Access

**Location**: `internal/knowledge/documents.go`, `internal/knowledge/engine.go`

**Embedded directories** (`documents.go:10-14`):
```go
//go:embed themes/*.md bases/*.md all:recipes all:guides all:decisions
var contentFS embed.FS
```

**5 directories**:
1. **themes/** — Platform reference (core, universals, services, operations)
2. **bases/** — Infrastructure runtimes (alpine, docker, nginx, static, ubuntu)
3. **recipes/** — 29 framework guides (laravel, nextjs, echo-go, etc.)
4. **guides/** — Docs-driven content
5. **decisions/** — Decision frameworks (choose-database, choose-cache, etc.)

**Loading** (`documents.go:39-59`):
- `loadFromEmbedded()` walks all dirs, parses each `.md` file
- Creates `Document` type with metadata (title, keywords, TLDR, description)
- Returns `map[uri]*Document` — cached in singleton `GetEmbeddedStore()`

**Document structure** (`documents.go:61-90`):
- Path: `themes/core.md`
- URI: `zerops://themes/core`
- Title: extracted from H1
- Keywords: from `## Keywords` section (legacy)
- TLDR: from `## TL;DR` section (legacy)
- Content: markdown body (without frontmatter)
- Description: frontmatter > TLDR > first paragraph

**H2 section parsing** (`documents.go:30-37`):
- `H2Sections()` lazy-parses document by H2 headers
- Respects fenced code blocks (doesn't split inside triple-backtick)
- Returns `map[sectionName]sectionContent`
- Cached via `sync.Once` for thread safety

**Section extraction** (`sections.go:159-195`):
- `getRuntimeGuide(slug)`: resolution chain for runtime guides
- `getServiceCard(name)`: H2 section from `services.md`
- `getWiringSyntax()`: H2 section from `services.md`
- `getRelevantDecisions()`: compact decision summaries based on stack

---

## 5. File → Delivery Mapping (Complete)

### `zerops://themes/core` (platform model, YAML schemas, rules)

**Delivery paths**:
1. **scope="infrastructure"** → returned second (after universals)
2. **bootstrap provision step** → `getCoreSection("import.yaml Schema")`
3. **bootstrap generate step** → `getCoreSection("zerops.yaml Schema")`
5. **recipe provision step** → `getCoreSection("import.yaml Schema")`
6. **recipe generate step** → `getCoreSection("zerops.yaml Schema")` + runtime briefing + env vars
7. **recipe finalize step** → `getCoreSection("import.yaml Schema")`
8. **deploy prepare step** → knowledge pointers injected via `buildKnowledgeMap()`
9. **query search** → if "schema" or "rules" in query

**Access method**: Stored as H2 sections in document, extracted via `doc.H2Sections()[name]`

### `zerops://themes/universals` (platform principles, common concepts)

**Delivery paths**:
1. **scope="infrastructure"** → returned first (prepended to core)
2. **recipe={name}** → prepended before recipe content
3. **briefing** → NOT included (briefing focuses on stack-specific, not universals)
4. **query search** → if "principle", "universal", "best practice" in query

**Access method**: Full content via `GetUniversals()`

### `zerops://themes/services` (service cards, wiring patterns)

**Delivery paths**:
1. **briefing** → layers 4-5 (service cards + wiring syntax)
2. **query search** → if service name in query
3. **Internal only**: `getServiceCard()` extracts H2 section per service

**Access method**: 
- `getServiceCard()` → `doc.H2Sections()[normalizedName]`
- `getWiringSyntax()` → `doc.H2Sections()["Wiring Syntax"]`

### `zerops://themes/operations` (operations guide)

**Delivery path**: 
1. **query search only** — no structured delivery

### `zerops://recipes/*` (29 frameworks)

**Delivery paths**:
1. **recipe=name** → full content (with universals prepended, mode adaptation)
2. **briefing** → via `matchingRecipes()` hint list (layer 3)
3. **briefing** → via `getRuntimeGuide()` resolution chain (layer 2) for hello-world recipes
4. **query search** → if framework name in query
5. **bootstrap generate step** → via runtime briefing lookup (if recipe contains setup code)

**Access methods**:
- `GetRecipe(name, mode)` → full content with context
- `getRuntimeGuide(slug)` → resolution: `recipes/{slug}-hello-world` → `recipes/{slug}` → fallback
- `matchingRecipes(runtimeBase)` → filtered list for hint

### `zerops://bases/*` (alpine, docker, nginx, static, ubuntu)

**Delivery paths**:
1. **briefing** → via `getRuntimeGuide()` fallback (if no recipe)
2. **query search** → if base name in query
3. **bootstrap generate step** → if runtime maps to base

**Access method**: `getRuntimeGuide()` resolution chain fallback

### `zerops://guides/*` (docs-driven content)

**Delivery path**:
1. **query search only** — no structured delivery

### `zerops://decisions/*` (choose-database, choose-cache, etc.)

**Delivery paths**:
1. **briefing** → layer 6 (decision hints with summaries)
2. **query search** → if decision name in query

**Access method**:
- `getRelevantDecisions()` builds compact list based on runtime/services
- `getDecisionSummary()` extracts TL;DR + first paragraph

---

## 6. Knowledge Store Provider Interface

**Location**: `internal/knowledge/engine.go:28-37`

```go
type Provider interface {
    List() []Resource
    Get(uri string) (*Document, error)
    Search(query string, limit int) []SearchResult
    GetCore() (string, error)
    GetUniversals() (string, error)
    GetBriefing(runtime string, services []string, mode string, liveTypes []platform.ServiceStackType) (string, error)
    GetRecipe(name, mode string) (string, error)
}
```

**All methods return best-effort** (errors handled gracefully, never panic).

---

## 7. Workflow State Persistence & Hints

**Location**: `internal/workflow/session.go`, `internal/workflow/bootstrap.go`

**State stored at**: `.zcp/state/` (relative to working directory)

**Session structure**:
- `WorkflowState` per session: Bootstrap, Deploy, Recipe, or CICD
- Each has `Plan`, `Strategies`, `DiscoveredEnvVars`, `Steps[]` with attestations
- Persisted as JSON (one file per session ID)

**Hint generation** (`internal/server/instructions.go:88-140`):
- `buildWorkflowHint()` reads registry
- Returns active session hints (step 3/5) + resumable dead-PID hints
- Injected into MCP instructions BEFORE any tool call

---

## 8. Live Data Integration

### Live Service Stack Types (`internal/ops/stack_cache.go`)

**Caching**: `StackTypeCache` with TTL (default 5 minutes)

**Usage in briefing**:
- `FormatServiceStacks(liveTypes)` → layer 1 of GetBriefing
- Lists current versions running vs available

**Usage in scope**:
- Prepended to core/universals content
- Shows version choices available

### Service Classification (`internal/server/instructions.go:158-198`)

**Classification**: 
- Bootstrapped (has ServiceMeta with complete fields)
- Unmanaged runtime (no meta or incomplete)
- Managed infrastructure (DB, cache, storage)

**Used in orientation**: Annotates each service with bootstrap status, mount paths

---

## 9. Mode Adaptation & Routing

**Mode filter** (`tools/knowledge.go:26-47`):
- Explicit `input.Mode` parameter overrides auto-detection
- Auto-detection from session: Bootstrap.PlanMode() or Deploy.Mode
- Passed to `GetBriefing(mode)` and `GetRecipe(mode)`

**Mode-specific sections**:
- Recipes: prepend `prependModeAdaptation()` — dev/standard/simple pointer
- Bootstrap generate: include mode-specific sections from bootstrap.md
- Briefing: NOT mode-filtered (all modes get same briefing)

**Router** (`internal/workflow/router.go`):
- Suggests workflows based on service state, active sessions, unmanaged runtimes
- Formatted as offerings with priority + hint
- Injected into project summary (buildProjectSummary → FormatOfferings)

---

## 10. Design Principles (Verified from Code)

1. **Layered composition** — briefing assembles 7 layers; guides layer static + knowledge
2. **Best-effort knowledge** — errors silently skipped; never block guidance
3. **Section extraction** — H2 sections are the granular knowledge unit
4. **No global context pollution** — each tool call is stateless; state only in workflow JSON
5. **Mode-aware prepending** — universals + core prepended once; recipes adapted per mode
6. **Live integration optional** — all knowledge works without live API; live data enriches
7. **Query expansion** — aliases expand "postgres" → "postgres postgresql"
8. **Structured vs search** — scope/briefing/recipe are structured; query is full-text search
9. **Iteration escalation** — iteration > 0 REPLACES normal guidance entirely
10. **Environment-aware routing** — same guidance extracted differently for local vs container

---

## Summary: Two Knowledge Flows

### **Explicit (Agent calls):**
- `zerops_knowledge scope=infrastructure` → scope + briefing + recipe + query via tool UI
- Agent selects mode (query specific, scope for reference, briefing for stack, recipe for framework)

### **Implicit (Workflow automation):**
- `zerops_workflow action="start"` → guidance assembled per step
- Bootstrap/deploy/recipe steps call `buildGuide()` automatically
- Guidance = static section + knowledge layers
- Iteration > 0 replaces entire guidance with recovery tactics

**Key insight**: Knowledge is delivered via TWO independent paths:
1. **MCP tool** for on-demand agent queries (scope, briefing, recipe, search)
2. **Workflow guidance** for automated step context (static + injected layers)

Both paths use the same underlying Provider interface and document store, but deliver differently.
