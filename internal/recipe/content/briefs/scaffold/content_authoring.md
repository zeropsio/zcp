# Content authoring

Produce your codebase's `zerops.yaml` (with inline comments) + record
5 fragments via `zerops_recipe action=record-fragment`:

- `codebase/<h>/intro` — one paragraph
- `codebase/<h>/integration-guide` — porter-facing numbered items
- `codebase/<h>/knowledge-base` — `**symptom**` bullets with guide ids
- `codebase/<h>/claude-md/service-facts` — port/hostname facts
- `codebase/<h>/claude-md/notes` — operator notes (dev loop, SSH)

## Placement

- Stanza IS in yaml → yaml inline comment
- Absence / alternative / consequence → knowledge-base
- Topology walkthrough → integration-guide
- Dev loop / SSH / curl → claude-md/notes

Why-not-what. Use `because`, `so that`, `otherwise`, `trade-off`.

## Classify before routing

Self-inflicted bugs and pure framework quirks DISCARD. Platform ×
framework intersections → KB with a `zerops_knowledge` citation.
