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
- CONFORMANT: dev+stage naming pattern detected (e.g., appdev+appstage)
- NON_CONFORMANT: runtime services exist without dev/stage pattern

Route:
- FRESH → proceed normally through all steps
- CONFORMANT → skip to deploy step (services already exist)
- NON_CONFORMANT → warn user about non-standard naming, suggest reset or manual approach`,
		Tools:        []string{"zerops_discover"},
		Verification: "Project state classified as FRESH/CONFORMANT/NON_CONFORMANT with evidence",
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

ALL managed services require mode: NON_HA or HA — including object-storage and shared-storage. No exceptions.

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
		Guidance: `Generate import.yml ONLY — do NOT write zerops.yml or application code yet.

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
- Preprocessor: #yamlPreprocessor=on if using <@...> functions

Output: import.yml content only. Code and zerops.yml are written in the generate-code step AFTER env var discovery.`,
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
		Name:     "generate-code",
		Category: CategoryCreative,
		Guidance: `Write zerops.yml and application code to the mounted dev service filesystem.

PREREQUISITES (from prior steps):
- Dev services mounted at /var/www/{devHostname}/
- Env vars discovered from managed services

For EACH runtime service:

1. Write zerops.yml to mount path with setup entries for BOTH dev and stage hostnames
   - Dev: deployFiles: [.] (ALWAYS), source-mode start command
   - Stage: appropriate build output, compiled start command
   - envVariables: ONLY use variables discovered in discover-envs step
2. Write application code with required endpoints:
   - GET / — app root
   - GET /health — health check (200 OK)
   - GET /status — connectivity proof (SELECT 1 for DB, PING for cache)
3. Write .gitignore appropriate for the runtime

Use zerops_knowledge for runtime-specific build/deploy patterns.

MANDATORY PRE-DEPLOY CHECK (do NOT proceed to deploy until all pass):
- zerops.yml has setup entry for EVERY planned runtime hostname
- Dev setup uses deployFiles: [.] — NO EXCEPTIONS
- run.start is the RUN command (not a build tool like go build)
- run.ports port matches what the app listens on
- envVariables ONLY uses variables discovered in discover-envs step
- App binds to 0.0.0.0:{port}, NOT localhost

Skip this step if no runtime services exist (managed-only project).`,
		Tools:        []string{"zerops_knowledge"},
		Verification: "zerops.yml and app code written to mount path with correct env var mappings and /status endpoint",
		Skippable:    true,
	},
	{
		Name:     "deploy",
		Category: CategoryBranching,
		Guidance: `Deploy application code to all runtime services.

BRANCH by service count:
- 1 service pair (or inline): deploy directly in this conversation
- 2+ service pairs: spawn one subagent per service pair

Bootstrap deploys ALWAYS use SSH mode (sourceService + targetService).
NEVER use local mode (targetService only) — git operations fail on SSHFS mounts.

For EACH runtime service pair (dev + stage):

1. Deploy dev: zerops_deploy sourceService="{devHostname}" targetService="{devHostname}" freshGit=true
2. Enable subdomain + verify dev: zerops_subdomain action="enable" → zerops_verify
3. Deploy stage: zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}" freshGit=true
4. Enable subdomain + verify stage: zerops_subdomain action="enable" → zerops_verify
5. If shared-storage is in the stack: after stage becomes ACTIVE, connect storage:
   zerops_manage action="connect-storage" serviceHostname="{stageHostname}" storageHostname="{storageHostname}"

Platform rule: SSHFS mounts do not support git — always deploy via SSH mode.

Iteration loop (max 3 attempts per service):
- If deploy fails → check logs → fix → redeploy
- If /status fails → check connectivity → fix code → redeploy

Skip this step if no runtime services exist (managed-only project).`,
		Tools:        []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify"},
		Verification: "All runtime services deployed, /status endpoints returning 200 with connectivity proof",
		Skippable:    true,
	},
	{
		Name:     "verify",
		Category: CategoryFixed,
		Guidance: `Independent verification of ALL services — do NOT trust self-reports from deploy step.

For each runtime service:
1. zerops_discover service="{hostname}" — confirm status is RUNNING
2. zerops_subdomain action="enable" — get subdomainUrls from response, HTTP check
3. Check /status endpoint returns 200 with connectivity proof

For each managed service:
1. zerops_discover service="{hostname}" — confirm status is RUNNING

If any service fails verification:
- Record the failure in attestation (e.g., "3/5 services healthy, apidev failing")
- Do NOT block — the conductor accepts partial success
- The attestation captures the actual state`,
		Tools:        []string{"zerops_discover", "zerops_verify"},
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
- Include subdomainUrls from zerops_subdomain enable responses
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
