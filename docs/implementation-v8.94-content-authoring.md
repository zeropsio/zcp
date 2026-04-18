# v8.94 — Content Authoring Pipeline + Workflow Efficiency (standalone implementation guide)

**Intended reader**: A fresh Opus 4.7 instance (or equivalent) tasked with implementing this change from scratch. This doc is self-contained — you don't need prior conversation context.

**Prerequisite reading (in order)**:
1. [docs/spec-content-surfaces.md](spec-content-surfaces.md) — THE source of truth for what content goes on which surface. You will embed large portions of this into the new sub-agent brief. Read it fully before writing any code.
2. [docs/recipe-version-log.md](recipe-version-log.md) §v28 entry — the run that triggered this plan. Skim for the symptom pattern.
3. [CLAUDE.md](../CLAUDE.md) — project conventions, especially the TDD rule ("RED before GREEN") and the "max 350 lines per .go file" constraint.

**Target ship window**: single release (v8.94). Do not split into v8.94.1/.2 unless implementation discovers a hard blocker.

---

## 1. Context — why this exists

Across v20–v28, recipe content quality has been the limiting dimension on the recipe workflow. v28 passed every token-level content check (gotcha names a mechanism + failure mode; IG items have code blocks; CLAUDE.md clears byte floor) yet the honest reader-facing audit found:

- **~33% of gotchas were genuine platform teaching** (the rest were self-inflicted, wrong-surface, or framework-quirks belonging in framework docs).
- **1 folk-doctrine defect shipped** — workerdev gotcha #1 claims *"The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed"* — this is fabricated. The correct rule (in the `env-var-model` guide the agent had access to) is "cross-service vars auto-inject project-wide; never declare `key: ${key}`." Same class as v23's "Recovering `zsc execOnce` burn" fabrication.
- **2 env-comment factual errors** — env 5 "NATS with JetStream-style durability" (recipe uses core NATS, not JetStream); env 1 "tsbuildinfo gitignored so first watch cycle emits dist" (`nest start --watch` uses ts-node, not `nest build`).
- **Pervasive cross-surface duplication** — same facts re-authored on 3–4 surfaces each, with drift between them.
- **Env READMEs are 7 lines of template boilerplate** — zero tier-transition teaching.

The root cause is NOT another missing check. The root cause is **mental-model poisoning**: the agent that spent 85 min debugging writes self-narrative (what confused me, what I tried, what worked) instead of reader-facing content (what will surprise a fresh developer who read the platform docs). Token-level checks are trivially satisfied by a journal entry phrased in the right vocabulary.

## 2. Goals (two, in priority order)

**Primary goal**: Move all reader-facing content authoring into a fresh-context sub-agent that reads structured facts + platform guides + surface contracts (not the run transcript), classifies each fact before routing, cites `zerops_knowledge` for any topic the platform already documents, and writes every surface — including substantive Env READMEs — in a single pass.

**Secondary goal**: Close five framework-agnostic workflow inefficiencies identified in the v28 end-to-end analysis — workspace-state crawl redundancy, MCP-serialized deploys, error-recovery iteration cost, scaffold runtime-incident cost, and main/scaffold concurrency gap. Expected wall-time reduction: 20–35 min off an 85-min baseline. Both goals are framework-agnostic by construction; neither encodes NestJS/Svelte/Vite specifics in the workflow layer.

### Why pair the two goals in one release

The content-authoring reform consumes ~10 min of wall time on its own (subagent dispatch + fresh-context onboarding). Pairing it with workflow efficiency fixes keeps v29's wall time comparable to or better than v28's. Shipping content-quality alone would trade better content for +10 min of wall; shipping both means better content AND shorter wall. The efficiency fixes are also structurally independent of the content pipeline, so their risk surface is disjoint — a bug in I-5 (parallel deploy) can't hurt the content-authoring subagent and vice versa.

## 3. Scope — what's in and what's not

### In scope for v8.94

**Content-authoring reform (primary goal)**:

1. **Mandatory `zerops_record_fact` usage during deploy** — guidance + prompt pressure so facts are logged at the moment of freshest knowledge, not in retrospect.
2. **New fresh-context content-authoring sub-agent** dispatched at `deploy.readmes` substep. Reads the facts log, platform guides, and surface contracts. Writes all six surface types. Current brief at recipe.md §"Per-codebase README with extract fragments" gets replaced.
3. **New eager guidance topic `content-surface-contracts`** — the authoring sub-agent fetches this on dispatch; it contains the surface-by-surface contracts + classification taxonomy + counter-examples + citation map. This topic IS the distilled spec-content-surfaces.md.
4. **Env READMEs grow from 7-line templates to 40–80-line tier-transition teaching**, authored by the same sub-agent during `finalize` (the env-comments writer role v22/v23 had, but now driven by the same content-authoring subagent).
5. **Scaffold pre-flight self-verification (upgraded from a prose traps list)** — subagent runs concrete assertions against its own output before returning; fixes and re-verifies if any fail. Converts "read the list before shipping" into "prove the list is satisfied before shipping." See §5.4.
6. **`env_self_shadow` check-surface fix** — v8.85's check exists at [internal/tools/workflow_checks_generate.go:290](../internal/tools/workflow_checks_generate.go#L290) but v28 evidence shows it didn't enumerate the worker hostname at `complete step=generate`. Audit the codebase-enumeration code path; ensure every hostname with a zerops.yaml is checked.

**Workflow efficiency (secondary goal)**:

7. **Workspace state manifest** — main agent maintains `/tmp/zcp-workspace-{sessionID}.json` (or under `.zcp/` state dir) tracking each codebase's scaffold state, source-file purposes, managed-service wiring, pre-flight results, contract bindings, feature-implementation status. Subagents read it instead of crawling 30+ files. See §5.8.
8. **`zerops_deploy_batch` MCP action** — kicks off parallel deploys server-side, returns when all complete. Closes the MCP-STDIO serialization penalty (I-5). See §5.9.
9. **Main/scaffold concurrency** — prompt change in the generate-step guidance: "immediately after dispatching scaffold subagents, begin writing zerops.yaml files for each codebase in parallel with the scaffold work." Trivial change, 2-3 min savings. See §5.10.

### Explicitly NOT in scope for v8.94

