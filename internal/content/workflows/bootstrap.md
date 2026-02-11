# Bootstrap: Starting a New Zerops Project

## Overview

Set up a new Zerops project from scratch with services, configuration, and initial deployment.

## Steps

### 1. Discover Current State

Check what already exists in your project:

```
zerops_discover
```

### 2. Define Infrastructure

Create an `import.yml` with your services. Example full-stack setup:

```yaml
services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
    buildFromGit: https://github.com/your/repo
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
```

### 3. Validate Before Import

Always validate your YAML before importing:

```
zerops_validate content="<your yaml>" type="import.yml"
```

### 4. Dry Run

Preview what will be created without actually creating anything:

```
zerops_import content="<your yaml>" dryRun=true
```

### 5. Import Services

Create the services:

```
zerops_import content="<your yaml>"
```

### 6. Track Progress

Monitor the import process:

```
zerops_process processId="<id from import>"
```

### 7. Configure Environment Variables

Set required environment variables for your services:

```
zerops_env action="set" serviceHostname="api" variables=["DATABASE_URL=postgresql://db:5432/app", "CACHE_URL=redis://cache:6379"]
```

### 8. Enable Public Access

Enable subdomain for web-facing services:

```
zerops_subdomain serviceHostname="api" action="enable"
```

## Critical Rules

- Use `http://` for all internal service connections (never `https://`).
- Database and cache services MUST have `mode: NON_HA` or `mode: HA` in import.yml.
- Environment variable cross-references use underscores: `${api_hostname}`, not `${api-hostname}`.
- Ports must be in range 10-65435.

## Common Patterns

**Web + API + DB**: nodejs/go/python API, postgresql DB, optional valkey cache.
**Static frontend + API backend**: static service for SPA, nodejs/go API, postgresql DB.
**Microservices**: multiple runtime services communicating via hostnames over VXLAN.
