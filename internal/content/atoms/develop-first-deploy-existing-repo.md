---
id: develop-first-deploy-existing-repo
priority: 1
phases: [develop-active]
envelopeDeployStates: [never-deployed]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
multiService: aggregate
title: "Existing repository — run its own dev flow in the dev service"
references-fields: [ops.DeployResult.NotCarried]
---

### A repository that already exists

A repository you did not write — in the working directory, or one the
person asks to run or develop here — runs in the dev service as its
developers run it locally. Scaffold nothing for it.

1. **Read how the repo is developed before you write any setup:**
   `AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`, README, package scripts,
   `Makefile`, `.devcontainer/`, `Procfile`, a compose file — the
   install and dev commands, ports, system packages, host setting.
2. **The dev setup stays minimal:** base, system packages in
   `run.prepareCommands`, `run.ports` with every port a dev server will
   listen on (a port added later takes a redeploy, which replaces the dev
   container), `deployFiles: [.]`, no dependency install in
   `buildCommands`; the first deploy puts its `run.start` under the
   dev-server supervisor. A package manager the run image
   already ships is used as it stands: installing it again globally fails
   with `EEXIST`.
3. **Size it before the first install** from the repo's own hints, else
   for a JavaScript workspace with several apps
   `zerops_scale serviceHostname="{hostname}" minRam=8 minCpu=2`.
4. **Install as its docs say, in the running container:**
   `ssh {hostname} "cd <repo dir> && <the repo's install command>"`
   (`/var/www`, or the code volume's `mountPath`).
   Nothing answers a prompt over SSH: prefix the command with `CI=true`
   and end it with `</dev/null`, or a prompt hangs it until the call
   times out. A restart keeps the installed dependencies; a redeploy replaces the
   container with what git carries (`notCarried` lists what is gone), so
   install again after one.
5. **Start each dev command as a named dev server**, the command exactly
   as the repo runs it:
   `zerops_dev_server action=start hostname="{hostname}" name="<name>" command="<the repo's dev command>" port=<port>`.
6. **The subdomain reaches a dev server only beyond loopback:** use the
   repo's own host or allowed-hosts setting, or a dev mode the repo
   documents for remote use. Never put your own proxy (socat, a Node
   pipe) in front of a dev server or edit the repo's config to widen its
   bind. Without such a setting, tell the person plainly that it listens
   only on loopback — do not improvise.
