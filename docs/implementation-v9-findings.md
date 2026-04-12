# Implementation Guide: v9 Session Findings

**Source**: NestJS showcase v9 session analysis (2026-04-12)
**Scope**: 5 structural issues found in progressive guidance delivery system
**Principle**: Fix at root cause, no framework/runtime hardcoding, structural over patch

---

## Issue Map

| # | Issue | Root Cause | Severity | Files |
|---|-------|-----------|----------|-------|
| 1 | Agent used `# ===` separators despite fetching guidance | `comment-style` topic only in Finalize, not reachable from Generate | HIGH |  recipe_topic_registry.go, recipe.md |
| 2 | Duplicate APP_SECRET in finalize | Template silently emits duplicate — no dedup, no validation | HIGH | recipe_templates_import.go, workflow_checks_finalize.go |
| 3 | 3 topics exceed 5 KB cap | Monolithic blocks mixing procedure + reference | MEDIUM | recipe.md, recipe_topic_registry.go |
| 4 | Duplicate `zerops_knowledge` calls | Cancelled parallel calls retried — no session-level cache | LOW | knowledge.go |
| 5 | Subagent peer-dependency conflict | Smoke-test mentions it but subagent brief doesn't | LOW | recipe.md |

---

## Issue 1: Comment Style Unreachable at Generate

### Problem

The agent fetched `zerops-yaml-rules` and `code-quality` during generate. Neither mentions
comment formatting anti-patterns (heading separators like `# ===`). The `comment-style` topic
that forbids them lives in RecipeStepFinalize — the agent only reaches it 20+ minutes later,
long after zerops.yaml is written.

The `zerops_yaml_comment_headings` check then fails, forcing a retry.

### Root Cause

Comment formatting rules are split across steps: _what_ to comment is at Generate
(`code-quality` says ratio >= 30%, WHY-not-WHAT), _how_ to format comments is at Finalize
(`comment-style` says no separators). But zerops.yaml is written at Generate, so the
formatting rules arrive too late.

### Fix

**Move the anti-pattern rules to where zerops.yaml is written — Generate.**

The `comment-style` block in recipe.md contains two concerns:
1. **Anti-patterns** (no `# ===`, no `# -- Section --`, no decorator comments) — needed at Generate
2. **Voice/tone** (dev-to-dev, WHY+HOW, 2-3 sentences) — needed at Finalize for env comments

Split the block into two blocks in recipe.md:

```
<block:comment-anti-patterns>
... anti-pattern rules (currently lines ~1617-1627 of comment-style block) ...
</block:comment-anti-patterns>

<block:comment-voice>
... voice/tone rules (currently the rest of comment-style block) ...
</block:comment-voice>
```

Then update the topic registry:

```go
// In recipeGenerateTopics — add new topic
{
    ID: "comment-anti-patterns", Step: RecipeStepGenerate,
    Description: "Comment formatting anti-patterns (separators, decorators)",
    BlockNames:  []string{"comment-anti-patterns"},
},

// In recipeFinalizeTopics — update existing topic
{
    ID: "comment-style", Step: RecipeStepFinalize,
    Description: "Writing style reference for env comments",
    BlockNames:  []string{"comment-voice"},
},
```

Add an expansion rule so `zerops-yaml-rules` suggests `comment-anti-patterns`:

```go
"zerops-yaml-rules": {
    {TopicID: "dual-runtime-urls", Predicate: isDualRuntime},
    {TopicID: "worker-setup", Predicate: hasWorker},
    {TopicID: "comment-anti-patterns"},  // NEW
},
```

Add a `[topic: comment-anti-patterns]` marker in the generate skeleton after the
zerops-yaml-rules line:

```
2. Write zerops.yaml — YOU, not a sub-agent [topic: zerops-yaml-rules]
   ...
   - Comment formatting rules [topic: comment-anti-patterns]
```

### Verification

- `TestTopicRegistry_PredicateParity` still passes (new topic has no predicate)
- `TestTopicRegistry_AllTopicBlocksExist` passes (new block name exists in recipe.md)
- `TestRecipe_DetailedGuide_MonotonicityInvariant` passes (narrow shapes still subset of wide)
- Manual: agent fetching `zerops-yaml-rules` at Generate now sees comment-anti-patterns
  in the expansion suggestion

### Files Changed

| File | Change |
|------|--------|
| `internal/content/workflows/recipe.md` | Split `comment-style` block into `comment-anti-patterns` + `comment-voice`. Add skeleton marker. |
| `internal/workflow/recipe_topic_registry.go` | Add `comment-anti-patterns` topic to Generate. Update `comment-style` to use `comment-voice` block. Add expansion rule. |

