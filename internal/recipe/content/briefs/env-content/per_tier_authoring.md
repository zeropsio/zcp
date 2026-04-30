# Per-tier authoring workflow

You author env-level surfaces (root + per-tier intros + import-comments)
across 6 tiers (0..5). The brief carries:

- Per-tier capability matrix (already computed)
- Cross-tier deltas from `tiers.go::Diff`
- Engine-emitted `tier_decision` facts (one per cross-tier whole-tier
  delta + one per per-service mode change)
- The plan snapshot (codebases + services)
- Parent-recipe pointer (when present)

## Workflow

1. **Author root/intro** — one sentence that frames what the recipe IS
   (stack + scenario), not what Zerops does with it. ≤ 500 chars, no
   markdown headings.
2. **For each tier 0..5**:
   - Author `env/<N>/intro` (1-2 sentences naming who this tier is for
     + the cost/availability tradeoff). ≤ 350 chars, no `## ` headings,
     no `<!-- #ZEROPS_EXTRACT_*` tokens (engine stamps those at stitch).
   - For each service block in the tier import.yaml that has a causal
     story, author `env/<N>/import-comments/<host>` (≤ 8 lines per block).
   - Author `env/<N>/import-comments/project` for the project-level
     block (secrets, corePackage).
3. **Cross-reference parent** when present — read parent's per-tier
   intros and dedup before authoring.

## Voice

Friendly authority, declarative, present-tense, porter-facing. State
the platform mechanism, name the tradeoff, end on the porter signal
that triggers a change. Never "we chose X", never "during scaffold".

PASS — production-tier APP_KEY block:

> *APP_KEY — production encryption key shared across all app and
> worker containers. Critical for session validity when the L7
> balancer distributes requests across multiple containers.*

Mechanism (encryption key shared) → consequence (session validity
under L7 balancing). Two sentences, no field narration.

PASS — tier-0 PostgreSQL block:

> *PostgreSQL — single instance with the smallest managed RAM.
> Snapshots run, but there is no replica — restoring means downtime,
> which is acceptable because tier-0 data is disposable. Priority 10
> ensures it accepts connections before any runtime initCommand fires
> migrations.*

Names the mode (single instance, NON_HA implied), the operational
consequence (manual-restore window), the audience (tier-0 data is
disposable), and the priority's effect (initCommand ordering).

PASS — tier-4 small-prod app block:

> *Small production — minContainers: 2 guarantees two app containers
> at all times, enabling rolling deploys with zero downtime (one
> container serves traffic while the other rebuilds). Zerops
> autoscales RAM within verticalAutoscaling bounds to absorb traffic
> spikes without manual intervention.*

Mechanism (two containers always running) → outcome (rolling deploys
without downtime) → operational reality (autoscaling absorbs spikes).

## One causal teaching per block, deduped across services + tiers

Each block teaches a non-obvious choice. **Skip blocks where every
service in this tier shares the same teaching as the previous tier.**
Tier 0 ships full per-block teaching because there's no prior tier to
inherit from. Tiers 1-5 ship comments only on blocks where this tier
introduces a causal change relative to the previous tier — a mode
flip, a corePackage upgrade, a minContainers bump, a service
appearance/removal. Identical-to-prior-tier service blocks need no
comment.

Cross-service dedup within a tier: if all four managed services share
the same NON_HA-with-snapshots-and-no-replica story at tier 0, the
project-level and per-service blocks each carry the part the porter
needs at THAT scope — the project block names project-wide invariants
(secret scope, corePackage), and each service block names ONLY what's
specific to that service (RAM target, priority, peer dependency).

## Anti-patterns

The run-17 baseline shipped 100-135 indented `#` lines per tier. Every
class of waste shows up in the diff against the reference (~22 lines
per tier). Avoid:

**Field narration** — restating the directive name in prose:

```
# minContainers: 2
# This sets the minimum number of containers to 2.
```

The directive value already says "2"; the comment says nothing the
yaml doesn't. Replace with mechanism + reason:

```
# Two containers always running enables rolling deploys —
# one serves traffic while the other rebuilds, no downtime.
```

**Repetition across services** — copy-pasting the same NON_HA-with-
snapshots paragraph onto db, cache, broker, and search blocks at tier
0. Write it once at the project level (or on the first service block),
and the per-service blocks carry only what's specific to that service.

**Repetition across tiers** — explaining `verticalAutoscaling` on every
single tier. Explain it once, where it first appears or where it
changes. Subsequent tiers' service blocks carry no comment when the
configuration is identical to the prior tier's.

**Tier-promotion narratives** — *"Promote to tier 1 once a human porter
takes over"*, *"Outgrow this when..."*, *"Upgrade from tier N to N+1
when..."*. The contrast between tier yamls is the promotion signal —
the porter scrolling through tiers sees what changes. Don't narrate
the promotion path in prose.

**Authoring voice** — *"we chose X over Y"*, *"during scaffold"*,
*"recipe author decision"*. These are sub-agent process language; the
porter doesn't operate as part of the authoring run.

**"See the X guide" trailing slugs** — *"See: env-var-model guide."*,
*"see `init-commands`"*. The agent's `zerops_knowledge` tool slugs
don't resolve as docs URLs for porters. Cite by inline prose mention
or omit the citation.

**Authoring-tool names in published comments** — `zerops_*`,
`zsc <subcommand>`, `zcli`, `zcp` — the porter operates with framework-
canonical commands, not the agent's tool inventory.

## Per-block density target

3-5 lines per block, every line carries mechanism or reason. Skip
"# Base image" / "# Run command" labels — those say nothing the yaml
doesn't. The 8-line per-block cap is the validator's hard limit;
3-5 is the quality target.

A block under the cap is not the goal. The goal is: every line teaches
a non-obvious choice. If you cannot say something non-obvious about a
block, skip the comment.

## Self-validate

`zerops_recipe action=complete-phase phase=env-content` runs
EnvGates() validators. Fix violations via `record-fragment mode=replace`
until the gate passes.
