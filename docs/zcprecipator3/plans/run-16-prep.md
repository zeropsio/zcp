# Run-16 architecture prep — fields/comments separation + content phases

**Status**: pre-implementation design doc. Triple-verifiable: every file:line, every spec section, every reference quote in this doc is cite-checked against the actual artifact.

**Scope**: a structural reshape of how recipe content is authored — separates the **deploy-critical layer** (yaml fields, code) from the **documentation layer** (yaml comments, READMEs, KB, IG, CLAUDE.md). Adds two new phases (codebase content, env content) between feature and finalize. Closes 4 of 7 R-15-N defects by construction; the other 3 (R-15-1 subdomain, R-15-2 slug list, R-15-7 classification adoption) sit on orthogonal surfaces and are addressed separately.

**Predecessor**: this prep doc supersedes [run-15-prep.md](run-15-prep.md). The run-15 readiness plan addressed content-quality at the validator layer (post-hoc structural caps); this run-16 prep addresses content-quality at the authoring-architecture layer (engine-emitted structure with content-phase synthesis from structured facts).

**Reading order**:

1. §1 — the shift in three sentences.
2. §2 — the corrections that anchor the design (each was a distinct error in the run-15 walk).
3. §3 — the empirically-derived IG taxonomy (read [/Users/fxck/www/recipes/laravel-jetstream/](../../../../recipes/laravel-jetstream/) and [/Users/fxck/www/recipes/laravel-showcase/](../../../../recipes/laravel-showcase/) READMEs alongside this section to verify).
4. §4 — the phase shape.
5. §5-§9 — engine implementation by file.
6. §10-§11 — sequencing + risks.
7. §12 — defect-closure mapping.
8. §13 — triple-confirmation checklist (the fresh instance verifies in this order).

---

## §1. The shift in three sentences

Today every content surface is authored mid-deploy by the agent that's also wrestling the codebase to boot — content quality slips because the agent is wearing two hats. Tomorrow the deploy phases (scaffold + feature) write **only deploy-critical fields and code, plus structured `porter_change` / `field_rationale` facts capturing every non-obvious choice at densest context**; two new content phases (codebase content, env content) read those facts plus full spec verbatim and synthesize all documentation surfaces with single-author / cross-surface-aware authoring. The engine emits structure (templates, slots) and refuses structurally-wrong fragment bodies at record-fragment time, so post-hoc validators that polish structure get retired in favour of slot-shape refusal at the source.

---

## §2. The corrections that anchor this design

Each of these was an error I made in the run-15 walk that the user corrected. They gate the design.

### §2.1 Workspace yaml vs deliverable yaml

Two distinct yaml shapes per [docs/zcprecipator3/system.md §2](../system.md):

- **Workspace yaml** (used at provision via `zerops_import`): services-only, no project block, dev runtimes carry `startWithoutCode: true`, no ports declared. **Cannot carry `enableSubdomainAccess: true`** — there is no port stack for the flag to gate against.
- **Deliverable yaml** (the published tier import.yaml; what end-users click-deploy): full structure, ports declared, `enableSubdomainAccess: true` per HTTP-serving service.

The §A run-15 fix changed the eligibility predicate from `Ports[].HTTPSupport` (deploy-time signal) to `detail.SubdomainAccess` (import-time signal). For end-users that's correct. For recipe authoring it broke the existing path because workspace yaml can't carry `enableSubdomainAccess: true`. **Manual `zerops_subdomain action=enable` IS the happy path during recipe authoring scaffold/feature deploys** — the §A criterion 52 ("zero manual enables") was unreachable by the workflow's import shape.

R-15-1 closure (separate from this run-16 prep): OR both signals — `detail.SubdomainAccess: true` (end-user path) OR `Ports[].HTTPSupport: true` AND no prior subdomain (recipe-authoring path). Engine-side fix at [internal/tools/deploy_subdomain.go::maybeAutoEnableSubdomain](../../../internal/tools/deploy_subdomain.go) — out of scope for this prep doc.

### §2.2 Fields vs comments split

For zerops.yaml:

- **Fields** (`build:`, `run.envVariables:`, `ports:`, `initCommands:`, `start:`, `deployFiles:`, `readinessCheck:`) — must exist at scaffold time because [`zerops_deploy`](../../../internal/tools/) consumes them. Feature legitimately extends them when a feature needs new env vars or new init commands. **Both phases need write access to the field layer.**
- **Comments** (the causal "why" prose above each block) — pure documentation. Has zero deploy-time consumer. The deploy works fine with no comments. The comments only matter at IG #1 stitch time and to a porter reading the yaml later. **Can be deferred to a phase with full context.**

The same split applies to every surface (§4 below). The engine doesn't need to model yaml STRUCTURE (scaffold + feature can author fields fine); the engine needs to defer COMMENTS to a phase where the writer has full context.

### §2.3 Comments need full field context

Deferring comment authoring isn't enough — the writer needs to know what each field IS so it can explain WHY it was chosen. The "why" is currently in the deploy-phase agent's head and gets lost across phases unless captured.

Solution: the deploy phases record **structured `field_rationale` and `porter_change` facts** at the moment of writing. Content phase reads the facts + the committed yaml + the committed code + spec verbatim and authors documentation. Facts are the bridge.

### §2.4 Filtered fact recording — only spec-classifiable changes

The agent does NOT record every code change as a fact. Recording rule mirrors the spec's [classification × surface compatibility](../../spec-content-surfaces.md#classification--surface-compatibility) table — only changes that classify as **platform-invariant**, **intersection**, **scaffold-decision (config)**, or **scaffold-decision (code, porter literally copies)** become facts. **Framework-quirk, library-metadata, self-inflicted, scaffold-decision-recipe-internal, pure operational** classes are discarded or stay as in-code documentation only.

The agent classifies before recording. Engine refuses incompatible (class × candidate-surface) pairs at record-fact time — same shape as F.3 today applied to facts.

### §2.5 IG taxonomy derived from references, not invented

Four classes emerged from reading both reference recipes' IG content + run-15's IG verbatim. §3 below.

---

## §3. The empirically-derived IG taxonomy

Derived from three recipes:

- [/Users/fxck/www/laravel-jetstream-app/README.md](../../../../laravel-jetstream-app/README.md) — 4 IG items (human-authored)
- [/Users/fxck/www/laravel-showcase-app/README.md](../../../../laravel-showcase-app/README.md) — 5 IG items (early recipe-flow)
- [/Users/fxck/www/zcp/docs/zcprecipator3/runs/15/apidev/README.md](../runs/15/apidev/README.md) — 5 numbered items + 1 unnumbered (run-15 dogfood)

### §3.1 Class A — Engine-emittable structural item

**One per codebase: IG #1 = "Adding `zerops.yaml`".**

Engine reads the committed `<cb.SourceRoot>/zerops.yaml` from disk, generates a 1-2-sentence intro, embeds the yaml verbatim in a fenced code block. No agent authoring at all.

