package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkRecipeFinalize validates that all recipe repo files are generated and correct.
func checkRecipeFinalize(outputDir string) workflow.RecipeStepChecker {
	return func(ctx context.Context, plan *workflow.RecipePlan, _ *workflow.RecipeState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}

		dir := outputDir
		if dir == "" {
			return nil, fmt.Errorf("output directory not set")
		}

		var checks []workflow.StepCheck

		// Check main README exists.
		checks = append(checks, checkFileExists(dir, "README.md")...)

		// Check per-environment files.
		for i := 0; i < workflow.EnvTierCount(); i++ {
			folder := workflow.EnvFolder(i)
			checks = append(checks, checkFileExists(dir, filepath.Join(folder, "import.yaml"))...)
			checks = append(checks, checkFileExists(dir, filepath.Join(folder, "README.md"))...)
		}

		// Validate import.yaml files.
		for i := 0; i < workflow.EnvTierCount(); i++ {
			folder := workflow.EnvFolder(i)
			importPath := filepath.Join(dir, folder, "import.yaml")
			data, err := os.ReadFile(importPath)
			if err != nil {
				continue // file existence already checked above
			}
			checks = append(checks, validateImportYAML(ctx, string(data), plan, i, folder)...)
		}

		// Reject TODO scaffold markers in the app README deliverable — if the
		// agent didn't overlay the real README from the mount, the scaffold's
		// `TODO: paste ...` / `**TODO** — add framework-specific gotchas`
		// would otherwise reach the published recipe.
		checks = append(checks, checkAppREADMENoScaffoldTODOs(dir)...)

		// C-6: canonical output tree only (P8). check-rewrite.md §16 places
		// this at close-entry; the close step currently has no checker
		// (administrative trigger), so it fires here at finalize-complete —
		// immediately before close-step entry, which is the semantically
		// identical firing point. Closes v33 phantom-tree class via the
		// positive allow-list on per-hostname mounts.
		checks = append(checks, checkCanonicalOutputTreeOnly(dir)...)

		// C-6: no version anchors in published porter-facing content (P6).
		// Scans each per-codebase README / CLAUDE.md / environments/*/README.md
		// for `v\d+(\.\d+)*` tokens. Closes v33 version-log leakage class.
		checks = append(checks, checkNoVersionAnchorsInPublishedContent(dir)...)

		// C-10: surface-derived coupling + next-round prediction removed
		// (P1 supersedes). The per-check PreAttestCmd is the runnable
		// form; authors re-evaluate by running the shim.
		allPassed := checksAllPassed(checks)
		summary := "finalize checks passed"
		if !allPassed {
			summary = "finalize checks failed"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
	}
}

// checkFileExists returns a pass/fail check for file existence.
func checkFileExists(baseDir, relPath string) []workflow.StepCheck {
	fullPath := filepath.Join(baseDir, relPath)
	if _, err := os.Stat(fullPath); err != nil {
		return []workflow.StepCheck{{
			Name:   "file_" + strings.ReplaceAll(relPath, "/", "_"),
			Status: statusFail,
			Detail: fmt.Sprintf("file not found: %s", relPath),
		}}
	}
	return []workflow.StepCheck{{
		Name:   "file_" + strings.ReplaceAll(relPath, "/", "_"),
		Status: statusPass,
	}}
}

// checkAppREADMENoScaffoldTODOs fails if the deliverable appdev/README.md
// still contains the generate-finalize scaffold's TODO markers. The agent
// should write the real README to the SSHFS mount during the generate step;
// generate-finalize overlays it automatically. If this check fires, either
// the agent didn't write the README on the mount OR the overlay failed
// validation — tell them the exact fix.
func checkAppREADMENoScaffoldTODOs(outputDir string) []workflow.StepCheck {
	readmePath := filepath.Join(outputDir, "appdev", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		// No app README to check — not a failure at this layer (the
		// file_appdev_README.md existence check above catches missing files).
		return nil
	}
	content := string(data)
	if strings.Contains(content, "TODO: paste the full zerops.yaml content here") ||
		strings.Contains(content, "**TODO** \u2014 add framework-specific gotchas") {
		return []workflow.StepCheck{{
			Name:   "appdev_readme_no_todo_scaffold",
			Status: statusFail,
			Detail: "appdev/README.md still contains scaffold TODO markers — write the real README at /var/www/{appHostname}dev/README.md during the generate step with all 3 extract fragments filled in, then re-run generate-finalize to overlay it into the deliverable.",
		}}
	}
	return []workflow.StepCheck{{
		Name:   "appdev_readme_no_todo_scaffold",
		Status: statusPass,
	}}
}

