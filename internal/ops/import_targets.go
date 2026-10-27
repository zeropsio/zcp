package ops

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// IdentifyOverrideTargets returns the subset of import-YAML hostnames that
// already exist in the project — i.e. the services that override=true would
// REPLACE. Used by tools.RegisterImport to feed the diagnose-before-destruct
// gate (plan v4 §3.2): the gate only fires for services with failed
// appVersion history, and the candidate set is exactly the would-replace
// hostnames.
//
// Returns an empty slice (not nil) when the YAML lists hostnames but none
// match existing services. Errors propagate from the YAML parse and the
// ListServices call; YAML structural issues (missing services list,
// non-string hostnames) yield an empty result rather than an error so the
// downstream Import call can produce its own structured error.
func IdentifyOverrideTargets(
	ctx context.Context,
	client platform.Client,
	projectID string,
	content string,
	filePath string,
) ([]string, error) {
	yamlContent, err := resolveInput(content, filePath)
	if err != nil {
		return nil, err
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &doc); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidImportYml,
			fmt.Sprintf("invalid YAML: %v", err),
			"Check YAML syntax",
		)
	}

	yamlHostnames := extractHostnames(doc)
	if len(yamlHostnames) == 0 {
		return []string{}, nil
	}

	existing, err := listExistingHostnames(ctx, client, projectID)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(yamlHostnames))
	for _, h := range yamlHostnames {
		if existing[h] {
			out = append(out, h)
		}
	}
	return out, nil
}

// zcpTypePrefix is every version of the zcp container's service type.
const zcpTypePrefix = "zcp@"

// ImportedZcpServices returns the hostnames of the import YAML's services
// whose type is a zcp container (`zcp@…`). Used by tools.RegisterImport: a
// Mate's project holds one zcp service, the Mate's own (spec-mate §6.6). A
// service whose hostname or type is not a string is none of them, so the
// Import call produces its own structured error for it.
func ImportedZcpServices(content, filePath string) ([]string, error) {
	yamlContent, err := resolveInput(content, filePath)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &doc); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidImportYml,
			fmt.Sprintf("invalid YAML: %v", err),
			"Check YAML syntax",
		)
	}
	servicesList, _ := doc["services"].([]any)
	var out []string
	for _, svc := range servicesList {
		svcMap, _ := svc.(map[string]any)
		hostname, _ := svcMap["hostname"].(string)
		serviceType, _ := svcMap["type"].(string)
		if hostname != "" && strings.HasPrefix(strings.TrimSpace(serviceType), zcpTypePrefix) {
			out = append(out, hostname)
		}
	}
	return out, nil
}

// boxCapacityMessage is the part of the platform's message a Zerops Box's
// create fails with outside prg1: "Sorry, there is currently not enough
// hardware capacity to run your container." (live: ny1, 2026-10-23 and
// 2026-10-24).
const boxCapacityMessage = "not enough hardware capacity"

// BoxRegionWarnings names the Box region rule for each Zerops Box of the
// import whose create FAILED with the platform's capacity message: a Box can
// be created only in a project in region prg1 (owner-stated platform fact,
// 2026-10-24), and elsewhere the message reads like a passing shortage. The
// failure reason itself stays as the platform gave it. Pure over the import
// YAML and the polled processes — zcp reads no project region for it (spec-
// workflows.md §8 PS-10). A document that does not read yields none.
func BoxRegionWarnings(content, filePath string, procs []ImportProcessOutput) []string {
	yamlContent, err := resolveInput(content, filePath)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &doc); err != nil {
		return nil
	}
	boxes := map[string]bool{}
	servicesList, _ := doc["services"].([]any)
	for _, svc := range servicesList {
		svcMap, _ := svc.(map[string]any)
		hostname, _ := svcMap["hostname"].(string)
		serviceType, _ := svcMap["type"].(string)
		if hostname != "" && topology.IsSandboxType(serviceType) {
			boxes[hostname] = true
		}
	}
	var out []string
	warned := map[string]bool{}
	for _, p := range procs {
		if !boxes[p.Service] || warned[p.Service] || p.Status != platform.ProcessStatusFailed ||
			p.FailReason == nil || !strings.Contains(*p.FailReason, boxCapacityMessage) {
			continue
		}
		warned[p.Service] = true
		out = append(out, fmt.Sprintf("%s is a Zerops Box, and a Box can be created only in a project in region prg1: in any other region its create fails with this %q message. If this project is not in prg1, a retry here fails the same way — import the Box into a project in prg1.",
			p.Service, boxCapacityMessage))
	}
	return out
}

// ImportedBoxServices returns the hostnames of the import YAML's services
// whose type is a Zerops Box (`ubuntu/zbox@1`, or the bare `zbox@1` the
// platform stores as it): zerops_import finishes each one's setup once its
// create finished (docs/spec-workflows.md §8 PS-11). A service whose hostname
// or type is not a string is none of them.
func ImportedBoxServices(content, filePath string) ([]string, error) {
	yamlContent, err := resolveInput(content, filePath)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &doc); err != nil {
		return nil, platform.NewPlatformError(platform.ErrInvalidImportYml, fmt.Sprintf("invalid YAML: %v", err), "Check YAML syntax")
	}
	servicesList, _ := doc["services"].([]any)
	var out []string
	for _, svc := range servicesList {
		svcMap, _ := svc.(map[string]any)
		hostname, _ := svcMap["hostname"].(string)
		serviceType, _ := svcMap["type"].(string)
		if hostname != "" && topology.IsSandboxType(serviceType) {
			out = append(out, hostname)
		}
	}
	return out, nil
}
