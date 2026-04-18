package workflow

import (
	"fmt"
	"time"
)

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
