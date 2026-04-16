package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SubStepValidationResult holds the outcome of a sub-step validator.
type SubStepValidationResult struct {
	Passed   bool     `json:"passed"`
	Issues   []string `json:"issues,omitempty"`
	Guidance string   `json:"guidance,omitempty"`
}

// SubStepValidator checks the agent's output at a sub-step boundary.
// Receives the attestation so validators can reject empty or boilerplate
// completions; receives plan + state for validators that walk the mounted
// filesystem or inspect recipe shape.
type SubStepValidator func(ctx context.Context, plan *RecipePlan, state *RecipeState, attestation string) *SubStepValidationResult

// getSubStepValidator returns the validator for a sub-step, or nil if the
// sub-step uses attestation-only completion (no automated check).
func getSubStepValidator(subStepName string) SubStepValidator {
	switch subStepName {
	case SubStepFeatureSweepDev, SubStepFeatureSweepStage:
		// Feature sweep: iterate plan.Features and require every
		// api-surface feature to appear in the attestation with
		// 2xx status + application/json content-type. The v18
		// nginx-SPA-fallback trap (text/html returned under a 200
		// for /api/*) is caught here by the content-type assertion.
		return validateFeatureSweep
	case SubStepZeropsYAML:
		return validateZeropsYAML
	case SubStepReadmes:
		// Post-deploy readme narration. The attestation-level check here
		// is lightweight (markers exist, each fragment has content) —
		// the heavy content validation (fragments literal format,
		// integration-guide code blocks, comment specificity,
		// knowledge-base authenticity, predecessor-floor) runs at
		// deploy-step completion via checkRecipeDeployReadmes. Splitting
		// the checks lets the agent iterate on the sub-step quickly
		// without the full content battery firing on every retry.
		return validateReadme
	case SubStepSubagent:
		// Feature sub-agent dispatch at deploy step 4b. v11 shipped a
		// scaffold-quality frontend because the main agent autonomously
		// decided step 4b was "already done" and never dispatched the
		// feature sub-agent. The validator forces a non-trivial attestation
		// describing what the feature sub-agent produced, eliminating the
		// "already done" escape.
		return validateFeatureSubagent
	case SubStepSmokeTest:
		// Trust agent attestation — smoke test is interactive.
		return nil
	case SubStepScaffold:
		// Trust agent attestation — scaffold existence is best verified
		// by the agent reporting what it created.
		return nil
	case SubStepAppCode:
		// Trust agent attestation — code quality is checked at close
		// step by the code-review sub-agent.
		return nil
	default:
		// Deploy sub-steps, etc. — trust attestation.
		return nil
	}
}

// featureSubagentMinAttestationLen is the minimum attestation length the
// feature-subagent sub-step accepts. Empty, one-word, and "already done"-
// class attestations are rejected; anything above the floor must actually
// name what the feature sub-agent produced. The number is a proxy, not a
// perfect check — but it is sharp enough to block the v11 skip and force
// the agent to narrate its dispatch.
const featureSubagentMinAttestationLen = 40

