#!/bin/bash
# Topic-specific help for Zerops Workflow

# Source full help for extraction functions
# Use local variable to avoid overwriting parent SCRIPT_DIR
_HELP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$_HELP_DIR/full.sh"

show_topic_help() {
    local topic="$1"

    case "$topic" in
        discover)
            show_help_discover
            ;;
        develop)
            show_help_develop
            ;;
        deploy)
            show_help_deploy
            ;;
        verify)
            show_help_verify
            ;;
        done)
            show_help_done
            ;;
        vars)
            show_help_vars
            ;;
        services)
            show_help_services
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
            show_help_extend
            ;;
        bootstrap)
            show_help_bootstrap
            ;;
        cheatsheet)
            show_help_cheatsheet
            ;;
        *)
            echo "❌ Unknown help topic: $topic"
            echo ""
            echo "Available topics:"
            echo "  discover, develop, deploy, verify, done"
            echo "  vars, services, trouble, example, gates"
            echo "  extend, bootstrap, cheatsheet"
            return 1
            ;;
    esac
}

show_help_discover() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 DISCOVER PHASE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Purpose: Authenticate to Zerops and discover service IDs

Commands:
  zcli login --region=gomibako \
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
      "$ZEROPS_ZCP_API_KEY"

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
}

show_help_develop() {
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

Database - Verify persistence (run from ZCP, NOT via ssh!):
  psql "$db_connectionString" -c "SELECT * FROM {table} ORDER BY id DESC LIMIT 5;"
  # ⚠️  Don't use: ssh {dev} "psql ..." — runtime containers don't have psql!

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
}

show_help_deploy() {
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

1. Stop dev process (triple-kill pattern):
   ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; \
              fuser -k {port}/tcp 2>/dev/null; true'

2. Authenticate from dev container:
   ssh {dev} "zcli login --region=gomibako \
       --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
       \"\$ZEROPS_ZCP_API_KEY\""

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
}

show_help_verify() {
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
  psql "$db_connectionString" -c "SELECT * FROM users LIMIT 5;"

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
}

show_help_done() {
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


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
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
}

show_help_vars() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔐 ENVIRONMENT VARIABLES - COMPREHENSIVE REFERENCE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  CRITICAL: Service names are ARBITRARY (user-defined hostnames)
   Variables use HOSTNAME, not service type!
   Must discover actual names via: zcli service list -P $projectId

Variable Structure:
  Pattern: {hostname}_{VARIABLE}
  Example: ${myapp_zeropsSubdomain}, ${usersdb_password}

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
│ Usage from ZCP (prefer connection strings):                      │
│   psql "${postgres_connectionString}"                           │
│   redis-cli -u "${cache_connectionString}"                      │
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
  {hostname}_ZEROPS_ZCP_API_KEY    # zcli authentication key
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
}

show_help_services() {
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
  • Can be ANYTHING: myapp, backend, usersdb, cache1, apiprod
  • ⚠️  Hostnames: lowercase alphanumeric only (a-z, 0-9), no hyphens/underscores
  • Used for: SSH, variables, networking
  • Examples: "myapp", "postgresmain", "rediscache"

Service Type (Zerops-Defined):
  • The technology: postgresql, nats, valkey, nodejs, go
  • Cannot be changed
  • Internal to Zerops
  • Examples: "postgresql@17", "valkey@7.2", "go@1.22"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  WHY THIS MATTERS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Variables use HOSTNAME, not type!

If PostgreSQL service is named "usersdb":
  ${usersdb_connectionString}    ✅ CORRECT
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
    - hostname: usersdb          # Type: postgresql
    - hostname: sessioncache     # Type: valkey
    - hostname: eventqueue       # Type: nats

  Variables: ${usersdb_password}, ${sessioncache_port}

Pattern 3: Environment Suffixes
  services:
    - hostname: apidev         # Type: nodejs (development)
    - hostname: apistage       # Type: nodejs (staging)
    - hostname: apiprod        # Type: nodejs (production)

  Variables: ${apidev_zeropsSubdomain}, ${apiprod_zeropsSubdomain}

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
  │ def456... │ backendapi     │ ACTIVE │ go          │
  │ ghi789... │ usersdb        │ ACTIVE │ postgresql  │
  │ jkl012... │ cache          │ ACTIVE │ valkey      │
  └───────────┴────────────────┴────────┴─────────────┘

Then use DISCOVERED hostnames:
  ssh myapp "..."
  echo "${backendapi_zeropsSubdomain}"
  psql "${usersdb_connectionString}"

⚠️  NEVER assume service names match types!
⚠️  NEVER hardcode service names in scripts!
⚠️  ALWAYS discover first with zcli service list!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🎯 THIS IS WHY DISCOVER PHASE IS MANDATORY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

You cannot proceed without knowing actual service hostnames!

.zcp/workflow.sh create_discovery uses DISCOVERED names:
  .zcp/workflow.sh create_discovery \
    "abc123" "myapp" \       ← Actual hostname from zcli service list
    "def456" "backendapi"    ← NOT type name, actual hostname

This ensures all subsequent operations use correct names.

See also: .zcp/workflow.sh --help vars (for variable access patterns)
EOF
}

