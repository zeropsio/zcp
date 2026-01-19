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
DEPLOY_EVIDENCE_FILE="/tmp/deploy_evidence.json"

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

check_evidence_freshness() {
    local file="$1"
    local max_age_hours="${2:-24}"

    if [ ! -f "$file" ]; then
        return 0  # No file = no staleness check
    fi

    local timestamp
    timestamp=$(jq -r '.timestamp // empty' "$file" 2>/dev/null)
    if [ -z "$timestamp" ]; then
        return 0  # No timestamp = can't check
    fi

    # Parse timestamp and calculate age
    local evidence_epoch now_epoch age_hours

    # Try GNU date first, then BSD date
    if evidence_epoch=$(date -d "$timestamp" +%s 2>/dev/null); then
        : # GNU date worked
    elif evidence_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$timestamp" +%s 2>/dev/null); then
        : # BSD date worked
    else
        return 0  # Can't parse = skip check
    fi

    now_epoch=$(date +%s)
    age_hours=$(( (now_epoch - evidence_epoch) / 3600 ))

    if [ "$age_hours" -gt "$max_age_hours" ]; then
        echo ""
        echo "⚠️  STALE EVIDENCE WARNING"
        echo "   File: $file"
        echo "   Age: ${age_hours} hours (threshold: ${max_age_hours}h)"
        echo "   Created: $timestamp"
        echo ""
        echo "   Consider re-verifying to ensure current system state"
        echo "   (Proceeding anyway - this is a warning, not a blocker)"
        echo ""
    fi
    return 0
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
      "$ZEROPS_ZAGENT_API_KEY"

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

Stop dev process:
  ssh {dev} 'pkill -9 {proc}; fuser -k {port}/tcp 2>/dev/null; true'

Authenticate from dev container:
  ssh {dev} "zcli login --region=gomibako \
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
      \"\$ZEROPS_ZAGENT_API_KEY\""

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
    "$ZEROPS_ZAGENT_API_KEY"
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
ssh appdev 'pkill -9 app; fuser -k 8080/tcp 2>/dev/null; true'
ssh appdev "zcli login --region=gomibako \
    --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
    \"\$ZEROPS_ZAGENT_API_KEY\""
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
      "$ZEROPS_ZAGENT_API_KEY"

  zcli service list -P $projectId

Record discovery:
  .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

  Example:
    .zcp/workflow.sh create_discovery \
        "abc123def456" "appdev" \
        "ghi789jkl012" "appstage"

Transition:
  .zcp/workflow.sh transition_to DISCOVER

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

Purpose: Build, test, and iterate until the feature works correctly

⚠️  CRITICAL MINDSET:
    Dev is where you iterate. Fix all errors HERE before deploying.
    Stage is for final validation, not debugging.

    A human developer doesn't deploy broken code to stage to "see if it works."
    They test locally, fix issues, repeat until it works, THEN deploy to stage.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔄 THE DEVELOP LOOP
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

┌─────────────────────────────────────────────────────────────────┐
│  1. Make changes (edit files via SSHFS)                         │
│  2. Build & restart the app                                     │
│  3. Test the actual functionality                               │
│  4. Check for errors (logs, responses, browser console)         │
│  5. If errors exist → Fix → Go to step 1                        │
│  6. Only when working → run verify.sh → transition to DEPLOY    │
└─────────────────────────────────────────────────────────────────┘

This loop may repeat many times. That's normal and expected.
Deploying broken code to stage to "see if it works" is not acceptable.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📁 Context
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Files: /var/www/{dev}/     (edit directly via SSHFS)
  Run:   ssh {dev} "cmd"     (execute inside container)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔧 Build & Run
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Triple-kill pattern (clear orphan processes):
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; \
             fuser -k {port}/tcp 2>/dev/null; true'

Build & run:
  ssh {dev} "{build_command}"
  ssh {dev} './{binary} >> /tmp/app.log 2>&1'

  ⚠️  Set run_in_background=true in Bash tool parameters!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 FUNCTIONAL TESTING (not just HTTP status!)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

HTTP 200 means "server responded." It does NOT mean "feature works."

Backend APIs - Read and verify response content:
  # Don't just check status - examine the actual response
  ssh {dev} "curl -s http://localhost:{port}/api/endpoint" | jq .

  # Test the operation you implemented
  ssh {dev} "curl -s -X POST http://localhost:{port}/api/items \
      -H 'Content-Type: application/json' -d '{\"name\":\"test\"}'"

  # Verify data persisted correctly
  ssh {dev} "curl -s http://localhost:{port}/api/items" | jq .

Frontend - Check for JavaScript/runtime errors:
  URL=$(ssh {dev} "echo \$zeropsSubdomain")
  agent-browser open "$URL"
  agent-browser errors          # ← MUST be empty before deploy
  agent-browser console         # ← Look for runtime errors
  agent-browser screenshot      # ← Visual verification

Logs - Look for errors, not just "it started":
  ssh {dev} "tail -50 /tmp/app.log"
  ssh {dev} "grep -iE 'error|exception|panic|fatal' /tmp/app.log"

Database - Verify persistence:
  PGPASSWORD=$db_password psql -h $db_hostname -U $db_user -d $db_database \
      -c "SELECT * FROM {table} ORDER BY id DESC LIMIT 5;"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
❌ DO NOT proceed to DEPLOY if:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  • API response contains error messages or unexpected data
  • Logs show exceptions, stack traces, or error messages
  • Browser console has JavaScript errors
  • UI is broken, not rendering, or has visual bugs
  • Data isn't persisting or returning correctly
  • You haven't actually tested the feature you implemented

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ Proceed to DEPLOY only when:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  • The feature works as expected on dev
  • No errors in logs or browser console
  • You've tested actual functionality, not just "server responds"
  • You could demo this feature to a user right now

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🐛 Debugging
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  # Check process running
  ssh {dev} "pgrep -f {proc}"
  ssh {dev} "ps aux | grep {proc}"

  # Check port listening
  ssh {dev} "ss -tlnp | grep {port}"

  # Follow logs in real-time
  ssh {dev} "tail -f /tmp/app.log"

Gate requirement:
  • verify.sh must pass (creates /tmp/dev_verify.json)
  • Feature must work correctly (not just HTTP 200)
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
       \"\$ZEROPS_ZAGENT_API_KEY\""

3. Deploy to stage:
   ssh {dev} "zcli push {stage_service_id} --setup={setup} --versionName=v1.0.0"

   • {stage_service_id} = ID from discovery (not hostname!)
   • {setup} = setup name from zerops.yaml
   • --versionName optional but recommended

4. Wait for completion:
   .zcp/status.sh --wait {stage}

Redeploy/Retry procedure:
  If deployment fails or needs retry:
  1. zcli project notifications -P $projectId    # Check error
  2. Fix the issue (usually deployFiles or code)
  3. ssh {dev} "zcli push {stage_id} --setup={setup}"
  4. .zcp/status.sh --wait {stage}

Gate requirement:
  • .zcp/status.sh shows SUCCESS notification
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
   .zcp/verify.sh {stage} {port} / /status /api/...

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
  .zcp/workflow.sh complete

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


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Next task? Run workflow.sh again to decide:
   .zcp/workflow.sh init    → deploying
   .zcp/workflow.sh --quick → exploring

Failure output:
  ❌ Evidence validation failed:
     • Missing evidence files
     • Session ID mismatches
     • Verification failures

  💡 Instructions to fix the issue
EOF
            ;;
        vars)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔐 ENVIRONMENT VARIABLES - COMPREHENSIVE REFERENCE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  CRITICAL: Service names are ARBITRARY (user-defined hostnames)
   Variables use HOSTNAME, not service type!
   Must discover actual names via: zcli service list -P $projectId

