# z3 in zcp — make it opt-in, make the install a lifecycle, unwind the .gitignore change

Proposal, 2026-08-31. Branch: `feat/z3-continuation` (local only, never pushed).

**Status: phases A–D are implemented and committed on this branch.** See §11 at the end for what
landed and what is still open. Phase E (the fork) is not started.

The owner's note asks for three things: (1) don't break what works today, (2) solve installation
properly, (3) line zcp's responsibilities up with what z3 actually owns. This document says what
changes, in what order, and what stays.

---

## 1. The one architectural idea

**zcp stops asserting z3 and starts reconciling it.** Today every z3 artefact — the init step, the
nginx locations, the readiness route, the port-3773 block, the `.gitignore` backfill — is
unconditional, and installation is a one-way "if the binary is missing, fetch the pin". After this
change there is a single desired-state input (`ZCP_Z3_ENABLED`) and one reconcile pass per
`zcp init` that converges four things — bundle version, environment contract, systemd unit, nginx
routes — in both directions, on and off. Nothing z3-shaped renders, downloads, binds or blocks
unless the flag says so.

The acceptance test is the owner's own: **with the flag unset, a container running the new zcp
must render an nginx config byte-identical to today's `main`, run the same processes, and touch no
git state it does not touch today.**

---

## 2. Phases

| # | Headline | Dnes → Po opravě |
|---|---|---|
| **A** | The `.gitignore` backfill leaves | Every host-side git self-heal writes a language `.gitignore` into a user's service → zcp writes no `.gitignore`, ever |
| **B** | z3 becomes opt-in and reversible | Init step, nginx `/z3/`, `/healthz` shadow and the 3773 block are unconditional → all four keyed off `ZCP_Z3_ENABLED`, with a real disable path |
| **C** | Install becomes an update lifecycle | "binary present ⇒ done, no version check" → versioned prefix, staged install, atomic activate, previous version survives a failed update |
| **D** | The zcp↔z3 contract gets written down | Implicit, discovered by crash → a numbered contract + `ContractVersion`, which is what "latest compatible" would later need |

---

## 3. Phase A — the `.gitignore` backfill leaves

### Dnes

Every host-side git self-heal site runs this on a user's dev service, including **before every SSH
deploy**:

```sh
test -e .gitignore || printf '%s\n' '.env' '.zcp/' '.DS_Store' '*.log' \
  'node_modules/' 'dist/' '.next/' '.nuxt/' '.output/' > .gitignore
```

and on a genuinely fresh repo that blob goes **into the marker commit** (`git mktree` with one
entry instead of the empty tree, plus a matching `update-index --cacheinfo`).

Reach: `ops.GitEnsureRepoHeadCommand` (deploy safety-net, bootstrap `InitServiceGit`,
git-push-setup pre-probe), `BuildGitOriginSyncCommand`, `BuildGitReconstructCommand`,
`git_auth_probe`.

### Proč to bolí

It changes deploy *content* and the shape of an existing project's first commit, on a path every
deploy crosses — for a problem that belongs to a different repo. It landed as a companion to z3's
245 s first-checkpoint measurement, and it is the one change on this branch that can alter a
user's repository without the user asking.

### Po opravě

`git revert 255443a9` — the commit is isolated (nothing after it touched those six files), so the
revert is mechanical. `GitEnsureRepoHeadCommand(workingDir)` loses its `serviceType` parameter
again, the marker commit goes back to the empty tree, `internal/topology/gitignore.go` and its test
are deleted. Docs: `docs/spec-z3.md §6.6` and `docs/spec-workflows.md` GLC-7 go with it.

### Trade-off

The 245 s worst case comes back for a *newly initialised* node repo, and the remaining mitigation
is weaker than the spec implies. z3's untracked-file guard (`ls-files --others --exclude-standard`,
256 KB cap) does refuse that repository's checkpoint — but it **fails open** (a probe that errors
is read as "not overflowing"), and its de-memoisation fix is unit-tested only: the live audit that
caught `node_modules` going into a checkpoint is recorded as *"live re-run pending the next push"*.
So Phase A must land paired with the z3-side hardening in §8 — probe fails closed, re-verified live
— before z3 is enabled anywhere that matters. Containment is real in the meantime: z3 is opt-in
after Phase B, and the failure is a slow turn plus a fat checkpoint ref, never lost user data.

