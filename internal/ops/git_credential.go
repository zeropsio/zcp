package ops

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// gitCredentialHelperShell is the inline git credential helper that replaced
// the ephemeral-.netrc pattern (spec-git-delivery-target §4). It answers the
// credential protocol's "get" action by emitting username/password from the
// INVOKING session's $GIT_TOKEN at request time.
//
// Why session env is the right read surface: GIT_TOKEN's single validated
// home is the push source's service-scope SECRET env; every ZCP git
// operation runs in a FRESH SSH session, and fresh sessions see a rotated
// env value within seconds of the platform write — no restart, no file read
// (live-verified on eval-zcp 2026-06-10, alpine/bun runtime). The secret
// travels helper-stdout → git over an anonymous pipe: never in argv, never
// on disk, no residue to clean up (the trap-based ~/.netrc cleanup this
// replaces failed open on SIGKILL/transport drop).
//
// "store"/"erase" actions fall through silently — there is nothing to
// persist; the platform env IS the store.
const gitCredentialHelperShell = `!f() { test "$1" = get && { echo username=oauth2; echo "password=$GIT_TOKEN"; }; }; f`

// giteaCredentialHelperShell is the helper persisted for a remote on the
// Mate's own Gitea. The dev service's sessions answer from GIT_TOKEN as above;
// the Mate's shell, which runs git on the same repository through the mount,
// carries no GIT_TOKEN — the bot's token reaches it as GITEA_TOKEN. That
// value is the one the shell started with: the broker rotates the token on
// the zcp service, and a service env change reaches a running process only
// with a restart. So the helper asks `zcp mate git-token` for the token as
// the container's live env store holds it now, handing it git's request on
// stdin (mate.GiteaToken answers only the Gitea's own host), and falls back
// to $GITEA_TOKEN where zcp does not answer — a container without one, one
// that predates the verb, or a request it declines. With no token anywhere
// it answers nothing at all, never an empty password, so git goes on to its
// next helper. Only this host's helper reads the bot's token: a remote
// anywhere else never receives it.
const giteaCredentialHelperShell = `!f() { test "$1" = get && { t=$GIT_TOKEN; test -n "$t" || t=$(zcp mate git-token 2>/dev/null) || t=$GITEA_TOKEN; test -n "$t" && { echo username=oauth2; echo "password=$t"; }; }; }; f`

// gitCredentialHelperArgs returns the `-c` git arguments that make ONE git
// invocation authenticate via the session-env helper. The leading empty
// `credential.helper=` RESETS any configured helper list so a stale or
// foreign helper can never answer ahead of the inline one — the
// per-invocation analog of the old single-purpose .netrc file.
func gitCredentialHelperArgs() string {
	return "-c credential.helper= -c credential.helper=" + shellQuote(gitCredentialHelperShell)
}

// BuildGitAuthedLsRemoteCommand builds the single authenticated remote-ref
// read: `git ls-remote <url> <ref>` via the session-env credential helper,
// emitting the bare SHA (or nothing). Shared by the launch push-proof so
// tools/ carries no inline auth duplicates (the 2026-05-28 audit-ordered
// consolidation). `|| true` keeps git-state problems (no token, unreachable
// remote) flowing as EMPTY OUTPUT — state evidence — while SSH/exec
// failures still surface as transport errors to the caller.
//
// ref is the tracked ref (GF-7, docs/spec-workflows.md §12.6) — the caller
// resolves it from ServiceMeta.TrackedRef (falling back to "main") rather
// than passing the symbolic "HEAD" ref, so the compare is against the
// SPECIFIC branch a target consumes, not whatever the remote happens to
// consider its default branch.
func BuildGitAuthedLsRemoteCommand(remoteURL, ref string) string {
	return fmt.Sprintf(
		"GIT_TERMINAL_PROMPT=0 git %s ls-remote %s %s 2>/dev/null | head -1 | cut -f1 || true",
		gitCredentialHelperArgs(), shellQuote(remoteURL), shellQuote(ref),
	)
}

// BuildGitSessionAuthProbeCommand builds the post-write verification probe: an
// authenticated remote-HEAD read WITHOUT the inline candidate token — it
// authenticates from the SESSION's $GIT_TOKEN,
// proving the whole chain (platform env store → fresh-SSH-session env →
// helper → remote) end-to-end before git-push-setup stamps `configured`.
// Fails loud (no `|| true`): a non-zero exit IS the signal the env value
// has not propagated yet (caller retries within the ~5-10s zembed window)
// or the write landed wrong.
func BuildGitSessionAuthProbeCommand(remoteURL string) string {
	return fmt.Sprintf(
		"GIT_TERMINAL_PROMPT=0 git %s ls-remote %s HEAD",
		gitCredentialHelperArgs(), shellQuote(remoteURL),
	)
}

