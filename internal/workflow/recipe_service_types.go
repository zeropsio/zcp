package workflow

// Canonical recipe setup names. Minimal recipes use dev+prod; showcase recipes
// add worker. All bootstrap/deploy guidance and checkers reference them.
const (
	RecipeSetupDev    = "dev"    // development workspace — idle start, full source, no healthCheck
	RecipeSetupProd   = "prod"   // production/stage/simple — real start, healthCheck
	RecipeSetupWorker = "worker" // showcase only — background job processor, no HTTP
)
