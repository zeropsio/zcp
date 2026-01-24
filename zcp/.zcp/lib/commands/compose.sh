#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # Variables used by sourced scripts

# Bootstrap Synthesis Commands
# compose: Generate infrastructure from requirements
# verify_synthesis: Validate agent-created code

# =============================================================================
# SERVICE TYPE REGISTRY (bash 3.x compatible using functions)
# =============================================================================

# Get service type info: type|run_base|os (for runtimes) or type|default_mode|env_vars (for managed)
get_service_type_info() {
    local name="$1"
    case "$name" in
        # Runtimes
        go) echo "go@1|alpine@3.21|ubuntu" ;;
        nodejs) echo "nodejs@22|nodejs@22|ubuntu" ;;
        python) echo "python@3.12|python@3.12|ubuntu" ;;
        php) echo "php-nginx@8.4|php-nginx@8.4|ubuntu" ;;
        rust) echo "rust@1|alpine@3.21|ubuntu" ;;
        bun) echo "bun@1|bun@1|ubuntu" ;;
        java) echo "java@21|java@21|ubuntu" ;;
        dotnet) echo "dotnet@8|dotnet@8|ubuntu" ;;
        # Managed services
        postgresql) echo "postgresql@17|NON_HA|hostname,port,user,password,dbName,connectionString" ;;
        mysql) echo "mysql@8|NON_HA|hostname,port,user,password,dbName,connectionString" ;;
        mariadb) echo "mariadb@11|NON_HA|hostname,port,user,password,dbName,connectionString" ;;
        mongodb) echo "mongodb@7|NON_HA|hostname,port,user,password,dbName,connectionString" ;;
        valkey) echo "valkey@7.2|NON_HA|hostname,port,password,connectionString" ;;
        keydb) echo "keydb@6|NON_HA|hostname,port,password,connectionString" ;;
        rabbitmq) echo "rabbitmq@3|NON_HA|hostname,port,user,password,connectionString" ;;
        nats) echo "nats@2|NON_HA|hostname,port,user,password,connectionString" ;;
        elasticsearch) echo "elasticsearch@8|NON_HA|hostname,port,user,password" ;;
        minio) echo "minio@latest|NON_HA|accessKeyId,secretAccessKey,apiUrl,bucketName" ;;
        *) echo "" ;;
    esac
}

# Get environment variable mappings for managed services
get_env_mapping() {
    local name="$1"
    case "$name" in
        postgresql|mysql|mariadb) echo '{"DB_HOST":"${%s_hostname}","DB_PORT":"${%s_port}","DB_USER":"${%s_user}","DB_PASS":"${%s_password}","DB_NAME":"${%s_dbName}"}' ;;
        mongodb) echo '{"MONGO_HOST":"${%s_hostname}","MONGO_PORT":"${%s_port}","MONGO_USER":"${%s_user}","MONGO_PASS":"${%s_password}","MONGO_DB":"${%s_dbName}"}' ;;
        valkey|keydb) echo '{"REDIS_HOST":"${%s_hostname}","REDIS_PORT":"${%s_port}","REDIS_PASSWORD":"${%s_password}"}' ;;
        rabbitmq) echo '{"AMQP_HOST":"${%s_hostname}","AMQP_PORT":"${%s_port}","AMQP_USER":"${%s_user}","AMQP_PASS":"${%s_password}"}' ;;
        nats) echo '{"NATS_HOST":"${%s_hostname}","NATS_PORT":"${%s_port}","NATS_USER":"${%s_user}","NATS_PASS":"${%s_password}"}' ;;
        elasticsearch) echo '{"ES_HOST":"${%s_hostname}","ES_PORT":"${%s_port}","ES_USER":"${%s_user}","ES_PASS":"${%s_password}"}' ;;
        minio) echo '{"S3_ACCESS_KEY":"${%s_accessKeyId}","S3_SECRET_KEY":"${%s_secretAccessKey}","S3_ENDPOINT":"${%s_apiUrl}","S3_BUCKET":"${%s_bucketName}"}' ;;
        *) echo "" ;;
    esac
}

