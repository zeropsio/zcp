# Simulation Prompt — Progressive Guidance Delivery Audit

Paste this into the **same session** as the implementing agent, AFTER all three phases are implemented and all tests pass.

---

## Context

You just implemented the progressive guidance delivery system per `docs/spec-progressive-guidance.md`. Now you must verify it works from the agent's perspective — not just that tests pass, but that the EXPERIENCE is correct, complete, smooth, and first-principles-based across all 4 canonical shapes.

Read these files to ground yourself:
- `docs/spec-progressive-guidance.md` — the spec you implemented (you know it, but re-read the invariants)
- `internal/workflow/recipe_topic_registry.go` — the topic → block mappings you built
- `internal/workflow/recipe_section_catalog.go` — the block catalogs (now including finalize + close)
- `internal/workflow/recipe_plan_predicates.go` — the predicate functions
- `internal/workflow/recipe_guidance.go` — skeleton composition + topic resolution
- `internal/workflow/recipe_guidance_test.go` lines 30–110 — the 4 shape fixtures
- `internal/content/workflows/recipe.md` — the skeletons + blocks

## Session isolation — CRITICAL

**Do NOT call any `mcp__zerops__*` tools or `zerops_workflow`.** The ZCP server has a single session. Multiple agents calling it would corrupt shared state. This audit is STATIC ANALYSIS of the guidance content.

**How to get the outputs**: Write a temporary Go test file (`internal/workflow/recipe_progressive_simulation_test.go`) that exercises both the skeleton AND every topic the agent would fetch.

Pattern:

```go
func TestProgressiveSimulation(t *testing.T) {
    plan := fixtureForShape(ShapeHelloWorld) // vary per agent
    steps := []string{
        RecipeStepResearch, RecipeStepProvision, RecipeStepGenerate,
        RecipeStepDeploy, RecipeStepFinalize, RecipeStepClose,
    }

    for _, step := range steps {
        // 1. The skeleton the agent receives at step start
        skeleton := resolveRecipeGuidance(step, string(plan.Tier), plan)
        t.Logf("\n===== %s SKELETON (%d B, %.1f KB) =====\n%s",
            step, len(skeleton), float64(len(skeleton))/1024, skeleton)

        // 2. Every topic referenced in the skeleton, fetched on-demand
        //    (parse [topic: xxx] markers from the skeleton)
        topics := extractTopicReferences(skeleton) // you'll need this helper
        for _, topicID := range topics {
            content, err := ResolveTopic(topicID, step, plan)
            if err != nil {
                t.Logf("  topic %q: ERROR: %v", topicID, err)
                continue
            }
            if content == "" {
                t.Logf("  topic %q: (predicate filtered — does not apply to this shape)", topicID)
                continue
            }
            t.Logf("\n  ----- topic: %s (%d B, %.1f KB) -----\n%s",
                topicID, len(content), float64(len(content))/1024, content)
        }
    }
}

// extractTopicReferences parses [topic: xxx] markers from text
func extractTopicReferences(text string) []string {
    re := regexp.MustCompile(`\[topic:\s*(\S+?)\]`)
    matches := re.FindAllStringSubmatch(text, -1)
    seen := map[string]bool{}
    var result []string
    for _, m := range matches {
        id := m[1]
        if !seen[id] {
            seen[id] = true
            result = append(result, id)
        }
    }
    return result
}
```

Run: `go test ./internal/workflow/ -run TestProgressiveSimulation -v -count=1 2>&1 | tee /tmp/sim_output.txt`

Then read the output file to analyze. **Delete the test file when done.**

## What to do

Dispatch **4 parallel agents**, one per shape. Each agent:

1. Writes the temporary test with their shape, runs it, reads the ACTUAL output
2. Walks through the entire recipe flow as the agent would experience it — skeleton first, then topic fetches in the order they'd naturally happen
3. Flags every issue in a structured report

### Audit dimensions (apply to EVERY agent, EVERY step)

