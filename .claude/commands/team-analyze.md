# Team Analyze

Parse `$ARGUMENTS` to extract **reference files** and **task description**.

### Argument parsing rules

1. **Identify all file references**: any quoted path (`'path'` or `"path"`), or any token that resolves to an existing file. These become **reference files**.
2. **Everything else** is the **task description** (focus question).
3. **Pipe separator** (`|`) is also supported: `<filepath> | <task description>`.
4. If no arguments provided, ask the user what to analyze.

### Determine primary document vs reference files

- If exactly ONE reference file is a `.md` plan/design document → it's the **primary document**. Output files are named after it.
- If MULTIPLE `.md` files or NO `.md` files → create a **synthesis document** (`plans/analysis-{descriptive-slug}.md`) as the primary.
- Non-`.md` reference files (`.go`, `.yml`, etc.) are always **reference material**, never primary.

### Examples

```
# Codebase analysis
/team-analyze 'internal/workflow/' study the implementation, find improvements

# Flow tracing
/team-analyze 'internal/ops/discover.go' trace what flows back to the LLM

# Document review
/team-analyze plans/my-plan.md | is this plan ready for implementation?

# Implementation planning
/team-analyze 'internal/tools/' prepare an implementation plan for batch operations

# Refactoring analysis
/team-analyze 'internal/workflow/' trim bloat, find dead code ultrathink
```

## Setup

1. **Read all reference files**. If a reference is a directory, Glob for `**/*.go` and `**/*.md` within it.
2. Read `/Users/macbook/Documents/Zerops-MCP/zcp/CLAUDE.md`.
3. **If no primary document exists** — create a synthesis document:
   ```markdown
   # Analysis: {descriptive title from task description}
   **Date**: {today}
   **Task**: {task description}
   **Reference files**:
   - {file1} — {one-line description after reading}
   - {file2} — ...
   ```
   Write this to `plans/analysis-{slug}.md`. This becomes the primary document for output naming.
4. **Codebase pre-exploration**:
   - Read the primary document + reference files to identify relevant packages/areas.
   - Use Glob and Grep to map ALL relevant source files beyond what was explicitly referenced.
   - Build a **file map**: list of relevant files with one-line descriptions and line counts.
5. Detect version state:
   - Extract `<basename>` and `<dir>` from the primary document path.
   - Check if `<dir>/<basename>.context.md` exists. If yes, read it — this is a continuation.
   - Count existing `.review-*.md` and `.analysis-*.md` files for this basename → iteration N.
6. Store the task description for inclusion in all agent prompts.

## Task Type Detection

Classify the task by scanning the description. First match wins:

| Task Type | Detection signals |
|-----------|-----------------|
| `flow-tracing` | "trace", "flow", "chain", "what happens when", "data path", "what flows", "step by step", "retezec" |
| `refactoring-analysis` | "trim", "dead code", "bloat", "simplify", "rename", "cleanup", "refactor", "unused", "osekat", "balast" |
| `implementation-planning` | "plan", "prepare", "design", "how to implement", "roadmap", "priprav", "sestav" |
| `document-review` | Exactly ONE `.md` primary AND description contains "review", "ready", "check", "evaluate", "over" (or description absent) |
| `codebase-analysis` | Default — anything not matching above |

Report: "**Task type**: {type}" — proceed immediately, no wait.

## Complexity Detection

Score signals to determine analysis depth:

- **File count**: 1-3 = 0pts, 4-10 = 1pt, 10+ = 2pts
- **Package count**: 1 = 0pts, 2-3 = 1pt, 4+ = 2pts
- **Task keywords** (+1 each): "security", "architecture", "platform", "end-to-end", "comprehensive"
- **Platform claims** in referenced files (import YAML, zerops.yml, scaling, env vars) = +2pts
- **Override**: "ultrathink" anywhere = force **Deep**

