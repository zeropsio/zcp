package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// statusInfo marks a check as informational — a soft nudge the agent
// should see but that doesn't block step completion. checksAllPassed
// only flags statusFail, so info findings appear in the result payload
// alongside pass/fail but leave allPassed intact.
//
// Introduced for v8.82 §4.4 (container-ops-in-README boundary nudge).
// Future soft checks should reuse this constant instead of reinventing
// a per-check info string — the workflow layer treats the set of
// {"pass", "fail", "skip", "info"} as stable JSON values.
const statusInfo = "info"

// containerOpsTokens are substrings whose presence in a README gotcha
// bullet suggests the bullet is teaching CONTAINER OPERATIONS (things
// you do AS the operator on the dev container) rather than PLATFORM
// FAILURE MODES (things the Zerops platform does that surprise you).
//
// recipe.md explicitly separates the two surfaces: platform surprises
// live in README gotchas; repo-local ops live in CLAUDE.md. The rule is
// stated but not enforced — v22 workerdev README included prepareCommands
// bloat as a gotcha (bordering on container-ops) while CLAUDE.md
// correctly carried the SSHFS/fuser content. Boundaries slip when rules
// aren't checked.
//
// This soft check fires as info-only: agent sees the nudge, decides
// whether to reorganize. After 3–5 runs of false-positive data we
// either tune the corpus or escalate to hard-gate in v8.83.
var containerOpsTokens = []string{
	"sshfs",
	"fuser",
	"sudo chown",
	"uid 2023", // Zerops container UID
	"zcli vpn up",
	"npx tsc resolves",
	".ssh/known_hosts",
	"ssh zerops@",
	"ssh-keygen",
	"chown zerops:",
	"chmod +x",
	"nohup",
	"tmux new-session",
	"screen -dm",
	"/root/.ssh/",
	"/home/zerops/",
	"rm -rf node_modules", // dev-loop cleanup, not a platform failure
}

// checkReadmeContainerOps scans each gotcha bullet inside the README's
// knowledge-base fragment for tokens from containerOpsTokens. Each match
// emits ONE info-status check naming the bullet's stem, the matched
// token, and a pointer to CLAUDE.md as the correct location.
//
// Returns the empty slice when no matches are found — callers iterate
// the slice regardless of outcome, so an explicit "no findings" pass
// check is unnecessary; silent absence is the positive signal here.
//
// hostname is used to scope the check names so multi-codebase recipes
// can surface findings per-codebase.
func checkReadmeContainerOps(readmeContent, hostname string) []workflow.StepCheck {
	if readmeContent == "" {
		return nil
	}
	kb := extractFragmentContent(readmeContent, "knowledge-base")
	if kb == "" {
		kb = readmeContent // allow callers to pass the KB fragment directly
	}
	entries := workflow.ExtractGotchaEntries(kb)
	if len(entries) == 0 {
		return nil
	}
	var checks []workflow.StepCheck
	for _, e := range entries {
		text := strings.ToLower(e.Stem + " " + e.Body)
		for _, token := range containerOpsTokens {
			if !strings.Contains(text, token) {
				continue
			}
			checks = append(checks, workflow.StepCheck{
				Name:   hostname + "_readme_container_ops_nudge",
				Status: statusInfo,
				Detail: fmt.Sprintf(
					"%s gotcha %q mentions %q — this reads like container-ops (something you do AS the operator on the dev container) rather than a platform failure mode. recipe.md's rule: platform surprises live in README gotchas; repo-local dev-loop operations (SSHFS mount, fuser/kill patterns, ssh config, chown/chmod, nohup/tmux) live in CLAUDE.md. Consider moving this content to CLAUDE.md and leaving the README gotcha as a platform-facing symptom (HTTP status, error name, strong failure verb). This is advisory; if the gotcha genuinely describes a platform-caused failure that happens to mention %q as context (e.g. 'our seed script times out on SSHFS'), ignore the nudge.",
					hostname, e.Stem, token, token,
				),
			})
			break // one info per bullet — enough signal
		}
	}
	return checks
}
