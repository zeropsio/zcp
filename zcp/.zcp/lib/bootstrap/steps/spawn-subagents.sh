#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/spawn-subagents.sh
# Step: Output instructions for agent to spawn subagents for code generation
#
# This step doesn't execute code - it outputs instructions for the main agent
# to spawn Claude subagents via the Task tool. Each subagent handles one
# service pair (dev + stage) through the complete code generation and
# deployment cycle.
#
# Inputs: bootstrap_handoff.json from finalize step
# Outputs: Subagent instructions with handoff data

step_spawn_subagents() {
    local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"

    if [ ! -f "$handoff_file" ]; then
        json_error "spawn-subagents" "No handoff file found" '{}' '["Run finalize step first: .zcp/bootstrap.sh step finalize"]'
        return 1
    fi

    local handoffs
    handoffs=$(jq -c '.service_handoffs' "$handoff_file")

    if [ "$handoffs" = "null" ] || [ -z "$handoffs" ]; then
        json_error "spawn-subagents" "No service handoffs in handoff file" '{}' '["Re-run finalize step"]'
        return 1
    fi

    local count
    count=$(echo "$handoffs" | jq 'length')

    if [ "$count" -eq 0 ]; then
        json_error "spawn-subagents" "Empty service handoffs array" '{}' '["Check bootstrap plan and re-run finalize"]'
        return 1
    fi

    # Build subagent instructions for each service pair
    local instructions='[]'
    local i=0

    while [ "$i" -lt "$count" ]; do
        local handoff
        handoff=$(echo "$handoffs" | jq ".[$i]")

        local dev_hostname stage_hostname dev_id stage_id mount_path runtime
        dev_hostname=$(echo "$handoff" | jq -r '.dev_hostname')
        stage_hostname=$(echo "$handoff" | jq -r '.stage_hostname')
        dev_id=$(echo "$handoff" | jq -r '.dev_id')
        stage_id=$(echo "$handoff" | jq -r '.stage_id')
        mount_path=$(echo "$handoff" | jq -r '.mount_path')
        runtime=$(echo "$handoff" | jq -r '.runtime')

        # Build managed services description for prompt
        local managed_desc=""
        local managed_services
        managed_services=$(echo "$handoff" | jq -c '.managed_services // []')
        local managed_count
        managed_count=$(echo "$managed_services" | jq 'length')

        if [ "$managed_count" -gt 0 ]; then
            managed_desc=$(echo "$managed_services" | jq -r '[.[] | "\(.name) (\(.type)) - env prefix: \(.env_prefix)"] | join(", ")')
        fi

        # Build the subagent prompt
        local prompt
        prompt=$(cat <<PROMPT
You are bootstrapping service pair: ${dev_hostname}/${stage_hostname}

## Handoff Data
$(echo "$handoff" | jq -M .)

## Recipe Patterns
Available in: /tmp/recipe_review.json

## Tasks

1. **Create zerops.yml** at ${mount_path}/zerops.yml
   - Use recipe_patterns for correct build/run structure
   - Port: 8080
   - Include envs for managed services (${managed_desc:-"none"})
   - Create separate dev and prod setups

2. **Deploy config to dev**
   \`\`\`bash
   ssh ${dev_hostname} "zcli push ${dev_id}"
   \`\`\`
   Wait for RUNNING status

3. **Enable subdomain on dev**
   \`\`\`bash
   zcli service enable-subdomain -P \$projectId ${dev_id}
   \`\`\`

4. **Generate minimal status page code**
   - Simple HTTP server on port 8080
   - GET / returns HTML welcome page
   - GET /health returns {"status": "ok"}
   - GET /status returns {"status": "ok", "services": {...}} with connection test to each managed service
   - Use the env vars defined in zerops.yml

5. **Test dev**
   \`\`\`bash
   .zcp/verify.sh ${dev_hostname} 8080 / /health /status
   \`\`\`

6. **Deploy to stage** (push from dev since code is in the mounted dev workspace)
   \`\`\`bash
   ssh ${dev_hostname} "zcli push ${stage_id}"
   \`\`\`

7. **Enable subdomain on stage**
   \`\`\`bash
   zcli service enable-subdomain -P \$projectId ${stage_id}
   \`\`\`

8. **Test stage via subdomain**

9. **Mark complete**
   \`\`\`bash
   source .zcp/lib/bootstrap/state.sh
   set_service_state "${dev_hostname}" "phase" "complete"
   \`\`\`
PROMPT
)

        local instruction
        instruction=$(jq -n \
            --arg hostname "$dev_hostname" \
            --arg stage_hostname "$stage_hostname" \
            --arg dev_id "$dev_id" \
            --arg stage_id "$stage_id" \
            --arg mount_path "$mount_path" \
            --arg runtime "$runtime" \
            --arg prompt "$prompt" \
            --argjson handoff "$handoff" \
            '{
                hostname: $hostname,
                stage_hostname: $stage_hostname,
                dev_id: $dev_id,
                stage_id: $stage_id,
                mount_path: $mount_path,
                runtime: $runtime,
                handoff: $handoff,
                subagent_prompt: $prompt,
                tasks: [
                    "Create zerops.yml using recipe_patterns",
                    "Deploy config to dev: ssh \($hostname) \"zcli push \($dev_id)\"",
                    "Enable subdomain on dev",
                    "Generate minimal status page code with managed service connection tests",
                    "Test dev: .zcp/verify.sh \($hostname) 8080 / /health /status",
                    "Deploy to stage (from dev workspace): ssh \($hostname) \"zcli push \($stage_id)\"",
                    "Enable subdomain on stage",
                    "Test stage via subdomain",
                    "Mark complete: set_service_state \($hostname) phase complete"
                ]
            }')

        instructions=$(echo "$instructions" | jq --argjson i "$instruction" '. + [$i]')
        i=$((i + 1))
    done

    # Build response data
    local data
    data=$(jq -n \
        --argjson instructions "$instructions" \
        --argjson count "$count" \
        '{
            subagent_count: $count,
            instructions: $instructions,
            spawn_method: "Use Task tool with subagent_type=general-purpose for each service pair",
            parallel_execution: "Launch all subagents in parallel for maximum efficiency",
            after_all_complete: "Run: .zcp/bootstrap.sh step aggregate-results",
            notes: [
                "Each subagent receives full handoff data and recipe patterns",
                "Subagents work independently on their service pair",
                "Use set_service_state to track progress",
                "aggregate-results step checks all services are complete"
            ]
        }')

    record_step "spawn-subagents" "complete" "$data"

    local msg
    if [ "$count" -eq 1 ]; then
        msg="Spawn 1 subagent for code generation"
    else
        msg="Spawn $count subagents - one per service pair (can run in parallel)"
    fi

    json_response "spawn-subagents" "$msg" "$data" "aggregate-results"
}

export -f step_spawn_subagents
