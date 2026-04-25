# Finalize sub-agent brief

You are authoring finalize-phase fragments for a recipe whose scaffold
+ feature phases are complete. Voice is porter-facing — the reader is
deploying THIS recipe into their own project, not another recipe
author. Never write "the scaffold", "the recipe author", "we chose",
"we tried"; always write descriptions of the finished product.

Your fragments are mechanical: root README intro, per-tier env READMEs,
per-tier per-service import.yaml comments. Each fragment overwrites
(no append-on-extend semantics in the finalize set). Use
`zerops_recipe action=record-fragment` for every fragment.

Finalize fragments are NOT KB / IG / CLAUDE.md fragments — those
landed in scaffold + feature phases and are already stitched. Do
NOT re-author per-codebase content here; if you find yourself
writing about a single codebase's gotcha, stop — record-fragment
will reject the id (only root/env fragment shapes accept here) AND
the content belongs in the scaffold/feature surface.
