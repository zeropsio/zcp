# Phase 6 — Release / live URL

Goal: get the user a live URL they can open. What "release" means depends on the tier (from the PRD) — most guided apps live happily at a dev/demo URL; only a production-business app goes through the production launch.

## Dev / experiment / real-but-lean → the live URL is the release

For experiment and real-but-lean tiers, there is no separate launch step. Each verified slice (phase 5) is already live on the dev runtime; that URL is the deliverable. Make it reachable: read the URL from `zerops_discover`, and if the runtime has no public subdomain yet, enable it with `zerops_subdomain` — never hand-author subdomain access into import YAML. Hand the user the URL, let them react, and re-narrate per slice as the app grows. When a milestone is worth a clean built-verification, promote to stage (phase 5) and hand over the stage URL instead.

The tripwire is the one exception: if Align flagged a tripwire, do **NOT** auto-enable the public subdomain — surface the scoped harm-gate question first and proceed only within what the user confirms.

## Production-business → the launch-production flow

When the PRD tier is production-business and the user wants to go live for real ("ship it", "customers will use this", "run the business on it"), drive `zerops_workflow` launch-production and **follow its guidance** — don't hand-author production YAML or pre-narrate the mechanics. The flow owns the whole sequence (scope → source-control gate → env classification → prod-setup → the user-owned launch token → first release) and hands you each step, its gate recovery, and the token contract at the point you need them. Your job is to enter it at the right moment and drive it to a verified live URL.

The one judgment guided adds on top of the flow: **promote proportionately, on a signal.** Raise a managed dep to HA only when availability or criticality actually calls for it — HA is a type variant the flow's owners resolve, not a default you reach for because the tier reads "production".

## State the final result, then stay ready for the next feature

When the app is live, tell the user plainly:

- the **concrete topology** (the service set that's running, the dev/stage pair),
- what you **added** vs **deliberately deferred** (the visible roadmap from the PRD's out-of-scope),
- the **owners** you consulted for each service choice,
- the **live result**, named as a composite with ownership kept distinct — *reachable at the live URL* is ZCP-verified (`zerops_verify`); *acceptance tests green + reviewed* are your own host-side work, never a platform guarantee.

These are real Zerops services the user fully owns — SSH, scale, logs, dashboard are theirs. Keep that visible.

When the user comes back with "now add X", return to Align (phase 1): fold the new feature into the PRD, add slices, and build them the same way on the dev runtime. The infra already exists — only a genuinely new capability needs a fresh service (a bootstrap side-trip), then back to building.