Variable Structure:
  Pattern: {hostname}_{VARIABLE}
  Example: ${myapp_zeropsSubdomain}, ${users-db_password}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 ACCESS PATTERNS BY CONTEXT
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

┌─ ZCP → Service Variable ───────────────────────────────────────┐
│ Pattern: ${hostname}_VARIABLE                                   │
│                                                                  │
│ Examples:                                                        │
│   echo "${myapp_zeropsSubdomain}"                               │
│   echo "${backend_hostname}"                                     │
│   curl "${api_zeropsSubdomain}/"                                │
│                                                                  │
│ ⚠️  CRITICAL: zeropsSubdomain is FULL URL                       │
│     curl "${myapp_zeropsSubdomain}/"        ✅ CORRECT          │
│     curl "https://${myapp_zeropsSubdomain}/" ❌ WRONG (double)  │
└──────────────────────────────────────────────────────────────────┘

┌─ Inside Service → Own Variables ───────────────────────────────┐
│ Pattern: $VARIABLE (no prefix inside container)                 │
│                                                                  │
│ Examples:                                                        │
│   ssh myapp "echo \$hostname"          # myapp                  │
│   ssh myapp "echo \$zeropsSubdomain"   # Full HTTPS URL         │
│   ssh myapp "echo \$serviceId"         # Service ID             │
└──────────────────────────────────────────────────────────────────┘

┌─ ZCP → Database Variables ─────────────────────────────────────┐
│ Pattern: ${dbhostname}_* (use actual DB hostname!)              │
│                                                                  │
│ PostgreSQL:                                                      │
│   ${postgres_connectionString}                                  │
│   ${postgres_hostname}, ${postgres_user}, ${postgres_password}  │
│   ${postgres_port}, ${postgres_dbName}                          │
│                                                                  │
│ NATS:                                                            │
│   ${nats_connectionString}                                      │
│   ${nats_hostname}, ${nats_user}, ${nats_password}              │
│                                                                  │
│ Valkey/Redis:                                                    │
│   ${cache_connectionString}                                     │
│   ${cache_hostname}, ${cache_port}                              │
│                                                                  │
│ Typesense:                                                       │
│   ${search_connectionString}                                    │
│   ${search_apiKey}, ${search_hostname}                          │
│                                                                  │
│ Usage from ZCP:                                                  │
│   PGPASSWORD=${postgres_password} psql -h ${postgres_hostname} \│
│       -U ${postgres_user} -d ${postgres_dbName}                 │
│   redis-cli -h ${cache_hostname} -p ${cache_port}               │
└──────────────────────────────────────────────────────────────────┘

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  VARIABLE TIMING
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Services capture env vars at START TIME. To see new/changed vars → restart.

When ZCP doesn't have a variable (service added after ZCP started):
  echo "${appdev_PORT}"              # Empty
  ssh appdev "echo \$PORT"           # Get it from appdev directly

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔧 COMMON SERVICE VARIABLES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Every service has (replace {hostname} with actual service name):

Identity:
  {hostname}_serviceId          # Unique service ID (for zcli -S flag)
  {hostname}_hostname           # Service hostname (for ssh, URLs)
  {hostname}_projectId          # Parent project ID

Network:
  {hostname}_zeropsSubdomain       # Full HTTPS URL (don't prepend!)
  {hostname}_zeropsSubdomainString # Template: https://{host}-{num}-${port}...

Security:
  {hostname}_ZEROPS_ZAGENT_API_KEY    # zcli authentication key
  {hostname}_envIsolation       # "none" or "service"
  {hostname}_sshIsolation       # SSH access rules

Metadata (Runtime):
  {hostname}_appVersionId       # Deployed version ID
  {hostname}_appVersionName     # Version name (e.g., "main")
  {hostname}_startCommand       # Start command
  {hostname}_workingDir         # Working directory

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🏗️  BUILD CONTAINER VARIABLES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Build containers use RUNTIME_ prefix to access target runtime variables:

  build{name}_RUNTIME_hostname         # Target service hostname
  build{name}_RUNTIME_serviceId        # Target service ID
  build{name}_RUNTIME_zeropsSubdomain  # Target service URL
  build{name}_RUNTIME_DB_HOST          # Target DB connection

This allows builds to know deployment target environment!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔐 ENV ISOLATION MODES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

envIsolation=none (Legacy):
  • All service variables visible to ZCP with prefix
  • Enables ${hostname}_variable pattern
  • Less secure, but simpler for development

envIsolation=service (Recommended):
  • Strict variable isolation per service
  • Must explicitly reference: ${service@variable}
  • Better security, prevents leaks

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 SUMMARY TABLE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Context          │ Pattern              │ Example
─────────────────┼──────────────────────┼────────────────────────────
ZCP → Own        │ $VAR                 │ $projectId
ZCP → Service    │ ${hostname}_VAR      │ ${myapp_hostname}
ZCP → Database   │ ${dbname}_*          │ ${postgres_password}
Service → Own    │ $VAR via SSH         │ ssh myapp "echo \$hostname"
Service → Service│ http://hostname:port │ http://postgres:5432

See also: .zcp/workflow.sh --help services (for service naming details)
EOF
            ;;
        services)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🏷️  SERVICE NAMING - CRITICAL UNDERSTANDING
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  SERVICE HOSTNAMES ARE ARBITRARY (NOT TIED TO SERVICE TYPE)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 THE CRITICAL DISTINCTION
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Hostname (User-Defined):
  • What YOU name the service in zerops.yaml
  • Can be ANYTHING: myapp, backend, users-db, cache1, api-prod
  • Used for: SSH, variables, networking
  • Examples: "myapp", "postgres-main", "redis-cache"

Service Type (Zerops-Defined):
  • The technology: postgresql, nats, valkey, nodejs, go
  • Cannot be changed
  • Internal to Zerops
  • Examples: "postgresql@17", "valkey@7.2", "go@1.22"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  WHY THIS MATTERS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Variables use HOSTNAME, not type!

If PostgreSQL service is named "users-db":
  ${users-db_connectionString}   ✅ CORRECT
  ${postgres_connectionString}   ❌ WRONG
  ${db_connectionString}         ❌ WRONG (unless you named it "db")

If app service is named "backend":
  ${backend_zeropsSubdomain}     ✅ CORRECT
  ssh backend "echo \$hostname"  ✅ CORRECT
  ${app_zeropsSubdomain}         ❌ WRONG

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📖 COMMON NAMING PATTERNS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Pattern 1: Type as Hostname (Simple)
  services:
    - hostname: db            # Type: postgresql
    - hostname: cache         # Type: valkey
    - hostname: nats          # Type: nats

  Variables: ${db_password}, ${cache_connectionString}, ${nats_user}

Pattern 2: Descriptive Names (Production)
  services:
    - hostname: users-database      # Type: postgresql
    - hostname: session-cache       # Type: valkey
    - hostname: event-queue         # Type: nats

  Variables: ${users-database_password}, ${session-cache_port}

