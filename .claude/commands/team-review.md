# Team Review

Parse `$ARGUMENTS` to extract **reference files** and **task description**.

### Argument parsing rules

1. **Identify all file references**: any quoted path (`'path'` or `"path"`), or any token that resolves to an existing file. These become **reference files**.
2. **Everything else** is the **task description** (focus question).
3. **Pipe separator** (`|`) is also supported: `<filepath> | <task description>`.
4. If no arguments provided, ask the user what to review.

### Determine primary document vs reference files

- If exactly ONE reference file is a `.md` plan/design document → it's the **primary document** (the thing being reviewed). Output files are named after it.
- If MULTIPLE `.md` files or NO `.md` files → there is no single primary document. The orchestrator will **create a synthesis document** (`plans/analysis-{descriptive-slug}.md`) as the primary, listing all reference files and the task description. Output files are named after this synthesis document.
- Non-`.md` reference files (`.go`, `.yml`, etc.) are always treated as **reference material**, never as the primary document.

### Examples

```
# Single plan review
/team-review plans/my-plan.md

# Plan + focus question
/team-review plans/my-plan.md | is TDD coverage sufficient?

# Multiple files + task
/team-review 'plans/workflow-arch.md' 'internal/workflow/engine.go' compare design vs implementation

# Pure codebase analysis
/team-review 'internal/workflow/' find overcomplications in the workflow system
→ creates plans/analysis-workflow-overcomplications.md as synthesis entry point
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
4. **Codebase pre-exploration** (when task involves codebase analysis):
   - Read the primary document + reference files to identify which packages/areas are relevant.
   - Use Glob and Grep to map ALL relevant source files beyond what was explicitly referenced.
   - Build a **file map**: list of relevant files with one-line descriptions and line counts.
   - **Distribute files across agents**: assign each agent a primary set of files to deep-read (based on their role), plus the shared file map.
5. Detect version state:
   - Extract `<basename>` (filename without extension) and `<dir>` from the primary document path.
   - Check if `<dir>/<basename>.context.md` exists. If yes, read it — this is a continuation.
   - Determine iteration N: count existing `.review-*.md` files for this basename. This review will be `review-{N+1}`.
   - The revised plan output will be `.v{N+2}.md` (since original = v1).
6. Store the task description for inclusion in all agent prompts.

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

### 7. READ-ONLY — MANDATORY
- You are a review agent. You MUST NOT write files, edit code, create commits, or create worktrees.
- Your ONLY outputs are SendMessage broadcasts to other agents.
- The orchestrator writes all report files.

### 8. ZCP-Specific
- Go, TDD mandatory, table-driven tests, max 350 lines/file
- Architecture: cmd → server → tools → ops → platform (strict)
- MCP = "dumb" — data quality + guardrails, not cleverness
- Extend existing mechanisms before creating new ones
- Simplest solution: plain functions > abstractions
- Zerops docs: `../zerops-docs/` is the canonical reference
```

## Phase 1: Knowledge Assembly

Before spawning reviewers, spawn the `zerops-knowledge` custom agent to produce a factual brief. This feeds into all reviewers as ground truth.

### Team Creation

Use `TeamCreate` with name `"review-team"`.

### Agent: `kb-scout` — via `zerops-knowledge` custom agent

1. Use Agent tool with `subagent_type="zerops-knowledge"`, name `"kb-scout"`
   - The agent has baked-in Zerops platform knowledge + persistent memory from past sessions
   - In the prompt, provide: the full input document + task description + file map (from setup step 4)
   - The agent checks its memory first, then looks up only what's missing in docs/code
   - It produces a structured Factual Brief with evidence labels (VERIFIED/DOCUMENTED/MEMORY/EMBEDDED/UNCHECKED)
2. Wait for `kb-scout` to complete and report its Knowledge Brief.
3. The Knowledge Brief becomes ground truth for all Phase 2 agents.

## Phase 2: Evidence-Based Review

### CRITICAL: All review agents are READ-ONLY

