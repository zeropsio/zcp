#!/bin/bash
# Service planning commands for Zerops Workflow

cmd_plan_services() {
    local runtime="$1"
    local managed_service="${2:-}"

    if [ -z "$runtime" ]; then
        echo "❌ Usage: .zcp/workflow.sh plan_services {runtime} [managed-service]"
        echo ""
        echo "Supported runtimes: go, nodejs, php, python, rust, bun"
        echo ""
        echo "Example: .zcp/workflow.sh plan_services go postgresql"
        return 1
    fi

    # Check Gate 0 first
    if [ ! -f "$RECIPE_REVIEW_FILE" ]; then
        echo "❌ BLOCKED: Recipe review required first"
        echo ""
        echo "Run: .zcp/recipe-search.sh quick $runtime $managed_service"
        return 1
    fi

    local session_id
    session_id=$(get_session)
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Extract patterns from recipe review based on runtime
    local prod_base dev_base dev_os build_cmd
    case "$runtime" in
        go|golang)
            prod_base="alpine@3.21"
            dev_base="go@1"
            dev_os="ubuntu"
            build_cmd="go build -o app main.go"
            ;;
        nodejs|node)
            prod_base="nodejs@22"
            dev_base="nodejs@22"
            dev_os="ubuntu"
            build_cmd="npm install && npm run build"
            ;;
        php)
            prod_base="php@8.3"
            dev_base="php@8.3"
            dev_os="ubuntu"
            build_cmd="composer install"
            ;;
        python)
            prod_base="python@3.12"
            dev_base="python@3.12"
            dev_os="ubuntu"
            build_cmd="pip install -r requirements.txt"
            ;;
        rust)
            prod_base="alpine@3.21"
            dev_base="rust@1"
            dev_os="ubuntu"
            build_cmd="cargo build --release"
            ;;
        bun)
            prod_base="bun@1"
            dev_base="bun@1"
            dev_os="ubuntu"
            build_cmd="bun install && bun run build"
            ;;
        *)
            prod_base="${runtime}@1"
            dev_base="${runtime}@1"
            dev_os="ubuntu"
            build_cmd="echo 'Build command not set'"
            ;;
    esac

    # Override with recipe patterns if available
    if command -v jq &>/dev/null && [ -f "$RECIPE_REVIEW_FILE" ]; then
        local recipe_prod recipe_dev recipe_os
        recipe_prod=$(jq -r ".patterns_extracted.runtime_patterns.${runtime}.prod_runtime_base // \"\"" "$RECIPE_REVIEW_FILE" 2>/dev/null)
        recipe_dev=$(jq -r ".patterns_extracted.runtime_patterns.${runtime}.dev_runtime_base // \"\"" "$RECIPE_REVIEW_FILE" 2>/dev/null)
        recipe_os=$(jq -r ".patterns_extracted.runtime_patterns.${runtime}.dev_os // \"\"" "$RECIPE_REVIEW_FILE" 2>/dev/null)
        [ -n "$recipe_prod" ] && [ "$recipe_prod" != "null" ] && prod_base="$recipe_prod"
        [ -n "$recipe_dev" ] && [ "$recipe_dev" != "null" ] && dev_base="$recipe_dev"
        [ -n "$recipe_os" ] && [ "$recipe_os" != "null" ] && dev_os="$recipe_os"
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Service Planning: $runtime${managed_service:+ + $managed_service}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "Based on recipe patterns, planning:"
    echo ""
    echo "  Runtime Services:"
    echo "    • appdev  (${dev_base}, zeropsSetup: dev)"
    echo "    • appstage (${dev_base}, zeropsSetup: prod)"
    echo ""

    local db_version="17"
    if [ -n "$managed_service" ]; then
        echo "  Managed Services:"
        echo "    • db (${managed_service}@${db_version}, mode: NON_HA)"
        echo ""
    fi

    echo "  Setups:"
    echo "    • dev:  full source, $dev_os, zsc noop --silent"
    echo "    • prod: binary only, $prod_base, auto-start"
    echo ""

    # Create evidence file using jq for proper JSON (safe escaping)
    # Build services array with jq to prevent injection issues
    local services_json
    if [ -n "$managed_service" ]; then
        services_json=$(jq -n \
            --arg db "$dev_base" \
            --arg ms "${managed_service}@${db_version}" \
            '[
                {hostname: "appdev", type: $db, zeropsSetup: "dev", purpose: "Development with full source code", enableSubdomainAccess: true},
                {hostname: "appstage", type: $db, zeropsSetup: "prod", purpose: "Staging/production with binary only", enableSubdomainAccess: true},
                {hostname: "db", type: $ms, mode: "NON_HA", purpose: "Database for all environments"}
            ]')
    else
        services_json=$(jq -n \
            --arg db "$dev_base" \
            '[
                {hostname: "appdev", type: $db, zeropsSetup: "dev", purpose: "Development with full source code", enableSubdomainAccess: true},
                {hostname: "appstage", type: $db, zeropsSetup: "prod", purpose: "Staging/production with binary only", enableSubdomainAccess: true}
            ]')
    fi

    local plan_json
    plan_json=$(jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --arg rt "$runtime" \
        --arg ms "$managed_service" \
        --arg pb "$prod_base" \
        --arg db "$dev_base" \
        --arg do "$dev_os" \
        --arg bc "$build_cmd" \
        --argjson svcs "$services_json" \
        '{
            session_id: $sid,
            timestamp: $ts,
            runtime: $rt,
            managed_service: (if $ms == "" then null else $ms end),
            services: $svcs,
            setups: {
                dev: {
                    build_base: $db,
                    build_commands: [$bc],
                    cache: true,
                    deploy_files: ".",
                    runtime_base: $db,
                    runtime_os: $do,
                    start: "zsc noop --silent"
                },
                prod: {
                    build_base: $db,
                    build_commands: [$bc],
                    cache: true,
                    deploy_files: "./app",
                    runtime_base: $pb,
                    start: "./app"
                }
            },
            validation: {
                dev_vs_prod_different: true,
                cache_enabled_both: true,
                prod_uses_alpine: (if ($pb | contains("alpine")) then true else false end),
                dev_uses_noop: true
            }
        }')

    safe_write_json "$SERVICE_PLAN_FILE" "$plan_json"

    echo "✓ Service plan created: $SERVICE_PLAN_FILE"
    echo ""
    echo "Next: Create import.yml based on this plan, then:"
    echo "  .zcp/workflow.sh extend import.yml"
}

