# Team Deep Review

Parse `$ARGUMENTS` to extract **reference files** and **task description**.

### Argument parsing rules

1. **Identify all file references**: any quoted path (`'path'` or `"path"`), or any token that resolves to an existing file. These become **reference files**.
2. **Everything else** is the **task description** (focus question).
3. **Pipe separator** (`|`) is also supported: `<filepath> | <task description>`.
4. If no arguments provided, ask the user what to analyze.

### Determine primary document vs reference files

- If exactly ONE reference file is a `.md` plan/design document → it's the **primary document** (the thing being reviewed). Output files are named after it.
- If MULTIPLE `.md` files or NO `.md` files → there is no single primary document. The orchestrator will **create a synthesis document** (`plans/analysis-{descriptive-slug}.md`) as the primary, listing all reference files and the task description. Output files are named after this synthesis document.
- Non-`.md` reference files (`.go`, `.yml`, etc.) are always treated as **reference material**, never as the primary document.

### Examples

```
# Single plan review
/team-deep-review plans/my-plan.md

# Plan + task description
/team-deep-review plans/my-plan.md | compare with actual implementation

# Multiple files + task
/team-deep-review 'plans/workflow-arch.md' 'internal/workflow/engine.go' analyze every command, find overcomplications

# Pure codebase analysis
/team-deep-review 'internal/workflow/' analyze the workflow system end-to-end
→ creates plans/analysis-workflow-system.md as synthesis entry point
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
   - This file map will be distributed to Stage 1 knowledge agents.
5. Detect version state:
   - Extract `<basename>` (filename without extension) and `<dir>` from the primary document path.
   - Check if `<dir>/<basename>.context.md` exists. If yes, read it — this is a continuation.
   - Determine iteration N: count existing `.review-*.md` files for this basename. This review will be `review-{N+1}`.
   - The revised plan output will be `.v{N+2}.md` (or `.final.md` if this is the culminating deep review).
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

### 7. READ-ONLY — MANDATORY (ZERO TOLERANCE)
- You are an ANALYST, not an implementer. You FIND and REPORT problems. You do NOT fix them.
- You MUST NOT write files, edit code, create commits, or create worktrees.
- You MUST NOT use Bash to write files (cat/echo/tee/heredoc to files), run git add, git commit, or any command that modifies the filesystem. Bash is allowed ONLY for read-only commands: git log, git show, git diff, git blame.
- You MUST NOT implement recommendations from other agents. If another agent finds a bug, REPORT it — do not fix it.
- You MUST NOT claim credit for changes you did not make.
- Your ONLY outputs are SendMessage broadcasts to other agents (max 2 messages total).
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

## STAGE 1: Knowledge Assembly

**Purpose**: Build a factual knowledge base BEFORE any analysis. Analysis agents in Stage 2 will receive this KB and MUST cite it. This prevents speculation.

### Team Creation

Use `TeamCreate` with name `"deep-knowledge-team"`.

### 2 Custom Agents (parallel)

Spawn both agents in parallel. They have baked-in knowledge, persistent memory, and safety constraints.

#### Agent: `kb-research` — via `zerops-knowledge` custom agent

Use Agent tool with `subagent_type="zerops-knowledge"`, name `"kb-research"`.

- Has baked-in Zerops platform knowledge + persistent memory from past sessions + knowledge map
- In the prompt, provide: full input document + task description + file map (from setup step 4)
- The agent checks its memory first, then looks up docs and code for specifics
- Produces a combined **Factual Brief** covering both documentation AND codebase facts (replaces both `kb-docs` AND `kb-code` from the old workflow)
- Evidence labels: VERIFIED / DOCUMENTED / MEMORY / EMBEDDED / UNCHECKED

#### Agent: `kb-verifier` — via `platform-verifier` custom agent

Use Agent tool with `subagent_type="platform-verifier"`, name `"kb-verifier"`.

- Has MCP tools (zerops_discover, zerops_verify, zerops_env, zerops_logs, etc.) + SSH access
- Safety-constrained: CANNOT delete, import, mount, deploy, or modify workflows
- In the prompt, provide: full input document + list of testable claims extracted from the document
- Checks its memory for already-verified stable facts (skips re-verification if <30 days old)
- Produces **Verification Results**: CONFIRMED / REFUTED / PARTIAL / UNTESTABLE with raw evidence

### Stage 1 Completion

Wait for both agents to complete. Combine outputs into the **Knowledge Base (KB)**.

The KB has two sections:
1. **Factual Brief** (from `kb-research`) — docs + code facts combined
2. **Verification Results** (from `kb-verifier`) — live platform test results

Use `TeamDelete` to clean up `"deep-knowledge-team"`.

---

## STAGE 2: Evidence-Based Analysis

**Purpose**: Analyze the document using the KB as ground truth. Every finding MUST cite KB entries or independently verify claims.

### CRITICAL: All analysis agents are READ-ONLY

All 4 Stage 2 agents MUST be spawned with `subagent_type="Explore"` and `mode="plan"`. The `mode="plan"` parameter is a structural enforcement — agents cannot make changes without orchestrator approval. This is the primary guardrail; the text-based READ-ONLY instructions in Team DNA are secondary.

**WARNING**: Explore agents have access to Bash. Without `mode="plan"`, they CAN write files via shell commands (heredoc, echo, tee) and create git commits despite the READ-ONLY instruction in their prompt. Always use `mode="plan"` to enforce this structurally.

Only the orchestrator (you) writes output files.

### Team Creation

Use `TeamCreate` with name `"deep-analysis-team"`.

### 4 Analysis Agents (parallel)

Spawn all 4 agents using `subagent_type="Explore"` and `mode="plan"`. Each receives: Team DNA + full document + the complete KB from Stage 1 + task description + previous context.

Agent prompt structure:

```
{Team DNA}

