#!/bin/bash
# Extract per-version content metrics from a nestjs-showcase run.
# Usage: version_metrics.sh <base-dir>
# Expects one subdirectory per version (nestjs-showcase-v6, -v7, ...)

set -eu

BASE="${1:-/Users/fxck/www/zcprecipator/nestjs-showcase}"
cd "$BASE" || exit 1

printf "%-5s %-10s %-9s %-9s %-9s %-9s\n" "ver" "codebase" "readme" "claude" "gotchas" "ig_items"
echo "-----------------------------------------------------------------"

for v in v6 v7 v8 v9 v10 v11 v12 v13 v14 v15 v16; do
  dir="nestjs-showcase-$v"
  [ -d "$dir" ] || continue
  for c in apidev appdev workerdev; do
    rml="-"
    cml="-"
    gc="-"
    ig="-"
    if [ -f "$dir/$c/README.md" ]; then
      rml=$(wc -l < "$dir/$c/README.md" | tr -d ' ')
      gc=$(awk '/#ZEROPS_EXTRACT_START:knowledge-base/{f=1;next} /#ZEROPS_EXTRACT_END:knowledge-base/{f=0} f && /^- \*\*/' "$dir/$c/README.md" | wc -l | tr -d ' ')
      ig=$(awk '/#ZEROPS_EXTRACT_START:integration-guide/{f=1;next} /#ZEROPS_EXTRACT_END:integration-guide/{f=0} f && /^### [0-9]/' "$dir/$c/README.md" | wc -l | tr -d ' ')
    fi
    if [ -f "$dir/$c/CLAUDE.md" ]; then
      cml=$(wc -l < "$dir/$c/CLAUDE.md" | tr -d ' ')
    fi
    printf "%-5s %-10s %-9s %-9s %-9s %-9s\n" "$v" "$c" "$rml" "$cml" "$gc" "$ig"
  done
done
