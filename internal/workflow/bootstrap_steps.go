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
		Name:     StepDiscover,
		Category: CategoryFixed,
		Guidance: `Discover project state and plan services.
1. Call zerops_discover to inspect the current project
2. Classify: FRESH (no runtime services), CONFORMANT (dev+stage pattern), NON_CONFORMANT
3. Identify runtime + managed services from user intent
4. Validate types against availableStacks
5. PRESENT the plan to user for confirmation before submitting:
   "I'll set up: [services]. Mode: [standard/dev/simple]. OK?"
6. Submit confirmed plan via zerops_workflow action="complete" step="discover" plan=[...]
NOTE: Platform knowledge is delivered with each step guide automatically.
For specific frameworks: zerops_knowledge recipe="{name}"

CONFORMANT projects with matching stack: route to deploy workflow instead.
NON_CONFORMANT: ASK user before any changes.`,
		Tools:        []string{"zerops_discover", "zerops_knowledge", "zerops_workflow"},
		Verification: "SUCCESS WHEN: project state classified (FRESH/CONFORMANT/NON_CONFORMANT), stack components identified, plan submitted via zerops_workflow action=complete step=discover with valid targets. NEXT: proceed to provision step.",
		Skippable:    false,
	},
	{
		Name:     StepProvision,
		Category: CategoryFixed,
		Guidance: `Generate import.yml, import services, mount dev filesystems, discover env vars.
1. Generate import.yml with correct hostnames, types, enableSubdomainAccess
2. zerops_import to create services, poll process to completion
3. zerops_discover to verify all services exist in expected states
4. zerops_mount dev runtime filesystems (NOT stage, NOT managed)
5. zerops_discover includeEnvs=true for each managed service
6. Record discovered env var names for use in generate step`,
		Tools:        []string{"zerops_import", "zerops_process", "zerops_discover", "zerops_mount"},
		Verification: "SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND dev filesystems mounted AND env vars recorded in session state. NEXT: proceed to generate step.",
		Skippable:    false,
	},
	{
		Name:     StepGenerate,
		Category: CategoryCreative,
		Guidance: `Write zerops.yml and application code to mounted filesystem.
Platform rules, runtime knowledge, and discovered env vars are included below.
Mode-specific zerops.yml rules (standard/dev/simple) are included in the detailed guide.
PREREQUISITES: services mounted, env vars discovered from provision step.
1. Write zerops.yml entry per the mode rules below (standard/dev = noop start, simple = real start)
2. deployFiles: [.] — ALWAYS for self-deploying services
3. envVariables: map discovered vars using ${hostname_varName} references
4. Write application code with GET /, GET /health, GET /status endpoints
5. App reads env vars from runtime's native env var API

Env vars activate after deploy. Write code, deploy, then start and test.
Skip if no runtime services exist (managed-only project).`,
		Tools:        []string{"zerops_knowledge"},
		Verification: "SUCCESS WHEN: zerops.yml exists with dev setup entry AND env var references match discovered variables AND app code exposes /health and /status endpoints. NEXT: proceed to deploy step.",
		Skippable:    true,
	},
	{
		Name:     StepDeploy,
		Category: CategoryBranching,
		Guidance: `Deploy to all runtime services, start servers, enable subdomains, verify.
INVARIANT: zerops_deploy to dev restarts container with "zsc noop --silent" — server DIES.
You MUST start the server via SSH after every dev deploy, before zerops_verify.
Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): skip — auto-starts.

For EACH runtime service pair (dev + stage):
1. Deploy dev: zerops_deploy targetService="{devHostname}"
2. Start dev server via SSH (deploy killed it — kill-then-start pattern)
3. Verify dev: zerops_subdomain action="enable", zerops_verify
4. Generate stage entry in zerops.yml (now you know what works from dev)
5. Deploy stage: zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"
6. Enable subdomain for stage, zerops_verify
7. Connect shared-storage if applicable

Iteration loop (max 3 per service): fail -> fix -> redeploy -> start server -> re-verify.
Skip if no runtime services exist.`,
		Tools:        []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify", "zerops_manage"},
		Verification: "SUCCESS WHEN: all runtime services deployed (RUNNING status) AND subdomains enabled AND zerops_verify returns healthy for each service. NEXT: proceed to verify step.",
		Skippable:    true,
	},
	{
		Name:     StepVerify,
		Category: CategoryFixed,
		Guidance: `Independent verification and final report.
1. zerops_verify (batch) — verify all plan target services
2. Check /status endpoints for connectivity proof
3. Present final results: hostnames, types, status, URLs
4. Group by: runtime dev, runtime stage, managed
5. Include subdomain URLs and actionable next steps`,
		Tools:        []string{"zerops_discover", "zerops_verify"},
		Verification: "SUCCESS WHEN: zerops_verify batch confirms all plan targets healthy AND /status endpoints return connectivity proof AND final report presented with URLs. NEXT: proceed to strategy step.",
		Skippable:    false,
	},
	{
		Name:     StepStrategy,
		Category: CategoryFixed,
		Guidance: `Ask user to choose deployment strategy for each runtime service.
Options: push-dev (SSH push, dev-first), ci-cd (Git pipeline), manual (monitoring only).
Present options with trade-offs. Record choice via zerops_workflow action="complete" step="strategy".
Skip this step for managed-only projects (no runtime services).`,
		Tools:        []string{"zerops_workflow"},
		Verification: "SUCCESS WHEN: strategy recorded for all runtime services via action=complete step=strategy with strategies param. NEXT: bootstrap complete.",
		Skippable:    true,
	},
}
