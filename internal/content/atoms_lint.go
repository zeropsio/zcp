package content

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// AtomLintViolation describes one authoring-contract violation in an atom
// body. The atom's filename is included to speed up editor navigation.
type AtomLintViolation struct {
	AtomFile string // e.g. "bootstrap-close.md"
	Category string // "spec-id" | "handler-behavior" | "invisible-state" | "plan-doc" | "axis-l" | "axis-k" | "axis-m" | "axis-n"
	Pattern  string // the rule name that matched
	Line     int    // 1-indexed line in the atom file (including frontmatter)
	Snippet  string // the matching line, trimmed
}

// atomLintAllowlist keys are "<atomFile>::<exact-line-trimmed>" pairs.
// Allowlist entries require a short rationale committed alongside the
// entry — keep the set empty by default; every entry is an audit target.
//
// Used by every rule family (regex rules, axis-L, axis-K, axis-M, axis-N).
// Axis-K, axis-M, and axis-N also accept inline `<!-- axis-{k,m,n}-keep -->`
// / `<!-- axis-{k,m,n}-drop -->` markers as a per-line opt-in suppression
// (see atoms_lint_axes.go); the allowlist is for whole-line allowances
// across rules without modifying the atom body.
var atomLintAllowlist = map[string]string{
	// Empty on purpose. Add entries in the form:
	//   "bootstrap-close.md::some specific line prose" : "rationale why this is not a violation",
}

type atomLintRule struct {
	name     string
	category string
	pattern  *regexp.Regexp
}

// AcceptedWorkflowActions lists every `action="X"` value that
// `zerops_workflow`'s dispatcher accepts. Source of truth is
// `internal/tools/workflow.go::handleWorkflowAction` — the early
// `if input.Action == "X"` guards plus the `switch input.Action` cases.
// This duplicate is here because content/ cannot import tools/ (layer
// inversion); `TestAtomLintAcceptedActionsMatchDispatcher` keeps the two
// in sync. If you add a new action there, add it here too.
var AcceptedWorkflowActions = []string{
	"start", "reset", "iterate", "complete", "generate-finalize",
	"skip", "status", "close", "resume", "list", "route",
	"close-mode", "git-push-setup", "build-integration",
	"classify", "adopt-local",
	"dispatch-brief-atom", "build-subagent-brief",
	"verify-subagent-dispatch", "record-deploy",
}

// AcceptedDeployStrategies lists every `strategy="X"` value that
// `zerops_deploy` accepts. Source of truth is `validateDeployStrategyParam`
// at `internal/tools/deploy_strategy_gate.go`. The empty string (default
// zcli push) is always allowed and does not appear in atom-body
// `strategy="..."` literals — so the list only enumerates non-default
// values that may appear quoted.
// `TestAtomLintAcceptedStrategiesMatchGate` keeps the two in sync.
var AcceptedDeployStrategies = []string{
	"git-push",
}

var atomLintRules = []atomLintRule{
	{
		name:     "spec-id",
		category: "spec-id",
		pattern:  regexp.MustCompile(`\bDM-[0-9]|\bDS-0[1-4]|\bGLC-[1-6]|\bKD-[0-9]{2}|\bTA-[0-9]{2}|\bE[1-8]\b|\bO[1-4]\b|\bF#[1-9]|\bINV-[0-9]+`),
	},
	{
		name:     "handler-behavior-handler",
		category: "handler-behavior",
		pattern:  regexp.MustCompile(`(?i)\bhandler\b[^\n]{0,80}\b(automatically|auto-\w+|writes|stamps|activates|enables|disables)\b`),
	},
	{
		name:     "handler-behavior-tool-auto",
		category: "handler-behavior",
		pattern:  regexp.MustCompile(`(?i)\btool\b[^\n]{0,40}\b(auto-\w+|automatically)\b`),
	},
	{
		name:     "handler-behavior-zcp",
		category: "handler-behavior",
		pattern:  regexp.MustCompile(`\bZCP\s+(writes|stamps|activates|enables|disables)\b`),
	},
	{
		name:     "invisible-state",
		category: "invisible-state",
		pattern:  regexp.MustCompile(`\bFirstDeployedAt\b|\bBootstrapSession\b|\bCloseDeployModeConfirmed\b`),
	},
	{
		name:     "plan-doc",
		category: "plan-doc",
		pattern:  regexp.MustCompile(`\bplans/[a-z][a-z0-9-]+\.md\b`),
	},
}