// importYAMLDoc is a minimal struct for validating import.yaml structure.
type importYAMLDoc struct {
	Project struct {
		Name         string            `yaml:"name"`
		CorePackage  string            `yaml:"corePackage,omitempty"`
		EnvVariables map[string]string `yaml:"envVariables,omitempty"`
	} `yaml:"project"`
	Services []importService `yaml:"services"`
}

type importService struct {
	Hostname            string                   `yaml:"hostname"`
	Type                string                   `yaml:"type"`
	Priority            *int                     `yaml:"priority,omitempty"`
	Mode                string                   `yaml:"mode,omitempty"`
	EnableSubdomain     *bool                    `yaml:"enableSubdomainAccess,omitempty"`
	MinContainers       *int                     `yaml:"minContainers,omitempty"`
	ZeropsSetup         string                   `yaml:"zeropsSetup,omitempty"`
	BuildFromGit        string                   `yaml:"buildFromGit,omitempty"`
	StartWithoutCode    *bool                    `yaml:"startWithoutCode,omitempty"`
	MaxContainers       *int                     `yaml:"maxContainers,omitempty"`
	VerticalAutoscaling *importVerticalAutoscale `yaml:"verticalAutoscaling,omitempty"`
}

type importVerticalAutoscale struct {
	MinFreeRAMGB *float64 `yaml:"minFreeRamGB,omitempty"` //nolint:tagliatelle // Zerops API field name
	CPUMode      string   `yaml:"cpuMode,omitempty"`
}

