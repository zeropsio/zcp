# Plan: telemetry-launch

## Run State
- `phase:` awaiting-approval
- `base:` f90181d2f6b5023234e42d6370ffb0afe9cf1a22 (session start; rebase target = main 4b87cfbd)
- `integration:` —
- `approved:` —
- `codex:` RESHAPE (incorporated) — /tmp/codex-out-1784284919-55049-17692.md; findings folded into register R2
- `next:` OWNER GATE 1 — approve reshaped register + 2 open decisions (enable-contract, endpoint-strategy); erasure-SLA correction acknowledged
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: The parked telemetry system (`feat/telemetry-v1`) is landed on
`main` in a v1 test posture — telemetry DEFAULT-OFF in the released binary,
enabled explicitly via env (`ZCP_TELEMETRY` truthy) — with the production
ingest endpoint publicly live (spoof-resistant client-IP derivation) so
production testing can start; disclosure + LIA exist as in-repo documents
only. The full default-on public launch (PRD P2) is deferred to v2.

**Owner decisions (2026-07-17 checkpoint)**: v1 default-OFF + env opt-in;
"zásadní je to dostat do produkce, aby se to tam dalo začít testovat" ·
disclosure = document kept with the code/spec, published nowhere · erasure
mechanism documented WITHOUT a time-bound SLA · LIA drafted by Fable, Karel
approves · domain: NOT zerops.io — name pending (OPEN-1, owner supplies at
Gate 1; `ZCP_TELEMETRY_ENDPOINT` override keeps v1 testable regardless).

| obs | evidence |
|---|---|
| Telemetry is COMPLETE + parked: client (internal/telemetry + wire) + ingest (internal/ingest + cmd/zcp-ingest) + CH pipeline; all gates were green at parking | feat/telemetry-v1:PARKED.md; commit c756f50a |
| Live internal infra runs: Zerops project `zcp-telemetry` — CH HA `db`, ingest internal-only (subdomain OFF per R5), Grafana 4 dashboards | feat/telemetry-v1:PARKED.md |
| Only unexecuted phase = PRD P2: disclosure page live, LIA filed, production domain, release flips external default-on, go/no-go gate on real traffic | feat/telemetry-v1:plans/prd-telemetry-2026-07-02.md:275-276 |
| Ingest derives client IP as LEFTMOST X-Forwarded-For (fallback RemoteAddr); per-IP limiter, new-install cardinality breaker, and INGEST_BLOCK_IPS all key on it | feat/telemetry-v1:internal/ingest/handler.go:239-249,114-169 (adversarial C1/C2/C6) |
| Leftmost XFF is client-spoofable → behind a public edge, per-IP limits + breaker + blocklist are trivially bypassable; fix = trusted-hop-from-right or authoritative X-Real-IP | adversarial C1/C2; design gated on PROBE-1 |
| NO code channel gate exists: external is the default channel, Enabled is decided only by opt-out/disclosure precedence; "flip default-on" (B-5) = release act + endpoint standup, zero code | feat/telemetry-v1:internal/telemetry/config.go (resolveChannel), client.go:154,204 (adversarial C3) |
| Rebase onto main = exactly 3 conflicted files, all cmd/zcp/, mechanical (cliDispatch map vs new `capture` verb + sync/eval single-exit touches); server.go + .golangci.yaml auto-merge clean | git merge-tree --write-tree main feat/telemetry-v1 (divergence + adversarial C4) |
| Ingest is single-replica BY DESIGN (min=max=1); limiter/dedup/blocklist in-memory per-process + startup migration applier assume 1 replica | ../zcp-telemetry/import-project.yaml (adversarial C5) |
| Zerops prod exposure: custom domain = dedicated HA balancer + auto Let's Encrypt; zerops.app subdomain = shared balancer, 50MB cap, "not recommended for production" | [KB:references/networking/public-access.mdx L19-72,147-172] |
| L7 balancer forwards X-Forwarded-For/X-Real-IP; edge offers per-IP rate limiting (binary_remote_addr) + CIDR access policy; NO native DDoS/WAF (Cloudflare recommended) | [KB:guides/networking.mdx L44-50; references/networking/l7-balancer-config.mdx L396-454; references/networking/dns.mdx L15] |
| dataconsole (on feat/managed-data-console, 34 commits unmerged) registers no MCP tools; its HTTP data plane is invisible to tool_call telemetry by construction; only future touchpoint = `studio` entry in cliDispatch | divergence report (c) |