---

## Issue 2: Duplicate APP_SECRET in Finalize

### Problem

The template (`writeProjectSection`) always emits `APP_SECRET: <@generateRandomString(<32>)>`
when `plan.Research.NeedsAppSecret` is true (line 108-109 of recipe_templates_import.go). If
the agent also passes `APP_SECRET` in `projectEnvVariables`, it's emitted twice. The finalize
validation parses into `map[string]string` which silently deduplicates — the duplicate is never
detected or flagged.

The v9 agent did this because the `project-env-vars` topic shows examples with URL constants
only but never explicitly says "do not include the app secret key".

### Root Cause

Two independent problems:

1. **No deduplication in the template writer** — `writeProjectSection` emits the secret then
   emits all projectEnvVariables keys without checking for overlap.
2. **No validation for duplicate YAML keys** — the finalize checker parses into a Go map,
   which silently drops duplicates.

The guidance gap is a symptom. The structural fix is: **make it impossible to produce
duplicate keys**, then the guidance doesn't need to warn about it.

### Fix

**Fix A — Dedup in writeProjectSection (primary fix)**

In `writeProjectSection` (recipe_templates_import.go:88-123), skip the app secret key
from projectEnvVariables if it's already emitted by the template:

```go
// After line 109 (hasSecret emitted), when iterating envVars:
for _, name := range names {
    if hasSecret && name == plan.Research.AppSecretKey {
        continue // already emitted by template
    }
    fmt.Fprintf(b, "    %s: %s\n", name, envVars[name])
}
```

This is a 3-line change. The template is the single source of truth for the secret —
if the agent redundantly passes it, it's silently ignored rather than creating invalid YAML.

**Fix B — Duplicate key validation (defense in depth)**

In `validateImportYAML` (workflow_checks_finalize.go), add a raw YAML duplicate key
check before parsing into the Go struct. Scan for repeated keys under `envVariables:`:

```go
// In validateImportYAML, after reading the file but before parsing:
if dupes := findDuplicateYAMLKeys(raw); len(dupes) > 0 {
    issues = append(issues, fmt.Sprintf("duplicate YAML keys: %s", strings.Join(dupes, ", ")))
}
```

The `findDuplicateYAMLKeys` function scans lines for repeated `key:` patterns at the
same indentation level. This catches all duplicate keys, not just APP_SECRET — it's
structural, not special-cased.

### Verification

- Write a test case in `recipe_templates_import_test.go`: plan with NeedsAppSecret=true
  and projectEnvVariables containing the same key. Assert output has exactly one occurrence.
- Write a test case in `workflow_checks_finalize_test.go`: import.yaml with duplicate
  `envVariables` keys. Assert validation fails with specific issue.

### Files Changed

| File | Change |
|------|--------|
| `internal/workflow/recipe_templates_import.go` | Skip app secret key from projectEnvVariables loop |
| `internal/tools/workflow_checks_finalize.go` | Add `findDuplicateYAMLKeys` check |
| `internal/workflow/recipe_templates_import_test.go` | Test: duplicate key dedup |
| `internal/tools/workflow_checks_finalize_test.go` | Test: duplicate key validation |

---

## Issue 3: Oversized Topics (browser-walk 9.5 KB, env-comments 7.7 KB, deploy-flow 7.5 KB)

### Problem

Three topics exceed the 5 KB progressive delivery target. A 9.5 KB `browser-walk` fetch
undermines the benefit of delivering guidance in focused chunks.

### Root Cause

Each oversized block mixes **procedural guidance** (what to do) with **reference material**
(command tables, example templates, execution order tables). The reference material is useful
but shouldn't be inlined with the procedure — the agent needs the procedure to know what to
do, and consults the reference only when stuck.

### Fix

Split each block along the **procedure vs. reference** boundary. Extract reference material
into sub-topic blocks, keep the procedure in the main block, and wire them via
`topicExpansionRules` so the agent discovers them naturally.

#### 3a: browser-walk (9.5 KB → ~4 KB + 2 sub-topics)

The `dev-deploy-browser-walk` block has 4 sections:
1. Intro + why browser verification (0.7 KB) — keep
2. Tool rules + non-negotiables (1.5 KB) — keep (core procedure)
3. Command vocabulary table (1.6 KB) — **extract** → `browser-commands`
4. Three-phase canonical flow (4 KB) — keep (core procedure, includes code examples)

**After extraction**: main block ~6.2 KB (still over 5 KB). Extract the "What to avoid"
bullet list at the end (~1.2 KB) into the command reference too → main block ~5 KB.

