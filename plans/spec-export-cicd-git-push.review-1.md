# Review Report: spec-export-cicd-git-push.md — Review 1
**Date**: 2026-04-04
**Reviewed version**: `plans/spec-export-cicd-git-push.md`
**Agents**: kb (zerops-knowledge), verifier (platform-verifier), primary (Explore), adversarial (Explore)
**Complexity**: Deep (ultrathink) — 4 agents
**Focus**: Evaluate shortcomings, problems, or confirm the spec is sound

## Evidence Summary
| Agent | Findings | Verified | Logical | Unverified | Downgrades |
|-------|----------|----------|---------|------------|------------|
| kb | 9 platform claims | 8 | 1 | 0 | 0 |
| verifier | 6 claims tested | 5 | 0 | 1 | 0 |
| primary | 18 core claims + 6 recommendations | 15 | 2 | 1 | 0 |
| adversarial | 7 confirmed, 5 missed | 10 | 2 | 0 | 1 |

**Overall**: **SOUND with CONCERNS** — the spec is well-structured and architecturally correct. The core design (LLM commits / tool pushes, .netrc auth, strategy integration) is validated. **1 blocking ambiguity** and **9 actionable issues** found.

---

## Knowledge Base

### Zerops Platform Facts

| Claim | Status | Source |
|-------|--------|--------|
| `.git/` preserved with `deployFiles: [.]` + `-g` | VERIFIED (nuanced) | KB: zerops-docs `zcli/commands.mdx:211`, `specification.mdx:189-231` |
| Project-level env vars persist across deploys | VERIFIED | Verifier: live test + KB: `env-variables.mdx:88-121` |
| `.netrc` auth pattern (oauth2 + token) | VERIFIED | KB: standard git practice, not Zerops-specific |
| `buildFromGit` is real import.yaml feature | VERIFIED | KB: `import.mdx:398-403` — **one-time build only** |
| Deploy = new container, env vars persist | VERIFIED | KB: `deploy-process.mdx:49-54` |
| Git available in containers | VERIFIED (code evidence) | Verifier: `deploy_ssh.go:171-178` runs git commands in production |
| SSH access works (with VPN) | VERIFIED | Verifier: live test, requires `zcli vpn up` |
| `zcli push` ignores git remotes | VERIFIED | KB: `commands.mdx:201-223` — no remote-related flags |

### KB Nuances (important for spec accuracy)

1. **`.git/` persistence framing is misleading**: Every deploy creates new containers from the artefact. `.git/` is "preserved" because it's included in the artefact (via `-g` + `deployFiles: ./`), not because of persistent storage. **Commits made inside a running container are LOST on next deploy unless pushed to a remote first.** The spec's §5.1 should emphasize this.

2. **`buildFromGit` is one-time**: It triggers a single build at import time, NOT continuous deployment. The spec uses it correctly in export import.yaml (§6.3 Step 4) for creating importable infrastructure, not for CI/CD. No issue, but worth noting.

3. **Project-level GIT_TOKEN visible to ALL services**: `zerops_env project=true` sets the var on every service in the project. Consider whether service-level `envSecrets` would be more appropriate for a git token.

---

## Agent Reports

### Primary Analyst Review
**Assessment**: SOUND — with minor clarifications needed
**Evidence basis**: 15 of 18 core claims VERIFIED

#### Verified Architecture (all correct)
- `buildSSHCommand` structure at `deploy_ssh.go:160-194` — matches spec §2.1
- `DeploySSHInput` at `tools/deploy_ssh.go:14-20` — 5 fields, matches spec §3.1 "before"
- 3 strategy constants at `service_meta.go:13-17` — matches spec §9.2 "before"
- All strategy maps (StrategyToSection, strategyDescriptions, validStrategies, strategyOfferings) verified at stated locations
- `shellQuote` pattern at `deploy_ssh.go:22-26` — exists, spec should use it for remoteUrl
- Self-deploy `.git` auto-force at `deploy_ssh.go:63-65` — correct
- `deploy.md` sections verified: 11 sections exist, no `deploy-git-push` yet — matches spec §12

#### Previously flagged as "Blocking Ambiguity" — RESOLVED

