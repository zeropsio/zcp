package sync

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/schema"
)

// PushRecipes pushes local recipe knowledge to GitHub app repos as PRs.
func PushRecipes(cfg *Config, root, filter string, dryRun bool) ([]PushResult, error) {
	recipesDir := filepath.Join(root, cfg.Paths.Output, "recipes")
	recipes, err := findLocalRecipes(recipesDir, filter)
	if err != nil {
		return nil, err
	}

	var results []PushResult
	for _, slug := range recipes {
		result := pushOneRecipe(cfg, root, slug, dryRun)
		results = append(results, result)
	}
	return results, nil
}

func findLocalRecipes(dir, filter string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read recipes dir %s: %w", dir, err)
	}

	var slugs []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(entry.Name(), ".md")
		if filter != "" && slug != filter {
			continue
		}
		slugs = append(slugs, slug)
	}
	return slugs, nil
}

// recipeFragments holds all extracted fragments from a recipe .md file.
// ZeropsYAML is always derived from IntegrationGuide — they share the same
// YAML code block. Editing the YAML in the integration-guide section
// automatically updates both the README markers and the zerops.yaml file.
type recipeFragments struct {
	KnowledgeBase    string
	IntegrationGuide string
	Intro            string
	ZeropsYAML       string // derived from IntegrationGuide, never independent
}

func extractFragments(content string) recipeFragments {
	ig := ExtractIntegrationGuide(content)

	// ZeropsYAML is the YAML code block WITHIN the integration-guide.
	// Single source of truth: edit the YAML in the ## zerops.yaml section,
	// and both the README integration-guide markers AND zerops.yaml file update.
	var yaml string
	if ig != "" {
		yaml = extractYAMLFromFragment(ig)
	}

	return recipeFragments{
		KnowledgeBase:    ExtractKnowledgeBase(content),
		IntegrationGuide: ig,
		Intro:            ExtractIntro(content),
		ZeropsYAML:       yaml,
	}
}

func (f recipeFragments) hasContent() bool {
	return f.KnowledgeBase != "" || f.IntegrationGuide != "" || f.Intro != ""
}

// extractYAMLFromFragment extracts the first ```yaml block from a fragment.
func extractYAMLFromFragment(fragment string) string {
	lines := strings.Split(fragment, "\n")
	inYAML := false
	var out []string

	for _, line := range lines {
		if strings.HasPrefix(line, "```yaml") {
			inYAML = true
			continue
		}
		if inYAML && strings.HasPrefix(line, "```") {
			break
		}
		if inYAML {
			out = append(out, line)
		}
	}

	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n")
}

func pushOneRecipe(cfg *Config, root, slug string, dryRun bool) PushResult {
	// 1. Read local recipe
	recipeFile := filepath.Join(root, cfg.Paths.Output, "recipes", slug+".md")
	content, err := os.ReadFile(recipeFile)
	if err != nil {
		return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("read recipe: %w", err)}
	}

	// 2. Extract all fragments
	frags := extractFragments(string(content))
	if !frags.hasContent() {
		return PushResult{Slug: slug, Status: Skipped, Reason: "no pushable content"}
	}

	// 3. Resolve GitHub repo from frontmatter (written during pull from API's gitRepo field)
	repo := resolveRepo(string(content), cfg, slug)
	if repo == "" {
		return PushResult{Slug: slug, Status: Skipped, Reason: "no repo in frontmatter and pattern resolution failed"}
	}

	gh := &GH{Repo: repo}

	if dryRun {
		return pushRecipeDryRun(gh, slug, frags)
	}

	return pushRecipeCreate(cfg, gh, slug, frags)
}

// resolveRepo extracts the app repo from frontmatter (authoritative, written by pull).
// Falls back to config pattern matching if frontmatter has no repo field.
func resolveRepo(content string, cfg *Config, slug string) string {
	repoURL := ExtractRepo(content)
	if repoURL != "" {
		// Convert "https://github.com/org/repo" → "org/repo"
		repoURL = strings.TrimPrefix(repoURL, "https://github.com/")
		repoURL = strings.TrimPrefix(repoURL, "http://github.com/")
		repoURL = strings.TrimSuffix(repoURL, ".git")
		return repoURL
	}

	// Fallback: pattern matching (for recipes without frontmatter repo)
	return cfg.ResolveRecipeRepo(slug, &GH{})
}

