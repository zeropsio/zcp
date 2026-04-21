# Zerops

Zerops is a PaaS with its own schema — not Kubernetes, Docker Compose, or
Helm.

## Where you are

You're on **ZCP** — either the ZCP container (Ubuntu with `Read`/`Edit`/`Write`,
`zcli`, `psql`, `mysql`, `redis-cli`, `jq`, and network to every service;
runtime code is SSHFS-mounted at `/var/www/{hostname}/`) or a local
machine (no mount; reach managed services over `zcli vpn up {projectId}`).
Runtime app code always runs in Zerops runtime containers, not where you are.

## Starting a task

Any task that concerns a specific service's code — editing, adding,
scaffolding, debugging, deploying, or even planning or discussing the
change — starts a develop workflow:

```
zerops_workflow action="start" workflow="develop" intent="<one-liner>"
```

The develop workflow guides each step, tracks every deploy and verify,
and auto-closes once the services you're working on are deployed and verified.

**Bootstrap first** when there are no services yet or you need to add
infrastructure (new service, mode expansion):

```
zerops_workflow action="start" workflow="bootstrap"
```

When bootstrap closes, start a develop workflow for any code work that
follows.

If infrastructure work comes up mid-develop, start bootstrap — your
develop session stays open and resumes after bootstrap closes.

**Direct tools skip the workflow** — pure operations on existing services
(`zerops_scale`, `zerops_manage` start/stop/restart/reload,
`zerops_subdomain`, `zerops_env`) and read-only queries (`zerops_logs`,
`zerops_discover`, `zerops_events`) auto-apply without a deploy cycle.

If state is unclear (after compaction or between tasks):
`zerops_workflow action="status"` returns the current phase and next action.

Per-service rules (reload behaviour, start commands, asset pipeline) live
at `/var/www/{hostname}/CLAUDE.md`. Read before editing.