cmd_snapshot_dev() {
    local version_name="${1:-}"

    # Check Gate 4 passed (uses existing dev_verify.json)
    if [ ! -f "$DEV_VERIFY_FILE" ]; then
        echo "❌ BLOCKED: Dev verification not complete"
        echo "Run: .zcp/verify.sh {dev} {port} / /status"
        return 1
    fi

    local failures
    failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
    if [ "$failures" != "0" ]; then
        echo "❌ BLOCKED: Dev tests have $failures failure(s)"
        echo "Fix failures before creating snapshot"
        return 1
    fi

    local session_id timestamp snapshot_id snapshot_type
    session_id=$(get_session)
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Create snapshot via git or timestamp
    if git rev-parse --git-dir > /dev/null 2>&1; then
        snapshot_id=$(git rev-parse HEAD 2>/dev/null || echo "no-git-$(date +%s)")
        snapshot_type="git_commit"
        echo "✓ Git snapshot: ${snapshot_id:0:8}"
    else
        snapshot_id="snapshot-$(date +%s)"
        snapshot_type="timestamp"
        echo "✓ Timestamp snapshot: $snapshot_id"
    fi

    local evidence
    evidence=$(jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --arg st "$snapshot_type" \
        --arg snap "$snapshot_id" \
        --arg vn "${version_name:-$snapshot_id}" \
        '{
            session_id: $sid,
            timestamp: $ts,
            snapshot_type: $st,
            snapshot_id: $snap,
            version_name: $vn,
            verified_working: true,
            ready_for_stage: true,
            note: "Snapshot allows rollback if stage deployment has issues"
        }')

    safe_write_json "$DEV_SNAPSHOT_FILE" "$evidence"

    echo ""
    echo "✓ Dev snapshot created: $DEV_SNAPSHOT_FILE"
    echo "  Snapshot ID: $snapshot_id"
    echo ""
    echo "Next: .zcp/workflow.sh transition_to DEPLOY"
}
