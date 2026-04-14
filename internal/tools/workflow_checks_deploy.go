package tools

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkDevProdEnvDivergence flags dev and prod setups whose run.envVariables
// maps are bit-identical. Two setups named differently exist to be different —
// the agent writes them precisely to carry different values for the framework
// to observe (mode flags, debug toggles, log levels, feature toggles). When
// the maps are literally equal, it is a copy-paste: the dev container will
// behave exactly like prod, hiding stack traces and enabling caches while
// developers iterate.
//
// This is a structural invariant — no knowledge of which env var keys carry
// mode signals for which framework is required. If an agent intentionally
// wants two setups with identical env vars, they can distinguish them with a
// single semantically-meaningful key (e.g. the framework's own env flag).
func checkDevProdEnvDivergence(doc *ops.ZeropsYmlDoc) []workflow.StepCheck {
	devEntry := doc.FindEntry(workflow.RecipeSetupDev)
	prodEntry := doc.FindEntry(workflow.RecipeSetupProd)
	if devEntry == nil || prodEntry == nil {
		// Only fires when both setups coexist in zerops.yaml.
		return nil
	}
	devEnv := devEntry.Run.EnvVariables
	prodEnv := prodEntry.Run.EnvVariables
	// If either side has no run.envVariables block, there is nothing to
	// compare — the framework's own defaults, OS env vars, or envSecrets
	// carry the mode signal.
	if len(devEnv) == 0 || len(prodEnv) == 0 {
		return nil
	}

	if !maps.Equal(devEnv, prodEnv) {
		return []workflow.StepCheck{{
			Name: "dev_prod_env_divergence", Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "dev_prod_env_divergence",
		Status: statusFail,
		Detail: "dev and prod setups in zerops.yaml have bit-identical run.envVariables — the dev container will behave exactly like prod (caches enabled, stack traces hidden). Differentiate the two setups using whichever env var your framework reads for its run mode",
	}}
}

// findAndParseZeropsYml locates and parses zerops.yaml from project root or mount paths.
// Returns the parsed doc and the directory where zerops.yaml was found.
// hostname is used to check the mount path (container env: /projectRoot/{hostname}/zerops.yaml).
func findAndParseZeropsYml(projectRoot, hostname string) (*ops.ZeropsYmlDoc, string, error) {
	// Try mount path for target hostname (container environment).
	if hostname != "" {
		mountPath := filepath.Join(projectRoot, hostname)
		if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
			if doc, err := ops.ParseZeropsYml(mountPath); err == nil {
				return doc, mountPath, nil
			}
		}
	}
	// Fall back to project root (local environment).
	doc, err := ops.ParseZeropsYml(projectRoot)
	return doc, projectRoot, err
}

// validateDeployFiles checks that cherry-picked deployFiles paths exist on the filesystem.
// Skipped when deployFiles is [.] (deploys everything).
func validateDeployFiles(projectRoot, hostname string, entry *ops.ZeropsYmlEntry) []workflow.StepCheck {
	if !entry.HasDeployFiles() {
		return nil
	}
	deployFiles := entry.DeployFilesList()
	// Skip if deploying everything.
	if slices.Contains(deployFiles, ".") || slices.Contains(deployFiles, "./") {
		return nil
	}

	var missing []string
	for _, df := range deployFiles {
		p := filepath.Join(projectRoot, df)
		if _, err := os.Stat(p); err != nil {
			missing = append(missing, df)
		}
	}
	if len(missing) > 0 {
		return []workflow.StepCheck{{
			Name:   hostname + "_deploy_files",
			Status: statusFail,
			Detail: fmt.Sprintf("deployFiles paths not found: %s — these will be missing from the deploy artifact", strings.Join(missing, ", ")),
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_deploy_files",
		Status: statusPass,
	}}
}
