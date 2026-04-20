---
id: bootstrap-classic-verify-deploy-static
priority: 4
phases: [bootstrap-active]
routes: [classic]
runtimes: [static]
steps: [deploy]
title: "Static runtime — deploy verification"
---

### Static runtime — deploy verification

Static services verify by HTTP 200 on `/`, not by logs — nginx serves
`deployFiles` the moment deploy completes.

Deploy pattern (per static runtime in the plan):

```
zerops_deploy targetService="{hostname}" setup="prod"
zerops_verify serviceHostname="{hostname}"
```

`zerops_verify` fetches the project subdomain and asserts HTTP 200.
If no subdomain is enabled yet, run `zerops_subdomain action="enable"
serviceHostname="{hostname}"` first, or reach the service from the ZCP
host over the project's private network.

**Failure patterns unique to static runtimes:**

- **404 on /** — `deployFiles` pointed at a directory without
  `index.html`. Check the path pattern: `./` deploys the repo root;
  `dist/~./` deploys the *contents* of `dist/` at container root
  (the `~./` suffix matters).
- **403 on /** — directory listing is disabled and the target directory
  has no index file, or file permissions are wrong for nginx to read.
- **Stale content** — deploy succeeded but the browser caches. Add a
  cache-busting query or hard-refresh before declaring failure.

No start command runs, so `zerops_logs` only shows nginx access/error
entries — useful for 404/403 triage but not for process-crash debugging
(there is no process to crash).
