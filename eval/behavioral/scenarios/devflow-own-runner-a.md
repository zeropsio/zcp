---
id: devflow-own-runner-a
description: |
  An existing repository developed by its own runner (mono-z-dev-in-place):
  README says npm install then npm run dev; the runner binds loopback unless
  NOTES_DEV_HOST is set and refuses unknown hosts unless
  NOTES_DEV_ALLOWED_HOSTS lists them; the dev install is heavy; only a prod
  setup exists. The agent runs the repo's own dev command as a named dev
  server under the supervisor, reaches it on the subdomain through the
  repo's knobs, adds no proxy, and installs no dependencies in the dev
  setup's build. English phrasing: "get it running the way it is meant to be developed".
seed: imported
fixture: fixtures/devflow-own-runner-imported.yaml
preseedScript: preseed/plant-devflow-own-runner.sh
tags: [develop, existing-repo, dev-server, supervisor, nodejs]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You own this repository and develop it with its README dev flow. You want to open the running app in your browser. Accept what the agent proposes; answer a question in one short sentence. If the agent asks a second time about the same thing, say "as you proposed".
verification:
  mode: required
  spec: spec-mate.md §2.8a
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
  toolArg:
    - {always: "zerops_dev_server{action=start}"}
  liveness: {service: appdev, marker: "notes-dev-ready"}
  containerCheck:
    - service: appdev
      cmd: "grep -qE '\"command\": *\"[^\"]*(npm run dev|dev-runner\\.mjs)' /var/www/.zcp/dev-servers.json && zcp-devsup ctl status --json | grep -qE '\"status\": *\"running\"' && echo repo-dev-command-running"
      match: "^repo-dev-command-running"
    - service: appdev
      cmd: |-
        cd /var/www && node -e '
        const fs = require("fs");
        const f = ["zerops.yaml", "zerops.yml"].find((x) => fs.existsSync(x));
        const dev = fs.readFileSync(f, "utf8").split(/\n\s*-\s*setup:/).find((b) => b.includes("zcp-devsup run")) || "";
        const hits = dev.split("\n").filter((l) => /\b(npm|pnpm|yarn|bun)\s+(i|install|ci|add)\b/.test(l) && !/\s(-g|--global)\b/.test(l));
        console.log(dev === "" ? "no-supervised-dev-setup" : hits.length ? "build-install: " + hits.join(" | ") : "no-build-install");'
      match: "^no-build-install"
    - service: appdev
      cmd: |-
        node -e '
        const fs = require("fs");
        const port = (5173).toString(16).toUpperCase().padStart(4, "0");
        const inodes = new Set(); let wide = false;
        for (const f of ["/proc/net/tcp", "/proc/net/tcp6"]) {
          let rows = []; try { rows = fs.readFileSync(f, "utf8").split("\n").slice(1); } catch {}
          for (const l of rows) {
            const c = l.trim().split(/\s+/); if (c[3] !== "0A") continue;
            const [addr, p] = (c[1] || "").split(":"); if (p !== port) continue;
            inodes.add(c[9]); if (/^0+$/.test(addr)) wide = true;
          }
        }
        const owners = [];
        for (const pid of fs.readdirSync("/proc").filter((d) => /^\d+$/.test(d))) {
          let fds = []; try { fds = fs.readdirSync("/proc/" + pid + "/fd"); } catch { continue; }
          for (const fd of fds) {
            let t = ""; try { t = fs.readlinkSync("/proc/" + pid + "/fd/" + fd); } catch {}
            const m = /^socket:\[(\d+)\]$/.exec(t); if (m && inodes.has(m[1]) && !owners.includes(pid)) owners.push(pid);
          }
        }
        const chain = (pid) => { const out = []; let p = pid;
          while (p && p !== "0" && p !== "1") { let st; try { st = fs.readFileSync("/proc/" + p + "/stat", "utf8"); } catch { break; }
            out.push(fs.readFileSync("/proc/" + p + "/cmdline", "utf8").split("\0").join(" ").trim()); p = st.slice(st.lastIndexOf(")") + 2).split(" ")[1]; }
          return out; };
        const supervised = owners.length > 0 && owners.every((p) => chain(p).some((c) => c.includes("zcp-devsup")));
        console.log(wide && supervised ? "supervised-dev-server-listens-wide" : "not: " + JSON.stringify({ wide, owners: owners.map(chain) }));'
      match: "^supervised-dev-server-listens-wide"
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

The repository on appdev is a project I want to work on. Get it running there the way it is meant to be developed, and give me the link to open it.
