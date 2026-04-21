package server

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/runtime"
)

// Instructions delivered to the MCP client at server init. Pure static text:
// no API calls, no state reads, no dynamic content. State is encapsulated
// behind zerops_workflow action="status", which is the one canonical entry
// point the LLM needs to remember.
const baseInstructions = `ZCP manages Zerops PaaS infrastructure through workflows.

Primary entry — every user task that changes code or deploys:
  zerops_workflow action="start" workflow="develop" intent="<one-liner>"

The develop workflow carries step-by-step guidance, tracks deploys and
verifies, and auto-closes when scoped services are green.

Other entries (when they fit):
  workflow="bootstrap"  — create/adopt infrastructure (empty project, add a service, mode expansion)
  workflow="recipe"     — build recipe repo files
  workflow="cicd"       — generate pipeline config

Direct tools — read-only queries and config tweaks that don't need a
deploy cycle: zerops_discover, zerops_logs, zerops_events,
zerops_knowledge, zerops_env, zerops_manage, zerops_scale,
zerops_subdomain, zerops_verify.

Workflow-gated: zerops_deploy (adopted services), zerops_mount and
zerops_import (active workflow session).

Recovery — after compaction or when the state is unclear:
  zerops_workflow action="status"`

const containerEnvironment = `
Running as the ZCP control-plane container — Ubuntu with zcli, psql, mysql,
redis-cli, jq, and network to every service. App code lives in the runtime
containers (reach via ssh {hostname} "..."), not here. Files at
/var/www/{hostname}/ are SSHFS mounts — edit with Read/Edit/Write, not SSH.
CLI helpers like jq/psql are missing inside runtimes; pipe output back to
ZCP. Edits on the mount survive restart but not deploy. zerops_discover
refreshes service state.`

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
