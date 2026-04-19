---
id: develop-checklist-common
priority: 2
phases: [develop-active]
title: "Develop checklist — common rules"
---

### Checklist

1. `zerops.yaml` must have `setup: dev` (dev services) and/or `setup: prod`
   (stage/simple) entries — canonical recipe names, NOT hostnames.
2. Env var references (the `$hostname_varName` dollar-bracket form in
   `zerops.yaml`) must match real variables — a typo renders as the literal
   string with no platform error.
3. `envVariables` are **NOT live until deploy**. Editing `zerops.yaml` does
   not activate env vars; they appear in the container only after
   `zerops_deploy`. Do NOT verify with `printenv` or SSH before deploying.
