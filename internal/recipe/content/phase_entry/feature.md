# Feature phase — implement the showcase feature suite

For hello-world + minimal tiers, this phase is trivial (one endpoint
proving the scaffold). For showcase, this phase implements every
feature-kind from the feature brief.

## Dispatch

1. **Compose brief**: `zerops_recipe action=build-brief slug=<slug>
   briefKind=feature`. Returns the feature-kind catalog (crud,
   cache-demo, queue-demo, storage-upload, search-items), the
   `content_extension.md` append-semantics rubric, the scaffold-phase
   symbol table, and — when `Plan.FeatureKinds` declares `seed` or
   `scout-import` — the execOnce key-shape concept atom.

2. **One dispatch** for the whole feature suite — feature sub-agent
   works across every codebase that needs edits. Description:
   `features-<slug>`.

3. **Behavioral verification** per feature: each feature-kind has an
   observable signal (cache-demo emits `X-Cache: HIT`, queue-demo has
   a round-trip status endpoint, etc.). Curl the signal, don't grep
   the source.

4. **Redeploy affected codebases**: `zerops_deploy` on each codebase
   the feature agent touched. Re-run `zerops_verify`.

## Feature kinds (showcase tier only)

- **crud** — one resource with list+create+show+update+delete
- **cache-demo** — timing + header surfaces a cache hit/miss
- **queue-demo** — endpoint enqueues; worker consumes; result readable
- **storage-upload** — upload file, receive retrievable URL
- **search-items** — full-text search against the crud resource

## Content extends scaffold

Feature sub-agent extends scaffold's fragments via `record-fragment`:
`codebase/<hostname>/integration-guide`, `knowledge-base`, and
`claude-md/*` ids append on extend. When a feature adds a stanza to
`zerops.yaml`, add an inline comment at the same commit — every
stanza must carry a causal "why" comment (finalize validator).

## What NOT to do here

- Don't add new managed services. The service set was decided at
  research and provisioned at provision. Features extend the
  plan-declared services; they don't extend the plan.
- Don't add codebases. Codebase count is locked at research.
- Don't implement mailer unless the plan declared `mail` as a service
  (it won't for showcase — mail is out-of-scope).
