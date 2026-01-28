# Bootstrap Failure Fix Proposal

**Based on:** REPORT.md analysis of 2026-01-28 Go+PostgreSQL bootstrap failure
**Root Cause:** Subagent didn't understand `start: zsc noop --silent` requires manual server startup
**Impact:** Dev environment 100% broken (0/3 endpoints), marked complete anyway

---

## Fix Summary

| # | Component | File | Issue | Fix |
|---|-----------|------|-------|-----|
| 1 | Task List | spawn-subagents.sh:336-355 | Task 10 "Verify dev" has no manual start step | Split into 10a-10d with explicit server start |
| 2 | Verification | verify.sh | No pre-check if port is listening | Add pre-flight port check with helpful error |
| 3 | Completion | mark-complete.sh | Allows completion despite failed verification | Add verification gate |
| 4 | Helper | NEW: ensure-dev-running.sh | No helper to start dev server | Create runtime-aware server starter |
| 5 | Documentation | CLAUDE.md | No dev server management docs | Add "Dev Server Management" section |
| 6 | Aggregation | aggregate-results.sh | Only checks file existence, not verification | Add verification status check |

---

## Fix 1: spawn-subagents.sh Task List Enhancement

**File:** `zcp/.zcp/lib/bootstrap/steps/spawn-subagents.sh`
**Lines:** 336-355 (task table) and 367-374 (recovery section)

### Current (Broken)

```markdown
| # | Task | Command/Action |
|---|------|----------------|
...
| 10 | Verify dev | `.zcp/verify.sh ${dev_hostname} 8080 / /health /status` |
...
```

### Proposed Fix

Replace lines 336-377 with:

```bash
## Tasks

| # | Task | Command/Action |
|---|------|----------------|
| 1 | Read recipe | \`cat /tmp/fetched_recipe.md\` — understand patterns |
| 2 | Write zerops.yml | To \`${mount_path}/zerops.yml\` — setups: \`dev\`, \`prod\` |
| 3 | Write app code | HTTP server on :8080 with \`/\`, \`/health\`, \`/status\` |
| 4 | Init deps | \`ssh ${dev_hostname} "cd /var/www && <init>"\` |
| 5 | Auth zcli | \`ssh ${dev_hostname} 'zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "\$ZEROPS_ZCP_API_KEY"'\` |
| 6 | Git init | \`ssh ${dev_hostname} "cd /var/www && git config --global user.email 'zcp@zerops.io' && git config --global user.name 'ZCP' && git init && git add -A && git commit -m 'Bootstrap'"\` |
| 7 | Deploy dev | \`ssh ${dev_hostname} "cd /var/www && zcli push ${dev_id} --setup=dev --deploy-git-folder"\` |
| 8 | Wait dev | \`.zcp/status.sh --wait ${dev_hostname}\` |
| 9 | Subdomain dev | \`zcli service enable-subdomain -P \$projectId ${dev_id}\` |
| **10a** | **Start dev server** | \`ssh ${dev_hostname} "cd /var/www && nohup <run-cmd> > /tmp/app.log 2>&1 &"\` |
| **10b** | **Wait for port** | \`sleep 3 && ssh ${dev_hostname} "netstat -tlnp 2>/dev/null \\| grep 8080 \\|\\| ss -tlnp \\| grep 8080"\` |
| **10c** | **Test locally** | \`ssh ${dev_hostname} "curl -sf http://localhost:8080/ && curl -sf http://localhost:8080/health"\` |
| **10d** | **Verify dev** | \`.zcp/verify.sh ${dev_hostname} 8080 / /health /status\` |
| 11 | Deploy stage | \`ssh ${dev_hostname} "cd /var/www && zcli push ${stage_id} --setup=prod"\` |
| 12 | Wait stage | \`.zcp/status.sh --wait ${stage_hostname}\` |
| 13 | Subdomain stage | \`zcli service enable-subdomain -P \$projectId ${stage_id}\` |
| 14 | Verify stage | \`.zcp/verify.sh ${stage_hostname} 8080 / /health /status\` |
| 15 | **Done** | \`.zcp/mark-complete.sh ${dev_hostname}\` — then end session |

## CRITICAL: Dev Server Manual Start

⚠️ **Dev uses \`start: zsc noop --silent\` — NO PROCESS RUNS AUTOMATICALLY**

After deploy dev (step 7-9), the container is running but **nothing is listening on port 8080**.

**You MUST manually start the server before verification:**

| Runtime | Start Command |
|---------|---------------|
| Go | \`ssh ${dev_hostname} "cd /var/www && nohup go run *.go > /tmp/app.log 2>&1 &"\` |
| Node.js | \`ssh ${dev_hostname} "cd /var/www && nohup npm start > /tmp/app.log 2>&1 &"\` |
| Python | \`ssh ${dev_hostname} "cd /var/www && nohup python app.py > /tmp/app.log 2>&1 &"\` |
| PHP | Built-in server or configured separately |

**If verify.sh returns HTTP 000 (connection refused):**
1. Server not running → Start it manually
2. Check logs: \`ssh ${dev_hostname} "cat /tmp/app.log"\`
3. Check port: \`ssh ${dev_hostname} "netstat -tlnp | grep 8080"\`

**DO NOT proceed to stage deployment if dev verification fails.**

## Platform Rules
...
```

