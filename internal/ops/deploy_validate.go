package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
	"gopkg.in/yaml.v3"
)

// ValidateZeropsYml checks zerops.yaml for common issues before deploy.
//
// serviceType is the Zerops service type (e.g. "php-nginx@8.4") — used to detect implicit
// webservers when zerops.yaml bases alone are insufficient (e.g. build.base: php@8.4 for a
// php-nginx service). Pass "" if unknown.
//
// class distinguishes self-deploy (source == target, DM-1) from cross-deploy.
// Self-deploy with narrower-than-[.] deployFiles is rejected as a hard error
// (DM-2) — source destruction is guaranteed, not an advisory concern.
//
// roles carries the explicit service role from the caller's ServiceMeta.
// Callers that don't know the role (empty) skip role-specific advisories.
// Returns (warnings, err) — err is non-nil for DM-2 violations. Both channels
// remain populated when err != nil so callers can surface partial findings.
//
// Scope per DM-4 (docs/spec-workflows.md §8 Deploy Modes): validates only
// source-tree-knowable facts (yaml shape, schema coherence, deploy-class
// contract, role-mode advisories). Post-build filesystem existence of
// deployFiles paths is Zerops builder's authority, not ZCP's.
func ValidateZeropsYml(workingDir, targetHostname, serviceType string, class DeployClass, roles ...string) ([]string, error) {
	var warnings []string

	doc, err := ParseZeropsYml(workingDir)
	if err != nil {
		// Parse failures stay in the warnings channel (non-blocking): the
		// API-side validator (RunPreDeployValidation in deploy_ssh.go /
		// deploy_local.go) is authoritative for yaml syntax/schema errors
		// and will block with a structured apiMeta response. The err
		// channel is reserved for DM-2 (deploy-class contract) violations.
		return []string{err.Error()}, nil //nolint:nilerr // parse error demoted to warning; authoritative validation is API-side
	}

	if len(doc.Zerops) == 0 {
		return []string{"zerops.yaml has no setup entries under 'zerops:' key"}, nil
	}

	// Find matching setup entry.
	entry := doc.FindEntry(targetHostname)
	if entry == nil {
		warnings = append(warnings, fmt.Sprintf("no setup entry for hostname %q in zerops.yaml", targetHostname))
		return warnings, nil
	}

	implicitWS := hasImplicitWebServer(entry.Run.Base, entry.Build.BaseStrings()) || IsImplicitWebServerType(serviceType)
	if !implicitWS {
		if entry.Run.Start == "" {
			warnings = append(warnings, "run.start is empty — app will not start after deploy")
		}

		if len(entry.Run.Ports) == 0 {
			warnings = append(warnings, "run.ports is empty — no ports exposed, HTTP checks will fail")
		}
	}

	deployFiles := entry.Build.deployFilesList()
	if len(deployFiles) == 0 {
		warnings = append(warnings, "build.deployFiles is empty — nothing will be deployed to run container")
	}

	// Detect deployFiles in wrong section (run: instead of build:).
	if entry.Run.DeployFiles != nil {
		warnings = append(warnings, "deployFiles is under 'run:' but belongs under 'build:' — move it to build.deployFiles")
	}

	// Package install commands need sudo — containers run as zerops user.
	if HasPkgInstallWithoutSudo(entry.Run.PrepareCommands) {
		warnings = append(warnings, "run.prepareCommands has package install without sudo (apk add / apt-get install) — containers run as zerops user, prefix with sudo")
	}
	if HasPkgInstallWithoutSudo(entry.Build.PrepareCommands) {
		warnings = append(warnings, "build.prepareCommands has package install without sudo (apk add / apt-get install) — containers run as zerops user, prefix with sudo")
	}

	// DM-2: self-deploy with cherry-pick deployFiles is destructive.
	// The source container IS the target; extracting a narrow artifact
	// overwrites its working tree with only the selection, and subsequent
	// self-deploys cannot re-push what is no longer on disk. Hard error,
	// never an advisory. See docs/spec-workflows.md §8 Deploy Modes.
	if class == DeployClassSelf && len(deployFiles) > 0 &&
		!slices.Contains(deployFiles, ".") && !slices.Contains(deployFiles, "./") {
		return warnings, platform.NewPlatformError(
			platform.ErrInvalidZeropsYml,
			fmt.Sprintf("self-deploy setup %q: deployFiles must be [.] or [./] — narrower patterns destroy the target's working tree on artifact extraction (DM-2)", entry.Setup),
			"Set `deployFiles: [.]` for self-deploy. To cherry-pick build output, use cross-deploy (pass sourceService != targetService, or strategy=git-push).",
		)
	}

	// Explicit role, no fallback. Empty role skips role-specific advisories.
	role := ""
	if len(roles) > 0 {
		role = roles[0]
	}
	isDev := role == string(workflow.DeployRoleDev)
	isStage := role == string(workflow.DeployRoleStage)

	// Stage services with "zsc noop" build command are likely misconfigured.
	if isStage && entry.Build.hasZscNoop() {
		warnings = append(warnings, fmt.Sprintf(
			"setup %q: stage service uses 'zsc noop' build command — stage should have real build commands, 'zsc noop' is for dev services only",
			entry.Setup))
	}

	if isDev && entry.Run.HealthCheck != nil {
		warnings = append(warnings, fmt.Sprintf(
			"setup %q: dev service has run.healthCheck — this causes unwanted container restarts during iteration. Remove healthCheck from dev entries (keep it on stage only).",
			entry.Setup))
	}
	if isDev && entry.Deploy.ReadinessCheck != nil {
		warnings = append(warnings, fmt.Sprintf(
			"setup %q: dev service has deploy.readinessCheck — unnecessary for dev (agent verifies manually). Remove readinessCheck from dev entries.",
			entry.Setup))
	}

	return warnings, nil
}

