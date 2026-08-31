# StateEnvelope grows without a bound in project service count

**Surfaced**: 2026-08-31, during the release review of `feat/z3-continuation`. Found independently
twice (measured by hand and by a review agent, ~330 vs ~390 B/service against different fixtures —
same threshold). Karel: "dej to do backlogu ten envelope. budu se na to celkove chtit vice
podivat."

**Why deferred**: it degrades rather than breaks, it needs a project with ~21+ services to fire at
all, and the right fix is a product decision about what the lifecycle strip must show — not a
mechanical cap. Not worth blocking a release on; worth deciding properly.

**Trigger to promote**: any of —
- a real project with 20+ services runs `zerops_workflow action="status"` and the result comes back
  truncated, or the z3 lifecycle strip goes blank on one (the first observable symptom);
- the z3 client's strip gets specified or reworked, since the open question below is exactly what
  that work has to answer anyway;
- someone touches `ComposeBodyBudget`, `AppendEnvelope` or `buildServiceSnapshots` for another
  reason — fold this in rather than paying the context twice;
- an owner pass on the envelope design generally (Karel flagged wanting one).

## The problem

An MCP tool response has a **32 KB** ceiling. The code splits it:

| part | budget | enforced by |
|---|---|---|
| guidance atoms | ≤ 24 KB | `workflow.ComposeUnderBudget` — demotes least-important atoms to fit |
| everything else: headers, `renderServices`, plan, **the envelope** | the remaining ~8 KB | **nothing** |

`workflow.AppendEnvelope` appends the fenced `json zcp-envelope` block after the body, and
`StateEnvelope.Services` is built by `buildServiceSnapshots` (`internal/workflow/compute_envelope.go`)
over **every non-system service in the project** — 13 fields each. It is not counted against any
budget, and unlike guidance it has no mechanism to shrink.

Measured (`AppendEnvelope` block alone, one service = ~330 B):

```
 1 service  ->    927 B
10 services ->  3 867 B
20 services ->  7 187 B
30 services -> 10 507 B   <- past the ~8 KB headroom on its own
50 services -> 17 147 B
```

**~21–23 services** and the envelope alone consumes the whole remaining headroom, before
`renderServices` or any header adds a byte.

`internal/workflow/compose.go`'s own comment calls the 24 KB cap "a runtime GUARANTEE here, not just
a test assertion" and records that "the historical standard-mode multi-service envelopes hit 40 KB"
— i.e. this branch put a second uncapped O(N) cost on top of a headroom the code already knew was
tight, for a project size it already knew occurs.

## Blast radius

- Only the three **prose** carriers — `zerops_workflow` `action="status"`, `workflow="develop"
  action="start"`, `action="close"` — because only they compete with the guidance budget. JSON
  carriers (deploy / verify / import / mount) have room to spare.
- **Ungated**: it ships to every user, z3 or not.
- Over the cap the response is truncated client-side. The envelope is appended last, so it is cut
  first: the JSON ends mid-object, the reducer gets nothing, and a large enough overflow starts
  eating the agent's guidance too. No crash, no data loss.

## Why the tests missed it

`TestAppendEnvelope_BlockSizeBudget` (`internal/workflow/envelope_wire_test.go`) pins a **fixed
4-service fixture** (1 703 B) against the 8 KB headroom. It never scales N, so the threshold is
invisible to it. Whatever fix lands should replace that with a test that scales the service count.

## Three options, and the one worth arguing for

| option | effect | cost |
|---|---|---|
| Subtract the envelope's own bytes from `ComposeBodyBudget` before composing | envelope always complete; guidance shrinks under pressure | a large project's agent gets less guidance — though that is exactly what the composer is for |
| Truncate `Services` past some N | envelope bounded | cripples the thing the envelope exists for, arbitrarily |
| **Phase-aware relevance** (below) | small where the pressure is, complete where the list is the point | needs one product decision |

### The phase-aware idea

zcp *does* know which services are being worked on — the envelope already carries it:

```json
"workSession":{"intent":"...","services":["apidev"],"roles":{"apidev":"required"}}
"selfService":{"hostname":"zcp"}
```

And the pressure is not uniform:

| phase | what is actually relevant | work session present? | guidance pressure |
|---|---|---|---|
| `idle` | the whole list — `deriveIdleScenario` classifies incomplete/bootstrapped/adopt/empty from it, and the user picks what to work on | no | low |
| `develop-active` | the work session's services + self | **yes, by name** | high |

So: in `develop-active` carry full snapshots only for the work session's services and self, and
reduce the rest to hostname + status; in `idle` keep the full list. That puts the shrink exactly
where the tension is and takes nothing where the list *is* the information.

**The open question that decides it**: does the lifecycle strip in the z3 client render services
that are not in the work session, and with what detail? That lives in the fork
(`packages/client-runtime/src/zerops/`, and whatever renders the strip). Answer that first — if the
strip shows all services with status during develop, the reduced form has to keep enough for it.

## Related, worth noting while in here

`renderServices` in the prose body is the *other* uncapped O(N) cost sharing the same 8 KB. Any
serious pass at this should account for both, not just the envelope.
