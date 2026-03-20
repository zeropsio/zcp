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
var stepDetails = []StepDetail{
	{
		Name:         StepDiscover,
		Category:     CategoryFixed,
		Tools:        []string{"zerops_discover", "zerops_knowledge", "zerops_workflow"},
		Verification: "SUCCESS WHEN: project state classified (FRESH/CONFORMANT/NON_CONFORMANT), stack components identified, plan submitted via zerops_workflow action=complete step=discover with valid targets.",
		Skippable:    false,
	},
	{
		Name:         StepProvision,
		Category:     CategoryFixed,
		Tools:        []string{"zerops_import", "zerops_process", "zerops_discover", "zerops_mount"},
		Verification: "SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND dev filesystems mounted AND env vars recorded in session state.",
		Skippable:    false,
	},
	{
		Name:         StepGenerate,
		Category:     CategoryCreative,
		Tools:        []string{"zerops_knowledge"},
		Verification: "SUCCESS WHEN: zerops.yml exists with dev setup entry AND env var references match discovered variables AND app code exposes /health and /status endpoints.",
		Skippable:    true,
	},
	{
		Name:         StepDeploy,
		Category:     CategoryBranching,
		Tools:        []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify", "zerops_manage"},
		Verification: "SUCCESS WHEN: all runtime services deployed (RUNNING status) AND subdomains enabled AND zerops_verify returns healthy for each service.",
		Skippable:    true,
	},
	{
		Name:         StepVerify,
		Category:     CategoryFixed,
		Tools:        []string{"zerops_discover", "zerops_verify"},
		Verification: "SUCCESS WHEN: zerops_verify batch confirms all plan targets healthy AND /status endpoints return connectivity proof AND final report presented with URLs.",
		Skippable:    false,
	},
	{
		Name:         StepStrategy,
		Category:     CategoryFixed,
		Tools:        []string{"zerops_workflow"},
		Verification: "SUCCESS WHEN: strategy recorded for all runtime services via action=complete step=strategy with strategies param. NEXT: bootstrap complete.",
		Skippable:    true,
	},
}