The alternative considered — keep the mapping but apply it only at bootstrap `InitServiceGit`, not
on the deploy path — was rejected because it still writes into a user's repo unasked, and a
half-reach is harder to reason about than none.

---

## 4. Phase B — z3 becomes opt-in and reversible

### Dnes

```
zcp init            → always: download/verify/install the bundle, write ~/.zcp/z3.env,
                       zsc unit create z3
zcp init nginx      → always renders:
                       location /z3/                      → 127.0.0.1:3773
                       location ~ ^/(abs)?proxy/3773(/|$) → 404
                       location = /healthz                → the init marker (shadows code-server's)
```

There is no off. Removing z3 means editing the binary.

### Proč to bolí

Three unconditional regressions on every container that takes the next zcp release: port 3773 stops
being reachable through code-server's generic proxy for any user app; `/healthz` stops being
code-server's; and every boot spends a download + `npm install` (~58 s cold, measured) on a feature
nobody asked for. Plus there is no disable lifecycle at all — the `zsc` unit survives restarts and
`zsc unit` has no upsert.

### Po opravě

**One input.** `runtime.Info.Z3Enabled` ← `ZCP_Z3_ENABLED` ∈ {`1`,`true`}, read in
`runtime.Detect()` next to the existing `ZCP_AUTHORING` gate.

**One reconcile pass**, `zcp init` (in-container only):

| desired | actual | action |
|---|---|---|
| on | no bundle | install (Phase C) → env file → `zsc unit create z3` |
| on | bundle, current version | rewrite env file → ensure unit |
| on | bundle, older version | staged install → activate → ensure unit → restart the unit if running |
| off | unit present | `sudo systemctl stop zerops@z3` → `sudo -E zsc unit remove z3` → drop `~/.zcp/z3.env` (bundle files stay on disk) |
| off | nothing present | nothing — the step is not even registered, so init prints no extra line |

**nginx.** The three z3 blocks go inside `{{- if .Z3Enabled}}`; `RunNginx` reads the same flag.
With it off the rendered file is byte-identical to today's `main`, pinned by a golden test.

**Readiness moves out of the root namespace.** `location = /healthz` becomes
`location = /z3/healthz` (an exact match beats the `/z3/` prefix, so it is served by nginx from the
init marker and never reaches z3). Consequences: code-server keeps its own `/healthz` in every
configuration, and the container-readiness probe lives where its only consumer is. z3's own
liveness stays the second probe, `/z3/.well-known/t3/environment`, which the client already treats
as the readiness authority.

This is a **gain** for the client, not just a move. Today `/healthz` answers on every zcp container
whether or not z3 is there, so the client reads it only to tell "still starting" from "container
predates Zerops Code" (`packages/client-runtime/src/zerops/containerHealth.ts`). Under the flag,
`/z3/healthz` answering at all means *z3 is enabled here*, and a 404 means *it is not* — the exact
distinction an opt-in feature needs and the current route cannot make. The client change is one
path constant plus the new third state.

**Defence in depth.** `zcp service start z3` refuses with a named message when the flag is off, so
a unit that outlived a failed removal cannot resurrect the server.

### Trade-off

Gating nginx on the *flag* rather than on "is z3 actually installed and healthy" means a degraded
install leaves `/z3/` answering 502. Chosen deliberately: desired state drives the route, a 502 is
a truthful and diagnosable signal, and the alternative couples the nginx render to a filesystem
probe that is stale by the time nginx reloads. `/z3/healthz` still answers from a static file with
no process behind it, so the container can be diagnosed while z3 is down.

---

## 5. Phase C — install becomes an update lifecycle

### Dnes

```go
bin := z3.BinPath()                       // ~/.zcp/z3/node_modules/.bin/z3
if _, err := os.Stat(bin); err != nil {   // present ⇒ used as-is, no version check, no network
    z3Install()                           // npm install --prefix ~/.zcp/z3 <verified tarball>
}
```

### Proč to bolí

Three defects in one rule. A container that installed 0.1.0 will run 0.1.0 forever, because the
binary exists — a zcp release carrying a newer pin changes nothing on any warm container. The
install writes straight into the live prefix, so a failed `npm install` (registry hiccup, half the
198 dependencies) leaves a broken bundle where a working one stood. And "binary exists" is also how
a hand-pushed dev build is protected, so the protection and the version check are the same
un-splittable rule.

