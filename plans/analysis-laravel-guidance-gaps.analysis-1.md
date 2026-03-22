# Implementation Plan: Recipe Auto-Injection with Mode Optimization

**Date**: 2026-03-22
**Based on**: `plans/analysis-laravel-guidance-gaps.md`
**Goal**: Auto-inject full recipe content into bootstrap guidance so agents get framework-specific knowledge without manual `zerops_knowledge` calls

## Current State

```
assembleKnowledge() at generate step:
  1. GetBriefing(runtime, deps, mode) → runtime guide (php.md) + recipe HINTS + service cards
  2. formatEnvVarsForGuide() → discovered env vars
  3. getCoreSection("zerops.yml Schema") → YAML schema
  4. getCoreSection("Rules & Pitfalls") → platform rules

GetBriefing() internally:
  L3: Runtime guide (php.md content, mode-filtered via filterDeployPatterns)
  L3b: matchingRecipes() → "Available recipes: laravel, symfony, ..." (HINT ONLY)
  L4: Service cards
  L5: Wiring syntax
```

**Problem**: Recipe content (laravel.md = 160 lines) never enters the guidance. Agent must manually call `zerops_knowledge recipe="laravel"`. The intent "Create php-nginx service... with Laravel" contains the framework name but nothing uses it.

## Design: Intent-Based Recipe Detection + Auto-Injection

### Detection Strategy

The `Intent` string (from workflow start) contains the framework name. Use `findMatchingRecipes()` (existing fuzzy search) against the intent to detect the recipe:

```
Intent: "Create php-nginx service for multi-page ZCP documentation site with Laravel"
                                                                            ^^^^^^^ matches recipe "laravel"
```

**Detection chain**:
1. Tokenize intent into words
2. For each word, try `findMatchingRecipes(word)`
3. If exactly 1 match → auto-inject that recipe
4. If multiple matches → inject all (recipes are ~160 lines, context windows are large)
5. If 0 matches → fall back to current hint-only behavior

**Why intent, not runtime type**: Runtime type is `php-nginx@8.4` which maps to 5 recipes (laravel, symfony, nette, filament, twill). Intent disambiguates: "Laravel" → laravel recipe only.

### Injection Points

**Generate step** (primary): Full recipe content including zerops.yml, import.yml, Scaffolding, Configuration, Gotchas, Common Failures.

**Provision step** (secondary): Only import.yml section from recipe — needed for envSecrets guidance BEFORE import.yml is generated.

### Mode Optimization

`GetRecipe(name, mode)` already calls `prependModeAdaptation(mode, runtime)` which prepends:

- **dev/standard**: "This recipe shows production patterns. For dev: deployFiles [.], zsc noop, no healthCheck"
- **simple**: "Use production patterns but keep deployFiles [.]. healthCheck applies as-is."
- **PHP implicit**: "Omit start: and run.ports (webserver built-in)"

This is already correct — no additional mode filtering needed for recipe content.

**Future optimization** (not now): Recipe sections could have mode markers (`<!-- mode:dev -->`, `<!-- mode:simple -->`) to filter irrelevant content. But at ~160 lines per recipe, the full content is fine for current context windows.

## Implementation Steps

### Step 1: Add RecipeName field to GuidanceParams

**File**: `internal/workflow/guidance.go`

```go
type GuidanceParams struct {
    // ... existing fields ...
    RecipeName string // auto-detected from intent, empty = no recipe
}
```

### Step 2: Add intent-based recipe detection

**File**: `internal/workflow/bootstrap_guide_assembly.go`

New function `detectRecipeFromIntent` that tokenizes the intent and searches for matching recipes:

```go
// detectRecipeFromIntent searches the workflow intent string for a matching recipe name.
// Returns the first single-match recipe found, or empty string if none/ambiguous.
func detectRecipeFromIntent(intent string, kp knowledge.Provider) string {
    if intent == "" || kp == nil {
        return ""
    }
    store, ok := kp.(*knowledge.Store)
    if !ok {
        return ""
    }
    // Tokenize and search each word
    for _, word := range strings.Fields(strings.ToLower(intent)) {
        if len(word) < 3 { continue } // skip short words
        matches := store.FindMatchingRecipes(word) // need to export this
        if len(matches) == 1 {
            return matches[0]
        }
    }
    return ""
}
```

**Note**: `findMatchingRecipes` is currently unexported on `Store`. Need to either:
- Export it as `FindMatchingRecipes` (simple)
- Or add a `DetectRecipe(query string) string` method to `Provider` interface (cleaner)

**Preferred**: Add to Provider interface — keeps knowledge detection inside knowledge package:

```go
// Provider interface addition:
DetectRecipe(query string) (string, error) // single match or error
```

### Step 3: Wire intent into buildGuide

**File**: `internal/workflow/bootstrap_guide_assembly.go`

```go
func (b *BootstrapState) buildGuide(step string, iteration int, _ Environment, kp knowledge.Provider, intent string) string {
    // ... existing code ...
    recipeName := detectRecipeFromIntent(intent, kp)

    return assembleGuidance(GuidanceParams{
        // ... existing fields ...
        RecipeName: recipeName,
    })
}
```

