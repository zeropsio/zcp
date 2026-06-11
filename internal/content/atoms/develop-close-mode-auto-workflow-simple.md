---
id: develop-close-mode-auto-workflow-simple
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [simple]
runtimes: [dynamic, implicit-webserver]
closeDeployModes: [auto]
gitPushStates: [unconfigured, broken]
environments: [container]
multiService: aggregate
title: "close-mode=auto iteration cycle (simple mode)"
---

### Development workflow

Edit code at `/var/www/<hostname>/` for each in-scope simple-mode runtime. Every code change → redeploy; the runtime container auto-starts on deploy (via `run.start`):

```
{services-list:zerops_deploy targetService="{hostname}" setup="prod"
zerops_verify serviceHostname="{hostname}"}
```

Config-only changes still deploy; env-var live timing rules carry the same axis.