Pattern 3: Environment Suffixes
  services:
    - hostname: api-dev       # Type: nodejs (development)
    - hostname: api-stage     # Type: nodejs (staging)
    - hostname: api-prod      # Type: nodejs (production)

  Variables: ${api-dev_zeropsSubdomain}, ${api-prod_zeropsSubdomain}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 DISCOVERING ACTUAL SERVICE NAMES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ALWAYS use zcli service list to discover actual hostnames:

  zcli service list -P $projectId

Output shows:
  ┌───────────┬────────────────┬────────┬─────────────┐
  │ ID        │ NAME (hostname)│ STATUS │ TYPE        │
  ├───────────┼────────────────┼────────┼─────────────┤
  │ abc123... │ myapp          │ ACTIVE │ nodejs      │
  │ def456... │ backend-api    │ ACTIVE │ go          │
  │ ghi789... │ users-db       │ ACTIVE │ postgresql  │
  │ jkl012... │ cache          │ ACTIVE │ valkey      │
  └───────────┴────────────────┴────────┴─────────────┘

Then use DISCOVERED hostnames:
  ssh myapp "..."
  echo "${backend-api_zeropsSubdomain}"
  PGPASSWORD=${users-db_password} psql ...

⚠️  NEVER assume service names match types!
⚠️  NEVER hardcode service names in scripts!
⚠️  ALWAYS discover first with zcli service list!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🎯 THIS IS WHY DISCOVER PHASE IS MANDATORY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

You cannot proceed without knowing actual service hostnames!

.zcp/workflow.sh create_discovery uses DISCOVERED names:
  .zcp/workflow.sh create_discovery \
    "abc123" "myapp" \      ← Actual hostname from zcli service list
    "def456" "backend-api"  ← NOT type name, actual hostname

This ensures all subsequent operations use correct names.

See also: .zcp/workflow.sh --help vars (for variable access patterns)
EOF
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
        extend)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔧 EXTENDING YOUR PROJECT WITH NEW SERVICES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Common scenario: You have a working app and want to add PostgreSQL,
Valkey (Redis), NATS, or another managed service.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  THE CHALLENGE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Environment variables are captured at ZCP start
2. New services' vars ($db_host, etc.) won't be visible until restart
3. discovery.json doesn't auto-update when services are added

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP-BY-STEP FLOW
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. CREATE THE IMPORT FILE
   ─────────────────────────────────────────────────────────────
   cat > add-service.yml <<'YAML'
   services:
     - hostname: db
       type: postgresql@16
       mode: NON_HA
   YAML

   Service types: postgresql@16, valkey@7, nats@2, mariadb@10

2. IMPORT THE SERVICE
   ─────────────────────────────────────────────────────────────
   zcli project import ./add-service.yml -P $projectId

3. WAIT FOR SERVICE TO BE READY
   ─────────────────────────────────────────────────────────────
   # Check status
   zcli service list -P $projectId | grep db

   # Wait for RUNNING state (usually 1-2 minutes for databases)
   while ! zcli service list -P $projectId | grep -q "db.*RUNNING"; do
     echo "Waiting for db..."
     sleep 10
   done

4. ACCESS CREDENTIALS
   ─────────────────────────────────────────────────────────────
   ⚠️  ZCP captured env vars at START. New vars not visible!

   Option A: Restart ZCP (picks up all new env vars)
     - Close your IDE session
     - Reconnect to ZCP
     - New vars will be available as ${db_hostname}, etc.

   Option B: Read directly via SSH (no restart needed)
     ssh db 'echo $hostname'
     ssh db 'echo $port'
     ssh db 'echo $user'
     ssh db 'echo $password'

5. UPDATE YOUR CODE
   ─────────────────────────────────────────────────────────────
   Use connection pattern from the service.

   Go + PostgreSQL:
     connStr := fmt.Sprintf(
         "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
         os.Getenv("db_host"),
         os.Getenv("db_port"),
         os.Getenv("db_user"),
         os.Getenv("db_password"),
         os.Getenv("db_dbName"),
     )

   Node + PostgreSQL:
     const pool = new Pool({
       host: process.env.db_hostname,
       port: process.env.db_port,
       user: process.env.db_user,
       password: process.env.db_password,
       database: process.env.db_dbName,
     });

6. TEST CONNECTION
   ─────────────────────────────────────────────────────────────
   # Get credentials via SSH (since ZCP env not updated)
   DB_HOST=$(ssh db 'echo $hostname')
   DB_PASS=$(ssh db 'echo $password')
   DB_USER=$(ssh db 'echo $user')
   DB_NAME=$(ssh db 'echo $dbName')

   # Test PostgreSQL connection
   PGPASSWORD=$DB_PASS psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "SELECT 1"

   # Test Valkey/Redis connection
   redis-cli -h $(ssh cache 'echo $hostname') -p $(ssh cache 'echo $port') PING

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 CONNECTION PATTERNS BY SERVICE TYPE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

PostgreSQL (hostname: db)
  ${db_hostname}, ${db_port}, ${db_user}, ${db_password}, ${db_dbName}
  ${db_connectionString}  ← Full connection string

Valkey/Redis (hostname: cache)
  ${cache_hostname}, ${cache_port}, ${cache_password}
  ${cache_connectionString}

NATS (hostname: nats)
  ${nats_hostname}, ${nats_port}, ${nats_user}, ${nats_password}
  ${nats_connectionString}

Object Storage (hostname: storage)
  ${storage_accessKeyId}, ${storage_secretAccessKey}
  ${storage_apiUrl}, ${storage_bucketName}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  ENV VAR TIMING - CRITICAL
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ZCP captures environment variables at START TIME.

When you add a new service, ZCP does NOT automatically see its vars.

Your options:
  1. RESTART ZCP: Reconnect to pick up new vars
  2. SSH READ: ssh {service} 'echo $varname' to get values directly

This is platform behavior, not a bug. Plan for it.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📖 COMPLETE EXAMPLE: Adding PostgreSQL to Go App
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 1. Create import file
cat > add-postgres.yml <<'YAML'
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
YAML

# 2. Import
zcli project import ./add-postgres.yml -P $projectId

# 3. Wait for ready
while ! zcli service list -P $projectId | grep -q "db.*RUNNING"; do
  echo "Waiting for db service..."
  sleep 10
done
echo "Database ready!"

# 4. Get credentials via SSH
DB_HOST=$(ssh db 'echo $hostname')
DB_PORT=$(ssh db 'echo $port')
DB_USER=$(ssh db 'echo $user')
DB_PASS=$(ssh db 'echo $password')
DB_NAME=$(ssh db 'echo $dbName')

# 5. Test connection
PGPASSWORD=$DB_PASS psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c "SELECT 1"

# 6. Update your Go code to use these env vars
# The app will read them at runtime after deployment
EOF
            ;;
        bootstrap)
            cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🚀 BOOTSTRAPPING A NEW PROJECT FROM SCRATCH
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

You're starting with an empty project. Here's how to go from
zero to deployed application.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 THE FLOW
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

DEFINE → CREATE → DEVELOP → DEPLOY → VERIFY → DONE

Instead of discovery-first (services exist), this is creation-first.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📝 STEP 1: DEFINE (Create import.yml)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Create an import.yml for AI Agent pattern (dev + stage):

cat > import.yml <<'YAML'
services:
  # Dev service (edit files here)
  - hostname: appdev
    type: go@latest
    buildFromGit: false
    enableSubdomainAccess: true

  # Stage service (deploy here)
  - hostname: appstage
    type: go@latest
    buildFromGit: false
    enableSubdomainAccess: true
YAML

Language options: go@latest, nodejs@20, php@8, python@3, etc.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📦 STEP 2: CREATE (Import services)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

