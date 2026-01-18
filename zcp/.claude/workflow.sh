#!/bin/bash

# Zerops Workflow Management System
# Self-documenting phase orchestration with enforcement gates

set -o pipefail

# State files
SESSION_FILE="/tmp/claude_session"
MODE_FILE="/tmp/claude_mode"
PHASE_FILE="/tmp/claude_phase"
DISCOVERY_FILE="/tmp/discovery.json"
DEV_VERIFY_FILE="/tmp/dev_verify.json"
STAGE_VERIFY_FILE="/tmp/stage_verify.json"

# Valid phases
PHASES=("INIT" "DISCOVER" "DEVELOP" "DEPLOY" "VERIFY" "DONE")

# ============================================================================
# UTILITY FUNCTIONS
# ============================================================================

get_session() {
    if [ -f "$SESSION_FILE" ]; then
        cat "$SESSION_FILE"
    fi
}

get_mode() {
    if [ -f "$MODE_FILE" ]; then
        cat "$MODE_FILE"
    fi
}

get_phase() {
    if [ -f "$PHASE_FILE" ]; then
        cat "$PHASE_FILE"
    else
        echo "NONE"
    fi
}

set_phase() {
    echo "$1" > "$PHASE_FILE"
}

validate_phase() {
    local phase="$1"
    for p in "${PHASES[@]}"; do
        if [ "$p" = "$phase" ]; then
            return 0
        fi
    done
    return 1
}

check_evidence_session() {
    local file="$1"
    local current_session
    local evidence_session

    current_session=$(get_session)
    if [ -z "$current_session" ]; then
        return 1
    fi

    if [ ! -f "$file" ]; then
        return 1
    fi

    if ! command -v jq &>/dev/null; then
        echo "⚠️  Warning: jq not found, cannot validate evidence"
        return 0
    fi

    evidence_session=$(jq -r '.session_id // empty' "$file" 2>/dev/null)
    if [ "$evidence_session" = "$current_session" ]; then
        return 0
    fi
    return 1
}

# ============================================================================
# HELP SYSTEM
# ============================================================================

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

workflow.sh init                    # Start enforced workflow
workflow.sh --quick                 # Quick mode (no gates)
workflow.sh --help                  # This reference
workflow.sh --help {topic}          # Topic-specific help
workflow.sh transition_to {phase}   # Advance phase
workflow.sh create_discovery ...    # Record service discovery
workflow.sh show                    # Current status
workflow.sh complete                # Verify evidence
workflow.sh reset                   # Clear all state

Topics: discover, develop, deploy, verify, done, vars, trouble,
        example, gates

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 PHASE: DISCOVER
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Authenticate and discover services:

  zcli login --region=gomibako \
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
      "$ZAGENTS_API_KEY"

  zcli service list -P $projectId

Record discovery:
  workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

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

Test endpoints:
  verify.sh {dev} {port} / /status /api/...

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

Stop dev process:
  ssh {dev} 'pkill -9 {proc}; fuser -k {port}/tcp 2>/dev/null; true'

Authenticate from dev container:
  ssh {dev} "zcli login --region=gomibako \
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
      \"\$ZAGENTS_API_KEY\""

Deploy to stage:
  ssh {dev} "zcli push {stage_service_id} --setup={setup} --versionName=v1.0.0"

Wait for completion:
  status.sh --wait {stage}

Redeploy/Retry (if needed):
  1. Check: zcli project notifications -P $projectId
  2. Fix the issue
  3. Re-run: ssh {dev} "zcli push {stage_id} --setup={setup}"
  4. Wait: status.sh --wait {stage}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ PHASE: VERIFY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Check deployed artifacts:
  ssh {stage} "ls -la /var/www/"

Verify endpoints:
  verify.sh {stage} {port} / /status /api/...

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

Run from ZCP (not container):

PostgreSQL:
  PGPASSWORD=$db_password psql -h $db_hostname -U $db_user -d $db_database

Redis:
  redis-cli -h $redis_hostname -a $redis_password

