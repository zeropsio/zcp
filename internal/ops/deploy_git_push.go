package ops

import (
	"fmt"
	"net/url"
	"strings"
)

const defaultGitHost = "github.com"
const defaultBranch = "main"

// BuildGitPushCommand builds an SSH command that pushes committed code to an
// external git remote (GitHub/GitLab). The tool handler's committed-code
// pre-flight guarantees .git exists and HEAD points at a commit before this
// fires — no git init, no auto-commit fallback. Those fallbacks used to
// masquerade "agent forgot to commit" as successful pushes of empty state.
//
// Identity is NOT set here. Bootstrap's InitServiceGit wrote agent@zerops.io
// into /var/www/.git/config the moment the service was mounted (GLC-1), and
// that value persists across deploys on the container's filesystem. Re-
// applying it on every git-push was redundant; the identity check belongs
// exclusively to InitServiceGit (at bootstrap time) and the deploy_ssh
// atomic safety-net (as migration/recovery fallback for services without
// .git/). Callers outside bootstrap (hypothetical — none today) hit the
// pre-flight's "HEAD must exist" guard first; the correct remediation
// there is to initialize the repo, not to patch identity onto a missing
// .git/.
//
// Security: .netrc created with trap-based cleanup (runs even on failure),
// umask 077 prevents world-readable token, remoteURL is shell-quoted.
func BuildGitPushCommand(workingDir, remoteURL, branch string) string {
	if branch == "" {
		branch = defaultBranch
	}
	host := parseGitHost(remoteURL)

	var parts []string

	// Trap-based cleanup: .netrc removed on exit regardless of success/failure.
	parts = append(parts, "trap 'rm -f ~/.netrc' EXIT")

	// Auth: .netrc from $GIT_TOKEN env var (never in command args or git config).
	netrc := fmt.Sprintf(
		`umask 077 && echo "machine %s login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc`,
		host,
	)
	parts = append(parts, netrc)

	// Working directory.
	parts = append(parts, fmt.Sprintf("cd %s", workingDir))

	// Remote setup (idempotent): only if remoteURL provided.
	if remoteURL != "" {
		quoted := shellQuote(remoteURL)
		parts = append(parts, fmt.Sprintf(
			"(git remote add origin %s 2>/dev/null || git remote set-url origin %s)",
			quoted, quoted,
		))
	}

	// Push. Pre-flight guarantees there is at least one commit to push.
	parts = append(parts, fmt.Sprintf("git push -u origin %s", branch))

	return strings.Join(parts, " && ")
}

// parseGitHost extracts the hostname from a git remote URL.
// Supports https://host/..., http://host/..., and host:port formats.
// Returns "github.com" as default if parsing fails or URL is empty.
func parseGitHost(rawURL string) string {
	if rawURL == "" {
		return defaultGitHost
	}

	// Try standard URL parsing.
	if strings.Contains(rawURL, "://") {
		u, err := url.Parse(rawURL)
		if err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}

	// Fallback for URLs without scheme (e.g., "github.com/user/repo").
	if idx := strings.Index(rawURL, "/"); idx > 0 {
		host := rawURL[:idx]
		// Strip port if present.
		if colonIdx := strings.LastIndex(host, ":"); colonIdx > 0 {
			host = host[:colonIdx]
		}
		if host != "" {
			return host
		}
	}

	return defaultGitHost
}
