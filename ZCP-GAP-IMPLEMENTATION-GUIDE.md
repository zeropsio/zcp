# ZCP Gap Implementation Guide

**Purpose:** Implementation specifications for addressing workflow gaps identified in scenario analysis
**Scope:** 4 critical gaps + 5 high-value partial gaps
**Version:** 1.0.0

---

## Overview

| Gap | Scenario | Priority | Complexity |
|-----|----------|----------|------------|
| 44 | Concurrent Deploy Conflict | HIGH | Low |
| 45 | Deploy Order Dependency | MEDIUM | Medium |
| 46 | Shared Database Schema | MEDIUM | Low |
| 48 | Service Communication | LOW | Medium |
| 32 | Wrong Port Verified | HIGH | Low |
| 50 | Multi-Service Iteration | MEDIUM | Low |

---

## Gap 44: Concurrent Deploy Conflict

### Problem
Two agents deploying simultaneously don't know about each other. While Zerops queues deploys (and cancels queued ones if the first fails), agents have no visibility into this.

### Zerops Behavior
- Deploys to same service are **queued**
- Visible via `zcli service list --format json`
- If first deploy fails, queued deploys are **cancelled**

### Implementation

#### A. Enhance `status.sh` to detect queued deploys

```bash
# In status.sh, add function:

check_deploy_queue() {
    local service="$1"
    local pid
    pid=$(get_project_id)

    # Get service info with queue status
    local service_json
    service_json=$(zcli service list -P "$pid" --format json 2>/dev/null | \
        sed 's/\x1b\[[0-9;]*m//g' | \
        jq --arg svc "$service" '.services[] | select(.name == $svc)' 2>/dev/null)

    if [ -z "$service_json" ]; then
        echo "NOT_FOUND"
        return 1
    fi

    # Check for queued processes
    # NOTE: .processes[] contains deploy queue entries
    # Verify the exact structure in your Zerops version
    local process_count
    process_count=$(echo "$service_json" | jq '.processes | length // 0' 2>/dev/null)

    # Also check for explicit status if available
    local building_count
    building_count=$(echo "$service_json" | jq '[.processes[]? | select(.status == "BUILDING" or .status == "PENDING")] | length // 0' 2>/dev/null)

    if [ "$building_count" -gt 1 ] || [ "$process_count" -gt 1 ]; then
        echo "QUEUED:$process_count"
        return 0
    elif [ "$building_count" -eq 1 ] || [ "$process_count" -eq 1 ]; then
        echo "BUILDING"
        return 0
    else
        echo "IDLE"
        return 0
    fi
}
```

#### B. Add pre-deploy check in guidance

Update `output_phase_guidance()` in `transition.sh` for DEPLOY phase:

```bash
# Add to DEPLOY guidance:
echo "Pre-deploy check (optional):"
echo "  .zcp/status.sh --check-queue {stage}"
echo "  # Shows if another deploy is in progress/queued"
```

#### C. Modify `status.sh --wait` to handle queue

```bash
# In wait_for_deployment(), after checking status:

# Check if we're queued behind another deploy
local queue_status
queue_status=$(check_deploy_queue "$service")

case "$queue_status" in
    QUEUED:*)
        local queue_count="${queue_status#QUEUED:}"
        echo "  [${elapsed}/${timeout}s] ⏳ Queued (${queue_count} deploys in queue)"
        echo "    → Your deploy will start after current one completes"
        ;;
    # ... existing cases
esac
```

### Files to Modify
- `.zcp/status.sh` - Add `check_deploy_queue()`, update `--wait` logic
- `.zcp/lib/commands/transition.sh` - Add queue check to DEPLOY guidance

### Evidence
No new evidence files needed. Queue status is transient.

---

## Gap 45: Deploy Order Dependency

### Problem
When Service A (e.g., API) depends on Service B (e.g., Worker) being deployed first, there's no guidance or enforcement.

### Zerops Behavior
- `priority` in import.yml controls **creation** order only (higher = sooner)
- Services become available when ready - no built-in dependency waiting
- Existing wait utilities can be used