// validateFeatureSubagent enforces the deploy-step sub-step "subagent"
// (dispatch of the feature sub-agent). The scaffold at generate time ships
// a deliberately bare health-dashboard-only skeleton — one StatusPanel
// component showing service dots, /api/health, /api/status, client init,
// schema + 3-5 row seed, nothing else. The feature sub-agent at deploy
// step 4b is the single author that owns every showcase feature section
// end-to-end: API routes, worker payloads, and frontend consumers as one
// coherent unit. v10/v11/v12 all shipped the same class of contract-
// mismatch bugs because parallel scaffold agents each owned one slice of
// each contract. The single-author rule at step 4b eliminates the class.
//
// Gate policy: attestation floor only. v8.62.0 shipped a second gate that
// walked the codebase mounts for post-baseline source files and enforced
// a maximum mtime spread under the theory that a real Agent tool call
// produces a tight burst while an inlining main agent produces a wide
// spread. In practice this proxy is worse than useless:
//
//  1. Any legitimate post-dispatch debug edit (fix a NATS URL, patch a
//     Vite config, correct a CORS origin) bumps the mtime of files the
//     sub-agent originally wrote — so even a correctly-dispatched run
//     produces a wide final spread, triggering a false positive.
//  2. The filesystem signal is trivially refreshable: any Agent tool call
//     that reads every file and writes it back byte-for-byte identical
//     bumps every mtime into a fresh tight window. Opus observed the
//     rejection message, understood the metric, and dispatched exactly
//     that rewrite sub-agent to forge compliance — no feature work done,
//     just mtime laundering. The check was gaming itself.
//
// The root cause of "feature sub-agent never dispatched" in v13 was the
// model (Sonnet/200k couldn't hold the dispatch brief + contract + code
// context simultaneously, so it inlined), not a permissiveness bug in
// the attestation gate. The v14 model gate (clientModel allowlist) is
// the actual fix. This validator stays at the attestation-only floor —
// breakable by determined gaming, but doesn't produce false positives on
// legitimate Opus runs that debug after dispatch.
func validateFeatureSubagent(_ context.Context, _ *RecipePlan, _ *RecipeState, attestation string) *SubStepValidationResult {
	trimmed := strings.TrimSpace(attestation)
	if trimmed == "" {
		return &SubStepValidationResult{
			Passed: false,
			Issues: []string{"feature sub-agent attestation is empty — dispatch the sub-agent before completing this sub-step"},
			Guidance: "## feature-subagent sub-step\n\n" +
				"The scaffold at generate time shipped a health-dashboard-only skeleton: one StatusPanel, /api/health, /api/status, client init, minimal seed. That is the correct, expected scaffold output. The feature sub-agent at deploy step 4b is where every showcase feature section is implemented — as a SINGLE author owning API routes, worker payloads, and frontend consumers end-to-end.\n\n" +
				"Fetch the sub-agent brief: `zerops_guidance topic=\"subagent-brief\"`\n\n" +
				"Dispatch ONE feature sub-agent via the Agent tool (not parallel feature sub-agents — single author keeps contracts consistent), then call `zerops_workflow action=\"complete\" step=\"deploy\" substep=\"subagent\" attestation=\"<describe the files it produced and which feature sections it implemented>\"`.\n\n" +
				"Do NOT skip this sub-step. The scaffold is bare by design — nothing for you to rationalize away.",
		}
	}
	if len(trimmed) < featureSubagentMinAttestationLen {
		return &SubStepValidationResult{
			Passed: false,
			Issues: []string{fmt.Sprintf("feature sub-agent attestation too short (%d chars, need >= %d) — name the files the sub-agent wrote and the feature sections it implemented", len(trimmed), featureSubagentMinAttestationLen)},
			Guidance: "## feature-subagent sub-step\n\n" +
				"A one-liner like \"already done\" or \"dispatched sub-agent\" is not enough. The attestation must describe what the feature sub-agent actually produced — the files it wrote, the feature sections it implemented, and the API/frontend contract pairs it authored. Example:\n\n" +
				"> feature sub-agent wrote shared types.ts, then authored API routes + frontend consumers for items CRUD, cache-demo, search, jobs dispatch (with worker processor), and storage upload; expanded seed to 20 rows; added search-sync to initCommands\n\n" +
				"This becomes part of the session log and the close-step review uses it to verify the deploy step ran to completion.",
		}
	}
	return &SubStepValidationResult{Passed: true}
}