zcli project import ./import.yml -P $projectId

Wait for services to be ready:

while zcli service list -P $projectId | grep -qE "PENDING|BUILDING"; do
  echo "Waiting for services to be ready..."
  sleep 30
done
echo "Services ready!"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💻 STEP 3: DEVELOP
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Now services exist. Run normal workflow:

  .zcp/workflow.sh init
  zcli service list -P $projectId
  .zcp/workflow.sh create_discovery {dev_id} appdev {stage_id} appstage
  .zcp/workflow.sh transition_to DISCOVER
  .zcp/workflow.sh transition_to DEVELOP

Create your application code at /var/www/appdev/

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📖 COMPLETE EXAMPLE: Go Hello World
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 1. Create import.yml
cat > import.yml <<'YAML'
services:
  - hostname: appdev
    type: go@latest
    buildFromGit: false
    enableSubdomainAccess: true
  - hostname: appstage
    type: go@latest
    buildFromGit: false
    enableSubdomainAccess: true
YAML

# 2. Import
zcli project import ./import.yml -P $projectId

# 3. Wait
while zcli service list -P $projectId | grep -qE "PENDING|BUILDING"; do
  sleep 30
done

# 4. Start workflow
.zcp/workflow.sh init
zcli service list -P $projectId  # Get IDs
.zcp/workflow.sh create_discovery "abc123" "appdev" "def456" "appstage"
.zcp/workflow.sh transition_to DISCOVER
.zcp/workflow.sh transition_to DEVELOP

# 5. Create hello world
cat > /var/www/appdev/main.go <<'GO'
package main

import (
    "fmt"
    "net/http"
)

func main() {
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "Hello, World!")
    })
    http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "OK")
    })
    http.ListenAndServe(":8080", nil)
}
GO

# 6. Create zerops.yaml
cat > /var/www/appdev/zerops.yaml <<'YAML'
zerops:
  - setup: app
    build:
      base: go@latest
      buildCommands:
        - go build -o app main.go
      deployFiles:
        - ./app
    run:
      base: go@latest
      ports:
        - port: 8080
      start: ./app
YAML

# 7. Build, run, verify
ssh appdev "cd /var/www && go build -o app main.go"
ssh appdev "/var/www/app >> /tmp/app.log 2>&1"  # run_in_background=true
.zcp/verify.sh appdev 8080 / /status

# Continue with normal DEPLOY → VERIFY → DONE flow
EOF
            ;;
        *)
            echo "❌ Unknown help topic: $topic"
            echo ""
            echo "Available topics:"
            echo "  discover, develop, deploy, verify, done"
            echo "  vars, services, trouble, example, gates"
            echo "  extend, bootstrap"
            return 1
            ;;
    esac
}

# ============================================================================
# COMMANDS
# ============================================================================

cmd_init() {
    local mode_flag="$1"
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

    # Handle --dev-only mode
    if [ "$mode_flag" = "--dev-only" ]; then
        local session_id
        session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
        echo "$session_id" > "$SESSION_FILE"
        echo "dev-only" > "$MODE_FILE"
        echo "INIT" > "$PHASE_FILE"

        cat <<'EOF'
✅ DEV-ONLY MODE

📋 Flow: INIT → DISCOVER → DEVELOP → DONE
   (No deployment, no stage verification)

💡 Use this for:
   - Rapid prototyping
   - Dev iteration without deployment
   - Testing before committing to deploy

⚠️  To upgrade to full deployment later:
   .zcp/workflow.sh upgrade-to-full
EOF
        return 0
    fi

    # Handle --hotfix mode
    if [ "$mode_flag" = "--hotfix" ]; then
        # Check for recent discovery
        if [ -f "$DISCOVERY_FILE" ]; then
            local timestamp age_hours
            timestamp=$(jq -r '.timestamp // empty' "$DISCOVERY_FILE" 2>/dev/null)
            if [ -n "$timestamp" ]; then
                local disco_epoch now_epoch
                # Try GNU date first, then BSD date
                if disco_epoch=$(date -d "$timestamp" +%s 2>/dev/null); then
                    : # GNU date worked
                elif disco_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$timestamp" +%s 2>/dev/null); then
                    : # BSD date worked
                fi

                if [ -n "$disco_epoch" ]; then
                    now_epoch=$(date +%s)
                    age_hours=$(( (now_epoch - disco_epoch) / 3600 ))

                    local max_age="${HOTFIX_MAX_AGE_HOURS:-24}"
                    if [ "$age_hours" -lt "$max_age" ]; then
                        local session_id
                        session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
                        echo "$session_id" > "$SESSION_FILE"
                        echo "hotfix" > "$MODE_FILE"
                        echo "DEVELOP" > "$PHASE_FILE"

                        # Update session in discovery
                        if jq --arg sid "$session_id" '.session_id = $sid' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp"; then
                            mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"
                        else
                            rm -f "${DISCOVERY_FILE}.tmp"
                            echo "Failed to update discovery.json" >&2
                            return 1
                        fi

                        cat <<EOF
🚨 HOTFIX MODE

✓ Reusing discovery from ${age_hours}h ago
  Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")
  Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")

📋 Flow: DEVELOP → DEPLOY → VERIFY → DONE
   (Skipping discovery and dev verification)

⚠️  REDUCED SAFETY:
   - No dev verification (you may deploy untested code)
   - Stage verification still REQUIRED

Ready. Start implementing your hotfix.
EOF
                        return 0
                    fi
                fi
            fi
        fi

        echo "❌ Cannot use hotfix mode"
        echo "   No recent discovery (< ${HOTFIX_MAX_AGE_HOURS:-24}h) found"
        echo "   Run: .zcp/workflow.sh init (normal mode)"
        return 1
    fi

    # Create new session
    local session_id
    session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
    echo "$session_id" > "$SESSION_FILE"
    echo "full" > "$MODE_FILE"
    echo "INIT" > "$PHASE_FILE"

    # Check for preserved discovery and update session_id
    if [ -f "$DISCOVERY_FILE" ]; then
        local old_session
        old_session=$(jq -r '.session_id // empty' "$DISCOVERY_FILE" 2>/dev/null)
        if [ -n "$old_session" ]; then
            # Update session_id in existing discovery
            if jq --arg sid "$session_id" '.session_id = $sid' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp"; then
                mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"
            else
                rm -f "${DISCOVERY_FILE}.tmp"
                echo "Failed to update discovery.json" >&2
                return 1
            fi

            echo "✅ Session: $session_id"
            echo ""
            echo "📋 Preserved discovery detected:"
            echo "   Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")"
            echo "   Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")"
            echo ""
            echo "💡 NEXT: Skip DISCOVER, go directly to DEVELOP"
            echo "   .zcp/workflow.sh transition_to DISCOVER"
            echo "   .zcp/workflow.sh transition_to DEVELOP"
            return 0
        fi
    fi

    # Normal init (no preserved discovery)
    cat <<EOF
✅ Session: $session_id

📋 Workflow: INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE

💡 NEXT: DISCOVER phase
   1. zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "\$ZEROPS_ZAGENT_API_KEY"
   2. zcli service list -P \$projectId
   3. .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
   4. .zcp/workflow.sh transition_to DISCOVER

⚠️  Cannot skip DISCOVER - creates required evidence

📖 Full reference: .zcp/workflow.sh --help
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
   .zcp/status.sh --wait {svc}       # Wait for deploy
   .zcp/verify.sh {svc} {port} /...  # Test endpoints
   .zcp/workflow.sh --help           # Full reference

