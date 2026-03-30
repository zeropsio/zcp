package eval

// InstructionVariant defines an MCP instruction text variant for A/B testing.
// All variants share the SAME process: zerops_discover first, then workflow.
// They differ only in HOW this is communicated (framing).
type InstructionVariant struct {
	ID          string // short identifier
	Name        string // human-readable name
	Base        string // baseInstructions replacement
	Container   string // containerEnvironment replacement
	Local       string // localEnvironment replacement
	Description string // what framing this variant tests
}

// InstructionScenario defines a user prompt to test against.
type InstructionScenario struct {
	ID     string // short identifier
	Prompt string // what the user says
}

// sharedContainer is the container environment text shared by all variants.
const sharedContainer = `
Control plane container — manages OTHER services, does not serve traffic.
Files: /var/www/{hostname}/ = SSHFS mount to live service (not local). Commands: ssh {hostname} "..."
Edits on mount survive restarts but not deploys.`

// sharedLocal is the local environment text shared by all variants.
const sharedLocal = `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yml at repo root, each deploy = new container).`

// InstructionVariants returns instruction text variants to evaluate.
// All variants encode the same process (discover → workflow → code).
// They differ in framing: rule, information gap, process, consequence, etc.
// Each must fit in ~700 bytes (leaving ~1300 for dynamic content within 2KB MCP cap).
func InstructionVariants() []InstructionVariant {
	return []InstructionVariant{
		{
			ID:          "rule",
			Name:        "Rule-based (current)",
			Description: "Direct rule: before ANY work, start a workflow",
			Base: `ZCP manages Zerops PaaS infrastructure.
Before ANY work on service code (reading, debugging, fixing, deploying), start a workflow:
  zerops_workflow action="start" workflow="..."
  deploy — deploy, fix, or investigate services
  bootstrap — create/adopt services
  cicd — set up CI/CD pipelines
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_deploy (manual strategy), zerops_discover, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
		{
			ID:          "info-gap",
			Name:        "Information gap",
			Description: "You're missing platform info not in source files — discover first",
			Base: `ZCP manages Zerops PaaS infrastructure.
Service code depends on platform config NOT in source files (env vars, runtime settings, deploy constraints). You will produce incorrect analysis without this context.
  zerops_discover — see services, state, env vars (ALWAYS call first)
  zerops_workflow action="start" workflow="deploy|bootstrap|cicd" — get full platform context
  deploy — deploy, fix, or investigate    bootstrap — create/adopt    cicd — CI/CD
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
		{
			ID:          "process-123",
			Name:        "Numbered process",
			Description: "Explicit 1-2-3 steps — discover, workflow, then code",
			Base: `ZCP manages Zerops PaaS infrastructure.
For ANY task involving services, follow this process:
1. zerops_discover — see what services exist, their state, env vars
2. zerops_workflow action="start" workflow="deploy|bootstrap|cicd" — get platform context
3. Then read/edit code with full understanding
Workflows: deploy (fix/investigate/deploy), bootstrap (create/adopt), cicd (CI/CD)
Direct tools: zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
		{
			ID:          "consequence",
			Name:        "Consequence-based",
			Description: "Warns about what goes wrong without context",
			Base: `ZCP manages Zerops PaaS infrastructure.
Files on services are live — editing without platform context causes incorrect fixes. Env vars, runtime config, and deploy constraints are NOT in source code.
  zerops_discover — call FIRST (shows services, env vars, state)
  zerops_workflow action="start" workflow="deploy|bootstrap|cicd" — provides platform docs and constraints
  deploy — fix, investigate, deploy    bootstrap — create/adopt    cicd — CI/CD
Direct tools: zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
		{
			ID:          "identity",
			Name:        "Orchestrator identity",
			Description: "You are the orchestrator — first action is always discover",
			Base: `ZCP manages Zerops PaaS infrastructure. You are the orchestrator — you manage services, not code directly.
Your first action for ANY task: zerops_discover (understand services and state).
Then: zerops_workflow action="start" workflow="deploy|bootstrap|cicd" for platform context.
  deploy — fix, investigate, deploy    bootstrap — create/adopt    cicd — CI/CD
Direct tools: zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
		{
			ID:          "discover-gate",
			Name:        "Discover as gate",
			Description: "zerops_discover is the mandatory entry point — everything starts there",
			Base: `ZCP manages Zerops PaaS infrastructure.
FIRST STEP (always): zerops_discover — shows services, state, env vars, what needs attention.
THEN: zerops_workflow action="start" workflow="deploy|bootstrap|cicd" — platform context for code work.
  deploy — fix, investigate, or deploy code
  bootstrap — create new or adopt existing services
  cicd — set up CI/CD pipelines
Without discover you don't know the platform state. Without workflow you lack runtime docs and constraints.
Direct tools: zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
		{
			ID:          "question",
			Name:        "Self-check questions",
			Description: "LLM asks itself questions before acting",
			Base: `ZCP manages Zerops PaaS infrastructure.
Before working on any service, check:
- Do I know what services exist and their state? → zerops_discover (call first)
- Do I have platform context for this service? → zerops_workflow action="start" workflow="deploy"
- Am I editing a live mount (/var/www/{hostname}/)? → these are live services, not local files
Workflows: deploy (fix/investigate/deploy), bootstrap (create/adopt), cicd (CI/CD)
Direct tools: zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
		{
			ID:          "blunt",
			Name:        "Blunt stop",
			Description: "STOP directive — do not touch files until discover+workflow done",
			Base: `ZCP manages Zerops PaaS infrastructure.
STOP. Do not Read, Edit, or Grep service files until you have:
1. Called zerops_discover (shows services, env vars, platform state)
2. Started zerops_workflow action="start" workflow="deploy" (provides runtime docs and constraints)
Other workflows: bootstrap (create/adopt services), cicd (CI/CD setup)
Direct tools: zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
		{
			ID:          "minimal",
			Name:        "Ultra-minimal",
			Description: "Absolute minimum — one-liner, maximize dynamic content space",
			Base: `ZCP manages Zerops PaaS. Always: zerops_discover first, then zerops_workflow before any code work.
Workflows: deploy (fix/investigate/deploy), bootstrap (create/adopt), cicd. Direct: zerops_scale, zerops_manage, zerops_env, zerops_knowledge.`,
			Container: `
Control plane. /var/www/{hostname}/ = live SSHFS mount. ssh {hostname} for commands.`,
			Local: `
Local dev. Deploy: zcli push. Infra on Zerops.`,
		},
		{
			ID:          "checklist",
			Name:        "Pre-flight checklist",
			Description: "Checkbox-style checklist before code work",
			Base: `ZCP manages Zerops PaaS infrastructure.
Pre-flight (complete before ANY service code work):
[ ] zerops_discover — I know services, state, env vars
[ ] zerops_workflow started — I have platform context (runtime docs, constraints)
Workflows: deploy (fix/investigate/deploy), bootstrap (create/adopt), cicd (CI/CD)
Direct tools (skip checklist): zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_knowledge`,
			Container: sharedContainer,
			Local:     sharedLocal,
		},
	}
}

// InstructionScenarios returns user prompts to test each variant against.
func InstructionScenarios() []InstructionScenario {
	return []InstructionScenario{
		{ID: "audit-cs", Prompt: "udělej audit kódu u kamarádky"},
		{ID: "fix-cs", Prompt: "oprav bug v kamarádce — cyklí se zvuk na mobilu"},
		{ID: "add-feature-cs", Prompt: "přidej autentizaci na websocket v kamarádce"},
		{ID: "look-cs", Prompt: "podívej se co je v kódu kamarádky"},
		{ID: "audit-en", Prompt: "audit the kamaradka service code"},
		{ID: "fix-en", Prompt: "fix the audio echo loop bug in kamaradka"},
		{ID: "create-en", Prompt: "create a new nodejs API service"},
		{ID: "deploy-cs", Prompt: "nasaď poslední změny kamarádky"},
	}
}
