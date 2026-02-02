#!/bin/bash
# Validate import.yml against recipe patterns
# Part of Gate 0.5: IMPORT_VALIDATION
#
# This gate exists because of a documented failure where an agent:
# - Read the recipe showing buildFromGit and zeropsSetup
# - Created import.yml WITHOUT these critical fields
# - Caused services to be stuck in READY_TO_DEPLOY (empty, no code)
#
# This validator enforces: "USE THE RECIPE'S IMPORT.YML - don't invent your own!"

set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/utils.sh"
# Source state.sh for get_session function (FIX: sourcing bug)
source "$SCRIPT_DIR/lib/state.sh"

# Colors are now defined in utils.sh

RECIPE_FILE="$RECIPE_REVIEW_FILE"
FETCHED_RECIPE="/tmp/fetched_recipe.md"
IMPORT_VALIDATED_FILE="/tmp/import_validated.json"

# Runtime types that REQUIRE code to function
RUNTIME_TYPES="go|golang|nodejs|node|php|python|rust|dotnet|java|bun|nginx|static|alpine"

# Managed service types that don't need code
MANAGED_TYPES="postgresql|mysql|mariadb|mongodb|valkey|redis|rabbitmq|elasticsearch|nats|minio|shared-storage"

get_service_category() {
    local type="$1"
    # Extract base type (before @)
    local base="${type%%@*}"

    if echo "$base" | grep -qE "^($MANAGED_TYPES)$"; then
        echo "managed"
    elif echo "$base" | grep -qE "^($RUNTIME_TYPES)$"; then
        echo "runtime"
    else
        echo "unknown"
    fi
}

