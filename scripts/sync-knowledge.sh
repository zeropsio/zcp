#!/usr/bin/env bash
# Bidirectional sync between ZCP knowledge and canonical external sources.
#
# Pull uses local clones when available (fast, offline), falls back to GitHub.
# Push always writes to local clones (you commit + push to GitHub yourself).
#
# Synced files are gitignored — run `pull` before build.
set -euo pipefail

# ZCP repo root (this script lives in scripts/)
ZCP_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ZCP_KNOWLEDGE="${ZCP_ROOT}/internal/knowledge"

# Recipe API
RECIPE_API="https://api.zerops.io/api/recipes"

# Local sibling repos (for push — pull uses the API)
LOCAL_DOCS="${DOCS_GUIDES:-$(dirname "$ZCP_ROOT")/docs/apps/docs/content/guides}"
LOCAL_RECIPE_APPS="${RECIPE_APPS:-$(dirname "$ZCP_ROOT")/recipe-apps}"

# ============================================================
# PULL: External → ZCP (before starting work / CI build)
# ============================================================

pull_guides() {
  echo "=== Pulling docs/guides → ZCP knowledge ==="
  local count=0

  # Only works with local docs clone for now (MDX parsing needs local files)
  if [[ ! -d "$LOCAL_DOCS" ]]; then
    echo "  SKIP: no local docs clone at ${LOCAL_DOCS}"
    echo "  Set DOCS_GUIDES=/path/to/docs/guides or clone docs repo"
    return
  fi

  for mdx in "${LOCAL_DOCS}"/*.mdx; do
    [[ -f "$mdx" ]] || continue
    slug=$(basename "$mdx" .mdx)

    if [[ "$slug" == choose-* ]]; then
      target="${ZCP_KNOWLEDGE}/decisions/${slug}.md"
    else
      target="${ZCP_KNOWLEDGE}/guides/${slug}.md"
    fi

    title=$(sed -n '2s/^title: //p' "$mdx")
    {
      echo "# ${title}"
      echo ""
      awk '
        NR==1 && /^---$/ { in_front=1; next }
        in_front && /^---$/ { in_front=0; skip_blanks=1; next }
        in_front { next }
        /^```/ { in_code=!in_code }
        !in_code && /^import / { next }
        skip_blanks && /^$/ { next }
        skip_blanks { skip_blanks=0 }
        {
          while (match($0, /`(zerops:\/\/[^`]+)`/)) {
            inner = substr($0, RSTART+1, RLENGTH-2)
            $0 = substr($0, 1, RSTART-1) inner substr($0, RSTART+RLENGTH)
          }
          print
        }
      ' "$mdx"
    } > "$target"

    echo "  ${slug} → $(basename "$(dirname "$target")")/"
    count=$((count + 1))
  done

  echo "Pulled ${count} files"
}

pull_recipes() {
  echo "=== Pulling ALL recipes from API → ZCP recipes ==="
  local count=0

  # Fetch all recipes except service-utility category
  local api_url="${RECIPE_API}?filters%5BrecipeCategories%5D%5Bslug%5D%5B%24ne%5D=service-utility&populate%5BrecipeCategories%5D=true&populate%5BrecipeLanguageFrameworks%5D%5Bpopulate%5D=*&pagination%5BpageSize%5D=100"
  local api_response
  api_response=$(curl -sfL "$api_url" || true)

  if [[ -z "$api_response" ]]; then
    echo "  ERROR: recipe API not reachable"
    return 1
  fi

  # Get all recipe slugs dynamically from the API response
  local slugs
  slugs=$(echo "$api_response" | jq -r '.data[].slug' 2>/dev/null)

  if [[ -z "$slugs" ]]; then
    echo "  ERROR: no recipes found in API response"
    return 1
  fi

  mkdir -p "${ZCP_KNOWLEDGE}/recipes"

  # Slug remapping for API quirks (e.g., "recipe" → "nodejs-hello-world")
  remap_slug() {
    case "$1" in
      recipe) echo "nodejs-hello-world" ;;
      *)      echo "$1" ;;
    esac
  }

  for api_slug in $slugs; do
    local slug
    slug=$(remap_slug "$api_slug")
    local target="${ZCP_KNOWLEDGE}/recipes/${slug}.md"

    local recipe_json
    recipe_json=$(echo "$api_response" | jq -r --arg s "$api_slug" '.data[] | select(.slug == $s)' 2>/dev/null)

    local name
    name=$(echo "$recipe_json" | jq -r '.name // empty' 2>/dev/null)

    # Get intro (strip markdown links, collapse to single line)
    local intro
    intro=$(echo "$recipe_json" | jq -r '.sourceData.extracts.intro // empty' \
      | sed 's/\[\([^]]*\)\]([^)]*)/ \1/g' \
      | tr '\n' ' ' | sed 's/  */ /g; s/^ *//; s/ *$//')

    # Get knowledge-base from first service that has it (promote H3→H2)
    local kb_content
    kb_content=$(echo "$recipe_json" | jq -r '
      [.sourceData.environments[0].services[]
       | select(.extracts["knowledge-base"] != null and .extracts["knowledge-base"] != "")]
      | first // empty
      | .extracts["knowledge-base"]' 2>/dev/null)

    # Get zerops.yaml from first service that has it
    local yaml_content
    yaml_content=$(echo "$recipe_json" | jq -r '
      [.sourceData.environments[0].services[]
       | select(.zeropsYaml != null and .zeropsYaml != "")]
      | first // empty
      | .zeropsYaml' 2>/dev/null)

    # Skip recipes with no useful content at all
    if [[ -z "$kb_content" && -z "$yaml_content" && -z "$intro" ]]; then
      echo "  SKIP ${slug}: no content in API"
      continue
    fi

    # Build recipe file
    {
      if [[ -n "$intro" ]]; then
        echo "---"
        echo "description: \"${intro}\""
        echo "---"
        echo ""
      fi

      echo "# ${name:-$slug} on Zerops"
      echo ""

      if [[ -n "$kb_content" ]]; then
        echo "$kb_content" | sed 's/^### /## /'
        echo ""
      fi

      if [[ -n "$yaml_content" ]]; then
        echo "## zerops.yml"
        echo ""
        echo "> Reference implementation — learn the patterns, adapt to your project."
        echo ""
        echo '```yaml'
        echo "$yaml_content"
        echo '```'
      fi
    } > "$target"

    echo "  ${slug} → recipes/${slug}.md"
    count=$((count + 1))
  done

  echo "Pulled ${count} recipe files"
}

# ============================================================
# PUSH: ZCP → Distribution targets (needs local clones)
# ============================================================

push_guides() {
  echo "=== Pushing ZCP knowledge → docs/guides ==="

  if [[ ! -d "$LOCAL_DOCS" ]]; then
    echo "  SKIP: no local docs clone at ${LOCAL_DOCS}"
    return
  fi

  mkdir -p "$LOCAL_DOCS"
  local count=0

  for md in "${ZCP_KNOWLEDGE}/guides/"*.md "${ZCP_KNOWLEDGE}/decisions/"*.md; do
    [[ -f "$md" ]] || continue
    slug=$(basename "$md" .md)
    target="${LOCAL_DOCS}/${slug}.mdx"

    if [[ -f "$target" ]]; then
      existing_frontmatter=$(awk 'NR==1 && /^---$/{found=1; next} found && /^---$/{exit} found{print}' "$target")
    else
      existing_frontmatter=""
    fi

    if [[ -n "$existing_frontmatter" ]]; then
      cat > "$target" <<FRONTMATTER
---
${existing_frontmatter}
---

FRONTMATTER
    else
      title=$(grep -m1 '^# ' "$md" | sed 's/^# //')
      [[ -z "$title" ]] && title="$slug"
      description=$(sed -n '/^## TL;DR/,/^##/{/^## TL;DR/d;/^##/d;/^$/d;p;}' "$md" | head -1 | sed 's/"/\\"/g')
      [[ -z "$description" ]] && description="Guide: ${title}"
      cat > "$target" <<FRONTMATTER
---
title: ${title}
description: "${description}"
---

FRONTMATTER
    fi

    sed '1{/^# /d;}' "$md" \
      | awk '/^```/{f=!f} !f{gsub(/(zerops:\/\/[^ ]*\{[^}]+\})/, "`&`")} {print}' \
      >> "$target"

    echo "  guides/${slug}.mdx"
    count=$((count + 1))
  done

  echo "Pushed ${count} guide pages"
}

push_recipes() {
  echo "=== Pushing ZCP recipes → app READMEs + zerops.yaml ==="
  local count=0

  for src in "${ZCP_KNOWLEDGE}/recipes/"*.md; do
    [[ -f "$src" ]] || continue
    local slug
    slug=$(basename "$src" .md)

    # Try common app-repo naming conventions
    local app_dir=""
    for candidate in "${LOCAL_RECIPE_APPS}/${slug}-app" "${LOCAL_RECIPE_APPS}/${slug}"; do
      if [[ -d "$candidate" ]]; then
        app_dir="$candidate"
        break
      fi
    done
    if [[ -z "$app_dir" ]]; then
      continue  # no local clone, skip silently
    fi

    local readme="${app_dir}/README.md"
    [[ -f "$readme" ]] || continue

    # Extract knowledge-base portion (skip frontmatter, skip H1, stop before ## zerops.yml), demote H2→H3
    local fragment
    fragment=$(awk '
      NR==1 && /^---$/ { in_fm=1; next }
      in_fm && /^---$/ { in_fm=0; next }
      in_fm { next }
      /^## zerops\.yml/ { exit }
      { print }
    ' "$src" \
      | sed '/^# /d; s/^## /### /' \
      | awk 'NF{p=1} p' | awk '{lines[NR]=$0} END{while(lines[NR]=="") NR--; for(i=1;i<=NR;i++) print lines[i]}')

    [[ -z "$fragment" ]] && continue

    local frag_file
    frag_file=$(mktemp)
    echo "$fragment" > "$frag_file"

    if grep -q 'ZEROPS_EXTRACT_START:knowledge-base' "$readme" 2>/dev/null; then
      awk -v fragfile="$frag_file" '
        /ZEROPS_EXTRACT_START:knowledge-base/ { print; while ((getline line < fragfile) > 0) print line; skip=1; next }
        /ZEROPS_EXTRACT_END:knowledge-base/ { skip=0 }
        !skip { print }
      ' "$readme" > "${readme}.tmp"
      mv "${readme}.tmp" "$readme"
    else
      {
        cat "$readme"
        echo ""
        echo "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->"
        cat "$frag_file"
        echo "<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->"
      } > "${readme}.tmp"
      mv "${readme}.tmp" "$readme"
    fi
    rm -f "$frag_file"

    # Push zerops.yaml back
    local yaml_content
    yaml_content=$(awk '
      /^## zerops\.yml/ { found=1; next }
      found && /^```yaml/ { in_yaml=1; next }
      found && in_yaml && /^```/ { exit }
      found && in_yaml { print }
    ' "$src")

    if [[ -n "$yaml_content" ]]; then
      local yaml_target=""
      if [[ -f "${app_dir}/zerops.yaml" ]]; then
        yaml_target="${app_dir}/zerops.yaml"
      elif [[ -f "${app_dir}/zerops.yml" ]]; then
        yaml_target="${app_dir}/zerops.yml"
      fi
      if [[ -n "$yaml_target" ]]; then
        echo "$yaml_content" > "$yaml_target"
      fi
    fi

    echo "  ${slug}"
    count=$((count + 1))
  done

  echo "Pushed ${count} recipe fragments"
}

# ============================================================
# Diff check
# ============================================================

show_changes() {
  echo ""
  echo "=== Changes ==="
  case "$1" in
    pull)
      echo "ZCP knowledge changes:"
      cd "${ZCP_ROOT}" && git diff --stat internal/knowledge/ 2>/dev/null && git diff --stat --cached internal/knowledge/ 2>/dev/null || echo "  (no changes or untracked)"
      ;;
    push)
      if [[ -d "$(dirname "$ZCP_ROOT")/docs/.git" ]]; then
        echo "Docs changes:"
        cd "$(dirname "$ZCP_ROOT")/docs" && git diff --stat apps/docs/content/guides/ 2>/dev/null || echo "  (no changes)"
        echo ""
      fi
      echo "App README changes:"
      for dir in "${LOCAL_RECIPE_APPS}"/*/; do
        [[ -d "$dir/.git" ]] || continue
        changes=$(cd "$dir" && git diff --stat README.md zerops.yaml zerops.yml 2>/dev/null)
        [[ -n "$changes" ]] && echo "  $(basename "$dir"): ${changes}"
      done
      ;;
  esac
}

# ============================================================
# Main
# ============================================================

case "${1:-}" in
  pull)
    shift
    case "${1:-all}" in
      guides)  pull_guides ;;
      recipes) pull_recipes ;;
      all)     pull_guides; echo ""; pull_recipes ;;
    esac
    show_changes pull
    ;;
  push)
    shift
    case "${1:-all}" in
      guides)  push_guides ;;
      recipes) push_recipes ;;
      all)     push_guides; echo ""; push_recipes ;;
    esac
    show_changes push
    ;;
  *)
    echo "Usage: $0 {pull|push} [guides|recipes|all]"
    echo ""
    echo "  pull  — Sync from canonical sources into ZCP"
    echo "  push  — Push tested ZCP edits to local app repo clones"
    echo ""
    echo "  guides  — docs/guides/*.mdx ↔ ZCP guides/ + decisions/"
    echo "  recipes — Recipe API ↔ ZCP recipes/ (all recipes, dynamic)"
    echo "  all     — all of the above (default)"
    echo ""
    echo "Environment:"
    echo "  DOCS_GUIDES=/path/to/docs/guides  — override local docs location"
    echo "  RECIPE_APPS=/path/to/recipe-apps   — override local recipe apps location"
    exit 1
    ;;
esac