### Also add to recovery section (after line 367):

```bash
## Recovery

| Problem | Fix |
|---------|-----|
| "not a git repository" | \`ssh ${dev_hostname} "cd /var/www && git config --global user.email 'zcp@zerops.io' && git config --global user.name 'ZCP' && git init && git add -A && git commit -m 'Fix'"\` |
| "unauthenticated" | Re-run Task 5 |
| **HTTP 000 (connection refused)** | **Server not running → \`ssh ${dev_hostname} "cd /var/www && nohup go run *.go > /tmp/app.log 2>&1 &"\`** |
| **verify.sh all fail** | **Check port: \`ssh ${dev_hostname} "netstat -tlnp \\| grep 8080"\` — if empty, start server** |
| Endpoints fail (not 000) | \`zcli service log -P \$projectId ${dev_id}\` |
```

---

## Fix 2: verify.sh Pre-Flight Port Check

**File:** `zcp/.zcp/verify.sh`
**Location:** After hostname/port validation (line 242), before endpoint testing (line 270)

### Add Pre-Flight Check Function (after line 170)

```bash
# ============================================================================
# PRE-FLIGHT CHECK: Is port actually listening?
# ============================================================================

check_port_listening() {
    local service="$1"
    local port="$2"

    debug_log "Pre-flight: checking if port $port is listening on $service"

    # Check if port is listening (try netstat first, fall back to ss)
    local listening
    listening=$(ssh "$service" "netstat -tlnp 2>/dev/null | grep -E ':$port\\s' || ss -tlnp 2>/dev/null | grep -E ':$port\\s'" 2>/dev/null)

    if [ -z "$listening" ]; then
        return 1  # Nothing listening
    fi
    return 0  # Port is listening
}

show_no_server_warning() {
    local service="$1"
    local port="$2"

    cat <<EOF

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  NO SERVER LISTENING ON PORT $port
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

The service "$service" has no process listening on port $port.

This is expected for DEV services using:
    start: zsc noop --silent

DEV SERVICES REQUIRE MANUAL SERVER START:

    # For Go:
    ssh $service "cd /var/www && nohup go run *.go > /tmp/app.log 2>&1 &"

    # For Node.js:
    ssh $service "cd /var/www && nohup npm start > /tmp/app.log 2>&1 &"

    # For Python:
    ssh $service "cd /var/www && nohup python app.py > /tmp/app.log 2>&1 &"

After starting, verify port is listening:
    ssh $service "netstat -tlnp | grep $port"

Then re-run verification:
    .zcp/verify.sh $service $port / /health /status

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF
}
```

### Add Pre-Flight Check to main() (after line 248, before testing loop)

```bash
    # PRE-FLIGHT: Check if port is listening before attempting verification
    echo "Pre-flight check: verifying port $port is listening..."
    if ! check_port_listening "$service" "$port"; then
        show_no_server_warning "$service" "$port"

        # Create evidence file showing pre-flight failure
        local preflight_file="/tmp/${service}_verify.json"
        jq -n \
            --arg sid "$session_id" \
            --arg svc "$service" \
            --argjson prt "$port" \
            --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            '{
                session_id: $sid,
                service: $svc,
                port: $prt,
                timestamp: $ts,
                preflight_failed: true,
                preflight_reason: "No process listening on port",
                results: [],
                passed: 0,
                failed: 0,
                error: "PRE-FLIGHT FAILED: No server listening. Dev services require manual start."
            }' > "$preflight_file"

        echo ""
        echo "Evidence: $preflight_file (preflight_failed: true)"
        exit 1
    fi
    echo "  ✓ Port $port is listening"
    echo ""
```