### Implementation

#### A. Add optional `deploy_order` to discovery.json

```json
{
  "services": [
    {
      "dev": {"id": "...", "name": "workerdev"},
      "stage": {"id": "...", "name": "workerstage"},
      "runtime": "go",
      "deploy_order": 1
    },
    {
      "dev": {"id": "...", "name": "apidev"},
      "stage": {"id": "...", "name": "apistage"},
      "runtime": "go",
      "deploy_order": 2,
      "depends_on": ["workerstage"]
    }
  ]
}
```

#### B. Create `cmd_create_discovery` enhancement

Add `--ordered` flag to `create_discovery` for multi-service:

```bash
# Usage:
# .zcp/workflow.sh create_discovery_multi \
#   workerdev:worker123:workerstage:worker456:1 \
#   apidev:api123:apistage:api456:2:workerstage

# Format: dev_name:dev_id:stage_name:stage_id:order[:depends_on]
```

#### C. Update DEPLOY guidance for ordered deploys

In `cmd_show()` when `service_count > 1` and `deploy_order` exists:

```bash
# Show ordered deploy sequence
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📦 DEPLOY ORDER (dependencies detected)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Sort by deploy_order and show sequence
jq -r '.services | sort_by(.deploy_order // 99) | .[] |
    "[\(.deploy_order // "?")] \(.stage.name)" +
    if .depends_on then " (after: \(.depends_on | join(", ")))" else "" end' \
    "$DISCOVERY_FILE"

echo ""
echo "⚠️  Deploy in this order. Wait for each before starting next:"
echo ""

# Generate sequential commands
# NOTE: Use process substitution to avoid subshell variable scope issue
# (piping creates subshell where $i increments are lost)
local i=1
while IFS='|' read -r dev_name stage_id stage_name; do
    echo "Step $i: $stage_name"
    echo "   ssh $dev_name \"zcli push $stage_id --setup=prod\""
    echo "   .zcp/status.sh --wait $stage_name"
    echo ""
    ((i++))
done < <(jq -r '.services | sort_by(.deploy_order // 99) | .[] |
    "\(.dev.name)|\(.stage.id)|\(.stage.name)"' "$DISCOVERY_FILE")
```

#### D. Add deploy order gate check (optional enforcement)

```bash
# In gates.sh, add to check_gate_develop_to_deploy():

# Check if multi-service with dependencies
if [ -f "$DISCOVERY_FILE" ]; then
    local has_deps
    has_deps=$(jq -r '[.services[] | select(.depends_on)] | length' "$DISCOVERY_FILE" 2>/dev/null)

    if [ "$has_deps" -gt 0 ]; then
        gate_warn "Deploy order dependencies detected - deploy services in order shown by 'workflow.sh show'"
    fi
fi
```

### Files to Modify
- `.zcp/lib/commands/discovery.sh` - Add ordered discovery support
- `.zcp/lib/commands/status.sh` - Show deploy order in guidance
- `.zcp/lib/gates.sh` - Optional warning for dependencies

### Evidence
- `discovery.json` extended with `deploy_order` and `depends_on` fields

---

## Gap 46: Shared Database Schema

### Problem
When multiple services share a database, migrations might run multiple times or not at all.

### Zerops Behavior
- `zsc execOnce "id" "command"` - Runs command once per service (across all containers of that service)
- `initCommands` in zerops.yml - Runs on deploy before start
- These are **per-service**, not cross-service

### Implementation

#### A. Add migration guidance to DEVELOP phase

Update `output_phase_guidance()` for DEVELOP:

```bash
# Add to DEVELOP guidance when multi-service with shared DB detected:

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🗄️  DATABASE MIGRATIONS (shared database detected)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Choose ONE service to run migrations. In its zerops.yml:"
echo ""
echo "  run:"
echo "    initCommands:"
echo "      - zsc execOnce \"\${appVersionId}-migrate\" \"./migrate.sh\""
echo ""
echo "Other services should NOT run migrations."
echo ""
echo "Pattern: zsc execOnce \"unique-id\" \"command\""
echo "  • Runs once per service (idempotent across containers)"
echo "  • Use \$appVersionId for automatic refresh on each deploy"
echo "  • Format: \"\${appVersionId}-{task}\" ensures fresh execution per deploy"
echo "  • Docs: https://docs.zerops.io/references/zsc#execonce"
echo ""
echo "⚠️  Do NOT use manual versioning like \"migrate-v1\", \"migrate-v2\""
echo "   This requires manual bumping and is error-prone."
```

#### B. Detect shared database in discovery

When creating discovery, check for shared database patterns:

```bash
# In cmd_create_discovery or a helper:

detect_shared_database() {
    # Check if multiple services reference same db_* variables
    # This is heuristic - check if $db_connectionString appears in multiple services

    local services_with_db=0

    for svc in $(jq -r '.services[].dev.name' "$DISCOVERY_FILE" 2>/dev/null); do
        if ssh "$svc" 'echo $db_connectionString' 2>/dev/null | grep -q .; then
            services_with_db=$((services_with_db + 1))
        fi
    done

    if [ "$services_with_db" -gt 1 ]; then
        # Update discovery with shared_database flag
        jq '.shared_database = true' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp" && \
            mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"
    fi
}
```

#### C. Add migration tracking evidence (optional)

```bash
# .zcp/record-migration.sh

#!/bin/bash
# Records that a service has run migrations

service="$1"
migration_id="$2"

if [ -z "$service" ] || [ -z "$migration_id" ]; then
    echo "Usage: .zcp/record-migration.sh {service} {migration_id}"
    exit 1
fi

session_id=$(cat /tmp/claude_session 2>/dev/null)
timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

jq -n \
    --arg sid "$session_id" \
    --arg svc "$service" \
    --arg mid "$migration_id" \
    --arg ts "$timestamp" \
    '{
        session_id: $sid,
        service: $svc,
        migration_id: $mid,
        timestamp: $ts
    }' > "/tmp/migration_${service}_${migration_id}.json"

echo "✓ Migration recorded: $migration_id on $service"
```

### Files to Modify
- `.zcp/lib/commands/transition.sh` - Add migration guidance
- `.zcp/lib/commands/discovery.sh` - Detect shared database
- `.zcp/record-migration.sh` - New file (optional)

### Evidence
- `discovery.json` extended with `shared_database: true` flag
- `/tmp/migration_{service}_{id}.json` - Optional migration evidence

---

## Gap 48: Service Communication Failure

### Problem
When Service A can't reach Service B internally, there's no diagnostic tooling.

### Zerops Behavior
- Services use **vxlan** private network
- Standard, predictable networking: `http://{hostname}:{port}`
- All services in project share the network

### Implementation

#### A. Create internal connectivity check script

