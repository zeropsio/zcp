package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateContractSpec produces the cross-codebase contract spec YAML from
// a research plan (v8.86 §3.5). The spec is consumed by scaffold sub-agent
// briefs as required input — each scaffold verifies the named files exist
// with the specified shape before returning, and the README writer reads
// the spec so IG items reference real contracts rather than paraphrases.
//
// The form is framework-agnostic by design (HTTP response shapes, DB
// schema, NATS subject + queue group, graceful shutdown). Instance content
// is derived from plan shape: a recipe with no NATS service skips the
// nats_subjects section; a recipe with no separate-codebase worker skips
// the graceful_shutdown section; etc.
//
// Determinism: target iteration order is sorted, field order is fixed,
// consumers are sorted alphabetically. The spec is meant to be diffable
// across runs of the same plan — a drift in output signals a spec
// generator bug, not a plan change.
func GenerateContractSpec(plan *RecipePlan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("contract spec: nil plan")
	}

	var sb strings.Builder
	sb.WriteString("# Cross-codebase contract spec (v8.86 §3.5)\n")
	sb.WriteString("# Generated at generate.contract-spec sub-step; consumed by scaffold sub-agents + readmes writer.\n")
	sb.WriteString("# Each section lists the shape bindings that must stay in sync across codebases.\n\n")
	sb.WriteString("contract_spec:\n")

	writeHTTPSection(&sb, plan)
	writeDatabaseSection(&sb, plan)
	writeNATSSection(&sb, plan)
	writeGracefulShutdownSection(&sb, plan)

	return sb.String(), nil
}

func writeHTTPSection(sb *strings.Builder, plan *RecipePlan) {
	sb.WriteString("  http_endpoints:\n")
	apiHostname := ""
	appHostname := ""
	for _, t := range plan.Targets {
		if t.Role == RecipeRoleAPI {
			apiHostname = t.Hostname
		}
		if t.Role == RecipeRoleApp {
			appHostname = t.Hostname
		}
	}
	// /api/status is the standard showcase endpoint: flat object keyed by
	// managed-service name. v23 shipped a StatusPanel that expected an
	// array-of-objects; the scaffold's response was flat-object. Naming
	// the shape_kind explicitly here catches that class at scaffold time.
	sb.WriteString("    /api/status:\n")
	sb.WriteString("      response_shape: '{\"db\":\"ok\",\"redis\":\"ok\",\"nats\":\"ok\",\"storage\":\"ok\",\"search\":\"ok\"}'\n")
	sb.WriteString("      response_shape_kind: flat-object  # NOT nested-array\n")
	if apiHostname != "" || appHostname != "" {
		sb.WriteString("      consumed_by:\n")
		consumers := []string{}
		if appHostname != "" {
			consumers = append(consumers, fmt.Sprintf("%sdev/src/lib/StatusPanel.svelte", appHostname))
		}
		if apiHostname != "" {
			consumers = append(consumers, fmt.Sprintf("%sdev/src/controllers/status.controller.ts", apiHostname))
		}
		sort.Strings(consumers)
		for _, c := range consumers {
			fmt.Fprintf(sb, "        - %s\n", c)
		}
	}
	sb.WriteString("\n")
}

