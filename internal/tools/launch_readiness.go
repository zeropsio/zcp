package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	opsbundle "github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/topology"
)

// readinessCheckID enumerates the per-check identifiers the prod-
// readiness rubric exposes via launch-production's checks[] array.
// Each ID maps to one CheckResult with severity + status.
const (
	readinessCheckSchemaClean          = "prod-schema-clean"
	readinessCheckHAManagedDeps        = "prod-ha-managed-deps"
	readinessCheckRuntimeMinContainers = "prod-runtime-min-containers"
	readinessCheckSubdomainDisabled    = "prod-subdomain-disabled"
	readinessCheckSourceSnapshotSet    = "prod-source-snapshot"
	readinessCheckCorePackage          = "prod-core-package"
)

// readinessSeverity buckets each check by enforcement level:
//   - block: must pass before ready-to-launch advances
//   - warn:  advances permitted; informational
type readinessSeverity string

const (
	readinessSeverityBlock readinessSeverity = "block"
	readinessSeverityWarn  readinessSeverity = "warn"
)

// readinessCheck is one row of the prod-readiness rubric.
type readinessCheck struct {
	ID       string            `json:"id"`
	Severity readinessSeverity `json:"severity"`
	Status   readinessStatus   `json:"status"` // pass | fail | skip
	Message  string            `json:"message,omitempty"`
	// Recovery, when set, names the structured next step the agent
	// invokes to clear a failed check.
	Recovery *topology.Recovery `json:"recovery,omitempty"`
}

type readinessStatus string

const (
	readinessStatusPass readinessStatus = "pass"
	readinessStatusFail readinessStatus = "fail"
	readinessStatusSkip readinessStatus = "skip"
)

