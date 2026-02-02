#!/bin/bash
# Retarget command for Zerops Workflow Continuity
# Changes deployment target without full reset

cmd_retarget() {
    local target_env="$1"
    local service_id="$2"
    local service_name="$3"

    # Validate arguments
    if [ -z "$target_env" ] || [ -z "$service_id" ] || [ -z "$service_name" ]; then
        echo "❌ Usage: .zcp/workflow.sh retarget {dev|stage} {service_id} {service_name}"
        echo ""
        echo "Example:"
        echo "  .zcp/workflow.sh retarget stage svc-abc123 api-stage"
        echo ""
        echo "This changes your deployment target without full reset."
        return 1
    fi

    # Validate target environment
    case "$target_env" in
        dev|stage)
            ;;
        *)
            echo "❌ Invalid target environment: $target_env"
            echo "   Valid: dev, stage"
            return 1
            ;;
    esac

    # Check discovery exists
    if [ ! -f "$DISCOVERY_FILE" ]; then
        echo "❌ No discovery.json found"
        echo ""
        echo "💡 Create discovery first:"
        echo "   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}"
        return 1
    fi

    # Get current values (CRITICAL-5: Use --arg for dynamic field access)
    local old_name old_id
    old_name=$(jq -r --arg env "$target_env" '.[$env].name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
    old_id=$(jq -r --arg env "$target_env" '.[$env].id // "?"' "$DISCOVERY_FILE" 2>/dev/null)

    # Update discovery.json
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # CRITICAL-5: Use --arg for target_env to prevent jq injection
    # Use PID-unique temp file (CRITICAL-4)
    local tmp_file="${DISCOVERY_FILE}.tmp.$$"
    if ! jq --arg env "$target_env" --arg id "$service_id" --arg name "$service_name" --arg ts "$timestamp" \
        '.[$env].id = $id | .[$env].name = $name | .timestamp = $ts' \
        "$DISCOVERY_FILE" > "$tmp_file"; then
        echo "❌ Failed to update discovery.json"
        rm -f "$tmp_file"
        return 1
    fi

    mv "$tmp_file" "$DISCOVERY_FILE"

    # Invalidate relevant evidence based on target
    local invalidated=()

    if [ "$target_env" = "dev" ]; then
        # Retargeting dev invalidates everything
        [ -f "$DEV_VERIFY_FILE" ] && rm -f "$DEV_VERIFY_FILE" && invalidated+=("dev_verify.json")
        [ -f "$DEPLOY_EVIDENCE_FILE" ] && rm -f "$DEPLOY_EVIDENCE_FILE" && invalidated+=("deploy_evidence.json")
        [ -f "$STAGE_VERIFY_FILE" ] && rm -f "$STAGE_VERIFY_FILE" && invalidated+=("stage_verify.json")
    else
        # Retargeting stage invalidates deploy and stage evidence only
        [ -f "$DEPLOY_EVIDENCE_FILE" ] && rm -f "$DEPLOY_EVIDENCE_FILE" && invalidated+=("deploy_evidence.json")
        [ -f "$STAGE_VERIFY_FILE" ] && rm -f "$STAGE_VERIFY_FILE" && invalidated+=("stage_verify.json")
    fi

    # Output summary
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║  RETARGET COMPLETE                                               ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Retargeted $target_env: $old_name → $service_name ($service_id)"
    echo ""

    if [ ${#invalidated[@]} -gt 0 ]; then
        echo "❌ Invalidated:"
        for f in "${invalidated[@]}"; do
            echo "   $f"
        done
        echo ""
    fi

    # Show what's preserved
    if [ "$target_env" = "stage" ] && [ -f "$DEV_VERIFY_FILE" ]; then
        echo "✅ Preserved:"
        echo "   dev_verify.json (code unchanged)"
        echo ""
    fi

    # Provide next steps
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "💡 NEXT STEPS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    local dev_name
    dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)

    if [ "$target_env" = "stage" ]; then
        echo "Deploy to new target:"
        echo "  ssh $dev_name \"zcli push $service_id --setup={setup}\""
        echo ""
        echo "Then wait and verify:"
        echo "  .zcp/status.sh --wait $service_name"
        echo "  ssh $service_name \"curl -s localhost:{port}/\""
        echo "  .zcp/verify.sh $service_name \"curl ok, logs clean\""
    else
        echo "Re-verify on new dev target:"
        echo "  ssh $service_name \"{build_command}\""
        echo "  ssh $service_name \"curl -s localhost:{port}/\""
        echo "  .zcp/verify.sh $service_name \"curl ok, logs clean\""
    fi
}