---

## Fix 3: mark-complete.sh Verification Gate

**File:** `zcp/.zcp/mark-complete.sh`
**Location:** Add new function and modify mark_complete()

### Add Verification Check Function (after line 86)

```bash
# Check if verification passed for this service
check_verification_passed() {
    local hostname="$1"
    local verify_file="/tmp/${hostname}_verify.json"

    # If no verification file exists, can't confirm
    if [ ! -f "$verify_file" ]; then
        echo "no_evidence"
        return 2
    fi

    # Check for preflight failure
    local preflight_failed
    preflight_failed=$(jq -r '.preflight_failed // false' "$verify_file" 2>/dev/null)
    if [ "$preflight_failed" = "true" ]; then
        echo "preflight_failed"
        return 1
    fi

    # Check pass/fail counts
    local passed failed
    passed=$(jq -r '.passed // 0' "$verify_file" 2>/dev/null)
    failed=$(jq -r '.failed // 0' "$verify_file" 2>/dev/null)

    if [ "$failed" -gt 0 ]; then
        echo "failed:$failed"
        return 1
    fi

    if [ "$passed" -eq 0 ]; then
        echo "no_tests"
        return 1
    fi

    echo "passed:$passed"
    return 0
}
```

### Modify mark_complete() Function (replace lines 88-127)

```bash
# Mark a service as complete
mark_complete() {
    local hostname="$1"
    local force="${2:-false}"
    local service_dir="$STATE_DIR/$hostname"
    local status_file="$service_dir/status.json"
    local timestamp
    timestamp=$(get_timestamp)

    # VERIFICATION GATE: Check if dev verification passed
    if [ "$force" != "true" ] && [ "$force" != "--force" ]; then
        local verify_status
        verify_status=$(check_verification_passed "$hostname")
        local verify_exit=$?

        case "$verify_exit" in
            0)
                echo -e "${GREEN}✓ Verification passed ($verify_status)${NC}"
                ;;
            1)
                echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}" >&2
                echo -e "${RED}❌ CANNOT MARK COMPLETE: Verification failed${NC}" >&2
                echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}" >&2
                echo "" >&2
                echo "  Status: $verify_status" >&2
                echo "  Evidence: /tmp/${hostname}_verify.json" >&2
                echo "" >&2

                case "$verify_status" in
                    preflight_failed)
                        echo "  Problem: No server was listening on the port" >&2
                        echo "" >&2
                        echo "  Fix: Start the dev server manually:" >&2
                        echo "    ssh $hostname \"cd /var/www && nohup go run *.go > /tmp/app.log 2>&1 &\"" >&2
                        echo "" >&2
                        echo "  Then re-run verification:" >&2
                        echo "    .zcp/verify.sh $hostname 8080 / /health /status" >&2
                        ;;
                    failed:*)
                        local fail_count="${verify_status#failed:}"
                        echo "  Problem: $fail_count endpoint(s) failed HTTP check" >&2
                        echo "" >&2
                        echo "  Debug steps:" >&2
                        echo "    1. Check logs: ssh $hostname \"cat /tmp/app.log\"" >&2
                        echo "    2. Test locally: ssh $hostname \"curl http://localhost:8080/\"" >&2
                        echo "    3. Fix issues and re-verify" >&2
                        ;;
                esac

                echo "" >&2
                echo "  To force completion anyway (NOT RECOMMENDED):" >&2
                echo "    .zcp/mark-complete.sh --force $hostname" >&2
                echo "" >&2
                return 1
                ;;
            2)
                echo -e "${YELLOW}⚠️  No verification evidence found (/tmp/${hostname}_verify.json)${NC}" >&2
                echo "  Run verification first: .zcp/verify.sh $hostname 8080 / /health /status" >&2
                echo "" >&2
                echo "  To mark complete without verification (NOT RECOMMENDED):" >&2
                echo "    .zcp/mark-complete.sh --force $hostname" >&2
                return 1
                ;;
        esac
    else
        echo -e "${YELLOW}⚠️  Force mode: skipping verification check${NC}"
    fi

    # Create directory
    if ! mkdir -p "$service_dir" 2>/dev/null; then
        echo -e "${RED}ERROR: Cannot create state directory: $service_dir${NC}" >&2
        return 1
    fi

    # Write status file atomically (write to temp, then move)
    local tmp_file="${status_file}.tmp.$$"
    local verify_info=""
    if [ -f "/tmp/${hostname}_verify.json" ]; then
        verify_info=$(jq -c '{passed: .passed, failed: .failed, timestamp: .timestamp}' "/tmp/${hostname}_verify.json" 2>/dev/null || echo '{}')
    fi

    if ! cat > "$tmp_file" << EOF
{
    "phase": "complete",
    "completed_at": "$timestamp",
    "marked_by": "mark-complete.sh",
    "verification": $verify_info,
    "forced": $( [ "$force" = "true" ] || [ "$force" = "--force" ] && echo "true" || echo "false" )
}
EOF
    then
        echo -e "${RED}ERROR: Cannot write status file${NC}" >&2
        rm -f "$tmp_file" 2>/dev/null
        return 1
    fi

    if ! mv "$tmp_file" "$status_file"; then
        echo -e "${RED}ERROR: Cannot finalize status file${NC}" >&2
        rm -f "$tmp_file" 2>/dev/null
        return 1
    fi

    echo -e "${GREEN}✓ Marked $hostname as complete${NC}"
    echo "  State file: $status_file"
    return 0
}
```

