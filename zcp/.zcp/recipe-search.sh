#!/bin/bash
# Zerops Recipe & Documentation Search Tool
# Optimized for finding recipes, patterns, and configuration examples

set -o pipefail
# Note: -e removed intentionally - grep returns 1 on no match, which is valid

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Recipe repository structure (known patterns)
RECIPE_BASE_URL="https://github.com/zerops-recipe-apps"
DOCS_BASE_URL="https://docs.zerops.io"
RECIPE_API_URL="https://stage-vega.zerops.dev"
RECIPE_DATA_API="https://api-d89-1337.prg1.zerops.app/api"

# Fetch recipe from API when local patterns don't exist
fetch_recipe_from_api() {
    local runtime="$1"
    local recipe_name="$2"

    if ! command -v curl &>/dev/null; then
        echo -e "${YELLOW}⚠ curl not available, cannot fetch from API${NC}"
        return 1
    fi

    echo -e "${BOLD}Fetching recipe from API...${NC}"

    # FETCH 1: Get import.yml to find hostname
    local fetch1_url="${RECIPE_API_URL}/recipes/${recipe_name}.md?environment=ai-agent"
    echo "  → Fetching: $fetch1_url"

    local import_response
    import_response=$(curl -sf "$fetch1_url" 2>/dev/null)
    if [ -z "$import_response" ]; then
        echo -e "${RED}✗ Failed to fetch recipe${NC}"
        return 1
    fi

    # Save fetch 1 response
    echo "$import_response" > /tmp/fetched_recipe_import.md

    # Extract hostname from import.yml section
    # Look for "hostname:" in the YAML code block
    local hostname
    hostname=$(echo "$import_response" | grep -E "^\s*hostname:" | head -1 | sed 's/.*hostname:\s*//' | tr -d ' "')

    if [ -z "$hostname" ]; then
        # Try alternate patterns
        hostname=$(echo "$import_response" | grep -oE "hostname: [a-z0-9]+" | head -1 | sed 's/hostname: //')
    fi

    if [ -z "$hostname" ]; then
        # Look for common names in the content
        if echo "$import_response" | grep -q 'appstage'; then
            hostname="appstage"
        elif echo "$import_response" | grep -q 'app:'; then
            hostname="app"
        else
            echo -e "${YELLOW}⚠ Could not determine hostname from recipe${NC}"
            hostname="appstage"  # Default fallback
        fi
    fi

    echo "  → Found hostname: $hostname"

    # FETCH 2: Get zerops.yml with guideApp
    local fetch2_url="${RECIPE_API_URL}/recipes/${recipe_name}.md?environment=ai-agent&guideFlow=integrate&guideEnv=ai-agent&guideApp=${hostname}"
    echo "  → Fetching zerops.yml: $fetch2_url"

    local full_response
    full_response=$(curl -sf "$fetch2_url" 2>/dev/null)
    if [ -z "$full_response" ]; then
        echo -e "${YELLOW}⚠ Could not fetch full recipe, using first response${NC}"
        full_response="$import_response"
    fi

    # Save to temp file for parsing
    echo "$full_response" > /tmp/fetched_recipe.md
    echo -e "${GREEN}✓ Recipe fetched and saved to /tmp/fetched_recipe.md${NC}"

    # Extract key patterns from the response and save to temp JSON for create_evidence_file
    extract_patterns_from_response "$runtime" "$full_response"

    # Mark that we have fetched data
    echo "$runtime" > /tmp/fetched_runtime

    return 0
}

# Extract patterns from API response
extract_patterns_from_response() {
    local runtime="$1"
    local response="$2"

    echo ""
    echo -e "${BOLD}Extracted from API:${NC}"

    # Try to find version strings - look for common runtimes (including @latest)
    local versions
    versions=$(echo "$response" | grep -oE "(go|golang|nodejs|node|php|python|bun|rust|dotnet|java|alpine)@[0-9a-z.]+" | sort -u | head -10)
    local versions_json="[]"
    if [ -n "$versions" ]; then
        echo -e "${GREEN}Versions found:${NC}"
        versions_json="["
        while read v; do
            echo "  - $v"
            versions_json+="\"$v\","
        done <<< "$versions"
        versions_json="${versions_json%,}]"
    fi

    # Find the main runtime base from "base:" or "type:" in YAML
    local runtime_base
    # First try to get from "base:" in zerops.yml
    runtime_base=$(echo "$response" | grep -E "^\s*base:" | head -1 | sed 's/.*base:\s*//' | tr -d ' "')
    if [ -z "$runtime_base" ]; then
        # Try to get from "type:" in import.yml
        runtime_base=$(echo "$response" | grep -E "^\s*type:" | head -1 | sed 's/.*type:\s*//' | tr -d ' "')
    fi
    if [ -z "$runtime_base" ]; then
        # Fallback: get first version found
        runtime_base=$(echo "$versions" | head -1)
    fi

    if [ -n "$runtime_base" ]; then
        echo -e "${GREEN}Runtime base:${NC} $runtime_base"
    fi

    # Try to find alpine base (production runtime)
    local alpine
    alpine=$(echo "$response" | grep -oE "alpine@[0-9.]+" | head -1)
    if [ -n "$alpine" ]; then
        echo -e "${GREEN}Prod runtime:${NC} $alpine"
    fi

    # Check for startWithoutCode
    local has_start_without_code="false"
    if echo "$response" | grep -q "startWithoutCode"; then
        echo -e "${GREEN}startWithoutCode:${NC} found in recipe"
        has_start_without_code="true"
    fi

    # Check for buildFromGit
    local git_url=""
    git_url=$(echo "$response" | grep -oE "buildFromGit: [^ ]+" | head -1 | sed 's/buildFromGit: //')
    if [ -n "$git_url" ]; then
        echo -e "${GREEN}buildFromGit:${NC} $git_url"
    fi

    # Check for cache setting
    local has_cache="false"
    if echo "$response" | grep -qE "cache:\s*(true|node_modules|vendor|\.venv)"; then
        echo -e "${GREEN}cache:${NC} found in recipe"
        has_cache="true"
    fi

    # Extract deployFiles patterns
    local deploy_files
    deploy_files=$(echo "$response" | grep -A5 "deployFiles:" | grep -E "^\s*-" | head -5 | sed 's/^\s*-\s*//' | tr '\n' ',' | sed 's/,$//')
    if [ -n "$deploy_files" ]; then
        echo -e "${GREEN}deployFiles:${NC} $deploy_files"
    fi

    echo ""
    echo -e "${CYAN}Full recipe saved to: /tmp/fetched_recipe.md${NC}"
    echo "Review this file for complete import.yml and zerops.yml patterns."

    # For the evidence file, use the first non-alpine version as the runtime base
    local evidence_runtime_base="$runtime_base"
    if [ -z "$evidence_runtime_base" ] || echo "$evidence_runtime_base" | grep -q "alpine"; then
        evidence_runtime_base=$(echo "$versions" | grep -v "alpine" | head -1)
    fi
    [ -z "$evidence_runtime_base" ] && evidence_runtime_base="${runtime}@1"

    # Use alpine if found, otherwise use runtime base for prod
    local prod_base="${alpine:-$evidence_runtime_base}"

    # Save extracted patterns to temp JSON for create_evidence_file
    cat > /tmp/fetched_patterns.json <<EOF
{
    "runtime": "${runtime}",
    "runtime_base": "${evidence_runtime_base}",
    "alpine_base": "${alpine:-null}",
    "prod_base": "${prod_base}",
    "versions_found": ${versions_json},
    "has_start_without_code": ${has_start_without_code},
    "has_cache": ${has_cache},
    "build_from_git": "${git_url}",
    "deploy_files": "${deploy_files}",
    "fetched": true,
    "source": "api"
}
EOF
}

