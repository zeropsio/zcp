// Package cicd composes the GitHub Actions handoff (workflow YAML +
// stdin gh-secret-set command) for stage and production CI/CD per
// plans/workflow-family-architecture-2026-05-14.md §9.3. Shared by
// `action=git-push-setup` confirm (stage) and the launch terminal
// phase (production).
//
// The package is provider-agnostic at the composition surface:
// DetectProvider classifies the remote URL; callers decide whether
// to use Actions push-mode (preferred for GitHub), the Zerops
// native webhook (preferred for GitLab + non-GitHub), or skip CI/CD
// entirely (manual zcli push).
package cicd

import "strings"

// Provider identifies the git hosting provider of a remote URL.
// Closed enum — the workflow-family redesign only distinguishes
// providers Actions push-mode supports natively (GitHub) from those
// that route through the dashboard OAuth webhook flow (GitLab).
type Provider int

const (
	// ProviderGitHub matches github.com remotes. Actions push-mode is
	// the recommended cicd method.
	ProviderGitHub Provider = iota
	// ProviderGitLab matches gitlab.com + self-hosted gitlab.*
	// remotes. The Zerops native webhook (pull-mode) is the
	// recommended cicd method; Actions push-mode is not natively
	// supported.
	ProviderGitLab
	// ProviderUnknown is the fallback for remotes outside the
	// supported set. Callers emit manual `zcli push` guidance.
	ProviderUnknown
)

// String returns the symbolic name for logging + atom guidance.
func (p Provider) String() string {
	switch p {
	case ProviderGitHub:
		return "github"
	case ProviderGitLab:
		return "gitlab"
	case ProviderUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// DetectProvider classifies a git remote URL by host. Empty input
// returns ProviderUnknown. The match is host-substring — handles
// both HTTPS (https://github.com/owner/repo) and SSH
// (git@github.com:owner/repo) URL shapes without parsing.
func DetectProvider(remoteURL string) Provider {
	if remoteURL == "" {
		return ProviderUnknown
	}
	lc := strings.ToLower(remoteURL)
	switch {
	case strings.Contains(lc, "github.com"):
		return ProviderGitHub
	case strings.Contains(lc, "gitlab.com"), strings.Contains(lc, "gitlab."):
		return ProviderGitLab
	default:
		return ProviderUnknown
	}
}