- AC1: `feat/telemetry-v1` rebased onto main; full battery + `-race` + `make lint-local` green; capture verb + single-exit cliDispatch both preserved; the two SEMANTIC conflicts land correctly (sync.go still exits nonzero on a partial corpus; eval capture begin/end markers retained) — planned evidence: battery run + merge-tree empty + named regression tests `TestRunSyncPull_PartialCorpus_ExitsNonzero`, `TestEvalLocal_CaptureMarkers_Preserved`.
- AC2: v1 opt-in gate (Codex precedence): `ZCP_TELEMETRY` unset ⇒ disabled with `ReasonDefaultOff`, no install ID minted, no stamp, nothing printed; explicit opt-in token {1,true,on,yes} ⇒ enters disclosure/enabled path; `=0`/`false`/`off`/`no` keep `ReasonOptedOutEnvTelemetry`; DO_NOT_TRACK + install-file-disabled keep their frozen precedence — planned evidence: table-driven config-precedence tests via dedicated `isTelemetryOptInToken` (NOT a broadened `isTruthy`).
- AC3: client-IP derivation is spoof-resistant: identity keys on balancer-authoritative `X-Real-IP` canonicalized via `net/netip` (invalid/multi-value ⇒ RemoteAddr fallback), client-supplied XFF ignored entirely; documented narrower threat model (a direct in-project caller CAN forge X-Real-IP — the internal VXLAN is the trust boundary) — planned evidence: handler tests w/ spoofed-XFF/X-Real-IP + malformed/IPv6-spelling fixtures + ledger-P1 pinned in the comment.
- AC4: public ingress is HARDENED before exposure: ops routes (`/healthz`,`/statsz`) are not an unauthenticated public DoS surface (isolated or rate-limited; `/healthz` CH-ping bounded); `http.Server` carries read/write/idle timeouts (Slowloris-safe) — planned evidence: server tests asserting timeouts set + ops-route protection; `TestServer_OpsRoutesNotPublicUnbounded`.
- AC5: ingest publicly reachable as a DISPOSABLE, MONITORED test endpoint (honest framing — shared-subdomain = one global rate-limit bucket, DoS-able; defense = default-off client + exercised subdomain-off kill switch): an opt-in client's event lands in CH end-to-end and moves an observable `/statsz` accepted-events counter — planned evidence: external curl + Grafana pipeline-health + a NEW accepted-events counter reading; live subdomain-off rollback exercised once.
- AC6: full disclosure surface exists IN the binary — `zcp telemetry disclosure` prints the complete GDPR-Art-13 notice (opt-IN framing, controller/contact, legal basis + specific legitimate interest, retention, recipients, rights, complaint route, erasure channel; no dead docs link); LIA draft filed in-repo; erasure process bounded to the statutory ~1-month window (no ADVERTISED SLA, per owner) — planned evidence: `zcp telemetry disclosure` output assertion + docs/ files + owner approval at Gate 2.
- AC7: kill-order verified end-to-end: default-off (unset) emits nothing AND mints no install ID; a FIRST `ZCP_TELEMETRY=1` process prints disclosure + emits nothing; a SECOND `ZCP_TELEMETRY=1` process emits; `=0`/DNT/`disable` each suppress — planned evidence: two-process e2e with ZCP_TELEMETRY_DEBUG + spool inspection.
- AC8: v1 test-traffic gate is OBSERVABLE: `/statsz` (or Grafana) exposes accepted-events, schema-reject, and 5xx totals; under soak the reject/5xx counters stay flat — planned evidence: counter readout in retest pack (drives the go/no-go).

