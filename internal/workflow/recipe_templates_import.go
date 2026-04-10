package workflow

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// GenerateEnvImportYAML returns the import.yaml for a specific environment tier.
// Emits structural YAML (hostnames, types, zeropsSetup, scaling fields). All
// prose commentary comes from plan.EnvComments[envKey] — the agent writes
// tailored comments per env, the template serializes them without adding
// platform-knowledge comments of its own.
func GenerateEnvImportYAML(plan *RecipePlan, envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}
	envKey := strconv.Itoa(envIndex)
	envComments := envCommentsFor(plan, envKey)

	var b strings.Builder

	// Preprocessor directive MUST be the very first line — Zerops parser
	// rejects it anywhere else, causing catastrophic import failure.
	if plan.Research.NeedsAppSecret {
		b.WriteString("#zeropsPreprocessor=on\n\n")
	}

	writeEnvHeader(&b, plan, envIndex)
	writeProjectSection(&b, plan, envIndex, envComments.Project)

	b.WriteString("\nservices:\n")

	for _, target := range plan.Targets {
		// Runtime services in env 0-1 get a dev+stage pair — EXCEPT shared-
		// codebase workers (SharesCodebaseWith set), which get stage only.
		// The host target's dev container runs both the web server and
		// worker as separate SSH processes from one mount — a separate
		// workerdev would be a zombie container running the same code with
		// no worker process started. Separate-codebase workers (empty
		// SharesCodebaseWith, which is the DEFAULT) get their own dev+stage
		// regardless of whether the base runtime happens to match.
		if IsRuntimeType(target.Type) && envIndex <= 1 {
			if SharesAppCodebase(target) {
				// Shared codebase: stage only (host target's dev runs both processes).
				writeStageService(&b, plan, target, envComments.Service)
			} else {
				writeDevService(&b, plan, target, envComments.Service)
				writeStageService(&b, plan, target, envComments.Service)
			}
		} else {
			writeSingleService(&b, plan, target, envIndex, envComments.Service)
		}
	}

	return b.String()
}

// envCommentsFor returns the EnvComments for an env key, with nil-safe defaults.
func envCommentsFor(plan *RecipePlan, envKey string) EnvComments {
	if plan == nil || plan.EnvComments == nil {
		return EnvComments{}
	}
	return plan.EnvComments[envKey]
}

// writeEnvHeader writes the file-level comment block describing the tier purpose.
func writeEnvHeader(b *strings.Builder, plan *RecipePlan, envIndex int) {
	desc := envDescription(plan, envIndex)
	full := envTiers[envIndex].IntroLabel + " " + desc
	for _, line := range wrapText(full, 78) {
		fmt.Fprintf(b, "# %s\n", line)
	}
	b.WriteByte('\n')
}

// writeProjectSection writes the project: block with the agent-authored
// project comment (if any) emitted above it. The project-level envVariables
// block merges, in this order:
//
//  1. The shared-secret line, if plan.Research.NeedsAppSecret is set
//     (always first — the secret is the most-referenced project-wide value).
//  2. The per-env projectEnvVariables map (if any), emitted in sorted key
//     order so diffs are stable across reruns.
//
// If both are absent, no envVariables: line is emitted at all (no empty block).
func writeProjectSection(b *strings.Builder, plan *RecipePlan, envIndex int, projectComment string) {
	projectName := fmt.Sprintf("%s-%s", plan.Slug, envTiers[envIndex].Suffix)

	writeAgentCommentAtIndent(b, projectComment, "")

	b.WriteString("project:\n")
	fmt.Fprintf(b, "  name: %s\n", projectName)

	if envIndex == 5 {
		b.WriteString("  corePackage: SERIOUS\n")
	}

	hasSecret := plan.Research.NeedsAppSecret && plan.Research.AppSecretKey != ""
	envVars := projectEnvVariablesFor(plan, envIndex)

	if !hasSecret && len(envVars) == 0 {
		return
	}

	b.WriteString("  envVariables:\n")
	if hasSecret {
		fmt.Fprintf(b, "    %s: <@generateRandomString(<32>)>\n", plan.Research.AppSecretKey)
	}

	// Deterministic order — sort by name for diff stability. Values emitted
	// verbatim so ${zeropsSubdomainHost} interpolation markers reach the
	// generated file unchanged.
	names := make([]string, 0, len(envVars))
	for name := range envVars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(b, "    %s: %s\n", name, envVars[name])
	}
}

