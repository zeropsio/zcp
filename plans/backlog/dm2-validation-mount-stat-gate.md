# C1 — DM-2 self-destruction validation gated on os.Stat(mountPath)

**Status:** deferred from the audit-fixes P4 sweep (2026-06-10). Flagged to Karel, not silently dropped.

## What

`internal/ops/deploy_ssh.go:162` gates the ENTIRE pre-deploy validation block —
including the DM-2 self-destruction hard error (`ValidateZeropsYml`) and
`RunPreDeployValidation` — behind `if _, statErr := os.Stat(mountPath); statErr == nil`.
When the SSHFS source mount is momentarily not stat-able (stale transport window),
validation is skipped and the deploy proceeds. For a `DeployClassSelf` (source==target)
with narrower-than-`[.]` deployFiles during a stale-mount window, the source working
tree can be destroyed — the exact outcome DM-2 exists to prevent.

## Why deferred (not fixed in the sweep)

- **Verified narrow trigger (Codex):** the dominant production path is already validated
  by the TOOLS-layer preflight (`deployPreFlight`) before reaching `ops.DeploySSH`. The
  ops-layer skip only bites when the tools preflight is bypassed (recipe / pre-bootstrap),
  where the mount legitimately does not exist.
- **Proper fix has real blast radius:** the Codex-preferred fix (read the yaml over SSH
  for `DeployClassSelf` instead of relying on the local mount stat) changes
  `ValidateZeropsYml`'s data source and **breaks the ops unit suite**, which runs with no
  `/var/www/<source>` on the dev machine and expects the skip branch (BUILD_TRIGGERED).
  It would also need to distinguish a STALE mount (suspicious) from a legitimately-ABSENT
  mount (recipe/pre-bootstrap) — those have different correct behaviors.
- A blanket "fail-hard on `DeployClassSelf` when mount unstattable" would block legitimate
  recipe/pre-bootstrap self-deploys (deployFiles=`[.]`, which is safe) that have no mount.

## Sketch of the real fix

1. For `class == DeployClassSelf` only: when the mount can't be stat'd, read the
   zerops.yaml over SSH (the git-push path already reads yaml over SSH — reuse that) and
   run DM-2 on the SSH-read content. Cross-deploy + full-tree self-deploy stay on the
   current best-effort path.
2. Update the ops unit suite: the self-deploy tests must either provide an SSH-readable
   yaml stub or assert the new fail/validate behavior.
3. Distinguish stale-vs-absent: in container mode an adopted self-deploy SHOULD have a
   mount; recipe sessions (probe-owned host) legitimately don't. Gate the strictness on
   adoption state / recipe-probe presence.

## Trigger to promote

A reported source-tree-loss on a self-deploy, OR when the recipe/pre-bootstrap self-deploy
path is next touched (do both together — they share the mount-presence assumption).

## Pin when fixed

`TestValidateZeropsYml_DM2_*` (exists) + a new test driving `ops.DeploySSH` with
`class=DeployClassSelf`, narrow deployFiles, and an unstattable mount → asserts the
deploy is refused (or validates over SSH), not silently proceeded.
