---
id: bootstrap-wait-active
priority: 3
phases: [bootstrap-active]
routes: [classic]
steps: [provision]
title: "Wait for services to reach ACTIVE"
---

### Wait until services are ACTIVE

After `zerops_import` completes, the Zerops engine provisions runtime containers
asynchronously. Subsequent deploy or verify calls against a service that is
still `CREATING` / `STARTING` will fail with a retryable error.

Poll service state:

```
zerops_discover
```

Repeat until every service reports `status: ACTIVE`. Production services
take 30–90 seconds to transition; managed services (databases) usually
longer.
