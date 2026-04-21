# brief-scaffold-minimal-diff.md

**Purpose**: diff against the minimal tier's current scaffold flow. Per RESUME decision #1 there is no live v34 minimal session log; the reference is the spec-derived flow at [../01-flow/flow-minimal-spec-main.md](../01-flow/flow-minimal-spec-main.md) + [../../../internal/content/workflows/recipe.md](../../../internal/content/workflows/recipe.md) block content.

Current minimal scaffold flow (per recipe.md):
- No named `<block>` for "minimal scaffold dispatch" — minimal single-codebase scaffold is framed inline under the general scaffold guidance in recipe.md L790–1125 (`scaffold-subagent-brief` block).
- The block IS written as a sub-agent dispatch template. For single-codebase minimal, main applies the same guidance inline (no dispatch fires).
- Minimal has no separate scaffold-brief atom; the same showcase block is consumed in a reduced form.

## 1. Removed from current-minimal → disposition

| Current (recipe.md) segment | Disposition | New home |
|---|---|---|
| `scaffold-subagent-brief` L790–1125 (336 lines, mixed dispatcher + sub-agent audience) | load-bearing → atoms (split 8 ways, per atomic-layout.md §3) | Same atom tree as showcase scaffold; minimal consumes the subset applicable to single-codebase path |
| Recipe-slug mentions "nestjs-showcase" inside the scaffold block | dispatcher → DISPATCH.md | Recipe-slug is interpolated at stitch time; atom bodies are generic |
| `<<<MANDATORY>>>` wrappers | dispatcher → DISPATCH.md | P2 closure |
| "The three codebases" references inside scaffold block | scar tissue for minimal (tier-inappropriate) | Stitcher tier-branches (`.IsSingleCodebase` vs `.IsMultiCodebase`) so minimal composition does NOT carry showcase's 3-codebase coordination prose |
| Platform-principles prose inline | load-bearing → atom | `principles/platform-principles/01..06.md` pointer-include, filtered by role (minimal api-style: 01/02/03/05/06; static-frontend: 02/06) |
| v34 `Fix E PerturbsChecks` scaffolding inside the block | scar tissue → deleted (supplanted by P1 author-runnable pre-attest) | — |

## 2. Added to new composition (specific to minimal)

| Content | Closes defect class |
|---|---|
| Tier-gate on `phases/generate/scaffold/where-to-write-single.md` | New — formalizes the "single-codebase → main-inline" path. Current system handles this branching implicitly in Go (`recipe_substeps.go`). |
| `briefs/scaffold/*` atoms consumed IN-BAND when `!multi_codebase` | Closes the architectural gap where minimal consumed the showcase-shaped sub-agent brief without audience adaptation. |
| `SymbolContract` filtered to minimal's managed-service subset | **v34-cross-scaffold-env-var** trivially N/A for single-codebase; but multi-codebase minimals (dual-runtime) still use the contract the same as showcase |
| Positive-form `phases/generate/scaffold/*` atoms (no enumerated prohibitions) | **v33 enumeration class** (P8) |

## 3. Boundary changes

| Axis | Current (minimal) | New |
|---|---|---|
| Source of scaffold guidance | Shared showcase-shaped block (scaffold-subagent-brief) | Same atoms, tier-branched by single vs multi codebase |
| Dispatch mechanism | Main consumes inline (no Agent tool call) | Same — but now explicitly atom-structured, not block-carved |
| Audience of the atoms | Implicit sub-agent assumption | Audience-adaptive (stitcher substitutes phrasing when in-band consumed) |
| Pre-ship aggregate | Shared with showcase (single hand-maintained bash block) | Contract-driven FixRecurrenceRules (applicable subset) + framework-role-branched reminder |

## 4. Byte-budget reconciliation

Current minimal does NOT transmit a scaffold brief (no dispatch); it reads the showcase-shaped block inline. Approximate consumed byte-budget from recipe.md L790–1125 ≈ 13 KB. New minimal composed guidance ≈ 6 KB.

| Segment | Current minimal | New minimal | delta |
|---|---:|---:|---:|
| Preamble (sub-agent framing — wasted on main) | ~1500 | 0 | -1500 |
| MANDATORY wrapper (wasted) | ~800 | 0 | -800 |
| Service plan context (showcase-sized: 5 services; minimal usually has 1) | ~800 | ~200 (contract filtered) | -600 |
| Scaffold task body | ~3500 | ~2800 | -700 |
| Platform principles | ~400 | ~250 (filtered to applicable) | -150 |
| Pre-ship aggregate | ~2000 | ~1500 (framework-branched) | -500 |
| Reporting back | ~400 | ~300 | -100 |
| PriorDiscoveries slot (not applicable for main-inline minimal) | — | — | 0 |
| **Total** | **~13 KB** | **~6 KB** | **-7 KB (~54% reduction)** |

Minimal tier is where the atomic rewrite has the largest byte-reduction lever because the current system forces minimal to consume showcase-shaped content.

## 5. Silent-drops audit

The current system has no clean "minimal-scaffold-brief" to diff against — the work was implicit. All relevant content is preserved in the atoms; tier-branching removes only showcase-specific content that was scar tissue for minimal.
