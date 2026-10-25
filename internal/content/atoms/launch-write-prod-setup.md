---
id: launch-write-prod-setup
priority: 5
phases: [launch-production-active]
title: "Write the production setup block to source zerops.yaml"
references-fields: []
---

### Write the production setup block to source zerops.yaml

Launch needs the production `setup:` block in the source repo's `zerops.yaml` **before** publishing.
Production builds from the same git URL as dev/stage; the prod-specific build/run commands live under
a separate `setup:` entry the launch bundle references.

**Use the proposed block from the response — do not author one from scratch.** When the block is
missing, the launch response carries a concrete `setup:` block DERIVED from the repo's existing
dev/stage setup (same build/run shape, production-adjusted). Two paths, in preference order:

1. **Apply the proposed block**: append it to the top-level `zerops:` list in `zerops.yaml`, review
   the derivation notes (healthCheck, prod-only deps, production env), commit, push to the configured
   remote.
2. **Target an existing setup block**: when the yaml already has a production-suitable setup under a
   different name, re-call the launch workflow with `prodSetupNameOverride="<name>"` instead of
   writing anything.

Include `run.healthCheck` — strongly recommended for production: a container receives traffic only once its readiness check passes, so without one a half-started container can serve requests.

**Production keeps the app's strict framing.** A setup written in a Mate may let Mate's preview pane frame the app: a `frame-ancestors` naming `https://mate.zerops.io`, no `X-Frame-Options`. The proposed block leaves Mate's sources out (`'none'` where nothing else is left). A stage that builds Mate's preview variant of a setup — one that `extends` it and changes only the framing — runs a setup production never builds: re-call with `prodSetupNameOverride="<the setup it extends>"`.

After commit + push, re-call the launch workflow (same start call, same accumulated inputs); the
workflow re-probes and advances once the block resolves.
