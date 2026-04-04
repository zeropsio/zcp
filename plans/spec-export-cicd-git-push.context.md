# Context: spec-export-cicd-git-push
**Last updated**: 2026-04-04
**Iterations**: 1
**Task type**: document-review

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Spec architecture is SOUND | KB verified all platform claims, code review confirms feasibility | 1 | LLM-commits/tool-pushes split, .netrc auth, strategy integration all correct |
| D7 | Rename ci-cd → push-git (3 strategies, not 4) | Strategy = transport mechanism; CI/CD is config, not strategy | 1 | push-dev/push-git/manual is cleaner taxonomy |
| D8 | No explicit CI/CD state tracking | Guidance covers both paths generically | 1 | Avoids stale state; LLM observes actual behavior |
| D9 | Strategy name ≠ tool param: push-git vs git-push | push-* pattern for strategies, git operation for tool | 1 | Naming consistency |
| D2 | Use `trap` + `umask 077` for .netrc | Shell best practice, dev containers persist, umask varies | 1 | `&&` chaining leaves .netrc on failure; umask could expose token |
| D3 | `shellQuote()` required for remoteUrl | `deploy_ssh.go:167` existing pattern | 1 | Shell injection prevention |
| D4 | Options struct needed for DeploySSH | 10+ params exceeds clean code threshold | 1 | Go convention for functions with many params |
| D5 | Separate file `deploy_git_push.go` | 194 lines + additions near 350 limit | 1 | Follow `deploy_classify.go` precedent |
| D6 | `.git/` persistence = artefact inclusion, not filesystem persistence | KB: deploy = new container from artefact | 1 | Spec framing could mislead; commits lost unless pushed |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|-------------|-----------------|-----------|--------------|
| — | None rejected yet | — | 1 | Core design is sound, only details need adjustment |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| RC1 | `instructions_orientation.go` might not exist | Glob confirmed it exists | 1 | 1 | File verified at `internal/server/instructions_orientation.go` |
| RC2 | `GIT_HISTORY_CONFLICT` detection needs auth | `.netrc` created before `git ls-remote` in command sequence | 1 | 1 | Design is correct — auth available when needed |
| RC3 | `buildFromGit` might not be real | KB verified: `import.mdx:398-403` | 1 | 1 | Real feature, one-time build at import |
| RC4 | `bootstrap_guide_assembly.go` might not exist | Grep found `BuildTransitionMessage` at line 63 | 1 | 1 | File exists, transition message logic confirmed |

## Open Questions (require author decision)
1. ~~**BLOCKING**: Is `git-push` a persistent DeployStrategy or transient action?~~ **RESOLVED**: persistent strategy. `ServiceMeta.DeployStrategy` drives guidance via `StrategyToSection` → deploy.md section extraction. Phase 2 with 10 files is correct.
2. **HIGH**: Should `ExportState` be persisted (stateful export) or should export remain stateless? `state.go:23` struct exists but is never written. See review MF1.
3. **MINOR**: Branch name validation — Git rejects invalid names natively. Is additional ZCP validation needed?
4. **MINOR**: Local mode git-push (Open Question #1) — spec recommends it but no implementation task exists.

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|--------------|------------|----------------|
| Core architecture (LLM commits / tool pushes) | HIGH | KB verified, code reviewed, all 4 agents agree |
| .netrc auth model | HIGH | KB verified standard pattern, security sound |
| Strategy integration (4th strategy) | HIGH | All 10 files verified, persistent strategy confirmed — Phase 2 scope correct |
| Error handling model | MEDIUM | `classifySSHError` needs git-push patterns, `parseGitHost` unspecified |
| `pollDeployBuild` interaction | MEDIUM | Identified gap, fix is straightforward |
| Export workflow (§6) | MEDIUM | ExportState persistence unclear |
| Test plan completeness | MEDIUM | Missing annotations_test, strategy tests, invalid-input tests |
| Platform claims | HIGH | 8/9 verified by KB + verifier live tests |
