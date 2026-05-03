# Worker subscription code shape — queue group + drain are MANDATORY

Loaded at feature when `plan.Tier == "showcase" && plan.HasWorkerCodebase()`.
At showcase tier the worker runs at `minContainers ≥ 2` from tier 4
onwards — multi-replica failure modes are first-class concerns the
moment you write worker source. The KB content shape (how to phrase
the same trap for porter readers) lives at
`briefs/codebase-content/worker_kb_supplements.md`; cross-reference
that atom when you record the worker's KB fragment, but author the
source code right at this phase first.

## Required at every `nc.subscribe(...)` in worker source

```ts
this.sub = this.nc.subscribe(SUBJECT, { queue: 'workers' });
```

The queue name is stable per logical workload — replicas that share
the name share the work. Pick one name per worker; don't randomize
per-replica. A NATS subscription without a queue group fans out each
message to every replica → double-indexing, double-LPUSH, broken
ordering. The queue group is what makes the worker horizontally
scalable.

## Required in shutdown handler

NestJS `OnModuleDestroy` / `process.on('SIGTERM', ...)`:

```ts
await this.sub.drain();   // stop receiving + finish iterator
await this.nc.drain();    // flush pending writes
await app.close();        // run framework lifecycle hooks
```

`unsubscribe()` is NOT a substitute. It stops receiving but abandons
in-flight messages — rolling deploys (tier 4-5) lose events on every
replacement. Always `drain()` before exiting.

## Engine-side enforcement

Both rules are validated at codebase-content phase by
`gateWorkerSubscription` (see
`internal/recipe/validators_worker_subscription.go`). Naked
`nc.subscribe(SUBJECT)` without a `queue:` option, or `unsubscribe()`
inside the shutdown handler without a co-located `drain()`, refuses
`complete-phase` at codebase-content. The validator is a backstop —
the right place to land the queue group + drain shape is here at
feature, when you first author the worker source. Authoring with
this contract in mind avoids the edit-in-place loop that the
codebase-content gate would otherwise force.
