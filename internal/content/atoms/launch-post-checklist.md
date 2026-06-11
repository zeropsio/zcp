---
id: launch-post-checklist
priority: 2
phases: [launch-production-active]
title: "Launch complete — remaining steps to a running application"
references-fields: []
---

### Launch complete — remaining steps to a running application

ZCP has created the production project and imported the services. The runtimes are ACTIVE with EMPTY containers — the application is NOT running until the first release deploys through the production pipeline. Work the steps in order; the `firstRelease` block on the launched response carries the family-specific commands.

**Production L7 exposure baseline — production has NO HTTP access enabled by default.**

`{hostname}_zeropsSubdomain` env vars are populated on every HTTP-eligible runtime (platform always emits them), but the launch composer strips `enableSubdomainAccess` from the production import YAML per P-PROD-2 — so no L7 backend is registered. `curl` to that URL returns 502 until you either attach a custom domain OR explicitly enable the zerops.app subdomain in the prod project's dashboard.

1. **Set external secrets FIRST** — open the production project, navigate to each service that needs Stripe/OpenAI/SMTP/etc. values, and set them under Env Variables → Secret. ZCP listed the keys needed in the prior response. Do this before the first release so the application boots with real values.
2. **Wire the production delivery** — per the `firstRelease.deliveryFamily`:
   - **actions** — run the `prodCd.secret.command`: it reads the staged `ZEROPS_TOKEN_PROD` secret and sets it as the GitHub repo secret (secret-to-secret — no value is pasted and nobody is re-asked for the token). Then write `prodCd.workflowFile` at `.github/workflows/zerops-prod.yml`, commit + push.
   - *Hardening (actions, recommend to the user):* a plain repo secret is effectively readable by ANY collaborator with write access — a workflow edit can exfiltrate it. Where the GitHub plan allows, move `ZEROPS_TOKEN_PROD` to a `production` **environment** secret with required reviewers and pin the deploy job with `environment: production` (environments on private repos need Pro/Team; required reviewers on private repos need Enterprise; public repos get both on any plan).
   - **webhook** — configure the dashboard TAG integration on each production runtime per the `pipeline-not-configured-*` blockers (deep-link + recommended values).
   - **none** — ask the user which of the two to wire; never pick silently.
3. **First release** — `zerops_workflow action="release"` (or `git tag v1.0.0 && git push --tags`, matching the tag regex, default `^v\d+\.\d+\.\d+$`). This is the FIRST production build — the pipeline builds your pushed HEAD and deploys it into the empty runtimes.
4. **Watch it land** — `action="prod-ops"` shows the production services as the release deploys (the launch-window token is read from the staged secret; no launchKey re-send); build logs are in the GitHub Actions run (actions) or the prod project's dashboard (webhook).
5. **Establish HTTP exposure (MANDATORY before smoke test)** — pick one:
   - **Custom domain (recommended for prod)** — Project → Public Access → HTTP Routing → Add Domain in the prod project's dashboard. The dashboard shows the DNS records to create (TXT verification + A/AAAA); add them at the registrar, click Verify. Domain attachment is operator-owned — ZCP does not touch production routing.
   - **zerops.app subdomain (explicit opt-in)** — Project → Service → Public Access → Enable Subdomain in the prod project's dashboard. ZCP cannot do this from the source-project MCP session because `zerops_subdomain` is bound to the current project; explicit enable requires either a new MCP session against the prod project (with a project-scoped `ZCP_API_KEY` for that project) or the dashboard click-through.
   - **No public access** — leave the runtime reachable only via internal hostname for backend / worker services. Skip step 6.
6. **Smoke test** — hit the URL from step 5 with a known request shape; check response and logs in dashboard.
7. **Close the launch window** — once the user confirms production is fully functional, call `zerops_workflow action="confirm-production" productionProjectName="<name>" confirmFunctional=true`. The staged `ZEROPS_TOKEN_PROD` secret is deleted (launch-window calls have nothing left to read); the response carries the token-hygiene note — the token itself stays valid for GitHub Actions, and regenerating it in the dashboard (then refreshing the repo secret in the user's own terminal) invalidates every copy this conversation ever saw.

After step 7, the launch is complete. For ongoing prod iteration: generate a separate project-scoped `ZCP_API_KEY` (Custom access per project, this one project, Full access) and configure a fresh ZCP MCP session against the production project. Every later release ships the same way as step 3 — tag, pipeline builds, production updates.
