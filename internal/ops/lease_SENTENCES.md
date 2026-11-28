# Lease and pool decisions (`lease_ownership.go`, `lease_group.go`, `lease_park.go`, `pool_preview_ports.go`, `lease_warm_clone.go`, `lease_checkout.go`, `lease_supervisor.go`)

- LP-MATE-ACQUIRE — A lease record adds its alias and branch to the Mate status file without exposing its credential, while retaining the other held leases.
- LP-MATE-RELEASE — Dropping a lease record replaces the Mate's lease set without the released lease and retains the other held leases.

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
- LP-WARM-HQ — In a Mate, the clone of a pool's repository on the Mate's HQ into a free warm container is handed to a zcp process of its own, which holds its relayed session until the clone ends; the process that warmed the container, which may end first (a zcp lease call, an MCP server), never runs it.
- LP-WARM-HELD — A held clone's process records what the clone did: done once it finished, skipped when it left the work dir alone (a checkout took it, or it held a repository), failed naming the clone's exit status, and failed — never tried again into a skip — when the session ended while the clone ran.
- LP-WARM-LOST — A held clone that still reads running past its bound never reported back (the process holding its session ended first, as at a restart): status says it failed, never that it runs and an acquire waits for it.
- LP-BRANCHREF — A checkout's branch is compared with its ref on origin, read through the git relay: no ref is unpublished, a ref at the checkout's head is current, one elsewhere counts what the checkout is ahead and behind or says its history is unknown, and a ref the relay does not answer, or answers for another branch, is not read. Breaks: an unpushed branch reads published and its copy is handed over or moved.
- LP-SUP-ACQUIRE — A fresh acquire on a BusyBox-based pool with an older, idle supervisor ends it, installs the acquiring zcp's release binary, and starts and reports that version.
- LP-SUP-SAME — A fresh acquire whose supervisor is already on the acquiring zcp's version sends no binary and leaves the running supervisor in place.
- LP-SUP-HELD — A branch acquires its held lease again, even to finish an interrupted checkout, without delivering a supervisor or stopping the one that runs.
- LP-SUP-UNPARK — Unpark installs the acquiring zcp's supervisor before starting it on a parked lease and leaves an already running lease on its version.
- LP-SUP-WARM — An acquire's warm top-up leaves its new free container with the acquiring zcp's release binary.
- LP-SUP-FALLBACK — A missing release asset or a failed copy leaves a fresh lease held with its image supervisor and a note naming the failure.
- LP-SUP-ACTIVE — An answering supervisor with running declared servers is left in place, and the lease reports its version and why it was kept.
- LP-SUP-DEV — A development build leaves a new lease on its image supervisor without a version probe for delivery or a binary copy.
- LP-SUP-CHECKSUM — zcp holds only a companion binary verified by its exact release checksum, and an acquire can use those held bytes without fetching them again.