In recipe.md:
```
<block:dev-deploy-browser-walk>
... sections 1, 2, 4 minus "What to avoid" ...
</block:dev-deploy-browser-walk>

<block:browser-command-reference>
... section 3 (command vocabulary table) + "What to avoid" bullets ...
</block:browser-command-reference>
```

In topic registry — add sub-topic:
```go
{
    ID: "browser-commands", Step: RecipeStepDeploy,
    Description: "Browser tool command vocabulary and anti-patterns",
    Predicate:   isShowcase,
    BlockNames:  []string{"browser-command-reference"},
},
```

In expansion rules:
```go
"browser-walk": {
    {TopicID: "browser-commands", Predicate: isShowcase},
},
```

#### 3b: env-comments (7.7 KB → ~3 KB + 1 sub-topic)

The `env-comment-rules` block has:
1. Environment distinctions + service key rules (1.2 KB) — keep
2. Full YAML example with all 6 envs (3 KB) — **extract** → `env-comments-example`
3. Coverage requirements + style (2.4 KB) — keep

In recipe.md:
```
<block:env-comment-rules>
... sections 1 + 3 (distinctions, service keys, coverage, style) ...
</block:env-comment-rules>

<block:env-comments-example>
... section 2 (full YAML example with all 6 environments) ...
</block:env-comments-example>
```

In topic registry — add sub-topic:
```go
{
    ID: "env-comments-example", Step: RecipeStepFinalize,
    Description: "Complete env comment YAML example template",
    BlockNames:  []string{"env-comments-example"},
},
```

In expansion rules:
```go
"env-comments": {
    {TopicID: "env-comments-example"},
},
```

#### 3c: deploy-flow (7.5 KB → ~5.5 KB + 1 sub-topic)

The `deploy-core-universal` block has:
1. Execution order table (0.6 KB) — **extract** → `deploy-execution-order`
2. Steps 1-4 (5.3 KB) — keep (tightly coupled procedure)

In recipe.md:
```
<block:deploy-execution-order>
... execution order table + parameter naming clarification ...
</block:deploy-execution-order>

<block:deploy-core-universal>
... Steps 1-4 only ...
</block:deploy-core-universal>
```

In topic registry — add sub-topic:
```go
{
    ID: "deploy-execution-order", Step: RecipeStepDeploy,
    Description: "Deploy step execution order by recipe type",
    BlockNames:  []string{"deploy-execution-order"},
},
```

In expansion rules:
```go
"deploy-flow": {
    {TopicID: "subagent-brief", Predicate: isShowcase},
    {TopicID: "deploy-execution-order"},  // NEW
},
```

### Size Budget After Splits

| Topic | Before | After | Sub-topics |
|-------|--------|-------|------------|
| browser-walk | 9.5 KB | ~5 KB | + browser-commands (~3.5 KB) |
| env-comments | 7.7 KB | ~3.6 KB | + env-comments-example (~3 KB) |
| deploy-flow | 7.5 KB | ~5.7 KB | + deploy-execution-order (~0.9 KB) |

deploy-flow is still slightly over 5 KB — Steps 1-4 are tightly coupled and splitting
them would break the procedure's coherence. This is acceptable: the content is all procedure,
no reference material left to extract.

### Verification

- `TestTopicRegistry_AllTopicBlocksExist` passes (new block names exist in recipe.md)
- `TestTopicRegistry_PredicateParity` passes (new topics inherit parent predicates where applicable)
- Measure new block sizes: `grep -A9999 'block:...' recipe.md | ...` — all main blocks <= 6 KB
- Expansion rules: verify `ExpandTopic("browser-walk", showcasePlan, empty)` returns `browser-commands`

### Files Changed

| File | Change |
|------|--------|
| `internal/content/workflows/recipe.md` | Split 3 blocks into 6. Move reference material to new blocks. |
| `internal/workflow/recipe_topic_registry.go` | Add 3 sub-topics (browser-commands, env-comments-example, deploy-execution-order). Add 3 expansion rules. |

---

## Issue 4: Duplicate `zerops_knowledge` Calls

### Problem

The main agent called 4 identical `zerops_knowledge` queries (connection patterns for
valkey, S3, nats, meilisearch), then called them again 11 seconds later. Root cause:
the first batch was cancelled when a parallel Bash call (SSH to kill a process) failed
with exit code 255. Claude Code cancels all parallel tool calls on any failure.

The agent then retried the knowledge calls. No deduplication or caching exists.

### Root Cause

