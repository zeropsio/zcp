package workflow

// Recipe step name constants.
const (
	RecipeStepResearch  = "research"
	RecipeStepProvision = "provision"
	RecipeStepGenerate  = "generate"
	RecipeStepDeploy    = "deploy"
	RecipeStepFinalize  = "finalize"
	RecipeStepClose     = "close"
)

// recipeStepDetails defines the 6 recipe steps in order.
// Only close is skippable. All others are mandatory.
var recipeStepDetails = []StepDetail{
	{
		Name:         RecipeStepResearch,
		Tools:        []string{"zerops_knowledge", "zerops_discover", "zerops_workflow"},
		Verification: "SUCCESS WHEN: RecipePlan submitted with all research fields, types validated against live catalog, decision branches resolved.",
	},
	{
		Name:         RecipeStepProvision,
		Tools:        []string{"zerops_import", "zerops_process", "zerops_discover", "zerops_mount"},
		Verification: "SUCCESS WHEN: all workspace services exist with expected status AND types match AND managed dep env vars recorded.",
	},
	{
		Name:         RecipeStepGenerate,
		Tools:        []string{"zerops_knowledge"},
		Verification: "SUCCESS WHEN: zerops.yaml valid with base+prod+dev setups AND app README has integration-guide fragment with commented zerops.yaml AND knowledge-base fragment exists with Gotchas section AND comment ratio >= 0.3.",
	},
	{
		Name:         RecipeStepDeploy,
		Tools:        []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify", "zerops_manage"},
		Verification: "SUCCESS WHEN: all runtime services deployed, accessible, AND healthy.",
	},
	{
		Name:         RecipeStepFinalize,
		Tools:        []string{"zerops_workflow"},
		Verification: "SUCCESS WHEN: all recipe repo files generated (6 import.yaml + 7 READMEs), fragment tags valid, YAML valid, env scaling correct, no placeholders.",
	},
	{
		Name:         RecipeStepClose,
		Tools:        []string{"zerops_workflow"},
		Verification: "SUCCESS WHEN: recipe administratively closed, publish commands presented.",
	},
}
