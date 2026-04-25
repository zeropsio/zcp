You're on a **developer machine** bound to a Zerops project. Code in
your working directory is the source of truth — deploy via
`zerops_deploy targetService="<hostname>"` (pushes the working
directory to the matching service, blocks until build completes).
Requires `zerops.yaml` at repo root. Reach managed services over
`zcli vpn up <projectId>`.