show_help_extend() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔧 EXTENDING YOUR PROJECT WITH NEW SERVICES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Common scenario: You have a working app and want to add PostgreSQL,
Valkey (Redis), NATS, or another managed service.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📚 LIVE DOCUMENTATION (fetch for latest info)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Import YAML reference (all fields, options):
  https://docs.zerops.io/references/import.md

Service type list (all available types):
  https://docs.zerops.io/references/import-yaml/type-list

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📦 AVAILABLE SERVICE TYPES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Databases:
  postgresql@14, postgresql@16, postgresql@17
  mariadb@10.6, keydb@6, valkey@7.2
  qdrant@1.10, qdrant@1.12

Search Engines:
  elasticsearch@8.16, meilisearch@1.10, typesense@27.1

Message Brokers:
  kafka@3.8, nats@2.10

Storage:
  object-storage, shared-storage

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

   Common import fields:
     hostname          # Service name (max 25 chars, lowercase alphanumeric)
     type              # Service type (see list above)
     mode              # HA or NON_HA (default: NON_HA)
     priority          # Creation order (higher = created first)
     envSecrets        # Secret environment variables
     objectStorageSize # For object-storage type (in GB)
     objectStoragePolicy # public-read or private

   Vertical autoscaling:
     minCpu, maxCpu, minRam, maxRam, minDisk, maxDisk

   Horizontal autoscaling:
     minContainers, maxContainers (1-10)

2. IMPORT THE SERVICE
   ─────────────────────────────────────────────────────────────
   zcli project service-import ./add-service.yml -P \$projectId

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
   ⚠️  ZCP captured env vars at START. New service vars not visible!

   Option A: Restart ZCP (recommended - picks up all new env vars)
     - Close your IDE session
     - Reconnect to ZCP
     - New vars available: $db_hostname, $db_connectionString, etc.

   Option B: Use connection string (no restart needed)
     # Database services provide a ready-to-use connection string
     # Access it via the service's environment
     ssh db 'echo $connectionString'  # Shows full connection URL

   ⚠️  NEVER echo passwords to terminal. Use connection strings instead.

5. UPDATE YOUR CODE
   ─────────────────────────────────────────────────────────────
   ⚠️  CRITICAL: Your app container ALREADY HAS these variables!
      Zerops injects them at container start. Don't pass them manually.

   Go + PostgreSQL (use connection string):
     connStr := os.Getenv("db_connectionString")

   Node + PostgreSQL (use connection string):
     const connectionString = process.env.db_connectionString;
     const pool = new Pool({ connectionString });

6. TEST CONNECTION (from ZCP directly - NOT via ssh!)
   ─────────────────────────────────────────────────────────────
   # Use connection string - secure, no password exposure
   psql "$db_connectionString" -c "SELECT 1"

   # Valkey/Redis
   redis-cli -u "$cache_connectionString" PING

   # ⚠️  Runtime containers don't have DB tools - run from ZCP!

7. RUN YOUR APP
   ─────────────────────────────────────────────────────────────
   # ✅ CORRECT - container already has all variables
   ssh appdev './app'

   # ❌ WRONG - don't do this (unnecessary, exposes secrets)
   # ssh appdev "DB_HOST=... DB_PASS=... ./app"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 CONNECTION PATTERNS BY SERVICE TYPE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

