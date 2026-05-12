---
id: launch-post-checklist
priority: 2
phases: [launch-production-active]
title: "Launch complete — user-owned steps remaining"
references-fields: []
---

### Launch complete — user-owned steps remaining

ZCP has imported services and validated first deploy. The following steps require the user to act in the Zerops dashboard. ZCP cannot perform them (no standing prod access).

1. **Delete the launch-window key** — open Settings → Access Tokens Management and revoke the token named `zcp-launch-<production-project-name>`.
2. **Set external secrets** — open the production project, navigate to each service that needs Stripe/OpenAI/SMTP/etc. values, and set them under Env Variables → Secret. ZCP listed the keys needed in the prior response.
3. **Attach custom domain** (if requested at scope time) — Project → Public Access → HTTP Routing → Add Domain. Use the DNS records ZCP emitted; add them at the registrar; click Verify in dashboard.
4. **Verify production smoke test** — hit the live URL with a known request shape; check response and logs in dashboard.

After step 4 passes, the launch is complete. For ongoing prod iteration: generate a separate project-scoped `ZCP_API_KEY` (Custom access per project, this one project, Full access) and configure a fresh ZCP MCP session against the production project.

5. **Pipeline trigger (if launched response had no `pipeline-not-configured-*` blockers)** — push a release tag to deploy: `git tag v1.0.0 && git push --tags` (matching the integration's tag regex, default `^v\d+\.\d+\.\d+$`). If the launched response carried such blockers, configure each runtime via Zerops dashboard first using the deep-link the blocker provides.