// ZeropsYmlDoc is the top-level zerops.yaml structure (minimal for validation).
type ZeropsYmlDoc struct {
	Zerops []ZeropsYmlEntry `yaml:"zerops"`
}

// ZeropsYmlEntry represents a single setup entry in zerops.yaml.
type ZeropsYmlEntry struct {
	Setup        string            `yaml:"setup"`
	Build        zeropsYmlBuild    `yaml:"build"`
	Deploy       zeropsYmlDeploy   `yaml:"deploy"`
	Run          zeropsYmlRun      `yaml:"run"`
	EnvVariables map[string]string `yaml:"envVariables"`
}

// HasPorts returns true if the entry has at least one run.ports entry.
func (e ZeropsYmlEntry) HasPorts() bool {
	return len(e.Run.Ports) > 0 || hasImplicitWebServer(e.Run.Base, e.Build.BaseStrings())
}

// HasDeployFiles returns true if the entry has non-empty build.deployFiles.
func (e ZeropsYmlEntry) HasDeployFiles() bool {
	return len(e.Build.deployFilesList()) > 0
}

// DeployFilesList returns the normalized list of deploy file paths.
func (e ZeropsYmlEntry) DeployFilesList() []string {
	return e.Build.deployFilesList()
}

// HasImplicitWebServer returns true if the entry's runtime has a built-in web
// server that starts automatically (no run.start or run.ports needed).
func (e ZeropsYmlEntry) HasImplicitWebServer() bool {
	return hasImplicitWebServer(e.Run.Base, e.Build.BaseStrings())
}

