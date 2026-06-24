# Phase 7 — Release / live URL

Goal: get the user a live URL they can open. What "release" means depends on the tier (from the PRD) — most guided apps live happily at a dev/demo URL; only a production-business app goes through the production launch.

## Dev / experiment / real-but-lean → the deploy URL is the release

For experiment and real-but-lean tiers, **there is no separate launch step**. Each verified slice (phase 6) is already deployed and reachable; the live URL is the deliverable. For eligible dev modes the **first deploy already auto-enabled the public subdomain** — just read the URL from `zerops_discover` and hand it over. Reach for `zerops_subdomain` (enable) only as the explicit opt-in / recovery path (a mode that didn't auto-enable, or a tripwire you've since cleared); never hand-author subdomain access into import YAML. Then hand the user the URL, let them react, and re-narrate per slice as the app grows. Done.

The tripwire is the one exception: if Align flagged a tripwire, do **NOT** auto-enable the public subdomain — surface the scoped harm-gate question first and proceed only within what the user confirms.

## Production-business → the launch-production flow

When the PRD tier is production-business and the user wants to go live for real ("ship it", "customers will use this", "run the business on it"), drive the **existing** launch-production pipeline — do not hand-author production YAML or invent promotion rules:

- **Drive `zerops_workflow` launch-production.** It owns the scope → readiness → source-control gate → prod-setup → token-guarded release sequence. It is **pipeline-first**: the prod import carries no `buildFromGit`; runtimes start with `startWithoutCode`, and the first release IS the first build.
- **Promote proportionately, on a signal.** Raise a managed dep to `:ha` and its profile only when availability/criticality calls for it; take the promoted variant/profile from the flow's owners, never a typed tier. HA is the type variant, not a "bigger core".
- **Source-control gates.** Production releases go through the git/source-control gates the flow enforces — don't bypass them.
- **The launch token is user-owned.** The single launch token is a secret the user provides; it enters the conversation once and never crosses response / state / audit surfaces. Never fabricate it; ask the user, following the credential contract.
- **Region is not assumed.** Take the region default + valid list from the launch prompt / live schema; never assume `prg1` or any fixed location.

## State the final result

When the app is live, tell the user plainly:

- the **concrete topology** (the service set that's running),
- what you **added** vs **deliberately deferred** (the visible roadmap from the PRD's out-of-scope),
- the **owners** you consulted for each service choice,
- the **verified** live result — as the composite (deployed + reachable + acceptance tests green + reviewed), and the **live URL**.

These are real Zerops services the user fully owns — SSH, scale, logs, dashboard are theirs. Keep that visible.