### Update main() to Handle --force Flag (modify case statement around line 278)

```bash
        *)
            # Check for --force flag
            if [ "$1" = "--force" ]; then
                if [ -z "${2:-}" ]; then
                    echo "Usage: $0 --force <hostname>" >&2
                    exit 1
                fi
                validate_hostname "$2" || exit 1
                mark_complete "$2" "--force"
            else
                validate_hostname "$1" || exit 1
                mark_complete "$1"
            fi
            ;;
```

---

## Fix 4: New ensure-dev-running.sh Helper Script

**File:** `zcp/.zcp/ensure-dev-running.sh` (NEW FILE)

```bash
#!/usr/bin/env bash
# .zcp/ensure-dev-running.sh - Ensure dev server is running before verification
#
# This script handles the manual server startup required for dev services
# that use `start: zsc noop --silent`.
#
# Usage:
#   .zcp/ensure-dev-running.sh <hostname> [port] [runtime]
#   .zcp/ensure-dev-running.sh appdev 8080 go
#   .zcp/ensure-dev-running.sh appdev          # Defaults: port=8080, auto-detect runtime

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

show_help() {
    cat <<'EOF'
.zcp/ensure-dev-running.sh - Ensure dev server is running

USAGE:
    .zcp/ensure-dev-running.sh <hostname> [port] [runtime]

ARGUMENTS:
    hostname    Service hostname (e.g., appdev)
    port        Port to check (default: 8080)
    runtime     Runtime type: go, nodejs, python (default: auto-detect)

EXAMPLES:
    .zcp/ensure-dev-running.sh appdev
    .zcp/ensure-dev-running.sh appdev 8080 go
    .zcp/ensure-dev-running.sh myapp 3000 nodejs

WHY THIS EXISTS:
    Dev services use `start: zsc noop --silent` which means no process
    runs automatically. This script detects if the server is running
    and starts it if needed.

WHAT IT DOES:
    1. Checks if port is already listening
    2. If not, detects runtime from files in /var/www
    3. Starts appropriate server command
    4. Waits for port to become available
    5. Returns 0 on success, 1 on failure
EOF
}

# Detect runtime from files in container
detect_runtime() {
    local hostname="$1"

    # Check for Go
    if ssh "$hostname" "ls /var/www/*.go /var/www/go.mod 2>/dev/null" | grep -q .; then
        echo "go"
        return
    fi

    # Check for Node.js
    if ssh "$hostname" "ls /var/www/package.json 2>/dev/null" | grep -q .; then
        echo "nodejs"
        return
    fi

    # Check for Python
    if ssh "$hostname" "ls /var/www/*.py /var/www/requirements.txt 2>/dev/null" | grep -q .; then
        echo "python"
        return
    fi

    # Check for PHP
    if ssh "$hostname" "ls /var/www/*.php /var/www/composer.json 2>/dev/null" | grep -q .; then
        echo "php"
        return
    fi

    # Check for Rust
    if ssh "$hostname" "ls /var/www/Cargo.toml 2>/dev/null" | grep -q .; then
        echo "rust"
        return
    fi

    echo "unknown"
}

# Get start command for runtime
get_start_command() {
    local runtime="$1"
    local port="$2"

    case "$runtime" in
        go)
            echo "go run *.go"
            ;;
        nodejs)
            # Check for start script in package.json
            echo "npm start"
            ;;
        python)
            # Try common entry points
            echo "python app.py || python main.py || python server.py"
            ;;
        php)
            echo "php -S 0.0.0.0:$port -t ."
            ;;
        rust)
            echo "cargo run"
            ;;
        *)
            echo ""
            ;;
    esac
}

# Check if port is listening
is_port_listening() {
    local hostname="$1"
    local port="$2"

    ssh "$hostname" "netstat -tlnp 2>/dev/null | grep -q ':$port ' || ss -tlnp 2>/dev/null | grep -q ':$port '" 2>/dev/null
}

# Check if our server process is running
is_server_running() {
    local hostname="$1"
    local runtime="$2"

    case "$runtime" in
        go)
            ssh "$hostname" "pgrep -f 'go run' || pgrep -f './app' || pgrep -f 'main'" 2>/dev/null | grep -q .
            ;;
        nodejs)
            ssh "$hostname" "pgrep -f 'node' || pgrep -f 'npm'" 2>/dev/null | grep -q .
            ;;
        python)
            ssh "$hostname" "pgrep -f 'python'" 2>/dev/null | grep -q .
            ;;
        *)
            return 1
            ;;
    esac
}

main() {
    if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
        show_help
        exit 0
    fi

    local hostname="${1:-}"
    local port="${2:-8080}"
    local runtime="${3:-}"

    if [ -z "$hostname" ]; then
        echo -e "${RED}ERROR: hostname required${NC}" >&2
        echo "Usage: $0 <hostname> [port] [runtime]" >&2
        exit 1
    fi

    # Validate hostname
    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo -e "${RED}ERROR: Invalid hostname: $hostname${NC}" >&2
        exit 1
    fi

    echo "=== Ensuring dev server is running ==="
    echo "  Hostname: $hostname"
    echo "  Port: $port"

    # Step 1: Check if port is already listening
    if is_port_listening "$hostname" "$port"; then
        echo -e "  ${GREEN}✓ Port $port is already listening${NC}"
        exit 0
    fi

    echo -e "  ${YELLOW}○ Port $port not listening${NC}"

    # Step 2: Detect runtime if not provided
    if [ -z "$runtime" ]; then
        echo "  Detecting runtime..."
        runtime=$(detect_runtime "$hostname")
        echo "  Detected: $runtime"
    fi

    if [ "$runtime" = "unknown" ]; then
        echo -e "${RED}ERROR: Could not detect runtime${NC}" >&2
        echo "  Specify runtime explicitly: $0 $hostname $port <go|nodejs|python>" >&2
        exit 1
    fi

    # Step 3: Get start command
    local start_cmd
    start_cmd=$(get_start_command "$runtime" "$port")

    if [ -z "$start_cmd" ]; then
        echo -e "${RED}ERROR: No start command for runtime: $runtime${NC}" >&2
        exit 1
    fi

    echo "  Starting server: $start_cmd"

    # Step 4: Start the server
    ssh "$hostname" "cd /var/www && nohup $start_cmd > /tmp/app.log 2>&1 &"

    # Step 5: Wait for port to become available
    echo "  Waiting for port $port..."
    local attempts=0
    local max_attempts=30

    while [ $attempts -lt $max_attempts ]; do
        if is_port_listening "$hostname" "$port"; then
            echo -e "  ${GREEN}✓ Server started successfully (port $port listening)${NC}"
            exit 0
        fi

        # Check if process crashed
        if [ $attempts -gt 5 ] && ! is_server_running "$hostname" "$runtime"; then
            echo -e "${RED}ERROR: Server process died${NC}" >&2
            echo "  Check logs: ssh $hostname \"cat /tmp/app.log\"" >&2
            exit 1
        fi

        sleep 1
        attempts=$((attempts + 1))
        echo -n "."
    done

    echo ""
    echo -e "${RED}ERROR: Server failed to start within ${max_attempts}s${NC}" >&2
    echo "  Check logs: ssh $hostname \"cat /tmp/app.log\"" >&2
    echo "  Manual start: ssh $hostname \"cd /var/www && $start_cmd\"" >&2
    exit 1
}

main "$@"
```

