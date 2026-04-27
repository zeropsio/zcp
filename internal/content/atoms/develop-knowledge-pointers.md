---
id: develop-knowledge-pointers
priority: 3
phases: [develop-active]
title: "Knowledge on demand — where to pull extra context"
---

### Knowledge on demand — where to pull extra context

When the embedded guidance is not enough, these are the canonical lookups:

- **`zerops.yaml` schema / field reference**:
  `zerops_knowledge query="zerops.yaml schema"`
- **Runtime-specific docs** (build tools, start commands, conventions):
  `zerops_knowledge query="<your runtime>"` — e.g. `nodejs`, `go`,
  `php-apache`, `bun`. Match the base stack name of the service you are
  working with.
- **Env var keys** (no values — safe by default):
  `zerops_discover includeEnvs=true`. Add `includeEnvValues=true` only
  for troubleshooting.
- **Infrastructure changes** (shared storage, scaling rules, nginx
  fragments): see `develop-platform-rules-common`. For dev → standard
  mode expansion, start a new bootstrap session with `isExisting=true`
  on the existing service plus a `stageHostname` for the new stage pair.
- **Platform constants** (status codes, managed service categories,
  runtime classes): `zerops_knowledge query="<topic>"` — examples:
  `"service status"`, `"managed services"`, `"subdomain"`.
