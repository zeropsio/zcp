# Response-Driven Steering for MCP Tools

## Status: DRAFT

## 1. The Fundamental Problem

An MCP server is passive — it answers when asked. An LLM is reactive — it acts on the
latest context. The server cannot control which tools the LLM calls, in what order, or
whether it reads instructions before acting.

**Current design assumes the LLM follows a mental model**: "gather knowledge first, then
act." This is instruction-driven orchestration — we tell the LLM what to do and hope it
complies. It doesn't work reliably:

```
Instructions say: "Call zerops_context first, zerops_workflow for multi-step, zerops_knowledge before YAML"
LLM actually does: zerops_workflow → zerops_discover → zerops_knowledge → generates YAML → fails
```

The LLM skipped `zerops_context` (live versions), used `bun@1` (doesn't exist), passed
a shallow dry-run, and failed on real import. The knowledge was available — but scattered
across tools the LLM didn't call.

**This isn't a bug to patch. It's a design flaw**: we organized tools by data source
(context, knowledge, workflow) instead of by the LLM's actual interaction pattern.

## 2. First Principles

### P1: The only reliable steering mechanism is the tool response

Instructions get ignored. Tool descriptions get skimmed. But the LLM MUST process tool
responses — they're the input for its next decision. Every tool response is an opportunity
to steer the LLM's next action.

### P2: Each response must be self-sufficient for its purpose

No tool should assume previous tools were called. If the LLM calls `zerops_knowledge`
without calling `zerops_context` first, the knowledge response should still contain
everything needed to generate correct YAML — including valid versions.

### P3: Steer through the response chain, not through call order

Instead of: "call A, then B, then C" (instruction-driven)
Do this: A's response tells the LLM what to do next, with all context needed (response-driven)

```
Instruction-driven:                    Response-driven:
"Please call these tools               "Here's what you asked for.
 in this specific order."               Here's what you need next.
                                         Here's why."
```

### P4: Make the correct path the easiest path

A well-designed API makes misuse harder than correct use. The LLM shouldn't need to know
about our internal architecture (three-tier context model, StackTypeCache, etc). It should
just call the tool that matches its intent and get a response that leads to correct output.

### P5: Defense in depth — every layer independently prevents failure

If the LLM skips step 1, step 2 should still work. If it skips steps 1 and 2, step 3
should catch the error. No single point of failure in the chain.

## 3. Current Architecture (What's Wrong)

### Knowledge is fragmented by data source

```
zerops_context    → platform overview + LIVE service stacks (the only source of valid versions)
zerops_workflow   → step-by-step procedure (no versions, no runtime specifics)
zerops_knowledge  → runtime rules + service cards + wiring (no versions)
zerops_discover   → current project state
```

The LLM's intent is "create bun + postgres." To do this correctly it needs:
1. Valid bun versions (from context)
2. Bootstrap procedure (from workflow)
3. Bun-specific rules + postgres wiring (from knowledge)
4. Current project state (from discover)

**Four tool calls, correct order required, no tool is self-sufficient.**

### Decision points lack context

When the LLM decides "I'll use bun@1", it hasn't seen the version list. When it generates
import.yml, it hasn't been asked about dev vs production. When dry-run passes, it hasn't
validated versions. Each decision is made with incomplete information.

### The StackTypeCache is siloed

```
StackTypeCache ──→ zerops_context (sole consumer)
                   ╳ zerops_knowledge (blind)
                   ╳ zerops_import (blind)
```

The cache is created in `server.registerTools()` and passed only to `RegisterContext()`.
The two tools that most need version information — the one assembling pre-generation
knowledge and the one validating YAML — have no access.

## 4. Design: Response-Driven Steering

### Core idea: every tool response steers toward the correct next action

Instead of hoping the LLM calls tools in order, make each tool response self-sufficient
and forward-looking. The response answers the immediate question AND includes what the
LLM needs for its next decision.

### The response chain

```
User: "create bun + postgres"
         │
         ▼
ANY first zerops tool call
         │
         ▼ (response includes available stacks + next-step guidance)
         │
zerops_knowledge(runtime, services)
         │
         ▼ (response includes rules + version validation + "generate YAML now")
         │
zerops_import(dryRun=true)
         │
         ▼ (response validates everything: structure + versions + warnings)
         │
zerops_import(real)
         │
         ▼ success on first try
```

Each tool independently catches the `bun@1` error. The LLM can enter the chain at
any point and still produce correct output.

### Layer 1: Workflow as the strategic entry point

`zerops_workflow("bootstrap")` is the most likely first call for multi-step operations.
Its response should include EVERYTHING the LLM needs to start correctly:

**Current response**: step-by-step procedure only.

**New response**: procedure + live service stacks + decision prompts.

```markdown
# Bootstrap: Setting Up a Zerops Project
...
## Available Service Stacks (live)

Runtime: bun@{1.1.34,1.2} [B] | deno@{1,2} [B] | dotnet@{6,8,9} [B] | ...
Managed: postgresql@{16} | mariadb@{10.6,10.11,11} | valkey@{7.2,8.0} | ...
Shared storage: shared-storage@1
Object storage: object-storage@1

**Use only these versions in import.yml.** Versions not listed here will fail on import.

---

## Phase 1: Configuration
### Step 1 — Discover current state
...
### Step 2 — Identify stack components
...
Decide environment type:
- **Development**: Include Mailpit (dev SMTP) + Adminer (DB GUI). NON_HA mode.
- **Production**: Skip dev services. Consider HA mode for databases. See: zerops_knowledge query="production checklist"

If the user hasn't specified, default to development.
```

**Implementation**: `zerops_workflow` handler receives `StackTypeCache` + `Client`.
When returning `bootstrap` workflow, appends the live stacks section. Other workflows
(deploy, debug, scale) get the same treatment where relevant.

**Why this works**: The LLM sees valid versions BEFORE it ever identifies stack components.
"bun@1" isn't in the list → the LLM picks "bun@1.2" from the start. The dev/production
decision is prompted at the moment the LLM identifies components.

### Layer 2: Knowledge as the tactical detail layer

`zerops_knowledge(runtime, services)` is the pre-YAML briefing. Its response should
validate the requested versions and include corrections.

**Current response**: core principles + runtime exceptions + service cards + wiring.

**New response**: same + version validation + explicit next-step guidance.

```markdown
# Zerops Core Principles
...
## Runtime-Specific: Bun
...
## Service Cards
### PostgreSQL
...
## Wiring Patterns
...

---

## Version Check

✓ postgresql@16 — valid
⚠ bun@1 — not found. Available: bun@1.1.34, bun@1.2. Use bun@1.2 (latest stable).

---

Next: Generate import.yml and zerops.yml using the rules above. Use only validated
versions. Then validate with zerops_import dryRun=true.
```

**Implementation**: `RegisterKnowledge()` receives `StackTypeCache` + `Client`.
In briefing mode, after assembling the standard content, appends a version check
section. Uses the same `StackTypeCache` instance as context/workflow tools.

**Why this works**: Even if the LLM skipped `zerops_workflow` entirely and went
straight to `zerops_knowledge`, it still gets version validation. The explicit
"Next:" guidance steers toward dry-run validation.

### Layer 3: Import dry-run as the safety net

`zerops_import(dryRun=true)` is the last checkpoint before real import. Currently
it only validates YAML structure. It should validate everything it can.

**Current validation**: YAML parses, `services:` exists, no `project:` key.

**New validation**: all of the above + version validation + mode validation + hints.

```json
{
  "dryRun": true,
  "valid": true,
  "services": [...],
  "warnings": [
    "Service 'app': type 'bun@1' not found. Available: bun@1.1.34, bun@1.2",
    "Service 'db': missing 'mode' field. Add mode: NON_HA or mode: HA (mandatory for postgresql)"
  ]
}
```

**Implementation**: `RegisterImport()` receives `StackTypeCache` + `Client`.
`importDryRun()` validates service types against cached stacks and checks for
common omissions (missing `mode` on managed services — the other known dry-run gap).

**Why this works**: Even if the LLM skipped both workflow and knowledge, dry-run
catches invalid versions AND missing mode BEFORE the real API call. The warnings
are actionable — the LLM can fix and retry without a failed import.

### Layer 4: Instructions as the weak-but-helpful hint

Instructions remain the lightest-touch layer. They can't be relied upon, but they
help compliant LLMs do the right thing on the first try.

**New instructions** (concise, unambiguous):

```
ZCP manages Zerops PaaS infrastructure. For multi-step operations (creating services,
deploying, debugging), start with zerops_workflow — it includes live service versions
and step-by-step guidance. Call zerops_knowledge before generating YAML for runtime-
specific rules and version validation. Use zerops_discover to check current state.
```

**Key change**: No more contradictory "call context first" vs "call workflow first."
The workflow IS the entry point and it now includes context.

## 5. What Happens to `zerops_context`?

`zerops_context` becomes **an optional deep-dive tool**, not a required first step.

**Before**: Must-call prerequisite (unreliably followed).
**After**: Standalone reference for platform fundamentals + full version list.

It's still useful for:
- LLMs that want to understand Zerops before doing anything
- Non-workflow interactions ("what service types are available?")
- Other MCP clients that don't use workflows

But it's no longer a single point of failure. The versions it uniquely provided are
now available through workflow responses and knowledge briefings.

## 6. Decision Tree (What the LLM Actually Does)

```
User request
│
├─ "create bun + postgres" (multi-step)
│   │
│   ├─ LLM calls zerops_workflow("bootstrap")    [IDEAL]
│   │   └─ Response: procedure + LIVE VERSIONS + dev/prod choice
│   │       └─ LLM picks bun@1.2, asks about dev services
│   │           └─ zerops_knowledge(bun@1.2, pg@16) → validated briefing
│   │               └─ zerops_import dryRun → passes with valid versions ✓
│   │
│   ├─ LLM calls zerops_knowledge(bun@1, pg@16)  [SKIPPED WORKFLOW]
│   │   └─ Response: briefing + "⚠ bun@1 not found, use bun@1.2"
│   │       └─ LLM fixes to bun@1.2 before generating YAML
│   │           └─ zerops_import dryRun → passes ✓
│   │
│   ├─ LLM generates YAML and calls import directly  [SKIPPED EVERYTHING]
│   │   └─ zerops_import dryRun → "⚠ bun@1 not found"
│   │       └─ LLM fixes and retries → passes ✓
│   │
│   └─ LLM skips dry-run entirely  [WORST CASE]
│       └─ zerops_import real → API error "Service stack Type not found"
│           └─ Same as today — but 3 earlier layers tried to prevent this
│
├─ "deploy my app" (single service)
│   └─ zerops_workflow("deploy") → includes relevant stacks
│
├─ "check logs" (single tool)
│   └─ zerops_logs directly (no workflow needed)
│
└─ "what can Zerops run?" (exploratory)
    └─ zerops_context → full platform overview + versions
```

**Every path except the worst case produces correct YAML on first try.**
The worst case (skip all validation) is the same as today — no regression.

## 7. Implementation

### StackTypeCache wiring (the core change)

```
BEFORE:
  StackTypeCache ──→ zerops_context

AFTER:
  StackTypeCache ──→ zerops_context
                 ──→ zerops_workflow (bootstrap, deploy)
                 ──→ zerops_knowledge (briefing mode)
                 ──→ zerops_import (dry-run validation)
```

Same cache instance, same TTL (1h), same thread-safety. First tool call that
needs versions triggers the API fetch; all subsequent calls use cached data.

### Files changed

| File | What | Size |
|------|------|------|
| **`internal/knowledge/versions.go`** | **New**. `FormatVersionCheck()` — validates requested vs live types. `FormatStackList()` — compact version list for workflow embedding. Shared by knowledge + workflow + import. | ~100 lines |
| `internal/knowledge/engine.go` | `GetBriefing()` accepts optional version info, appends check section | ~15 lines |
| `internal/tools/knowledge.go` | `RegisterKnowledge()` accepts `StackTypeCache` + `Client`, passes to briefing | ~10 lines |
| `internal/tools/workflow.go` | `RegisterWorkflow()` accepts `StackTypeCache` + `Client`, appends stacks to bootstrap/deploy responses | ~20 lines |
| `internal/ops/import.go` | `importDryRun()` accepts cached types, validates service types + mode | ~40 lines |
| `internal/tools/import.go` | `RegisterImport()` accepts `StackTypeCache` + `Client` | ~5 lines |
| `internal/server/server.go` | Wire `stackCache` to workflow + knowledge + import registrations | ~5 lines |
| `internal/server/instructions.go` | Rewrite instructions (workflow as entry point) | text only |
| `internal/content/workflows/bootstrap.md` | Add stacks placeholder + dev/prod decision in Step 2 | text only |

**Total new Go code**: ~190 lines. **Modified**: ~35 lines. Zero new packages.

### Version check module (`versions.go`)

Single module used by three consumers:

```go
// FormatStackList returns compact version list for workflow embedding.
// Example: "Runtime: bun@{1.1.34,1.2} [B] | nodejs@{18,20,22} [B] | ..."
func FormatStackList(types []platform.ServiceStackType) string

// FormatVersionCheck validates requested types against live types.
// Returns markdown section with ✓/⚠ per requested type + suggestions.
// Returns "" if types is nil/empty (graceful degradation).
func FormatVersionCheck(runtime string, services []string, types []platform.ServiceStackType) string

// ValidateServiceTypes checks import.yml service types against live types.
// Returns warning strings for mismatches. Used by import dry-run.
func ValidateServiceTypes(services []map[string]any, types []platform.ServiceStackType) []string
```

Three functions, three consumers, one source of truth for version logic.

### Workflow stacks injection

The workflow tool currently returns static markdown from embedded files. For bootstrap
and deploy workflows, the response needs dynamic content (live stacks).

**Approach**: Template placeholder in workflow markdown:

```markdown
<!-- STACKS_PLACEHOLDER -->
```

Workflow handler replaces the placeholder with `FormatStackList()` output at response time.
If cache is empty (API down), placeholder is removed — workflow still works without versions.

### Bootstrap workflow changes (content)

**Step 2 — add environment decision**:

```markdown
### Step 2 — Identify stack components

From the user's request, identify:
- **Runtime services**: type + framework (e.g., bun@1.2 with Hono, nodejs@22 with Next.js)
- **Managed services**: type + version (e.g., postgresql@16, valkey@7.2)

**Verify all types against the Available Service Stacks section above.**

**Environment type** (ask if not specified):
- **Development** (default): Include Mailpit (dev SMTP catch-all) + Adminer (DB GUI).
  Use NON_HA mode for all managed services.
- **Production**: Skip dev services. Consider HA mode for databases.
  Call zerops_knowledge query="production checklist" for hardening guidance.
```

**Step 3 — version validation awareness**:

```markdown
**What you get back:**
- Core principles, runtime exceptions, service cards, wiring patterns
- **Version validation** — warnings if any requested type isn't available
```

## 8. Test Plan

### Unit: `knowledge/versions_test.go`

| Test | Verifies |
|------|----------|
| `TestFormatStackList_Groups` | Compact notation: `bun@{1.1.34,1.2}` |
| `TestFormatStackList_Empty` | Returns `""` for nil/empty input |
| `TestFormatStackList_Categories` | Runtime/Managed/Storage grouping |
| `TestFormatVersionCheck_AllValid` | `✓` for every matched type, no warnings |
| `TestFormatVersionCheck_InvalidVersion` | `⚠` with suggestion for `bun@1` |
| `TestFormatVersionCheck_UnknownBase` | `⚠` for `ruby@3` (base not in stacks) |
| `TestFormatVersionCheck_Empty` | Returns `""` when no live types available |
| `TestValidateServiceTypes_Valid` | Empty warnings for valid services |
| `TestValidateServiceTypes_Invalid` | Warning with available versions |
| `TestValidateServiceTypes_MissingMode` | Warning for managed service without `mode` |

### Unit: `ops/import_test.go` (additions)

| Test | Verifies |
|------|----------|
| `TestImportDryRun_VersionWarnings` | Warnings array populated for invalid types |
| `TestImportDryRun_ModeWarnings` | Warnings for managed service missing `mode` |
| `TestImportDryRun_NoCache_NoWarnings` | Graceful: nil cache = no version warnings |

### Tool: `tools/knowledge_test.go` (additions)

| Test | Verifies |
|------|----------|
| `TestKnowledge_Briefing_IncludesVersionCheck` | Mock cache → response has version section |
| `TestKnowledge_Briefing_NoCache_StillWorks` | Nil cache → briefing without version section |

### Tool: `tools/workflow_test.go` (additions)

| Test | Verifies |
|------|----------|
| `TestWorkflow_Bootstrap_IncludesStacks` | Mock cache → response has stacks section |
| `TestWorkflow_Bootstrap_NoCache_CleanOutput` | Nil cache → placeholder removed, no artifacts |
| `TestWorkflow_NonBootstrap_NoStacks` | `scale` workflow doesn't get stacks appended |

### Integration

| Test | Verifies |
|------|----------|
| Bootstrap flow (mock) | workflow → knowledge → import: version warning appears in at least one response |

### E2E

| Test | Verifies |
|------|----------|
| Live stacks in workflow | `zerops_workflow("bootstrap")` includes real API versions |
| Invalid version dry-run | `zerops_import dryRun=true` with `bun@1` returns version warning |

## 9. Design Decisions

### D1: Steer through responses, not instructions

**Principle**: P1 (tool response is the only reliable steering mechanism).

Instructions are a hint. Tool responses are the contract. If the workflow response includes
live versions, the LLM uses them — regardless of whether it read the instructions.

### D2: Every layer is independently sufficient

**Principle**: P2 (self-sufficiency) + P5 (defense in depth).

The LLM can enter the chain at any point (workflow, knowledge, import) and still get
version validation. No layer assumes previous layers ran. This means some information
is present in multiple responses — that's intentional redundancy, not waste.

### D3: Workflow becomes the strategic entry point (absorbs context's role)

**Principle**: P4 (correct path = easiest path).

`zerops_context` was designed as a "load everything first" tool. But LLMs don't work
that way — they want to ACT on the user's intent. The workflow matches intent. Making
the workflow response include live versions means the LLM gets versions as a side-effect
of following its natural instinct to find the procedure first.

`zerops_context` remains available but is no longer a prerequisite.

### D4: Shared StackTypeCache across all consumers

**Principle**: Simplicity.

One cache instance (1h TTL, double-checked locking) serves context + workflow + knowledge
+ import. First access triggers the API call. No duplication, no extra latency.

### D5: Graceful degradation everywhere

**Principle**: P2 (self-sufficiency).

If the API is unreachable (cache empty):
- Workflow: returns procedure without version list (still useful)
- Knowledge: returns briefing without version check (still useful)
- Import dry-run: validates structure only, no version warnings (same as today)

No tool errors out because of missing version data. Versions are a bonus layer
of correctness, not a hard dependency.

### D6: Dev/production is a workflow decision, not a knowledge decision

Dev services (Mailpit, Adminer) are environment choices, not platform facts. The
bootstrap workflow is the right place to prompt for this decision — at the moment
the LLM identifies stack components (Step 2). The knowledge base already has the
patterns; the workflow just needs to surface the question.

### D7: Version validation produces warnings, not errors

Versions change on the API side. Our cache might be stale. A strict "invalid type" error
would create false negatives. Warnings surface the issue without blocking a potentially
valid request. The LLM reads warnings and self-corrects.

## 10. Relationship to Existing Design

### Extends flow-orchestration.md

The three-tier model (instructions → workflows → knowledge) remains. This design
changes HOW each tier works:

| Tier | Before | After |
|------|--------|-------|
| 1. Instructions | Contradictory call order | Workflow as entry point, no prerequisites |
| 2. Workflows | Pure procedure, no context data | Procedure + live stacks + decision prompts |
| 3. Knowledge | Authoritative but version-blind | Authoritative + version-validated |
| (new) 4. Import | Structure-only dry-run | Structure + version + mode validation |

### Supersedes the "call zerops_context first" pattern

`zerops_context` was the single-point-of-truth for versions. After this change,
versions flow through every tool that needs them. `zerops_context` becomes one of
several ways to access version info, not the required first step.

## 11. Implementation Order

```
1. versions.go + tests              (foundation: format + validate functions)
2. engine.go: GetBriefing + versions (knowledge briefing gains version check)
3. import.go: dry-run validation     (import gains version + mode warnings)
4. workflow.go: stacks injection     (workflow gains live stacks section)
5. server.go: wire StackTypeCache    (connect cache to all consumers)
6. bootstrap.md: content updates     (dev/prod decision, version references)
7. instructions.go: rewrite          (workflow as entry point)
8. Integration + E2E tests           (verify the response chain end-to-end)
```

Steps 1-4 are independent units with their own tests (TDD). Step 5 wires them together.
Steps 6-7 are text-only. Step 8 validates the complete chain.
