---
id: export-07-generate-import
priority: 2
phases: [export-active]
title: "Export — Generate import.yaml with buildFromGit"
---

## Generate — import.yaml with buildFromGit

### Build import.yaml

Use the export YAML from the discover step as a base, enriched with discover data:

```yaml
project:
  name: <project-name>
  # corePackage, envVariables from export API

services:
  # Managed services — no buildFromGit, just infrastructure
  - hostname: <db-hostname>
    type: <db-type>
    mode: <HA or NON_HA>       # from zerops_discover (Mode field)
    priority: 10

  # Runtime services — with buildFromGit
  - hostname: <app-hostname>
    type: <app-type>
    buildFromGit: <repo-url>   # from prepare step
    enableSubdomainAccess: true
    envSecrets:
      <key>: <value>           # envSecrets are rare (APP_KEY etc.) — auto-generated at import time or provided by user. envVariables use dollar-curly references (keys-only discover is sufficient)
    verticalAutoscaling:
      cpuMode: <SHARED or DEDICATED>
      minRam: <value>
      minFreeRamGB: <value>
    minContainers: <value>
    maxContainers: <value>
```

### Field Mapping

| import.yaml field | Source |
|-------------------|--------|
| project.name | Export API |
| project.corePackage | Export API |
| project.envVariables | Export API |
| hostname, type | Discover |
| mode | Discover (Mode field) |
| verticalAutoscaling | Discover (Resources) |
| minContainers, maxContainers | Discover (Containers) |
| enableSubdomainAccess | Discover (SubdomainEnabled) |
| envSecrets | Discover (env vars with isSecret) |
| buildFromGit | Repo URL from prepare step |
| priority | 10 for managed, omit for runtime |

### Present to User

Show the generated import.yaml for review:

"Here's the import.yaml for your project. It will recreate all services with buildFromGit pointing to your repo:

```yaml
<generated import.yaml>
```

Review it — especially:
- Are the service types and versions correct?
- Are the env vars complete? (secrets are included)
- Is the scaling config appropriate?

Want me to adjust anything?"
