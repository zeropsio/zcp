---
id: launch-post-checklist
priority: 2
phases: [launch-production-active]
title: "Launch complete — user-owned steps remaining"
references-fields: []
---

### Launch complete — user-owned steps remaining

ZCP has imported services and validated first deploy. The following steps require the user to act in the Zerops dashboard. ZCP cannot perform them (no standing prod access).

**Production L7 exposure baseline — production has NO HTTP access enabled by default.**

`{hostname}_zeropsSubdomain` env vars are populated on every HTTP-eligible runtime (platform always emits them), but the launch composer strips `enableSubdomainAccess` from the production import YAML per P-PROD-2 — so no L7 backend is registered. `curl` to that URL returns 502 until you either attach a custom domain OR explicitly enable the zerops.app subdomain in the prod project's dashboard.

This is intentional, not a bug. Production prefers a custom domain over the `*.zerops.app` developer URL. Pick ONE path below before treating the launch as user-reachable; both paths require dashboard action against the prod project.

1. **Delete the launch-window key** — open Settings → Access Tokens Management and revoke the token named `zcp-launch-<production-project-name>`.
2. **Set external secrets** — open the production project, navigate to each service that needs Stripe/OpenAI/SMTP/etc. values, and set them under Env Variables → Secret. ZCP listed the keys needed in the prior response.
3. **Establish HTTP exposure (MANDATORY before smoke test)** — pick one:
   - **Custom domain (recommended for prod)** — Project → Public Access → HTTP Routing → Add Domain in the prod project's dashboard. Use the DNS records ZCP emitted when the launch input carried `customDomain`. Add at the registrar, click Verify in dashboard.
   - **zerops.app subdomain (explicit opt-in)** — Project → Service → Public Access → Enable Subdomain in the prod project's dashboard. ZCP cannot do this from the source-project MCP session because `zerops_subdomain` is bound to the current project; explicit enable requires either a new MCP session against the prod project (with a project-scoped `ZCP_API_KEY` for that project) or the dashboard click-through.
   - **No public access** — leave the runtime reachable only via internal hostname for backend / worker services. Skip step 4.
4. **Smoke test** — hit the URL from step 3 with a known request shape; check response and logs in dashboard. If step 3 is "no public access", skip directly to step 5 (services reachable only via internal hostname from peer services in the same project).
5. **Pipeline trigger (if launched response had no `pipeline-not-configured-*` blockers)** — push a release tag to deploy: `git tag v1.0.0 && git push --tags` (matching the integration's tag regex, default `^v\d+\.\d+\.\d+$`). If the launched response carried such blockers, configure each runtime via Zerops dashboard first using the deep-link the blocker provides.

After step 5 passes, the launch is complete. For ongoing prod iteration: generate a separate project-scoped `ZCP_API_KEY` (Custom access per project, this one project, Full access) and configure a fresh ZCP MCP session against the production project.
