# One-collect GitHub credential flow (ZCP as GitHub actor)

**Status:** open deferral (Karel decision 2026-06-10: backlog; gh-CLI stays the trust boundary).

**What:** collect ONE GitHub credential at git-push-setup (scopes derived from the chosen
integration: Contents rw; +Secrets rw +Workflows rw for actions) and, while it is in-request, do ALL
GitHub-side work directly: verify git, set the two Actions repo secrets via the GitHub REST API
(sealed-box encryption via `golang.org/x/crypto/nacl/box` after `GET .../actions/secrets/public-key`),
commit the workflow file via the contents API. The gh-CLI path SURVIVES as the fallback (non-GitHub
hosts, scope-short PATs) — deleting it would re-create the B1 class.

**Why deferred:** ZCP making authenticated WRITES to the user's GitHub account is a new trust
surface + a new external API coupling. The cheap UX gaps shipped instead (F6 credentialsRequired
typed asks + wait-for-user contract; F5 service-scope GIT_TOKEN).

**Sketch:** new internal GitHub API client (raw HTTP, logfetcher precedent — no go-github dep);
git-push-setup confirm gains optional `integration` execution; build-integration demotes to
declaration + verification only. Earn signals from F1 (workflow-file-at-pushed-HEAD, platform
integration read) stay the verifiers.

**Risks:** GitHub API shape coupling; secrets write needs the sealed-box dance; PAT scope
insufficiency surfaces only at call time; trust framing must be explicit in the ask.

**Promote when:** Karel green-lights ZCP-as-GitHub-actor, OR the measured friction of the manual
4-step Actions setup (post-F1 declared/verified split) stays high in flow-eval retrospectives.