# Get conventional hostname for managed services
get_service_hostname() {
    local name="$1"
    case "$name" in
        postgresql|mysql|mariadb|mongodb) echo "db" ;;
        valkey|keydb) echo "cache" ;;
        rabbitmq|nats) echo "queue" ;;
        elasticsearch) echo "search" ;;
        minio) echo "storage" ;;
        *) echo "$name" ;;
    esac
}

# =============================================================================
# COMPOSE COMMAND
# =============================================================================

cmd_compose() {
    local runtime=""
    local services=""
    local mode=$(get_mode 2>/dev/null || echo "full")
    local hostname_prefix="api"
    local dev_only=false

    # Check if already in dev-only mode
    if [ "$mode" = "dev-only" ]; then
        dev_only=true
    fi

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --runtime|-r)
                runtime="$2"
                shift 2
                ;;
            --services|-s)
                services="$2"
                shift 2
                ;;
            --dev-only)
                dev_only=true
                shift
                ;;
            --hostname|-h)
                hostname_prefix="$2"
                # Validate hostname format (lowercase alphanumeric with hyphens, must start with letter)
                if [[ ! "$hostname_prefix" =~ ^[a-z][a-z0-9-]*$ ]]; then
                    cat <<EOF
{
  "error": "INVALID_HOSTNAME",
  "message": "Hostname prefix must start with a letter and contain only lowercase letters, numbers, and hyphens",
  "provided": "$hostname_prefix",
  "example": "api, myapp, web-service"
}
EOF
                    return 1
                fi
                shift 2
                ;;
            --help)
                show_compose_help
                return 0
                ;;
            *)
                echo "Unknown option: $1" >&2
                return 1
                ;;
        esac
    done

    # Validate runtime
    if [ -z "$runtime" ]; then
        cat <<EOF
{
  "error": "MISSING_RUNTIME",
  "message": "--runtime is required",
  "example": ".zcp/workflow.sh compose --runtime go --services postgresql",
  "valid_runtimes": ["go", "nodejs", "python", "php", "rust", "bun", "java", "dotnet"]
}
EOF
        return 1
    fi

    local runtime_info=$(get_service_type_info "$runtime")
    if [ -z "$runtime_info" ]; then
        cat <<EOF
{
  "error": "INVALID_RUNTIME",
  "message": "Unknown runtime: $runtime",
  "valid_runtimes": ["go", "nodejs", "python", "php", "rust", "bun", "java", "dotnet"]
}
EOF
        return 1
    fi

    local session=$(get_session 2>/dev/null || echo "unknown")
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Parse runtime info using function
    local rt_info=$(get_service_type_info "$runtime")
    local build_base=$(echo "$rt_info" | cut -d'|' -f1)
    local run_base=$(echo "$rt_info" | cut -d'|' -f2)

    # Parse managed services
    local managed_services=""
    local env_mapping="{}"
    local connections_list=""

    if [ -n "$services" ]; then
        IFS=',' read -ra svc_array <<< "$services"
        for svc in "${svc_array[@]}"; do
            # Handle HA mode: postgresql:HA
            local svc_name="${svc%%:*}"
            local svc_mode="NON_HA"
            if [[ "$svc" == *":HA"* ]]; then
                svc_mode="HA"
            fi

            local svc_info=$(get_service_type_info "$svc_name")
            if [ -z "$svc_info" ]; then
                cat <<EOF
{
  "error": "INVALID_SERVICE",
  "message": "Unknown service: $svc_name",
  "valid_services": ["postgresql", "mysql", "mariadb", "mongodb", "valkey", "keydb", "rabbitmq", "nats", "elasticsearch", "minio"]
}
EOF
                return 1
            fi

            local hostname=$(get_service_hostname "$svc_name")
            local svc_type=$(echo "$svc_info" | cut -d'|' -f1)

            # Build managed services JSON
            if [ -n "$managed_services" ]; then
                managed_services="$managed_services,{\"hostname\":\"$hostname\",\"type\":\"$svc_type\",\"mode\":\"$svc_mode\"}"
            else
                managed_services="{\"hostname\":\"$hostname\",\"type\":\"$svc_type\",\"mode\":\"$svc_mode\"}"
            fi

            # Build connections list for runtimes
            if [ -n "$connections_list" ]; then
                connections_list="$connections_list,\"$hostname\""
            else
                connections_list="\"$hostname\""
            fi

            # Build environment mapping using jq for proper JSON construction
            local mapping=$(get_env_mapping "$svc_name")
            if [ -n "$mapping" ]; then
                # Replace %s with hostname
                mapping=$(echo "$mapping" | sed "s/%s/$hostname/g")
                # Merge into env_mapping using jq
                env_mapping=$(echo "$env_mapping" | jq --arg h "$hostname" --argjson m "$mapping" '. + {($h): $m}' 2>/dev/null || echo "$env_mapping")
            fi
        done
    fi

    # Determine dev/stage hostnames
    local dev_hostname="${hostname_prefix}dev"
    local stage_hostname="${hostname_prefix}stage"

    # Build managed services JSON array
    local managed_json="[]"
    if [ -n "$managed_services" ]; then
        managed_json="[$managed_services]"
    fi

    # Get required files for runtime
    local required_files=$(get_required_files "$runtime")

    # Build managed services array properly using jq
    local managed_services_array="[]"
    if [ -n "$services" ]; then
        managed_services_array=$(echo "$services" | tr ',' '\n' | jq -R '[inputs | select(length > 0) | split(":")[0]]' 2>/dev/null || echo "[]")
    fi

    # Build synthesis plan
    local synthesis_plan=$(cat <<EOF
{
  "session_id": "$session",
  "timestamp": "$timestamp",

  "requirements": {
    "runtime": "$runtime",
    "managed_services": $managed_services_array,
    "mode": "$(if [ "$dev_only" = "true" ]; then echo "dev-only"; else echo "full"; fi)"
  },

  "services": {
    "runtimes": [
      {
        "hostname": "$dev_hostname",
        "type": "$build_base",
        "role": "dev",
        "connections": [${connections_list}]
      }$(if [ "$dev_only" = "false" ]; then echo ",
      {
        \"hostname\": \"$stage_hostname\",
        \"type\": \"$build_base\",
        \"role\": \"stage\",
        \"connections\": [${connections_list}]
      }"; fi)
    ],
    "managed": $managed_json
  },

  "env_mapping": $env_mapping,

  "synthesis_type": "$(if [ -n "$services" ]; then echo "connectivity_proof"; else echo "static_hello"; fi)",
  "dev_service": "$dev_hostname",
  "stage_service": $(if [ "$dev_only" = "false" ]; then echo "\"$stage_hostname\""; else echo "null"; fi),
  "mount_path": "/var/www/$dev_hostname",

  "code_requirements": {
    "runtime": "$runtime",
    "build_base": "$build_base",
    "run_base": "$run_base",
    "has_managed_services": $(if [ -n "$services" ]; then echo "true"; else echo "false"; fi),
    "files_to_create": $required_files
  }
}
EOF
)

    # Write synthesis plan
    local tmp="${ZCP_TMP_DIR:-/tmp}"
    echo "$synthesis_plan" | jq '.' > "${tmp}/synthesis_plan.json" 2>/dev/null || echo "$synthesis_plan" > "${tmp}/synthesis_plan.json"

    # Generate import.yml
    generate_import_yml "$dev_hostname" "$stage_hostname" "$build_base" "$dev_only" "$managed_services" "$tmp"

    # Update workflow state if function is available (loaded via commands.sh)
    if type update_workflow_state &>/dev/null; then
        update_workflow_state 2>/dev/null
    fi

    # Output success with next steps
    cat <<EOF
{
  "status": "SUCCESS",
  "message": "Synthesis plan generated",
  "files_created": [
    "${tmp}/synthesis_plan.json",
    "${tmp}/synthesized_import.yml"
  ],
  "next_steps": [
    {
      "step": 1,
      "command": ".zcp/workflow.sh extend ${tmp}/synthesized_import.yml",
      "description": "Import services to Zerops (wait for RUNNING)"
    },
    {
      "step": 2,
      "action": "CREATE CODE",
      "description": "Create files in /var/www/$dev_hostname/",
      "files": $required_files,
      "reference": "Read ${tmp}/synthesis_plan.json for env mappings"
    },
    {
      "step": 3,
      "command": ".zcp/workflow.sh verify_synthesis",
      "description": "Validate synthesized code structure"
    }
  ],
  "synthesis_plan": $(cat "${tmp}/synthesis_plan.json")
}
EOF
}