// validateImportYAML runs structural checks on an import.yaml file.
func validateImportYAML(ctx context.Context, content string, plan *workflow.RecipePlan, envIndex int, folder string) []workflow.StepCheck {
	prefix := folder + "_import"
	var checks []workflow.StepCheck

	// Duplicate YAML key check — raw text scan BEFORE Go map parse, which
	// silently deduplicates (or errors, depending on yaml.v3 strictness).
	if dupes := findDuplicateYAMLKeys(content); len(dupes) > 0 {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_duplicate_keys", Status: statusFail,
			Detail: fmt.Sprintf("duplicate YAML keys: %s", strings.Join(dupes, ", ")),
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_duplicate_keys", Status: statusPass,
		})
	}

	// Parse YAML.
	var doc importYAMLDoc
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return append(checks, workflow.StepCheck{
			Name: prefix + "_valid_yaml", Status: statusFail,
			Detail: fmt.Sprintf("invalid YAML: %v", err),
		})
	}
	checks = append(checks, workflow.StepCheck{
		Name: prefix + "_valid_yaml", Status: statusPass,
	})

	// Project name convention: {slug}-{suffix}.
	if doc.Project.Name == "" {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_project_name", Status: statusFail,
			Detail: "project name is empty",
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_project_name", Status: statusPass,
			Detail: doc.Project.Name,
		})
	}

	svcMap := make(map[string]importService, len(doc.Services))
	for _, svc := range doc.Services {
		svcMap[svc.Hostname] = svc
	}

	checks = append(checks, checkServiceStructure(doc, svcMap, plan, envIndex, prefix)...)

	// Recipe deliverables must NOT have startWithoutCode (workspace-only).
	for _, svc := range doc.Services {
		if svc.StartWithoutCode != nil && *svc.StartWithoutCode {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_" + svc.Hostname + "_no_start_without_code", Status: statusFail,
				Detail: fmt.Sprintf("service %q has startWithoutCode — recipe deliverables must use buildFromGit only (startWithoutCode is workspace-only)", svc.Hostname),
			})
		}
	}

	// Env 5: HA checks.
	if envIndex == 5 {
		checks = append(checks, checkEnv5Requirements(doc, plan, prefix)...)
	}

	// Env 4: minContainers check.
	if envIndex == 4 {
		checks = append(checks, checkEnv4Requirements(doc, plan, prefix)...)
	}

	// No placeholders.
	if matches := placeholderRe.FindAllString(content, -1); len(matches) > 0 {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_no_placeholders", Status: statusFail,
			Detail: fmt.Sprintf("found placeholders: %s", strings.Join(uniqueStrings(matches), ", ")),
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_no_placeholders", Status: statusPass,
		})
	}

	// Comment ratio.
	ratio := commentRatio(content)
	if ratio >= 0.3 {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_comment_ratio", Status: statusPass,
			Detail: fmt.Sprintf("%.0f%%", ratio*100),
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name:   prefix + "_comment_ratio",
			Status: statusFail,
			Detail: fmt.Sprintf(
				"comment ratio %.0f%% in %s/import.yaml is below 30%% minimum. Add `#` comment lines above each service block explaining WHY this env's tier shape was chosen (mode, minContainers, corePackage). Each env file is published standalone — describe THIS tier on its own terms without naming siblings.",
				ratio*100, folder,
			),
		})
	}

	// Comment line width.
	checks = append(checks, checkCommentWidth(content, prefix)...)

	// Comment depth rubric: grade comments on whether they explain
	// WHY a decision was made, not just WHAT the field does. v7
	// gold-standard comments carried production wisdom; v16 regressed
	// to field narration. The rubric requires >= 35% of substantive
	// comments to contain a reasoning marker (because, otherwise,
	// without, rotation, rolling deploy, etc).
	checks = append(checks, checkCommentDepth(ctx, content, prefix)...)

	// Factual-claim linter: declarative numeric claims in comments
	// ("10 GB quota", "minContainers 3") must match the adjacent YAML
	// value in the same service block. Subjunctive phrasing ("bump to
	// N GB when usage grows") is allowed. See checkFactualClaims.
	checks = append(checks, checkFactualClaims(ctx, content, prefix)...)

	// Section-heading comment patterns — labels, not explanations.
	checks = append(checks, checkSectionHeadingComments(content, prefix)...)

	// Cross-env references — each env's import.yaml is published as a
	// standalone deploy target; siblings are invisible to the reader.
	checks = append(checks, checkCrossEnvReferences(content, prefix)...)

	// Shared secret check — should be project-level envVariables, not per-service envSecrets.
	if plan.Research.NeedsAppSecret {
		if len(doc.Project.EnvVariables) > 0 {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_shared_secret", Status: statusPass,
				Detail: "shared secret in project.envVariables",
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_shared_secret", Status: statusFail,
				Detail: "needsAppSecret=true but no project.envVariables found — shared secrets belong at project level, not per-service envSecrets",
			})
		}
	}

	// Preprocessor check — fires on ANY use of <@...> syntax, regardless of
	// whether the plan has a framework secret. v16's nestjs-showcase hit this:
	// the agent added JWT_SECRET with <@generateRandomString(<32>)> at project
	// level even though NestJS has no framework secret (NeedsAppSecret=false),
	// and the check was silently skipped because it was nested inside the
	// NeedsAppSecret branch. Without the directive the Zerops import API may
	// leave the literal "<@generateRandomString(<32>)>" string in the env var
	// instead of generating a random value.
	if strings.Contains(content, "<@") {
		if strings.Contains(content, "zeropsPreprocessor=on") {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_preprocessor", Status: statusPass,
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_preprocessor", Status: statusFail,
				Detail: "#zeropsPreprocessor=on required at the top of import.yaml when using <@...> preprocessor functions (generateRandomString, etc). Without the directive the literal <@...> string is imported verbatim instead of being expanded.",
			})
		}
	}

	return checks
}

