# `workSessionState.enabled: false` field reads as failure on first encounter

**Surfaced:** 2026-05-04, eval suite `20260504-065807`. Three retros
flagged it: `classic-static-nginx-simple`, `greenfield-node-postgres-dev-stage`,
`existing-standard-appdev-only-reminders`. Earlier runs flagged it too
(static-nginx 211240).

**Why deferred:** semantic-framing finding; the `reason` field already
mitigates by spelling out "gated on close-mode not set yet". Real but
not blocking.

## What

After a successful deploy + verify, the response carries:

```json
"workSessionState": {
  "enabled": false,
  "reason": "auto-close gated on close-mode being set"
}
```

Agents read `enabled: false` first and assume something failed. They
recover by reading `reason`, but the field-name fights the meaning —
"enabled" sounds like a feature toggle that's currently off, not "this
specific automated step is waiting on a precondition".

Agent quotes (this run):
- static-nginx: "looked alarming after both the deploy and verify
  passed, but the `reason` field made clear it was just gated on
  `close-mode` not being set yet."
- greenfield-node: "mildly confusing on first read. It says 'enabled:
  false' with a reason about auto-close being gated, which sounds like
  something is broken."

## Trigger to promote

Promote when work-session work is otherwise active, OR if a future
eval has an agent making a wrong-direction decision (e.g., re-running
deploy thinking it failed) before reading the reason.

## Sketch

Three possible directions, increasing cost:

1. **Rename the field.** Replace `enabled: false` with
   `autoCloseStatus: "gated"` (or `"active"`, `"disabled"`). The enum
   carries semantic weight; `false` doesn't. Subtractive — drop the
   bool, use a string discriminator that names what it means.

2. **Reorder the fields.** Put `reason` before `enabled` in JSON. LLMs
   read top-down; reason-first reduces the misread window. Cheaper but
   less complete.

3. **Atom-level priming.** Add a brief atom that names the field shape
   so agents see it during develop-active. Cheapest, doesn't fix the
   field; just trains the agent to expect it.

Option (1) is structurally right and aligns with Phase 2's `kind`
discriminator philosophy — name the state, don't encode it as a bool.

**Update 2026-05-20:** another 4 retros across two eval batches (suites
`20260520-161258`, `20260520-161651`, `20260520-162922`, `20260520-171709`,
`20260520-172213`, `20260520-173405`) — 6/9 agents this round flag the
close-mode gate as a confusing trailing blocker after deploy+verify both
pass. Verbatim quotes converge: "stays open", "wonder why the session is
still open", "auto-close gate is easy to forget". Frequency growing; tips
toward Option (1) (field rename to `autoCloseStatus` enum) as the right
move. Trigger to promote conditions essentially satisfied — anyone
touching work-session state next should fold this in.

## Risks

- Renaming the field is a JSON schema change. Atoms and tests
  reference `workSessionState.enabled` — must be done with the corpus
  + test update.
- Some auto-close logic may depend on the bool branch internally;
  rename should keep the underlying domain type, just rename its JSON
  tag + downstream string.