MySQL/MariaDB:
  mysql -h $mysql_hostname -u $mysql_user -p$mysql_password $mysql_database

Connection strings also available:
  $db_connectionString

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
Session mismatch             │ Stale evidence     │ workflow.sh reset
                             │                    │ && init
─────────────────────────────┼────────────────────┼─────────────────
verify.sh silent             │ Script error       │ Use --debug flag
─────────────────────────────┼────────────────────┼─────────────────
Files missing post-deploy    │ Checked too early  │ status.sh --wait
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
/var/www/.claude/workflow.sh init

# 2. DISCOVER
zcli login --region=gomibako \
    --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
    "$ZAGENTS_API_KEY"
zcli service list -P $projectId
/var/www/.claude/workflow.sh create_discovery "svc123" "appdev" "svc456" "appstage"
/var/www/.claude/workflow.sh transition_to DISCOVER

# 3. DEVELOP
/var/www/.claude/workflow.sh transition_to DEVELOP
ssh appdev "go build -o app main.go"
ssh appdev './app >> /tmp/app.log 2>&1'  # run_in_background=true
/var/www/.claude/verify.sh appdev 8080 / /status /api/items

# 4. DEPLOY
/var/www/.claude/workflow.sh transition_to DEPLOY
cat /var/www/appdev/zerops.yaml | grep -A10 deployFiles
ls -la /var/www/appdev/app /var/www/appdev/templates/
ssh appdev 'pkill -9 app; fuser -k 8080/tcp 2>/dev/null; true'
ssh appdev "zcli login --region=gomibako \
    --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
    \"\$ZAGENTS_API_KEY\""
ssh appdev "zcli push svc456 --setup=api --versionName=v1.0.0"
/var/www/.claude/status.sh --wait appstage

# 5. VERIFY
/var/www/.claude/workflow.sh transition_to VERIFY
ssh appstage "ls -la /var/www/"
/var/www/.claude/verify.sh appstage 8080 / /status /api/items
# If frontend:
URL=$(ssh appstage "echo \$zeropsSubdomain")
agent-browser open "$URL"
agent-browser errors
agent-browser screenshot

# 6. DONE
/var/www/.claude/workflow.sh transition_to DONE
/var/www/.claude/workflow.sh complete

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

show_topic_help() {
    local topic="$1"

    case "$topic" in
        discover)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 DISCOVER PHASE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Purpose: Authenticate to Zerops and discover service IDs

Commands:
  zcli login --region=gomibako \
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
      "$ZAGENTS_API_KEY"

  zcli service list -P $projectId

Record discovery:
  workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

  Example:
    workflow.sh create_discovery \
        "abc123def456" "appdev" \
        "ghi789jkl012" "appstage"

Transition:
  workflow.sh transition_to DISCOVER

⚠️  Critical:
  • Never use 'zcli scope' - it's buggy
  • Use service IDs from list, not hostnames
  • Service ID ≠ hostname (ID for -S flag, hostname for ssh)

Gate requirement:
  • /tmp/discovery.json must exist
  • deploy_target must be different from dev service name
EOF
            ;;
        develop)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💻 DEVELOP PHASE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Purpose: Build and test on dev service

Context reminders:
  📁 Files: /var/www/{dev}/     (edit directly via SSHFS)
  💻 Run:   ssh {dev} "cmd"     (execute inside container)

Triple-kill pattern (clear orphan processes):
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; \
             fuser -k {port}/tcp 2>/dev/null; true'

Build & run:
  ssh {dev} "{build_command}"
  ssh {dev} './{binary} >> /tmp/app.log 2>&1'

  ⚠️  Set run_in_background=true in Bash tool parameters!

Testing:
  # Endpoint verification
  verify.sh {dev} {port} / /status /api/...

  # Internal connectivity
  ssh {dev} "curl -sf http://localhost:{port}/"

  # TCP connectivity
  timeout 5 bash -c "</dev/tcp/{service}/{port}" && echo OK

  # External (if subdomain available)
  curl -sf "${dev_zeropsSubdomain}/endpoint"