// ParseZeropsYml reads and parses zerops.yaml (or zerops.yml fallback) from workingDir.
// Returns the parsed document or an error if the file is missing or invalid.
func ParseZeropsYml(workingDir string) (*ZeropsYmlDoc, error) {
	ymlPath := filepath.Join(workingDir, "zerops.yaml")
	data, err := os.ReadFile(ymlPath)
	if err != nil {
		ymlPath = filepath.Join(workingDir, "zerops.yml")
		data, err = os.ReadFile(ymlPath)
		if err != nil {
			return nil, fmt.Errorf("zerops.yaml not found in %s (also tried zerops.yml)", workingDir)
		}
	}
	var doc ZeropsYmlDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s invalid YAML: %w", filepath.Base(ymlPath), err)
	}
	return &doc, nil
}

// ReadZeropsYmlRaw reads zerops.yaml (or zerops.yml fallback) and returns raw bytes.
// Used for schema field validation — the typed ParseZeropsYml silently drops unknown fields.
func ReadZeropsYmlRaw(workingDir string) ([]byte, error) {
	ymlPath := filepath.Join(workingDir, "zerops.yaml")
	data, err := os.ReadFile(ymlPath)
	if err != nil {
		ymlPath = filepath.Join(workingDir, "zerops.yml")
		data, err = os.ReadFile(ymlPath)
		if err != nil {
			return nil, fmt.Errorf("zerops.yaml not found in %s", workingDir)
		}
	}
	return data, nil
}

// FindEntry returns the entry matching hostname, or nil if not found.
func (d *ZeropsYmlDoc) FindEntry(hostname string) *ZeropsYmlEntry {
	for i := range d.Zerops {
		if d.Zerops[i].Setup == hostname {
			return &d.Zerops[i]
		}
	}
	return nil
}

// SetupNames returns every declared setup name in the order they appear in
// zerops.yaml. Used by pre-flight to print the available setups when a
// resolve attempt fails, so the agent sees the valid choices (rather than
// zcli's generic "Cannot find corresponding setup" error).
func (d *ZeropsYmlDoc) SetupNames() []string {
	if d == nil {
		return nil
	}
	names := make([]string, 0, len(d.Zerops))
	for _, e := range d.Zerops {
		if e.Setup != "" {
			names = append(names, e.Setup)
		}
	}
	return names
}

type zeropsYmlDeploy struct {
	ReadinessCheck any `yaml:"readinessCheck"`
}

type zeropsYmlBuild struct {
	Base            any `yaml:"base"`            // string or []string — Zerops accepts both
	PrepareCommands any `yaml:"prepareCommands"` // string or []string — for sudo detection
	BuildCommands   any `yaml:"buildCommands"`   // string or []string
	DeployFiles     any `yaml:"deployFiles"`     // string or []string — Zerops accepts both
}

// deployFilesList normalizes DeployFiles to []string regardless of YAML format.
func (b zeropsYmlBuild) deployFilesList() []string {
	switch v := b.DeployFiles.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

type zeropsYmlRun struct {
	Base            string            `yaml:"base"`
	Start           string            `yaml:"start"`
	Ports           []zeropsYmlPort   `yaml:"ports"`
	HealthCheck     any               `yaml:"healthCheck"`
	DeployFiles     any               `yaml:"deployFiles"`     // catch misplaced field (belongs under build:)
	PrepareCommands any               `yaml:"prepareCommands"` // for /var/www detection
	EnvVariables    map[string]string `yaml:"envVariables"`    // canonical location (zerops.yaml schema)
}

type zeropsYmlPort struct {
	Port int `yaml:"port"`
}

// BaseStrings normalizes Base to []string regardless of YAML format (string or []string).
func (b zeropsYmlBuild) BaseStrings() []string {
	switch v := b.Base.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// hasZscNoop returns true if buildCommands contains "zsc noop".
func (b zeropsYmlBuild) hasZscNoop() bool {
	switch v := b.BuildCommands.(type) {
	case string:
		return strings.TrimSpace(v) == "zsc noop"
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) == "zsc noop" {
				return true
			}
		}
	}
	return false
}

// EnvRefError describes an invalid ${hostname_varName} reference in env vars.
type EnvRefError struct {
	Variable  string `json:"variable"`  // env var name containing the bad ref
	Reference string `json:"reference"` // the ${hostname_varName} reference
	Reason    string `json:"reason"`    // "unknown hostname" or "unknown variable"
}

