package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// BriefValidationCommands maps a content-check family identifier to the
// exact shell command the writer sub-agent must run against its own
// draft, per content check, before returning (v8.86 §3.2). The keys are
// the stable family identifiers — `content_reality`, `gotcha_causal_
// anchor`, etc. — stripped of their hostname prefix so the brief can
// substitute per-codebase paths at render time.
//
// Parity with the gate-side check is the load-bearing invariant (plan
// §6 risk: brief says pass but gate says fail). TestBriefValidation
// RegistryCoversKnownContentCheckFamilies enforces coverage; manual
// review enforces semantic parity when a check changes its logic.
var BriefValidationCommands = map[string]string{
	// Per-host content checks — file paths use {mountpath} placeholder
	// the writer substitutes per codebase.
	"content_reality": `grep -rnE 'dist/[^[:space:]"` + "`" + `]+\.js|process\.env\.[a-zA-Z_]+|res\.json\(|response\.json\b|import\.meta\.env\.[a-zA-Z_]+' {mountpath}/README.md {mountpath}/CLAUDE.md || true
# Each match must be inside a fenced code block AND framed as 'output of npm run build' or 'compiled JavaScript' — NOT authoritative file paths. If any match is advisory-only, you're fine. If any reads as a real file path that doesn't exist in the codebase, rewrite it.`,

	"gotcha_causal_anchor": `awk '/### Gotchas/,/^### [^G]/' {mountpath}/README.md | grep -E '^[-*] '
# For EACH gotcha bullet, verify the bullet contains:
#   (a) at least one platform mechanism token (execOnce, L7 balancer, ${db_hostname}, httpSupport, readinessCheck, SSHFS mount, advisory lock, buildFromGit, cross-service env injection)
#   (b) at least one strong-symptom verb (rejects, deadlocks, drops, crashes, times out, throws, fails, returns 4xx, returns 5xx, returns wrong content-type, hangs, never reads, silent no-op)
# Bullets that mention only generic-platform advice without both tokens fail causal-anchor.`,

	"gotcha_distinct_from_guide": `awk '/#ZEROPS_EXTRACT_START:integration-guide#/,/#ZEROPS_EXTRACT_END:integration-guide#/' {mountpath}/README.md | grep -E '^### [0-9]+\.'
# For each gotcha bullet in knowledge-base, compare its normalized stem to every integration-guide H3 heading. A stem match = restatement bloat, rewrite to focus on symptom (not topic) or delete.`,

	"claude_readme_consistency": `grep -nE 'synchronize.*true|drop[[:space:]]+(table|database)[[:space:]]+cascade|db:reset|ds\.synchronize' {mountpath}/CLAUDE.md || true
# Any match must be inside a block marked '(dev only — see README)' or similar cross-reference. Patterns known to be hazardous in production must carry an explicit dev-only marker in CLAUDE.md.`,

	"claude_md_no_burn_trap_folk": `grep -nE '(?i)\bburn(ed|s|-?trap)?\b.{0,100}execonce|execonce.{0,100}\bburn(ed|s|-?trap)?\b' {mountpath}/README.md {mountpath}/CLAUDE.md || true
# No matches must exist. The term 'burn trap' is fictional — execOnce keys on appVersionId (fresh per deploy), so the lock is never pre-burned. Rewrite any 'burn' wording near execOnce to name the real mechanism + real failure mode. See zerops_guidance topic="execOnce-semantics".`,

	"scaffold_hygiene": `test -f {mountpath}/.gitignore && test -f {mountpath}/.env.example && ! find {mountpath}/node_modules {mountpath}/dist {mountpath}/build {mountpath}/.DS_Store 2>/dev/null | head -1 | grep -q .
# Every codebase ships with .gitignore + .env.example; no build output / node_modules / OS-cruft leaks into the published tree.`,

	"service_coverage": `awk '/### Gotchas/,/^### [^G]/' {mountpath}/README.md > /tmp/kb.txt
# Count distinct managed services mentioned in gotchas — must cover every managed service in the plan (db, cache, storage, search, queue, mail — whichever apply) for showcase recipes. Worker codebases get a pass on surfaces handled in the host README.`,

	"ig_per_item_standalone": `awk '/#ZEROPS_EXTRACT_START:integration-guide#/,/#ZEROPS_EXTRACT_END:integration-guide#/' {mountpath}/README.md | awk '/^### [0-9]/,/^### [0-9]/'
# Every ### N. block inside integration-guide must stand alone: its own fenced code block + a platform anchor (execOnce, httpSupport, L7, ${}, readinessCheck, cross-service var, etc.) in the first prose paragraph.`,

	"knowledge_base_authenticity": `awk '/### Gotchas/,/^### [^G]/' {mountpath}/README.md
# Every gotcha bullet must narrate a concrete behavior the agent actually observed during deploy — grep the facts log for the mechanism + failure_mode pair that anchors the bullet. Bullets that repeat the predecessor recipe verbatim, or that read as research-time speculation, fail authenticity.`,

	// Hostnameless content checks.
	"cross_readme_gotcha_uniqueness": `# For each normalized gotcha stem (first 40 chars, lowercase, strip punctuation):
# awk '/### Gotchas/,/^### [^G]/' apidev/README.md appdev/README.md workerdev/README.md | ...
# A stem must appear in EXACTLY ONE README. NATS facts belong to the codebase where NATS is actually used; execOnce facts belong to the codebase that runs initCommand; etc. A fact duplicated across READMEs fails uniqueness — pick the canonical home codebase, delete from the others.`,

	"recipe_architecture_narrative": `# Root README must carry an architecture section that names every runtime and managed service plus the request flow. Read ./README.md and confirm:
#   - every hostname in plan.Targets appears in the architecture prose
#   - the flow between them (HTTP, NATS, DB) is named
#   - a one-line 'why this recipe' rationale is present
# Missing sections fail the narrative check.`,

	"knowledge_base_exceeds_predecessor": `# Compare the number of gotcha bullets in this recipe's README.md against the direct predecessor recipe's README.md.
# The predecessor is the chain root declared in plan.Research.Predecessor (fetch via zerops_knowledge topic="recipes/{slug}").
# This recipe's total (summed across every codebase's knowledge-base) must be >= predecessor total. Compression is always a bug in LLM-only development — if lived experience produced fewer gotchas than the predecessor, either the debug rounds were too short or gotchas were dropped during rewriting.`,
}

