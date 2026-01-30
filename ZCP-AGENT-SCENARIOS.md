# ZCP Workflow Agent Scenarios

**Purpose:** 50 real-world scenarios for agent exploration and testing
**Scope:** Post-bootstrap workflow (DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE)
**Version:** 1.0.0

---

## How to Use This Document

Each scenario includes:
- **Setup:** Initial state before the scenario
- **Trigger:** What causes the situation
- **Challenge:** What the agent must handle
- **Recovery:** Expected agent behavior

Scenarios are grouped by category and increase in complexity.

---

## CATEGORY A: Clean Entry Points (1-8)

### Scenario 01: Fresh Start After Bootstrap
```
Setup:    Bootstrap just completed, services running
State:    Phase=DEVELOP, Mode=full, discovery.json exists
Evidence: None (fresh session)
Trigger:  User says "help me deploy my app"

Challenge: Agent must recognize it's in DEVELOP phase and guide through
           build → verify → deploy flow without re-bootstrapping
Recovery:  .zcp/workflow.sh show → follow DEVELOP guidance
```

### Scenario 02: Entry at DISCOVER Phase
```
Setup:    Services exist but no discovery.json
State:    Phase=DISCOVER, Mode=full
Evidence: recipe_review.json exists
Trigger:  User says "continue from where we left off"

Challenge: Agent must create discovery before proceeding
Recovery:  zcli service list → create_discovery → transition_to DEVELOP
```

### Scenario 03: Entry at DEPLOY Phase
```
Setup:    Code works on dev, verified
State:    Phase=DEPLOY, Mode=full
Evidence: dev_verify.json valid, no deploy_evidence.json
Trigger:  User says "deploy to production"

Challenge: Agent must execute deploy from dev container
Recovery:  ssh {dev} "zcli push {stage_id}" → status.sh --wait
```

### Scenario 04: Entry at VERIFY Phase
```
Setup:    Deploy completed, stage running
State:    Phase=VERIFY, Mode=full
Evidence: deploy_evidence.json valid, no stage_verify.json
Trigger:  User says "check if it's working"

Challenge: Agent must verify stage endpoints
Recovery:  .zcp/verify.sh {stage} 8080 / /health → transition_to DONE
```

### Scenario 05: Entry at DONE Phase
```
Setup:    Full workflow completed successfully
State:    Phase=DONE, Mode=full
Evidence: All evidence files valid
Trigger:  User says "I need to make a small change"

Challenge: Agent must start new iteration, not new session
Recovery:  .zcp/workflow.sh iterate "fix: description" → DEVELOP
```

### Scenario 06: Entry with Quick Mode Active
```
Setup:    Previous session was quick exploration
State:    Phase=QUICK, Mode=quick
Evidence: None
Trigger:  User says "now I want to deploy this"

Challenge: Agent must recognize need to switch from quick to full mode
Recovery:  .zcp/workflow.sh init → proper workflow with gates
```

### Scenario 07: Entry with Dev-Only Mode
```
Setup:    Previous session was dev-only
State:    Phase=DONE, Mode=dev-only
Evidence: dev_verify.json only
Trigger:  User says "actually, deploy this to stage"

Challenge: Agent must upgrade mode to full
Recovery:  .zcp/workflow.sh upgrade-to-full → DEPLOY phase
```

### Scenario 08: Entry with Hotfix Mode
```
Setup:    Emergency fix in progress
State:    Phase=DEPLOY, Mode=hotfix
Evidence: No dev_verify.json (skipped)
Trigger:  User says "push the fix now"

Challenge: Agent must proceed without dev verification
Recovery:  Execute deploy → verify stage → complete
```

---

## CATEGORY B: Context Compaction Scenarios (9-18)

### Scenario 09: CC Reset During DEVELOP - No Memory of Code
```
Setup:    Agent was writing Go code, 500 lines in
State:    Phase=DEVELOP, discovery.json exists
Evidence: None
Trigger:  Context compacted, agent has no memory of code written

Challenge: Agent sees placeholders like {build_cmd} but doesn't know runtime
Recovery:  .zcp/workflow.sh context → reads runtime from discovery →
           shows "go build -o app" instead of placeholder
```

