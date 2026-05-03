# Worker KB content shape — multi-replica gotchas

Conditionally appended to the codebase-content brief when this recipe
is the showcase tier and this codebase is a separate worker codebase
(`plan.Tier == "showcase" && cb.IsWorker`). At showcase tier the
worker runs at `minContainers ≥ 2` from tier 4 onwards, so the
multi-replica failure modes are first-class KB concerns.

The corresponding **code shape** taught to the feature sub-agent at
authoring time lives at
`briefs/feature/worker_subscription_shape.md`. That atom carries the
MANDATORY queue-group + drain source-level contract; this atom
covers how to phrase the same trap for the worker's KB readers (the
porter humans / agents who skim the KB before editing the worker).

## Required worker KB gotchas

When the worker runs at tier ≥ small-production, two gotchas MUST
appear in the worker KB:

### Queue-group / consumer-group semantics under multi-replica

The worker has multiple replicas (`minContainers: 2` from tier 4). If
the broker subscription does NOT join a queue group (NATS) or
consumer group (Kafka, RabbitMQ), every replica receives every
message → duplicated work → corrupted state.

The KB stem names the broker, the term "queue group" (or library
equivalent), and "per-replica" or "exactly-once". The body shows the
exact client option that sets the group.

Sample shape:

> *"**Duplicated work when worker scales beyond 1 replica** — Without
> a queue group on the NATS subscription, each replica receives every
> message and runs the handler twice. Pass `queue: 'workers'` to
> `nc.subscribe(...)` so NATS load-balances delivery across replicas
> instead of fan-out."*

### Graceful SIGTERM drain

Rolling deploys send SIGTERM to the outgoing container while the new
one warms up. Without explicit drain, in-flight messages die mid-
handler → poison-pill loops or lost work depending on the broker's
ack semantics. `unsubscribe()` is the wrong call here — it stops
accepting new messages but abandons whatever the iterator hasn't
processed.

The KB stem names SIGTERM or "drain" or "graceful shutdown". The body
carries a fenced code block showing the catch → drain → exit
sequence.

Sample shape:

> *"**In-flight messages dropped on rolling deploy** — Without a
> SIGTERM handler, the outgoing replica exits while the broker still
> has unacked messages assigned to it. Wire a handler that calls
> `await sub.drain()` (NOT `unsubscribe()`) before exiting."*

```typescript
process.on('SIGTERM', async () => {
  await subscription.drain();
  await nc.drain();
  process.exit(0);
});
```

Both items cite the rolling-deploys platform topic (Citation map →
`rolling-deploys`).

## When to HOLD

- The worker is NOT a separate codebase (shares with api/monolith) —
  the items belong to the shared codebase's KB if at all. Skip.
- The worker's `minContainers` stays at 1 even at showcase tier
  (rare; only when the recipe explicitly downscales) — the multi-
  replica gotchas are vacuous. Skip but record a notice.
- Either gotcha is already authored under a symptom-first stem — no
  new bullet needed. The contract is "topics covered", not "exact
  stem text".

## Engine-side enforcement of the source code

The worker's actual source-code shape (`{queue: 'workers'}` on
subscribe + `await sub.drain()` on shutdown) is enforced at
codebase-content `complete-phase` by `gateWorkerSubscription` (see
`internal/recipe/validators_worker_subscription.go`). The teaching
that lands the right code AT WRITE TIME is at feature — see
`briefs/feature/worker_subscription_shape.md`. Your job here is the
KB prose only.