**Already implemented today** at [internal/recipe/assemble.go:204-215::codebaseIGItem1](../../../internal/recipe/assemble.go#L204-L215) and [:170-195::injectIGItem1](../../../internal/recipe/assemble.go#L170-L195). The intro sentence is generated by [yamlIntroSentence](../../../internal/recipe/assemble.go#L223) which scans for setups, initCommands, readinessCheck/healthCheck.

**No change needed for this class.** It's the precedent that the other classes follow.

### §3.2 Class B — Universal-for-role

**Per HTTP-serving codebase (RoleAPI, RoleFrontend, RoleMonolith), N items where N = the platform contract's surface area for that role.**

Verbatim examples:

| Recipe | Item | Source |
|--------|------|--------|
| Jetstream IG #2 | "Add Support For Object Storage" — composer require + Jetstream config update | jetstream README §2 |
| Showcase IG #2 | "Trust the reverse proxy" — `$middleware->trustProxies(at: '*')` in `bootstrap/app.php` | showcase README §2 |
| Run-15 apidev IG #2 | "Bind `0.0.0.0` and trust the L7 proxy" | run-15 apidev README §2 |
| Run-15 apidev IG #5 | "Drain in-flight requests on SIGTERM" | run-15 apidev README §5 |

Pattern: every HTTP-serving role gets these contracts independently of framework. The framework-specific syntax IS the diff slot; the why prose is engine-knowable.

**Engine knows from `cb.Role` and `cb.BaseRuntime`**:
- Role=API/Frontend/Monolith → "Bind 0.0.0.0 + trust L7" applies
- Role=API/Frontend/Monolith + nodejs → "Drain on SIGTERM" applies
- Role=API/Frontend/Monolith + php-nginx → no SIGTERM item (PHP-FPM handles it)
- Role=Worker → "Boot application context, not HTTP server" applies (NestJS pattern)

**Engine-emit decision: YES.** Engine pre-emits a structured `porter_change` fact per applicable item; agent fills only the framework-specific code-diff slot. The why prose comes from authoritative platform atoms ([content/principles/](../../../internal/recipe/content/principles/)).

### §3.3 Class C — Universal-for-recipe (per managed service consumed)

**Per managed service the codebase consumes, one IG item teaching how to connect.**

Verbatim examples:

| Recipe | Item | Source |
|--------|------|--------|
| Jetstream IG #2 | "Add Support For Object Storage" — `composer require league/flysystem-aws-s3-v3` | jetstream §2 |
| Showcase IG #3 | "Configure Redis client" — `composer require predis/predis`, `REDIS_CLIENT=predis` | showcase §3 |
| Showcase IG #4 | "Configure S3 object storage" — `composer require league/flysystem-aws-s3-v3`, path-style | showcase §4 |
| Showcase IG #5 | "Configure Meilisearch search" — `composer require laravel/scout meilisearch/meilisearch-php` | showcase §5 |
| Run-15 apidev IG #4 | "Connect to NATS with credentials as connect options" — connect-options syntax not URL-embedded | run-15 §4 |
| Run-15 apidev IG #3 | "Read managed-service credentials from own-key aliases" — DB_HOST: ${db_hostname} pattern | run-15 §3 |

Pattern: per-service idiom. Engine knows the service type from `plan.Services`; per-service connection idioms live in `zerops_knowledge runtime=<type>` content (the citation-map atom names them).

**Engine-emit decision: PARTIAL.** Engine pre-emits the structured fact (heading slot + topic) — "Configure <service-type> client" — and pre-fills the why prose from the per-service knowledge atom. Agent fills the framework-specific `composer require` / `npm install` / connect-options diff slot.

For "Read managed-service credentials from own-key aliases" — engine knows the alias pattern from `plan.ProjectEnvVars`; pre-emits a structured fact with the cross-service-env-var-aliases template; agent fills the framework-specific `process.env.X` / `env('X')` syntax.

### §3.4 Class D — Framework × scenario (feature-discovered)

**Items only relevant because of a specific scenario the recipe demonstrates.**

Verbatim examples:

| Recipe | Item | Why it's scenario-specific |
|--------|------|----------------------------|
| Jetstream IG #4 | "Setup Production Mailer" — change MAIL_* for real SMTP | Only relevant if porter wants production mail (jetstream demo uses Mailpit) |
| Run-15 apidev (unnumbered) | "Custom response headers across origins" — `app.enableCors({ exposedHeaders: ['X-Cache'] })` | Only relevant because the cache panel exposes X-Cache cross-origin |
| Run-15 appdev (unnumbered) | "Streamed proxy bodies need duplex: 'half'" | Only relevant because there's a same-origin proxy in this recipe |

Pattern: emerges from feature-phase work. Engine doesn't know these a priori — they surface when feature sub-agent observes the trap.

**Engine-emit decision: NO.** Class D facts are ALWAYS agent-recorded during feature phase as `porter_change` facts. The classification + candidate routing is the agent's call (some Class D items are KB-class — see R-15-6 — others are IG-class). The content sub-agent decides routing using spec §Cross-surface duplication.

### §3.5 Summary table

| Class | Source | Engine knows? | Author | Per-codebase count typical |
|-------|--------|---------------|--------|----------------------------|
| A — Engine-emittable | Committed yaml | ✓ Fully | Engine (no agent) | 1 (always IG #1) |
| B — Universal-for-role | `cb.Role` × `cb.BaseRuntime` | ✓ Why prose; ✗ framework-specific diff | Engine pre-fills fact; agent fills diff slot | 1-3 |
| C — Universal-for-recipe | `plan.Services` + per-service knowledge atom | ✓ Why prose; ✗ framework-specific syntax | Engine pre-fills fact; agent fills diff slot | 1 per managed service consumed |
| D — Framework × scenario | Agent observation during feature phase | ✗ Unknown a priori | Agent records porter_change fact; content sub-agent classifies + routes (IG or KB) | 0-2 |

Per-codebase IG total = 1 (A) + 1-3 (B) + 1-N (C, where N = managed services consumed) + 0-2 (D). Spec cap is 5. For typical showcase: A=1, B=2 (bind+SIGTERM), C=2 (DB + cache), D=0 → 5. For multi-managed-service codebase like nestjs-showcase apidev (5 managed services): A=1, B=2, C=2-5, D=1 → too many; spec forces routing decisions (group multi-service C-class items into "Read managed-service credentials" + per-service items in zerops.yaml comments only).

---

## §4. The proposed phase shape

7 phases (5 today + 2 new):

```
1 Research          | main agent           | plan, contracts            | (no fragments)
2 Provision         | main agent           | workspace yaml + import    | (no fragments)
3 Codebase deploy   | sub × N (parallel)   | code + zerops.yaml fields  | (no fragments)
                    |                      | + porter_change facts      |
                    |                      | + field_rationale facts    |
4 Feature deploy    | sub × 1              | feature code + yaml field  | (no fragments)
                    |                      | extensions                 |
                    |                      | + porter_change facts      |
                    |                      | + field_rationale facts    |
5 Codebase content  | sub × N (parallel)   | (no code changes)          | codebase/<h>/intro
                    |                      |                            | codebase/<h>/integration-guide
                    |                      |                            | codebase/<h>/knowledge-base
                    |                      |                            | codebase/<h>/claude-md/*
                    |                      |                            | codebase/<h>/zerops-yaml-comments
6 Env content       | sub × 1              | (no code changes)          | root/intro
                    |                      |                            | env/<N>/intro × 6
                    |                      |                            | env/<N>/import-comments/* × 54
7 Finalize          | main                 | stitch                     | (validator iterations only)
```

### §4.1 What changes vs today

- **Phases 3 + 4 stop authoring fragments.** Today scaffold-app records 5 codebase fragments + ssh-edits zerops.yaml comments inline; tomorrow it records 0 fragments (records facts instead). Same for feature.
- **Phases 5 + 6 are new.** They author all documentation. Today's finalize phase becomes phase 7 (stitch + validate only).
- **Two new fact classes** (`porter_change`, `field_rationale`) join the existing `FactRecord` shape (or extend it; see §6.6).
- **Engine pre-emits universal-for-role and universal-for-recipe facts** at scaffold dispatch. Agent fills only framework-specific slots.

### §4.2 Why this addresses the user's concern about zerops.yaml authoring

The user's example: "why is zerops.yaml authored by scaffold and not deferred to after feature?"

Corrected understanding: yaml FIELDS must be authored at scaffold (deploy needs them) and extended at feature (new features add fields). What CAN defer is the COMMENTS. Today scaffold writes comments inline at field-authoring time without knowing what feature will add. Tomorrow:

- Scaffold writes yaml WITHOUT comments. Records `field_rationale` facts as it goes.
- Feature extends yaml WITHOUT comments. Records additional `field_rationale` facts.
- Codebase content sub-agent at phase 5 reads the committed (uncommented) yaml + all `field_rationale` facts + spec §Surface 7 verbatim. Records `codebase/<h>/zerops-yaml-comments/<block-name>` fragments. Engine inserts comments at finalize stitch.

Net: the comments are authored once, with full context, by an agent that has the spec verbatim and sees the entire yaml.

---

## §5. The fact schema extension

Three new fact subtypes (or fact classes — see implementation choice in §6.6).

### §5.1 `porter_change` fact

Captures: a code change a porter would have to make to their own application to run on Zerops. Recorded at the moment the deploy-phase agent makes the change.

```jsonc
{
  "topic": "apidev-cors-expose-x-cache",
  "kind": "porter_change",
  "scope": "apidev/code/src/main.ts",
  "phase": "feature",
  "changeKind": "code-addition",                          // code-addition | library-install | config-change
  "library": "@nestjs/common",                             // when applicable
  "diff": "app.enableCors({ origin, credentials: true, exposedHeaders: ['X-Cache'] });",
  "why": "Browsers strip every non-CORS-safelisted response header from cross-origin JS reads unless the server explicitly lists them in Access-Control-Expose-Headers. Without exposedHeaders, fetch(...).headers.get('x-cache') returns null even when the server sets X-Cache: HIT.",
  "candidateClass": "intersection",                       // per spec §Classification taxonomy
  "candidateHeading": "Custom response headers across origins",
  "candidateSurface": "CODEBASE_KB",                      // OR CODEBASE_IG; agent's hint, content phase decides
  "citationGuide": "http-support",                        // optional; matches citation map
  "engineEmitted": false                                  // true when engine pre-emitted, agent only filled diff slot
}
```

**Recording rule**: agent records only when classification ∈ {`platform-invariant`, `intersection`, `scaffold-decision`} AND `candidateSurface` is in spec's [classification × surface compatibility](../../spec-content-surfaces.md#classification--surface-compatibility) for that class. Filter is mechanical — engine refuses with a redirect if `candidateClass` × `candidateSurface` is incompatible.

**Engine pre-emit**: at scaffold dispatch, engine emits `porter_change` facts with `engineEmitted: true` for every applicable Class B and Class C item per §3. Agent's only job is to fill the `diff` slot via `zerops_recipe action=fill-fact-slot factTopic=<topic> diff=<diff>`.

### §5.2 `field_rationale` fact

Captures: a non-obvious yaml field's reasoning. Recorded when the deploy-phase agent writes a field whose value isn't self-explanatory.

```jsonc
{
  "topic": "apidev-s3-region-us-east-1",
  "kind": "field_rationale",
  "scope": "apidev/zerops.yaml/run.envVariables.S3_REGION",
  "phase": "scaffold",
  "fieldPath": "run.envVariables.S3_REGION",
  "fieldValue": "us-east-1",
  "why": "us-east-1 is the only region MinIO accepts. The value is ignored downstream but every S3 SDK requires it set.",
  "alternatives": "Setting any other region throws SignatureDoesNotMatch on first bucket call.",
  "candidateClass": "scaffold-decision",
  "candidateSurface": "CODEBASE_ZEROPS_COMMENTS",
  "citationGuide": "object-storage"                        // optional
}
```

**Recording rule**: scaffold/feature record only for fields where the value-or-presence isn't self-explanatory. `port: 3000` doesn't need rationale; `S3_REGION: us-east-1` does. Filter is judgement-based but framed positively in the brief: *"if a porter would ask 'why this value?', record the rationale."*

**Engine pre-emit**: at scaffold dispatch, engine emits `field_rationale` facts for every universal field the engine knows the why for (`zsc execOnce ${appVersionId}-<step>` rationale; `deployFiles: ./` at dev rationale; etc.). Agent fills nothing — these are pure engine knowledge.

### §5.3 `contract` fact (cross-codebase)

Captures: a contract between codebases (NATS subject schema, route paths, payload shapes). Recorded by main agent at research phase OR by deploy-phase agent when the contract surfaces during code authoring.

```jsonc
{
  "topic": "nats-items-created-contract",
  "kind": "contract",
  "scope": "cross-codebase/contract",
  "phase": "research",
  "publishers": ["api"],
  "subscribers": ["worker"],
  "subject": "items.created",
  "queueGroups": ["worker-indexer"],
  "payloadSchema": "{ id: uuid, name: string, createdAt: ISO8601 }",
  "purpose": "Worker mirrors items into Meilisearch on create"
}
```

**Recording rule**: any cross-codebase coupling. Read by all codebase content sub-agents in phase 5 — both publisher and subscriber sides see the contract; KB / IG references the same contract from both sides without divergence.

---

## §6. Engine-side changes by file

### §6.1 [internal/recipe/workflow.go](../../../internal/recipe/workflow.go)

**Current state** ([workflow.go:14-22](../../../internal/recipe/workflow.go#L14-L22)):

```go
const (
    PhaseResearch  Phase = "research"
    PhaseProvision Phase = "provision"
    PhaseScaffold  Phase = "scaffold"
    PhaseFeature   Phase = "feature"
    PhaseFinalize  Phase = "finalize"
)
```

**Change**: extend Phase enum with two new values.

```go
const (
    PhaseResearch         Phase = "research"
    PhaseProvision        Phase = "provision"
    PhaseScaffold         Phase = "scaffold"        // renamed conceptually to "codebase deploy"; keep string for back-compat
    PhaseFeature          Phase = "feature"         // renamed conceptually to "feature deploy"; keep string for back-compat
    PhaseCodebaseContent  Phase = "codebase-content" // NEW
    PhaseEnvContent       Phase = "env-content"     // NEW
    PhaseFinalize         Phase = "finalize"
)
```

**Adjacent-forward order** ([workflow.go:75-94::EnterPhase](../../../internal/recipe/workflow.go#L75-L94)) updates: research → provision → scaffold → feature → codebase-content → env-content → finalize.

**Tests**:
- New: `TestPhase_AdjacentForward_CodebaseContentAfterFeature`
- New: `TestPhase_AdjacentForward_EnvContentAfterCodebaseContent`
- New: `TestPhase_AdjacentForward_FinalizeAfterEnvContent`
- Update existing: any test that asserts the 5-phase ordering.

**Risk**: phase-switch sites at [phase_entry.go:32-39::gatesForPhase](../../../internal/recipe/phase_entry.go#L32-L39) and [phase_entry.go:12-19::loadPhaseEntry](../../../internal/recipe/phase_entry.go#L12-L19) must add cases for the two new phases.

### §6.2 [internal/recipe/briefs.go](../../../internal/recipe/briefs.go)

**Current state**: 3 brief composers — [BuildScaffoldBriefWithResolver:144-262](../../../internal/recipe/briefs.go#L144), [BuildFeatureBrief:267-326](../../../internal/recipe/briefs.go#L267), [BuildFinalizeBrief:339-436](../../../internal/recipe/briefs.go#L339).

**Changes**:

1. **Strip content-authoring atoms from scaffold brief.** [`content/briefs/scaffold/content_authoring.md`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) (15.3 KB) drops out of scaffold brief; replaced by ~2 KB `decision_recording.md` atom. Net brief size: -13 KB.

2. **Strip content-extension atoms from feature brief.** [`content/briefs/feature/content_extension.md`](../../../internal/recipe/content/briefs/feature/content_extension.md) (7.4 KB) drops out; replaced by ~2 KB `decision_recording.md` (same atom, scoped to feature). Net brief size: -5 KB.

3. **Add `BuildCodebaseContentBrief(plan, codebase, parent)`** — new composer. Brief content:
   - Spec verbatim — entire [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) (~30 KB)
   - content-research.md verbatim — entire [`docs/zcprecipator3/content-research.md`](../content-research.md) (~15 KB)
   - This codebase's `porter_change` fact log (filtered from `FactsLog`)
   - This codebase's `field_rationale` fact log
   - This codebase's `platform_trap` fact log (existing surfaceHint)
   - The committed code's file index + zerops.yaml verbatim
   - Codebase metadata (hostname, role, base runtime, managed services consumed)
   - Cross-codebase `contract` facts where this codebase is publisher OR subscriber
   - Engine-derived "fragment slots to fill" list
   
   Net brief size: ~80 KB per dispatch × N codebases.

4. **Add `BuildEnvContentBrief(plan)`** — new composer. Brief content:
   - Spec verbatim
   - content-research.md verbatim
   - Per-tier capability matrix (already computed today by [BuildFinalizeBrief:383-385](../../../internal/recipe/briefs.go#L383))
   - Cross-tier deltas (computed by [tiers.go:95-129::Diff](../../../internal/recipe/tiers.go#L95-L129) — already exists)
   - All codebases + roles + services
   - "Fragment slots to fill" list (61 fragments per current finalize)
   - Friendly-authority voice section verbatim
   
   Net brief size: ~90 KB.

5. **Reduce `BuildFinalizeBrief`** to stitch + validate only. Drop content-authoring instructions; finalize sub-agent (or main agent) only iterates validator notices. ~3 KB brief.

**Tests**:
- New: `TestBuildCodebaseContentBrief_CarriesSpecVerbatim` (asserts `## The seven content surfaces` heading appears in brief body)
- New: `TestBuildCodebaseContentBrief_CarriesContentResearchVerbatim`
- New: `TestBuildCodebaseContentBrief_CarriesPorterChangeFacts` (with fixture facts)
- New: `TestBuildEnvContentBrief_CarriesPerTierCapabilityMatrix`
- New: `TestBuildEnvContentBrief_CarriesFriendlyAuthorityVoiceSection`
- Update: any test that asserts scaffold brief size or content-authoring atom presence.

### §6.3 [internal/recipe/briefs_subagent_prompt.go](../../../internal/recipe/briefs_subagent_prompt.go)

**Current state**: [buildSubagentPromptForPhase:49-97](../../../internal/recipe/briefs_subagent_prompt.go#L49-L97) routes scaffold/feature/finalize via [buildBriefForKind:109-124](../../../internal/recipe/briefs_subagent_prompt.go#L109-L124).

**Change**: extend `BriefKind` enum + `buildBriefForKind` switch + `buildSubagentPromptForPhase` per-kind context block to handle:
- `BriefKind = "codebase-content"` → `BuildCodebaseContentBrief(plan, codebase, parent)`
- `BriefKind = "env-content"` → `BuildEnvContentBrief(plan)`

**Tests**: extend [briefs_dispatch_test.go](../../../internal/recipe/briefs_dispatch_test.go) (the §B run-15 e2e test file) to cover the two new dispatch kinds.

### §6.4 [internal/recipe/handlers.go](../../../internal/recipe/handlers.go)

**Current state**: action enum at [handlers.go:204-228::dispatch](../../../internal/recipe/handlers.go#L204-L228), 13 actions today.

**Changes**:

1. **Add action `fill-fact-slot`** — for engine-emitted facts where agent only contributes the slot value. Input: `factTopic` + slot value (`diff` for porter_change, etc.). Engine merges into the existing fact record.

2. **Extend `record-fragment` with slot-shape refusal** at [handlers.go:349-385::handleRecordFragment](../../../internal/recipe/handlers.go#L349-L385). Per-fragment-id constraints:
   - `env/<N>/intro` — refuse if body > 350 chars OR contains `<!-- #ZEROPS_EXTRACT_*` tokens OR contains `## ` heading.
   - `codebase/<h>/claude-md/notes` — refuse if body contains `## ` heading (template owns the H2).
   - `codebase/<h>/integration-guide/<n>` (NEW slotted form) — refuse if body has more than 1 `### ` heading.
   - `codebase/<h>/zerops-yaml-comments/<block-name>` (NEW) — refuse if body > N lines (per spec §Surface 7 cap).
   
   Refusal returns a `RecipeResult` with `OK: false` + `Notice` field carrying the slot constraint and a re-author hint.

3. **Add action `register-contract`** — for `contract` facts. Same shape as `record-fact` but with the cross-codebase fact subtype.

**Tests**:
- New: `TestRecordFragment_RefusesNestedExtractMarkers` (covers R-15-3)
- New: `TestRecordFragment_RefusesH2InNotesSlot` (covers R-15-4)
- New: `TestRecordFragment_RefusesMultiHeadingInIGSlot` (covers R-15-5)
- New: `TestFillFactSlot_MergesDiffIntoEngineEmittedFact`

### §6.5 [internal/recipe/handlers_fragments.go](../../../internal/recipe/handlers_fragments.go)

**Current state**: [isValidFragmentID:153-199](../../../internal/recipe/handlers_fragments.go#L153-L199) accepts 9 fragment ID shapes.

**Changes**: extend with 2 new shapes:

- `codebase/<h>/integration-guide/<n>` (n = 1..5) — slotted IG item. Today's `codebase/<h>/integration-guide` is a single string fragment; tomorrow it's 5 separate slot fragments. Engine concatenates at stitch time.
- `codebase/<h>/zerops-yaml-comments/<block-name>` — per-block yaml comment. Block names enumerated from the committed yaml's structure (build, run, run.envVariables, deploy, etc.).

**Migration**: keep the old `codebase/<h>/integration-guide` shape accepted for back-compat during rollout; add new slotted shapes; switch IG content sub-agent to slotted form.

**Tests**:
- New: `TestIsValidFragmentID_SlottedIGItems`
- New: `TestIsValidFragmentID_ZeropsYamlCommentsByBlock`

### §6.6 [internal/recipe/facts.go](../../../internal/recipe/facts.go)

**Current state**: single `FactRecord` struct at [facts.go:24-37](../../../internal/recipe/facts.go#L24-L37) with 5 required + 7 optional fields. Stored as JSONL at `<outputRoot>/facts.jsonl`. [Validate:40-54](../../../internal/recipe/facts.go#L40-L54) checks required fields.

**Implementation choice**:

- **Option A**: extend `FactRecord` with optional fields for the new fact classes (`Kind`, `ChangeKind`, `Library`, `Diff`, `FieldPath`, `FieldValue`, `Publishers`, etc.). One struct, polymorphic via `Kind` field. Lower implementation cost; weaker type safety.
- **Option B**: keep `FactRecord` for the existing platform-trap class; add `PorterChangeRecord`, `FieldRationaleRecord`, `ContractRecord` as separate types. Stronger type safety; more migration work; new JSONL files per type.

**Recommendation**: Option A. The fact log is already JSONL and queryable via [FilterByHint:134-145](../../../internal/recipe/facts.go#L134-L145). Adding `Kind` field + per-Kind required-field validation in `Validate()` preserves the existing pipeline. Type safety lives in the brief composer's filter logic ([§6.2 above](#62-internalrecipebriefsgo)).

**Concrete schema extension** (Option A):

```go
type FactRecord struct {
    // ─── Existing fields (preserved) ───────────────────────
    Topic       string            `json:"topic"`
    Symptom     string            `json:"symptom,omitempty"`     // → optional with Kind extension
    Mechanism   string            `json:"mechanism,omitempty"`   // → optional with Kind extension
    SurfaceHint string            `json:"surfaceHint,omitempty"` // → optional with Kind extension
    Citation    string            `json:"citation,omitempty"`    // → optional with Kind extension
    FailureMode string            `json:"failureMode,omitempty"`
    FixApplied  string            `json:"fixApplied,omitempty"`
    Evidence    string            `json:"evidence,omitempty"`
    Scope       string            `json:"scope,omitempty"`
    RecordedAt  string            `json:"recordedAt,omitempty"`
    Author      string            `json:"author,omitempty"`
    Extra       map[string]string `json:"extra,omitempty"`
    
    // ─── NEW: discriminator + per-Kind fields ──────────────
    Kind string `json:"kind,omitempty"` // "" = platform-trap (back-compat); "porter_change" | "field_rationale" | "contract"
    
    // PorterChange fields (Kind=porter_change):
    Phase            string `json:"phase,omitempty"`
    ChangeKind       string `json:"changeKind,omitempty"` // code-addition | library-install | config-change
    Library          string `json:"library,omitempty"`
    Diff             string `json:"diff,omitempty"`
    Why              string `json:"why,omitempty"`
    CandidateClass   string `json:"candidateClass,omitempty"`
    CandidateHeading string `json:"candidateHeading,omitempty"`
    CandidateSurface string `json:"candidateSurface,omitempty"`
    CitationGuide    string `json:"citationGuide,omitempty"`
    EngineEmitted    bool   `json:"engineEmitted,omitempty"`
    
    // FieldRationale fields (Kind=field_rationale):
    FieldPath    string `json:"fieldPath,omitempty"`
    FieldValue   string `json:"fieldValue,omitempty"`
    Alternatives string `json:"alternatives,omitempty"`
    
    // Contract fields (Kind=contract):
    Publishers    []string `json:"publishers,omitempty"`
    Subscribers   []string `json:"subscribers,omitempty"`
    Subject       string   `json:"subject,omitempty"`
    QueueGroups   []string `json:"queueGroups,omitempty"`
    PayloadSchema string   `json:"payloadSchema,omitempty"`
    Purpose       string   `json:"purpose,omitempty"`
}
```

**Validate() extension**: dispatch on `Kind`. Existing call paths with `Kind == ""` validate as platform-trap (back-compat). New `Kind` values validate against per-Kind required fields.

**Tests**:
- New: `TestFactRecord_Validate_PorterChange_RequiresFields`
- New: `TestFactRecord_Validate_FieldRationale_RequiresFields`
- New: `TestFactRecord_Validate_Contract_RequiresFields`
- Extend: `TestFactRecord_Validate_PlatformTrap_BackCompat`

**FilterByKind helper**: add alongside existing `FilterByHint` for the brief composer.

### §6.7 [internal/recipe/assemble.go](../../../internal/recipe/assemble.go)

**Current state**: `injectIGItem1` at [assemble.go:170-195](../../../internal/recipe/assemble.go#L170-L195) generates IG #1 from the committed yaml; `substituteFragmentMarkers` at [:388-438](../../../internal/recipe/assemble.go#L388-L438) splices fragments into templates.

**Changes**:

1. **New `injectIGItems` (replaces single-fragment IG)**: generates IG #1 (yaml verbatim, unchanged) + concatenates IG items #2..N from slotted fragments `codebase/<h>/integration-guide/<n>`. If no slotted fragments present, falls back to legacy single-string fragment for back-compat.

2. **New `injectZeropsYamlComments`**: at finalize stitch, reads `codebase/<h>/zerops-yaml-comments/<block-name>` fragments. Walks the committed yaml, inserts each comment fragment as inline yaml comment lines above the matching block (build, run, run.envVariables, etc.). Yaml structure preserved; comments added.
   
   Implementation: needs a yaml-aware insertion (block-name → byte position). Likely 50-80 LoC + a per-block-name marker scan.
   
   Block names enumerated by the engine at content-phase dispatch (so the agent knows which slots to fill). Standard names: `build`, `run`, `run.envVariables`, `run.initCommands`, `deploy.readinessCheck`, `run.healthCheck`, `run.verticalAutoscaling`.

3. **New `injectIGItem1Intro`**: today's [yamlIntroSentence:223-274](../../../internal/recipe/assemble.go#L223-L274) generates the IG #1 intro from yaml introspection. Extend to ALSO read `field_rationale` facts for the codebase and weave them into the intro where a non-obvious field's why prose is captured. (Optional polish; keep yamlIntroSentence as the fallback.)

**Tests**:
- New: `TestInjectIGItems_ConcatenatesSlottedFragments`
- New: `TestInjectIGItems_FallsBackToLegacySingleFragment`
- New: `TestInjectZeropsYamlComments_PreservesYamlStructure`
- New: `TestInjectZeropsYamlComments_PerBlockInsertion`

### §6.8 [internal/recipe/validators_*.go](../../../internal/recipe/) — validator inventory changes

Today's validators (registered at [validators.go:199-207](../../../internal/recipe/validators.go#L199-L207)):

| Validator | Surface | Action |
|-----------|---------|--------|
| `validateRootREADME` | SurfaceRootREADME | **Keep**, narrow scope (slot refusal at record-fragment handles 60% of checks; remaining = factuality, voice). |
| `validateEnvREADME` | SurfaceEnvREADME | **Reduce**: char-cap moves to record-fragment slot constraint (closes R-15-3 by construction). Remaining: factuality. |
| `validateEnvImportComments` | SurfaceEnvImportComments | **Keep**, narrow scope. Comment slot caps move to record-fragment refusal. |
| `validateCodebaseIG` | SurfaceCodebaseIG | **Reshape**: item-cap = 5 enforced by slot existence (`integration-guide/1..5` only); validator narrows to per-item content checks. Closes R-15-5. |
| `validateCodebaseKB` | SurfaceCodebaseKB | **Keep**, narrow scope. Bullet-cap holds (worked in run-15). Add cross-surface duplication check vs IG (closes R-15-6). |
| `validateCodebaseCLAUDE` | SurfaceCodebaseCLAUDE | **Reshape**: H2 structure enforced by slot constraints (notes can't contain H2; service-facts can't contain H2). Closes R-15-4. |
| `validateCodebaseYAML` | SurfaceCodebaseZeropsComments | **Keep**, narrow scope. Per-block comment insertion happens at engine-stitch; validator only checks for any comment slots that should have been filled but weren't. |

**New validators**:
- `validateCrossSurfaceDuplication` (registered to SurfaceCodebaseKB and SurfaceCodebaseIG) — Jaccard similarity check between IG items and KB bullets, > 70% similarity → blocking violation. Closes R-15-6.

### §6.9 [internal/recipe/content/](../../../internal/recipe/content/) — atom changes

**Delete**:
- `briefs/scaffold/content_authoring.md` (15.3 KB) — content-authoring instructions move to phase 5 brief which carries spec verbatim
- `briefs/feature/content_extension.md` (7.4 KB) — same reason, phase 5 owns codebase content
- `briefs/finalize/intro.md` and `briefs/finalize/validator_tripwires.md` and `briefs/finalize/anti_patterns.md` — finalize is stitch-only; these atoms move to phase 6 (env content) brief

**Add**:
- `briefs/scaffold/decision_recording.md` (~2 KB) — teaches scaffold sub-agent to record `porter_change` and `field_rationale` facts at densest context. Filter rule (only spec-classifiable changes). Examples of when to record vs skip.
- `briefs/feature/decision_recording.md` (~2 KB) — same shape, scoped to feature-added fields.
- `briefs/codebase-content/intro.md` (~2 KB) — phase 5 brief preface. Voice rules. Single-author / cross-surface-aware authoring.
- `briefs/codebase-content/synthesis_workflow.md` (~3 KB) — how to read facts, how to group into IG items, how to dedup against KB, how to author zerops.yaml comments per block.
- `briefs/env-content/intro.md` (~2 KB) — phase 6 brief preface. Tier delta narrative. Friendly-authority voice.
- `briefs/env-content/per_tier_authoring.md` (~3 KB) — how to author each env fragment. Slot constraints.

**Phase entry atoms** ([content/phase_entry/](../../../internal/recipe/content/phase_entry/)):
- New: `codebase-content.md` — phase 5 entry. Procedural recipe.
- New: `env-content.md` — phase 6 entry.
- Update: `scaffold.md` — remove content-authoring instructions; add decision-recording instructions.
- Update: `feature.md` — same shape.
- Update: `finalize.md` — strip authoring; finalize is stitch + validate only.

---

## §7. Engine-emit hooks for universal-for-role facts

When `BuildCodebaseDeployBrief` (renamed scaffold brief) dispatches per codebase, the engine emits `porter_change` facts ahead of the agent's work. Per [§3.2 / §3.3](#32-class-b--universal-for-role).

### §7.1 Per-role rule table

```go
// internal/recipe/engine_emitted_facts.go (NEW FILE)

func emittedFactsForCodebase(plan *Plan, cb Codebase) []FactRecord {
    var facts []FactRecord
    
    // Class B: universal-for-role
    if cb.Role == RoleAPI || cb.Role == RoleFrontend || cb.Role == RoleMonolith {
        // HTTP roles
        facts = append(facts, FactRecord{
            Topic: cb.Hostname + "-bind-and-trust-proxy",
            Kind:  "porter_change",
            Scope: cb.Hostname + "/code",
            ChangeKind: "code-addition",
            Why: "Default bind to 127.0.0.1 is unreachable from the L7 balancer (which routes to the container's VXLAN IP). Trust the X-Forwarded-* headers so request.ip / request.protocol reflect the real caller.",
            CandidateClass:   "platform-invariant",
            CandidateHeading: "Bind 0.0.0.0 and trust the L7 proxy",
            CandidateSurface: "CODEBASE_IG",
            EngineEmitted:    true,
        })
        
        if strings.HasPrefix(cb.BaseRuntime, "nodejs") {
            facts = append(facts, FactRecord{
                Topic: cb.Hostname + "-sigterm-drain",
                Kind:  "porter_change",
                Scope: cb.Hostname + "/code",
                ChangeKind: "code-addition",
                Why: "Rolling deploys send SIGTERM to the old container while the new one warms up. Without explicit shutdown handling, in-flight requests fail mid-response.",
                CandidateClass:   "platform-invariant",
                CandidateHeading: "Drain in-flight requests on SIGTERM",
                CandidateSurface: "CODEBASE_IG",
                EngineEmitted:    true,
            })
        }
    }
    
    if cb.Role == RoleWorker {
        if strings.HasPrefix(cb.BaseRuntime, "nodejs") {
            facts = append(facts, FactRecord{
                Topic: cb.Hostname + "-application-context",
                Kind:  "porter_change",
                Scope: cb.Hostname + "/code",
                Why: "A worker has no HTTP surface. NestFactory.create starts an Express listener that has nothing to listen for and fights the platform's empty run.ports. Use createApplicationContext for a no-HTTP Nest app.",
                CandidateClass:   "platform-invariant",
                CandidateHeading: "Boot a Nest application context, not an HTTP server",
                CandidateSurface: "CODEBASE_IG",
                EngineEmitted:    true,
            })
        }
    }
    
    // Class C: universal-for-recipe (per managed service)
    services := managedServicesConsumedBy(plan, cb)
    if len(services) > 0 {
        // Always emit the env-var aliasing item if any managed service
        facts = append(facts, FactRecord{
            Topic: cb.Hostname + "-own-key-aliases",
            Kind:  "porter_change",
            Why: "Cross-service references like ${db_hostname} auto-inject project-wide under platform-side keys. Reading those names directly couples the application to Zerops naming. Declare own-key aliases in zerops.yaml run.envVariables and read those.",
            CandidateClass:   "platform-invariant",
            CandidateHeading: "Read managed-service credentials from own-key aliases",
            CandidateSurface: "CODEBASE_IG",
            CitationGuide:    "env-var-model",
            EngineEmitted:    true,
        })
    }
    
    for _, svc := range services {
        // Per-service connection idiom — engine looks up the service-type-specific atom
        if hint, ok := serviceConnectionHint(svc.Type, cb.BaseRuntime); ok {
            facts = append(facts, FactRecord{
                Topic: cb.Hostname + "-connect-" + svc.Hostname,
                Kind:  "porter_change",
                Why: hint.Why,
                CandidateClass:   "intersection",
                CandidateHeading: hint.Heading,
                CandidateSurface: "CODEBASE_IG",
                CitationGuide:    hint.GuideID,
                Library:          hint.LibrarySuggestion,  // e.g. "nats", "predis/predis"
                EngineEmitted:    true,
            })
        }
    }
    
    return facts
}
```

### §7.2 Per-managed-service connection hints

Lives in `internal/recipe/managed_service_hints.go` (NEW FILE). Lookup table indexed by service-type × base-runtime:

```go
type ConnectionHint struct {
    Why               string
    Heading           string
    GuideID           string
    LibrarySuggestion string
}

var managedServiceHints = map[string]map[string]ConnectionHint{
    "nats@2.12": {
        "nodejs@22": {
            Why:               "broker_connectionString and a manually composed nats://user:pass@host:port URL both look right but the second form fails: most NATS clients parse the embedded credentials AND issue a SASL CONNECT with the same values, and the broker rejects the second authentication as Authorization Violation. Pass the URL credential-free and the credentials as connect options.",
            Heading:           "Connect to NATS with credentials as connect options",
            GuideID:           "managed-services-nats",
            LibrarySuggestion: "nats",
        },
        "php-nginx@8.4": {
            // ... PHP-specific hint
        },
    },
    "valkey@7.2": { /* ... */ },
    "object-storage": { /* ... */ },
    "meilisearch@1.20": { /* ... */ },
    "postgresql@18": { /* ... */ },
}
```

Hints curated by reading `zerops_knowledge runtime=<type>` content for each managed-service family. Covered in tranche 2.

### §7.3 Test pinning engine-emitted prose to atom content

```go
// internal/recipe/engine_emitted_facts_test.go (NEW)

func TestEngineEmitted_BindAndTrustProxy_WhyMatchesAtom(t *testing.T) {
    t.Parallel()
    cb := Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}
    plan := &Plan{Codebases: []Codebase{cb}}
    facts := emittedFactsForCodebase(plan, cb)
    
    var bindFact *FactRecord
    for i := range facts {
        if facts[i].Topic == "api-bind-and-trust-proxy" {
            bindFact = &facts[i]
            break
        }
    }
    if bindFact == nil { t.Fatal("expected bind-and-trust-proxy fact") }
    
    // The Why prose should align with the platform_principles atom HTTP section.
    atom := mustReadAtom(t, "scaffold/platform_principles.md")
    if !atomHTTPSectionMentions(atom, "Bind 0.0.0.0") {
        t.Error("atom doesn't teach 'Bind 0.0.0.0'; engine-emit Why might drift")
    }
    if !strings.Contains(bindFact.Why, "L7 balancer") {
        t.Errorf("Why prose should mention L7 balancer; got: %s", bindFact.Why)
    }
}
```

This is the safety net against engine-emitted prose drifting away from atom-source content.

---

## §8. Slot-shape refusal at record-fragment

Move structural caps from finalize-validator (post-hoc) to record-fragment refusal (record-time). Per spec.

### §8.1 Refusal table

| Fragment ID | Constraint | Refusal message |
|-------------|------------|-----------------|
| `root/intro` | ≤ 500 chars; no markdown headings | "root/intro is a 1-sentence string, ≤ 500 chars, no markdown. See spec §Surface 1." |
| `env/<N>/intro` | ≤ 350 chars; no `## ` headings; no `<!-- #ZEROPS_EXTRACT_*` tokens | "env/<N>/intro is a 1-2 sentence string, ≤ 350 chars, no markdown headings, no nested extract markers. See spec §Surface 2." |
| `env/<N>/import-comments/project` | ≤ 8 lines | "tier import.yaml project comment ≤ 8 lines per spec §Surface 3." |
| `env/<N>/import-comments/<host>` | ≤ 8 lines | "tier import.yaml service-block comment ≤ 8 lines per spec §Surface 3." |
| `codebase/<h>/intro` | ≤ 500 chars; no `## ` headings | "codebase intro is a 1-2 sentence string, ≤ 500 chars per spec §Surface 4." |
| `codebase/<h>/integration-guide/<n>` | exactly 1 `### ` heading per slot; body ≤ 30 lines | "IG slot is one item: one `### ` heading + body ≤ 30 lines. See spec §Surface 4." |
| `codebase/<h>/knowledge-base` | each `- ` bullet must start with `**Topic** —`; total bullets ≤ 8 | "KB bullet shape: `- **Topic** — 2-4 sentences`. Cap 8 bullets per spec §Surface 5." |
| `codebase/<h>/claude-md/service-facts` | no `## ` headings (template owns H2); body ≤ 20 lines | "service-facts is a bullet list, no H2. Template owns the H2. See spec §Surface 6." |
| `codebase/<h>/claude-md/notes` | no `## ` headings; ≤ 8 bullets | "notes is a bullet list, no H2. ≤ 8 bullets per spec §Surface 6." |
| `codebase/<h>/zerops-yaml-comments/<block>` | ≤ 6 lines | "zerops.yaml block comment ≤ 6 lines per spec §Surface 7." |

### §8.2 Implementation site

[`handlers.go::handleRecordFragment`](../../../internal/recipe/handlers.go#L349-L385) — after `isValidFragmentID` passes, before `recordFragment` writes:

```go
if violation := checkSlotShape(in.FragmentID, in.Fragment); violation != "" {
    r.OK = false
    r.Notice = violation
    return r
}
```

`checkSlotShape` lives in new file `internal/recipe/slot_shape.go` with the per-fragment-ID refusal rules.

### §8.3 What this closes

R-15-3 (duplicate extract markers): `env/<N>/intro` slot refuses bodies containing `<!-- #ZEROPS_EXTRACT_*` tokens. Agent can't ship the run-14 ladder under outer markers because the slot constraint refuses the agent's body before it's stored.

R-15-4 (duplicate H2 in CLAUDE.md): `codebase/<h>/claude-md/notes` slot refuses bodies with `## ` headings. Engine owns the H2 structure; agent fills bullets only.

R-15-5 (unnumbered IG sub-section): `codebase/<h>/integration-guide/<n>` is per-slot. Slot 6 doesn't exist. Agent can author at most 5 numbered IG items + cannot ship a sub-section.

---

## §9. Test plan

### §9.1 Tranche 1 tests (RED-then-GREEN)

For each new function, write a RED test first (asserts the expected behaviour; fails because the function doesn't exist yet); then implement until GREEN.

**Order**:
1. Phase enum + AdjacentForward tests → workflow.go change.
2. FactRecord.Validate per-Kind tests → facts.go schema extension.
3. checkSlotShape per-fragment-ID tests → slot_shape.go new file.
4. handleRecordFragment refusal tests → handlers.go integration.
5. emittedFactsForCodebase per-role tests → engine_emitted_facts.go new file.
6. ConnectionHint table tests → managed_service_hints.go new file.
7. BuildCodebaseContentBrief content presence tests → briefs.go new composer.
8. BuildEnvContentBrief content presence tests → briefs.go new composer.
9. injectIGItems concatenation tests → assemble.go change.
10. injectZeropsYamlComments tests → assemble.go new function.

### §9.2 e2e dispatch tests (per run-15 §0 lesson)

Every brief / response / validator extension has an e2e test that observes production output:

- `TestCodebaseContentDispatch_BriefCarriesSpecVerbatim`
- `TestEnvContentDispatch_BriefCarriesSpecVerbatim`
- `TestRecordFragment_RefusalReachesAgent_AndAgentCanRecover`

### §9.3 Reference-data tests

The empirical floor stays anchored:

- `TestSurfaceContract_LineCaps_MatchSpecLineBudgetTable` — asserts the LineCap / ItemCap / IntroExtractCharCap values in [surfaces.go:128-200::surfaceContracts](../../../internal/recipe/surfaces.go#L128-L200) match the [spec §Per-surface line-budget table](../../spec-content-surfaces.md#per-surface-line-budget-table) parsed live.
- `TestEngineEmitted_BindAndTrustProxy_WhyMatchesAtom` and similar.

---

## §10. Tranche ordering

### Tranche 1 — Schema + slot refusal (closes R-15-3, R-15-4, R-15-5)

- Commits: `facts.go` Kind extension, `slot_shape.go` new file, `handlers.go` refusal integration.
- Test count: ~15 unit + 3 integration.
- LoC: ~250.
- **Risk**: existing FactRecord callers continue to work (Kind="" = back-compat). Existing fragment authoring continues to work (new fragment IDs are additive).

### Tranche 2 — Engine-emit + connection hints

- Commits: `engine_emitted_facts.go` new file, `managed_service_hints.go` new file with hint table for the 5 canonical managed services × 2 base runtimes (nodejs, php-nginx).
- Test count: ~20 unit (one per role × runtime × managed service combo).
- LoC: ~400.
- **Risk**: hint prose curation. Each hint must align with the corresponding `zerops_knowledge` guide content. Audit each hint by reading the guide first.

### Tranche 3 — New phase enum + new brief composers

- Commits: `workflow.go` Phase extension, `briefs.go` two new composers, `briefs_subagent_prompt.go` routing extension, `content/briefs/codebase-content/`, `content/briefs/env-content/` new atom files, `content/phase_entry/{codebase-content,env-content}.md` new atoms.
- Test count: ~25 unit + 5 e2e.
- LoC: ~600 (mostly atom content, not engine code).
- **Risk**: brief size (~80-90 KB per dispatch) — confirm context-window headroom.

### Tranche 4 — New fragment ID shapes + slotted IG + zerops.yaml comment fragments

- Commits: `handlers_fragments.go` extension, `assemble.go::injectIGItems` rewrite, `assemble.go::injectZeropsYamlComments` new.
- Test count: ~15 unit.
- LoC: ~300.
- **Risk**: yaml comment insertion is the trickiest engine work. Per-block-name → byte position resolution requires yaml-aware (not just regex) handling.

### Tranche 5 — Validator narrowing / deletion

- Commits: each `validators_*.go` file gets its scope reduced to what's left after slot-shape refusal handles structural caps. New `validateCrossSurfaceDuplication` (R-15-6 closure).
- Test count: existing tests stay; new test for cross-surface dup.
- LoC: -200 (net deletion).

### Tranche 6 — Atom file deletion (content-authoring atoms move to content phases)

- Delete `briefs/scaffold/content_authoring.md`, `briefs/feature/content_extension.md`, `briefs/finalize/{intro,validator_tripwires,anti_patterns}.md`.
- Update `phase_entry/{scaffold,feature,finalize}.md`.

### Tranche 7 — Sign-off

- CHANGELOG entry.
- system.md §4 verdict-table updates.
- spec-content-surfaces.md amendment if anything changed.

---

## §11. Risk register

### Risk 1 — Engine-emitted prose drifts from atom content

The engine's `Why` prose for universal-for-role facts is curated (in code) from atom content (in markdown). They can drift.

**Mitigation**: §9.3 reference-data tests assert engine-emitted Why prose contains key phrases from the atom. CI breaks if atom changes without engine-emit update.

### Risk 2 — Brief size headroom

Codebase-content brief: ~80 KB. Env-content brief: ~90 KB. Plus the existing scaffold (29-32 KB) and feature (22 KB). Total per-recipe dispatched bytes: ~270 KB.

**Mitigation**: well within Opus 4.7 1M-context window. If size ever bites, subset the spec verbatim per phase (codebase-content brief carries §Surfaces 4-7 only; env-content carries §Surfaces 1-3 only).

### Risk 3 — Yaml comment insertion is structurally fragile

`injectZeropsYamlComments` parses yaml block boundaries by line scanning. If the agent's yaml has unusual indentation or comments inside blocks the parser doesn't expect, comment insertion can land at wrong byte positions.

**Mitigation**: use a real yaml AST library (already in deps via `gopkg.in/yaml.v3` if used by yaml_emitter.go). Per-block insertion via AST round-trip preserves structure. Test against the run-15 dogfooded yamls + both reference recipes' yamls.

### Risk 4 — Per-codebase content sub-agent doesn't see deploy-iteration narrative

Today scaffold sub-agent has the densest context (just figured out NATS auth). Tomorrow's content sub-agent reads facts.jsonl + committed code, NOT live iteration.

**Mitigation**: facts capture mechanism + why at densest context. The deploy phase's job is to record well. If a deploy-phase agent forgets to record a learned platform trap as a `porter_change` fact, the content sub-agent doesn't know about it — same way today's run loses information across phase boundaries.

This shifts the failure mode from "scaffold writes thin IG / KB" to "scaffold doesn't record enough facts." Both are recoverable via the brief teaching; the structural shape favours the latter.

### Risk 5 — Migration: existing recipes (run-15 deliverable) shouldn't have to re-author

Run-15's nestjs-showcase deliverable is already published. The new fragment ID shapes shouldn't break re-stitch of existing Plans.

**Mitigation**: keep the legacy `codebase/<h>/integration-guide` (single fragment) shape accepted by `isValidFragmentID`. `injectIGItems` falls back to legacy if no slotted fragments present.

### Risk 6 — R-15-1 (subdomain) and R-15-2 (slug list) not addressed by this plan

Out of scope for the architecture reshape. Need separate fixes:

- R-15-1: extend [tools/deploy_subdomain.go::maybeAutoEnableSubdomain](../../../internal/tools/deploy_subdomain.go) to OR `detail.SubdomainAccess` with deploy-time `Ports[].HTTPSupport`.
- R-15-2: confirm `## Recipe-knowledge slugs you may consult` section's actual consumer; if no concrete consumer, delete the §B work entirely.

---

## §12. R-15-N defect closure mapping

| Defect | What this run-16 prep closes | How |
|--------|-------------------------------|-----|
| R-15-1 (subdomain) | NOT closed by this prep | Out of scope; needs deploy_subdomain.go fix |
| R-15-2 (slug list) | NOT closed by this prep | Out of scope; recommend deletion if no consumer |
| R-15-3 (duplicate extract markers) | ✓ Closed | `env/<N>/intro` slot refuses bodies with `<!-- #ZEROPS_EXTRACT_*` tokens (§8.1). Engine emits markers; agent fills string only. |
| R-15-4 (duplicate H2 in CLAUDE.md) | ✓ Closed | `codebase/<h>/claude-md/{service-facts,notes}` slots refuse bodies with `## ` headings (§8.1). Template owns H2. |
| R-15-5 (unnumbered IG sub-section) | ✓ Closed | `codebase/<h>/integration-guide/<n>` is slotted (n=1..5). Slot 6 doesn't exist. (§6.5, §8.1.) |
| R-15-6 (cross-surface duplication X-Cache + duplex) | ✓ Closed | Codebase-content sub-agent sees BOTH IG and KB candidate fact lists in one phase (§4). New `validateCrossSurfaceDuplication` validator backs it up. |
| R-15-7 (F.3 classification adoption 5.3%) | Partially addressed | Not made mandatory. But porter_change facts carry `candidateClass` mandatorily (§5.1), so classification reach extends to fact-time even without making fragment classification mandatory. |
| R-15-P-1 (slug list not in brief) | Same as R-15-2 |  |
| R-15-P-2 (F.2 not in feature brief) | ✓ Closed | Feature phase no longer authors content. Codebase-content phase carries spec verbatim (§6.2). |
| R-15-P-3 (§A's e2e observer not in readiness) | NOT closed by this prep | Operational discipline change; needs `make verify-dogfood-*` family of grep gates against latest dogfood artifact. |
| R-15-P-4 (F.3 adoption) | Same as R-15-7 |  |

**Defect closures**: 4 / 7 (R-15-3, -4, -5, -6) closed by structural changes. R-15-7 / R-15-P-2 partially closed. R-15-1, R-15-2, R-15-P-1, R-15-P-3 are orthogonal and need separate fixes outside this plan.

---

## §13. Triple-confirmation checklist for the fresh instance

For the fresh Opus instance reading this guide to verify its accuracy. Each item names the verification command + the expected match.

**§13.1 Architecture corrections (§2)**

- [ ] Read [docs/zcprecipator3/system.md §2](../system.md) — confirm workspace yaml ≠ deliverable yaml shape distinction. Look for "startWithoutCode: true" + "no project block" claims.
- [ ] Read run-15 [environments/0 — AI Agent/import.yaml](../runs/15/environments/0%20—%20AI%20Agent/import.yaml) — confirm `enableSubdomainAccess: true` per HTTP service block (this is deliverable yaml).
- [ ] Read [docs/spec-content-surfaces.md §Classification × surface compatibility](../../spec-content-surfaces.md#classification--surface-compatibility) — confirm the table contents match §5.1's recording rule.

**§13.2 IG taxonomy derivation (§3)**

- [ ] Read [/Users/fxck/www/laravel-jetstream-app/README.md](../../../../laravel-jetstream-app/README.md) — count IG items (expect 4).
- [ ] Read [/Users/fxck/www/laravel-showcase-app/README.md](../../../../laravel-showcase-app/README.md) — count IG items (expect 5), count KB bullets (expect 7).
- [ ] Read [docs/zcprecipator3/runs/15/apidev/README.md](../runs/15/apidev/README.md) — count `### N.` numbered IG items (expect 5), count unnumbered `### ` items inside IG markers (expect 1).
- [ ] Cross-check §3.5 summary table — confirm class assignments match the actual reference content.

**§13.3 Engine code refs (§6)**

- [ ] `grep -n "Phase " internal/recipe/workflow.go` — confirm Phase enum at line 14-22 with 5 const values today.
- [ ] `grep -n "func BuildScaffoldBriefWithResolver" internal/recipe/briefs.go` — confirm signature at ~line 144.
- [ ] `grep -n "func BuildFeatureBrief" internal/recipe/briefs.go` — confirm signature at ~line 267.
- [ ] `grep -n "func BuildFinalizeBrief" internal/recipe/briefs.go` — confirm signature at ~line 339.
- [ ] `grep -n "func handleRecordFragment\|case \"record-fragment\"" internal/recipe/handlers.go` — confirm dispatcher at ~line 349-385.
- [ ] `grep -n "func isValidFragmentID" internal/recipe/handlers_fragments.go` — confirm at ~line 153-199.
- [ ] `grep -n "type FactRecord" internal/recipe/facts.go` — confirm struct at ~line 24-37.
- [ ] `grep -n "type SurfaceContract" internal/recipe/surfaces.go` — confirm struct at ~line 101-126.
- [ ] `grep -n "var surfaceContracts" internal/recipe/surfaces.go` — confirm map literal at ~line 128-200.
- [ ] `grep -n "func injectIGItem1\|func codebaseIGItem1" internal/recipe/assemble.go` — confirm at ~lines 170, 204.
- [ ] `grep -n "RegisterValidator" internal/recipe/validators.go` — confirm 7 registrations at ~line 199-207.
- [ ] `ls internal/recipe/content/briefs/scaffold/` — confirm `content_authoring.md` exists (will be deleted in tranche 6).
- [ ] `ls internal/recipe/content/briefs/feature/` — confirm `content_extension.md` exists (will be deleted in tranche 6).

**§13.4 Schema details (§5, §6.6)**

- [ ] Read [internal/recipe/handlers.go:107-128::RecipeInput](../../../internal/recipe/handlers.go#L107-L128) — confirm `Classification string` field exists (run-15 F.3).
- [ ] Read [internal/recipe/handlers.go:130-186::RecipeResult](../../../internal/recipe/handlers.go#L130-L186) — confirm `SurfaceContract`, `FragmentID`, `BodyBytes`, `Appended`, `PriorBody`, `Notice` fields exist.
- [ ] Read [internal/recipe/classify.go:251-272::classificationCompatibleWithSurface](../../../internal/recipe/classify.go#L251-L272) — confirm refusal logic + redirect message.
- [ ] Read [internal/recipe/classify.go:289-307::compatibleSurfaces](../../../internal/recipe/classify.go#L289-L307) — confirm class × surface map matches spec.
- [ ] Read [internal/recipe/facts.go:24-54](../../../internal/recipe/facts.go#L24-L54) — confirm `FactRecord` struct + `Validate` shape.

**§13.5 Test conventions (§9)**

- [ ] Read [internal/recipe/validators_kb_quality_test.go](../../../internal/recipe/validators_kb_quality_test.go) — confirm `t.Parallel()` + body+SurfaceInputs test pattern + `containsCode(vs, "code-name")` helper.
- [ ] Read [internal/recipe/handlers_test.go:46-100::TestDispatch_StartStatusRecordFactEmitFinishYAML](../../../internal/recipe/handlers_test.go#L46-L100) — confirm sequential-dispatch handler test pattern.
- [ ] Read [internal/recipe/assemble_test.go:17-44::TestAssemble_TemplateRendersStructuralData](../../../internal/recipe/assemble_test.go#L17-L44) — confirm syntheticShowcasePlan + AssembleX + (out, missing, error) pattern.

**§13.6 Defect closure verification (§12)**

- [ ] Read [docs/zcprecipator3/runs/15/ANALYSIS.md §7](../runs/15/ANALYSIS.md) — confirm R-15-N defect numbering matches §12 table.
- [ ] Read [docs/zcprecipator3/runs/15/CONTENT_COMPARISON.md §2](../runs/15/CONTENT_COMPARISON.md) — confirm R-15-3 (duplicate extract markers) finding's evidence (all 6 tier READMEs).
- [ ] Read [docs/zcprecipator3/runs/15/CONTENT_COMPARISON.md §6](../runs/15/CONTENT_COMPARISON.md) — confirm R-15-4 (duplicate H2 in apidev/CLAUDE.md) finding's evidence.

**§13.7 Risk register (§11)**

- [ ] Confirm Risk 1 mitigation: §9.3 reference-data tests are listed as part of tranche 1.
- [ ] Confirm Risk 5 mitigation: §6.5 keeps legacy `codebase/<h>/integration-guide` ID accepted for back-compat.
- [ ] Confirm Risk 6 acknowledgement: R-15-1 + R-15-2 are out of scope (§12 names them as not addressed).

**§13.8 Triple-verification result format**

The fresh instance reports back with:

```
## Triple-verification report

§13.1 Architecture corrections: PASS / FAIL with notes
§13.2 IG taxonomy: PASS / FAIL with notes
§13.3 Engine code refs: PASS / FAIL with notes
§13.4 Schema details: PASS / FAIL with notes
§13.5 Test conventions: PASS / FAIL with notes
§13.6 Defect closure: PASS / FAIL with notes
§13.7 Risk register: PASS / FAIL with notes

## Anomalies found
<list of any discrepancies between this guide and the actual artifacts>

## Recommended corrections to the guide
<list of changes the guide needs before implementation starts>

## Confidence level for implementation
<HIGH / MEDIUM / LOW with reasoning>
```

If all checklist items PASS and no anomalies surface, the guide is implementation-ready. If anomalies surface, this guide gets corrected before any code is written.

---

## §14. What this guide deliberately does NOT cover

- Run-16 readiness plan with tranche dates / commit messages — that's the next document, derived from this prep.
- Engine code beyond `internal/recipe/` — slot-shape refusal lives in this package; tooling-side changes (`internal/tools/deploy_subdomain.go` for R-15-1) are separate.
- Runtime behaviour of the new fact pipeline — implementation determines memory + I/O profile; risks are flagged in §11 but not benchmarked here.
- Migration of existing dogfooded artifacts — run-15 deliverable stays as-is; the architectural change is for run-16+ runs.

---

## §15. Next steps after triple-confirmation

1. Fresh instance reports back per §13.8.
2. Address all anomalies / corrections.
3. Write `run-16-readiness.md` plan with tranche dates, exact commit titles, and the risk-mitigation checkpoint per tranche.
4. Implementation begins.