validate_import_yml() {
    local import_file="$1"
    local zerops_yml="${2:-}"
    local errors=0
    local warnings=0
    local critical_errors=0

    # Track services and their validation status
    local services_validated=0
    local runtime_services_found=0
    local issues_json="[]"

    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}Import Validation: $import_file${NC}"
    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""

    if [ ! -f "$import_file" ]; then
        echo -e "${RED}❌ File not found: $import_file${NC}"
        return 1
    fi

    # Check for yq (required for YAML parsing)
    if ! command -v yq &>/dev/null; then
        echo -e "${RED}❌ yq is required for import validation${NC}"
        echo "   Install: brew install yq (macOS) or apt install yq (Linux)"
        return 1
    fi

    # Validate YAML syntax first
    if ! yq '.' "$import_file" > /dev/null 2>&1; then
        echo -e "${RED}❌ Invalid YAML syntax in: $import_file${NC}"
        return 1
    fi

    # Check if recipe review exists (Gate 0 should have run)
    local has_recipe_patterns=false
    local recipe_build_from_git=""
    local recipe_zerops_setups=""

    if [ -f "$RECIPE_FILE" ]; then
        has_recipe_patterns=true
        recipe_build_from_git=$(jq -r '.patterns_extracted.runtime_patterns | to_entries[0].value.build_from_git // empty' "$RECIPE_FILE" 2>/dev/null)
    fi

    # Check if we have fetched recipe with exact import.yml to compare
    local has_fetched_recipe=false
    local fetched_import_services=""

    if [ -f "$FETCHED_RECIPE" ]; then
        has_fetched_recipe=true
        echo -e "${CYAN}ℹ️  Comparing against fetched recipe: $FETCHED_RECIPE${NC}"
        echo ""
    fi

    # ==========================================================================
    # EXTRACT ALL SERVICES FROM IMPORT.YML
    # ==========================================================================

    local service_count
    service_count=$(yq '.services | length' "$import_file" 2>/dev/null || echo "0")

    if [ "$service_count" -eq 0 ]; then
        # Maybe it's a flat structure without 'services:' wrapper
        service_count=$(yq '. | length' "$import_file" 2>/dev/null || echo "0")
        if [ "$service_count" -eq 0 ]; then
            echo -e "${RED}❌ No services found in import.yml${NC}"
            return 1
        fi
    fi

    echo -e "${BOLD}Found $service_count service(s) to validate${NC}"
    echo ""

    # ==========================================================================
    # VALIDATE EACH SERVICE
    # ==========================================================================

    local i=0
    while [ $i -lt "$service_count" ]; do
        local hostname type zerops_setup build_from_git start_without_code enable_subdomain mode

        # Try both .services[i] and .[i] structures
        hostname=$(yq ".services[$i].hostname // .[$i].hostname // empty" "$import_file" 2>/dev/null)
        type=$(yq ".services[$i].type // .[$i].type // empty" "$import_file" 2>/dev/null)
        zerops_setup=$(yq ".services[$i].zeropsSetup // .[$i].zeropsSetup // empty" "$import_file" 2>/dev/null)
        build_from_git=$(yq ".services[$i].buildFromGit // .[$i].buildFromGit // empty" "$import_file" 2>/dev/null)
        start_without_code=$(yq ".services[$i].startWithoutCode // .[$i].startWithoutCode // empty" "$import_file" 2>/dev/null)

        if [ -z "$hostname" ] || [ "$hostname" = "null" ]; then
            ((i++))
            continue
        fi

        ((services_validated++))
        local category
        category=$(get_service_category "$type")

        echo -e "${BOLD}Service: $hostname${NC} (type: $type, category: $category)"

        # ----------------------------------------------------------------------
        # RUNTIME SERVICE VALIDATION
        # ----------------------------------------------------------------------
        if [ "$category" = "runtime" ]; then
            ((runtime_services_found++))
            local service_has_error=false

            # CHECK 1: buildFromGit OR startWithoutCode must be present
            if [ -z "$build_from_git" ] || [ "$build_from_git" = "null" ]; then
                if [ -z "$start_without_code" ] || [ "$start_without_code" = "null" ] || [ "$start_without_code" = "false" ]; then
                    echo -e "  ${RED}✗ CRITICAL: Missing buildFromGit AND startWithoutCode${NC}"
                    echo -e "    ${YELLOW}→ Service will be stuck in READY_TO_DEPLOY (empty container)${NC}"
                    echo -e "    ${YELLOW}→ Add: buildFromGit: <repo-url> (for initial code)${NC}"
                    echo -e "    ${YELLOW}→ Or:  startWithoutCode: true (for dev with SSHFS mount)${NC}"
                    ((critical_errors++))
                    service_has_error=true

                    # Check if recipe HAD buildFromGit
                    if [ -n "$recipe_build_from_git" ] && [ "$recipe_build_from_git" != "null" ]; then
                        echo -e "    ${RED}⚠️  Recipe shows: buildFromGit: $recipe_build_from_git${NC}"
                        echo -e "    ${RED}   You MUST use this field - don't omit it!${NC}"
                    fi

                    issues_json=$(echo "$issues_json" | jq --arg h "$hostname" --arg t "$type" \
                        '. + [{service: $h, type: $t, issue: "missing_code_source", severity: "critical",
                               message: "Runtime service has no buildFromGit and no startWithoutCode:true"}]')
                else
                    echo -e "  ${GREEN}✓ Has startWithoutCode: true${NC} (dev mode - needs SSHFS mount)"
                fi
            else
                echo -e "  ${GREEN}✓ Has buildFromGit: $build_from_git${NC}"
            fi

            # CHECK 2: zeropsSetup should be present for runtime services
            if [ -z "$zerops_setup" ] || [ "$zerops_setup" = "null" ]; then
                echo -e "  ${RED}✗ CRITICAL: Missing zeropsSetup${NC}"
                echo -e "    ${YELLOW}→ zeropsSetup links import.yml to zerops.yml setup blocks${NC}"
                echo -e "    ${YELLOW}→ Add: zeropsSetup: dev (or zeropsSetup: prod)${NC}"
                ((critical_errors++))
                service_has_error=true

                issues_json=$(echo "$issues_json" | jq --arg h "$hostname" --arg t "$type" \
                    '. + [{service: $h, type: $t, issue: "missing_zerops_setup", severity: "critical",
                           message: "Runtime service missing zeropsSetup field"}]')
            else
                echo -e "  ${GREEN}✓ Has zeropsSetup: $zerops_setup${NC}"

                # Validate zeropsSetup against zerops.yml if provided
                if [ -n "$zerops_yml" ] && [ -f "$zerops_yml" ]; then
                    local setup_exists
                    setup_exists=$(yq ".zerops[] | select(.setup == \"$zerops_setup\") | .setup" "$zerops_yml" 2>/dev/null | head -1)
                    if [ -z "$setup_exists" ]; then
                        echo -e "  ${YELLOW}⚠️  zeropsSetup '$zerops_setup' not found in $zerops_yml${NC}"
                        ((warnings++))
                    fi
                fi
            fi

            if [ "$service_has_error" = false ]; then
                echo -e "  ${GREEN}✓ Service configuration valid${NC}"
            fi

        # ----------------------------------------------------------------------
        # MANAGED SERVICE VALIDATION
        # ----------------------------------------------------------------------
        elif [ "$category" = "managed" ]; then
            # Managed services don't need buildFromGit or zeropsSetup
            echo -e "  ${GREEN}✓ Managed service - no code fields required${NC}"

            # Check mode is valid
            local mode
            mode=$(yq ".services[$i].mode // .[$i].mode // empty" "$import_file" 2>/dev/null)
            if [ -n "$mode" ] && [ "$mode" != "null" ]; then
                if [ "$mode" != "NON_HA" ] && [ "$mode" != "HA" ]; then
                    echo -e "  ${YELLOW}⚠️  Unknown mode: $mode (expected: NON_HA or HA)${NC}"
                    ((warnings++))
                else
                    echo -e "  ${GREEN}✓ Mode: $mode${NC}"
                fi
            fi
        else
            echo -e "  ${YELLOW}⚠️  Unknown service type: $type${NC}"
            ((warnings++))
        fi

        echo ""
        ((i++))
    done

    # ==========================================================================
    # COMPARE WITH FETCHED RECIPE (if available)
    # ==========================================================================

    if [ "$has_fetched_recipe" = true ] && [ $critical_errors -eq 0 ]; then
        echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo -e "${BOLD}Recipe Comparison${NC}"
        echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

        # Extract key patterns from fetched recipe
        local recipe_has_build_from_git
        recipe_has_build_from_git=$(grep -c "buildFromGit:" "$FETCHED_RECIPE" 2>/dev/null || echo "0")

        local import_has_build_from_git
        import_has_build_from_git=$(grep -c "buildFromGit:" "$import_file" 2>/dev/null || echo "0")

        if [ "$recipe_has_build_from_git" -gt 0 ] && [ "$import_has_build_from_git" -eq 0 ]; then
            echo -e "${RED}❌ Recipe has buildFromGit but your import.yml doesn't!${NC}"
            echo -e "   ${YELLOW}The recipe shows how services should be bootstrapped.${NC}"
            echo -e "   ${YELLOW}You're missing critical deployment configuration.${NC}"
            ((critical_errors++))
        elif [ "$recipe_has_build_from_git" -gt 0 ]; then
            echo -e "${GREEN}✓ buildFromGit usage matches recipe pattern${NC}"
        fi

        local recipe_has_zerops_setup
        recipe_has_zerops_setup=$(grep -c "zeropsSetup:" "$FETCHED_RECIPE" 2>/dev/null || echo "0")

        local import_has_zerops_setup
        import_has_zerops_setup=$(grep -c "zeropsSetup:" "$import_file" 2>/dev/null || echo "0")

        if [ "$recipe_has_zerops_setup" -gt 0 ] && [ "$import_has_zerops_setup" -eq 0 ]; then
            echo -e "${RED}❌ Recipe has zeropsSetup but your import.yml doesn't!${NC}"
            echo -e "   ${YELLOW}zeropsSetup links services to zerops.yml configuration.${NC}"
            ((critical_errors++))
        elif [ "$recipe_has_zerops_setup" -gt 0 ]; then
            echo -e "${GREEN}✓ zeropsSetup usage matches recipe pattern${NC}"
        fi

        echo ""
    fi

    # ==========================================================================
    # FINAL SUMMARY
    # ==========================================================================

    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}Validation Summary${NC}"
    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "  Services validated: $services_validated"
    echo "  Runtime services:   $runtime_services_found"
    echo "  Critical errors:    $critical_errors"
    echo "  Warnings:           $warnings"
    echo ""

    # Create evidence file
    local session_id timestamp
    session_id=$(get_session)
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local evidence
    evidence=$(jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --arg file "$import_file" \
        --argjson services "$services_validated" \
        --argjson runtime "$runtime_services_found" \
        --argjson critical "$critical_errors" \
        --argjson warnings "$warnings" \
        --argjson issues "$issues_json" \
        --argjson valid "$([ $critical_errors -eq 0 ] && echo true || echo false)" \
        '{
            session_id: $sid,
            timestamp: $ts,
            file_validated: $file,
            services_validated: $services,
            runtime_services: $runtime,
            critical_errors: $critical,
            warnings: $warnings,
            issues: $issues,
            all_checks_passed: $valid,
            validation_rules: [
                "Runtime services MUST have buildFromGit OR startWithoutCode:true",
                "Runtime services MUST have zeropsSetup to link to zerops.yml",
                "Fields must match recipe patterns when available"
            ]
        }')

    safe_write_json "$IMPORT_VALIDATED_FILE" "$evidence"

    if [ $critical_errors -eq 0 ]; then
        echo -e "${GREEN}✅ IMPORT VALIDATION PASSED${NC}"
        echo ""
        echo "Evidence created: $IMPORT_VALIDATED_FILE"
        echo ""
        echo -e "${BOLD}Why this matters:${NC}"
        echo "  • Runtime services need code to function"
        echo "  • buildFromGit provides initial deployment from repository"
        echo "  • zeropsSetup links import.yml to zerops.yml build/run config"
        echo "  • Without these, services are stuck in READY_TO_DEPLOY"
        return 0
    else
        echo -e "${RED}❌ IMPORT VALIDATION FAILED${NC}"
        echo ""
        echo -e "${BOLD}Common mistakes this gate catches:${NC}"
        echo "  1. Cherry-picking fields from recipe (missing buildFromGit)"
        echo "  2. Treating import.yml as 'service creation' only"
        echo "  3. Not understanding zeropsSetup links import→zerops.yml"
        echo ""
        echo -e "${BOLD}Fix:${NC}"
        if [ -f "$FETCHED_RECIPE" ]; then
            echo "  Copy the import.yml section from: $FETCHED_RECIPE"
            echo "  The recipe's import.yml is tested and complete."
        else
            echo "  Add missing fields to your import.yml"
            echo "  Or run: .zcp/recipe-search.sh quick {runtime}"
        fi
        echo ""
        echo -e "${YELLOW}⚠️  USE THE RECIPE'S IMPORT.YML - don't invent your own!${NC}"
        return 1
    fi
}