# Fetch documentation from docs.zerops.io as fallback when no recipe exists
# Uses llms.txt structure: every runtime has /runtime/overview.md and /runtime/how-to/build-pipeline.md
fetch_docs_fallback() {
    local runtime="$1"

    if ! command -v curl &>/dev/null; then
        echo -e "${YELLOW}⚠ curl not available${NC}"
        return 1
    fi

    echo -e "${BOLD}Fetching from documentation...${NC}"

    # Fetch overview page
    local overview_url="${DOCS_BASE_URL}/${runtime}/overview.md"
    echo "  → Fetching: $overview_url"

    local overview_response
    overview_response=$(curl -sf "$overview_url" 2>/dev/null)

    if [ -z "$overview_response" ]; then
        echo -e "${RED}✗ Documentation not found for: ${runtime}${NC}"
        return 1
    fi

    # Fetch build-pipeline page for more details
    local pipeline_url="${DOCS_BASE_URL}/${runtime}/how-to/build-pipeline.md"
    echo "  → Fetching: $pipeline_url"

    local pipeline_response
    pipeline_response=$(curl -sf "$pipeline_url" 2>/dev/null)

    # Combine responses
    local combined_response="$overview_response"
    if [ -n "$pipeline_response" ]; then
        combined_response="${overview_response}

---

${pipeline_response}"
    fi

    # Save to temp file
    echo "$combined_response" > /tmp/fetched_docs.md
    echo -e "${GREEN}✓ Documentation fetched and saved to /tmp/fetched_docs.md${NC}"

    # Extract patterns from documentation
    extract_patterns_from_docs "$runtime" "$combined_response"

    return 0
}

# Extract patterns from documentation (different format than recipe API)
extract_patterns_from_docs() {
    local runtime="$1"
    local response="$2"

    echo ""
    echo -e "${BOLD}Extracted from documentation:${NC}"

    # Try to find version strings (including @latest)
    local versions
    versions=$(echo "$response" | grep -oE "(${runtime}|go|golang|nodejs|node|php|python|bun|rust|dotnet|java|alpine)@[0-9a-z.]+" | sort -u | head -10)
    local versions_json="[]"
    if [ -n "$versions" ]; then
        echo -e "${GREEN}Versions found:${NC}"
        versions_json="["
        while read v; do
            echo "  - $v"
            versions_json+="\"$v\","
        done <<< "$versions"
        versions_json="${versions_json%,}]"
    fi

    # Find runtime base from documentation examples
    local runtime_base
    runtime_base=$(echo "$response" | grep -oE "${runtime}@[0-9.]+" | head -1)
    if [ -z "$runtime_base" ]; then
        runtime_base=$(echo "$versions" | grep -v "alpine" | head -1)
    fi

    if [ -n "$runtime_base" ]; then
        echo -e "${GREEN}Runtime base:${NC} $runtime_base"
    else
        runtime_base="${runtime}@1"
        echo -e "${YELLOW}Runtime base (default):${NC} $runtime_base"
    fi

    # Check for zerops.yml examples in docs
    if echo "$response" | grep -q "zerops:"; then
        echo -e "${GREEN}zerops.yml examples:${NC} found in documentation"
    fi

    # Check for import.yml examples
    if echo "$response" | grep -q "hostname:"; then
        echo -e "${GREEN}import.yml examples:${NC} found in documentation"
    fi

    echo ""
    echo -e "${CYAN}Full documentation saved to: /tmp/fetched_docs.md${NC}"
    echo "Review this file for configuration examples."

    # For the evidence file
    local evidence_runtime_base="$runtime_base"
    [ -z "$evidence_runtime_base" ] && evidence_runtime_base="${runtime}@1"

    # Save extracted patterns to temp JSON for create_evidence_file
    cat > /tmp/fetched_patterns.json <<EOF
{
    "runtime": "${runtime}",
    "runtime_base": "${evidence_runtime_base}",
    "alpine_base": null,
    "prod_base": "${evidence_runtime_base}",
    "versions_found": ${versions_json},
    "has_start_without_code": false,
    "has_cache": false,
    "build_from_git": "",
    "deploy_files": "",
    "fetched": true,
    "source": "docs"
}
EOF
}

# Helper function to get recipe patterns (bash 3 compatible)
# NOTE: Only list recipes that ACTUALLY EXIST at stage-vega.zerops.dev/recipes.md
# Last verified: 2026-01-22
get_recipe_patterns() {
    local runtime="$1"
    case "$runtime" in
        # VERIFIED EXISTING RECIPES from API:
        go) echo "go-hello-world" ;;
        bun) echo "bun-hello-world" ;;
        nodejs|node) echo "nestjs-hello-world" ;;
        php) echo "laravel-jetstream" ;;
        python) echo "django" ;;
        # These have recipes that USE them but aren't runtime recipes:
        postgresql|postgres) echo "go-hello-world" ;;  # go recipe includes postgresql
        valkey|redis) echo "laravel-jetstream" ;;      # laravel recipe includes valkey
        *) echo "" ;;
    esac
}

# Helper function to get docs paths (bash 3 compatible)
get_docs_paths() {
    local topic="$1"
    case "$topic" in
        go) echo "go/overview go/how-to/build-pipeline" ;;
        nodejs) echo "nodejs/overview nodejs/how-to/build-pipeline" ;;
        php) echo "php/overview php/how-to/build-pipeline" ;;
        python) echo "python/overview python/how-to/build-pipeline" ;;
        postgresql) echo "postgresql/overview postgresql/how-to/create" ;;
        valkey) echo "valkey/overview" ;;
        import) echo "references/import references/import-yaml/type-list" ;;
        zerops.yml) echo "references/zeropsyml" ;;
        pipeline) echo "features/pipeline" ;;
        *) echo "" ;;
    esac
}

