# Slice brief: S8 — onboarding copy: imperative example + authored recipe footprint + single-consent rule

Self-contained: no other file is required to execute this. Cite spec §s, never the plan.

**Defects (live owner test 2026-08-04, Codex agent in a fresh project):**
1. Menu bullet example is a bare noun phrase `("a weather dashboard in Bun")` — the owner wants an imperative command shape: `("create a weather dashboard in Bun")`.
2. The agent GUESSED the provisioning footprint from the user's idea ("No database is needed") instead of stating the recipe's fixed shape, then after committing the route discovered the recipe ships a PostgreSQL db and had to re-ask consent — the user was asked the same thing twice. The playbook's footprint-consent step never tells the agent what the footprint IS.

**Outcome** (observable): the menu block's Build-something bullet reads `("create a weather dashboard in Bun")`; the playbook's consent step states that the footprint to disclose is the RECIPE'S OWN fixed shape — every mapped recipe provisions a dev service + a stage service + a small PostgreSQL database (the scaffold ships it even when the idea doesn't need one) — with an explicit rule: never infer the footprint from the idea, and ask for consent ONCE; re-ask only when the confirmed shape CHANGES after commit (EXISTS flip on a managed dependency, or populated-project mixing). Pins and spec updated in the same commit.

**Allowed scope**
- Files: `internal/knowledge/playbooks/onboarding.md`, `internal/tools/knowledge_playbook_content_test.go`, `docs/spec-onboarding.md` (§3 menu copy + staged-consent wording; §4 if the footprint sentence lands there)
- Explicitly excluded: orientation.md, scenarios (unless a scenario asserts the old example string — grep `eval/behavioral/scenarios/onboard-*.md` for `weather dashboard` and update ONLY if a positive assertion breaks), workflow/sync code, deploy error paths.

**Spec citations**: `docs/spec-onboarding.md` §3 (verbatim menu block — you are amending the authored copy itself; spec text, playbook text, and pin needles move in the SAME commit, per O3) and §3 staged-consent sequence (amend step 1 wording: footprint = recipe shape, stated not guessed; single ask) + §7 O3 row if needle wording changes.

**Exact copy changes**
1. Menu bullet: `- **Build something** — describe an idea in one line, with a technology if you care ("create a weather dashboard in Bun"); I set up the environment from a ready-made recipe and build it with you to a live URL.` (only the quoted example changes).
2. Consent step 1 (FOOTPRINT CONSENT) gains authored substance, e.g.: "State the recipe's fixed footprint — every mapped recipe provisions a dev service (your workbench), a stage service (serves the public URL), and a small PostgreSQL database; the scaffold ships the database even when the idea doesn't need one. Never infer the footprint from the idea. Ask once; re-ask ONLY if the confirmed shape changes after commit (a dependency flips to EXISTS, or the project is populated)." Adjust the existing step-3 (RENEWED CONSENT ON EXISTS) so the two read as one coherent rule, not two independent asks.

**RED test list** (extend `TestPlaybookOnboarding_ContentPins_CoreContract`; migrate stale needles in the same commit)
- required needle: `("create a weather dashboard in Bun")` — layer: tool
- forbidden needle: `("a weather dashboard in Bun")` (the old example) — layer: tool
- required needles for the footprint rule: `Never infer the footprint from the idea` (or the exact sentence you land) and an `Ask once` / re-ask-condition needle — layer: tool

**Protocol**: RED → GREEN → REFACTOR.
1. RED: `go test ./internal/tools -run TestPlaybookOnboarding -short -count=1 -v`
2. Edit playbook + spec until green; run `go test ./internal/tools ./internal/content/... -short -count=1` and `go test ./internal/eval -run TestOnboardingScenarios -short -count=1` (scenario guard).
3. `make lint-fast`.

**Report contract**: RED + GREEN outputs with exit codes · files touched · layer-matrix lines (tool + content + eval guard) · independent-oracle note (copy literals from this brief's owner-approved wording, needles not read back from the playbook).

**Stop conditions**: scope drift · material unknown · AC change · repeated unexplained failure. Your worktree branches from origin/main — verify it contains the v3 playbook (menu block with **Build something**); if not, `git merge origin/main` equivalent per coordinator instruction and re-check.

**Definition of Done**
- [ ] RED replay: fails at base, passes at head
- [ ] Named tests pass with `-count=1 -v`
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