# ==========================================================================
# POST-IMPORT STATUS CHECK
# ==========================================================================

check_post_import_status() {
    local pid="${1:-}"

    if [ -z "$pid" ]; then
        pid=$(cat /tmp/projectId 2>/dev/null || echo "$projectId")
    fi

    if [ -z "$pid" ]; then
        echo "No project ID - skipping status check"
        return 0
    fi

    if ! command -v zcli &>/dev/null; then
        echo "zcli not found - skipping status check"
        return 0
    fi

    echo ""
    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}Post-Import Service Status Check${NC}"
    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""

    local services_json
    services_json=$(zcli service list -P "$pid" --format json 2>/dev/null | \
        sed 's/\x1b\[[0-9;]*m//g' | \
        awk '/^\s*[\{\[]/{found=1} found{print}')

    if [ -z "$services_json" ]; then
        echo "Could not fetch service list"
        return 0
    fi

    # Check for READY_TO_DEPLOY services (indicates missing buildFromGit)
    local ready_to_deploy
    ready_to_deploy=$(echo "$services_json" | jq -r '.services[] | select(.status == "READY_TO_DEPLOY") | .name' 2>/dev/null)

    if [ -n "$ready_to_deploy" ]; then
        echo -e "${RED}⚠️  SERVICES IN READY_TO_DEPLOY STATUS:${NC}"
        echo ""
        echo "$ready_to_deploy" | while read -r svc; do
            echo -e "  ${YELLOW}• $svc${NC}"
        done
        echo ""
        echo -e "${BOLD}What this means:${NC}"
        echo "  Services are created but have NO CODE deployed."
        echo "  This typically happens when import.yml is missing buildFromGit."
        echo ""
        echo -e "${BOLD}If this is intentional (dev with SSHFS):${NC}"
        echo "  1. Ensure startWithoutCode: true was in import.yml"
        echo "  2. Mount the service: .zcp/mount.sh {service}"
        echo "  3. Deploy code manually"
        echo ""
        echo -e "${BOLD}If this is NOT intentional:${NC}"
        echo "  1. Delete the services: zcli service delete -P $pid -S {service_id}"
        echo "  2. Fix import.yml to include buildFromGit"
        echo "  3. Re-import: .zcp/workflow.sh extend import.yml"
        return 1
    else
        echo -e "${GREEN}✓ No services stuck in READY_TO_DEPLOY${NC}"

        # Show service status summary
        local active building
        active=$(echo "$services_json" | jq -r '[.services[] | select(.status == "ACTIVE")] | length' 2>/dev/null)
        building=$(echo "$services_json" | jq -r '[.services[] | select(.status == "BUILDING")] | length' 2>/dev/null)

        echo "  Active: $active"
        [ "$building" -gt 0 ] && echo "  Building: $building"
    fi

    return 0
}

