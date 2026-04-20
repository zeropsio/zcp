# Close — phase entry

This phase runs the post-deploy review that qualifies the recipe as shipped. It completes when every substep's predicate holds on the mount; `zerops_workflow action=status` shows the authoritative substep list — read it first, then work each substep in the order the server returns.

## Close substeps (autonomous — no user prompt gates any of them)

1. **code-review** — dispatch the framework code-review sub-agent, apply any CRITICAL or WRONG fixes it returns, redeploy, attest.
2. **close-browser-walk** (showcase) — main agent walks the deployed dev + stage URLs in the browser, confirming every declared feature renders.

Run every substep every time. Autonomy applies: once deploy completes, close begins and runs through to attestation without pausing for permission. If the impulse arises to ask the user "should I run the review?" — treat that as a misread of this phase; the review is the phase.

## Constraints

- **Step gate**: `zerops_workflow action="complete" step="close"` requires every substep the server returned to be attested. Missing any substep — server-side rejection. Read `zerops_workflow action=status` before attempting step-complete.
- **Browser walk is main agent only** — the browser walk at close-browser-walk runs single-threaded from the main agent, same as the deploy-step browser walk. Sub-agents do not open browsers.
- **Single canonical output tree** — the generated recipe tree lives at exactly `{projectRoot}/zcprecipator/{slug}/` with the canonical env folder names (`0 — AI Agent`, `1 — Remote (CDE)`, `2 — Local`, `3 — Stage`, `4 — Small Production`, `5 — Highly-available Production`). That directory IS the deliverable — the publish CLI reads it at exactly that path.
- **Export + publish are user-request-only** — after the close step completes, the workflow response carries reference commands for local export and publish. Both run only when the user explicitly asks. The workflow is done at close; the user (or their orchestrator) decides whether to archive locally or open a PR.
