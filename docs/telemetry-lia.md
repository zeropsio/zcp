# Legitimate Interests Assessment (LIA) — ZCP Usage Telemetry

**Status: DRAFT — internal accountability artifact, awaiting owner + legal
sign-off.** This is not a public-facing document; it exists to document, for
GDPR Art 6(1)(f) accountability purposes, why ZCP's anonymous usage
telemetry relies on legitimate interest as its legal basis. Companion
documents: `docs/spec-telemetry.md` (design contract),
`docs/telemetry-disclosure.md` (the notice given to data subjects),
`docs/telemetry-runbook.md` §7 (erasure operating procedure).

The operator commits to responding to access/erasure requests within the
statutory one-month window (GDPR Art 12(3)) — see `telemetry-runbook.md` §7.

TODO(owner): identify and confirm the legal controller entity for this
processing before this draft is finalized.
TODO(owner): assign a DPO / privacy contact of record (placeholder used
throughout: privacy@zerops.io) and confirm reachability.
TODO(owner): route this document through legal review before relying on it
as the basis for processing in any jurisdiction.

## 1. Purpose test

**What is the legitimate interest?**

Zerops (the ZCP maintainer) has an interest in understanding real-world
usage of the `zcp` CLI/MCP server: which tools and commands are used, how
often operations succeed or fail, how long operations take, and which
workflow routes/steps are exercised. This is standard product-improvement
analytics: it lets the team prioritize fixes, spot high-failure-rate paths,
and understand adoption across versions — without which the team is
working blind on a distributed CLI tool that does not otherwise phone home.

This is a real, present, and legitimate interest of a kind the ordinary
software industry practice already recognizes (usage analytics for a
developer tool), not a speculative or novel one.

## 2. Necessity test

**Is this processing necessary and proportionate to that purpose?**

- **Necessary**: aggregate, cross-install usage patterns (which tools fail,
  which routes are slow, which versions are in the field) cannot be derived
  from any data ZCP already holds locally — each install only ever sees its
  own process. There is no less-intrusive way to get a cross-install signal
  than *some* form of usage reporting.
- **Proportionate — data minimisation is designed in, not bolted on**:
  - The event schema is a closed, fixed set of fields (tool, command,
    action, duration, success, error code/subcode, workflow route/step, a
    small enumerable "dims" set, os/arch/version) — see
    `docs/spec-telemetry.md` §1 hard boundaries B1-B2. Command arguments,
    free text, file paths, and hostnames are structurally excluded, not
    merely policy-excluded.
  - Identity is exactly two random UUIDs (install id, session id) — never a
    user, account, or project identifier (B1).
  - IP address is used only in-memory for rate-limiting at the ingest edge
    and is never persisted or logged (B6).
  - The system is **opt-in, default-off** in v1: no event is ever sent
    unless the user explicitly sets `ZCP_TELEMETRY=1` (spec §3.1 rule 4).
    This is a stronger-than-required posture for a legitimate-interest
    basis (which does not itself mandate opt-in) and is itself a
    data-minimisation and proportionality choice — the volume and
    population of data collected is bounded to users who affirmatively
    chose in, rather than defaulting to on with an opt-out escape hatch.
  - No event leaves the machine before the disclosure notice has been shown
    on that install (B5) — processing cannot begin before transparency has
    been discharged.

Given the above, the processing is limited to what is genuinely needed for
the stated purpose, and no more.

## 3. Balancing test

**Do the individual's rights and freedoms override the interest?**

Weighing factors:

- **Nature of the data**: anonymous by construction (two random UUIDs, no
  PII, no free text/paths/hostnames/arguments). The residual re-identification
  risk is low — there is no realistic path from an event record back to a
  specific person, account, or organization.
- **Reasonable expectations**: developer tooling that reports anonymous
  usage telemetry is common industry practice; a user who opts in via
  `ZCP_TELEMETRY=1` (v1's default-off gate) has taken an affirmative step
  and is shown the disclosure notice before the first event is ever sent —
  there is no surprise processing.
  - Even setting aside that v1's own posture is opt-in, the underlying
    interest assessment holds independently: were a future version to
    default to on, the anonymity design (below) and disclosure/opt-out
    mechanics would still keep the balance in favor of the legitimate
    interest.
- **Potential impact / mitigations in place**:
  - No PII is collected — nothing in the event schema can be tied back to
    an individual without additional information the operator does not
    hold and has no path to obtaining.
  - Identity is two random, unlinkable UUIDs, not a persistent account or
    device identifier that spans other systems.
  - Full transparency: `zcp telemetry disclosure` gives the complete
    Art-13 notice on demand at any time; `zcp telemetry status` shows the
    live resolved state; every notice explains the opt-out.
  - Easy, permanent opt-out: `ZCP_TELEMETRY=0`, `DO_NOT_TRACK=1`, or
    `zcp telemetry disable` (persists across the opt-in env var being set
    again).
  - Erasure path: `zcp telemetry id` gives the data subject their install
    id, which is the sole key needed to erase their raw event history (see
    `telemetry-runbook.md` §7 for the operator-side SQL).
  - Bounded retention: raw events auto-expire after 15 months (ClickHouse
    TTL); only depersonalized aggregates (no per-install identity) are kept
    indefinitely.
  - No third-party recipients: data stays within the maintainers' internal
    pipeline; it is not sold or shared onward.

**Conclusion (draft)**: given the anonymous-by-construction design, the
opt-in default-off posture, the transparent and repeatable disclosure, the
easy opt-out, the available erasure path, and the bounded raw-data
retention, the legitimate interest in product-improvement analytics is not
overridden by the data subject's rights and freedoms. This conclusion is a
draft pending the sign-offs noted above and should be revisited if the
processing's scope, retention, or default posture changes materially (e.g.
a future default-on version, or an expansion of the event schema).