// projectEnvVariablesFor returns the agent-supplied per-env project env vars
// for an env index, with nil-safe defaults.
func projectEnvVariablesFor(plan *RecipePlan, envIndex int) map[string]string {
	if plan == nil || plan.ProjectEnvVariables == nil {
		return nil
	}
	return plan.ProjectEnvVariables[strconv.Itoa(envIndex)]
}

// writeDevService writes a dev service block for env 0-1. Called only for
// runtime targets, so target.Type is guaranteed IsRuntimeType. Reads the
// agent's comment keyed by the actual service hostname ("{base}dev").
// Falls back to a computed default if the agent didn't provide a comment.
func writeDevService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, serviceComments map[string]string) {
	devHost := target.Hostname + "dev"
	comment := serviceComments[devHost]
	if comment == "" {
		comment = defaultDevComment(target)
	}
	writeAgentCommentAtIndent(b, comment, "  ")

	fmt.Fprintf(b, "  - hostname: %s\n", devHost)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	// Priority: API services start before frontends.
	if target.Role == RecipeRoleAPI {
		b.WriteString("    priority: 5\n")
	}
	b.WriteString("    zeropsSetup: dev\n")
	writeRuntimeBuildFromGit(b, plan, target)
	// Non-worker runtimes serve HTTP and need a subdomain.
	if !target.IsWorker {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	writeDevAutoscaling(b, target)
	b.WriteByte('\n')
}

// writeStageService writes a stage service block for env 0-1. Called only for
// runtime targets, so target.Type is guaranteed IsRuntimeType. Reads the
// agent's comment keyed by the actual service hostname ("{base}stage").
// Falls back to a computed default if the agent didn't provide a comment.
func writeStageService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, serviceComments map[string]string) {
	stageHost := target.Hostname + "stage"
	comment := serviceComments[stageHost]
	if comment == "" {
		comment = defaultStageComment(target, plan)
	}
	writeAgentCommentAtIndent(b, comment, "  ")

	fmt.Fprintf(b, "  - hostname: %s\n", stageHost)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	// Priority: API services start before frontends.
	if target.Role == RecipeRoleAPI {
		b.WriteString("    priority: 5\n")
	}
	fmt.Fprintf(b, "    zeropsSetup: %s\n", recipeSetupName(target, false, plan))
	writeRuntimeBuildFromGit(b, plan, target)
	if !target.IsWorker {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	writeAutoscaling(b, target, 0)
	b.WriteByte('\n')
}

// writeSingleService writes a service entry for env 2-5 (and non-runtime
// services in env 0-1). Reads the agent's comment keyed by base hostname —
// there's only one entry per service in these files.
func writeSingleService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int, serviceComments map[string]string) {
	writeAgentCommentAtIndent(b, serviceComments[target.Hostname], "  ")

	fmt.Fprintf(b, "  - hostname: %s\n", target.Hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)

	// Priority: managed services start first (10), API services before frontends (5).
	if !IsRuntimeType(target.Type) {
		b.WriteString("    priority: 10\n")
	} else if target.Role == RecipeRoleAPI {
		b.WriteString("    priority: 5\n")
	}

	// Mode: only managed services that support it.
	if ServiceSupportsMode(target.Type) {
		if envIndex == 5 {
			b.WriteString("    mode: HA\n")
		} else {
			b.WriteString("    mode: NON_HA\n")
		}
	}

	// Recipe runtime services: zeropsSetup + buildFromGit.
	if IsRuntimeType(target.Type) {
		fmt.Fprintf(b, "    zeropsSetup: %s\n", recipeSetupName(target, false, plan))
		writeRuntimeBuildFromGit(b, plan, target)
	}

	// Utility services: zeropsSetup + buildFromGit from the utility repo.
	if IsUtilityType(target.Type) {
		b.WriteString("    zeropsSetup: app\n")
		fmt.Fprintf(b, "    buildFromGit: %s\n", utilityBuildFromGitURL(target.Type))
	}

	// Subdomain: runtime apps (non-workers) + utility services with web UI.
	if (IsRuntimeType(target.Type) && !target.IsWorker) || IsUtilityType(target.Type) {
		b.WriteString("    enableSubdomainAccess: true\n")
	}

	// minContainers: runtime services in production tiers.
	if IsRuntimeType(target.Type) && envIndex >= 4 {
		b.WriteString("    minContainers: 2\n")
	}

	// Object storage: size and policy instead of autoscaling.
	if IsObjectStorageType(target.Type) {
		b.WriteString("    objectStorageSize: 1\n")
		b.WriteString("    objectStoragePolicy: private\n")
	}

	// Vertical autoscaling: only services that support it.
	if ServiceSupportsAutoscaling(target.Type) {
		writeAutoscaling(b, target, envIndex)
	}

	b.WriteByte('\n')
}