### Scenario 10: CC Reset During DEVELOP - Partial Verification
```
Setup:    2-service project, 1 service verified before CC
State:    Phase=DEVELOP, service_count=2
Evidence: apidev_verify.json exists, webdev_verify.json missing
Trigger:  Agent tries transition_to DEPLOY

Challenge: Gate blocks with "1/2 dev services verified"
Recovery:  Agent verifies second service → gate passes
```

### Scenario 11: CC Reset After Successful Dev Verify
```
Setup:    Dev verified, agent was about to deploy
State:    Phase=DEVELOP (should be DEPLOY)
Evidence: dev_verify.json valid
Trigger:  Agent lost memory of verification success

Challenge: Agent doesn't know it already verified
Recovery:  .zcp/workflow.sh show → "Dev verified" → transition_to DEPLOY
```

### Scenario 12: CC Reset During Deploy Push
```
Setup:    Agent started zcli push, CC happened mid-push
State:    Phase=DEPLOY
Evidence: No deploy_evidence.json
Trigger:  Push may or may not have completed

Challenge: Agent doesn't know if deploy succeeded
Recovery:  .zcp/status.sh --wait {stage} → creates evidence if running
```

### Scenario 13: CC Reset with Stale Evidence
```
Setup:    Evidence from previous session exists
State:    Phase=DEVELOP, new session_id
Evidence: dev_verify.json has old session_id
Trigger:  Agent tries to use old evidence

Challenge: Gate rejects "session_id mismatch"
Recovery:  Re-run verification with current session
```

### Scenario 14: CC Reset - Lost Service Names
```
Setup:    Agent was working with "myappdev" and "myappstage"
State:    Phase=DEVELOP
Evidence: discovery.json exists
Trigger:  Agent doesn't remember service names

Challenge: Agent needs to recover service context
Recovery:  .zcp/workflow.sh context → exports DEV_NAME, STAGE_NAME
```

### Scenario 15: CC Reset - Multi-Service Confusion
```
Setup:    3-service project: api, web, worker
State:    Phase=DEVELOP, service_count=3
Evidence: Partial verification
Trigger:  Agent doesn't remember which services were verified

Challenge: Agent needs to determine verification status per service
Recovery:  .zcp/workflow.sh show → displays all 3 services with status
```

### Scenario 16: CC Reset - Lost Build Command Knowledge
```
Setup:    Agent had figured out complex build: "CGO_ENABLED=0 go build -ldflags..."
State:    Phase=DEVELOP, runtime=go
Evidence: None
Trigger:  Agent only knows it's a Go project

Challenge: Agent needs build command but doesn't have custom one
Recovery:  get_build_from_zerops_yml() extracts from zerops.yml if present
```

### Scenario 17: CC Reset - Intent Lost
```
Setup:    User said "add JWT authentication", agent was implementing
State:    Phase=DEVELOP
Evidence: None
Trigger:  Agent doesn't remember what feature to build

Challenge: No technical context about the task
Recovery:  Agent asks user, or checks .zcp/workflow.sh intent
```

### Scenario 18: CC Reset - Notes Available
```
Setup:    Previous agent left notes about tricky bugs
State:    Phase=DEVELOP
Evidence: context.json has notes array
Trigger:  New agent context

Challenge: Agent should check for breadcrumbs
Recovery:  .zcp/workflow.sh show --full → displays recent notes
```

---

## CATEGORY C: Container Restart Scenarios (19-28)

### Scenario 19: Container Restart - /tmp Wiped
```
Setup:    All evidence was in /tmp, container restarted
State:    Phase=NONE (no phase file)
Evidence: All /tmp files gone
Trigger:  Agent runs .zcp/workflow.sh show

Challenge: No state, no evidence, no session
Recovery:  State restored from .zcp/state/ persistent storage
```

### Scenario 20: Container Restart - Partial Restore
```
Setup:    Persistent storage had session/phase but not evidence
State:    Phase=DEPLOY restored
Evidence: No evidence files restored
Trigger:  Agent tries transition_to VERIFY

Challenge: Gate fails - no deploy evidence
Recovery:  Re-run deploy or manually record: .zcp/workflow.sh record_deployment
```

