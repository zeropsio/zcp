---
id: develop-knowledge-pointers
priority: 3
phases: [develop-active]
title: "Knowledge on demand — where to pull extra context"
---

### Knowledge on demand — pull extra context

- **`zerops.yaml` schema** — `zerops_knowledge query="zerops.yaml schema"`
- **Runtime docs** (build tools, start commands) — `zerops_knowledge
  query="<base stack>"`.
- **Env var keys** — `zerops_discover includeEnvs=true` (`includeEnvValues=true`
  only to troubleshoot).
- **Other platform topics** (scaling, status codes, managed services) —
  `zerops_knowledge query="<topic>"`.
- **Code the app's users send** (snippets, an agent's tool calls, plugins) runs
  in a Zerops Box sandbox, never the app's container:
  `zerops_knowledge uri="zerops://themes/pools-and-box"`, section "Running code
  the app's users send".