| Level | Score | Agents |
|-------|-------|--------|
| **Light** | 0-1 | KB + 1 primary = 2 |
| **Medium** | 2-3 | KB + primary + secondary = 3 |
| **Deep** | 4+ or ultrathink | KB + verifier + primary + adversarial = 4 |

Report: "**Complexity**: {level} ({N} agents)" — proceed immediately.

## Team DNA — Include verbatim in EVERY agent prompt

```
## ZCP Team DNA — Mandatory Principles

You are working on the ZCP project (Zerops Control Plane — Go binary, MCP protocol).

### 1. Fix Upstream, Not Downstream
- Trace concerns to ROOT CAUSE. Don't flag symptoms.
- "Where should this be prevented so it never propagates?"
- If fixed at source, downstream consequences disappear — don't flag them separately.
- Prefer structural enforcement (types, validation, gates) over text instructions.

### 2. Verify, Don't Assume
- READ actual code/docs before claims. Never reason from what "should" exist.
- If you reference a file/function/API — verify it exists and works as described.
- Unverified claims: say "UNVERIFIED: [claim]" explicitly.
- Zerops is NOT Kubernetes. Don't map K8s concepts onto it.

### 3. Rational Pragmatism
- Flag REAL problems with realistic failure scenarios. No theoretical concerns.
- No over-engineering: no abstractions without 3+ uses. Simple > clever.
- No over-simplification: if complexity is genuinely needed, say so.
- Measure: does this reliably deliver a working result end-to-end?

### 4. Logical Consistency End-to-End
- If X is true, what else MUST be true? Follow the chain.
- Internal consistency: no contradictions between sections.
- External consistency: matches codebase, CLAUDE.md, test patterns.

### 5. Evidence Over Opinion
- Every claim must have a basis: code reference, doc citation, test result, or API response.
- "I think" is worthless. "I verified by reading X" or "line 47 of Y shows Z" has value.
- If you cannot verify a claim, mark it UNVERIFIED. Do not present it as fact.
- Disputes are resolved by EVIDENCE, not by majority or rhetoric.

### 6. Confidence Levels
- VERIFIED = checked against code, docs, or live platform. Cite the source.
- LOGICAL = follows from verified facts but not directly tested. State the reasoning chain.
- UNVERIFIED = speculative or based on general knowledge. Must be flagged explicitly.
- Never present UNVERIFIED as VERIFIED.

### 7. READ-ONLY — MANDATORY (ZERO TOLERANCE)
- You are an ANALYST, not an implementer. You FIND and REPORT problems. You do NOT fix them.
- You MUST NOT write files, edit code, create commits, or create worktrees.
- You MUST NOT use Bash to write files (cat/echo/tee/heredoc to files), run git add, git commit, or any command that modifies the filesystem. Bash is allowed ONLY for read-only commands: git log, git show, git diff, git blame.
- Your ONLY outputs are SendMessage broadcasts (max 2 messages total).
- The orchestrator writes all report files and decides what to implement.
- VIOLATION: If you edit files, commit code, or implement fixes, your entire analysis is INVALIDATED.

### 8. ZCP-Specific
- Go, TDD mandatory, table-driven tests, max 350 lines/file
- Architecture: cmd → server → tools → ops → platform (strict)
- MCP = "dumb" — data quality + guardrails, not cleverness
- Extend existing mechanisms before creating new ones
- Simplest solution: plain functions > abstractions
- Zerops docs: `../zerops-docs/` is the canonical reference
```

---

## Stage 1: Knowledge Assembly

Use `TeamCreate` with name `"analyze-team"`.

### Agent: `kb` — via `zerops-knowledge` custom agent (ALL levels)

Use Agent tool with `subagent_type="zerops-knowledge"`, name `"kb"`.

Provide: full input document + task description + file map + detected task type.

The agent adapts its brief based on task type (flow-tracing adds data flow trace, refactoring adds dead code scan).

### Agent: `verifier` — via `platform-verifier` custom agent (DEEP only)