### Po opravě

**Layout.** Each version is its own npm prefix; a symlink names the live one.

```
~/.zcp/z3/
  versions/0.1.0/node_modules/.bin/z3
  versions/0.2.0/node_modules/.bin/z3
  current -> versions/0.2.0            # BinPath() = ~/.zcp/z3/current/node_modules/.bin/z3
```

**Installed version** is read from `current/node_modules/zerops-code/package.json` — npm's own
record, not a side file zcp has to keep honest.

**Desired version** comes from one seam, `z3.DesiredRelease() → {version, url, sha256}`. Its only
implementation today returns the compiled-in pin, unchanged in spirit from what ships now.

**The pass**: equal versions ⇒ return, no network at all · otherwise download to a temp file,
verify SHA-256, `npm install --prefix versions/<new>`, smoke it (`z3 --version` exits 0), then
`symlink` + `rename` the `current` pointer atomically, then prune to the two newest versions. Every
failure returns before the swap, so `current` still points at the version that was working.

**Dev builds are protected explicitly**, not incidentally: an installed version carrying a
prerelease tag (`0.1.0-dev.<sha>`, what `z3-dev-push.sh` produces) is never replaced automatically.
`zcp z3 update --force` overrides.

**Legacy migration**: a container with today's flat `~/.zcp/z3/node_modules` and no `current` gets
that tree moved to `versions/<its version>` and linked — no download.

**Manual update, no container restart**: `zcp z3 update [--force]` runs the same pass and then
`sudo systemctl restart zerops@z3` when the unit exists.

### Trade-off

Two installed versions cost disk (~2× the bundle plus its 198 dependencies). Accepted for the
"previous version survives a failed update" requirement, bounded by pruning to two. The simpler
alternative — install to `<prefix>.staging` then `mv` over the live prefix — was rejected: it
renames directories out from under a running node process, and z3 is running at exactly that
moment.

**Not done now: "latest compatible".** Automatic tracking needs a release to *declare* the contract
it satisfies, which no release does yet. `DesiredRelease()` is the seam it would land behind, so
v1 stays hard-pinned and honest about it — the digest compiled into zcp remains the integrity
authority, which a `latest` resolver would have to give up.

---

## 6. Phase D — write the contract down

`z3.ContractVersion = 1`, and a spec section stating what a z3 release may not break without
bumping it:

1. the executable is `z3` at `node_modules/.bin/z3` in a `zerops-code-<version>.tgz` release asset
   on `zeropsio/z3`;
2. `serve` accepts `--mode web --host --port --base-dir --no-browser
   --auto-bootstrap-project-from-cwd <cwd positional>`; **an unknown flag is fatal**, so any flag
   added later reaches production only behind a capability probe (`--base-path` is the precedent
   and stays one);
3. `T3CODE_ZEROPS_{PROJECT_ID,API_HOST,ALLOWED_ORIGINS}` keep their meaning; `PROJECT_ID` non-empty
   remains the sole Zerops-environment signal;
4. liveness is `GET {basePath}/.well-known/t3/environment` → `200 application/json` carrying
   `basePath`;
5. the server binds loopback only and never claims a declared platform port;
6. **`/z3` is baked into the released artifact, not chosen by zcp.** The release workflow builds the
   bundled client with `VITE_BASE_PATH=/z3`, and `pack` refuses a tarball without
   `dist/client/index.html`. zcp's `BasePath` constant must equal it; moving the prefix is a
   two-repo change, never a zcp edit.

zcp installs a release only when its declared contract ≤ `ContractVersion`. Until releases carry
that field, the hard pin *is* the enforcement.

A later "latest compatible" resolver is well-formed on the release side already: the workflow
triggers only on stable `v<major>.<minor>.<patch>` tags, so the GitHub releases list *is* the
candidate set — nightlies never publish one.

---

## 7. What is NOT in scope, and why