# List of known runtimes for help display
# Only list runtimes that have VERIFIED recipes at stage-vega.zerops.dev
KNOWN_RUNTIMES="go bun nodejs php python"

show_help() {
  cat <<EOF
${BOLD}Zerops Recipe & Documentation Search${NC}

${BOLD}USAGE:${NC}
  recipe-search.sh <command> [options]

${BOLD}COMMANDS:${NC}

  ${GREEN}recipe${NC} <runtime> [<managed-service>]
    Find official recipes for runtime + optional managed service
    Examples:
      recipe-search.sh recipe go
      recipe-search.sh recipe go postgresql
      recipe-search.sh recipe nodejs valkey

  ${GREEN}pattern${NC} <service-type>
    Extract common patterns for a service type
    Examples:
      recipe-search.sh pattern go
      recipe-search.sh pattern postgresql

  ${GREEN}docs${NC} <topic>
    Find documentation for a topic
    Examples:
      recipe-search.sh docs go
      recipe-search.sh docs import
      recipe-search.sh docs pipeline

  ${GREEN}version${NC} <service-type>
    Find valid versions for a service type
    Examples:
      recipe-search.sh version go
      recipe-search.sh version postgresql

  ${GREEN}field${NC} <yaml-section>
    Find valid fields for YAML sections
    Examples:
      recipe-search.sh field import
      recipe-search.sh field build
      recipe-search.sh field run

  ${GREEN}example${NC} <runtime> <feature>
    Find example for specific feature
    Examples:
      recipe-search.sh example go cache
      recipe-search.sh example nodejs env-vars

  ${GREEN}quick${NC} <runtime> [<managed-service>]
    Quick recipe search + pattern extraction
    Creates /tmp/recipe_review.json
    Examples:
      recipe-search.sh quick go postgresql

${BOLD}OUTPUT:${NC}
  Results are displayed on screen and optionally saved to:
  - /tmp/recipe_search_results.json (structured data)
  - /tmp/recipe_review.json (for Gate 0 compliance)

${BOLD}EXAMPLES:${NC}

  # Find Go + PostgreSQL recipe
  recipe-search.sh recipe go postgresql

  # Extract Go build patterns
  recipe-search.sh pattern go

  # Get valid PostgreSQL versions
  recipe-search.sh version postgresql

  # Quick search for nodejs + valkey (creates evidence file)
  recipe-search.sh quick nodejs valkey

${BOLD}RECIPE STRUCTURE:${NC}
  Official recipes follow this structure:
  - {runtime}-hello-world (basic example)
  - {runtime}-hello-world-remote (with managed services)
  - {runtime}-{framework} (framework-specific)

${BOLD}KNOWN RECIPES:${NC}
EOF

  echo ""
  for runtime in $KNOWN_RUNTIMES; do
    local patterns
    patterns=$(get_recipe_patterns "$runtime")
    [ -n "$patterns" ] && echo -e "  ${CYAN}$runtime${NC}: $patterns"
  done

  echo ""
  echo -e "${BOLD}DOCS STRUCTURE:${NC}"
  echo "  https://docs.zerops.io/{runtime}/overview"
  echo "  https://docs.zerops.io/{runtime}/how-to/build-pipeline"
  echo "  https://docs.zerops.io/references/import"
  echo "  https://docs.zerops.io/references/zeropsyml"
  echo ""
}

# Search for recipe
search_recipe() {
  local runtime="$1"
  local managed_service="${2:-}"

  echo -e "${BOLD}Searching for ${runtime} recipes...${NC}"
  echo ""

  local recipes
  recipes=$(get_recipe_patterns "$runtime")

  if [[ -z "$recipes" ]]; then
    echo -e "${YELLOW}⚠ No known recipes for runtime: $runtime${NC}"
    echo ""
    echo "Try web search:"
    echo "  https://github.com/zerops-recipe-apps?q=${runtime}"
    return 1
  fi

  local found=false

  for recipe in $recipes; do
    # If managed service specified, prefer recipes with that service
    if [[ -n "$managed_service" ]]; then
      if [[ "$recipe" == *"$managed_service"* ]] || [[ "$recipe" == *"remote"* ]]; then
        echo -e "${GREEN}✓ Recipe:${NC} $recipe"
        echo -e "  ${BLUE}URL:${NC} ${RECIPE_BASE_URL}/${recipe}"
        echo -e "  ${BLUE}Files:${NC}"
        echo "    - ${RECIPE_BASE_URL}/${recipe}/blob/main/import.yml"
        echo "    - ${RECIPE_BASE_URL}/${recipe}/blob/main/zerops.yml"
        echo ""
        found=true
      fi
    else
      echo -e "${GREEN}✓ Recipe:${NC} $recipe"
      echo -e "  ${BLUE}URL:${NC} ${RECIPE_BASE_URL}/${recipe}"
      echo ""
      found=true
    fi
  done

  if [[ "$found" == "false" ]] && [[ -n "$managed_service" ]]; then
    echo -e "${YELLOW}⚠ No specific recipe for $runtime + $managed_service${NC}"
    echo ""
    echo "General recipes available:"
    for recipe in $recipes; do
      echo "  - ${RECIPE_BASE_URL}/${recipe}"
    done
    echo ""
  fi

  # Suggest documentation
  local docs_paths
  docs_paths=$(get_docs_paths "$runtime")
  if [[ -n "$docs_paths" ]]; then
    echo -e "${BOLD}Related Documentation:${NC}"
    for path in $docs_paths; do
      echo "  - ${DOCS_BASE_URL}/${path}"
    done
    echo ""
  fi

  return 0
}

# Extract patterns from known recipe structure
extract_patterns() {
  local service_type="$1"

  echo -e "${BOLD}Extracting patterns for ${service_type}...${NC}"
  echo ""

  # Common patterns based on service type
  case "$service_type" in
    go)
      cat <<EOF
${BOLD}Go Runtime Patterns:${NC}

${GREEN}Valid Versions:${NC}
  - go@1
  - go@1.22
  - golang@1

${GREEN}Production Runtime:${NC}
  base: alpine@3.21   # Minimal runtime (5-10 MB)
  start: ./app        # Auto-start compiled binary

