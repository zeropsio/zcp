---
id: develop-knowledge-pointers
priority: 8
phases: [develop-active]
title: "Knowledge on demand"
---

### Knowledge on demand

- `zerops.yaml` schema: `zerops_knowledge query="zerops.yaml schema"`
- Runtime-specific docs: `zerops_knowledge query="<your runtime>"` (e.g.
  `nodejs`, `go`, `php-apache`, `bun`). Match the base stack name of the
  service you are working with.
- Env var keys (no values): `zerops_discover includeEnvs=true`.
  Troubleshooting and need the values? Add `includeEnvValues=true`.