# Get required files for runtime
get_required_files() {
    local runtime="$1"
    case "$runtime" in
        go)
            echo '["zerops.yml", "main.go", "go.mod"]'
            ;;
        nodejs)
            echo '["zerops.yml", "index.js", "package.json"]'
            ;;
        python)
            echo '["zerops.yml", "main.py", "requirements.txt"]'
            ;;
        php)
            echo '["zerops.yml", "index.php", "composer.json"]'
            ;;
        rust)
            echo '["zerops.yml", "src/main.rs", "Cargo.toml"]'
            ;;
        bun)
            echo '["zerops.yml", "index.ts", "package.json"]'
            ;;
        java)
            echo '["zerops.yml", "src/main/java/App.java", "pom.xml"]'
            ;;
        dotnet)
            echo '["zerops.yml", "Program.cs", "app.csproj"]'
            ;;
        *)
            echo '["zerops.yml"]'
            ;;
    esac
}

# Generate import.yml
generate_import_yml() {
    local dev_hostname="$1"
    local stage_hostname="$2"
    local build_base="$3"
    local dev_only="$4"
    local managed_services_json="$5"
    local tmp="${6:-${ZCP_TMP_DIR:-/tmp}}"

    cat > "${tmp}/synthesized_import.yml" <<EOF
# Generated by ZCP Bootstrap Synthesis
# Timestamp: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
#
# Runtime services use startWithoutCode: true
# After services are RUNNING, create code in /var/www/$dev_hostname/

services:
  # Dev runtime - starts empty, agent creates code
  - hostname: $dev_hostname
    type: $build_base
    startWithoutCode: true
    enableSubdomainAccess: true
EOF

    if [ "$dev_only" = "false" ]; then
        cat >> "${tmp}/synthesized_import.yml" <<EOF

  # Stage runtime - receives code via zcli push
  - hostname: $stage_hostname
    type: $build_base
    startWithoutCode: true
    enableSubdomainAccess: true
EOF
    fi

    # Add managed services (parse from JSON string using jq if available)
    if [ -n "$managed_services_json" ]; then
        if command -v jq &>/dev/null; then
            # Use jq to properly parse JSON with process substitution
            while IFS= read -r svc; do
                local hostname=$(echo "$svc" | jq -r '.hostname')
                local type=$(echo "$svc" | jq -r '.type')
                local svc_mode=$(echo "$svc" | jq -r '.mode')

                if [ -n "$hostname" ] && [ "$hostname" != "null" ]; then
                    cat >> "${tmp}/synthesized_import.yml" <<EOF

  # Managed service: $hostname
  - hostname: $hostname
    type: $type
    mode: $svc_mode
EOF
                fi
            done < <(echo "[$managed_services_json]" | jq -c '.[]' 2>/dev/null)
        else
            # Fallback: parse with grep/sed (less reliable but works without jq)
            # Split by },{ to get individual objects
            local objects=$(echo "$managed_services_json" | sed 's/},{/}\n{/g')
            while IFS= read -r svc; do
                local hostname=$(echo "$svc" | grep -o '"hostname":"[^"]*"' | cut -d'"' -f4)
                local type=$(echo "$svc" | grep -o '"type":"[^"]*"' | cut -d'"' -f4)
                local svc_mode=$(echo "$svc" | grep -o '"mode":"[^"]*"' | cut -d'"' -f4)

                if [ -n "$hostname" ]; then
                    cat >> "${tmp}/synthesized_import.yml" <<EOF

  # Managed service: $hostname
  - hostname: $hostname
    type: $type
    mode: $svc_mode
EOF
                fi
            done <<< "$objects"
        fi
    fi
}