Logs:
  ssh {dev} "tail -f /tmp/app.log"
  ssh {dev} "cat /tmp/app.log"

Debugging:
  # Check process running
  ssh {dev} "pgrep -f {proc}"
  ssh {dev} "ps aux | grep {proc}"

  # Check port listening
  ssh {dev} "ss -tlnp | grep {port}"
  ssh {dev} "netstat -tlnp | grep {port}"

Gate requirement:
  • verify.sh must pass (creates /tmp/dev_verify.json)
  • failures == 0
EOF
            ;;
        deploy)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🚀 DEPLOY PHASE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  PRE-DEPLOYMENT CHECKLIST - DO THIS FIRST:

1. Verify deployFiles configuration:
   cat /var/www/{dev}/zerops.yaml | grep -A10 deployFiles

2. Verify ALL artifacts exist:
   ls -la /var/www/{dev}/app
   ls -la /var/www/{dev}/templates/
   ls -la /var/www/{dev}/static/
   ls -la /var/www/{dev}/config/

3. If you created new directories, ADD them to deployFiles!
   Edit /var/www/{dev}/zerops.yaml

⚠️  Most common failure: Agent builds files but forgets to update deployFiles

zerops.yaml structure:
  zerops:
    - setup: api              # ← This is the --setup value
      build:
        base: go@1.22
        buildCommands:
          - go build -o app main.go
        deployFiles:          # ← CRITICAL SECTION
          - ./app
          - ./templates       # Don't forget if you created these!
          - ./static
          - ./config
      run:
        base: go@1.22
        ports:
          - port: 8080
        start: ./app

Deployment steps:

1. Stop dev process:
   ssh {dev} 'pkill -9 {proc}; fuser -k {port}/tcp 2>/dev/null; true'

2. Authenticate from dev container:
   ssh {dev} "zcli login --region=gomibako \
       --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
       \"\$ZAGENTS_API_KEY\""

3. Deploy to stage:
   ssh {dev} "zcli push {stage_service_id} --setup={setup} --versionName=v1.0.0"

   • {stage_service_id} = ID from discovery (not hostname!)
   • {setup} = setup name from zerops.yaml
   • --versionName optional but recommended

4. Wait for completion:
   status.sh --wait {stage}

Redeploy/Retry procedure:
  If deployment fails or needs retry:
  1. zcli project notifications -P $projectId    # Check error
  2. Fix the issue (usually deployFiles or code)
  3. ssh {dev} "zcli push {stage_id} --setup={setup}"
  4. status.sh --wait {stage}

Gate requirement:
  • status.sh shows SUCCESS notification
  • Deployment fully complete before verification
EOF
            ;;
        verify)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ VERIFY PHASE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Purpose: Verify deployment on stage service

Basic verification:

1. Check deployed artifacts:
   ssh {stage} "ls -la /var/www/"

2. Verify endpoints:
   verify.sh {stage} {port} / /status /api/...

3. Check service logs:
   zcli service log -S {stage_service_id} -P $projectId
   zcli service log -S {stage_service_id} -P $projectId --follow

⚠️  BROWSER TESTING (MANDATORY for frontends):

If your app has HTML/CSS/JS/templates:

  URL=$(ssh {stage} "echo \$zeropsSubdomain")
  agent-browser open "$URL"          # Don't prepend https://!
  agent-browser errors               # Must show no errors
  agent-browser console              # Check runtime errors
  agent-browser network requests     # Verify assets load
  agent-browser screenshot           # Visual evidence

⚠️  CRITICAL: HTTP 200 ≠ working UI
   CSS/JS errors return 200 but break the app.
   Screenshots can show broken layout that curl cannot detect.

💡 Tool awareness - You CAN:
   • See screenshots and reason about visual issues
   • Test functionality with curl, not just status codes
   • Query database to verify data persistence
   • Check network requests for failed asset loads
   • Test actual user workflows, not just server health

Advanced verification:

Database persistence:
  PGPASSWORD=$db_password psql -h $db_hostname -U $db_user \
      -d $db_database -c "SELECT * FROM users LIMIT 5;"