```bash
#!/bin/bash
# .zcp/check-connectivity.sh
# Tests internal service-to-service connectivity

set -o pipefail

show_help() {
    cat <<'EOF'
.zcp/check-connectivity.sh - Test internal service connectivity

USAGE:
  .zcp/check-connectivity.sh {from_service} {to_service} {port} [endpoint]

EXAMPLES:
  .zcp/check-connectivity.sh apidev workerdev 8080
  .zcp/check-connectivity.sh apidev workerdev 8080 /health

DESCRIPTION:
  SSHs into {from_service} and attempts to curl {to_service}:{port}
  on the internal vxlan network.
EOF
}

if [ "$1" = "--help" ] || [ -z "$3" ]; then
    show_help
    exit 0
fi

from_svc="$1"
to_svc="$2"
port="$3"
endpoint="${4:-/}"

echo "Testing: $from_svc → $to_svc:$port$endpoint"
echo ""

# Validate service names
if [[ ! "$from_svc" =~ ^[a-zA-Z][a-zA-Z0-9_-]*$ ]] || \
   [[ ! "$to_svc" =~ ^[a-zA-Z][a-zA-Z0-9_-]*$ ]]; then
    echo "❌ Invalid service name"
    exit 1
fi

# Test DNS resolution first
# NOTE: getent may not be available on all containers, so include fallbacks
echo "1. DNS Resolution..."
dns_result=$(ssh "$from_svc" "getent hosts $to_svc 2>/dev/null || \
    nslookup $to_svc 2>/dev/null | grep -A1 'Name:' | tail -1 || \
    host $to_svc 2>/dev/null | head -1" 2>&1)
dns_exit=$?

if [ $dns_exit -eq 0 ] && [ -n "$dns_result" ]; then
    # Extract IP from various formats
    resolved_ip=$(echo "$dns_result" | grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' | head -1)
    if [ -n "$resolved_ip" ]; then
        echo "   ✓ $to_svc resolves to: $resolved_ip"
    else
        echo "   ✓ $to_svc resolved (format: ${dns_result:0:50}...)"
    fi
else
    echo "   ✗ DNS resolution failed for $to_svc"
    echo "   → Is $to_svc running? Check: zcli service list"
    exit 1
fi

# Test TCP connectivity
# NOTE: /dev/tcp is bash-specific, nc (netcat) is more portable
echo ""
echo "2. TCP Port Check..."
tcp_result=$(ssh "$from_svc" "
    if timeout 5 bash -c '</dev/tcp/$to_svc/$port' 2>/dev/null; then
        echo 'OPEN'
    elif command -v nc >/dev/null 2>&1 && timeout 5 nc -z $to_svc $port 2>/dev/null; then
        echo 'OPEN'
    elif command -v curl >/dev/null 2>&1 && timeout 5 curl -s --connect-timeout 3 http://$to_svc:$port >/dev/null 2>&1; then
        echo 'OPEN'
    else
        echo 'CLOSED'
    fi
" 2>&1)

if [ "$tcp_result" = "OPEN" ]; then
    echo "   ✓ Port $port is open"
else
    echo "   ✗ Port $port is not reachable"
    echo "   → Is $to_svc listening on $port? Check: ssh $to_svc 'netstat -tlnp | grep $port'"
    exit 1
fi

# Test HTTP endpoint
echo ""
echo "3. HTTP Request..."
http_result=$(ssh "$from_svc" "curl -s -w '\n%{http_code}' --connect-timeout 5 http://$to_svc:$port$endpoint" 2>&1)
http_code="${http_result##*$'\n'}"
http_body="${http_result%$'\n'*}"

if [[ "$http_code" =~ ^2 ]]; then
    echo "   ✓ HTTP $http_code"
    echo ""
    echo "Response (first 200 chars):"
    echo "${http_body:0:200}"
else
    echo "   ✗ HTTP $http_code"
    echo ""
    echo "Response:"
    echo "${http_body:0:500}"
    exit 1
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ Connectivity OK: $from_svc → $to_svc:$port$endpoint"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
```

#### B. Add connectivity guidance for multi-service

In `cmd_show()` when `service_count > 1`:

```bash
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔗 INTERNAL CONNECTIVITY"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Services communicate via internal network (vxlan):"
echo "  http://{hostname}:{port}"
echo ""
echo "Test connectivity between services:"
echo "  .zcp/check-connectivity.sh {from} {to} {port}"
echo ""
echo "Example:"
echo "  .zcp/check-connectivity.sh apidev workerdev 8080 /health"
```

#### C. Add to recovery command reference

Update the recovery command reference table in scenarios doc to include:

```
| Internal network issue | .zcp/check-connectivity.sh {from} {to} {port} |
```

### Files to Create
- `.zcp/check-connectivity.sh` - New connectivity test script

### Files to Modify
- `.zcp/lib/commands/status.sh` - Add connectivity guidance for multi-service

---

## Gap 32: Wrong Port Verified

### Problem
Agent uses default port (8080) but app runs on different port. Pre-flight fails but doesn't suggest checking zerops.yml.

### Zerops Behavior
- Port defined in `zerops.yml` under `run.ports[].port`
- This is the source of truth

### Implementation

#### A. Add port extraction helper

