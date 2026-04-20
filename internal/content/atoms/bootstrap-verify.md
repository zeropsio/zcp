---
id: bootstrap-verify
priority: 5
phases: [bootstrap-active]
steps: [deploy]
title: "Verify the project is reachable"
---

### Verify the project end-to-end

Whole-project verify proves cross-service wiring works.

```
zerops_verify
```

The tool iterates over every non-managed service and aggregates pass/fail.
A failing verify means: a service that reported ACTIVE is not actually
reachable (typically a port-binding or start-command mismatch), OR a
service expects an env var from a peer and the peer has not been deployed
yet.

Do not close the bootstrap session while verify reports failure —
`ServiceMeta` files written against a broken project poison every
downstream consumer.