${GREEN}Development Runtime:${NC}
  os: ubuntu          # Full toolset for development
  base: go@1          # Go toolchain for building
  start: zsc noop --silent  # Manual control

${GREEN}Build Configuration:${NC}
  build:
    base: go@1
    buildCommands:
      - go build -o app main.go  # Explicit file reference
    deployFiles: ./app           # Production: binary only
    cache: true                  # CRITICAL: 5-10x faster rebuilds

${GREEN}Environment Variables:${NC}
  envVariables:
    DB_HOST: \${db_hostname}
    DB_PORT: \${db_port}
    DB_USER: \${db_user}
    DB_PASS: \${db_password}
    DB_NAME: \${db_dbName}

${GREEN}Import Configuration:${NC}
  - hostname: appdev
    type: go@1
    zeropsSetup: dev
    enableSubdomainAccess: true

${YELLOW}Reference:${NC}
  Recipe: ${RECIPE_BASE_URL}/go-hello-world-remote
  Docs: ${DOCS_BASE_URL}/go/how-to/build-pipeline
EOF
      ;;

    nodejs|node)
      cat <<EOF
${BOLD}Node.js Runtime Patterns:${NC}

${GREEN}Valid Versions:${NC}
  - nodejs@20
  - nodejs@22

${GREEN}Production Runtime:${NC}
  base: nodejs@20
  start: npm start

${GREEN}Development Runtime:${NC}
  base: nodejs@20
  start: zsc noop --silent

${GREEN}Build Configuration:${NC}
  build:
    base: nodejs@20
    buildCommands:
      - npm install
      - npm run build  # If needed
    deployFiles:
      - package.json
      - package-lock.json
      - node_modules
      - dist  # Or src, depending on project
    cache: node_modules

${GREEN}Environment Variables:${NC}
  envVariables:
    NODE_ENV: production
    DB_HOST: \${db_hostname}
    DB_PORT: \${db_port}

${YELLOW}Reference:${NC}
  Recipe: ${RECIPE_BASE_URL}/nodejs-hello-world
  Docs: ${DOCS_BASE_URL}/nodejs/how-to/build-pipeline
EOF
      ;;

    postgresql|postgres)
      cat <<EOF
${BOLD}PostgreSQL Patterns:${NC}

${GREEN}Valid Versions:${NC}
  - postgresql@16
  - postgresql@17  # Recommended

${GREEN}Import Configuration:${NC}
  - hostname: db
    type: postgresql@17
    mode: NON_HA  # Or HA for high availability

${GREEN}Environment Variables Provided:${NC}
  \${db_hostname}     → Hostname of database service
  \${db_port}         → Port (typically 5432)
  \${db_user}         → Auto-generated username
  \${db_password}     → Auto-generated password
  \${db_dbName}       → Database name
  \${db_connectionString}  → Full connection string (optional)

${GREEN}Usage in Runtime Services:${NC}
  envVariables:
    DB_HOST: \${db_hostname}
    DB_PORT: \${db_port}
    DB_USER: \${db_user}
    DB_PASS: \${db_password}
    DB_NAME: \${db_dbName}

${GREEN}Connection from ZCP:${NC}
  psql "\$db_connectionString" -c "SELECT 1"

${YELLOW}⚠ IMPORTANT:${NC}
  - Runtime containers do NOT have psql
  - Use psql from ZCP only
  - New service vars not visible until ZCP restart

${YELLOW}Reference:${NC}
  Recipe: ${RECIPE_BASE_URL}/go-hello-world-remote
  Docs: ${DOCS_BASE_URL}/postgresql/overview
EOF
      ;;

    valkey|redis)
      cat <<EOF
${BOLD}Valkey (Redis) Patterns:${NC}

${GREEN}Valid Versions:${NC}
  - valkey@7.2

${GREEN}Import Configuration:${NC}
  - hostname: cache
    type: valkey@7.2
    mode: NON_HA

${GREEN}Environment Variables Provided:${NC}
  \${cache_hostname}
  \${cache_port}
  \${cache_password}
  \${cache_connectionString}

${GREEN}Usage in Runtime Services:${NC}
  envVariables:
    REDIS_HOST: \${cache_hostname}
    REDIS_PORT: \${cache_port}
    REDIS_PASSWORD: \${cache_password}

${GREEN}Connection from ZCP:${NC}
  redis-cli -u "\$cache_connectionString" PING

${YELLOW}Reference:${NC}
  Recipe: ${RECIPE_BASE_URL}/nodejs-valkey
  Docs: ${DOCS_BASE_URL}/valkey/overview
EOF
      ;;

    bun)
      cat <<EOF
${BOLD}Bun Runtime Patterns:${NC}

${GREEN}Valid Versions:${NC}
  - bun@1

${GREEN}Production Runtime:${NC}
  base: bun@1
  start: bun run start

${GREEN}Development Runtime:${NC}
  os: ubuntu
  base: bun@1
  start: zsc noop --silent  # Manual control

${GREEN}Build Configuration:${NC}
  build:
    base: bun@1
    buildCommands:
      - bun install
      - bun run build  # If needed
    deployFiles:
      - package.json
      - bun.lockb
      - node_modules
      - dist  # Or src
    cache: node_modules

${GREEN}Environment Variables:${NC}
  envVariables:
    DB_HOST: \${db_hostname}
    DB_PORT: \${db_port}
    DB_USER: \${db_user}
    DB_PASS: \${db_password}
    DB_NAME: \${db_dbName}

${GREEN}Import Configuration:${NC}
  - hostname: appdev
    type: bun@1
    zeropsSetup: dev
    startWithoutCode: true
    enableSubdomainAccess: true

${YELLOW}Reference:${NC}
  Recipe: ${RECIPE_BASE_URL}/bun-hello-world
EOF
      ;;

    php)
      cat <<EOF
${BOLD}PHP Runtime Patterns:${NC}

${GREEN}Valid Versions:${NC}
  - php-nginx@8.4
  - php-apache@8.4

${GREEN}Production Runtime:${NC}
  base: php-nginx@8.4
  start: (handled by nginx)

${GREEN}Development Runtime:${NC}
  os: ubuntu
  base: php-nginx@8.4
  start: zsc noop --silent

${GREEN}Build Configuration:${NC}
  build:
    base: php-nginx@8.4
    buildCommands:
      - composer install --no-dev --optimize-autoloader
    deployFiles:
      - vendor
      - public
      - app
      - config
      - routes
    cache: vendor

${GREEN}Environment Variables:${NC}
  envVariables:
    DB_HOST: \${db_hostname}
    DB_PORT: \${db_port}
    DB_USER: \${db_user}
    DB_PASS: \${db_password}
    DB_NAME: \${db_dbName}
    CACHE_DRIVER: redis
    REDIS_HOST: \${cache_hostname}

