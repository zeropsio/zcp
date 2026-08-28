# Spec draft — Zerops Code (z3) delivery: the zcp half

Written for `docs/spec-z3.md` to absorb; another slice owns that file and CLAUDE.md, so nothing
here edits either. Sections are numbered as a suggestion, not a claim on the file's structure.
Every statement below is pinned by a test named at the end of its section.

---

## §1 Where z3 sits

Zerops Code (z3) is a fork of T3 Code. Its server runs inside the `zcp@1` container and is
reached through the container's existing nginx on port 8080, under the path prefix `/z3/`. It has
no declared port of its own: it binds `127.0.0.1:3773`, so nginx is the only way in.

That placement is the whole delivery mechanism. The platform recipe for `zcp@1` runs `install.sh`
on every container start, which installs the latest zcp release; a plain service restart therefore
upgrades the container and turns z3 on for a project whose container predates it — with no change
to the platform API, the image, the recipe, or pool provisioning.

Everything zcp knows about z3 lives in `internal/z3`: the pinned version, the port, the base path,
the paths, the `t3 serve` argv, the environment contract. One definition each; moving the prefix or
bumping the version is a single edit.

---

## §2 The init step — local bundle first

`zcp init` gains a container-only step, **Zerops Code (z3)**, after *SSH config*. In order:

1. **Refuse without a project.** `runtime.Info.ProjectID` empty ⇒ the step fails (degraded). The
   z3 server treats a non-empty project id as the sole signal that it runs inside a Zerops
   project; starting without one would leave a plain upstream server on the container's origin.
2. **Bundle.** `~/.zcp/z3/node_modules/.bin/t3` present ⇒ used as-is: no version check, no
   network. Absent ⇒ one `npm install --prefix ~/.zcp/z3 --no-audit --no-fund --loglevel=error
   t3@<PinnedVersion>`, capped at 3 minutes.
3. **Capability note.** The bundle's `serve --help` is read once; when it does not advertise
   `--base-path`, the step says so on stderr. A bundle without it answers under `/z3/` but emits
   root-absolute asset URLs that the code-server cookie gate redirects — the page loads and
   nothing works, with no error to point at.
4. **Environment.** `~/.zcp/z3.env` is rewritten (mode 0600) — see §4.
5. **Unit.** When `/usr/lib/systemd/system/zerops@z3.service` is absent,
   `sudo -E zsc unit create z3 "zcp service start z3"`.

**One rule, two delivery paths.** A bundle placed by hand (the dev loop) and a bundle fetched from
the registry (the release path) are the same code path from step 2 onward. It also keeps a warm
restart off the network entirely: `~/.zcp/z3` survives a restart, and only a redeploy — which
replaces the container — loses it.

**The step is best-effort** (`step.degraded`), because `zcp init` is a `run.init` command whose
exit code gates the container start. A container with no bundle and no registry must still boot.
When the bundle cannot be had, **no unit is registered either**: a unit whose ExecStart cannot
resolve crash-loops at every boot and buries the real cause under a restart counter.

**The unit-file check is the idempotency.** `zsc unit` has only `create` and `remove` — no
upsert — a registered unit survives a container restart, and `zcp init` runs on every boot.

*Pinned by:* `TestRun_Z3_UsesExistingBundle_NoInstall`, `TestRun_Z3_InstallsPinnedBundle_WhenAbsent`,
`TestRun_Z3_UnitCreateArgs`, `TestRun_Z3_SkipsUnitCreate_WhenAlreadyRegistered`,
`TestRun_Z3_InstallFailure_Degrades`, `TestRun_Z3_NoProjectID_Degrades`, `TestRun_NoZ3_OutsideContainer`.

---

## §3 The supervised command

```
~/.zcp/z3/node_modules/.bin/t3 serve \
  --mode web --host 127.0.0.1 --port 3773 [--base-path /z3] \
  --base-dir ~/.t3 --no-browser \
  --auto-bootstrap-project-from-cwd /var/www
```

- **Never `npx`.** Resolving the package at every container start cost 58 s on an image-fresh
  container, and it is the bundle a hand-delivered dev build replaces.
- **The workspace is `/var/www`**, the same directory every zcp agent already works in, with each
  mounted dev service under `/var/www/<hostname>`. `--auto-bootstrap-project-from-cwd` is a
  BOOLEAN flag and the working directory is a POSITIONAL argument; writing the directory as that
  flag's value would silently bootstrap the unit's launch directory instead.