Functionality testing:
  # Create test data
  curl -X POST "${stage_zeropsSubdomain}/api/items" \
      -H "Content-Type: application/json" \
      -d '{"name":"test"}'

  # Verify it persisted
  curl -sf "${stage_zeropsSubdomain}/api/items" | jq

Performance testing:
  time curl -sf "${stage_zeropsSubdomain}/" > /dev/null

Gate requirement:
  • verify.sh must pass (creates /tmp/stage_verify.json)
  • failures == 0
  • Browser testing complete (if frontend)
EOF
            ;;
        done)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🎉 DONE PHASE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Final step: Verify all evidence and output completion promise

Command:
  workflow.sh complete

What it checks:
  • All evidence files exist
  • All evidence has matching session_id
  • All verify files have failures == 0

Success output:
  ✅ Evidence validated:
     • Session: 20260118160000-1234-5678
     • Discovery: /tmp/discovery.json ✓
     • Dev verify: /tmp/dev_verify.json (0 failures) ✓
     • Stage verify: /tmp/stage_verify.json (0 failures) ✓

  <completed>WORKFLOW_DONE</completed>

Failure output:
  ❌ Evidence validation failed:
     • Missing evidence files
     • Session ID mismatches
     • Verification failures

  💡 Instructions to fix the issue
EOF
            ;;
        vars)
            show_full_help | sed -n '/🔐 VARIABLES/,/📋 WORKFLOW COMMANDS/p' | head -n -1
            ;;
        trouble)
            show_full_help | sed -n '/🔧 TROUBLESHOOTING/,/📖 COMPLETE EXAMPLE/p' | head -n -1
            ;;
        example)
            show_full_help | sed -n '/📖 COMPLETE EXAMPLE/,/🚪 GATES/p' | head -n -1
            ;;
        gates)
            show_full_help | sed -n '/🚪 GATES/,$p'
            ;;
        *)
            echo "❌ Unknown help topic: $topic"
            echo ""
            echo "Available topics:"
            echo "  discover, develop, deploy, verify, done"
            echo "  vars, trouble, example, gates"
            exit 1
            ;;
    esac
}

# ============================================================================
# COMMANDS
# ============================================================================

cmd_init() {
    local existing_session
    existing_session=$(get_session)

    # Idempotent init - don't create duplicate sessions
    if [ -n "$existing_session" ]; then
        echo "✅ Session already active: $existing_session"
        echo ""
        echo "💡 Current state:"
        cmd_show
        return 0
    fi

    # Create new session
    local session_id
    session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
    echo "$session_id" > "$SESSION_FILE"
    echo "full" > "$MODE_FILE"
    echo "INIT" > "$PHASE_FILE"

    cat <<EOF
✅ Session: $session_id

📋 Workflow: INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE

💡 NEXT: DISCOVER phase
   1. zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "\$ZAGENTS_API_KEY"
   2. zcli service list -P \$projectId
   3. workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
   4. workflow.sh transition_to DISCOVER

⚠️  Cannot skip DISCOVER - creates required evidence

📖 Full reference: workflow.sh --help
EOF
}

cmd_quick() {
    local session_id
    session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
    echo "$session_id" > "$SESSION_FILE"
    echo "quick" > "$MODE_FILE"
    echo "QUICK" > "$PHASE_FILE"

    cat <<'EOF'
✅ Quick mode - no enforcement

💡 Available tools:
   status.sh                    # Check deployment state
   status.sh --wait {svc}       # Wait for deploy
   verify.sh {svc} {port} /...  # Test endpoints
   workflow.sh --help           # Full reference

⚠️  Remember:
   Files: /var/www/{service}/   (SSHFS direct edit)
   Commands: ssh {service} "cmd"
EOF
}