func writeDatabaseSection(sb *strings.Builder, plan *RecipePlan) {
	// Only emit when the plan includes a managed SQL DB.
	hasDB := false
	for _, t := range plan.Targets {
		if isSQLDatabaseType(t.Type) {
			hasDB = true
			break
		}
	}
	if !hasDB {
		sb.WriteString("  database_tables: {}  # no SQL DB in plan\n\n")
		return
	}
	sb.WriteString("  database_tables:\n")
	sb.WriteString("    items:  # standard showcase entity; adjust name per plan\n")
	sb.WriteString("      columns:\n")
	sb.WriteString("        - id uuid PRIMARY KEY\n")
	sb.WriteString("        - title varchar(200)  # NOT 255 — keep consistent across migration + entity\n")
	sb.WriteString("        - body text\n")
	sb.WriteString("        - created_at timestamptz DEFAULT now()  # snake_case, NOT camelCase\n")
	sb.WriteString("        - updated_at timestamptz DEFAULT now()\n")
	sb.WriteString("      consumed_by:\n")
	consumers := []string{}
	for _, t := range plan.Targets {
		if t.Role == RecipeRoleAPI {
			consumers = append(consumers, fmt.Sprintf("%sdev/src/migrations/CreateItems*.ts", t.Hostname))
			consumers = append(consumers, fmt.Sprintf("%sdev/src/entities/Item.entity.ts", t.Hostname))
		}
		if t.IsWorker && t.SharesCodebaseWith == "" {
			consumers = append(consumers, fmt.Sprintf("%sdev/src/entities/Item.entity.ts  # MUST mirror apidev", t.Hostname))
		}
	}
	sort.Strings(consumers)
	for _, c := range consumers {
		fmt.Fprintf(sb, "        - %s\n", c)
	}
	sb.WriteString("\n")
}

func writeNATSSection(sb *strings.Builder, plan *RecipePlan) {
	// Only emit subjects when the plan includes NATS AND a separate-codebase
	// worker exists. Shared-codebase workers run in the host's entry point;
	// the queue-group binding applies to separate-codebase workers that
	// scale horizontally on their own.
	hasNATS := false
	for _, t := range plan.Targets {
		if isNATSType(t.Type) {
			hasNATS = true
			break
		}
	}
	var separateWorker, apiHostname string
	for _, t := range plan.Targets {
		if t.Role == RecipeRoleAPI {
			apiHostname = t.Hostname
		}
		if t.IsWorker && t.SharesCodebaseWith == "" {
			separateWorker = t.Hostname
		}
	}
	if !hasNATS {
		sb.WriteString("  nats_subjects: {}  # no NATS in plan\n\n")
		return
	}
	sb.WriteString("  nats_subjects:\n")
	sb.WriteString("    jobs.process:\n")
	if separateWorker != "" {
		sb.WriteString("      queue_group: 'jobs-workers'  # REQUIRED when minContainers > 1 (horizontal scaling OR HA/rolling-deploy)\n")
	}
	if apiHostname != "" {
		fmt.Fprintf(sb, "      published_by: %sdev/src/services/jobs.service.ts\n", apiHostname)
	}
	if separateWorker != "" {
		fmt.Fprintf(sb, "      subscribed_by: %sdev/src/worker.controller.ts  # @EventPattern + queueGroup\n", separateWorker)
	}
	sb.WriteString("\n")
}

func writeGracefulShutdownSection(sb *strings.Builder, plan *RecipePlan) {
	// Only separate-codebase workers need the SIGTERM drain pattern named
	// here. Shared-codebase workers inherit the host's shutdown hook.
	var workerHost string
	for _, t := range plan.Targets {
		if t.IsWorker && t.SharesCodebaseWith == "" {
			workerHost = t.Hostname
			break
		}
	}
	if workerHost == "" {
		return
	}
	sb.WriteString("  graceful_shutdown:\n")
	fmt.Fprintf(sb, "    %sdev_main_ts:\n", workerHost)
	sb.WriteString("      pattern: 'enableShutdownHooks() + process.on(\"SIGTERM\", () => app.close())'\n")
	sb.WriteString("      rationale: 'rolling deploys send SIGTERM; in-flight NATS messages finish or get re-delivered via queue group'\n")
	sb.WriteString("\n")
}

// isSQLDatabaseType returns true when the service type names a managed SQL
// database. Kept narrow — only the types we actually support.
func isSQLDatabaseType(serviceType string) bool {
	base := baseTypeOf(serviceType)
	switch base {
	case svcPostgreSQL, svcMariaDB, svcMySQL:
		return true
	}
	return false
}

// isNATSType returns true when the service type names a NATS managed service.
func isNATSType(serviceType string) bool {
	return baseTypeOf(serviceType) == svcNATS
}

// baseTypeOf strips the "@version" suffix from a service type identifier.
func baseTypeOf(serviceType string) string {
	if idx := strings.Index(serviceType, "@"); idx > 0 {
		return serviceType[:idx]
	}
	return serviceType
}
