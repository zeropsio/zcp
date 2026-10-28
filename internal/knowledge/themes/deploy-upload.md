# Deploy upload — what it carries, its size and timeout, and keeping files out of it

`zerops_deploy` runs `zcli push` in the source container, and what it uploads is the source's **git snapshot**: every tracked file under the working directory, plus untracked files `.gitignore` does not exclude. A self-deploy also uploads the whole `.git` directory, so the repository's history counts too. A tree of large tracked assets therefore uploads in full on every deploy, to dev and stage alike, and the build container receives all of it before any build command runs.

## What does and does not make it smaller

- **`.deployignore` and `deployFiles` do NOT make the upload smaller.** They act after the build, on what reaches the runtime. The exception is a deploy of a commit (`sha=` on a cross-deploy): it ships that commit's tree without git, and there `.deployignore` does filter the upload.
- **`--workspace-state` does not either.** It picks which version of the tracked tree ships (`all`, the default, is the working tree; `staged` the index; `clean` HEAD); every tracked file is in each of them.
- **Untracked files:** anything `.gitignore` does not exclude ships. Ignore build output, caches and local data.
- **Tracked files neither the build nor the app needs:** zcli packs with `git archive`, which skips paths `.gitattributes` marks `export-ignore` (e.g. `app/src/assets/frames export-ignore`; a directory marked so is skipped whole). That holds for every push from the repository, a self-deploy included: the new dev container's working tree then lacks those files (the repository keeps them — `git checkout -- <path>` brings them back), so mark only what neither the build nor the dev workflow reads.
- **Large binary assets** usually belong in object storage, not in git.

## Size and how long it may take

The deploy result's `upload` field says what was uploaded — its bytes, tracked files, untracked part, a self-deploy's `.git`, and the largest top-level paths — and a warning names an upload past 256 MiB. The push waits for its upload 5 min plus 1 s per MiB it carries, at most 60 min. One still uploading then is stopped before any build starts and reported as an upload that did not finish: slow, not a network failure, and the fix is a smaller upload, not a retry loop.

## A self-deploy's `.git`

A self-deploy carries the repository, so its history is part of every upload. Duplicate packs double that: `git count-objects -vH` shows `packs` and `size-pack`, and `git repack -a -d` merges the packs into one.

## Only `zerops_deploy` pushes

zcp's own Zerops key never leaves its container, and zcli is never logged in with it — not in a service, not in zcp's container. When an upload cannot be made to fit, tell the person what blocks the deploy instead of pushing around `zerops_deploy`.