⚠️  Remember:
   Files: /var/www/{service}/   (SSHFS direct edit)
   Commands: ssh {service} "cmd"
EOF
}

cmd_transition_to() {
    local target_phase="$1"
    local back_flag=""

    # Handle --back flag
    if [ "$1" = "--back" ]; then
        back_flag="--back"
        shift
        target_phase="$1"
    fi

    if [ -z "$target_phase" ]; then
        echo "❌ Usage: .zcp/workflow.sh transition_to [--back] {phase}"
        echo "Phases: DISCOVER, DEVELOP, DEPLOY, VERIFY, DONE"
        echo ""
        echo "Options:"
        echo "  --back    Go backward (invalidates evidence)"
        return 1
    fi

    if ! validate_phase "$target_phase"; then
        echo "❌ Invalid phase: $target_phase"
        echo "Valid phases: ${PHASES[*]}"
        return 1
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

    # In dev-only mode, truncated flow: DISCOVER → DEVELOP → DONE
    if [ "$mode" = "dev-only" ]; then
        case "$target_phase" in
            DISCOVER|DEVELOP)
                set_phase "$target_phase"
                output_phase_guidance "$target_phase"
                return 0
                ;;
            DONE)
                if [ "$current_phase" = "DEVELOP" ]; then
                    echo "✅ Dev-only workflow complete"
                    echo ""
                    echo "💡 To deploy this work later:"
                    echo "   .zcp/workflow.sh upgrade-to-full"
                    set_phase "$target_phase"
                    return 0
                fi
                ;;
            DEPLOY|VERIFY)
                echo "❌ DEPLOY/VERIFY not available in dev-only mode"
                echo ""
                echo "💡 To enable deployment:"
                echo "   .zcp/workflow.sh upgrade-to-full"
                return 1
                ;;
        esac
    fi

    # In hotfix mode, skip discovery and dev verification
    if [ "$mode" = "hotfix" ]; then
        case "$target_phase" in
            DEPLOY)
                # Skip dev verification gate in hotfix mode
                set_phase "$target_phase"
                echo "🚨 HOTFIX: Skipping dev verification"
                output_phase_guidance "$target_phase"
                return 0
                ;;
            VERIFY|DONE)
                # Still enforce verification in hotfix mode
                ;;
        esac
    fi

    # Handle backward transitions with --back flag
    if [ "$back_flag" = "--back" ]; then
        case "$(get_phase)→$target_phase" in
            VERIFY→DEVELOP|DEPLOY→DEVELOP)
                rm -f "$STAGE_VERIFY_FILE"
                rm -f "$DEPLOY_EVIDENCE_FILE" 2>/dev/null
                echo "⚠️  Backward transition: Stage verification evidence invalidated"
                echo "   You will need to re-verify stage after redeploying"
                set_phase "$target_phase"
                output_phase_guidance "$target_phase"
                return 0
                ;;
            DONE→VERIFY)
                echo "⚠️  Backward transition: Re-verification mode"
                set_phase "$target_phase"
                output_phase_guidance "$target_phase"
                return 0
                ;;
            *)
                echo "❌ Cannot go back to $target_phase from $(get_phase)"
                echo ""
                echo "Allowed backward transitions:"
                echo "  VERIFY → DEVELOP (invalidates stage evidence)"
                echo "  DEPLOY → DEVELOP (invalidates stage evidence)"
                echo "  DONE → VERIFY"
                return 1
                ;;
        esac
    fi

    # In full mode, enforce gates
    case "$target_phase" in
        DISCOVER)
            if [ "$current_phase" != "INIT" ]; then
                echo "❌ Cannot transition to DISCOVER from $current_phase"
                echo "📋 Run: .zcp/workflow.sh init"
                return 2
            fi
            ;;
        DEVELOP)
            if [ "$current_phase" != "DISCOVER" ]; then
                echo "❌ Cannot transition to DEVELOP from $current_phase"
                echo "📋 Required flow: INIT → DISCOVER → DEVELOP"
                return 2
            fi
            if ! check_gate_discover_to_develop; then
                return 2
            fi
            ;;
        DEPLOY)
            if [ "$current_phase" != "DEVELOP" ]; then
                echo "❌ Cannot transition to DEPLOY from $current_phase"
                echo "📋 Required flow: DEVELOP → DEPLOY"
                return 2
            fi
            if ! check_gate_develop_to_deploy; then
                return 2
            fi
            ;;
        VERIFY)
            if [ "$current_phase" != "DEPLOY" ]; then
                echo "❌ Cannot transition to VERIFY from $current_phase"
                echo "📋 Required flow: DEPLOY → VERIFY"
                return 2
            fi
            if ! check_gate_deploy_to_verify; then
                return 2
            fi
            ;;
        DONE)
            if [ "$current_phase" != "VERIFY" ]; then
                echo "❌ Cannot transition to DONE from $current_phase"
                echo "📋 Required flow: VERIFY → DONE"
                return 2
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
       "$ZEROPS_ZAGENT_API_KEY"

   zcli service list -P $projectId

📋 Then record discovery:
   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

⚠️  Never use 'zcli scope' - it's buggy
⚠️  Use service IDs (from list), not hostnames

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: /tmp/discovery.json must exist
📋 Next: .zcp/workflow.sh transition_to DEVELOP
EOF
            ;;
        DEVELOP)
            cat <<'EOF'
✅ Phase: DEVELOP

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📁 Files: /var/www/{dev}/     (edit directly via SSHFS)
💻 Run:   ssh {dev} "cmd"     (execute inside container)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  CRITICAL: Dev is where you iterate and fix errors.
    Stage is for final validation AFTER dev confirms success.

    You MUST verify the feature works correctly on dev before deploying.
    If you find errors on stage, you did not test properly on dev.

┌─────────────────────────────────────────────────────────────────┐
│  DEVELOP LOOP (repeat until feature works):                     │
│                                                                 │
│  1. Build & Run                                                 │
│  2. Test functionality (not just HTTP status!)                  │
│  3. Check for errors (logs, responses, browser console)         │
│  4. If errors → Fix → Go to step 1                              │
│  5. Only when working → run verify.sh → transition to DEPLOY    │
└─────────────────────────────────────────────────────────────────┘

Kill existing process:
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'

Build & run:
  ssh {dev} "{build_command}"
  ssh {dev} './{binary} >> /tmp/app.log 2>&1'
  ↑ Set run_in_background=true in Bash tool parameters

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 FUNCTIONAL TESTING (required before deploy):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

HTTP 200 is NOT enough. You must verify the feature WORKS.

Backend APIs:
  # GET the actual response and check content
  ssh {dev} "curl -s http://localhost:{port}/api/endpoint" | jq .

  # POST and verify the operation succeeded
  ssh {dev} "curl -s -X POST http://localhost:{port}/api/items -d '{...}'"

  # Check the data persisted
  ssh {dev} "curl -s http://localhost:{port}/api/items"

Frontend/Full-stack:
  URL=$(ssh {dev} "echo \$zeropsSubdomain")
  agent-browser open "$URL"
  agent-browser errors          # ← MUST be empty
  agent-browser console         # ← Check for runtime errors
  agent-browser screenshot      # ← Visual verification

Logs (check for errors/exceptions):
  ssh {dev} "tail -50 /tmp/app.log"
  ssh {dev} "grep -i error /tmp/app.log"
  ssh {dev} "grep -i exception /tmp/app.log"

