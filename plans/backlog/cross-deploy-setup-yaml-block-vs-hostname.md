# Cross-deploy `setup=<yaml-block-name>` fights agent intuition (`setup=<target-hostname>`)

- **Surfaced**: 2026-05-20 flow-eval `classic-rust-postgres-standard`
  (suite `20260520-173405`). Self-review specifically named the
  `setup` parameter on `zerops_deploy` cross-deploy as "a conceptual
  trap" — intuition wants `setup="appstage"` (target hostname), the
  correct value is `setup="prod"` (the zerops.yaml `setup:` block name).
- **Why deferred**: the deploy guidance does explain this; the friction
  is a single retro and the agent recovered without a failed deploy
  (read the deploy atom carefully). One eval signal isn't enough to
  warrant a schema rename. Recording it because it's a concrete
  agent-intuition fight that could compound if another setup-named
  field (e.g., `setupName` in the launch flow) reinforces the wrong
  mental model.
- **Trigger to promote**: any of —
  - Another retro / eval surfaces the same intuition conflict (2nd
    data point converts this from "one-off" to "pattern").
  - An agent in a real run mis-passes `setup=<hostname>`, deploy
    silently builds from the wrong block, and the failure mode is
    cryptic (e.g., 'unknown command in setup block X' when X is the
    hostname not the yaml-block).
  - A deploy-tool schema pass is already touching the `setup`
    parameter description (cheap to fold a clarification in).

## What agents see

```
zerops_deploy
  sourceService="appdev"
  targetService="appstage"
  setup="prod"    # ← zerops.yaml setup block name, NOT a hostname
```

Agent verbatim (rust):

> "The `setup` parameter on cross-deploy is a conceptual trap. When you
> deploy `sourceService=\"appdev\" targetService=\"appstage\"`, you pass
> `setup=\"prod\"` — that's the *zerops.yaml block name*, not the target
> hostname or environment concept. The guidance explains this clearly,
> but it fights your intuition. You're deploying *to* appstage, so your
> hand wants to type `setup=\"appstage\"`. A future agent who doesn't
> read the deploy atom carefully will get this wrong and either hit an
> error or build from the wrong setup block."

The conflict is multi-step:

1. The tool parameter is named `setup` (single word).
2. The agent's `sourceService` / `targetService` mental model is keyed
   on hostnames.
3. The yaml-block concept (`setup: appdev`, `setup: prod`) doesn't
   carry the word "block" in agent-facing docs.
4. So `setup=` reads to the agent as "setup name", and the only setup
   names they've seen in scope are the hostnames.

## Sketch — candidate fixes (none committed)

1. **Atom-level disambiguation** — the cross-deploy atom (likely
   `develop-deploy-cross.md` or similar) gets a 1-line opener:
   *"`setup=` names the zerops.yaml `setup:` block to build from
   (typically `prod` for cross-deploy to stage). It is NOT a hostname."*
   Cheapest fix; one atom edit.

2. **Schema description tightening** — the `setup` parameter in
   `zerops_deploy.InputSchema` adds the same disambiguation.
   Slightly stronger because it's the first surface the agent reads.

3. **Rename `setup` → `setupBlock` (or `yamlSetup`) in the tool
   schema.** Largest change; touches every call-site and may break
   third-party expectations. Mentioned for completeness; not the
   right first move.

Option 1 + 2 together close the gap cheaply. Defer 3 until the rename
delivers value somewhere else in the schema.

## Risks

- The "setup" naming is also load-bearing in zerops.yaml itself
  (`setup: <name>`). A tool-side rename to `setupBlock` would create a
  ZCP↔platform vocab asymmetry the next agent would have to bridge.
  Don't pursue 3 without parallel platform vocab.

## Refs

- Tool: `internal/tools/deploy*.go` (`setup` parameter).
- Cross-deploy atom: search `internal/content/atoms/` for `deploy.*cross`
  / `develop-deploy-cross*`.
- DM-* invariants (CLAUDE.md): cross-deploy contract pinned by
  `TestValidateZeropsYml_DM3_*` — schema-level constraints already exist;
  the gap is agent-facing prose only.
- 2026-05-20 retro referenced under **Surfaced**.
