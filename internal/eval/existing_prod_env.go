package eval

// ExistingProjectIDEnvVar and ExistingProdTokenEnvVar name the standing
// existing-production project and its project-scoped token that
// launch-to-existing-prod-project reads. Same pass-through shape as
// GitHubPATEnvVar: the farm controller injects each into a run project only
// when the scenario names it in requiredEnvVars, and wrapper.sh redacts the
// token before upload. docs/spec-eval-farm.md §2.4/§3.3.
const (
	ExistingProjectIDEnvVar = "ZCP_E2E_EXISTING_PROJECT_ID"
	ExistingProdTokenEnvVar = "ZCP_E2E_EXISTING_PROD_TOKEN"
)