- **No new content-quality checks** added at generate or deploy complete. The existing check surface stays exactly as-is. The fix is at the authoring layer, not the verification layer. (If v8.94 evidence shows a specific check is missing, that's a v8.95+ decision, not a v8.94 slip.)
- **No platform-fact registry** (the longer-term idea of Zerops-side canonical platform facts that recipes cite) — this would require content-engineering on the Zerops docs side. v8.94 leverages the existing `zerops_knowledge` surface via citation instead.
- **No editorial-pass sub-agent** (a second reviewer that reads content back against the tests). Add only if v29 evidence shows the single-author pass isn't sufficient.
- **No rollback of v8.86's `zerops_record_fact` tool** — that stays. v28 showed the agent uses it voluntarily when given the chance; v8.94 makes that usage prompt-mandatory.
- **No error-to-remediation hint lookup in tool responses** — structurally the same anti-pattern as "add more content checks." A hint table duplicates `zerops_knowledge` guide content in the MCP response layer, drifts when platform error wording changes, and short-circuits the citation discipline the content-authoring reform is trying to establish (agent should consult the guide, not follow a cached hint). The 3–6 min iteration cost is acceptable at v29's target wall time and represents the agent doing the right work (reading docs, learning mechanisms). The correct home for "MinIO needs `forcePathStyle: true`" is platform docs or `zerops_knowledge`, not tool responses.
- **No async deploy API (`deploy_start` + `deploy_wait`)** — Tier-2 inefficiency (I-6). Requires agent-side mental-model shift alongside server-side tool changes. Revisit in v8.95 if v29 wall time doesn't clear the 75-min bar.
- **No scaffold-reference cache** — Tier-2 inefficiency (I-2). The 60-75s-per-scaffold `{framework} new scratch` cost is real but cache drift management is non-trivial. Defer.
- **No per-step context compaction** — Tier-3 inefficiency (I-14). Too invasive; framework-level change.
- **No `zerops_facts_list` query tool** — Tier-2 inefficiency (I-13). Low impact; add only if content-authoring subagent evidence shows dedup pain.
- **No structured-default tool responses** — Tier-3 inefficiency (I-12). Invasive MCP contract change.

---

## 4. Architecture — the before/after shape

### Before (v28 state)

```
deploy step:
  ... substeps ...
  SubStepReadmes:
    main agent reads zerops.yaml / source / logs (all from its context)
    main agent writes 6 README.md + 3 CLAUDE.md inline OR dispatches a README-writer subagent
    content checks fire at complete step=deploy
    if fail: iterate in main context (v22 pattern) or dispatch fix-subagent (v23 pattern — rolled back)

finalize step:
  main agent writes env comments via generate-finalize (structured input)
  env READMEs auto-generated from template (7 lines of boilerplate)
```

### After (v8.94 state)

```
During deploy substeps (all):
  agent calls zerops_record_fact for every incident, every non-obvious scaffold decision,
    every platform-behavior-verification. No content writing during deploy.

SubStepReadmes:
  main agent dispatches ONE content-authoring sub-agent. Fresh context. Its brief contains:
    - Surface contracts (embedded from spec-content-surfaces.md)
    - Classification taxonomy (embedded)
    - Citation map (topic → zerops_knowledge guide)
    - Counter-example catalog (v28 wrong-surface items)
    - Instruction: read facts log (path = ops.FactLogPath(sessionID)), read final recipe state,
      fetch matching guides via zerops_knowledge, classify each fact, route to one surface,
      write all six surface types.
  main agent waits for the sub-agent to return; validates output shape; completes substep.

finalize step:
  same content-authoring sub-agent is dispatched a SECOND time with the env-README-expansion
    brief section active. Reads env import.yaml set, writes per-env README teaching content.
    Env comments still authored via generate-finalize structured input (unchanged).
```

The run transcript stays with the main agent. The authoring sub-agent never sees it.

---

## 5. File-by-file implementation plan

### 5.1 `internal/content/workflows/recipe.md` — replace the README writer brief

**Current location**: Lines ~1869–2040 (the `### Per-codebase README with extract fragments (post-deploy readmes sub-step)` section).

**Action**: Replace this section with the new content-authoring sub-agent brief. The replacement brief:

1. Opens with the TOOL-USE POLICY block (unchanged — preserve existing v8.90 policy).
2. States the explicit role: "You are a content-authoring sub-agent. You have NO memory of the run you were dispatched from. Your inputs are: (a) the structured facts log at `$ZCP_FACTS_LOG` (read via `ReadFacts` or `cat`), (b) the final recipe state at the SSHFS-mounted paths, (c) platform guides via `zerops_knowledge`. You will write every reader-facing content surface."
3. Embeds or references `spec-content-surfaces.md` content:
   - The six surface table (audience / purpose / test)
   - The classification taxonomy table (7 classifications, routing destinations)
   - The citation map (topic → guide ID)
   - The counter-example catalog (wrong-surface items from v28)
4. Specifies the workflow:
   - Read facts log. Group facts by codebase + substep.
   - For each fact, classify using the taxonomy. Discard self-inflicted and framework-quirk. Route the rest.
   - For each routed fact whose topic matches an entry in the citation map, call `zerops_knowledge topic=<id>` and read the guide BEFORE writing. Use the guide's framing.
   - Write all six surfaces. Apply the per-surface test to each item before committing it. Remove anything that fails.
5. Specifies the deliverables:
   - 3× `README.md` (per codebase; with intro / integration-guide / knowledge-base fragments — extract markers preserved byte-exact)
   - 3× `CLAUDE.md` (per codebase; operational guide shape)
   - `env-readme-expansions` JSON artifact (see §5.3) — one entry per env, to be consumed by the main agent's `generate-finalize` call at the finalize step.
6. Specifies the self-review gate:
   - Before returning, walk every surface and every item on it.
   - For each item, write (in the return message, not the content) the answer to the surface's test question.
   - Any item whose test answer is "no" must be removed from the surface, not rewritten to pass.

**Concrete brief text**: See §6 below for the full text to embed.

**Note on topic registry**: The new brief is delivered via the existing substep-to-topic mapping. You can either (a) register a new topic `content-authoring-brief` and remap `SubStepReadmes → content-authoring-brief` in [internal/workflow/recipe_substeps.go](../internal/workflow/recipe_substeps.go) `subStepToTopic`, or (b) replace the body of the existing `readme-fragments` topic. Option (a) is cleaner; option (b) is fewer LOC. Pick based on the existing topic-registry shape — read `internal/workflow/recipe_topic_registry.go` before deciding.

### 5.2 `internal/content/workflows/recipe.md` — mandatory-fact guidance during deploy

**Current state**: `zerops_record_fact` is described in the tool spec but there is no prompt-level pressure for the main agent to call it.

**Action**: In the deploy step-entry section and in the feature-subagent brief, add a block:

```
### Fact recording — mandatory during deploy

Every non-obvious incident encountered during deploy must be logged via
`zerops_record_fact` at the moment the fix is applied. This is how the
content-authoring sub-agent at the end of deploy writes the gotchas section.

CALL `zerops_record_fact` when you:
- Apply a fix for a non-trivial build / deploy / runtime failure
- Verify a non-obvious platform behavior (e.g., execOnce idempotency, readiness gate, L7 routing)
- Establish a cross-codebase contract binding (DB schema owner, NATS queue group name, HTTP response shape)
- Notice the scaffold emitted a known-trap pattern that required a runtime rewrite

Required fields: type, title. Recommended: substep, codebase, mechanism, failureMode, fixApplied, evidence.

Valid types: gotcha_candidate, ig_item_candidate, verified_behavior,
platform_observation, fix_applied, cross_codebase_contract.

Content DO NOT WRITE during deploy. README.md, CLAUDE.md, IG items, and gotchas
are authored by a separate sub-agent at the readmes sub-step. Your job during
deploy is to record facts, not to narrate them.
```

**Placement**: Add this block to recipe.md inside the deploy step-entry section AND at the top of the feature-subagent brief (both readers need it).

### 5.3 Env-README expansion — new content surface

**Current state**: Env READMEs are generated from a template in the recipe-finalize code path and ship as 7 lines of intro + boilerplate. See v28 `environments/*/README.md`.

**Action — spec side**: Add a `EnvReadmeExpansions` field (or similar) to the `generate-finalize` input or to a new workflow action. The main agent invokes this after the content-authoring subagent returns with its `env-readme-expansions` artifact.

**Action — template side**: Each env README becomes:

```
# NestJS Showcase — {Tier Name} Environment

This is the {tier name} environment for [NestJS Showcase (info + deploy)](https://app.zerops.io/recipes/{slug}?environment={tier-slug}) recipe on [Zerops](https://zerops.io).

<!-- #ZEROPS_EXTRACT_START:intro# -->
{1-2 sentence intro — audience + use case}
<!-- #ZEROPS_EXTRACT_END:intro# -->

## Who this is for

{Specific audience — AI agent iterating, remote dev, local dev, stage reviewer, small prod, HA prod}

## What changes vs the adjacent tier

{Concrete diff from the lower tier if any, or "entry-level tier" note}

## Promoting to the next tier

{What to flip, what to redeploy, what to verify — or "terminal tier" note for HA prod}

## Tier-specific operational concerns

{Things specific to THIS tier — SSH-driven iteration, rolling-deploy testing, HA failover verification, etc.}
```

Target: 40–80 lines per env README. Each section is 3–8 lines. Content is authored by the content-authoring sub-agent from the tier's import.yaml + the adjacent tier's import.yaml.

**Implementation approach**: Extend the existing finalize writer (wherever env READMEs are emitted today). Search for the template string:

```bash
grep -rn "ai agent environment\|small production environment offers" internal/
```

Replace the boilerplate generator with a path that takes structured `EnvReadmeExpansion` data (produced by the content-authoring subagent) and writes it into the README template.

### 5.4 `internal/content/workflows/recipe.md` — scaffold pre-flight self-verification

**Design principle**: A prose "before you ship, check this list" is verification by inspection — brittle because it depends on the subagent remembering and applying each rule. A runnable checklist is verification by execution — the assertions either pass or they don't, and the subagent cannot ship without passing them.

**Current location**: scaffold-subagent-brief block (search for `<block name="scaffold-subagent-brief">` in [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md)).

**Action**: Inside that block, add the following Pre-Ship Self-Verification section. Each rule is paired with a concrete grep/assertion that the subagent runs against its own output on the SSHFS mount. Any failure blocks return; subagent fixes and re-runs the assertion set.

```markdown
### Pre-ship self-verification (MANDATORY — do not return to the main agent until all assertions pass)

Before returning, you MUST run the following assertions against your generated code
on the SSHFS mount (`/var/www/{hostname}/`). Each assertion is a concrete command.
If it exits non-zero, fix the underlying issue in your code and re-run the FULL
assertion set before returning.

Copy-paste this block as a shell script (replace `{hostname}` with your codebase):

```bash
HOST={hostname}
MOUNT=/var/www/$HOST
FAIL=0

# Assertion 1 — NO self-shadow in run.envVariables
# Matches `  key: ${key}` pattern. Skips mode flags like NODE_ENV.
if grep -nE "^\s+([a-zA-Z_][a-zA-Z0-9_]*):\s*\\\$\{\1\}\s*$" $MOUNT/zerops.yaml \
    | grep -v "NODE_ENV" | head -1; then
    echo "FAIL: self-shadow pattern in zerops.yaml — see env-var-model guide"
    FAIL=1
fi

# Assertion 2 — If app.listen is used, bind 0.0.0.0
if grep -rl "app\.listen\|\.listen(" $MOUNT/src/ 2>/dev/null | while read f; do
    if grep -q "app.listen\|\.listen(" "$f"; then
        grep -qE "'0\.0\.0\.0'|\"0\.0\.0\.0\"|0\.0\.0\.0" "$f" || echo "$f"
    fi
done | head -1; then
    echo "FAIL: app.listen without 0.0.0.0 binding — L7 balancer returns 502 on localhost"
    FAIL=1
fi

# Assertion 3 — Express `trust proxy` if Express detected
if grep -rq "express\|NestFactory" $MOUNT/src/ 2>/dev/null; then
    if ! grep -rq "trust proxy\|trustProxy" $MOUNT/src/; then
        echo "FAIL: Express/Nest without trust proxy — req.protocol/req.ip reflect balancer, not client"
        FAIL=1
    fi
fi

# Assertion 4 — AWS SDK S3Client with forcePathStyle
if grep -rq "new S3Client\|S3Client(" $MOUNT/src/ 2>/dev/null; then
    if ! grep -rq "forcePathStyle:\s*true" $MOUNT/src/; then
        echo "FAIL: S3Client without forcePathStyle: true — MinIO rejects virtual-hosted-style"
        FAIL=1
    fi
fi

# Assertion 5 — NATS ClientProxy NOT using URL-embedded credentials
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: URL-embedded NATS creds — v2 client strips them silently"
    FAIL=1
fi

# Assertion 6 — Valkey / Redis connection string with password segment
# (Zerops managed Valkey has no auth — adding :password@ breaks DNS)
if grep -rnE "redis://[^@]*:[^@]*@|valkey://[^@]*:[^@]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: Valkey/Redis connection with :password@ — managed service has no auth"
    FAIL=1
fi

# Assertion 7 — Static base: deployFiles uses tilde suffix
if grep -q "base:\s*static" $MOUNT/zerops.yaml; then
    if grep -q "deployFiles:\s*\./dist\s*$" $MOUNT/zerops.yaml; then
        echo "FAIL: static base with deployFiles: ./dist (no tilde) — Nginx 404s on /"
        FAIL=1
    fi
fi

# Assertion 8 — .gitignore exists and covers node_modules + dist
if [ ! -f $MOUNT/.gitignore ]; then
    echo "FAIL: .gitignore missing"
    FAIL=1
elif ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore; then
    echo "FAIL: .gitignore does not ignore node_modules — 209 MB bloat class (v21)"
    FAIL=1
fi

# Assertion 9 — .env.example preserved as documentation (not populated .env)
if [ -f $MOUNT/.env ] && [ ! -f $MOUNT/.env.example ]; then
    echo "FAIL: .env exists without .env.example — dotenv will shadow OS env vars at runtime"
    FAIL=1
fi

exit $FAIL
```

Run this via `bash <script>` over ssh into the codebase container (or equivalent
on the mount). If FAIL=1, fix the specific issue reported and re-run the ENTIRE
script until it exits 0. Do NOT return to the main agent until all nine
assertions pass.

Cite the relevant `zerops_knowledge` guide when applying a fix — `env-var-model`
for self-shadow, `http-support` for 0.0.0.0 bind + trust proxy, `object-storage`
for forcePathStyle. Do NOT invent mental models; follow the guide's framing.

As new recurrent traps surface in future runs, append new assertions. Each
entry prevents the next recipe from repeating a runtime incident that already
cost time once.
```

**Record-fact instruction**: Each time an assertion fails and is fixed, the subagent should call `zerops_record_fact` with `type=fix_applied` (NOT `gotcha_candidate`) so the content-authoring subagent later classifies it correctly as a scaffold decision, not a platform gotcha.

**Framework-agnostic property**: the MECHANISM (self-verifying subagent that runs assertions against its own output) is universal. The ASSERTIONS are framework-specific, which is correct — each codebase's framework determines what patterns to check. The list extends over time as new recurrent traps are identified.

### 5.5 `internal/tools/workflow_checks_generate.go` — fix `env_self_shadow` enumeration

**Current state**: [internal/tools/workflow_checks_generate.go:290](../internal/tools/workflow_checks_generate.go#L290) `checkEnvSelfShadow(hostname, entry)` exists and `ops.DetectSelfShadows` matches the exact pattern. v28 evidence: workerdev shipped 9 self-shadow lines, `complete step=generate` returned 11 checks, zero `worker_*`-prefixed checks were present.

**Investigation required**: Open [internal/tools/workflow_checks_generate.go](../internal/tools/workflow_checks_generate.go) and trace upward from `checkEnvSelfShadow` call site to the loop that iterates hostnames. Something is skipping the worker hostname at generate-complete. Possibilities:
- Hostname enumeration reads only `app`/`api` prefixed hosts
- Iterates codebases only if a zerops.yaml parse succeeds AND some predicate holds
- Conditional early-exit when a parent check fails

**Fix**: whichever it is, make the enumeration cover every hostname with a `zerops.yaml` on disk.

**TDD requirement** (per CLAUDE.md):
1. Write RED test: a plan with apidev + appdev + workerdev, all three with `zerops.yaml` shipped, workerdev's yaml containing `db_hostname: ${db_hostname}` in `run.envVariables`. Call `complete step=generate`. Assert the response's `checks` array contains an item with `name: "worker_env_self_shadow"` and `status: "fail"`.
2. Verify test fails on current code.
3. Fix enumeration.
4. Verify test passes.
5. Add a second test: same plan with workerdev's yaml clean (no shadows) — assert `worker_env_self_shadow: pass`.

**File targets**: new test at `internal/tools/workflow_checks_generate_test.go` (or the existing `_test.go` if one exists). Source fix at `workflow_checks_generate.go`.

### 5.6 `internal/workflow/recipe_topic_registry.go` — register new guidance topic

If you chose option (a) in §5.1 (new topic `content-authoring-brief`):

Add a topic entry to the registry (search for existing entries like `subagent-brief`, `readme-fragments`):

```go
{
    ID:       "content-authoring-brief",
    Title:    "Content-authoring sub-agent brief",
    EagerAt:  "", // NOT eager — only delivered at SubStepReadmes substep-complete
    BuildBody: func(plan *RecipePlan) string {
        return contentAuthoringBriefBody(plan)
    },
},
```

And remap in `subStepToTopic`:

```go
case SubStepReadmes:
    return "content-authoring-brief"
```

The body builder function loads a markdown template (embed or inline) containing the full brief from §6.

If you chose option (b) (replace `readme-fragments` body): edit that topic's `BuildBody` to return the new brief text.

### 5.7 Tests

Per CLAUDE.md's TDD rule, write tests BEFORE implementation. The test suite:

1. **`internal/tools/record_fact_mandatory_test.go`** (or extend existing record_fact test): not a functional test — verify the recipe.md guidance at the deploy step-entry contains the phrase `mandatory during deploy` and lists the four required call situations. Shape check against string contents.
2. **`internal/workflow/recipe_substep_briefs_test.go`** (extend): assert that `subStepToTopic(RecipeStepDeploy, SubStepReadmes, plan)` returns the new topic ID and that `buildSubStepGuide(RecipeStepDeploy, SubStepReadmes)` response contains the key phrases from the content-surface spec — "surface contract", "classification taxonomy", "citation map", at minimum the six surface names.
3. **`internal/tools/workflow_checks_generate_test.go`**: the `env_self_shadow` enumeration tests from §5.5.
4. **`internal/workflow/recipe_substeps_test.go`** (extend): if you change substep order or add substeps, update. Otherwise no-op.
5. **Integration test**: end-to-end `complete step=deploy substep=readmes` response contains the new brief. Run via `go test ./internal/workflow/ ./internal/tools/`.

Full command: `go test ./... -count=1 -short` then `go test ./... -count=1 -race` before shipping.

---

### 5.8 Workspace state manifest (workflow efficiency — I-4)

**Problem**: Each subagent (scaffold ×3, feature, code-review, content-authoring) starts with empty context and crawls the filesystem to understand what's there. ~30 file reads × 5-6 subagents that need orientation = ~150 redundant file reads per recipe run. Main-agent wall time during these crawls is ~2-3 min per subagent spin-up.

**Solution shape**: Main agent maintains a structured JSON manifest at a well-known path. Writers (scaffold subagents, feature subagent) append entries on return. Readers (subsequent subagents) consult the manifest once, skip the crawl.

**File path convention**: `/tmp/zcp-workspace-{sessionID}.json` (same directory as `FactLogPath` from [internal/ops/facts_log.go:54](../internal/ops/facts_log.go#L54) — colocated for easy discovery).

**Schema** (evolves over the run; here's the complete end-state):

```json
{
  "session_id": "c0b06dd3b24748be",
  "plan_slug": "nestjs-showcase",
  "last_updated": "2026-04-18T12:14:30Z",
  "codebases": {
    "apidev": {
      "framework": "NestJS 11",
      "runtime": "nodejs@22",
      "scaffold_completed_at": "2026-04-18T11:08:00Z",
      "source_files": [
        {"path": "src/main.ts", "purpose": "bootstrap + listen", "exports": ["bootstrap"]},
        {"path": "src/app.module.ts", "purpose": "root module", "exports": ["AppModule"]},
        {"path": "src/services/storage.service.ts", "purpose": "S3 client wrapper", "exports": ["StorageService"]},
        {"path": "src/services/nats.service.ts", "purpose": "NATS client (publisher)", "exports": ["NatsService"]},
        {"path": "src/services/cache.service.ts", "purpose": "Valkey client", "exports": ["CacheService"]},
        {"path": "src/services/search.service.ts", "purpose": "Meilisearch client", "exports": ["SearchService"]},
        {"path": "src/entities/item.entity.ts", "purpose": "Item entity", "exports": ["Item"]},
        {"path": "src/entities/job.entity.ts", "purpose": "Job entity (shared w/ workerdev)", "exports": ["Job"]},
        {"path": "src/migrate.ts", "purpose": "TypeORM migration runner", "exports": []},
        {"path": "src/seed.ts", "purpose": "DB seed + Meilisearch index sync", "exports": []}
      ],
      "zerops_yaml": {
        "path": "zerops.yaml",
        "setups": ["dev", "prod"],
        "managed_services_wired": ["db", "cache", "queue", "storage", "search"],
        "has_init_commands": true,
        "exposes_http": true,
        "http_port": 3000
      },
      "pre_flight_checks": {
        "passed": ["self-shadow", "0.0.0.0-bind", "trust-proxy", "forcePathStyle", "nats-creds-separate", "valkey-no-password", "gitignore"],
        "failed": []
      }
    },
    "appdev": { "...": "..." },
    "workerdev": { "...": "..." }
  },
  "contracts": {
    "nats_subjects": {
      "jobs.dispatch": {
        "publisher": "apidev (JobsController)",
        "consumer": "workerdev (WorkerController)",
        "queue_group": "jobs-workers",
        "payload_shape": "{id: string, payload: unknown}"
      }
    },
    "http_response_shapes": {
      "/api/status": "{db: ok|error, cache: ok|error, queue: ok|error, storage: ok|error, search: ok|error}"
    },
    "shared_entities": {
      "Job": {"owner": "apidev/src/entities/job.entity.ts", "consumers": ["workerdev"]}
    }
  },
  "features_implemented": [
    {"id": "items-crud", "touches": ["apidev/src/items/", "appdev/src/lib/features/ItemsCrud.svelte"], "at": "2026-04-18T11:35:00Z"},
    {"id": "cache-demo", "touches": ["apidev/src/cache/", "appdev/src/lib/features/CacheDemo.svelte"], "at": "..."},
    {"id": "storage-upload", "touches": ["..."], "at": "..."},
    {"id": "search-items", "touches": ["..."], "at": "..."},
    {"id": "jobs-dispatch", "touches": ["apidev/src/jobs/", "appdev/src/lib/features/JobsDispatch.svelte", "workerdev/src/worker.controller.ts"], "at": "..."}
  ]
}
```

**New MCP tools**:

- `zerops_workspace_manifest action=read` — returns the full manifest (or an empty skeleton if not yet initialized)
- `zerops_workspace_manifest action=update` — takes a JSON-patch-style partial update, merges it. Called by main agent after each subagent return (or by subagents themselves if the brief grants access — recommend main-only to keep the manifest authoritative).

**Implementation location**:
- Schema + read/write helpers: `internal/ops/workspace_manifest.go` (new file, ~150 lines — follows the shape of `facts_log.go`)
- MCP tool handler: `internal/tools/workspace_manifest.go` (new file)
- Registration: add to `internal/server/server.go` alongside `RegisterRecordFact`

**Brief changes**:
- **Scaffold subagent brief**: at return time, call `zerops_workspace_manifest action=update` with the codebase's entry (framework, runtime, source_files, zerops_yaml shape, pre_flight_checks results).
- **Feature subagent brief**: at start, call `zerops_workspace_manifest action=read` to see what scaffolds produced; at return, call `action=update` with `contracts` + `features_implemented` entries.
- **Code-review subagent brief**: at start, read the manifest to scope review (focus on feature-modified files, cross-codebase contracts).
- **Content-authoring subagent brief**: at start, read the manifest — it is one of the three primary inputs alongside the facts log and platform guides. Use it to understand what exists without crawling.

**Expected savings**: 2-3 min per subagent × 4 subagents that read the manifest = **8-12 min off recipe wall**.

**Framework-agnostic property**: the schema has no framework-specific fields. `source_files[].purpose` is a short human string written by the scaffold subagent based on what it actually wrote. `managed_services_wired` uses the service-hostname convention that's universal on Zerops. `contracts` is a free-form map written by the feature subagent — encode whatever shape-bindings matter for the recipe.

### 5.9 `zerops_deploy_batch` — parallel deploys (workflow efficiency — I-5)

**Problem**: MCP STDIO serializes tool calls. v23 discovered this: three parallel `zerops_deploy` calls cause two to return `Not connected` mid-build because the channel is busy. Agent falls back to sequential deploys: 50-90s × 3 = 150-270s per 3-codebase cluster. With 4-5 clusters per run (dev initial, snapshot-dev, stage-initial, close-dev-redeploy, close-cross-stage), this costs **8-10 minutes per run** of serialized build wall.

**Solution shape**: New MCP tool `zerops_deploy_batch` accepts an array of deploy targets, kicks off all builds server-side in parallel, returns when all complete (or any fail). Single MCP call → N parallel builds.

**Tool signature**:

```go
type DeployBatchInput struct {
    Targets []DeployTarget `json:"targets" jsonschema:"required,Array of deploy targets. Each target is one service+setup combination. Minimum 1, recommend 2-3 for parallelism. Values beyond 5 may hit build-queue limits and fall back to serial."`
    WaitAll bool           `json:"waitAll,omitempty" jsonschema:"Default true — wait until all builds complete. Set false to return immediately after kickoff (agent polls status via zerops_deploy_batch action=status)."`
}

type DeployTarget struct {
    TargetService string `json:"targetService" jsonschema:"required"`
    Setup         string `json:"setup" jsonschema:"required"`
}

type DeployBatchResponse struct {
    BatchID  string                    `json:"batchId"`
    Results  []DeployResult            `json:"results"` // Filled when waitAll=true
    Summary  string                    `json:"summary"` // "3/3 succeeded" or "2/3 succeeded, 1 failed"
}
```

**Implementation location**:
- Ops-layer: `internal/ops/deploy_batch.go` — coordinates parallel builds via goroutines, aggregates results
- Tool handler: `internal/tools/deploy_batch.go`
- Registration: `internal/server/server.go`
- Tests: `internal/tools/deploy_batch_test.go` — MockClient returns staged responses; verify parallel kickoff, verify aggregation, verify partial-failure handling

**Brief changes**:
- `recipe.md` deploy step-entry: replace the serial-deploy narrative with the batch pattern. Show a concrete example: *"at cross-deploy, call `zerops_deploy_batch targets=[{apistage,prod},{workerstage,prod},{appstage,prod}]` in one call instead of three serial `zerops_deploy` calls."*
- Same for initial-dev, snapshot-dev, close-redeploy.

**Expected savings**: 5-8 minutes off recipe wall.

**Framework-agnostic property**: entirely platform-layer. No framework assumptions.

**Failure modes to test**:
- One target fails, others succeed → response shows per-target results, agent applies fix to the failed target only
- `waitAll: false` + agent calls `status` too early → returns partial results with `completed: false` per target
- Partial batch: 2 of 3 targets have stale commits, one has none → only 2 actually deploy, third reports "no-op"

### 5.10 Main/scaffold concurrency (workflow efficiency — I-3)

**Problem**: After dispatching parallel scaffold subagents, main agent sits idle waiting for their return (~3 min). Main has enough information from the plan to write all three `zerops.yaml` files: runtime types are declared, ports are declared, setups are declared, managed-service list is declared. zerops.yaml shape does not depend on scaffold-specific code.

**Solution shape**: Prompt-level change in `recipe.md` generate-step section. After the scaffold-dispatch paragraph, add:

```markdown
### Concurrency — begin zerops.yaml authoring in parallel with scaffolds

The moment you dispatch the three scaffold sub-agents, begin writing the three
`zerops.yaml` files yourself. The scaffold sub-agents do not write the zerops.yaml
(see scaffold-subagent-brief §"What you do NOT do" — zerops.yaml is main-agent
territory). Their work produces source code; yours produces deploy config. These
work streams are independent.

You have everything needed: plan.runtime.type, plan.targets[].hostname,
plan.targets[].ports, plan.targets[].setups, the managed-service list from
plan.services. Write each zerops.yaml now using the topic:zerops-yaml-rules
guidance and the field reference elsewhere in this section. When the scaffold
sub-agents return, you merge their source output (not their config output —
they don't produce config) with your zerops.yaml that's already on disk.

Do NOT wait for scaffold sub-agents to return before starting. That wastes
2-3 minutes of wall time doing nothing.
```

**Implementation**: one section-insertion in [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md) near the scaffold-dispatch paragraph. No code changes.

**Expected savings**: 2-3 min off generate step.

**Framework-agnostic property**: the concurrency principle ("begin work the main agent can do with plan-level information while subagents do work that requires their own context") applies to every recipe shape. The specific application (zerops.yaml during scaffold) is the current example; the pattern generalizes.

---

## 6. The content-authoring sub-agent brief — full text to embed

This is the complete replacement for recipe.md §"Per-codebase README with extract fragments (post-deploy readmes sub-step)". Embed verbatim (update timestamps / version strings as needed). Length: ~400 lines.

```markdown
### Content authoring — all reader-facing surfaces (post-deploy `readmes` sub-step)

**⚠ TOOL-USE POLICY — if this brief is used as a sub-agent dispatch prompt, read before your first tool call.**

{existing tool-use-policy block from v8.90 — preserve verbatim}

---

**Role**: You are a content-authoring sub-agent. You have NO memory of the run that dispatched you. Your context is intentionally clean of the debug spiral, because reader-facing content must be written from the reader's perspective, not the author's. Three pathologies shipped across v20–v28 when the debugging agent also wrote the content:

1. **Fabricated mental models** — inventing mechanisms to explain observations ("interpolator resolved before shadow formed", "execOnce burned the key at workspace creation")
2. **Wrong-surface placement** — framework documentation / npm metadata / own-scaffold details shipped as Zerops gotchas
3. **Self-referential decoration** — documenting the recipe's own helpers as universal integration steps

Your job is to avoid all three by writing against reader-facing tests, not author-facing impressions.

**Inputs you have**:
- The structured facts log at `$ZCP_FACTS_LOG` (path: `/tmp/zcp-facts-{sessionID}.jsonl`). Read it with `cat` or any JSON-line parser. Each line is a `FactRecord` (see ops/facts_log.go).
- The final recipe state at the SSHFS-mounted paths (`/var/www/apidev/`, `/var/www/appdev/`, `/var/www/workerdev/`). Read-only except for the files you write.
- Platform guides via `zerops_knowledge topic=<id>`. Call on demand — see the Citation Map below for which topics map to which guides.

**Inputs you do NOT have**: the run transcript, the main agent's context, any memory of what went wrong. If you want to know what happened during deploy, read the facts log.

---

### The six content surfaces

Every recipe has six kinds of reader-facing content. Each surface has a specific reader, purpose, and one-question test. An item that fails its surface's test is **removed, not rewritten to pass**.

**1. Root README** (`/var/www/zcprecipator/nestjs-showcase/README.md`)
- Reader: developer browsing zerops.io/recipes
- Purpose: decide whether to deploy, pick a tier
- Test: *"Can a reader decide in 30 seconds whether this deploys what they need and pick the right tier?"*
- Typical: 20–30 lines

**2. Environment README** (`environments/{N — Tier}/README.md`, 6 files)
- Reader: someone deciding WHICH tier to deploy or promote to
- Purpose: teach tier audience + how it differs from the adjacent tier
- Test: *"Does this teach me when to outgrow this tier and what changes at the next one?"*
- Typical: 40–80 lines — NOT the 7-line boilerplate that shipped in v28

**3. Environment `import.yaml` comments** (`environments/{N — Tier}/import.yaml`, 6 files — comments only, structure is generated)
- Reader: someone reading the manifest in Zerops dashboard
- Purpose: explain every decision (scale, mode, presence)
- Test: *"Does each service block explain a decision, not narrate what the field does?"*

**4. Per-codebase README integration-guide fragment** (`{codebase}/README.md`, between `#ZEROPS_EXTRACT_START:integration-guide#` and `#ZEROPS_EXTRACT_END:integration-guide#`)
- Reader: porter bringing their own existing app
- Purpose: enumerate Zerops-specific changes the porter must make in their own codebase
- Test: *"Does a porter bringing their own code need to copy THIS exact content?"*

**5. Per-codebase README knowledge-base/gotchas fragment** (`{codebase}/README.md`, between `#ZEROPS_EXTRACT_START:knowledge-base#` and `#ZEROPS_EXTRACT_END:knowledge-base#`)
- Reader: developer hitting a confusing failure on Zerops
- Purpose: surface platform traps that are non-obvious even to someone who read the docs
- Test: *"Would a developer who read the Zerops docs AND the framework docs STILL be surprised by this?"*

**6. Per-codebase CLAUDE.md** (`{codebase}/CLAUDE.md`)
- Reader: someone with THIS repo checked out working on it
- Purpose: operational guide for dev loop, testing, resetting state
- Test: *"Is this useful for operating THIS repo — not for deploying or porting?"*

**7. Per-codebase `zerops.yaml` comments** (`{codebase}/zerops.yaml` — comments only; structure was written at generate)
- Reader: someone reading the deploy config
- Purpose: explain non-obvious choices
- Test: *"Does this explain a trade-off the reader couldn't infer from the field name?"*

---

### Fact classification taxonomy

Every fact from the facts log gets classified BEFORE it is placed on any surface. Classification determines routing. Facts that classify as self-inflicted or framework-only are DISCARDED, not published.

| Classification | Test | Route to |
|---|---|---|
| **Platform invariant** | Fact is true of Zerops regardless of this recipe's scaffold choices. A different framework entirely would hit it. | Knowledge-Base gotcha (with citation if guide exists) |
| **Platform × framework intersection** | Framework-specific AND platform-caused. Neither alone produces it. | Knowledge-Base gotcha, naming both sides clearly |
| **Framework quirk** | Framework's own behavior; Zerops not involved. | **DISCARD** — belongs in framework docs |
| **Library metadata** | npm / composer / pip / cargo concern. | **DISCARD** — belongs in manifest comments |
| **Scaffold decision** | "We chose X over Y." Non-obvious design choice in recipe's own code. | `zerops.yaml` comment (config), IG prose (code principle), or CLAUDE.md (operational) |
| **Operational detail** | How to iterate / test / reset this repo. | CLAUDE.md |
| **Self-inflicted** | Our code had a bug; we fixed it; a reasonable porter doesn't hit it. | **DISCARD** — not content material |

**Concrete classification rules**:

1. Separate mechanism (what Zerops does) from symptom (what our code did wrong). Classify on mechanism.
2. Ask "would they hit this with different scaffold code?" — no → scaffold decision or self-inflicted; yes → invariant or intersection.
3. If a `zerops_knowledge` guide covers this topic, the fact is probably a platform invariant — route as gotcha WITH citation, don't duplicate guide content.
4. Self-inflicted test: "Could this observation be summarized as 'our code did X, we fixed it to do Y'?" If yes, discard. The fix belongs in code; no teaching for a porter.

---

### Citation map — mandatory guide consultation

When a fact's topic matches one of these, you MUST call `zerops_knowledge topic=<id>` and read the guide BEFORE writing about that topic. Folk-doctrine ships when authors invent mental models for things the platform already documents.

| Topic area | Guide ID |
|---|---|
| Cross-service env vars, self-shadow, aliasing | `env-var-model` |
| `zsc execOnce`, `appVersionId`, init commands | `init-commands` (or closest match) |
| Rolling deploys, SIGTERM, HA replicas, minContainers two-axis | `rolling-deploys` (or closest) |
| Object Storage (MinIO, forcePathStyle) | `object-storage` |
| L7 balancer, httpSupport, VXLAN routing, trust proxy | `http-support` / `l7-balancer` |
| Deploy files, tilde suffix, static base | `deploy-files` / `static-runtime` |
| Readiness check / health check | `readiness-health-checks` |

If `zerops_knowledge` returns "no matching topic" for a citation-map entry, log that and proceed — the guide may not exist yet and your content is genuinely filling a gap. But you must have tried.

---

### Counter-examples — wrong-surface / folk-doctrine patterns from v28

Do NOT ship content that matches any of these patterns.

**Self-inflicted shipped as gotcha**:
- "`zsc execOnce` can record a successful seed that produced zero output" — Agent's seed script silently exited 0. `execOnce` correctly honored exit code. This is a seed-script bug, not a platform trap. DISCARD.

**Framework quirks shipped as gotchas**:
- "`app.setGlobalPrefix('api')` collides with `@Controller('api/...')`" — Pure NestJS fact. DISCARD.
- "`@sveltejs/vite-plugin-svelte@^5` peer-requires Vite 6" — npm metadata. DISCARD.

**Self-referential decoration**:
- "`api.ts`'s content-type check catches SPA fallback" — `api.ts` is the recipe's own helper. Principle (Nginx SPA fallback returns `200 text/html`) belongs in IG; implementation detail belongs in code comments.

**Folk-doctrine defects** (fabricated mental models — the worst class):
- "*The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that.*" — FABRICATED. Both codebases had identical shadow patterns and both were broken. The real rule (from `env-var-model` guide): cross-service vars auto-inject project-wide; NEVER declare `key: ${key}`. If you're writing something about env vars, you MUST have read the guide first.
- "*NATS 2.12 in mode: HA — clustered broker with JetStream-style durability*" — the recipe uses core NATS, not JetStream. Describe actual behavior, not plausible-sounding adjacent mechanisms.

---

### Workflow

1. **Read the facts log**. `cat $ZCP_FACTS_LOG` or equivalent. Group records by codebase + substep.
2. **Read the final recipe state**. Each `{codebase}/zerops.yaml`, `{codebase}/src/`, `environments/*/import.yaml`.
3. **Classify every fact** using the taxonomy. For each, identify: destination surface (if any), matching citation-map entry (if any).
4. **Fetch matching guides** via `zerops_knowledge`. Read before writing about the topic.
5. **Write all six surface types** — one pass, top-down:
   - Root README (use prettyName from plan + service list)
   - Env READMEs (6 files — from adjacent tier comparison)
   - Env import.yaml comments (via generate-finalize structured input — you emit the comment set, the main agent applies it)
   - Per-codebase README (IG fragment + KB fragment + intro fragment, extract markers byte-exact)
   - Per-codebase CLAUDE.md
   - Per-codebase zerops.yaml comments
6. **Self-review before return**. For each item on each surface, write (in your return message) the answer to the surface's test. Any "no" → remove the item.

---

### Deliverables

- `/var/www/zcprecipator/nestjs-showcase/README.md`
- `/var/www/zcprecipator/nestjs-showcase/environments/{N — Tier}/README.md` × 6
- `/var/www/apidev/README.md`, `/var/www/appdev/README.md`, `/var/www/workerdev/README.md`
- `/var/www/apidev/CLAUDE.md`, `/var/www/appdev/CLAUDE.md`, `/var/www/workerdev/CLAUDE.md`
- Updates (comments only) to `/var/www/{codebase}/zerops.yaml` (3 files) if existing comments fail their test
- A structured `env-comment-set` JSON (per the generate-finalize schema) for the 6 env import.yamls — returned in your completion message for the main agent to apply.

Per-codebase README format (marker shape enforced byte-for-byte by the extract checker):

```markdown
# {Framework} {PrettyName} Recipe App

<!-- #ZEROPS_EXTRACT_START:intro# -->
{1-2 sentence intro with service list}
<!-- #ZEROPS_EXTRACT_END:intro# -->

⬇️ **Full recipe page and deploy with one-click**

[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/{slug}?environment=small-production)

![{framework} cover](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-{framework}.svg)

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding `zerops.yaml`
{full commented yaml pasted from disk — DO NOT rewrite from memory}

### 2. {Platform-forced change — one per real change in the scaffold}
{code diff + Zerops reason}

### 3. {etc}

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **{Concrete symptom}** — {mechanism and failure mode with evidence; cite guide if citation-map match}
- ...

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
```

Per-codebase CLAUDE.md: plain markdown, no fragments. Sections: Dev Loop / Migrations & Seed / Container Traps / Testing / Resetting dev state / Driving a feature end-to-end. ≥1200 bytes, ≥2 custom sections beyond the template.

Env README: see template above. 40–80 lines each.

---

### Self-review checklist — apply before returning

For each deliverable, answer in your completion message:

**Root README**: "A reader browsing recipes — can they decide in 30 sec? Yes / No."

**Each env README**: "A reader considering this tier — do they learn when to outgrow it and what the next tier changes? Yes / No."

**Each env import.yaml comment block**: "Does this block explain a decision, not narrate what the field does? Yes / No." (One answer per service block.)

**Each IG item**: "A porter bringing their own code — do they need to copy THIS exact content? Yes / No."

**Each gotcha**: "A developer who read the Zerops docs AND the framework docs — would they STILL be surprised by this? Yes / No. If the topic matches the citation map, did I read the guide? Yes / No."

**Each CLAUDE.md section**: "Useful for operating THIS repo, not for deploying or porting? Yes / No."

**Each zerops.yaml comment**: "Does this explain a trade-off the reader couldn't infer from the field name? Yes / No."

Any item with "no" is removed from its surface. Do not rewrite to pass the test — rewrite means the item was on the wrong surface to begin with.

Return a completion message with:
1. List of files written + byte counts
2. Classification summary: how many facts from the log → invariant / intersection / framework-quirk / library-meta / scaffold-decision / operational / self-inflicted
3. Self-review answers above
4. `env-comment-set` JSON payload for the main agent to apply at generate-finalize
```

---

## 7. The `content-surface-contracts` guidance topic

If you chose option (a) — new topic — this is the body. If option (b), replace `readme-fragments` body with this.

The body IS primarily §6's brief text. Register it as:

```go
{
    ID:       "content-authoring-brief",
    Title:    "Content-authoring sub-agent brief (all reader-facing surfaces)",
    EagerAt:  "", // delivered at SubStepReadmes substep-complete only
    BuildBody: buildContentAuthoringBrief,
},
```

`buildContentAuthoringBrief(plan *RecipePlan) string` returns the §6 text with `{framework}` / `{PrettyName}` / `{slug}` substituted. Load from an embedded template via `go:embed` or inline as a Go multiline string (keep `internal/content/workflows/recipe.md` ≤350 lines per file constraint — if it overflows, move the brief to a separate `.md` file and embed).

---

## 8. Acceptance criteria — v29 calibration bar

v29 is the first full recipe run after v8.94 ships. It passes if:

**Content-quality criteria (primary goal)**:

1. **Gotcha quality**: ≥80% of gotchas pass the fresh-developer surprise test. Manual audit: walk each gotcha, apply the test from spec-content-surfaces.md §3 Surface 5, tally.
2. **0 folk-doctrine defects**: every gotcha whose topic matches the citation map carries a guide citation and uses the guide's framing. Grep test: for each gotcha, if its text contains terms matching a citation-map topic, response must include a pointer to the matching guide.
3. **0 cross-surface fact duplication**: each fact lives on exactly one surface. Other surfaces that need it cross-reference. Diff test: no two surfaces contain the same 10-word span about the same mechanism.
4. **Env READMEs ≥40 lines each** with genuine teaching (not boilerplate). Count test: `wc -l environments/*/README.md` each ≥40.
5. **Env import.yaml comments have zero factual errors**: manual audit against the recipe's actual code (no more "JetStream-style" or "tsbuildinfo-in-dev-watch" class errors).
6. **Every `env_self_shadow` check fires for every enumerated host**: grep `complete step=generate` response for `{hostname}_env_self_shadow` — one entry per host with a zerops.yaml.
7. **Content-authoring subagent writes all six surface types**: main agent writes 0 content files after SubStepSubagent returns. Metric: 0 `Write` or `Edit` tool uses by main agent targeting `{codebase}/README.md`, `{codebase}/CLAUDE.md`, `environments/*/README.md`, `{codebase}/zerops.yaml` after the content-authoring subagent dispatches.
8. **`zerops_record_fact` called ≥5 times** during deploy (v28 baseline: 3 voluntary). Baseline shift to ≥5 reflects the mandatory-use prompt. Upper bound reasonable at ~15 — if >20, the prompt is over-steering.
9. **No new content-check failures at generate or deploy complete** beyond v28 baseline (the existing check surface stays as-is).

**Workflow-efficiency criteria (secondary goal)**:

10. **Wall-clock ≤75 min recipe-work** (v28: 85 min). Expected median after all Tier-1 fixes: 55–65 min. A ≤75-min bar is conservative and leaves headroom for first-run subagent-dispatch overhead.
11. **Workspace manifest reads ≥3 times** during the run (feature subagent + code-review subagent + content-authoring subagent must each call `zerops_workspace_manifest action=read` at least once). Grep the session logs for `workspace_manifest` tool calls.
12. **Subagent scaffold-phase crawl drops ≥30%** (measured by non-zerops-yaml Read count in scaffold/feature/code-review/content-authoring subagents). v28 baseline: ~75 Reads across non-scaffold subagents. v29 target: ≤50.
13. **`zerops_deploy_batch` used for every 3-deploy cluster**: at least 4 `zerops_deploy_batch` calls per run (initial-dev, snapshot-dev, cross-stage, close-redeploy-dev). Serial `zerops_deploy` calls should only appear for single-service redeploys (init-commands retry, workerdev rebuild after a fix).
14. **Main agent writes zerops.yaml files in parallel with scaffold dispatch**: in the session timeline, zerops.yaml Write calls and scaffold-subagent dispatches overlap in time (first zerops.yaml Write before the first scaffold-subagent return).

**Failure-mode triage**:

- Fails 1–2 → v8.94 content-brief steering failure. Diagnose the subagent's classification step; tighten brief wording before a v8.95 follow-up.
- Fails 3–6 → editorial lapse. Consider adding an editorial-pass subagent in v8.95.
- Fails 7 → dispatch pattern not followed. Main agent absorbed content work (v22-class regression). Check whether the substep-brief delivery mechanism is working end-to-end.
- Fails 10 or 11 → workspace manifest not being used. Most likely cause: subagent briefs don't reference the manifest clearly, or the main agent is forgetting to update it after scaffold returns.
- Fails 13 → agent defaulting to serial deploys despite the new batch tool. Check whether the deploy-step prompt actually instructs batch usage.
- Fails 14 → concurrency prompt not steering hard enough. Bolder wording in recipe.md generate section.

---

## 9. Rollout sequence

Nine phases total — five on the content-authoring track (1C–5C), four on the efficiency track (1E–4E). The tracks are independent; you can interleave or run in parallel. Listed priority order is risk-ascending: each phase's blast radius is larger than the prior one. Stop and run the test suite + a full recipe between phases — don't accumulate unverified changes.

**Content-authoring track**:

1. **Phase 1C — spec + shadow-check fix** (lowest risk). Ship `spec-content-surfaces.md` (done as of this implementation). Ship the `env_self_shadow` enumeration fix with its RED/GREEN tests (§5.5). No behavior change elsewhere. Full test suite + one recipe run to confirm the shadow check fires for workers.
2. **Phase 2C — content-authoring subagent brief + topic**. Replace recipe.md readmes-substep brief (§5.1, §6). Register new guidance topic or remap (§5.6). Run a recipe to verify the new brief reaches the dispatched subagent.
3. **Phase 3C — mandatory-fact guidance**. Add the "fact recording mandatory" block to deploy step-entry and feature-subagent brief (§5.2).
4. **Phase 4C — env README expansion**. Replace the 7-line boilerplate template with the 40–80-line structured template + the generate-finalize path that consumes the subagent's env-expansion artifact (§5.3).
5. **Phase 5C — scaffold pre-flight self-verification**. Add the assertion block to scaffold-subagent-brief (§5.4).

**Efficiency track**:

6. **Phase 1E — main/scaffold concurrency prompt** (zero code, lowest risk). Add the concurrency section to recipe.md generate step (§5.10). Run a recipe to confirm the agent actually starts zerops.yaml writes in parallel.
7. **Phase 2E — workspace state manifest**. Implement `ops/workspace_manifest.go` + `tools/workspace_manifest.go` + register. Update all subagent briefs (scaffold, feature, code-review, content-authoring) to read/write (§5.8). Run a recipe; measure subagent Read reduction.
8. **Phase 3E — `zerops_deploy_batch`**. Ship the tool (§5.9) + update recipe.md deploy-step prompt to use batch. Run a recipe and measure `zerops_deploy_batch` usage across clusters.

Between phases, run the full test suite (`go test ./... -count=1 -race`) and `make lint-local`. Between Phase 2C and Phase 3C, run an actual recipe end-to-end to confirm the new brief works under realistic conditions before layering on the mandatory-fact guidance. Phase 3E is the highest-risk mechanical change because it alters the tool surface the deploy-step prompt depends on — land it last, with a feature flag if possible so the old serial-deploy path remains functional for a rollback window.

---

## 10. Open questions for the implementer

### Content-authoring questions

1. **Single dispatch vs two dispatches?** The current plan dispatches the content-authoring subagent once at `readmes` substep, and that subagent produces an `env-comment-set` artifact that the main agent applies at finalize. Alternative: dispatch the same subagent a second time at finalize for the env-side work. Trade-off: single dispatch is cheaper (one context load) but means env content is authored 20+ min before the main agent applies it — the subagent hasn't yet seen the finalize-time env comment constraints. Two dispatches are more expensive but each dispatch sees the state relevant to its task. **Recommendation**: start with single-dispatch + artifact; revisit if env content quality lags.

2. **Sub-agent brief length**. The full brief in §6 is ~400 lines. This lands well over the substep-complete response-size budgets v8.83/v8.84 tuned for. Options: (a) inline in recipe.md (risks 350-line file cap), (b) separate `.md` template via `go:embed`, (c) compressed brief in-band + a `zerops_guidance topic=content-authoring-full` the subagent can call to expand. **Recommendation**: option (b) — `go:embed` a separate template. Clean separation between workflow prose and subagent brief.

3. **Should the self-review answers be machine-checked?** The brief asks the subagent to write its test answers in its completion message. Currently this is narrative-only; no parser validates. Adding a structural check ("each item has a test-answer line") is tempting but returns us to the check-on-content path. **Recommendation**: don't add a check. Treat the self-review as a thinking aid, not a gate. If v29 shows the subagent skipping the self-review, address via brief-wording in v8.95.

### Efficiency questions

4. **Where does the workspace manifest live — `/tmp` or `.zcp/`?** `ops.FactLogPath` uses `/tmp`. The argument for `/tmp`: matches existing convention, zero configuration, auto-cleans on reboot. The argument for `.zcp/`: survives system reboot, can be inspected after a run for debugging, version-controlled if in a repo context. **Recommendation**: `/tmp` to match `facts_log.go`; revisit if post-run forensics proves useful.

5. **Should subagents write to the workspace manifest directly, or only via the main agent?** Subagents writing directly is simpler (fewer round-trips). Main-agent-only keeps the manifest authoritative and prevents race conditions. v8.90 established a "workflow state is main-agent-only" policy — applying that principle, the manifest should follow the same rule. **Recommendation**: main-only writes; subagents return structured data in their completion message, main calls `workspace_manifest action=update` on receipt. Yes this means an extra tool call per subagent return, but (a) it's one call of negligible cost, (b) it's consistent with the state-ownership discipline v8.90 established.

6. **`zerops_deploy_batch` — is there a max batch size?** Zerops's build queue has concurrency limits per project. Batching 3 builds is routine; batching 10 is probably not. **Recommendation**: soft limit of 5 in the tool handler. If input has >5 targets, the tool should fall back to sequential internally OR return an error suggesting smaller batches. Document the limit in the tool's jsonschema description.

---

## 11. What a fresh instance should do first

If you just landed on this doc:

1. Read [docs/spec-content-surfaces.md](spec-content-surfaces.md) fully — this is THE content-quality reference.
2. Skim [docs/recipe-version-log.md](recipe-version-log.md) §v28 — understand both the content-quality symptom AND the workflow-efficiency baseline (wall time, tool-call mix, subagent shape).
3. Read [internal/tools/record_fact.go](../internal/tools/record_fact.go) + [internal/ops/facts_log.go](../internal/ops/facts_log.go) — understand the existing facts-log infrastructure. Your workspace manifest (§5.8) follows the same pattern.
4. Read [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md) §`Per-codebase README with extract fragments` (search for "post-deploy `readmes` sub-step") and the scaffold-subagent-brief block — understand what's being replaced and extended.
5. Read [internal/workflow/recipe_topic_registry.go](../internal/workflow/recipe_topic_registry.go) + [internal/workflow/recipe_substeps.go](../internal/workflow/recipe_substeps.go) — understand the substep-to-topic mapping.
6. Skim [internal/platform/deployer.go](../internal/platform/deployer.go) + [internal/ops/deploy.go](../internal/ops/deploy.go) — understand the deploy tool's current shape so you can design `zerops_deploy_batch` consistent with it.
7. Write the RED tests from §5.5 (shadow enumeration) first — watch them fail.
8. Implement **Phase 1C** (the shadow-enumeration fix) and **Phase 1E** (error hints — purely additive, lowest risk) to validate your TDD flow. Both can land in the same PR.
9. Implement **Phase 2C** (content-authoring subagent brief + topic) — the load-bearing content change — alongside **Phase 2E** (concurrency prompt — zero code). Run a recipe to validate both.
10. Layer Phases 3C–5C and 3E–4E on top. The order within each track is risk-ascending. Cross-track order is flexible — content and efficiency tracks don't share code paths.
11. Run a full recipe to validate — `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v29/` will be the test. Measure against §8 acceptance criteria (15 bars, content 1–9 + efficiency 10–15).

Full test command before shipping: `go test ./... -count=1 -race && make lint-local`.

---

## 12. Summary

Two goals in one release, pursued via independent tracks:

**Content authoring**: Stop letting the agent that debugged for 85 minutes write the reader-facing content. Dispatch a fresh-context sub-agent at `deploy.readmes` with the structured facts log, platform guides, and surface contracts — classify every fact before routing, cite `zerops_knowledge` for every topic it already documents, and write every surface (including substantive Env READMEs) from the reader's perspective, not the author's.

**Workflow efficiency**: Stop paying framework-agnostic inefficiency costs on every recipe run. Give subagents a workspace manifest so they skip re-crawling. Batch parallel deploys server-side so MCP STDIO serialization doesn't cost 8 min per run. Run zerops.yaml writes in parallel with scaffold dispatch so main-agent idle time disappears. And self-verify scaffold output with runnable assertions so recurrent runtime incidents stop being born.

Expected v29: recipe wall 60–70 min (v28: 85 min), ≥80% genuine gotchas, 0 folk-doctrine defects, 0 cross-surface duplication, env READMEs substantive for the first time in the log. **A− overall rating** by the S/C/O/W rubric.

Neither track encodes NestJS, Svelte, Vite, or any other framework-specific knowledge in workflow logic. The content-authoring taxonomy is universal. The workspace manifest schema is framework-neutral. The deploy-batch tool operates on service hostnames. Pre-flight assertions use framework-specific greps but the mechanism (self-verify before return) generalizes. This release shifts the recipe workflow from "optimized for this NestJS showcase" toward "generally correct for any platform recipe."
