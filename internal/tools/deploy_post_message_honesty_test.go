package tools

import (
	"os"
	"strings"
	"testing"
)

// TestDeployPostMessageHonesty pins invariant DS-01 from
// plans/dev-server-canonical-primitive.md: post-deploy message
// construction in deploy_poll.go and next_actions.go does NOT branch on
// runtime-class heuristics (NeedsManualStart / IsImplicitWebServerType)
// to claim process liveness. The code asserts only what the platform
// told us. Runtime-class-specific guidance (start the dev server via
// zerops_dev_server, or via harness background task in local env) lives
// in atoms — one prescriptive home per fact.
//
// Why a file-content scan rather than an AST walk: the forbidden
// identifier `NeedsManualStart` is deleted from the codebase as part of
// the same commit; `IsImplicitWebServerType` remains available (it has
// legitimate consumers in deploy_validate.go and
// workflow_checks_generate.go for run.start requirement and
// port/healthCheck validation), but must not appear inside the two
// message-construction files. A substring check over these two files is
// the minimum that enforces the contract.
//
// Update this test — do NOT re-route an assertion through one of these
// identifiers inside deploy_poll.go / next_actions.go — whenever the
// post-deploy message contract evolves. The atom corpus and the
// zerops_dev_server tool together own the dev-server lifecycle
// narrative; deploy_poll stays honest about what the platform returned.
func TestDeployPostMessageHonesty(t *testing.T) {
	t.Parallel()

	const (
		pollPath  = "deploy_poll.go"
		nextPath  = "next_actions.go"
		validate  = "../ops/deploy_validate.go"
		needsSym  = "NeedsManualStart"
		webSym    = "IsImplicitWebServerType"
		sshPhrase = `ssh %s "cd /var/www`
	)

	readFile := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(data)
	}

	poll := readFile(pollPath)
	next := readFile(nextPath)

	// DS-01: message-construction files do not reference the runtime-class
	// heuristics that produced dishonest state claims.
	for _, bundle := range []struct {
		path string
		body string
	}{
		{pollPath, poll},
		{nextPath, next},
	} {
		if strings.Contains(bundle.body, needsSym) {
			t.Errorf("%s must not reference %s — the identifier is deleted and post-deploy messages stay runtime-class-agnostic (see plans/dev-server-canonical-primitive.md DS-01)",
				bundle.path, needsSym)
		}
		if strings.Contains(bundle.body, webSym) {
			t.Errorf("%s must not reference %s — it has legit consumers elsewhere (deploy_validate.go, workflow_checks_generate.go) but never inside post-deploy message construction (see plans/dev-server-canonical-primitive.md DS-01)",
				bundle.path, webSym)
		}
		if strings.Contains(bundle.body, sshPhrase) {
			t.Errorf("%s must not embed SSH start commands in message text — zerops_dev_server (container) and harness background task (local) own dev-server lifecycle (see plans/dev-server-canonical-primitive.md DS-04)",
				bundle.path)
		}
	}

	// DS-04: NeedsManualStart is deleted from the codebase entirely. It
	// had zero non-UX consumers. If it re-appears anywhere in
	// internal/ops/deploy_validate.go, the deletion regressed.
	vbody := readFile(validate)
	if strings.Contains(vbody, "func "+needsSym) {
		t.Errorf("%s must not define %s — the heuristic is retired (see plans/dev-server-canonical-primitive.md W1)",
			validate, needsSym)
	}
}
