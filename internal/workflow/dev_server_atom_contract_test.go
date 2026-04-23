package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestDevServerAtomContract pins the canonical dev-server lifecycle
// primitives in the develop atom corpus:
//
//   - Container env: `zerops_dev_server` MCP tool (actions start / status /
//     stop / restart / logs). Detach, timeouts, port-free polling, and
//     structured reason codes are owned by the tool.
//   - Local env:     harness background task primitive (e.g. `Bash
//     run_in_background=true` in Claude Code). Dev server runs on the
//     user's machine; ZCP does not spawn local processes.
//
// Establishing this contract in the atom corpus is step one of
// plans/dev-server-canonical-primitive.md (invariants DS-02, DS-03,
// DS-04). The matching code-side honesty invariant (DS-01, neutral
// post-deploy message) is pinned by TestDeployPostMessageHonesty in
// internal/tools/.
//
// The test fails if any listed atom is missing its canonical prescription
// (required phrases), or if it regresses to raw-SSH dev-server patterns
// that zerops_dev_server was built to replace (forbidden phrases). Raw
// SSH for one-shot commands (git ops, framework CLI, curl localhost)
// remains legitimate — the forbidden phrases are scoped to dev-server
// start contexts.
//
// Update this test — do NOT edit the atom body to re-introduce a raw-SSH
// dev-server pattern — whenever the dev-server contract evolves. The
// atom corpus, the tool, and the spec (O4) must ship in lockstep, or the
// LLM will drift back to 300-second SSH-hang territory the moment one
// atom ages out of sync.
func TestDevServerAtomContract(t *testing.T) {
	t.Parallel()

	type rule struct {
		atomID      string
		required    []string // all must appear
		forbidden   []string // none may appear
		explanation string
	}

	rules := []rule{
		{
			atomID:      "develop-dynamic-runtime-start-container",
			required:    []string{"zerops_dev_server action=start"},
			forbidden:   []string{`'cd /var/www && {start-command}'`},
			explanation: "container dynamic-runtime start must prescribe zerops_dev_server action=start; raw SSH-and-background is the legacy pattern the tool replaces",
		},
		{
			atomID:      "develop-dynamic-runtime-start-local",
			required:    []string{"run_in_background=true"},
			forbidden:   []string{`zerops_dev_server action=start`, "ssh -o StrictHostKeyChecking=no"},
			explanation: "local dev server runs on the user's machine via harness background task primitive; no SSH-to-remote, no imperative zerops_dev_server (container-only tool)",
		},
		{
			atomID:      "develop-close-push-dev-dev",
			required:    []string{"zerops_dev_server"},
			forbidden:   []string{"start the server via a NEW SSH session", `ssh {hostname} "cd /var/www`, `ssh {hostname} 'cd /var/www`},
			explanation: "close-task in dev mode must prescribe zerops_dev_server; raw SSH start inside the close flow is the legacy pattern",
		},
		{
			atomID:      "develop-close-push-dev-standard",
			required:    []string{"zerops_dev_server"},
			forbidden:   []string{"start the server via a NEW SSH session", `ssh {hostname} "cd /var/www`, `ssh {hostname} 'cd /var/www`},
			explanation: "close-task in standard mode must prescribe zerops_dev_server; raw SSH start inside the close flow is the legacy pattern",
		},
		{
			atomID:      "develop-push-dev-workflow-dev",
			required:    []string{"zerops_dev_server"},
			forbidden:   []string{"Restart the server over SSH", `ssh {hostname} "cd /var/www`, `ssh {hostname} 'cd /var/www`},
			explanation: "iteration loop must prescribe zerops_dev_server action=restart; raw SSH restart was the 300s-hang trap",
		},
		{
			atomID:      "develop-manual-deploy",
			required:    []string{"zerops_dev_server"},
			forbidden:   []string{"Start manually via SSH"},
			explanation: "manual-strategy variant must prescribe zerops_dev_server, not raw SSH",
		},
		{
			atomID:      "develop-dev-server-triage",
			required:    []string{"zerops_dev_server action=status", "run_in_background=true"},
			explanation: "triage atom teaches the env-agnostic pattern; both container (zerops_dev_server) and local (harness task) implementations must appear",
		},
		{
			atomID:      "bootstrap-runtime-classes",
			required:    []string{"zerops_dev_server"},
			explanation: "runtime-classes taxonomy must reference zerops_dev_server so the LLM sees the canonical primitive from bootstrap onwards",
		},
		{
			atomID:      "develop-checklist-dev-mode",
			required:    []string{"zerops_dev_server"},
			forbidden:   []string{"agent starts the server over SSH"},
			explanation: "dev-mode checklist must reference the canonical primitive, not generic SSH wording",
		},
		{
			atomID:      "develop-platform-rules-container",
			required:    []string{"zerops_dev_server"},
			explanation: "platform rules must surface zerops_dev_server as the long-running-process primitive, with SSH reserved for one-shot commands",
		},
		{
			atomID:      "develop-platform-rules-local",
			required:    []string{"run_in_background=true"},
			explanation: "local platform rules must prescribe harness background task primitive for dev servers on the user's machine",
		},
	}

	atoms, err := content.ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}

	byID := make(map[string]string, len(atoms))
	for _, a := range atoms {
		id := strings.TrimSuffix(a.Name, ".md")
		byID[id] = a.Content
	}

	for _, r := range rules {
		body, ok := byID[r.atomID]
		if !ok {
			t.Errorf("atom %q not found in corpus — add the atom or remove the rule (see plans/dev-server-canonical-primitive.md)",
				r.atomID)
			continue
		}
		for _, phrase := range r.required {
			if !strings.Contains(body, phrase) {
				t.Errorf("atom %q must contain %q — %s (see plans/dev-server-canonical-primitive.md)",
					r.atomID, phrase, r.explanation)
			}
		}
		for _, phrase := range r.forbidden {
			if strings.Contains(body, phrase) {
				t.Errorf("atom %q must not contain %q — %s (see plans/dev-server-canonical-primitive.md)",
					r.atomID, phrase, r.explanation)
			}
		}
	}
}
