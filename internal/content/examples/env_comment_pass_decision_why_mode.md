---
surface: env-comment
verdict: pass
reason: decision-why-ok
title: "NON_HA mode at small-prod — cost trade-off explained"
---

> ```yaml
> # PostgreSQL in mode: NON_HA at Small Production.
> #
> # Trade-off: single replica keeps managed-service cost low at this
> # tier, but node-level failure means a brief outage until the
> # platform restarts the instance. Small-prod workloads tolerate that
> # window in exchange for roughly half the DB cost of HA.
> #
> # The mode flip to HA happens at env 5 and it is one-way —
> # managed-service mode is immutable after creation, so the HA
> # version lives in a separate project with data cutover, not an
> # in-place upgrade. Plan the cutover during a low-traffic window.
> - hostname: db
>   type: postgresql@17
>   mode: NON_HA
> ```

**Why this passes the env-comment test.**
- Names the decision being made (NON_HA over HA at this tier).
- States the trade-off explicitly (cost ↔ availability window).
- Flags the upgrade-path mechanic (mode immutable, separate project,
  data cutover).
- Every yaml field referenced is emitted exactly as quoted.

Spec §5 test: *"Does each service-block comment explain a decision
(scale, mode, why this service exists at this tier), not just narrate
what the field does?"* — yes; it names a decision, its trade-off, and
its downstream consequence.
