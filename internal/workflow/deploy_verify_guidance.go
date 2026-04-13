package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// verifyAgentTemplate is the per-target prompt for the verify agent.
// Placeholders: hostname (3x), runtimeType (1x).
const verifyAgentTemplate = `Verify Zerops service "%s" (%s) works for end users.

## Protocol
1. ` + "`" + `zerops_verify serviceHostname="%s"` + "`" + ` — infrastructure baseline
2. If NOT healthy → VERDICT: FAIL (cite failed checks from zerops_verify response)
3. ` + "`" + `zerops_discover service="%s"` + "`" + ` — get subdomainUrl or connection info
4. Determine reachable URL:
   - subdomainUrl available → use it (public HTTPS)
   - no subdomain, no custom domain → VERDICT: UNCERTAIN (cannot reach from outside)
   - unreachable after timeout → VERDICT: UNCERTAIN
5. ` + "`" + `agent-browser open {url}` + "`" + `
6. ` + "`" + `agent-browser snapshot` + "`" + ` — accessibility tree for AI analysis
7. Evaluate: does the page render meaningful content?
   - Interactive elements (buttons, links, forms)?
   - Text content (headings, paragraphs)?
   - Or empty/broken (empty root div, error page, blank screen)?
8. If concerns: ` + "`" + `agent-browser eval "JSON.stringify(Array.from(document.querySelectorAll('script[src]')).map(s=>s.src))"` + "`" + ` for loaded scripts
9. For SPAs: ` + "`" + `agent-browser eval "window.__errors || []"` + "`" + ` AND check if console has errors

## Rules
- zerops_verify unhealthy/degraded → always VERDICT: FAIL (never override infra checks)
- HTTP 401/403 with rendered content (login page, auth challenge) → VERDICT: PASS (auth is working correctly)
- HTTP 401/403 with empty body → VERDICT: UNCERTAIN (cannot determine if intentional)
- zerops_verify healthy + page empty/broken → VERDICT: FAIL (cite what you see)
- zerops_verify healthy + page renders real content → VERDICT: PASS
- agent-browser unavailable or URL unreachable → VERDICT: UNCERTAIN

## Output (mandatory format)
### Infrastructure
{zerops_verify status and check summary}

### Application
{what you observed — DOM content, JS errors, visual state}

### Evidence
{accessibility tree excerpt or error details}

### VERDICT: {PASS|FAIL|UNCERTAIN} — {one line justification}
`

// verifyVerdictProtocol explains how the orchestrator should interpret agent verdicts.
const verifyVerdictProtocol = `### Verdict protocol
- **VERDICT: PASS** → service verified, proceed
- **VERDICT: FAIL** → agent found visual/functional issue; enter iteration loop with agent's evidence as diagnosis
- **VERDICT: UNCERTAIN** → fallback to zerops_verify result (agent couldn't determine)
- **Malformed output / agent timeout** → treat as UNCERTAIN, fall back to zerops_verify
`

// buildVerifyGuide generates personalized verify guidance per target.
// Web-facing services (HTTPSupport=true) get a verify agent prompt for visual/functional checks.
// Non-web services get direct zerops_verify only.
func buildVerifyGuide(d *DeployState) string {
	base := getVerifyBase()

	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\n\n### Per-service verification\n\n")

	for _, t := range d.Targets {
		if t.HTTPSupport {
			writeAgentVerify(&sb, t)
		} else {
			writeDirectVerify(&sb, t)
		}
	}

	sb.WriteString(verifyVerdictProtocol)
	return sb.String()
}

func writeDirectVerify(sb *strings.Builder, t DeployTarget) {
	fmt.Fprintf(sb, "**%s** (%s): `zerops_verify serviceHostname=\"%s\"` — check status=healthy.\n\n",
		t.Hostname, t.RuntimeType, t.Hostname)
}

func writeAgentVerify(sb *strings.Builder, t DeployTarget) {
	fmt.Fprintf(sb, "**%s** (%s, web-facing): Spawn verify agent:\n", t.Hostname, t.RuntimeType)
	sb.WriteString("```\nAgent(model=\"sonnet\", prompt=\"\"\"\n")
	fmt.Fprintf(sb, verifyAgentTemplate, t.Hostname, t.RuntimeType, t.Hostname, t.Hostname)
	sb.WriteString("\"\"\")\n```\n\n")
}

func getVerifyBase() string {
	md, err := content.GetWorkflow("develop")
	if err != nil {
		return "Run zerops_verify for each target service. Check health status."
	}
	section := ExtractSection(md, "deploy-verify")
	if section == "" {
		return "Run zerops_verify for each target service. Check health status."
	}
	return section
}