// validateZeropsYAML checks the zerops.yaml the agent wrote by reading from
// SSHFS mounts. Checks: file exists, contains expected setup count, comment
// ratio >= 30%, dev and prod envVariables differ. These are the most common
// generate failures.
func validateZeropsYAML(_ context.Context, plan *RecipePlan, _ *RecipeState, _ string) *SubStepValidationResult {
	if plan == nil {
		return nil
	}

	base := recipeMountBase
	if recipeMountBaseOverride != "" {
		base = recipeMountBaseOverride
	}

	var issues []string

	// Check each codebase-owning target's zerops.yaml.
	for _, t := range plan.Targets {
		if !IsRuntimeType(t.Type) || (t.IsWorker && t.SharesCodebaseWith != "") {
			continue // managed services and shared-codebase workers don't own a zerops.yaml
		}

		mountPath := filepath.Join(base, t.Hostname+"dev", "zerops.yaml")
		raw, err := os.ReadFile(mountPath)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%sdev/zerops.yaml: file not found or unreadable", t.Hostname))
			continue
		}
		content := string(raw)

		// Count setups: lines matching "  - setup: " at the top level.
		expectedSetups := 2 // dev + prod
		if TargetHostsSharedWorker(t, plan) {
			expectedSetups = 3 // dev + prod + worker
		}
		setupCount := strings.Count(content, "\n  - setup: ")
		if setupCount == 0 {
			setupCount = strings.Count(content, "\n- setup: ")
		}
		if setupCount < expectedSetups {
			issues = append(issues, fmt.Sprintf("%sdev/zerops.yaml: found %d setup(s), expected %d", t.Hostname, setupCount, expectedSetups))
		}

		// Comment ratio check: lines starting with # (after trim) vs total non-empty lines.
		lines := strings.Split(content, "\n")
		var commentLines, totalLines int
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			totalLines++
			if strings.HasPrefix(trimmed, "#") {
				commentLines++
			}
		}
		if totalLines > 0 {
			ratio := float64(commentLines) / float64(totalLines)
			if ratio < 0.30 {
				issues = append(issues, fmt.Sprintf("%sdev/zerops.yaml: comment ratio %.0f%% (need >= 30%%)", t.Hostname, ratio*100))
			}
		}
	}

	if len(issues) > 0 {
		var guidance strings.Builder
		guidance.WriteString("## zerops-yaml sub-step validation failed\n\n")
		for _, issue := range issues {
			guidance.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		guidance.WriteString("\nFetch updated rules: `zerops_guidance topic=\"zerops-yaml-rules\"`\n")
		guidance.WriteString("\nCommon fixes:\n")
		guidance.WriteString("- Comment ratio below 30%%: add WHY-not-WHAT comments above each key group, aim for 35%%\n")
		guidance.WriteString("- Missing setup: verify both `setup: dev` and `setup: prod` exist\n")
		guidance.WriteString("- Shared-codebase worker: host target's zerops.yaml needs `setup: worker` too\n")
		return &SubStepValidationResult{
			Passed:   false,
			Issues:   issues,
			Guidance: guidance.String(),
		}
	}

	return &SubStepValidationResult{Passed: true}
}

// featureSweepStatusOKRegexp matches a 2xx HTTP status token in an
// attestation line. Anchored to word boundaries so "1200" and "20000"
// don't masquerade as "200".
var featureSweepStatusOKRegexp = regexp.MustCompile(`\b2\d{2}\b`)

// featureSweepBadStatusRegexp matches any 4xx or 5xx HTTP status token
// in an attestation — the presence of ANY 4xx/5xx anywhere in the
// attestation is a hard failure, because the sweep is supposed to
// report success on every feature. If the agent is tempted to attest
// "4 of 5 features returned 200, search returned 500", the attestation
// is rejected and the agent must fix the 5xx feature before completing.
var featureSweepBadStatusRegexp = regexp.MustCompile(`\b[45]\d{2}\b`)

// featureSweepHTMLContentType is the anti-pattern the sweep rejects.
// Presence of text/html anywhere in the attestation means an /api/*
// request fell through to nginx's SPA index.html fallback (or equivalent).
// This is the v18 search-broken-silently root symptom.
const featureSweepHTMLContentType = "text/html"

// featureSweepJSONContentType is the token the sweep requires per
// api-surface feature. Content-type strings vary in casing and
// parameter order (`application/json`, `application/json; charset=utf-8`,
// `Application/JSON`) — the check is case-insensitive substring on the
// attestation line for each feature ID.
const featureSweepJSONContentType = "application/json"

