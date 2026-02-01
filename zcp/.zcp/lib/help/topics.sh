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
            show_full_help | sed -n '/🔧 TROUBLESHOOTING/,/📖 COMPLETE EXAMPLE/p' | sed '$d'
            ;;
        example)
            show_full_help | sed -n '/📖 COMPLETE EXAMPLE/,/🚪 GATES/p' | sed '$d'
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
        import-validation|validate-import)
            show_help_import_validation
            ;;
        *)
            echo "❌ Unknown help topic: $topic"
            echo ""
            echo "Available topics:"
            echo "  discover, develop, deploy, verify, done"
            echo "  vars, services, trouble, example, gates"
            echo "  extend, bootstrap, cheatsheet, import-validation"
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

Scale up for heavy builds (local compilation on dev):
  ssh {dev} 'zsc scale ram 1GiB 10m'   # Temporary boost for 10 min
  ssh {dev} 'zsc scale ram auto'       # Reset to normal

  ⚠️  Only needed for LOCAL builds on dev container.
      zcli push uses separate Zerops build containers (no scaling needed).

Build & run:
  ssh {dev} "{build_command}"              # Runs synchronously (see logs)
  ssh {dev} './{binary} >> /tmp/app.log 2>&1'

  ⚠️  run_in_background=true for server (blocks forever)
      NOT for builds/push — run those synchronously to see logs!

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

QUICKEST WAY - Use the deploy helper:

  .zcp/deploy.sh stage              # Deploy ALL services
  .zcp/deploy.sh stage appdev       # Deploy specific service
  .zcp/deploy.sh --commands stage   # Show commands without executing

This handles auth, service IDs, and correct flags automatically.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  PRE-DEPLOYMENT CHECKLIST - DO THIS FIRST:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

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
        base: go@1            # Versions from plan.json
        buildCommands:
          - go build -o app main.go
        deployFiles:          # ← CRITICAL SECTION
          - ./app
          - ./templates       # Don't forget if you created these!
          - ./static
          - ./config
      run:
        base: go@1            # Or alpine@3.21 for smaller prod image
        ports:
          - port: 8080
        start: ./app

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  ZCLI PUSH - RECOMMENDED APPROACH
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

PREFERRED (git-based deploy):

  ssh {dev} "cd /var/www && zcli push {service_id} --setup={setup} --deploy-git-folder"

--setup={setup}        REQUIRED when zerops.yml has multiple setups (dev/prod)
--deploy-git-folder    Uses git for versioning and .gitignore patterns

This is the STANDARD approach. Directories should be git-initialized.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ALTERNATIVE (only if git not available):

  zcli push {service_id} --setup={setup} --noGit

--noGit    Skips git checks (use only if directory is NOT a git repo)

Most projects are git-initialized. If you see "folder is not initialized
via git init", either:
  A. Initialize git: git init && git add . && git commit -m "init"
  B. Use --noGit (not recommended, loses version tracking)

⚠️  Don't pipe zcli output! Causes "allowed only in interactive terminal"
    ❌ zcli service list | grep foo
    ✅ zcli service list

Deployment steps:

1. Stop dev process (triple-kill pattern):
   ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; \
              fuser -k {port}/tcp 2>/dev/null; true'

2. Authenticate from dev container:
   ssh {dev} "zcli login --region=gomibako \
       --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
       \"\$ZEROPS_ZCP_API_KEY\""

3. Deploy to stage:
   ssh {dev} "zcli push {stage_service_id} --setup={setup} --noGit --versionName=v1.0.0"

   • {stage_service_id} = ID from discovery (not hostname!)
   • {setup} = setup name from zerops.yaml (REQUIRED for multi-setup)
   • --noGit = skip git checks (or init git first)
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
  • Get valid versions: .zcp/recipe-search.sh quick {runtime}

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
⚠️  PREREQUISITES (REQUIRED)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Start a session:
   .zcp/workflow.sh init

