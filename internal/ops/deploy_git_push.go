package ops

import (
	"fmt"
	"net/url"
	"regexp"
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
// Identity is NOT set here. Bootstrap's InitServiceGit fills it (set-if-
// absent — never stomping a user-set value) the moment the service is
// mounted (GLC-1), and repo-local identity is persistent + user-owned from
// then on. Re-asserting it on every git-push was both redundant and, before
// the set-if-absent fix, actively harmful (it re-clobbered a user's own
// fix on the next deploy); the identity-fill responsibility belongs to the
// small set of self-heal sites (InitServiceGit at bootstrap, the
// buildSSHCommand safety-net, BuildGitOriginSyncCommand, and
// BuildGitReconstructCommand — all composing the same
// gitIdentityEnsureFragment single owner). Callers outside bootstrap
// (hypothetical — none today) hit the pre-flight's "HEAD must exist" guard
// first; the correct remediation there is to initialize the repo, not to
// patch identity onto a missing .git/.
//
// Security: auth flows through the inline credential helper reading the
// SESSION's $GIT_TOKEN (fresh per SSH session — live within seconds of a
// rotation, no restart; spec-git-delivery-target §4). The secret travels
// helper-stdout → git over an anonymous pipe: never in argv, never on
// disk — the trap-cleaned ~/.netrc this replaced failed open on SIGKILL.
// remoteURL and branch are shell-quoted.
func BuildGitPushCommand(workingDir, remoteURL, branch string) string {
	if branch == "" {
		branch = defaultBranch
	}

	var parts []string

	// Working directory (agent-supplied — shell-quote).
	parts = append(parts, fmt.Sprintf("cd %s", shellQuote(workingDir)))

	// Remote setup (idempotent): only if remoteURL provided.
	if remoteURL != "" {
		quoted := shellQuote(remoteURL)
		parts = append(parts, fmt.Sprintf(
			"(git remote add origin %s 2>/dev/null || git remote set-url origin %s)",
			quoted, quoted,
		))
	}

	// Push via the session-env credential helper. GIT_TERMINAL_PROMPT=0:
	// an empty/missing $GIT_TOKEN must fail fast as a credential error,
	// never hang the MCP session on a username/password prompt.
	parts = append(parts, fmt.Sprintf(
		"GIT_TERMINAL_PROMPT=0 git %s push -u origin %s",
		gitCredentialHelperArgs(), shellQuote(branch),
	))

	return strings.Join(parts, " && ")
}

// gitHostnameRe matches a valid hostname (letters, digits, dots, hyphens).
// Used to reject anything else extracted from a remote URL before it is
// interpolated into the url-scoped credential config key
// (`credential.https://<host>.helper`). The key is shell-quoted at the emit
// site, so this is defense in depth (B3 heritage) plus url-scoping
// correctness — a malformed host must not silently widen the helper's match.
var gitHostnameRe = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

// parseGitHost extracts the hostname from a git remote URL.
// Supports https://host/..., http://host/..., and host:port formats.
// Returns "github.com" as default if parsing fails, the URL is empty, or the
// extracted host is not a syntactically valid hostname (so it can never carry
// shell metacharacters into the credential config key).
func parseGitHost(rawURL string) string {
	if rawURL == "" {
		return defaultGitHost
	}

	// Scheme-prefixed URL: trust url.Parse's hostname only when it is a valid
	// hostname; otherwise return the default (do NOT fall through to the
	// scheme-less fallback, which would mis-parse the scheme as the host).
	if strings.Contains(rawURL, "://") {
		u, err := url.Parse(rawURL)
		if err == nil && gitHostnameRe.MatchString(u.Hostname()) {
			return u.Hostname()
		}
		return defaultGitHost
	}

	// Fallback for URLs without scheme (e.g., "github.com/user/repo").
	if idx := strings.Index(rawURL, "/"); idx > 0 {
		host := rawURL[:idx]
		// Strip port if present.
		if colonIdx := strings.LastIndex(host, ":"); colonIdx > 0 {
			host = host[:colonIdx]
		}
		if gitHostnameRe.MatchString(host) {
			return host
		}
	}

	return defaultGitHost
}