// BuildGitReconstructCommand rebuilds a missing /var/www/.git from the
// recorded remote (spec-git-delivery-target §5 / gate blocker
// git-state-missing): init on main + identity (set-if-absent, from the
// supplied identity — F3: pass a GitHub-derived identity when available so
// a reconstructed repo lands human-attributed from the first init, or
// ops.DeployGitIdentity for the robot fallback when no derivation ran) +
// origin + persistent credential helper, then an authenticated fetch of
// the remote HEAD and a MIXED reset onto it — the index aligns to the
// remote tree while the WORKING TREE is never touched, so nothing on the
// container can be lost. When the artifact tree matches the pushed code
// (the normal case after a CI build), `git status` comes out clean; any
// genuine divergence stays visible as uncommitted changes for the caller
// to report honestly.
//
// Guarded by `test ! -d .git` — on a present repo the `if` condition is
// false and the whole command no-ops (a bare `if...then...fi` with no
// `else` exits 0 on a false condition; nothing extra is needed), so
// callers may run it idempotently after a presence check without a
// TOCTOU window.
// Auth: the SESSION env credential helper — reconstruction only runs for
// pairs whose GIT_TOKEN service secret already exists.
func BuildGitReconstructCommand(workingDir, remoteURL, giteaURL string, identity GitIdentity) string {
	quoted := shellQuote(remoteURL)
	return fmt.Sprintf(
		`cd %s && if test ! -d .git; then git init -q -b main && %s && git remote add origin %s && %s && GIT_TERMINAL_PROMPT=0 git %s fetch -q origin HEAD && git update-ref refs/heads/main FETCH_HEAD && git reset -q FETCH_HEAD; fi`,
		shellQuote(workingDir),
		gitIdentityEnsureFragmentFor(identity),
		quoted,
		gitCredentialHelperConfigFragment(remoteURL, giteaURL),
		gitCredentialHelperArgs(),
	)
}

// SelfBuildTarget reports whether a delivery's build target IS its push
// source — the single predicate that owns ".git ships in the artifact"
// (spec-git-delivery-target §5). ZCP's own ssh deploys derive `zcli push
// -g` from it, and the emitted GitHub Actions workflow template must
// mirror it: a CI build of the push source without -g replaces the
// container with an artifact carrying no /var/www/.git, destroying the
// origin + history the launch gate reads (the prod.txt T2 wipe spiral).
// Cross-builds (pair dev→stage) correctly stay git-less — ZCP never
// reads git state from a build-only target.
func SelfBuildTarget(pushSource, buildTarget string) bool {
	return pushSource != "" && pushSource == buildTarget
}

// gitCredentialHelperConfigFragment returns the shell fragment that
// persists the helper into the repo's .git/config, url-scoped to the
// remote's host (`credential.https://<host>.helper` — a GLOBAL helper
// would answer for ANY https host, including an untrusted second remote;
// url-scoping is parity with the retired .netrc `machine <host>` line).
//
// Persisting the helper serves git invocations OUTSIDE ZCP's own commands
// (manual `ssh <host> git push`, the Mate's own shell on the mount, user
// tooling) — ZCP's own operations carry the helper per-invocation via
// gitCredentialHelperArgs and do not depend on this config state. Because
// the helper lives in .git/config, it rides the `-g` artifact into
// replacement containers exactly like the deploy identity does.
//
// The helper's text depends on the host: a remote on the Mate's Gitea
// (giteaURL, "" on a container with no Gitea wiring) gets the helper that
// also answers the Mate's shell (giteaCredentialHelperShell); every other
// host answers GIT_TOKEN only. The Gitea helper's scope carries the
// Gitea's port when it is not 443 (giteaCredentialScope). A remote whose host cannot be a scope (an
// IPv6 literal, a name with an underscore, metacharacters) gets no helper at
// all: parseGitHost's github.com default is never a scope, since a helper
// stored there would answer github.com with this remote's token.
//
// The trailing `rm -f ~/.netrc` is the one-way migration off the
// ephemeral-.netrc era: any stray fail-open residue dies the first time
// the single owner re-asserts wiring.
func gitCredentialHelperConfigFragment(remoteURL, giteaURL string) string {
	return gitCredentialHelperWriteFragment(remoteURL, giteaURL) + " && rm -f ~/.netrc"
}

