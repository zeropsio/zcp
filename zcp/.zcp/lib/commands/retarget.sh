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

    # Get current values
    local old_name old_id
    old_name=$(jq -r ".${target_env}.name // \"?\"" "$DISCOVERY_FILE" 2>/dev/null)
    old_id=$(jq -r ".${target_env}.id // \"?\"" "$DISCOVERY_FILE" 2>/dev/null)

    # Update discovery.json
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    if ! jq --arg id "$service_id" --arg name "$service_name" --arg ts "$timestamp" \
        ".${target_env}.id = \$id | .${target_env}.name = \$name | .timestamp = \$ts" \
        "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp"; then
        echo "❌ Failed to update discovery.json"
        rm -f "${DISCOVERY_FILE}.tmp"
        return 1
    fi

    mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"

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
        echo "  .zcp/verify.sh $service_name {port} / /api/..."
    else
        echo "Re-verify on new dev target:"
        echo "  ssh $service_name \"{build_command}\""
        echo "  .zcp/verify.sh $service_name {port} / /api/..."
    fi
}
