# Mate-wide copies (`mate_copies.go`, `hq_lease_completion.go`)

- MC-CLASSIC — A non-pool dev half appears in the Mate status file with its checked-out branch, while a pool does not appear as a classic dev half.
- MC-MERGE — When HQ confirms a change merged and releases its lease, the Mate status file removes that lease and retains leases of other branches.
- MC-PREVIEW — Opening a held Draft returns its declared preview server's address once it runs, starts it when stopped, and reports a failed start without an address.
- MC-PREVIEW-STATE — Opening a held Draft reports its preview server's state to the Mate, or unknown when its supervisor lacks the capability.
- MC-WAIT — Replacing the status file without changing a copy's state keeps its single existing supervisor wait.
- MC-ENDED — Opening a lease whose recorded identity no longer matches the Draft reports that the Draft ended.
- MC-BRANCH — Opening a classic dev half refuses a Draft whose branch is no longer checked out there.
