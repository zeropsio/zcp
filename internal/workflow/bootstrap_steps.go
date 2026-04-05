package workflow

// Step name constants.
const (
	StepDiscover  = "discover"
	StepProvision = "provision"
	StepGenerate  = "generate"
	StepDeploy    = "deploy"
	StepClose     = "close"
)

// stepDetails defines the 5 bootstrap steps in order.
// Skip rules enforced by validateSkip(): discover/provision mandatory,
// generate/deploy/close skippable for managed-only.
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
		Name:         StepGenerate,
		Tools:        []string{"zerops_knowledge"},
		Verification: "SUCCESS WHEN: zerops.yaml exists with setup entry for each target AND env var references match discovered variables AND run.start present (dynamic runtimes) AND deployFiles set (dev uses [.]) AND ports defined.",
	},
	{
		Name:         StepDeploy,
		Tools:        []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify", "zerops_manage"},
		Verification: "SUCCESS WHEN: all runtime services deployed, accessible, AND healthy (VerifyAll: HTTP, logs, startup, subdomains enabled).",
	},
	{
		Name:         StepClose,
		Tools:        []string{"zerops_workflow"},
		Verification: "SUCCESS WHEN: bootstrap administratively closed (metas written, transition presented).",
	},
}
