# Team Pipeline

`$ARGUMENTS` — same flexible format as `/team-review` and `/team-deep-review`: reference files (quoted paths or existing files) + task description (everything else). Pipe separator (`|`) also supported. If no arguments, ask the user what to analyze.

The first review iteration uses the full `$ARGUMENTS` (including all reference files and task description). Subsequent iterations automatically target the latest `.v*.md` revision — but the task description and reference files carry forward.

## Overview

This command orchestrates iterative review cycles followed by execution. It runs `/team-review` or `/team-deep-review` in a loop, with user confirmation between each stage, then hands off to `/team-execute` with full accumulated context.

## Setup

1. Parse arguments using the same rules as `/team-review` (reference files + task description, primary document detection, synthesis document creation if needed).
2. Extract `<basename>` and `<dir>` from the primary document path.
3. Detect current state:
   - Count existing `.review-*.md` files → determines starting iteration
   - Check for existing `.context.md` → continuation of prior pipeline
   - Find latest `.v*.md` → current version to review
4. Set `current_version` = the filepath provided (or latest `.v*.md` if resuming).
5. Set `iteration` = number of existing reviews + 1.

## Pipeline Loop

### Step 1: Run Review

Execute the `/team-review` command logic (from `team-review.md`) on `current_version`.

This produces:
- `<basename>.review-{iteration}.md` — full review report with revised version
- `<basename>.v{iteration+1}.md` — extracted revised plan
- `<basename>.context.md` — updated accumulated context

### Step 2: Progress Summary

After the review completes, present a progress summary to the user:

```
## Pipeline Progress — Iteration {iteration}

**Current version**: v{iteration+1}
**Reviews completed**: {iteration}

### Issue Tracker
| Category | Review 1 | Review 2 | ... | Current |
|----------|----------|----------|-----|---------|
| Critical | {count} | {count} | ... | {count} |
| Major | {count} | {count} | ... | {count} |
| Minor | {count} | {count} | ... | {count} |

### Trend
{e.g., "V1 had 5 critical issues. V2 resolved 3, introduced 1 new. V3 has 2 remaining."}

### Latest Verdict: {APPROVE / CONDITIONAL APPROVE / REVISE / REJECT}

### Files Created
- `{basename}.review-{iteration}.md`
- `{basename}.v{iteration+1}.md`
- `{basename}.context.md` (updated)
```

### Step 3: Ask User for Next Action

```
What's next?

1. **Review again** — Run another /team-review on v{iteration+1}
2. **Deep review** — Run /team-deep-review on v{iteration+1} (thorough, 2-stage)
3. **Execute** — Run /team-execute on v{iteration+1} (implementation)
4. **Stop** — Save progress, resume later

Choice [1/2/3/4]:
```

**STOP AND WAIT for user response.**

### Step 4: Branch Based on Choice

#### Choice 1: Review Again
- Set `current_version` = `<dir>/<basename>.v{iteration+1}.md`
- Increment `iteration`
- Go to Step 1

#### Choice 2: Deep Review
- Execute `/team-deep-review` command logic on `current_version`
- This produces `.review-{N}.md`, `.final.md` (or `.v{N+1}.md`), updates `.context.md`
- Show progress summary (same format as Step 2 but noting this was a deep review)
- Ask user: **Execute** or **Stop**?
- If Execute → go to Choice 3
- If Stop → go to Choice 4

#### Choice 3: Execute
- Determine the latest plan version:
  - If `.final.md` exists, use it
  - Otherwise use the latest `.v*.md`
- Execute `/team-execute` command logic on the chosen plan file
- The execute team automatically receives `.context.md` with full review history
- After execution completes, show final summary and report location

#### Choice 4: Stop
- Show what files exist and how to resume:
```
## Pipeline Paused

**Current state**: v{N} reviewed, v{N+1} ready
**Files saved**:
- {list all .review-*.md, .v*.md, .context.md files}

**To resume**:
- Review again: `/team-review {dir}/{basename}.v{N+1}.md`
- Deep review: `/team-deep-review {dir}/{basename}.v{N+1}.md`
- Execute: `/team-execute {dir}/{basename}.v{N+1}.md`
- Full pipeline: `/team-pipeline {dir}/{basename}.v{N+1}.md`
```

## Key Behaviors

- **Never auto-advances** — always waits for user confirmation between stages
- **Accumulates context** — each review iteration adds to `.context.md`, so later reviews and execution benefit from all prior analysis
- **Resumable** — if stopped, all files are saved; user can restart at any point with any command
- **Tracks progress** — shows issue count trends across iterations so user can see convergence
- **Flexible** — user can mix review types (standard + deep) in any order
- **Context carries through** — the execute team gets the FULL `.context.md` with all review decisions, rejected alternatives, and resolved concerns