- **`--base-dir` is `~/.t3`**, under the home directory rather than a mount, so a restart keeps
  the thread history. A redeploy does not: it replaces the container and `~/.t3` comes back empty.
- **`--base-path` is a capability, not a preference.** The z3 CLI treats an unknown flag as a
  fatal parse error, so the flag is passed only when `serve --help` advertises it. Two reasons it
  cannot be a version gate: our fork before and after the flag both report `0.0.35-dev.<sha>`, and
  the registry's `t3@0.0.35` (upstream) has no such flag at all, so passing it blind turns the
  fresh-container path into a crash loop. Omitting it degrades in the safe direction and is logged
  at both sites (`zcp init`'s output and the unit's journal).
- **z3's process environment = the container's live env store + the T3CODE_* file**, so the
  agents and `zcp` it spawns see what a login shell sees; the store is read at unit start — a
  change to the service env needs `sudo systemctl restart zerops@z3` (or a future re-read).

*Pinned by:* `TestServeArgv`, `TestServeArgv_CwdIsPositional`, `TestSupportsBasePath`,
`TestStart_Z3_Argv`, `TestInstallArgs`, `TestLoadLiveEnv`, `TestMergeZ3Env_OrderAndPrecedence`.

---

## §4 The environment contract

`zcp init` runs as a `run.init` command and therefore sees the container's full environment; a unit
registered through `zsc unit create` is not guaranteed to inherit it. So the values the z3 server
needs are written to `~/.zcp/z3.env` on every boot and merged over the inherited environment by
`zcp service start z3`.

| Key | Source | Written when |
|---|---|---|
| `T3CODE_ZEROPS_PROJECT_ID` | `runtime.Info.ProjectID` (the injected lowercase `projectId`) | always — set and non-empty is THE "this is a Zerops environment" signal for the whole server; nothing else votes |
| `T3CODE_ZEROPS_API_HOST` | `ZCP_API_HOST`, else `schema.CanonicalAPIHost`; bare host (scheme and trailing slash stripped, port kept). The server joins `https://<host>/api/rest/public` | always |
| `T3CODE_ZEROPS_ALLOWED_ORIGINS` | the service env `ZCP_Z3_ALLOWED_ORIGINS` | only when that env is set — an unwritten key is what makes the server keep its own default |
| `T3CODE_ZEROPS_MEMBERSHIP_TTL_SECONDS` | — | never; the server's 900 s default stands unless someone configures the unit by hand |

**Only non-secret identifiers.** No token ever enters this file.

**Rewritten every boot, so the unit's command never has to change.** A unit's ExecStart is frozen
at creation and units survive a restart; anything that can differ between releases must be resolved
by the process at launch, never baked into the unit file. That is why `UnitCommand` is the bare
`zcp service start z3`.

A missing or unreadable env file is reported and z3 starts anyway: a server that declines the
Zerops identity path is diagnosable, a unit that refuses to launch is not.

*Pinned by:* `TestEnvLines`, `TestParseEnvFile`, `TestResolveAPIHost`, `TestRun_Z3_WritesEnvContract`,
`TestRun_Z3_WritesAllowedOrigins_WhenConfigured`, `TestStart_Z3_MergesEnvFile`,
`TestStart_Z3_MissingEnvFile_StillStarts`.

---

## §5 nginx — three locations, all outside the cookie gate

All three render identically whether or not `VSCODE_PASSWORD` is set.

**`location /z3/`** proxies to `http://127.0.0.1:3773/` — **with the trailing slash**, which strips
the prefix, so z3's own routes stay at the loopback root and only the URLs it *emits* carry the
prefix. Websocket upgrade headers and `proxy_read_timeout 86400s`. It sits outside the cookie gate
because the z3 server owns its own authentication — a Zerops identity, not the container's shared
password — and a browser client has to reach it before it holds any cookie.

**`location ~ ^/(abs)?proxy/3773(/|$)` returns 404.** code-server's own `/proxy/<port>/` and
`/absproxy/<port>/` reach any loopback port for whoever holds the container cookie: a second door
into z3, authenticated differently from the first. One door only. A regex location is evaluated
before the prefix `location /` that hands requests to code-server, which is what closes it.

**`location = /healthz`** serves the init marker (§6) verbatim as `application/json`, `no-store`,
falling back to a literal `{"initComplete":false,"initAt":null}` when there is none. No proxy and
no process, so it answers even when everything but nginx is down. It also shadows code-server's own
`/healthz`, deliberately.

