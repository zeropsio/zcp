# Zerops platform claims — query before authoring

Recipe prose is read by porters who will deploy this recipe on
their own Zerops project. A claim about how the Zerops platform
behaves is a porter-facing contract. Inventing one is a porter
trap.

## The protocol

Before authoring any prose claim about how a Zerops platform
feature behaves, **call `zerops_knowledge query=<topic>` to read
the canonical guide**. The guide is the source of truth. Do not
extrapolate from the feature's name.

If the guide doesn't say what you want to claim:

1. **Omit the claim.** Recipe prose that doesn't mention the feature
   at all is fine.
2. **Or describe how the porter discovers the answer** (dashboard
   path to inspect, env-var name to read, command to run).

Do not infer behavior from the feature's name. "SERIOUS" doesn't
imply HA; "DEDICATED" doesn't imply HA; "HA" varies per service
family; "subdomain" can mean two different variables; etc.

## Topics that have caused fabrications in past runs

Your training-data instinct on these is unreliable. Treat any prose
claim involving them as "must query the guide first":

- `corePackage` (`LIGHT` / `SERIOUS`)
- `mode: HA` vs `mode: NON_HA` (semantics vary per service family —
  some families don't support HA at all)
- `httpSupport: true` vs `enableSubdomainAccess: true`
- `${zeropsSubdomain}` vs `${zeropsSubdomainHost}` (different
  variables, different scopes, different formats)
- `verticalAutoscaling` fields (`minRam`, `maxRam`, `minCpu`,
  `maxCpu`, `cpuMode`, `minFreeRamGB` — separate concerns, easy to
  conflate)
- `mode: NON_HA` carve-outs at HA tier (some service families like
  `meilisearch` and `object-storage` are single-node-only on Zerops;
  the engine downgrades them at emit. Verify per family before
  claiming a service "is HA" at tier 5.)

The canonical knowledge corpus sits in
`internal/knowledge/themes/` (per-service catalog) and
`internal/knowledge/guides/` (platform-wide topics). The brief's
**Citation map** section names which topics have guides — query
them by name. Topics not in the citation map have no canonical
coverage; default to omission.

## Published-prose URLs — no `<placeholder>` tokens

Zerops UI URLs use real path segments, not template placeholders.
Recipe prose that emits a literal `<id>` or `<placeholder>` to the
porter is a 404 the moment the porter clicks it.

GOOD: "Configure your custom domain in the Zerops dashboard under
your project's **Access** settings."

BAD: "Configure at `https://app.zerops.io/projects/<id>/access`."

If a path is genuinely run-time-known (the porter's project id,
their service id), describe how the porter finds it
("dashboard → your project → Access settings"); don't fabricate a
URL with a placeholder that won't resolve.

## When the brief tells you to omit, omit

Recipe prose under-stating a feature is fine. Recipe prose
over-stating a feature is a deploy-time surprise for every porter
who reads it. The correct fallback when the guide is silent is
silence.
