# Research-phase tier_decision recording

`tier_decision` facts are **engine pre-emitted** at research phase from
two sources:

1. **`tiers.go::Diff`** — whole-tier deltas (RuntimeMinContainers,
   CPUMode, CorePackage, MinFreeRAMGB, RuntimeMinRAM, ManagedMinRAM,
   RunsDevContainer).
2. **`TierServiceModeDelta`** — per-service mode deltas, applying the
   yaml emitter's downgrade rule (managed-service families that don't
   support HA stay NON_HA at tier 5).

You do NOT record tier_decision facts directly during research. The
engine emits them; the env-content sub-agent at phase 6 extends
auto-derived `TierContext` slots via `fill-fact-slot` when the engine
prose is too thin to author the comment from.

## When to extend `TierContext`

If the engine-derived `TierContext` reads like a mechanical delta
("ServiceMode moves NON_HA → HA") but the audience-voice rationale
(why THIS tier accepts/rejects HA cost) needs human framing, the
env-content sub-agent fills:

```
zerops_recipe action=fill-fact-slot factTopic=<topic>
  fact={ topic: "<topic>", tierContext: "<audience-voice rationale>" }
```

Engine prose stays as the mechanical baseline; the agent extends with
the WHY-for-this-audience layer.