# ==========================================================================
# MAIN
# ==========================================================================

show_help() {
    cat <<'EOF'
Usage: .zcp/validate-import.sh <import.yml> [zerops.yml]

Validates import.yml against recipe patterns to prevent deployment failures.

WHAT THIS VALIDATES:
  1. Runtime services have buildFromGit OR startWithoutCode:true
  2. Runtime services have zeropsSetup (links to zerops.yml)
  3. Fields match fetched recipe patterns (if available)
  4. YAML syntax is valid

WHY THIS EXISTS:
  A documented failure occurred where an agent:
  - Read a recipe showing buildFromGit and zeropsSetup
  - Created import.yml WITHOUT these critical fields
  - Caused services to be stuck in READY_TO_DEPLOY

  This gate enforces: "USE THE RECIPE'S IMPORT.YML - don't invent your own!"

EXAMPLES:
  .zcp/validate-import.sh import.yml
  .zcp/validate-import.sh import.yml /var/www/app/zerops.yml

EVIDENCE FILE:
  Creates /tmp/import_validated.json for Gate 0.5 compliance

RELATED:
  .zcp/validate-config.sh   - Validates zerops.yml (Gate 3)
  .zcp/recipe-search.sh     - Fetches recipes (Gate 0)
EOF
}

main() {
    if [ -z "$1" ] || [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
        show_help
        exit 0
    fi

    if [ "$1" = "--check-status" ]; then
        check_post_import_status "$2"
        exit $?
    fi

    validate_import_yml "$1" "$2"
    exit $?
}

main "$@"