2. Review recipes for valid patterns:
   .zcp/recipe-search.sh quick {runtime} [managed-service]
   Example: .zcp/recipe-search.sh quick go postgresql

   This provides:
   • Correct version strings (e.g. runtime@version, never @latest)
   • Valid YAML structure and fields
   • startWithoutCode requirement for runtime services
   • Production patterns from official recipes

The extend command WILL FAIL without these steps.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📚 LIVE DOCUMENTATION (fetch for latest info)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Import YAML reference (all fields, options):
  https://docs.zerops.io/references/import.md

Service type list (all available types):
  https://docs.zerops.io/references/import-yaml/type-list

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
   Use patterns from recipe-search.sh output to create import.yml.
   The recipe search provides correct version strings and required fields.

   Structure (versions from plan.json / data.json):
   services:
     - hostname: {name}
       type: {from recipe search}
       mode: NON_HA
       startWithoutCode: true  # Required for runtime services

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

# 0. Get valid patterns first (Gate 0 requirement)
.zcp/recipe-search.sh quick go postgresql
# Version is stored in plan.json from data.json

# 1. Create import file (version from plan.json)
cat > add-postgres.yml <<'YAML'
services:
  - hostname: db
    type: {version from plan.json: .managed_services[].version}
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
ssh {dev} 'zsc scale ram 1GiB 10m'     # Scale up for heavy local builds
ssh {dev} "{build_command}"
ssh {dev} './{binary} >> /tmp/app.log 2>&1'
# ↑ run_in_background=true (server blocks forever)

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
• Server start: run_in_background=true (NOT for builds/push!)
• DB tools (psql, redis-cli): Run from ZCP, NOT via ssh to runtime
• Runtime containers are minimal — no dev tools installed

ZCLI PUSH - REQUIRED FLAGS
─────────────────────────────────────────────────────────────────
zcli push {id} --setup={setup} --noGit   # Always include both!

• --setup required when zerops.yml has multiple setups (dev/prod)
• --noGit required if no git repo (or run git init first)
• Don't pipe zcli output — causes "allowed only in interactive terminal"

SUBDOMAIN SEQUENCE
─────────────────────────────────────────────────────────────────
❌ Import → Enable subdomain → Deploy    (FAILS - no HTTP config)
✅ Import → Deploy zerops.yml → Enable subdomain (WORKS)

Subdomain requires HTTP ports from zerops.yml deployment first!

EXPECTED WARNINGS (NOT ERRORS)
─────────────────────────────────────────────────────────────────
• "Unexpected EOF" during deploy — operation likely succeeded, check status
• Empty mount with startWithoutCode: true — this is correct behavior
• Slow first `go run` in dev — downloads Go toolchain, use `go build` instead

EVIDENCE FILES
─────────────────────────────────────────────────────────────────
/tmp/zcp_session                # Session ID
/tmp/zcp_phase                  # Current phase
/tmp/recipe_review.json         # Gate 0: Recipe patterns
/tmp/import_validated.json      # Gate 0.5: Import validation
/tmp/discovery.json             # Service mapping
/tmp/dev_verify.json            # Dev results
/tmp/stage_verify.json          # Stage results
/tmp/deploy_evidence.json       # Deploy proof

IMPORT VALIDATION (Gate 0.5)
─────────────────────────────────────────────────────────────────
.zcp/validate-import.sh import.yml          # Validate before import
.zcp/workflow.sh extend import.yml          # Auto-validates
.zcp/workflow.sh --help import-validation   # Full documentation
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

GET AI AGENT VARIANT (TWO FETCHES REQUIRED):

  FETCH 1 - Get import.yml to find service hostnames:
    https://stage-vega.zerops.dev/recipes/{name}.md?environment=ai-agent

  FETCH 2 - Get zerops.yml for specific service:
    https://stage-vega.zerops.dev/recipes/{name}.md?environment=ai-agent&guideFlow=integrate&guideEnv=ai-agent&guideApp={hostname}

  guideApp must match a hostname from FETCH 1's import.yml (e.g., appstage, app)

