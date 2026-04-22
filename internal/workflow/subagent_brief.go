package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// concatAtomsRendered is the render-aware cousin of concatAtoms. Each
// atom body is rendered through LoadAtomBodyRendered against the
// supplied plan context before being joined. The Cx-SUBAGENT-BRIEF-
// BUILDER path MUST use this helper, not concatAtoms, so the prompt
// that reaches the Task dispatch has zero surviving `{{...}}` tokens.
// (The pre-Cx-5 envelope path rendered at last-mile fetch via
// dispatch-brief-atom; Cx-5 moves the render into the build step so
// the SHA can be computed over a fully-resolved prompt.)
func concatAtomsRendered(ctx AtomRenderContext, ids ...string) (string, error) {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		body, err := LoadAtomBodyRendered(id, ctx)
		if err != nil {
			return "", fmt.Errorf("render atom %q: %w", id, err)
		}
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

// SubagentBriefResult carries everything a `build-subagent-brief` tool
// call returns. The prompt is the fully-stitched dispatch brief; the
// main agent's contract is to forward Prompt verbatim to Task without
// paraphrasing, compressing, or re-ordering. PromptSHA is stored in
// RecipeState.LastSubagentBrief so a subsequent VerifySubagentDispatch
// call can compare against the hash of whatever prompt actually got
// dispatched — the v37 F-17 headline fix.
type SubagentBriefResult struct {
	Role        string `json:"role"`
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
	PromptSHA   string `json:"promptSha"`
	NextTool    string `json:"nextTool"`
}

// SubagentBriefRecord is what the engine persists in RecipeState per
// role. BuiltAt is the RFC-3339 timestamp at build time. It travels
// along with the session json file and therefore survives compaction
// of the main-agent conversation (see docs/spec-work-session.md).
type SubagentBriefRecord struct {
	Role        string `json:"role"`
	Description string `json:"description"`
	PromptSHA   string `json:"promptSha"`
	BuiltAt     string `json:"builtAt"`
	PromptSize  int    `json:"promptSize"`
}

// Canonical role identifiers for the build-subagent-brief / verify-
// subagent-dispatch actions. Role keys are case-normalised to these
// strings before lookup; call-sites accept any case.
const (
	SubagentRoleWriter           = "writer"
	SubagentRoleEditorialReview  = "editorial-review"
	SubagentRoleCodeReview       = "code-review"
	subagentBriefTool            = "Task"
	subagentBriefNextToolMessage = "dispatch via Task(description=<returned description>, subagent_type=\"general-purpose\", prompt=<returned prompt>) — prompt bytes MUST match verbatim"
)

// NormalizeSubagentRole returns the canonical form of a role name or
// the empty string if the role is not recognised. Lowercase-only match;
// callers are expected to surface INVALID_PARAMETER on empty return.
func NormalizeSubagentRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case SubagentRoleWriter, "writer-sub-agent":
		return SubagentRoleWriter
	case SubagentRoleEditorialReview, "editorial_review", "editorial":
		return SubagentRoleEditorialReview
	case SubagentRoleCodeReview, "code_review", "codereview":
		return SubagentRoleCodeReview
	}
	return ""
}

