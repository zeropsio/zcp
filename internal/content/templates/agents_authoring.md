## Recipe authoring (maintainer mode)

`ZCP_AUTHORING` is on — the authoring surface (`zerops_recipe`, `zerops_port`) is available. This is for AUTHORING new recipes for the Zerops corpus, NOT for deploying from an existing recipe (that's bootstrap `route="recipe"` above — available to everyone).

**Two authoring tools — route by what is being authored:**

- **Framework showcase recipe** (Laravel, Next.js, Django, … — ZCP scaffolds the application code itself): `zerops_recipe`.
- **Foreign self-hosted OSS** (Strapi, PostHog, umami, Ghost, Mailpit, … — someone else's software, ported to run on Zerops): `zerops_port`. "create umami recipe" / "port strapi" / "make a recipe for posthog" all mean the PORT flow when the subject is third-party OSS — `zerops_recipe` would wrongly author it from scratch as if it were a framework showcase.

### zerops_recipe (framework showcase)

- `zerops_recipe action="start" slug="..." outputRoot="..."` — opens an authoring session and returns the research-phase guidance plus the parent recipe inline. The engine drives the whole port → author → stitch pipeline through its own responses; follow each response's next action. The tool's errors tell you the exact `outputRoot` shape if you get it wrong.
- `zerops_recipe action="status"` — phase recovery after compaction (the authoring analogue of `zerops_workflow action="status"`).
- Publish a finished recipe via the CLI: `zcp sync recipe {create-repo,push-app,publish}` (needs `gh auth` + `.sync.yaml`).

### zerops_port (OSS port flow)

A port → debug → harden → capture loop: get foreign OSS running WELL on Zerops by iterating against live deploys, then freeze the result as a curated recipe. You research the OSS off-platform; the tool classifies, grades, and records — it never deploys (you run every deploy via the existing tools).

- `zerops_port action="start" target={name, acquisitionHint, dependencies, runtimes}` — recon: returns the PortPlan + feasibility band with zero deploy.
- `zerops_port action="iterate"` — one deploy-debug turn: deploy via `zerops_deploy`/`zerops_import`, read `failureClassification` off the response, pass the observed `failureClass` + `signals` (or `deploySucceeded=true`); the response carries the next fix-class. The loop self-terminates on stall/cap/budget — never keep redeploying past a stop.
- `zerops_port action="harden"` — first call without `rubric` returns the harden plan; re-call with `rubric={...}` reporting what you OBSERVED to score the measured FitCeiling.
- `zerops_port action="capture"` — Stage B (separate, checkpoint by design): emits the honored-tier recipe + drives the curated publish. Requires a feasible scored FitCeiling.
- `zerops_port action="status"` — loop recovery after compaction.