// gitCredentialHelperWriteFragment is the helper write alone — the no-op `:` when
// the remote's host is no scope to write it under.
func gitCredentialHelperWriteFragment(remoteURL, giteaURL string) string {
	scope, ok := gitCredentialScopeHost(remoteURL)
	if !ok {
		return ":"
	}
	helper := gitCredentialHelperShell
	if giteaScope, onGitea := giteaCredentialScope(remoteURL, giteaURL, scope); onGitea {
		scope, helper = giteaScope, giteaCredentialHelperShell
	}
	return fmt.Sprintf("git config %s %s",
		shellQuote("credential.https://"+scope+".helper"),
		shellQuote(helper),
	)
}

// giteaCredentialScope is the scope the Gitea helper is stored under — the
// Gitea's host, with its port when it names one other than 443, since git
// matches a scope's port exactly — and whether remoteURL is on the Mate's
// Gitea at all. host is the scope host the remote already resolved to: the
// helper that falls back to the bot's token is written only when it and the
// forge classification name the same host, on the Gitea's own port.
func giteaCredentialScope(remoteURL, giteaURL, host string) (string, bool) {
	if topology.ClassifyGitHost(remoteURL, giteaURL) != topology.GitHostGitea {
		return "", false
	}
	remote, err := url.Parse(remoteURL)
	if err != nil || remote.Scheme != httpsScheme {
		return "", false
	}
	gitea, err := url.Parse(giteaURL)
	if err != nil || !strings.EqualFold(remote.Hostname(), host) || !strings.EqualFold(gitea.Hostname(), host) {
		return "", false
	}
	port := httpsPort(remote)
	if port != httpsPort(gitea) {
		return "", false
	}
	if port == defaultHTTPSPort {
		return host, true
	}
	return host + ":" + port, true
}

// The persisted helper's scope is always https (credential.https://…); a
// Gitea remote over any other scheme gets the plain helper.
const (
	httpsScheme      = "https"
	defaultHTTPSPort = "443"
)

// httpsPort is the port an https URL connects to.
func httpsPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	return defaultHTTPSPort
}

// gitCredentialScopeHost is the host a persisted helper is stored under, and
// whether the remote has one: parseGitHost's answer only when it is the
// remote's own host rather than its default.
func gitCredentialScopeHost(remoteURL string) (string, bool) {
	host := parseGitHost(remoteURL)
	if host != defaultGitHost {
		return host, true
	}
	if u, err := url.Parse(remoteURL); err == nil && strings.Contains(remoteURL, "://") {
		return host, strings.EqualFold(u.Hostname(), defaultGitHost)
	}
	return host, strings.HasPrefix(strings.ToLower(remoteURL), defaultGitHost+"/")
}

// BuildGitCredentialHelperAssertCommand re-persists the helper origin sync
// writes, on a repository that already exists. A helper persisted before the
// Mate's shell could authenticate to its Gitea keeps the old text until
// git-push-setup syncs origin again; the push-credential step runs this before
// each delivery, so such a repository heals on its next one. No repository,
// nothing written: the command never creates one. It writes the helper and
// nothing else — the one-way ~/.netrc cleanup stays with origin sync and
// reconstruction, since nothing ZCP runs writes that file any more and one
// there now is the user's own.
func BuildGitCredentialHelperAssertCommand(workingDir, remoteURL, giteaURL string) string {
	return fmt.Sprintf("cd %s && if test -d .git; then %s; fi",
		shellQuote(workingDir), gitCredentialHelperWriteFragment(remoteURL, giteaURL))
}

// BuildGitTagListCommand lists the remote's version tags (authenticated —
// works for private repos too) for the release act's next-version
// suggestion. Output: one `<sha>\trefs/tags/vX.Y.Z` line per tag.
func BuildGitTagListCommand(remoteURL string) string {
	return fmt.Sprintf(
		"GIT_TERMINAL_PROMPT=0 git %s ls-remote --tags %s 'v*' || true",
		gitCredentialHelperArgs(), shellQuote(remoteURL),
	)
}

// BuildGitTagPushCommand creates an annotated release tag at the CURRENT
// HEAD and pushes it (spec-git-delivery-target §7). The caller has
// already verified clean-tree + HEAD-on-remote (the P-LP-11 read) — the
// tag therefore names exactly the pushed state the production pipeline
// will build. Auth via the session-env credential helper.
func BuildGitTagPushCommand(workingDir, version string) string {
	qv := shellQuote(version)
	return fmt.Sprintf(
		"cd %s && git tag -a %s -m %s && GIT_TERMINAL_PROMPT=0 git %s push origin %s",
		shellQuote(workingDir), qv, shellQuote("release "+version), gitCredentialHelperArgs(), qv,
	)
}
