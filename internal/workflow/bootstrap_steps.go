package workflow

// Step name constants.
const (
	StepDiscover  = "discover"
	StepProvision = "provision"
	StepClose     = "close"
)

// stepDetails defines the 3 bootstrap steps in order (Option A infra-only).
// Bootstrap stops at provisioning + registration — code generation and deploy
// are owned by the develop flow. Skip rules enforced by validateSkip():
// discover/provision mandatory, close skippable for managed-only plans.
//
// Verification strings describe what the step's StepChecker enforces.
// Checkers are the real gates — attestations are audit trail only.
var stepDetails = []StepDetail{
	{
		Name:         StepDiscover,
		Tools:        []string{"zerops_discover", "zerops_knowledge", "zerops_workflow"},
		Verification: "SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).",
	},
	{
		Name:         StepProvision,
		Tools:        []string{"zerops_import", "zerops_process", "zerops_discover"},
		Verification: "SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND service types match plan AND managed dependency env vars recorded in session state. Runtime services are auto-mounted on completion.",
	},
	{
		Name:         StepClose,
		Tools:        []string{"zerops_workflow"},
		Verification: "SUCCESS WHEN: bootstrap administratively closed (metas written, transition to develop presented).",
	},
}
