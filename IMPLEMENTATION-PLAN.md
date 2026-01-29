# Bootstrap Data Flow Fix - Implementation Plan

## Executive Summary

Fix fragmented data flow where versions are fetched but discarded. Single source of truth: `docs.zerops.io/data.json`. **Zero hardcoded fallbacks.**

## Data.json Actual Structure (VERIFIED)

```json
{
  "valkey": {
    "import": [["valkey@7.2"]],
    "readable": ["7.2"]
  },
  "go": {
    "default": "1.22",
    "base": [["go@1.22", "go@1", "golang@1"]],
    "import": [["go@1.22", "go@1", "golang@1"]],
    "readable": ["1.22"]
  },
  "postgresql": {
    "import": [["postgresql@17"], ["postgresql@16"], ["postgresql@14"]],
    "readable": ["17 (17.5)", "16 (16.9)", "14 (14.18)"]
  }
}
```

**Key:** `import[0][0]` = default version, `base[0]` = base images array (runtimes only)

---

## New Data Flow

```
docs.zerops.io/data.json
         │
         ▼
   resolve-types.sh
   ├─ validates types exist
   ├─ extracts versions: .import[0][0]
   └─ extracts bases: .base[0]
         │
         ▼
      plan.sh
   stores FULL objects:
   {
     runtimes: [{type, version, base}],
     managed_services: [{type, version}]
   }
         │
         ▼
      plan.json
         │
    ┌────┴────┐
    ▼         ▼
import-gen  recipe-search
(reads ver  (reads ver from
from plan)  plan, NO grep)
```

---

## File Changes (Execution Order)

### PHASE 1: resolve-types.sh - Add Version Functions

**File:** `zcp/.zcp/lib/bootstrap/resolve-types.sh`

#### 1.1 ADD after line 72 (after `type_exists` function):

```bash
# Get default import version from data.json
# Returns first element of first import array: .import[0][0]
get_default_version() {
    local type_name="$1"
    ensure_data_json || return 1
    jq -r --arg t "$type_name" '.[$t].import[0][0] // empty' "$CACHE_FILE"
}

# Get base images array for runtime types
# Returns first base array: .base[0]
get_base_images() {
    local type_name="$1"
    ensure_data_json || return 1
    jq -c --arg t "$type_name" '.[$t].base[0] // []' "$CACHE_FILE"
}

# Check if type has base images (is a runtime that can build code)
is_runtime_type() {
    local type_name="$1"
    ensure_data_json || return 1
    local has_base
    has_base=$(jq -r --arg t "$type_name" '.[$t] | has("base")' "$CACHE_FILE" 2>/dev/null)
    [ "$has_base" = "true" ]
}
```

#### 1.2 DELETE lines 74-88 entirely:

```bash
# DELETE THIS BLOCK:
# Known runtime types (fallback when data.json unavailable)
KNOWN_RUNTIMES="go golang nodejs bun python php rust java dotnet"
KNOWN_SERVICES="postgresql mariadb mysql valkey keydb rabbitmq nats elasticsearch meilisearch typesense kafka clickhouse qdrant objectstorage sharedstorage mongodb"

# Check if type is a known runtime (fallback mode)
is_known_runtime() {
    local type_name="$1"
    [[ " $KNOWN_RUNTIMES " =~ " $type_name " ]]
}

# Check if type is a known service (fallback mode)
is_known_service() {
    local type_name="$1"
    [[ " $KNOWN_SERVICES " =~ " $type_name " ]]
}
```

#### 1.3 MODIFY `get_type_category()` function (lines 90-114):

**REPLACE entire function with:**

```bash
# Determine if type is a runtime (can run user code) or managed service
# Uses is_runtime_type() which checks for "base" field in data.json
get_type_category() {
    local type_name="$1"
    ensure_data_json || { echo "unknown"; return; }

    if is_runtime_type "$type_name"; then
        echo "runtime"
    else
        # Has entry in data.json but no base = managed service
        if type_exists "$type_name"; then
            echo "managed"
        else
            echo "unknown"
        fi
    fi
}
```

#### 1.4 MODIFY `resolve_types()` function (lines 145-231):

**REPLACE entire function with:**