Use Agent tool with `subagent_type="platform-verifier"`, name `"verifier"`.

Provide: full input document + list of testable platform claims extracted from the document.

Only spawned when complexity is Deep. Checks memory first, skips re-verification of stable facts <30 days old.

### Stage 1 Completion

Wait for all Stage 1 agents to complete. Combine outputs into the **Knowledge Base (KB)**:
1. **Factual Brief** (from `kb`) — docs + code facts, with optional flow trace or dead code analysis
2. **Verification Results** (from `verifier`, Deep only) — live platform test results

---

## Stage 2: Analysis

### CRITICAL: All analysis agents are READ-ONLY

All Stage 2 agents MUST be spawned with `subagent_type="Explore"` and `mode="plan"`.

**WARNING**: Explore agents have Bash access. Without `mode="plan"`, they CAN write files via heredoc/echo/tee. Always use `mode="plan"`.

### Analyst Role Selection

Select roles based on task type:

| Task Type | Primary focus | Secondary focus (medium+) |
|-----------|--------------|--------------------------|
| `codebase-analysis` | Architecture + Correctness | Security + DX |
| `flow-tracing` | Correctness + Data Flow | Architecture + Edge Cases |
| `document-review` | Correctness + Completeness | Architecture + Consistency |
| `implementation-planning` | Architecture + Feasibility | QA + Risk |
| `refactoring-analysis` | Dead Code + Structure | Correctness + Regression Risk |

### Agent Prompt Template

```
{Team DNA}

## Your Role: {role description from table above}

## Task Type: {detected task type}

You are analyzing this as a **{task type}** task. Orient your analysis accordingly.

## Input

{full contents of the primary document OR synthesis document}

## Reference Files

{list of reference files with one-line descriptions}

## Knowledge Base — GROUND TRUTH

CITE these facts. Do not contradict without independent verification.

{output from kb agent}

{IF Deep:}
### Platform Verification Results
{output from verifier}

## CLAUDE.md Summary

{key conventions from CLAUDE.md}

{IF task description exists:}
## Task Description — PRIMARY DIRECTIVE
{task description}

Orient ALL analysis around this. Use your tools (Read, Grep, Glob) to investigate.

{IF previous context exists:}
## Previous Context
{contents of .context.md}
Do NOT re-raise resolved concerns. Do NOT re-propose rejected alternatives.

## Instructions

1. Read the ENTIRE input and Knowledge Base carefully.
2. Every finding MUST cite evidence:
   - [KB: section] — citing Knowledge Base
   - [SELF-VERIFIED: file:line] — you checked independently
   - [UNVERIFIED] — could not verify, flagged explicitly
3. Before claiming any function/type does NOT exist: grep for it. Cite grep result.
4. Send ONE broadcast via SendMessage to "*" with ALL findings.
5. After broadcasting, WAIT SILENTLY.
6. If the adversarial analyst challenges you (Deep only), respond with ONE message. Then STOP.
7. Total messages: 2 maximum.

## CRITICAL CONSTRAINT — REPORT ONLY
- Do NOT write code, tests, documentation, or config changes
- Do NOT create git commits or modify any files via Bash
- Do NOT implement your own recommendations

## Output Format

{task-type-specific format — see below}
```

### Task-Type-Specific Output Formats

**codebase-analysis:**
```
### {Role} Analysis
**Scope**: {files/packages analyzed}
#### Findings
- [F1] {finding} — {CRITICAL/MAJOR/MINOR} — Evidence: {citation}
#### Recommendations
- [R1] {recommendation} — Evidence: {citation}
```

**flow-tracing:**
```
### {Role} — Flow Analysis
**Entry point**: {file:function}
#### Trace Steps
1. {file:line} — {what happens} — Evidence: {citation}
2. ...
#### Data Shape at Each Step
| Step | Type | Fields | Notes |
#### Failure Paths
- [FP1] At step {N}: {what can fail} — Evidence: {citation}
```