All 6 Phase 2 agents MUST be spawned with `subagent_type="Explore"`. Explore agents can only read files, search, and communicate — they CANNOT write files, edit code, create commits, or create worktrees. Only the orchestrator (you) writes output files.

### 5 Review Agents (parallel) + 1 Evidence Challenger

Spawn 6 agents in parallel using `subagent_type="Explore"`. Each receives: Team DNA + document + KB from scout + CLAUDE.md summary + task description + previous context.

### Agent Prompt Structure

```
{Team DNA}

## Your Role: {role name}

You are the {role} reviewer. {role-specific focus below}.

## Input Document

{full contents of the primary document}

## Knowledge Brief — GROUND TRUTH

The following was produced by the Knowledge Scout who read actual docs and code. CITE these facts. Do not contradict them without independently verifying.

{output from kb-scout}

## CLAUDE.md Summary

{key conventions from CLAUDE.md — architecture, TDD, conventions, do-nots}

{IF task description exists:}
## Task Description — PRIMARY DIRECTIVE
{task description}

This defines YOUR PRIMARY TASK. Orient ALL analysis around this. Use your tools (Read, Grep, Glob) to investigate further as needed. Your role-specific perspective still applies.

{IF previous context exists:}
## Previous Review Context
{contents of .context.md}
Do NOT re-raise resolved concerns. Do NOT re-propose rejected alternatives.

## Instructions

1. Read the ENTIRE document and Knowledge Brief carefully.
2. Analyze from your role's perspective.
3. Every finding MUST cite evidence:
   - [KB: section] — citing Knowledge Brief
   - [SELF-VERIFIED: source] — you checked independently
   - [UNVERIFIED] — could not verify, flagged explicitly
4. Use SendMessage to broadcast your findings to "*" using the output format below.
5. After broadcasting, WAIT for the evidence challenger's response.
6. After receiving the challenge, send your final assessment to "*" — either provide the demanded evidence or downgrade your finding to UNVERIFIED.

## Output Format

### {Role} Review
**Assessment**: SOUND | CONCERNS | UNSOUND
**Evidence basis**: {X of Y findings are VERIFIED or SELF-VERIFIED}

#### Findings
- [C1] {finding} (CRITICAL/MAJOR/MINOR) — Evidence: {citation}
- [C2] ...

#### Response to Evidence Challenge
{After receiving challenge — provide requested evidence or acknowledge finding is UNVERIFIED}

#### Recommendations
- [R1] {recommendation} — Evidence for why needed: {citation}
- [R2] ...
```

### Role-Specific Focus

| Agent ID | Role | Focus instruction |
|----------|------|-------------------|
| `architect` | Architect | "Focus on: structure, dependencies, separation of concerns, package boundaries. Use KB codebase facts to verify claims about architecture. Check actual dependency direction against CLAUDE.md. Flag discrepancies between described and actual architecture WITH code references." |
| `security` | Security | "Focus on: input validation, injection vectors, secret handling, auth flows, error info leakage. Use KB to check actual input handling code. Don't flag generic OWASP issues — flag REAL issues visible in the codebase. Every security finding must reference actual code." |
| `qa-lead` | QA Lead | "Focus on: testability, edge cases, TDD compliance, test coverage. Use KB codebase facts to check what's actually tested and what's not. Reference actual test files. Flag untested critical paths with evidence." |
| `dx-product` | DX/Product | "Focus on: developer experience, API ergonomics, error messages, naming clarity. Check actual error messages in code via KB. Would a new team member or LLM understand this? Focus on concrete friction points, not theoretical usability." |
| `zerops-expert` | Zerops Platform Expert | "Focus on: correctness of ALL Zerops-specific claims. Use KB platform facts AND independently read `../zerops-docs/` to verify every Zerops-related assertion. Check: import YAML syntax, zerops.yml fields, service types, scaling behavior, env var handling, deployment semantics. Flag anything that contradicts Zerops docs or known platform behavior. You are the AUTHORITY on Zerops — other reviewers defer to you on platform questions." |
| `evidence-challenger` | Evidence Challenger | "Wait for all 5 reviewers to broadcast findings. Then for EACH reviewer, demand evidence for their TOP 3 findings. Send via SendMessage to '*': 'To {role}: [C1] — show me the code/doc/test that proves this. [C2] — is this VERIFIED or UNVERIFIED? [C3] — what KB entry supports this?' Your job is NOT to disagree — it's to FORCE evidence. Findings without evidence get downgraded to UNVERIFIED. Findings with evidence stay." |

