# Auto-wire GitHub Actions secret via GitHub API

**Surfaced**: 2026-04-29 — during `build-integration=actions` confirm-response
work. Live-agent feedback that the agent had to be told `ZCP_API_KEY` ↔
`ZEROPS_TOKEN` equivalence and to run `gh secret set` manually. User signed
off on the snippet-based fix as the immediate scope and parked the
zero-touch GitHub-API approach for a later round.

**Why deferred**: First-iteration fix is to enrich the confirm response with
prefilled `gh secret set` snippets + the reuse hint, which solves ~90% of
the UX problem. Going one step further — having ZCP itself POST the secret
to GitHub via the REST API — needs sealed-encryption (libsodium-style box)
implementation in Go (~200 LOC), GitHub fine-grained PAT scope detection
(so we fail-fast when `Secrets: Read and write` is missing), and graceful
fallback to the manual `gh secret set` path. Out of scope for the immediate
UX fix; worth doing once the snippet approach is shipped and we see whether
agents actually run the snippets without friction.

**Trigger to promote**: real-world feedback that agents skip / mis-run the
`gh secret set` snippets, or that users explicitly ask for zero-touch setup.
Either signal flips this from "nice to have" to "next iteration".

## Sketch

- New optional parameter on `zerops_workflow action="build-integration"`:
  `autoWireSecret=true` (default false).
- When set, after stamping `BuildIntegration=actions`:
  1. Read project env `GIT_TOKEN` value via `ops.FetchProjectEnv` (must
     exist + have `Secrets: Read and write` scope on the target repo).
  2. Parse `meta.RemoteURL` to `{owner}/{repo}`.
  3. `GET /repos/{owner}/{repo}/actions/secrets/public-key` to fetch the
     repo's public key.
  4. Sealed-box encrypt `os.Getenv("ZCP_API_KEY")` against the public key
     (`golang.org/x/crypto/nacl/box` via `crypto_box_seal` semantics;
     existing GitHub libs like `go-github` have helpers).
  5. `PUT /repos/{owner}/{repo}/actions/secrets/ZEROPS_TOKEN` with
     `encrypted_value` + `key_id`.
  6. Same for `ZEROPS_SERVICE_ID` (no encryption-of-secret-content concern
     since it's a numeric ID, but the API still wants the encrypted-value
     envelope).
  7. On any failure (403, missing scope, network), fall back to returning
     the snippet response — never half-succeed.
- Atom + spec update: walkthrough mentions the `autoWireSecret=true`
  shortcut as opt-in.

## Risks

- ZCP becomes a holder of GitHub API credentials at runtime — increases
  the trust surface of the ZCP server itself.
- `GIT_TOKEN` would now need broader scope (`Secrets: Read and write`)
  persisted on Zerops indefinitely. The minimal-privilege story degrades
  if users opt in. Atom should call this trade-off out explicitly.
- Sealed-box encryption is non-trivial to get right; needs solid test
  coverage against real GitHub responses (not just unit tests with
  synthetic keys).

## Refs

- Original feedback context: live agent run 2026-04-29
- Manual-snippet fix lives in
  `internal/tools/workflow_build_integration.go::handleBuildIntegration`
  confirm branch
- GitHub Actions secrets API:
  <https://docs.github.com/en/rest/actions/secrets>
