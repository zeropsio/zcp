# Knowledge Distillation Process

How to extract knowledge from zerops-docs into ZCP's layered knowledge system.

## Architecture

```
zerops-docs (290 MDX files, 2.2 MB)     <- Rich tutorials, concepts, visuals
    |
    v  (structured distillation)
ZCP knowledge layers:
    L0: foundation/platform-model.md    <- Conceptual understanding (HOW Zerops works)
    L1: foundation/rules.md             <- Actionable DO/DON'T rules (pitfalls)
    L2: foundation/grammar.md           <- YAML schema reference (field definitions)
    L3: foundation/runtimes.md          <- Runtime-specific deltas
    L4: foundation/services.md          <- Managed service reference cards
    L5: foundation/wiring.md            <- Cross-service wiring templates
    L6: decisions/*.md                  <- Decision hints (choose-database, etc.)
    L7: guides/*.md                     <- Deep-dive guides (networking, scaling, etc.)
    L8: recipes/*.md                    <- Complete working examples
```

## Source Mapping

| zerops-docs Source | Target Layer | What to Extract |
|---|---|---|
| Overview, architecture pages | Platform Model (L0) | Concepts, lifecycle, networking model, causality |
| How-to pages, constraints, FAQ | Rules (L1) | DO/DON'T with reasons, common mistakes |
| Reference pages, YAML specs | Grammar (L2) | Field definitions, types, schema annotations |
| Per-runtime pages | Runtimes (L3) | Base image includes, build procedures, binding |
| Per-service pages | Services (L4) | Type, ports, env vars, HA specifics, gotchas |
| Integration guides | Wiring (L5) | Connection patterns, env var templates |
| Comparison/decision pages | Decisions (L6) | When to use which service, TL;DR hints |
| Deep technical guides | Guides (L7) | Networking internals, cache architecture, scaling |
| Tutorials, quickstarts | Recipes (L8) | Complete import.yml + zerops.yml examples |

## Distillation Principles

### 1. Concepts > Syntax
Extract WHY something works the way it does, not just WHAT the YAML field is. An LLM that understands the lifecycle can reason about any scenario; an LLM that only knows field names can only template-match.

### 2. Causality Always
Every rule must include its reason. "ALWAYS bind 0.0.0.0" is a rule. "ALWAYS bind 0.0.0.0 BECAUSE the L7 LB routes to the VXLAN IP, not localhost" is knowledge.

### 3. No Duplication Across Layers
Each fact lives in exactly one layer:
- **Platform Model**: concepts, mental models, cause-effect chains
- **Rules**: actionable constraints (ALWAYS/NEVER)
- **Grammar**: field definitions and schema (WHAT the YAML looks like)
- **Runtimes**: per-runtime deltas only (not universal rules)

If a concept appears in grammar.md AND rules.md, remove it from grammar.md.

### 4. Compression Without Loss
Each distillation step compresses content 2-5x. The key is removing:
- Verbose explanations (replace with dense bullet points)
- Redundant examples (keep one canonical example per concept)
- UI instructions (ZCP is API/CLI only)
- Step-by-step tutorials (collapse to build procedures)

### 5. Battle-Tested First
Prioritize knowledge that has caused real failures:
- Eval scenario failures -> immediate rule addition
- Production incidents -> gotchas and causal chains
- Common support questions -> platform model clarifications

## Process for Adding New Knowledge

### From a new zerops-docs page:
1. Read the page and identify which layer(s) it maps to
2. Check if the information already exists in the target layer
3. If new: add to the appropriate layer following its format
4. If update: modify the existing entry, don't create duplicates
5. Run tests: `go test ./internal/knowledge/... -count=1`

### From an eval failure:
1. Identify the root cause (missing concept? missing rule? wrong syntax?)
2. Add to the appropriate layer:
   - Missing understanding -> platform-model.md
   - Missing constraint -> rules.md
   - Wrong schema -> grammar.md
   - Runtime-specific -> runtimes.md
3. Add the causal chain to platform-model.md section 10 if applicable
4. Run tests

### From a new service/runtime version:
1. Update grammar.md field constraints if needed
2. Update runtimes.md or services.md with version-specific changes
3. Update wiring.md connection patterns if ports/env vars changed
4. Run tests

## Quality Checks

After any knowledge update:
- [ ] `go test ./internal/knowledge/... -count=1` passes
- [ ] `go test ./... -count=1 -short` passes (no regression)
- [ ] New content follows the layer's format (e.g., ALWAYS/NEVER in rules.md)
- [ ] No duplication across layers
- [ ] Causal reasons included where applicable
- [ ] BM25 search returns new content for relevant queries
