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

	importErrors := schema.ValidateImportYAML(importYAML)
	zeropsErrors := schema.ValidateZeropsYAML(inputs.ZeropsYAMLBody, inputs.SetupName)
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
func composeImportYAML(
	inputs BundleInputs,
	classifications map[string]topology.SecretClassification,
) (string, []string, error) {
	projectEnvs, warnings := composeProjectEnvVariables(inputs.ProjectEnvs, classifications)

	zeropsRefs := extractZeropsYAMLRunEnvRefs(inputs.ZeropsYAMLBody)
	warnings = append(warnings, detectIndirectInfraReferences(inputs.ProjectEnvs, classifications, zeropsRefs)...)

	runtimeEntry := map[string]any{
		"hostname":     inputs.TargetHostname,
		"type":         inputs.ServiceType,
		"mode":         runtimeImportMode(inputs.SourceMode),
		"buildFromGit": inputs.RepoURL,
		"zeropsSetup":  inputs.SetupName,
	}
	if inputs.SubdomainEnabled {
		runtimeEntry["enableSubdomainAccess"] = true
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
