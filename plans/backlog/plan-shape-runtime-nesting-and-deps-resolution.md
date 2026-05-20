# Plan shape: `runtime.{bootstrapMode,stageHostname}` nesting + `dependencies[].resolution` discoverability

- **Surfaced**: 2026-05-20 flow-eval batch. Three retros (`classic-bun-simple`
  20260520-172213, `classic-python-postgres-dev-only` 20260520-172631,
  `greenfield-node-postgres-dev-stage` 20260520-162922) flagged the same
  underlying friction: the plan JSON shape passed to `zerops_workflow
  action="complete" step="discover"` has nesting rules and required
  fields that are not obvious from the schema scan alone.
- **Why deferred**: each agent recovered (the relevant guidance does
  document the rules, just buried in long paragraphs); but the
  recurring nature shows the cost is paid per-session. Not blocking —
  candidates for atom-prose / schema-description tightening.
- **Trigger to promote**: any of —
  - Another retro flags the same friction → 4/N pattern.
  - A bootstrap-active atom edit pass is already underway (cheap to
    fold in).
  - A new field is added to `RuntimeTarget` that compounds the nesting
    ambiguity (each new field amplifies the cost of getting the shape
    wrong).

## What agents see — three concrete failure-near-miss patterns

### 1. `bootstrapMode` / `stageHostname` MUST nest inside `runtime`

```jsonc
// WRONG — top-level (silent reject or hard reject depending on validator)
{
  "targets": [{
    "runtime": {"type": "nodejs@22"},
    "bootstrapMode": "standard",
    "stageHostname": "appstage"
  }]
}

// RIGHT — nested in runtime
{
  "targets": [{
    "runtime": {
      "type": "nodejs@22",
      "bootstrapMode": "standard",
      "stageHostname": "appstage"
    }
  }]
}
```

Agent quote (bun): *"the nesting rules are strict and non-obvious:
`bootstrapMode` and `stageHostname` must be inside the `runtime` object,
not flattened to the top level. The guidance says this explicitly and
warns about a hard reject, which is why I got it right, but if you skim
past that paragraph you'll waste a round-trip."*

Agent quote (greenfield-node): *"The plan shape for the discover step
is finicky in ways the example doesn't fully cover. ... `dependencies`
array requires a `resolution` field (`CREATE`, `EXISTS`, `SHARED`) that
isn't in the example's dependency objects — you have to read the schema
description to catch it."*

### 2. `dependencies[].resolution` not shown in the example

```jsonc
// Example shows just type + mode:
{"type": "postgresql@17", "mode": "NON_HA"}

// But validator requires resolution too:
{"type": "postgresql@17", "mode": "NON_HA", "resolution": "CREATE"}
```

Agent quote (python): *"`dependencies[].mode` for managed services (e.g.,
`'NON_HA'`) isn't shown in the plan example — I carried it from the
import YAML conventions mentioned in the provision step guidance. It
worked, but I was guessing at whether `mode` belonged in the plan
dependency or only in the import YAML."*

### 3. `dependencies: null` vs omitted entirely — ambiguous

Agent quote (bun): *"The `dependencies` field being nullable (pass
`null` vs. omitting it) wasn't totally clear either — I passed `null`
and it worked, but I wasn't sure if omitting the key entirely would
have been accepted too."*

## Sketch — candidate fixes (none committed)

1. **Improve the plan-shape atom's example with a complete-shape sample
   including dependencies[].resolution.** The current example shows the
   nesting but omits the required dependency fields. Smallest fix; one
   atom edit. Includes a "fully populated reference shape" block so
   agents copy-paste rather than reason.

2. **Replace the example with a per-mode reference plan.** A dev-only
   plan (no `stageHostname`), a standard plan (with), and an
   adopt-route plan with explicit type-vs-live constraints. Larger atom
   change but disambiguates per-route.

3. **Tighten schema descriptions on `RuntimeTarget`** — add a leading
   sentence: *"`bootstrapMode`, `stageHostname`, and `dependencies` all
   live INSIDE `runtime`. Top-level placement is a hard reject."*
   Cheapest in absolute size; relies on agent reading schema descriptions
   (which they often skim).

Option 1 + 3 together would close the recurring loss. Option 2 is more
work than the eval evidence warrants.

## Refs

- Schema: `internal/workflow/types.go::RuntimeTarget` (nesting source).
- Validator: `internal/workflow/validate.go::ValidateBootstrapPlan` (where
  shape errors get surfaced).
- Atom: `internal/content/atoms/bootstrap-classic-plan-dynamic.md` /
  `bootstrap-classic-plan-static.md` (current plan-shape examples).
- 2026-05-20 retros referenced under **Surfaced**.
