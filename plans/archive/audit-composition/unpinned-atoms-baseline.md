# Unpinned-atoms baseline — 2026-04-26

Source-of-truth for `knownUnpinnedAtoms` allowlist landed by Phase 0
step 7. Derived via §4.2 derivation script on commit
`96b9bab7` (working tree clean, branch ahead of origin by 2 commits
which are the plan commits themselves).

**Counts (matches §4.2 baseline exactly):**

```
ZERO mentions: 68 atoms
1-2 mentions:   8 atoms
3+ mentions:    3 atoms
TOTAL:         79 atoms
```

## 68 unpinned atom IDs (Phase 0 allowlist baseline)

```
bootstrap-adopt-discover
bootstrap-classic-plan-dynamic
bootstrap-classic-plan-static
bootstrap-close
bootstrap-discover-local
bootstrap-env-var-discovery
bootstrap-mode-prompt
bootstrap-provision-local
bootstrap-provision-rules
bootstrap-recipe-close
bootstrap-recipe-match
bootstrap-resume
bootstrap-route-options
bootstrap-runtime-classes
bootstrap-verify
bootstrap-wait-active
develop-api-error-meta
develop-auto-close-semantics
develop-change-drives-deploy
develop-checklist-dev-mode
develop-checklist-simple-mode
develop-close-manual
develop-close-push-dev-dev
develop-close-push-dev-local
develop-close-push-dev-simple
develop-close-push-dev-standard
develop-close-push-git-container
develop-close-push-git-local
develop-deploy-files-self-deploy
develop-deploy-modes
develop-dev-server-reason-codes
develop-dev-server-triage
develop-dynamic-runtime-start-container
develop-dynamic-runtime-start-local
develop-env-var-channels
develop-first-deploy-asset-pipeline-container
develop-first-deploy-asset-pipeline-local
develop-first-deploy-env-vars
develop-first-deploy-execute
develop-first-deploy-execute-cmds
develop-first-deploy-intro
develop-first-deploy-promote-stage
develop-first-deploy-scaffold-yaml
develop-first-deploy-verify
develop-first-deploy-verify-cmds
develop-first-deploy-write-app
develop-http-diagnostic
develop-implicit-webserver
develop-intro
develop-knowledge-pointers
develop-local-workflow
develop-manual-deploy
develop-mode-expansion
develop-platform-rules-common
develop-platform-rules-container
develop-platform-rules-local
develop-push-dev-deploy-local
develop-push-dev-workflow-simple
develop-push-git-deploy
develop-ready-to-deploy
develop-static-workflow
develop-strategy-awareness
develop-verify-matrix
idle-orphan-cleanup
strategy-push-git-push-container
strategy-push-git-push-local
strategy-push-git-trigger-actions
strategy-push-git-trigger-webhook
```

## Pinned atoms (kept here for context — NOT in allowlist)

```
mention_count  atom_id
1              develop-push-dev-deploy-container
1              develop-push-dev-workflow-dev
2              bootstrap-intro
2              bootstrap-recipe-import
2              develop-strategy-review
2              idle-adopt-entry
2              idle-bootstrap-entry
2              strategy-push-git-intro
4              develop-closed-auto
4              idle-develop-entry
5              export
```

## Re-derivation snippet

```bash
for atom in internal/content/atoms/*.md; do
  id=$(basename "${atom%.md}")
  cnt=$(grep -c "$id" \
    internal/workflow/scenarios_test.go \
    internal/workflow/corpus_coverage_test.go \
    | awk -F: '{sum+=$2} END {print sum+0}')
  echo "$cnt $id"
done | sort -n
```

Run any time. If a Phase 8 commit pinned an atom, its count moves
from 0 → ≥ 1; the matching `knownUnpinnedAtoms` entry must be removed
in the SAME commit (R5 mitigation — ratchet shrink-only).
