package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
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
func ValidateZeropsYml(workingDir, targetHostname, serviceType string, class DeployClass, roles ...topology.DeployRole) ([]string, error) {
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
	var role topology.DeployRole
	if len(roles) > 0 {
		role = roles[0]
	}
	isDev := role == topology.DeployRoleDev
	isStage := role == topology.DeployRoleStage

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

	if err := CheckReservedEnvNames(entry); err != nil {
		return warnings, err
	}

	return warnings, nil
}

// hardReservedEnvKeys lists env-var keys the Zerops API rejects in any
// envVariables block (build or run) with code=userDataUseOfSystemKey.
// Case-sensitive exact match against the platform's denylist as verified
// 2026-05-16 (plans/audit-env-vars-20260515/VERIFY-reserved-names.md).
var hardReservedEnvKeys = map[string]bool{
	"hostname":        true,
	"PATH":            true,
	"serviceId":       true,
	"projectId":       true,
	"appVersionId":    true,
	"appVersionName":  true,
	"zeropsSubdomain": true,
}

// runScopeReservedEnvKeys lists keys that pass the API check but crash
// the runtime container init when set in run.envVariables (the
// build.envVariables side is accepted). Symptom: BUILD_FAILED event in
// 4-5s with zero build logs.
var runScopeReservedEnvKeys = map[string]bool{
	"HOSTNAME": true,
	"Path":     true,
	"path":     true,
}

// CheckReservedEnvNames scans entry.Build.EnvVariables and
// entry.Run.EnvVariables for reserved keys. Returns an ErrInvalidZeropsYml
// platform error naming the offending keys + scope when any reserved key
// is present; nil otherwise.
//
// Pinning: TestCheckReservedEnvNames_* in deploy_validate_test.go.
// Empirical basis: 26 probes documented in
// plans/audit-env-vars-20260515/VERIFY-reserved-names.md.
func CheckReservedEnvNames(entry *ZeropsYmlEntry) error {
	type violation struct {
		scope string
		key   string
	}
	var violations []violation
	for key := range entry.Build.EnvVariables {
		if hardReservedEnvKeys[key] {
			violations = append(violations, violation{"build", key})
		}
	}
	for key := range entry.Run.EnvVariables {
		if hardReservedEnvKeys[key] || runScopeReservedEnvKeys[key] {
			violations = append(violations, violation{"run", key})
		}
	}
	if len(violations) == 0 {
		return nil
	}
	slices.SortFunc(violations, func(a, b violation) int {
		if a.scope != b.scope {
			return strings.Compare(a.scope, b.scope)
		}
		return strings.Compare(a.key, b.key)
	})
	keys := make([]string, 0, len(violations))
	scoped := make([]string, 0, len(violations))
	for _, v := range violations {
		keys = append(keys, v.key)
		scoped = append(scoped, fmt.Sprintf("%s.envVariables.%s", v.scope, v.key))
	}
	return platform.NewPlatformError(
		platform.ErrInvalidZeropsYml,
		fmt.Sprintf("setup %q: reserved env-var key(s) in zerops.yaml: %s", entry.Setup, strings.Join(scoped, ", ")),
		fmt.Sprintf("Remove or rename %s. Hard-reserved keys (%s) are rejected by the Zerops API; HOSTNAME/Path/path in run.envVariables crash runtime-init with empty build logs. See the develop-reserved-env-names atom for the full set + rationale.", strings.Join(keys, ", "), "hostname, PATH, serviceId, projectId, appVersionId, appVersionName, zeropsSubdomain"),
	)
}

// ZeropsYmlDoc is the top-level zerops.yaml structure (minimal for validation).
type ZeropsYmlDoc struct {
	Zerops []ZeropsYmlEntry `yaml:"zerops"`
}

// ZeropsYmlEntry represents a single setup entry in zerops.yaml.
type ZeropsYmlEntry struct {
	Setup  string          `yaml:"setup"`
	Build  zeropsYmlBuild  `yaml:"build"`
	Deploy zeropsYmlDeploy `yaml:"deploy"`
	Run    zeropsYmlRun    `yaml:"run"`
}