### Scenario 21: Container Restart - SSHFS Remount Needed
```
Setup:    Dev filesystem was mounted at /var/www/appdev
State:    Phase=DEVELOP
Evidence: discovery.json exists
Trigger:  Mount point empty after restart

Challenge: Agent can't access code
Recovery:  .zcp/mount.sh appdev → remounts SSHFS
```

### Scenario 22: Container Restart - Process Not Running
```
Setup:    Agent was testing, server was running
State:    Phase=DEVELOP
Evidence: None
Trigger:  verify.sh fails with "preflight_failed"

Challenge: Server process died with container
Recovery:  Start server again → re-verify
```

### Scenario 23: Container Restart - zcli Auth Lost
```
Setup:    zcli was authenticated
State:    Phase=DEPLOY
Evidence: dev_verify.json valid
Trigger:  zcli push fails with auth error

Challenge: Token expired or cleared
Recovery:  zcli login with $ZEROPS_ZCP_API_KEY → retry push
```

### Scenario 24: Container Restart - Environment Variables Changed
```
Setup:    $db_connectionString was working
State:    Phase=DEVELOP
Evidence: None
Trigger:  Database connection fails

Challenge: Env vars may have changed (new credentials)
Recovery:  Check current env vars, update code if needed
```

### Scenario 25: Container Restart - Mid-Bootstrap
```
Setup:    Bootstrap was running spawn-subagents step
State:    Mode=bootstrap, checkpoint=spawn-subagents
Evidence: bootstrap_handoff.json exists
Trigger:  Container restarted

Challenge: Don't know if subagents completed
Recovery:  .zcp/bootstrap.sh step aggregate-results → checks completion
```

### Scenario 26: Container Restart - Different Container
```
Setup:    Work was on container A, now on container B
State:    Phase=DEVELOP
Evidence: discovery.json points to different service IDs
Trigger:  Service IDs don't match current project

Challenge: Wrong project context
Recovery:  .zcp/workflow.sh reset → init → rediscover services
```

### Scenario 27: Container Restart - Services Recreated
```
Setup:    Services were deleted and recreated with new IDs
State:    Phase=DEVELOP
Evidence: discovery.json has old IDs
Trigger:  SSH fails, push fails - IDs don't exist

Challenge: Discovery is stale
Recovery:  .zcp/workflow.sh refresh_discovery or create_discovery with new IDs
```

### Scenario 28: Container Restart - Multiple Restarts
```
Setup:    3rd restart in an hour
State:    Phase=DEVELOP
Evidence: Fragmented across restarts
Trigger:  State is inconsistent

Challenge: Evidence from different sessions mixed
Recovery:  .zcp/workflow.sh reset --keep-discovery → clean slate
```

---

## CATEGORY D: Verification Failures (29-38)

### Scenario 29: Pre-flight Failure - No Server
```
Setup:    Code exists but server not started
State:    Phase=DEVELOP
Evidence: None
Trigger:  verify.sh creates preflight_failed evidence

Challenge: Gate blocks even though failed=0
Recovery:  Start server → re-verify → preflight passes
```

### Scenario 30: Endpoint Returns 500
```
Setup:    Server running but buggy
State:    Phase=DEVELOP
Evidence: dev_verify.json shows failed=2
Trigger:  transition_to DEPLOY blocked

Challenge: Must fix bugs before proceeding
Recovery:  Debug → fix → rebuild → re-verify
```

### Scenario 31: Partial Endpoint Success
```
Setup:    / returns 200, /api/users returns 404
State:    Phase=DEVELOP
Evidence: passed=1, failed=1
Trigger:  Gate blocks

Challenge: Need to identify which endpoint failed
Recovery:  Read evidence file → fix missing endpoint → re-verify
```

### Scenario 32: Wrong Port Verified
```
Setup:    App runs on 3000, verified on 8080
State:    Phase=DEVELOP
Evidence: preflight_failed (nothing on 8080)
Trigger:  Agent used default port

Challenge: Port mismatch
Recovery:  Check zerops.yml for correct port → verify correct port
```