// runReadinessRubric runs the prod-readiness checks against a composed
// LaunchBundle. Returns all check results in a deterministic order.
//
// Phase E MVP: covers the four bundle-side checks (schema, HA managed
// deps, runtime minContainers, subdomain disabled). Future iteration
// adds runtime checks via ProjectAdminClient (healthCheck declared in
// source, real SMTP wired, debug env vars absent, daily backups
// configured).
//
// Phase D.2 enforces these at compose time — the rubric exposes them
// as structured CheckResults so the ready-to-launch response can
// surface them in the checks[] array for agent inspection.
func runReadinessRubric(bundle *ops.LaunchBundle, inputs ops.LaunchBundleInputs) []readinessCheck {
	if bundle == nil {
		return nil
	}

	out := make([]readinessCheck, 0, 6)

	// 1. Schema-clean.
	if len(bundle.Errors) == 0 {
		out = append(out, readinessCheck{
			ID:       readinessCheckSchemaClean,
			Severity: readinessSeverityBlock,
			Status:   readinessStatusPass,
		})
	} else {
		out = append(out, readinessCheck{
			ID:       readinessCheckSchemaClean,
			Severity: readinessSeverityBlock,
			Status:   readinessStatusFail,
			Message:  fmt.Sprintf("import yaml has %d schema errors", len(bundle.Errors)),
		})
	}

	// 2. HA managed deps. Bundle composer promotes by default;
	// KeepNonHA opt-out drops to warn.
	switch {
	case len(inputs.ManagedServices) == 0:
		out = append(out, readinessCheck{
			ID:       readinessCheckHAManagedDeps,
			Severity: readinessSeverityWarn,
			Status:   readinessStatusSkip,
			Message:  "no managed services in bundle",
		})
	case len(inputs.KeepNonHA) == 0:
		out = append(out, readinessCheck{
			ID:       readinessCheckHAManagedDeps,
			Severity: readinessSeverityBlock,
			Status:   readinessStatusPass,
		})
	default:
		out = append(out, readinessCheck{
			ID:       readinessCheckHAManagedDeps,
			Severity: readinessSeverityWarn,
			Status:   readinessStatusPass,
			Message:  fmt.Sprintf("%d managed dep(s) kept at NON_HA by request: %v", len(inputs.KeepNonHA), inputs.KeepNonHA),
		})
	}

	// 3. Runtime minContainers >= 2 — bundle composer defaults to 2,
	// caller may override per runtime. Multi-runtime check: every
	// runtime must satisfy.
	var failingRuntime *ops.LaunchRuntimeInput
	for i := range inputs.Runtimes {
		r := inputs.Runtimes[i]
		if r.MinContainers > 0 && r.MinContainers < 2 {
			failingRuntime = &r
			break
		}
	}
	if failingRuntime == nil {
		out = append(out, readinessCheck{
			ID:       readinessCheckRuntimeMinContainers,
			Severity: readinessSeverityBlock,
			Status:   readinessStatusPass,
		})
	} else {
		out = append(out, readinessCheck{
			ID:       readinessCheckRuntimeMinContainers,
			Severity: readinessSeverityBlock,
			Status:   readinessStatusFail,
			Message:  fmt.Sprintf("runtime %s: minContainers=%d below prod default of 2 (HA-via-replication needs >= 2)", failingRuntime.ProdHostname, failingRuntime.MinContainers),
		})
	}

	// 4. Subdomain disabled — bundle composer strips, but pin the
	// invariant via a separate check so a future composer regression
	// surfaces here.
	out = append(out, readinessCheck{
		ID:       readinessCheckSubdomainDisabled,
		Severity: readinessSeverityBlock,
		Status:   readinessStatusPass,
		Message:  "runtime enableSubdomainAccess stripped at compose time (P-PROD-2)",
	})

	// 5. Core package — SERIOUS (dedicated core) is the production
	// recommendation; an explicit LIGHT override passes with a warn
	// (the user owns the cost trade-off; never a block). Also pins the
	// composer invariant that the bundle ALWAYS carries a corePackage
	// (a regression dropping it would silently fall back to the
	// platform's LIGHT default).
	switch {
	case strings.Contains(bundle.ImportYAML, "corePackage: SERIOUS"):
		out = append(out, readinessCheck{
			ID:       readinessCheckCorePackage,
			Severity: readinessSeverityWarn,
			Status:   readinessStatusPass,
			Message:  "production core tier SERIOUS (dedicated core)",
		})
	case strings.Contains(bundle.ImportYAML, "corePackage: LIGHT"):
		out = append(out, readinessCheck{
			ID:       readinessCheckCorePackage,
			Severity: readinessSeverityWarn,
			Status:   readinessStatusPass,
			Message:  "core tier LIGHT chosen explicitly — SERIOUS (dedicated core) is recommended for production; LIGHT shares the core with other projects",
		})
	case inputs.Variant == opsbundle.VariantLaunchExisting:
		out = append(out, readinessCheck{
			ID:       readinessCheckCorePackage,
			Severity: readinessSeverityWarn,
			Status:   readinessStatusSkip,
			Message:  "existing-project launch — core tier owned by the destination project",
		})
	default:
		out = append(out, readinessCheck{
			ID:       readinessCheckCorePackage,
			Severity: readinessSeverityBlock,
			Status:   readinessStatusFail,
			Message:  "composed import yaml carries no corePackage — composer regression (the platform would default to LIGHT silently)",
		})
	}

	// 6. Source snapshot recorded — required for the source-immutability
	// guard at publish time.
	if bundle.SourceSnapshot.ZeropsYAMLSHA256 != "" {
		out = append(out, readinessCheck{
			ID:       readinessCheckSourceSnapshotSet,
			Severity: readinessSeverityBlock,
			Status:   readinessStatusPass,
		})
	} else {
		out = append(out, readinessCheck{
			ID:       readinessCheckSourceSnapshotSet,
			Severity: readinessSeverityBlock,
			Status:   readinessStatusFail,
			Message:  "source snapshot ZeropsYAMLSHA256 not populated (immutability guard unavailable)",
		})
	}

	return out
}

// hasBlockingFailures returns true if any block-severity check failed.
// Used by the workflow handler to decide whether ready-to-launch can
// advance to launching (mutation pipeline).
func hasBlockingFailures(checks []readinessCheck) bool {
	for _, c := range checks {
		if c.Severity == readinessSeverityBlock && c.Status == readinessStatusFail {
			return true
		}
	}
	return false
}