cmd_transition_to() {
    local target_phase="$1"

    if [ -z "$target_phase" ]; then
        echo "❌ Usage: workflow.sh transition_to {phase}"
        echo "Phases: DISCOVER, DEVELOP, DEPLOY, VERIFY, DONE"
        exit 1
    fi

    if ! validate_phase "$target_phase"; then
        echo "❌ Invalid phase: $target_phase"
        echo "Valid phases: ${PHASES[*]}"
        exit 1
    fi

    local current_phase
    local mode
    current_phase=$(get_phase)
    mode=$(get_mode)

    # In quick mode, allow any transition
    if [ "$mode" = "quick" ]; then
        set_phase "$target_phase"
        output_phase_guidance "$target_phase"
        return 0
    fi

    # In full mode, enforce gates
    case "$target_phase" in
        DISCOVER)
            if [ "$current_phase" != "INIT" ]; then
                echo "❌ Cannot transition to DISCOVER from $current_phase"
                echo "📋 Run: workflow.sh init"
                exit 2
            fi
            ;;
        DEVELOP)
            if [ "$current_phase" != "DISCOVER" ]; then
                echo "❌ Cannot transition to DEVELOP from $current_phase"
                echo "📋 Required flow: INIT → DISCOVER → DEVELOP"
                exit 2
            fi
            if ! check_gate_discover_to_develop; then
                return 2
            fi
            ;;
        DEPLOY)
            if [ "$current_phase" != "DEVELOP" ]; then
                echo "❌ Cannot transition to DEPLOY from $current_phase"
                echo "📋 Required flow: DEVELOP → DEPLOY"
                exit 2
            fi
            if ! check_gate_develop_to_deploy; then
                return 2
            fi
            ;;
        VERIFY)
            if [ "$current_phase" != "DEPLOY" ]; then
                echo "❌ Cannot transition to VERIFY from $current_phase"
                echo "📋 Required flow: DEPLOY → VERIFY"
                exit 2
            fi
            ;;
        DONE)
            if [ "$current_phase" != "VERIFY" ]; then
                echo "❌ Cannot transition to DONE from $current_phase"
                echo "📋 Required flow: VERIFY → DONE"
                exit 2
            fi
            if ! check_gate_verify_to_done; then
                return 2
            fi
            ;;
    esac

    set_phase "$target_phase"
    output_phase_guidance "$target_phase"
}

output_phase_guidance() {
    local phase="$1"

    case "$phase" in
        DISCOVER)
            cat <<'EOF'
✅ Phase: DISCOVER

📋 Commands:
   zcli login --region=gomibako \
       --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
       "$ZAGENTS_API_KEY"

   zcli service list -P $projectId

📋 Then record discovery:
   workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

⚠️  Never use 'zcli scope' - it's buggy
⚠️  Use service IDs (from list), not hostnames

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: /tmp/discovery.json must exist
📋 Next: workflow.sh transition_to DEVELOP
EOF
            ;;
        DEVELOP)
            cat <<'EOF'
✅ Phase: DEVELOP

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📁 Files: /var/www/{dev}/     (edit directly via SSHFS)
💻 Run:   ssh {dev} "cmd"     (execute inside container)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Kill existing process:
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'

Build & run:
  ssh {dev} "{build_command}"
  ssh {dev} './{binary} >> /tmp/app.log 2>&1'
  ↑ Set run_in_background=true in Bash tool parameters

Verify:
  verify.sh {dev} {port} / /status /api/...

Logs:
  ssh {dev} "tail -f /tmp/app.log"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: verify.sh must pass (creates /tmp/dev_verify.json)
📋 Next: workflow.sh transition_to DEPLOY
EOF
            ;;
        DEPLOY)
            cat <<'EOF'
✅ Phase: DEPLOY

⚠️  PRE-DEPLOYMENT CHECKLIST (do this BEFORE deploying):
   1. cat /var/www/{dev}/zerops.yaml | grep -A10 deployFiles
   2. Verify ALL artifacts exist:
      ls -la /var/www/{dev}/app
      ls -la /var/www/{dev}/templates/  # if using templates
      ls -la /var/www/{dev}/static/     # if using static files
   3. If you created templates/ or static/, add them to deployFiles!

⚠️  Common failure: Agent builds files but doesn't update deployFiles

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Stop dev process:
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'