```bash
# Main resolution function
# Returns JSON with types, versions, and base images
resolve_types() {
    local input="$*"
    input=$(echo "$input" | tr ',' ' ' | tr '[:upper:]' '[:lower:]')

    # REQUIRE data.json - no fallback mode
    if ! ensure_data_json; then
        jq -n '{
            success: false,
            runtimes: [],
            services: [],
            errors: ["Cannot fetch data.json - network required"],
            warnings: [],
            flags: {runtime: "", services: ""}
        }'
        return 1
    fi

    local runtimes=() services=() errors=() warnings=()
    local runtime_objects='[]'
    local service_objects='[]'

    for term in $input; do
        [[ "$term" == --* ]] && continue
        [ -z "$term" ] && continue

        local base_type="${term%%@*}"

        # Apply alias first
        local resolved="${ALIASES[$base_type]:-$base_type}"
        [ "$base_type" != "$resolved" ] && warnings+=("'$base_type' resolved to '$resolved'")

        # Validate against data.json
        if type_exists "$resolved"; then
            local category version base_json
            category=$(get_type_category "$resolved")
            version=$(get_default_version "$resolved")

            if [ -z "$version" ]; then
                errors+=("No import version found for '$resolved'")
                continue
            fi

            if [ "$category" = "runtime" ]; then
                if [[ ! " ${runtimes[*]:-} " =~ " ${resolved} " ]]; then
                    runtimes+=("$resolved")
                    base_json=$(get_base_images "$resolved")
                    runtime_objects=$(echo "$runtime_objects" | jq \
                        --arg t "$resolved" \
                        --arg v "$version" \
                        --argjson b "$base_json" \
                        '. + [{type: $t, version: $v, base: $b}]')
                fi
            else
                if [[ ! " ${services[*]:-} " =~ " ${resolved} " ]]; then
                    services+=("$resolved")
                    service_objects=$(echo "$service_objects" | jq \
                        --arg t "$resolved" \
                        --arg v "$version" \
                        '. + [{type: $t, version: $v}]')
                fi
            fi
        else
            local suggestions
            suggestions=$(get_suggestions "$term")
            if [ -n "$suggestions" ]; then
                errors+=("Unknown type: '$term'. Try: $suggestions")
            else
                errors+=("Unknown type: '$term'")
            fi
        fi
    done

    # Build JSON output
    local runtimes_csv services_csv
    runtimes_csv=$(printf '%s\n' "${runtimes[@]:-}" | grep -v '^$' | paste -sd ',' -)
    services_csv=$(printf '%s\n' "${services[@]:-}" | grep -v '^$' | paste -sd ',' -)

    local errors_json warnings_json
    errors_json=$(printf '%s\n' "${errors[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')
    warnings_json=$(printf '%s\n' "${warnings[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')

    local success="true"
    [ ${#errors[@]} -gt 0 ] && success="false"
    [ ${#runtimes[@]} -eq 0 ] && { success="false"; errors_json=$(echo "$errors_json" | jq '. + ["No runtimes specified"]'); }

    jq -n \
        --argjson runtimes "$runtime_objects" \
        --argjson services "$service_objects" \
        --argjson errors "$errors_json" \
        --argjson warnings "$warnings_json" \
        --argjson success "$success" \
        --arg rt_csv "$runtimes_csv" \
        --arg svc_csv "$services_csv" \
        '{
            success: $success,
            runtimes: $runtimes,
            services: $services,
            errors: $errors,
            warnings: $warnings,
            flags: {
                runtime: $rt_csv,
                services: $svc_csv
            }
        }'
}
```

#### 1.5 MODIFY export line (line 270):

**REPLACE:**
```bash
export -f ensure_data_json data_json_available type_exists is_known_runtime is_known_service get_type_category resolve_type resolve_types
```

**WITH:**
```bash
export -f ensure_data_json data_json_available type_exists get_default_version get_base_images is_runtime_type get_type_category resolve_type get_suggestions resolve_types
```

---

### PHASE 2: plan.sh - Store Version Objects

**File:** `zcp/.zcp/lib/bootstrap/steps/plan.sh`

#### 2.1 REPLACE lines 47-82 (resolution section) with:

```bash
    # ============================================================
    # AUTOMATIC TYPE RESOLUTION (via resolve-types.sh)
    # ============================================================
    # Returns full objects with versions from data.json

    local all_inputs="$runtime"
    [ -n "$services" ] && all_inputs="$all_inputs $services"

    local resolved_json
    if type resolve_types &>/dev/null; then
        all_inputs=$(echo "$all_inputs" | tr ',' ' ')
        resolved_json=$(resolve_types $all_inputs)

        local resolution_success
        resolution_success=$(echo "$resolved_json" | jq -r '.success')

        if [ "$resolution_success" != "true" ]; then
            local errors
            errors=$(echo "$resolved_json" | jq -r '.errors | join("; ")')
            json_error "plan" "Type resolution failed: $errors" "$resolved_json" '["Ensure network access to docs.zerops.io", "Run: .zcp/lib/bootstrap/resolve-types.sh --list"]'
            return 1
        fi

        # Show warnings about alias resolution
        local warnings
        warnings=$(echo "$resolved_json" | jq -r '.warnings[]' 2>/dev/null)
        [ -n "$warnings" ] && echo "Type resolution: $warnings" >&2
    else
        json_error "plan" "resolve-types.sh not loaded" '{}' '["Source resolve-types.sh first"]'
        return 1
    fi
```

#### 2.2 REPLACE lines 84-141 (array building) with:

```bash
    # Extract runtime objects with versions
    local runtime_objects
    runtime_objects=$(echo "$resolved_json" | jq '.runtimes')

    # Extract service objects with versions
    local service_objects
    service_objects=$(echo "$resolved_json" | jq '.services')

    # Get type names for prefix matching
    local runtime_names
    runtime_names=$(echo "$runtime_objects" | jq -r '.[].type' | tr '\n' ' ')
    local runtime_array=()
    read -ra runtime_array <<< "$runtime_names"

    local prefix_array=()
    IFS=',' read -ra prefix_array <<< "$prefix"

    # Validate we have at least one runtime
    if [ ${#runtime_array[@]} -eq 0 ] || [ -z "${runtime_array[0]}" ]; then
        json_error "plan" "No valid runtimes specified" '{}' '["At least one runtime required: --runtime go"]'
        return 1
    fi

    # Validate prefixes
    for pfx in "${prefix_array[@]}"; do
        if [[ ! "$pfx" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || [ ${#pfx} -gt 58 ]; then
            json_error "plan" "Invalid prefix: $pfx" '{}' '["Must be lowercase alphanumeric, may contain hyphens, max 58 chars"]'
            return 1
        fi
    done

    # If fewer prefixes than runtimes, use first prefix for all
    local num_runtimes=${#runtime_array[@]}
    local num_prefixes=${#prefix_array[@]}

    if [ $num_prefixes -lt $num_runtimes ]; then
        for ((i=num_prefixes; i<num_runtimes; i++)); do
            prefix_array+=("${prefix_array[0]}")
        done
    fi

    # Build hostnames for each runtime
    local dev_hostnames=()
    local stage_hostnames=()

    for ((i=0; i<num_runtimes; i++)); do
        dev_hostnames+=("${prefix_array[$i]}dev")
        stage_hostnames+=("${prefix_array[$i]}stage")
    done

    # Build JSON arrays for hostnames
    local dev_hostnames_json stage_hostnames_json
    dev_hostnames_json=$(printf '%s\n' "${dev_hostnames[@]}" | jq -R . | jq -s .)
    stage_hostnames_json=$(printf '%s\n' "${stage_hostnames[@]}" | jq -R . | jq -s .)
```

#### 2.3 REPLACE lines 143-158 (plan_data building) with:

```bash
    local plan_data
    plan_data=$(jq -n \
        --argjson runtimes "$runtime_objects" \
        --argjson managed "$service_objects" \
        --argjson dev_hosts "$dev_hostnames_json" \
        --argjson stage_hosts "$stage_hostnames_json" \
        --arg ha "$ha_mode" \
        '{
            runtimes: $runtimes,
            managed_services: $managed,
            dev_hostnames: $dev_hosts,
            stage_hostnames: $stage_hosts,
            ha_mode: ($ha == "true"),
            dev_hostname: $dev_hosts[0],
            stage_hostname: $stage_hosts[0]
        }')
```