// WriterBriefInput is the argument to BuildWriterBrief (v8.86 §3.2).
// The writer sub-agent brief is assembled from these inputs — the facts
// log accumulated during deploy, the contract spec, and the plan — then
// renders a validation section derived from the check registry so the
// writer self-verifies before returning.
type WriterBriefInput struct {
	Plan         *RecipePlan
	FactsLogBody string
	ContractSpec string
}

// BuildWriterBrief renders the complete writer sub-agent brief for the
// deploy.readmes sub-step. The brief is structured in four sections:
//
//  1. INPUT — facts log body, contract spec, plan summary, scaffold paths
//  2. OUTPUT — the six files the writer must produce
//  3. VALIDATION — runnable shell command per content-check family
//  4. ITERATE UNTIL CLEAN — the self-verification loop contract
//
// The validation section is generated from BriefValidationCommands so
// every gate-side check has a brief-side command the writer can run
// against its own draft. A check without a registry entry is a brief-
// completeness bug (surfaced by the writer-brief-bug content_fix gate
// in v8.86 §3.3).
func BuildWriterBrief(input WriterBriefInput) (string, error) {
	var sb strings.Builder

	sb.WriteString("# Writer sub-agent brief (v8.86 §3.2 — self-verifying shape)\n\n")
	sb.WriteString("You are the README + CLAUDE.md writer sub-agent dispatched at the `deploy.readmes` sub-step. ")
	sb.WriteString("The goal of this brief: you ship clean content on the FIRST return. ")
	sb.WriteString("You run the validations at the bottom of this brief against your own draft and iterate until every one passes. ")
	sb.WriteString("No external fix sub-agent will save you — the v8.81 dispatch loop has been removed. A post-return content failure is a writer-brief bug, and the workflow will block.\n\n")

	sb.WriteString("## 1. INPUT\n\n")
	sb.WriteString("### Facts log accumulated during deploy\n\n")
	if strings.TrimSpace(input.FactsLogBody) == "" {
		sb.WriteString("_(facts log empty — record facts via zerops_record_fact during deploy substeps; writing gotchas without facts falls back to archaeology)_\n\n")
	} else {
		sb.WriteString("```jsonl\n")
		sb.WriteString(input.FactsLogBody)
		if !strings.HasSuffix(input.FactsLogBody, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n\n")
	}

	sb.WriteString("### Cross-codebase contract spec (generate.contract-spec substep output)\n\n")
	if strings.TrimSpace(input.ContractSpec) == "" {
		sb.WriteString("_(contract spec empty — upstream substep did not run; IG items should still cite real files but cross-codebase shape bindings are not available)_\n\n")
	} else {
		sb.WriteString("```yaml\n")
		sb.WriteString(input.ContractSpec)
		if !strings.HasSuffix(input.ContractSpec, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n\n")
	}

	sb.WriteString("### Plan shape\n\n")
	writeWriterBriefPlanSummary(&sb, input.Plan)

	sb.WriteString("## 2. OUTPUT\n\n")
	sb.WriteString("Write or rewrite these files on the mount (paths are per-codebase):\n\n")
	for _, t := range appAndSeparateWorkerTargets(input.Plan) {
		fmt.Fprintf(&sb, "- `%sdev/README.md` — fragments + gotchas (see readme-fragments topic)\n", t.Hostname)
		fmt.Fprintf(&sb, "- `%sdev/CLAUDE.md` — repo-local dev-loop operations\n", t.Hostname)
	}
	sb.WriteString("- `./README.md` — root architecture narrative\n\n")

	sb.WriteString("## 3. VALIDATION — run each command BEFORE returning; iterate until ALL pass\n\n")
	sb.WriteString("For each command below, substitute `{mountpath}` with the codebase mount (e.g. `apidev`, `appdev`, `workerdev`). Run per codebase where applicable.\n\n")

	keys := make([]string, 0, len(BriefValidationCommands))
	for k := range BriefValidationCommands {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "### Check: `%s`\n\n", k)
		sb.WriteString("```bash\n")
		sb.WriteString(BriefValidationCommands[k])
		sb.WriteString("\n```\n\n")
	}

	sb.WriteString("## 4. ITERATE UNTIL CLEAN\n\n")
	sb.WriteString("The self-validation loop:\n\n")
	sb.WriteString("1. Draft all output files using the facts log + contract spec as authoritative input.\n")
	sb.WriteString("2. Run every validation command above against your draft.\n")
	sb.WriteString("3. If ANY validation reports a violation: fix the relevant content, re-run the affected validation(s), and continue.\n")
	sb.WriteString("4. Only when every validation passes, return.\n\n")
	sb.WriteString("A post-return failure = writer-brief bug. Do not assume the gate will allow a second chance.\n")

	return sb.String(), nil
}

// writeWriterBriefPlanSummary emits a compact plan summary — tier, targets
// with role + type, worker shape. Keeps the brief self-contained so the
// writer doesn't fetch the full plan via a separate tool call.
func writeWriterBriefPlanSummary(sb *strings.Builder, plan *RecipePlan) {
	if plan == nil {
		sb.WriteString("_(no plan available — writer cannot proceed)_\n\n")
		return
	}
	fmt.Fprintf(sb, "- Tier: `%s`\n", plan.Tier)
	if plan.Framework != "" {
		fmt.Fprintf(sb, "- Framework: `%s`\n", plan.Framework)
	}
	sb.WriteString("- Targets:\n")
	for _, t := range plan.Targets {
		marker := ""
		if t.IsWorker {
			if t.SharesCodebaseWith != "" {
				marker = fmt.Sprintf(" (worker, shares codebase with %s)", t.SharesCodebaseWith)
			} else {
				marker = " (separate-codebase worker)"
			}
		}
		fmt.Fprintf(sb, "  - `%s` (%s)%s\n", t.Hostname, t.Type, marker)
	}
	sb.WriteString("\n")
}

// appAndSeparateWorkerTargets returns the runtime targets that ship their
// own codebase (no shared-codebase workers). Matches the set of codebases
// that have their own README + CLAUDE.md on the mount.
func appAndSeparateWorkerTargets(plan *RecipePlan) []RecipeTarget {
	if plan == nil {
		return nil
	}
	out := make([]RecipeTarget, 0, len(plan.Targets))
	for _, t := range plan.Targets {
		if !IsRuntimeType(t.Type) {
			continue
		}
		if t.IsWorker && t.SharesCodebaseWith != "" {
			continue
		}
		out = append(out, t)
	}
	return out
}
