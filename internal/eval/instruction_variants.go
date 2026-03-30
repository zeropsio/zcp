package eval

// InstructionVariant defines an MCP instruction text variant for A/B testing.
type InstructionVariant struct {
	ID          string // short identifier (e.g., "rule", "gap", "process")
	Name        string // human-readable name
	Base        string // baseInstructions replacement
	Container   string // containerEnvironment replacement
	Local       string // localEnvironment replacement
	Description string // what this variant tests
}

// InstructionScenario defines a user prompt to test against.
type InstructionScenario struct {
	ID     string // short identifier
	Prompt string // what the user says
	Lang   string // "cs" or "en"
}

// InstructionVariants returns all instruction text variants to evaluate.
// Each variant fits within the 2KB MCP instructions limit (~700B static + ~1300B dynamic).
func InstructionVariants() []InstructionVariant {
	return []InstructionVariant{
		{
			ID:          "current",
			Name:        "Current (v6.32.0)",
			Description: "Current production text — rule-based directive first",
			Base: `ZCP manages Zerops PaaS infrastructure.
Before ANY work on service code (reading, debugging, fixing, deploying), start a workflow:
  zerops_workflow action="start" workflow="..."
  deploy — deploy, fix, or investigate services
  bootstrap — create/adopt services    cicd — CI/CD setup
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_deploy (manual strategy), zerops_discover, zerops_knowledge`,
			Container: `
Control plane container — manages OTHER services, does not serve traffic.
Files: /var/www/{hostname}/ = SSHFS mount to live service (not local). Commands: ssh {hostname} "..."
Edits on mount survive restarts but not deploys. zerops_discover refreshes service state.`,
			Local: `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yml at repo root, each deploy = new container).
zerops_discover refreshes service state.`,
		},
		{
			ID:          "info-gap",
			Name:        "Information gap",
			Description: "Frames workflow as missing information, not a rule to follow",
			Base: `ZCP manages Zerops PaaS infrastructure.
Service code depends on platform config NOT visible in source files (env vars, runtime settings, deploy constraints, mount state). Call zerops_discover before analyzing any service, then start a workflow for full context:
  zerops_workflow action="start" workflow="..."
  deploy — deploy, fix, or investigate services
  bootstrap — create/adopt services    cicd — CI/CD setup
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_deploy (manual), zerops_discover, zerops_knowledge`,
			Container: `
Control plane container — manages OTHER services, does not serve traffic.
Files: /var/www/{hostname}/ = SSHFS mount to live service (not local). Commands: ssh {hostname} "..."
Edits on mount survive restarts but not deploys.`,
			Local: `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yml at repo root, each deploy = new container).`,
		},
		{
			ID:          "step-by-step",
			Name:        "Step-by-step process",
			Description: "Explicit numbered process — always start with discover",
			Base: `ZCP manages Zerops PaaS infrastructure.
For ANY task involving service code, follow this process:
1. zerops_discover — see services, env vars, mount state
2. zerops_workflow action="start" workflow="deploy|bootstrap|cicd" — get platform context
3. Then read/edit code with full understanding of the platform
Workflows: deploy (deploy/fix/investigate), bootstrap (create/adopt), cicd (CI/CD setup)
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_discover, zerops_knowledge`,
			Container: `
Control plane container — manages OTHER services, does not serve traffic.
Files: /var/www/{hostname}/ = SSHFS mount to live service (not local). Commands: ssh {hostname} "..."
Edits on mount survive restarts but not deploys.`,
			Local: `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yml at repo root, each deploy = new container).`,
		},
		{
			ID:          "consequence",
			Name:        "Consequence-based",
			Description: "Emphasizes what goes wrong without workflow",
			Base: `ZCP manages Zerops PaaS infrastructure.
Service files are on live Zerops services. Editing without platform context produces incorrect fixes — env vars, runtime config, and deploy constraints are NOT in the source code. Previous attempts to fix code without workflow context introduced bugs.
ALWAYS start with: zerops_workflow action="start" workflow="deploy|bootstrap|cicd"
Workflows: deploy (deploy/fix/investigate), bootstrap (create/adopt), cicd (CI/CD setup)
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_discover, zerops_knowledge`,
			Container: `
Control plane container. Files: /var/www/{hostname}/ = SSHFS mount to live service.
Commands: ssh {hostname} "...". Edits survive restarts but not deploys.`,
			Local: `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yml at repo root).`,
		},
		{
			ID:          "identity",
			Name:        "Identity-based",
			Description: "Frames the LLM as orchestrator whose first action is always discovery",
			Base: `ZCP manages Zerops PaaS infrastructure. You are the orchestrator.
Your first action for ANY task is always: zerops_discover (understand what exists), then zerops_workflow action="start" to get platform context before touching code.
Workflows: deploy (deploy/fix/investigate), bootstrap (create/adopt), cicd (CI/CD setup)
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_discover, zerops_knowledge`,
			Container: `
This container is the control plane — it manages OTHER services.
Files: /var/www/{hostname}/ = SSHFS mount to live service. Commands: ssh {hostname} "..."
Edits survive restarts but not deploys.`,
			Local: `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yml at repo root).`,
		},
		{
			ID:          "discover-first",
			Name:        "Discover-first",
			Description: "Makes zerops_discover the mandatory first step, not workflow",
			Base: `ZCP manages Zerops PaaS infrastructure.
FIRST STEP for any task: call zerops_discover to see services, their state, env vars, and what needs attention. Then start a workflow for context:
  zerops_workflow action="start" workflow="deploy|bootstrap|cicd"
Without discover, you don't know the platform state. Without workflow, you don't have runtime docs or deploy constraints.
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_discover, zerops_knowledge`,
			Container: `
Control plane container. /var/www/{hostname}/ = SSHFS mount to live service (not local files).
Commands: ssh {hostname} "...". Edits survive restarts but not deploys.`,
			Local: `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yml at repo root).`,
		},
		{
			ID:          "question",
			Name:        "Question-based",
			Description: "Starts with questions the LLM should ask itself before acting",
			Base: `ZCP manages Zerops PaaS infrastructure.
Before touching any service code, ask yourself:
- Do I know what services exist? → zerops_discover
- Do I have platform context (env vars, runtime config)? → zerops_workflow action="start" workflow="deploy"
- Am I about to edit a live service mount? → /var/www/{hostname}/ is live, not local
If any answer is "no", call the tool first.
Workflows: deploy (deploy/fix/investigate), bootstrap (create/adopt), cicd (CI/CD setup)
Direct tools: zerops_scale, zerops_manage, zerops_discover, zerops_knowledge`,
			Container: `
Control plane container. /var/www/{hostname}/ = SSHFS mount to live service.
Commands: ssh {hostname} "...". Edits survive restarts but not deploys.`,
			Local: `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yml at repo root).`,
		},
		{
			ID:          "checklist",
			Name:        "Checklist gate",
			Description: "Pre-flight checklist that must pass before code work",
			Base: `ZCP manages Zerops PaaS infrastructure.
Pre-flight checklist (complete before reading or editing service code):
[ ] zerops_discover called — I know what services exist and their state
[ ] zerops_workflow started — I have platform context (runtime docs, env vars, constraints)
Skip only for simple ops: zerops_scale, zerops_manage, zerops_knowledge
Workflows: deploy (deploy/fix/investigate), bootstrap (create/adopt), cicd (CI/CD setup)`,
			Container: `
Control plane container. /var/www/{hostname}/ = live service (SSHFS mount, not local).
ssh {hostname} "..." for commands. Edits survive restarts but not deploys.`,
			Local: `
Local machine — code in working dir, infra on Zerops. Deploy: zcli push.`,
		},
		{
			ID:          "minimal",
			Name:        "Minimal directive",
			Description: "Absolute minimum — one sentence directive, maximize dynamic space",
			Base: `ZCP manages Zerops PaaS. Always call zerops_discover then zerops_workflow before any code work.
Workflows: deploy, bootstrap, cicd. Direct: zerops_scale, zerops_manage, zerops_env, zerops_knowledge.`,
			Container: `
Control plane. /var/www/{hostname}/ = live SSHFS mount. ssh {hostname} for commands.`,
			Local: `
Local dev. Deploy: zcli push. Infra on Zerops.`,
		},
		{
			ID:          "blunt",
			Name:        "Blunt/direct",
			Description: "Very direct, no explanation — just the command",
			Base: `ZCP manages Zerops PaaS infrastructure.
STOP. Do not Read, Edit, or Grep files under /var/www/ until you have called:
  1. zerops_discover
  2. zerops_workflow action="start" workflow="deploy"
This gives you env vars, runtime config, and platform constraints you cannot see in code.
Other workflows: bootstrap (new/adopt services), cicd (CI/CD setup).
Direct tools (no workflow): zerops_scale, zerops_manage, zerops_knowledge.`,
			Container: `
Control plane. /var/www/{hostname}/ = SSHFS mount to live service. ssh {hostname} for commands.`,
			Local: `
Local machine. Code in working dir, infra on Zerops. Deploy: zcli push.`,
		},
	}
}

// InstructionScenarios returns test prompts to evaluate each variant against.
func InstructionScenarios() []InstructionScenario {
	return []InstructionScenario{
		{ID: "audit-cs", Prompt: "udělej audit kódu u kamarádky", Lang: "cs"},
		{ID: "fix-cs", Prompt: "oprav bug v kamarádce — cyklí se zvuk na mobilu", Lang: "cs"},
		{ID: "add-feature-cs", Prompt: "přidej autentizaci na websocket v kamarádce", Lang: "cs"},
		{ID: "audit-en", Prompt: "audit the kamaradka service code", Lang: "en"},
		{ID: "fix-en", Prompt: "fix the audio echo loop bug in kamaradka", Lang: "en"},
		{ID: "create-en", Prompt: "create a new nodejs service for an API", Lang: "en"},
		{ID: "look-cs", Prompt: "podívej se co je v kódu kamarádky", Lang: "cs"},
		{ID: "deploy-cs", Prompt: "nasaď poslední změny kamarádky", Lang: "cs"},
	}
}