---

### PHASE 3: import-gen.sh - Remove Hardcoded Versions

**File:** `zcp/.zcp/lib/bootstrap/import-gen.sh`

#### 3.1 DELETE lines 18-55 entirely (get_service_version function):

```bash
# DELETE THIS ENTIRE FUNCTION:
get_service_version() {
    local service_type="$1"

    # Check if recipe_review.json exists and has version info
    if [ -f "/tmp/recipe_review.json" ]; then
        ...
    fi

    # Defaults from docs.zerops.io/data.json (2025-01)
    case "$service_type" in
        go) echo "go@1" ;;
        nodejs) echo "nodejs@22" ;;
        ... ALL HARDCODED VERSIONS ...
    esac
}
```

#### 3.2 MODIFY generate_import_yml function signature and body:

**REPLACE lines 57-147 with:**

```bash
# Generate import.yml
# Usage: generate_import_yml --runtime-versions '{"go":"go@1.22"}' --service-versions '{"valkey":"valkey@7.2"}' --prefix app --ha
# Version maps are JSON objects: {"type": "version@X"}
generate_import_yml() {
    local runtime_versions='{}' service_versions='{}' prefixes="app" ha_mode="false" output_file="/tmp/bootstrap_import.yml"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --runtime-versions) runtime_versions="$2"; shift 2 ;;
            --service-versions) service_versions="$2"; shift 2 ;;
            --prefix|--prefixes) prefixes="$2"; shift 2 ;;
            --ha) ha_mode="true"; shift ;;
            --output) output_file="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    # Extract runtimes and their versions
    local runtime_types
    runtime_types=$(echo "$runtime_versions" | jq -r 'keys[]')

    if [ -z "$runtime_types" ]; then
        echo "ERROR: --runtime-versions required with at least one runtime" >&2
        return 1
    fi

    # Split prefixes
    IFS=',' read -ra prefix_arr <<< "$prefixes"

    local runtime_arr=()
    while IFS= read -r rt; do
        [ -n "$rt" ] && runtime_arr+=("$rt")
    done <<< "$runtime_types"

    local num_runtimes=${#runtime_arr[@]}
    local num_prefixes=${#prefix_arr[@]}

    # If fewer prefixes than runtimes, use runtime name as prefix
    if [ $num_prefixes -lt $num_runtimes ]; then
        for ((i=num_prefixes; i<num_runtimes; i++)); do
            prefix_arr+=("${runtime_arr[$i]}")
        done
    fi

    # Start YAML
    {
        echo "# Generated by ZCP Bootstrap"
        echo "# Versions from docs.zerops.io/data.json"
        echo "services:"

        # Managed services FIRST with priority: 10
        local svc_types
        svc_types=$(echo "$service_versions" | jq -r 'keys[]')

        for svc_name in $svc_types; do
            [ -z "$svc_name" ] && continue

            local hostname version mode
            hostname=$(get_managed_hostname "$svc_name")
            version=$(echo "$service_versions" | jq -r --arg s "$svc_name" '.[$s]')
            mode=$( [ "$ha_mode" = "true" ] && echo "HA" || echo "NON_HA" )

            echo "  - hostname: $hostname"
            echo "    type: $version"
            echo "    mode: $mode"
            echo "    priority: 10"
            echo ""
        done

        # Generate dev/stage pairs for EACH runtime
        for ((i=0; i<num_runtimes; i++)); do
            local rt="${runtime_arr[$i]}"
            local px="${prefix_arr[$i]}"
            local runtime_version
            runtime_version=$(echo "$runtime_versions" | jq -r --arg r "$rt" '.[$r]')

            # Dev runtime
            echo "  - hostname: ${px}dev"
            echo "    type: $runtime_version"
            echo "    startWithoutCode: true"
            echo "    verticalAutoscaling:"
            echo "      minRam: 0.5"
            echo ""

            # Stage runtime
            echo "  - hostname: ${px}stage"
            echo "    type: $runtime_version"
            echo "    startWithoutCode: true"
            echo ""
        done

    } > "$output_file"

    echo "$output_file"
}

export -f get_managed_hostname generate_import_yml
```

