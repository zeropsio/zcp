package workflow

// stepDetails defines the 10 bootstrap steps in order.
// Skippable: mount-dev, discover-envs, deploy (managed-only fast path).
var stepDetails = []StepDetail{
	{
		Name:     "detect",
		Category: CategoryFixed,
		Guidance: `Call zerops_discover to inspect the current project.
Classify the project state:
- FRESH: no runtime services exist (only managed or none)
- PARTIAL: some services exist but import incomplete
- CONFORMANT: dev+stage naming pattern detected
- EXISTING: runtime services without dev/stage pattern

Route:
- FRESH → proceed normally through all steps
- PARTIAL → resume from failed step (check zerops_events for errors)
- CONFORMANT → skip to deploy step (services already exist)
- EXISTING → warn user about non-standard naming, suggest reset or manual approach`,
		Tools:        []string{"zerops_discover"},
		Verification: "Project state classified as FRESH/PARTIAL/CONFORMANT/EXISTING with evidence",
		Skippable:    false,
	},
	{
		Name:     "plan",
		Category: CategoryCreative,
		Guidance: `Identify the application stack from user intent:
1. Runtime services: language + framework (e.g., bun@1.2 + Hono, go@1 + net/http)
2. Managed services: databases, caches, storage (e.g., postgresql@16, valkey@7.2)
3. Environment mode: standard (dev+stage pairs) or simple (single service)

Hostname rules (STRICT):
- Only [a-z0-9], NO hyphens, NO underscores
- Max 25 characters, immutable after creation
- Dev pattern: {app}dev (e.g., "appdev")
- Stage pattern: {app}stage (e.g., "appstage")

Output: list of services with hostnames, types, and versions.
Validate all types against available stacks from zerops_knowledge.`,
		Tools:        []string{"zerops_knowledge"},
		Verification: "Service list with hostnames, types, versions documented",
		Skippable:    false,
	},
	{
		Name:     "load-knowledge",
		Category: CategoryFixed,
		Guidance: `Two MANDATORY knowledge calls:

1. Runtime briefing: zerops_knowledge runtime="{type}" services=["{managed1}", "{managed2}"]
   - Returns binding rules, ports, env vars, wiring patterns
   - Call once per unique runtime type

2. Infrastructure rules: zerops_knowledge scope="infrastructure"
   - Returns import.yml schema, zerops.yml schema, env var system
   - MUST be loaded before generating any YAML

Optional: zerops_knowledge recipe="{framework}" for framework-specific configs.

Do NOT proceed to generate-import without both calls completed.`,
		Tools:        []string{"zerops_knowledge"},
		Verification: "Runtime briefing loaded for each runtime type AND infrastructure scope loaded",
		Skippable:    false,
	},
	{
		Name:     "generate-import",
		Category: CategoryCreative,
		Guidance: `Generate import.yml following infrastructure rules from load-knowledge.

Dev services:
- enableSubdomainAccess: true
- startWithoutCode: true (allows service to start without initial deploy)
- minContainers: 1, maxContainers: 1

Stage services:
- enableSubdomainAccess: true
- Do NOT set startWithoutCode (starts on first deploy)
- minContainers: 1, maxContainers: auto-scale as needed

Managed services:
- Shared between dev and stage
- Use mode: NON_HA for dev environments
- Set appropriate versions (e.g., postgresql@16, valkey@7.2)

Validation checklist:
- All hostnames follow [a-z0-9] pattern
- Service types match available stacks
- No duplicate hostnames
- object-storage requires objectStorageSize field
- Preprocessor: #yamlPreprocessor=on if using <@...> functions`,
		Tools:        []string{"zerops_knowledge"},
		Verification: "import.yml generated with valid hostnames, types, and all required fields",
		Skippable:    false,
	},
	{
		Name:     "import-services",
		Category: CategoryFixed,
		Guidance: `Execute the import:

1. zerops_import content="<generated import.yml>"
2. Poll: zerops_process processId="<returned id>" until status is FINISHED
3. Verify: zerops_discover to confirm all services exist and states are correct
   - Dev runtime services: should be RUNNING (startWithoutCode: true)
   - Stage runtime services: should be NEW or READY_TO_DEPLOY
   - Managed services: should be RUNNING

If import fails, check zerops_events for error details.
Common failures: invalid hostname, unknown type, duplicate service name.`,
		Tools:        []string{"zerops_import", "zerops_process", "zerops_discover"},
		Verification: "All services imported and verified in expected states",
		Skippable:    false,
	},
	{
		Name:     "mount-dev",
		Category: CategoryFixed,
		Guidance: `Mount ONLY dev runtime service filesystems:

zerops_mount action="mount" serviceHostname="{devHostname}"

Rules:
- Mount each dev RUNTIME service (e.g., appdev, apidev)
- Do NOT mount stage services
- Do NOT mount managed services (postgresql, valkey, etc.)
- Do NOT mount shared-storage (it has no filesystem to mount)

After mounting, the service filesystem is available at the mount path
(typically /var/www/{hostname}/ or shown in mount output).

Skip this step if no runtime services exist (managed-only project).`,
		Tools:        []string{"zerops_mount"},
		Verification: "All dev runtime service filesystems mounted successfully",
		Skippable:    true,
	},
	{
		Name:     "discover-envs",
		Category: CategoryFixed,
		Guidance: `Discover actual environment variables for each managed service:

zerops_discover service="{hostname}" includeEnvs=true

For EACH managed service (db, cache, storage, etc.):
- Record exact env var names (connectionString, host, port, user, password, dbName)
- These are the REAL values set by Zerops, not guesses
- Use these exact names in zerops.yml envVariables section

This data MUST be available BEFORE writing any application code or zerops.yml.
Do NOT hardcode env var names — always discover them first.

Skip this step if no managed services exist.`,
		Tools:        []string{"zerops_discover"},
		Verification: "All managed service env vars discovered and documented",
		Skippable:    true,
	},
	{
		Name:     "deploy",
		Category: CategoryBranching,
		Guidance: `Deploy application code to all runtime services.

BRANCH by service count:
- 1 service pair (or inline): deploy directly in this conversation
- 2+ service pairs: spawn one subagent per service pair

For EACH runtime service pair (dev + stage):

1. Write zerops.yml with correct build/run commands and discovered env vars
2. Write application code with required endpoints:
   - GET / — app root
   - GET /health — health check (200 OK)
   - GET /status — connectivity proof (SELECT 1 for DB, PING for cache)
3. Deploy dev: zerops_deploy targetService="{devHostname}"
4. Verify dev: zerops_discover + HTTP check on zeropsSubdomain URL
5. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{devHostname}"
6. Deploy stage: zerops_deploy targetService="{stageHostname}"
7. Verify stage: same checks as dev
8. Enable subdomain: zerops_subdomain action="enable" serviceHostname="{stageHostname}"
9. If shared-storage is in the stack: after stage becomes ACTIVE, connect storage:
   zerops_manage action="connect-storage" serviceHostname="{stageHostname}" storageHostname="{storageHostname}"
   (Stage was READY_TO_DEPLOY during import, so import mount: did not apply to it)

Dev vs prod deploy differentiation:
| Aspect | Dev (source deploy) | Stage (build deploy) |
|--------|-------------------|---------------------|
| zerops.yml deployFiles | [.] (source) | build output only |
| Build step | optional/minimal | full build pipeline |
| Env vars | dev-friendly | production-ready |

Iteration loop (max 3 attempts per service):
- If deploy fails → check logs → fix → redeploy
- If /status fails → check connectivity → fix code → redeploy

Skip this step if no runtime services exist (managed-only project).`,
		Tools:        []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount"},
		Verification: "All runtime services deployed, /status endpoints returning 200 with connectivity proof",
		Skippable:    true,
	},
	{
		Name:     "verify",
		Category: CategoryFixed,
		Guidance: `Independent verification of ALL services — do NOT trust self-reports from deploy step.

For each runtime service:
1. zerops_discover service="{hostname}" — confirm status is RUNNING
2. HTTP check: curl/fetch the zeropsSubdomain URL
3. Check /status endpoint returns 200 with connectivity proof

For each managed service:
1. zerops_discover service="{hostname}" — confirm status is RUNNING

If any service fails verification:
- Record the failure in attestation (e.g., "3/5 services healthy, apidev failing")
- Do NOT block — the conductor accepts partial success
- The attestation captures the actual state`,
		Tools:        []string{"zerops_discover"},
		Verification: "All services independently verified with status documented",
		Skippable:    false,
	},
	{
		Name:     "report",
		Category: CategoryFixed,
		Guidance: `Present final results to the user:

Format:
- List each service with: hostname, type, status, URL (if applicable)
- Group by: runtime dev, runtime stage, managed
- Include zeropsSubdomain URLs for services with subdomain enabled
- Mention /status endpoint for connectivity monitoring

Summary:
- Total services created/verified
- Any skipped steps and reasons
- Any partial failures from deploy/verify steps

End with actionable next steps (e.g., "Your dev environment is ready at...")`,
		Tools:        []string{"zerops_discover"},
		Verification: "Final report presented to user with all service URLs and statuses",
		Skippable:    false,
	},
}