Database verification (if applicable):
  PGPASSWORD=$db_password psql -h $db_hostname -U $db_user -d $db_database \
      -c "SELECT * FROM {table} ORDER BY id DESC LIMIT 5;"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
❌ DO NOT deploy to stage if:
   • Response contains error messages
   • Logs show exceptions or stack traces
   • Browser console has JavaScript errors
   • Data isn't persisting correctly
   • UI is broken or not rendering

✅ Deploy to stage ONLY when:
   • Feature works as expected on dev
   • No errors in logs or console
   • You've tested the actual functionality, not just "server responds"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 When ready (feature works, no errors):
   .zcp/verify.sh {dev} {port} / /status /api/...
   .zcp/workflow.sh transition_to DEPLOY
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
      \"\$ZEROPS_ZAGENT_API_KEY\""

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
  .zcp/status.sh --wait {stage}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: .zcp/status.sh shows SUCCESS notification
📋 Next: .zcp/workflow.sh transition_to VERIFY
EOF
            ;;
        VERIFY)
            cat <<'EOF'
✅ Phase: VERIFY

Check deployed artifacts:
  ssh {stage} "ls -la /var/www/"

Verify endpoints:
  .zcp/verify.sh {stage} {port} / /status /api/...

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
📋 Next: .zcp/workflow.sh transition_to DONE
EOF
            ;;
        DONE)
            cat <<'EOF'
✅ Phase: DONE

Run completion check:
  .zcp/workflow.sh complete

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
    local single_mode=false

    # Handle --single flag
    if [ "$dev_id" = "--single" ]; then
        single_mode=true
        dev_id="$2"
        dev_name="$3"
        stage_id="$2"
        stage_name="$3"

        if [ -z "$dev_id" ] || [ -z "$dev_name" ]; then
            echo "❌ Usage: .zcp/workflow.sh create_discovery --single {service_id} {service_name}"
            return 1
        fi

        echo "⚠️  SINGLE-SERVICE MODE"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        echo "Using same service for dev AND stage: $dev_name"
        echo ""
        echo "RISKS YOU'RE ACCEPTING:"
        echo "  1. zcli push may overwrite source files"
        echo "  2. No isolation between development and deployment"
        echo "  3. A failed deploy affects your development environment"
        echo ""
        echo "WHEN THIS IS SAFE:"
        echo "  - Build creates separate artifact (Go binary, bundled JS)"
        echo "  - Small project where dev/stage separation is overkill"
        echo ""
        echo "Proceeding with single-service mode..."
        echo ""
    fi

    if [ -z "$dev_id" ] || [ -z "$dev_name" ] || [ -z "$stage_id" ] || [ -z "$stage_name" ]; then
        echo "❌ Usage: .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}"
        echo ""
        echo "Example:"
        echo "  .zcp/workflow.sh create_discovery 'abc123' 'appdev' 'def456' 'appstage'"
        echo ""
        echo "Or for single-service mode:"
        echo "  .zcp/workflow.sh create_discovery --single 'abc123' 'myservice'"
        return 1
    fi

    if ! command -v jq &>/dev/null; then
        echo "❌ jq required but not found"
        return 1
    fi

    local session_id
    session_id=$(get_session)
    if [ -z "$session_id" ]; then
        echo "❌ No active session. Run: .zcp/workflow.sh init"
        return 1
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
        --argjson single "$single_mode" \
        '{
            session_id: $sid,
            timestamp: $ts,
            single_mode: $single,
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
    if [ "$single_mode" = true ]; then
        echo "Mode:  SINGLE-SERVICE (dev = stage)"
    fi
    echo ""
    echo "📋 Next: .zcp/workflow.sh transition_to DISCOVER"
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
        # Indicate if evidence was invalidated by backward transition
        if [ "$(get_phase)" = "DEVELOP" ] && [ -f "$DEV_VERIFY_FILE" ]; then
            echo "    ⚠️  Stage evidence may have been invalidated (backward transition)"
        fi
    fi

    # Check deploy evidence
    if [ -f "$DEPLOY_EVIDENCE_FILE" ] 2>/dev/null; then
        if check_evidence_session "$DEPLOY_EVIDENCE_FILE"; then
            echo "  ✓ /tmp/deploy_evidence.json (current session)"
        else
            echo "  ✗ /tmp/deploy_evidence.json (stale session)"
        fi
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "💡 NEXT STEPS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Give specific guidance based on phase and what's missing
    case "$phase" in
        INIT)
            if ! check_evidence_session "$DISCOVERY_FILE"; then
                cat <<'GUIDANCE'
1. Discover services:
   zcli login --region=gomibako \
       --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
       "$ZEROPS_ZAGENT_API_KEY"
   zcli service list -P $projectId

2. Record discovery (use IDs from step 1):
   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

3. Transition:
   .zcp/workflow.sh transition_to DISCOVER
GUIDANCE
            else
                echo "Discovery exists. Run: .zcp/workflow.sh transition_to DISCOVER"
            fi
            ;;
        DISCOVER)
            if check_evidence_session "$DISCOVERY_FILE"; then
                echo "Discovery complete. Run: .zcp/workflow.sh transition_to DEVELOP"
            else
                cat <<'GUIDANCE'
Discovery missing or stale. Re-run:
   zcli service list -P $projectId
   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
GUIDANCE
            fi
            ;;
        DEVELOP)
            if ! check_evidence_session "$DEV_VERIFY_FILE"; then
                local dev_name
                dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
                cat <<GUIDANCE
1. Build and test on dev ($dev_name)
2. Verify endpoints work:
   .zcp/verify.sh $dev_name {port} / /api/...
3. Then: .zcp/workflow.sh transition_to DEPLOY
GUIDANCE
            else
                echo "Dev verified. Run: .zcp/workflow.sh transition_to DEPLOY"
            fi
            ;;
        DEPLOY)
            if ! check_evidence_session "$DEPLOY_EVIDENCE_FILE" 2>/dev/null; then
                local dev_name stage_id stage_name
                dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
                stage_id=$(jq -r '.stage.id // "STAGE_ID"' "$DISCOVERY_FILE" 2>/dev/null)
                stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)
                cat <<GUIDANCE
1. Check deployFiles in zerops.yaml includes all artifacts
2. Deploy:
   ssh $dev_name "zcli login ... && zcli push $stage_id --setup={setup}"
3. Wait:
   .zcp/status.sh --wait $stage_name
4. Then: .zcp/workflow.sh transition_to VERIFY
GUIDANCE
            else
                echo "Deploy complete. Run: .zcp/workflow.sh transition_to VERIFY"
            fi
            ;;
        VERIFY)
            if ! check_evidence_session "$STAGE_VERIFY_FILE"; then
                local stage_name
                stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)
                cat <<GUIDANCE
1. Verify stage endpoints:
   .zcp/verify.sh $stage_name {port} / /api/...
2. Then: .zcp/workflow.sh transition_to DONE
GUIDANCE
            else
                echo "Stage verified. Run: .zcp/workflow.sh transition_to DONE"
            fi
            ;;
        DONE)
            echo "Run: .zcp/workflow.sh complete"
            ;;
        QUICK)
            echo "Quick mode - no enforcement. Use any tools as needed."
            ;;
        *)
            echo "Unknown phase. Run: .zcp/workflow.sh init"
            ;;
    esac
}

