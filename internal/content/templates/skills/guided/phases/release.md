# Phase 6 — Release / live URL

Goal: get the user a live URL they can open. What "release" means depends on the tier (from the PRD) — most guided apps live happily at a dev/demo URL; only a production-business app goes through the production launch.

## Dev / experiment / real-but-lean → the live URL is the release

For experiment and real-but-lean tiers, there is no separate launch step. Each verified slice (phase 5) is already live on the dev runtime; that URL is the deliverable. Make it reachable: read the URL from `zerops_discover`, and if the runtime has no public subdomain yet, enable it with `zerops_subdomain` — never hand-author subdomain access into import YAML. Hand the user the URL, let them react, and re-narrate per slice as the app grows. When a milestone is worth a clean built-verification, promote to stage (phase 5) and hand over the stage URL instead.

The tripwire is the one exception: if Align flagged a tripwire, do **NOT** auto-enable the public subdomain — surface the scoped harm-gate question first and proceed only within what the user confirms.

## Production-business → the launch-production flow

When the PRD tier is production-business and the user wants to go live for real ("ship it", "customers will use this", "run the business on it"), drive the launch-production pipeline — do not hand-author production YAML or invent promotion rules:

- **Drive `zerops_workflow` launch-production.** It owns the scope → readiness → source-control gate → prod-setup → token-guarded release sequence. It is pipeline-first: the prod import carries no `buildFromGit`; runtimes start with `startWithoutCode`, and the first release IS the first build.
- **Promote proportionately, on a signal.** Raise a managed dep to `:ha` and its profile only when availability/criticality calls for it; take the promoted variant/profile from the flow's owners, never a typed tier. HA is the type variant, not a "bigger core".
- **Source-control gates.** Production releases go through the git/source-control gates the flow enforces — don't bypass them.
- **The launch token is user-owned.** The single launch token is a secret the user provides; it enters the conversation once and never crosses response / state / audit surfaces. Never fabricate it; ask the user, following the credential contract.
- **Region is not assumed.** Take the region default + valid list from the launch prompt / live schema; never assume `prg1` or any fixed location.

## State the final result, then stay ready for the next feature

When the app is live, tell the user plainly:

- the **concrete topology** (the service set that's running, the dev/stage pair),
- what you **added** vs **deliberately deferred** (the visible roadmap from the PRD's out-of-scope),
- the **owners** you consulted for each service choice,
- the **live result**, named as a composite with ownership kept distinct — *reachable at the live URL* is ZCP-verified (`zerops_verify`); *acceptance tests green + reviewed* are your own host-side work, never a platform guarantee.

These are real Zerops services the user fully owns — SSH, scale, logs, dashboard are theirs. Keep that visible.

When the user comes back with "now add X", return to Align (phase 1): fold the new feature into the PRD, add slices, and build them the same way on the dev runtime. The infra already exists — only a genuinely new capability needs a fresh service (a bootstrap side-trip), then back to building.
