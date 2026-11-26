# A change's labels (`changeLabels`)

- CL-1 — A change's labels say GitHub's checks failed or are running when HQ's state says so for its head, and say nothing of checks that passed or that HQ does not know. Breaks: an agent reports a change as ready while its checks are red.
- CL-2 — A change labels each Review build state, its unavailable reason and a checks handoff, alongside GitHub checks, and says nothing of an absent or unknown Review build. Breaks: an agent mistakes the build's outcome or misses that the checks are over to the person.
- CL-3 — An awaiting Review build becomes unavailable once its deadline has passed, even when the Mate's last heard state has not changed. Breaks: an agent waits on a result that will never arrive.
- CL-4 — A Review build and checks handoff from HQ reach the current and last-told change labels and the description's answer. Breaks: an agent sees no build outcome despite HQ reporting one.
