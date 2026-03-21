package workflow

import (
	"fmt"
	"strings"
)

// BuildServiceContextSummary creates a markdown summary of project services
// from service metas. Used to inject context into stateless workflows.
func BuildServiceContextSummary(metas []*ServiceMeta) string {
	if len(metas) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Your Project Services\n\n")

	var runtimes, managed []string

	for _, m := range metas {
		entry := fmt.Sprintf("- **%s**", m.Hostname)
		if m.Mode != "" {
			entry += fmt.Sprintf(" [%s mode]", m.Mode)
		}
		if m.StageHostname != "" {
			entry += fmt.Sprintf(" → stage: **%s**", m.StageHostname)
		}
		if m.DeployStrategy != "" {
			entry += fmt.Sprintf(" (strategy: %s)", m.DeployStrategy)
		}

		// Classify as runtime vs managed.
		if m.Mode != "" || m.StageHostname != "" {
			runtimes = append(runtimes, entry)
		} else {
			managed = append(managed, entry)
		}
	}

	if len(runtimes) > 0 {
		sb.WriteString("**Runtime services:**\n")
		for _, r := range runtimes {
			sb.WriteString(r + "\n")
		}
		sb.WriteString("\n")
	}
	if len(managed) > 0 {
		sb.WriteString("**Managed services:**\n")
		for _, m := range managed {
			sb.WriteString(m + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
