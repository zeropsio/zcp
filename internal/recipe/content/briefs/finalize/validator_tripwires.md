## Validator tripwires

Finalize gates reject on these — fix at author-time:

- IG item #1 is engine-owned; per-codebase IG items already authored.
  Do NOT touch `codebase/<h>/integration-guide` ids.
- Env READMEs use porter voice. Forbidden: "agent", "sub-agent",
  "zerops_knowledge", "scaffold", "feature phase". The "AI Agent" tier
  label is allowed (it's the literal tier name).
- Env READMEs target 45+ lines (threshold 40; leave margin).
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