The AI Agent environment includes:
  • appdev service (development, Ubuntu, tools pre-installed)
  • appstage service (production build)
  • Database/cache services if needed
  • Proper zerops.yaml with dev and prod setups

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔧 LOCAL RECIPE SEARCH (REQUIRED FOR GATE 0)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Before creating import.yml, run the local recipe search tool:

  .zcp/recipe-search.sh quick {runtime} [managed-service]

Examples:
  .zcp/recipe-search.sh quick go postgresql
  .zcp/recipe-search.sh quick nodejs valkey

This creates /tmp/recipe_review.json with:
  • Valid YAML structure and required fields
  • startWithoutCode requirement for runtime services
  • Production patterns (alpine runtime, cache: true)
Note: Version strings now come from docs.zerops.io/data.json via plan.json

Gate 0 BLOCKS service imports until this is done.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 THE FLOW
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

INIT → RECIPE SEARCH → IMPORT → DEVELOP → DEPLOY → VERIFY → DONE

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📝 STEP 1: FIND AND FETCH RECIPE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Example: Bootstrap a Go app with PostgreSQL

1. Fetch the recipe list:
   URL: https://stage-vega.zerops.dev/recipes.md?category=hello-world-examples

2. Fetch the AI Agent variant with full zerops.yml:
   URL: https://stage-vega.zerops.dev/recipes/go-hello-world.md?environment=ai-agent&guideFlow=integrate&guideEnv=ai-agent&guideApp=appstage

3. Extract the import YAML from the recipe (example):

   Use patterns from .zcp/recipe-search.sh quick {runtime} {managed}

   Structure (versions from plan.json / data.json):
   services:
     - hostname: appstage
       type: {runtime from recipe search}
       zeropsSetup: prod
       buildFromGit: {if available in recipe}
       enableSubdomainAccess: true

     - hostname: appdev
       type: {runtime from recipe search}
       zeropsSetup: dev
       startWithoutCode: true  # OR buildFromGit if available
       enableSubdomainAccess: true

     - hostname: db
       type: {managed service from recipe search}
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
📚 ADDITIONAL REFERENCES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Import YAML reference (all fields):
  https://docs.zerops.io/references/import.md

zerops.yaml specification (build/deploy config):
  https://docs.zerops.io/zerops-yaml/specification.md

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📖 MANUAL EXAMPLE: Go Hello World
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 0. Get valid patterns (REQUIRED - Gate 0)
.zcp/recipe-search.sh quick go

# 1. Create import.yml (version from plan.json / data.json)
cat > import.yml <<'YAML'
services:
  - hostname: appdev
    type: {version from plan.json: .runtimes[0].version}
    startWithoutCode: true
    enableSubdomainAccess: true
  - hostname: appstage
    type: {version from plan.json: .runtimes[0].version}
    startWithoutCode: true
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
# Get runtime version: jq -r '.runtimes[0].version' /tmp/bootstrap_plan.json
# Example output: go@1
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
ssh appdev "./app >> /tmp/app.log 2>&1"  # run_in_background=true (server!)
.zcp/verify.sh appdev 8080 / /status

# Continue with normal DEPLOY → VERIFY → DONE flow

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  SUBDOMAIN SEQUENCE (CRITICAL)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Subdomain can only be enabled AFTER HTTP ports are configured!

❌ WRONG ORDER (fails with "Service stack is not http or https"):
   1. Import service with startWithoutCode: true
   2. zcli service enable-subdomain -S {id}  ← FAILS!
   3. Deploy zerops.yml

✅ CORRECT ORDER:
   1. Import service with startWithoutCode: true
   2. Deploy zerops.yml (contains ports with httpSupport: true)
   3. zcli service enable-subdomain -S {id}  ← Works!

The zerops.yml deployment configures HTTP ports. Only then can
subdomains be enabled.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  ZCLI PUSH FLAGS (CRITICAL)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

When pushing to a service, use these flags:

  zcli push {service_id} --setup={setup} --noGit

--setup={setup}  Required when zerops.yml has multiple setups (dev/prod)
--noGit          Required if directory is not a git repository

Alternative to --noGit - initialize git first:
  git init && git add . && git commit -m "init"
  zcli push {service_id} --setup={setup}