// HasPorts returns true when the setup declares an HTTP-serving runtime.
// Explicit run.ports is authoritative. Implicit webserver bases serve HTTP
// only when no custom run.start overrides the runtime's built-in web start.
func (e ZeropsYmlEntry) HasPorts() bool {
	if len(e.Run.Ports) > 0 {
		return true
	}
	if strings.TrimSpace(e.Run.Start) != "" {
		return false
	}
	return hasImplicitWebServer(e.Run.Base, e.Build.BaseStrings())
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

// zeropsYmlPrimary / zeropsYmlFallback are the canonical zerops.yaml filenames
// (primary + legacy fallback), centralized so the repeated literals don't drift —
// and don't collide (goconst) with the EnvLayerYamlBaked label that shares the
// zeropsYmlPrimary value but is a semantically distinct env-layer name.
const (
	zeropsYmlPrimary  = "zerops.yaml"
	zeropsYmlFallback = "zerops.yml"
)

// ParseZeropsYml reads and parses zerops.yaml (or zerops.yml fallback) from workingDir.
// Returns the parsed document or an error if the file is missing or invalid.
func ParseZeropsYml(workingDir string) (*ZeropsYmlDoc, error) {
	ymlPath := filepath.Join(workingDir, zeropsYmlPrimary)
	data, err := os.ReadFile(ymlPath)
	source := zeropsYmlPrimary
	if err != nil {
		ymlPath = filepath.Join(workingDir, zeropsYmlFallback)
		data, err = os.ReadFile(ymlPath)
		if err != nil {
			return nil, fmt.Errorf("zerops.yaml not found in %s (also tried zerops.yml)", workingDir)
		}
		source = zeropsYmlFallback
	}
	return ParseZeropsYmlContent(data, source)
}

// ParseZeropsYmlContent parses raw zerops.yaml bytes into a typed document.
// source is the originating filename (zerops.yaml / zerops.yml) used in the
// error message so a malformed YAML hauled in over SSH still names the right
// file. Used by the git-push pre-flight (deploy-decomp P4): yaml content is
// fetched from the container via SSH cat, parsed here, then env-var refs are
// validated against live API state.
func ParseZeropsYmlContent(data []byte, source string) (*ZeropsYmlDoc, error) {
	if source == "" {
		source = zeropsYmlPrimary
	}
	var doc ZeropsYmlDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s invalid YAML: %w", source, err)
	}
	return &doc, nil
}

// ReadZeropsYmlRaw reads zerops.yaml (or zerops.yml fallback) and returns raw bytes.
// Used for schema field validation — the typed ParseZeropsYml silently drops unknown fields.
func ReadZeropsYmlRaw(workingDir string) ([]byte, error) {
	ymlPath := filepath.Join(workingDir, zeropsYmlPrimary)
	data, err := os.ReadFile(ymlPath)
	if err != nil {
		ymlPath = filepath.Join(workingDir, zeropsYmlFallback)
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
	Base            any               `yaml:"base"`            // string or []string — Zerops accepts both
	PrepareCommands any               `yaml:"prepareCommands"` // string or []string — for sudo detection
	BuildCommands   any               `yaml:"buildCommands"`   // string or []string
	DeployFiles     any               `yaml:"deployFiles"`     // string or []string — Zerops accepts both
	EnvVariables    map[string]string `yaml:"envVariables"`    // build-scope env (caught by CheckReservedEnvNames)
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

// EnvRefError describes an unconfirmed ${hostname_varName} reference in env vars.
type EnvRefError struct {
	Variable  string `json:"variable"`  // env var name containing the ref
	Reference string `json:"reference"` // the ${hostname_varName} reference
	Host      string `json:"host"`      // target service hostname (for lifecycle partition)
	Reason    string `json:"reason"`    // "unknown variable"
}

// ValidateEnvReferences checks ${hostname_varName} patterns in env var
// values against the platform-discovered env vars per hostname. Lone refs
// (bodies that match no live hostname prefix) are skipped — they're either
// project-level vars (handled by GetProjectEnv at deploy time) or runtime
// placeholders the platform resolves inside the container. Empty live-
// hostnames slice loosens validation to "skip everything", matching the
// shim-mode contract documented on CheckEnvRefs.
func ValidateEnvReferences(envVars map[string]string, discoveredEnvVars map[string][]string, liveHostnames []string) []EnvRefError {
	services := make([]platform.ServiceStack, 0, len(liveHostnames))
	for _, h := range liveHostnames {
		services = append(services, platform.ServiceStack{Name: h})
	}
	classifier := NewEnvRefClassifier(services)

	var errs []EnvRefError
	for varName, value := range envVars {
		for _, m := range FindEnvRefs(value) {
			host, varPart, isCross := classifier.Classify(m.Body)
			if !isCross {
				continue
			}
			knownVars := discoveredEnvVars[host]
			if !slices.Contains(knownVars, varPart) {
				errs = append(errs, EnvRefError{
					Variable:  varName,
					Reference: m.Raw,
					Host:      host,
					Reason:    fmt.Sprintf("unknown variable %q on hostname %q", varPart, host),
				})
			}
		}
	}
	return errs
}

// IsImplicitWebServerType returns true if the given service type (e.g. "php-nginx@8.4"
// or post-Sunday-release "alpine/php-nginx@8.4") has a built-in web server
// that starts automatically. Strips OS prefix via topology.CanonicalBareForm
// before the switch so composite and bare shapes match the same case.
func IsImplicitWebServerType(serviceType string) bool {
	canonical := topology.CanonicalBareForm(serviceType)
	b, _, _ := strings.Cut(canonical, "@")
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
//
// Post-Sunday-release the live zerops.yaml schema enums non-`static` bases
// as composite-only (`alpine/php-nginx@8.4`); recipe yamls may emit either
// shape. Strip OS prefix via topology.CanonicalBareForm before the switch
// so both shapes hit the same case. Standalone `runtimeStatic` (no `@`)
// remains a special bare token preserved by the live schema.
func hasImplicitWebServer(runBase string, buildBases []string) bool {
	bases := append([]string{runBase}, buildBases...)
	for _, base := range bases {
		if base == "" {
			continue
		}
		if base == runtimeStatic {
			return true
		}
		canonical := topology.CanonicalBareForm(base)
		b, _, _ := strings.Cut(canonical, "@")
		switch b {
		case runtimePHPApach, runtimePHPNginx, runtimeNginx, runtimeStatic:
			return true
		}
	}
	return false
}