// checkServiceStructure validates data service priority, zeropsSetup+buildFromGit
// on runtime/utility services, and dev/stage hostname pairs in env 0-1.
func checkServiceStructure(doc importYAMLDoc, svcMap map[string]importService, plan *workflow.RecipePlan, envIndex int, prefix string) []workflow.StepCheck {
	var checks []workflow.StepCheck

	// Non-runtime services (managed + utility) need priority: 10 so they
	// start before the app container.
	for _, target := range plan.Targets {
		if workflow.IsRuntimeType(target.Type) {
			continue
		}
		svc, exists := svcMap[target.Hostname]
		if exists && (svc.Priority == nil || *svc.Priority != 10) {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_" + target.Hostname + "_priority", Status: statusFail,
				Detail: fmt.Sprintf("service %q should have priority: 10", target.Hostname),
			})
		} else if exists {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_" + target.Hostname + "_priority", Status: statusPass,
			})
		}
	}

	// Runtime and utility services MUST have zeropsSetup+buildFromGit.
	// Managed services (db/cache/search/storage/messaging) must NOT — they
	// are platform-owned; buildFromGit is inert and would add noise.
	for _, svc := range doc.Services {
		svcType := findTargetType(plan, svc.Hostname)
		if svcType == "" {
			continue // service not in plan (agent-added extras aren't checked)
		}
		needsGitCheck := workflow.IsRuntimeType(svcType) || workflow.IsUtilityType(svcType)
		if !needsGitCheck {
			continue
		}
		hasSetup := svc.ZeropsSetup != ""
		hasGit := svc.BuildFromGit != ""
		checkName := prefix + "_" + svc.Hostname + "_setup_git"

		switch {
		case hasSetup && hasGit:
			checks = append(checks, workflow.StepCheck{Name: checkName, Status: statusPass})
		case !hasSetup && !hasGit:
			checks = append(checks, workflow.StepCheck{
				Name: checkName, Status: statusFail,
				Detail: fmt.Sprintf("service %q is missing zeropsSetup and buildFromGit — recipe deliverables require both (do NOT rewrite auto-generated files from scratch)", svc.Hostname),
			})
		case hasSetup && !hasGit:
			checks = append(checks, workflow.StepCheck{
				Name: checkName, Status: statusFail,
				Detail: fmt.Sprintf("service %q has zeropsSetup without buildFromGit (API requires both)", svc.Hostname),
			})
		default:
			checks = append(checks, workflow.StepCheck{
				Name: checkName, Status: statusFail,
				Detail: fmt.Sprintf("service %q has buildFromGit without zeropsSetup (must specify which setup to build)", svc.Hostname),
			})
		}
	}

	// Env 0-1: runtime services must use dev/stage hostname pairs.
	// Exception: shared-codebase workers get only {hostname}stage — no
	// {hostname}dev because appdev runs both processes via SSH.
	if envIndex <= 1 {
		for _, target := range plan.Targets {
			if !workflow.IsRuntimeType(target.Type) {
				continue
			}
			sharedWorker := workflow.SharesAppCodebase(target)
			stageHost := target.Hostname + "stage"
			if _, ok := svcMap[stageHost]; !ok {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + stageHost + "_exists", Status: statusFail,
					Detail: fmt.Sprintf("env 0-1 should have %q (stage service) — do NOT use bare hostname %q", stageHost, target.Hostname),
				})
			}
			if sharedWorker {
				// Shared-codebase worker: no dev service — appdev is the workspace.
				devHost := target.Hostname + "dev"
				if _, ok := svcMap[devHost]; ok {
					checks = append(checks, workflow.StepCheck{
						Name: prefix + "_" + devHost + "_absent", Status: statusFail,
						Detail: fmt.Sprintf("shared-codebase worker must NOT have %q — appdev runs both processes via SSH", devHost),
					})
				}
				continue
			}
			devHost := target.Hostname + "dev"
			if _, ok := svcMap[devHost]; !ok {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + devHost + "_exists", Status: statusFail,
					Detail: fmt.Sprintf("env 0-1 should have %q (dev service) — do NOT use bare hostname %q", devHost, target.Hostname),
				})
			}
		}
	}

	return checks
}

// checkEnv5Requirements validates HA production requirements.
func checkEnv5Requirements(doc importYAMLDoc, plan *workflow.RecipePlan, prefix string) []workflow.StepCheck {
	var checks []workflow.StepCheck

	// corePackage at project level.
	if doc.Project.CorePackage != "SERIOUS" {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_core_package", Status: statusFail,
			Detail: "env 5 project should have corePackage: SERIOUS",
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_core_package", Status: statusPass,
		})
	}

	for _, svc := range doc.Services {
		svcType := findTargetType(plan, svc.Hostname)
		if svcType == "" {
			continue
		}

		// HA mode on services that support mode (managed, excluding object-storage).
		if workflow.ServiceSupportsMode(svcType) && svc.Mode != "HA" {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_" + svc.Hostname + "_ha_mode", Status: statusFail,
				Detail: fmt.Sprintf("env 5 service %q should have mode: HA", svc.Hostname),
			})
		}

		// DEDICATED cpuMode on runtime services (excludes utility, which uses
		// shared CPU — mailpit's workload is tiny and doesn't justify DEDICATED).
		if svc.VerticalAutoscaling != nil && workflow.IsRuntimeType(svcType) {
			if svc.VerticalAutoscaling.CPUMode != "DEDICATED" {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + svc.Hostname + "_cpu_mode", Status: statusFail,
					Detail: fmt.Sprintf("env 5 service %q should have cpuMode: DEDICATED", svc.Hostname),
				})
			}
		}
	}

	return checks
}