| Item | Verdict | Reason |
|---|---|---|
| The web client (build, config, static serving, updates) | zcp already does none of it | The centralized client talks to `/z3/`; the release tarball still carries one and zcp simply does not use it. A server-only artefact is not merely absent — `cli.ts pack` **asserts** `dist/client/index.html` and fails without it, so it is a fork-side change if ever wanted. `--base-path` stays regardless: it is the *server* that emits prefixed URLs (`/ws`, well-known, assets) |
| Envelope on the wire (§1 of the spec) | keep | Independent of the z3 server; small (140 B fenced idle, ~1.2 KB on a JSON result), total (a failure attaches nothing). See open question 3 |
| `runtime.HomeDir()` extraction, `services()` map→function, `SetRunFunc` signature | keep | Pure refactors, behaviour-identical for nginx/vscode |
| `mark-oauth` non-sensitive migration | keep | Unrelated to z3, already spec'd |
| z3-side git hardening (SSHFS fallback, untracked restore) | z3 repo, separate stream | Named in §8 |

---

## 8. The z3-side items (verified in `../z3`, not zcp work)

The owner's note named two holes. Reading the code confirms both and turns up a third. All four
items below are policy edits in `ZeropsPolicy` / `ZeropsGitSpawner` / `ZeropsCheckpointTargets` /
`GitVcsDriver`, independent of everything above, and they are what makes the Phase A revert safe.

| # | Finding | Today | Conservative first version |
|---|---|---|---|
| 1 | **Git does fall back to the sshfs mount.** Two branches pass a git spawn through unrewritten: the topology reading `unavailable` (no creds, `zcp` missing, timeout — the module comment says outright *"it runs against the mount, slowly"*), and a path that resolves to no mounted service | one `logWarning` per outage, the spawn itself silent | on Zerops, refuse the git operation rather than run it over the mount — a skipped checkpoint is honest, a 12.7 s/turn silent degradation is not |
| 2 | **Restore does overwrite untracked files.** Capture stages everything with `add -A` into a temp index, so a file untracked at capture time *is* a blob in the checkpoint; restore's `git restore --source … --worktree --staged` writes it straight back. `git clean -fd` is correctly off on Zerops, which only protects paths the checkpoint never recorded | no test covers the overwrite case | restore tracked-at-capture files only on Zerops; leave anything the working tree did not track alone |
| 3 | **Restore also discards staged state.** `restore --staged` writes the **real** index and `reset --quiet -- .` then resets it to HEAD — a user's staged-but-uncommitted work is gone with no warning | undocumented | either preserve the index or refuse a restore over a dirty index, and say which |
| 4 | **The untracked guard fails open and is unverified live.** The probe's `catchCause` maps any failure to "not overflowing", and the de-memoisation fix that closed the live `node_modules` defect is recorded as *"live re-run pending the next push"* | unit-tested only | fail closed on a probe error, then re-run the live audit — this is Phase A's safety net |

Everything else the note asks of z3's git contract already holds, verified: no branch ref is ever
moved (checkpoint commits are parentless under `refs/t3/checkpoints/**`), no worktree, no fetch, no
push, no PR, and **no code in `apps/server/src` writes a `.gitignore`** — it is read-only input to
`add -A` and `--exclude-standard`.

---

## 9. Order, gates, effort

1. **A** (revert) — independent, lands first, cheap to verify: `go test ./... -short`, and the
   deploy/bootstrap E2E that pins the SSH command bodies.
2. **B** (gate) — gate before lifecycle, so every later change is already invisible when the flag is
   off. Gate: the byte-identical-nginx golden, plus a live pass on `z3-eval` with the flag **unset**
   (code-server, `/proxy/<port>/`, deploy, mounts all unchanged) and then set.
3. **C** (install lifecycle) — gate: a live upgrade on `z3-eval` from 0.1.0 to a 0.1.1 tag, a
   deliberately broken update proving the old version keeps serving, and a dev-push proving it is
   not clobbered.
4. **D** (spec) — reconciled at each landing, per `/flow` LAND.

Effort: A ≈ 0.5 day (−400 LOC), B ≈ 1 day, C ≈ 1–1.5 days, D ≈ 0.5 day. ≈ 3 days total.

Backward compatibility, per surface:
- **nginx / code-server / user ports** — unchanged with the flag off (golden-pinned); with it on,
  only `/proxy/3773` closes.
- **`/healthz`** — becomes code-server's again in every configuration; the container-readiness body
  moves to `/z3/healthz`. **The hosted z3 web client must follow this path change.**
- **`zcp init` output** — one extra step line only when z3 is enabled or previously installed.
- **MCP tool results** — the envelope is the one unconditional change on this branch (open
  question 3).

---

## 10. Open questions for the owner

