# Empty account → Gitea → a Mate that ships to production — runbook

Written 2026-09-06, after proving every step on the Onboarding org. This is the execution script
for doing it again from nothing. The reasoning lives in
`z3-new-mate-and-gitea-cicd-research-2026-09-06.md`; this file is only *what to run, in what order,
and what to expect*.

**The owner does exactly one thing: authorize the coding agent in the new Mate** (step 4). Everything
else is API calls. See §Open decisions for the one place that claim is currently thin.

---

## 0. Preconditions

| Thing | State needed |
| --- | --- |
| `zcli` login | present at `~/Library/Application Support/zerops/cli.data` (`Token`, `RegionData.address`) |
| Region | the account's projects land in `prg1`; every derived URL below assumes it |
| `zeropsio/recipe-gitea` | `main` already mints its own admin (merged 2026-09-06, PR #2) |
| mate / zcp | mate 0.5.0, pinned by zcp v9.167.0 |

**"Empty account" is ambiguous and must be settled before starting** — see §Open decisions.

### The helper every step uses

Nothing here needs a package. One file, `z.py`, next to wherever you work:

```python
import json, os, urllib.request, urllib.error
_c = json.load(open(os.path.expanduser("~/Library/Application Support/zerops/cli.data")))
TOKEN = _c["Token"]
_h = _c["RegionData"]["address"]
HOST = _h if _h.startswith("http") else "https://" + _h

def api(method, path, body=None, timeout=120):
    req = urllib.request.Request(f"{HOST}/api/rest/public{path}", method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json",
                 "Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            t = r.read().decode(); return r.status, (json.loads(t) if t.strip() else {})
    except urllib.error.HTTPError as e:
        t = e.read().decode()
        try: return e.code, json.loads(t)
        except Exception: return e.code, {"raw": t[:400]}

def client_id():
    return (api("GET", "/user/info")[1].get("clientUserList") or [{}])[0].get("clientId")

def services(pid):                       # NOTE: the list is under "list", not "items"
    return api("GET", f"/project/{pid}/service-stack?limit=100")[1].get("list") or []

def env(sid):                            # sensitive values need an INTEGRATION token; see §Traps
    return api("GET", f"/service-stack/{sid}/user-data?limit=200")[1].get("list") or []

def origin(pid, name, port):             # public URL of a service port
    p = api("GET", f"/project/{pid}")[1]
    host, zone = p.get("zeropsSubdomainHost"), p.get("publicZone") or ""
    region = zone.split(".")[-2].split("-")[0] if "." in zone else ""
    return f"https://{name}-{host}-{port}.{region}.zerops.app" if host and region else None
```

Creating a project is always this body:

```python
api("POST", f"/client/{client_id()}/project", {"name": NAME, "description": "", "tagList": TAGS,
    "location": None, "clientId": client_id(), "mode": "LIGHT", "maxCreditLimit": None, "userRoles": []})
```

---

## 1. Gitea, with credentials, in ~3 minutes

Create a project tagged `mate:tool:gitea`, then import. **No admin step follows — that is the point.**

```yaml
#zeropsPreprocessor=on
services:
  - hostname: db
    type: postgresql:ha@18
    profile: oltp-staging
    priority: 10
  - hostname: volume
    type: local-storage@1
    priority: 10
  - hostname: web
    type: ubuntu@26.04
    vault:
      DB_PASSWORD:
        value: <@generateRandomString(<32>)>
        sensitive: true
      GITEA_DOMAIN: web-${zeropsSubdomainHost}-3000.prg1.zerops.app
    maxContainers: 1
    verticalAutoscaling:
      minRam: 0.25
    buildFromGit: https://github.com/zeropsio/recipe-gitea
    enableSubdomainAccess: true
```

`api("POST", f"/project/{pid}/service-stack/import", {"yaml": YAML})`

Then poll `env(web_service_id)` until `GITEA_ADMIN_TOKEN` appears. Expect **~175-185 s** from import.
Along the way the container deliberately restarts four or five times over ~15 s while `start.sh`
waits for the secrets `init.sh` just wrote — that is normal, not a fault.

Read `GITEA_ADMIN_USERNAME` (`mate`), `GITEA_ADMIN_PASSWORD`, `GITEA_ADMIN_TOKEN` straight out of
`env()`. The URL is `origin(pid, "web", 3000)`.

**Gitea client.** Token auth for most routes; **basic auth is mandatory** for anything under
`/users/{u}/tokens` — a token cannot mint a token.

```python
import base64, json, urllib.request, urllib.error
def gitea(method, path, base, body=None, token=None, basic=None):
    h = {"Accept": "application/json"}
    if basic: h["Authorization"] = "Basic " + base64.b64encode(basic.encode()).decode()
    elif token: h["Authorization"] = "token " + token
    if body is not None: h["Content-Type"] = "application/json"
    req = urllib.request.Request(f"{base}/api/v1{path}", method=method, headers=h,
                                 data=None if body is None else json.dumps(body).encode())
    try:
        with urllib.request.urlopen(req, timeout=180) as r:
            t = r.read().decode(); return r.status, (json.loads(t) if t.strip() else {})
    except urllib.error.HTTPError as e:
        t = e.read().decode()
        try: return e.code, json.loads(t)
        except Exception: return e.code, {"raw": t[:300]}
```

---

## 2. Runners

```
POST /admin/actions/runners/registration-token      (token auth; note the /actions/ segment)
```

Import the addon into the **Gitea project**, substituting the token. No `zeropsSetup:` — the
hostname `runner` already selects the setup of that name:

```yaml
services:
  - hostname: runner
    type: ubuntu@26.04
    vault:
      RUNNER_REGISTRATION_TOKEN:
        value: <the token>
        sensitive: true
    minContainers: 1
    maxContainers: 1
    verticalAutoscaling:
      minRam: 0.5
    buildFromGit: https://github.com/zeropsio/recipe-gitea
```

**~100 s** to one online runner (`GET /admin/actions/runners`). `minContainers: 1` keeps the demo
cheap; the published recipe ships 3.

---

## 3. The group's dev environment (the Mate)

Project tagged `mate`, `mate:bot:<Name>`, `mate:g:<groupid>`, `mate:role:dev`, `mate:name:<Group>`,
then `PUT /project/{id}/first-class-recipe/development-container` with the zcp service YAML
(`buildZcpServiceImportYaml` in the fork emits it; `createIntegrationToken: true`).

Import the application beside it in whatever shape the demo wants.

---

## 4. **The owner's step: authorize the agent**

Open the Mate and sign the coding agent in. This is the only manual action in the runbook.

---

## 5. Give the Mate its Gitea credential

One token per Mate, named `mate/<bot>`, scope `write:repository`. Minting **requires basic auth**:

```
POST /users/mate/tokens   {"name": "mate/Fen", "scopes": ["write:repository"]}   (basic auth)
DELETE /users/mate/tokens/mate%2FFen                                             (delete by NAME)
```

Write both onto the Mate's **zcp** service, `sensitive: true`, then restart it — a service env write
reaches new processes only, and the agent inherits the mate server's boot environment:

```python
api("POST", f"/service-stack/{zcp_id}/user-data", {"key": "GITEA_URL",   "content": url,   "sensitive": True})
api("POST", f"/service-stack/{zcp_id}/user-data", {"key": "GITEA_TOKEN", "content": token, "sensitive": True})
api("PUT",  f"/service-stack/{zcp_id}/restart")
```

Upsert = delete then create; there is no update. `DELETE /user-data/{id}`.

---

## 6. The repository

```
POST /repos/migrate  {"clone_addr": "<source>", "repo_name": "<name>", "repo_owner": "mate",
                      "service": "git", "private": false}
```

or `POST /user/repos` for an empty one the Mate fills. Then **tell the Mate what it has** — it has no
idea otherwise:

> You have a Gitea instance at `$GITEA_URL` and an access token in `$GITEA_TOKEN`, both already in
> your environment. Your repository is `mate/<name>`. Look at `/var/www` before you push anything and
> tell me what is there. Then wire up git push for the app service against that repository and push.

The "look first" clause matters: a Mate that pushes blind will force-update the remote and take any
workflow files with it.

---

## 7. Production, pipeline-first

```yaml
services:
  - hostname: db
    type: postgresql:single@18
    priority: 10
  - hostname: app
    type: nodejs@22          # match the app; run.base in zerops.yaml can re-base it later
    startWithoutCode: true
    minContainers: 1
```

- **No `zeropsSetup`** without `buildFromGit` — rejected as `projectImportInvalidParameter`.
- **`enableSubdomainAccess` does not take** on a `startWithoutCode` service. Call
  `PUT /service-stack/{id}/enable-subdomain-access` **after** the first deploy; before it, 400.
- Never delete a service to change its runtime — `run.base` in `zerops.yaml` does that, and deleting
  breaks every repository secret holding the old service id.

Deploy token, scoped to that project alone:

```python
api("POST", f"/client/{client_id()}/integration-token", {"name": "gitea-deploy-<x>-prod",
    "roleCode": "NO_ACCESS", "canCreateProjects": False, "canViewFinances": False,
    "canEditFinances": False, "projects": [{"projectId": PROD, "roleCode": "ADMIN"}]})
```

The value is returned once, in `token`.

---

## 8. Wire CI and ship

```
PUT /repos/mate/<repo>/actions/secrets/ZEROPS_TOKEN            {"data": "<deploy token>"}
PUT /repos/mate/<repo>/actions/secrets/ZEROPS_PROD_SERVICE_ID  {"data": "<prod app service id>"}
```

`.gitea/workflows/deploy-prod.yaml`, via the contents API or a Mate push:

```yaml
name: deploy to production
on:
  push:
    tags: ["v*"]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Push to production
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
        run: |
          zcli push \
            --service-id "${{ secrets.ZEROPS_PROD_SERVICE_ID }}" \
            --setup <the setup name in the app's zerops.yaml> \
            --version-name "${GITHUB_REF_NAME}"
```

No install step: the runner recipe puts `zcli` in `/usr/local/bin` in its `prepareCommands`.
Host-mode jobs run as the `zerops` user and `image:` has no effect, so anything installed per-user
would be at `$HOME/.local/bin`, never `/root`. `--setup` must name a setup the app's own
`zerops.yaml` defines.

Then tag and push (`git push origin v1.0.0`) and watch
`GET /repos/mate/<repo>/actions/tasks`. **~68 s** from tag to production `ACTIVE`. Enable subdomain
access, then read the page.

---

## Timings measured

| Step | Time |
| --- | --- |
| Gitea import → credentials published | ~175 s |
| Runner import → one online runner | ~100 s |
| Tag → production `ACTIVE` | ~68 s |
| Mate container (zcp) import → usable | see the fork's ledger, ~2 min |

---

## Traps, all paid for once already

- **`user-data` answers `REDACTED` to a user access token.** The token `POST /auth/login` returns
  reads every `sensitive: true` value as the literal string `REDACTED`. An **integration token**
  reads them in clear, and one scoped to a single project
  (`roleCode: NO_ACCESS` + `projects: [{projectId, roleCode: "ADMIN"}]`) is enough. Measured
  2026-09-06 on a fresh account.
- `services()` reads `list`, not `items`.
- `GITEA_DOMAIN` comes back from `env()` **unresolved** (`web-${zeropsSubdomainHost}-...`). Build
  URLs from the project's `zeropsSubdomainHost` + `publicZone` instead.
- `git ls-remote https://oauth2:<token>@host/...` works — Gitea ignores the username whenever the
  password is a token, so zcp's credential helper needs no Gitea flavour.
- A `write:repository` token is refused by `/user` and `/admin/...` with 403. That is correct, not a
  broken token; git still works.
- `zsc` env writes reach **new processes only**, ~15 s.
- A failing start command is retried **alone** — init commands do not re-run with it.

---

## Open decisions before the next run

1. **What "empty account" means.** A different Zerops org, or this one wiped? Wiping deletes
   `zerops-mate10`, three Beviro projects, three Acme Docs projects, `scratch-playground`,
   `mate-gitea` and `hello-go - production`. That is not reversible and must be said out loud first.
2. **Whether the owner really does only one thing.** Agent authorization is one action; *giving the
   Mate its task* is a second, unless A-1 (the composed first prompt) ships first so the instruction
   of §6 is seeded at creation. Decide which before claiming a one-touch demo.
3. **Whether to run it by API again or build the client first.** §1–§8 are all API calls today. B-8
   and B-9 would move §5 and §6 into the product, which is the difference between demonstrating that
   it works and demonstrating the product.