**document-review:**
```
### {Role} Review
**Assessment**: SOUND | CONCERNS | UNSOUND
**Evidence basis**: {X of Y findings VERIFIED}
#### Findings
- [C1] {finding} — {CRITICAL/MAJOR/MINOR} — Evidence: {citation}
#### Recommendations
- [R1] {recommendation} — Evidence: {citation}
```

**implementation-planning:**
```
### {Role} — Plan Assessment
**Feasibility**: HIGH | MEDIUM | LOW
**Risk level**: HIGH | MEDIUM | LOW
#### Feasibility Findings
- [F1] {finding} — Evidence: {citation}
#### Missing Elements
- [M1] {what's missing} — Evidence: {citation}
#### Risk Assessment
- [RISK1] {risk} — Likelihood: {H/M/L} — Impact: {H/M/L} — Evidence: {citation}
```

**refactoring-analysis:**
```
### {Role} — Refactoring Analysis
#### Dead Code
- [DC1] {what} at {file:line} — Evidence: {citation}
#### Simplification Opportunities
- [S1] {current} → {proposed} — lines saved: {N} — Evidence: {citation}
#### Regression Risks
- [RR1] Removing {X} could break {Y} — Evidence: {citation}
```

### Stage 2 Execution

- **Light**: Spawn primary analyst only. Wait for completion.
- **Medium**: Spawn primary + secondary in parallel. Wait for both.
- **Deep**: Spawn primary only (secondary skipped — adversarial replaces it in Stage 3).

---

## Stage 3: Adversarial Challenge (DEEP only)

This stage is **sequential** — the adversarial agent receives ALL output from Stage 2.

Spawn with `subagent_type="Explore"` and `mode="plan"`, name `"adversarial"`.

```
{Team DNA}

## Your Role: Adversarial Analyst

Challenge the primary analyst's findings AND the document itself.

## Rules

1. For EVERY finding you challenge, cite specific code: file path and line number.
2. Challenges without code citations are worthless — do not make them.
3. Focus on CRITICAL and MAJOR. Ignore MINOR unless trivially wrong.
4. Also identify findings the analyst MISSED — things the KB reveals that weren't flagged.
5. ONE broadcast. Make it count.

## Primary Analyst Output
{full output from primary analyst}

## Knowledge Base
{full KB from Stage 1}

## Input Document
{primary document}

## Output Format

### Adversarial Challenge
#### Challenged Findings
- [CH1] Re: [F{N}] — {challenge} — Counter-evidence: {file:line shows...}
#### Missed Findings
- [MF1] {what was missed} — {CRITICAL/MAJOR/MINOR} — Evidence: {file:line}
#### Confirmed (survived challenge)
- [F{N}] — confirmed: {why it holds}
```

---

## Orchestrator Self-Challenge (Light and Medium only)

When no adversarial agent is spawned:

1. List all CRITICAL and MAJOR findings from analyst(s).
2. For each: does it cite a specific file and line? Can you verify by reading?
3. Verified → keep. Unverifiable → downgrade to UNVERIFIED.

---

## Pre-Synthesis Verification

1. Run `git status` — working tree should be clean
2. Run `git log --since={session_start_time} --oneline` — check for unexpected commits
3. If violations found → flag in report, consider reverting

## Synthesis — Output by Task Type

Resolution is by **evidence strength**, not majority.

### For `codebase-analysis`:

```markdown
# Analysis: {title} — Iteration {N}
**Date**: {today}
**Scope**: {files/packages}
**Agents**: {list}
**Complexity**: {level}
**Task**: {description}

## Summary
{3-5 sentence executive summary}

## Findings by Severity

### Critical
| # | Finding | Evidence | Source |
### Major
{same}
### Minor
{same}

## Recommendations (max 7, evidence-backed)
| # | Recommendation | Priority | Evidence | Effort |

## Evidence Map
| Finding | Confidence | Basis |

{IF Deep: ## Adversarial Challenges}
{IF Light/Medium: ## Self-Challenge Results}
```

