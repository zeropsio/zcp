package workflow

import (
	"fmt"
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

	writeEnvHeader(&b, plan, envIndex)
	writeProjectSection(&b, plan, envIndex, envComments.Project)

	b.WriteString("\nservices:\n")

	for _, target := range plan.Targets {
		// Runtime services in env 0-1 get a dev+stage pair; everything else
		// (managed, utility, and runtime in env 2-5) gets a single entry.
		if IsRuntimeType(target.Type) && envIndex <= 1 {
			writeDevService(&b, plan, target, envComments.Service)
			writeStageService(&b, plan, target, envComments.Service)
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
// project comment (if any) emitted above it.
func writeProjectSection(b *strings.Builder, plan *RecipePlan, envIndex int, projectComment string) {
	projectName := fmt.Sprintf("%s-%s", plan.Slug, envTiers[envIndex].Suffix)

	if plan.Research.NeedsAppSecret {
		b.WriteString("#zeropsPreprocessor=on\n\n")
	}

	writeAgentCommentAtIndent(b, projectComment, "")

	b.WriteString("project:\n")
	fmt.Fprintf(b, "  name: %s\n", projectName)

	if envIndex == 5 {
		b.WriteString("  corePackage: SERIOUS\n")
	}

	if plan.Research.NeedsAppSecret && plan.Research.AppSecretKey != "" {
		b.WriteString("  envVariables:\n")
		fmt.Fprintf(b, "    %s: <@generateRandomString(<32>)>\n", plan.Research.AppSecretKey)
	}
}

// writeDevService writes a dev service block for env 0-1. Called only for
// runtime targets, so target.Type is guaranteed IsRuntimeType. Reads the
// agent's comment keyed by the actual service hostname ("{base}dev").
func writeDevService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, serviceComments map[string]string) {
	devHost := target.Hostname + "dev"
	writeAgentCommentAtIndent(b, serviceComments[devHost], "  ")

	fmt.Fprintf(b, "  - hostname: %s\n", devHost)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: dev\n")
	writeRecipeAppBuildFromGit(b, plan)
	// Non-worker runtimes serve HTTP and need a subdomain.
	if !target.IsWorker {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	writeAutoscaling(b, target, 0) // env 0-1 share same scaling
	b.WriteByte('\n')
}

// writeStageService writes a stage service block for env 0-1. Called only for
// runtime targets, so target.Type is guaranteed IsRuntimeType. Reads the
// agent's comment keyed by the actual service hostname ("{base}stage").
func writeStageService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, serviceComments map[string]string) {
	stageHost := target.Hostname + "stage"
	writeAgentCommentAtIndent(b, serviceComments[stageHost], "  ")

	fmt.Fprintf(b, "  - hostname: %s\n", stageHost)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	fmt.Fprintf(b, "    zeropsSetup: %s\n", recipeSetupName(target.IsWorker, false))
	writeRecipeAppBuildFromGit(b, plan)
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

	// Priority: non-runtime services start before app.
	if !IsRuntimeType(target.Type) {
		b.WriteString("    priority: 10\n")
	}

	// Mode: only managed services that support it.
	if ServiceSupportsMode(target.Type) {
		if envIndex == 5 {
			b.WriteString("    mode: HA\n")
		} else {
			b.WriteString("    mode: NON_HA\n")
		}
	}

	// Recipe runtime services: zeropsSetup + buildFromGit from recipe app repo.
	if IsRuntimeType(target.Type) {
		fmt.Fprintf(b, "    zeropsSetup: %s\n", recipeSetupName(target.IsWorker, false))
		writeRecipeAppBuildFromGit(b, plan)
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

// writeRecipeAppBuildFromGit writes the buildFromGit URL for recipe app services.
func writeRecipeAppBuildFromGit(b *strings.Builder, plan *RecipePlan) {
	fmt.Fprintf(b, "    buildFromGit: %s%s-app\n", RecipeAppRepoBase, plan.Slug)
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