1. **`/healthz` → `/z3/healthz`**: recommended, and it needs one path constant plus a third state in
   `containerHealth.ts` on the fork side. Cheap now (only `z3-eval` runs it), permanent later.
2. **`.gitignore`**: full revert (recommended), or keep it narrowed to bootstrap-only
   `InitServiceGit`? Either way §8 item 4 (guard fails closed + live re-verify) should be booked in
   the same breath — it is the mitigation the revert leans on.
3. **Envelope**: leave it on for everyone (recommended — it is zcp's own lifecycle state and the
   `action="status"` recovery primitive already carried it in prose), or gate it on `Z3Enabled` too
   for strict "nothing changes without the flag" parity? A one-line gate either way.
4. **Flag spelling**: `ZCP_Z3_ENABLED=1`, accepting `1`/`true`. Confirm the name before it becomes
   a service-env contract.


---

## 11. What landed (2026-08-31)

Five commits, each independently green (`go build ./...` + `go test ./... -short` verified at every
one, plus `-race` and the full `golangci-lint` on the tip):

| Commit | Phase | What |
|---|---|---|
| `6b17c1ca` | A | Revert of the `.gitignore` backfill, its spec §6.6 and `GLC-7` |
| `dedd2fd5` | B | `runtime.Info.Z3Enabled` ← `ZCP_Z3_ENABLED` (`1`/`true`, case-insensitive) |
| `c8fcdaa5` | B | `reconcileZ3` — two-directional step, conditional registration, marker gated, `zcp service start z3` guard |
| `37e65b06` | B | The three nginx locations behind `{{if .Z3Enabled}}`; readiness at `/z3/healthz` |
| `b74d4c62` | C | Versioned prefixes, `EnsureInstalled`, atomic activation, `zcp z3 update`, dev-push loop |
| `440f8785` | D | Spec §2.0/§2.1/§2.1a–c/§2.4/§2.5/§2.6/§2.8 + Z3D-11…14, CLAUDE.md map + trap |

### Proof, beyond the unit tests

- **The acceptance principle, literally.** Rendering `main`'s nginx template and this branch's with
  the flag off produces **byte-for-byte identical** output, with and without `VSCODE_PASSWORD`. Not
  asserted by proxy — the two files were diffed.
- **Real nginx.** All four renders (flag × auth) pass `nginx -t` under nginx 1.29, so the new
  conditional region is valid config and not merely a string that looks right.
- **Absence, not just presence.** `TestRunNginx_Z3Disabled_RendersNoZ3Surface` asserts the disabled
  render contains no `/z3`, no `3773`, no `healthz`, no marker path — *and* still contains every
  non-z3 structure.
- **Atomicity.** Separate tests drive an npm failure and a smoke failure and assert `current` still
  names the version that was working.

### Changed from the proposal during implementation

- **Migration converges in one pass.** The first implementation migrated a legacy flat install and
  returned, so a container that was also behind on versions needed a second restart. It now
  continues into the version compare — `zcp init` reconciles in one go, which is the point.
- **No `z3.ContractVersion` constant.** The proposal called for one; nothing would read it, and an
  unread exported constant is exactly the orphan CLAUDE.md forbids. §2.8 instead states plainly that
  the hard pin *is* today's enforcement, and names what has to exist before a resolver can replace it.
- **`legacyPlaceholderVersion` is dash-free** (`legacy.pre.versioning`): `IsDevVersion` reads a `-`
  as a semver prerelease, and a placeholder that looked like one would have described an unreadable
  install as a dev build worth protecting.

### Still open

- **Phase E, the fork** (§8) — not started. Items 1–3 are policy edits; item 4 (untracked guard
  fails closed + live re-verify) is the safety net Phase A's revert leans on and should go first.
- **The client's readiness path** — `containerHealth.ts` still probes `/healthz`. It needs the new
  path plus the third state (404 ⇒ z3 not enabled here).
- **Live verification on `z3-eval`** — everything above is offline proof. The live pass needs VPN
  (`zcli vpn up nTV3oMB2SS634ImDJnQckg`, sudo) and should cover: flag unset ⇒ code-server,
  `/proxy/<port>/`, deploy and mounts unchanged; flag set ⇒ `/z3/` up; then an upgrade, a
  deliberately broken update proving the old version keeps serving, and a dev-push proving it is
  not clobbered.
