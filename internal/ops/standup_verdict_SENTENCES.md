# Stand-up verdict (`standup_verdict.go`)

- SV-READY — While a stage still builds and every pool is done, the headline is "ready", so the agent starts work on the pools instead of waiting for the stages.
- SV-F17 — With two parts where one failed and the other is done, the headline is "failed", never "done".
- SV-F2 — A service still being created, with no version and no failure, is "building", never "done".
- SV-BEARER-WAIT — A pool whose version is active but whose bearer reference does not resolve yet is "building", not leasable from the active version alone.
- SV-F10 — A pool whose bearer still does not resolve once the wait is over is "failed" and its cause names the variable `<pool>_LEASE_TOKEN`, never "restart zcp".
- SV-F4 — A refusal before any build is "failed" with class "refused" and the refusal's own words.
- SV-F5 — A failed first pool build is one "failed" row with its commit and build process, whose next step fixes the code on zcp's own container, not a bearer cause.
