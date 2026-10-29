---
id: pool-migrate-move
priority: 3
phases: [pool-migrate-active]
title: "Move a part's work through its origin"
references-fields: []
---

### Move a part's work through its origin

A pool's leased container starts empty and takes its branch from the origin,
so the part's work moves by git, and what the dev service holds that the
origin lacks stays behind with it. Every step's answer carries `census` — the
dev service's repository as it is now, its main checkout first and then every
worktree — and `workBranch`, the branch a lease of the pool checks out.
`census.clean: true` means nothing is left to move.

Each item the census names, and how to keep it (you commit and push; the
call commits nothing):

| census | what it is | to keep it |
|---|---|---|
| `trees[].staged`, `modified`, `untracked`, `conflicted` | uncommitted work in that checkout | commit it on a branch you keep, then push it |
| `trees[].inProgress` | a merge, rebase, cherry-pick, revert or bisect left unfinished | finish it (`--continue`) or abort it (`--abort`) |
| `trees[].detachedCommits` | commits only a detached HEAD holds | `git switch -c <branch>` in that checkout, then push it |
| `branches[]` | a local branch's commits on no remote branch | `git push -u origin <branch>` |
| `stashes[]` | stash entries | `git stash branch <branch> <ref>` with the entry's `ref`, then commit and push; drop the ones you do not keep |
| `reflogOnly` | commits only a reflog still reaches: reset, amended or rebased away | the retire's bundle keeps each one, so they never hold it; `git branch <branch> <commit>` and push the ones you want on the origin |
| `trees[].envFiles` | `.env` files, which git does not carry | move each value into the pool's service env with `zerops_env`, then delete the file |

In a Mate, run each git command in the dev service, `zcp git <dev> <git args>`
— a worktree below `/tmp/` with `-C <its path>` — never on its mount; a
detached HEAD's commits get their branch from its main checkout with
`zcp git <dev> branch <branch> <head>`, that tree's `head`.

`trees[].ignored` and `t3Refs` are named for the record, not moved: ignored
files come back from the repository (dependencies, build output), and refs
under `refs/t3/` are the Mate app's own.

**The work's branch.** `workBranch.branch` is the branch a lease checks out:
in a Mate the open change's branch (`source: change`), or without an open
change the dev checkout's own branch (`current`); elsewhere the dev
checkout's branch's upstream on origin (`upstream`). Where
`workBranch.missing` names none yet, it says what names one. In a Mate, HQ
takes a push to any branch but `main`, and to a `mate/` branch only as an
open change's.

Call the flow again after moving: each moved item drops out of the census,
and what it still names is what the old dev service would take with it.
