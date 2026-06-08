package topology

import (
	"net/url"
	"strings"
)

// RedactRepoURLCredentials strips embedded credentials (the `user:password@`
// or `user@` userinfo of an http(s) URL) before a repo URL is echoed into any
// agent-facing payload. A live `git remote get-url origin` can carry a full PAT
// (`https://user:ghp_xxx@github.com/owner/repo`) the user pasted manually, and
// ZCP must never reflect that secret back into a response, a blocker message, or
// a drift warning. Single owner: every echo site routes through here so a new
// surface cannot reintroduce the leak.
//
// Non-http(s) forms (scp-style git@host:owner/repo, ssh://) and unparseable
// strings are returned unchanged — scp userinfo is a host login, not a secret,
// and a parse failure must not silently blank a value the agent needs to see.
// The replacement keeps the structure visible: the userinfo becomes `***`.
func RedactRepoURLCredentials(remote string) string {
	trimmed := strings.TrimSpace(remote)
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		return remote
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.User == nil {
		return remote
	}
	// url.User("***").String() percent-encodes the mask; do the replacement on
	// the raw string instead so the masked value stays human-readable. Parsing
	// above guarantees the first `@` before any path separator IS the userinfo
	// delimiter, so this surgery is safe.
	const sep = "://"
	scheme, rest, _ := strings.Cut(trimmed, sep)
	_, afterAt, _ := strings.Cut(rest, "@")
	return scheme + sep + "***@" + afterAt
}

// CanonicalRepoURL normalizes a git repository URL to the form Zerops'
// `buildFromGit` clone-preflight accepts: no surrounding whitespace, no
// trailing slash, no trailing ".git" suffix.
//
// Why this matters: a trailing ".git" — the conventional git clone-URL form
// (https://github.com/owner/repo.git) that `git remote get-url origin` and
// most "copy clone URL" buttons hand out — makes the platform's clone-preflight
// REJECT the import. The build reaches terminal FAILED in ~0.3s with no build
// container and no logs; the same URL without ".git" builds cleanly. ZCP owns
// what it writes into `buildFromGit`, so it must emit the canonical form.
//
// Scope is deliberately narrow suffix canonicalization for repo IDENTITY:
// trim space, one trailing slash, one trailing ".git", then a trailing slash
// again (covers the ".git/" combo). Host, scheme, auth, path case, and the
// scp-form (git@host:owner/repo) are preserved untouched — never rewritten.
//
// Idempotent: CanonicalRepoURL(CanonicalRepoURL(x)) == CanonicalRepoURL(x),
// so emit sites and identity-comparison sites can apply it freely.
func CanonicalRepoURL(remote string) string {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, "/")
	remote = strings.TrimSuffix(remote, ".git")
	remote = strings.TrimSuffix(remote, "/")
	return remote
}