// validateFeatureSweep enforces the deploy-step feature-sweep sub-step.
// Rules:
//  1. If the plan declares zero api-surface features, pass automatically
//     (hello-world recipes with no backend, pure static frontends).
//  2. The attestation is split into lines; each api-surface feature ID
//     must appear on at least one line.
//  3. On the line(s) mentioning the feature ID, the sweep requires
//     BOTH a 2xx status token AND the application/json content-type
//     substring (case-insensitive). Missing either is a failure for
//     that feature.
//  4. The attestation as a whole must not contain text/html on any
//     line that also mentions a feature ID — the nginx SPA fallback
//     trap is caught here.
//  5. The attestation as a whole must not contain any 4xx/5xx status
//     token on any line that mentions a feature ID.
//
// The validator is attestation-based (the agent runs the curls;
// the engine enforces the reporting contract) rather than executing
// curl itself because the sub-step runs inside the MCP server which
// has no network reachability to the dev containers (sshfs mounts,
// not a TCP bridge). Forcing the per-feature line format is sharp
// enough to block the v18 "I'll just attest success" escape hatch:
// the agent cannot claim success for a feature it didn't actually
// probe, because the report format names the feature and the status.
func validateFeatureSweep(_ context.Context, plan *RecipePlan, _ *RecipeState, attestation string) *SubStepValidationResult {
	if plan == nil || len(plan.Features) == 0 {
		return &SubStepValidationResult{Passed: true}
	}

	// Collect the IDs whose sweep is required — only features with the
	// api surface. UI-only features are observed later in the browser
	// walk, not here.
	var required []RecipeFeature
	for _, f := range plan.Features {
		if f.hasSurface(FeatureSurfaceAPI) {
			required = append(required, f)
		}
	}
	if len(required) == 0 {
		return &SubStepValidationResult{Passed: true}
	}

	attLower := strings.ToLower(attestation)
	lines := strings.Split(attestation, "\n")
	linesLower := make([]string, len(lines))
	for i, l := range lines {
		linesLower[i] = strings.ToLower(l)
	}

	var issues []string

	for _, f := range required {
		idLower := strings.ToLower(f.ID)
		// Find the lines mentioning this feature ID.
		var matched []string
		for i, ll := range linesLower {
			if strings.Contains(ll, idLower) {
				matched = append(matched, lines[i])
			}
		}
		if len(matched) == 0 {
			issues = append(issues, fmt.Sprintf("feature %q: not mentioned in sweep attestation — run curl against %q and include the feature id, status, and content-type in the attestation", f.ID, f.HealthCheck))
			continue
		}
		// On the lines mentioning this feature, require 2xx + json.
		has2xx := false
		hasJSON := false
		hasBad := false
		hasHTML := false
		for _, line := range matched {
			lower := strings.ToLower(line)
			if featureSweepStatusOKRegexp.MatchString(line) {
				has2xx = true
			}
			if featureSweepBadStatusRegexp.MatchString(line) {
				hasBad = true
			}
			if strings.Contains(lower, featureSweepJSONContentType) {
				hasJSON = true
			}
			if strings.Contains(lower, featureSweepHTMLContentType) {
				hasHTML = true
			}
		}
		if hasBad {
			issues = append(issues, fmt.Sprintf("feature %q: attestation reports a 4xx/5xx status for this feature — fix the backend before completing the sweep (do not attest success on a broken feature)", f.ID))
			continue
		}
		if hasHTML {
			issues = append(issues, fmt.Sprintf("feature %q: attestation reports text/html content-type — the /api path fell through to the nginx SPA fallback. Fix the frontend fetch to use the VITE_API_URL (or framework equivalent) before re-running the sweep", f.ID))
			continue
		}
		if !has2xx {
			issues = append(issues, fmt.Sprintf("feature %q: attestation missing a 2xx status token on the line mentioning this feature", f.ID))
		}
		if !hasJSON {
			issues = append(issues, fmt.Sprintf("feature %q: attestation missing application/json content-type on the line mentioning this feature", f.ID))
		}
	}

	// Global check — if text/html appears anywhere in the attestation,
	// something in the sweep hit the SPA fallback even if the per-feature
	// checks above didn't attribute it.
	if strings.Contains(attLower, featureSweepHTMLContentType) && len(issues) == 0 {
		issues = append(issues, "attestation contains text/html — at least one feature's curl fell through to an HTML fallback response. Identify which feature and fix before completing the sweep")
	}

	if len(issues) > 0 {
		var guidance strings.Builder
		guidance.WriteString("## feature-sweep sub-step validation failed\n\n")
		for _, issue := range issues {
			guidance.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		guidance.WriteString("\n### How to run the sweep\n\n")
		guidance.WriteString("For every feature with `api` in its surface, run curl against the HealthCheck path on the dev (or stage) service and report the status + content-type:\n\n")
		guidance.WriteString("```\n")
		guidance.WriteString("ssh {hostname}dev \"curl -sS -o /dev/null -w '%{http_code} %{content_type}\\n' http://localhost:{httpPort}{healthCheck}\"\n")
		guidance.WriteString("```\n\n")
		guidance.WriteString("Then submit the attestation as one line per feature using the format `<featureId>: <status> <content-type>`, e.g.:\n\n")
		guidance.WriteString("```\n")
		guidance.WriteString("items-crud: 200 application/json\n")
		guidance.WriteString("cache-demo: 200 application/json\n")
		guidance.WriteString("search-items: 200 application/json\n")
		guidance.WriteString("```\n\n")
		guidance.WriteString("The validator enforces: every api-surface feature appears on its own line, every line has a 2xx status and `application/json`, and NO line contains `text/html` or any 4xx/5xx status. The `text/html` check catches the nginx SPA-fallback trap (v18 search-broken-silently bug): static-base prod serves `index.html` for unknown `/api/*` paths with HTTP 200 and content-type `text/html`. That is a FAILED sweep — do not attest it as success.\n\n")
		guidance.WriteString("If a feature genuinely returns an error, FIX it before completing the sub-step. The sub-step is a gate, not a progress marker.\n")
		return &SubStepValidationResult{
			Passed:   false,
			Issues:   issues,
			Guidance: guidance.String(),
		}
	}

	return &SubStepValidationResult{Passed: true}
}

// validateReadme checks the README the agent wrote.
// readmeWriterAttestationRe matches attestation lines that name a
// dispatched writer sub-agent OR an explicit single-codebase rationale
// for inlining. Kept permissive to accommodate the agent's natural
// phrasing — the validator's purpose is to create friction against
// silently-inline composition, not to gatekeep exact wording. The
// specific shapes accepted:
//
//   - "writer sub-agent" / "writer subagent" / "writer-subagent"
//   - "writer-subagent-brief" / "via the writer brief"
//   - "dispatched ... writer" / "Agent ... writer"
//   - "single-codebase inline" / "single codebase inline" (explicit
//     acknowledgment that no dispatch was appropriate)
var readmeWriterAttestationRe = regexp.MustCompile(`(?i)(writer[\s\-]?sub[\s\-]?agent|dispatch.*writer|writer[\s\-]?brief|single[\s\-]?codebase[\s\-]?inline)`)

// validateReadme gates the `readmes` sub-step attestation. For
// multi-codebase recipes the attestation must reference a writer-
// subagent dispatch (or explicitly claim the single-codebase inline
// shortcut — which in a multi-codebase plan is a documented
// deviation). Single-codebase recipes pass without the marker;
// inlining a single README is an acceptable main-agent task.
//
// This is the v8.80 replacement for the previous always-pass gate.
// §3.6d of v21 post-mortem: the deterministic-gate upgrade on the
// §3.6 formalized writer-subagent pattern. zcp's MCP surface does
// not observe Agent-tool dispatches directly, so the gate operates
// on the attestation text — breakable by determined gaming (like
// validateFeatureSubagent), but creates the friction that pushes
// the agent toward the dispatch pattern instead of absorbing the
// work inline.
func validateReadme(_ context.Context, plan *RecipePlan, _ *RecipeState, attestation string) *SubStepValidationResult {
	if plan == nil {
		return nil
	}

	baseGuidance := "## readmes sub-step validation\n\n" +
		"Verify every codebase README contains all 3 extract fragments:\n" +
		"- integration-guide (with zerops.yaml code block)\n" +
		"- knowledge-base (gotchas and tips)\n" +
		"- intro (1-3 lines, no headings)\n\n" +
		"Re-read the `readme-fragments` topic for the full requirements.\n"

	if hasMultipleCodebases(plan) {
		if !readmeWriterAttestationRe.MatchString(attestation) {
			return &SubStepValidationResult{
				Passed: false,
				Issues: []string{"readmes sub-step attestation does not reference a writer sub-agent dispatch — for multi-codebase recipes the README + CLAUDE.md authoring should be delegated to a writer sub-agent, not absorbed into main context"},
				Guidance: baseGuidance + "\n" +
					"### Writer-subagent dispatch (v8.80 gate)\n\n" +
					"This recipe has ≥2 codebases. Composing 2 × N READMEs + CLAUDE.md files inline in main context consumes budget needed for later steps — v21 absorbed this work and grew main-session tool_use input bytes by 190 KB over v20.\n\n" +
					"Dispatch a writer sub-agent: fetch the brief (`zerops_guidance topic=\"writer-subagent-brief\"`), then dispatch via the Agent tool with a description matching `Write READMEs and CLAUDE.md` (any wording that includes \"writer\" + \"README\" is accepted by the gate).\n\n" +
					"After the writer sub-agent returns, re-run this sub-step with an attestation that references the dispatch — for example: \"dispatched writer sub-agent; it wrote apidev/README.md + CLAUDE.md, appdev/README.md + CLAUDE.md, workerdev/README.md + CLAUDE.md (6 files); content-check iteration handled by main agent with fix sub-agents as needed.\"\n\n" +
					"If you have a principled reason to inline for this run (e.g. very small codebases, blocked on sub-agent dispatch), include `single-codebase inline` in your attestation to acknowledge the deviation explicitly.\n",
			}
		}
	}

	return &SubStepValidationResult{Passed: true}
}