```bash
# In .zcp/lib/utils.sh or verify.sh:

get_port_from_zerops_yml() {
    local hostname="$1"
    local setup_name="${2:-}"  # Optional: dev, prod, etc.
    local config_file="/var/www/${hostname}/zerops.yml"

    if [ ! -f "$config_file" ]; then
        echo ""
        return 1
    fi

    if ! command -v yq &>/dev/null; then
        echo ""
        return 1
    fi

    local port=""

    # IMPORTANT: zerops.yml has multiple setups with different run sections
    # Structure:
    #   zerops:
    #     - setup: dev
    #       run:
    #         ports:
    #           - port: 8080
    #     - setup: prod
    #       run:
    #         ports:
    #           - port: 3000

    if [ -n "$setup_name" ]; then
        # Match by explicit setup name
        port=$(yq e ".zerops[] | select(.setup == \"$setup_name\") | .run.ports[0].port // empty" "$config_file" 2>/dev/null)
    fi

    # Fallback 1: Try matching by hostname field
    if [ -z "$port" ] || [ "$port" = "null" ]; then
        port=$(yq e ".zerops[] | select(.hostname == \"$hostname\") | .run.ports[0].port // empty" "$config_file" 2>/dev/null)
    fi

    # Fallback 2: Infer setup from hostname pattern (dev/stage suffix)
    if [ -z "$port" ] || [ "$port" = "null" ]; then
        if [[ "$hostname" == *"dev"* ]]; then
            port=$(yq e '.zerops[] | select(.setup == "dev") | .run.ports[0].port // empty' "$config_file" 2>/dev/null)
        elif [[ "$hostname" == *"stage"* ]] || [[ "$hostname" == *"prod"* ]]; then
            port=$(yq e '.zerops[] | select(.setup == "prod") | .run.ports[0].port // empty' "$config_file" 2>/dev/null)
        fi
    fi

    # Fallback 3: Take first setup's port (last resort)
    if [ -z "$port" ] || [ "$port" = "null" ]; then
        port=$(yq e '.zerops[0].run.ports[0].port // empty' "$config_file" 2>/dev/null)
    fi

    if [ -n "$port" ] && [ "$port" != "null" ]; then
        echo "$port"
        return 0
    fi

    echo ""
    return 1
}
```

#### B. Enhance pre-flight failure message

In `verify.sh`, update `show_no_server_error()`:

```bash
show_no_server_error() {
    local service="$1"
    local port="$2"

    # Try to get expected port from zerops.yml
    local expected_port
    expected_port=$(get_port_from_zerops_yml "$service" 2>/dev/null)

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "⚠️  NO SERVER LISTENING ON PORT $port"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check for port mismatch
    if [ -n "$expected_port" ] && [ "$expected_port" != "$port" ]; then
        echo ""
        echo "⚠️  PORT MISMATCH DETECTED"
        echo "   You tried:     $port"
        echo "   zerops.yml:    $expected_port"
        echo ""
        echo "   Try: .zcp/verify.sh $service $expected_port /"
        echo ""
    fi

    # ... rest of existing function
}
```

#### C. Add port hint to verify.sh help

```bash
# In show_help():
echo "PORT DETECTION:"
echo "  If unsure which port, check zerops.yml:"
echo "    cat /var/www/{service}/zerops.yml | grep -A5 'ports:'"
echo ""
echo "  Or use yq (note: different setups may have different ports):"
echo "    # For dev setup:"
echo "    yq '.zerops[] | select(.setup == \"dev\") | .run.ports[0].port' /var/www/{service}/zerops.yml"
echo "    # For prod setup:"
echo "    yq '.zerops[] | select(.setup == \"prod\") | .run.ports[0].port' /var/www/{service}/zerops.yml"
```

### Files to Modify
- `.zcp/verify.sh` - Add port extraction, enhance error message
- `.zcp/lib/utils.sh` - Add `get_port_from_zerops_yml()` helper

---

## Gap 50: Multi-Service Iteration

### Problem
After DONE with 3-service system, user wants to update only API. No way to iterate single service.

### Implementation

#### A. Add `--service` flag to iterate

