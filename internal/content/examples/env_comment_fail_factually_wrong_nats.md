---
surface: env-comment
verdict: fail
reason: factually-wrong
title: "JetStream-style durability claim on core-NATS service (v28 env 5)"
---

> ```yaml
> # NATS 2.12 in mode: HA — clustered broker with JetStream-style durability
> # and at-least-once delivery for job queues.
> - hostname: queue
>   type: nats@2.12
>   mode: HA
> ```

**Why this fails the env-comment test.**
The recipe uses core NATS pub/sub with queue groups (`Transport.NATS` +
`queue: 'jobs-workers'`) — NOT JetStream. JetStream is a separate NATS
subsystem with streams, consumers, and message persistence. The comment
conflates the two. A reader looking up "JetStream" in the NATS docs will
find behavior that does not apply to this deployment.

**Correct shape**: describe the clustered core-NATS behavior without
invoking JetStream.

```yaml
# NATS 2.12 in mode: HA — clustered broker replicated across failure
# domains. Workers subscribe with queue groups so only one replica
# processes each message; at-most-once delivery semantics.
- hostname: queue
  type: nats@2.12
  mode: HA
```

Check every named subsystem or protocol feature against what the recipe
actually uses before committing to the claim.