### Scenario 33: Multi-Service Partial Failure
```
Setup:    apidev passes, webdev fails
State:    Phase=DEVELOP, service_count=2
Evidence: apidev_verify.json OK, webdev_verify.json failed=1
Trigger:  Gate shows "1/2 services verified"

Challenge: Must fix specific failing service
Recovery:  Focus on webdev → fix → verify → gate passes
```

### Scenario 34: Stage Verification Fails After Deploy
```
Setup:    Dev worked, deployed, stage broken
State:    Phase=VERIFY
Evidence: deploy_evidence.json OK, stage_verify.json failed
Trigger:  Can't transition to DONE

Challenge: Something broke in deploy
Recovery:  Check deployFiles, check stage logs, fix → redeploy → verify
```

### Scenario 35: HTTP 200 but Wrong Content
```
Setup:    Endpoint returns 200 with error JSON
State:    Phase=DEVELOP
Evidence: passed (verify only checks status code)
Trigger:  User reports feature doesn't work

Challenge: verify.sh passed but feature broken
Recovery:  Agent must do deeper testing: curl response body, check logs
```

### Scenario 36: Frontend JS Errors
```
Setup:    Backend OK, frontend has console errors
State:    Phase=VERIFY
Evidence: stage_verify.json passed
Trigger:  User sees broken UI

Challenge: HTTP checks pass but app broken
Recovery:  agent-browser errors → find JS errors → fix → redeploy
```

### Scenario 37: Database Not Seeded
```
Setup:    App runs, DB empty
State:    Phase=DEVELOP
Evidence: verify passed (/ returns 200)
Trigger:  /api/users returns empty array

Challenge: App "works" but useless without data
Recovery:  Run migrations/seeds → verify again with data checks
```

### Scenario 38: Environment Variable Missing
```
Setup:    Works on dev (has vars), fails on stage (missing vars)
State:    Phase=VERIFY
Evidence: stage_verify.json failed
Trigger:  Stage missing $API_KEY

Challenge: Different env between dev and stage
Recovery:  Check zerops.yml envSecrets → add missing vars → redeploy
```

---

## CATEGORY E: Deploy Failures (39-44)

### Scenario 39: Deploy Timeout
```
Setup:    zcli push started, never completed
State:    Phase=DEPLOY
Evidence: No deploy_evidence.json
Trigger:  status.sh --wait times out

Challenge: Build might be stuck
Recovery:  Check build logs in Zerops GUI → fix build → retry
```

### Scenario 40: deployFiles Missing Artifact
```
Setup:    Build succeeds, deploy fails
State:    Phase=DEPLOY
Evidence: None
Trigger:  Stage has missing files

Challenge: zerops.yml deployFiles incomplete
Recovery:  Add missing files to deployFiles → rebuild → redeploy
```

### Scenario 41: Wrong --setup Flag
```
Setup:    Push with --setup=dev to stage
State:    Phase=DEPLOY
Evidence: None
Trigger:  Stage uses dev config

Challenge: Deployed wrong configuration
Recovery:  Re-push with --setup=prod
```

### Scenario 42: Build Fails on Stage
```
Setup:    Dev build works, stage build fails
State:    Phase=DEPLOY
Evidence: None
Trigger:  Stage uses buildFromGit with errors

Challenge: Git repo has different state than local
Recovery:  Commit and push changes → trigger rebuild
```

### Scenario 43: Push from Wrong Location
```
Setup:    Agent runs zcli push from ZCP instead of dev container
State:    Phase=DEPLOY
Evidence: None
Trigger:  Push fails or pushes wrong code

Challenge: Must push from inside dev container
Recovery:  ssh {dev} "zcli push {stage_id} --setup=prod"
```

### Scenario 44: Concurrent Deploy Conflict
```
Setup:    Two agents both trying to deploy
State:    Phase=DEPLOY
Evidence: Conflicting
Trigger:  One deploy overwrites another

Challenge: Race condition
Recovery:  Check current deployment → coordinate → single deploy
```