```bash
# In .zcp/lib/commands/iterate.sh, update cmd_iterate():

cmd_iterate() {
    local summary=""
    local target_phase="DEVELOP"
    local target_service=""

    # Parse arguments
    while [ $# -gt 0 ]; do
        case "$1" in
            --to)
                target_phase="$2"
                shift 2
                ;;
            --service)
                target_service="$2"
                shift 2
                ;;
            *)
                summary="$1"
                shift
                ;;
        esac
    done

    # ... existing validation ...

    # If --service specified, validate it exists in discovery
    if [ -n "$target_service" ]; then
        if [ ! -f "$DISCOVERY_FILE" ]; then
            echo "❌ No discovery.json - cannot use --service"
            return 1
        fi

        # Check service exists (could be dev.name or stage.name)
        local found_dev found_stage
        found_dev=$(jq -r --arg svc "$target_service" \
            '.services[] | select(.dev.name == $svc) | .dev.name' \
            "$DISCOVERY_FILE" 2>/dev/null)
        found_stage=$(jq -r --arg svc "$target_service" \
            '.services[] | select(.stage.name == $svc) | .stage.name' \
            "$DISCOVERY_FILE" 2>/dev/null)

        if [ -z "$found_dev" ] && [ -z "$found_stage" ]; then
            echo "❌ Service '$target_service' not found in discovery"
            echo ""
            echo "Available services:"
            jq -r '.services[] | "  • \(.dev.name) / \(.stage.name)"' "$DISCOVERY_FILE"
            return 1
        fi

        # Show which role matched
        if [ -n "$found_dev" ]; then
            echo "  Matched dev service: $found_dev"
        elif [ -n "$found_stage" ]; then
            echo "  Matched stage service: $found_stage"
        fi

        # Record focused iteration
        record_focused_iteration "$target_service" "$summary"
    fi

    # ... rest of existing function ...

    # Show focused iteration info
    if [ -n "$target_service" ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "🎯 FOCUSED ITERATION: $target_service"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        echo "Only verify/deploy this service. Other services unchanged."
        echo ""
        echo "⚠️  Gate will still check ALL services at transition."
        echo "   Existing evidence for other services will be preserved."
    fi
}
```

#### B. Preserve non-focused service evidence

```bash
# In archive_iteration_evidence():

archive_iteration_evidence() {
    local n="$1"
    local focused_service="$2"  # New parameter
    local iter_dir="$WORKFLOW_ITERATIONS_DIR/$n"

    mkdir -p "$iter_dir"

    # If focused iteration, only archive the focused service's evidence
    if [ -n "$focused_service" ]; then
        # Archive only focused service evidence
        local verify_file="/tmp/${focused_service}_verify.json"
        if [ -f "$verify_file" ]; then
            mv "$verify_file" "$iter_dir/${focused_service}_verify.json"
        fi

        # IMPORTANT: Clean up symlinks pointing to the archived file
        # verify.sh creates symlinks: /tmp/dev_verify.json -> /tmp/{service}_verify.json
        # Moving the target leaves dangling symlinks that break gate checks
        for link in /tmp/dev_verify.json /tmp/stage_verify.json; do
            if [ -L "$link" ]; then
                local target
                target=$(readlink "$link" 2>/dev/null)
                # Check if symlink pointed to the file we just archived
                if [ "$target" = "$verify_file" ] || [ "$target" = "/tmp/${focused_service}_verify.json" ]; then
                    rm -f "$link"
                    echo "  → Removed dangling symlink: $link"
                fi
            fi
        done

        # Keep other services' evidence in place
        echo "  → Preserved evidence for non-focused services"
    else
        # Original behavior: archive all evidence
        for file in dev_verify stage_verify deploy_evidence; do
            if [ -f "/tmp/${file}.json" ]; then
                mv "/tmp/${file}.json" "$iter_dir/${file}.json"
            fi
        done
    fi
}
```

#### C. Update help text

```bash
# In workflow.sh usage:
echo "  iterate [--service {name}] [--to PHASE] [summary]"
echo "          Start new iteration, optionally focused on single service"
```