# =============================================================================
# VERIFY SYNTHESIS COMMAND
# =============================================================================

cmd_verify_synthesis() {
    local tmp="${ZCP_TMP_DIR:-/tmp}"
    if [ ! -f "${tmp}/synthesis_plan.json" ]; then
        cat <<EOF
{
  "error": "NO_SYNTHESIS_PLAN",
  "message": "No synthesis plan found. Run compose first.",
  "fix": ".zcp/workflow.sh compose --runtime {runtime} --services {services}"
}
EOF
        return 1
    fi

    local plan=$(cat "${tmp}/synthesis_plan.json")
    local dev_service=$(echo "$plan" | jq -r '.dev_service')
    local mount_path=$(echo "$plan" | jq -r '.mount_path')
    local runtime=$(echo "$plan" | jq -r '.requirements.runtime // .code_requirements.runtime')
    local session=$(get_session 2>/dev/null || echo "unknown")
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local errors=()
    local files_found=()

    # Check mount exists
    if [ ! -d "$mount_path" ]; then
        errors+=("Mount path $mount_path does not exist. Is the service RUNNING?")
    else
        # Check zerops.yml
        if [ ! -f "$mount_path/zerops.yml" ]; then
            errors+=("zerops.yml missing in $mount_path")
        else
            files_found+=("zerops.yml")

            # Validate zerops.yml structure
            if command -v yq &>/dev/null; then
                if ! yq e '.zerops' "$mount_path/zerops.yml" > /dev/null 2>&1; then
                    errors+=("zerops.yml missing 'zerops:' top-level wrapper")
                fi

                # Check for setup
                local setup_count=$(yq e '.zerops | length' "$mount_path/zerops.yml" 2>/dev/null || echo "0")
                if [ "$setup_count" = "0" ]; then
                    errors+=("zerops.yml has no setup configurations")
                fi
            fi
        fi

        # Check runtime-specific files
        case "$runtime" in
            go)
                [ -f "$mount_path/main.go" ] && files_found+=("main.go") || errors+=("main.go missing")
                [ -f "$mount_path/go.mod" ] && files_found+=("go.mod") || errors+=("go.mod missing")
                ;;
            nodejs)
                [ -f "$mount_path/index.js" ] && files_found+=("index.js") || errors+=("index.js missing")
                [ -f "$mount_path/package.json" ] && files_found+=("package.json") || errors+=("package.json missing")
                ;;
            python)
                [ -f "$mount_path/main.py" ] && files_found+=("main.py") || errors+=("main.py missing")
                [ -f "$mount_path/requirements.txt" ] && files_found+=("requirements.txt") || errors+=("requirements.txt missing")
                ;;
            php)
                [ -f "$mount_path/index.php" ] && files_found+=("index.php") || errors+=("index.php missing")
                ;;
            rust)
                [ -f "$mount_path/src/main.rs" ] && files_found+=("src/main.rs") || errors+=("src/main.rs missing")
                [ -f "$mount_path/Cargo.toml" ] && files_found+=("Cargo.toml") || errors+=("Cargo.toml missing")
                ;;
            bun)
                [ -f "$mount_path/index.ts" ] && files_found+=("index.ts") || errors+=("index.ts missing")
                [ -f "$mount_path/package.json" ] && files_found+=("package.json") || errors+=("package.json missing")
                ;;
            java)
                [ -f "$mount_path/src/main/java/App.java" ] && files_found+=("src/main/java/App.java") || errors+=("src/main/java/App.java missing")
                [ -f "$mount_path/pom.xml" ] && files_found+=("pom.xml") || errors+=("pom.xml missing")
                ;;
            dotnet)
                [ -f "$mount_path/Program.cs" ] && files_found+=("Program.cs") || errors+=("Program.cs missing")
                [ -f "$mount_path/app.csproj" ] && files_found+=("app.csproj") || errors+=("app.csproj missing")
                ;;
        esac
    fi

    # Build result
    if [ ${#errors[@]} -gt 0 ]; then
        local error_json=$(printf '%s\n' "${errors[@]}" | jq -R . | jq -s .)
        local files_json="[]"
        if [ ${#files_found[@]} -gt 0 ]; then
            files_json=$(printf '%s\n' "${files_found[@]}" | jq -R . | jq -s .)
        fi

        cat <<EOF
{
  "status": "FAILED",
  "errors": $error_json,
  "files_found": $files_json,
  "fix": {
    "action": "Create missing files in $mount_path/",
    "reference": "${tmp}/synthesis_plan.json contains env mappings and requirements"
  }
}
EOF
        return 1
    fi

    # Success - create evidence
    local files_json=$(printf '%s\n' "${files_found[@]}" | jq -R . | jq -s .)
    local plan_hash=$(md5sum "${tmp}/synthesis_plan.json" 2>/dev/null | cut -d' ' -f1 || echo "unknown")

    local evidence=$(cat <<EOF
{
  "session_id": "$session",
  "timestamp": "$timestamp",
  "status": "complete",
  "dev_service": "$dev_service",
  "mount_path": "$mount_path",
  "runtime": "$runtime",
  "files_verified": $files_json,
  "synthesis_plan_hash": "$plan_hash"
}
EOF
)

    echo "$evidence" | jq '.' > "${tmp}/synthesis_complete.json" 2>/dev/null || echo "$evidence" > "${tmp}/synthesis_complete.json"

    # Persist evidence with error handling
    local evidence_dir="${SCRIPT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}/state/workflow/evidence"
    if ! mkdir -p "$evidence_dir" 2>/dev/null; then
        echo "Warning: Could not create evidence directory: $evidence_dir" >&2
    fi
    if [ -f "${tmp}/synthesis_complete.json" ]; then
        if ! cp "${tmp}/synthesis_complete.json" "$evidence_dir/" 2>/dev/null; then
            echo "Warning: Could not persist evidence to $evidence_dir" >&2
        fi
    fi

    # Update workflow state if function is available (loaded via commands.sh)
    if type update_workflow_state &>/dev/null; then
        update_workflow_state 2>/dev/null
    fi

    cat <<EOF
{
  "status": "SUCCESS",
  "message": "Synthesis verified",
  "evidence_file": "${tmp}/synthesis_complete.json",
  "files_verified": $files_json,
  "next_action": {
    "command": ".zcp/workflow.sh transition_to DEVELOP",
    "description": "Transition to DEVELOP phase to build and test"
  }
}
EOF
}

# =============================================================================
# HELP
# =============================================================================

show_compose_help() {
    cat <<'EOF'
COMPOSE - Generate synthesis plan and infrastructure

USAGE:
  .zcp/workflow.sh compose --runtime <runtime> [--services <services>] [options]

OPTIONS:
  --runtime, -r    Runtime type (required): go, nodejs, python, php, rust, bun, java, dotnet
  --services, -s   Managed services (comma-separated): postgresql, mysql, valkey, nats, etc.
  --hostname, -h   Hostname prefix (default: api) -> creates apidev, apistage
  --dev-only       Skip stage service creation

EXAMPLES:
  # Go API with PostgreSQL
  .zcp/workflow.sh compose --runtime go --services postgresql

  # Node.js with PostgreSQL and Redis
  .zcp/workflow.sh compose --runtime nodejs --services postgresql,valkey

  # Python API only (no managed services)
  .zcp/workflow.sh compose --runtime python

  # Go with HA PostgreSQL
  .zcp/workflow.sh compose --runtime go --services postgresql:HA

OUTPUT:
  Creates synthesis_plan.json and synthesized_import.yml in temp directory

NEXT STEPS:
  1. .zcp/workflow.sh extend (path shown in compose output)
  2. Create code in /var/www/{dev}/ (read synthesis_plan.json for env mappings)
  3. .zcp/workflow.sh verify_synthesis
EOF
}
