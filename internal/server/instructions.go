package server

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/runtime"
)

// Instructions delivered to the MCP client at server init. Pure static text:
// no API calls, no state reads, no dynamic content. State is encapsulated
// behind zerops_workflow action="status", which is the one canonical entry
// point the LLM needs to remember.
const baseInstructions = `ZCP manages Zerops PaaS infrastructure.

ALWAYS begin with project state:
  zerops_workflow action="status"
Returns the current phase (idle | bootstrap | develop | recipe), services,
setup, progress, and the concrete next action.

Workflows (via zerops_workflow action="start" workflow="..."):
  bootstrap — create/adopt infrastructure (services, project import)
  develop   — code tasks (edit → deploy → verify; auto-closes when complete)
  recipe    — build recipe repo files
  cicd      — generate pipeline files

Direct tools: zerops_scale, zerops_manage, zerops_env, zerops_subdomain,
zerops_discover, zerops_knowledge, zerops_deploy, zerops_verify, zerops_mount.`

const containerEnvironment = `
Running in a Zerops container (control plane). Manages other services;
does not serve traffic. Files at /var/www/{hostname}/ are SSHFS mounts.
Commands: ssh {hostname} "...". Edits on the mount survive restart but not
deploy. zerops_discover refreshes service state.`

const localEnvironment = `
Running on a local machine. Code in the working directory; infrastructure
on Zerops. Deploy via zcli push (zerops.yaml at repo root; each deploy is
a new container). zerops_discover refreshes service state.`

// BuildInstructions returns the static MCP instructions text. Varies only by
// environment (container vs local) and self-hostname — no state, no API.
func BuildInstructions(rt runtime.Info) string {
	if rt.InContainer {
		out := baseInstructions + containerEnvironment
		if rt.ServiceName != "" {
			out += fmt.Sprintf("\nYou are running on '%s'. Other services in this project are yours to manage.", rt.ServiceName)
		}
		return out
	}
	return baseInstructions + localEnvironment
}