${YELLOW}Reference:${NC}
  Recipe: ${RECIPE_BASE_URL}/laravel-jetstream
  Docs: ${DOCS_BASE_URL}/php/how-to/build-pipeline
EOF
      ;;

    python)
      cat <<EOF
${BOLD}Python Runtime Patterns:${NC}

${GREEN}Valid Versions:${NC}
  - python@3.12

${GREEN}Production Runtime:${NC}
  base: python@3.12
  start: gunicorn myapp.wsgi:application

${GREEN}Development Runtime:${NC}
  os: ubuntu
  base: python@3.12
  start: zsc noop --silent

${GREEN}Build Configuration:${NC}
  build:
    base: python@3.12
    buildCommands:
      - pip install -r requirements.txt
    deployFiles:
      - .
    cache: .venv

${GREEN}Environment Variables:${NC}
  envVariables:
    DB_HOST: \${db_hostname}
    DB_PORT: \${db_port}
    DB_USER: \${db_user}
    DB_PASS: \${db_password}
    DB_NAME: \${db_dbName}
    DJANGO_SETTINGS_MODULE: myapp.settings

${YELLOW}Reference:${NC}
  Recipe: ${RECIPE_BASE_URL}/django
  Docs: ${DOCS_BASE_URL}/python/how-to/build-pipeline
EOF
      ;;

    *)
      echo -e "${YELLOW}⚠ No pattern template for: $service_type${NC}"
      echo ""
      echo "Try searching for recipes:"
      echo "  recipe-search.sh recipe $service_type"
      return 1
      ;;
  esac

  echo ""
}

# Get valid versions for service type
get_versions() {
  local service_type="$1"

  echo -e "${BOLD}Valid versions for ${service_type}:${NC}"
  echo ""

  case "$service_type" in
    go|golang)
      echo "  - go@1"
      echo "  - go@1.22"
      echo "  - golang@1"
      echo ""
      echo -e "${GREEN}Recommended:${NC} go@1 (tracks latest stable)"
      ;;
    nodejs|node)
      echo "  - nodejs@20"
      echo "  - nodejs@22"
      echo ""
      echo -e "${GREEN}Recommended:${NC} nodejs@22"
      ;;
    php)
      echo "  - php@8.1"
      echo "  - php@8.2"
      echo "  - php@8.3"
      echo ""
      echo -e "${GREEN}Recommended:${NC} php@8.3"
      ;;
    python)
      echo "  - python@3.11"
      echo "  - python@3.12"
      echo ""
      echo -e "${GREEN}Recommended:${NC} python@3.12"
      ;;
    postgresql|postgres)
      echo "  - postgresql@16"
      echo "  - postgresql@17"
      echo ""
      echo -e "${GREEN}Recommended:${NC} postgresql@17"
      ;;
    valkey)
      echo "  - valkey@7.2"
      ;;
    *)
      echo -e "${YELLOW}⚠ Unknown service type: $service_type${NC}"
      echo ""
      echo "Check official docs:"
      echo "  ${DOCS_BASE_URL}/references/import-yaml/type-list"
      return 1
      ;;
  esac

  echo ""
  echo -e "${BLUE}Full list:${NC} ${DOCS_BASE_URL}/references/import-yaml/type-list"
  echo ""
}

# Get valid fields for YAML sections
get_fields() {
  local section="$1"

  echo -e "${BOLD}Valid fields for ${section}:${NC}"
  echo ""

  case "$section" in
    import)
      cat <<EOF
${GREEN}Service Import YAML Fields:${NC}

Required:
  hostname: <string>          # Service name (lowercase, max 25 chars)
  type: <string>              # Service type and version (e.g., go@1)

Optional:
  zeropsSetup: <string>       # Links to zerops.yml setup
  buildFromGit: <string>      # Git repository URL
  enableSubdomainAccess: true # Enable public subdomain
  mode: NON_HA|HA            # For managed services (default: NON_HA)

  verticalAutoscaling:
    minRam: <number>          # Min RAM (GB)
    maxRam: <number>          # Max RAM (GB)
    minCpu: <number>          # Min CPU cores
    maxCpu: <number>          # Max CPU cores

  minContainers: <number>     # Horizontal scaling min (1-10)
  maxContainers: <number>     # Horizontal scaling max (1-10)

  envSecrets:                 # Secret environment variables
    KEY: value

${YELLOW}Reference:${NC} ${DOCS_BASE_URL}/references/import
EOF
      ;;

    build)
      cat <<EOF
${GREEN}Build Configuration Fields:${NC}

Required:
  base: <runtime>@<version>   # Build image (e.g., go@1)

Optional:
  os: alpine|ubuntu           # OS selection (default: alpine)

  prepareCommands:            # Run before build (cached)
    - <command>

  buildCommands:              # Build commands
    - <command>

  deployFiles:                # Files to deploy
    - <path>                  # Can be file, directory, or .

  cache:                      # Cache configuration
    - <path>                  # OR
    true                      # Auto-cache (Go: GOCACHE/GOMODCACHE)

  envVariables:               # Build-time env vars
    KEY: value

${YELLOW}Reference:${NC} ${DOCS_BASE_URL}/references/zeropsyml
EOF
      ;;

    run)
      cat <<EOF
${GREEN}Runtime Configuration Fields:${NC}

Required:
  start: <command>            # Command to start the app

Optional:
  base: <runtime>@<version>   # Runtime image
  os: alpine|ubuntu           # OS selection

  ports:                      # Exposed ports
    - port: <number>
      protocol: TCP|UDP       # Default: TCP
      httpSupport: true       # Enable HTTP routing

  prepareCommands:            # Run before start
    - <command>

  initCommands:               # Run on every container start
    - <command>

  envVariables:               # Runtime env vars
    KEY: value
    VAR: \${service_var}      # Reference other service's vars

  healthCheck:                # Health check config
    httpGet:
      port: <number>
      path: <string>
      scheme: HTTP|HTTPS

  readinessCheck:             # Readiness check
    httpGet:
      port: <number>
      path: <string>

${YELLOW}Reference:${NC} ${DOCS_BASE_URL}/references/zeropsyml
EOF
      ;;

    *)
      echo -e "${YELLOW}⚠ Unknown section: $section${NC}"
      echo ""
      echo "Valid sections: import, build, run"
      return 1
      ;;
  esac

  echo ""
}

# No local patterns - always fetch from API or docs
has_local_patterns() {
  # Always return false - recipe API and docs are the only sources of truth
  return 1
}

