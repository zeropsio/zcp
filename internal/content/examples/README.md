# Content-surface examples bank

Annotated examples of reader-facing content on Zerops recipe surfaces,
seeded from:

- `docs/spec-content-surfaces.md` §11 (v28 counter-examples — the bad catalog)
- v38 post-correction content after editorial-review CRIT fixes (good cases)

## Frontmatter schema

Every file under this directory carries a YAML frontmatter header:

```yaml
---
surface: gotcha | ig-item | intro | claude-section | env-comment | zerops-yaml-comment
verdict: pass | fail
reason: folk-doctrine | framework-quirk | library-metadata | scaffold-decision | self-inflicted | wrong-surface | field-narration | invented-number | factually-wrong | platform-invariant-ok | decision-why-ok | concrete-action-ok
title: short descriptive title
---
```

- **surface** — which of the seven content surfaces in spec-content-surfaces.md §2-§7
  this example would belong on if shipped.
- **verdict** — `fail` for anti-patterns; `pass` for content that models the
  surface's contract cleanly.
- **reason** — taxonomy tag from spec §7 (classification failure modes) for fails,
  or an ok-tagged shape for passes.
- **title** — short enough to cite; no line break.

## How the engine uses this bank

1. At writer sub-agent dispatch (deploy.readmes substep), the engine appends
   a "Pre-loaded input block" to the writer brief. That block includes
   `SampleFor(surface, n)` results per surface — 2-3 examples each, mixing
   pass + fail.
2. At generate-step / generate-finalize step entry for the main agent, the
   engine's guidance composer injects examples for the zerops.yaml-comment
   and env-comment surfaces respectively. (Landing in a follow-up pass.)

## Maintenance

When a future v-run surfaces a new fabrication class via editorial-review,
promote the offending prose to a new `gotcha_fail_*.md` (or matching surface)
file and tag with the appropriate `reason`. Post-fix corrected content
becomes a new `_pass_*.md`. The bank grows as the run corpus grows — that
is the design.

**Do NOT delete examples to "shorten" the bank.** Sampling is rotating;
every file participates in some dispatch. Removing a file means the failure
mode it illustrates is no longer a pattern writer sub-agents train against.