## Your Role: {role name}

{role-specific instructions}

## Input Document

{full contents of the primary document}

## Knowledge Base — GROUND TRUTH

The following was produced by Stage 1 knowledge agents who read the actual docs, code, and tested the live platform. CITE these findings. Do not contradict them without independent verification.

### Factual Brief
{output from kb-research — combined docs + code facts}

### Verification Results
{output from kb-verifier — live platform test results}

## CLAUDE.md Summary

{key conventions from CLAUDE.md}

{IF task description:}
## Task Description — PRIMARY DIRECTIVE
{task description}

This defines YOUR PRIMARY TASK. Orient ALL analysis around this. Use your tools (Read, Grep, Glob) to investigate further as needed.

{IF previous context:}
## Previous Review Context
{contents of .context.md}
Do NOT re-raise resolved concerns. Do NOT re-propose rejected alternatives.

## Instructions

1. Read the ENTIRE document and KB. Understand deeply.
2. Every finding MUST cite evidence:
   - [KB-FACT: topic] — citing factual brief (docs or code facts from kb-research)
   - [KB-PLATFORM: claim] — citing verification results (from kb-verifier)
   - [SELF-VERIFIED: source] — you verified independently by reading code/docs
   - [UNVERIFIED] — you could not verify. State this explicitly.
3. Do NOT make claims without evidence. "I think X might be a problem" is forbidden. "KB-CODE shows X does Y (engine.go:47), which conflicts with Z" is required.
4. Send your complete analysis via SendMessage to "*".
   This is your ONE AND ONLY broadcast. Do not send multiple messages, summaries,
   follow-up reports, or "final consolidated" versions. ONE message with ALL findings.
5. After broadcasting, WAIT SILENTLY. Do not send messages to individual agents.
   Do not validate, confirm, or cross-reference other agents' findings.
6. If the adversarial analyst challenges you, respond with ONE message containing evidence. Then STOP.
7. Total messages allowed: 2 maximum (initial analysis + challenge response).

## CRITICAL CONSTRAINT — REPORT ONLY
You are an ANALYST. Your job is to FIND and REPORT problems with evidence.
- Do NOT write code, tests, documentation, or config changes
- Do NOT create git commits or modify any files via Bash
- Do NOT implement your own recommendations or other agents' findings
- Do NOT produce action plans, implementation timelines, or "phase" roadmaps
- If you discover a fixable problem, describe it with evidence. The orchestrator decides what to fix and when.

## Output Format

### {Role} Analysis
**Assessment**: SOUND | CONCERNS | UNSOUND
**Evidence basis**: {how many of your findings are VERIFIED vs UNVERIFIED}

#### Findings (evidence-based)
- [F1] {finding} — {CRITICAL/MAJOR/MINOR} — {evidence citation}
- [F2] ...

#### Deep Analysis
{2-4 paragraphs grounded in KB evidence}

#### Recommendations
- [R1] {recommendation} — {evidence for why this is needed}
- [R2] ...