// LintAtomCorpus scans every atom body (frontmatter excluded) for the
// authoring-contract violations defined in atomLintRules and the four
// content-quality axes K/L/M/N (see atoms_lint_axes.go +
// docs/spec-knowledge-distribution.md §11.5/§11.6). The returned slice is
// empty when the corpus is clean. Allowlist entries suppress specific
// matches with a documented rationale.
//
// Called by TestAtomAuthoringLint. Kept as an exported function so a
// future `zcp lint atoms` CLI or CI gate could call it directly.
func LintAtomCorpus() ([]AtomLintViolation, error) {
	atoms, err := ReadAllAtoms()
	if err != nil {
		return nil, fmt.Errorf("read atoms: %w", err)
	}
	return lintAtomCorpus(atoms), nil
}

// lintAtomCorpus runs the rule engine over an arbitrary atom slice.
// Unexported on purpose — production code goes through LintAtomCorpus
// (which sources atoms from the embedded corpus). The helper exists so
// fires-on-fixture tests can pass synthetic atoms in directly without
// monkeying with ReadAllAtoms.
func lintAtomCorpus(atoms []AtomFile) []AtomLintViolation {
	out := make([]AtomLintViolation, 0, len(atoms))
	for _, atom := range atoms {
		ctx := buildAtomLintCtx(atom)
		out = append(out, regexLintRules(ctx)...)
		out = append(out, axisLViolations(ctx)...)
		out = append(out, axisKViolations(ctx)...)
		out = append(out, axisMViolations(ctx)...)
		out = append(out, axisNViolations(ctx)...)
		out = append(out, closeDeployModeViolations(ctx)...)
		out = append(out, gitPushStateViolations(ctx)...)
		out = append(out, buildIntegrationViolations(ctx)...)
		out = append(out, staleActionViolations(ctx)...)
		out = append(out, staleStrategyViolations(ctx)...)
	}
	return out
}

var (
	workflowActionRe = regexp.MustCompile(`zerops_workflow[^\n]{0,200}\baction="([a-z][a-z0-9-]*)"`)
	deployStrategyRe = regexp.MustCompile(`zerops_deploy[^\n]{0,200}\bstrategy="([a-z][a-z0-9-]*)"`)
)

// staleActionViolations flags `zerops_workflow action="X"` literals in atom
// bodies where X is not in AcceptedWorkflowActions. This is the
// class-prevention net for vocab drift after refactors like
// deploy-strategy-decomposition (which retired `action="strategy"`).
// Bodies that reference renamed actions surface immediately.
func staleActionViolations(ctx atomLintCtx) []AtomLintViolation {
	var out []AtomLintViolation
	accepted := make(map[string]bool, len(AcceptedWorkflowActions))
	for _, a := range AcceptedWorkflowActions {
		accepted[a] = true
	}
	for i, line := range ctx.bodyLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		matches := workflowActionRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			action := m[1]
			if accepted[action] {
				continue
			}
			key := ctx.file + "::" + trimmed
			if _, allowed := atomLintAllowlist[key]; allowed {
				continue
			}
			out = append(out, AtomLintViolation{
				AtomFile: ctx.file,
				Category: "stale-action",
				Pattern:  "stale-workflow-action",
				Line:     ctx.frontmatterLines + i + 1,
				Snippet:  trimmed,
			})
		}
	}
	return out
}

// staleStrategyViolations flags `zerops_deploy strategy="X"` literals in
// atom bodies where X is not in AcceptedDeployStrategies. Catches retired
// values like "push-dev" reappearing post-decomposition.
func staleStrategyViolations(ctx atomLintCtx) []AtomLintViolation {
	var out []AtomLintViolation
	accepted := make(map[string]bool, len(AcceptedDeployStrategies))
	for _, s := range AcceptedDeployStrategies {
		accepted[s] = true
	}
	for i, line := range ctx.bodyLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		matches := deployStrategyRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			value := m[1]
			if accepted[value] {
				continue
			}
			key := ctx.file + "::" + trimmed
			if _, allowed := atomLintAllowlist[key]; allowed {
				continue
			}
			out = append(out, AtomLintViolation{
				AtomFile: ctx.file,
				Category: "stale-strategy",
				Pattern:  "stale-deploy-strategy",
				Line:     ctx.frontmatterLines + i + 1,
				Snippet:  trimmed,
			})
		}
	}
	return out
}

// closeDeployModeViolations enforces axis-specific body-prose rules for
// atoms declaring `closeDeployModes:`. Phase-1 stub introduced by the
// deploy-strategy decomposition (plans/deploy-strategy-decomposition-2026-04-28.md);
// rules land in Phase 8 alongside the atom corpus restructure. Candidates
// for Phase 8 enforcement: `closeDeployModes: [manual]` atoms MUST NOT
// contain `zerops_deploy` invocations (spec D7 — agents inform on manual
// mode, they do not call deploy on the user's behalf).
func closeDeployModeViolations(_ atomLintCtx) []AtomLintViolation {
	return nil
}

