# Classification & routing — on-demand lookup

Every fact in the facts log classifies into exactly one of six classes (framework-invariant, framework × platform intersection, framework-quirk, scaffold-decision, operational, self-inflicted) and routes to exactly one destination. The classes + routing table are NOT inlined in this brief — pattern-matching against the `gotcha / ig-item / claude-section / zerops-yaml-comment` examples already shipped in the "Pre-loaded input" section of this brief covers the common cases.

When a specific fact's classification or routing is non-obvious, call the runtime lookup:

```
zerops_workflow action=classify factType=<Type> titleKeywords=<words from the fact's title>
```

The response returns:

- `classification` — one of the six taxonomy classes.
- `defaultRouteTo` — the route a fact of this class defaults to (e.g. framework-invariant → `content_gotcha`; framework-quirk → `discarded`).
- `requiresCitation` — true when the target route is one where the completion gate requires citations (currently `content_gotcha` + `content_ig`).
- `guidance` — short prose explaining the taxonomy rule and any override conditions (e.g. "framework-quirk + self-inflicted may re-route away from `discarded` only with a non-empty `override_reason` in the manifest").

Use the lookup WHEN the fact does not obviously match an annotated example. For obvious cases (a cross-service env-var trap is framework-invariant; a NestJS decorator collision is framework-quirk), skip the lookup and apply the pattern directly.

## Override-reason rule (unchanged from prior taxonomy atoms)

Classes whose default route is `discarded` (framework-quirk, self-inflicted) may be routed elsewhere only when the manifest entry carries a non-empty `override_reason` reframing the fact as porter-facing — e.g. "reframed from scaffold-internal bug to porter-facing symptom with concrete failure mode and platform-mechanism citation". The classify lookup returns this rule in its `guidance` field; re-read it before writing the override.

## Single-routing rule (unchanged)

Every fact appears in exactly one manifest entry and every entry has exactly one `routed_to` value. Cross-surface duplication is handled by cross-reference prose in the other surfaces, not by a second manifest entry. The routing-honesty gate walks every published surface and flags duplicates.