// ValidateEnvReferences checks ${hostname_varName} patterns in env var values
// against discovered env vars and live hostnames. Returns errors for invalid refs.
func ValidateEnvReferences(envVars map[string]string, discoveredEnvVars map[string][]string, liveHostnames []string) []EnvRefError {
	hostnameSet := make(map[string]bool, len(liveHostnames))
	for _, h := range liveHostnames {
		hostnameSet[h] = true
	}

	var errs []EnvRefError
	for varName, value := range envVars {
		refs := parseEnvRefs(value)
		for _, ref := range refs {
			hostname, varPart := ref.hostname, ref.varName
			if !hostnameSet[hostname] {
				errs = append(errs, EnvRefError{
					Variable:  varName,
					Reference: ref.raw,
					Reason:    fmt.Sprintf("unknown hostname %q", hostname),
				})
				continue
			}
			knownVars := discoveredEnvVars[hostname]
			if !slices.Contains(knownVars, varPart) {
				errs = append(errs, EnvRefError{
					Variable:  varName,
					Reference: ref.raw,
					Reason:    fmt.Sprintf("unknown variable %q on hostname %q", varPart, hostname),
				})
			}
		}
	}
	return errs
}

type envRef struct {
	raw      string // e.g. "${db_connectionString}"
	hostname string // e.g. "db"
	varName  string // e.g. "connectionString"
}

// parseEnvRefs extracts all ${hostname_varName} references from a string.
func parseEnvRefs(s string) []envRef {
	var refs []envRef
	for {
		idx := strings.Index(s, "${")
		if idx == -1 {
			break
		}
		s = s[idx:]
		end := strings.Index(s, "}")
		if end == -1 {
			break
		}
		inner := s[2:end] // hostname_varName
		s = s[end+1:]

		// Must contain exactly one underscore separating hostname and varName.
		underIdx := strings.Index(inner, "_")
		if underIdx <= 0 || underIdx == len(inner)-1 {
			continue
		}
		refs = append(refs, envRef{
			raw:      "${" + inner + "}",
			hostname: inner[:underIdx],
			varName:  inner[underIdx+1:],
		})
	}
	return refs
}

// IsImplicitWebServerType returns true if the given service type (e.g. "php-nginx@8.4")
// has a built-in web server that starts automatically.
func IsImplicitWebServerType(serviceType string) bool {
	b, _, _ := strings.Cut(serviceType, "@")
	switch b {
	case runtimePHPApach, runtimePHPNginx, runtimeNginx, runtimeStatic:
		return true
	}
	return false
}

// HasPkgInstallWithoutSudo checks if any command in a YAML commands field
// contains apk add or apt-get install without a sudo prefix.
func HasPkgInstallWithoutSudo(commands any) bool {
	var cmds []string
	switch v := commands.(type) {
	case string:
		cmds = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				cmds = append(cmds, s)
			}
		}
	}
	for _, cmd := range cmds {
		cmd = strings.TrimSpace(cmd)
		if (strings.Contains(cmd, "apk add") || strings.Contains(cmd, "apt-get install")) &&
			!strings.Contains(cmd, "sudo") {
			return true
		}
	}
	return false
}

// hasImplicitWebServer returns true if the runtime has a built-in web server
// that starts automatically (no run.start or run.ports needed).
// Checks run.base first, falls back to build.base strings.
func hasImplicitWebServer(runBase string, buildBases []string) bool {
	bases := append([]string{runBase}, buildBases...)
	for _, base := range bases {
		if base == "" {
			continue
		}
		if base == runtimeStatic {
			return true
		}
		b, _, _ := strings.Cut(base, "@")
		switch b {
		case runtimePHPApach, runtimePHPNginx, runtimeNginx, runtimeStatic:
			return true
		}
	}
	return false
}
