package topology

import "strings"

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