**Non-goals**: default-on flip + release announcement + published disclosure
page (ALL deferred to v2 = PRD P2 proper) · erasure SLA time commitment ·
dataconsole HTTP data-plane observation (invisible to tool_call by design;
`studio` cliDispatch entry lands with that branch, not here) · multi-replica
ingest / shared-store limits (v1 stays 1 replica) · auth beyond R5
no-client-secret stance · args-hash (O-2, parked) · session_end for CLI
one-shots (O-3) · Cloudflare fronting (revisit only if abuse materializes).
**Constraints**: privacy boundaries B1-B7 frozen (no user/project ids, no
args/paths, env names frozen, IP never durable) · wire stays single-owner
stdlib-only; depguard `wire-stdlib-only`/`ingest-allowlist`/ops+workflow
telemetry bans survive the rebase intact · no new secrets (R5) · working tree
is mid-dataconsole — all telemetry work happens in worktrees off main, never
on the current checkout · live `zcp-telemetry` project mutations (domain
enable) are owner-gated slices.

**Risk class**: high — trigger: public wire contract + security surface
(public unauthenticated ingest, GDPR/ePrivacy posture); plus multi-session
scope.

**Assumptions**:
- [VERIFIED] Launch blockers = B-1 disclosure page, B-2 LIA, B-3 prod domain, B-4 public enable, B-5 release default-on (release act, not code), B-6 release notes, B-7 traffic gate — feat/telemetry-v1:plans/prd-telemetry-2026-07-02.md:275-276 + adversarial C3.
- [VERIFIED] clientIP() trusts leftmost XFF; spoof-bypassable at public edge — feat/telemetry-v1:internal/ingest/handler.go:239-249.
- [VERIFIED] Rebase surface = 3 mechanical conflicts — merge-tree (C4).
- [VERIFIED] Single-replica ingest posture is intentional and internally consistent — import-project.yaml + limits/dedup/blocklist/migrate (C5).
- [VERIFIED] P1 probed live: balancer APPENDS client XFF (spoof stays leftmost — exploit confirmed); X-Real-IP is balancer-authoritative (client spoof dropped). On the shared `*.zerops.app` path the real client IP is ABSENT (X-Real-IP + rightmost XFF = constant proxy address). Fix design: key limiter/blocklist on X-Real-IP, drop ALL trust in client XFF; on the subdomain path this degenerates to one global bucket (acceptable for v1 opt-in test traffic); custom-domain origin-IP exposure = deferred ledger row, precondition for v2 default-on — ledger P1.
- [VERIFIED→DEFERRED] P2: local MCP token is project-scoped to `zcp-eval-clean`; `zcp-telemetry` is unreachable from this session (read-only probe impossible). Infra health folds into the first owner-gated infra slice — ledger P2.
- [VERIFIED] The v1 default-off change is small and reversible: Enabled is decided solely by the opt-out/disclosure precedence in resolve (no channel coupling), so flipping the unset-default to disabled + recognizing explicit truthy is a localized config change; v2 default-on = restoring the unset-default — feat/telemetry-v1:internal/telemetry/config.go (adversarial C3).
- [ASSUMED→PARTLY REFUTED] Subdomain and custom-domain forwarding headers are NOT assumed identical anymore: the subdomain path fronts through `proxy.app-prg1.zerops.io` which masks the origin IP; whether the dedicated custom-domain balancer exposes it is the DEFERRED P1-followup ledger row (v2 precondition, not v1).
- OPEN-1 (owner input, due Gate 1): production domain name (NOT zerops.io). Until chosen, DefaultEndpoint stays TBD and v1 testing uses `ZCP_TELEMETRY_ENDPOINT`; the domain slice is the only work it blocks.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| P1: balancer XFF append-vs-overwrite + X-Real-IP authority | AC3 / clientIP fix design | verifier | header-echo `tmpverifyxff1` (nodejs, subdomain) on zcp-eval-clean; curl plain + spoofed XFF/X-Real-IP from 2 external origins | Spoofed XFF APPENDED leftmost (`x-forwarded-for: 203.0.113.7, 127.0.0.1, 2a00:1ed0:1100::160:0:0`); spoofed X-Real-IP DROPPED (balancer-authoritative); BUT real client IP absent on `*.zerops.app` path — X-Real-IP + rightmost XFF = constant proxy addr (matches subdomain AAAA `proxy.app-prg1.zerops.io`); socket peer = internal nginx 10.12.160.4 | CONFIRMED (append; X-Real-IP authoritative) — premise "client IP present" REFUTED on shared-subdomain path | handler tests: X-Real-IP keying + client-XFF ignored; spec-telemetry §6 note on subdomain degeneration |
| P1-followup: custom-domain (dedicated balancer) exposes real client IP in X-Real-IP | v2 default-on gate; v1 accepts degeneration | verifier (deferred — needs OPEN-1 domain + DNS) | header-echo behind the production custom domain, once stood up | — | DEFERRED (blocked on OPEN-1; run in ASSEMBLE of the domain slice) | runbook note + v2 precondition |
| P2: zcp-telemetry live infra health | AC4 | mcp | project search via local token (org KRLS): by-name `zcp-telemetry` → 0 items; token resolves only `zcp-eval-clean` (ACTIVE) | local cli.data token is project-scoped to zcp-eval-clean; zcp-telemetry unreachable read-only; no retry value on same surface | INCONCLUSIVE → resolved by DEFER: health check folds into the first owner-gated infra slice (owner access required there anyway) | runbook note |