// BuildSubagentBrief stitches the role-specific dispatch brief from
// the atom corpus, computes its SHA-256 hash, and returns the full
// result the handler sends back to the main agent.
//
// Paths:
//
//   - writer             → BuildWriterDispatchBrief(plan, factsLogPath)
//   - editorial-review   → BuildEditorialReviewDispatchBrief(plan, factsLogPath, manifestPath)
//   - code-review        → BuildCodeReviewDispatchBrief(plan, manifestPath)
//
// The plan MUST be non-nil — brief stitching interpolates live plan
// context (hostnames, slug, etc.) and a nil plan would produce a
// template-studded prompt. Caller guards this.
func BuildSubagentBrief(plan *RecipePlan, role, factsLogPath, manifestPath string) (SubagentBriefResult, error) {
	canonical := NormalizeSubagentRole(role)
	if canonical == "" {
		return SubagentBriefResult{}, fmt.Errorf("unknown subagent role %q (expected one of: writer, editorial-review, code-review)", role)
	}
	if plan == nil {
		return SubagentBriefResult{}, fmt.Errorf("build-subagent-brief role=%s requires an active recipe plan", canonical)
	}

	ctx := RenderContextFromPlan(plan, "")
	ctx.FactsLogPath = factsLogPath
	ctx.ManifestPath = manifestPath

	var prompt string
	var description string
	var err error

	switch canonical {
	case SubagentRoleWriter:
		prompt, err = buildWriterBriefRendered(ctx, factsLogPath)
		description = "Author recipe READMEs + CLAUDE.md + manifest"
	case SubagentRoleEditorialReview:
		prompt, err = buildEditorialReviewBriefRendered(ctx, factsLogPath, manifestPath)
		description = "Editorial review of recipe reader-facing content"
	case SubagentRoleCodeReview:
		prompt, err = buildCodeReviewBriefRendered(ctx, manifestPath)
		description = "Code review of recipe scaffold + features"
	}
	if err != nil {
		return SubagentBriefResult{}, fmt.Errorf("build %s brief: %w", canonical, err)
	}
	if strings.Contains(prompt, "{{") || strings.Contains(prompt, "}}") {
		return SubagentBriefResult{}, fmt.Errorf(
			"build %s brief: prompt carries unresolved template tokens — refuse to ship a paraphrase-prone brief to the dispatch guard",
			canonical,
		)
	}

	return SubagentBriefResult{
		Role:        canonical,
		Prompt:      prompt,
		Description: description,
		PromptSHA:   HashPromptSHA(prompt),
		NextTool:    subagentBriefNextToolMessage,
	}, nil
}

// writerExamplesPerSurface is how many examples per writer-authored
// surface the engine samples into the dispatch brief's pre-loaded
// input block. 2 gives one pass + one fail per surface when both are
// in the bank; keeps the block under ~4KB at current bank size.
const writerExamplesPerSurface = 2

// buildWriterBriefRendered stitches the writer dispatch brief using the
// render-aware atom loader so every `{{.Field}}` reference resolves
// against the plan context BEFORE the hash is computed. Canonical
// concatenation order: body atoms, principles atoms, pre-loaded input
// block (annotated examples per surface — v39 Commit 3), input files.
//
// v39 Commit 3 — the pre-loaded input block inlines 2 annotated
// examples per writer-authored surface (gotcha, ig-item, claude-section,
// zerops-yaml-comment). Pattern-matching against concrete shapes closes
// folk-doctrine + wrong-surface at authoring time instead of relying
// on post-hoc editorial-review catches. Examples are bank-sampled
// deterministically so brief SHA stays stable across retries.
func buildWriterBriefRendered(ctx AtomRenderContext, factsLogPath string) (string, error) {
	body, err := concatAtomsRendered(ctx, writerBriefBodyAtomIDs()...)
	if err != nil {
		return "", err
	}
	principles, err := concatAtomsRendered(ctx, writerPrinciples()...)
	if err != nil {
		return "", err
	}
	examples, err := BuildWriterExampleInputBlock(writerExamplesPerSurface)
	if err != nil {
		return "", fmt.Errorf("build example input block: %w", err)
	}
	var b strings.Builder
	b.WriteString(body)
	if principles != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(principles)
	}
	if examples != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString("# Pre-loaded input — annotated surface examples\n\n")
		b.WriteString("Pattern-match every piece of content you author against these examples BEFORE treating it as shippable. Each section covers one of the writer-authored surfaces with `[FAIL]` (shapes to avoid) and `[PASS]` (shapes to model after) cases. When a bullet you plan to write resembles a FAIL example, reclassify per the classification taxonomy and reroute (or discard) before publishing.\n\n")
		b.WriteString(examples)
	}
	if yamls := renderWriterCodebaseYAMLInput(ctx); yamls != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(yamls)
	}
	if factsLogPath != "" {
		b.WriteString("\n\n---\n\n## Input files\n\n")
		fmt.Fprintf(&b, "- Facts log: `%s`\n", factsLogPath)
		b.WriteString("\nThe facts log carries one JSONL record per zerops_record_fact call made during deploy. When authoring gotchas + IG items, prioritize facts with `routeTo: content_gotcha` or `routeTo: content_ig` — those are the surfaces you author directly. Facts with other routing values (claude_md, zerops_yaml_comment, discarded, scaffold_preamble, feature_preamble) inform cross-surface decisions but you do not author content from them. Empty or unset `routeTo` means the recorder did not classify — read those as candidates and classify them via the classification-pointer before authoring.\n")
	}
	return b.String(), nil
}

