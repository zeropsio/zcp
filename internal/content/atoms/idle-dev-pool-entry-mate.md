---
id: idle-dev-pool-entry-mate
priority: 1
phases: [idle]
mate: [in]
runtimes: [pool]
environments: [container]
multiService: aggregate
title: "A Swarm pool in the project — a part's dev pool is worked on in a lease"
references-fields: [tools.leaseToolResult.Warm]
---

### A Swarm pool in the project

A Swarm pool (`swarm@1`) is no runtime to bootstrap, adopt or deploy. It is
one of two things — tell which before you touch it:

- **A part's dev pool.** Its zerops.yaml setup starts `zcp-devsup run`, or
  the repository's `.zcp/dev-servers.json` names it as a server's `service`.
  Each branch you work on leases one of its containers, and you work in that
  leased container:
  - `zerops_lease action=status` lists the leases you hold — alias, branch,
    state — and each pool's `warm` container.
  - `zerops_lease action=acquire pool="<pool>" repo="<https clone url>" branch="<branch>"`
    leases a container on the branch, checks the branch out in it and starts
    its dev servers; `pools=["<pool>","<pool>"]` leases one per part for a
    branch that spans several parts. `repo` is needed on a pool's first
    acquire only.
  - `zerops_workflow action="start" workflow="develop" intent="{task-description}" scope=["<pool>"]`
    answers with the loop — work, commit and push, sync, release. In a linked
    Mate a paired pool gets edit guidance; otherwise a paired pool opens a
    deploy session on its stage. An unpaired pool opens no session: nothing
    deploys or verifies a pool itself.
  - Look at each edit on the lease's preview, in seconds:
    `zerops_dev_server action=status hostname="<alias>" name="<server>"`
    names its `previewUrl`, and `zerops_verify serviceHostname="<alias>"`
    opens it in the browser.
- **Infrastructure an app uses.** The app calls its orchestrator with
  `${<pool>_API_TOKEN}`. Leave it to the app: a lease takes one of its
  containers away from it.