cmd_complete() {
    local session_id
    session_id=$(get_session)

    if [ -z "$session_id" ]; then
        echo "❌ No active session"
        return 1
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
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "📋 Next task? Run workflow.sh again to decide:"
        echo "   .zcp/workflow.sh init    → deploying"
        echo "   .zcp/workflow.sh --quick → exploring"
        return 0
    else
        echo "❌ Evidence validation failed:"
        echo ""
        echo "   • Session: $session_id"
        printf '%s\n' "${messages[@]}"
        echo ""
        echo "💡 Fix the issues above and run: .zcp/workflow.sh complete"
        return 3
    fi
}

cmd_reset() {
    local keep_discovery=false
    if [ "$1" = "--keep-discovery" ]; then
        keep_discovery=true
    fi

    # Always clear session state and verification evidence
    rm -f "$SESSION_FILE" "$MODE_FILE" "$PHASE_FILE"
    rm -f "$DEV_VERIFY_FILE" "$STAGE_VERIFY_FILE" "$DEPLOY_EVIDENCE_FILE"

    if [ "$keep_discovery" = true ]; then
        if [ -f "$DISCOVERY_FILE" ]; then
            echo "✓ Discovery preserved"
            echo "  Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")"
            echo "  Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")"
            echo ""
            echo "💡 Next: .zcp/workflow.sh init"
            echo "   Discovery will be reused with new session"
        else
            echo "⚠️  No discovery to preserve"
            rm -f "$DISCOVERY_FILE"
            echo ""
            echo "💡 Start fresh: .zcp/workflow.sh init"
        fi
    else
        rm -f "$DISCOVERY_FILE"
        echo "✅ All workflow state cleared"
        echo ""
        echo "💡 Start fresh: .zcp/workflow.sh init"
    fi
}

cmd_extend() {
    local import_file="$1"

    if [ -z "$import_file" ] || [ "$import_file" = "--help" ]; then
        show_topic_help "extend"
        return 0
    fi

    if [ ! -f "$import_file" ]; then
        echo "❌ File not found: $import_file"
        return 1
    fi

    # Validate YAML if yq is available
    if command -v yq &>/dev/null; then
        if ! yq '.' "$import_file" > /dev/null 2>&1; then
            echo "❌ Invalid YAML: $import_file"
            return 1
        fi
    fi

    local pid
    pid=$(cat /tmp/projectId 2>/dev/null || echo "$projectId")
    if [ -z "$pid" ]; then
        echo "❌ No projectId found"
        return 1
    fi

    echo "📦 Importing services from: $import_file"
    echo ""

    if ! zcli project import "$import_file" -P "$pid"; then
        echo "❌ Import failed"
        return 1
    fi

    echo ""
    echo "⏳ Waiting for new services to be ready..."

    local attempts=0
    local timeout_seconds=600
    local interval=10
    local max_attempts=$((timeout_seconds / interval))
    while zcli service list -P "$pid" 2>/dev/null | grep -qE "PENDING|BUILDING"; do
        ((attempts++))
        if [ $attempts -ge $max_attempts ]; then
            echo "⚠️  Timeout waiting for services (${timeout_seconds}s)"
            echo "   Check: zcli service list -P $pid"
            break
        fi
        echo "   Still building... (${attempts}/${max_attempts})"
        sleep $interval
    done

    echo ""
    echo "✅ Services ready"
    echo ""
    echo "⚠️  IMPORTANT: Environment Variable Timing"
    echo "   New services' vars are NOT visible in ZCP until restart."
    echo ""
    echo "   To access new credentials:"
    echo "   Option A: Restart ZCP (reconnect your IDE)"
    echo "   Option B: ssh {service} 'echo \$password'"
    echo ""
    echo "💡 See: .zcp/workflow.sh --help extend"
}

cmd_upgrade_to_full() {
    local current_mode
    current_mode=$(get_mode)

    if [ "$current_mode" = "full" ]; then
        echo "✅ Already in full mode"
        return 0
    fi

    if [ "$current_mode" != "dev-only" ]; then
        echo "❌ Can only upgrade from dev-only mode"
        echo "   Current mode: $current_mode"
        return 1
    fi

    echo "full" > "$MODE_FILE"

    local phase
    phase=$(get_phase)

    echo "✅ Upgraded to full mode"
    echo ""
    echo "📋 New workflow: DEVELOP → DEPLOY → VERIFY → DONE"
    echo ""

    if [ "$phase" = "DONE" ]; then
        # Revert to DEVELOP so they can go through full flow
        echo "DEVELOP" > "$PHASE_FILE"
        echo "💡 Reset to DEVELOP phase. Now:"
        echo "   1. .zcp/verify.sh {dev} {port} / /status /api/..."
        echo "   2. .zcp/workflow.sh transition_to DEPLOY"
    else
        echo "💡 Continue from current phase: $phase"
        echo "   Next: .zcp/workflow.sh transition_to DEPLOY"
    fi
}

cmd_refresh_discovery() {
    if [ ! -f "$DISCOVERY_FILE" ]; then
        echo "❌ No existing discovery to refresh"
        echo "   Run create_discovery first"
        return 1
    fi

    local old_dev old_stage session_id
    old_dev=$(jq -r '.dev.name' "$DISCOVERY_FILE")
    old_stage=$(jq -r '.stage.name' "$DISCOVERY_FILE")
    session_id=$(get_session)

    echo "Current discovery:"
    echo "  Dev:   $old_dev"
    echo "  Stage: $old_stage"
    echo ""

    local pid
    pid=$(cat /tmp/projectId 2>/dev/null || echo "$projectId")
    if [ -z "$pid" ]; then
        echo "❌ No projectId found"
        return 1
    fi

    echo "Available services:"
    zcli service list -P "$pid" 2>/dev/null | grep -v "^Using config" | head -15
    echo ""

    # Check if services still exist
    local services
    services=$(zcli service list -P "$pid" 2>/dev/null)
    local dev_exists=false
    local stage_exists=false

    if echo "$services" | grep -q "$old_dev"; then
        dev_exists=true
    fi
    if echo "$services" | grep -q "$old_stage"; then
        stage_exists=true
    fi

    if $dev_exists && $stage_exists; then
        echo "✓ Existing dev/stage pair still valid"
        echo "  No changes needed"
    else
        echo "⚠️  Discovery may be stale:"
        $dev_exists || echo "  - Dev '$old_dev' not found"
        $stage_exists || echo "  - Stage '$old_stage' not found"
        echo ""
        echo "Run create_discovery with updated service IDs"
    fi
}

cmd_record_deployment() {
    local service="$1"

    if [ -z "$service" ]; then
        echo "❌ Usage: .zcp/workflow.sh record_deployment {service_name}"
        return 1
    fi

    local session_id
    session_id=$(get_session)
    if [ -z "$session_id" ]; then
        echo "❌ No active session. Run: .zcp/workflow.sh init"
        return 1
    fi

    jq -n \
        --arg sid "$session_id" \
        --arg svc "$service" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        '{
            session_id: $sid,
            service: $svc,
            timestamp: $ts,
            status: "MANUAL"
        }' > "$DEPLOY_EVIDENCE_FILE"

    echo "✅ Deployment evidence recorded for $service"
    echo ""
    echo "💡 Next: .zcp/workflow.sh transition_to VERIFY"
}

# ============================================================================
# GATE CHECKS
# ============================================================================

