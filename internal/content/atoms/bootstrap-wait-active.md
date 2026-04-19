---
id: bootstrap-wait-active
priority: 3
phases: [bootstrap-active]
steps: [provision]
title: "Wait for services to reach ACTIVE"
---

### Wait until services are ACTIVE

After `zerops_import` completes, the Zerops engine provisions containers
asynchronously. Subsequent deploy or verify calls against a service that is
still `CREATING` / `STARTING` will fail with a retryable error.

Poll service state:

```
zerops_discover
```

Repeat until every service reports `status: ACTIVE`. The polling itself is
free — no side effects — so a tight loop (every few seconds) is fine.
Production services may take 30–90 seconds to transition; managed services
(databases) usually take longer than runtime services.
