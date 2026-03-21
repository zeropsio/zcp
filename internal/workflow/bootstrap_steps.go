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
// Skippable: generate, deploy, close (managed-only fast path).
//
// Verification strings describe what the step's StepChecker enforces.
// Checkers are the real gates — attestations are audit trail only.
var stepDetails = []StepDetail{
	{
		Name:         StepDiscover,
		Tools:        []string{"zerops_discover", "zerops_knowledge", "zerops_workflow"},
		Verification: "SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).",
		Skippable:    false,
	},
	{
		Name:         StepProvision,
		Tools:        []string{"zerops_import", "zerops_process", "zerops_discover", "zerops_mount"},
		Verification: "SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND service types match plan AND managed dependency env vars recorded in session state.",
		Skippable:    false,
	},
	{
		Name:         StepGenerate,
		Tools:        []string{"zerops_knowledge"},
		Verification: "SUCCESS WHEN: zerops.yml exists with setup entry for each target AND env var references match discovered variables AND run.start present (dynamic runtimes) AND deployFiles set (dev uses [.]) AND ports defined.",
		Skippable:    true,
	},
	{
		Name:         StepDeploy,
		Tools:        []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify", "zerops_manage"},
		Verification: "SUCCESS WHEN: all runtime services deployed, accessible, AND healthy (VerifyAll: HTTP, logs, startup, subdomains enabled).",
		Skippable:    true,
	},
	{
		Name:         StepClose,
		Tools:        []string{"zerops_workflow"},
		Verification: "SUCCESS WHEN: bootstrap administratively closed (metas written, transition presented).",
		Skippable:    true,
	},
}
