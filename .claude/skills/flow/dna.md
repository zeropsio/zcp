# Flow DNA — epistemics only

Project rules (architecture, TDD, tiers, conventions) are NOT restated here —
every subagent gets CLAUDE.md automatically. This is what CLAUDE.md doesn't
cover: how a flow run reasons and when it stops.

- **Evidence grades**: tag every material claim **VERIFIED** (repo evidence,
  cite `[SELF-VERIFIED:file:line]`) · **LOGICAL** (derived from a VERIFIED
  fact — state the inference) · **UNVERIFIED** (asserted, not checked, tag
  `[UNVERIFIED]`); KB facts cite `[KB:<source>]`. Never present a lower grade
  as a higher one.
- **Fix upstream, not downstream**: fix a defect at its source, never patch
  the symptom site. A fix that falls outside the current slice is a material
  decision (see no-silent-scope, below).
- **Verify, don't assume**: Zerops is not Kubernetes. Read the actual
  code/docs/live state before asserting platform behavior.
- **Observed verification**: never report a check you did not see finish.
  Every result is one of 4 states: `passed | failed | blocked | not-run`; a
  failure is reported as failed, never omitted or softened.
- **No silent scope**: a new material decision found mid-BUILD (interface
  change, new assumption, altered acceptance) goes back to SHAPE — never
  improvised in place.
- **Self-contained briefs**: every subagent brief is executable by a
  zero-context reader — state the outcome, allowed scope, and cite spec `§`s
  directly. Never cite the plan file as a source — plans are transient.
- **AFK stop conditions**: halt and write a handoff on any of scope drift · a
  material unknown · an acceptance-criteria change · a repeated unexplained
  check failure.