#### A. Skeleton quality
- **Execution clarity**: Can the agent follow the skeleton without any topic fetches? The skeleton must be independently actionable as a minimum viable guide.
- **Topic placement**: Are `[topic: xxx]` references at the right points in the execution flow? If the agent fetches a topic too early or too late relative to the sub-task, it's a timing error.
- **Predicate filtering in skeleton**: For this shape, are irrelevant topic references removed? (e.g., `[topic: dual-runtime-urls]` must not appear in hello-world skeleton). If a topic reference appears but `ResolveTopic` returns empty, the skeleton has a filtering bug.
- **Size**: Skeleton must be under 5 KB for every shape at every step. Log the exact bytes.

#### B. Topic content quality
- **Self-containedness**: Can the agent act on the topic content without fetching another topic first? Exception: compound topics that explicitly reference related topics.
- **Completeness**: Does the topic contain everything the agent needs for that sub-task? Anything missing that the agent would need to guess?
- **No dead references**: If the topic references content from another step (e.g., "see deploy Step 4c"), verify that the referenced content is reachable via a topic fetch, not buried in a skeleton the agent already scrolled past.
- **Deduplication**: The spec says "where commands run", browser walk rules, and comment style each appear once. Verify: no topic duplicates content from another topic. If two topics overlap, flag it.
- **Size**: Each topic must be under 5 KB. Log exact bytes.

