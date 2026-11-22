# Lease and pool decisions (`lease_ownership.go`, `lease_group.go`, `lease_park.go`, `pool_preview_ports.go`)

- LP-RETAKEN — A lease the orchestrator answers unknown-lease for while it lists the container reserved is another holder's, lost, never ours.
- LP-STOPPED — A lease whose container the orchestrator renews while stopping or starting it (a park, an unpark) stays ours.
- LP-UNVERIFIED — A renew the orchestrator did not answer (any error that is not unknown-lease or container-not-found) is unverified: neither ours nor lost, so nothing reaches the container and its record stays.
- LP-DIRTY-BRANCH — Releasing a branch whose one lease holds uncommitted files or unpushed commits refuses the whole branch and releases none.
- LP-UNCHECKED-BRANCH — Releasing a branch where one lease could not be checked for unsaved work refuses the whole branch.
- LP-MAIN — No lease is ever acquired on the repository's default branch; any other branch is accepted.
- LP-DUP-POOL — A group acquire that names one pool twice is refused before any lock: a group holds one lease per pool.
- LP-PREVIEW-RAISE — A dev pool asked for more containers than its preview ports cover gets a shortfall naming each container past the block and its port; asked for exactly the block's count, none.
- LP-NO-RAISE — The same maxContainers again, or a lower one, is no raise; a higher one is.
- LP-PARKED — A reserved container that is stopped reads parked (as does one being stopped or started); a reserved running one does not.
- LP-UNKNOWN-POOL — An acquire naming a service that is not in the project, or not a Swarm pool, is refused naming that service, also when it passes no repo and none is recorded: it is never asked for a repo first.