Authenticate from dev container:
  ssh {dev} "zcli login --region=gomibako \\
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \\
      \"\$ZAGENTS_API_KEY\""

Deploy to stage:
  ssh {dev} "zcli push {stage_service_id} --setup={setup} --versionName=v1.0.0"

  --setup={setup} → references zerops.yaml build config name
  --versionName   → optional but recommended

**zerops.yaml structure reference:**
zerops:
  - setup: api                    # ← --setup value
    build:
      base: go@1.22
      buildCommands:
        - go build -o app main.go
      deployFiles:
        - ./app
        - ./templates             # Don't forget if you created these!
        - ./static
    run:
      base: go@1.22
      ports:
        - port: 8080
      start: ./app

Wait for completion:
  status.sh --wait {stage}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: status.sh shows SUCCESS notification
📋 Next: workflow.sh transition_to VERIFY
EOF
            ;;
        VERIFY)
            cat <<'EOF'
✅ Phase: VERIFY

Check deployed artifacts:
  ssh {stage} "ls -la /var/www/"

Verify endpoints:
  verify.sh {stage} {port} / /status /api/...

Service logs:
  zcli service log -S {stage_service_id} -P $projectId --follow

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  BROWSER TESTING (required if frontend exists):
   If your app has HTML/CSS/JS/templates:

   URL=$(ssh {stage} "echo \$zeropsSubdomain")
   agent-browser open "$URL"          # Don't prepend https://!
   agent-browser errors               # Must show no errors
   agent-browser console              # Check runtime errors
   agent-browser network requests     # Verify assets load
   agent-browser screenshot           # Visual evidence

⚠️  HTTP 200 ≠ working UI. CSS/JS errors return 200 but break the app.

💡 Tool awareness: You CAN see screenshots and reason about them.
   You CAN use curl to test functionality, not just status codes.
   You CAN query the database to verify data persistence.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: verify.sh must pass (creates /tmp/stage_verify.json)
📋 Next: workflow.sh transition_to DONE
EOF
            ;;
        DONE)
            cat <<'EOF'
✅ Phase: DONE

Run completion check:
  workflow.sh complete

This will verify all evidence and output the completion promise.
EOF
            ;;
    esac
}

cmd_create_discovery() {
    local dev_id="$1"
    local dev_name="$2"
    local stage_id="$3"
    local stage_name="$4"

    if [ -z "$dev_id" ] || [ -z "$dev_name" ] || [ -z "$stage_id" ] || [ -z "$stage_name" ]; then
        echo "❌ Usage: workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}"
        echo ""
        echo "Example:"
        echo "  workflow.sh create_discovery 'abc123' 'appdev' 'def456' 'appstage'"
        exit 1
    fi

    if ! command -v jq &>/dev/null; then
        echo "❌ jq required but not found"
        exit 1
    fi

    local session_id
    session_id=$(get_session)
    if [ -z "$session_id" ]; then
        echo "❌ No active session. Run: workflow.sh init"
        exit 1
    fi

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --arg did "$dev_id" \
        --arg dname "$dev_name" \
        --arg stid "$stage_id" \
        --arg stname "$stage_name" \
        '{
            session_id: $sid,
            timestamp: $ts,
            dev: {
                id: $did,
                name: $dname
            },
            stage: {
                id: $stid,
                name: $stname
            }
        }' > "$DISCOVERY_FILE"

    echo "✅ Discovery recorded: $DISCOVERY_FILE"
    echo ""
    echo "Dev:   $dev_name ($dev_id)"
    echo "Stage: $stage_name ($stage_id)"
    echo ""
    echo "📋 Next: workflow.sh transition_to DISCOVER"
}

