package ops

import (
	"crypto/sha1" //nolint:gosec // G505: git's object id, matched against git's own (SecretDigest)
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
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

// hqCredentialHelperShell is the helper persisted for a remote on the Mate's
// HQ, which reads git's user as `mate` and the Mate credential as its
// password. The dev service's sessions answer from GIT_TOKEN, the service
// secret git-push-setup writes the credential to; the Mate's shell, which
// runs git on the same repository through the mount, carries no GIT_TOKEN —
// it asks `zcp hq git-credential`, which answers from the enrollment as it is
// now (a re-enrollment rotates the credential) and only for the enrolled HQ's
// own host, handing it git's request on stdin. With neither it answers
// nothing at all, never an empty password, so git goes on to its next helper.
const hqCredentialHelperShell = `!f() { test "$1" = get || return 0; if test -n "$GIT_TOKEN"; then echo username=mate; echo "password=$GIT_TOKEN"; else zcp hq git-credential get; fi; }; f`

// hqCredentialHelperShellInline is hqCredentialHelperShell for one git
// invocation run in a dev service's session, which always carries GIT_TOKEN.
const hqCredentialHelperShellInline = `!f() { test "$1" = get && { echo username=mate; echo "password=$GIT_TOKEN"; }; }; f`

// gitCredentialHelperArgs returns the `-c` git arguments that make ONE git
// invocation authenticate via the session-env helper. The leading empty
// `credential.helper=` RESETS any configured helper list so a stale or
// foreign helper can never answer ahead of the inline one — the
// per-invocation analog of the old single-purpose .netrc file.
func gitCredentialHelperArgs() string {
	return "-c credential.helper= -c credential.helper=" + shellQuote(gitCredentialHelperShell)
}

// hqCredentialHelperArgs is gitCredentialHelperArgs for a remote on the
// Mate's HQ, whose git reads the user `mate`.
func hqCredentialHelperArgs() string {
	return "-c credential.helper= -c credential.helper=" + shellQuote(hqCredentialHelperShellInline)
}

// gitCredentialHelperArgsFor is the per-invocation helper for remoteURL: HQ's
// when the remote is on the Mate's HQ (hqURL, "" on a container not enrolled
// with one), the session-env helper otherwise.
func gitCredentialHelperArgsFor(remoteURL, hqURL string) string {
	if IsHQRemote(remoteURL, hqURL) {
		return hqCredentialHelperArgs()
	}
	return gitCredentialHelperArgs()
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
//
// Against a remote on the Mate's HQ it runs under HQ's bounds (hqGit), so a
// delivery's proof fails fast like its every other step.
func BuildGitSessionAuthProbeCommand(remoteURL, hqURL string) string {
	if IsHQRemote(remoteURL, hqURL) {
		return hqGit(hqCredentialHelperArgs() + " ls-remote " + shellQuote(remoteURL) + " HEAD")
	}
	return fmt.Sprintf(
		"GIT_TERMINAL_PROMPT=0 git %s ls-remote %s HEAD",
		gitCredentialHelperArgsFor(remoteURL, hqURL), shellQuote(remoteURL),
	)
}

// BuildSessionGitTokenDigestCommand prints git's blob hash of the session's
// $GIT_TOKEN — what SecretDigest computes of a credential — so whether a
// credential written onto a service has reached its fresh sessions is read
// without the credential on any command line, and without asking the remote.
func BuildSessionGitTokenDigestCommand() string {
	return `printf %s "$GIT_TOKEN" | git hash-object --stdin`
}

// SecretDigest is git's blob hash of value: what
// BuildSessionGitTokenDigestCommand prints for a session whose GIT_TOKEN is
// value. A digest to compare, never to keep.
func SecretDigest(value string) string {
	sum := sha1.Sum(fmt.Appendf(nil, "blob %d\x00%s", len(value), value)) //nolint:gosec // G401: git's object id, matched against git's own, not a protection
	return hex.EncodeToString(sum[:])
}

// GitCredentialRefused reports whether a git command's output says the remote
// refused its credential: git's own "Authentication failed" for a 401, or a
// 401 or 403 it reports. Not reached, a 5xx or a missing repository is not.
func GitCredentialRefused(output string) bool {
	return strings.Contains(output, "Authentication failed") ||
		strings.Contains(output, "returned error: 401") || strings.Contains(output, "returned error: 403")
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
func BuildGitReconstructCommand(workingDir, remoteURL, hqURL string, identity GitIdentity) string {
	quoted := shellQuote(remoteURL)
	return fmt.Sprintf(
		`cd %s && if test ! -d .git; then git init -q -b main && %s && git remote add origin %s && %s && GIT_TERMINAL_PROMPT=0 git %s fetch -q origin HEAD && git update-ref refs/heads/main FETCH_HEAD && git reset -q FETCH_HEAD; fi`,
		shellQuote(workingDir),
		gitIdentityEnsureFragmentFor(identity),
		quoted,
		gitCredentialHelperConfigFragment(remoteURL, hqURL),
		gitCredentialHelperArgsFor(remoteURL, hqURL),
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
// The helper's text depends on the host: a remote on the Mate's HQ (hqURL,
// "" on a container not enrolled with one) gets the helper that also answers
// the Mate's shell (hqCredentialHelperShell); every other host answers
// GIT_TOKEN only. HQ's scope carries its port when it is not 443
// (hqCredentialScope). A remote whose host cannot be a scope (an IPv6
// literal, a name with an underscore, metacharacters) gets no helper at all:
// parseGitHost's github.com default is never a scope, since a helper stored
// there would answer github.com with this remote's token.
//
// The trailing `rm -f ~/.netrc` is the one-way migration off the
// ephemeral-.netrc era: any stray fail-open residue dies the first time
// the single owner re-asserts wiring.
func gitCredentialHelperConfigFragment(remoteURL, hqURL string) string {
	return gitCredentialHelperWriteFragment(remoteURL, hqURL) + " && rm -f ~/.netrc"
}

// gitCredentialHelperWriteFragment is the helper write alone — the no-op `:` when
// the remote's host is no scope to write it under.
func gitCredentialHelperWriteFragment(remoteURL, hqURL string) string {
	scope, ok := gitCredentialScopeHost(remoteURL)
	if !ok {
		return ":"
	}
	helper := gitCredentialHelperShell
	if hqScope, onHQ := hqCredentialScope(remoteURL, hqURL, scope); onHQ {
		scope, helper = hqScope, hqCredentialHelperShell
	}
	return fmt.Sprintf("git config %s %s",
		shellQuote("credential.https://"+scope+".helper"),
		shellQuote(helper),
	)
}

// IsHQRemote reports whether remoteURL is a repository on the Mate's HQ:
// https, on HQ's own host and port. hqURL is HQ's address, "" when this
// container is not enrolled with one.
func IsHQRemote(remoteURL, hqURL string) bool {
	if hqURL == "" {
		return false
	}
	remote, err := url.Parse(remoteURL)
	if err != nil || !strings.EqualFold(remote.Scheme, httpsScheme) {
		return false
	}
	hq, err := url.Parse(hqURL)
	if err != nil || !strings.EqualFold(hq.Scheme, httpsScheme) || hq.Hostname() == "" {
		return false
	}
	return strings.EqualFold(remote.Hostname(), hq.Hostname()) && httpsPort(remote) == httpsPort(hq)
}

// hqCredentialScope is the scope HQ's helper is stored under — HQ's host,
// with its port when it names one other than 443, since git matches a
// scope's port exactly — and whether remoteURL is on the Mate's HQ at all.
// host is the scope host the remote already resolved to: the helper that
// falls back to `zcp hq git-credential` is written only when it and HQ's
// address name the same host.
func hqCredentialScope(remoteURL, hqURL, host string) (string, bool) {
	if !IsHQRemote(remoteURL, hqURL) {
		return "", false
	}
	remote, err := url.Parse(remoteURL)
	if err != nil || !strings.EqualFold(remote.Hostname(), host) {
		return "", false
	}
	if port := httpsPort(remote); port != defaultHTTPSPort {
		return host + ":" + port, true
	}
	return host, true
}

// The persisted helper's scope is always https (credential.https://…); a
// remote over any other scheme gets the plain helper.
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
// Mate's shell could authenticate to its HQ keeps the old text until
// git-push-setup syncs origin again; the push-credential step runs this before
// each delivery, so such a repository heals on its next one. No repository,
// nothing written: the command never creates one. It writes the helper and
// nothing else — the one-way ~/.netrc cleanup stays with origin sync and
// reconstruction, since nothing ZCP runs writes that file any more and one
// there now is the user's own.
func BuildGitCredentialHelperAssertCommand(workingDir, remoteURL, hqURL string) string {
	return fmt.Sprintf("cd %s && if test -d .git; then %s; fi",
		shellQuote(workingDir), gitCredentialHelperWriteFragment(remoteURL, hqURL))
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