// gitPushStateViolations enforces axis-specific body-prose rules for
// atoms declaring `gitPushStates:`. Phase-1 stub. Rules land in Phase 8.
func gitPushStateViolations(_ atomLintCtx) []AtomLintViolation {
	return nil
}

// buildIntegrationViolations enforces axis-specific body-prose rules for
// atoms declaring `buildIntegrations:`. Phase-1 stub. Rules land in
// Phase 8 — candidates: enforce UTILITY framing ("ZCP-managed integration",
// not "CI/CD"; warn if "no build will fire" appears alongside
// `buildIntegrations: [none]` since users may have independent CI).
func buildIntegrationViolations(_ atomLintCtx) []AtomLintViolation {
	return nil
}

// regexLintRules runs the legacy regex rule family against the atom body.
// Operates line-by-line; allowlist suppresses by `<file>::<trimmed-line>`.
// Code fences are NOT skipped here — preserves prior behavior.
func regexLintRules(ctx atomLintCtx) []AtomLintViolation {
	var out []AtomLintViolation
	for i, line := range ctx.bodyLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, rule := range atomLintRules {
			if !rule.pattern.MatchString(line) {
				continue
			}
			key := ctx.file + "::" + trimmed
			if _, allowed := atomLintAllowlist[key]; allowed {
				continue
			}
			out = append(out, AtomLintViolation{
				AtomFile: ctx.file,
				Category: rule.category,
				Pattern:  rule.name,
				Line:     ctx.frontmatterLines + i + 1,
				Snippet:  trimmed,
			})
		}
	}
	return out
}

// atomLintCtx holds the per-atom state pre-computed once and threaded
// through every rule family. Code-fence tracking and frontmatter parsing
// are expensive enough that running them once per axis would waste cycles.
type atomLintCtx struct {
	file             string            // atom filename, e.g. "develop-static-workflow.md"
	frontmatter      map[string]string // raw frontmatter key→value
	frontmatterLines int               // count of frontmatter lines (incl. delimiters)
	bodyLines        []string          // body split by "\n"
	inCodeFence      []bool            // bodyLines[i] is inside a ``` block
	markers          map[int][]string  // body-line-index → axis markers active for that line
}

// buildAtomLintCtx prepares the per-atom lint context. Frontmatter parsing
// uses bufio.Scanner; code-fence tracking is a single forward pass with a
// toggling bool. Marker map keys are body-line indices (0-indexed).
func buildAtomLintCtx(atom AtomFile) atomLintCtx {
	front, body, fmLines := splitFrontmatterForLint(atom.Content)
	bodyLines := strings.Split(body, "\n")

	inFence := make([]bool, len(bodyLines))
	fenceOpen := false
	fenceRe := regexp.MustCompile("^\\s*```")
	for i, line := range bodyLines {
		if fenceRe.MatchString(line) {
			fenceOpen = !fenceOpen
			inFence[i] = true // the fence delimiter line itself
			continue
		}
		inFence[i] = fenceOpen
	}

	markers := extractAxisMarkers(bodyLines)

	return atomLintCtx{
		file:             atom.Name,
		frontmatter:      parseLintFrontmatter(front),
		frontmatterLines: fmLines,
		bodyLines:        bodyLines,
		inCodeFence:      inFence,
		markers:          markers,
	}
}

// splitFrontmatterForLint splits the atom into (frontmatter, body,
// frontmatterLineCount). Mirrors splitAtomBody but also returns the raw
// frontmatter so per-axis rules can read fields like `title:` and
// `environments:`. frontmatterLineCount counts the opening `---`, every
// frontmatter content line, and the closing `---`.
func splitFrontmatterForLint(content string) (string, string, int) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content, 0
	}
	rest := content[4:]
	front, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return "", content, 0
	}
	return front, body, 2 + strings.Count(front, "\n") + 1
}

// parseLintFrontmatter is a minimal frontmatter reader for lint purposes.
// It does not validate types or arrays — every value is the raw RHS string.
// The authoritative parser lives in internal/workflow/atom.go::ParseAtom;
// duplicating the surface here avoids a circular import (workflow depends
// on content for atom bytes; lint runs over content directly).
func parseLintFrontmatter(front string) map[string]string {
	fields := map[string]string{}
	if front == "" {
		return fields
	}
	scanner := bufio.NewScanner(strings.NewReader(front))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		fields[key] = val
	}
	return fields
}
