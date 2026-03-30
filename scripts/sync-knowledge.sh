#!/usr/bin/env bash
# Bidirectional sync between ZCP knowledge and canonical external sources.
#
# ZCP is a read-only consumer — docs and app READMEs are the sources of truth.
#   pull: syncs external edits INTO ZCP's internal/knowledge/
#   push: distributes tested ZCP edits OUT to docs + app READMEs
set -euo pipefail

# ZCP repo root (this script lives in scripts/)
ZCP_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ZCP_KNOWLEDGE="${ZCP_ROOT}/internal/knowledge"

# Sibling repos — override via env vars if needed
DOCS_GUIDES="${DOCS_GUIDES:-$(dirname "$ZCP_ROOT")/docs/apps/docs/content/guides}"
RECIPE_APPS="${RECIPE_APPS:-$(dirname "$ZCP_ROOT")/recipe-apps}"

# ============================================================
# PULL: External → ZCP (before starting work)
# ============================================================

pull_guides() {
  echo "=== Pulling docs/guides → ZCP knowledge ==="
  local count=0

  for mdx in "${DOCS_GUIDES}"/*.mdx; do
    [[ -f "$mdx" ]] || continue
    slug=$(basename "$mdx" .mdx)

    # Route choose-* to decisions/, rest to guides/
    if [[ "$slug" == choose-* ]]; then
      target="${ZCP_KNOWLEDGE}/decisions/${slug}.md"
    else
      target="${ZCP_KNOWLEDGE}/guides/${slug}.md"
    fi

    # Strip MDX frontmatter (--- block) and import statements, restore H1 from title
    title=$(sed -n '2s/^title: //p' "$mdx")
    {
      echo "# ${title}"
      echo ""
      # Skip frontmatter (line 1 = ---, find closing ---), skip imports, trim leading blanks
      awk '
        NR==1 && /^---$/ { in_front=1; next }
        in_front && /^---$/ { in_front=0; skip_blanks=1; next }
        in_front { next }
        /^```/ { in_code=!in_code }
        !in_code && /^import / { next }
        skip_blanks && /^$/ { next }
        skip_blanks { skip_blanks=0 }
        {
          # Un-escape MDX backtick wrapping around zerops:// URIs with {var}
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
  echo "=== Pulling app README fragments → ZCP recipes (hello-world) ==="
  local runtimes=(bun deno dotnet elixir gleam go java nodejs php python ruby rust)
  local count=0

  for runtime in "${runtimes[@]}"; do
    local app_dir="${RECIPE_APPS}/${runtime}-hello-world-app"
    local readme="${app_dir}/README.md"
    local target="${ZCP_KNOWLEDGE}/recipes/${runtime}-hello-world.md"

    [[ -f "$readme" ]] || continue

    # Extract knowledge-base fragment
    local kb_content
    kb_content=$(sed -n '/ZEROPS_EXTRACT_START:knowledge-base/,/ZEROPS_EXTRACT_END:knowledge-base/p' "$readme" \
      | grep -v 'ZEROPS_EXTRACT' || true)

    [[ -z "$kb_content" ]] && continue

    # Determine H1 title from existing file or generate
    local h1=""
    if [[ -f "$target" ]]; then
      h1=$(grep -m1 '^# ' "$target" | head -1)
    fi
    [[ -z "$h1" ]] && h1="# ${runtime^} Hello World on Zerops"

    # Build the recipe file: knowledge-base (H3→H2) + zerops.yml from app repo
    {
      echo "$h1"
      echo ""
      echo "$kb_content" | sed 's/^### /## /'
      echo ""

      # Append zerops.yaml from the app repo if it exists
      local yaml_file=""
      if [[ -f "${app_dir}/zerops.yaml" ]]; then
        yaml_file="${app_dir}/zerops.yaml"
      elif [[ -f "${app_dir}/zerops.yml" ]]; then
        yaml_file="${app_dir}/zerops.yml"
      fi

      if [[ -n "$yaml_file" ]]; then
        echo "## zerops.yml"
        echo ""
        echo "> Reference implementation — learn the patterns, adapt to your project."
        echo ""
        echo '```yaml'
        cat "$yaml_file"
        echo '```'
      fi
    } > "$target"

    echo "  ${runtime} → recipes/${runtime}-hello-world.md"
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
    readme="${RECIPE_APPS}/${repo}/README.md"
    target="${ZCP_KNOWLEDGE}/recipes/${slug}.md"

    [[ -f "$readme" ]] || continue

    content=$(sed -n '/ZEROPS_EXTRACT_START:knowledge-base/,/ZEROPS_EXTRACT_END:knowledge-base/p' "$readme" \
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
# PUSH: ZCP → Distribution targets (after make test passes)
# ============================================================

push_guides() {
  echo "=== Pushing ZCP knowledge → docs/guides ==="
  mkdir -p "$DOCS_GUIDES"
  local count=0

  # Guides
  for md in "${ZCP_KNOWLEDGE}/guides/"*.md; do
    [[ -f "$md" ]] || continue
    slug=$(basename "$md" .md)
    target="${DOCS_GUIDES}/${slug}.mdx"

    # Preserve existing frontmatter if file exists, only generate for new files
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

  # Decisions
  for md in "${ZCP_KNOWLEDGE}/decisions/"*.md; do
    [[ -f "$md" ]] || continue
    slug=$(basename "$md" .md)
    target="${DOCS_GUIDES}/${slug}.mdx"

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
      [[ -z "$description" ]] && description="Decision guide: ${title}"
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

    echo "  guides/${slug}.mdx (decision)"
    count=$((count + 1))
  done

  echo "Pushed ${count} guide pages"
}

push_runtimes() {
  echo "=== Pushing ZCP recipes → app README fragments + zerops.yaml ==="
  local runtimes=(bun deno dotnet elixir gleam go java nodejs php python ruby rust)
  local count=0

  for runtime in "${runtimes[@]}"; do
    local src="${ZCP_KNOWLEDGE}/recipes/${runtime}-hello-world.md"
    local app_dir="${RECIPE_APPS}/${runtime}-hello-world-app"

    [[ -f "$src" ]] || continue
    [[ -d "$app_dir" ]] || continue

    local readme="${app_dir}/README.md"

    # Extract knowledge-base portion only (everything before ## zerops.yml)
    # Drop H1, demote H2→H3 for README context
    local fragment
    fragment=$(awk '/^## zerops\.yml/{exit} {print}' "$src" \
      | sed '/^# /d; s/^## /### /' \
      | awk 'NF{p=1} p' | awk '{lines[NR]=$0} END{while(lines[NR]=="") NR--; for(i=1;i<=NR;i++) print lines[i]}')

    [[ -z "$fragment" ]] && continue

    # Write fragment to temp file for awk to read
    local frag_file
    frag_file=$(mktemp)
    echo "$fragment" > "$frag_file"

    # Replace or append knowledge-base fragment in README
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

    # Push zerops.yaml back to app repo if ## zerops.yml section exists in recipe
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
      cd "${ZCP_ROOT}" && git diff --stat internal/knowledge/ 2>/dev/null || echo "  (no changes)"
      ;;
    push)
      if [[ -d "$(dirname "$ZCP_ROOT")/docs" ]]; then
        echo "Docs changes:"
        cd "$(dirname "$ZCP_ROOT")/docs" && git diff --stat apps/docs/content/guides/ 2>/dev/null || echo "  (no changes)"
        echo ""
      fi
      echo "App README changes:"
      for dir in "${RECIPE_APPS}"/*/; do
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
    echo "  pull  — External edits → ZCP knowledge (before starting work)"
    echo "  push  — Tested ZCP content → docs + app READMEs (after make test passes)"
    echo ""
    echo "  guides   — docs/guides/*.mdx ↔ ZCP guides/ + decisions/"
    echo "  runtimes — app README fragments ↔ ZCP recipes/*-hello-world"
    echo "  recipes  — app README fragments ↔ ZCP recipes/ (needs recipe-map.txt)"
    echo "  all      — all of the above (default)"
    exit 1
    ;;
esac
