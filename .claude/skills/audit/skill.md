---
name: audit
description: Verified code audit for one Go package — finds real issues, presents max 3, waits for approval before any edits
---

# Audit: Verified Code Review

Audits one Go package for real issues. Separates detection from evaluation to filter out false positives. Presents only findings worth acting on. No edits without explicit approval.

## Step 1: Determine scope

If user provided a path argument (e.g. `/audit ./internal/init/`), use that package.
If no argument, run `git diff --name-only HEAD~1 HEAD` to find which packages changed — pick the one with most changes.
If no diff and no argument, ask the user.

**Scope is ALWAYS one Go package (one directory).** Never audit multiple packages at once.

## Step 2: Detection (3 agents in parallel, NO EDITS)

Launch three Agent tool calls in a single message. Each agent MUST:
- Read ALL non-test `.go` files in the package completely
- Grep callers/references OUTSIDE the package to understand usage
- Report raw findings with code snippets and grep evidence
- NOT rank, filter, or assign importance — just report what they see

### Shared verification rules (all agents)

Before reporting any finding:
1. Read the ENTIRE file containing the code (not just a snippet)
2. Grep for all callers/references of the affected symbol
3. If the symbol could be an interface implementation, verify
4. If the finding involves removing/changing a param, check all callers

Do NOT report:
- Symbols that look unused but may be called via interface/reflection/embed
- Parameters that satisfy an interface signature
- Patterns that differ from neighbors without proof it's unintentional
- "Simplifications" that would change observable behavior (return values, error messages, log output)

### Agent 1: Reuse
Search the full codebase for existing helpers that could replace code in this package. Flag duplicated logic within the package. Flag hand-rolled operations where a stdlib or project utility exists.

### Agent 2: Quality
Review for inconsistent patterns within the package, copy-paste with variation, exported symbols that should be internal, stale docstrings that don't match implementation, redundant state.

### Agent 3: Efficiency + Safety
Review for redundant computations, hardcoded values that duplicate a constant defined elsewhere, missing error context (bare err returns), TOCTOU patterns, unsafe concurrent access to shared state.

## Step 3: Evaluation (single pass, NO EDITS)

Collect all raw findings from Step 2. For EACH finding, answer these four questions:

**1. IS IT REAL?**
Read the full function and surrounding code. Check for comments explaining the pattern. Check if the pattern appears intentional in context. If you cannot rule out that it's intentional, discard it.

**2. WHAT BREAKS IF WE DON'T FIX IT?**
Describe a concrete scenario: "if someone does X, this will silently Y because Z." If no concrete scenario exists, it's cosmetic — move to appendix.

**3. AUTHOR TEST:**
If the person who wrote this code saw your finding, would they say "good catch" or "you don't understand the context"? If you're not confident they'd say "good catch", move to appendix.

**4. BLAST RADIUS:**
How many files does the fix touch? If >3 files, flag as "larger refactor, needs its own round."

After evaluation, categorize into two buckets:

### Fix now (max 3 findings)
Must satisfy ALL of:
- Concrete break scenario exists
- Author would agree it's a problem
- Fix touches <=3 files

Order by impact (what breaks worst comes first).

### Appendix
Everything else. Summarize in one line: "There are also N minor findings (cosmetic, style, minor inconsistencies). Say 'show appendix' to see them."

## Step 4: Present and STOP

For each "fix now" finding, show:
- **Where**: file:line
- **What's wrong**: 2-3 lines with code snippet
- **What breaks**: the concrete scenario
- **Fix**: 1 line describing the change

Then say: **"Fix these? (yes / pick numbers / skip)"**

**Do NOT proceed to any edits without explicit user instruction.**

## Step 5: Fix (only after user approval)

For each approved finding, in sequence:
1. Make the single change
2. `go build ./...` — must compile
3. `go test ./<package>/... -count=1 -short` — must pass
4. If build or test fails -> revert the change, report why, move to next finding
5. If both pass -> report success, move to next finding

After all approved fixes are done, run `go test ./... -count=1 -short` (full suite).

Summarize: what was fixed, what was skipped, what failed.

If there are appendix items, remind: "There are N more minor findings from the appendix. Say '/audit' again or 'show appendix' to continue."