// renderWriterCodebaseYAMLInput (v39 Commit 3c) inlines each codebase's
// actual `zerops.yaml` into the writer brief as a pre-loaded input
// block. When the writer authors gotchas or IG items that reference
// config-level behavior (execOnce, deployFiles, httpSupport,
// initCommands, etc.), it pattern-matches against the real yaml rather
// than inventing patterns from memory. Closes a class of folk-doctrine
// at its source: the author sees the exact file the recipe ships.
//
// Reads from RecipeProjectRoot/{hostname}dev/zerops.yaml. Files missing
// at brief-build time (pre-scaffold or mid-iteration paths) are
// skipped silently with a placeholder — the block still renders, and
// writer knows which codebases had available yaml.
func renderWriterCodebaseYAMLInput(ctx AtomRenderContext) string {
	if len(ctx.Hostnames) == 0 || ctx.ProjectRoot == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Pre-loaded input — per-codebase `zerops.yaml`\n\n")
	b.WriteString("Each codebase below carries the `zerops.yaml` the recipe actually ships. When you write a gotcha or IG item referencing a config mechanism (`execOnce`, `deployFiles`, `httpSupport`, `initCommands`, `readinessCheck`, etc.), pattern-match your wording against the EXACT field values below — do NOT invent patterns from memory. A claim about \"our `./dist/~` tilde strip\" that doesn't match what's below is a fabrication; either reword to match the yaml or drop the claim.\n\n")
	haveAny := false
	for _, host := range ctx.Hostnames {
		path := filepath.Join(ctx.ProjectRoot, host+"dev", "zerops.yaml")
		data, err := os.ReadFile(path) // #nosec G304 -- path composed from vetted plan hostnames under a const project root
		if err != nil {
			fmt.Fprintf(&b, "## `%s` — `zerops.yaml` not yet present (`%s`)\n\nThe codebase's `zerops.yaml` was not readable at brief-build time. Author config-referencing content cautiously; prefer qualitative phrasing over field-specific quotes for this codebase.\n\n", host, path)
			haveAny = true
			continue
		}
		haveAny = true
		fmt.Fprintf(&b, "## `%s` — `%s`\n\n```yaml\n%s```\n\n", host, path, string(data))
	}
	if !haveAny {
		return ""
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// buildEditorialReviewBriefRendered stitches the editorial-review brief.
// Porter-premise requires fresh-reader stance; no prior-discoveries
// block is appended here.
func buildEditorialReviewBriefRendered(ctx AtomRenderContext, factsLogPath, manifestPath string) (string, error) {
	body, err := concatAtomsRendered(ctx,
		"briefs.editorial-review.mandatory-core",
		"briefs.editorial-review.porter-premise",
		"briefs.editorial-review.surface-walk-task",
		"briefs.editorial-review.single-question-tests",
		"briefs.editorial-review.classification-reclassify",
		"briefs.editorial-review.citation-audit",
		"briefs.editorial-review.counter-example-reference",
		"briefs.editorial-review.cross-surface-ledger",
		"briefs.editorial-review.reporting-taxonomy",
		"briefs.editorial-review.completion-shape",
	)
	if err != nil {
		return "", err
	}
	principles, err := concatAtomsRendered(ctx, editorialReviewPrinciples()...)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(body)
	if principles != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(principles)
	}
	if factsLogPath != "" || manifestPath != "" {
		b.WriteString("\n\n---\n\n## Pointer inputs (open on demand only)\n\n")
		if factsLogPath != "" {
			fmt.Fprintf(&b, "- Facts log: `%s`\n", factsLogPath)
		}
		if manifestPath != "" {
			fmt.Fprintf(&b, "- Content manifest: `%s`\n", manifestPath)
		}
	}
	return b.String(), nil
}

// buildCodeReviewBriefRendered stitches the code-review dispatch brief.
func buildCodeReviewBriefRendered(ctx AtomRenderContext, manifestPath string) (string, error) {
	body, err := concatAtomsRendered(ctx,
		"briefs.code-review.mandatory-core",
		"briefs.code-review.task",
		"briefs.code-review.manifest-consumption",
		"briefs.code-review.reporting-taxonomy",
		"briefs.code-review.completion-shape",
	)
	if err != nil {
		return "", err
	}
	principles, err := concatAtomsRendered(ctx, codeReviewPrinciples()...)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(body)
	if principles != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(principles)
	}
	if manifestPath != "" {
		fmt.Fprintf(&b, "\n\n---\n\n## Input files\n\n- Content manifest: `%s`\n", manifestPath)
	}
	return b.String(), nil
}

