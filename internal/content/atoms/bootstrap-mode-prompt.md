---
id: bootstrap-mode-prompt
priority: 3
phases: [bootstrap-active]
routes: [classic]
steps: [discover]
title: "Confirm mode — dev / standard / simple per service (classic route)"
references-fields: [workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.StageHostname]
---

### Confirm mode per service

Every runtime service needs a **mode**; confirm with the user before
submitting the plan.

- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.
  The app runs ONLY via `zerops_dev_server` (no supervised `run.start`),
  so the public URL **502s after any container cycle** until restarted.
  Pick dev for hands-on iteration with no durable end-state — never as the
  final state of a service the user wants to stay reachable.
- **standard** — dev + stage pair. The envelope reports `stageHostname`
  on the dev snapshot and a separate snapshot with `mode: stage` for
  the stage service.
  - **Plan MUST set `stageHostname` explicitly on every standard target**
    (e.g. `{"runtime": {"devHostname": "appdev", "type": "...", "bootstrapMode": "standard", "stageHostname": "appstage"}}`).
    A submission omitting `stageHostname` rejects with an actionable
    error pointing back to `bootstrapMode="dev"` if a single container
    was the actual intent.
- **simple** — single always-on container: `run.start` runs the real app
  on every deploy, platform-supervised so it survives container cycles.
  Pick simple for a durable single service the user wants reachable at a
  URL (web app, API, dashboard) and for background workers.
- **stage** — never bootstrapped alone; it is the stage half of a
  standard pair.

Choose on the OUTCOME, not iteration habit: a service that should stay
reachable → **simple** (or **standard** for a dev+stage split); a scratch
space for hands-on iteration with no durable end-state → **dev**. For a
"build me X" request that ends at a URL, **simple** is the safe default —
dev's transience is a footgun for anything left running. The plan commits
the mode when you submit it; the envelope then exposes it as
`ServiceSnapshot.Mode`. Changing mode later requires a mode-expansion
bootstrap session, surfaced in develop when actionable.