---

## Fix 5: CLAUDE.md Dev Server Management Section

**File:** `zcp/CLAUDE.md`
**Location:** After "## Critical Rules" section (line 117)

### Add New Section

```markdown
## Dev Server Management

**Dev services use `start: zsc noop --silent` — NO PROCESS RUNS AUTOMATICALLY.**

This is intentional: dev environment is for manual control during development.

### Starting the Dev Server

| Runtime | Command |
|---------|---------|
| Go | `ssh {dev} "cd /var/www && nohup go run *.go > /tmp/app.log 2>&1 &"` |
| Node.js | `ssh {dev} "cd /var/www && nohup npm start > /tmp/app.log 2>&1 &"` |
| Python | `ssh {dev} "cd /var/www && nohup python app.py > /tmp/app.log 2>&1 &"` |
| Helper | `.zcp/ensure-dev-running.sh {dev} 8080` |

### Stopping the Dev Server

```bash
# Go
ssh {dev} "pkill -f 'go run' || pkill -f './app'"

# Node.js
ssh {dev} "pkill -f 'node' || pkill -f 'npm'"

# Python
ssh {dev} "pkill -f 'python'"
```

### Pre-Verification Checklist

Before running `.zcp/verify.sh`:

```bash
# 1. Is port listening?
ssh {dev} "netstat -tlnp | grep 8080"