⚠️  Empty mount with startWithoutCode: true is EXPECTED behavior!
EOF
}

show_help_import_validation() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔒 GATE 0.5: IMPORT VALIDATION
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

This gate prevents a documented failure where import.yml was created
without critical fields, causing services to be stuck in READY_TO_DEPLOY.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
❌ THE FAILURE THAT CREATED THIS GATE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

An agent:
  1. Read the recipe showing buildFromGit and zeropsSetup
  2. Created import.yml WITHOUT these critical fields
  3. Services ended up in READY_TO_DEPLOY (empty containers)
  4. Couldn't mount SSHFS (no code to mount)
  5. Couldn't run the app (nothing to run)

Root cause: Treating import.yml as "service creation only" instead of
"service creation WITH initial deployment configuration."

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ WHAT THE VALIDATOR CHECKS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

For RUNTIME services (go, nodejs, php, python, etc.):

1. CODE SOURCE (CRITICAL)
   Must have EITHER:
     buildFromGit: <repo-url>      # Zerops clones & deploys
     OR
     startWithoutCode: true        # Dev mode with SSHFS mount

   Without either, the service is an empty container that can't run.

2. ZEROPS SETUP (CRITICAL)
   Must have:
     zeropsSetup: dev              # Links to zerops.yml setup: dev
     OR
     zeropsSetup: prod             # Links to zerops.yml setup: prod

   This tells Zerops WHICH build/run configuration to use.

For MANAGED services (postgresql, valkey, etc.):
   No code fields required - they run automatically.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 CORRECT IMPORT.YML STRUCTURE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Using buildFromGit (recommended for initial deploy):

  services:
    - hostname: appstage
      type: go@1
      zeropsSetup: prod                    # ← Links to zerops.yml
      buildFromGit: https://github.com/... # ← Initial code source
      enableSubdomainAccess: true

    - hostname: appdev
      type: go@1
      zeropsSetup: dev                     # ← Links to zerops.yml
      buildFromGit: https://github.com/... # ← Same repo, different setup
      enableSubdomainAccess: true

    - hostname: db
      type: postgresql@17
      mode: NON_HA                         # Managed: no code fields needed

Using startWithoutCode (dev with manual mount):

  services:
    - hostname: appdev
      type: go@1
      zeropsSetup: dev
      startWithoutCode: true               # ← Will use SSHFS mount
      enableSubdomainAccess: true

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔧 HOW TO USE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Manual validation:
  .zcp/validate-import.sh import.yml

With zerops.yml cross-check:
  .zcp/validate-import.sh import.yml /var/www/app/zerops.yml

Automatic (via extend command):
  .zcp/workflow.sh extend import.yml
  # Validation runs automatically before import

Bypass (NOT recommended):
  .zcp/workflow.sh extend import.yml --skip-validation

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  UNDERSTANDING READY_TO_DEPLOY STATUS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

If you see services in READY_TO_DEPLOY after import:

  zcli service list -P $projectId
  # Shows: appdev READY_TO_DEPLOY go@1

This means:
  • Container exists but has NO CODE
  • Cannot reach ACTIVE status
  • Waiting for initial deployment

Causes:
  1. import.yml missing buildFromGit (most common)
  2. startWithoutCode: true but no manual deployment yet

Fixes:
  A. Delete and re-import with buildFromGit
  B. Deploy code manually: ssh {dev} "zcli push ..."
  C. Use mount + startWithoutCode for dev workflow

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📖 THE GOLDEN RULE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  USE THE RECIPE'S IMPORT.YML - DON'T INVENT YOUR OWN!

The recipe's import.yml is:
  • Tested and known to work
  • Contains all required fields
  • Follows production best practices

When documentation says "use this pattern," USE IT EXACTLY.
Cherry-picking fields leads to broken deployments.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 EVIDENCE FILE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Creates: /tmp/import_validated.json

Contains:
  • session_id (for workflow tracking)
  • services validated
  • critical errors found
  • specific issues per service
  • validation rules applied

Use this file to understand exactly what failed validation.
EOF
}

