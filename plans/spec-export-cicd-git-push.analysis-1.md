# Mental Model Test: Strategy Redesign (push-dev / push-git / manual)
**Date**: 2026-04-04

## System State After Changes

```
Strategies: push-dev, push-git, manual
Workflows:  bootstrap, deploy, cicd, export, recipe
Router:     push-git → [deploy(P1), cicd(P2), export(P3)]
            push-dev → [deploy(P1)]
            manual   → [] (direct zerops_deploy)
Tool:       zerops_deploy strategy="git-push" (action parameter)
```

---

## Scenario 1: Fresh bootstrap → push-git (just git, nothing else)

**User:** "Set up a Node.js app with PostgreSQL. I just want code on GitHub."

```
bootstrap start → provision nodejsdev + nodejsstage + db
bootstrap close → "Choose strategy: push-dev, push-git, manual"
user picks push-git
→ ServiceMeta.DeployStrategy = "push-git"
→ guidance from deploy-push-git section
→ router offers: deploy(P1), cicd(P2), export(P3)

user starts deploy workflow
→ deploy-push-git guidance:
  "First time:
   1. Get GIT_TOKEN → zerops_env action=set project=true
   2. Commit: ssh nodejsdev 'cd /var/www && git add -A && git commit -m ...'
   3. Push: zerops_deploy targetService=nodejsdev strategy=git-push remoteUrl=https://..."

user does it. Code is on GitHub. Done. No CI/CD, no export.
```

**Verdict:** ✓ Works. User picks push-git, pushes to git, ignores cicd/export offerings.

**Gap found:** The `deploy-push-git` section in spec §12 mentions "GIT_TOKEN set as project env var" as prerequisite but doesn't walk through setup. It should include first-time guidance (ask user for token → zerops_env). See **Fix 1** below.

---

## Scenario 2: Adoption → push-git

**User:** "I have running services on Zerops, adopt them and push to GitHub"

```
bootstrap start (isExisting=true) → registers appdev, appstage, db
bootstrap close → strategy selection
user picks push-git
→ service has .git (from previous zcli push deploys) but no remote (state S1)

user starts deploy
→ push-git guidance: commit, push with remoteUrl
→ zerops_deploy strategy=git-push remoteUrl=... → tool adds remote, pushes
→ all existing deploy history goes to GitHub
```

**Verdict:** ✓ Clean. Existing .git history preserved and pushed.

---

## Scenario 3: push-git service → add CI/CD later

**User:** "I've been pushing to GitHub manually. Now set up CI/CD."

```
appdev: push-git, remote configured, pushing to GitHub

router offers: deploy(P1), cicd(P2), export(P3)
user starts cicd workflow

cicd.md:
→ buildCICDContext() filters by StrategyPushGit → finds appdev ✓
→ git state: S2 (remote exists) → skips git setup
→ guides: GitHub Actions OR webhook setup
→ user configures Actions
→ verifies: zerops_events shows build triggered

next deploy: push-git guidance says "push → CI/CD triggers automatically"
```

**Verdict:** ✓ CI/CD is additive. No strategy change. Works exactly as described.

---

## Scenario 4: push-git service → export to SAME repo

**User:** "Generate an import.yaml for my project"

```
appdev: push-git, pushing to github.com/user/myapp

user starts export workflow
→ discovers S2 (git + remote exist)
→ skips git setup entirely
→ generates import.yaml with buildFromGit: https://github.com/user/myapp
→ presents to user for review
```

**Verdict:** ✓ Perfect. Export just reads existing state and generates the YAML.

---

## Scenario 5: push-git service → export to DIFFERENT repo

**User:** "Create a separate clean repo for infrastructure export"

```
appdev: push-git, remote → github.com/user/myapp
user wants to push to github.com/user/myapp-infra (separate repo)

user starts export workflow
→ discovers S2 (remote exists)
→ export.md S2 path: "Note the remote URL, verify zerops.yml, skip to Generate"
→ generates import.yaml with buildFromGit: github.com/user/myapp (existing!)

user: "No, I want a different repo"
→ ???
```

**Problem:** Export S2 path assumes existing remote IS the target. To push to a different repo, the user would need to manually change the remote or use a separate flow.

