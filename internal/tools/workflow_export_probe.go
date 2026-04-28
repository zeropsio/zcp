package tools

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	exportRepoRoot          = "/var/www"
	exportZeropsYAMLPath    = "/var/www/zerops.yaml"
	exportZeropsYMLFallback = "/var/www/zerops.yml"
)

// readZeropsYAMLBody SSH-reads the chosen container's zerops.yaml.
// Tries `.yaml` first, then `.yml` fallback. Empty stdout = file
// missing — handler chains to scaffold-zerops-yaml. SSH errors
// propagate as platform.PlatformError for handler conversion.
func readZeropsYAMLBody(ctx context.Context, ssh ops.SSHDeployer, hostname string) (string, error) {
	cmd := fmt.Sprintf(
		`cat %s 2>/dev/null || cat %s 2>/dev/null || true`,
		exportZeropsYAMLPath, exportZeropsYMLFallback,
	)
	out, err := ssh.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		return "", platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("Read zerops.yaml from %q: %v", hostname, err),
			"Verify the container is reachable via SSH and /var/www exists.",
		)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// readGitRemoteURL SSH-reads `git remote get-url origin` from the
// chosen container's /var/www. Empty stdout = no remote configured —
// handler chains to setup-git-push. The cached ServiceMeta.RemoteURL
// is intentionally NOT consulted here per plan §6 Phase 6 contract:
// live remote is the source of truth; cache is just a hint.
func readGitRemoteURL(ctx context.Context, ssh ops.SSHDeployer, hostname string) (string, error) {
	cmd := fmt.Sprintf(`cd %s 2>/dev/null && git remote get-url origin 2>/dev/null || true`, exportRepoRoot)
	out, err := ssh.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		return "", platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("Read git remote from %q: %v", hostname, err),
			"Verify the container is reachable via SSH.",
		)
	}
	return strings.TrimSpace(string(out)), nil
}

// pickSetupName resolves the zerops.yaml setup block matching the
// chosen runtime hostname. Strategy in order: exact match against
// hostname → match against pair-suffix-stripped hostname → match
// against well-known suffix names (dev/prod/stage/worker) → first
// setup if exactly one is present → error listing available names.
//
// The legacy atom encoded a heuristic mapping (export.md:82-95):
// `*dev` → setup `dev`, `*stage`/`*prod` → setup `prod`, `*worker`
// → setup `worker`. This implementation generalizes the mapping
// without hardcoded tables — it tries the most specific candidate
// first and falls back through progressively looser matches.
func pickSetupName(zeropsYAMLBody, targetHostname string, sourceMode topology.Mode) (string, error) {
	available, err := listSetupNames(zeropsYAMLBody)
	if err != nil {
		return "", err
	}
	if len(available) == 0 {
		return "", platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"zerops.yaml contains no setup blocks",
			"Edit /var/www/zerops.yaml so the `zerops:` list has at least one entry with `setup: <name>`.",
		)
	}

	candidates := setupCandidatesFor(targetHostname, sourceMode)
	for _, candidate := range candidates {
		for _, name := range available {
			if name == candidate {
				return name, nil
			}
		}
	}

	if len(available) == 1 {
		return available[0], nil
	}

	return "", platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("Cannot resolve zerops.yaml setup for hostname %q (mode=%s) — multiple setups present and no convention matches", targetHostname, sourceMode),
		fmt.Sprintf("Available setups: %s. Rename a setup block to match the hostname (or its pair suffix), then re-run.", strings.Join(available, ", ")),
	)
}

