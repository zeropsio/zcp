# ZCP Telemetry — Full Disclosure Notice

Canonical human-readable copy of the GDPR Article 13 transparency notice
kept alongside `docs/spec-telemetry.md`. This is **not published anywhere in
v1** — no webpage, no `docs.zerops.io` route. The identical text ships
in-binary as `zcp telemetry disclosure` (`internal/telemetry/cli.go`,
`FullDisclosure`); if the two ever drift, the code wins and this file should
be updated to match.

## What is collected

Anonymous usage events, each carrying only:

- tool name, command, action (which ZCP tool/subcommand ran)
- duration (ms), success (bool), error code + optional error subcode
- workflow route and workflow step (which flow stage, if any)
- a small fixed set of enum "dims" (closed value sets — never free text)
- os, arch, zcp version
- two random UUIDs: an install id (per machine) and a session id (per
  process) — neither is derived from anything identifying you

## What is never collected

- command arguments, free text, or file paths
- hostnames
- IP addresses — used only in-memory for rate-limiting at the ingest edge,
  never stored or logged
- any account, project, or user identifier

## Why / legal basis

Legitimate interest (GDPR Art 6(1)(f)): understanding real usage to improve
the product. The data is anonymous by construction, so it cannot identify
you or your organization.

## Opt-in / opt-out

Telemetry is OFF by default. It sends data ONLY when you explicitly set
`ZCP_TELEMETRY=1`. Turn it off again with `ZCP_TELEMETRY=0` or
`DO_NOT_TRACK=1` — or run `zcp telemetry disable` to record a persistent
opt-out that survives even if `ZCP_TELEMETRY=1` is set again later.

## Retention

Raw events auto-expire after 15 months. Aggregate statistics (which carry
no per-install identity) are kept indefinitely.

## Recipients / transfers

None. Data goes only to the ZCP maintainers' internal telemetry pipeline.
It is not sold and not shared with third parties.

## Your rights

You have the right to access and erasure. Run `zcp telemetry id` to print
your install id, then request deletion via the contact below. You may also
lodge a complaint with your data-protection authority.

## Controller & contact

- Controller: Zerops s.r.o.
- Contact: privacy@zerops.io

<!-- TODO(owner): confirm the legal controller entity + a real privacy
     contact address before any public release. -->

For the full text again at any time, run `zcp telemetry disclosure`.
