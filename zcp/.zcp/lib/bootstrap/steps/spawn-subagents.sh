#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/spawn-subagents.sh
# Step: Output comprehensive instructions for spawning code generation subagents
#
# This is the CRITICAL handoff point where context must be FULLY transferred.
# Subagents are spawned fresh with NO prior conversation context.
# Everything they need must be EMBEDDED in the prompt, not referenced.
#
# Inputs: bootstrap_handoff.json from finalize step
# Outputs: Self-contained subagent instructions with complete context

# Build environment variable section from DISCOVERED data
# No hardcoding - uses actual discovery results from discover-services step
# This replaces the old build_env_var_mappings() which assumed what vars existed
build_env_var_section() {
    local discovery_file="${ZCP_TMP_DIR:-/tmp}/service_discovery.json"

    if [ ! -f "$discovery_file" ]; then
        echo "**WARNING**: No service discovery data found. Run discover-services step first."
        echo ""
        echo "Falling back to common patterns - verify these exist in your environment:"
        return
    fi

    local services
    services=$(jq -r '.services | keys[]' "$discovery_file" 2>/dev/null)

    if [ -z "$services" ]; then
        echo "No managed services discovered."
        return
    fi

    local result=""

    for svc in $services; do
        local vars has_pass has_user has_conn has_db var_list

        # Get list of variables for this service
        var_list=$(jq -r --arg s "$svc" '.services[$s].variables[]' "$discovery_file" 2>/dev/null | tr '\n' ', ' | sed 's/,$//')

        if [ -z "$var_list" ]; then
            continue
        fi

        # Check what's available (from discovery metadata or by checking var names)
        has_pass=$(jq -r --arg s "$svc" '.services[$s].has_password // (.services[$s].variables | map(select(endswith("_password") or endswith("_pass"))) | length > 0)' "$discovery_file" 2>/dev/null)
        has_user=$(jq -r --arg s "$svc" '.services[$s].has_user // (.services[$s].variables | map(select(endswith("_user"))) | length > 0)' "$discovery_file" 2>/dev/null)
        has_conn=$(jq -r --arg s "$svc" '.services[$s].has_connection_string // (.services[$s].variables | map(select(contains("connectionString"))) | length > 0)' "$discovery_file" 2>/dev/null)
        has_db=$(jq -r --arg s "$svc" '.services[$s].has_db_name // (.services[$s].variables | map(select(endswith("_dbName"))) | length > 0)' "$discovery_file" 2>/dev/null)

        result+="
### ${svc}

**Available variables** (discovered from running service):
\`\`\`
${var_list}
\`\`\`
"

        # Add guidance based on what's ACTUALLY available
        if [ "$has_conn" = "true" ]; then
            result+="
**Recommendation**: Use \`\${${svc}_connectionString}\` - it contains everything needed for connection.
"
        fi

        if [ "$has_pass" = "false" ]; then
            result+="
**Note**: No password variable exists - this service runs without authentication in the private network.
"
        fi

        result+="
**In zerops.yml** (use ONLY the variables that exist above):
\`\`\`yaml
envVariables:
  # Map to your app's expected variable names
  # Example: YOUR_VAR_NAME: \${${svc}_variableName}
"

        # Add specific examples based on what exists
        if echo "$var_list" | grep -q "${svc}_hostname"; then
            result+="  HOST: \${${svc}_hostname}
"
        fi
        if echo "$var_list" | grep -q "${svc}_port"; then
            result+="  PORT: \${${svc}_port}
"
        fi
        if [ "$has_user" = "true" ]; then
            result+="  USER: \${${svc}_user}
"
        fi
        if [ "$has_pass" = "true" ]; then
            result+="  PASS: \${${svc}_password}
"
        fi
        if [ "$has_db" = "true" ]; then
            result+="  DB_NAME: \${${svc}_dbName}
"
        fi
        if [ "$has_conn" = "true" ]; then
            result+="  # Or use connection string: \${${svc}_connectionString}
"
        fi

        result+="\`\`\`
"
    done

    echo "$result"
}

# Legacy wrapper for compatibility - calls new discovery-based function
build_env_var_mappings() {
    local managed_services="$1"
    # Try discovery first
    local discovery_result
    discovery_result=$(build_env_var_section)

    if [ -n "$discovery_result" ] && ! echo "$discovery_result" | grep -q "WARNING"; then
        echo "$discovery_result"
        return
    fi

    # Fallback to managed_services data if discovery not available
    local managed_count
    managed_count=$(echo "$managed_services" | jq 'length')

    if [ "$managed_count" -eq 0 ]; then
        echo "No managed services configured."
        return
    fi

    local result="**Note**: Using plan data (discovery not run). Verify variables exist before using.
"
    local i=0
    while [ "$i" -lt "$managed_count" ]; do
        local svc_name svc_type hostname
        svc_name=$(echo "$managed_services" | jq -r ".[$i].name")
        svc_type=$(echo "$managed_services" | jq -r ".[$i].type")
        hostname="$svc_name"

        result+="
### ${svc_name} (${svc_type})

Common variables (verify these exist):
- \${${hostname}_hostname}
- \${${hostname}_port}
"
        case "$svc_type" in
            postgresql*|mysql*|mariadb*|mongodb*)
                result+="- \${${hostname}_user}
- \${${hostname}_password}
- \${${hostname}_dbName}
- \${${hostname}_connectionString}
"
                ;;
            valkey*|keydb*|redis*)
                result+="- \${${hostname}_connectionString}
**Note**: May not have password in private network - check discovery.
"
                ;;
            nats*|rabbitmq*)
                result+="- \${${hostname}_user}
- \${${hostname}_password}
- \${${hostname}_connectionString}
"
                ;;
        esac
        i=$((i + 1))
    done

    echo "$result"
}

# Build runtime-specific guidance (P3: uses runtime-specific recipe file)
# This now just points to the fetched files - no hardcoded patterns
build_runtime_guidance() {
    local runtime="$1"
    local runtime_version="$2"
    local recipe_file="${3:-}"  # P3: Now accepts recipe file path

    local recipe_source
    if [ -n "$recipe_file" ] && [ -f "$recipe_file" ]; then
        recipe_source="**Recipe file**: \`${recipe_file}\` ✓ (read this for ${runtime}-specific patterns)"
    else
        recipe_source="**Recipe file**: Not found - check documentation"
    fi

    cat <<GUIDANCE
### ${runtime} Runtime (${runtime_version})

${recipe_source}

**Documentation**: https://docs.zerops.io/${runtime}/how-to/build-pipeline

**General workflow** (via SSH, inside /var/www):
1. Read the recipe file for ${runtime}-specific build commands and structure
2. Initialize dependencies (runtime-specific - check recipe file)
3. Verify it builds/runs locally
4. Then proceed with zcli push
GUIDANCE
}

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

        local dev_hostname stage_hostname dev_id stage_id mount_path runtime runtime_version recipe_file
        dev_hostname=$(echo "$handoff" | jq -r '.dev_hostname')
        stage_hostname=$(echo "$handoff" | jq -r '.stage_hostname')
        dev_id=$(echo "$handoff" | jq -r '.dev_id')
        stage_id=$(echo "$handoff" | jq -r '.stage_id')
        mount_path=$(echo "$handoff" | jq -r '.mount_path')
        runtime=$(echo "$handoff" | jq -r '.runtime')
        runtime_version=$(echo "$handoff" | jq -r '.runtime_version // "'"${runtime}@1"'"')
        # P3: Get runtime-specific recipe file
        recipe_file=$(echo "$handoff" | jq -r '.recipe_file // ""')

        # Get managed services for env var mappings
        local managed_services
        managed_services=$(echo "$handoff" | jq -c '.managed_services // []')
        local env_var_mappings
        env_var_mappings=$(build_env_var_mappings "$managed_services")

        # Get runtime-specific guidance (P3: passes recipe_file)
        local runtime_guidance
        runtime_guidance=$(build_runtime_guidance "$runtime" "$runtime_version" "$recipe_file")

        # Build managed services list for zerops.yml template
        local managed_env_block=""
        local managed_count
        managed_count=$(echo "$managed_services" | jq 'length')

        if [ "$managed_count" -gt 0 ]; then
            local j=0
            while [ "$j" -lt "$managed_count" ]; do
                local svc_name env_prefix hostname
                svc_name=$(echo "$managed_services" | jq -r ".[$j].name")
                env_prefix=$(echo "$managed_services" | jq -r ".[$j].env_prefix")
                hostname="$svc_name"

                managed_env_block+="        ${env_prefix}_HOST: \${${hostname}_hostname}
        ${env_prefix}_PORT: \${${hostname}_port}
        ${env_prefix}_USER: \${${hostname}_user}
        ${env_prefix}_PASS: \${${hostname}_password}
"
                # Add DB name for databases
                local svc_type
                svc_type=$(echo "$managed_services" | jq -r ".[$j].type")
                case "$svc_type" in
                    postgresql*|mysql*|mariadb*|mongodb*)
                        managed_env_block+="        ${env_prefix}_NAME: \${${hostname}_dbName}
"
                        ;;
                esac
                j=$((j + 1))
            done
        fi

        # ============================================================
        # EXTRACT PATTERNS FROM RECIPE (recipe-search.sh provides these)
        # Recipe search falls back to docs automatically - no hardcoding needed
        # ============================================================
        local recipe_patterns
        recipe_patterns=$(echo "$handoff" | jq -c '.recipe_patterns // {}')

        # Extract from patterns_extracted.runtime_patterns.$runtime
        local patterns_for_runtime
        patterns_for_runtime=$(echo "$recipe_patterns" | jq -c --arg rt "$runtime" '.patterns_extracted.runtime_patterns[$rt] // {}')

        # VALIDATION: Check if we actually have patterns for this runtime (Issue 7)
        if [ "$patterns_for_runtime" = "{}" ] || [ "$patterns_for_runtime" = "null" ]; then
            local available_patterns
            available_patterns=$(echo "$recipe_patterns" | jq -r '.patterns_extracted.runtime_patterns | keys | join(", ")' 2>/dev/null || echo "none")
            echo "WARNING: No recipe patterns found for runtime '$runtime'" >&2
            echo "  Available patterns: $available_patterns" >&2
            echo "  Subagent should use documentation: https://docs.zerops.io/${runtime}/how-to/build-pipeline" >&2
        fi

        # Extract from patterns_extracted.dev_vs_prod
        local dev_vs_prod
        dev_vs_prod=$(echo "$recipe_patterns" | jq -c '.patterns_extracted.dev_vs_prod // {}')

        # Get configuration guidance and source info
        local config_guidance pattern_source
        config_guidance=$(echo "$recipe_patterns" | jq -r '.configuration_guidance // ""')
        pattern_source=$(echo "$recipe_patterns" | jq -r '.pattern_source // "unknown"')

        # Check for fetched files
        local fetched_recipe_file="${ZCP_TMP_DIR:-/tmp}/fetched_recipe.md"
        local fetched_docs_file="${ZCP_TMP_DIR:-/tmp}/fetched_docs.md"
        local has_fetched_recipe="false"
        local has_fetched_docs="false"
        [ -f "$fetched_recipe_file" ] && has_fetched_recipe="true"
        [ -f "$fetched_docs_file" ] && has_fetched_docs="true"

        # Extract values from recipe patterns (all come from recipe-search.sh)
        local prod_runtime_base dev_runtime_base dev_start prod_start dev_os

        # From patterns_extracted.runtime_patterns.$runtime
        dev_runtime_base=$(echo "$patterns_for_runtime" | jq -r '.dev_runtime_base // "'"${runtime_version}"'"')
        prod_runtime_base=$(echo "$patterns_for_runtime" | jq -r '.prod_runtime_base // "'"${runtime_version}"'"')
        dev_os=$(echo "$patterns_for_runtime" | jq -r '.dev_os // "ubuntu"')

        # From patterns_extracted.dev_vs_prod
        dev_start=$(echo "$dev_vs_prod" | jq -r '.dev.start // "zsc noop --silent"')
        prod_start=$(echo "$dev_vs_prod" | jq -r '.prod.start // "./app"')

        # Build recipe source info for the prompt
        local recipe_source_info=""
        case "$pattern_source" in
            recipe_hello_world|recipe_framework|recipe_api)
                recipe_source_info="
**PATTERN SOURCE: Zerops recipe API (${pattern_source})**

The file \`/tmp/fetched_recipe.md\` contains a reference zerops.yml for ${runtime}.

⚠️ **DO NOT COPY IT VERBATIM** - Use it as a reference to understand:
- What base images to use (build and run)
- What build commands are typical for ${runtime}
- What files to deploy and cache
- The general structure of dev vs prod setups

Then **construct YOUR OWN zerops.yml** adapted to your specific application needs.

Configuration guidance: ${config_guidance}"
                ;;
            documentation)
                recipe_source_info="
**PATTERN SOURCE: Documentation (docs.zerops.io)**

The file \`/tmp/fetched_docs.md\` contains examples for ${runtime}.

⚠️ **These are examples, not templates** - Use them to understand:
- Correct base images and versions
- Typical build commands for ${runtime}
- Expected deploy files and cache configuration

Then **construct YOUR OWN zerops.yml** adapted to your specific application.

Configuration guidance: ${config_guidance}"
                ;;
            *)
                recipe_source_info="
**Pattern source: ${pattern_source}**

Check files in /tmp/ for fetched patterns, or see:
https://docs.zerops.io/${runtime}/how-to/build-pipeline

Use these as references to construct your own zerops.yml."
                ;;
        esac

        # Build the subagent prompt - optimized for signal density
        local prompt
        prompt=$(cat <<PROMPT
# Bootstrap: ${dev_hostname} / ${stage_hostname}

You are bootstrapping a Zerops service pair. This prompt is self-contained—no prior context exists.

## Environment

You're on **ZCP** (control plane), not inside app containers.

| Location | Path | How to use |
|----------|------|------------|
| ZCP (here) | \`${mount_path}/\` | Write files directly |
| Container | \`/var/www/\` | \`ssh ${dev_hostname} "cd /var/www && ..."\` |

Files written to \`${mount_path}/\` appear at \`/var/www/\` inside the container.

## Your Services

| Role | Hostname | ID | zerops.yml setup |
|------|----------|----|------------------|
| Dev | ${dev_hostname} | ${dev_id} | \`dev\` |
| Stage | ${stage_hostname} | ${stage_id} | \`prod\` |

**⚠️ Setup names are \`dev\` and \`prod\`—NOT hostnames.** This is the #1 mistake.

## Managed Services - DISCOVERED Environment Variables

**CRITICAL**: The following variables were discovered from the ACTUAL running services.
Use ONLY the variables listed below. Do NOT assume other variables exist.
If a variable isn't listed (e.g., password), the service doesn't require it.

${env_var_mappings:-None.}

## Runtime: ${runtime} (${runtime_version})
${recipe_source_info}

**Recipe file:** \`${recipe_file}\`

If the recipe file doesn't exist, use documentation:
https://docs.zerops.io/${runtime}/how-to/build-pipeline

## zerops.yml Template

\`\`\`yaml
zerops:
  - setup: dev
    build:
      base: ${runtime_version}
      buildCommands: [<from recipe>]
      deployFiles: [.]
      cache: true
    run:
      base: ${runtime_version}
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
${managed_env_block:-        # (none)}
      start: ${dev_start}

  - setup: prod
    build:
      base: ${runtime_version}
      buildCommands: [<from recipe>]
      deployFiles: [<binary>]
      cache: true
    run:
      base: ${prod_runtime_base}
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
${managed_env_block:-        # (none)}
      start: ${prod_start}
\`\`\`

## Tasks

| # | Task | Command/Action |
|---|------|----------------|
| 1 | Read recipe | \`cat ${recipe_file}\` — understand ${runtime} patterns |
| 2 | Write zerops.yml | To \`${mount_path}/zerops.yml\` — setups: \`dev\`, \`prod\` |
| 3 | Write app code | HTTP server on :8080 with \`/\`, \`/health\`, \`/status\` |
| 4 | Init deps | \`ssh ${dev_hostname} "cd /var/www && <init>"\` |
| 5 | Write .gitignore | \`.zcp/lib/gitignore-template.sh > ${mount_path}/.gitignore\` |
| 6 | Git init | \`ssh ${dev_hostname} "cd /var/www && git config --global user.email 'zcp@zerops.io' && git config --global user.name 'ZCP' && git init && git add -A && git commit -m 'Bootstrap'"\` |
| 7 | Deploy dev | \`ssh ${dev_hostname} 'cd /var/www && zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "\$ZEROPS_ZCP_API_KEY" && zcli push ${dev_id} --setup=dev --deploy-git-folder'\` |
| 8 | Wait dev | \`.zcp/status.sh --wait ${dev_hostname}\` |
| 9 | Subdomain dev | \`zcli service enable-subdomain -P \$projectId ${dev_id}\` |
| 10 | Start dev server | SSH in, run appropriate start command for runtime (see below) |
| 11 | Wait for server | \`.zcp/wait-for-server.sh ${dev_hostname} 8080 120\` — waits up to 120s |
| 12 | Verify dev | \`.zcp/verify.sh ${dev_hostname} 8080 / /health /status\` — includes port preflight |
| 13 | Deploy stage | \`ssh ${dev_hostname} 'cd /var/www && zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "\$ZEROPS_ZCP_API_KEY" && zcli push ${stage_id} --setup=prod'\` |
| 14 | Wait stage | \`.zcp/status.sh --wait ${stage_hostname}\` |
| 15 | Subdomain stage | \`zcli service enable-subdomain -P \$projectId ${stage_id}\` |
| 16 | Verify stage | \`.zcp/verify.sh ${stage_hostname} 8080 / /health /status\` |
| 17 | **Done** | \`.zcp/mark-complete.sh ${dev_hostname}\` — completion evidence auto-generated |

## CRITICAL: Task 10 — Dev Server Manual Start

Dev setup uses \`start: zsc noop --silent\` — **nothing runs automatically**.

After deploy + subdomain (tasks 7-9), the container is running but **port 8080 has nothing listening**.

**Start command by runtime:**
| Runtime | Command |
|---------|---------|
| Go | \`ssh ${dev_hostname} "cd /var/www && nohup go run . > /tmp/app.log 2>&1 &"\` |
| Node.js | \`ssh ${dev_hostname} "cd /var/www && nohup node index.js > /tmp/app.log 2>&1 &"\` |
| Python | \`ssh ${dev_hostname} "cd /var/www && nohup python app.py > /tmp/app.log 2>&1 &"\` |

**Then wait (Task 11)** — first startup may take 60-120s for dependency download:
\`\`\`bash
.zcp/wait-for-server.sh ${dev_hostname} 8080 120
\`\`\`

**If verify.sh reports "NO SERVER LISTENING"** → server not running → start it first.

**Stage is different**: Stage uses \`start: ./app\` (or equivalent) — Zerops runs it automatically.

## Platform Rules

**Generated files — NEVER write these:**
- Lock files: \`go.sum\`, \`bun.lock\`, \`package-lock.json\`, \`yarn.lock\`, \`Cargo.lock\`
- Dependency directories: \`node_modules/\`, \`vendor/\`, \`.venv/\`

Write manifests only (package.json, go.mod). Let package managers generate locks via SSH commands.

**Getting service URLs:**
\`\`\`bash
# CORRECT — URL is an env var inside the container
ssh ${dev_hostname} 'echo \$zeropsSubdomain'

# WRONG — zcli service list does NOT return URLs
zcli service list  # No URLs here!
\`\`\`

**Tool availability:**
| Tool | ZCP | Containers |
|------|-----|------------|
| jq | ✅ | ❌ |
| curl | ✅ | ✅ |
| psql/redis-cli | ✅ | ❌ |
| netstat/ss | ❌ | ✅ |

Run data processing (jq) on ZCP: \`ssh svc "curl ..." \| jq .\`

**zcli service commands:** \`list\`, \`push\`, \`deploy\`, \`enable-subdomain\`, \`log\`, \`start\`, \`stop\`
— no \`get\`/\`info\`/\`status\`. Use \`.zcp/status.sh\` for status.

**Database tools** (psql, redis-cli): Run from ZCP, not via SSH.

**Long SSH commands**: Set \`run_in_background=true\` if >30s.

**Never** \`env\`/\`printenv\` — leaks secrets. Fetch specific vars only.

## Recovery

| Problem | Fix |
|---------|-----|
| "not a git repository" | \`ssh ${dev_hostname} "cd /var/www && git config --global user.email 'zcp@zerops.io' && git config --global user.name 'ZCP' && git init && git add -A && git commit -m 'Fix'"\` |
| "unauthenticated" | Re-run the combined auth+push command (Task 7 or 13) |
| **HTTP 000 on dev** | **Server not running — see Task 10, start it manually** |
| Server won't start | \`.zcp/wait-for-server.sh ${dev_hostname} 8080\` then check logs |
| Endpoints fail (not 000) | \`zcli service log -P \$projectId ${dev_id}\` |

## Done

After \`.zcp/mark-complete.sh\` succeeds → end session. Main agent handles aggregation.
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
            --arg recipe_file "$recipe_file" \
            --arg prompt "$prompt" \
            --argjson handoff "$handoff" \
            '{
                hostname: $hostname,
                stage_hostname: $stage_hostname,
                dev_id: $dev_id,
                stage_id: $stage_id,
                mount_path: $mount_path,
                runtime: $runtime,
                recipe_file: $recipe_file,
                handoff: $handoff,
                subagent_prompt: $prompt,
                tasks: [
                    "Create zerops.yml with dev/prod setups (NOT hostname names!)",
                    "Create application code (HTTP server on 8080)",
                    "Initialize runtime dependencies (go mod init, npm init, etc.)",
                    "Write .gitignore (prevents node_modules from being committed)",
                    "Initialize git repository",
                    "Deploy to dev with fresh auth: ssh \($hostname) 'cd /var/www && zcli login ... && zcli push \($dev_id) --setup=dev'",
                    "Wait for dev: .zcp/status.sh --wait \($hostname)",
                    "Enable dev subdomain",
                    "Start dev server manually (nohup ... &)",
                    "Wait for server: .zcp/wait-for-server.sh \($hostname) 8080 120",
                    "Verify dev: .zcp/verify.sh \($hostname) 8080 / /health /status",
                    "Deploy to stage with fresh auth: ssh \($hostname) 'cd /var/www && zcli login ... && zcli push \($stage_id) --setup=prod'",
                    "Wait for stage: .zcp/status.sh --wait \($stage_hostname)",
                    "Enable stage subdomain",
                    "Verify stage: .zcp/verify.sh \($stage_hostname) 8080 / /health /status",
                    "Mark complete: .zcp/mark-complete.sh \($hostname) (completion evidence auto-generated)"
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
            critical_notes: [
                "Subagents receive COMPLETE context in their prompt - no file references",
                "Setup names are dev/prod, NOT hostnames like appdev/appstage",
                "zcli must be authenticated INSIDE the container via SSH",
                "Git init required before zcli push",
                "Always use --setup=dev or --setup=prod with zcli push",
                "Write files to mount_path, but SSH commands use /var/www"
            ],
            recovery_if_pending: [
                "If aggregate-results shows pending services:",
                "1. Verify files exist: ls /var/www/{hostname}/zerops.yml",
                "2. Mark complete manually: .zcp/mark-complete.sh {hostname}",
                "3. Re-run: .zcp/bootstrap.sh step aggregate-results"
            ]
        }')

    record_step "spawn-subagents" "complete" "$data"

    local msg
    if [ "$count" -eq 1 ]; then
        msg="Spawn 1 subagent with comprehensive bootstrap instructions"
    else
        msg="Spawn $count subagents - comprehensive instructions for each service pair"
    fi

    json_response "spawn-subagents" "$msg" "$data" "aggregate-results"
}

export -f step_spawn_subagents build_env_var_mappings build_env_var_section build_runtime_guidance
