package ops

import (
	"context"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// GetContext returns Zerops platform context for AI agents.
// If client and cache are provided, appends dynamic service stack types.
// Falls back to static-only when client/cache is nil or API fails.
func GetContext(ctx context.Context, client platform.Client, cache *StackTypeCache) string {
	static := zeropsContext

	if client == nil || cache == nil {
		return static
	}

	types := cache.Get(ctx, client)
	dynamic := formatServiceStacks(types)
	if dynamic == "" {
		return static
	}

	return static + "\n\n" + dynamic
}

const statusActive = "ACTIVE"

// hiddenCategories are internal categories not shown to users.
var hiddenCategories = map[string]bool{
	"CORE":             true, // contains only internal "Core" type
	"INTERNAL":         true,
	"BUILD":            true,
	"PREPARE_RUNTIME":  true,
	"HTTP_L7_BALANCER": true,
}

// categoryOrder defines display order for user-facing categories.
var categoryOrder = []string{
	"USER",
	"STANDARD",
	"SHARED_STORAGE",
	"OBJECT_STORAGE",
}

func formatServiceStacks(types []platform.ServiceStackType) string {
	if len(types) == 0 {
		return ""
	}

	// Collect build version names from BUILD category for cross-reference.
	buildVersions := collectBuildVersions(types)
	// Track which build versions get matched to a visible type.
	matchedBuild := make(map[string]bool)

	// Group visible types by category.
	grouped := make(map[string][]platform.ServiceStackType)
	for _, st := range types {
		if hiddenCategories[st.Category] {
			continue
		}
		grouped[st.Category] = append(grouped[st.Category], st)
	}

	if len(grouped) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Service Stacks (live)\n")

	writeCategory := func(cat string, stacks []platform.ServiceStackType) {
		sb.WriteString("\n### ")
		sb.WriteString(categoryDisplayName(cat))
		sb.WriteByte('\n')
		for _, st := range stacks {
			sb.WriteString(formatStackEntry(st, buildVersions, matchedBuild))
		}
	}

	for _, cat := range categoryOrder {
		if stacks, ok := grouped[cat]; ok {
			writeCategory(cat, stacks)
		}
	}

	// Any remaining categories not in categoryOrder, sorted for determinism.
	var remaining []string
	for cat := range grouped {
		if slices.Contains(categoryOrder, cat) {
			continue
		}
		remaining = append(remaining, cat)
	}
	slices.Sort(remaining)
	for _, cat := range remaining {
		writeCategory(cat, grouped[cat])
	}

	// Show unmatched BUILD versions (e.g., php@8.4 for PHP build base).
	buildSection := formatUnmatchedBuildTypes(types, matchedBuild)
	if buildSection != "" {
		sb.WriteString(buildSection)
	}

	return sb.String()
}

// collectBuildVersions returns a set of version names from BUILD category types.
func collectBuildVersions(types []platform.ServiceStackType) map[string]bool {
	result := make(map[string]bool)
	for _, st := range types {
		if st.Category != "BUILD" {
			continue
		}
		for _, v := range st.Versions {
			if v.Status == statusActive {
				result[v.Name] = true
			}
		}
	}
	return result
}

func formatStackEntry(st platform.ServiceStackType, buildVersions, matchedBuild map[string]bool) string {
	var versions []string
	for _, v := range st.Versions {
		if v.Status != statusActive {
			continue
		}
		versions = append(versions, v.Name)
	}

	if len(versions) == 0 {
		return ""
	}

	// Determine build capability from BUILD category cross-reference.
	// If ANY version of this type also exists in BUILD → type supports build+run.
	hasBuild := false
	for _, vn := range versions {
		if buildVersions[vn] {
			hasBuild = true
			matchedBuild[vn] = true
		}
	}

	label := "run"
	if hasBuild {
		label = "build+run"
	}

	return "- **" + st.Name + "** — " + label + ": " + strings.Join(versions, ", ") + "\n"
}

// formatUnmatchedBuildTypes returns a markdown section for BUILD versions
// that didn't match any visible run type (e.g., php@8.4 as PHP build base).
func formatUnmatchedBuildTypes(types []platform.ServiceStackType, matchedBuild map[string]bool) string {
	var entries []string
	for _, st := range types {
		if st.Category != "BUILD" {
			continue
		}
		// Skip internal build runtime types.
		if !strings.HasPrefix(st.Name, "zbuild ") {
			continue
		}
		var unmatched []string
		for _, v := range st.Versions {
			if v.Status == statusActive && !matchedBuild[v.Name] {
				unmatched = append(unmatched, v.Name)
			}
		}
		if len(unmatched) == 0 {
			continue
		}
		name := strings.TrimPrefix(st.Name, "zbuild ")
		entries = append(entries, "- **"+name+"** — "+strings.Join(unmatched, ", "))
	}
	if len(entries) == 0 {
		return ""
	}
	return "\n### Build-only Bases (zerops.yml build.base)\n" + strings.Join(entries, "\n") + "\n"
}

func categoryDisplayName(cat string) string {
	switch cat {
	case "USER":
		return "Runtime & Container"
	case "STANDARD":
		return "Managed Services"
	case "SHARED_STORAGE":
		return "Shared Storage"
	case "OBJECT_STORAGE":
		return "Object Storage"
	default:
		return cat
	}
}

const zeropsContext = `# Platform Context

## Overview

Zerops is a developer-first PaaS built on bare-metal infrastructure. It runs full Linux containers
(Incus/LXD), not serverless functions. Every container has SSH access, a real filesystem, and runs
managed services (PostgreSQL, MariaDB, Valkey, Elasticsearch, Meilisearch, Kafka, S3-compatible
object storage). Infrastructure is VXLAN-networked with automatic service discovery.

## How Zerops Works

Zerops organizes resources in a hierarchy: Project -> Services -> Containers.

- **Project**: isolated environment with private VXLAN network. All services within a project
  can communicate via hostnames (e.g., http://db:5432).
- **Service**: a logical unit (e.g., "api", "db", "cache") backed by one or more containers.
  Each service has a hostname, type, and scaling configuration.
- **Container**: actual Linux instance running the service. Auto-scaled horizontally (1-N containers)
  and vertically (CPU/RAM/disk).

Networking: services communicate over a private VXLAN overlay network using hostnames.
External traffic enters through an L7 load balancer that terminates SSL.

## Critical Rules

- **Internal networking uses http://, NEVER https://** — SSL terminates at the L7 balancer.
  Services must connect to each other via http://hostname:port.
- **Ports must be in range 10-65435** — ports 0-9 and 65436+ are reserved by the platform.
- **HA mode is immutable** — once a service is created as HA or NON_HA, it cannot be changed.
  Recreate the service to switch modes.
- **Database/cache services REQUIRE mode** — import.yml must specify mode: NON_HA or HA for
  databases (postgresql, mariadb, clickhouse) and caches (valkey, keydb). Omitting mode passes
  dry-run validation but fails real import.
- **Environment variable cross-references use underscores** — ${service_hostname}, not
  ${service-hostname}. Dashes in hostnames are replaced with underscores in env var references.
- **No localhost** — services cannot use localhost/127.0.0.1 to reach other services. Always
  use the service hostname.
- **prepareCommands are cached** — they run once and are cached. Use initCommands for logic
  that must run on every container start.

## Configuration

- **zerops.yml** — build + deploy + run configuration per service. Defines build pipeline
  (base, prepareCommands, buildCommands, deployFiles), runtime (base, initCommands, start),
  and ports/routing.
- **import.yml** — infrastructure-as-code for service creation. Contains a services: array
  defining service type, version, mode, hostname, and initial scaling. Must NOT contain a
  project: section (projects are created separately).

## Defaults

When not specified, Zerops uses these defaults:
- postgresql@16, valkey@7.2, meilisearch@1.10, nats@2.10
- alpine base image for custom containers
- NON_HA mode (single container, no high availability)
- SHARED CPU mode (burstable, cost-effective)

## Pointers

- Use zerops_knowledge tool to search Zerops documentation for specific topics.
- Use zerops_workflow tool for step-by-step guidance on common tasks (bootstrap, deploy, debug, scale, configure, monitor).
- Use zerops_discover tool to inspect current project and service state.`
