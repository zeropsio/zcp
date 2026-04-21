# Where to write — single codebase

For single-runtime recipes (full-stack on one runtime), write all source code, the zerops.yaml, and the on-mount artifacts to `/var/www/appdev/` from the zcp side.

## Tool choice — SSHFS vs SSH

| Operation | Use |
|---|---|
| Creating, editing, or reading a file | `Write`, `Edit`, `Read` against the SSHFS mount path `/var/www/appdev/` |
| Running a command that needs the base image's built-in tools (framework scaffolder, `composer create-project`, `git init`, compiled CLI) | `ssh appdev "cd /var/www && <command>"` — the container-side path is `/var/www`, not `/var/www/appdev` |

Files placed on the mount are already on the dev container — `zerops_deploy` does not "send" them; it triggers a build from the tree that is already there. That applies to every write in this phase.