PostgreSQL (hostname: db)
  ${db_hostname}, ${db_port}, ${db_user}, ${db_password}, ${db_dbName}
  ${db_connectionString}  ← Full connection string (preferred)

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
zcli project service-import ./add-postgres.yml -P \$projectId

# 3. Wait for ready
while ! zcli service list -P $projectId | grep -q "db.*RUNNING"; do
  echo "Waiting for db service..."
  sleep 10
done
echo "Database ready!"

# 4. Test connection from ZCP (NOT via ssh!)
psql "$db_connectionString" -c "SELECT 1"

# 5. Your app reads env vars automatically
# Zerops injects db_hostname, db_password, etc. into container env
# Just run: ssh appdev './app'
# Don't pass DB vars manually - they're already there!
EOF
}

show_help_cheatsheet() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 CHEATSHEET — Quick Reference
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WORKFLOW COMMANDS
─────────────────────────────────────────────────────────────────
.zcp/workflow.sh init              # Start full workflow
.zcp/workflow.sh init --dev-only   # Dev only (no deploy)
.zcp/workflow.sh init --hotfix     # Skip dev verification
.zcp/workflow.sh --quick           # Quick mode (no gates)
.zcp/workflow.sh show              # Current status
.zcp/workflow.sh show --guidance   # Status + phase guidance
.zcp/workflow.sh recover           # Full context recovery
.zcp/workflow.sh state             # One-line state
.zcp/workflow.sh transition_to {PHASE}
.zcp/workflow.sh complete          # Finish workflow

ZCLI LOGIN
─────────────────────────────────────────────────────────────────
zcli login --region=gomibako \
    --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
    "$ZEROPS_ZCP_API_KEY"

zcli service list -P $projectId    # List services (need -P!)

TRIPLE-KILL PATTERN
─────────────────────────────────────────────────────────────────
ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'

BUILD & RUN
─────────────────────────────────────────────────────────────────
ssh {dev} "{build_command}"
ssh {dev} './{binary} >> /tmp/app.log 2>&1'
# ↑ Set run_in_background=true in Bash tool!

DEPLOY
─────────────────────────────────────────────────────────────────
# From dev container, NOT ZCP!
ssh {dev} "zcli login ... && zcli push {stage_id} --setup={setup}"

# Wait for completion
.zcp/status.sh --wait {stage}

VERIFY
─────────────────────────────────────────────────────────────────
.zcp/verify.sh {service} {port} / /status /api/...

BROWSER TESTING
─────────────────────────────────────────────────────────────────
URL=$(ssh {dev} "echo \$zeropsSubdomain")   # Don't prepend https://!
agent-browser open "$URL"
agent-browser errors                         # Must be empty
agent-browser console
agent-browser screenshot

DATABASE ACCESS (from ZCP directly — NOT via ssh!)
─────────────────────────────────────────────────────────────────
psql "$db_connectionString"                  # PostgreSQL
redis-cli -u "$cache_connectionString"       # Valkey/Redis
# ⚠️  Runtime containers don't have DB tools — run these from ZCP!

VARIABLES
─────────────────────────────────────────────────────────────────
$projectId                      # On ZCP
${hostname}_VAR                 # ZCP → service var
ssh svc 'echo $VAR'             # Inside service

CRITICAL RULES
─────────────────────────────────────────────────────────────────
• HTTP 200 ≠ working — check content, logs, console
• Deploy from dev container, NOT ZCP
• deployFiles must include ALL artifacts
• zeropsSubdomain is full URL — don't prepend https://
• Long commands: run_in_background=true
• DB tools (psql, redis-cli): Run from ZCP, NOT via ssh to runtime
• Runtime containers are minimal — no dev tools installed

EVIDENCE FILES
─────────────────────────────────────────────────────────────────
/tmp/claude_session             # Session ID
/tmp/claude_phase               # Current phase
/tmp/discovery.json             # Service mapping
/tmp/dev_verify.json            # Dev results
/tmp/stage_verify.json          # Stage results
/tmp/deploy_evidence.json       # Deploy proof
EOF
}

show_help_bootstrap() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🚀 BOOTSTRAPPING A NEW PROJECT FROM SCRATCH
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

You're starting with an empty project. Here's how to go from
zero to deployed application.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📚 ZEROPS RECIPE SYSTEM (RECOMMENDED)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Zerops provides ready-made recipes with dev/stage pairs for AI Agent
workflows. FETCH these URLs to get complete, working configurations:

FIND A RECIPE:
  Browse all:     https://stage-vega.zerops.dev/recipes.md
  By language:    https://stage-vega.zerops.dev/recipes.md?lf={lang}
  By category:    https://stage-vega.zerops.dev/recipes.md?category={cat}

  Language filters (?lf=):
    golang, nodejs, php, python, rust, java, dotnet, bun, deno
    laravel, django, phoenix, symfony, nestjs, nextjs, nuxt, react

  Category filters (?category=):
    hello-world-examples    ← Simple starters
    starter-kit             ← Full stack templates
    cms, crm, e-commerce, ai-ml, observability

GET AI AGENT VARIANT (has dev/stage pairs):
  https://stage-vega.zerops.dev/recipes/{name}.md?environment=ai-agent

  Examples:
    recipes/go-hello-world.md?environment=ai-agent
    recipes/laravel-jetstream.md?environment=ai-agent
    recipes/nestjs-hello-world.md?environment=ai-agent

The AI Agent environment includes:
  • appdev service (development, Ubuntu, tools pre-installed)
  • appstage service (production build)
  • Database/cache services if needed
  • Proper zerops.yaml with dev and prod setups

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 THE FLOW
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

FIND RECIPE → FETCH CONFIG → IMPORT → DEVELOP → DEPLOY → VERIFY → DONE

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📝 STEP 1: FIND AND FETCH RECIPE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Example: Bootstrap a Go app with PostgreSQL

1. Fetch the recipe list for Go:
   URL: https://stage-vega.zerops.dev/recipes.md?lf=golang

2. Fetch the AI Agent variant of go-hello-world:
   URL: https://stage-vega.zerops.dev/recipes/go-hello-world.md?environment=ai-agent

3. Extract the import YAML from the recipe (example):

   project:
     name: go-hello-world-agent

   services:
     - hostname: appstage
       type: go@1
       zeropsSetup: prod
       buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app
       enableSubdomainAccess: true

     - hostname: appdev
       type: go@1
       zeropsSetup: dev
       buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app
       enableSubdomainAccess: true
       verticalAutoscaling:
         minRam: 0.5

     - hostname: db
       type: postgresql@17
       mode: NON_HA

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📦 STEP 2: CREATE (Import services)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Save the import YAML and run:

  zcli project service-import ./import.yml -P \$projectId

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
📦 AVAILABLE RUNTIME TYPES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Runtimes:
  go@1, nodejs@18, nodejs@20, nodejs@22
  php-nginx@8.1, php-nginx@8.2, php-nginx@8.3, php-nginx@8.4
  php-apache@8.1, php-apache@8.2, php-apache@8.3, php-apache@8.4
  python@3.11, python@3.12
  bun, deno, dotnet@6, dotnet@7, dotnet@8, dotnet@9
  elixir, gleam, java@17, java@21, rust

Static:
  nginx@1.22, static@1.0

Containers:
  alpine@3.17, alpine@3.18, alpine@3.19, alpine@3.20
  ubuntu@22.04, ubuntu@24.04

Full list: https://docs.zerops.io/references/import-yaml/type-list

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📚 ADDITIONAL REFERENCES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Import YAML reference (all fields):
  https://docs.zerops.io/references/import.md

zerops.yaml specification (build/deploy config):
  https://docs.zerops.io/zerops-yaml/specification.md

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📖 MANUAL EXAMPLE: Go Hello World (no recipe)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

If you prefer to create manually without fetching a recipe:

# 1. Create import.yml
cat > import.yml <<'YAML'
services:
  - hostname: appdev
    type: go@1
    enableSubdomainAccess: true
  - hostname: appstage
    type: go@1
    enableSubdomainAccess: true
YAML

# 2. Import
zcli project service-import ./import.yml -P \$projectId

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
      base: go@1
      buildCommands:
        - go build -o app main.go
      deployFiles:
        - ./app
    run:
      base: go@1
      ports:
        - port: 8080
      start: ./app
YAML

# 7. Build, run, verify
ssh appdev "go build -o app main.go"
ssh appdev "./app >> /tmp/app.log 2>&1"  # run_in_background=true
.zcp/verify.sh appdev 8080 / /status

# Continue with normal DEPLOY → VERIFY → DONE flow
EOF
}
