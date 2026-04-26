package tools

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
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
//
// Lookup precedence:
//
//   - If hostname is set AND the per-service mount has a present zerops.yaml,
//     that file is canonical. A parse failure on the present file is
//     returned immediately — falling back to projectRoot's yaml here would
//     silently validate an unrelated config (Codex review of G16 fix).
//   - If the per-service mount probe ERRORS (permission denied, stale
//     SSHFS, anything other than confirmed absence), return that error
//     immediately. Falling back to projectRoot under degraded mount
//     conditions reproduces the same shadow class.
//   - If the per-service mount is confirmed absent OR is a clean directory
//     with no zerops.yaml present, the projectRoot is tried as a fallback
//     (local-env shape).
//   - On total miss, the returned error names every directory searched
//     plus the hostname context so the agent scaffolds at the right
//     per-service mount without re-deriving the SSHFS layout from
//     CLAUDE.md prose.
func findAndParseZeropsYml(projectRoot, hostname string) (*ops.ZeropsYmlDoc, string, error) {
	var triedPaths []string

	// Try mount path for target hostname (container environment).
	if hostname != "" {
		mountPath := filepath.Join(projectRoot, hostname)
		state, probeErr := probeMountForZeropsYml(mountPath)
		switch {
		case probeErr != nil:
			// Probe itself failed (permission denied, stale SSHFS,
			// non-directory, etc.). Don't fall back — surfacing the
			// real cause beats validating an unrelated root yaml.
			return nil, mountPath, fmt.Errorf("per-service mount %s probe failed: %w", mountPath, probeErr)
		case state == mountStatePresent:
			// File present in the canonical location — its parse
			// outcome owns the result. Don't fall back; another
			// path's yaml describes a different service.
			doc, parseErr := ops.ParseZeropsYml(mountPath)
			if parseErr != nil {
				return nil, mountPath, fmt.Errorf("per-service zerops.yaml at %s is invalid: %w", mountPath, parseErr)
			}
			return doc, mountPath, nil
		case state == mountStateNoYaml:
			triedPaths = append(triedPaths, mountPath+" (per-service mount, no zerops.yaml present)")
		case state == mountStateAbsent:
			triedPaths = append(triedPaths, mountPath+" (per-service mount, missing)")
		}
	}

	// Fall back to project root (local environment).
	doc, err := ops.ParseZeropsYml(projectRoot)
	if err == nil {
		return doc, projectRoot, nil
	}
	triedPaths = append(triedPaths, projectRoot+" (project root)")

	hint := ""
	if hostname != "" {
		hint = fmt.Sprintf(" — scaffold zerops.yaml for service %q at %s",
			hostname, filepath.Join(projectRoot, hostname))
	}
	return nil, projectRoot, fmt.Errorf("zerops.yaml not found: tried %s%s",
		strings.Join(triedPaths, ", "), hint)
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