# 2. Is process running?
ssh {dev} "ps aux | grep -E 'go|node|python' | grep -v grep"

# 3. Test locally first
ssh {dev} "curl -sf http://localhost:8080/"
```

### Why verify.sh Returns HTTP 000

HTTP 000 = **connection refused** = **no server listening**

| Symptom | Cause | Fix |
|---------|-------|-----|
| All endpoints HTTP 000 | Server not started | Start it manually (see above) |
| zcli log shows `zsc noop` | Expected for dev | This is correct, start server manually |
| Server starts then dies | Crash on startup | `ssh {dev} "cat /tmp/app.log"` |

### Dev vs Stage Comparison

| Aspect | Dev | Stage |
|--------|-----|-------|
| Start command | `zsc noop --silent` | `./app` |
| Auto-start? | NO | YES |
| Server management | Manual | Automatic |
| When to verify | After manual start | After deploy completes |
```

---

## Fix 6: aggregate-results.sh Verification Gate

**File:** `zcp/.zcp/lib/bootstrap/steps/aggregate-results.sh`
**Location:** In step_aggregate_results(), after checking service state (around line 126)

### Add Verification Check Function (after line 67)

```bash
# Check if dev verification passed for this service
check_dev_verification() {
    local dev_hostname="$1"
    local verify_file="/tmp/${dev_hostname}_verify.json"

    # No verification file = not verified
    if [ ! -f "$verify_file" ]; then
        echo "not_verified"
        return 2
    fi

    # Check for preflight failure
    local preflight_failed
    preflight_failed=$(jq -r '.preflight_failed // false' "$verify_file" 2>/dev/null)
    if [ "$preflight_failed" = "true" ]; then
        echo "preflight_failed"
        return 1
    fi

    # Check pass/fail
    local passed failed
    passed=$(jq -r '.passed // 0' "$verify_file" 2>/dev/null)
    failed=$(jq -r '.failed // 0' "$verify_file" 2>/dev/null)

    if [ "$failed" -gt 0 ]; then
        echo "failed:$passed/$((passed + failed))"
        return 1
    fi

    if [ "$passed" -eq 0 ]; then
        echo "no_tests"
        return 1
    fi

    echo "passed:$passed"
    return 0
}
```

### Modify Service Loop to Check Verification (around line 126, after auto_mark_complete)