// checkEnv4Requirements validates small production requirements.
func checkEnv4Requirements(doc importYAMLDoc, plan *workflow.RecipePlan, prefix string) []workflow.StepCheck {
	var checks []workflow.StepCheck

	for _, svc := range doc.Services {
		svcType := findTargetType(plan, svc.Hostname)
		if svcType == "" {
			continue
		}
		if workflow.IsRuntimeType(svcType) {
			if svc.MinContainers == nil || *svc.MinContainers < 2 {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + svc.Hostname + "_min_containers", Status: statusFail,
					Detail: fmt.Sprintf("env 4 app service %q should have minContainers: 2", svc.Hostname),
				})
			} else {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + svc.Hostname + "_min_containers", Status: statusPass,
				})
			}
		}
	}

	return checks
}

// checkCommentWidth validates that comment lines are <= 80 chars.
func checkCommentWidth(content, prefix string) []workflow.StepCheck {
	lines := strings.Split(content, "\n")
	var longLines []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && len(trimmed) > 80 {
			longLines = append(longLines, fmt.Sprintf("line %d (%d chars)", i+1, len(trimmed)))
		}
	}
	if len(longLines) > 0 {
		detail := strings.Join(longLines, ", ")
		if len(longLines) > 3 {
			detail = strings.Join(longLines[:3], ", ") + fmt.Sprintf(" and %d more", len(longLines)-3)
		}
		return []workflow.StepCheck{{
			Name: prefix + "_comment_width", Status: statusFail,
			Detail: "comment lines exceed 80 chars: " + detail,
		}}
	}
	return []workflow.StepCheck{{
		Name: prefix + "_comment_width", Status: statusPass,
	}}
}

// checkSectionHeadingComments detects decorator-style comment headings
// like "# -- Dev Runtime --", "# === Database ===" or pure separator
// lines like "# ---------------------------------------------------".
// These label sections rather than explain decisions — YAML structure
// provides grouping, comments should explain WHY.
func checkSectionHeadingComments(content, prefix string) []workflow.StepCheck {
	var headings []string
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		// Pure separator: only dashes, equals, or underscores.
		stripped := strings.NewReplacer("-", "", "=", "", "_", "", " ", "").Replace(body)
		if len(body) > 3 && stripped == "" {
			headings = append(headings, fmt.Sprintf("line %d", i+1))
			continue
		}
		// Heading pattern: "-- Text --", "== Text ==", etc.
		if (strings.HasPrefix(body, "-- ") && strings.HasSuffix(body, " --")) ||
			(strings.HasPrefix(body, "== ") && strings.HasSuffix(body, " ==")) {
			headings = append(headings, fmt.Sprintf("line %d", i+1))
		}
	}
	if len(headings) > 0 {
		detail := strings.Join(headings, ", ")
		if len(headings) > 3 {
			detail = strings.Join(headings[:3], ", ") + fmt.Sprintf(" and %d more", len(headings)-3)
		}
		return []workflow.StepCheck{{
			Name: prefix + "_comment_headings", Status: statusFail,
			Detail: "section-heading comments found: " + detail + " — use explanatory comments, not labels",
		}}
	}
	return []workflow.StepCheck{{
		Name: prefix + "_comment_headings", Status: statusPass,
	}}
}

// crossEnvRefPattern matches phrases that name a sibling environment by its
// tier number ("env 0", "env 5", "envs 4-5", "env4", "environment 3"). Each
// recipe deliverable is published standalone on zerops.io/recipes — users see
// one env's files, never the siblings — so such references are context-free
// at the point of reading. The pattern is intentionally scoped to the
// "env/environment + number" spelling; legitimate prose like "environment
// variables" or "production" is not flagged.
var crossEnvRefPattern = regexp.MustCompile(`\b[Ee]nv(ironment)?s?\s*[0-9]`)

