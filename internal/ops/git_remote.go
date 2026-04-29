package ops

import (
	"net/url"
	"strings"
)

// ParseGitRemoteOwnerRepo extracts the owner + repo segment from a git remote
// URL. Accepts the three shapes ServiceMeta.RemoteURL may carry:
//
//   - URI form HTTPS:  https://github.com/owner/repo[.git][/]
//   - URI form SSH:    ssh://git@github.com/owner/repo[.git]
//   - scp-form SSH:    git@github.com:owner/repo[.git]
//
// Returns (owner, repo, true) on a clean parse; (\"\", \"\", false) when the
// shape is unrecognized or the path doesn't yield two non-empty segments.
//
// The returned repo is normalized: trailing \".git\" is stripped, trailing
// slash is dropped. Owner and repo are returned as-found (no case folding;
// GitHub treats owners as case-insensitive but the literal value is what the
// agent will paste into `gh secret set -R owner/repo`).
//
// Used by handleBuildIntegration to splice owner/repo into prefilled
// `gh secret set` snippets for the Actions integration handoff.
func ParseGitRemoteOwnerRepo(remote string) (owner, repo string, ok bool) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", "", false
	}

	// scp-form (git@host:owner/repo[.git]) has no scheme; detect by the
	// pattern user@host:path with no slash before the colon.
	if !strings.Contains(remote, "://") {
		if at := strings.Index(remote, "@"); at != -1 {
			if colon := strings.Index(remote[at:], ":"); colon != -1 {
				path := remote[at+colon+1:]
				return splitOwnerRepo(path)
			}
		}
		return "", "", false
	}

	u, err := url.Parse(remote)
	if err != nil {
		return "", "", false
	}
	return splitOwnerRepo(u.Path)
}

// splitOwnerRepo takes the path segment of a git URL (everything after the
// host) and returns the last two non-empty segments as owner/repo. Strips
// leading slash, trailing slash, and trailing .git suffix.
func splitOwnerRepo(path string) (owner, repo string, ok bool) {
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner = parts[len(parts)-2]
	repo = parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}