# Find the framework slug from the API (handles aliases like go->golang)
find_framework_slug() {
  local search="$1"

  local frameworks
  frameworks=$(curl -sf "${RECIPE_DATA_API}/recipe-language-frameworks?pagination%5BpageSize%5D=200" 2>/dev/null)

  if [ -z "$frameworks" ]; then
    return 1
  fi

  # Try exact slug match first, then name match (case insensitive)
  local slug
  slug=$(echo "$frameworks" | jq -r --arg s "$search" '.data[] | select(.slug == $s or (.name | ascii_downcase) == ($s | ascii_downcase)) | .slug' | head -1)

  # Handle common aliases
  if [ -z "$slug" ]; then
    case "$search" in
      go) slug="golang" ;;
      node|nodejs) slug="node-js" ;;
      dotnet) slug="net" ;;
    esac
  fi

  [ -n "$slug" ] && echo "$slug"
}

# Find recipe for a runtime from API
# Returns: recipe_slug:category_type (e.g., "go-hello-world:hello-world" or "django:framework")
# Logic:
#   - Base runtimes (python, go, etc.) → only use hello-world recipes, else fallback to docs
#   - Explicit frameworks (django, laravel) → use framework recipe
find_recipe_for_runtime() {
  local runtime="$1"

  echo -e "${CYAN}Searching recipe API...${NC}" >&2

  # Step 1: Check if this is an explicit framework search (e.g., "django", "laravel")
  # by looking for a recipe with this exact slug
  local direct_recipe_url="${RECIPE_DATA_API}/recipes?filters%5Bslug%5D=${runtime}&populate%5BrecipeCategories%5D=true"
  local direct_response
  direct_response=$(curl -sf "$direct_recipe_url" 2>/dev/null)

  if [ -n "$direct_response" ]; then
    local direct_slug direct_category
    direct_slug=$(echo "$direct_response" | jq -r '.data[0].slug // empty')
    direct_category=$(echo "$direct_response" | jq -r '.data[0].recipeCategories[0].slug // empty')

    if [ -n "$direct_slug" ] && [ "$direct_category" = "framework-oss-examples" ]; then
      # User searched for a framework directly (e.g., "django")
      echo -e "${GREEN}Found framework recipe: ${direct_slug}${NC}" >&2
      echo "${direct_slug}:framework"
      return 0
    fi
  fi

  # Step 2: Find the framework slug for runtime search
  local framework_slug
  framework_slug=$(find_framework_slug "$runtime")

  if [ -z "$framework_slug" ]; then
    echo -e "${YELLOW}⚠ Unknown runtime: ${runtime}${NC}" >&2
    return 1
  fi

  echo -e "${CYAN}Found framework: ${framework_slug}${NC}" >&2

  # Step 3: Query recipes for this framework
  local recipes_url="${RECIPE_DATA_API}/recipes?filters%5BrecipeCategories%5D%5Bslug%5D%5B%24ne%5D=service-utility&filters%5BrecipeLanguageFrameworks%5D%5Bslug%5D%5B%24in%5D=${framework_slug}&populate%5BrecipeCategories%5D=true&populate%5BrecipeLanguageFrameworks%5D=true"

  local recipes_response
  recipes_response=$(curl -sf "$recipes_url" 2>/dev/null)

  if [ -z "$recipes_response" ]; then
    echo -e "${YELLOW}⚠ Could not fetch recipes${NC}" >&2
    return 1
  fi

  # Step 4: For runtime searches, ONLY use hello-world recipes
  # (framework recipes should not be used for basic runtime skeleton)
  local hello_world_recipe
  hello_world_recipe=$(echo "$recipes_response" | jq -r '.data[] | select(.recipeCategories[]?.slug == "hello-world-examples") | .slug' | head -1)

  if [ -n "$hello_world_recipe" ]; then
    echo -e "${GREEN}Found hello-world recipe: ${hello_world_recipe}${NC}" >&2
    echo "${hello_world_recipe}:hello-world"
    return 0
  fi

  # No hello-world recipe for this runtime - caller should fall back to docs
  local framework_recipes
  framework_recipes=$(echo "$recipes_response" | jq -r '.data[] | select(.recipeCategories[]?.slug == "framework-oss-examples") | .slug' | tr '\n' ', ' | sed 's/,$//')

  if [ -n "$framework_recipes" ]; then
    echo -e "${YELLOW}⚠ Only framework recipes exist (${framework_recipes}) - use docs for basic ${runtime} skeleton${NC}" >&2
  else
    echo -e "${YELLOW}⚠ No recipes for: ${runtime}${NC}" >&2
  fi

  return 1
}

# Quick search: recipe + pattern extraction
quick_search() {
  local runtime="$1"
  local managed_service="${2:-}"

  echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BOLD}Quick Recipe Search: $runtime${NC}" "${managed_service:+${BOLD}+ $managed_service${NC}}"
  echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo ""

  local api_recipe_found="false"
  local recipe_type="none"  # hello-world, framework, or docs

  # Search recipe API
  local recipe_result
  recipe_result=$(find_recipe_for_runtime "$runtime")

  if [ -n "$recipe_result" ]; then
    # Parse result: recipe_slug:category_type
    local recipe_slug="${recipe_result%%:*}"
    recipe_type="${recipe_result##*:}"

    echo ""
    if [ "$recipe_type" = "hello-world" ]; then
      echo -e "${GREEN}✓ Found hello-world recipe: ${recipe_slug}${NC}"
      echo "  (Ideal for skeleton dev/stage setup)"
    else
      echo -e "${YELLOW}⚠ Found framework recipe: ${recipe_slug}${NC}"
      echo "  (Framework-specific - may have extra config, consider using docs for basic runtime)"
    fi
    echo ""

    api_recipe_found="true"
    fetch_recipe_from_api "$runtime" "$recipe_slug"

    # Store recipe type for evidence
    echo "$recipe_type" > /tmp/recipe_type
  else
    echo ""
    echo -e "${YELLOW}⚠ No recipe exists for ${runtime}${NC}"
    echo "Falling back to documentation..."
    echo ""

    # Try to fetch from documentation instead
    if fetch_docs_fallback "$runtime"; then
      api_recipe_found="true"
      recipe_type="docs"
      echo "docs" > /tmp/recipe_type
    else
      echo ""
      echo -e "${RED}✗ Could not fetch documentation for: ${runtime}${NC}"
      echo ""
      # Mark that we searched but found nothing
      echo "none" > /tmp/recipe_type
    fi
  fi

  # Store search status for create_evidence_file
  echo "true:${api_recipe_found}" > /tmp/api_search_status

  if [[ -n "$managed_service" ]]; then
    echo ""
    echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}Managed service: ${managed_service}${NC}"
    echo ""
    echo "Fetching documentation for ${managed_service}..."
    # Fetch managed service docs
    local managed_url="${DOCS_BASE_URL}/${managed_service}/overview.md"
    local managed_docs
    managed_docs=$(curl -sf "$managed_url" 2>/dev/null)
    if [ -n "$managed_docs" ]; then
      echo -e "${GREEN}✓ Documentation found${NC}"
      echo "  → ${DOCS_BASE_URL}/${managed_service}/overview"
      # Extract version from docs
      local managed_version
      managed_version=$(echo "$managed_docs" | grep -oE "${managed_service}@[0-9a-z.]+" | head -1)
      [ -n "$managed_version" ] && echo "  → Version: $managed_version"
    else
      echo -e "${YELLOW}⚠ Documentation not found at: ${managed_url}${NC}"
      echo "  Try: ${DOCS_BASE_URL}/${managed_service}/overview"
    fi
  fi

  echo ""
  echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

  # Create evidence file for Gate 0
  create_evidence_file "$runtime" "$managed_service"
}