// writeRuntimeBuildFromGit writes the buildFromGit URL for recipe runtime services.
// Repo suffix is decided per target:
//   - Shared-codebase worker (SharesCodebaseWith != "") → inherits the HOST
//     target's suffix by looking it up in the plan. Host is the target named
//     by SharesCodebaseWith; its own Role determines whether that suffix is
//     -api (dual-runtime backend) or -app (single-app or full-stack).
//   - Separate-codebase worker (IsWorker + empty SharesCodebaseWith) → -worker
//     (its own repo with its own zerops.yaml and dev+prod setups).
//   - Role="api" runtime → -api.
//   - Everything else (frontend, single-app, utility host) → -app.
//
// Validation (validateWorkerCodebaseRefs) guarantees that a non-empty
// SharesCodebaseWith resolves to a real non-worker runtime target with a
// matching base runtime, so findTarget never returns nil for a worker that
// passed validation.
func writeRuntimeBuildFromGit(b *strings.Builder, plan *RecipePlan, target RecipeTarget) {
	fmt.Fprintf(b, "    buildFromGit: %s%s%s\n", RecipeAppRepoBase, plan.Slug, runtimeRepoSuffix(plan, target))
}

// runtimeRepoSuffix returns the "-app"/"-api"/"-worker" repo suffix for a
// runtime target. Factored out of writeRuntimeBuildFromGit so unit tests can
// pin the decision table without re-parsing YAML.
func runtimeRepoSuffix(plan *RecipePlan, target RecipeTarget) string {
	switch {
	case target.IsWorker && target.SharesCodebaseWith == "":
		return "-worker"
	case target.IsWorker && target.SharesCodebaseWith != "":
		// Shared worker: inherit host target's suffix.
		host := findTarget(plan, target.SharesCodebaseWith)
		if host != nil && host.Role == RecipeRoleAPI {
			return "-api"
		}
		return "-app"
	case target.Role == RecipeRoleAPI:
		return "-api"
	default:
		return "-app"
	}
}

// writeDevAutoscaling writes the verticalAutoscaling block for a dev-slot
// runtime service in env 0-1. Dev containers host the agent's iteration
// loop: npm install / composer install / pip install on a showcase-scale
// dependency tree, plus a hot-reload process (nest --watch, bun --hot,
// php artisan serve) that keeps the toolchain hot. 0.25 GB OOMs npm
// install on any non-trivial package.json; 0.5 GB just barely survives
// small recipes and dies on dual-runtime showcases. 1 GB is the defensible
// floor for every runtime family we support. Stage slots keep the
// lighter default — stage runs the built artifact, not the toolchain.
//
// This is also the fix for "type: static with dev nodejs override"
// (dual-runtime frontend): the dev slot runs Node despite the prod
// run.base being static, so it needs the runtime-family memory profile.
// The fix works by predicate, not by special-casing the static type —
// any target that reaches writeDevService gets the dev-slot profile
// because writeDevService is only called for runtime targets (envIndex
// <= 1 && IsRuntimeType(target.Type) check in GenerateEnvImportYAML).
func writeDevAutoscaling(b *strings.Builder, target RecipeTarget) {
	_ = target // reserved for future per-target tuning (e.g. heavier runtimes)
	b.WriteString("    verticalAutoscaling:\n")
	b.WriteString("      minRam: 1\n")
}