cmd_show() {
    local session_id
    local mode
    local phase

    session_id=$(get_session)
    mode=$(get_mode)
    phase=$(get_phase)

    cat <<EOF
╔══════════════════════════════════════════════════════════════════╗
║  WORKFLOW STATUS                                                 ║
╚══════════════════════════════════════════════════════════════════╝

Session:  ${session_id:-none}
Mode:     ${mode:-none}
Phase:    ${phase:-none}

Evidence:
EOF

    # Check discovery
    if check_evidence_session "$DISCOVERY_FILE"; then
        echo "  ✓ /tmp/discovery.json (current session)"
    elif [ -f "$DISCOVERY_FILE" ]; then
        echo "  ✗ /tmp/discovery.json (stale session)"
    else
        echo "  ✗ /tmp/discovery.json (missing)"
    fi

    # Check dev verify
    if check_evidence_session "$DEV_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        echo "  ✓ /tmp/dev_verify.json (current session, $failures failures)"
    elif [ -f "$DEV_VERIFY_FILE" ]; then
        echo "  ✗ /tmp/dev_verify.json (stale session)"
    else
        echo "  ✗ /tmp/dev_verify.json (missing)"
    fi

    # Check stage verify
    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        echo "  ✓ /tmp/stage_verify.json (current session, $failures failures)"
    elif [ -f "$STAGE_VERIFY_FILE" ]; then
        echo "  ✗ /tmp/stage_verify.json (stale session)"
    else
        echo "  ✗ /tmp/stage_verify.json (missing)"
    fi

    echo ""

    case "$phase" in
        INIT|DISCOVER)
            echo "Next: workflow.sh transition_to DISCOVER"
            ;;
        DEVELOP)
            echo "Next: workflow.sh transition_to DEPLOY"
            ;;
        DEPLOY)
            echo "Next: workflow.sh transition_to VERIFY"
            ;;
        VERIFY)
            echo "Next: workflow.sh transition_to DONE"
            ;;
        DONE)
            echo "Next: workflow.sh complete"
            ;;
    esac
}

cmd_complete() {
    local session_id
    session_id=$(get_session)

    if [ -z "$session_id" ]; then
        echo "❌ No active session"
        exit 1
    fi

    local all_valid=true
    local messages=()

    # Check all evidence
    if check_evidence_session "$DISCOVERY_FILE"; then
        messages+=("   • Discovery: /tmp/discovery.json ✓")
    else
        messages+=("   ✗ Discovery: /tmp/discovery.json MISSING or stale")
        all_valid=false
    fi

    if check_evidence_session "$DEV_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        if [ "$failures" -eq 0 ]; then
            messages+=("   • Dev verify: /tmp/dev_verify.json (0 failures) ✓")
        else
            messages+=("   ✗ Dev verify: /tmp/dev_verify.json ($failures failures)")
            all_valid=false
        fi
    else
        messages+=("   ✗ Dev verify: /tmp/dev_verify.json MISSING or stale")
        all_valid=false
    fi

    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        if [ "$failures" -eq 0 ]; then
            messages+=("   • Stage verify: /tmp/stage_verify.json (0 failures) ✓")
        else
            messages+=("   ✗ Stage verify: /tmp/stage_verify.json ($failures failures)")
            all_valid=false
        fi
    else
        messages+=("   ✗ Stage verify: /tmp/stage_verify.json MISSING or stale")
        all_valid=false
    fi

    if [ "$all_valid" = true ]; then
        echo "✅ Evidence validated:"
        echo "   • Session: $session_id"
        printf '%s\n' "${messages[@]}"
        echo ""
        echo "<completed>WORKFLOW_DONE</completed>"
        return 0
    else
        echo "❌ Evidence validation failed:"
        echo ""
        echo "   • Session: $session_id"
        printf '%s\n' "${messages[@]}"
        echo ""
        echo "💡 Fix the issues above and run: workflow.sh complete"
        return 3
    fi
}

cmd_reset() {
    rm -f "$SESSION_FILE" "$MODE_FILE" "$PHASE_FILE"
    rm -f "$DISCOVERY_FILE" "$DEV_VERIFY_FILE" "$STAGE_VERIFY_FILE"
    echo "✅ All workflow state cleared"
    echo ""
    echo "💡 Start fresh:"
    echo "   workflow.sh init"
}

# ============================================================================
# GATE CHECKS
# ============================================================================

