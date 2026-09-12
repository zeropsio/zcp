# Farm report reader prompt

Fixed prompt for an interactive Claude Code session reading one `farm
report` output. Not scenario-specific — the same text runs over every
failed batch. Advisory only: it summarizes what happened, it never issues
a verdict on whether the run should be trusted.

## Inputs (named explicitly, supplied alongside this prompt)

1. The `farm report` output for the batch (or run) under review.
2. For each row in that report with `result: failed` or `result: blocked`:
   the transcript window immediately around the point that row's check was
   decided — the tool calls and responses spanning roughly the last agent
   turn before the failure became observable, through the point the
   failure or block was recorded.

## Task

For every failed or blocked row in the report, write one paragraph
covering exactly these three things, in this order:

1. **What the agent saw** — the tool result, error, or platform state the
   agent was responding to at that point in the transcript.
2. **What it decided** — the tool call(s) the agent made in response, and
   in one clause, why (if the transcript makes the reasoning legible).
3. **What ZCP told it at that point** — the guidance surfaced by the tool
   response (atom text, error message, recovery hint) that was available
   to the agent at that step, whether or not the agent followed it.

Do not add a fourth sentence assigning blame, proposing a fix, or judging
whether the failure was "reasonable." That synthesis is for whoever reads
this output next, not for this pass.

## Output contract

- One paragraph per failed or blocked row. No paragraph covers more than
  one row.
- Plain prose, no headers, no bullet lists inside a paragraph.
- Advisory framing throughout ("the agent appears to have...", "ZCP's
  response at that point said...") — never "the agent was wrong to..." or
  "this is a bug."
- At most 60 lines total, including row-separating blank lines.
- No scenario-specific text: do not name a scenario id's intent, quote its
  frozen expectation, or explain why the scenario exists. Read only the
  report and the transcript window; describe only what they show.
