**Why rejected**: Superseded by structural fix at the preflight layer (commits b26a50d6 + b65afaf2 + 3d51f486 + this Phase-3 sweep, 2026-05-02). The PREFLIGHT_FAILED was a regression from `c53e86b1` (target-hostname yaml lookup, project-root fallback) introduced 2 weeks before; e769c9f7 then documented that wrong behavior as canonical in `develop-deploy-modes.md`. Both are now reverted: yaml resolves from the source service's mount (per `ops.deploySSH` + spec-workflows.md §1132), the project-root fallback is gone in container env, and the atom no longer teaches the workaround. The proposed atom edits below would actively harm now — they'd reintroduce the wrong layout doctrine. Filed here so the same idea isn't re-discovered.

# Cross-deploy zerops.yaml location is undocumented

**Surfaced**: 2026-04-30 — Tier-3 eval `bootstrap-recipe-static-simple` (suite `20260430t154900-9c00ab`). Agent's first cross-deploy `appdev → appstage` failed with `PREFLIGHT_FAILED: zerops.yaml not found: tried /var/www/appstage, /var/www`. Agent had `zerops.yaml` at `/var/www/appdev/zerops.yaml` (source-mount, the obvious place after dev-side scaffolding). Recovery was copying it to `/var/www/` (project root). From the EVAL REPORT (Information gap):

> No documentation stated where zerops.yaml must be located for cross-deploy scenarios. The knowledge base SHOULD document that the deploy tool searches `targetService mount → project root (/var/www/)`, so for cross-deploy the zerops.yaml must be at one of those locations.

**Why deferred**: small atom edit BUT need to confirm the actual search-order contract first (atom must match what the deploy preflight does). Also the cleaner structural fix would be making `zerops_deploy` accept a `--zeropsYaml <path>` flag so cross-deploy can point at the source-side YAML directly, but that's bigger. Document-the-existing-behavior fix is acceptable for now and unblocks future agents.

**Trigger to promote**: any cross-deploy scenario hits the same `PREFLIGHT_FAILED` — currently 1× (S10 Tier-3). Two more would suggest the PREFLIGHT contract is broken, not just the docs.

## Sketch

Two complementary edits:

1. **Atom `develop-first-deploy-promote-stage.md`** (priority 2, fires for standard-mode first-deploy promotion): add a "**Where zerops.yaml lives for cross-deploy**" note pointing out that PREFLIGHT searches `targetService mount → project root` and recommending project root (`/var/www/zerops.yaml`) as the canonical location for standard-mode pairs since both halves' deploys read from there.

2. **Atom `develop-deploy-modes.md`** (DM-3 documentation): add a paragraph on the file-resolution behavior of cross-deploy — explicit about source-mount NOT being searched, project-root being canonical for pair-based deploys.

3. **Optional handler change** (defer further if atoms suffice): `zerops_deploy` accepts `--zeropsYaml <path>` so the preflight can point at a specific file when source-mount is the only place it lives. Solves the migration case where the agent has scaffolded under `/var/www/<source>/` and wants to cross-deploy without copying.

Recommend doing both atom edits in one commit; flag handler change for follow-up only if the atom guidance proves insufficient in a later eval.

## Risks

- Need to verify the actual PREFLIGHT search order in `internal/ops/deploy_preflight.go` or the SSH preflight path before writing the docs — if I'm wrong about the order, the atom misleads.
- Two atoms touching cross-deploy — risk of inconsistent prose. Cross-link them.

## Refs

- Tier-3 triage: `/Users/macbook/Documents/Zerops-MCP-evals/2026-04-30/TIER3-TRIAGE.md` §S10 Information gaps
- Per-scenario log: `tier3/bootstrap-recipe-static-simple/log.jsonl` (PREFLIGHT_FAILED + recovery)
- DM-3 invariant: `docs/spec-workflows.md §8 DM-3` — cross-deploy deployFiles is over post-buildCommands filesystem
- Atoms to update: `internal/content/atoms/develop-first-deploy-promote-stage.md`, `internal/content/atoms/develop-deploy-modes.md`