**File**: `internal/workflow/bootstrap.go` line 235 — pass intent:

```go
resp.Current.DetailedGuide = b.buildGuide(detail.Name, iteration, env, kp, intent)
```

### Step 4: Inject recipe content in assembleKnowledge

**File**: `internal/workflow/guidance.go`

In `assembleKnowledge()`, after runtime briefing (line 90), add recipe injection:

```go
// Recipe content injection (replaces hint-only behavior).
if needsRuntimeKnowledge(params.Step) && params.RecipeName != "" {
    if recipe, err := params.KP.GetRecipe(params.RecipeName, params.Mode); err == nil && recipe != "" {
        parts = append(parts, "## Framework Recipe: "+params.RecipeName+"\n\n"+recipe)
    }
}
```

For provision step, inject only the import.yml section from the recipe:

```go
// Recipe import.yml section at provision (for envSecrets guidance).
if params.Step == StepProvision && params.RecipeName != "" {
    if recipe, err := params.KP.GetRecipe(params.RecipeName, params.Mode); err == nil {
        if importSection := extractH2Section(recipe, "import.yml"); importSection != "" {
            parts = append(parts, "## Framework Import Pattern\n\n"+importSection)
        }
    }
}
```

### Step 5: Remove duplicate runtime guide from recipe injection

`GetRecipe()` currently calls `prependRecipeContext()` which prepends the runtime guide (php.md) and universals. When injecting into guidance, the runtime guide is already in the briefing (from Step 4 above, line 88). So we'd get php.md twice.

**Option A**: New method `GetRecipeContent(name, mode)` that returns recipe content + mode adaptation WITHOUT runtime guide and universals prepend.

**Option B**: Accept duplication — runtime guide is 60 lines, context windows are large.

**Preferred**: Option A for cleanliness. Add to Store:

```go
func (s *Store) GetRecipeContent(name, mode string) (string, error) {
    doc, err := s.Get("zerops://recipes/" + name)
    if err != nil {
        return "", err
    }
    content := doc.Content
    if mode != "" {
        rt := s.detectRecipeRuntime(name)
        content = prependModeAdaptation(mode, rt) + content
    }
    return content, nil
}
```

### Step 6: Update bootstrap.md text changes

**File**: `internal/content/workflows/bootstrap.md`

1. **Line 67** — change "optional" to "recommended":
```
Strongly recommended for known frameworks — recipes contain critical setup patterns
(secrets, scaffolding, common failures) not in generic runtime guides. If a matching
recipe exists, its content is auto-injected into your guidance.
```

2. **After line 118** — add envSecrets checklist item:
```
| Framework secrets | If using Laravel/Rails/Django/etc., `envSecrets` with `<@generateRandomString(...)>` and `#yamlPreprocessor=on` |
```

3. **Line 210** — add framework-aware .env warning:
```
Do NOT create `.env` files — empty values shadow OS env vars. Tools like `composer create-project`
create `.env` files automatically; use `--no-scripts` to prevent this. Check your framework recipe
for the correct scaffolding command.
```

## Files to Modify

| File | Changes | Lines |
|------|---------|-------|
| `internal/knowledge/engine.go` | Add `DetectRecipe` to Provider interface, export `FindMatchingRecipes` or add detection method | ~15 |
| `internal/knowledge/briefing.go` | Add `GetRecipeContent` method (recipe without runtime/universals prepend) | ~15 |
| `internal/workflow/guidance.go` | Add `RecipeName` to GuidanceParams, inject recipe in assembleKnowledge | ~15 |
| `internal/workflow/bootstrap_guide_assembly.go` | Add `detectRecipeFromIntent`, pass intent to buildGuide | ~20 |
| `internal/workflow/bootstrap.go` | Pass intent to buildGuide | ~2 |
| `internal/content/workflows/bootstrap.md` | Text changes (3 locations) | ~8 |

## Test Plan

| Layer | Tests | Package |
|-------|-------|---------|
| Unit | `TestDetectRecipeFromIntent_*` — intent matching | `workflow` |
| Unit | `TestGetRecipeContent_*` — mode adaptation without prepend | `knowledge` |
| Unit | `TestAssembleKnowledge_WithRecipe_*` — recipe injection at generate/provision | `workflow` |
| Unit | `TestBuildGuide_Generate_ContainsRecipe` — end-to-end recipe in guidance | `workflow` |
| Tool | Verify recipe content appears in workflow response at generate step | `tools` |
| Integration | Bootstrap with "Laravel" intent → verify guidance contains APP_KEY, envSecrets, --no-scripts | `integration` |

## Risk Assessment

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|-----------|--------|------------|
| R1 | Intent doesn't contain framework name | Medium | Recipe not injected (falls back to hints) | Acceptable — same as today. Future: allow explicit recipe in plan |
| R2 | Multiple recipe matches from intent | Low | Could inject wrong recipe | Use first single-match only; skip ambiguous |
| R3 | Recipe content too large for context | Very Low | Recipes are ~160 lines, context windows are 128K-1M tokens | Monitor; future mode-based section filtering if needed |
| R4 | GetRecipeContent adds method to Provider interface | N/A | MockProvider needs update | Update mock in tests |