- **[C5] ~~Is `git-push` a persistent DeployStrategy or transient action?~~** — **RESOLVED: persistent strategy, spec is correct.**
  - Spec §9.1 correctly distinguishes "DeployStrategy (long-term)" from "strategy param (action)"
  - §9.2 correctly lists `git-push` as **fourth long-term strategy** — this drives guidance selection via `StrategyToSection` → `deploy.md` section extraction → LLM receives instructions on how to deploy
  - The `strategy` param on `zerops_deploy` is the implementation-level action; the `DeployStrategy` in `ServiceMeta` is the persistent decision that determines which guidance the LLM receives
  - **Phase 2 with 10 files is correct.** No ambiguity — the analysts misunderstood the guidance delivery model.
  - Evidence: [VERIFIED: `deploy_guidance.go:10-15` StrategyToSection drives section extraction, `service_meta.go:26` DeployStrategy is persistent]

### Adversarial Challenge

#### Confirmed Findings (survived challenge)

| # | Finding | Severity | Evidence |
|---|---------|----------|----------|
| C1 | `.netrc` cleanup: `rm -f` won't run if `git push` fails due to `&&` chaining | MAJOR | Spec §3.3 lines 112-135, shell `&&` semantics |
| C2 | `remoteUrl` not shell-quoted — injection risk | MAJOR | `deploy_ssh.go:167` uses `shellQuote()`, spec doesn't for URL |
| C3 | `classifySSHError` has no git-push error patterns | MAJOR | `deploy_classify.go:26-67` only classifies zcli-push failures |
| C4 | `pollDeployBuild` called unconditionally — git-push would hang | MAJOR | `tools/deploy_ssh.go:56` always polls |
| C6 | `DeploySSH()` growing to 13 params | MAJOR | `deploy_ssh.go:34-45` has 10 params already |
| C8 | `deploy_ssh.go` at 194 lines + additions → near 350 limit | MAJOR | `wc -l` verified |

#### Adversarial Missed Findings

| # | Finding | Severity | Evidence |
|---|---------|----------|----------|
| MF1 | `ExportState` exists (`state.go:23`) but never instantiated or persisted in export workflow. No task in spec for state persistence. Export cannot resume across sessions. | CRITICAL | `workflow/state.go:22-31`, absent from spec §10 |
| MF2 | No validation of `strategy` enum in `DeploySSHInput`. User can pass `strategy="garbage"`. Spec §3.2 has validation rules but §10 Task 1.7 doesn't mention invalid strategy test. | MAJOR | `tools/deploy_ssh.go:14-20` has no validation |
| MF3 | No `parseGitHost()` function specified. Spec §3.3 says "parse host from remoteUrl" but no regex, no edge cases (SSH URLs, embedded auth, custom domains). | MAJOR | Spec §3.3 lines 148-151 |
| MF4 | Fallback commit message hardcoded as `'initial commit'`. Conflicts with §7.2 guidance saying "meaningful messages". Minor UX inconsistency. | MINOR | Spec §3.3 line 128 |
| MF5 | No `umask 077` before `.netrc` creation. Default umask could make token world-readable on some systems. | MINOR | Spec §4.2 line 212 |

#### Duplicate §7.3
- Spec lines 435 and 446 both numbered §7.3. Second should be §7.4. [VERIFIED]

---

## Evidence-Based Resolution

### Previously blocking — RESOLVED

| # | Finding | Resolution |
|---|---------|------------|
| ~~C5~~ | ~~Is `git-push` a persistent DeployStrategy or transient action?~~ | **RESOLVED: persistent strategy.** `DeployStrategy` in `ServiceMeta` drives guidance via `StrategyToSection` → `deploy.md` section. Phase 2 with 10 files is correct. |

### High priority (resolve before implementation)

| # | Finding | Resolution |
|---|---------|------------|
| MF1 | `ExportState` never persisted | Add explicit tasks for state persistence in Phase 3, or remove `ExportState` if export stays stateless. |

### Must fix (MAJOR, verified)

