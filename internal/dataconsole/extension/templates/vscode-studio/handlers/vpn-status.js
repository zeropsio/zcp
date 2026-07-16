"use strict";

// vpn-status handler (L-EV-2 / L-EV-3).
//
// Best-effort Tier-2 reachability probe — NOT a definitive "is VPN up" oracle.
// We read topology via ctx.runTransport(), pick the first managed service, and
// attempt a short TCP connect to its hostname on a port derived from its type.
// A successful connect means the managed service is reachable from this machine
// (the VPN is doing its job); a failure is reported as "could not reach", framed
// as a Tier-2 reachability hint with the `zcli vpn up <projectId>` affordance —
// NEVER a blanket "no VPN" claim (R-VPN-COPY / LG4). When there are no managed
// services we post connected:null with a note (nothing to reach).
//
// `net` is required lazily inside handle so module load stays side-effect free
// (R-FLEET — enumerateHandlers requires every handler at activation).

// Tier-2 reach targets the managed service's default wire port. Keyed off the
// service type's base name (type is like "postgresql@16" / "valkey@7").
function portForType(type) {
  const base = String(type || "").split("@")[0].toLowerCase();
  if (base.indexOf("postgres") >= 0) return 5432;
  if (base.indexOf("valkey") >= 0 || base.indexOf("redis") >= 0 || base.indexOf("keydb") >= 0) return 6379;
  if (base.indexOf("mariadb") >= 0 || base.indexOf("mysql") >= 0) return 3306;
  if (base.indexOf("meilisearch") >= 0) return 7700;
  return 80;
}

async function handle(msg, ctx) {
  const net = require("net");

  const t = await ctx.runTransport();
  const uiMap = (t && t.ok && t.uiMap) || { project: {}, services: [] };
  const projectId = (uiMap.project && uiMap.project.id) || "";
  const command = "zcli vpn up " + projectId;
  const services = uiMap.services || [];

  let managed = null;
  for (let i = 0; i < services.length; i++) {
    if (services[i] && services[i].category === "managed") {
      managed = services[i];
      break;
    }
  }

  if (!managed) {
    ctx.postMessage({
      type: "vpn-status-result",
      connected: null,
      projectId: projectId,
      command: command,
      managedHost: null,
      note:
        "No managed services in this project to reach — VPN (Tier 2) is only " +
        "needed to run your local app against a managed DB/cache.",
    });
    return;
  }

  const host = managed.hostname;
  const port = portForType(managed.type);

  let settled = false;
  const socket = new net.Socket();
  function finish(connected) {
    if (settled) return;
    settled = true;
    try {
      socket.destroy();
    } catch (_) {
      /* socket already gone */
    }
    ctx.postMessage({
      type: "vpn-status-result",
      connected: connected,
      projectId: projectId,
      command: command,
      managedHost: host,
    });
  }

  socket.setTimeout(1500);
  socket.once("connect", function () {
    finish(true);
  });
  socket.once("timeout", function () {
    finish(false);
  });
  socket.once("error", function () {
    finish(false);
  });
  try {
    socket.connect(port, host);
  } catch (_) {
    finish(false);
  }
}

module.exports = { type: "vpn-status", handle: handle };
