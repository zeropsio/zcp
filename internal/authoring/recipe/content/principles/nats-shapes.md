# NATS messaging shapes — pick one, document one

NATS supports two distinct messaging shapes. They are NOT layered or
interchangeable. Pick ONE per recipe; the one the recipe's code uses
is the only one yaml comments and KB / IG / import-comment content
may invoke. Mixing the two — or invoking JetStream framing for a
recipe that uses only core pub/sub — produces fabricated content
that contradicts the running code.

## The two shapes

**Core pub/sub + queue groups.**
- Code: `nc.subscribe('subject', { queue: 'workers' })`. Fire-and-forget
  delivery; queue groups load-balance across replicas; nothing is
  persisted server-side.
- HA story: cluster nodes preserve pub/sub liveness on node loss; **there
  is no stream state to replicate** because no streams exist.
- Use when "fan-out + load balance + at-most-once redelivery" is enough.
  Most showcase recipes pick this shape.

**JetStream streams + durable consumers.**
- Code: `js = jetstream(nc); await jsm.streams.add({ name, subjects });
  js.subscribe('subject', { config: { durable_name: '...' } })`.
  Persistent message store on top of NATS; durable consumers replay on
  reconnect; ack/redeliver semantics.
- HA story: cluster replicates stream state across nodes, surviving node
  loss without losing acked-but-unprocessed messages.
- Use when at-least-once delivery + replay + server-side persistence are
  required.

## Authoring rule

Look at the actual recipe code:

- If you see `jetstream(nc)`, `JetStreamManager`, `jsm.streams.add`, or
  any `js.subscribe` / `js.publish` calls → the recipe uses JetStream.
  Yaml comments and KB content for HA tiers SHOULD invoke the JetStream
  framing (replicated streams, ack/redeliver, durable consumers).
- If you see ONLY `nc.subscribe(...)` / `nc.publish(...)` (no
  `jetstream(...)` import or call) → the recipe uses core pub/sub. Yaml
  comments and KB content MUST NOT invoke JetStream framing —
  "quorum-replicated streams", "JetStream persistence across restarts",
  "durable consumers replay on reconnect" are all stream concepts; the
  recipe doesn't open streams, so claiming the platform replicates them
  is content that contradicts the code.

The HA framing for a core-pub/sub recipe is liveness preservation
(cluster nodes keep accepting connections through node loss; queue-group
load-balancing keeps working). The HA framing for a JetStream recipe is
stream-state replication. Pick the framing that matches the code.

`JET_STREAM_ENABLED=1` is a platform default — JetStream is a server
capability, not an opt-in flag. The recipe opts IN by writing
JetStream client code. A recipe with `JET_STREAM_ENABLED=1` but no
JetStream client calls is core pub/sub and must be documented as
such.