| # | Finding | Fix |
|---|---------|-----|
| C1 | `.netrc` survives on push failure | Use `trap 'rm -f ~/.netrc' EXIT` instead of trailing `rm` |
| C2 | `remoteUrl` shell injection | Add `shellQuote(remoteUrl)` — matches existing `deploy_ssh.go:167` pattern |
| C4 | `pollDeployBuild` hangs on git-push | Skip polling when strategy="git-push", return `GitPushResult` directly |
| C6 | 13-param function signature | Define `DeploySSHOptions` struct |
| MF2 | No strategy enum validation | Add validation in handler + unit test for invalid strategy |
| MF3 | No host parsing logic specified | Define `parseGitHost()` with test cases: GitHub, GitLab, custom domains, SSH URLs |
| C3 | `classifySSHError` incomplete | Add git-push patterns or rely on existing `Diagnostic` field passthrough |
| C8 | File size near limit | Plan `deploy_git_push.go` as separate file (follows `deploy_classify.go` precedent) |

### Should fix (MINOR)

| # | Finding | Fix |
|---|---------|-----|
| MF5 | `.netrc` umask | Add `umask 077` before `echo ... > ~/.netrc` |
| C14 | GIT_TOKEN as envSecret vs envVariable | Specify envSecrets for token storage |
| MF4 | Hardcoded `'initial commit'` | Document limitation in guidance — acceptable for init case |
| §7.3 | Duplicate section numbering | Renumber to §7.4 |
| C7 | Missing `annotations_test.go` in test plan | Add to §14 |
| C11 | Missing strategy tests in §14 | Add router_test.go, deploy_guidance_test.go |

### Recommendations (ordered by impact)

| # | Recommendation | Priority | Effort |
|---|---------------|----------|--------|
| ~~R1~~ | ~~Resolve git-push as Strategy vs Action (C5)~~ | ~~RESOLVED~~ | Persistent strategy, Phase 2 correct |
| R2 | Define `ExportState` persistence or remove it (MF1) | HIGH | Small |
| R3 | Use `trap` for .netrc cleanup + `umask 077` (C1, MF5) | HIGH | Trivial — 2 lines |
| R4 | Add `shellQuote(remoteUrl)` (C2) | HIGH | Trivial |
| R5 | Address `pollDeployBuild` skip + strategy validation (C4, MF2) | HIGH | Small |
| R6 | Define `parseGitHost()` spec + edge cases (MF3) | MEDIUM | Small |
| R7 | Plan file splits: `deploy_git_push.go` + options struct (C6, C8) | MEDIUM | Medium |

---

## Change Log
| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | §9.1–§9.2 | Clarify: is git-push a persistent strategy or transient action? | §9.1 vs §9.2 internal tension | primary [C5] |
| 2 | §6/§10 | Define ExportState persistence or mark export as stateless | `state.go:22-31` exists unused | adversarial [MF1] |
| 3 | §3.3 | Use `trap 'rm -f ~/.netrc' EXIT` + `umask 077` | Shell best practice | primary [C1], adversarial [MF5] |
| 4 | §3.3 | Apply `shellQuote()` to remoteUrl | `deploy_ssh.go:167` pattern | primary [C2] |
| 5 | §3.6 | Skip `pollDeployBuild` for git-push; return `GitPushResult` directly | `tools/deploy_ssh.go:56` | primary [C4] |
| 6 | §3.2 | Add strategy enum validation + test for invalid values | No validation in current spec | adversarial [MF2] |
| 7 | §3.3 | Specify `parseGitHost()` function with edge case test cases | Spec assumes trivial parse | adversarial [MF3] |
| 8 | §2.3 | Add `deploy_git_push.go` to affected files; define `DeploySSHOptions` struct | 194 lines + 10 params | primary [C6, C8] |
| 9 | §4.1 | Note envSecrets (not envVariables) for GIT_TOKEN | Security best practice | primary [C14], KB nuance |
| 10 | §5.1 | Emphasize: commits inside container LOST on next deploy unless pushed | KB: deploy = new container from artefact | KB nuance |
| 11 | §7.3 | Fix duplicate section numbering → §7.4 | Spec lines 435/446 | primary [C5 minor] |
| 12 | §14 | Add: annotations_test.go, router_test.go, deploy_guidance_test.go | CLAUDE.md requirements | primary [C7, C11] |