Live nginx edits do not survive: every boot re-renders the template. Container-side changes belong
in `internal/content/templates/nginx.conf.tmpl`, never in `/etc/nginx/nginx.conf`.

*Pinned by:* `TestRunNginx_Z3OutsideCookieGate`, `TestRunNginx_Z3ProxyStripsThePrefix`,
`TestRunNginx_ClosesCodeServerProxyDoorToZ3`, `TestRunNginx_HealthzServesTheInitMarker`,
`TestRunNginx_HealthzFallbackIsValidJSON`, plus the `TestRunNginx_With{,out}Password` tables.

---

## §6 Readiness — two probes, no process

**`GET /healthz`** → always `200 application/json`:

```json
{"initComplete": true, "initAt": "2026-08-28T14:07:16Z"}
```

The body is the file `zcp init` writes at `/var/www/.zcp/state/init-complete` (mode 0644) when it
reaches the end of its container step list. It records that the **list finished**, not that every
step succeeded — a degraded step still leaves the marker, because "still initializing" and
"a step degraded" are different answers and `/healthz` only distinguishes the first from "broken".
`initAt` moving is how a client sees that a restart re-initialized the container.

**`GET /z3/.well-known/t3/environment`** → z3's liveness. Up means `200` **and**
`content-type: application/json` **and** a body carrying `"basePath":"/z3"`. Never the status code
alone: a stripped or mis-proxied prefix answers `200 text/html` from the SPA catch-all.

**`z3Up` is the client-side conjunction of the two.** `/healthz` deliberately does not carry it:
stock nginx cannot branch a *response body* on a subrequest, so a single endpoint answering all
three fields would need exactly the sidecar process this design removes. Proxying `/healthz` to z3
and falling back on `error_page` was considered and rejected — the "down" body would be ours and
the "up" body z3's, which is a worse contract than two honest probes.

Budget for the client (measured live): a restart is ~17 s to `/healthz` answering, ~19 s to z3 up,
with ~14 s of L7 `502` in between. Poll for `z3Up`, render the 502 as "restarting", cap at 30 s.

*Pinned by:* `TestRun_WritesInitCompleteMarker`, `TestRun_Z3_DegradedStepStillMarksInitComplete`,
`TestRunNginx_HealthzServesTheInitMarker`, `TestRunNginx_HealthzFallbackIsValidJSON`.

---

## §7 What survives what

| | restart | redeploy |
|---|---|---|
| `~/.zcp/z3` (the bundle) | kept | lost |
| `~/.t3` (threads, sessions, auth) | kept | **lost — recreated empty** |
| `~/.zcp/z3.env` | kept (rewritten anyway) | rewritten by the new container's init |
| `zerops@z3` unit | kept | lost, re-created by init |
| `/var/www/.zcp/state/init-complete` | rewritten each boot | rewritten each boot |
| live nginx edits | erased (template re-rendered) | erased |
| container id | unchanged | changes |

A restart is also an upgrade: `install.sh` re-runs and replaces `/usr/local/bin/zcp` with the
latest release. Thread history is one redeploy away from gone — a client must say so before any
redeploy-shaped action.

---

## §8 Candidate CLAUDE.md trap (one line, if the owner wants it)

> **A z3 flag is a CAPABILITY, not a preference** — `--base-path` is passed only when the installed
> bundle's `serve --help` advertises it; the CLI treats an unknown flag as fatal, and the registry's
> pinned `t3` is upstream, so passing it blind crash-loops the unit at every boot.
> `TestStart_Z3_Argv`.

Everything else in this draft is spec-shaped or test-shaped and needs no CLAUDE.md line.

---

## §9 Open items this slice does not decide

1. **Where the pinned bundle comes from on the release path.** `PackageSpec` resolves upstream
   `t3@0.0.35` today, which has no `--base-path`; a fresh container would serve a base-path-less z3
   until the fork is the published package. S0.6 / the release gate owns this.
2. **The `ZCP_Z3_ALLOWED_ORIGINS` source name** was chosen here, not by S1.
3. **Two `serve --help` probes per boot** (one in `zcp init` for the log line, one in the
   supervisor for the argv), ~1 s each. Collapsible to one if the boot budget matters more than the
   init log.
4. **No `zcp init z3` subcommand** — the step runs inside `zcp init`, which is what the recipe
   calls. If the dev-push script wants a narrower verb it is ~10 lines in `cmd/zcp/main.go`.