### For `flow-tracing`:

```markdown
# Flow Trace: {title} — Iteration {N}
**Date**: {today}
**Entry point**: {file:function}
**Agents**: {list}

## Summary
{what this flow does, 2-3 sentences}

## Trace
### Step 1: {description}
**Location**: `{file}:{line}`
**Input**: {type/shape}
**Action**: {what happens}
**Output**: {type/shape}

### Step 2: ...

## Data Shape Evolution
| Step | Type | Key Fields | Transformation |

## Failure Paths
| # | At Step | Condition | Result | Handled? | Evidence |

## Observations
{what works, what's fragile, what to improve}
```

### For `document-review`:

```markdown
# Review Report: {filename} — Review {N}
**Date**: {today}
**Reviewed version**: {filepath}
**Agents**: {list}
**Complexity**: {level}
**Focus**: {description}

## Evidence Summary
| Agent | Findings | Verified | Unverified | Downgrades |
**Overall**: {SOUND / CONCERNS / UNSOUND}

## Knowledge Base
{KB output — NOT the input document}

## Agent Reports
{full output from each agent}

## Evidence-Based Resolution
### Verified Concerns
### Logical Concerns
### Unverified Concerns
### Recommendations (max 7)

## Revised Version
{document rewritten with VERIFIED+LOGICAL concerns. UNVERIFIED NOT incorporated.}

## Change Log
| # | Section | Change | Evidence | Source |
```

### For `implementation-planning`:

```markdown
# Implementation Plan: {title} — Iteration {N}
**Date**: {today}
**Agents**: {list}
**Task**: {description}

## Goal
{2-3 sentences}

## Scope
### Files to Create
| File | Purpose |
### Files to Modify
| File | Changes | Lines |

## Implementation Steps
### Step 1: {title}
**Files**: {list}
**TDD**: {test to write first}
**Dependencies**: {prior steps or none}
**Evidence**: {citation}

## Risk Assessment
| # | Risk | Likelihood | Impact | Mitigation |

## Test Plan
| Layer | Tests | Package |
```

### For `refactoring-analysis`:

```markdown
# Refactoring Analysis: {title} — Iteration {N}
**Date**: {today}
**Scope**: {packages}
**Agents**: {list}

## Summary
{2-3 sentences}

## Dead Code
| # | Item | Location | Type | Last Reference | Evidence |

## Duplication
| # | Pattern | Locations | Lines | Suggested Fix |

## Simplification Opportunities
| # | Current | Proposed | Lines Saved | Risk | Evidence |

## Regression Risks
| # | If Changed | Could Break | Test Coverage | Evidence |

## Recommended Sequence
### Phase 1: Safe Removals (no behavioral change)
### Phase 2: Unification (behavioral equivalence)
### Phase 3: Simplification (needs tests first)
```

## File Output

### For `document-review`:
1. Full report → `<dir>/<basename>.review-{N}.md`
2. Revised version → `<dir>/<basename>.v{N+1}.md`
3. Context → `<dir>/<basename>.context.md`

### For all other task types:
1. Full report → `<dir>/<basename>.analysis-{N}.md`
2. Context → `<dir>/<basename>.context.md`

### Context file format (all task types):

```markdown
# Context: {basename}
**Last updated**: {today}
**Iterations**: {N}
**Task type**: {type}

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |

## Open Questions (Unverified)
{items needing investigation}

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
```

## Cleanup

Use `TeamDelete` to clean up `"analyze-team"`.

Report to user:
- **Task type**: {type}
- **Complexity**: {level} ({N} agents)
- **Files created**: {list}
- **Evidence**: {X} VERIFIED, {Y} LOGICAL, {Z} UNVERIFIED
- **Key insight**: {single most important finding}
- **Next step**: {suggestion based on task type}