check_gate_discover_to_develop() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: DISCOVER → DEVELOP"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: discovery.json exists
    ((checks_total++))
    if [ -f "$DISCOVERY_FILE" ]; then
        echo "  ✓ discovery.json exists"
        ((checks_passed++))
    else
        echo "  ✗ discovery.json missing"
        echo "    → Run: .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}"
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$DISCOVERY_FILE"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        local current_session=$(get_session)
        local disco_session=$(jq -r '.session_id // "none"' "$DISCOVERY_FILE" 2>/dev/null)
        echo "  ✗ session_id mismatch"
        echo "    → Current session: $current_session"
        echo "    → Discovery session: $disco_session"
        echo "    → Run create_discovery again or .zcp/workflow.sh reset"
        all_passed=false
    fi

    # Check 3: dev != stage (unless single_mode)
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$DISCOVERY_FILE" ]; then
        local dev_name stage_name single_mode
        dev_name=$(jq -r '.dev.name' "$DISCOVERY_FILE" 2>/dev/null)
        stage_name=$(jq -r '.stage.name' "$DISCOVERY_FILE" 2>/dev/null)
        single_mode=$(jq -r '.single_mode // false' "$DISCOVERY_FILE" 2>/dev/null)

        if [ "$dev_name" != "$stage_name" ]; then
            echo "  ✓ dev ≠ stage ($dev_name vs $stage_name)"
            ((checks_passed++))
        elif [ "$single_mode" = "true" ]; then
            echo "  ⚠ single-service mode (dev = stage = $dev_name)"
            echo "    → Intentional: source corruption risk acknowledged"
            ((checks_passed++))
        else
            echo "  ✗ dev.name == stage.name ('$dev_name')"
            echo "    → Cannot use same service for dev and stage"
            echo "    → Source corruption risk: zcli push overwrites /var/www/"
            echo "    → Use --single flag if you understand the risk"
            all_passed=false
        fi
    else
        echo "  ⚠ Cannot verify dev≠stage (jq unavailable or no discovery)"
        ((checks_passed++))
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        check_evidence_freshness "$DISCOVERY_FILE" 24
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}

check_gate_develop_to_deploy() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: DEVELOP → DEPLOY"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: dev_verify.json exists
    ((checks_total++))
    if [ -f "$DEV_VERIFY_FILE" ]; then
        echo "  ✓ dev_verify.json exists"
        ((checks_passed++))
    else
        echo "  ✗ dev_verify.json missing"
        echo "    → Run: .zcp/verify.sh {dev} {port} / /status /api/..."
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$DEV_VERIFY_FILE"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        echo "  ✗ session_id mismatch"
        echo "    → Evidence is from a different session"
        echo "    → Re-run verify.sh for current session"
        all_passed=false
    fi

    # Check 3: failures == 0
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$DEV_VERIFY_FILE" ]; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        # Validate numeric before comparison
        if ! [[ "$failures" =~ ^[0-9]+$ ]]; then
            echo "  ✗ Cannot read failure count from evidence file"
            all_passed=false
        elif [ "$failures" -eq 0 ]; then
            local passed
            passed=$(jq -r '.passed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
            echo "  ✓ verification passed ($passed endpoints, 0 failures)"
            ((checks_passed++))
        else
            echo "  ✗ verification has $failures failure(s)"
            echo "    → Fix failing endpoints before deploying"
            echo "    → Check: jq '.results[] | select(.pass==false)' /tmp/dev_verify.json"
            all_passed=false
        fi
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        check_evidence_freshness "$DEV_VERIFY_FILE" 24
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}

check_gate_deploy_to_verify() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: DEPLOY → VERIFY"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: deploy_evidence.json exists
    ((checks_total++))
    if [ -f "$DEPLOY_EVIDENCE_FILE" ]; then
        echo "  ✓ deploy_evidence.json exists"
        ((checks_passed++))
    else
        echo "  ✗ deploy_evidence.json missing"
        echo "    → Run: .zcp/status.sh --wait {stage}"
        echo "    → Or:  .zcp/workflow.sh record_deployment {stage}"
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$DEPLOY_EVIDENCE_FILE"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        echo "  ✗ session_id mismatch"
        echo "    → Deployment evidence is from a different session"
        echo "    → Re-deploy and wait: .zcp/status.sh --wait {stage}"
        all_passed=false
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        check_evidence_freshness "$DEPLOY_EVIDENCE_FILE" 24
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}

check_gate_verify_to_done() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: VERIFY → DONE"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: stage_verify.json exists
    ((checks_total++))
    if [ -f "$STAGE_VERIFY_FILE" ]; then
        echo "  ✓ stage_verify.json exists"
        ((checks_passed++))
    else
        echo "  ✗ stage_verify.json missing"
        echo "    → Run: .zcp/verify.sh {stage} {port} / /status /api/..."
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        echo "  ✗ session_id mismatch"
        echo "    → Evidence is from a different session"
        echo "    → Re-run verify.sh for current session"
        all_passed=false
    fi

    # Check 3: failures == 0
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$STAGE_VERIFY_FILE" ]; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        # Validate numeric before comparison
        if ! [[ "$failures" =~ ^[0-9]+$ ]]; then
            echo "  ✗ Cannot read failure count from evidence file"
            all_passed=false
        elif [ "$failures" -eq 0 ]; then
            local passed
            passed=$(jq -r '.passed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
            echo "  ✓ verification passed ($passed endpoints, 0 failures)"
            ((checks_passed++))
        else
            echo "  ✗ verification has $failures failure(s)"
            echo "    → Fix failing endpoints"
            echo "    → Use: .zcp/workflow.sh transition_to --back DEVELOP"
            all_passed=false
        fi
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        check_evidence_freshness "$STAGE_VERIFY_FILE" 24
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    local command="$1"
    shift

    case "$command" in
        init)
            cmd_init "$@"
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
            cmd_reset "$@"
            ;;
        record_deployment)
            cmd_record_deployment "$@"
            ;;
        extend)
            cmd_extend "$@"
            ;;
        refresh_discovery)
            cmd_refresh_discovery "$@"
            ;;
        upgrade-to-full)
            cmd_upgrade_to_full
            ;;
        "")
            cat <<'EOF'
╔══════════════════════════════════════════════════════════════════╗
║  ZEROPS WORKFLOW                                                 ║
╚══════════════════════════════════════════════════════════════════╝

Will this work be deployed (now or later)?

┌─────────────────────────────────────────────────────────────────┐
│  YES  →  .zcp/workflow.sh init                                       │
│          Features, bug fixes to ship, config changes,           │
│          schema changes, new files/directories                  │
│                                                                 │
│          Enforced phases with gates that catch mistakes         │
│          You can stop at any phase and resume later             │
├─────────────────────────────────────────────────────────────────┤
│  NO   →  .zcp/workflow.sh --quick                                    │
│          Investigating, exploring code, reading logs,           │
│          database queries, dev-only testing                     │
│                                                                 │
│          Full access, no enforcement, all tools available       │
├─────────────────────────────────────────────────────────────────┤
│  UNCERTAIN?  →  .zcp/workflow.sh init                                │
│          Default to safety. You can always reset if overkill.   │
└─────────────────────────────────────────────────────────────────┘

Commands:
  .zcp/workflow.sh init          Start enforced workflow
  .zcp/workflow.sh --quick       Quick mode (no enforcement)
  .zcp/workflow.sh --help        Full platform reference
  .zcp/workflow.sh show          Current workflow status
EOF
            ;;
        *)
            echo "❌ Unknown command: $command"
            echo ""
            echo "Run: .zcp/workflow.sh --help"
            exit 1
            ;;
    esac
}

main "$@"