```bash
        # VERIFICATION GATE: Check if dev verification passed
        local verify_status="unknown"
        local verify_passed=false
        if [ "$phase" = "complete" ]; then
            verify_status=$(check_dev_verification "$dev_hostname")
            if [ $? -eq 0 ]; then
                verify_passed=true
            fi
        fi

        local result
        result=$(jq -n \
            --arg h "$dev_hostname" \
            --arg p "$phase" \
            --argjson auto "$auto_detected" \
            --arg vs "$verify_status" \
            --argjson vp "$verify_passed" \
            --argjson s "$service_state" \
            '{hostname: $h, phase: $p, auto_detected: $auto, verify_status: $vs, verify_passed: $vp, state: $s}')
```

### Add Verification Failure Handling (after checking for pending, around line 186)

```bash
    # Check for verification failures (even if marked complete)
    local verify_failures=()
    local i=0
    while [ "$i" -lt "$count" ]; do
        local dev_hostname
        dev_hostname=$(echo "$handoffs" | jq -r ".[$i].dev_hostname")

        local verify_status
        verify_status=$(check_dev_verification "$dev_hostname")
        local verify_exit=$?

        if [ $verify_exit -ne 0 ]; then
            verify_failures+=("$dev_hostname:$verify_status")
        fi

        i=$((i + 1))
    done

    if [ ${#verify_failures[@]} -gt 0 ]; then
        local verify_json
        verify_json=$(printf '%s\n' "${verify_failures[@]}" | jq -R . | jq -s .)

        local data
        data=$(jq -n \
            --argjson complete "$complete" \
            --argjson total "$count" \
            --argjson verify_failures "$verify_json" \
            --argjson results "$results" \
            '{
                complete: $complete,
                total: $total,
                verify_failures: $verify_failures,
                results: $results,
                action: "Dev verification failed - endpoints not working",
                critical: "DO NOT mark bootstrap complete with broken dev environment",
                fix_steps: [
                    "1. Start dev server: ssh {hostname} \"cd /var/www && nohup go run *.go > /tmp/app.log 2>&1 &\"",
                    "2. Wait for port: ssh {hostname} \"netstat -tlnp | grep 8080\"",
                    "3. Test locally: ssh {hostname} \"curl http://localhost:8080/\"",
                    "4. Re-verify: .zcp/verify.sh {hostname} 8080 / /health /status",
                    "5. Re-run: .zcp/bootstrap.sh step aggregate-results"
                ]
            }')

        json_error "aggregate-results" \
            "Verification failed for ${#verify_failures[@]} service(s) - dev endpoints not working" \
            "$data" \
            '["Start dev server manually", "Re-run verification", "Then re-run aggregate-results"]'
        return 1
    fi
```

---

## Implementation Order

1. **ensure-dev-running.sh** — Create new helper script first (no dependencies)
2. **verify.sh** — Add pre-flight check (catches problem at source)
3. **spawn-subagents.sh** — Update task list with manual start steps (prevents issue)
4. **mark-complete.sh** — Add verification gate (blocks false completion)
5. **aggregate-results.sh** — Add verification check (final safety net)
6. **CLAUDE.md** — Add documentation (education/reference)

---

## Testing the Fixes

After implementing, test with:

```bash
# 1. Run bootstrap
.zcp/workflow.sh bootstrap --runtime go --services postgresql

# 2. Follow steps through spawn-subagents

# 3. In subagent, verify new flow:
#    - Deploy dev
#    - Try verify.sh WITHOUT starting server → should fail with helpful message
#    - Start server manually
#    - Verify → should pass
#    - mark-complete → should succeed

# 4. Try to mark complete without verification:
.zcp/mark-complete.sh appdev
# Should fail with verification gate error

# 5. Force completion (test override):
.zcp/mark-complete.sh --force appdev
# Should succeed with warning

# 6. Run aggregate-results
.zcp/bootstrap.sh step aggregate-results
# Should check verification status
```

---

## Summary

These 6 fixes create a **defense-in-depth** approach:

1. **spawn-subagents.sh** — Tells subagent exactly what to do (prevention)
2. **verify.sh** — Catches "no server" early with actionable error (detection)
3. **ensure-dev-running.sh** — Helper to start server correctly (assistance)
4. **mark-complete.sh** — Blocks completion if verification failed (enforcement)
5. **aggregate-results.sh** — Final check before bootstrap completion (validation)
6. **CLAUDE.md** — Documents the pattern for future reference (education)

The subagent cannot accidentally mark complete with a broken dev environment anymore.
