# Phase 6 — Release / an address they can open

Goal: get the user an address they can open, named honestly. What "release" means depends on the tier (from the PRD) — most guided apps stay happily on their preview; only a production-business app goes through the production launch.

## Dev / experiment / real-but-lean → the preview is the release

For experiment and real-but-lean tiers, there is no separate launch step. Each verified slice (phase 5) already runs on the dev runtime; that URL is the deliverable. Make it reachable: read the URL from `zerops_discover`, and if the runtime has no public subdomain yet, enable it with `zerops_subdomain` — never hand-author subdomain access into import YAML. Hand the user the URL as **their preview** — a copy for trying it, whose data is a test copy, never "online" or "live" and nothing to install or share — let them react, and re-narrate per slice as the app grows. When a milestone is worth a clean built-verification, promote to stage (phase 5) and hand over the stage half's URL instead — still their preview, never "stage".

Tripwire exception: if Align flagged a tripwire, do **NOT** auto-enable the public subdomain — surface the scoped harm-gate question first (`phases/align.md`) and proceed only within what the user confirms.

Ask for targeted feedback, not a generic opinion: point the user at one acceptance action and the specific outcome you need validated. Do not wait for that feedback as a release gate unless a tripwire requires it.

## Production-business → the launch-production flow

When the PRD tier is production-business and the user wants to go live for real ("ship it", "customers will use this", "run the business on it"), drive `zerops_workflow` launch-production and **follow its guidance** — don't hand-author production YAML or pre-narrate the mechanics. The flow owns the whole sequence and hands you each step, its gate recovery, and the token contract at the point you need them; enter it at the right moment and drive it to a verified production URL. In a Mate, production is the application's: the person releases to it from Mate's projects page, and you never launch it yourself.

The one judgment guided adds on top of the flow: **promote proportionately, on a signal.** Raise a managed dep to HA only when availability or criticality actually calls for it — HA is a type variant the flow's owners resolve, not a default you reach for because the tier reads "production".

## State the final result, then stay ready for the next feature

When the app runs, tell the user plainly:

- **what runs, in their words** — their preview, and production once a release runs there (never the service names or the dev/stage pair),
- what you **added** vs **deliberately deferred** (the visible roadmap from the PRD's out-of-scope),
- the **owners** you consulted for each service choice,
- the **result**, stated as the composite (`SKILL.md`) — *reachable at its address* is ZCP-verified (`zerops_verify`); *acceptance tests green + reviewed* are your own host-side work.

Once production runs its first release, its address is the real app's: say so plainly — that is the one to use, share and install, with real data — and that the preview stays where the next change is tried.

Add one PRD note, **Next signal to watch**, naming the user or operational signal that would change the next slice/topology. This is intent, not status.

When the user comes back with "now add X", return to Align (phase 1) — the infra already exists, so only a genuinely new capability needs a fresh service (a bootstrap side-trip).
