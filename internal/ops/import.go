package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// ServiceImportError represents an error for a specific service during import.
// Meta carries server-sent field-level detail for this service's rejection
// (same shape as PlatformError.APIMeta — see internal/platform/errors.go).
// When non-nil the LLM reads apiMeta[].metadata for the failing fields
// instead of guessing from the generic Message.
type ServiceImportError struct {
	Service string                 `json:"service"`
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Meta    []platform.APIMetaItem `json:"meta,omitempty"`
}

// ImportResult is returned after a successful API import.
type ImportResult struct {
	ProjectID     string                `json:"projectId"`
	ProjectName   string                `json:"projectName"`
	Processes     []ImportProcessOutput `json:"processes"`
	ServiceErrors []ServiceImportError  `json:"serviceErrors,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
	Summary       string                `json:"summary,omitempty"`
	NextActions   string                `json:"nextActions,omitempty"`
	// ProjectEnvsSet lists the keys applied from an inline project.vault or project.envVariables
	// block (Fix B1), sorted for deterministic output. Empty when the import
	// YAML carried no project: block.
	ProjectEnvsSet []string `json:"projectEnvsSet,omitempty"`
}

// ImportProcessOutput represents one process from the import result.
type ImportProcessOutput struct {
	ProcessID  string  `json:"processId"`
	ActionName string  `json:"actionName"`
	Status     string  `json:"status"`
	Service    string  `json:"service"`
	ServiceID  string  `json:"serviceId"`
	FailReason *string `json:"failReason,omitempty"`
}

// Import imports services from YAML into a project.
// Input: content XOR filePath (not both, not neither).
//
// Validation split: the Zerops API is the authoritative validator for every
// platform concept (service types, modes, field names, cross-field rules,
// hostname format). ZCP's pre-flight does only the things the API does NOT
// tell the LLM clearly:
//  1. `envVariables` at service level is deprecated and lands plain, so a
//     secret written there is stored readable — surfaced as a warning that
//     names the secret-shaped keys (serviceEnvVariablesWarnings).
//  2. A 'project:' section — WHITELISTED to `vault` and `envVariables` (Fix B1):
//     project.vault and project.envVariables are applied via the project-env channel (EnvSet)
//     before service creation, then stripped from the YAML sent to the
//     API. Any OTHER project.* field (preprocessor, scaling, name, ...)
//     still hard-rejects with a specific code instead of the generic
//     projectImport error — full project YAML belongs to project creation
//     (zcli or web UI), not zerops_import which operates inside an
//     existing project.
//
// Everything else — field names, hostname format, mode enums, type
// existence — the API catches with structured meta that PlatformError.APIMeta
// now propagates to the LLM (see plans/api-validation-plumbing.md).
//
// override: when true, sets `override: true` on every service so the API
// replaces existing service stacks instead of rejecting with
// serviceStackNameUnavailable.
func Import(
	ctx context.Context,
	client platform.Client,
	projectID string,
	content string,
	filePath string,
	override bool,
) (*ImportResult, error) {
	yamlContent, err := resolveInput(content, filePath)
	if err != nil {
		return nil, err
	}

	// Parse YAML into generic map for the ZCP-specific preflights.
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &doc); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidImportYml,
			fmt.Sprintf("invalid YAML: %v", err),
			"Check YAML syntax",
		)
	}

	// Process project: block if present — K12 in the validation-plumbing
	// plan, extended by Fix B1. Whitelist policy: only `envVariables` is
	// supported at import time — apply it via the existing project-env
	// channel (EnvSet), then strip the project: key before the YAML goes
	// to the API. Any other project.* field (preprocessor, scaling, name,
	// ...) still rejects with the specific IMPORT_HAS_PROJECT code instead
	// of the generic projectImport error, because the resolution ("remove
	// it, use project creation instead") is unambiguous.
	//
	// Reject-before-mutate: unsupported keys are checked FIRST, before any
	// EnvSet call, so a rejected import never leaves a partially-applied
	// project block behind.
	projectEnvsSet, hasProject, err := applyProjectBlock(ctx, client, projectID, doc)
	if err != nil {
		return nil, err
	}

	// When override is requested, set `override: true` on each service so
	// the API replaces existing service stacks instead of rejecting with
	// serviceStackNameUnavailable. Replacement is destructive — the
	// previous container, deployed code, env vars, and the SSHFS mount it
	// backs are all lost on the replaced service. ZCP collects the list of
	// pre-existing hostnames here (B10) so the response can name them in
	// `Warnings` instead of letting the destruction be silent.
	var overrideReplacements []string
	if override {
		existing, err := listExistingHostnames(ctx, client, projectID)
		if err != nil {
			return nil, err
		}
		if raw, ok := doc["services"].([]any); ok {
			for _, svc := range raw {
				svcMap, ok := svc.(map[string]any)
				if !ok {
					continue
				}
				svcMap["override"] = true
				if hostname, ok := svcMap["hostname"].(string); ok && existing[hostname] {
					overrideReplacements = append(overrideReplacements, hostname)
				}
			}
		}
	}

	// An app's own values reach Zerops through the import itself, so the
	// name rule is written into them here: envSecrets (deprecated) moves into
	// vault, and a secret-shaped name without a flag of its own goes in
	// sensitive.
	secretsMarked, err := markServiceSecrets(doc)
	if err != nil {
		return nil, err
	}

	// Sole remarshal point: doc was mutated above (project: stripped,
	// override: true injected, secrets marked) — re-serialize once so every
	// mutation lands in the YAML sent to the API, keeping the preprocessor
	// header, without which every `<@…>` would be stored as literal text.
	// When nothing changed, the original yamlContent passes through
	// byte-for-byte.
	if hasProject || override || secretsMarked {
		remarshaled, err := yaml.Marshal(doc)
		if err != nil {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidImportYml,
				fmt.Sprintf("re-marshal after project/override handling: %v", err),
				"Report this as a zcp bug.",
			)
		}
		yamlContent = preprocessorHeader(yamlContent) + string(remarshaled)
	}

	warnings := serviceEnvVariablesWarnings(doc)

	// B10: surface the destructive blast radius of override=true. Replacing
	// a service stack tears down its container, deployed code, env vars,
	// and the SSHFS mount path becomes empty as the new (often empty)
	// container reattaches. Naming the affected hostnames keeps later
	// agent reasoning honest about what just happened.
	if len(overrideReplacements) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"override=true REPLACED existing service stack(s) %v — the previous container, deployed code, env vars, and SSHFS mount contents (/var/www/<hostname>/) on those services are gone. Re-deploy any code that was on them; re-set any per-service env vars. Mount path now reflects the new (likely empty) container.",
			overrideReplacements,
		))
	}

	// Wait for any DELETING services with conflicting hostnames to finish.
	// Race prevention, not validation — API would reject the import with a
	// timing-dependent error and the LLM would have to retry anyway.
	hostnames := extractHostnames(doc)
	if err := waitForDeletingServices(ctx, client, projectID, hostnames); err != nil {
		return nil, err
	}

	result, err := client.ImportServices(ctx, projectID, yamlContent)
	if err != nil {
		return nil, err
	}

	var processes []ImportProcessOutput
	var serviceErrors []ServiceImportError
	for _, ss := range result.ServiceStacks {
		if ss.Error != nil {
			serviceErrors = append(serviceErrors, ServiceImportError{
				Service: ss.Name,
				Code:    ss.Error.Code,
				Message: ss.Error.Message,
				Meta:    ss.Error.Meta,
			})
		}
		for _, p := range ss.Processes {
			processes = append(processes, ImportProcessOutput{
				ProcessID:  p.ID,
				ActionName: p.ActionName,
				Status:     p.Status,
				Service:    ss.Name,
				ServiceID:  ss.ID,
				FailReason: p.FailReason,
			})
		}
	}

	return &ImportResult{
		ProjectID:      result.ProjectID,
		ProjectName:    result.ProjectName,
		Processes:      processes,
		ServiceErrors:  serviceErrors,
		Warnings:       warnings,
		ProjectEnvsSet: projectEnvsSet,
	}, nil
}

// listExistingHostnames returns the set of hostnames currently provisioned
// in the project. Used by Import (override=true) to identify which target
// services would be REPLACED — destructive — so the response can name
// them explicitly instead of leaving the wipe silent (B10).
func listExistingHostnames(ctx context.Context, client platform.Client, projectID string) (map[string]bool, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services for override warning: %w", err)
	}
	out := make(map[string]bool, len(services))
	for _, svc := range services {
		out[svc.Name] = true
	}
	return out, nil
}

// extractHostnames parses service hostnames from the parsed import YAML document.
func extractHostnames(doc map[string]any) []string {
	raw, ok := doc["services"]
	if !ok {
		return nil
	}
	servicesList, ok := raw.([]any)
	if !ok {
		return nil
	}
	var hostnames []string
	for _, svc := range servicesList {
		svcMap, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		if h, ok := svcMap["hostname"].(string); ok && h != "" {
			hostnames = append(hostnames, h)
		}
	}
	return hostnames
}

// waitForDeletingServices polls ListServices until no DELETING services
// conflict with the requested hostnames. Returns ErrAPITimeout on context
// cancellation, deadline exceeded, or after a 5-minute hardcoded timeout.
func waitForDeletingServices(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostnames []string,
) error {
	if len(hostnames) == 0 {
		return nil
	}

	wantSet := make(map[string]bool, len(hostnames))
	for _, h := range hostnames {
		wantSet[h] = true
	}

	const (
		pollInterval = 3 * time.Second
		timeout      = 5 * time.Minute
	)
	start := time.Now()

	for {
		services, err := client.ListServices(ctx, projectID)
		if err != nil {
			return fmt.Errorf("list services for DELETING check: %w", err)
		}

		var conflicts []string
		for _, svc := range services {
			if svc.Status == "DELETING" && wantSet[svc.Name] {
				conflicts = append(conflicts, svc.Name)
			}
		}
		if len(conflicts) == 0 {
			return nil
		}

		if time.Since(start) > timeout {
			return platform.NewPlatformError(
				platform.ErrAPITimeout,
				fmt.Sprintf("timed out waiting for DELETING services after %s: %v", timeout, conflicts),
				"Services are still being deleted. Wait and retry, or use a different hostname.",
			)
		}

		select {
		case <-ctx.Done():
			return platform.NewPlatformError(
				platform.ErrAPITimeout,
				fmt.Sprintf("timed out waiting for DELETING services to finish: %v", conflicts),
				"Services are still being deleted. Wait and retry, or use a different hostname.",
			)
		case <-time.After(pollInterval):
			// Continue polling.
		}
	}
}

// resolveInput resolves content XOR filePath into YAML content string.
func resolveInput(content, filePath string) (string, error) {
	if content != "" && filePath != "" {
		return "", platform.NewPlatformError(
			platform.ErrInvalidUsage,
			"provide either content or filePath, not both",
			"Use content for inline YAML or filePath for a file",
		)
	}
	if content == "" && filePath == "" {
		return "", platform.NewPlatformError(
			platform.ErrInvalidUsage,
			"provide either content or filePath",
			"Use content for inline YAML or filePath for a file",
		)
	}
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", platform.NewPlatformError(
					platform.ErrFileNotFound,
					fmt.Sprintf("file not found: %s", filePath),
					"Check the file path",
				)
			}
			return "", platform.NewPlatformError(
				platform.ErrFileNotFound,
				fmt.Sprintf("read file: %v", err),
				"Check file permissions",
			)
		}
		return string(data), nil
	}
	return content, nil
}

// vaultItem is one value of an import `vault:` block: `KEY: value`, or
// `KEY: {value: …, sensitive: …}` (the import schema's two forms).
type vaultItem struct {
	value     string
	sensitive *bool
}

func parseVaultItem(block, key string, raw any) (vaultItem, error) {
	object, ok := raw.(map[string]any)
	if !ok {
		if raw == nil {
			return vaultItem{}, nil
		}
		return vaultItem{value: fmt.Sprint(raw)}, nil
	}
	value, ok := object["value"]
	if !ok {
		return vaultItem{}, platform.NewPlatformError(
			platform.ErrInvalidImportYml,
			fmt.Sprintf("%s.%s has no value", block, key),
			"Write `KEY: value`, or `KEY: {value: …, sensitive: true}`.",
		)
	}
	item := vaultItem{}
	if value != nil {
		item.value = fmt.Sprint(value)
	}
	if flag, ok := object["sensitive"].(bool); ok {
		item.sensitive = &flag
	}
	return item, nil
}

// serviceEnvVariablesWarnings warns about each service carrying a
// service-level `envVariables` block. The API accepts it and creates its
// values as the service's env, but the import schema marks it deprecated in
// favour of `vault`, and every value lands plain: the schema defines them as
// non-sensitive and zcp's name rule (markServiceSecrets) reads only vault, so
// a secret written there is stored readable. The warning says so and names
// the secret-shaped keys (topology.DefaultSensitiveValue); the block itself
// reaches the API as written (docs/spec-zerops-env-lifecycle.md §8b).
func serviceEnvVariablesWarnings(doc map[string]any) []string {
	services, ok := doc["services"].([]any)
	if !ok {
		return nil
	}
	var warnings []string
	for _, raw := range services {
		svc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		block, has := svc["envVariables"]
		if !has {
			continue
		}
		hostname, _ := svc["hostname"].(string)
		warning := fmt.Sprintf(
			"service %q: 'envVariables' is deprecated — the import schema says use 'vault'. Its values are still created as the service's env, every one plain (not sensitive). Write the service's own values under 'vault' (a secret as {value: …, sensitive: true}), or runtime config in zerops.yaml run.envVariables.",
			hostname,
		)
		values, _ := block.(map[string]any)
		var secrets []string
		for key, rawValue := range values {
			value := ""
			if rawValue != nil {
				value = fmt.Sprint(rawValue)
			}
			if topology.DefaultSensitiveValue(key, value) {
				secrets = append(secrets, key)
			}
		}
		if len(secrets) > 0 {
			sort.Strings(secrets)
			warning += fmt.Sprintf(
				" Secret-shaped names stored plain here: %s — under 'vault' they are stored sensitive.",
				strings.Join(secrets, ", "),
			)
		}
		warnings = append(warnings, warning)
	}
	return warnings
}

// markServiceSecrets writes the name rule into each service's own values:
// its envSecrets move into its vault, and every vault value whose name says
// secret (topology.DefaultSensitive) and that carries no flag of its own becomes
// `{value, sensitive: true}`. A key in both keeps its vault entry. Reports
// whether anything changed.
func markServiceSecrets(doc map[string]any) (bool, error) {
	services, ok := doc["services"].([]any)
	if !ok {
		return false, nil
	}
	changed := false
	for _, raw := range services {
		svc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		vault, _ := svc["vault"].(map[string]any)
		if secrets, ok := svc["envSecrets"].(map[string]any); ok {
			if vault == nil {
				vault = map[string]any{}
			}
			for key, value := range secrets {
				if _, held := vault[key]; !held {
					vault[key] = value
				}
			}
			delete(svc, "envSecrets")
			svc["vault"] = vault
			changed = true
		}
		for key, raw := range vault {
			hostname, _ := svc["hostname"].(string)
			item, err := parseVaultItem("services["+hostname+"].vault", key, raw)
			if err != nil {
				return false, err
			}
			if item.sensitive != nil || !topology.DefaultSensitiveValue(key, item.value) {
				continue
			}
			vault[key] = map[string]any{"value": item.value, "sensitive": true}
			changed = true
		}
	}
	return changed, nil
}

// preprocessorHeader is the preprocessor line an import YAML opens with, if
// it does — `#zeropsPreprocessor=on` or `#yamlPreprocessor=on`, as written —
// ready to put back in front of a re-serialized document.
func preprocessorHeader(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		compact := strings.ReplaceAll(trimmed, " ", "")
		if strings.EqualFold(compact, "#zeropsPreprocessor=on") || strings.EqualFold(compact, "#yamlPreprocessor=on") {
			return trimmed + "\n"
		}
		return ""
	}
	return ""
}