check_gate_discover_to_develop() {
    if ! check_evidence_session "$DISCOVERY_FILE"; then
        cat <<'EOF'
❌ Cannot transition to DEVELOP

📋 Gate requirement: /tmp/discovery.json must exist with current session

💡 Complete DISCOVER first:
   1. zcli service list -P $projectId
   2. workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
   3. workflow.sh transition_to DISCOVER
EOF
        return 1
    fi

    # Check that stage != dev
    if command -v jq &>/dev/null; then
        local dev_name stage_name
        dev_name=$(jq -r '.dev.name' "$DISCOVERY_FILE" 2>/dev/null)
        stage_name=$(jq -r '.stage.name' "$DISCOVERY_FILE" 2>/dev/null)

        if [ "$dev_name" = "$stage_name" ]; then
            echo "❌ Cannot transition to DEVELOP"
            echo ""
            echo "⚠️  Dev and stage services are the same: $dev_name"
            echo "    This would overwrite your source code!"
            echo ""
            echo "💡 Fix discovery.json with different services"
            return 1
        fi
    fi

    return 0
}

check_gate_develop_to_deploy() {
    if ! check_evidence_session "$DEV_VERIFY_FILE"; then
        cat <<'EOF'
❌ Cannot transition to DEPLOY

📋 Gate requirement: /tmp/dev_verify.json must exist with current session

💡 Complete DEVELOP verification first:
   1. verify.sh {dev} {port} / /status /api/...
   2. Ensure all endpoints pass
EOF
        return 1
    fi

    if command -v jq &>/dev/null; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        if [ "$failures" -ne 0 ]; then
            echo "❌ Cannot transition to DEPLOY"
            echo ""
            echo "⚠️  Dev verification has $failures failure(s)"
            echo ""
            echo "💡 Fix the failing endpoints first:"
            echo "   1. Review verify.sh output"
            echo "   2. Check logs: ssh {dev} \"tail -f /tmp/app.log\""
            echo "   3. Fix issues and re-run verify.sh"
            return 1
        fi
    fi

    return 0
}

check_gate_verify_to_done() {
    if ! check_evidence_session "$STAGE_VERIFY_FILE"; then
        cat <<'EOF'
❌ Cannot transition to DONE

📋 Gate requirement: /tmp/stage_verify.json must exist with current session

💡 Complete VERIFY first:
   1. verify.sh {stage} {port} / /status /api/...
   2. Ensure all endpoints pass
   3. Browser test if frontend exists
EOF
        return 1
    fi

    if command -v jq &>/dev/null; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        if [ "$failures" -ne 0 ]; then
            echo "❌ Cannot transition to DONE"
            echo ""
            echo "⚠️  Stage verification has $failures failure(s)"
            echo ""
            echo "💡 Fix the failing endpoints first"
            return 1
        fi
    fi

    return 0
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    local command="$1"
    shift

    case "$command" in
        init)
            cmd_init
            ;;
        --quick)
            cmd_quick
            ;;
        --help)
            if [ -z "$1" ]; then
                show_full_help
            else
                show_topic_help "$1"
            fi
            ;;
        transition_to)
            cmd_transition_to "$@"
            ;;
        create_discovery)
            cmd_create_discovery "$@"
            ;;
        show)
            cmd_show
            ;;
        complete)
            cmd_complete
            ;;
        reset)
            cmd_reset
            ;;
        "")
            echo "❌ No command specified"
            echo ""
            echo "Usage: workflow.sh {command}"
            echo ""
            echo "Commands:"
            echo "  init                    Start enforced workflow"
            echo "  --quick                 Quick mode (no enforcement)"
            echo "  --help [topic]          Show help"
            echo "  transition_to {phase}   Advance to phase"
            echo "  create_discovery ...    Record services"
            echo "  show                    Current status"
            echo "  complete                Verify evidence"
            echo "  reset                   Clear state"
            exit 1
            ;;
        *)
            echo "❌ Unknown command: $command"
            echo ""
            echo "Run: workflow.sh --help"
            exit 1
            ;;
    esac
}

main "$@"