**But is this realistic?** If the user already has code on GitHub (S2), why would they need to push to a DIFFERENT repo? The import.yaml just needs a `buildFromGit` URL — it can point to any repo, not necessarily the one the container pushes to.

**Resolution:** The user can manually edit the import.yaml to point buildFromGit to any URL. Or the LLM can generate it with a custom URL. The export workflow doesn't need to push — it just generates the YAML.

**Verdict:** ✓ Works. Edge case handled by import.yaml being editable. No code change needed.

---

## Scenario 6: Switch push-dev → push-git

**User:** "I want to start pushing to GitHub instead of zcli push"

```
appdev: push-dev → switching to push-git

zerops_workflow action="strategy" strategies={"appdev":"push-git"}
→ ServiceMeta updated
→ guidance from deploy-push-git
→ .git exists (from zcli push auto-init), no remote
→ first push: needs GIT_TOKEN + remoteUrl
→ all previous deploy commits go to GitHub
```

**Verdict:** ✓ Smooth transition. zcli push ignores remotes, git push ignores zcli — no conflicts.

---

## Scenario 7: Switch push-git → push-dev

**User:** "Don't need GitHub anymore, just deploy directly"

```
appdev: push-git → switching to push-dev

zerops_workflow action="strategy" strategies={"appdev":"push-dev"}
→ ServiceMeta updated
→ router: push-dev → offers deploy only
→ guidance: zerops_deploy (zcli push)
→ .git still has remote configured — harmless, zcli push ignores it
```

**Verdict:** ✓ Clean switch. No cleanup needed.

---

## Scenario 8: No strategy set → user wants export

**User:** "Create a repo from my running service" (no strategy selected yet)

```
services exist but DeployStrategy is empty

router: no strategy → strategyOfferings returns nil → no export offered
BUT: user can explicitly start export:
  zerops_workflow action="start" workflow="export"
→ export runs regardless of strategy
→ discovers state, sets up git, pushes, generates import.yaml
```

**Gap found:** Export workflow isn't discoverable via router without a strategy set. The LLM handles intent matching ("create a repo" → starts export), so this isn't blocking. But after export completes, the close section should suggest setting push-git strategy.

**Verdict:** ✓ Works functionally. Minor discoverability gap. See **Fix 2** below.

---

## Scenario 9: Fresh project → CI/CD from the start

**User:** "Set up Node.js with CI/CD from GitHub from day one"

```
bootstrap → push-git strategy
router offers: deploy(P1), cicd(P2), export(P3)

Path A — user starts cicd workflow first:
→ cicd.md: GIT_TOKEN setup → git init/remote → initial push → Actions setup → verify
→ everything configured in one workflow
→ subsequent: commit, push, auto-deploy

Path B — user starts deploy first:
→ deploy-push-git: GIT_TOKEN → commit → push with remoteUrl
→ code on GitHub, no CI/CD yet
→ then: zerops_workflow action=start workflow=cicd → adds CI/CD
→ same end state, two steps
```

**Verdict:** ✓ Both paths work. Path A is faster for CI/CD-first users.

---

## Scenario 10: Migration — existing ci-cd user upgrades ZCP

**User has:** ServiceMeta with `deployStrategy: "ci-cd"` on disk

```
ReadServiceMeta() hits migration: "ci-cd" → "push-git"
→ meta.DeployStrategy = "push-git"
→ on next write, file updated

guidance change:
  before: deploy-ci-cd: "Commit and push, CI/CD triggers automatically. No zerops_deploy needed."
  after:  deploy-push-git: "Commit, push. If CI/CD configured → auto-trigger. If not → deploy manually."

router change:
  before: ci-cd → cicd(P1), deploy(P2)
  after:  push-git → deploy(P1), cicd(P2), export(P3)
```

**Behavioral difference:** Priority flip — deploy is now P1, cicd is P2. For existing CI/CD users, this means the router's first suggestion changes. But since their CI/CD is already configured, they don't need the cicd workflow again. The deploy workflow with push-git guidance is more useful for day-to-day work.

**Verdict:** ✓ Functionally correct. Slight priority shift is actually better for existing users.

---

## Scenario 11: Multiple services, mixed strategies

**User has:** appdev=push-git, apidev=push-dev

