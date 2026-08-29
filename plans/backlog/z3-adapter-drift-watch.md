# z3 adapter drift watch — canary + GitHub-issue notifications

**Surfaced**: 2026-08-28, fork-methodology design (`plans/z3-fork-methodology-2026-08-28.md` v2,
Codex second opinion). Karel: "the proposal is good, GitHub-issue notifications feel right; we
finish the design later."

**Why deferred**: it presupposes the freeze stream's adapter SPI + recorded fixtures (the canary IS
the fixture recorder on a timer) and a dedicated canary identity nobody has decided yet. Until the
SPI exists there is nothing for the watch to compare against.

**Trigger to promote**:
- the freeze stream lands the adapter SPI + fixtures (`../z3/docs/internals/zerops/fork.md` §3.2), OR
- the first vendor-CLI break reaches a user before us (the cost the watch is meant to remove), OR
- the first upstream port turns out to be more than a mechanical cherry-pick.

## Sketch

Three signals → one channel → every alert ends in a compatibility-tested PR, not a toast.

1. **Upstream-diff signal** — daily GitHub Action in the fork: `git diff --stat <last-reviewed>..upstream/main --
   <imported + ported paths>`; non-empty → one issue "provider drift" (deduplicated by upstream SHA)
   listing files + commit subjects.
2. **Vendor-release signal** — the same job compares npm `@anthropic-ai/claude-code` and
   `@openai/codex` latest against the compatibility matrix (ported SHA × Claude CLI × Codex CLI ×
   Effect). Inside the server, upstream's `providerMaintenance` already emits a
   `ServerProviderVersionAdvisory` ("newer CLI available") — forward it from the container to a zcp
   notify hook instead of only toasting.
3. **Live canary** — a timer in `z3-eval` (or a dedicated `z3-canary` project): a canned turn through
   the real adapter with the really installed CLI (`claude -p` calling two `zerops_*` tools; a Codex
   twin) — asserts the turn completes, tool results arrive, the envelope is extracted; **re-records
   the raw event stream and diffs it against the checked-in fixtures**, so a vendor changing an
   event shape shows up as a fixture diff before anything visibly fails.

**Canary identity**: a dedicated subscription account (company mailbox; Claude Pro + ChatGPT Plus
class plans), credentials as secrets on the canary container only — never a copy of a person's
login (usage on a timer, refresh races, Q-12). Subscription rather than an API key because the
product path is subscription and plan/usage events differ. Owner decides whose mailbox / who pays.

**Channel**: GitHub issue in the fork = system of record (lives with the code, deduplicated, has
history, agent-readable: an agent takes the issue, ports, opens the PR referencing it); Slack only as
a mirror via the GitHub→Slack integration; e-mail never. Phase 2 "dynamic reaction": the issue
triggers an agent automatically (scheduled cloud agent / GH Action running the port) — the owner
reads PRs, not alerts. Alert closure/SLA: who closes an issue, by when — undecided.

**Model manifest**: upstream refreshes `model-manifest.json` at runtime from *their* `main`
(raw.githubusercontent.com) — unreviewed mutable production configuration; mirror a digest-pinned
copy under z3 control and let signal 1 watch the upstream file.

## Refs
- `plans/z3-fork-methodology-2026-08-28.md` §7 (v2) — until promoted to `../z3/docs/internals/zerops/fork.md`
- `../z3/apps/server/src/provider/providerMaintenance.ts` (version advisory), `ModelManifest.ts` (runtime fetch)
- ledger `../z3/docs/internals/zerops/verified.md` S0.12 (the probe the canary reuses), `questions.md` Q-12
