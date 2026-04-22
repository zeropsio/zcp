package server

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/runtime"
)

// Instructions delivered to the MCP client at server init. Base text is
// static (no API calls, no state reads). On local-env startup, server.New
// prepends an adoption note describing what auto-adopt did on this run —
// see BuildInstructions + workflow.FormatAdoptionNote. The note is
// one-shot (only emitted when LocalAutoAdopt actually wrote a new meta);
// re-runs against an already-initialized state dir get clean base text.
const baseInstructions = `ZCP manages Zerops PaaS infrastructure through workflows.

Primary entry — every user task that changes code or deploys:
  zerops_workflow action="start" workflow="develop" intent="<one-liner>"

The develop workflow carries step-by-step guidance, tracks deploys and
verifies, and auto-closes when scoped services are green.

Other entries (when they fit):
  workflow="bootstrap"  — create/adopt infrastructure (empty project, add a service, mode expansion)
  workflow="recipe"     — build recipe repo files
  workflow="export"     — turn a deployed service into a re-importable git repo

Deploy configuration:
  action="strategy" strategies={hostname:"push-dev|push-git|manual"}
    — central point for deploy config; push-git returns full setup flow
      (GIT_TOKEN, optional CI/CD via GitHub Actions or webhook, first push)

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
on Zerops. Deploy via zerops_deploy (targetService=<hostname>) — pushes
the working directory to the matching Zerops service and blocks until
build completes. Requires zerops.yaml at repo root. zerops_discover
refreshes service state.`

// BuildInstructions returns the MCP instructions text. Base text varies by
// environment (container vs local) and self-hostname. In local env,
// server.New may also pass an adoptionNote describing what auto-adopt
// just did (via BuildInstructionsWithNote). BuildInstructions itself
// stays note-free for unit tests that don't care about adoption.
func BuildInstructions(rt runtime.Info) string {
	return BuildInstructionsWithNote(rt, "")
}

// BuildInstructionsWithNote is the note-aware variant used by server.New
// after LocalAutoAdopt has run. Empty note → identical output to
// BuildInstructions. Non-empty note is appended after the env-specific
// block with a blank-line separator.
func BuildInstructionsWithNote(rt runtime.Info, adoptionNote string) string {
	var out string
	if rt.InContainer {
		out = baseInstructions + containerEnvironment
		if rt.ServiceName != "" {
			out += fmt.Sprintf("\nYou are running on '%s'. Other services in this project are yours to manage.", rt.ServiceName)
		}
	} else {
		out = baseInstructions + localEnvironment
	}
	if adoptionNote != "" {
		out += "\n\n" + adoptionNote
	}
	return out
}
