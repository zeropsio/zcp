#!/bin/bash
# Validate zerops.yml against recipe patterns
# Part of Gate 3: CONFIG_VALIDATION

set -o pipefail
# Note: -e not used - we want to continue checking all validations even if some fail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/utils.sh"
# Source state.sh for get_session function (FIX: sourcing bug)
source "$SCRIPT_DIR/lib/state.sh"

RECIPE_FILE="$RECIPE_REVIEW_FILE"
VALIDATION_OUTPUT="$CONFIG_VALIDATED_FILE"

validate_zerops_yml() {
    local yml_file="$1"
    local errors=0
    local warnings=0

    # Track checks as simple variables for JSON construction
    local check_zerops_wrapper="false"
    local check_separate_setups="false"
    local check_dev_cache="false"
    local check_prod_cache="false"
    local check_prod_alpine="false"
    local check_dev_noop="false"
    local check_dev_envvars="false"
    local check_prod_envvars="false"
    local check_granular_envvars="unknown"
    local check_dev_deploy_full="false"
    local check_prod_deploy_binary="false"

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Validating: $yml_file"
    echo "Against: $RECIPE_FILE"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    if [ ! -f "$yml_file" ]; then
        echo "❌ File not found: $yml_file"
        return 1
    fi

    if [ ! -f "$RECIPE_FILE" ]; then
        echo "⚠️  No recipe review found. Skipping pattern validation."
        echo "   Run: .zcp/recipe-search.sh quick {runtime}"
        return 0
    fi

    # Check 1: Has 'zerops:' top-level key
    if grep -q "^zerops:" "$yml_file"; then
        echo "  ✓ Has 'zerops:' top-level wrapper"
        check_zerops_wrapper="true"
    else
        echo "  ✗ Missing 'zerops:' top-level key"
        echo "    → Recipe pattern: zerops: -> - setup: NAME"
        ((errors++))
    fi

    # Check 2: Has separate dev and prod setups
    local has_dev has_prod
    has_dev=$(grep -c "setup: dev" "$yml_file" 2>/dev/null | tr -d '\n' || echo "0")
    has_prod=$(grep -c "setup: prod" "$yml_file" 2>/dev/null | tr -d '\n' || echo "0")
    # Ensure we have valid numbers
    [[ ! "$has_dev" =~ ^[0-9]+$ ]] && has_dev=0
    [[ ! "$has_prod" =~ ^[0-9]+$ ]] && has_prod=0

    if [ "$has_dev" -gt 0 ] && [ "$has_prod" -gt 0 ]; then
        echo "  ✓ Has separate dev and prod setups"
        check_separate_setups="true"
    else
        echo "  ✗ Missing dev or prod setup (dev=$has_dev, prod=$has_prod)"
        echo "    → Recipe pattern: Separate 'dev' and 'prod' setups required"
        ((errors++))
    fi

    # Check 3: cache: true in both setups
    if grep -A20 "setup: dev" "$yml_file" | grep -q "cache: true" 2>/dev/null; then
        echo "  ✓ Dev setup has cache: true"
        check_dev_cache="true"
    else
        echo "  ✗ Dev setup missing 'cache: true'"
        echo "    → Recipe pattern: cache: true (5-10x faster rebuilds)"
        ((errors++))
    fi

    if grep -A20 "setup: prod" "$yml_file" | grep -q "cache: true" 2>/dev/null; then
        echo "  ✓ Prod setup has cache: true"
        check_prod_cache="true"
    else
        echo "  ✗ Prod setup missing 'cache: true'"
        echo "    → Recipe pattern: cache: true (5-10x faster rebuilds)"
        ((errors++))
    fi

    # Check 4: Prod uses alpine runtime
    # Try yq first (more reliable), fall back to grep
    local prod_runtime=""
    if command -v yq &>/dev/null; then
        prod_runtime=$(yq -r '.zerops[] | select(.setup == "prod") | .run.base // .build.base // ""' "$yml_file" 2>/dev/null | head -1)
    fi
    # Fallback to grep if yq unavailable or returned empty
    if [ -z "$prod_runtime" ]; then
        prod_runtime=$(grep -A30 "setup: prod" "$yml_file" 2>/dev/null | grep -E "^\s+base:" | tail -1 | awk '{print $2}' 2>/dev/null || echo "")
    fi

    if [[ "$prod_runtime" =~ alpine ]]; then
        echo "  ✓ Prod runtime uses alpine ($prod_runtime)"
        check_prod_alpine="true"
    else
        echo "  ✗ Prod runtime not using alpine (found: ${prod_runtime:-none})"
        echo "    → Recipe pattern: base: alpine@3.21 (40x smaller, more secure)"
        ((errors++))
    fi

    # Check 5: Dev uses zsc noop
    if grep -A30 "setup: dev" "$yml_file" | grep -q "zsc noop" 2>/dev/null; then
        echo "  ✓ Dev setup uses 'zsc noop --silent'"
        check_dev_noop="true"
    else
        echo "  ⚠️  Dev setup not using 'zsc noop --silent' (manual control)"
        echo "    → Recipe pattern: start: zsc noop --silent"
        ((warnings++))
    fi

    # Check 6: Explicit envVariables
    if grep -A30 "setup: dev" "$yml_file" | grep -q "envVariables:" 2>/dev/null; then
        echo "  ✓ Dev setup has explicit envVariables"
        check_dev_envvars="true"
    else
        echo "  ✗ Dev setup missing explicit envVariables"
        echo "    → Recipe pattern: Declare env vars explicitly in zerops.yml"
        ((errors++))
    fi

    if grep -A30 "setup: prod" "$yml_file" | grep -q "envVariables:" 2>/dev/null; then
        echo "  ✓ Prod setup has explicit envVariables"
        check_prod_envvars="true"
    else
        echo "  ✗ Prod setup missing explicit envVariables"
        echo "    → Recipe pattern: Declare env vars explicitly in zerops.yml"
        ((errors++))
    fi

    # Check 7: Granular env vars (NOT connection string)
    if grep -A50 "envVariables:" "$yml_file" | grep -qE "DB_HOST:|db_hostname" 2>/dev/null; then
        echo "  ✓ Uses granular env vars (DB_HOST pattern)"
        check_granular_envvars="true"
    elif grep -A50 "envVariables:" "$yml_file" | grep -qiE "DATABASE_URL|CONNECTION_STRING|DSN" 2>/dev/null; then
        echo "  ✗ Uses connection string instead of granular vars"
        echo "    → Recipe pattern: Use DB_HOST, DB_PORT, DB_USER, DB_PASS, DB_NAME"
        echo "    → NOT: DATABASE_URL or connection strings"
        ((errors++))
        check_granular_envvars="false"
    else
        echo "  ⚠️  Could not verify env var pattern (no DB vars found)"
    fi

    # Check 8: Dev deployFiles is full source (.)
    local dev_deploy_files
    dev_deploy_files=$(grep -A20 "setup: dev" "$yml_file" | grep "deployFiles:" | head -1 | awk '{print $2}' 2>/dev/null)
    if [ "$dev_deploy_files" = "." ]; then
        echo "  ✓ Dev deploys full source (deployFiles: .)"
        check_dev_deploy_full="true"
    elif [ -n "$dev_deploy_files" ]; then
        echo "  ⚠️  Dev deployFiles is '$dev_deploy_files' (expected '.' for full source)"
        echo "    → Recipe pattern: Dev needs full source for iteration"
        ((warnings++))
    fi

    # Check 9: Prod deployFiles is binary only
    local prod_deploy_files
    prod_deploy_files=$(grep -A20 "setup: prod" "$yml_file" | grep "deployFiles:" | head -1 | awk '{print $2}' 2>/dev/null)
    if [[ "$prod_deploy_files" =~ ^\./[a-zA-Z] ]]; then
        echo "  ✓ Prod deploys binary only (deployFiles: $prod_deploy_files)"
        check_prod_deploy_binary="true"
    elif [ "$prod_deploy_files" = "." ]; then
        echo "  ✗ Prod deploys full source (should be binary only)"
        echo "    → Recipe pattern: deployFiles: ./app (binary only for prod)"
        ((errors++))
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $errors error(s), $warnings warning(s)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Create evidence file using jq
    local session_id timestamp
    session_id=$(get_session)
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local evidence
    evidence=$(jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --arg file "$yml_file" \
        --argjson errors "$errors" \
        --argjson warnings "$warnings" \
        --argjson zw "$check_zerops_wrapper" \
        --argjson ss "$check_separate_setups" \
        --argjson dc "$check_dev_cache" \
        --argjson pc "$check_prod_cache" \
        --argjson pa "$check_prod_alpine" \
        --argjson dn "$check_dev_noop" \
        --argjson de "$check_dev_envvars" \
        --argjson pe "$check_prod_envvars" \
        --arg ge "$check_granular_envvars" \
        --argjson df "$check_dev_deploy_full" \
        --argjson pb "$check_prod_deploy_binary" \
        '{
            session_id: $sid,
            timestamp: $ts,
            file_validated: $file,
            errors: $errors,
            warnings: $warnings,
            checks: {
                zerops_wrapper: $zw,
                separate_setups: $ss,
                dev_cache: $dc,
                prod_cache: $pc,
                prod_alpine: $pa,
                dev_noop: $dn,
                dev_envvars: $de,
                prod_envvars: $pe,
                granular_envvars: $ge,
                dev_deploy_full: $df,
                prod_deploy_binary: $pb
            },
            all_checks_passed: ($errors == 0)
        }')

    safe_write_json "$VALIDATION_OUTPUT" "$evidence"

    if [ $errors -eq 0 ]; then
        echo ""
        echo "✓ Validation passed"
        echo "✓ Evidence created: $VALIDATION_OUTPUT"
        return 0
    else
        echo ""
        echo "❌ Validation failed - fix errors before deploying"
        echo ""
        echo "Reference: $RECIPE_FILE"
        return 1
    fi
}

# Main
if [ -z "$1" ] || [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    cat <<'EOF'
Usage: .zcp/validate-config.sh {zerops.yml path}

Validates zerops.yml against recipe patterns from Gate 0.

Checks performed:
  1. Has 'zerops:' top-level wrapper
  2. Has separate dev and prod setups
  3. Both setups have cache: true
  4. Prod uses alpine runtime
  5. Dev uses zsc noop --silent
  6. Both have explicit envVariables
  7. Uses granular env vars (not connection strings)
  8. Dev deploys full source
  9. Prod deploys binary only

Example:
  .zcp/validate-config.sh /var/www/goapp/zerops.yml

Evidence file: /tmp/config_validated.json
EOF
    exit 0
fi

validate_zerops_yml "$1"