## Execution Flow

1. Spawn `kb-scout` first. Wait for completion.
2. Spawn all 6 review agents in parallel.
3. Agents `architect`, `security`, `qa-lead`, `dx-product`, `zerops-expert` analyze and broadcast findings.
4. Agent `evidence-challenger` waits for all 5 broadcasts, then demands evidence for top findings.
5. The 5 reviewers respond with evidence or downgrade findings.
6. After all agents complete, YOU (orchestrator) synthesize using evidence-based resolution.

## Synthesis (you write this — NO VOTING)

Compile the review report. Resolution is by EVIDENCE STRENGTH, not majority.

```markdown
# Review Report: {filename} — Review {N}
**Date**: {today}
**Reviewed version**: {filepath}
**Team**: kb-scout, architect, security, qa-lead, dx-product, zerops-expert, evidence-challenger
**Focus**: {task description or "General review"}
**Resolution method**: Evidence-based (no voting)

---

## Evidence Summary

| Agent | Findings | Verified | Unverified | Post-Challenge Downgrades |
|-------|----------|----------|------------|--------------------------|
| Architect | ... | ... | ... | ... |
| Security | ... | ... | ... | ... |
| QA Lead | ... | ... | ... | ... |
| DX/Product | ... | ... | ... | ... |
| Zerops Expert | ... | ... | ... | ... |

**Overall**: {SOUND / CONCERNS / UNSOUND} — based on VERIFIED findings only

---

## Input Document

{complete copy of the reviewed document}

---

## Knowledge Brief

{output from kb-scout}

---

## Agent Reports

{full output from each agent, including evidence challenges and responses}

---

## Evidence-Based Resolution

### Verified Concerns (drive changes)
{only concerns backed by evidence — with citations}

### Logical Concerns (inform changes)
{concerns that follow from verified facts}

### Unverified Concerns (flagged for investigation)
{concerns without evidence — listed but do NOT drive changes}

### Evidence Challenger Highlights
{findings that were downgraded after evidence was demanded — shows what was opinion vs fact}

### Top Recommendations (max 7, evidence-backed only)
{each with its evidence citation}

---

## Revised Version

{The input document, rewritten to incorporate VERIFIED and LOGICAL concerns. Each change cites evidence. UNVERIFIED concerns are NOT incorporated — they're listed in "Investigate" appendix.}

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | ... | ... | KB: codebase fact X | [C1] from Security |
| 2 | ... | ... | ../zerops-docs/Y:47 | [C2] from Zerops Expert |
```

## File Output

1. Write the full review report to `<dir>/<basename>.review-{N}.md`
2. Extract ONLY the "Revised Version" section and write to `<dir>/<basename>.v{N+1}.md`
3. Update/create `<dir>/<basename>.context.md` with accumulated context:

```markdown
# Review Context: {basename}
**Last updated**: {today}
**Reviews completed**: {N}
**Resolution method**: Evidence-based

## Decision Log
| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
{decisions backed by evidence}

## Rejected Alternatives
| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
{with evidence for why rejected}

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|

## Open Questions (Unverified)
{items that remain unverified — need investigation}

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
{VERIFIED = HIGH, LOGICAL = MEDIUM, UNVERIFIED = LOW}
```

## Cleanup

Use `TeamDelete` to clean up the review team.

Report to the user: files created, evidence summary (verified vs unverified findings), and suggestion for next step.