This is a Claude Code runtime behavior (parallel call cancellation), not a ZCP bug.
The agent correctly retried. The real issue is that there's no session-level cache in
`zerops_knowledge`, so cancelled-then-retried calls hit the search index twice.

### Fix

Add a lightweight session-level result cache to `zerops_knowledge`. The knowledge
store content doesn't change within a session, so caching by query string is safe.

In `internal/tools/knowledge.go`, the tool handler should check a session-scoped cache
before calling `store.Search`:

```go
// In the knowledge tool handler:
cacheKey := fmt.Sprintf("%s|%d", input.Query, input.Limit)
if cached, ok := session.KnowledgeCache[cacheKey]; ok {
    return cached, nil
}
result := store.Search(input.Query, input.Limit)
session.KnowledgeCache[cacheKey] = result
```

The cache lives on the session/engine object (not global state) and is cleared when
the session ends. This handles both the cancellation-retry pattern and the case where
the main agent and subagent both query the same patterns (though in v9, the subagent
didn't call `zerops_knowledge` at all).

### Verification

- Test: call `zerops_knowledge` with same query twice in same session → second call
  returns cached result, `store.Search` called once.
- Test: different queries → both hit the store.

### Files Changed

| File | Change |
|------|--------|
| `internal/tools/knowledge.go` | Add session-level cache lookup before `store.Search` |
| `internal/workflow/session.go` or `engine.go` | Add `KnowledgeCache map[string]interface{}` to session state |

---

## Issue 5: Subagent Peer-Dependency Conflict

### Problem

The feature subagent installed `@nestjs/microservices@11` which conflicted with
`@nestjs/core@10`. The `smoke-test` topic mentions catching "peer dependency mismatches"
but the subagent brief doesn't mention version alignment. The subagent runs after the
smoke test, so it doesn't see that guidance.

### Root Cause

The subagent brief (`dev-deploy-subagent-brief` block) focuses on _what_ to implement
and _where commands run_, but doesn't include a dependency hygiene rule. The subagent
operates independently and can introduce dependency conflicts that the main agent's
smoke test already validated against.

### Fix

Add a dependency hygiene paragraph to the `dev-deploy-subagent-brief` block. This
must be framework-agnostic (no "npm" or "pip" specifics):

```markdown
### Dependency hygiene
When adding packages, match the major version of existing dependencies in the same
ecosystem (e.g., if the scaffold has `@nestjs/core@10`, install `@nestjs/*@10`).
Run the project's install command after adding packages to catch peer-dependency
conflicts immediately — don't wait for build.
```

Wait — the example uses `@nestjs/core@10`. That's NestJS-specific. Rewrite structurally:

```markdown
### Dependency hygiene
When adding packages, check the existing lockfile or package manifest for the major
version of the framework's core package. Pin new packages from the same framework
family to the same major version. Run the project's install command after each batch
of package additions to catch peer-dependency conflicts immediately — do not wait for
build.
```

This is framework-agnostic: "framework's core package", "same framework family",
"project's install command" — works for npm, pip, composer, cargo, etc.

### Verification

- Read the updated block: no framework/runtime names mentioned
- Measure block size after addition: should stay within reasonable bounds

### Files Changed

| File | Change |
|------|--------|
| `internal/content/workflows/recipe.md` | Add dependency hygiene paragraph to `dev-deploy-subagent-brief` block |

---

## Execution Order

Issues are independent. Implement in this order for maximum test coverage:

1. **Issue 2** (duplicate key dedup) — pure Go, testable in isolation, highest impact
2. **Issue 1** (comment-style split) — recipe.md + registry, tests exist for parity
3. **Issue 3** (oversized topic splits) — recipe.md + registry, same test coverage
4. **Issue 5** (subagent brief) — recipe.md only, no code changes
5. **Issue 4** (knowledge cache) — requires session state change, lowest priority

Issues 1+3 share the same files and should be done together to avoid merge conflicts
in recipe.md and recipe_topic_registry.go.

---

## Invariants to Verify After All Changes

1. `go test ./internal/workflow/... -count=1` — all existing tests pass
2. `go test ./internal/tools/... -count=1` — all existing tests pass
3. `TestTopicRegistry_AllTopicBlocksExist` — every block name in registry exists in recipe.md
4. `TestTopicRegistry_PredicateParity` — topic fires iff its predicate matches plan shape
5. `TestRecipe_DetailedGuide_MonotonicityInvariant` — narrow shape topics are subset of wide shape topics
6. `make lint-local` — full lint clean
7. No framework/runtime names in any new or modified block text (grep for hardcoded names)
