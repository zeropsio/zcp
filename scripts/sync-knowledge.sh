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

pull_runtimes() {
  echo "=== Pulling hello-world recipes from API → ZCP recipes ==="
  local runtimes=(bun deno dotnet elixir gleam go java nodejs php python ruby rust)
  local count=0

  # Fetch all hello-world recipes in one API call
  local api_url="${RECIPE_API}?filters%5BrecipeCategories%5D%5Bslug%5D=hello-world-examples&populate%5BrecipeLanguageFrameworks%5D%5Bpopulate%5D=*&populate%5BrecipeCategories%5D=true&pagination%5BpageSize%5D=100"
  local api_response
  api_response=$(curl -sfL "$api_url" || true)

  if [[ -z "$api_response" ]]; then
    echo "  ERROR: recipe API not reachable"
    return 1
  fi

  for runtime in "${runtimes[@]}"; do
    local slug="${runtime}-hello-world"
    local target="${ZCP_KNOWLEDGE}/recipes/${slug}.md"

    # Extract this recipe's data from the API response
    local recipe_json
    recipe_json=$(echo "$api_response" | jq -r --arg s "$slug" '.data[] | select(.slug == $s)' 2>/dev/null)

    if [[ -z "$recipe_json" || "$recipe_json" == "null" ]]; then
      echo "  SKIP ${runtime}: not found in API"
      continue
    fi

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

    # If no knowledge-base in API, keep existing content from current file
    local kb_from_existing=""
    if [[ -z "$kb_content" && -f "$target" ]]; then
      kb_from_existing=$(awk '
        /^---$/ && NR==1 { in_fm=1; next }
        in_fm && /^---$/ { in_fm=0; next }
        in_fm { next }
        /^## zerops\.yml/ { exit }
        { print }
      ' "$target" | sed '1{/^# /d;}')
    fi

    if [[ -z "$kb_content" && -z "$kb_from_existing" ]]; then
      echo "  SKIP ${runtime}: no knowledge-base in API and no existing file"
      continue
    fi

    # Get zerops.yaml from first service that has it
    local yaml_content
    yaml_content=$(echo "$recipe_json" | jq -r '
      [.sourceData.environments[0].services[]
       | select(.zeropsYaml != null and .zeropsYaml != "")]
      | first // empty
      | .zeropsYaml' 2>/dev/null)

    # Determine H1 title from existing file or generate
    local h1=""
    if [[ -f "$target" ]]; then
      h1=$(awk '/^---$/ && NR==1{f=1;next} f && /^---$/{f=0;next} f{next} /^# /{print;exit}' "$target")
    fi
    [[ -z "$h1" ]] && h1="# ${runtime^} Hello World on Zerops"

    # Build recipe file
    {
      if [[ -n "$intro" ]]; then
        echo "---"
        echo "description: \"${intro}\""
        echo "---"
        echo ""
      fi

      echo "$h1"
      echo ""
      if [[ -n "$kb_content" ]]; then
        echo "$kb_content" | sed 's/^### /## /'
      else
        echo "$kb_from_existing"
      fi
      echo ""

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

    echo "  ${runtime} → recipes/${slug}.md"
    count=$((count + 1))
  done

  echo "Pulled ${count} hello-world recipe files"
}

pull_recipes() {
  echo "=== Pulling app README fragments → ZCP recipes ==="
  local mapfile="${ZCP_ROOT}/scripts/recipe-map.txt"

  if [[ ! -f "$mapfile" ]]; then
    echo "  SKIP: no recipe-map.txt"
    return
  fi

  local count=0
  while IFS='=' read -r slug repo; do
    [[ -z "$slug" || "$slug" == \#* ]] && continue
    local target="${ZCP_KNOWLEDGE}/recipes/${slug}.md"

    local readme_content
    readme_content=$(fetch_file "${LOCAL_RECIPE_APPS}/${repo}/README.md" "${GITHUB_ORG}/${repo}" "README.md")
    [[ -z "$readme_content" ]] && continue

    local content
    content=$(echo "$readme_content" \
      | sed -n '/ZEROPS_EXTRACT_START:knowledge-base/,/ZEROPS_EXTRACT_END:knowledge-base/p' \
      | grep -v 'ZEROPS_EXTRACT' || true)

    if [[ -n "$content" ]]; then
      echo "$content" > "$target"
      echo "  ${slug}"
      count=$((count + 1))
    fi
  done < "$mapfile"

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

push_runtimes() {
  echo "=== Pushing ZCP recipes → app READMEs + zerops.yaml ==="
  local runtimes=(bun deno dotnet elixir gleam go java nodejs php python ruby rust)
  local count=0

  for runtime in "${runtimes[@]}"; do
    local src="${ZCP_KNOWLEDGE}/recipes/${runtime}-hello-world.md"
    local app_dir="${LOCAL_RECIPE_APPS}/${runtime}-hello-world-app"

    [[ -f "$src" ]] || continue
    if [[ ! -d "$app_dir" ]]; then
      echo "  SKIP ${runtime}: no local clone at ${app_dir}"
      continue
    fi

    local readme="${app_dir}/README.md"

    # Extract knowledge-base portion (skip frontmatter, skip H1, before ## zerops.yml), demote H2→H3
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

    echo "  ${runtime}"
    count=$((count + 1))
  done

  echo "Pushed ${count} hello-world recipe fragments"
}

push_recipes() {
  echo "=== Pushing ZCP recipes → app README fragments ==="
  local mapfile="${ZCP_ROOT}/scripts/recipe-map.txt"

  if [[ ! -f "$mapfile" ]]; then
    echo "  SKIP: no recipe-map.txt"
    return
  fi

  echo "  TODO: same pattern as push_runtimes, using recipe-map.txt"
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
      guides)   pull_guides ;;
      runtimes) pull_runtimes ;;
      recipes)  pull_recipes ;;
      all)      pull_guides; echo ""; pull_runtimes; echo ""; pull_recipes ;;
    esac
    show_changes pull
    ;;
  push)
    shift
    case "${1:-all}" in
      guides)   push_guides ;;
      runtimes) push_runtimes ;;
      recipes)  push_recipes ;;
      all)      push_guides; echo ""; push_runtimes; echo ""; push_recipes ;;
    esac
    show_changes push
    ;;
  *)
    echo "Usage: $0 {pull|push} [guides|runtimes|recipes|all]"
    echo ""
    echo "  pull  — Sync from canonical sources into ZCP (local clones or GitHub)"
    echo "  push  — Push tested ZCP edits to local clones (requires local repos)"
    echo ""
    echo "  guides   — docs/guides/*.mdx ↔ ZCP guides/ + decisions/"
    echo "  runtimes — hello-world app READMEs ↔ ZCP recipes/*-hello-world"
    echo "  recipes  — framework app READMEs ↔ ZCP recipes/ (needs recipe-map.txt)"
    echo "  all      — all of the above (default)"
    echo ""
    echo "Environment:"
    echo "  DOCS_GUIDES=/path/to/docs/guides  — override local docs location"
    echo "  RECIPE_APPS=/path/to/recipe-apps   — override local recipe apps location"
    exit 1
    ;;
esac