---

## CATEGORY F: Multi-Service Complexity (45-50)

### Scenario 45: Services Must Deploy in Order
```
Setup:    API depends on Worker being deployed first
State:    Phase=DEPLOY, service_count=2
Evidence: None
Trigger:  API fails because Worker not ready

Challenge: Deployment order matters
Recovery:  Deploy worker first → wait → deploy API → verify both
```

### Scenario 46: Shared Database Schema
```
Setup:    Both services use same DB, one ran migrations
State:    Phase=DEVELOP
Evidence: apidev verified
Trigger:  webdev fails - schema mismatch

Challenge: Migrations ran on one service only
Recovery:  Ensure migrations are idempotent or run from single source
```

### Scenario 47: Different Runtimes, Different Commands
```
Setup:    apidev is Go, webdev is Bun
State:    Phase=DEVELOP
Evidence: None
Trigger:  Agent uses wrong build command for service

Challenge: Must track runtime per service
Recovery:  discovery.json has runtime per service → use correct commands
```

### Scenario 48: Service Communication Failure
```
Setup:    API calls Worker internally
State:    Phase=VERIFY
Evidence: API stage_verify passed, but feature broken
Trigger:  API can't reach Worker

Challenge: Internal networking issue
Recovery:  Check service discovery → ensure both on same network
```

### Scenario 49: Partial Multi-Service Deploy
```
Setup:    API deployed, Worker deploy failed
State:    Phase=DEPLOY
Evidence: Partial deploy_evidence
Trigger:  Inconsistent state

Challenge: Half-deployed system
Recovery:  Complete Worker deploy → verify both → proceed
```

### Scenario 50: Multi-Service Iteration
```
Setup:    Full 3-service system deployed, user wants to change API only
State:    Phase=DONE
Evidence: All verified
Trigger:  User says "update the API authentication"

Challenge: Must iterate single service without breaking others
Recovery:  iterate → develop API only → verify API → deploy API →
           verify full system → done
```

---

## Recovery Command Quick Reference

| Situation | Command |
|-----------|---------|
| Lost context | `.zcp/workflow.sh context` |
| Check status | `.zcp/workflow.sh show` |
| Full recovery | `.zcp/workflow.sh recover` |
| Start fresh | `.zcp/workflow.sh reset` |
| Keep discovery | `.zcp/workflow.sh reset --keep-discovery` |
| New iteration | `.zcp/workflow.sh iterate "description"` |
| Skip to phase | `.zcp/workflow.sh iterate --to VERIFY` |
| Manual deploy evidence | `.zcp/workflow.sh record_deployment {svc}` |
| Upgrade mode | `.zcp/workflow.sh upgrade-to-full` |
| Check single service | `.zcp/verify.sh {hostname} {port} /` |

---

## Evidence File Quick Reference

| File | Created By | Contains |
|------|------------|----------|
| `/tmp/discovery.json` | `create_discovery` or bootstrap | Service IDs, names, URLs, runtimes |
| `/tmp/dev_verify.json` | `verify.sh` (symlink) | Primary dev verification |
| `/tmp/{hostname}_verify.json` | `verify.sh` | Per-service verification |
| `/tmp/stage_verify.json` | `verify.sh` (symlink) | Primary stage verification |
| `/tmp/deploy_evidence.json` | `status.sh --wait` | Deploy completion proof |
| `/tmp/claude_session` | `init` | Current session ID |
| `/tmp/claude_phase` | `transition_to` | Current phase |
| `/tmp/claude_mode` | `init` | Workflow mode |

---

## Gate Requirements Summary

| Transition | Gate Checks |
|------------|-------------|
| DISCOVER → DEVELOP | discovery.json exists, session matches, dev ≠ stage |
| DEVELOP → DEPLOY | dev_verify.json exists, session matches, 0 failures, no preflight_failed, ALL services verified |
| DEPLOY → VERIFY | deploy_evidence.json exists, session matches |
| VERIFY → DONE | stage_verify.json exists, session matches, 0 failures, ALL services verified |

---

**Document Version:** 1.0.0
**Last Updated:** 2026-01-30