// HashPromptSHA returns the hex-encoded SHA-256 of a prompt string.
// The hash is the dispatch-guard's load-bearing comparison: a main-
// agent paraphrase (anything but a byte-for-byte copy) produces a
// different hash and the guard refuses the dispatch.
func HashPromptSHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SubagentDispatchDescriptionKeywords lists the lowercased substrings
// that identify a Task/Agent dispatch description as belonging to
// a guarded role. The role resolver returns the first matching role
// when the description contains any keyword for that role.
//
// Ordering matters: code-review comes before writer so "Code review of
// recipe READMEs" is not classified as writer because of "recipe readmes".
// The resolver tests each role block in the order listed.
var subagentDispatchDescriptionKeywords = []struct {
	role     string
	keywords []string
}{
	{SubagentRoleEditorialReview, []string{"editorial review", "editorial-review"}},
	{SubagentRoleCodeReview, []string{"code review", "code-review"}},
	{SubagentRoleWriter, []string{"author recipe", "writer sub-agent", "author readmes", "readme", "manifest"}},
}

// ResolveSubagentRoleFromDescription inspects a Task/Agent dispatch
// description and returns the role it targets or "" when the description
// doesn't match a guarded role. Used by the dispatch guard to decide
// whether a given Task call needs prompt-SHA verification.
func ResolveSubagentRoleFromDescription(description string) string {
	low := strings.ToLower(description)
	for _, entry := range subagentDispatchDescriptionKeywords {
		for _, kw := range entry.keywords {
			if strings.Contains(low, kw) {
				return entry.role
			}
		}
	}
	return ""
}

// VerifySubagentDispatch is the pure-function half of the dispatch
// guard. Given the description + prompt a Task call is about to use
// and the engine's current RecipeState, it returns (role, ok) where
// ok is true iff the prompt matches the last-built brief hash for
// the detected role. Callers map ok=false to a SUBAGENT_MISUSE error.
//
// Returns role="" + ok=true when the description doesn't match a
// guarded role — non-guarded Task dispatches pass through the guard
// untouched.
func VerifySubagentDispatch(state *RecipeState, description, prompt string) (role string, ok bool, reason string) {
	role = ResolveSubagentRoleFromDescription(description)
	if role == "" {
		return "", true, ""
	}
	if state == nil || state.LastSubagentBrief == nil {
		return role, false, fmt.Sprintf("no build-subagent-brief call for role=%s in this session", role)
	}
	record, exists := state.LastSubagentBrief[role]
	if !exists {
		return role, false, fmt.Sprintf("no build-subagent-brief call for role=%s in this session", role)
	}
	submittedSHA := HashPromptSHA(prompt)
	if submittedSHA != record.PromptSHA {
		return role, false, fmt.Sprintf(
			"prompt SHA %s does not match last-built brief SHA %s for role=%s — the Task prompt must be the byte-identical output of zerops_workflow action=build-subagent-brief role=%s",
			submittedSHA, record.PromptSHA, role, role,
		)
	}
	return role, true, ""
}
