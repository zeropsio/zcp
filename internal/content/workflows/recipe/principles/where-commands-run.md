# where-commands-run

Every command belongs to exactly one world: the target container's world (app runtime + app toolchain + managed-service reachability) or the zcp-orchestrator's world (MCP tools + mount filesystem access). The world the command belongs to determines HOW you invoke it.

## Execution-side commands run via SSH inside the target container

Anything in the app's own toolchain runs via SSH on the target container. The canonical shape is:

```
ssh {hostname} "cd /var/www && {command}"
```

This includes:

- Compilers and build tools: `tsc`, `nest build`, `go build`, `svelte-check`, `tsc --noEmit`
- Test runners: `jest`, `vitest`, `pytest`, `phpunit`
- Linters and formatters: `eslint`, `prettier`
- Package managers: `npm install`, `composer install`, `pnpm install`
- Framework CLIs: `artisan`, `nest`, `rails`, `sveltekit`
- Every git operation: `git init`, `git add`, `git commit`, `git status`, `git log`, `git remote`
- Any app-level `curl`, `node -e`, or `python -c` that hits the running app or a managed service

The target container is where the app's env vars live, where managed services are reachable by hostname, and where the correct runtime and dependency tree are installed. Running any of these in the wrong world produces wrong results (or resource-exhaustion cascades on the orchestrator).

## Orchestrator-side commands read the mount directly

Filesystem reads, edits, and tool calls that do not execute app code run orchestrator-side against the SSHFS mount of `/var/www/{hostname}/`:

- `zerops_*` MCP tools
- `zerops_browser` calls
- Read, Edit, Write, Grep, and Glob against the mount path
- Plain `ls`, `cat`, `find` against the mount for inspection

The mount is a bridge into the container's `/var/www/`. Reading through the mount shows you the same bytes the container sees.

## Dev-server lifecycle uses the dedicated tool

Starting a long-running dev server via `ssh host "cmd &"` holds the SSH channel open until timeout fires because the backgrounded child still owns stdio. Use `zerops_dev_server` for every start, stop, status probe, log tail, and restart — it detaches correctly via `ssh -T -n` + `setsid` with redirected stdio and returns structured result codes.

```
zerops_dev_server action=start hostname={host} command="{start-command}" port={port} healthPath="{path}"
zerops_dev_server action=status hostname={host} port={port} healthPath="{path}"
zerops_dev_server action=logs   hostname={host} lines=40
zerops_dev_server action=stop   hostname={host} port={port}
```

## Quick self-check before you run any command

Ask: "does this command need the app's runtime, env vars, or network reachability to a managed service?" If yes, it runs SSH-side via the canonical shape above. If it only needs to read or edit files, it runs orchestrator-side against the mount. If it starts a long-running process that must survive your tool call, it runs through `zerops_dev_server`.