// writeAutoscaling writes the verticalAutoscaling block per tier.
// Caller must ensure the service type supports autoscaling (callers check
// ServiceSupportsAutoscaling before invoking).
func writeAutoscaling(b *strings.Builder, target RecipeTarget, envIndex int) {
	isRT := IsRuntimeType(target.Type)   // genuine runtime (excludes utility)
	isUtil := IsUtilityType(target.Type) // mailpit and similar

	b.WriteString("    verticalAutoscaling:\n")

	switch {
	case envIndex <= 2:
		if isRT {
			b.WriteString("      minRam: 0.5\n")
		} else {
			b.WriteString("      minRam: 0.25\n")
		}

	case envIndex == 3:
		if isRT {
			b.WriteString("      minRam: 0.5\n")
		} else {
			b.WriteString("      minRam: 0.25\n")
		}
		b.WriteString("      minFreeRamGB: 0.25\n")

	case envIndex == 4:
		if isRT {
			b.WriteString("      minRam: 0.5\n")
		} else {
			b.WriteString("      minRam: 0.25\n")
		}
		b.WriteString("      minFreeRamGB: 0.25\n")

	case envIndex == 5:
		if isUtil {
			// Utilities (mailpit): lighter scaling, no DEDICATED.
			b.WriteString("      minRam: 0.25\n")
		} else {
			b.WriteString("      cpuMode: DEDICATED\n")
			if isRT {
				b.WriteString("      minRam: 0.5\n")
				b.WriteString("      minFreeRamGB: 0.25\n")
			} else {
				b.WriteString("      minRam: 1\n")
				b.WriteString("      minFreeRamGB: 0.5\n")
			}
		}
	}
}

// --- Default comment fallbacks ---

// defaultDevComment returns a computed comment for a dev service when the agent
// didn't provide one. Derived from service properties, not framework-specific.
func defaultDevComment(target RecipeTarget) string {
	host := target.Hostname + "dev"
	if target.IsWorker {
		return fmt.Sprintf("Dev workspace for %s — zeropsSetup:dev deploys the full source tree for SSH editing and manual process management.", host)
	}
	return fmt.Sprintf("Dev workspace — zeropsSetup:dev deploys the full source tree so %s is editable over SSHFS.", host)
}

// defaultStageComment returns a computed comment for a stage service when the
// agent didn't provide one. Derived from service properties, not framework-specific.
func defaultStageComment(target RecipeTarget, plan *RecipePlan) string {
	setup := recipeSetupName(target, false, plan)
	if target.IsWorker {
		return fmt.Sprintf("Stage worker — zeropsSetup:%s validates background job processing with production config.", setup)
	}
	return fmt.Sprintf("Staging slot — zeropsSetup:%s runs the production build for validation before release.", setup)
}

// --- Comment helpers ---

// writeAgentCommentAtIndent wraps free-form text into # comment lines at the
// given indent. Preserves explicit newlines in the input (for paragraph breaks).
// No-op when text is empty or whitespace-only.
func writeAgentCommentAtIndent(b *strings.Builder, text, indent string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	maxWidth := max(80-len(indent)-2, 20) // "# "
	for _, line := range wrapText(text, maxWidth) {
		if line == "" {
			fmt.Fprintf(b, "%s#\n", indent)
		} else {
			fmt.Fprintf(b, "%s# %s\n", indent, line)
		}
	}
}

// wrapText wraps text to the given width, breaking at word boundaries.
func wrapText(text string, width int) []string {
	var lines []string
	for paragraph := range strings.SplitSeq(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := words[0]
		for _, w := range words[1:] {
			if len(current)+1+len(w) > width {
				lines = append(lines, current)
				current = w
			} else {
				current += " " + w
			}
		}
		lines = append(lines, current)
	}
	return lines
}