---

### PHASE 4: generate-import.sh - Pass Versions from Plan

**File:** `zcp/.zcp/lib/bootstrap/steps/generate-import.sh`

#### 4.1 REPLACE lines 18-42 with:

```bash
    # Extract runtime versions from plan objects
    # Plan now has: {runtimes: [{type, version, base}], managed_services: [{type, version}]}
    local runtime_versions service_versions

    # Build runtime version map: {"go": "go@1.22", "bun": "bun@1.2"}
    # Uses null-safe access with fallback to empty object
    runtime_versions=$(echo "$plan" | jq '(.runtimes // []) | [.[] | {(.type): .version}] | add // {}')

    # Build service version map: {"valkey": "valkey@7.2", "postgresql": "postgresql@17"}
    service_versions=$(echo "$plan" | jq '(.managed_services // []) | [.[] | {(.type): .version}] | add // {}')

    # Extract prefixes from dev_hostnames
    local prefixes
    prefixes=$(echo "$plan" | jq -r 'if .dev_hostnames then .dev_hostnames | map(sub("dev$"; "")) | join(",") else "app" end')

    local ha_mode
    ha_mode=$(echo "$plan" | jq -r '.ha_mode // false')

    local import_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml"
```

#### 4.2 REPLACE lines 50-53 with:

```bash
    if [ -f "$import_gen" ]; then
        source "$import_gen"
        local result
        # Direct function call - avoid eval with JSON arguments
        local gen_ha_flag=""
        [ "$ha_mode" = "true" ] && gen_ha_flag="--ha"

        result=$(generate_import_yml \
            --runtime-versions "$runtime_versions" \
            --service-versions "$service_versions" \
            --prefix "$prefixes" \
            --output "$import_file" \
            $gen_ha_flag)
```

---

### PHASE 5: finalize.sh - Read Versions from Plan

**File:** `zcp/.zcp/lib/bootstrap/steps/finalize.sh`

#### 5.1 MODIFY lines 44-49 to extract version info:

**REPLACE with:**

```bash
    # Get runtimes with versions from plan (new format: [{type, version, base}])
    local runtime_objects
    runtime_objects=$(echo "$plan" | jq '.runtimes // []')

    # Get managed services with versions (new format: [{type, version}])
    local service_objects
    service_objects=$(echo "$plan" | jq '.managed_services // []')

    # Extract hostname lists
    local dev_hostnames stage_hostnames
    dev_hostnames=$(echo "$plan" | jq -r '.dev_hostnames // [.dev_hostname] | .[]')
    stage_hostnames=$(echo "$plan" | jq -r '.stage_hostnames // [.stage_hostname] | .[]')
```

#### 5.2 REPLACE lines 95-110 (runtime version extraction) with:

```bash
        # Get runtime info from plan objects (use .[]? for null-safe iteration)
        local runtime_obj runtime_version base_images recipe_file
        runtime_obj=$(echo "$runtime_objects" | jq --arg rt "$runtime" '.[]? | select(.type == $rt)')
        # Use jq --arg for variable interpolation in fallback
        runtime_version=$(echo "$runtime_obj" | jq -r --arg rt "$runtime" '.version // ($rt + "@1")')
        base_images=$(echo "$runtime_obj" | jq -c '.base // []')

        # Recipe file from recipe-search step
        local runtime_recipes
        runtime_recipes=$(echo "$recipe_search_data" | jq '.runtime_recipes // {}')
        recipe_file=$(echo "$runtime_recipes" | jq -r --arg rt "$runtime" '.[$rt].recipe_file // ""')
        [ -z "$recipe_file" ] && recipe_file="${ZCP_TMP_DIR:-/tmp}/recipe_${runtime}.md"
```

#### 5.3 REPLACE lines 121-173 (managed services loop) with:

