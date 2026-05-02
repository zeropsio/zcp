package tools

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"

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

// findAndParseZeropsYml locates and parses zerops.yaml.
// Returns the parsed doc and the directory where zerops.yaml was found.
//
// Lookup is split by environment, mirroring the deploy pipeline that runs
// after pre-flight (`ops.deploySSH` reads from `/var/www/<sourceService>/`,
// `ops.deployLocal` reads from `workingDir`):
//
//   - sourceHostname != "": container environment — yaml MUST live on the
//     source service's SSHFS mount at `<projectRoot>/<sourceHostname>/`.
//     This is the canonical per-codebase layout (spec-workflows.md §1132,
//     spec-workflows.md §8 E8). No project-root fallback: a yaml at
//     `<projectRoot>/zerops.yaml` describes nothing the platform
//     understands, and silently validating it masked the cross-deploy
//     "/var/www/<targetService> not found" failure (commit e769c9f7
//     papered over the same regression in atom guidance).
//   - sourceHostname == "": local environment — yaml lives at
//     `<projectRoot>/zerops.yaml` (the user's working directory). No
//     per-service subdirectories exist on a developer's local box.
//
// On parse failure of a present file, the error is returned immediately;
// silently falling back would validate the wrong config (Codex review of
// G16 fix). Probe errors (permission denied, stale SSHFS, non-directory)
// surface immediately for the same reason.
func findAndParseZeropsYml(projectRoot, sourceHostname string) (*ops.ZeropsYmlDoc, string, error) {
	if sourceHostname == "" {
		// Local environment: yaml at projectRoot.
		doc, err := ops.ParseZeropsYml(projectRoot)
		if err != nil {
			return nil, projectRoot, fmt.Errorf("zerops.yaml not found at %s (local environment): %w",
				projectRoot, err)
		}
		return doc, projectRoot, nil
	}

	// Container environment: yaml on the source service's mount.
	mountPath := filepath.Join(projectRoot, sourceHostname)
	state, probeErr := probeMountForZeropsYml(mountPath)
	switch {
	case probeErr != nil:
		return nil, mountPath, fmt.Errorf("source mount %s probe failed: %w", mountPath, probeErr)
	case state == mountStatePresent:
		doc, parseErr := ops.ParseZeropsYml(mountPath)
		if parseErr != nil {
			return nil, mountPath, fmt.Errorf("source-mount zerops.yaml at %s is invalid: %w",
				mountPath, parseErr)
		}
		return doc, mountPath, nil
	case state == mountStateAbsent:
		return nil, mountPath, fmt.Errorf("source mount %s missing — scaffold zerops.yaml for service %q there",
			mountPath, sourceHostname)
	default:
		// mountStateNoYaml.
		return nil, mountPath, fmt.Errorf("zerops.yaml not present on source mount %s — scaffold it for service %q",
			mountPath, sourceHostname)
	}
}

// mountState distinguishes the three outcomes of probing a per-service
// mount path for zerops.yaml. Tri-state on purpose: a binary present/
// absent flag reproduces the silent-shadow bug under degraded mount
// conditions (permission errors, stale SSHFS) — those fall into the
// probe-error path and never reach this enum (probeMountForZeropsYml
// returns err and the caller surfaces it).
type mountState int

const (
	// mountStateAbsent — directory does not exist at all.
	mountStateAbsent mountState = iota
	// mountStateNoYaml — directory exists, scanned cleanly, no zerops.yaml/yml.
	mountStateNoYaml
	// mountStatePresent — directory exists and contains a zerops.yaml or zerops.yml file.
	mountStatePresent
)

// probeMountForZeropsYml inspects mountPath and reports whether a
// canonical zerops.yaml file is present, the directory is empty of
// such, the directory itself is missing, or the probe failed (any
// non-IsNotExist error). The caller decides on each branch — falling
// back to projectRoot is only safe on confirmed absence.
func probeMountForZeropsYml(mountPath string) (mountState, error) {
	info, err := os.Stat(mountPath)
	switch {
	case err == nil:
		if !info.IsDir() {
			return 0, fmt.Errorf("%s is not a directory", mountPath)
		}
	case os.IsNotExist(err):
		return mountStateAbsent, nil
	default:
		return 0, err
	}
	for _, name := range []string{"zerops.yaml", "zerops.yml"} {
		fi, statErr := os.Stat(filepath.Join(mountPath, name))
		switch {
		case statErr == nil:
			if fi.IsDir() {
				return 0, fmt.Errorf("%s is a directory, not a file", filepath.Join(mountPath, name))
			}
			return mountStatePresent, nil
		case os.IsNotExist(statErr):
			continue
		default:
			return 0, statErr
		}
	}
	return mountStateNoYaml, nil
}

// NOTE: a stat-check of deployFiles paths used to live here. It was deleted to
// enforce DM-4 (docs/spec-workflows.md §8 Deploy Modes): post-build filesystem
// existence is the Zerops builder's authority, not ZCP's. `ValidateZeropsYml`
// in ops/deploy_validate.go owns the sole source-tree-level deploy contract
// (DM-2 self-deploy constraint); layered-authority duplication is an invariant
// violation.
