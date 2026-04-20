# Substep: readmes

This substep completes when every per-codebase `README.md` and `CLAUDE.md` has been authored on the SSHFS mount by the writer sub-agent and the writer's completion-shape report confirms coverage across the six content surfaces declared in the content-surfaces contract.

## What this substep accomplishes

README and CLAUDE.md authorship runs now — not during generate — so the gotchas section narrates debug rounds the recipe actually lived through. A speculative gotchas section written during generate produces authenticity failures; post-deploy authoring grounds the content in the facts logged via `zerops_record_fact` during this phase plus the final platform state.

Two files per codebase mount — `README.md` and `CLAUDE.md`. They have different audiences and neither substitutes for the other. `README.md` is published recipe-page content — fragments are extracted to zerops.io/recipes at finalize time; its audience is an integrator porting their own codebase. `CLAUDE.md` is a repo-local dev-loop operations guide — not extracted, not published; its audience is anyone (human or agent) who clones this codebase and needs to work in it.

For a dual-runtime showcase, that is six files:

- `/var/www/appdev/README.md` and `/var/www/appdev/CLAUDE.md`
- `/var/www/apidev/README.md` and `/var/www/apidev/CLAUDE.md`
- `/var/www/workerdev/README.md` and `/var/www/workerdev/CLAUDE.md`

For a minimal single-runtime recipe, two files — `README.md` and `CLAUDE.md` under `/var/www/appdev/`. The canonical output tree for env-folder READMEs and the root README is a separate surface authored by the same writer sub-agent; the writer brief declares the full tree.

## The action at this substep

Compose and transmit the writer sub-agent dispatch prompt, then await its return. The brief is assembled from the `briefs/writer/*` atoms by the Go stitching layer; you do not author brief content in this atom. You do:

1. Confirm every dev and stage target is verified and every feature sweep has passed — the writer needs the final platform state and the deploy phase's fact log as its input material.
2. Confirm `zerops_record_fact` calls during deploy have logged the non-trivial fixes, cross-codebase contracts, and known-trap observations the writer will classify. Under-recording hurts authenticity; over-recording (beyond ~15 calls) tends to mean micro-step narration rather than root-cause mechanisms.
3. Dispatch the writer sub-agent via the Agent tool with the composed brief.
4. Wait for the writer to return its completion-shape report. Do not edit content files while the writer runs.
5. On return, the writer has emitted `ZCP_CONTENT_MANIFEST.json` at `/var/www/` and the per-codebase `README.md` + `CLAUDE.md` files. The manifest is the writer's honesty declaration: every fact from the deploy fact-log is classified and routed to exactly one published surface (or to `discarded` with an override reason). The deploy-step checker walks the manifest plus the authored files at step completion.

## Coverage the step-closer verifies

When you call deploy-step completion after this substep returns, the checker walks every `README.md` and `CLAUDE.md` against the content-surface contract — fragment shape, per-item code-block presence inside the integration guide, predecessor-floor counts on gotchas, cross-README uniqueness, worker production-correctness gotchas (queue-group semantics plus graceful shutdown on SIGTERM) on separate-codebase worker READMEs, CLAUDE.md depth floors. Iterate on the content until every check passes, then the deploy step closes via the `completion` substep.