```bash
        # Use while read to safely iterate JSON objects (avoids word-splitting issues)
        while IFS= read -r svc_obj; do
            [ -z "$svc_obj" ] && continue

            local svc svc_type svc_name env_prefix env_vars reference_doc

            svc=$(echo "$svc_obj" | jq -r '.type')
            svc_type=$(echo "$svc_obj" | jq -r '.version')

            case "$svc" in
                postgresql*|mysql*|mariadb*|mongodb*)
                    svc_name="db"
                    env_prefix="DB"
                    env_vars='["hostname", "port", "user", "password", "dbName", "connectionString"]'
                    ;;
                valkey*|keydb*)
                    svc_name="cache"
                    env_prefix="REDIS"
                    env_vars='["hostname", "port", "password", "connectionString"]'
                    ;;
                rabbitmq*|nats*)
                    svc_name="queue"
                    env_prefix="AMQP"
                    env_vars='["hostname", "port", "user", "password"]'
                    ;;
                elasticsearch*)
                    svc_name="search"
                    env_prefix="ES"
                    env_vars='["hostname", "port", "user", "password"]'
                    ;;
                minio*|objectstorage*)
                    svc_name="storage"
                    env_prefix="S3"
                    env_vars='["hostname", "port", "accessKey", "secretKey"]'
                    ;;
                *)
                    svc_name="$svc"
                    env_prefix=$(echo "$svc" | tr '[:lower:]' '[:upper:]')
                    env_vars='["hostname", "port", "user", "password"]'
                    ;;
            esac

            # Get reference doc from recipe-search step
            reference_doc=$(echo "$service_docs_data" | jq -r --arg s "$svc" '.[$s].doc_file // ""')
            [ -z "$reference_doc" ] && reference_doc="${ZCP_TMP_DIR:-/tmp}/service_${svc}.md"

            managed_info=$(echo "$managed_info" | jq \
                --arg n "$svc_name" \
                --arg t "$svc_type" \
                --arg e "$env_prefix" \
                --argjson ev "$env_vars" \
                --arg rd "$reference_doc" \
                '. + [{name: $n, type: $t, env_prefix: $e, env_vars: $ev, reference_doc: $rd}]')
        done < <(echo "$service_objects" | jq -c '.[]')
```

---

### PHASE 6: steps/recipe-search.sh - Remove Version Grep

**File:** `zcp/.zcp/lib/bootstrap/steps/recipe-search.sh`

#### 6.1 MODIFY fetch_service_doc function (lines 41-79):

**REPLACE lines 50-55 with:**

```bash
    if [ -n "$svc_doc" ]; then
        echo "$svc_doc" > "${tmp_dir}/service_${svc}.md"

        # Get version from plan (passed via environment or state)
        # Use null-safe array access with .[]? to handle empty/null arrays
        local svc_version
        svc_version=$(get_plan 2>/dev/null | jq -r --arg s "$svc" '.managed_services[]? | select(.type == $s) | .version // empty' 2>/dev/null)
        [ -z "$svc_version" ] && svc_version="${svc}@latest"
```

#### 6.2 MODIFY fetch_runtime_recipe function (lines 8-37):

**REPLACE lines 21-36 with:**

```bash
    if [ -f "${tmp_dir}/fetched_recipe.md" ]; then
        mv "${tmp_dir}/fetched_recipe.md" "${tmp_dir}/recipe_${runtime}.md" 2>/dev/null || true

        # Get version from plan - use null-safe array access with .[]?
        local rt_version
        rt_version=$(get_plan 2>/dev/null | jq -r --arg r "$runtime" '.runtimes[]? | select(.type == $r) | .version // empty' 2>/dev/null)
        [ -z "$rt_version" ] && rt_version="${runtime}@1"

        if [ -f "${tmp_dir}/fetched_patterns.json" ]; then
            mv "${tmp_dir}/fetched_patterns.json" "${tmp_dir}/patterns_${runtime}.json" 2>/dev/null || true
        fi
        echo "{\"runtime\":\"$runtime\",\"version\":\"$rt_version\",\"recipe_file\":\"${tmp_dir}/recipe_${runtime}.md\",\"found\":true}" > "$result_file"
    elif [ -f "${tmp_dir}/fetched_docs.md" ]; then
        mv "${tmp_dir}/fetched_docs.md" "${tmp_dir}/recipe_${runtime}.md" 2>/dev/null || true

        local rt_version
        rt_version=$(get_plan 2>/dev/null | jq -r --arg r "$runtime" '.runtimes[]? | select(.type == $r) | .version // empty' 2>/dev/null)
        [ -z "$rt_version" ] && rt_version="${runtime}@1"

        if [ -f "${tmp_dir}/fetched_patterns.json" ]; then
            mv "${tmp_dir}/fetched_patterns.json" "${tmp_dir}/patterns_${runtime}.json" 2>/dev/null || true
        fi
        echo "{\"runtime\":\"$runtime\",\"version\":\"$rt_version\",\"recipe_file\":\"${tmp_dir}/recipe_${runtime}.md\",\"source\":\"docs\",\"found\":true}" > "$result_file"
    else
        local rt_version
        rt_version=$(get_plan 2>/dev/null | jq -r --arg r "$runtime" '.runtimes[]? | select(.type == $r) | .version // empty' 2>/dev/null)
        [ -z "$rt_version" ] && rt_version="${runtime}@1"
        echo "{\"runtime\":\"$runtime\",\"version\":\"$rt_version\",\"recipe_file\":null,\"source\":\"default\",\"found\":false}" > "$result_file"
    fi
```