### Files to Modify
- `.zcp/lib/commands/iterate.sh` - Add `--service` flag, focused iteration logic

---

## Implementation Order

### Phase 1: Quick Wins (Low effort, high value)
1. **Gap 32** - Port detection in verify.sh
2. **Gap 44** - Queue detection in status.sh

### Phase 2: Multi-Service Enhancement
3. **Gap 50** - Single-service iteration
4. **Gap 45** - Deploy order in discovery

### Phase 3: Advanced Features
5. **Gap 46** - Migration guidance and tracking
6. **Gap 48** - Connectivity check script

---

## Testing Checklist

### Gap 44: Concurrent Deploy
- [ ] Start two deploys to same service simultaneously
- [ ] Verify `status.sh --wait` shows queue status
- [ ] Verify cancellation is detected when first fails
- [ ] Verify `.processes[]` structure matches expected format

### Gap 45: Deploy Order
- [ ] Create multi-service discovery with `deploy_order`
- [ ] Verify `workflow.sh show` displays correct order
- [ ] Verify guidance shows sequential deploy commands
- [ ] **FIXED**: Verify step numbers increment correctly (was: all showed "Step 1:")

### Gap 46: Shared Database
- [ ] Create multi-service setup with shared database
- [ ] Verify `shared_database` flag detected
- [ ] Verify migration guidance shown in DEVELOP phase
- [ ] **FIXED**: Verify guidance shows `$appVersionId` usage (not "migrate-v1")
- [ ] Test that migrations re-run on each new deploy

### Gap 48: Service Communication
- [ ] Run `check-connectivity.sh` between two services
- [ ] Test DNS resolution failure case
- [ ] Test port closed failure case
- [ ] Test successful connectivity
- [ ] **FIXED**: Test on containers without `getent` (should use fallbacks)
- [ ] **FIXED**: Test on containers with `sh` instead of `bash`

### Gap 32: Wrong Port
- [ ] Verify with wrong port, check for zerops.yml hint
- [ ] Verify with correct port from zerops.yml
- [ ] **FIXED**: Test with multi-setup zerops.yml (dev vs prod have different ports)
- [ ] Verify correct port extracted when hostname contains "dev" vs "stage"

### Gap 50: Single-Service Iteration
- [ ] Run `iterate --service apidev "fix auth"`
- [ ] Verify only apidev evidence archived
- [ ] Verify other services' evidence preserved
- [ ] **FIXED**: Verify symlinks cleaned up (no dangling /tmp/dev_verify.json)
- [ ] Test matching by stage.name (not just dev.name)

---

---

## Verification Notes (v1.1.0)

This document was triple-verified on 2026-01-30. The following bugs were identified and fixed:

### Critical Bugs Fixed

| Gap | Issue | Fix |
|-----|-------|-----|
| 32 | yq query `.zerops[0].run.ports[0].port` assumed first setup is correct | Added setup name matching with fallbacks |
| 46 | `zsc execOnce "migrate-v1"` requires manual version bumping | Changed to `$appVersionId` for automatic refresh |

### Logic Bugs Fixed

| Gap | Issue | Fix |
|-----|-------|-----|
| 45 | While loop in pipe loses `$i` variable (all steps show "Step 1:") | Changed to process substitution `< <(jq ...)` |

### Portability Issues Fixed

| Gap | Issue | Fix |
|-----|-------|-----|
| 48 | `getent hosts` not available on all containers | Added fallbacks: nslookup, host |
| 48 | `/dev/tcp` is bash-specific | Added fallbacks: nc, curl |
| 50 | Moving verify file leaves dangling symlinks | Added symlink cleanup logic |

### Minor Improvements

| Gap | Issue | Fix |
|-----|-------|-----|
| 44 | `.processes[]` interpretation unclear | Added explicit status filtering |
| 50 | Service validation only returned dev.name | Now shows which role (dev/stage) matched |

---

**Document Version:** 1.1.0
**Last Updated:** 2026-01-30
**Verified:** Triple-verified with bug fixes applied