# Create evidence file (Gate 0 compliance)
create_evidence_file() {
  local runtime="$1"
  local managed_service="${2:-}"

  local evidence_file="/tmp/recipe_review.json"

  echo -e "${BOLD}Creating evidence file: $evidence_file${NC}"
  echo ""

  # Build recipes found list
  local recipes_json="{"
  local recipes
  recipes=$(get_recipe_patterns "$runtime")
  if [[ -n "$recipes" ]]; then
    for recipe in $recipes; do
      recipes_json+="\"$recipe\": {\"url\": \"${RECIPE_BASE_URL}/${recipe}\", \"reviewed\": true},"
    done
  fi
  recipes_json="${recipes_json%,}}"

  # Check for fetched patterns from API
  local fetched_patterns=""
  if [ -f /tmp/fetched_patterns.json ]; then
    fetched_patterns=$(cat /tmp/fetched_patterns.json)
  fi

  # Build patterns based on runtime (or use fetched data)
  local runtime_pattern="{}"

  # First check if we have fetched patterns for this runtime
  if [ -n "$fetched_patterns" ] && echo "$fetched_patterns" | grep -q "\"runtime\": \"$runtime\""; then
    # Use fetched patterns
    local fetched_versions
    fetched_versions=$(echo "$fetched_patterns" | grep -o '"versions_found": \[[^]]*\]' | sed 's/"versions_found": //')
    local fetched_base
    fetched_base=$(echo "$fetched_patterns" | grep -o '"runtime_base": "[^"]*"' | sed 's/"runtime_base": "//' | tr -d '"')
    local fetched_alpine
    fetched_alpine=$(echo "$fetched_patterns" | grep -o '"alpine_base": "[^"]*"' | sed 's/"alpine_base": "//' | tr -d '"')
    local fetched_cache
    fetched_cache=$(echo "$fetched_patterns" | grep -o '"has_cache": [a-z]*' | sed 's/"has_cache": //')
    local fetched_source
    fetched_source=$(echo "$fetched_patterns" | grep -o '"source": "[^"]*"' | sed 's/"source": "//' | tr -d '"')

    [ -z "$fetched_versions" ] && fetched_versions='["'$runtime'@1"]'
    [ -z "$fetched_base" ] && fetched_base="${runtime}@1"
    [ "$fetched_alpine" = "null" ] && fetched_alpine=""
    [ -z "$fetched_cache" ] && fetched_cache="true"
    [ -z "$fetched_source" ] && fetched_source="api"

    runtime_pattern="{
      \"valid_versions\": ${fetched_versions},
      \"dev_runtime_base\": \"${fetched_base}\",
      \"prod_runtime_base\": \"${fetched_alpine:-${fetched_base}}\",
      \"dev_os\": \"ubuntu\",
      \"build_cache\": ${fetched_cache},
      \"source\": \"${fetched_source}\"
    }"
  fi
  # No local patterns - recipe API and docs are the only sources of truth

  # Managed service patterns also come from fetched data
  local managed_pattern="{}"

  local managed_json="null"
  [ -n "$managed_service" ] && managed_json="[\"${managed_service}\"]"

  # Determine if we have complete patterns
  local is_verified="true"
  local fetch_required="null"
  local pattern_source="none"

  # Determine source and guidance based on recipe type
  local has_ready_import="false"
  local config_guidance=""

  # Read recipe type from temp file (set by quick_search)
  local recipe_type="none"
  if [ -f /tmp/recipe_type ]; then
    recipe_type=$(cat /tmp/recipe_type)
    rm -f /tmp/recipe_type
  fi

  # Check if we have patterns
  if [ "$runtime_pattern" = "{}" ]; then
    is_verified="false"
    pattern_source="none"
    config_guidance="No patterns available - check documentation manually at ${DOCS_BASE_URL}/${runtime}/overview"
    fetch_required="\"${DOCS_BASE_URL}/${runtime}/overview\""
  elif [ "$recipe_type" = "hello-world" ]; then
    # Hello-world recipe - ideal for skeleton imports
    pattern_source="recipe_hello_world"
    has_ready_import="true"
    config_guidance="Hello-world recipe provides clean dev/stage skeleton - use /tmp/fetched_recipe.md directly"
    is_verified="true"
  elif [ "$recipe_type" = "framework" ]; then
    # Framework recipe - may have extra framework-specific config
    pattern_source="recipe_framework"
    has_ready_import="true"
    config_guidance="Framework recipe found - may have framework-specific config. For basic runtime skeleton, consider using docs at ${DOCS_BASE_URL}/${runtime}/overview instead"
    is_verified="true"
  elif [ "$recipe_type" = "docs" ] || echo "$runtime_pattern" | grep -q '"source": "docs"'; then
    # Documentation fallback
    pattern_source="documentation"
    has_ready_import="false"
    config_guidance="Documentation provides examples - construct your own import.yml with dev (appdev) and stage (appstage) services. Review /tmp/fetched_docs.md"
    is_verified="true"
  else
    # API source but unknown type
    pattern_source="recipe_api"
    has_ready_import="true"
    config_guidance="Recipe found - use /tmp/fetched_recipe.md for import.yml and zerops.yml"
    is_verified="true"
  fi

  # Clean up temp files
  rm -f /tmp/fetched_patterns.json /tmp/fetched_runtime 2>/dev/null

  # Get session_id for evidence validation consistency
  local session_id
  session_id=$(cat /tmp/claude_session 2>/dev/null || echo "standalone-$(date +%s)")

  cat > "$evidence_file" <<EOF
{
  "session_id": "${session_id}",
  "request": "create ${runtime} app${managed_service:+ with ${managed_service}}",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "runtimes_identified": ["${runtime}@1"],
  "managed_services_identified": ${managed_json},

  "recipes_found": $recipes_json,

  "patterns_extracted": {
    "zerops_yml_structure": "zerops: -> - setup: NAME -> build/run",
    "import_yml_valid_fields": ["hostname", "type", "mode", "zeropsSetup", "buildFromGit", "enableSubdomainAccess"],

    "runtime_patterns": {
      "${runtime}": $runtime_pattern
    },

    "managed_service_patterns": $managed_pattern,

    "env_var_pattern": "explicit_in_zerops_yml",
    "env_var_example": {
      "DB_HOST": "\${db_hostname}",
      "DB_PORT": "\${db_port}",
      "DB_USER": "\${db_user}",
      "DB_PASS": "\${db_password}",
      "DB_NAME": "\${db_dbName}"
    },

    "dev_vs_prod": {
      "dev": {
        "deploy_files": ".",
        "runtime_base": "${runtime}@1",
        "os": "ubuntu",
        "start": "zsc noop --silent",
        "purpose": "Full source for iterative development"
      },
      "prod": {
        "deploy_files": "./app",
        "runtime_base": "alpine@3.21",
        "start": "./app",
        "purpose": "Binary only for production"
      }
    }
  },

  "validation_rules": [
    "Must use cache: true for build",
    "Prod runtime MUST be alpine for compiled languages",
    "Dev runtime MUST use zsc noop --silent",
    "Env vars MUST be explicit in zerops.yml",
    "Build commands MUST reference entry file explicitly"
  ],

  "verified": ${is_verified},
  "fetch_required": ${fetch_required},
  "pattern_source": "${pattern_source}",
  "has_ready_import_yml": ${has_ready_import},
  "configuration_guidance": "${config_guidance}",
  "tool": "recipe-search.sh",
  "tool_version": "1.1"
}
EOF

  echo -e "${GREEN}✓ Evidence file created: $evidence_file${NC}"
  echo ""

  # Check if API search was done
  local api_status=""
  if [ -f /tmp/api_search_status ]; then
    api_status=$(cat /tmp/api_search_status)
    rm -f /tmp/api_search_status
  fi
  local api_searched=$(echo "$api_status" | cut -d: -f1)
  local api_found=$(echo "$api_status" | cut -d: -f2)

  if [ "$is_verified" = "true" ]; then
    echo "This file satisfies Gate 0 (RECIPE_DISCOVERY) requirements."
    echo ""
    echo "Next steps:"
    echo "  1. Review patterns in: $evidence_file"
    echo "  2. Create import.yml using the patterns"
    echo "  3. Run: .zcp/workflow.sh extend import.yml"
  elif [ "$api_searched" = "true" ] && [ "$api_found" = "false" ]; then
    # API was searched but no recipe found
    echo -e "${YELLOW}⚠ No Zerops recipe exists for: ${runtime}${NC}"
    echo ""
    echo "The Zerops recipe API was searched but no recipe for '${runtime}' exists."
    echo ""
    echo "To proceed, check the documentation:"
    echo "  ${DOCS_BASE_URL}/${runtime}/overview"
    echo ""
    echo "Then create import.yml and zerops.yml manually based on:"
    echo "  ${DOCS_BASE_URL}/references/import"
    echo "  ${DOCS_BASE_URL}/references/zeropsyml"
    echo ""
    echo "Gate 0 will pass once you have working configuration."
  else
    # No local patterns and API wasn't searched (shouldn't happen now)
    echo -e "${YELLOW}⚠ Patterns incomplete for: ${runtime}${NC}"
    echo ""
    echo "Check documentation:"
    echo "  ${DOCS_BASE_URL}/${runtime}/overview"
    echo ""
    echo "Gate 0 will pass once you have working configuration."
  fi
  echo ""

  # Clean up temp files
  rm -f /tmp/api_search_result 2>/dev/null
}

