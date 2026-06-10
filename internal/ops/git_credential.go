package ops

import (
	"fmt"
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

// gitCredentialHelperArgs returns the `-c` git arguments that make ONE git
// invocation authenticate via the session-env helper. The leading empty
// `credential.helper=` RESETS any configured helper list so a stale or
// foreign helper can never answer ahead of the inline one — the
// per-invocation analog of the old single-purpose .netrc file.
func gitCredentialHelperArgs() string {
	return "-c credential.helper= -c credential.helper=" + shellQuote(gitCredentialHelperShell)
}

// BuildGitAuthedLsRemoteCommand builds the single authenticated remote-HEAD
// read: `git ls-remote <url> HEAD` via the session-env credential helper,
// emitting the bare SHA (or nothing). Shared by the launch push-proof so
// tools/ carries no inline auth duplicates (the 2026-05-28 audit-ordered
// consolidation). `|| true` keeps git-state problems (no token, unreachable
// remote) flowing as EMPTY OUTPUT — state evidence — while SSH/exec
// failures still surface as transport errors to the caller.
func BuildGitAuthedLsRemoteCommand(remoteURL string) string {
	return fmt.Sprintf(
		"GIT_TERMINAL_PROMPT=0 git %s ls-remote %s HEAD 2>/dev/null | head -1 | cut -f1 || true",
		gitCredentialHelperArgs(), shellQuote(remoteURL),
	)
}

// BuildGitSessionAuthProbeCommand builds the post-write verification probe:
// the SAME authenticated read as BuildGitAuthProbeCommand but WITHOUT the
// inline candidate token — it authenticates from the SESSION's $GIT_TOKEN,
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
// git-state-missing): init on main + deploy identity + origin + persistent
// credential helper, then an authenticated fetch of the remote HEAD and a
// MIXED reset onto it — the index aligns to the remote tree while the
// WORKING TREE is never touched, so nothing on the container can be lost.
// When the artifact tree matches the pushed code (the normal case after a
// CI build), `git status` comes out clean; any genuine divergence stays
// visible as uncommitted changes for the caller to report honestly.
//
// Guarded by `test ! -d .git` — on a present repo the command no-ops
// (exit 0 via the trailing `|| true` on the guard), so callers may run it
// idempotently after a presence check without a TOCTOU window.
// Auth: the SESSION env credential helper — reconstruction only runs for
// pairs whose GIT_TOKEN service secret already exists.
func BuildGitReconstructCommand(workingDir, remoteURL string) string {
	quoted := shellQuote(remoteURL)
	return fmt.Sprintf(
		`cd %s && if test ! -d .git; then git init -q -b main && git config user.email %s && git config user.name %s && git remote add origin %s && %s && GIT_TERMINAL_PROMPT=0 git %s fetch -q origin HEAD && git update-ref refs/heads/main FETCH_HEAD && git reset -q FETCH_HEAD; fi`,
		shellQuote(workingDir),
		shellQuote(DeployGitIdentity.Email), shellQuote(DeployGitIdentity.Name),
		quoted,
		gitCredentialHelperConfigFragment(remoteURL),
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
// parseGitHost stays the single host-derivation owner.
//
// Persisting the helper serves git invocations OUTSIDE ZCP's own commands
// (manual `ssh <host> git push`, user tooling) — ZCP's own operations carry
// the helper per-invocation via gitCredentialHelperArgs and do not depend
// on this config state. Because the helper lives in .git/config, it rides
// the `-g` artifact into replacement containers exactly like the deploy
// identity does.
//
// The trailing `rm -f ~/.netrc` is the one-way migration off the
// ephemeral-.netrc era: any stray fail-open residue dies the first time
// the single owner re-asserts wiring.
func gitCredentialHelperConfigFragment(remoteURL string) string {
	host := parseGitHost(remoteURL)
	return fmt.Sprintf("git config %s %s && rm -f ~/.netrc",
		shellQuote("credential.https://"+host+".helper"),
		shellQuote(gitCredentialHelperShell),
	)
}

// BuildGitUnauthenticatedLsRemoteCommand probes whether the remote is
// PUBLICLY clonable: helper list reset, NO credential supplied. The
// launch-production buildFromGit clone runs as the launch-created machine
// clientUser, which provably holds no GitHub OAuth grant — a private repo
// passes every authenticated ZCP gate and then fails the platform clone
// with the no-logs 0.3s shape AFTER the project exists (foundations S5).
// Exit status is the signal: 0 = public; non-zero = private/unreachable.
func BuildGitUnauthenticatedLsRemoteCommand(remoteURL string) string {
	return fmt.Sprintf(
		"GIT_TERMINAL_PROMPT=0 git -c credential.helper= ls-remote %s HEAD >/dev/null 2>&1 && echo public || echo not-public",
		shellQuote(remoteURL),
	)
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