// yamlAction is the verdict for whether to push the recipe's zerops.yaml block
// to the app repo. Replaces the old byte-length heuristic: length is not a proxy
// for correctness — it silently FROZE a schema-invalid published file whenever
// the curated fix was shorter (the B2/nodejs run.verticalAutoscaling case), and
// blocked legitimate field removals. Validity is the axis instead.
type yamlAction int

const (
	yamlNoop           yamlAction = iota // existing == new, nothing to do
	yamlCreate                           // no existing file yet
	yamlUpdate                           // overwrite the published file
	yamlSkipInvalidNew                   // new YAML is schema-invalid — never publish a broken file
	yamlSkipDivergent                    // both valid but differ — don't silently overwrite a possibly-richer published copy
)

// zeropsYAMLAction is the SINGLE owner of the zerops.yaml push decision; both the
// dry-run and the real push call it, so the preview can never drift from what is
// actually committed. Decision axis is schema validity, not length:
//   - new schema-invalid                 → refuse (never publish a broken file)
//   - existing schema-invalid, new valid → push (replace a broken published file
//     with the curated valid recipe version — the B2/nodejs case the old length
//     guard used to freeze, because the fix was shorter)
//   - both valid but differ              → skip + SURFACE the divergence (the
//     recipe .md may be thinner than a hand-tuned richer app-repo copy, e.g. a
//     cross-service ${db_*} ref; the now-accurate dry-run shows the diff so a
//     human reconciles at the recipe .md rather than a length heuristic guessing)
//
// Validation is structure-only (ValidateZeropsYAMLStructure: value enums stripped)
// so a base/runtime version newer than the embedded schema snapshot never
// false-rejects — same contract export/launch use.
func zeropsYAMLAction(newYAML, existing string, exists bool) (yamlAction, string) {
	if !exists {
		return yamlCreate, ""
	}
	if strings.TrimSpace(newYAML) == strings.TrimSpace(existing) {
		return yamlNoop, ""
	}
	if errs := schema.ValidateZeropsYAMLStructure(newYAML, ""); len(errs) > 0 {
		return yamlSkipInvalidNew, "recipe zerops.yaml is schema-invalid (" + errs[0].Error() + ") — not published"
	}
	if errs := schema.ValidateZeropsYAMLStructure(existing, ""); len(errs) > 0 {
		return yamlUpdate, "" // published file is schema-invalid; the curated recipe version replaces it
	}
	return yamlSkipDivergent, "live zerops.yaml diverges from the recipe (both schema-valid) — review the diff; reconcile at the recipe .md if it should change"
}

// recipePushDecision is the shared outcome both the dry-run and the real push
// drive off, so they cannot disagree about what would change.
type recipePushDecision struct {
	readme          string // README with fragments injected, ready to commit
	readmeSHA       string
	readmeChanged   bool
	yamlAct         yamlAction
	yamlReason      string
	existingYAMLSHA string
	changedParts    []string // human-facing list for the dry-run Diff
}

// shouldPR reports whether anything actually needs a PR (README changed or the
// zerops.yaml file would be created/updated). Kills the empty-PR drift where the
// real push created a branch+commit+PR even on a no-op README injection.
func (d recipePushDecision) shouldPR() bool {
	return d.readmeChanged || d.yamlAct == yamlCreate || d.yamlAct == yamlUpdate
}

// decideRecipePush reads the app repo's live README + zerops.yaml ONCE and
// computes every push decision. Single owner — pushRecipeDryRun and
// pushRecipeCreate both call it, so the dry-run is an exact preview of the push.
func decideRecipePush(gh *GH, frags recipeFragments) (recipePushDecision, error) {
	readme, readmeSHA, err := gh.ReadFile("README.md")
	if err != nil {
		return recipePushDecision{}, fmt.Errorf("read README: %w", err)
	}
	injected := injectAllFragments(readme, frags)
	d := recipePushDecision{readme: injected, readmeSHA: readmeSHA, readmeChanged: injected != readme}
	if d.readmeChanged {
		if frags.IntegrationGuide != "" {
			d.changedParts = append(d.changedParts, "integration-guide")
		}
		if frags.KnowledgeBase != "" {
			d.changedParts = append(d.changedParts, "knowledge-base")
		}
	}
	if frags.ZeropsYAML != "" {
		existing, sha, readErr := gh.ReadFile("zerops.yaml")
		d.existingYAMLSHA = sha
		d.yamlAct, d.yamlReason = zeropsYAMLAction(frags.ZeropsYAML, existing, readErr == nil)
		if d.yamlAct == yamlCreate || d.yamlAct == yamlUpdate {
			d.changedParts = append(d.changedParts, "zerops.yaml")
		}
	}
	return d, nil
}

