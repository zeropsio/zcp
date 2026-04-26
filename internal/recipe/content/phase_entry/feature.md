# Feature phase — implement the showcase feature suite

For hello-world + minimal tiers, this phase is trivial (one endpoint
proving the scaffold). For showcase, this phase implements every
feature-kind from the feature brief.

## Dispatch

1. **Compose dispatch prompt**: `zerops_recipe
   action=build-subagent-prompt slug=<slug> briefKind=feature`. Returns
   the engine-owned recipe-level context block + the feature brief
   body verbatim (feature-kind catalog, `content_extension.md`
   append-semantics rubric, scaffold-phase symbol table, the
   showcase scenario spec when `Plan.Tier == "showcase"`, and — when
   `Plan.FeatureKinds` declares `seed` or `scout-import` — the
   execOnce key-shape concept atom) + closing notes naming the
   self-validate path.

2. **One dispatch** for the whole feature suite — feature sub-agent
   works across every codebase that needs edits. Pass `response.prompt`
   verbatim. Description: `features-<slug>`.

3. **Behavioral verification** per feature: each feature-kind has an
   observable signal (cache-demo emits `X-Cache: HIT`, queue-demo has
   a round-trip status endpoint, etc.). Curl the signal, don't grep
   the source.

4. **Seed data** so the UI shows something on first click-deploy, not
   an empty dashboard. A porter deploying tier 4/5 should see real
   rows, search results, and uploaded objects before creating anything
   manually. The sub-agent picks the seed command shape for its
   framework; gate it on a static execOnce key (seeds are
   non-idempotent by design — see `init-commands-model.md`).

5. **Redeploy affected codebases**: `zerops_deploy` on each codebase
   the feature agent touched. Re-run `zerops_verify`.

6. **Verify initCommands ran** on each redeployed codebase — same
   attestation as scaffold (success line in runtime logs + post-deploy
   data query). If seed data is missing after a green deploy, the
   execOnce key was burned — recover by touching a source file and
   redeploying.

7. **Browser-walk verification** on the rendered UI: use the
   `zerops_browser` tool to navigate to the frontend dev URL and
   exercise each feature tab (list → create → update → delete →
   search → upload). After EVERY `zerops_browser` call, record one
   FactRecord via **`zerops_recipe action=record-fact`** (the v3
   tool — NOT the legacy `zerops_record_fact`) with
   `surfaceHint: browser-verification`. Fill:
   - `topic: <codebase>-<tab>-browser`
   - `symptom: <what you checked and whether the signal was visible>`
   - `mechanism: zerops_browser`
   - `citation: none`
   - `scope: <service>/<tab>`
   - `extra.screenshot: <path>` and `extra.console: <digest>`
   Any console error or blank view is a regression the sub-agent must
   fix before phase close.

8. **Cross-deploy dev → stage** for every codebase the feature
   touched: `zerops_deploy sourceService=<h>dev targetService=<h>stage`
   + `zerops_verify targetService=<h>stage`. Both slots must end
   green.

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

## After complete-phase phase=feature

When `complete-phase phase=feature` (no codebase, after every feature
sub-agent has terminated cleanly) returns `ok:true`, the engine has
recorded the phase as completed AND set the next phase. The next main
action is `enter-phase phase=finalize` — do NOT re-dispatch the
feature sub-agent. The work is done; re-dispatch only re-walks state
in a fresh sub-agent session and risks compounding session-loss
artifacts (run-13's features-2 burned ~50s on phase-realignment
re-walks after exactly this defensive re-dispatch).

If a compaction event leaves you uncertain whether the feature phase
closed, call `zerops_recipe action=status` first — the snapshot's
`current` and `completed` fields tell you whether to proceed to
finalize or re-do feature work.

## Wrapper discipline — what main decides vs sub-agent discovers

The main agent decides: which codebases the feature set spans, the
endpoint path shape, the feature-tab UX surface (list-first? search
bar?). The sub-agent discovers: library choice for the seed/queue/
search client, the exact file layout for its framework, the
framework-idiomatic command shape. Do NOT pre-chew the library
decision in the dispatch wrapper — the sub-agent consults
`zerops_knowledge` and picks.

## What NOT to do here

- Don't add new managed services. The service set was decided at
  research and provisioned at provision. Features extend the
  plan-declared services; they don't extend the plan.
- Don't add codebases. Codebase count is locked at research.
- Don't implement mailer unless the plan declared `mail` as a service
  (it won't for showcase — mail is out-of-scope).
