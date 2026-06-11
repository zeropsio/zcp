package content

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
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
	"close-mode", "git-push-setup", "build-integration", "prod-ops",
	"confirm-production", "classify", "adopt-local", "set-default-setup",
	"dispatch-brief-atom", "build-subagent-brief",
	"verify-subagent-dispatch", "record-deploy", "release",
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
	// Source-code references — atoms are LLM-facing runtime prose; the
	// LLM cannot navigate to Go source files or run named tests at
	// runtime. Any reference to internal/<pkg>/<file>.go or test
	// function names leaks dev-side concerns into agent guidance,
	// either misleading the agent (it tries to "look at" the file) or
	// wasting context tokens on unactionable information. Catch these
	// structurally so the runtime-vs-build-time layer stays clean.
	{
		name:     "source-go-path",
		category: "source-code-ref",
		pattern:  regexp.MustCompile(`\b(internal|cmd)/[a-z_][a-z0-9_]*(/[a-z_][a-z0-9_]*)*\.go\b`),
	},
	{
		name:     "source-test-name",
		category: "source-code-ref",
		pattern:  regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]*_[A-Z]`),
	},
	// drift detectors for env-var audit 2026-05-15 — banned phrases that
	// previously misled agents. "prefer it over assembling" lived in
	// develop-first-deploy-env-vars and pushed agents toward the broken
	// `${db_connectionString}` for Prisma (postgres connectionString omits
	// /dbName); "empty string at runtime" lived in the env-shadow gotcha
	// and misstated the symptom (literal `${var}` string, not empty).
	{
		name:     "env-vars-prefer-connection-string",
		category: "factual-drift",
		pattern:  regexp.MustCompile(`(?i)prefer\s+it\s+over\s+assembling`),
	},
	{
		name:     "env-shadow-empty-string-symptom",
		category: "factual-drift",
		pattern:  regexp.MustCompile(`(?i)resolves\s+to\s+an\s+empty\s+string\s+at\s+runtime`),
	},
	// ToolSearch select: tokens must use the MCP-server-prefixed form.
	// The bare form `select:zerops_workflow,...` returns "No matching
	// deferred tools found" — the host harness routes by the MCP server
	// name (`zerops` per `internal/content/templates/mcp-config.json`),
	// which registers tools as `mcp__zerops__zerops_*`. Atoms documenting
	// `select:zerops_*` (bare) instruct every fresh session to make a
	// dead-end call. Verified live in
	// `eval/behavioral/runs/20260518-132736/develop-loop-after-bootstrap/transcript.jsonl`.
	{
		name:     "toolsearch-select-missing-mcp-prefix",
		category: "tool-call-shape",
		pattern:  regexp.MustCompile(`\bselect:zerops_`),
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
//
// Axis R (atom-id-in-body) needs the set of known atom basenames so it
// can distinguish `develop-strategy-review` (atom navigation) from
// `git-push` / `auto-complete` (platform vocabulary). The set is
// derived from the input slice's filenames and passed into
// `axisRViolations` once.
func lintAtomCorpus(atoms []AtomFile) []AtomLintViolation {
	atomIDs := make(map[string]struct{}, len(atoms))
	for _, atom := range atoms {
		id := strings.TrimSuffix(atom.Name, ".md")
		atomIDs[id] = struct{}{}
	}
	out := make([]AtomLintViolation, 0, len(atoms))
	for _, atom := range atoms {
		ctx := buildAtomLintCtx(atom)
		out = append(out, regexLintRules(ctx)...)
		out = append(out, axisLViolations(ctx)...)
		out = append(out, axisKViolations(ctx)...)
		out = append(out, axisMViolations(ctx)...)
		out = append(out, axisNViolations(ctx)...)
		out = append(out, axisOViolations(ctx)...)
		out = append(out, axisRViolations(ctx, atomIDs)...)
		out = append(out, axisHotShellViolations(ctx)...)
		out = append(out, axisRuntimeViolations(ctx)...)
		out = append(out, gitPushStateAxisViolations(ctx)...)
		out = append(out, buildIntegrationViolations(ctx)...)
		out = append(out, staleActionViolations(ctx)...)
		out = append(out, staleStrategyViolations(ctx)...)
		out = append(out, statusTokenViolations(ctx)...)
	}
	return out
}

// backtickUpperToken extracts a backtick-wrapped UPPER_SNAKE / all-caps token
// (the form atoms use for platform status strings and other identifiers).
//
//nolint:gochecknoglobals // value-only regex, immutable after init.
var backtickUpperToken = regexp.MustCompile("`([A-Z][A-Z0-9_]{2,})`")

// statusSuffixFamily are the trailing fragments that mark a token as a
// platform STATUS (vs an env-var / config identifier). A token ending in one
// of these is status-shaped, so it must be a real status — this is what
// distinguishes a phantom like NOT_YET_DEPLOYED (ends in _DEPLOYED) from
// `APP_KEY` / `GIT_TOKEN`, which match no family and are never flagged.
//
//nolint:gochecknoglobals // immutable lookup data.
var statusSuffixFamily = []string{"_TO_DEPLOY", "_TO_BUILD", "_DEPLOYED", "_DEPLOY", "_FAILED", "_BUILD", "_RUNTIME"}

// statusSingletons are single-word platform statuses (no underscore) that the
// suffix family can't catch. Membership marks a token status-shaped; validity
// is still checked against platform.KnownStatusStrings.
//
//nolint:gochecknoglobals // immutable lookup data.
var statusSingletons = map[string]bool{
	"RUNNING": true, "ACTIVE": true, "STOPPED": true, "STOPPING": true, "STARTING": true,
	"PENDING": true, "FINISHED": true, "CANCELED": true, "CANCELLED": true, "CREATING": true,
	"RESTARTING": true, "RELOADING": true, "DELETED": true, "DELETING": true, "DEPLOYING": true,
	"UPLOADING": true, "BUILDING": true, "SCALING": true, "UPGRADING": true, "REPAIRING": true,
	"MOVING": true, "BACKUP": true, "PREPARING": true,
}

// statusTokenViolations flags a backtick-wrapped, status-shaped token in an
// atom body that is not a real platform status. The phantom `NOT_YET_DEPLOYED`
// (in zero live payloads, zero platform constants, zero SDK enums) shipped in
// 67 responses because nothing pinned status vocabulary against its owner.
// Single owner of the valid set: platform.KnownStatusStrings (B8). The
// suffix-family + singleton discriminator keeps env-var / config identifiers
// (`APP_KEY`, `GIT_TOKEN`, `NODE_ENV`) out of scope — only status-shaped tokens
// are validated, so the lint has no false-positive surface on the corpus.
func statusTokenViolations(ctx atomLintCtx) []AtomLintViolation {
	valid := platform.KnownStatusStrings()
	var out []AtomLintViolation
	for i, line := range ctx.bodyLines {
		for _, m := range backtickUpperToken.FindAllStringSubmatch(line, -1) {
			tok := m[1]
			if !isStatusShaped(tok) || valid[tok] {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if _, ok := atomLintAllowlist[ctx.file+"::"+trimmed]; ok {
				continue
			}
			out = append(out, AtomLintViolation{
				AtomFile: ctx.file,
				Category: "status-token",
				Pattern:  "phantom-status:" + tok,
				Line:     ctx.frontmatterLines + i + 1,
				Snippet:  trimmed,
			})
		}
	}
	return out
}

// isStatusShaped reports whether tok looks like a platform status (so it must
// be a real one). See statusSuffixFamily / statusSingletons.
func isStatusShaped(tok string) bool {
	if statusSingletons[tok] {
		return true
	}
	for _, suf := range statusSuffixFamily {
		if strings.HasSuffix(tok, suf) {
			return true
		}
	}
	return false
}

// runtimeMechanicTokens matches body content that describes runtime
// mechanics — start commands, dev-server orchestration, health checks,
// or runtime-class-specific noop primitives. Develop-active atoms that
// emit any of these tokens but lack a `runtimes:` axis fire for ALL
// runtime classes (dynamic, implicit-webserver, static), polluting the
// develop-step response for runtimes the guidance doesn't apply to.
//
// Codex's exact predicate (flow-eval suite 20260503-144814 review): scan
// the body INCLUDING code fences (command blocks are the highest-risk
// surface). The lint pins the corpus against future drift.
//
//nolint:gochecknoglobals // value-only regex, immutable after init.
var runtimeMechanicTokens = regexp.MustCompile(`\brun\.start\b|\brun\.ports\b|\bhealthCheck\b|\bzsc\s+noop\b|\bzerops_dev_server\b`)

// axisRuntimeViolations enforces: every develop-active atom that mentions
// runtime-mechanic tokens MUST declare a `runtimes:` axis. Universal
// atoms (workflow state, error meta, deploy strategy, close-mode taxonomy)
// don't trip the regex and remain wildcard-targeted.
func axisRuntimeViolations(ctx atomLintCtx) []AtomLintViolation {
	phases := ctx.frontmatter["phases"]
	if !strings.Contains(phases, "develop-active") {
		return nil
	}
	runtimes := strings.TrimSpace(ctx.frontmatter["runtimes"])
	// Treat empty / `[]` / unset as "no axis" — both forms are wildcard.
	if runtimes != "" && runtimes != "[]" {
		return nil
	}
	var out []AtomLintViolation
	for i, line := range ctx.bodyLines {
		// Body INCLUDING code fences: command blocks are the highest-risk
		// surface (Codex explicit on this).
		loc := runtimeMechanicTokens.FindStringIndex(line)
		if loc == nil {
			continue
		}
		out = append(out, AtomLintViolation{
			AtomFile: ctx.file,
			Category: "axis-runtime",
			Pattern:  "develop-active-runtime-token-no-axis:" + strings.TrimSpace(line[loc[0]:loc[1]]),
			Line:     ctx.frontmatterLines + i + 1,
			Snippet:  strings.TrimSpace(line),
		})
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

// gitPushStateAxisViolations enforces axis-conjunction rules for atoms
// gated on git-push CAPABILITY states. Catches a recurring class of bugs
// where an atom routes via the gitPushStates axis without also scoping
// to the modes that can act as the push SOURCE — letting the atom render
// a push command or setup walkthrough for modes where the handler
// hard-rejects (F12 round-3 audit lineage: needs-setup atom fired for
// ModeDev → walked through git-push-setup → deploy rejected with
// PushSourceModeUnsupported; originally pinned via the retired
// closeDeployModes:[git-push] axis before the delivery-ladder fold).
//
// Scope: triggers when `gitPushStates` values are a subset of
// {configured, broken} — gates that presuppose push capability exists
// (or existed), i.e. the atom instructs the push path. Gates that
// include `unconfigured` (e.g. [unconfigured, broken] on direct-deploy
// walkthroughs) describe the ABSENCE of the push path and legitimately
// span any mode, so the modes-filter requirement doesn't apply.
//
// Rule: such atoms in develop-active MUST declare `modes:` with values
// ⊆ the push-source set (standard, simple, local-stage, local-only).
// ModeDev and ModeStage cannot push; an atom firing for them leads the
// agent into a guaranteed handler rejection.
func gitPushStateAxisViolations(ctx atomLintCtx) []AtomLintViolation {
	statesRaw, ok := ctx.frontmatter["gitPushStates"]
	if !ok {
		return nil
	}
	if !strings.Contains(ctx.frontmatter["phases"], "develop-active") {
		return nil
	}
	const capabilityStates = "configured broken"
	for _, s := range axisListValues(statesRaw) {
		if !strings.Contains(capabilityStates, s) {
			return nil // gate includes a no-capability state — absence-class atom
		}
	}
	modesRaw, hasModes := ctx.frontmatter["modes"]
	if !hasModes {
		return []AtomLintViolation{{
			AtomFile: ctx.file,
			Category: "axis-conjunction",
			Pattern:  "gitPushStates:[capability]-without-modes:[push-source]",
			Line:     1,
			Snippet:  "gitPushStates: " + statesRaw + " — must also declare modes: with push-source-only values (standard, simple, local-stage, local-only). Otherwise the atom fires for ModeDev/ModeStage where the git-push handler hard-rejects.",
		}}
	}
	const pushSources = "standard simple local-stage local-only"
	for _, m := range axisListValues(modesRaw) {
		if !strings.Contains(pushSources, m) {
			return []AtomLintViolation{{
				AtomFile: ctx.file,
				Category: "axis-conjunction",
				Pattern:  "gitPushStates:[capability]-with-non-push-source-mode",
				Line:     1,
				Snippet:  "modes: " + modesRaw + " contains a non-push-source mode (" + m + "). gitPushStates ⊆ {configured, broken} requires modes ⊆ {standard, simple, local-stage, local-only}.",
			}}
		}
	}
	return nil
}

// axisListValues parses the YAML-flow-style axis list `[a, b, c]` into a
// slice of trimmed string values. Returns nil for malformed input rather
// than erroring — atoms_lint already enforces frontmatter shape elsewhere.
func axisListValues(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
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