func pushRecipeDryRun(gh *GH, slug string, frags recipeFragments) PushResult {
	d, err := decideRecipePush(gh, frags)
	if err != nil {
		// README missing on the app repo — a fresh push would create fragments.
		return PushResult{Slug: slug, Status: DryRun, Diff: "new file with fragments"}
	}
	if !d.shouldPR() {
		reason := "no changes"
		if d.yamlReason != "" {
			reason = d.yamlReason // surface a skip-divergent / skip-invalid yaml even when README is a no-op
		}
		return PushResult{Slug: slug, Status: Skipped, Reason: reason}
	}
	diff := fmt.Sprintf("would update: %s", strings.Join(d.changedParts, ", "))
	if d.yamlReason != "" {
		diff += " (zerops.yaml: " + d.yamlReason + ")"
	}
	return PushResult{Slug: slug, Status: DryRun, Diff: diff}
}

// injectAllFragments injects non-empty fragments into the README.
// Intro is NOT injected — the pull-side strips markdown links and collapses
// whitespace, making the frontmatter description lossy. Pushing it back would
// overwrite the richer original in the README. The intro marker is read-only.
func injectAllFragments(readme string, frags recipeFragments) string {
	if frags.IntegrationGuide != "" {
		readme = InjectFragment(readme, "integration-guide", frags.IntegrationGuide)
	}
	if frags.KnowledgeBase != "" {
		readme = InjectFragment(readme, "knowledge-base", frags.KnowledgeBase)
	}
	return readme
}

func pushRecipeCreate(cfg *Config, gh *GH, slug string, frags recipeFragments) PushResult {
	// Shared decision — same verdict the dry-run reported.
	d, err := decideRecipePush(gh, frags)
	if err != nil {
		return PushResult{Slug: slug, Status: Error, Err: err}
	}
	if !d.shouldPR() {
		reason := "no changes"
		if d.yamlReason != "" {
			reason = d.yamlReason
		}
		return PushResult{Slug: slug, Status: Skipped, Reason: reason}
	}

	// Branch — date + short random suffix so a same-day second push of the same
	// recipe doesn't hit "reference already exists".
	branch := fmt.Sprintf("%s/%s-%s-%s", cfg.Push.Recipes.BranchPrefix, slug, today(), shortRand())
	if err := gh.CreateBranch(branch); err != nil {
		return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("create branch: %w", err)}
	}

	commitMsg := fmt.Sprintf("%s: update %s", cfg.Push.Recipes.CommitPrefix, slug)
	if d.readmeChanged {
		if err := gh.UpdateFile("README.md", branch, commitMsg, d.readme, d.readmeSHA); err != nil {
			return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("update README: %w", err)}
		}
	}
	// zerops.yaml: create OR update only — never the byte-length guard. A
	// schema-invalid new file or a valid-but-divergent published copy is left
	// untouched (the dry-run surfaced the reason).
	switch d.yamlAct {
	case yamlCreate:
		_ = gh.UpdateFile("zerops.yaml", branch, commitMsg, frags.ZeropsYAML+"\n", "")
	case yamlUpdate:
		_ = gh.UpdateFile("zerops.yaml", branch, commitMsg, frags.ZeropsYAML+"\n", d.existingYAMLSHA)
	case yamlNoop, yamlSkipInvalidNew, yamlSkipDivergent:
		// no zerops.yaml commit
	}

	title := fmt.Sprintf("%s: update %s knowledge", cfg.Push.Recipes.CommitPrefix, slug)
	body := "Automated knowledge sync from ZCP.\n\nUpdates README.md fragments (intro, integration-guide, knowledge-base) and zerops.yaml."
	prURL, err := gh.CreatePR(branch, title, body)
	if err != nil {
		return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("create PR: %w", err)}
	}

	return PushResult{Slug: slug, Status: Created, PRURL: prURL}
}

func today() string {
	return time.Now().Format("20060102")
}

// shortRand returns a 4-character hex string for branch name uniqueness.
func shortRand() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b)
}
