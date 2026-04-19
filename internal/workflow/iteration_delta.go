package workflow

import "fmt"

// BuildIterationDelta returns a focused escalating recovery template for deploy iterations.
// Returns empty for non-deploy steps or iteration == 0.
// Escalation tiers: 1-2 = diagnose, 3-4 = systematic check, 5 = stop and ask user.
// defaultMaxIterations=5 caps the session so the STOP tier fires exactly once.
func BuildIterationDelta(step string, iteration int, _ *ServicePlan, lastAttestation string) string {
	if step != StepDeploy || iteration == 0 {
		return ""
	}
	remaining := max(maxIterations()-iteration, 0)

	var guidance string
	switch {
	case iteration <= 2:
		guidance = `DIAGNOSE: zerops_logs severity="error" since="5m"
FIX the specific error, then redeploy + verify.`

	case iteration <= 4:
		guidance = `PREVIOUS FIXES FAILED. Systematic check:
1. zerops_discover includeEnvs=true — are all env var keys present? (keys only, sufficient for cross-ref wiring)
2. Does zerops.yaml envVariables ONLY use discovered variable names?
3. Does the app bind 0.0.0.0 (not localhost/127.0.0.1)?
4. Is deployFiles correct? (dev MUST be [.], stage = build output)
5. Is run.ports.port matching what the app actually listens on?
6. Is run.start the RUN command (not a build command)?
Fix what's wrong, redeploy, verify.`

	default:
		guidance = `STOP. Multiple fixes failed. Present to user:
1. What you tried in each iteration
2. Current error (from zerops_logs + zerops_verify)
3. Ask: "Should I continue, or would you like to debug manually?"
Do NOT attempt another fix without user input.`
	}

	return fmt.Sprintf("ITERATION %d (session remaining: %d)\n\nPREVIOUS: %s\n\n%s",
		iteration, remaining, lastAttestation, guidance)
}