// checkCrossEnvReferences scans comment lines for cross-env references.
func checkCrossEnvReferences(content, prefix string) []workflow.StepCheck {
	var offenders []string
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		if crossEnvRefPattern.MatchString(trimmed) {
			offenders = append(offenders, fmt.Sprintf("line %d", i+1))
		}
	}
	if len(offenders) > 0 {
		detail := strings.Join(offenders, ", ")
		if len(offenders) > 3 {
			detail = strings.Join(offenders[:3], ", ") + fmt.Sprintf(" and %d more", len(offenders)-3)
		}
		envFolder := strings.TrimSuffix(prefix, "_import")
		return []workflow.StepCheck{{
			Name:   prefix + "_cross_env_refs",
			Status: statusFail,
			Detail: fmt.Sprintf(
				"comment in %s/import.yaml references a sibling environment by tier number at %s — each env's import.yaml is published standalone on zerops.io/recipes; readers never see the other envs, so references like 'env 0', 'env 4', 'see env 5' are context-free. Rewrite the flagged comment lines so each describes THIS env on its own terms — drop phrases like 'see env N', 'envs 4-5', 'bumps to env 4'.",
				envFolder, detail,
			),
		}}
	}
	return []workflow.StepCheck{{
		Name: prefix + "_cross_env_refs", Status: statusPass,
	}}
}

// findDuplicateYAMLKeys scans raw YAML for repeated keys within the same
// mapping scope. Go's yaml.Unmarshal silently deduplicates into maps,
// hiding the problem. This function catches it before parsing.
//
// Scope tracking: each list item (`- `) at a given indent starts a new
// mapping scope for its children. Keys like `type` or `hostname` across
// different list items are NOT duplicates.
func findDuplicateYAMLKeys(content string) []string {
	// listScope tracks the current list-item sequence number at each indent
	// level where a `- ` was seen. Keys below a list item inherit the
	// parent's scope number, so `type:` at indent 4 under different
	// `- hostname:` entries at indent 2 are separate.
	listScope := make(map[int]int)

	type scopeKey struct {
		parentScope int
		indent      int
		key         string
	}

	seen := make(map[scopeKey]int)
	dupeSet := make(map[string]bool)
	var dupes []string

	parentScopeFor := func(indent int) int {
		// Find the nearest list-item scope at a strictly lower indent.
		best := -1
		bestScope := 0
		for lvl, sc := range listScope {
			if lvl < indent && lvl > best {
				best = lvl
				bestScope = sc
			}
		}
		return bestScope
	}

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// List item: bump scope at this indent, clear deeper scopes.
		if strings.HasPrefix(trimmed, "- ") {
			listScope[indent]++
			for k := range listScope {
				if k > indent {
					delete(listScope, k)
				}
			}
			// Extract key from "- key: value" pattern.
			rest := trimmed[2:]
			colonIdx := strings.IndexByte(rest, ':')
			if colonIdx <= 0 {
				continue
			}
			key := rest[:colonIdx]
			if strings.ContainsAny(key, " \t") {
				continue
			}
			sk := scopeKey{parentScope: listScope[indent], indent: indent, key: key}
			seen[sk]++
			if seen[sk] == 2 && !dupeSet[key] {
				dupeSet[key] = true
				dupes = append(dupes, key)
			}
			continue
		}

		colonIdx := strings.IndexByte(trimmed, ':')
		if colonIdx <= 0 {
			continue
		}
		key := trimmed[:colonIdx]
		if strings.ContainsAny(key, " \t") {
			continue
		}

		sk := scopeKey{parentScope: parentScopeFor(indent), indent: indent, key: key}
		seen[sk]++
		if seen[sk] == 2 && !dupeSet[key] {
			dupeSet[key] = true
			dupes = append(dupes, key)
		}
	}
	return dupes
}

// findTargetType finds the service type for a hostname in the recipe plan.
// Handles env 0-1 suffixed hostnames (appdev/appstage → app target).
func findTargetType(plan *workflow.RecipePlan, hostname string) string {
	if t := findTarget(plan, hostname); t != nil {
		return t.Type
	}
	return ""
}

// findTarget finds a target by hostname, stripping dev/stage suffixes for env 0-1.
func findTarget(plan *workflow.RecipePlan, hostname string) *workflow.RecipeTarget {
	for i := range plan.Targets {
		if plan.Targets[i].Hostname == hostname {
			return &plan.Targets[i]
		}
	}
	for _, suffix := range []string{"dev", "stage"} {
		base := strings.TrimSuffix(hostname, suffix)
		if base != hostname {
			for i := range plan.Targets {
				if plan.Targets[i].Hostname == base {
					return &plan.Targets[i]
				}
			}
		}
	}
	return nil
}
