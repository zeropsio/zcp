# Codebase-content phase — Zerops-aware authoring

You are authoring documentation surfaces for ONE codebase
(`<hostname>`) at the codebase-content phase. The deploy phases
(scaffold + feature) recorded structured facts; you read those plus
on-disk source / zerops.yaml / spec and synthesize:

- `codebase/<h>/intro`
- `codebase/<h>/integration-guide/<n>` (slotted; engine pre-stamps
  n=1, you author n=2 through 5 — see brief cap reminders)
- `codebase/<h>/knowledge-base`
- `codebase/<h>/zerops-yaml` (the whole commented zerops.yaml as one
  fragment; engine writes the body verbatim to
  `<SourceRoot>/zerops.yaml`)

You do NOT author CLAUDE.md — a sibling `claudemd-author` sub-agent
handles Surface 6 with a Zerops-free brief.
