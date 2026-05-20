# Bootstrap route-menu first-call: agents miss the two-phase pattern

- **Surfaced**: 2026-05-20 flow-eval batch (suites `20260520-171709`,
  `20260520-172213`, `20260520-172631`, `20260520-173405`). 4/4 retros
  this batch flagged the bootstrap two-phase start as "non-obvious",
  "caught me off guard", "trap if you skim". Pre-existing across older
  eval runs too — recurring agent friction.
- **Why deferred**: response shape is correct; guidance text exists in
  the `route` parameter schema description. This is a discoverability
  problem, not a correctness bug — agents do recover (next call adds
  `route=`), losing one round-trip. Sister entry
  `e2e-bootstrap-helper-two-phase-wiring.md` covers the e2e test
  harness scope; this entry is the agent-surface counterpart.
- **Trigger to promote**: any of —
  - A response-shape pass is already touching the bootstrap-start
    handler (cheap to fold in).
  - An agent in a high-cost scenario (recipe author, multi-runtime
    bootstrap) loses ≥ 2 turns to the first-call confusion (one cycle
    over the budget threshold).
  - Eval coverage adds a fixture where the agent skips reading the
    response carefully and tries to submit plan content on the first
    call — surface fail is what would tip this from "1-turn cost" to
    "broken flow".

## What agents see

```
zerops_workflow action="start" workflow="bootstrap" intent="..."
→ {
    "kind": "route-menu",
    "routeOptions": [{"route": "classic", ...}, {"route": "adopt", ...}],
    "message": "..."
  }
```

No `sessionId`. The pattern: call `start` AGAIN with `route=` set. The
schema description for `route` says exactly this. The friction is that
agents instinctively expect "start means start" and reach for `route` +
`recipeSlug` on the first call, OR try to submit plan content on the
discovered routes.

Verbatim retro quotes this run:

> "The two-call bootstrap start isn't intuitive. `action=\"start\"
> workflow=\"bootstrap\"` doesn't start a session — it returns a route
> menu." (go)

> "The route-menu two-phase start caught me slightly off guard ... if
> you're pattern-matching on 'start means the thing is started,' you
> might try to submit a plan on the first response and get confused."
> (bun)

> "The two-phase bootstrap start is well-documented but still a trap.
> The first `action=\"start\"` returns `kind: \"route-menu\"`, not a
> session. ... if you're pattern-matching from other workflow tools
> where start means start, you might try to proceed with the plan
> immediately." (rust)

> "Key tell: check the `kind` field. `route-menu` means you're not in
> a session yet; `session-active` means you are." (bun, recovery hint)

## Sketch — candidate fixes (none committed)

1. **Add a top-level `nextAction` field to the route-menu response.**
   `nextAction: { tool: "zerops_workflow", input: { action: "start",
   workflow: "bootstrap", route: "<picked>", intent: "..." } }`. Names
   the protocol verbatim — agents copy-paste rather than reason about
   it. Cheapest UX win; no atom corpus change.

2. **Rename `kind` to `phase` and make the value loud.** Current
   `kind: "route-menu"` reads as a static label. `phase:
   "awaiting-route-pick"` (or similar imperative form) frames the
   state. Costlier — JSON schema + corpus shift.

3. **Emit a single `routeOptions` entry directly if there's an obvious
   unique choice (e.g., empty project + classic).** Skip the menu when
   it has only one option. Removes the friction entirely for the
   common-case path. Higher cost: handler change + new "auto-route"
   policy decision.

Option 1 is the right first move — it's additive to the current
response, doesn't change semantics, costs little to implement, and
addresses the root cause (agent doesn't reach for the schema description
on first scan; needs the verb in the response body).

## Refs

- Handler: `internal/tools/workflow.go::handleBootstrapStart` (route-empty
  discovery branch vs route-set commit branch).
- Schema: `internal/tools/workflow.go::WorkflowInput.Route` jsonschema
  string.
- Response shape: `BootstrapDiscoveryResponse` in `internal/workflow/`.
- Sibling backlog: `e2e-bootstrap-helper-two-phase-wiring.md` (test
  harness scope of the same two-phase shape).
- 2026-05-20 eval retros: 4/4 from suites listed under **Surfaced**.