#### C. Shape correctness
- **Predicate accuracy**: For every topic that fires (or doesn't fire), verify the predicate is correct for this shape.
- **Content relevance**: Every sentence in every emitted topic must be relevant to this shape. Flag content that's shape-irrelevant but not filtered out.
- **Cross-step consistency**: A rule taught in one step's topic must not contradict a rule in another step's topic.

#### D. First principles
- **No framework/runtime hardcoding**: Every instruction must be structural — derived from plan data (package manager, build commands, port, target types), never mentioning a specific framework/runtime by name.
- **No hardcoded hostnames or ports**: Instructions must use `{hostname}dev`, `{httpPort}`, etc., not `apidev`, `3000`, etc.
- **Agent autonomy**: The guidance tells the agent WHAT to do and WHY, not a framework-specific HOW. The agent fills the HOW from its training data + chain recipe.

#### E. Progressive flow simulation
Walk through the entire recipe as the agent would experience it. At each sub-task:
1. Read the skeleton entry for this sub-task
2. Fetch the referenced topic
3. Simulate: "Do I have everything I need to act?" If not, what's missing?
4. Simulate: "After I complete this sub-task, does the next skeleton entry make sense?"
5. For showcase shapes: simulate the sub-agent experience. The sub-agent receives a brief from a topic — is the brief self-contained?

#### F. Sub-step validation (Phase B)
If Phase B sub-step orchestration is implemented:
- Walk through the sub-step sequence for generate and deploy
- At each sub-step boundary: what does the validator check? Is the check sufficient? Too strict? Too lax?
- After a validation failure: does the returned guidance actually help fix the issue?
- Simulate a common failure (e.g., comment ratio 25%, missing setup:worker) and trace the agent's recovery path

#### G. Adaptive guidance (Phase C)
If Phase C adaptive guidance is implemented:
- Simulate: agent fetches `zerops-yaml-rules` for a dual-runtime plan. Does `dual-runtime-urls` auto-expand?
- Simulate: agent fails zerops-yaml validation on comment ratio. Does the retry delta mention comment ratio specifically, and NOT mention unrelated issues?
- Verify: an agent that fetches ALL topics (belt-and-suspenders approach) gets no duplicated content

---

### Agent 1: Hello-World shape

```
Shape: ShapeHelloWorld (nodejs-hello-world)
Plan: tier=hello-world, 2 targets (app: nodejs@22, db: postgresql@17)
Predicates: hasManagedServiceCatalog only
Characteristic: SIMPLEST. The skeleton must be lean. Topic fetches should be minimal (3-5 per step max).
```

Shape-specific checks:
- Generate skeleton: no showcase, no dual-runtime, no worker references. Topics: `where-to-write`, `zerops-yaml-rules`, `readme-fragments`, `smoke-test` at most. `fragment-quality` should NOT appear (hello-world slug → `planNeedsFragmentsDeepDive` returns false).
- Deploy skeleton: no subagent, no browser walk. Just deploy → start → verify → stage.
- Finalize: `project-env-vars` topic must NOT appear (not dual-runtime). `showcase-service-keys` must NOT appear.
- Close: `close-browser-walk` must NOT appear. Export section should be minimal (1 codebase).
- Total topic fetches across all steps: should be under 15. If more, the skeleton is over-referencing.
- The agent should be able to complete this recipe fetching ~30-40 KB total (down from 72 KB monolithic).

### Agent 2: Backend-Minimal shape (implicit webserver)

```
Shape: ShapeBackendMinimal (laravel-minimal)
Plan: tier=minimal, 2 targets (app: php-nginx@8.3, db: postgresql@17)
Predicates: hasManagedServiceCatalog only
Characteristic: Same predicates as hello-world but different tier. Implicit webserver. Multi-base detection edge case.
```

Shape-specific checks:
- Generate skeleton: nearly identical to hello-world. But `fragment-quality` SHOULD appear (not a hello-world slug).
- Smoke test topic: implicit-webserver exception must be clear (skip the start command).
- Deploy topic: Step 2a must say "skip for implicit-webserver runtimes" — not buried in a paragraph.
- `hasBundlerDevServer`: verify it returns FALSE for this plan (php-nginx is not a bundler framework, not dual-runtime, `needsMultiBaseGuidance` is... check this carefully. Does the fixture plan have npm in BuildCommands? If yes → multi-base → `hasBundlerDevServer` returns true. If not → false. Trace the actual fixture.)
- `needsMultiBaseGuidance`: check the fixture. Does `laravel-minimal` have JS build commands? The minimal fixture should NOT have them (no Vite, no asset pipeline). Verify.

### Agent 3: Full-Stack Showcase (shared worker)

```
Shape: ShapeFullStackShowcase (laravel-showcase)
Plan: tier=showcase, 7 targets, shared-codebase worker
Predicates: isShowcase, hasWorker, hasSharedCodebaseWorker, hasManagedServiceCatalog, needsMultiBaseGuidance, hasBundlerDevServer
NOT: isDualRuntime, hasMultipleCodebases, hasSeparateCodebaseWorker, hasServeOnlyProd
Characteristic: Single mount (shared worker = 1 repo). Multi-base (PHP+Node). Implicit webserver + asset dev server.
```

Shape-specific checks:
- `where-to-write` topic: must resolve to the SINGLE-codebase variant (hasMultipleCodebases is false for shared worker)
- `worker-setup` topic: shared-codebase pattern — 3 setups in 1 zerops.yaml. Verify the content explains this clearly.
- `multi-base-dev` topic: fires (needsMultiBaseGuidance is true). Content must explain `zsc install nodejs@22` in dev.
- `dev-server-hostcheck` topic: fires (hasBundlerDevServer is true via needsMultiBaseGuidance). Content must be about Vite/webpack host-check config, not framework-specific.
- `dashboard-skeleton` topic: fires (isShowcase). Content is generic (not Laravel-specific).
- Deploy skeleton: has subagent and browser walk references. NO API-first interleaving.
- Deploy `subagent-brief` topic: references `where-commands-run` topic by ID (deduplication). Verify the reference works — the agent can fetch it.
- Finalize: `showcase-service-keys` fires. Content describes shared-codebase key list (no `workerdev` in envs 0-1).
- Finalize: `project-env-vars` does NOT fire (not dual-runtime).

### Agent 4: Dual-Runtime Showcase (separate worker)

```
Shape: ShapeDualRuntimeShowcase (nestjs-showcase)
Plan: tier=showcase, 11 targets, separate-codebase worker
Predicates: ALL of them fire except hasSharedCodebaseWorker
Characteristic: WIDEST. 3 repos. Serve-only prod. API-first deploy. This is the v8 shape.
```

Shape-specific checks:
- Generate skeleton: maximum topic references. Verify every topic that fires is referenced in the skeleton.
- `dual-runtime-urls` topic: complete URL pattern with env 0-1 vs 2-5 shapes. YAML examples.
- `serve-only-dev` topic: teaches dev type override WITHOUT `run.os`.
- `where-to-write` topic: multi-codebase variant. Describes 3-mount layout.
- Deploy skeleton: API-first interleaving order. All steps present.
- Deploy `deploy-flow` topic (or compound): must include `deploy-api-first` sub-block. Verify the API-first interleaving (Step 1-API → 2a-API → 3-API → Step 1 → ...) is in the content.
- Deploy `deploy-worker-process` sub-block: separate-codebase shape (own container, own redeploy).
- Finalize: BOTH `showcase-service-keys` AND `project-env-vars` fire. Verify both are referenced in skeleton and content is correct.
- Close: `close-browser-walk` fires. Content references deploy's browser walk topic (deduplication). Verify the reference resolves.
- Close: export section describes 3-repo publish (app + api + worker).

---

## Cross-agent synthesis

After all 4 agents complete, synthesize their reports into `docs/simulation-progressive-findings.md`. Structure:

### 1. Size comparison table

| Shape | Step | Old monolithic | New skeleton | Topics fetched | Total if all fetched | Reduction |
|-------|------|---------------|-------------|----------------|---------------------|-----------|
| hello-world | generate | 19.4 KB | ? KB | N topics | ? KB | ?% |
| ... | ... | ... | ... | ... | ... | ... |

### 2. Skeleton quality scorecard

For each shape × step: is the skeleton independently actionable? Grade: PASS (agent can act), PARTIAL (agent can act but may miss details), FAIL (agent cannot act without topic fetch).

The target: PARTIAL for all complex steps (generate, deploy). The skeleton gives the execution order and constraints; topics fill the details. FAIL is a bug — the skeleton must be a standalone minimum viable guide.

### 3. Topic coverage matrix

| Topic ID | hello-world | backend-minimal | fullstack-showcase | dual-runtime | Notes |
|----------|-------------|-----------------|-------------------|--------------|-------|
| where-to-write | single | single | single | multi | |
| zerops-yaml-rules | fires | fires | fires | fires | compound topic |
| dual-runtime-urls | — | — | — | fires | |
| ... | ... | ... | ... | ... | |

Verify: no topic fires for a shape where its predicate should be false. No topic is missing for a shape where its predicate should be true.

### 4. Deduplication audit

List every concept that the old system duplicated. For each:
- Where does it live now? (which topic?)
- Where is it referenced from? (which other topics or skeletons?)
- Is the reference clear enough for the agent to follow?

### 5. Bug list

Same format as `docs/simulation-v8-findings.md`: BUG N, affects, location, problem, impact, fix.

### 6. First principles check

- Framework leakage: any framework/runtime name hardcoded in a block?
- Hostname leakage: any literal hostname in guidance?
- Port leakage: any literal port number?

### 7. Agent experience assessment

For each shape, describe the agent's experience in 3-4 sentences:
- How many tool calls does the agent make per step?
- What's the largest single piece of content the agent holds at once?
- Where is the flow smoothest? Where is it roughest?
- Overall: is this better, same, or worse than the monolithic guide for this shape?

### 8. Invariant verification

The spec defines 7 invariants. For each, state PASS or FAIL with evidence:
1. No framework/runtime hardcoding
2. Predicate parity (topic ↔ block)
3. Monotonicity (narrow ≤ wide)
4. Self-containedness (each topic independently actionable)
5. Backward compatibility (skeleton alone is sufficient)
6. No content loss (every old sentence reachable)
7. Agent perspective (tool is optional enhancement, not requirement)

---

## Success criteria

The simulation passes if:
1. Zero BLOCKERs or BUGs
2. All 7 invariants PASS
3. Skeleton is independently actionable (PARTIAL or PASS) for every shape × step
4. No framework/runtime/hostname/port leakage
5. Every topic that should fire for a shape DOES fire, and vice versa
6. The agent experience assessment is "better than monolithic" for all 4 shapes
7. Total guidance (skeleton + all topic fetches) is smaller than the old monolithic guide for at least the 2 narrowest shapes (hello-world, backend-minimal)

If any of these fail, fix the implementation before declaring Phase A/B/C complete.