# Main command router
main() {
  if [[ $# -eq 0 ]]; then
    show_help
    exit 0
  fi

  local command="$1"
  shift

  case "$command" in
    recipe)
      if [[ $# -lt 1 ]]; then
        echo -e "${RED}Error: recipe command requires runtime argument${NC}"
        echo "Usage: recipe-search.sh recipe <runtime> [<managed-service>]"
        exit 1
      fi
      search_recipe "$@"
      ;;

    pattern)
      if [[ $# -lt 1 ]]; then
        echo -e "${RED}Error: pattern command requires service-type argument${NC}"
        echo "Usage: recipe-search.sh pattern <service-type>"
        exit 1
      fi
      extract_patterns "$@"
      ;;

    docs)
      if [[ $# -lt 1 ]]; then
        echo -e "${RED}Error: docs command requires topic argument${NC}"
        echo "Usage: recipe-search.sh docs <topic>"
        exit 1
      fi
      local topic="$1"
      echo -e "${BOLD}Documentation for: $topic${NC}"
      echo ""
      local docs_paths
      docs_paths=$(get_docs_paths "$topic")
      if [[ -n "$docs_paths" ]]; then
        for path in $docs_paths; do
          echo "  ${DOCS_BASE_URL}/${path}"
        done
      else
        echo -e "${YELLOW}⚠ No known docs for: $topic${NC}"
        echo ""
        echo "Try: ${DOCS_BASE_URL}/search?q=${topic}"
      fi
      echo ""
      ;;

    version|versions)
      if [[ $# -lt 1 ]]; then
        echo -e "${RED}Error: version command requires service-type argument${NC}"
        echo "Usage: recipe-search.sh version <service-type>"
        exit 1
      fi
      get_versions "$@"
      ;;

    field|fields)
      if [[ $# -lt 1 ]]; then
        echo -e "${RED}Error: field command requires yaml-section argument${NC}"
        echo "Usage: recipe-search.sh field <yaml-section>"
        exit 1
      fi
      get_fields "$@"
      ;;

    quick)
      if [[ $# -lt 1 ]]; then
        echo -e "${RED}Error: quick command requires runtime argument${NC}"
        echo "Usage: recipe-search.sh quick <runtime> [<managed-service>]"
        exit 1
      fi
      quick_search "$@"
      ;;

    help|-h|--help)
      show_help
      ;;

    *)
      echo -e "${RED}Error: Unknown command: $command${NC}"
      echo ""
      show_help
      exit 1
      ;;
  esac
}

main "$@"