#### Unverified Claims
{list anything you suspect but could not verify — explicitly marked}
```

### Role Definitions

| Agent ID | Role | Focus |
|----------|------|-------|
| `correctness` | Correctness & Logic | "Trace data flows and logic end-to-end using KB-FACT codebase sections. Check: does the document's description match what the code actually does? Are there logical contradictions? Missing edge cases? Race conditions? Use KB-FACT docs sections to verify Zerops-specific claims. Every claim about behavior must be checked against KB-FACT or KB-PLATFORM. Do NOT write code or tests. Do NOT implement fixes. REPORT findings only." |
| `architecture` | Architecture & Design | "Evaluate structure using KB-FACT codebase sections. Check: dependency direction, separation of concerns, package boundaries, API surface. Does the architecture match what CLAUDE.md prescribes? Are there violations? Compare the document's proposed/described architecture against the code reality in KB-FACT. Flag discrepancies with evidence. Do NOT write code or refactor. Do NOT create git commits. REPORT findings only." |
| `security` | Security & Resilience | "Security analysis grounded in code: injection vectors (check actual input handling in KB-FACT), auth gaps, secret exposure, error handling paths. Resilience: what does KB-FACT show about failure handling? What does KB-PLATFORM show about actual error behavior? Don't flag theoretical OWASP issues — flag REAL ones visible in the code. Do NOT fix security issues. Do NOT implement other agents' findings. REPORT findings only." |
| `adversarial` | Adversarial Analyst | "Challenge the OTHER analysts' findings AND the document itself. For each claim in the document, use KB to check if it's actually true. For each finding from other analysts, demand they show evidence. Propose CONCRETE failure scenarios grounded in KB-FACT (not theoretical). Your job: find what the KB revealed that nobody else noticed. Identify gaps between what the document SAYS and what KB-FACT/KB-PLATFORM SHOW. Do NOT implement fixes. ONLY challenge and demand evidence." |

### Stage 2 Completion

Wait for all 4 agents to complete. Collect all analysis outputs.

Use `TeamDelete` to clean up `"deep-analysis-team"`.

---

## Pre-Resolution Verification

Before writing the report, verify that analysis agents did not modify the codebase:

1. Run `git status` — working tree should be clean (no uncommitted changes from agents)
2. Run `git log --since={session_start_time} --oneline` — check for unexpected commits
3. If agents made commits or modified files: flag this as a READ-ONLY VIOLATION in the report, note which agent(s) violated, and consider whether the changes should be reverted

## STAGE 3: Evidence-Based Resolution (orchestrator writes this)

**NO VOTING. NO DEBATE.** Resolution is by evidence strength.

### Process

1. Collect all findings from Stage 2.
2. Classify each finding by evidence strength:
   - **VERIFIED**: cited KB entry or self-verified with source reference
   - **LOGICAL**: follows from verified facts via clear reasoning chain
   - **UNVERIFIED**: speculative, no evidence provided
3. For any CRITICAL finding that is UNVERIFIED → YOU (orchestrator) verify it directly by reading code/docs/using tools.
4. For disputed findings (analysts disagree) → check which side has stronger evidence. The side with verified evidence wins. If both have evidence, present both with your assessment.
5. Group findings into action items by evidence strength.

### Report Format

```markdown
# Deep Review Report: {filename} — Review {N}
**Date**: {today}
**Reviewed version**: {filepath}
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: {task description or "General deep review"}
**Resolution method**: Evidence-based (no voting)

---

## Input Document

{complete copy of the reviewed document}

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)
{output from kb-research — combined docs + code facts}

### Platform Verification Results (kb-verifier)
{output from kb-verifier — live platform test results}

---

## Stage 2: Analysis Reports

{all 4 analysis outputs, complete}

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)
| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
{only findings with solid evidence}

#### LOGICAL (follows from verified facts)
| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
{findings that logically follow but weren't directly verified}

#### UNVERIFIED (flagged but not confirmed)
| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
{speculative findings — THESE DO NOT DRIVE CHANGES unless promoted via verification}

### Disputed Findings
| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
{for each disagreement: who has better evidence wins}

### Key Insights from Knowledge Base
{things the KB revealed that the document didn't address — these are often the most valuable findings}

---

## Action Items

### Must Address (VERIFIED Critical + Major)
{only items backed by evidence}

### Should Address (LOGICAL Critical + Major, VERIFIED Minor)
{items with strong reasoning but not directly verified}

### Investigate (UNVERIFIED but plausible)
{items worth checking but not confirmed — flagged for future verification}

---

## Revised Version

{The input document, rewritten incorporating all Must Address and Should Address items. Each change MUST cite the evidence that justifies it. Do NOT incorporate UNVERIFIED findings — list them in "Investigate" instead.}

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | ... | ... | KB-CODE: engine.go:47 shows... | [F1] from Correctness |
| 2 | ... | ... | KB-DOCS: scaling.md confirms... | [F3] from Architecture |
```

## File Output

1. Write full report to `<dir>/<basename>.review-{N}.md`
2. Extract "Revised Version" to `<dir>/<basename>.v{N+1}.md` (or `<dir>/<basename>.final.md` if culminating review)
3. Update/create `<dir>/<basename>.context.md`:

```markdown
# Review Context: {basename}
**Last updated**: {today}
**Reviews completed**: {N}
**Resolution method**: Evidence-based

## Decision Log
| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
{decisions backed by evidence — cite KB entries}

## Rejected Alternatives
| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
{recommendations rejected WITH evidence for why}

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
{concerns resolved with evidence}

## Open Questions (Unverified)
{items that remain UNVERIFIED — need investigation in implementation phase}

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
{VERIFIED sections = HIGH, LOGICAL = MEDIUM, UNVERIFIED = LOW}
```

## Report to User

Files created, evidence summary (how many VERIFIED vs UNVERIFIED findings), key KB insights, and next step suggestion.
