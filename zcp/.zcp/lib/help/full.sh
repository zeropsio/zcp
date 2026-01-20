#!/bin/bash
# Full help reference for Zerops Workflow

show_full_help() {
    cat <<'EOF'
╔══════════════════════════════════════════════════════════════════╗
║  ZEROPS PLATFORM REFERENCE                                       ║
╚══════════════════════════════════════════════════════════════════╝

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 ORIENTATION
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

You are on ZCP (Zerops Control Plane), NOT inside containers.

┌─ File Operations (SSHFS) ──────────────────────────────────────┐
│ Path: /var/www/{service}/                                       │
│ Edit files directly, changes appear in container                │
│ Example: vim /var/www/appdev/main.go                            │
└──────────────────────────────────────────────────────────────────┘

┌─ Command Execution (SSH) ──────────────────────────────────────┐
│ Pattern: ssh {service} "command"                                │
│ Example: ssh appdev "go build -o app main.go"                   │
│ ⚠️  Use run_in_background=true in Bash tool for long processes │
└──────────────────────────────────────────────────────────────────┘

Service names vary by project:
  • appdev / appstage
  • apidev / apistage
  • webdev / webstage
  • db (database service)

Network: Services connect via hostname = service name
  Example: http://appdev:8080, http://db:5432

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔐 VARIABLES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

┌─ Variable Patterns ────────────────────────────────────────────┐
│ Context        │ Pattern      │ Example                        │
├────────────────┼──────────────┼────────────────────────────────┤
│ ZCP → service  │ ${svc}_VAR   │ ${appdev_PORT}                 │
│ ZCP → database │ $db_*        │ $db_hostname, $db_password     │
│ Inside service │ $VAR         │ ssh appdev "echo \$PORT"       │
└────────────────┴──────────────┴────────────────────────────────┘

⚠️  CRITICAL WARNINGS:
  • zeropsSubdomain is FULL URL - don't prepend https://
    ✓ Correct: curl "$zeropsSubdomain/api"
    ✗ Wrong:   curl "https://$zeropsSubdomain/api"

  • Variable timing: ZCP only has vars for pre-existing services
    For new deployments: ssh {service} "echo \$VAR"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 WORKFLOW COMMANDS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

.zcp/workflow.sh init                         # Start enforced workflow
.zcp/workflow.sh init --dev-only              # Dev mode (no deployment)
.zcp/workflow.sh init --hotfix                # Hotfix mode (skip dev verification)
.zcp/workflow.sh --quick                      # Quick mode (no gates)
.zcp/workflow.sh --help                       # This reference
.zcp/workflow.sh --help {topic}               # Topic-specific help
.zcp/workflow.sh transition_to {phase}        # Advance phase
.zcp/workflow.sh transition_to --back {phase} # Go backward (invalidates evidence)
.zcp/workflow.sh create_discovery ...         # Record service discovery
.zcp/workflow.sh create_discovery --single {id} {name}  # Single-service mode
.zcp/workflow.sh show                         # Current status
.zcp/workflow.sh complete                     # Verify evidence
.zcp/workflow.sh reset                        # Clear all state
.zcp/workflow.sh reset --keep-discovery       # Clear state but preserve discovery
.zcp/workflow.sh extend {import.yml}          # Add services to project
.zcp/workflow.sh refresh_discovery            # Validate current discovery
.zcp/workflow.sh upgrade-to-full              # Upgrade dev-only to full deployment

Topics: discover, develop, deploy, verify, done, vars, services,
        trouble, example, gates, extend, bootstrap

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 PHASE: DISCOVER
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Authenticate and discover services:

  zcli login --region=gomibako \
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
      "$ZEROPS_ZCP_API_KEY"

  zcli service list -P $projectId

Record discovery:
  .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

⚠️  Never use 'zcli scope' - it's buggy
⚠️  Use service IDs (from list), not hostnames

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💻 PHASE: DEVELOP
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Kill existing processes:
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; \
             fuser -k {port}/tcp 2>/dev/null; true'

Build & run:
  ssh {dev} "{build_command}"
  ssh {dev} './{binary} >> /tmp/app.log 2>&1'
  ↑ Set run_in_background=true in Bash tool parameters

⚠️  CRITICAL: Container already has all env vars (db_hostname, etc.)
   Just run the app - don't pass DB_HOST, DB_PASS manually!
   ✅ ssh appdev './app'
   ❌ ssh appdev "DB_HOST=... DB_PASS=... ./app"

Test endpoints:
  .zcp/verify.sh {dev} {port} / /status /api/...

Check logs:
  ssh {dev} "tail -f /tmp/app.log"

Internal connectivity test:
  ssh {dev} "curl -sf http://localhost:{port}/"
  timeout 5 bash -c "</dev/tcp/{service}/{port}" && echo OK

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🚀 PHASE: DEPLOY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  PRE-DEPLOYMENT CHECKLIST (CRITICAL):

  1. Verify deployFiles configuration:
     cat /var/www/{dev}/zerops.yaml | grep -A10 deployFiles

  2. Verify ALL artifacts exist:
     ls -la /var/www/{dev}/app
     ls -la /var/www/{dev}/templates/
     ls -la /var/www/{dev}/static/

  3. If you created new directories, ADD them to deployFiles!

⚠️  Common failure: Files built but not in deployFiles = missing on stage

zerops.yaml structure:
  zerops:
    - setup: api              # ← --setup value
      build:
        base: go@1.22
        buildCommands:
          - go build -o app main.go
        deployFiles:
          - ./app
          - ./templates       # Don't forget!
          - ./static
      run:
        base: go@1.22
        ports:
          - port: 8080
        start: ./app

Stop dev process (triple-kill pattern):
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; \
             fuser -k {port}/tcp 2>/dev/null; true'

Authenticate from dev container:
  ssh {dev} "zcli login --region=gomibako \
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
      \"\$ZEROPS_ZCP_API_KEY\""

Deploy to stage:
  ssh {dev} "zcli push {stage_service_id} --setup={setup} --versionName=v1.0.0"

Wait for completion:
  .zcp/status.sh --wait {stage}

Redeploy/Retry (if needed):
  1. Check: zcli project notifications -P $projectId
  2. Fix the issue
  3. Re-run: ssh {dev} "zcli push {stage_id} --setup={setup}"
  4. Wait: .zcp/status.sh --wait {stage}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ PHASE: VERIFY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Check deployed artifacts:
  ssh {stage} "ls -la /var/www/"

Verify endpoints:
  .zcp/verify.sh {stage} {port} / /status /api/...

Service logs:
  zcli service log -S {stage_service_id} -P $projectId --follow

⚠️  BROWSER TESTING (required for frontends):

   If app has HTML/CSS/JS/templates:

   URL=$(ssh {stage} "echo \$zeropsSubdomain")
   agent-browser open "$URL"          # Don't prepend https://!
   agent-browser errors               # Must show no errors
   agent-browser console              # Check runtime errors
   agent-browser network requests     # Verify assets load
   agent-browser screenshot           # Visual evidence

⚠️  HTTP 200 ≠ working UI
   CSS/JS errors return 200 but break the app.

💡 Tool awareness:
   • You CAN see screenshots and reason about them
   • You CAN test functionality, not just status codes
   • You CAN query database to verify persistence

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🗄️  DATABASE OPERATIONS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Run from ZCP (not container). Use connection strings for security:

PostgreSQL:
  psql "$db_connectionString"

Redis/Valkey:
  redis-cli -u "$cache_connectionString"

MySQL/MariaDB:
  mysql "$mysql_connectionString"

⚠️  Prefer connection strings over individual vars - avoids password exposure

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔧 TROUBLESHOOTING
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Problem                      │ Cause              │ Fix
─────────────────────────────┼────────────────────┼─────────────────
unbound variable             │ Wrong prefix       │ ZCP: ${svc}_VAR
                             │                    │ Service: $VAR
─────────────────────────────┼────────────────────┼─────────────────
No such file (ZCP)           │ Missing service    │ /var/www/{service}/
                             │ in path            │
─────────────────────────────┼────────────────────┼─────────────────
No such file (SSH)           │ Service in path    │ /var/www/path
                             │                    │ (no service)
─────────────────────────────┼────────────────────┼─────────────────
Connection refused           │ Not running        │ Start process,
                             │                    │ verify port
─────────────────────────────┼────────────────────┼─────────────────
Address in use               │ Orphan process     │ Triple-kill:
                             │                    │ pkill; killall;
                             │                    │ fuser -k; true
─────────────────────────────┼────────────────────┼─────────────────
SSH hangs                    │ Foreground proc    │ run_in_background
                             │                    │ =true
─────────────────────────────┼────────────────────┼─────────────────
Requires DISCOVER            │ Skipped phase      │ Run phases in
                             │                    │ order
─────────────────────────────┼────────────────────┼─────────────────
Session mismatch             │ Stale evidence     │ .zcp/workflow.sh reset
                             │                    │ && init
─────────────────────────────┼────────────────────┼─────────────────
.zcp/verify.sh silent        │ Script error       │ Use --debug flag
─────────────────────────────┼────────────────────┼─────────────────
Files missing post-deploy    │ Checked too early  │ .zcp/status.sh --wait
─────────────────────────────┼────────────────────┼─────────────────
Files missing post-deploy    │ Not in deployFiles │ Update zerops.yaml
                             │                    │ redeploy
─────────────────────────────┼────────────────────┼─────────────────
unexpected EOF               │ Network issue      │ Check zcli project
                             │                    │ notifications
─────────────────────────────┼────────────────────┼─────────────────
zcli scope errors            │ Buggy command      │ Never use it
─────────────────────────────┼────────────────────┼─────────────────
psql: not found              │ Wrong context      │ Run DB from ZCP
─────────────────────────────┼────────────────────┼─────────────────
Double https:// in URL       │ zeropsSubdomain    │ Don't prepend
                             │ is full URL        │ protocol
─────────────────────────────┼────────────────────┼─────────────────
Deploy missing templates     │ Not in deployFiles │ Add before deploy
─────────────────────────────┼────────────────────┼─────────────────
zcli permission error        │ Mixed ID/hostname  │ Use service ID
                             │                    │ for -S flag
─────────────────────────────┼────────────────────┼─────────────────
Build fails                  │ --setup mismatch   │ Match zerops.yaml
─────────────────────────────┼────────────────────┼─────────────────
Deploy to wrong target       │ Using dev as       │ Always deploy to
                             │ target             │ stage

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📖 COMPLETE EXAMPLE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 1. INIT
.zcp/workflow.sh init

# 2. DISCOVER
zcli login --region=gomibako \
    --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
    "$ZEROPS_ZCP_API_KEY"
zcli service list -P $projectId
.zcp/workflow.sh create_discovery "svc123" "appdev" "svc456" "appstage"
.zcp/workflow.sh transition_to DISCOVER

# 3. DEVELOP
.zcp/workflow.sh transition_to DEVELOP
ssh appdev "go build -o app main.go"
ssh appdev './app >> /tmp/app.log 2>&1'  # run_in_background=true
.zcp/verify.sh appdev 8080 / /status /api/items

# 4. DEPLOY
.zcp/workflow.sh transition_to DEPLOY
cat /var/www/appdev/zerops.yaml | grep -A10 deployFiles
ls -la /var/www/appdev/app /var/www/appdev/templates/
ssh appdev 'pkill -9 app; killall -9 app 2>/dev/null; fuser -k 8080/tcp 2>/dev/null; true'
ssh appdev "zcli login --region=gomibako \
    --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
    \"\$ZEROPS_ZCP_API_KEY\""
ssh appdev "zcli push svc456 --setup=api --versionName=v1.0.0"
.zcp/status.sh --wait appstage

# 5. VERIFY
.zcp/workflow.sh transition_to VERIFY
ssh appstage "ls -la /var/www/"
.zcp/verify.sh appstage 8080 / /status /api/items
# If frontend:
URL=$(ssh appstage "echo \$zeropsSubdomain")
agent-browser open "$URL"
agent-browser errors
agent-browser screenshot

# 6. DONE
.zcp/workflow.sh transition_to DONE
.zcp/workflow.sh complete

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🚪 GATES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

DISCOVER → DEVELOP:
  • /tmp/discovery.json exists with current session_id
  • deploy_target != dev service name

DEVELOP → DEPLOY:
  • /tmp/dev_verify.json exists with current session_id
  • failures == 0

DEPLOY → VERIFY:
  • Manual check via status.sh or zcli

VERIFY → DONE:
  • /tmp/stage_verify.json exists with current session_id
  • failures == 0

Exit (full mode only):
  • phase == DONE
  • All evidence files exist
  • All evidence has matching session_id
  • All verify files have failures == 0

EOF
}
