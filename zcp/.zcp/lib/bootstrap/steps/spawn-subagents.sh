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

# Build environment variable mappings for managed services (P3: enhanced with topology)
# Returns a string describing how to reference each service's vars in zerops.yml
build_env_var_mappings() {
    local managed_services="$1"
    local managed_count
    managed_count=$(echo "$managed_services" | jq 'length')

    if [ "$managed_count" -eq 0 ]; then
        echo "No managed services configured."
        return
    fi

    local result=""
    local i=0
    while [ "$i" -lt "$managed_count" ]; do
        local svc_name svc_type env_prefix hostname env_vars reference_doc
        svc_name=$(echo "$managed_services" | jq -r ".[$i].name")
        svc_type=$(echo "$managed_services" | jq -r ".[$i].type")
        env_prefix=$(echo "$managed_services" | jq -r ".[$i].env_prefix")
        # P3: Get enhanced topology data
        env_vars=$(echo "$managed_services" | jq -r ".[$i].env_vars // []")
        reference_doc=$(echo "$managed_services" | jq -r ".[$i].reference_doc // \"\"")

        # Derive hostname from service name
        hostname="$svc_name"

        result+="
### ${svc_name} (${svc_type})
**Hostname:** ${hostname}
**Available env vars:** "

        # List all env vars dynamically
        local var_count var_idx=0
        var_count=$(echo "$env_vars" | jq 'length')
        while [ "$var_idx" -lt "$var_count" ]; do
            local var_name
            var_name=$(echo "$env_vars" | jq -r ".[$var_idx]")
            result+="\${${hostname}_${var_name}}"
            [ $((var_idx + 1)) -lt "$var_count" ] && result+=", "
            var_idx=$((var_idx + 1))
        done

        # P3: Add reference doc if available
        if [ -n "$reference_doc" ] && [ "$reference_doc" != "null" ] && [ -f "$reference_doc" ]; then
            result+="
**Reference:** \`${reference_doc}\` (read this for Zerops-specific patterns)"
        fi

        result+="

Map these in zerops.yml envVariables section as:
\`\`\`yaml
envVariables:
  ${env_prefix}_HOST: \${${hostname}_hostname}
  ${env_prefix}_PORT: \${${hostname}_port}
  ${env_prefix}_USER: \${${hostname}_user}
  ${env_prefix}_PASS: \${${hostname}_password}"

        case "$svc_type" in
            postgresql*|mysql*|mariadb*|mongodb*)
                result+="
  ${env_prefix}_NAME: \${${hostname}_dbName}"
                ;;
        esac

        result+="\`\`\`
"
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

## Managed Services
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
| 5 | Auth zcli | \`ssh ${dev_hostname} 'zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "\$ZEROPS_ZCP_API_KEY"'\` |
| 6 | Git init | \`ssh ${dev_hostname} "cd /var/www && git config --global user.email 'zcp@zerops.io' && git config --global user.name 'ZCP' && git init && git add -A && git commit -m 'Bootstrap'"\` |
| 7 | Deploy dev | \`ssh ${dev_hostname} "cd /var/www && zcli push ${dev_id} --setup=dev --deploy-git-folder"\` |
| 8 | Wait dev | \`.zcp/status.sh --wait ${dev_hostname}\` |
| 9 | Subdomain dev | \`zcli service enable-subdomain -P \$projectId ${dev_id}\` |
| 10 | Start dev server | SSH in, run appropriate start command for runtime (see below) |
| 10.5 | Verify port | \`ssh ${dev_hostname} "netstat -tlnp 2>/dev/null \| grep 8080 \|\| ss -tlnp \| grep 8080"\` — must show LISTEN |
| 11 | Test local | \`ssh ${dev_hostname} "curl -s http://localhost:8080/"\` — must return response |
| 12 | Verify dev | \`.zcp/verify.sh ${dev_hostname} 8080 / /health /status\` |
| 13 | Deploy stage | \`ssh ${dev_hostname} "cd /var/www && zcli push ${stage_id} --setup=prod"\` |
| 14 | Wait stage | \`.zcp/status.sh --wait ${stage_hostname}\` |
| 15 | Subdomain stage | \`zcli service enable-subdomain -P \$projectId ${stage_id}\` |
| 16 | Verify stage | \`.zcp/verify.sh ${stage_hostname} 8080 / /health /status\` |
| 17 | Write completion | Write \`/tmp/${dev_hostname}_complete.json\` (see below) |
| 18 | **Done** | \`.zcp/mark-complete.sh ${dev_hostname}\` — then end session |

## CRITICAL: Task 10 — Dev Server Manual Start

Dev setup uses \`start: zsc noop --silent\` — **nothing runs automatically**.

After deploy + subdomain (tasks 7-9), the container is running but **port 8080 has nothing listening**.

**Start command by runtime:**
| Runtime | Command |
|---------|---------|
| Go | \`ssh ${dev_hostname} "cd /var/www && nohup go run . > /tmp/app.log 2>&1 &"\` |
| Node.js | \`ssh ${dev_hostname} "cd /var/www && nohup node index.js > /tmp/app.log 2>&1 &"\` |
| Python | \`ssh ${dev_hostname} "cd /var/www && nohup python app.py > /tmp/app.log 2>&1 &"\` |

**Then verify (Tasks 10.5 and 11)** — port must show LISTEN, curl must return response.

**If verify.sh returns HTTP 000** → server not running → start it first.

**Stage is different**: Stage uses \`start: ./app\` (or equivalent) — Zerops runs it automatically.

## Task 17 — Write Completion JSON

Before calling mark-complete.sh, write \`/tmp/${dev_hostname}_complete.json\`:

\`\`\`json
{
  "dev_hostname": "${dev_hostname}",
  "stage_hostname": "${stage_hostname}",
  "dev_url": "<from ssh ${dev_hostname} 'echo \$zeropsSubdomain'>",
  "stage_url": "<from ssh ${stage_hostname} 'echo \$zeropsSubdomain'>",
  "verification": {
    "dev": {"passed": 3, "failed": 0},
    "stage": {"passed": 3, "failed": 0}
  },
  "completed_at": "<ISO timestamp>"
}
\`\`\`

This is the **single source of truth** — main agent reads this instead of re-testing.

## Platform Rules

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
| "unauthenticated" | Re-run Task 5 |
| **HTTP 000 on dev** | **Server not running — see Task 10, start it manually** |
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
                    "Authenticate zcli in container",
                    "Initialize git repository",
                    "Deploy to dev: ssh \($hostname) \"cd /var/www && zcli push \($dev_id) --setup=dev --deploy-git-folder\"",
                    "Wait for dev: .zcp/status.sh --wait \($hostname)",
                    "Enable dev subdomain",
                    "Start dev server manually (nohup ... &)",
                    "Verify port listening (netstat/ss)",
                    "Test local: ssh \($hostname) \"curl -s http://localhost:8080/\"",
                    "Verify dev: .zcp/verify.sh \($hostname) 8080 / /health /status",
                    "Deploy to stage: ssh \($hostname) \"cd /var/www && zcli push \($stage_id) --setup=prod\"",
                    "Wait for stage: .zcp/status.sh --wait \($stage_hostname)",
                    "Enable stage subdomain",
                    "Verify stage: .zcp/verify.sh \($stage_hostname) 8080 / /health /status",
                    "Write completion JSON: /tmp/\($hostname)_complete.json",
                    "Mark complete: .zcp/mark-complete.sh \($hostname)"
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

export -f step_spawn_subagents build_env_var_mappings build_runtime_guidance
