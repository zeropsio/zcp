package recipe

import "fmt"

// devEngineVersion is the placeholder build version for non-release
// builds (no ldflags injection). Mirrors internal/server.Version's
// zero-value; gate skips when running against a dev binary.
const devEngineVersion = "dev"

func gateEngineVersionStamped(ctx GateContext) []Violation {
	if ctx.EngineVersion == devEngineVersion {
		return nil
	}
	if ctx.Plan == nil || ctx.Plan.EngineVersion == "" {
		return []Violation{{
			Code:     "engine-version-not-stamped",
			Path:     "plan.json",
			Severity: SeverityBlocking,
			Message:  "plan.EngineVersion is empty. Recreate the session via action=start with the current zcp binary so the engine version is recorded for downstream validation.",
		}}
	}
	if ctx.Plan.EngineVersion != ctx.EngineVersion {
		return []Violation{{
			Code:     "engine-version-mismatch",
			Path:     "plan.json",
			Severity: SeverityBlocking,
			Message: fmt.Sprintf(
				"plan was authored under engine version %q; current zcp binary is %q. Rebuild the dev-container zcp binary (or recreate the session under the current binary) before continuing — silent version drift cost run-36 a full dogfood.",
				ctx.Plan.EngineVersion, ctx.EngineVersion,
			),
		}}
	}
	return nil
}
