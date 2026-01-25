#!/usr/bin/env bash
# .zcp/lib/bootstrap/zerops-yml-gen.sh
# Generates zerops.yml skeleton for a service

# Get env var references for managed services
# Uses: zcli service list to find actual managed service hostnames
get_env_references() {
    local services
    services=$(zcli service list -P "$projectId" --json 2>/dev/null)

    if [ -z "$services" ] || [ "$services" = "null" ]; then
        return
    fi

    # Find managed services and generate env refs
    echo "$services" | jq -r '
        .services[] |
        select(.type | test("^(postgresql|mysql|mariadb|mongodb|valkey|keydb|rabbitmq|nats|elasticsearch|minio)@")) |
        .name as $h |
        .type | split("@")[0] as $t |

        # Generate env var lines based on service type
        if $t == "postgresql" or $t == "mysql" or $t == "mariadb" then
            "DB_HOST: ${\($h)_hostname}\nDB_PORT: ${\($h)_port}\nDB_USER: ${\($h)_user}\nDB_PASS: ${\($h)_password}\nDB_NAME: ${\($h)_dbName}"
        elif $t == "mongodb" then
            "MONGO_HOST: ${\($h)_hostname}\nMONGO_PORT: ${\($h)_port}\nMONGO_USER: ${\($h)_user}\nMONGO_PASS: ${\($h)_password}"
        elif $t == "valkey" or $t == "keydb" then
            "REDIS_HOST: ${\($h)_hostname}\nREDIS_PORT: ${\($h)_port}\nREDIS_PASS: ${\($h)_password}"
        elif $t == "rabbitmq" then
            "AMQP_HOST: ${\($h)_hostname}\nAMQP_PORT: ${\($h)_port}\nAMQP_USER: ${\($h)_user}\nAMQP_PASS: ${\($h)_password}"
        elif $t == "nats" then
            "NATS_URL: nats://${\($h)_hostname}:${\($h)_port}"
        elif $t == "elasticsearch" then
            "ES_HOST: ${\($h)_hostname}\nES_PORT: ${\($h)_port}\nES_USER: ${\($h)_user}\nES_PASS: ${\($h)_password}"
        elif $t == "minio" then
            "S3_ENDPOINT: ${\($h)_apiUrl}\nS3_ACCESS_KEY: ${\($h)_accessKeyId}\nS3_SECRET_KEY: ${\($h)_secretAccessKey}"
        else
            ""
        end
    ' 2>/dev/null | grep -v '^$'
}

# Generate zerops.yml skeleton
# Agent must fill in: buildCommands, deployFiles, start command
generate_zerops_yml_skeleton() {
    local hostname="$1"
    local port="${2:-8080}"
    local output_file="${3:-/var/www/${hostname}/zerops.yml}"

    local env_refs
    env_refs=$(get_env_references)

    {
        echo "# ZCP Bootstrap - Agent must complete build commands and start"
        echo "# Patterns available in /tmp/recipe_review.json"
        echo "zerops:"
        echo "  - setup: dev"
        echo "    build:"
        echo "      base: # AGENT: Get from recipe_review.json"
        echo "      buildCommands:"
        echo "        - # AGENT: Add build commands from recipe patterns"
        echo "      deployFiles:"
        echo "        - # AGENT: Add deploy files from recipe patterns"
        echo "      cache: true"
        echo "    run:"
        echo "      base: # AGENT: Same as build base for dev"
        echo "      ports:"
        echo "        - port: $port"
        echo "          httpSupport: true"
        echo "      start: zsc noop --silent  # Dev: manual control"
        echo "      envVariables:"
        echo "        PORT: \"$port\""
        if [ -n "$env_refs" ]; then
            echo "$env_refs" | sed 's/^/        /'
        fi
        echo ""
        echo "  - setup: prod"
        echo "    build:"
        echo "      base: # AGENT: Get from recipe_review.json"
        echo "      buildCommands:"
        echo "        - # AGENT: Add build commands from recipe patterns"
        echo "      deployFiles:"
        echo "        - # AGENT: Add deploy files from recipe patterns"
        echo "      cache: true"
        echo "    run:"
        echo "      base: # AGENT: alpine for Go/Rust, same as build for others"
        echo "      ports:"
        echo "        - port: $port"
        echo "          httpSupport: true"
        echo "      start: # AGENT: Add start command from recipe patterns"
        echo "      envVariables:"
        echo "        PORT: \"$port\""
        if [ -n "$env_refs" ]; then
            echo "$env_refs" | sed 's/^/        /'
        fi
    } > "$output_file"

    echo "$output_file"
}

export -f get_env_references generate_zerops_yml_skeleton
