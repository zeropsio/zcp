package workflow

import (
	"fmt"
	"strings"
	"time"
)

// BuildWorkSessionBlock renders the "## Lifecycle Status" work-session block
// for the system prompt. Returns empty string when there's no session.
//
// Strategy per service is read fresh from ServiceMeta — never stored in the
// work session itself (single source of truth lives in ServiceMeta).
//
// Rendering cases:
//   - Closed auto-complete: shows completion hint + next-task nudge.
//   - Active: intent, duration, services with strategy, deploys, verifies, next.
func BuildWorkSessionBlock(ws *WorkSession, metas []*ServiceMeta) string {
	if ws == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Lifecycle Status\n")

	if ws.ClosedAt != "" && ws.CloseReason == CloseReasonAutoComplete {
		sb.WriteString("Work session — task complete. All services deployed + verified.\n")
		sb.WriteString("  → Close: zerops_workflow action=\"close\" workflow=\"develop\"\n")
		sb.WriteString("  → Next task: zerops_workflow action=\"start\" workflow=\"develop\" intent=\"...\"\n")
		return sb.String()
	}

	duration := formatWorkDuration(ws.CreatedAt)
	fmt.Fprintf(&sb, "Work session active (%s) — intent: %q\n", duration, ws.Intent)

	metaByHost := make(map[string]*ServiceMeta, len(metas))
	for _, m := range metas {
		if m != nil {
			metaByHost[m.Hostname] = m
		}
	}

	if len(ws.Services) > 0 {
		sb.WriteString("  Services: ")
		parts := make([]string, 0, len(ws.Services))
		for _, h := range ws.Services {
			strategy := ""
			if m := metaByHost[h]; m != nil {
				strategy = m.EffectiveStrategy()
			}
			if strategy == "" {
				strategy = "strategy unset"
			}
			parts = append(parts, fmt.Sprintf("%s (%s)", h, strategy))
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("\n")
	}

	if deploys := formatAttemptsLine("Deploys", ws.Services, func(h string) string {
		return formatDeployStatus(ws.Deploys[h])
	}); deploys != "" {
		sb.WriteString("  ")
		sb.WriteString(deploys)
		sb.WriteString("\n")
	}
	if verifies := formatAttemptsLine("Verifies", ws.Services, func(h string) string {
		return formatVerifyStatus(ws.Verifies[h])
	}); verifies != "" {
		sb.WriteString("  ")
		sb.WriteString(verifies)
		sb.WriteString("\n")
	}

	if next := SuggestNext(ws); next != "" {
		fmt.Fprintf(&sb, "  → %s\n", next)
	}

	return sb.String()
}

// formatAttemptsLine builds "Label: host1 status | host2 status | ..." when any
// host has activity. Returns empty when every status is blank.
func formatAttemptsLine(label string, services []string, statusFor func(string) string) string {
	parts := make([]string, 0, len(services))
	hasActivity := false
	for _, h := range services {
		status := statusFor(h)
		if status != "" {
			hasActivity = true
		}
		if status == "" {
			status = "—"
		}
		parts = append(parts, fmt.Sprintf("%s %s", h, status))
	}
	if !hasActivity {
		return ""
	}
	return label + ": " + strings.Join(parts, " | ")
}

// formatDeployStatus summarizes deploy attempts for one hostname.
// Empty string means "no attempts yet" — caller decides the filler.
func formatDeployStatus(attempts []DeployAttempt) string {
	if len(attempts) == 0 {
		return ""
	}
	last := attempts[len(attempts)-1]
	if last.SucceededAt != "" {
		if len(attempts) == 1 {
			return "✓ " + formatAgo(last.SucceededAt)
		}
		return fmt.Sprintf("✓ (after %d attempts)", len(attempts))
	}
	reason := last.Error
	if reason == "" {
		reason = "in progress"
	}
	if len(attempts) == 1 {
		return fmt.Sprintf("✗ 1 attempt (%s)", reason)
	}
	return fmt.Sprintf("✗ %d attempts (last: %s)", len(attempts), reason)
}

// formatVerifyStatus summarizes verify attempts for one hostname.
func formatVerifyStatus(attempts []VerifyAttempt) string {
	if len(attempts) == 0 {
		return ""
	}
	last := attempts[len(attempts)-1]
	if last.Passed {
		return "✓ " + formatAgo(last.PassedAt)
	}
	summary := last.Summary
	if summary == "" {
		summary = "failed"
	}
	if len(attempts) == 1 {
		return fmt.Sprintf("✗ 1 attempt (%s)", summary)
	}
	return fmt.Sprintf("✗ %d attempts (last: %s)", len(attempts), summary)
}

// formatWorkDuration renders "1h 23m" / "5m" / "45s" from a CreatedAt timestamp.
func formatWorkDuration(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return "unknown"
	}
	d := max(time.Since(t), 0)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) - hours*60
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// formatAgo renders "3m ago" / "1h ago" from an absolute timestamp.
func formatAgo(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	d := max(time.Since(t), 0)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	hours := int(d.Hours())
	return fmt.Sprintf("%dh ago", hours)
}
