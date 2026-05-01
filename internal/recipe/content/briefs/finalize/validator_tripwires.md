## Validator tripwires

Finalize gates reject on these — fix at author-time:

- IG item #1 is engine-owned; per-codebase IG items already authored.
  Do NOT touch `codebase/<h>/integration-guide` ids.
- Env READMEs use porter voice. Forbidden: "agent", "sub-agent",
  "zerops_knowledge", "scaffold", "feature phase". The tier-0 label
  ("Include Coding Agents", legacy "AI Agent") is allowed — it's the
  literal tier name and the validator strips it before scanning.
- **Tier README intro extract** (between `<!-- #ZEROPS_EXTRACT_START:intro# -->`
  markers) is **1-2 sentences ≤ 350 chars**. The recipe-page UI
  renders this content as the tier-card description; ladder content
  (Shape at glance / Who fits / How iteration works / What you give
  up / When to outgrow) belongs in the tier `import.yaml` comments,
  NOT inside the extract markers. Both reference recipes settle at
  one sentence.
- **Tier `import.yaml` comments** carry the per-decision prose:
  ≤ 40 indented comment lines per tier; 3-5 lines per service block.
  Each block explains a decision (scale / mode / why this service at
  this tier), never narrates what the field does.
- **Codebase IG cap: 5 items per codebase** including engine-emitted
  IG #1. Showcase recipes do not get a higher cap; scope adds breadth
  via more codebases, not more items per codebase. Over-collection is
  the recipe explaining its own helpers (`api.ts`, `sirv` config,
  showcase panel design) — those go in code comments, not IG.
- **Codebase KB cap: 8 bullets per codebase.** Over-collection in KB
  is the signal of scaffold decisions / framework quirks / self-
  inflicted observations leaking in. Apply the spec test: "would a
  developer who read the Zerops docs AND framework docs STILL be
  surprised?" — if no, discard.
- **No fabricated yaml field names.** If a tier import.yaml comment
  references a field path, that path must exist in the yaml below.
  `project_env_vars` (snake_case) is wrong when the schema uses
  `project.envVariables` (camelCase, nested). The validator parses
  the yaml AST and refuses comment-named field paths absent from it.
- **Audience-voice patrol** runs on env import.yaml comments too:
  "recipe author", "during scaffold", "we chose", "for the recipe"
  emit notice. Comments speak about the porter's deployed runtime,
  never about the agent that wrote them.
- yaml comment blocks: one causal word per block, not per line —
  `because`, `so that`, `otherwise`, `trade-off`, em-dash.
- Citations are author-time signals, not render output. Do NOT write
  `Cite \`x\`` or `(cite \`x\`)` literally in any fragment body —
  those phrases produced run-10's `# # (cite \`init-commands\`)` env
  comment noise. The bullet's prose IS the citation.
- env/<N>/import-comments/<hostname> ids accept BOTH codebase
  hostnames AND managed-service hostnames (db, cache, storage). Use
  `<hostname>` as the bare logical name — never the slot suffix
  (`appdev` / `appstage`).
- Self-inflicted litmus: if a fragment summarizes "our code did X, we
  fixed it to do Y", discard. Spec rule 4 — see scaffold brief
  "Self-inflicted litmus" subsection for run-10 anti-patterns.