// applyProjectBlock applies an import's project block — K12 in the
// validation-plumbing plan, extended by Fix B1 and the vault: only `vault`
// and `envVariables` are supported at import time, applied through the
// project-env channel (EnvSetEach) before the block is stripped from the
// YAML sent to the API. Any other project.* field rejects with the specific
// IMPORT_HAS_PROJECT code, before anything is written.
func applyProjectBlock(
	ctx context.Context,
	client platform.Client,
	projectID string,
	doc map[string]any,
) ([]string, bool, error) {
	projectBlock, hasProject := doc["project"].(map[string]any)
	if !hasProject {
		return nil, false, nil
	}
	var projectEnvsSet []string
	for key := range projectBlock {
		if key != "envVariables" && key != "vault" {
			return nil, false, platform.NewPlatformError(
				platform.ErrImportHasProject,
				fmt.Sprintf("import YAML project.%s is not supported at import time — zerops_import operates within the existing project", key),
				"Remove project."+key+" — only project.vault (and the deprecated project.envVariables) is supported inline (applied automatically before services are created). Other project.* fields belong to project creation (zcli or web UI). To set them on the existing project first, use `zerops_env action=\"set\" scope=\"project\" key=\"<KEY>\" value=\"<value>\"` (preprocessor directives like `<@generateRandomString(<32>)>` are passed literally and evaluated server-side).",
			)
		}
	}
	// Every value first, then one write pass: a refused item never leaves
	// half a block behind, and generated values share one expansion store.
	var pairs []string
	flags := map[string]bool{}
	if envVarsRaw, ok := projectBlock["envVariables"].(map[string]any); ok {
		for k, v := range envVarsRaw {
			pairs = append(pairs, fmt.Sprintf("%s=%v", k, v))
		}
	}
	if vaultRaw, ok := projectBlock["vault"].(map[string]any); ok {
		for k, raw := range vaultRaw {
			item, err := parseVaultItem("project.vault", k, raw)
			if err != nil {
				return nil, false, err
			}
			pairs = append(pairs, k+"="+item.value)
			if item.sensitive != nil {
				flags[k] = *item.sensitive
			}
		}
	}
	if len(pairs) > 0 {
		sort.Strings(pairs) // deterministic ordering — map iteration is not
		if _, err := EnvSetEach(ctx, client, projectID, "", true, pairs, flags); err != nil {
			return nil, false, fmt.Errorf("apply project values from import: %w", err)
		}
		projectEnvsSet = make([]string, 0, len(pairs))
		for _, pair := range pairs {
			key, _, _ := strings.Cut(pair, "=")
			projectEnvsSet = append(projectEnvsSet, key)
		}
		sort.Strings(projectEnvsSet)
	}

	delete(doc, "project")
	return projectEnvsSet, true, nil
}
