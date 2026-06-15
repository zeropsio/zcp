package bundle

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// BuildExport composes the export-buildFromGit bundle for either the
// dev or stage variant. Pure composition — no I/O. The handler
// resolves source state (SSH + Discover + git remote) upstream and
// hands the typed inputs in.
func BuildExport(
	inputs BundleInputs,
	classifications map[string]topology.SecretClassification,
) (*ExportBundle, error) {
	if inputs.TargetHostname == "" {
		return nil, fmt.Errorf("export bundle: target hostname required")
	}
	if inputs.RepoURL == "" {
		return nil, fmt.Errorf("export bundle: repo URL required (chain to setup-git-push)")
	}
	if inputs.SetupName == "" {
		return nil, fmt.Errorf("export bundle: zerops.yaml setup name required")
	}
	if inputs.ServiceType == "" {
		return nil, fmt.Errorf("export bundle: runtime service type required")
	}
	if classifications == nil {
		classifications = map[string]topology.SecretClassification{}
	}

	if err := verifyZeropsYAMLSetup(inputs.ZeropsYAMLBody, inputs.SetupName); err != nil {
		return nil, fmt.Errorf("verify zerops.yaml: %w", err)
	}

	importYAML, warnings, err := composeImportYAML(inputs, classifications)
	if err != nil {
		return nil, fmt.Errorf("compose import.yaml: %w", err)
	}

	// Structure-only validation: service types + build/run bases in an export
	// bundle come from a live Discover of the user's running services, so they
	// are already platform-valid; the platform re-validates them at re-import.
	// Validating them against the binary's frozen enum would only false-reject
	// a bundle the platform already runs. Field-typo / required / stable-enum
	// guards are preserved.
	importErrors := schema.ValidateImportYAMLStructure(importYAML)
	zeropsErrors := schema.ValidateZeropsYAMLStructure(inputs.ZeropsYAMLBody, inputs.SetupName)
	validationErrors := make([]schema.ValidationError, 0, len(importErrors)+len(zeropsErrors))
	validationErrors = append(validationErrors, importErrors...)
	validationErrors = append(validationErrors, zeropsErrors...)

	return &ExportBundle{
		ImportYAML:       importYAML,
		ZeropsYAML:       inputs.ZeropsYAMLBody,
		ZeropsYAMLSource: "live",
		RepoURL:          inputs.RepoURL,
		TargetHostname:   inputs.TargetHostname,
		SetupName:        inputs.SetupName,
		Classifications:  classifications,
		Warnings:         warnings,
		Errors:           validationErrors,
	}, nil
}

// composeImportYAML produces the zerops-project-import.yaml
// body for an export bundle: project block + ONE runtime service
// entry with buildFromGit + zeropsSetup + (optional)
// enableSubdomainAccess, plus any managed services the agent
// included so `${db_*}` / `${redis_*}` references in zerops.yaml
// resolve in the destination project.
// projectScaling writes the live scaling snapshot onto a runtime import entry —
// minContainers/maxContainers at the service level + a verticalAutoscaling block
// (min/max CPU, RAM, disk), matching the import-yml JSON schema. Export is an
// IDENTITY transform: each field is reproduced verbatim, zero values omitted.
// Returns a warning when the snapshot is nil (scaling unreadable) so the silent
// revert-to-defaults the omission used to cause is now visible. Launch reuses
// this then applies its named production transforms (see launch.go).
func projectScaling(entry map[string]any, s *Scaling) string {
	if s == nil {
		return "scaling shape unread from the live service — the re-import will use platform defaults for containers/CPU/RAM/disk"
	}
	if s.MinContainers > 0 {
		entry["minContainers"] = s.MinContainers
	}
	if s.MaxContainers > 0 {
		entry["maxContainers"] = s.MaxContainers
	}
	va := map[string]any{}
	if s.CPUMode != "" {
		va["cpuMode"] = s.CPUMode
	}
	if s.MinCPU > 0 {
		va["minCpu"] = s.MinCPU
	}
	if s.MaxCPU > 0 {
		va["maxCpu"] = s.MaxCPU
	}
	if s.MinRAM > 0 {
		va["minRam"] = s.MinRAM
	}
	if s.MaxRAM > 0 {
		va["maxRam"] = s.MaxRAM
	}
	if s.MinDisk > 0 {
		va["minDisk"] = s.MinDisk
	}
	if s.MaxDisk > 0 {
		va["maxDisk"] = s.MaxDisk
	}
	if len(va) > 0 {
		entry["verticalAutoscaling"] = va
	}
	return ""
}

func composeImportYAML(
	inputs BundleInputs,
	classifications map[string]topology.SecretClassification,
) (string, []string, error) {
	projectEnvs, warnings := composeProjectEnvVariables(inputs.ProjectEnvs, classifications)

	zeropsRefs := extractZeropsYAMLRunEnvRefs(inputs.ZeropsYAMLBody)
	warnings = append(warnings, detectDroppedEnvReferences(inputs.ProjectEnvs, classifications, zeropsRefs)...)

	// No `mode`: runtimes are always HA on the platform (a mode/variant on a
	// runtime is ignored), so a legacy `mode: NON_HA` is vestigial noise. The
	// destination topology (dev/simple/local-only) is a bootstrap concern, not
	// import.yaml content.
	runtimeEntry := map[string]any{
		"hostname":     inputs.TargetHostname,
		"type":         inputs.ServiceType,
		"buildFromGit": topology.CanonicalRepoURL(inputs.RepoURL),
		"zeropsSetup":  inputs.SetupName,
	}
	if inputs.SubdomainEnabled {
		runtimeEntry["enableSubdomainAccess"] = true
	}
	// R7: project the live scaling shape so a re-import reproduces the deployed
	// containers/CPU/RAM/disk rather than silently reverting to platform
	// defaults. A nil snapshot is surfaced as a warning, never a silent drop.
	if w := projectScaling(runtimeEntry, inputs.Scaling); w != "" {
		warnings = append(warnings, w)
	}
	// GAP0-1: carry the runtime's per-service USER-set env (Type=SECRET
	// slim layer) as envSecrets so a key set via `zerops_env set
	// serviceHostname=X` survives re-import (buildFromGit does not rebuild
	// it — it is not in zerops.yaml). Secret-safe: unclassified entries
	// emit REPLACE_ME, never the verbatim value.
	svcSecrets, svcWarnings := composeServiceEnvSecrets(inputs.ServiceEnvs, classifications)
	warnings = append(warnings, svcWarnings...)
	if len(svcSecrets) > 0 {
		runtimeEntry["envSecrets"] = svcSecrets
	}

	services := make([]any, 0, 1+len(inputs.ManagedServices))
	services = append(services, runtimeEntry)
	for _, m := range inputs.ManagedServices {
		entry := managedEntryWithRules(m, false /*launch*/, false /*keepNonHA*/)
		services = append(services, entry)
	}

	project := map[string]any{
		"name": inputs.ProjectName,
	}
	if len(projectEnvs) > 0 {
		project["envVariables"] = projectEnvs
	}

	doc := map[string]any{
		"project":  project,
		"services": services,
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", nil, fmt.Errorf("marshal: %w", err)
	}
	body := string(out)
	body = addPreprocessorHeader(body, projectEnvs, svcSecrets)

	return body, warnings, nil
}
