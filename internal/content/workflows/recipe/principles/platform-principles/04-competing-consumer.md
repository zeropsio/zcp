# platform-principles / competing-consumer

Workers scale horizontally. When there are N worker replicas, each message from the queue is delivered to exactly one of them — not to all N. That is the competing-consumer pattern: replicas subscribe to the same subject, the broker picks one replica per message, and parallelism scales with replica count without re-work or duplicate side effects.

NATS expresses this via **queue groups**. A subscriber that declares a queue name joins the group; the broker round-robins subjects across group members. Subscribers without a queue name receive every message (fanout), which breaks idempotence the moment more than one replica runs.

## Subject, queue, and group

Three concepts, each with a distinct role:

- **Subject** — the topic the producer publishes to (for example `jobs.dispatch`). Stable and declared in the contract.
- **Queue name (group name)** — the string all worker replicas pass when subscribing (for example `workers`). All replicas that pass the same queue name form one group.
- **Group membership** — deliver-once semantics hold within a group. Two different queue names listening to the same subject each receive every message (two groups, each getting its own copy).

## The shape of a correct subscription

The scaffold reads subject and queue from the SymbolContract: `contract.natsSubjects['job_dispatch']` for the subject, `contract.natsQueues['workers']` for the queue name. Every worker replica subscribes with both.

Sketch (library-agnostic):

```
subscribe(
  subject: contract.natsSubjects['job_dispatch'],
  queue:   contract.natsQueues['workers'],
  handler: async (msg) => { await process(msg); msg.ack(); }
)
```

The `queue` parameter is the load-bearing bit. Without it, every replica gets every message.

## The worker runs as its own service (separate codebase)

Workers that do not share a codebase with a host runtime ship as their own Zerops service with hostname `workerdev` (dev) and `workerstage` (stage). The worker's `zerops.yaml` has no `ports`, no `healthCheck`, no `readinessCheck` — workers do not serve HTTP. The `start` command is the queue consumer entrypoint; build and envVariables match prod.

Workers that share a codebase with a host runtime do not have their own hostname; the host target starts both the web server and the queue consumer as processes within the same dev container.

## Pre-attest before returning

Grep your source for a subscription that passes a queue parameter (library forms vary — `subscribe`, `queueSubscribe`, `consumer.pull`, `jetstream.consume`):

```
ssh {host} "grep -rnE 'subscribe.*queue' /var/www/src 2>/dev/null | grep -q ."
```

Inspect the matches: the queue string must equal `contract.natsQueues[role]` character-for-character, and the subject must equal `contract.natsSubjects[<feature>]`.
