package workflow

// Step name constants.
const (
	StepDiscover  = "discover"
	StepProvision = "provision"
	StepGenerate  = "generate"
	StepDeploy    = "deploy"
	StepVerify    = "verify"
	StepStrategy  = "strategy"
)

// stepDetails defines the 6 consolidated bootstrap steps in order.
// Skippable: generate, deploy, strategy (managed-only fast path).
//
// Verification strings describe what the step's StepChecker enforces.
// Checkers are the real gates — attestations are audit trail only.
var stepDetails = []StepDetail{
	{
		Name:         StepDiscover,
		Category:     CategoryFixed,
		Tools:        []string{"zerops_discover", "zerops_knowledge", "zerops_workflow"},
		Verification: "SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).",
		Skippable:    false,
	},
	{
		Name:         StepProvision,
		Category:     CategoryFixed,
		Tools:        []string{"zerops_import", "zerops_process", "zerops_discover", "zerops_mount"},
		Verification: "SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND service types match plan AND managed dependency env vars recorded in session state.",
		Skippable:    false,
	},
	{
		Name:         StepGenerate,
		Category:     CategoryCreative,
		Tools:        []string{"zerops_knowledge"},
		Verification: "SUCCESS WHEN: zerops.yml exists with setup entry for each target AND env var references match discovered variables AND run.start present (dynamic runtimes) AND deployFiles set (dev uses [.]) AND ports defined.",
		Skippable:    true,
	},
	{
		Name:         StepDeploy,
		Category:     CategoryBranching,
		Tools:        []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify", "zerops_manage"},
		Verification: "SUCCESS WHEN: all runtime services deployed (RUNNING status) AND subdomains enabled for services with ports.",
		Skippable:    true,
	},
	{
		Name:         StepVerify,
		Category:     CategoryFixed,
		Tools:        []string{"zerops_discover", "zerops_verify"},
		Verification: "SUCCESS WHEN: zerops_verify confirms all plan targets healthy (individual checks: service_running, error_logs, startup_detected, http_root, http_status).",
		Skippable:    false,
	},
	{
		Name:         StepStrategy,
		Category:     CategoryFixed,
		Tools:        []string{"zerops_workflow"},
		Verification: "SUCCESS WHEN: valid strategy (push-dev, ci-cd, manual) assigned to every runtime target in plan.",
		Skippable:    true,
	},
}
