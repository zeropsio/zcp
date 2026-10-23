// The development runner: serves src/ on 5173 and reloads on change. It binds
// loopback unless NOTES_DEV_HOST says otherwise, and refuses a request whose
// Host is not localhost or listed in NOTES_DEV_ALLOWED_HOSTS.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { watch } from "node:fs";
import lodash from "lodash";

const host = process.env.NOTES_DEV_HOST || "127.0.0.1";
const port = 5173;
const allowed = (process.env.NOTES_DEV_ALLOWED_HOSTS || "").split(",").map((h) => h.trim()).filter(Boolean);
let version = 0;
watch(new URL("../src/", import.meta.url), () => { version += 1; });

const hostAllowed = (header) => {
  const name = String(header || "").replace(/:\d+$/, "");
  return name === "localhost" || name === "127.0.0.1" || allowed.includes("*") || allowed.includes(name);
};

createServer(async (req, res) => {
  if (!hostAllowed(req.headers.host)) {
    res.writeHead(403, { "content-type": "text/plain" });
    res.end(`host ${req.headers.host} is not allowed; add it to NOTES_DEV_ALLOWED_HOSTS\n`);
    return;
  }
  const body = await readFile(new URL("../src/index.html", import.meta.url), "utf8");
  res.writeHead(200, { "content-type": "text/html" });
  res.end(`${body}\n<!-- ${lodash.escape("dev build " + version)} -->\n`);
}).listen(port, host, () => console.log(`dev runner on http://${host}:${port}`));
