# Retest pack: onboard-agent-mode

Zero-context bar: every step runs from the worktree
`/Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/effervescent-imagining-melody`
(branch `feat/onboard-agent-mode`), no other file open, in minutes.

## Run

| command | expected line |
|---|---|
| `go test ./internal/content -run 'TestBuildAgentsMD_Onboarding\|TestRefreshAgentContext_OnboardingPreserved' -short -count=1 -v` | `PASS` (gate 8 subcases, ordering, trigger copy, refresh) |
| `go test ./internal/tools -run 'TestKnowledgeTool_PlaybookURI_FetchesEmbedded\|TestPlaybookOnboarding_ContentPins_CoreContract' -short -count=1 -v` | `PASS` (fetch + 16 content pins) |
| `go test ./internal/knowledge -run TestSearch_ExcludesPlaybooks_NoHits -short -count=1 -v` | `PASS` (needs synced corpus — already copied into this worktree) |
| `go test ./internal/content -run 'TestNoBareZeropsURIInAgentContent\|TestTemplatesContent_NoHardcodedVersions\|TestEvalScenarioManifest' -short -count=1 -v` | `PASS` |
| `go test ./internal/eval -run TestOnboardingScenarios_ExistAndParse -short -count=1 -v` | `PASS` (5 scenarios) |
| `make lint-fast` | `0 issues.` |

## Drive

1. AC2 — `mkdir -p /tmp/retest-onboard && cd /tmp/retest-onboard && go run github.com/zeropsio/zcp/cmd/zcp init` won't resolve outside the module; instead: `cd <worktree> && go build -o /tmp/zcp-retest ./cmd/zcp && cd $(mktemp -d) && /tmp/zcp-retest init && grep -n "## Zerops onboarding" AGENTS.md && grep -n "## Route every user turn" AGENTS.md` — expect: onboarding line number LOWER than the routing line number.
2. AC2 (negative) — same in a second mktemp dir with `ZCP_AUTHORING=1 /tmp/zcp-retest init` — expect: `grep -c "## Zerops onboarding" AGENTS.md` prints `0`.
3. AC3 — from any ZCP-bound agent session (e.g. this Claude session on localflow): call `zerops_knowledge uri="zerops://playbooks/onboarding"` — expect: the playbook body starting `# Onboard me to Zerops — conversation playbook` with the three forks.
4. AC1/AC4/AC5 (the headline product moment) — in a FRESH agent session on any bound project, type exactly: `onboard me to Zerops` — expect: playbook fetch → status → discover → a short greeting + **Bring an app** / **Start something new** / **Take a quick tour** (+ **Continue this project** on a populated project like localflow), and NO provisioning/import/deploy without your explicit yes. Note: the session must have a REGENERATED AGENTS.md (run `zcp init` first — AGENTS.md is a generated artifact; an old session's file predates the block).
5. AC1 (negative) — same session: `help me get started with PostgreSQL` — expect: normal routing (no onboarding fork).

## What changed

- S1: new committed knowledge family `internal/knowledge/playbooks/` (embed + knowledgeDirs) with `zerops://playbooks/onboarding`, excluded from `Store.Search` (direct-fetch-only).
- S2: `agents_onboarding.md` template spliced into `BuildAgentsMD` between env preamble and shared routing, gated `!rt.Authoring`.
- S3: full playbook content (state resolution, fork copy, branch playbooks, boundaries) + 16 content pins + agent-content lints extended over `templates/` + `playbooks/`.
- S5: five behavioral-eval scenarios (fresh/variant/negative/populated/guided-on incl. preseed script) + existence/parse pin.
- S6: two stale adoptionState enum comments fixed (five→six, bootstrapping listed).

## Rollback

`git revert 01367d13..0e99714c` on `feat/onboard-agent-mode` (or simply don't merge the branch — main is untouched). No platform state to undo; no follow-up needed.

## Docs

Promoted at GATE 1: `docs/spec-onboarding.md` §1 (trigger), §2 (state resolution), §3 (conversation + consent), §4 (branches), §5 (content home), §6 (guided/authoring interplay), §7 (invariants O1-O7).

## Deferred (owner-visible)

- Live flow-eval of the 5 scenarios: run post-LAND against the disposable `eval-zcp` project (this session's identity resolves to localflow — CleanupProject would delete its services).
- Side-finding, out of scope: `zcp init --help` treats the unknown flag as plain `init` and RUNS the init in cwd (dirtied the worktree during verification; restored). Worth a small CLI fix.
- Note: committed AGENTS.md files don't carry the block until re-init — it's a generated artifact; container fleets get it via the normal init-on-provision path.