```
strategyOfferings(): counts push-git=1, push-dev=1 → tie
→ dominant depends on Go map iteration order (undefined)
→ router may offer deploy-only (push-dev dominant) or deploy+cicd+export (push-git dominant)

BuildDeployTargets(): strategy = first non-empty from iteration
→ may be push-git or push-dev depending on order
```

**This is a pre-existing issue.** The rename doesn't change it. Mixed strategies have always been fragile because the system picks a "dominant" strategy.

**Verdict:** Pre-existing limitation. Not introduced by our changes. Not blocking.

---

## Scenario 12: push-git, dev+stage mode — who deploys to stage?

**User:** "I pushed to GitHub. How does code get to stage?"

```
appdev: push-git, nodejsdev + nodejsstage

deploy-push-git guidance:
  "3. If CI/CD is configured: build triggers automatically → zerops_events"
  "5. If no CI/CD: Deploy to stage manually:
      zerops_deploy sourceService=nodejsdev targetService=nodejsstage"
```

**Path A — with CI/CD:** Push triggers Actions → Actions calls `zcli push --service-id {stageId}` → stage rebuilt from GitHub code. Dev is untouched.

**Path B — without CI/CD:** User pushes to GitHub (code preserved), then separately does `zerops_deploy sourceService=dev targetService=stage` (zcli push cross-deploy). Stage gets code from dev container, not from GitHub.

**Wait — is Path B correct?** The user pushed to GitHub, but the cross-deploy takes code from the dev container (not GitHub). This is consistent: push-git puts code on GitHub, but deploying to Zerops stage still uses the container's code. The GitHub repo is for version control / CI/CD, not for direct Zerops deployment.

**This is actually the right behavior.** Without CI/CD:
- push-git keeps code versioned on GitHub
- zerops_deploy cross-deploy puts code on stage (from dev container)
- These are independent operations

**Verdict:** ✓ Correct. The two operations (push to git, deploy to stage) are independent.

---

## Fixes Needed

### Fix 1: deploy-push-git section needs first-time setup guidance

The current §12 lists "GIT_TOKEN set as project env var" as a prerequisite but doesn't include the setup flow. For a user who just selected push-git and has never pushed to git, the guidance should include:

```markdown
**First time setup:**
1. "I need a GitHub/GitLab token to push code."
   → Get token from user
   → `zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]`

2. Commit: `ssh {devHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"`

3. Push (with remote URL):
   `zerops_deploy targetService="{devHostname}" strategy="git-push" remoteUrl="{url}"`

**Subsequent deploys:**
1. Commit: `ssh {devHostname} "cd /var/www && git add -A && git commit -m '{message}'"`
2. Push: `zerops_deploy targetService="{devHostname}" strategy="git-push"`
```

**Impact:** Update spec §12 to include first-time flow.

### Fix 2: export.md close section should suggest push-git strategy

After export completes (service now has git remote), the close section should mention:

```markdown
**Set deploy strategy** (if not already set):
`zerops_workflow action="strategy" strategies={"{hostname}":"push-git"}`
```

**Impact:** Minor — add one line to export.md close section (Phase 3 task 3.2).

---

## Summary

| Scenario | Result | Notes |
|----------|--------|-------|
| 1. Fresh bootstrap → push-git only | ✓ | Fix 1: add first-time setup to guidance |
| 2. Adoption → push-git | ✓ | Clean |
| 3. push-git → add CI/CD | ✓ | CI/CD is additive, no strategy change |
| 4. push-git → export (same repo) | ✓ | S2 path: just generates import.yaml |
| 5. push-git → export (different repo) | ✓ | Edge case: user edits import.yaml URL |
| 6. Switch push-dev → push-git | ✓ | Preserves history, smooth |
| 7. Switch push-git → push-dev | ✓ | Old remote harmless |
| 8. No strategy → export | ✓ | Fix 2: suggest push-git after export |
| 9. Fresh → CI/CD from start | ✓ | Two paths both work |
| 10. Migration ci-cd → push-git | ✓ | Priority flip is actually better |
| 11. Mixed strategies | Pre-existing | Not our problem |
| 12. push-git stage deploy | ✓ | Git push and Zerops deploy are independent |

**Overall: The redesigned system covers all scenarios correctly.** Two minor guidance improvements needed (Fix 1, Fix 2), both are content updates in workflow .md files, not code changes.
