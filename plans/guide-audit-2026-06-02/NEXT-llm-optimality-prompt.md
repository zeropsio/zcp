# ZCP guide corpus — LLM-optimality pass (DESIGN + PLAN only, no edits)

CONTEXT (self-contained — assume no memory of any prior session):
- ZCP = single Go binary (MCP server + CLI) for Zerops PaaS. Agent knowledge lives in
  `internal/knowledge/{themes,bases,guides,decisions}/` (fetched on demand via the
  `zerops_knowledge` tool) and `internal/content/atoms/*.md` (composed into per-phase
  briefings by the workflow engine and PUSHED to the agent every turn).
- `guides/` + `decisions/` are gitignored, synced from `zeropsio/docs`
  (`apps/docs/content/guides/*.mdx`) via `zcp sync pull/push guides`. They are NOT in the
  docs website nav, but they DO feed the public `llms-full.txt`. Consumers: (1) the ZCP
  agent [ULTRA-primary], (2) any LLM reading llms-full.txt. Optimize hard for LLMs; ZCP-first
  but NOT ZCP-only.
- Already shipped, do not redo: a content-correctness audit (commit a339a9d5 + zeropsio/docs
  PR #346) and the retirement of the legacy `verify-web-agent-protocol` sub-agent guide
  (commit e72070ef). THIS pass is about FORM/encoding for LLMs, not facts.

PROBLEM: the guides are still human reference prose (they came from human docs), not
LLM-optimal. Three levers, with current data:
- A) Fetch-cost — `zerops_knowledge` returns the WHOLE guide; big ones are 130–218 lines
  (environment-variables 218, scaling 201, zerops-yaml-advanced 182, production-checklist 170,
  deployment-lifecycle 168). One fact floods the agent's context.
- B) Tool-leakage / missing guide↔atom boundary — 11 of 17 guides name `zerops_*` tools, yet
  they also feed public llms-full.txt (no such tools there) AND duplicate what atoms already
  deliver. Working hypothesis to validate or refute: guides should be tool-agnostic FACTS
  (TRIGGER→RULE→FAILURE, portable); atoms should own ZCP-tool ACTION (briefing-delivered).
  The ZCP agent gets action from atoms + fetches guides for facts.
- C) Human-only scaffolding — ASCII box-diagram (networking), 4-language SDK snippet sprawl
  (object-storage, 14 code fences), exhaustive provider tables (smtp). Low signal-per-token
  for an LLM.

TASK — produce a decision-ready PLAN; do NOT edit guides/atoms/code/tests/goldens yet:
1. Decide lever B FIRST (it governs the rest): the guide↔atom boundary policy. For each
   guide that names `zerops_*`, classify → keep tool-agnostic (and say which atom should own
   the moved ZCP-tool action) vs. genuinely guide-appropriate. Concrete, per occurrence.
2. Levers A + C, per guide: what to strip (scaffolding), what to front-load (a structured
   TL;DR an LLM can act on without the body), whether to split / leave to section-retrieval.
   Tag each action: sync-pushable content edit | knowledge-engine change (fetch shape /
   section retrieval) | Aleš-scope.
3. Write the plan to `plans/guide-llm-optimality-<YYYY-MM-DD>.md`: the boundary policy, a
   per-guide action table, phasing (content-pushable first; engine/atom changes flagged
   separately), effort/risk, and ONE concrete gate question.

CONSTRAINTS:
- Don't over-optimize for ZCP-only — guides must stay usable by a non-ZCP LLM.
- Recipe scope (`internal/content/workflows/recipe*`, `internal/recipe/`,
  `internal/tools/workflow_recipe*`, `docs/zcprecipator*`) is a different owner — flag, don't
  plan edits there.
- NEVER `make release` / `make install` / `zcp sync push` without an explicit ask.
- Guides are gitignored — verify any reference claim by grep, not `git diff`.
- Read first: representative guides (environment-variables, scaling, networking,
  object-storage-integration, public-access), develop atoms (develop-verify-matrix,
  develop-first-deploy-verify), `internal/knowledge/documents.go` +
  `internal/tools/knowledge.go` (embed + fetch model), and
  `plans/guide-audit-2026-06-02/REPORT.md` (prior audit + the §form assessment).

OUTPUT: the plan doc + a 1-screen decision summary (boundary policy + top per-guide actions +
the gate question). No edits — we review the plan, then execute precisely.