---

### PHASE 7: Top-level recipe-search.sh - Use data.json

**File:** `zcp/.zcp/recipe-search.sh`

#### 7.1 DELETE line 382:

```bash
# DELETE:
KNOWN_RUNTIMES="go bun nodejs php python"
```

#### 7.2 MODIFY lines 125, 156, 282, 296, 1220:

Replace all `grep -oE "...@[0-9a-z.]+"` patterns with calls to resolve-types.sh functions.

**Example replacement for line 1220:**

```bash
# BEFORE:
managed_version=$(echo "$managed_docs" | grep -oE "${managed_service}@[0-9a-z.]+" | head -1)

# AFTER:
if type get_default_version &>/dev/null; then
    managed_version=$(get_default_version "$managed_service")
else
    managed_version="${managed_service}@latest"
fi
```

---

### PHASE 8: Documentation Updates

**Files to update references in:**
- `zcp/.zcp/lib/help/full.sh` - lines 151, 346
- `zcp/.zcp/lib/help/topics.sh` - lines 267, 757, 880, 886, 1015, 1088, 1118, 1183, 1187, 1191, 1232
- `zcp/.zcp/lib/commands/transition.sh` - line 402

Update documentation to reflect that versions now come from `plan.json` rather than `recipe_review.json`.

---

## Verification Checklist

After implementation, verify:

1. **resolve-types.sh standalone test:**
   ```bash
   source zcp/.zcp/lib/bootstrap/resolve-types.sh
   resolve_types go bun postgresql valkey
   # Should return objects with versions from data.json
   ```

2. **Plan step outputs versions:**
   ```bash
   ./zcp/.zcp/bootstrap.sh step plan --runtime go,bun --services postgresql,valkey
   cat /tmp/bootstrap_plan.json | jq '.runtimes[0].version'
   # Should show "go@1.22" or similar
   ```

3. **Import.yml has correct versions:**
   ```bash
   cat /tmp/bootstrap_import.yml
   # Should show versions from data.json, NOT hardcoded ones
   ```

4. **No hardcoded versions remain:**
   ```bash
   grep -rn '@[0-9]' zcp/.zcp/lib/bootstrap/*.sh | grep -v '#'
   # Should find no hardcoded version strings in active code
   ```

---

## Rollback Plan

If issues arise:
1. Git revert the commits
2. The old code has fallback mode that will still work
3. No data migrations needed - just code changes

---

## New plan.json Schema

```json
{
  "runtimes": [
    {
      "type": "go",
      "version": "go@1.22",
      "base": ["go@1.22", "go@1", "golang@1"]
    },
    {
      "type": "bun",
      "version": "bun@1.2",
      "base": ["bun@1.2", "bun@1"]
    }
  ],
  "managed_services": [
    {
      "type": "postgresql",
      "version": "postgresql@17"
    },
    {
      "type": "valkey",
      "version": "valkey@7.2"
    }
  ],
  "dev_hostnames": ["appdev", "bundev"],
  "stage_hostnames": ["appstage", "bunstage"],
  "ha_mode": false,
  "dev_hostname": "appdev",
  "stage_hostname": "appstage"
}
```