// listSetupNames parses zerops.yaml and returns the ordered list of
// `setup:` block names. Returns an empty slice on parse failure or
// when the top-level `zerops:` key is absent — caller turns this
// into a user-actionable error.
func listSetupNames(body string) ([]string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Parse zerops.yaml: %v", err),
			"Fix the YAML syntax and re-run export.",
		)
	}
	setups, ok := doc["zerops"].([]any)
	if !ok {
		return nil, nil
	}
	var names []string
	for _, item := range setups {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["setup"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// setupCandidatesFor produces the ordered list of setup names to try
// for a given hostname + source mode. Most specific first.
//
// Worked examples:
//
//	"appdev"   ModeStandard     → [appdev, app, dev]
//	"appstage" ModeStage        → [appstage, app, prod, stage]
//	"app"      ModeSimple       → [app, simple]
//	"workerdev" ModeStandard    → [workerdev, worker, dev]
//	"site"     ModeLocalOnly    → [site, local-only]
//
// The candidate list is conservative — it never invents prefixes the
// hostname doesn't contain. Callers that hit none of the candidates
// fall back to "first setup if exactly one" then surface an error.
func setupCandidatesFor(hostname string, sourceMode topology.Mode) []string {
	if hostname == "" {
		return nil
	}
	candidates := []string{hostname}

	suffixes := map[topology.Mode][]string{
		topology.ModeStandard:   {"dev"},
		topology.ModeStage:      {"prod", "stage"},
		topology.ModeDev:        {"dev"},
		topology.ModeSimple:     {"simple"},
		topology.ModeLocalStage: {"dev"},
		topology.ModeLocalOnly:  {"local-only"},
	}
	suffixesForMode := suffixes[sourceMode]

	for _, suffix := range suffixesForMode {
		if strings.HasSuffix(hostname, suffix) && hostname != suffix {
			prefix := strings.TrimSuffix(hostname, suffix)
			if prefix != "" {
				candidates = append(candidates, prefix)
				// Try the bare prefix paired with every other suffix in the
				// suffix set — covers "appstage" → "appprod" (Laravel-style
				// prod/stage rename) and "appstage" → "appdev" (when only
				// dev setups exist for a stage hostname).
				for _, other := range suffixesForMode {
					if other != suffix {
						candidates = append(candidates, prefix+other)
					}
				}
			}
		}
		candidates = append(candidates, suffix)
	}

	return dedupeCandidates(candidates)
}

func dedupeCandidates(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, c := range in {
		if seen[c] || c == "" {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// readProjectEnvs lists project-level envVariables from the platform
// API. Each entry becomes an ops.ProjectEnvVar awaiting classification
// at Phase B.
func readProjectEnvs(ctx context.Context, client platform.Client, projectID string) ([]ops.ProjectEnvVar, error) {
	envs, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Read project env vars: %v", err),
			"",
		)
	}
	out := make([]ops.ProjectEnvVar, 0, len(envs))
	for _, env := range envs {
		out = append(out, ops.ProjectEnvVar{Key: env.Key, Value: env.Content})
	}
	return out, nil
}

// refreshRemoteURLCache compares the live git remote URL against the
// cached `ServiceMeta.RemoteURL` and writes the live value when they
// differ. Returns a warnings slice when a mismatch is observed so the
// agent surfaces the drift, and an error when the meta-write fails
// (the live URL is still authoritative for the current bundle either
// way — the cache is a hint for tooling that doesn't SSH-read).
//
// Per plan §6 Phase 6: live `git remote get-url origin` is the source
// of truth; the cache is refreshed on every export pass so subsequent
// reads (e.g., `zerops_workflow action="status"`) see the same URL.
//
// Empty live URL is handled by the caller — the chain to setup-git-push
// fires before this helper runs.
func refreshRemoteURLCache(stateDir string, meta *workflow.ServiceMeta, liveURL string) ([]string, error) {
	if meta == nil || liveURL == "" {
		return nil, nil
	}
	if meta.RemoteURL == liveURL {
		return nil, nil
	}
	var warnings []string
	if meta.RemoteURL != "" {
		warnings = append(warnings, fmt.Sprintf(
			"ServiceMeta.RemoteURL cache for %q drifted (cache=%q, live=%q) — live value wins for the bundle; cache refreshed.",
			meta.Hostname, meta.RemoteURL, liveURL))
	}
	meta.RemoteURL = liveURL
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		return warnings, err
	}
	return warnings, nil
}

// collectManagedServices walks the Discover output and returns the
// managed (`IsInfrastructure`) entries — excluding the chosen runtime.
// The bundle MAY include these so `${db_*}` / `${redis_*}` references
// in zerops.yaml resolve at re-import (per plan §3.4 contract).
//
// Mode is preserved verbatim from Discover; managed services without
// HA/NON_HA distinction (object-storage, shared-storage) carry an
// empty Mode which composeImportYAML omits from the import.yaml entry.
func collectManagedServices(discover *ops.DiscoverResult, excludeHostname string) []ops.ManagedServiceEntry {
	if discover == nil {
		return nil
	}
	var out []ops.ManagedServiceEntry
	for _, svc := range discover.Services {
		if !svc.IsInfrastructure {
			continue
		}
		if svc.Hostname == excludeHostname {
			continue
		}
		out = append(out, ops.ManagedServiceEntry{
			Hostname: svc.Hostname,
			Type:     svc.Type,
			Mode:     svc.Mode,
		})
	}
	return out
}
