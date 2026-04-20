# Provision — 3a. Configure git + init + initial commit (container-side, single SSH call)

Run git configuration, init, and the initial commit inside one SSH call per mount. `/var/www/{hostname}/` on zcp is an SSHFS write surface into the container's `/var/www/`; git ownership on `.git/` must match the container's `zerops` user for later `zerops_deploy` to lock `.git/config` successfully. Running the full sequence container-side keeps `.git/` owned by `zerops` from first write.

The SSH-only-for-git boundary (where commands run) lives in `principles/where-commands-run.md`.

## Canonical single SSH call per mount

```
ssh {hostname} "git config --global --add safe.directory /var/www && \
                git config --global user.email 'recipe@zerops.io' && \
                git config --global user.name 'Zerops Recipe' && \
                cd /var/www && \
                git init -q -b main && \
                git add -A && \
                git commit -q -m 'initial scaffold'"
```

One call covers config + init + add + commit. The working tree may contain root-owned files written via SSHFS from zcp — git only checks `.git/` ownership against the current user, which is `zerops` on both ends of this call, so the ownership cascade never fails.

## Concurrency rule — one git op per mount at a time

Within a single mount's `.git/`, git uses a `.git/index.lock` file. Two SSH calls performing concurrent git ops on the same mount will produce `fatal: Unable to create '.git/index.lock': File exists` on the second. The positive form: sequence git ops per mount. Different mounts have independent `.git/` trees and run in parallel safely.

## Re-init after every scaffold return

Scaffold work deletes `/var/www/.git/` before returning (a cleanup rule the scaffold brief declares — scaffolder-created `.git/` would collide with the canonical init). The pre-scaffold `.git/` created by provision no longer exists when a scaffold returns, so the post-scaffold commit on each mount re-runs the canonical init sequence before `git add && git commit`:

```
ssh {hostname} "git config --global --add safe.directory /var/www && \
                cd /var/www && \
                git init -q -b main && \
                git add -A && \
                git commit -q -m 'post-scaffold commit'"
```

The same single-SSH-call shape is used every time the repo needs to be in a clean, initialised state with a fresh commit.
