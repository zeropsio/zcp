# ZCP Philosophy

## Why This Exists

Current AI-assisted development tools (v0, Lovable, Replit Agent, Codex, Jules, etc.) share a common model: AI generates code, deployment is an afterthought, and handoff to humans means "export and figure it out."

ZCP takes a different approach: AI operates on real infrastructure with enforced checkpoints, producing auditable evidence at each step. Humans and AI use the same workflow, the same commands, the same environment.

---

## The Zerops Foundation

ZCP is possible because of how Zerops is built:

- **Linux containers (Incus)** — Real isolated environments, not sandboxes or VMs. SSH access, SSHFS mounts, standard tooling.
- **Environment parity** — Dev and stage are actual Zerops services, identical to production. Not simulations.
- **Flexible service model** — Any runtime, any managed service, any configuration. No artificial constraints.
- **Multi-project architecture** — Production is a separate project. Dev/stage pairs can be created per-developer (or per-agent).

Most platforms can't support this pattern because they weren't designed for it. Zerops was built as infrastructure-first, which makes ZCP a natural extension rather than a hack.

---

## Core Ideas

### 1. Evidence Over Trust

AI tools typically self-report success ("Deployment complete!"). ZCP requires JSON evidence files proving each step actually worked—session-scoped, timestamped, containing actual response data. Gates block progression until evidence exists.

### 2. Environment Parity by Default

Dev and stage are real Zerops services in the same project. Agent iterates on dev, validates on stage. Production is a completely separate project, accessed only through PR → human review → CI/CD. The AI never touches production.

### 3. Seamless Human/AI Handoff

Workflow state, intent, notes, and evidence persist. A human can run `.zcp/workflow.sh show` and continue exactly where an AI stopped. An AI can resume where a human left off. Same commands, same evidence requirements, same process.

### 4. Parallel Work

Multiple dev/stage pairs can exist for the same production project. Multiple agents (or humans) work independently, each producing PRs. Standard git workflow. No bottleneck on a single agent.

### 5. Structural Constraints

Guardrails are enforced by the workflow system, not by prompts or model behavior. The agent cannot skip verification because the gate physically blocks phase transitions without evidence files.

---

## Comparison

| Aspect | Typical AI Tools | ZCP |
|--------|------------------|-----|
| Environment | Sandbox / export | Real infrastructure with parity |
| Verification | Self-reported | Evidence-gated |
| Human handoff | Download code | Continue same workflow |
| Production access | Direct or none | Never (PR → human → CI/CD) |
| Multi-agent | No | Yes (parallel dev/stage pairs) |
| Guardrails | Prompt-based | Structural (gates) |

---

## The Bet

AI-assisted development will converge toward treating AI as a team member rather than a magic wand. That means:

- Constrained environments (not production access)
- Auditable work (not trust-based)
- Standard processes (not AI-specific workflows)
- Easy handoff (not export-and-rebuild)

ZCP implements this model. Zerops makes it possible.
