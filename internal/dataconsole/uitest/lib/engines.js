"use strict";

// Engine oracles — SSH into the container and ask the real engine, never the UI,
// what actually happened. This is how a scenario tells "the toast said success"
// apart from "the write was actually applied" (I-1, spec-dataconsole.md §7.1).

const { execFileSync, execSync } = require("child_process");
const { loadConfig } = require("./config");

// shellQuote is POSIX single-quote composition: never interpolate a raw value
// into a shell/SQL command string (CLAUDE.md "Shell/SQL composition" discipline,
// applied here in JS instead of Go). Close the quote, emit an escaped single
// quote, reopen it.
function shellQuote(s) {
  return "'" + String(s).replace(/'/g, "'\"'\"'") + "'";
}

// sh runs a command on the LOCAL machine (this Mac), not the container.
function sh(cmd) {
  return execSync(cmd, { encoding: "utf8", timeout: 20000 }).trim();
}

// container runs one already-composed command string on the Zerops box over the
// non-interactive SSH alias. cmd is passed as a single argv element to `ssh`
// (execFileSync — no local shell involved), so only the REMOTE shell parses it;
// every value embedded in cmd must already be shellQuote()-d by the caller.
function container(cmd) {
  const cfg = loadConfig();
  const host = cfg.DC_SSH_HOST || "zcp";
  return execFileSync("ssh", ["-o", "ConnectTimeout=8", host, cmd], { encoding: "utf8", timeout: 20000 }).trim();
}

// psql runs one read (or write) statement against the postgres oracle. Creds come
// from local.config.json, never inlined at the call site.
function psql(sql) {
  const cfg = loadConfig();
  const cmd =
    "PGPASSWORD=" + shellQuote(cfg.DC_PG_PASSWORD) +
    " psql -h " + shellQuote(cfg.DC_PG_HOST) +
    " -U " + shellQuote(cfg.DC_PG_USER) +
    " -d " + shellQuote(cfg.DC_PG_DB) +
    " -tAc " + shellQuote(sql);
  return container(cmd);
}

// redis runs one redis-cli command against the valkey oracle. args is either an
// array of argv-style tokens (preferred — each one is individually shellQuote()-d,
// so a value containing spaces is safe) or a pre-split string.
function redis(args) {
  const cfg = loadConfig();
  const list = Array.isArray(args) ? args : String(args).split(/\s+/).filter(Boolean);
  const cmd =
    "redis-cli -h " + shellQuote(cfg.DC_REDIS_HOST) +
    " -a " + shellQuote(cfg.DC_REDIS_PASSWORD) +
    " --no-auth-warning " + list.map(shellQuote).join(" ");
  return container(cmd);
}

module.exports = { shellQuote, sh, container, psql, redis };