## Design notes (SHAPE §1 — trade-offs; R2 = Codex-reshaped)
- **v1 default-off mechanism**: chosen = a new precedence rule in `Resolve`,
  positioned AFTER install-file-disabled and BEFORE disclosure creation
  (Codex ordering): unset/non-opt-in ⇒ disabled with `ReasonDefaultOff`, mints
  no install ID, writes no stamp, prints nothing; explicit opt-in token
  {1,true,on,yes} ⇒ falls through to the existing disclosure/enabled path.
  Uses a DEDICATED `isTelemetryOptInToken` — never a broadened `isTruthy`
  (that would silently move DO_NOT_TRACK / CI / debug semantics). Rejected =
  build-time `defaultEnabled` const. Preserves the frozen opt-out tokens +
  the Reason strings existing tests assert; v2 default-on = delete the rule.
- **clientIP derivation behind the balancer**: chosen = key on
  balancer-authoritative `X-Real-IP`, canonicalized with `net/netip`
  (invalid/multi-value ⇒ `RemoteAddr` fallback so malformed keys + IPv6
  spellings can't bypass blocklist equality), client-supplied XFF ignored
  entirely. Threat model EXPLICITLY documented: X-Real-IP is trustworthy only
  through the Zerops balancer; a direct in-project caller can forge it — the
  per-project VXLAN is the trust boundary (acceptable: internal traffic is
  already trusted). Rejected = rightmost-XFF-hop parsing (fragile hop-count).
  Accepted v1 limitation: shared-subdomain X-Real-IP = constant proxy addr ⇒
  ONE global rate-limit bucket ⇒ a single caller can DoS the endpoint and the
  IP blocklist can't isolate an external client. So v1 is a DISPOSABLE,
  MONITORED test endpoint with an exercised subdomain-off rollback, NOT a safe
  production ingress; per-client isolation needs the custom-domain origin-IP
  (ledger P1-followup, v2 precondition).
- **Public-ingress hardening (Codex-added S4)**: exposing the ingest also
  exposes `/healthz` (unbounded CH ping per request) + `/statsz` on the same
  listener, and `http.Server` today sets only `ReadHeaderTimeout`. Before any
  public enable: isolate/bound the ops routes and add read/write/idle
  timeouts. Also adds the `/statsz` counters (accepted-events, schema-reject,
  5xx) that make AC5/AC8 observable — the parked `/statsz` moves none today.

## Open decisions for OWNER GATE 1 (Codex-surfaced)
- **D1 — `zcp telemetry enable` contract** — DECIDED (2026-07-17): **env-only**.
  `enable` truthfully records consent (clears install-file disabled + stamps
  disclosure) but the printed message must NOT claim telemetry is on — it says
  consent is recorded and `ZCP_TELEMETRY=1` is still required to emit. No new
  install-file opt-in state. S3 implements this; AC2/AC7 follow it.
- **D2 — v1 endpoint strategy** — DEFERRED (2026-07-17): owner will supply the
  production domain ("to ti dodám, zatím s tím počkej"). Until then: proceed
  **override-only** — `DefaultEndpoint` UNTOUCHED, v1 test traffic uses
  `ZCP_TELEMETRY=1` + `ZCP_TELEMETRY_ENDPOINT`; **no S7**, and S6 (live
  exposure) waits for the domain too. This unblocks S1–S5 (all code) now; the
  only work D2 gates is the endpoint const + live enable.
- **D3 — erasure SLA correction**. Owner chose "no SLA". Codex/GDPR: the
  PUBLIC copy may omit a voluntary SLA (owner's choice stands), but the
  INTERNAL operator process for a rights request cannot be time-unbounded —
  the statutory ~1-month window applies. v1 posture: no advertised SLA; LIA +
  runbook commit to the statutory month. (Acknowledgement, not a fork.)

## Slice Register (R2 — Codex-reshaped)
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Rebase `feat/telemetry-v1` onto main; resolve 3 cmd/zcp conflicts (2 SEMANTIC: sync.go partial-corpus exit code, eval capture markers) + regression tests; fold `capture` into `cliDispatch` and pin its `cli_command` emit; add engine-independent Go architecture pin for `ingest-allowlist` + confirm capture-inspector rules survive; full battery green | — | `cmd/zcp/{main,sync,eval_behavioral_local}.go` (+ conflict-regression tests), `internal/topology/architecture_test.go` | unit+tool+integration+e2e | review | pending |
| S2 | `clientIP()` spoof-resistance: key on `X-Real-IP` via `net/netip` canonicalization (invalid/multi-value ⇒ `RemoteAddr` fallback), ignore client XFF; doc-comment the VXLAN threat model | S1 | `internal/ingest/handler.go`, `internal/ingest/handler_test.go` | unit | review | pending |
| S3 | v1 default-off + explicit-opt-in gate in `Resolve` (new rule after install-file-disabled, before disclosure; `ReasonDefaultOff`, dedicated `isTelemetryOptInToken`); resolve the `zcp telemetry enable` contract per D1 | S1 | `internal/telemetry/config.go`, `internal/telemetry/cli.go`, `+_test.go` for both | unit | review | pending |
| S4 | Public-ingress hardening: isolate/bound `/healthz`+`/statsz`, add `http.Server` read/write/idle timeouts, add `/statsz` counters (accepted-events, schema-reject, 5xx) so AC5/AC8 are observable | S1,S2 | `internal/ingest/server.go`, `internal/ingest/handler.go`, `+_test.go` | unit | review | pending |
| S5 | Full GDPR-Art-13 disclosure IN the binary via `zcp telemetry disclosure` (opt-IN framing, controller/contact/legal-basis/retention/rights/erasure-channel, no dead link) + LIA draft in-repo + erasure process bounded to the statutory month | S3 | `internal/telemetry/cli.go`, `internal/telemetry/config.go` (notice text), `docs/telemetry-lia.md`, `docs/telemetry-runbook.md` (erasure §) | unit + docs | owner | pending |
| S6 | Live production exposure (owner-run): reverify live replica bounds + rollout-overlap; deploy S2+S4 binary PRIVATE first; verify health/CH/RemoteAddr; enable public LAST; probe headers + global-bucket; subdomain-off rollback on mismatch | S1,S2,S4 | `../zcp-telemetry/import-project.yaml` (outside repo) + live platform | owner | pending |

D2 outcome folds into the register at Gate 1: **(a) override-only** ⇒ no new
slice, S6 documents the `ZCP_TELEMETRY_ENDPOINT` test contract; **(b) release**
⇒ add **S7** (OPEN-1 domain + `DefaultEndpoint` update + two-process smoke +
release; Depends S3,S6).

Waves (write-sets disjoint within each): **W1**=S1 (tracer bullet) · **W2**=S2
∥ S3 (handler.go vs config.go/cli.go — disjoint) · **W3**=S4 (Depends S2, shares
handler.go) ∥ nothing · **W4**=S5 (Depends S3, shares cli.go/config.go) ·
**W5**=S6 (owner-run live). S3 lands before S5; S2 before S4.

## Verify Trace (R2)
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | rebased tree: `go test ./... -race -short` + `make lint-local` green; `git merge-tree --write-tree main <integration>` empty | not-run | — |
| — | `TestRunSyncPull_PartialCorpus_ExitsNonzero` (main's failure propagation survives) + `TestEvalLocal_CaptureMarkers_Preserved` | not-run | — |
| — | `TestRunCLI_CaptureVerb_EmitsCliCommand` (capture dispatches + emits) | not-run | — |
| AC2 | config-precedence table: unset ⇒ Enabled=false, `ReasonDefaultOff`, no install ID, no stamp, nothing printed; opt-in token ⇒ enabled path | not-run | — |
| — | regression: `=0`/`false`/`off`/`no` ⇒ `ReasonOptedOutEnvTelemetry`; DNT=1 wins; install-file disabled honored; `isTruthy` semantics for DNT/CI/debug UNCHANGED | not-run | — |
| AC3 | handler test: spoofed `X-Forwarded-For` does NOT change the limiter key; `X-Real-IP` sets it (netip-canonical); malformed/multi-value X-Real-IP ⇒ RemoteAddr fallback | not-run | — |
| — | negative: two requests, different spoofed leftmost XFF, share one bucket (cannot escape per-IP limit) | not-run | — |
| AC4 | `TestServer_OpsRoutesNotPublicUnbounded` (/healthz + /statsz isolated or rate-limited); server has Read/Write/Idle timeouts set | not-run | — |
| AC5 | external curl lands an event; NEW `/statsz` accepted-events counter increments; Grafana pipeline-health shows it; subdomain-off rollback exercised once live | not-run | — |
| AC6 | `zcp telemetry disclosure` prints the full Art-13 notice (assert controller/legal-basis/retention/rights/erasure-channel substrings, opt-IN wording, no dead link); `docs/telemetry-lia.md` exists; runbook erasure § names the statutory month | not-run | — |
| AC7 | two-process e2e: unset ⇒ no events + no install ID; 1st `=1` ⇒ disclosure printed, no events; 2nd `=1` ⇒ events; `=0`/DNT/`disable` each suppress (ZCP_TELEMETRY_DEBUG + spool) | not-run | — |
| AC8 | soak readout: `/statsz` exposes accepted-events + schema-reject + 5xx totals; reject/5xx stay flat under v1 test traffic | not-run | — |

## Promotion (R2)
- Contracts → `docs/spec-telemetry.md §3.1` (v1 default-off/opt-in precedence
  rule + `ReasonDefaultOff` + D1 enable-contract). `§6` (client-IP =
  netip-canonical balancer-authoritative X-Real-IP, client-XFF ignored, VXLAN
  threat model, subdomain single-bucket + custom-domain v2 precondition;
  ops-route hardening + server timeouts). `§4.4/§6` (new `/statsz` counters:
  accepted-events, schema-reject, 5xx). `§3.5` (`zcp telemetry disclosure`
  surface + D1 enable semantics). `§7`/runbook (erasure = statutory-month
  process; aggregate-retention anonymous-vs-pseudonymous classification).
- Invariants → `docs/spec-telemetry.md §9`: `TestResolve_TelemetryUnset_DisabledByDefault`,
  `TestResolve_ExplicitOptIn_SecondProcessEmits`, `TestClientIP_SpoofedXFF_Ignored`,
  `TestClientIP_MalformedXRealIP_FallsBackRemoteAddr`, `TestServer_OpsRoutesNotPublicUnbounded`,
  `TestRunSyncPull_PartialCorpus_ExitsNonzero`, `TestIngestAllowlist_ArchPin` (engine-independent depguard pin).
- CLAUDE.md trap line (≤1): "Ingest `clientIP()` keys on balancer-authoritative
  `X-Real-IP` (netip-canonical, RemoteAddr fallback), NEVER client-supplied
  X-Forwarded-For — leftmost XFF is spoofable at the public edge; every new
  IP-rate-limited ingest route routes through it. `TestClientIP_SpoofedXFF_Ignored`." (spec §6)
- This plan → `plans/archive/` on LAND close
