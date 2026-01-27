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

# Build environment variable mappings for managed services
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
        local svc_name svc_type env_prefix hostname
        svc_name=$(echo "$managed_services" | jq -r ".[$i].name")
        svc_type=$(echo "$managed_services" | jq -r ".[$i].type")
        env_prefix=$(echo "$managed_services" | jq -r ".[$i].env_prefix")

        # Derive hostname from service name
        hostname="$svc_name"

        result+="
### ${svc_name} (${svc_type})
Zerops provides these variables from the '${hostname}' service:
- \${${hostname}_hostname} → Host address
- \${${hostname}_port} → Port number
- \${${hostname}_user} → Username
- \${${hostname}_password} → Password"

        # Add database-specific vars
        case "$svc_type" in
            postgresql*|mysql*|mariadb*|mongodb*)
                result+="
- \${${hostname}_dbName} → Database name
- \${${hostname}_connectionString} → Full connection string"
                ;;
        esac

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

# Build runtime-specific guidance
# This now just points to the fetched files - no hardcoded patterns
build_runtime_guidance() {
    local runtime="$1"
    local runtime_version="$2"

    cat <<GUIDANCE
### ${runtime} Runtime (${runtime_version})

**Configuration source**: Check the fetched files for authoritative patterns:
- \`/tmp/fetched_recipe.md\` - If a recipe was found
- \`/tmp/fetched_docs.md\` - If using documentation fallback

**Documentation**: https://docs.zerops.io/${runtime}/how-to/build-pipeline

**General workflow** (via SSH, inside /var/www):
1. Initialize dependencies (runtime-specific - check fetched files)
2. Verify it builds/runs locally
3. Then proceed with zcli push
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

        local dev_hostname stage_hostname dev_id stage_id mount_path runtime runtime_version
        dev_hostname=$(echo "$handoff" | jq -r '.dev_hostname')
        stage_hostname=$(echo "$handoff" | jq -r '.stage_hostname')
        dev_id=$(echo "$handoff" | jq -r '.dev_id')
        stage_id=$(echo "$handoff" | jq -r '.stage_id')
        mount_path=$(echo "$handoff" | jq -r '.mount_path')
        runtime=$(echo "$handoff" | jq -r '.runtime')
        runtime_version=$(echo "$handoff" | jq -r '.runtime_version // "'"${runtime}@1"'"')

        # Get managed services for env var mappings
        local managed_services
        managed_services=$(echo "$handoff" | jq -c '.managed_services // []')
        local env_var_mappings
        env_var_mappings=$(build_env_var_mappings "$managed_services")

        # Get runtime-specific guidance (uses hardcoded fallback for prerequisites)
        local runtime_guidance
        runtime_guidance=$(build_runtime_guidance "$runtime" "$runtime_version")

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

                managed_env_block+="    ${env_prefix}_HOST: \${${hostname}_hostname}
    ${env_prefix}_PORT: \${${hostname}_port}
    ${env_prefix}_USER: \${${hostname}_user}
    ${env_prefix}_PASS: \${${hostname}_password}
"
                # Add DB name for databases
                local svc_type
                svc_type=$(echo "$managed_services" | jq -r ".[$j].type")
                case "$svc_type" in
                    postgresql*|mysql*|mariadb*|mongodb*)
                        managed_env_block+="    ${env_prefix}_NAME: \${${hostname}_dbName}
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

        # Build the comprehensive subagent prompt
        local prompt
        prompt=$(cat <<PROMPT
# Bootstrap Subagent: ${dev_hostname}/${stage_hostname}

You are a subagent spawned to bootstrap a service pair. You have NO prior conversation context.
Everything you need is in this prompt. Read it completely before acting.

---

## SECTION 1: ENVIRONMENT CONTEXT

### Where You Are Running
- You are on **ZCP** (Zerops Control Plane), a container with management tools
- You are NOT inside the application containers

### File Access
- **SSHFS Mount**: Files at \`${mount_path}/\` on ZCP are mounted from the dev container
- Write files here: \`${mount_path}/zerops.yml\`, \`${mount_path}/main.go\`, etc.
- These files appear at \`/var/www/\` INSIDE the container (no hostname prefix!)

### Command Execution
- **Local commands** (on ZCP): \`zcli\`, \`curl\`, \`.zcp/verify.sh\`
- **Remote commands** (in container): \`ssh ${dev_hostname} "command"\`
- Build/run commands MUST go through SSH

### Critical Path Difference
| Location | Path |
|----------|------|
| On ZCP (where you write files) | \`${mount_path}/\` |
| Inside container (via SSH) | \`/var/www/\` |

Example:
- Write: \`${mount_path}/main.go\` (on ZCP)
- Build: \`ssh ${dev_hostname} "cd /var/www && go build"\` (via SSH, note /var/www not ${mount_path})

---

## SECTION 2: DOMAIN MODEL

### Key Concepts

| Term | Meaning | Example |
|------|---------|---------|
| **hostname** | Service identifier in Zerops | \`${dev_hostname}\`, \`${stage_hostname}\` |
| **service_id** | UUID for zcli commands | \`${dev_id}\` |
| **setup** | Named config block in zerops.yml | \`dev\`, \`prod\` (NOT hostnames!) |
| **mount_path** | Where files appear on ZCP | \`${mount_path}\` |

### CRITICAL: Setup Names vs Hostnames

**Setups are SEMANTIC names, NOT hostnames!**

\`\`\`yaml
# CORRECT - semantic names
zerops:
  - setup: dev     # ← Semantic name for development config
  - setup: prod    # ← Semantic name for production config

# WRONG - DO NOT use hostnames as setup names
zerops:
  - setup: ${dev_hostname}    # ← WRONG!
  - setup: ${stage_hostname}  # ← WRONG!
\`\`\`

The link between hostname and setup happens in import.yml via \`zeropsSetup:\`:
- Service \`${dev_hostname}\` uses \`zeropsSetup: dev\`
- Service \`${stage_hostname}\` uses \`zeropsSetup: prod\`

### Your Service Pair

| Role | Hostname | Service ID | Uses Setup |
|------|----------|------------|------------|
| Development | \`${dev_hostname}\` | \`${dev_id}\` | \`dev\` |
| Production | \`${stage_hostname}\` | \`${stage_id}\` | \`prod\` |

---

## SECTION 3: MANAGED SERVICES

${env_var_mappings:-No managed services configured.}

---

## SECTION 4: RUNTIME-SPECIFIC GUIDANCE

Runtime: **${runtime}** (${runtime_version})

${runtime_guidance}

---

## SECTION 5: ANTI-PATTERNS (DO NOT DO THESE)

### ❌ WRONG: Using hostnames as setup names
\`\`\`yaml
# WRONG
- setup: ${dev_hostname}
- setup: ${stage_hostname}
\`\`\`

### ❌ WRONG: Running build commands on ZCP
\`\`\`bash
# WRONG - this runs on ZCP, not in container
cd ${mount_path} && go build
\`\`\`

### ❌ WRONG: Using mount_path inside SSH
\`\`\`bash
# WRONG - ${mount_path} doesn't exist inside container
ssh ${dev_hostname} "cd ${mount_path} && go build"
\`\`\`

### ❌ WRONG: Pushing without git init
\`\`\`bash
# WRONG - zcli push requires a git repo
ssh ${dev_hostname} "zcli push ${dev_id}"
\`\`\`

### ❌ WRONG: Pushing without --setup
\`\`\`bash
# WRONG - which setup to use?
ssh ${dev_hostname} "cd /var/www && zcli push ${dev_id}"
\`\`\`

### ❌ WRONG: Forgetting zcli authentication
\`\`\`bash
# WRONG - zcli may not be authenticated in container
ssh ${dev_hostname} "zcli push ..."
\`\`\`

### ✅ CORRECT Pattern
\`\`\`bash
# Authenticate zcli in container
ssh ${dev_hostname} 'zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "\$ZEROPS_ZCP_API_KEY"'

# Initialize git, commit, push with correct setup
ssh ${dev_hostname} "cd /var/www && git init && git add -A && git commit -m 'Bootstrap' && zcli push ${dev_id} --setup=dev"
\`\`\`

---

## SECTION 6: ZEROPS.YML CONFIGURATION
${recipe_source_info}

### Key Configuration Values (from recipe patterns)

| Setting | Dev Setup | Prod Setup |
|---------|-----------|------------|
| Runtime base | \`${runtime_version}\` | \`${prod_runtime_base}\` |
| Start command | \`${dev_start}\` | \`${prod_start}\` |
| Deploy files | Full source (\`.\`) | Built artifacts only |

### Required Structure

Create \`${mount_path}/zerops.yml\` with this structure:

\`\`\`yaml
zerops:
  # Development setup - full source, manual control
  - setup: dev
    build:
      base: ${runtime_version}
      # Copy buildCommands from /tmp/fetched_recipe.md if available
      buildCommands:
        - <see fetched recipe or runtime docs>
      deployFiles:
        - .
      cache: true
    run:
      os: ubuntu
      base: ${runtime_version}
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
${managed_env_block:-        # No managed services}
      start: ${dev_start}

  # Production setup - optimized for deployment
  - setup: prod
    build:
      base: ${runtime_version}
      buildCommands:
        - <see fetched recipe or runtime docs>
      deployFiles:
        - <binary or dist files>
      cache: true
    run:
      base: ${prod_runtime_base}
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
${managed_env_block:-        # No managed services}
      start: ${prod_start}
\`\`\`

**IMPORTANT**: Use \`/tmp/fetched_recipe.md\` as a REFERENCE to understand the patterns,
then construct your own zerops.yml adapted to your application's specific needs.

---

## SECTION 7: TASK SEQUENCE

Execute these tasks IN ORDER. Each has prerequisites and verification.

### Task 1: Create zerops.yml

**Action**: Write the zerops.yml file with correct structure

**Prerequisites**: None

**Steps**:
1. **FIRST**: Read \`/tmp/fetched_recipe.md\` (or \`/tmp/fetched_docs.md\`) as a REFERENCE
2. Study the patterns: base images, build commands, deploy files, cache settings
3. **Construct your own zerops.yml** using the structure in SECTION 6 + patterns you learned
4. **ALWAYS**: Setup names must be \`dev\` and \`prod\` (NOT hostnames like appdev/appstage!)
5. **ALWAYS**: Add your managed service env vars to both setups

**Write to**: \`${mount_path}/zerops.yml\`

**Verification**:
- File exists at ${mount_path}/zerops.yml
- Has exactly two setups named 'dev' and 'prod'
- Dev setup has \`start: ${dev_start}\`
- Prod setup has \`start: ${prod_start}\`

---

### Task 2: Create Application Code

**Action**: Write a minimal HTTP server with health endpoints

**Prerequisites**: Task 1 complete

**Requirements**:
- Listen on port 8080
- GET / → HTML welcome page
- GET /health → {"status": "ok"}
- GET /status → {"status": "ok", "service": "${dev_hostname}", "runtime": "${runtime}"}

**Verification**: Source files exist at ${mount_path}/

---

### Task 3: Create Runtime Dependencies

**Action**: Initialize package manager files

**Prerequisites**: Task 2 complete

**Command** (via SSH):
\`\`\`bash
ssh ${dev_hostname} "cd /var/www && <runtime-specific init command>"
\`\`\`

**Verification**: Dependency files exist (go.mod, package.json, etc.)

---

### Task 4: Authenticate zcli in Container

**Action**: Login zcli inside the dev container

**Prerequisites**: Task 3 complete

**Command**:
\`\`\`bash
ssh ${dev_hostname} 'zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "\$ZEROPS_ZCP_API_KEY"'
\`\`\`

**Verification**: Command exits without error

---

### Task 5: Initialize Git Repository

**Action**: Create git repo and initial commit

**Prerequisites**: Task 4 complete

**Command**:
\`\`\`bash
ssh ${dev_hostname} "cd /var/www && git init && git add -A && git commit -m 'Bootstrap ${dev_hostname}'"
\`\`\`

**Verification**: .git directory exists, commit created

---

### Task 6: Deploy to Dev

**Action**: Push code to dev service using the 'dev' setup

**Prerequisites**: Tasks 1-5 complete

**Command**:
\`\`\`bash
ssh ${dev_hostname} "cd /var/www && zcli push ${dev_id} --setup=dev"
\`\`\`

**Verification**: zcli reports successful deployment

---

### Task 7: Wait for Dev Deployment

**Action**: Wait until dev service is running

**Prerequisites**: Task 6 complete

**Command**:
\`\`\`bash
.zcp/status.sh --wait ${dev_hostname}
\`\`\`

**Verification**: Service status is RUNNING

---

### Task 8: Enable Dev Subdomain

**Action**: Enable public subdomain access for dev

**Prerequisites**: Task 7 complete

**Command**:
\`\`\`bash
zcli service enable-subdomain -P \$projectId ${dev_id}
\`\`\`

**Verification**: Command succeeds

---

### Task 9: Test Dev Endpoints

**Action**: Verify dev service responds correctly

**Prerequisites**: Task 8 complete

**Command**:
\`\`\`bash
.zcp/verify.sh ${dev_hostname} 8080 / /health /status
\`\`\`

**Verification**: All endpoints return expected responses

---

### Task 10: Deploy to Stage

**Action**: Push code to stage service using the 'prod' setup

**Prerequisites**: Task 9 complete (dev verified working)

**Command**:
\`\`\`bash
ssh ${dev_hostname} "cd /var/www && zcli push ${stage_id} --setup=prod"
\`\`\`

**Verification**: zcli reports successful deployment

---

### Task 11: Wait for Stage Deployment

**Action**: Wait until stage service is running

**Prerequisites**: Task 10 complete

**Command**:
\`\`\`bash
.zcp/status.sh --wait ${stage_hostname}
\`\`\`

**Verification**: Service status is RUNNING

---

### Task 12: Enable Stage Subdomain

**Action**: Enable public subdomain access for stage

**Prerequisites**: Task 11 complete

**Command**:
\`\`\`bash
zcli service enable-subdomain -P \$projectId ${stage_id}
\`\`\`

**Verification**: Command succeeds

---

### Task 13: Test Stage Endpoints

**Action**: Verify stage service responds correctly

**Prerequisites**: Task 12 complete

**Command**:
\`\`\`bash
.zcp/verify.sh ${stage_hostname} 8080 / /health /status
\`\`\`

**Verification**: All endpoints return expected responses

---

### Task 14: Mark Complete (CRITICAL)

**Action**: Signal completion to the main agent

**Prerequisites**: All previous tasks complete

**Command**:
\`\`\`bash
.zcp/mark-complete.sh ${dev_hostname}
\`\`\`

**Verification**: Command exits successfully

**NOTE**: If this fails, the main agent can detect completion by checking for zerops.yml and source files.

---

## SECTION 8: RECOVERY PROCEDURES

### If zcli push fails with "not a git repository"
\`\`\`bash
ssh ${dev_hostname} "cd /var/www && git init && git add -A && git commit -m 'Fix'"
\`\`\`

### If zcli push fails with "unauthenticated"
\`\`\`bash
ssh ${dev_hostname} 'zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "\$ZEROPS_ZCP_API_KEY"'
\`\`\`

### If build fails
1. Check zerops.yml syntax (proper YAML indentation)
2. Verify buildCommands are correct for ${runtime}
3. Test locally first: \`ssh ${dev_hostname} "cd /var/www && <build-command>"\`

### If endpoints don't respond
1. Check if process is running: \`ssh ${dev_hostname} "ps aux | grep app"\`
2. Check logs: \`zcli service log -P \$projectId ${dev_id}\`
3. Verify port 8080 is being used

---

## SECTION 9: SUCCESS CRITERIA

Your task is complete when:
1. ✅ zerops.yml exists with 'dev' and 'prod' setups (NOT hostname names!)
2. ✅ Application code exists and compiles/runs
3. ✅ Dev service deployed and responding at /health
4. ✅ Stage service deployed and responding at /health
5. ✅ .zcp/mark-complete.sh ${dev_hostname} has been run

---

## QUICK REFERENCE

| What | Command |
|------|---------|
| Write files | Direct to ${mount_path}/ |
| Run in container | ssh ${dev_hostname} "cd /var/www && ..." |
| Auth zcli | ssh ${dev_hostname} 'zcli login ... "\$ZEROPS_ZCP_API_KEY"' |
| Deploy to dev | ssh ${dev_hostname} "cd /var/www && zcli push ${dev_id} --setup=dev" |
| Deploy to stage | ssh ${dev_hostname} "cd /var/www && zcli push ${stage_id} --setup=prod" |
| Wait for deploy | .zcp/status.sh --wait {hostname} |
| Test endpoints | .zcp/verify.sh {hostname} 8080 / /health |
| Mark done | .zcp/mark-complete.sh ${dev_hostname} |

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
                    "Create zerops.yml with dev/prod setups (NOT hostname names!)",
                    "Create application code (HTTP server on 8080)",
                    "Initialize runtime dependencies (go mod init, npm init, etc.)",
                    "Authenticate zcli in container",
                    "Initialize git repository",
                    "Deploy to dev: ssh \($hostname) \"cd /var/www && zcli push \($dev_id) --setup=dev\"",
                    "Wait for dev: .zcp/status.sh --wait \($hostname)",
                    "Enable dev subdomain",
                    "Test dev: .zcp/verify.sh \($hostname) 8080 / /health /status",
                    "Deploy to stage: ssh \($hostname) \"cd /var/www && zcli push \($stage_id) --setup=prod\"",
                    "Wait for stage: .zcp/status.sh --wait \($stage_hostname)",
                    "Enable stage subdomain",
                    "Test stage: .zcp/verify.sh \($stage_hostname) 8080 / /health /status",
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
